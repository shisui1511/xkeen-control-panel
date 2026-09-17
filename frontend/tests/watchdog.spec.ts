import { test, expect } from '@playwright/test';

test.describe('Watchdog status badge, incident banner, detail card and reset action', () => {
  let watchdogData = {
    state: 'degraded',
    consecutive_failures: 5,
    disarm_attempts: 3,
    last_disarm_error: 'iptables: failed to delete rule',
    interception_active: true,
    interception_family: 'ipv4+ipv6',
    next_attempt_at: 0,
    degraded_at: 1710000000
  };

  let resetCallCount = 0;
  let lastResetHeaders: Record<string, string> = {};

  test.beforeEach(async ({ page }) => {
    resetCallCount = 0;
    lastResetHeaders = {};
    watchdogData = {
      state: 'degraded',
      consecutive_failures: 5,
      disarm_attempts: 3,
      last_disarm_error: 'iptables: failed to delete rule',
      interception_active: true,
      interception_family: 'ipv4+ipv6',
      next_attempt_at: 0,
      degraded_at: 1710000000
    };

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
      const method = route.request().method();

      if (url.includes('/api/service/watchdog/reset') && method === 'POST') {
        resetCallCount++;
        lastResetHeaders = route.request().headers();
        if (resetCallCount === 1) {
          await route.fulfill({
            status: 409,
            contentType: 'application/json',
            body: JSON.stringify({
              success: false,
              error: 'cooldown active or disarm in flight'
            })
          });
        } else {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              success: true
            })
          });
        }
        return;
      }

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
      } else if (url.includes('/api/system/stats')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            memory: {
              total: 512 * 1024 * 1024,
              used: 180 * 1024 * 1024,
              free: 332 * 1024 * 1024
            },
            disk: {
              total: 1024 * 1024 * 1024,
              used: 300 * 1024 * 1024,
              free: 724 * 1024 * 1024
            },
            ssl_cert_days: 45,
            load: [0.15, 0.22, 0.18],
            uptime: { seconds: 86400, days: 1, hours: 2, minutes: 30 },
            go_runtime: {
              goroutines: 28,
              heap_alloc: 12 * 1024 * 1024,
              heap_sys: 24 * 1024 * 1024,
              num_gc: 14,
              go_version: 'go1.22.0',
              gomaxprocs: 4,
              goarch: 'arm64'
            },
            router_model: 'Keenetic Hopper (KN-3810)',
            hostname: 'Keenetic-Router',
            wan_status: 'online',
            default_gateway: '192.168.1.1',
            dns_servers: ['1.1.1.1', '8.8.8.8'],
            dns_resolving: true,
            invalid_config: false,
            platform: 'linux/arm64',
            kernel_version: '5.4.213',
            ip_interface: '172.16.0.1 (br0)',
            timezone: 'UTC+3',
            config_path: '/opt/etc/xkeen/',
            config_lines: 142,
            boot_time: '2026-08-19 10:00:00'
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
              watchdog: { ...watchdogData }
            }
          })
        });
      } else if (url.includes('/api/mihomo/status')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              is_running: true,
              version: '1.18.0'
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
    watchdogData.state = 'degraded';
    await page.goto('/#/services');

    const heroStatus = page.locator('.hero-status');
    await expect(heroStatus).toBeVisible();

    const degradedBadge = heroStatus
      .locator('.status-badge, .badge')
      .filter({ hasText: /Деградация|Degraded/ });
    await expect(degradedBadge).toBeVisible();
    await expect(degradedBadge).toContainText(/Деградация|Degraded/);
  });

  test('displays Armed watchdog badge in hero-status when state is armed', async ({ page }) => {
    watchdogData.state = 'armed';
    await page.goto('/#/services');

    const heroStatus = page.locator('.hero-status');
    await expect(heroStatus).toBeVisible();

    const armedBadge = heroStatus
      .locator('.status-badge, .badge')
      .filter({ hasText: /В строю|Armed/ });
    await expect(armedBadge).toBeVisible();
    await expect(armedBadge).toContainText(/В строю|Armed/);
  });

  test('Dashboard shows incident banner with alert-error when degraded, and hides it when armed', async ({
    page
  }) => {
    watchdogData.state = 'degraded';
    await page.goto('/#/dashboard');

    // Problems panel and alert-error banner are visible
    const problemItem = page.locator('.problem-item.alert-error');
    await expect(problemItem).toBeVisible();
    await expect(problemItem).toContainText(
      /Watchdog не может снять перехват TPROXY|Watchdog could not clear TPROXY interception/
    );

    // Action button is visible
    const actionBtn = problemItem.locator('button');
    await expect(actionBtn).toBeVisible();
    await expect(actionBtn).toContainText(/Повторить попытку|Retry Disarm/);

    // When armed, reload and banner should not be present
    watchdogData.state = 'armed';
    await page.reload();
    await expect(page.locator('text=Watchdog не может снять перехват TPROXY')).toHaveCount(0);
    await expect(page.locator('text=Watchdog could not clear TPROXY interception')).toHaveCount(0);
  });

  test('Color semantics: incident banner on Dashboard has alert-warning when state is disarmed', async ({
    page
  }) => {
    watchdogData.state = 'disarmed';
    await page.goto('/#/dashboard');

    const warningItem = page.locator('.problem-item.alert-warning');
    await expect(warningItem).toBeVisible();
    await expect(warningItem).toContainText(/Аварийный disarm выполнен|Emergency disarm completed/);
    // Must NOT have alert-error
    await expect(warningItem).not.toHaveClass(/alert-error/);
  });

  test('Watchdog card on Services page displays details when degraded, and empty state when armed', async ({
    page
  }) => {
    watchdogData = {
      state: 'degraded',
      consecutive_failures: 5,
      disarm_attempts: 3,
      last_disarm_error: 'iptables: rule does not exist',
      interception_active: true,
      interception_family: 'ipv4+ipv6',
      next_attempt_at: 0,
      degraded_at: 1710000000
    };

    await page.goto('/#/services');
    const card = page.locator('.watchdog-card');
    await expect(card).toBeVisible();

    // Attempts and full error text visible
    await expect(card).toContainText(/3/);
    await expect(card).toContainText('iptables: rule does not exist');
    const resetBtn = card.locator('button');
    await expect(resetBtn).toBeVisible();

    // In healthy armed state, empty state is shown, no attempts/error/button
    watchdogData = {
      state: 'armed',
      consecutive_failures: 0,
      disarm_attempts: 0,
      last_disarm_error: '',
      interception_active: false,
      interception_family: '',
      next_attempt_at: 0,
      degraded_at: 0
    };

    await page.reload();
    const armedCard = page.locator('.watchdog-card');
    await expect(armedCard).toBeVisible();
    await expect(armedCard).toContainText(/Инцидентов не зафиксировано|No incidents recorded/);
    await expect(armedCard.locator('.watchdog-error-row')).toHaveCount(0);
    await expect(armedCard.locator('button')).toHaveCount(0);
  });

  test('Manual reset sends POST with X-CSRF-Token, handles 409 error toast, and re-enables button', async ({
    page
  }) => {
    watchdogData.state = 'degraded';
    await page.goto('/#/services');

    const card = page.locator('.watchdog-card');
    await expect(card).toBeVisible();

    const resetBtn = card.locator('button');
    await expect(resetBtn).toBeVisible();
    await expect(resetBtn).toBeEnabled();

    // First click: receives 409
    await resetBtn.click();

    // Assert CSRF token was present in request headers (case-insensitive check)
    const csrfHeader = lastResetHeaders['x-csrf-token'] || lastResetHeaders['X-CSRF-Token'];
    expect(csrfHeader).toBe('mock-csrf-token');

    // Error toast appears
    const toast = page.locator('.toast, [role="alert"]');
    await expect(toast.first()).toBeVisible();

    // Button is immediately re-enabled (no backoff timer)
    await expect(resetBtn).toBeEnabled();

    // Second click: succeeds with 200
    await resetBtn.click();
    expect(resetCallCount).toBe(2);
  });

  test('Dashboard incident banner has no ticking countdown timer', async ({ page }) => {
    watchdogData.state = 'degraded';
    await page.goto('/#/dashboard');

    const problemItem = page.locator('.problem-item.alert-error');
    await expect(problemItem).toBeVisible();

    const textBefore = await problemItem.textContent();
    await page.waitForTimeout(1500);
    const textAfter = await problemItem.textContent();

    expect(textBefore).toBe(textAfter);
  });
});
