import { test, expect, type Page } from '@playwright/test';
import { kernelsFixture, systemStatsFixture } from './helpers/api-mocks';

// Страница «Сервисы»: опрос статуса ядер не должен зацикливаться после
// установки или ошибки, пустой ответ списка ядер не ломает страницу,
// а возраст статуса XKeen показывается тихим бейджем.

interface MockState {
  kernels: unknown;
  /** Поля data ответа /api/service/status поверх базовых. */
  serviceStatus?: Record<string, unknown>;
  /** Ответ POST /api/kernels/{k}/install; gate удерживает ответ до решения промиса. */
  install?: { status?: number; body?: string; gate?: Promise<void> };
  /** Очередь ответов GET status по ядру: по одному элементу на запрос, поверх записи в kernels. */
  statusQueue?: Record<string, Record<string, unknown>[]>;
  /** Активное ядро в ответе /api/capabilities (по умолчанию xray); читается при каждом запросе. */
  activeKernel?: string;
  /** Поля записи ядра после успешного POST /api/kernels/{k}/rollback. */
  rollbackPatch?: Record<string, Record<string, unknown>>;
}

interface Counters {
  kernelsList: number;
  kernelStatus: Record<string, number>;
  installPosts: number;
  rollbackPosts: string[];
}

async function mockRoutes(page: Page, state: MockState): Promise<Counters> {
  const counters: Counters = {
    kernelsList: 0,
    kernelStatus: {},
    installPosts: 0,
    rollbackPosts: []
  };

  await page.addInitScript(() => {
    window.localStorage.setItem('lang', 'ru');
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  await page.route('**/api/**', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const json = (body: unknown) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });

    if (path === '/api/auth/me') {
      return json({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' });
    }
    if (path === '/api/capabilities') {
      return json({
        success: true,
        data: {
          kernels: {
            xray: { installed: true, version: '1.8.4', channel: 'stable' },
            mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
          },
          active_kernel: state.activeKernel ?? 'xray',
          xkeen_installed: true,
          mihomo: { reachable: true, process_running: false, api_reachable: false }
        }
      });
    }
    if (path === '/api/settings') return json({ success: true, data: { dev_mode: false } });
    if (path === '/api/version') return json({ success: true, data: 'v0.25.0' });
    if (path === '/api/service/restart-log') return json([]);
    if (path === '/api/service/status') {
      return json({
        success: true,
        data: {
          is_running: true,
          active_kernel: 'xray',
          pid: 1234,
          uptime: '1h',
          binary_path: '/opt/sbin/xkeen',
          raw: 'Xray-core (running)\nXKeen is running',
          xkeen_installed: true,
          xkeen_installer_available: true,
          xkeen_setup_incomplete: false,
          ...state.serviceStatus
        }
      });
    }
    if (path === '/api/system/stats') return json(systemStatsFixture());

    const installMatch = path.match(/^\/api\/kernels\/([^/]+)\/install$/);
    if (installMatch && req.method() === 'POST') {
      counters.installPosts++;
      if (state.install?.gate) await state.install.gate;
      const status = state.install?.status ?? 200;
      if (status !== 200) {
        return route.fulfill({
          status,
          contentType: 'text/plain',
          body: state.install?.body ?? 'error'
        });
      }
      return json({ status: 'downloading', stage: 'starting' });
    }

    const rollbackMatch = path.match(/^\/api\/kernels\/([^/]+)\/rollback$/);
    if (rollbackMatch && req.method() === 'POST') {
      const name = rollbackMatch[1];
      counters.rollbackPosts.push(name);
      const list = Array.isArray(state.kernels) ? (state.kernels as { name: string }[]) : [];
      const entry = list.find((k) => k.name === name);
      const patch = state.rollbackPatch?.[name];
      if (entry && patch) Object.assign(entry, patch);
      return json({ status: 'rolled_back' });
    }

    const statusMatch = path.match(/^\/api\/kernels\/([^/]+)\/status$/);
    if (statusMatch && req.method() === 'GET') {
      const name = statusMatch[1];
      counters.kernelStatus[name] = (counters.kernelStatus[name] ?? 0) + 1;
      const list = Array.isArray(state.kernels) ? (state.kernels as { name: string }[]) : [];
      const patch = state.statusQueue?.[name]?.shift();
      const entry = list.find((k) => k.name === name);
      if (patch && entry) Object.assign(entry, patch);
      return json({ success: true, data: entry ?? {} });
    }
    if (path === '/api/kernels' && req.method() === 'GET') {
      counters.kernelsList++;
      return json({ success: true, data: state.kernels });
    }
    return json({ success: true, data: {} });
  });

  return counters;
}

test.describe('Services page — kernel status polling', () => {
  test('не зацикливает опрос для ядер в статусах done и failed', async ({ page }) => {
    const counters = await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        mihomo: { status: 'done', message: 'Updated to 1.18.0' },
        xray: { status: 'failed', message: 'download failed' }
      })
    });

    await page.clock.install();
    await page.goto('/#/services');
    await expect(page.locator('.updates-card')).toBeVisible();
    await expect(
      page.locator('.update-item', { hasText: 'Xray' }).locator('.status-badge')
    ).toBeVisible();

    await page.clock.runFor(30_000);
    // Короткая реальная пауза: ответы на запросы последнего тика доходят в реальном времени
    await page.waitForTimeout(300);

    expect(counters.kernelsList).toBeLessThanOrEqual(10);
    const statusCalls = Object.values(counters.kernelStatus).reduce((a, b) => a + b, 0);
    expect(statusCalls).toBeLessThanOrEqual(2);
  });

  for (const [label, data] of [
    ['пустой объект', {}],
    ['null', null]
  ] as const) {
    test(`пустой ответ ядер (${label}) не ломает страницу`, async ({ page }) => {
      const errors: string[] = [];
      page.on('pageerror', (e) => errors.push(e.message));
      page.on('console', (m) => {
        if (m.type() === 'error') errors.push(m.text());
      });
      await mockRoutes(page, { kernels: data });

      await page.goto('/#/services');
      await expect(page.locator('.updates-card')).toBeVisible();
      await expect(page.locator('.update-item')).toHaveCount(2);
      await expect.poll(() => errors.filter((e) => e.includes('forEach'))).toEqual([]);
    });
  }

  test('статус checking запускает опрос и останавливает его после idle', async ({ page }) => {
    const state: MockState = {
      kernels: kernelsFixture('xray', { xray: { status: 'checking' } })
    };
    const counters = await mockRoutes(page, state);
    // Первый ответ статуса сообщает, что проверка завершилась
    await page.route('**/api/kernels/xray/status', async (route) => {
      counters.kernelStatus['xray'] = (counters.kernelStatus['xray'] ?? 0) + 1;
      state.kernels = kernelsFixture('xray');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: (state.kernels as { name: string }[])[0] })
      });
    });

    await page.clock.install();
    await page.goto('/#/services');
    await expect(page.locator('.updates-card')).toBeVisible();
    await expect.poll(() => counters.kernelStatus['xray'] ?? 0).toBeGreaterThanOrEqual(1);

    await page.clock.runFor(10_000);
    await page.waitForTimeout(300);
    expect(counters.kernelStatus['xray']).toBeLessThanOrEqual(2);
  });
});

