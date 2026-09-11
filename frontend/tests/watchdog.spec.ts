import { test, expect } from '@playwright/test';

test.describe('Watchdog status badge on Services page', () => {
  let watchdogState = 'degraded';

  test.beforeEach(async ({ page }) => {
    watchdogState = 'degraded';

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
              active_kernel: 'xray',
              mihomo: {
                reachable: true,
                process_running: true,
                api_reachable: true,
                api_authenticated: true
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
            data: 'v0.25.0'
          })
        });
      } else if (url.includes('/api/service/restart-log')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/kernels')) {
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
              }
            ]
          })
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
              uptime: '2h 15m',
              binary_path: '/opt/sbin/xkeen',
              raw: 'Xray-core (running)\nXKeen is running',
              watchdog: {
                state: watchdogState,
                consecutive_failures: watchdogState === 'degraded' ? 5 : 0,
                disarm_attempts: watchdogState === 'degraded' ? 3 : 0,
                last_disarm_error: watchdogState === 'degraded' ? 'iptables error' : '',
                interception_active: true,
                interception_family: 'ipv4+ipv6',
                next_attempt_at: 0,
                degraded_at: watchdogState === 'degraded' ? 1710000000 : 0
              }
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
  });

  test('displays Degraded watchdog badge in hero-status when state is degraded', async ({
    page
  }) => {
    watchdogState = 'degraded';
    await page.goto('/#/services');

    const heroStatus = page.locator('.hero-status');
    await expect(heroStatus).toBeVisible();

    const degradedBadge = heroStatus.locator('.badge.badge-danger');
    await expect(degradedBadge).toBeVisible();
    await expect(degradedBadge).toContainText(/Деградация|Degraded/);
  });

  test('displays Armed watchdog badge in hero-status when state is armed', async ({ page }) => {
    watchdogState = 'armed';
    await page.goto('/#/services');

    const heroStatus = page.locator('.hero-status');
    await expect(heroStatus).toBeVisible();

    const armedBadge = heroStatus.locator('.badge.badge-success');
    await expect(armedBadge).toBeVisible();
    await expect(armedBadge).toContainText(/В строю|Armed/);
  });
});
