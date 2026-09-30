import { test, expect } from '@playwright/test';
import { kernelsFixture, systemStatsFixture } from './helpers/api-mocks';
import { LAZY_LOAD_TIMEOUT } from './helpers/timeouts';

// Карточка «Установка XKeen» на странице служб: видна только без XKeen,
// запускает официальный установщик в терминале с выбранным каналом и
// показывает результат по коду выхода.

async function mockCommonRoutes(
  page: import('@playwright/test').Page,
  // Объект читается при каждом запросе: тест может менять статус по ходу
  status: {
    installed: boolean;
    available: boolean;
    incomplete?: boolean;
    // Поля ядра Xray в /api/kernels поверх фикстуры; читаются при каждом запросе
    xray?: Record<string, unknown>;
    // Число POST /api/kernels/xray/check (заполняет мок)
    checkCalls?: number;
  }
) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  await page.route('**/api/**', async (route) => {
    const url = route.request().url();

    if (url.includes('/api/auth/me')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          authenticated: true,
          setup_required: false,
          csrf_token: 'mock-csrf-token'
        })
      });
    } else if (url.includes('/api/capabilities')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            kernels: {
              xray: { installed: true, version: '1.8.4', channel: 'stable' },
              mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
            },
            active_kernel: 'xray',
            xkeen_installed: status.installed,
            mihomo: { reachable: true, process_running: false, api_reachable: false }
          }
        })
      });
    } else if (url.includes('/api/settings')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: { dev_mode: false } })
      });
    } else if (url.includes('/api/version')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: 'v0.25.0' })
      });
    } else if (url.includes('/api/service/restart-log')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    } else if (url.includes('/api/service/status')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            is_running: true,
            active_kernel: 'xray',
            pid: 1234,
            uptime: '1h',
            binary_path: '/opt/sbin/xkeen',
            raw: 'Xray-core (running)\nXKeen is running',
            xkeen_installed: status.installed,
            xkeen_installer_available: status.available,
            xkeen_setup_incomplete: status.incomplete === true
          }
        })
      });
    } else if (url.includes('/api/kernels')) {
      const kernels = kernelsFixture('xray', { xray: status.xray });
      const path = new URL(url).pathname;
      const method = route.request().method();
      let data: unknown = kernels;
      if (path === '/api/kernels/xray/check' && method === 'POST') {
        status.checkCalls = (status.checkCalls ?? 0) + 1;
        // Проверка завершилась: латест-версия стала известна
        status.xray = { ...status.xray, latest_version: '26.3.27' };
        data = {};
      } else if (path === '/api/kernels/xray/status') {
        data = kernelsFixture('xray', { xray: status.xray })[0];
      }
      // Services.svelte ждёт массив ядер — универсальный { data: {} } ломал fetchKernels
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data })
      });
    } else if (url.includes('/api/system/stats')) {
      // Dashboard.svelte читает load[0] и uptime без разворачивания конверта
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(systemStatsFixture())
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    }
  });
}