test.describe('Services page — status age badge', () => {
  const badge = (page: Page) => page.getByTestId('status-stale-badge');

  test('возраст 45 с показывает тихий бейдж «данные от HH:MM»', async ({ page }) => {
    await mockRoutes(page, {
      kernels: kernelsFixture('xray'),
      serviceStatus: { stale: true, age_seconds: 45 }
    });
    await page.goto('/#/services');
    await expect(badge(page)).toBeVisible();
    await expect(badge(page)).toContainText(/^\s*(данные от|as of) \d{2}:\d{2}\s*$/);
  });

  test('граница: 30 с и отсутствие возраста бейджа не показывают', async ({ page }) => {
    const state: MockState = {
      kernels: kernelsFixture('xray'),
      serviceStatus: { stale: true, age_seconds: 30 }
    };
    await mockRoutes(page, state);
    await page.goto('/#/services');
    await expect(page.locator('.hero-card')).toBeVisible();
    await expect(badge(page)).toHaveCount(0);

    // Без поля возраста (свежий ответ) бейджа тоже нет
    state.serviceStatus = { stale: false };
    await page.reload();
    await expect(page.locator('.hero-card')).toBeVisible();
    await expect(badge(page)).toHaveCount(0);
  });

  test('холодный кэш показывает «Неизвестно», а не «остановлено»', async ({ page }) => {
    await mockRoutes(page, {
      kernels: kernelsFixture('xray', { xray: { process_status: 'stopped' } }),
      serviceStatus: { is_running: false, stale: true, raw: '' }
    });
    await page.goto('/#/services');
    const hero = page.locator('.hero-status');
    await expect(hero.getByTestId('status-xkeen-unknown')).toContainText(/Неизвестно|Unknown/);
    await expect(hero).not.toContainText(/остановлен|stopped/i);
    await expect(badge(page)).toHaveCount(0);
  });
});

