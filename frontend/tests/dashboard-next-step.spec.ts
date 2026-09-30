import { test, expect, type Page } from '@playwright/test';
import { kernelsFixture, systemStatsFixture } from './helpers/api-mocks';

// Дашборд: понятное состояние XKeen и одна подсказка следующего шага
// («лестница») в панели проблем. Состояние мока меняется между кейсами.

interface MockState {
  /** capabilities.xkeen_installed */
  xkeenInstalled: boolean;
  /** kernels.xray.installed / kernels.mihomo.installed */
  xrayInstalled: boolean;
  mihomoInstalled: boolean;
  activeKernel: 'xray' | 'mihomo' | 'none';
  /** Поля data ответа /api/service/status */
  service: Record<string, unknown>;
  /** Ответ /api/config/preflight (без конверта) */
  preflight: Record<string, unknown>;
  /** Если задан, /api/config/preflight отвечает этим кодом ошибки */
  preflightStatus?: number;
  /** Ответ /api/system/stats целиком */
  stats: unknown;
  /** Ответ /api/version (поле data) */
  version: { version: string; panel_version: string };
}

interface Counters {
  preflight: string[];
}

function baseState(overrides: Partial<MockState> = {}): MockState {
  return {
    xkeenInstalled: true,
    xrayInstalled: true,
    mihomoInstalled: false,
    activeKernel: 'xray',
    service: {
      is_running: false,
      xkeen_installed: true,
      xkeen_setup_incomplete: false
    },
    preflight: { valid: true, errors: [], warnings: [] },
    stats: systemStatsFixture(),
    version: { version: '2.0 Beta', panel_version: 'v0.25.0' },
    ...overrides
  };
}

async function mockRoutes(page: Page, state: MockState): Promise<Counters> {
  const counters: Counters = { preflight: [] };

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
    const url = new URL(req.url());
    const path = url.pathname;
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
            xray: { installed: state.xrayInstalled, version: '1.8.4', channel: 'stable' },
            mihomo: { installed: state.mihomoInstalled, version: '1.18.0', channel: 'stable' }
          },
          active_kernel: state.activeKernel,
          xkeen_installed: state.xkeenInstalled,
          mihomo: { reachable: false, process_running: false, api_reachable: false }
        }
      });
    }
    if (path === '/api/settings') return json({ success: true, data: { dev_mode: false } });
    if (path === '/api/version') return json({ success: true, data: state.version });
    if (path === '/api/service/restart-log') return json([]);
    if (path === '/api/service/status') return json({ success: true, data: state.service });
    if (path === '/api/config/preflight') {
      counters.preflight.push(url.searchParams.get('kernel') ?? '');
      if (state.preflightStatus) {
        return route.fulfill({
          status: state.preflightStatus,
          contentType: 'application/json',
          body: JSON.stringify({ success: false, error: 'mock failure' })
        });
      }
      return json(state.preflight);
    }
    if (path === '/api/kernels' && req.method() === 'GET') {
      return json({
        success: true,
        data: kernelsFixture('xray', {
          xray: { process_status: 'stopped' },
          mihomo: { process_status: 'stopped' }
        })
      });
    }
    if (path === '/api/system/stats') return json(state.stats);
    if (path === '/api/subscriptions') return json([]);
    return json({ success: true, data: {} });
  });

  return counters;
}

const step = (page: Page) => page.locator('[data-step]');
const xkeenCard = (page: Page) =>
  page
    .locator('.service-card')
    .filter({ has: page.locator('.service-name', { hasText: /^XKeen$/ }) });

