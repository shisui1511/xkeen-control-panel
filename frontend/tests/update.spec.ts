import { test, expect, type Page } from '@playwright/test';

test.use({ locale: 'ru-RU' });

const REPO = 'https://github.com/shisui1511/xkeen-control-panel';

const RC_NOTES = `> 🧪 **Release candidate v0.29.0**

### 🚀 Новые возможности

- автоматический выпуск release candidate [\`a4878ff\`](${REPO}/commit/a4878ff9)

### 🐛 Исправления

- сжатие \`.gz\` при скачивании вместо UPX [\`82e1755\`](${REPO}/commit/82e17554)

---

## 📥 Установка и обновление

- этот пункт не показывается
`;

interface MockOptions {
  check?: unknown;
  checkStatus?: number;
  backups?: unknown[];
  state?: Record<string, unknown>;
  version?: string;
  onSettings?: (body: Record<string, unknown>) => void;
}

const BASE_STATE = {
  channel: 'beta',
  current_version: '0.28.0',
  has_update: false,
  prerelease: false,
  auto_check: true,
  auto_install: false,
  install_window: '03:00-05:00'
};

async function mockApi(page: Page, opts: MockOptions = {}) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  const json = (data: unknown, status = 200) => ({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data)
  });

  await page.route('**/api/**', async (route) => {
    const url = route.request().url();

    if (url.includes('/api/auth/me')) {
      await route.fulfill(
        json({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' })
      );
    } else if (url.includes('/api/update/channel')) {
      await route.fulfill(json({ channel: 'beta' }));
    } else if (url.includes('/api/version')) {
      await route.fulfill(json({ panel_version: opts.version ?? 'v0.28.0' }));
    } else if (url.includes('/api/update/status')) {
      await route.fulfill(json({ status: 'idle', message: '', progress: 0 }));
    } else if (url.includes('/api/update/check')) {
      if (opts.checkStatus && opts.checkStatus >= 400) {
        await route.fulfill(
          json({ success: false, error: 'GitHub API: 403 rate limit' }, opts.checkStatus)
        );
      } else {
        await route.fulfill(json({ success: true, data: opts.check ?? {} }));
      }
    } else if (url.includes('/api/update/backups')) {
      await route.fulfill(json({ success: true, data: opts.backups ?? [] }));
    } else if (url.includes('/api/system/stats')) {
      // Дашборд читает load[0], uptime.days и т. п. — пустой объект уронил бы страницу
      await route.fulfill(
        json({
          memory: { total: 512 << 20, used: 180 << 20, free: 332 << 20 },
          disk: { total: 1 << 30, used: 300 << 20, free: 724 << 20 },
          ssl_cert_days: 45,
          load: [0.15, 0.22, 0.18],
          uptime: { seconds: 86400, days: 1, hours: 2, minutes: 30 },
          go_runtime: {
            goroutines: 28,
            heap_alloc: 12 << 20,
            heap_sys: 24 << 20,
            num_gc: 14,
            go_version: 'go1.22.0',
            gomaxprocs: 4,
            goarch: 'arm64'
          },
          hostname: 'Keenetic-Router',
          wan_status: 'online',
          dns_servers: ['1.1.1.1'],
          invalid_config: false
        })
      );
    } else if (url.includes('/api/update/state')) {
      await route.fulfill(json({ success: true, data: { ...BASE_STATE, ...opts.state } }));
    } else if (url.includes('/api/update/settings')) {
      const body = JSON.parse(route.request().postData() || '{}');
      opts.onSettings?.(body);
      await route.fulfill(json({ success: true, data: { ...BASE_STATE, ...opts.state, ...body } }));
    } else {
      await route.fulfill(json({ success: true, data: {} }));
    }
  });
}

async function openUpdatesTab(page: Page) {
  await page.goto('/#/settings');
  const updatesTab = page
    .locator('[role="tab"]:has-text("Обновления"), .tab-btn:has-text("Обновления")')
    .first();
  await expect(updatesTab).toBeVisible({ timeout: 5000 });
  await updatesTab.click();
}

test.describe('Settings updates tab', () => {
  test('shows the available release candidate with parsed release notes', async ({ page }) => {
    await mockApi(page, {
      check: {
        current_version: '0.28.0',
        latest_version: '0.29.0-rc.2',
        has_update: true,
        channel: 'beta',
        prerelease: true,
        download_size: 7_300_000,
        published_at: '2026-09-25T19:30:00Z',
        releases: [
          {
            version: '0.29.0-rc.2',
            prerelease: true,
            published_at: '2026-09-25T19:30:00Z',
            url: `${REPO}/releases/tag/v0.29.0-rc.2`,
            body: RC_NOTES
          },
          { version: '0.28.1', prerelease: false, body: '_Список изменений недоступен._' }
        ]
      }
    });
    await openUpdatesTab(page);

    await expect(page.getByText('Доступна версия 0.29.0-rc.2')).toBeVisible();
    await expect(page.getByText('Загрузка 7.0 MB')).toBeVisible();
    await expect(page.getByText('Новых версий: 2')).toBeVisible();

    await expect(page.locator('.card-label', { hasText: 'Что нового' })).toBeVisible();
    await expect(page.locator('.section-title', { hasText: 'Новые возможности' })).toBeVisible();
    await expect(page.locator('.release-section code', { hasText: '.gz' })).toBeVisible();
    await expect(page.locator('a.commit-link', { hasText: 'a4878ff' })).toHaveAttribute(
      'href',
      `${REPO}/commit/a4878ff9`
    );
    await expect(page.getByText('этот пункт не показывается')).toHaveCount(0);
    await expect(page.getByText('Описание изменений недоступно')).toBeVisible();

    await page.getByRole('button', { name: 'Установить' }).click();
    await expect(page.getByText(/Панель скачает версию 0\.29\.0-rc\.2 \(7\.0 MB\)/)).toBeVisible();
  });

  test('says the panel is up to date and lists backups for rollback', async ({ page }) => {
    await mockApi(page, {
      check: {
        current_version: '0.28.0',
        latest_version: '0.28.0',
        has_update: false,
        channel: 'beta'
      },
      backups: [
        {
          name: 'xcp.bak.1790000000.0.27.3',
          version: '0.27.3',
          created_at: 1790000000,
          size: 19_000_000
        },
        { name: 'xcp.bak.1780000000', created_at: 1780000000, size: 19_000_000 }
      ]
    });
    await openUpdatesTab(page);

    await expect(page.getByText('Установлена последняя версия канала')).toBeVisible();
    await expect(page.getByText(/Проверено в/)).toBeVisible();
    await expect(page.locator('.card-label', { hasText: 'Что нового' })).toHaveCount(0);

    await expect(page.getByText('v0.27.3')).toBeVisible();
    await expect(page.getByText('версия неизвестна')).toBeVisible();

    await page.getByRole('button', { name: 'Откатить' }).first().click();
    await expect(page.getByText('Панель перезапустится с версией 0.27.3')).toBeVisible();
  });

  test('shows the check error instead of a silent failure', async ({ page }) => {
    await mockApi(page, { checkStatus: 500 });
    await openUpdatesTab(page);

    await expect(page.getByText('GitHub API: 403 rate limit')).toBeVisible();
    await expect(page.getByText('Резервных копий пока нет')).toBeVisible();
  });

  test('no templates card on the updates tab', async ({ page }) => {
    await mockApi(page, {
      check: { current_version: '0.28.0', latest_version: '0.28.0', has_update: false }
    });
    await openUpdatesTab(page);
    await expect(page.locator('.card-label', { hasText: 'Шаблоны' })).toHaveCount(0);
  });

  test('automatic updates: install toggle reveals the window and saves settings', async ({
    page
  }) => {
    const saved: Record<string, unknown>[] = [];
    await mockApi(page, {
      check: { current_version: '0.28.0', latest_version: '0.28.0', has_update: false },
      state: {
        checked_at: 1790400000,
        last_auto_install: {
          version: '0.29.0',
          from: '0.28.0',
          started_at: 1790390000,
          ok: false
        }
      },
      onSettings: (body) => saved.push(body)
    });
    await page.goto('/#/settings?tab=updates');

    await expect(
      page.locator('.card-label', { hasText: 'Автоматические обновления' })
    ).toBeVisible();
    await expect(page.getByText(/Автообновление до 0\.29\.0 не удалось/)).toBeVisible();
    await expect(page.getByText(/Фоновая проверка:/)).toBeVisible();
    await expect(page.getByLabel('Окно установки')).toHaveCount(0);

    // Чекбокс спрятан за стилизованным переключателем — кликаем по его label
    await page
      .locator('label.toggle-switch', { has: page.getByLabel('Устанавливать автоматически') })
      .click();
    await expect.poll(() => saved).toContainEqual({ auto_install: true });
    await expect(page.getByLabel('Окно установки')).toBeVisible();
  });
});

test.describe('Update notifications', () => {
  test('dashboard banner and menu dot lead to the updates tab; hiding sticks', async ({ page }) => {
    await mockApi(page, {
      state: { has_update: true, latest_version: '0.29.0-rc.5', prerelease: true },
      check: { current_version: '0.28.0', latest_version: '0.29.0-rc.5', has_update: true }
    });
    await page.goto('/#/dashboard');

    const banner = page.locator('.update-banner');
    await expect(banner).toBeVisible({ timeout: 10000 });
    await expect(banner).toContainText('Доступна версия 0.29.0-rc.5');
    await expect(page.locator('.nav-badge-update')).toHaveCount(1);

    await banner.getByRole('link', { name: 'Подробнее' }).click();
    await expect(page).toHaveURL(/#\/settings\?tab=updates/);
    await expect(
      page.locator('.card-label', { hasText: 'Автоматические обновления' })
    ).toBeVisible();

    await page.goto('/#/dashboard');
    await page.locator('.update-banner').getByRole('button', { name: 'Скрыть' }).click();
    await expect(page.locator('.update-banner')).toHaveCount(0);
    await page.reload();
    await expect(page.locator('.nav-badge-update')).toHaveCount(1);
    await expect(page.locator('.update-banner')).toHaveCount(0);
  });

  test('shows a toast once after the panel version changed', async ({ page }) => {
    await page.addInitScript(() => {
      if (!sessionStorage.getItem('seeded')) {
        localStorage.setItem('xcp.version.seen', '0.28.0');
        sessionStorage.setItem('seeded', '1');
      }
    });
    await mockApi(page, { version: 'v0.29.0' });
    await page.goto('/#/dashboard');
    await expect(page.getByText('Панель обновлена до 0.29.0')).toBeVisible({ timeout: 10000 });

    await page.reload();
    await page.waitForTimeout(1500);
    await expect(page.getByText('Панель обновлена до 0.29.0')).toHaveCount(0);
  });
});