test.describe('Services page — kernel install progress', () => {
  const xrayRow = (page: Page) => page.locator('.update-item', { hasText: 'Xray' });
  const updatable = () =>
    kernelsFixture('xray', {
      xray: { current_version: '26.9.8', latest_version: '26.9.9', has_update: true }
    });

  test('«Старт…» сразу после клика, затем этапы и переведённый итог с одним тостом', async ({
    page
  }) => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => (release = resolve));
    const state: MockState = {
      kernels: updatable(),
      install: { gate },
      statusQueue: {
        xray: [
          { status: 'downloading', stage: 'downloading' },
          { status: 'installing', stage: 'extracting' },
          { status: 'installing', stage: 'replacing' },
          {
            status: 'done',
            stage: '',
            has_update: false,
            current_version: '26.9.9',
            result_kind: 'updated',
            result_version: '26.9.9',
            message: 'Updated to 26.9.9'
          }
        ]
      }
    };
    const counters = await mockRoutes(page, state);
    await page.addInitScript(() => {
      const seen: string[] = [];
      (window as unknown as { __toasts: string[] }).__toasts = seen;
      new MutationObserver((records) => {
        for (const r of records) {
          r.addedNodes.forEach((n) => {
            if (!(n instanceof HTMLElement)) return;
            const toasts = n.matches('.toast') ? [n] : Array.from(n.querySelectorAll('.toast'));
            toasts.forEach((el) => {
              if (el.textContent?.includes('Обновлено до v26.9.9')) seen.push(el.textContent);
            });
          });
        }
      }).observe(document, { childList: true, subtree: true });
    });
    await page.clock.install();
    await page.goto('/#/services');
    const button = xrayRow(page).getByRole('button', { name: 'Обновить' });
    await expect(button).toBeVisible();

    await button.click();
    // Ответ POST ещё не пришёл: кнопка уже занята и подписана
    const busy = xrayRow(page).locator('.update-actions .btn-primary');
    await expect(busy).toHaveText('Старт…');
    await expect(busy).toBeDisabled();
    expect(counters.kernelStatus['xray'] ?? 0).toBe(0);

    release();
    await expect(busy).toHaveText('Скачивание…');
    await page.clock.runFor(2_000);
    await expect(busy).toHaveText('Распаковка…');
    await page.clock.runFor(2_000);
    await expect(busy).toHaveText('Замена…');
    await page.clock.runFor(2_000);

    const result = page.getByTestId('kernel-result-xray');
    await expect(result).toContainText('Обновлено до v26.9.9');
    await expect(page.locator('.toast', { hasText: 'Обновлено до v26.9.9' })).toHaveCount(1);
    await expect(page.getByText('Updated to')).toHaveCount(0);

    // Дальнейшие опросы не дают повторного тоста (тосты считаются по всем кадрам)
    await page.clock.runFor(6_000);
    await page.waitForTimeout(300);
    const shown = await page.evaluate(
      () => (window as unknown as { __toasts: string[] }).__toasts.length
    );
    expect(shown).toBe(1);
  });

  for (const [kind, text] of [
    ['installed', 'Установлено v26.9.9'],
    ['reinstalled', 'Переустановлено v26.9.9']
  ] as const) {
    test(`итог ${kind} переводится по коду`, async ({ page }) => {
      await mockRoutes(page, {
        kernels: kernelsFixture('xray', {
          xray: {
            current_version: '26.9.9',
            latest_version: '26.9.9',
            status: 'done',
            result_kind: kind,
            result_version: '26.9.9',
            message: 'Installed 26.9.9'
          }
        })
      });
      await page.goto('/#/services');
      await expect(page.getByTestId('kernel-result-xray')).toContainText(text);
      await expect(page.getByText('Installed 26.9.9')).toHaveCount(0);
    });
  }

  test('ошибка POST 500 возвращает кнопку и показывает тост без опроса статуса', async ({
    page
  }) => {
    const counters = await mockRoutes(page, {
      kernels: updatable(),
      install: { status: 500, body: 'download failed' }
    });
    await page.clock.install();
    await page.goto('/#/services');
    const button = xrayRow(page).getByRole('button', { name: 'Обновить' });
    await button.click();

    await expect(page.locator('.toast--error', { hasText: 'download failed' })).toBeVisible();
    await expect(xrayRow(page).getByRole('button', { name: 'Обновить' })).toBeEnabled();
    await page.clock.runFor(6_000);
    await page.waitForTimeout(300);
    expect(counters.installPosts).toBe(1);
    expect(counters.kernelStatus['xray'] ?? 0).toBe(0);
  });

  test('ответ 409 показывает тост и не запускает опрос статуса', async ({ page }) => {
    const counters = await mockRoutes(page, {
      kernels: updatable(),
      install: { status: 409, body: 'install already in progress' }
    });
    await page.clock.install();
    await page.goto('/#/services');
    await xrayRow(page).getByRole('button', { name: 'Обновить' }).click();

    await expect(
      page.locator('.toast--error', { hasText: 'install already in progress' })
    ).toBeVisible();
    await expect(xrayRow(page).getByRole('button', { name: 'Обновить' })).toBeEnabled();
    await page.clock.runFor(4_000);
    await page.waitForTimeout(300);
    expect(counters.kernelStatus['xray'] ?? 0).toBe(0);
  });

  test('ошибка проверки релиза по error_kind показана по-русски с причиной', async ({ page }) => {
    await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        xray: {
          current_version: 'not installed',
          latest_version: '',
          status: 'failed',
          error_kind: 'release_lookup_failed',
          message: 'Release lookup failed: github api: HTTP 403'
        }
      })
    });
    await page.goto('/#/services');
    const hint = xrayRow(page).locator('.update-hint-error');
    await expect(hint).toContainText('HTTP 403');
    await expect(hint).not.toContainText('Release lookup failed');
  });

  test('ошибка без известного error_kind показывает прежний текст message', async ({ page }) => {
    await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        xray: { status: 'failed', message: 'download failed' }
      })
    });
    await page.goto('/#/services');
    await expect(xrayRow(page).locator('.update-hint-error')).toHaveText('download failed');
  });
});

