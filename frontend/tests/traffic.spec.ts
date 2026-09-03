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
    peak_hour_up_time: Math.floor(Date.now() / 1000) - 120,
    peak_hour_down_time: Math.floor(Date.now() / 1000) - 60,
    hour_start: Math.floor(Date.now() / 1000),
    day_start: Math.floor(Date.now() / 1000),
    week_start: Math.floor(Date.now() / 1000)
  },
  top_clients: [
    {
      ip: '192.168.1.105',
      upload: 512000,
      download: 2048000,
      total_bytes: 2560000,
      active_connections: 4
    }
  ]
});

test.describe('Traffic page test suite', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page, true);

    // Mock WebSocket для трафика
    await page.routeWebSocket('**/api/traffic/ws', async (ws) => {
      ws.send(TRAFFIC_FRAME);
      // Отправляем второй фрейм, чтобы у клиента было минимум 2 точки для отрисовки графиков
      await new Promise((resolve) => setTimeout(resolve, 50));
      ws.send(TRAFFIC_FRAME);
    });

    await page.goto('/#/traffic');
    // Ждем появления статуса live
    await page.waitForSelector('.badge-live-indicator.is-live', { timeout: 5000 });
  });

  test('live indicator appears when WS connects', async ({ page }) => {
    const status = page.locator('.badge-live-indicator');
    await expect(status).toHaveText(/live|в реальном времени/i);
  });

  test('KPI cards follow standard order: Download (1st), Upload (2nd), Connections (3rd)', async ({
    page
  }) => {
    const statCards = page.locator('.traffic-stats-grid .card');
    await expect(statCards).toHaveCount(3);

    // 1st Card: Download
    await expect(statCards.nth(0)).toContainText(/download|загрузка/i);
    const downloadSparkline = statCards.nth(0).locator('svg.sparkline');
    await expect(downloadSparkline).toBeVisible();

    // 2nd Card: Upload
    await expect(statCards.nth(1)).toContainText(/upload|отдача/i);
    const uploadSparkline = statCards.nth(1).locator('svg.sparkline');
    await expect(uploadSparkline).toBeVisible();

    // 3rd Card: Active Connections
    await expect(statCards.nth(2)).toContainText(/active connections|активные соединения/i);
    await expect(statCards.nth(2)).toContainText('3 TCP · 2 UDP');
  });

  test('displays peak load cards with timestamps and separate flows', async ({ page }) => {
    const peakCards = page.locator('.peak-period-card');
    await expect(peakCards).toHaveCount(3);

    // В первой карточке за час должны быть метрики
    await expect(peakCards.nth(0)).toContainText('7.8 KB/s'); // 8000 B/s download peak
    await expect(peakCards.nth(0)).toContainText('4.9 KB/s'); // 5000 B/s upload peak
  });

  test('displays top LAN clients widget', async ({ page }) => {
    const clientRows = page.locator('.client-row');
    await expect(clientRows).toHaveCount(1);
    await expect(clientRows.first()).toContainText('192.168.1.105');
    await expect(clientRows.first()).toContainText('4');
  });

  test('timeframe buttons switch active interval', async ({ page }) => {
    const tfPills = page.locator('.timeframe-picker .tf-pill');
    await expect(tfPills).toHaveCount(4);

    // По умолчанию выбран 1m
    await expect(tfPills.nth(0)).toHaveClass(/active/);

    // Кликаем по 5m
    await tfPills.nth(1).click();
    await expect(tfPills.nth(1)).toHaveClass(/active/);
    await expect(tfPills.nth(0)).not.toHaveClass(/active/);
  });

  test('pause button toggles pause status badge and mode', async ({ page }) => {
    const pauseBtn = page.locator('.ph-actions button').first();
    await expect(pauseBtn).toBeVisible();

    // Нажимаем паузу
    await pauseBtn.click();
    const status = page.locator('.badge-live-indicator');
    await expect(status).toHaveText(/paused|на паузе/i);

    // Возобновляем
    await pauseBtn.click();
    await expect(status).toHaveText(/live|в реальном времени/i);
  });

  test('reset stats button triggers dialog and requests API', async ({ page }) => {
    const resetButton = page.locator('.btn-reset');
    await expect(resetButton).toBeVisible();
    await resetButton.click();

    // Проверяем появление стилизованного ConfirmDialog
    const confirmBtn = page
      .locator('.confirm-actions button.btn-danger, .confirm-actions button.btn-primary')
      .first();
    await expect(confirmBtn).toBeVisible();
    await confirmBtn.click();
  });

  test('SVG charts have role and aria-labels for accessibility', async ({ page }) => {
    const mainChart = page.locator('.chart-svg-container svg');
    await expect(mainChart).toHaveAttribute('role', 'img');
    await expect(mainChart).toHaveAttribute(
      'aria-label',
      /Main traffic speed chart|Главный график скорости трафика/i
    );

    const downloadSparkline = page.locator('.traffic-stats-grid .card:nth-child(1) svg.sparkline');
    await expect(downloadSparkline).toHaveAttribute('role', 'img');
    await expect(downloadSparkline).toHaveAttribute(
      'aria-label',
      /Download sparkline|Мини-график загрузки/i
    );

    const uploadSparkline = page.locator('.traffic-stats-grid .card:nth-child(2) svg.sparkline');
    await expect(uploadSparkline).toHaveAttribute('role', 'img');
    await expect(uploadSparkline).toHaveAttribute(
      'aria-label',
      /Upload sparkline|Мини-график отдачи/i
    );
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
    await expect(emptyContainer).toContainText(/Waiting for traffic data|Ожидание данных трафика/i);
    await expect(emptyContainer).toContainText(
      /Connecting to the proxy kernel metrics|Подключение к службе сбора метрик/i
    );
  });
});

