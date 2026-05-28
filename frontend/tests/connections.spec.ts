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
      chains: ['PROXY', 'us-newyork-01', 'DIRECT'],
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
    // Ждём появления таблицы после получения WS-данных
    await page.waitForSelector('.connections-table', { timeout: 5000 });
  });

  test('live indicator appears when WS connects', async ({ page }) => {
    // После получения WS-сообщения wsConnected = true → .live-indicator виден
    await expect(page.locator('.live-indicator')).toBeVisible();
    // Индикатор не находится в состоянии reconnecting
    await expect(page.locator('.live-indicator')).not.toHaveClass(/live-reconnecting/);
  });

  test('connection columns display correct data from WS frame', async ({ page }) => {
    // В таблице 2 строки — по одной на каждое соединение из WS mock
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(2);
    // Первая строка содержит данные conn-1 (youtube.com, PROXY chain, TCP badge)
    await expect(page.locator('.connections-table tbody td.col-host').first()).toContainText(
      'youtube.com'
    );
    await expect(page.locator('.connections-table tbody td.col-chain').first()).toContainText(
      'PROXY'
    );
    await expect(page.locator('.connections-table tbody .net-badge').first()).toContainText('TCP');
  });

  test('filter input narrows visible connections by source IP', async ({ page }) => {
    // Изначально в таблице 2 строки
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(2);

    // Используем первый фильтр — Source (IP)...
    const sourceFilter = page.locator('input.filter-input').first();
    // Фильтруем по IP второго соединения — должна остаться 1 строка
    await sourceFilter.fill('192.168.1.99');
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(1);

    // Вводим несовпадающий IP — строк не должно быть
    await sourceFilter.fill('xxx.no.match');
    await expect(page.locator('.connections-table tbody tr.conn-row')).toHaveCount(0);
  });
});

// Тест reconnect — отдельная группа с собственным beforeEach (без успешного WS)
test.describe('Connections page — reconnect scenario', () => {
  test('reconnecting indicator appears when WS closes', async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page, true);

    // WS mock: отправляем пустой frame и сразу закрываем — триггерит onclose → wsReconnecting = true
    await page.routeWebSocket('**/api/mihomo/connections/ws', async (ws) => {
      ws.send(JSON.stringify({ connections: [] }));
      ws.close();
    });

    await page.goto('/#/connections');

    // wsReconnecting = true → появляется .live-reconnecting
    await expect(page.locator('.live-reconnecting')).toBeVisible({ timeout: 5000 });
  });
});

// Тест тоггла — отдельная группа с capabilities.reachable: false
test.describe('Connections page — toggle disabled when Mihomo offline', () => {
  test('process-name toggle is disabled when Mihomo offline', async ({ page }) => {
    await disableServiceWorker(page);
    // Настраиваем capabilities с mihomo.reachable: false
    await setupRestMocks(page, false);

    // WS не должен подключиться (mihomo offline), но нам нужна страница
    await page.routeWebSocket('**/api/mihomo/connections/ws', async (ws) => {
      ws.close();
    });

    await page.goto('/#/connections');
    await page.waitForSelector('.connections-table, .ph-actions', { timeout: 5000 });

    // isMihomoActive = false → тоггл disabled
    await expect(page.locator('.toggle-label input[type=checkbox]')).toBeDisabled();
  });
});