test.describe('Services page — rollback label', () => {
  const rollbackButton = (page: Page, name: string) => page.getByTestId(`rollback-${name}`);

  test('откат подписан версией резервной копии у обоих ядер, aria-подпись начинается с «Откатить»', async ({
    page
  }) => {
    await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        xray: { current_version: '26.9.8', has_backup: true, backup_version: '26.3.27' },
        mihomo: { has_backup: true, backup_version: '1.17.0' }
      })
    });
    await page.goto('/#/services');
    await expect(rollbackButton(page, 'xray')).toHaveText('Откатить на v26.3.27');
    await expect(rollbackButton(page, 'xray')).toHaveAttribute('aria-label', /^Откатить/);
    await expect(rollbackButton(page, 'mihomo')).toHaveText('Откатить на v1.17.0');
    await expect(rollbackButton(page, 'mihomo')).toHaveAttribute('aria-label', /^Откатить/);
  });

  test('без backup_version — «Откатить», без бэкапа — кнопки нет', async ({ page }) => {
    await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        xray: { has_backup: true },
        mihomo: { has_backup: false }
      })
    });
    await page.goto('/#/services');
    await expect(rollbackButton(page, 'xray')).toHaveText('Откатить');
    await expect(page.locator('.update-item', { hasText: 'Mihomo' })).toBeVisible();
    await expect(rollbackButton(page, 'mihomo')).toHaveCount(0);
  });

  test('бэкап той же версии, что установлена, кнопку не показывает', async ({ page }) => {
    await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        xray: { current_version: '26.9.8', has_backup: true, backup_version: '26.9.8' }
      })
    });
    await page.goto('/#/services');
    await expect(page.locator('.update-item', { hasText: 'Xray' })).toBeVisible();
    await expect(rollbackButton(page, 'xray')).toHaveCount(0);
  });

  test('подтверждённый откат шлёт POST и показывает итог «Откачено на vX»', async ({ page }) => {
    const counters = await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        xray: { current_version: '26.9.8', has_backup: true, backup_version: '26.3.27' }
      }),
      rollbackPatch: {
        xray: {
          current_version: '26.3.27',
          has_backup: false,
          backup_version: '',
          status: 'done',
          result_kind: 'rolled_back',
          result_version: '26.3.27',
          message: 'Rolled back'
        }
      }
    });
    page.on('dialog', (dialog) => dialog.accept());
    await page.goto('/#/services');
    await rollbackButton(page, 'xray').click();

    await expect(page.getByTestId('kernel-result-xray')).toContainText('Откачено на v26.3.27');
    await expect(rollbackButton(page, 'xray')).toHaveCount(0);
    expect(counters.rollbackPosts).toEqual(['xray']);
    await expect(page.getByText('Rolled back')).toHaveCount(0);
  });

  test('отклонённое подтверждение не шлёт POST', async ({ page }) => {
    const counters = await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        xray: { current_version: '26.9.8', has_backup: true, backup_version: '26.3.27' }
      })
    });
    page.on('dialog', (dialog) => dialog.dismiss());
    await page.goto('/#/services');
    await rollbackButton(page, 'xray').click();
    await page.waitForTimeout(300);
    expect(counters.rollbackPosts).toEqual([]);
  });
});