test.describe('Traffic Xray Live Statistics test suite (XRAY-07)', () => {
  test('активный Xray с включенным мониторингом показывает таблицу статистики исходящих тегов', async ({
    page
  }) => {
    await disableServiceWorker(page);

    await page.route('**/api/**', async (route: Route) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, csrf_token: 'mock-csrf' })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              kernels: { xray: { installed: true, version: '1.8.24' } },
              active_kernel: 'xray',
              xray: { conf_dir: '/opt/etc/xray', conf_dir_exists: true, grpc_ready: true }
            }
          })
        });
      } else if (url.includes('/api/xray/stats')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              'proxy-out': { uplink: 1048576, downlink: 5242880 },
              direct: { uplink: 512, downlink: 1024 }
            }
          })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });

    await page.routeWebSocket('**/api/traffic/ws', async (ws) => {});

    await page.goto('/#/traffic');

    const section = page.locator('[data-testid="xray-stats-section"]');
    await expect(section).toBeVisible({ timeout: 5000 });

    const table = page.locator('[data-testid="xray-stats-table"]');
    await expect(table).toBeVisible({ timeout: 5000 });

    const tags = page.locator('[data-testid="xray-stats-tag"]');
    await expect(tags.first()).toHaveText('proxy-out');
  });

  test('активное ядро Mihomo скрывает раздел статистики Xray (D-05)', async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page, true);
    await page.routeWebSocket('**/api/traffic/ws', async (ws) => {});

    await page.goto('/#/traffic');

    await expect(page.locator('[data-testid="xray-stats-section"]')).toHaveCount(0);
  });

  test('выключенный мониторинг показывает подсказку и не опрашивает статистику', async ({
    page
  }) => {
    await disableServiceWorker(page);
    let statsPolled = false;

    await page.route('**/api/**', async (route: Route) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, csrf_token: 'mock-csrf' })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              kernels: { xray: { installed: true } },
              active_kernel: 'xray',
              xray: { conf_dir: '/opt/etc/xray', conf_dir_exists: true, grpc_ready: false }
            }
          })
        });
      } else if (url.includes('/api/xray/stats')) {
        statsPolled = true;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });

    await page.routeWebSocket('**/api/traffic/ws', async (ws) => {});

    await page.goto('/#/traffic');

    const hint = page.locator('[data-testid="xray-stats-disabled-hint"]');
    await expect(hint).toBeVisible({ timeout: 5000 });
    await expect(page.locator('[data-testid="xray-stats-table"]')).toHaveCount(0);

    // Wait a short moment to ensure interval isn't polling
    await page.waitForTimeout(500);
    expect(statsPolled).toBe(false);
  });

  test('ошибка переключения мониторинга (конфликт порта 409) отображает уведомление', async ({
    page
  }) => {
    await disableServiceWorker(page);

    await page.route('**/api/**', async (route: Route) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, csrf_token: 'mock-csrf' })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              kernels: { xray: { installed: true } },
              active_kernel: 'xray',
              xray: { conf_dir: '/opt/etc/xray', conf_dir_exists: true, grpc_ready: false }
            }
          })
        });
      } else if (url.includes('/api/xray/grpc/monitoring')) {
        await route.fulfill({
          status: 409,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'Port 10085 is already in use by another service' })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });

    await page.routeWebSocket('**/api/traffic/ws', async (ws) => {});

    await page.goto('/#/traffic');

    const toggleBtn = page.locator('[data-testid="xray-grpc-toggle-btn"]');
    await expect(toggleBtn).toBeVisible({ timeout: 5000 });
    await toggleBtn.click();

    // Toast должен появиться с текстом ошибки
    const toast = page.locator('.toast, [role="alert"]');
    await expect(toast.first()).toBeVisible({ timeout: 3000 });
    await expect(toast.first()).toContainText(/10085|already in use|конфликт/i);
  });
});
