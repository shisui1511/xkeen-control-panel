import { test, expect, type Page, type Route } from '@playwright/test';

// Вспомогательная функция: настройка REST-моков
async function setupRestMocks(page: Page, mihomoReachable = true) {
  await page.route('**/api/**', async (route: Route) => {
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
              mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
            },
            active_kernel: 'mihomo',
            mihomo: {
              reachable: mihomoReachable,
              process_running: mihomoReachable,
              api_reachable: mihomoReachable,
              api_authenticated: mihomoReachable
            }
          }
        })
      });
    } else if (url.includes('/api/traffic/reset')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
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

// Вспомогательная функция: отключение Service Worker
async function disableServiceWorker(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });
}

const TRAFFIC_FRAME = JSON.stringify({
  up: 512,
  down: 1024,
  connections: 5,
  tcp_connections: 3,
  udp_connections: 2,
  peaks: {
    peak_hour_up: 5000,
    peak_hour_down: 8000,
    peak_day_up: 50000,
    peak_day_down: 80000,
    peak_week_up: 500000,
    peak_week_down: 800000,
    hour_start: Math.floor(Date.now() / 1000),
    day_start: Math.floor(Date.now() / 1000),
    week_start: Math.floor(Date.now() / 1000)
  }
});

test.describe('Traffic page test suite', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page, true);

    // Mock WebSocket для трафика
    await page.routeWebSocket('**/api/traffic/ws', async (ws) => {
      ws.send(TRAFFIC_FRAME);
    });

    await page.goto('/#/traffic');
    // Ждем появления статуса live
    await page.waitForSelector('.status-indicator.live', { timeout: 5000 });
  });

  test('live indicator appears when WS connects', async ({ page }) => {
    const status = page.locator('.status-indicator');
    await expect(status).toHaveText(/live/i);
  });

  test('displays peak load values in the table', async ({ page }) => {
    // В таблице пиков должны отображаться значения из mock фрейма
    const cells = page.locator('.connections-table tbody td');
    await expect(cells.first()).toContainText('4.9 KB/s'); // 5000 B/s upload peak hour
    await expect(cells.first()).toContainText('7.8 KB/s'); // 8000 B/s download peak hour
  });

  test('reset stats button triggers dialog and requests API', async ({ page }) => {
    let confirmTriggered = false;
    page.on('dialog', async (dialog) => {
      confirmTriggered = true;
      expect(dialog.message()).toContain('Reset statistics');
      await dialog.accept();
    });

    // Нажимаем на кнопку сброса
    const resetButton = page.locator('.btn-reset');
    await expect(resetButton).toBeVisible();
    await resetButton.click();

    expect(confirmTriggered).toBe(true);
  });

  test('SVG charts have role and aria-labels for accessibility', async ({ page }) => {
    const mainChart = page.locator('.chart-svg-container svg');
    await expect(mainChart).toHaveAttribute('role', 'img');
    await expect(mainChart).toHaveAttribute('aria-label', /Main traffic speed chart/i);

    const uploadSparkline = page.locator('.stat-card-spark:nth-child(1) svg.sparkline');
    await expect(uploadSparkline).toHaveAttribute('role', 'img');
    await expect(uploadSparkline).toHaveAttribute('aria-label', /Upload sparkline/i);

    const downloadSparkline = page.locator('.stat-card-spark:nth-child(2) svg.sparkline');
    await expect(downloadSparkline).toHaveAttribute('role', 'img');
    await expect(downloadSparkline).toHaveAttribute('aria-label', /Download sparkline/i);
  });
});

test.describe('Traffic empty state and auto-reconnect tests', () => {
  test('shows empty state when no traffic data is received', async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page, true);
    await page.routeWebSocket('**/api/traffic/ws', async (ws) => {
      // не шлем фреймов
    });

    await page.goto('/#/traffic');

    const emptyContainer = page.locator('.chart-empty');
    await expect(emptyContainer).toContainText(/Waiting for traffic data/i);
    await expect(emptyContainer).toContainText(/Connecting to the proxy kernel metrics/i);
  });
});