test.describe('Services page — nav lock during kernel install', () => {
  const XRAY_GROUPS = 4;
  const groups = (page: Page) => page.locator('.sidebar-nav .nav-group');
  const restart = (page: Page) => page.getByRole('button', { name: 'Перезапустить' });
  const holdingStatus = { status: 'downloading', stage: 'downloading' };

  test('пока идёт установка, меню не перестраивается; после done обновляется один раз', async ({
    page
  }) => {
    const state: MockState = {
      kernels: kernelsFixture('xray', {
        xray: { current_version: '26.9.8', latest_version: '26.9.9', has_update: true }
      }),
      // Пять «downloading» подряд, затем done
      statusQueue: {
        xray: [
          ...Array.from({ length: 6 }, () => ({ ...holdingStatus })),
          {
            status: 'done',
            stage: '',
            has_update: false,
            current_version: '26.9.9',
            result_kind: 'updated',
            result_version: '26.9.9'
          }
        ]
      }
    };
    await mockRoutes(page, state);
    await page.clock.install();
    await page.goto('/#/services');
    await expect(groups(page)).toHaveCount(XRAY_GROUPS);

    await page
      .locator('.update-item', { hasText: 'Xray' })
      .getByRole('button', { name: 'Обновить' })
      .click();
    await expect(
      page.locator('.update-item', { hasText: 'Xray' }).locator('.update-actions .btn-primary')
    ).toHaveText('Скачивание…');

    // Ядро сменилось на сервере, capabilities запрошены во время установки
    state.activeKernel = 'mihomo';
    await restart(page).click();
    await page.clock.runFor(4_000);
    await page.waitForTimeout(300);
    await expect(groups(page)).toHaveCount(XRAY_GROUPS);
    await expect(page.locator('a[href="#/proxies"]')).toHaveCount(0);

    // Терминальный статус снимает замок: меню обновляется
    await page.clock.runFor(10_000);
    await expect(page.getByTestId('kernel-result-xray')).toBeVisible();
    await expect(groups(page)).toHaveCount(XRAY_GROUPS + 2);
    await expect(page.locator('a[href="#/proxies"]')).toBeVisible();
  });

  test('ошибка POST install снимает замок', async ({ page }) => {
    const state: MockState = {
      kernels: kernelsFixture('xray', {
        xray: { current_version: '26.9.8', latest_version: '26.9.9', has_update: true }
      }),
      install: { status: 500, body: 'download failed' }
    };
    await mockRoutes(page, state);
    await page.clock.install();
    await page.goto('/#/services');
    await expect(groups(page)).toHaveCount(XRAY_GROUPS);

    state.activeKernel = 'mihomo';
    await page
      .locator('.update-item', { hasText: 'Xray' })
      .getByRole('button', { name: 'Обновить' })
      .click();
    await expect(page.locator('.toast--error', { hasText: 'download failed' })).toBeVisible();
    // Замок снят: последнее снятие тихо перечитывает capabilities
    await expect(groups(page)).toHaveCount(XRAY_GROUPS + 2);
  });

  test('уход со страницы во время установки снимает замок', async ({ page }) => {
    const state: MockState = {
      kernels: kernelsFixture('xray', {
        xray: { current_version: '26.9.8', latest_version: '26.9.9', has_update: true }
      }),
      statusQueue: { xray: Array.from({ length: 20 }, () => ({ ...holdingStatus })) }
    };
    await mockRoutes(page, state);
    await page.clock.install();
    await page.goto('/#/services');
    await expect(groups(page)).toHaveCount(XRAY_GROUPS);

    await page
      .locator('.update-item', { hasText: 'Xray' })
      .getByRole('button', { name: 'Обновить' })
      .click();
    await expect(
      page.locator('.update-item', { hasText: 'Xray' }).locator('.update-actions .btn-primary')
    ).toHaveText('Скачивание…');
    state.activeKernel = 'mihomo';
    await page.locator('a[href="#/dashboard"]').first().click();

    await expect(groups(page)).toHaveCount(XRAY_GROUPS + 2);
  });

  test('проверка обновлений (checking) замок не берёт', async ({ page }) => {
    const state: MockState = {
      kernels: kernelsFixture('xray', { xray: { status: 'checking' } }),
      statusQueue: {
        xray: Array.from({ length: 3 }, () => ({ status: 'checking' }))
      }
    };
    await mockRoutes(page, state);
    await page.clock.install();
    await page.goto('/#/services');
    await expect(groups(page)).toHaveCount(XRAY_GROUPS);
    await expect(page.getByTestId('kernel-checking-hint-xray')).toBeVisible();

    // Смена ядра при идущей проверке доходит до меню сразу: замка нет
    state.activeKernel = 'mihomo';
    await restart(page).click();
    await expect(groups(page)).toHaveCount(XRAY_GROUPS + 2);
  });
});
