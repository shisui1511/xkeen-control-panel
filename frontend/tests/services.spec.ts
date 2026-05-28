import { test, expect } from '@playwright/test';

test.describe('Proxy Kernels switching test suite', () => {
  let switchRequested = false;

  test.beforeEach(async ({ page }) => {
    switchRequested = false;

    // Отключаем Service Worker в тестах, чтобы запросы к API перехватывались через page.route
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Перехватываем все запросы к API
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
              mihomo: {
                reachable: true,
                process_running: false,
                api_reachable: false,
                api_authenticated: false
              }
            }
          })
        });
      } else if (url.includes('/api/settings')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              dev_mode: false
            }
          })
        });
      } else if (url.includes('/api/version')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: 'v0.15.1'
          })
        });
      } else if (url.includes('/api/service/restart-log')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/kernels')) {
        // Динамический ответ для ядер в зависимости от состояния переключения
        if (switchRequested) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              success: true,
              data: [
                {
                  name: 'xray',
                  display_name: 'Xray-core',
                  binary_path: '/opt/bin/xray',
                  current_version: '1.8.4',
                  latest_version: '1.8.4',
                  has_update: false,
                  channel: 'stable',
                  status: 'idle',
                  process_status: 'stopped',
                  message: 'stopped'
                },
                {
                  name: 'mihomo',
                  display_name: 'Mihomo',
                  binary_path: '/opt/bin/mihomo',
                  current_version: '1.18.0',
                  latest_version: '1.18.0',
                  has_update: false,
                  channel: 'stable',
                  status: 'idle',
                  process_status: 'running',
                  message: 'running on background'
                }
              ]
            })
          });
        } else {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              success: true,
              data: [
                {
                  name: 'xray',
                  display_name: 'Xray-core',
                  binary_path: '/opt/bin/xray',
                  current_version: '1.8.4',
                  latest_version: '1.8.4',
                  has_update: false,
                  channel: 'stable',
                  status: 'idle',
                  process_status: 'running',
                  message: 'running on background'
                },
                {
                  name: 'mihomo',
                  display_name: 'Mihomo',
                  binary_path: '/opt/bin/mihomo',
                  current_version: '1.18.0',
                  latest_version: '1.18.0',
                  has_update: false,
                  channel: 'stable',
                  status: 'idle',
                  process_status: 'stopped',
                  message: 'stopped'
                }
              ]
            })
          });
        }
      } else if (url.includes('/api/service/status')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              is_running: true,
              active_kernel: switchRequested ? 'mihomo' : 'xray',
              pid: 1234,
              uptime: '2h 15m',
              binary_path: '/opt/sbin/xkeen',
              raw: switchRequested ? 'Mihomo (running)\nXKeen is running' : 'Xray-core (running)\nXKeen is running'
            }
          })
        });
      } else if (url.includes('/api/service/control') && url.includes('action=switch_kernel')) {
        switchRequested = true;
        await new Promise((resolve) => setTimeout(resolve, 1000));
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: 'Ядро успешно переключено на mihomo'
        });
      } else {
        // Заглушка для любых других запросов к API (Clash, subscriptions и т.д.)
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  });

  test('successfully switches active kernel xray -> mihomo with optimistic UI spinner', async ({ page }) => {
    // Переходим на страницу
    await page.goto('/#/services');

    // 1. Проверяем исходное состояние UI
    const xrayButton = page.locator('button.ks-btn:has-text("Xray")');
    const mihomoButton = page.locator('button.ks-btn:has-text("Mihomo")');

    await expect(xrayButton).toHaveClass(/ks-active/);
    await expect(mihomoButton).not.toHaveClass(/ks-active/);

    // Проверяем статус-бейдж в строке Xray и Mihomo
    const xrayRow = page.locator('.kernel-card:has-text("Xray")');
    const mihomoRow = page.locator('.kernel-card:has-text("Mihomo")');
    await expect(xrayRow.locator('.status-badge.running')).toBeVisible();
    await expect(mihomoRow.locator('.status-badge.stopped')).toBeVisible();

    // Кликаем по кнопке Mihomo для запуска смены ядра
    await mihomoButton.click();

    // 2. Проверяем Optimistic UI во время запроса
    await expect(mihomoButton).toHaveClass(/ks-switching/);
    await expect(mihomoButton.locator('.ks-dot-spin')).toBeVisible();
    await expect(mihomoButton).toBeDisabled();

    // Ждем окончания запроса и обновления UI
    await page.waitForTimeout(1500);

    // 3. Проверяем финальное состояние после успешной смены
    await expect(mihomoButton).toHaveClass(/ks-active/);
    await expect(xrayButton).not.toHaveClass(/ks-active/);

    await expect(mihomoRow.locator('.status-badge.running')).toBeVisible();
    await expect(xrayRow.locator('.status-badge.stopped')).toBeVisible();
  });
});
