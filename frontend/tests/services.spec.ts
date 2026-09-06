import { test, expect } from '@playwright/test';

test.describe('Proxy Kernels switching test suite', () => {
  let switchRequested = false;

  test.beforeEach(async ({ page }) => {
    switchRequested = false;

    // Disable Service Worker in tests so requests are intercepted
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Intercept API routes
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
              active_kernel: switchRequested ? 'mihomo' : 'xray',
              mihomo: {
                reachable: true,
                process_running: switchRequested,
                api_reachable: switchRequested,
                api_authenticated: switchRequested
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
          body: JSON.stringify(
            switchRequested
              ? [
                  {
                    timestamp: Math.floor(Date.now() / 1000),
                    action: 'switch_kernel:mihomo',
                    success: true,
                    exit_code: 0,
                    output: 'switched to mihomo'
                  }
                ]
              : []
          )
        });
      } else if (url.includes('/api/kernels')) {
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
              raw: switchRequested
                ? 'Mihomo (running)\nXKeen is running'
                : 'Xray-core (running)\nXKeen is running'
            }
          })
        });
      } else if (url.includes('/api/service/control') && url.includes('action=switch_kernel')) {
        switchRequested = true;
        await new Promise((resolve) => setTimeout(resolve, 500));
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: 'Ядро успешно переключено на mihomo'
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  });

  test('successfully switches active kernel xray -> mihomo via radio selector and confirm dialog', async ({
    page
  }) => {
    await page.goto('/#/services');

    // 1. Check initial Hero state
    const xrayRadio = page.locator('.core-radio-card:has-text("Xray")');
    const mihomoRadio = page.locator('.core-radio-card:has-text("Mihomo")');

    await expect(xrayRadio).toHaveClass(/active/);
    await expect(mihomoRadio).not.toHaveClass(/active/);

    // 2. Click Mihomo radio option
    await mihomoRadio.click();

    // 3. Confirm modal appears and confirm switch
    const confirmBtn = page.locator(
      '.modal button.btn-primary, .confirm-dialog button.btn-primary, button:has-text("Сделать активным"), button:has-text("Make active")'
    );
    await expect(confirmBtn.first()).toBeVisible();
    await confirmBtn.first().click();

    // 4. Wait for UI update
    await page.waitForTimeout(1000);

    // 5. Check final state
    await expect(mihomoRadio).toHaveClass(/active/);
    await expect(xrayRadio).not.toHaveClass(/active/);
  });
});