test.describe('Dashboard — лестница следующего шага', () => {
  test('нет XKeen: шаг «XKeen не установлен», карточка «Не установлено»', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({
        xkeenInstalled: false,
        xrayInstalled: false,
        service: { is_running: false, xkeen_installed: false }
      })
    );
    await page.goto('/#/dashboard');

    const missing = page.getByTestId('problem-xkeen-missing');
    await expect(missing).toBeVisible();
    await expect(missing).toHaveAttribute('data-step', 'install_xkeen');
    await expect(missing).toContainText('XKeen не установлен');
    await expect(step(page)).toHaveCount(1);
    await expect(xkeenCard(page)).toContainText('Не установлено');
    await expect(xkeenCard(page)).not.toContainText('Неизвестно');
  });

  test('прерванная настройка: шаг «Установка XKeen не завершена»', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({
        service: { is_running: false, xkeen_installed: true, xkeen_setup_incomplete: true }
      })
    );
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toBeVisible();
    await expect(item).toHaveAttribute('data-step', 'finish_xkeen_setup');
    await expect(item).toContainText('Установка XKeen не завершена');
    await expect(step(page)).toHaveCount(1);
  });

  test('нет ядер: шаг «Прокси ядра не установлены»', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({ xrayInstalled: false, mihomoInstalled: false, activeKernel: 'none' })
    );
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toBeVisible();
    await expect(item).toHaveAttribute('data-step', 'install_kernel');
    await expect(item).toContainText('Прокси ядра не установлены');
    await expect(step(page)).toHaveCount(1);
  });

  test('нет подключений в конфиге xray: шаг «Настройте конфигурацию ядра»', async ({ page }) => {
    const counters = await mockRoutes(
      page,
      baseState({
        preflight: {
          valid: true,
          errors: [],
          warnings: [{ code: 'no_real_outbounds', message: 'no outbounds' }]
        }
      })
    );
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toBeVisible();
    await expect(item).toHaveAttribute('data-step', 'configure');
    await expect(item).toContainText('Настройте конфигурацию ядра');
    await expect(item).toContainText('Xray');
    await expect(step(page)).toHaveCount(1);
    expect(counters.preflight).toEqual(['xray']);

    await item.getByRole('button').click();
    await expect(page).toHaveURL(/#\/constructor/);
  });

  test('всё готово, остановлено: шаг «Запустите XKeen», карточка «Остановлено»', async ({
    page
  }) => {
    await mockRoutes(page, baseState());
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toBeVisible();
    await expect(item).toHaveAttribute('data-step', 'start');
    await expect(item).toContainText('Запустите XKeen');
    await expect(step(page)).toHaveCount(1);
    await expect(xkeenCard(page)).toContainText('Остановлено');
    await expect(xkeenCard(page)).not.toContainText('Неизвестно');

    await item.getByRole('button').click();
    await expect(page).toHaveURL(/#\/services/);
  });

  test('preflight отвечает 500: шага «Запустите» нет, пока конфигурация неизвестна', async ({
    page
  }) => {
    const counters = await mockRoutes(page, baseState({ preflightStatus: 500 }));
    await page.goto('/#/dashboard');

    await expect(xkeenCard(page)).toContainText('Остановлено');
    await expect.poll(() => counters.preflight.length).toBeGreaterThan(0);
    await expect(step(page)).toHaveCount(0);
    await expect(page.getByTestId('problem-next-step')).toHaveCount(0);
  });

  test('после создания конфигурации шаг «Настройте» сменяется на «Запустите» за один-два опроса', async ({
    page
  }) => {
    // Неготовый preflight кэшируется на 10 с, а не на минуту: шаг обновляется на втором опросе
    test.setTimeout(60_000);
    const state = baseState({
      preflight: {
        valid: true,
        errors: [],
        warnings: [{ code: 'no_real_outbounds', message: 'no outbounds' }]
      }
    });
    await mockRoutes(page, state);
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toHaveAttribute('data-step', 'configure');

    state.preflight = { valid: true, errors: [], warnings: [] };
    await expect(item).toHaveAttribute('data-step', 'start', { timeout: 30_000 });
    await expect(item).toContainText('Запустите XKeen');
  });

  test('запущено: шага нет', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({
        service: { is_running: true, xkeen_installed: true, xkeen_setup_incomplete: false }
      })
    );
    await page.goto('/#/dashboard');

    await expect(xkeenCard(page)).toContainText('Работает');
    await expect(step(page)).toHaveCount(0);
    await expect(page.getByTestId('problem-next-step')).toHaveCount(0);
  });

  test('холодный кэш статуса: «Неизвестно», шагов «Настройте»/«Запустите» нет', async ({
    page
  }) => {
    const counters = await mockRoutes(
      page,
      baseState({
        service: { is_running: false, stale: true, xkeen_installed: true }
      })
    );
    await page.goto('/#/dashboard');

    await expect(xkeenCard(page)).toContainText('Неизвестно');
    await expect(step(page)).toHaveCount(0);
    expect(counters.preflight).toEqual([]);
  });
});

test.describe('Dashboard — версия XKeen, возраст статуса, неполная статистика', () => {
  const versionValue = (page: Page) =>
    page
      .locator('.info-row')
      .filter({ has: page.locator('.lbl', { hasText: /^Версия XKeen$/ }) })
      .locator('.val');

  test('XKeen не установлен, версия unknown: «не установлен»', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({
        xkeenInstalled: false,
        service: { is_running: false, xkeen_installed: false },
        version: { version: 'unknown', panel_version: 'v0.25.0' }
      })
    );
    await page.goto('/#/dashboard');
    await expect(versionValue(page)).toHaveText('не установлен');
  });

  test('XKeen установлен, версия unknown: «—»', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({ version: { version: 'unknown', panel_version: 'v0.25.0' } })
    );
    await page.goto('/#/dashboard');
    await expect(versionValue(page)).toHaveText('—');
  });

  test('XKeen установлен, версия известна: показывается как есть', async ({ page }) => {
    await mockRoutes(page, baseState());
    await page.goto('/#/dashboard');
    await expect(versionValue(page)).toHaveText('2.0 Beta');
  });

  for (const [label, service, visible] of [
    ['возраст 45 с', { age_seconds: 45 }, true],
    ['возраст 30 с', { age_seconds: 30 }, false],
    ['возраста нет', {}, false]
  ] as const) {
    test(`бейдж «данные от»: ${label} — ${visible ? 'виден' : 'нет'}`, async ({ page }) => {
      await mockRoutes(
        page,
        baseState({
          service: {
            is_running: true,
            xkeen_installed: true,
            xkeen_setup_incomplete: false,
            ...service
          }
        })
      );
      await page.goto('/#/dashboard');
      await expect(xkeenCard(page)).toContainText('Работает');
      const badge = page.getByTestId('status-stale-badge');
      if (visible) {
        await expect(badge).toBeVisible();
        await expect(badge).toContainText(/^\s*данные от \d{2}:\d{2}/);
      } else {
        await expect(badge).toHaveCount(0);
      }
    });
  }

  test('системная статистика без load не роняет дашборд', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(e.message));
    await mockRoutes(page, baseState({ stats: { success: true, data: {} } }));
    await page.goto('/#/dashboard');
    await expect(xkeenCard(page)).toBeVisible();
    // Дать пройти нескольким опросам статистики
    await page.waitForTimeout(600);
    expect(errors.filter((e) => e.includes("reading '0'"))).toEqual([]);
    expect(errors).toEqual([]);
  });

  test('валидная статистика по-прежнему заполняет «О системе»', async ({ page }) => {
    await mockRoutes(page, baseState());
    await page.goto('/#/dashboard');
    await expect(page.locator('.info-row', { hasText: 'keenetic' }).first()).toBeVisible();
  });
});
