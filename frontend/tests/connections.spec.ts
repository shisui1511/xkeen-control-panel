import { test, expect, type Page, type Route } from '@playwright/test';

// Вспомогательная функция: настройка REST-моков с указанными capabilities
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
    } else if (url.includes('/api/system/clients')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          clients: {
            '192.168.1.5': {
              ip: '192.168.1.5',
              mac: 'AA:BB:CC:DD:EE:01',
              display_name: 'Work-MacBook',
              active: true
            },
            '192.168.1.99': {
              ip: '192.168.1.99',
              mac: 'AA:BB:CC:DD:EE:99',
              display_name: 'Smart-TV',
              active: true
            }
          }
        })
      });
    } else if (url.includes('/api/mihomo/proxy/configs')) {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ 'find-process-mode': 'off' })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: '{}'
        });
      }
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

// Mock WS-фрейм с двумя соединениями
const TWO_CONNECTIONS_FRAME = JSON.stringify({
  connections: [
    {
      id: 'conn-1',
      metadata: {
        network: 'TCP',
        type: 'HTTP',
        sourceIP: '192.168.1.5',
        sourcePort: '54321',
        destinationIP: '1.2.3.4',
        destinationPort: '443',
        host: 'youtube.com',
        process: 'Chrome'
      },
      upload: 1024,
      download: 8192,
      start: new Date().toISOString(),
      chains: ['SmartProxy', 'us-newyork-01'],
      rule: 'DOMAIN-SUFFIX',
      rulePayload: 'youtube.com'
    },
    {
      id: 'conn-2',
      metadata: {
        network: 'UDP',
        type: 'DNS',
        sourceIP: '192.168.1.99',
        sourcePort: '11111',
        destinationIP: '8.8.8.8',
        destinationPort: '53',
        host: 'google.com',
        process: ''
      },
      upload: 0,
      download: 0,
      start: new Date().toISOString(),
      chains: ['DIRECT'],
      rule: 'GEOIP',
      rulePayload: 'private'
    }
  ]
});

// Основная группа тестов — WS подключён, данные получены
test.describe('Connections page test suite', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page, true);

    // Mock WebSocket — отправляем 2 соединения в браузер
    await page.routeWebSocket('**/api/mihomo/connections/ws', async (ws) => {
      ws.send(TWO_CONNECTIONS_FRAME);
    });

    await page.goto('/#/connections');
    await page.waitForSelector('.connections-table', { timeout: 5000 });
  });

  test('live indicator badge appears when WS connects', async ({ page }) => {
    await expect(page.locator('.live-badge.running')).toBeVisible();
  });

  test('connection columns display correct data from WS frame', async ({ page }) => {
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(2);
    // Первая строка содержит данные conn-1 (youtube.com, SmartProxy chain, TCP badge)
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'youtube.com'
    );
    await expect(page.locator('.connections-table tbody td.col-chain').first()).toContainText(
      'SmartProxy'
    );
    await expect(page.locator('.connections-table tbody .net-badge').first()).toContainText('TCP');
  });

  test('global search narrows visible connections by host or client name', async ({ page }) => {
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(2);

    // При пустом поле поиска счётчик совпадений отсутствует в DOM
    await expect(page.locator('.match-badge')).toHaveCount(0);

    const searchInput = page.locator('.search-input');
    await searchInput.fill('Smart-TV');
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(1);
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'google.com'
    );
    await expect(page.locator('.match-badge')).toBeVisible();
    await expect(page.locator('.match-badge')).toHaveText(/(Найдено|Found):\s*1\/2/);

    await searchInput.fill('xxx.no.match');
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(0);
    await expect(page.locator('.match-badge')).toHaveText(/(Найдено|Found):\s*0\/2/);
  });

  test('quick filter chips filter by Proxy and Direct', async ({ page }) => {
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(2);

    // Click Direct only
    await page.locator('.f-chip:has-text("Direct")').click();
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(1);
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'google.com'
    );

    // Click Proxy only
    await page.locator('.f-chip:has-text("Proxy")').click();
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(1);
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'youtube.com'
    );

    // Click All
    await page.locator('.f-chip:has-text("Все"), .f-chip:has-text("All")').first().click();
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(2);
  });

  test('clicking row opens Connection Inspector Drawer', async ({ page }) => {
    await expect(page.locator('.inspector-drawer')).toHaveCount(0);

    // Click first row
    await page.locator('.connections-table tbody tr.conn-row').first().click();

    // Drawer opens
    await expect(page.locator('.inspector-drawer')).toBeVisible();
    await expect(page.locator('.inspector-drawer')).toContainText('youtube.com:443');
    await expect(page.locator('.inspector-drawer')).toContainText('Work-MacBook');

    // Close drawer via close button
    await page.locator('.drawer-close').click();
    await expect(page.locator('.inspector-drawer')).toHaveCount(0);
  });

  test('grouping by LAN client renders accordion cards', async ({ page }) => {
    const groupSelect = page.locator('select.group-select');
    await groupSelect.selectOption('client');

    await expect(page.locator('.group-card')).toHaveCount(2);
    await expect(page.locator('.group-card').first()).toContainText('Work-MacBook');
  });

  test('sorting by upload, download and time reorders connections', async ({ page }) => {
    // Check initial rows
    const firstHost = page.locator('.connections-table tbody td.col-host').first();
    await expect(firstHost).toContainText('youtube.com');

    // Click upload sort column header
    const uploadTh = page.locator('th.col-upload').first();
    await uploadTh.click();
    // Default desc sort -> conn-1 (1024 bytes) first
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'youtube.com'
    );

    // Toggle to asc -> conn-2 (0 bytes) first
    await uploadTh.click();
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'google.com'
    );

    // Click download sort column header -> conn-1 (8192 bytes) desc
    const downloadTh = page.locator('th.col-download').first();
    await downloadTh.click();
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'youtube.com'
    );
  });

  test('pause button toggles stream live status', async ({ page }) => {
    await expect(page.locator('.live-badge.running')).toBeVisible();

    // Click pause button in actions
    const pauseBtn = page.locator('.ph-actions button.btn-secondary').last();
    await pauseBtn.click();

    // Paused badge visible
    await expect(page.locator('.live-badge.paused')).toBeVisible();

    // Click resume button
    await pauseBtn.click();
    await expect(page.locator('.live-badge.running')).toBeVisible();
  });

  test('keyboard navigation opens drawer on Enter and closes on Escape', async ({ page }) => {
    const firstRow = page.locator('.connections-table tbody tr.conn-row').first();
    await firstRow.focus();
    await page.keyboard.press('Enter');

    await expect(page.locator('.inspector-drawer')).toBeVisible();
    await expect(page.locator('.inspector-drawer')).toContainText('youtube.com:443');

    // Press Escape to close
    await page.keyboard.press('Escape');
    await expect(page.locator('.inspector-drawer')).toHaveCount(0);
  });

  test('close all connections button opens confirmation dialog', async ({ page }) => {
    const closeAllBtn = page.locator('.ph-actions button.btn-danger-soft');
    await expect(closeAllBtn).toBeEnabled();

    // Click close all
    await closeAllBtn.click();

    // Confirm dialog modal appears
    await expect(page.locator('.modal-backdrop, .dialog-backdrop, .modal, .dialog')).toBeVisible();
  });
});