test.describe('Services page — XKeen installer card', () => {
  test('hidden when XKeen is installed', async ({ page }) => {
    await mockCommonRoutes(page, { installed: true, available: true });
    await page.goto('/#/services');
    await expect(page.locator('.hero-card')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.getByTestId('xkeen-install-card')).toHaveCount(0);
  });

  test('offers the installer again when XKeen setup was interrupted', async ({ page }) => {
    await mockCommonRoutes(page, { installed: true, available: true, incomplete: true });
    await page.goto('/#/services');
    const card = page.getByTestId('xkeen-install-card');
    await expect(card).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(card).toContainText(/не до конца|partly installed/);
    await expect(page.getByTestId('xkeen-install-start')).toBeVisible();
  });

  test('without Entware explains why installation is unavailable', async ({ page }) => {
    await mockCommonRoutes(page, { installed: false, available: false });
    await page.goto('/#/services');
    const card = page.getByTestId('xkeen-install-card');
    await expect(card).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(card).toContainText(/Entware/);
    await expect(page.getByTestId('xkeen-install-start')).toHaveCount(0);
  });

  test('runs the installer with the chosen channel and reports success', async ({ page }) => {
    await mockCommonRoutes(page, { installed: false, available: true });
    let wsUrl = '';
    await page.routeWebSocket(/\/api\/terminal\/ws/, (ws) => {
      wsUrl = ws.url();
      ws.send('Installing XKeen...\r\n');
      ws.send(JSON.stringify({ type: 'exit', code: 0 }));
    });

    await page.goto('/#/services');
    await expect(page.locator('.hero-card')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    const card = page.getByTestId('xkeen-install-card');
    await expect(card).toBeVisible();
    await card.getByRole('button', { name: /^(Бета|Beta)$/ }).click();
    await page.getByTestId('xkeen-install-start').click();

    const modal = page.getByTestId('xkeen-install-modal');
    await expect(modal).toBeVisible();
    await expect(modal.locator('.install-result.ok')).toBeVisible();
    expect(wsUrl).toContain('mode=xkeen-install');
    expect(wsUrl).toContain('channel=beta');
  });

  test('keeps the installer open when XKeen appears mid-install', async ({ page }) => {
    // Установщик кладёт бинарник xkeen до конца `xkeen -i`: статус становится
    // «установлен», а окно с терминалом должно остаться до закрытия
    const status = { installed: false, available: true };
    await mockCommonRoutes(page, status);
    let socket: import('@playwright/test').WebSocketRoute | null = null;
    await page.routeWebSocket(/\/api\/terminal\/ws/, (ws) => {
      socket = ws;
      ws.send('Installing XKeen...\r\n');
    });

    await page.clock.install();
    await page.goto('/#/services');
    await expect(page.locator('.hero-card')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await page.getByTestId('xkeen-install-start').click();
    const modal = page.getByTestId('xkeen-install-modal');
    await expect(modal).toBeVisible();
    await expect.poll(() => socket !== null).toBe(true);

    // Следующий опрос статуса (раз в 15 с) приносит xkeen_installed: true
    status.installed = true;
    const polled = page.waitForResponse(
      (r) => r.url().includes('/api/service/status') && r.request().method() === 'GET'
    );
    await page.clock.runFor(16_000);
    await polled;
    await expect(modal).toBeVisible();
    await expect(modal.locator('.install-terminal')).toBeVisible();

    socket!.send(JSON.stringify({ type: 'exit', code: 0 }));
    await expect(modal.locator('.install-result.ok')).toBeVisible();
    await modal.locator('.install-actions button').click();
    await expect(page.getByTestId('xkeen-install-card')).toHaveCount(0);
  });

  test('reloads XKeen settings once installation completes', async ({ page }) => {
    const status = { installed: false, available: true };
    await mockCommonRoutes(page, status);
    let settingsCalls = 0;
    await page.route('**/api/xkeen/settings', async (route) => {
      settingsCalls++;
      if (!status.installed) {
        await route.fulfill({ status: 404, json: { success: false, error: 'not installed' } });
        return;
      }
      await route.fulfill({
        json: {
          success: true,
          data: [
            {
              kind: 'port_proxying',
              path: '/opt/etc/xkeen/port_proxying.lst',
              exists: true,
              content: '#80\n',
              entries: 0,
              issues: []
            }
          ]
        }
      });
    });
    let socket: import('@playwright/test').WebSocketRoute | null = null;
    await page.routeWebSocket(/\/api\/terminal\/ws/, (ws) => {
      socket = ws;
    });

    await page.clock.install();
    await page.goto('/#/services');
    await expect(page.locator('.hero-card')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect.poll(() => settingsCalls).toBe(1);
    await page.getByTestId('xkeen-install-start').click();
    await expect.poll(() => socket !== null).toBe(true);

    status.installed = true;
    socket!.send(JSON.stringify({ type: 'exit', code: 0 }));
    const modal = page.getByTestId('xkeen-install-modal');
    await expect(modal.locator('.install-result.ok')).toBeVisible();
    await expect.poll(() => settingsCalls).toBeGreaterThan(1);
    await expect(page.getByText('/opt/etc/xkeen/port_proxying.lst')).toBeAttached();
  });

  test('shows the installer exit code on failure', async ({ page }) => {
    await mockCommonRoutes(page, { installed: false, available: true });
    await page.routeWebSocket(/\/api\/terminal\/ws/, (ws) => {
      ws.send(JSON.stringify({ type: 'exit', code: 3 }));
    });

    await page.goto('/#/services');
    await expect(page.locator('.hero-card')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await page.getByTestId('xkeen-install-start').click();
    const result = page.getByTestId('xkeen-install-modal').locator('.install-result.fail');
    await expect(result).toContainText('3');
  });

  test('dashboard problems panel points to the XKeen installer', async ({ page }) => {
    await mockCommonRoutes(page, { installed: false, available: true });
    await page.goto('/#/');
    const problem = page.getByTestId('problem-xkeen-missing');
    await expect(problem).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await problem.getByRole('button').click();
    await expect(page).toHaveURL(/#\/services/);
    await expect(page.getByTestId('xkeen-install-card')).toBeVisible({
      timeout: LAZY_LOAD_TIMEOUT
    });
  });

  test.describe('подсказка версии Xray над установщиком', () => {
    test('стабильная: версия Xray в карточке и в окне установщика', async ({ page }) => {
      await mockCommonRoutes(page, {
        installed: false,
        available: true,
        xray: { latest_version: '26.3.27', channel: 'stable' }
      });
      await page.routeWebSocket(/\/api\/terminal\/ws/, () => {});

      await page.goto('/#/services');
      const hint = page.getByTestId('xkeen-install-stable-hint');
      await expect(hint).toContainText('Xray v26.3.27', { timeout: LAZY_LOAD_TIMEOUT });

      await page.getByTestId('xkeen-install-start').click();
      await expect(page.getByTestId('xkeen-install-stable-hint-modal')).toContainText(
        'Xray v26.3.27'
      );
    });

    test('Xray на предварительном канале: общая подсказка без версии, у «Бета» подсказки нет', async ({
      page
    }) => {
      await mockCommonRoutes(page, {
        installed: false,
        available: true,
        xray: { latest_version: '26.9.9', channel: 'preview' }
      });

      await page.goto('/#/services');
      const hint = page.getByTestId('xkeen-install-stable-hint');
      await expect(hint).toContainText(
        /последний стабильный релиз Xray|latest stable Xray release/,
        { timeout: LAZY_LOAD_TIMEOUT }
      );
      await expect(hint).not.toContainText('26.9.9');

      await page
        .getByTestId('xkeen-install-card')
        .getByRole('button', { name: /^(Бета|Beta)$/ })
        .click();
      await expect(hint).toHaveCount(0);
    });

    test('неизвестная стабильная версия запрашивается один раз', async ({ page }) => {
      const status = {
        installed: false,
        available: true,
        checkCalls: 0,
        xray: { latest_version: '', channel: 'stable' }
      };
      await mockCommonRoutes(page, status);

      await page.goto('/#/services');
      const hint = page.getByTestId('xkeen-install-stable-hint');
      await expect(hint).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
      await expect.poll(() => status.checkCalls).toBe(1);
      // После ответа проверки версия появляется в подсказке, повторных проверок нет
      await expect(hint).toContainText('Xray v26.3.27');
      expect(status.checkCalls).toBe(1);
    });
  });
});