// Тест оффлайн состояния — отдельная группа с capabilities.reachable: false
test.describe('Connections page — offline state when Mihomo offline', () => {
  test('shows empty state when Mihomo offline', async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page, false);

    await page.routeWebSocket('**/api/mihomo/connections/ws', async (ws) => {
      ws.close();
    });

    await page.goto('/#/connections');
    await expect(page.locator('.empty-state')).toBeVisible({ timeout: 5000 });
  });
});

// Второй WS-фрейм с приращениями upload/download — даёт ненулевые скорости для теста сортировки по скорости
const SPEED_DELTA_FRAME = JSON.stringify({
  connections: [
    {
      id: 'conn-1',
      metadata: {
        network: 'TCP',
        type: 'HTTP',
        sourceIP: '192.168.1.5',
        sourcePort: '54321',
        destinationIP: '1.2.3.4',
        destinationPort: '443',
        host: 'youtube.com',
        process: 'Chrome'
      },
      upload: 2048,
      download: 9216,
      start: new Date().toISOString(),
      chains: ['SmartProxy', 'us-newyork-01'],
      rule: 'DOMAIN-SUFFIX',
      rulePayload: 'youtube.com'
    },
    {
      id: 'conn-2',
      metadata: {
        network: 'UDP',
        type: 'DNS',
        sourceIP: '192.168.1.99',
        sourcePort: '11111',
        destinationIP: '8.8.8.8',
        destinationPort: '53',
        host: 'google.com',
        process: ''
      },
      upload: 262144,
      download: 262144,
      start: new Date().toISOString(),
      chains: ['DIRECT'],
      rule: 'GEOIP',
      rulePayload: 'private'
    }
  ]
});

// Тест сортировки по колонке скорости (G-94-4) — отдельная группа со своим WS-моком из двух фреймов
test.describe('Connections page — sorting by speed column', () => {
  test('clicking SPEED column header sorts connections by total speed', async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page, true);

    await page.routeWebSocket('**/api/mihomo/connections/ws', async (ws) => {
      ws.send(TWO_CONNECTIONS_FRAME);
      setTimeout(() => ws.send(SPEED_DELTA_FRAME), 800);
    });

    await page.goto('/#/connections');
    await page.waitForSelector('.connections-table', { timeout: 5000 });

    // Дождаться ненулевой скорости после второго фрейма
    await expect(
      page.locator('.connections-table tbody td.col-speed .speed-active').first()
    ).toBeVisible({ timeout: 5000 });

    const speedTh = page.locator('th.col-speed').first();
    await expect(speedTh).toHaveAttribute('aria-sort', 'none');

    // Первый клик: по убыванию — conn-2 (google.com) самое быстрое
    await speedTh.click();
    await expect(speedTh).toHaveAttribute('aria-sort', 'descending');
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'google.com'
    );

    // Второй клик: по возрастанию — conn-1 (youtube.com) самое медленное
    await speedTh.click();
    await expect(speedTh).toHaveAttribute('aria-sort', 'ascending');
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'youtube.com'
    );
  });
});
