import { test, expect } from '@playwright/test';

test.describe('Dashboard 60/40 Redesign and Operational Controls', () => {
  test.beforeEach(async ({ page }) => {
    // Disable Service Worker in tests
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Mock API requests
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
              active_kernel: 'mihomo',
              mihomo: {
                reachable: true,
                process_running: true,
                api_reachable: true,
                api_authenticated: true,
                is_insecure_lan: false
              }
            }
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
              raw: 'XKeen is running'
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
      } else if (url.includes('/api/kernels')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            { name: 'xray', current_version: '1.8.4', process_status: 'stopped' },
            { name: 'mihomo', current_version: '1.18.0', process_status: 'running' }
          ])
        });
      } else if (url.includes('/api/system/stats')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            memory: { total: 512 * 1024 * 1024, used: 180 * 1024 * 1024, free: 332 * 1024 * 1024 },
            disk: { total: 1024 * 1024 * 1024, used: 300 * 1024 * 1024, free: 724 * 1024 * 1024 },
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
      } else if (url.includes('/api/version')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            version: 'v0.23.0',
            panel_version: 'v0.23.0'
          })
        });
      } else if (url.includes('/api/subscriptions')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: []
          })
        });
      } else if (url.includes('/api/mihomo/proxy/connections')) {
        if (route.request().method() === 'DELETE') {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ success: true })
          });
        } else {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              connections: [],
              downloadTotal: 0,
              uploadTotal: 0
            })
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
  });

  test('renders 60/40 dashboard grid layout with all core widgets', async ({ page }) => {
    await page.goto('/#/dashboard');

    // Verify 60/40 Grid Container
    const gridLayout = page.locator('.dashboard-layout-grid');
    await expect(gridLayout).toBeVisible();

    const leftCol = page.locator('.dash-col-left');
    const rightCol = page.locator('.dash-col-right');
    await expect(leftCol).toBeVisible();
    await expect(rightCol).toBeVisible();

    // Verify Service Status Group (3 service cards)
    const serviceGroup = page.locator('.service-status-container');
    await expect(serviceGroup).toBeVisible();
    await expect(page.locator('.service-card')).toHaveCount(3);
    await expect(page.locator('.service-card:has-text("XKeen")')).toBeVisible();
    await expect(page.locator('.service-card:has-text("Mihomo")')).toBeVisible();
    await expect(page.locator('.service-card:has-text("Xray")')).toBeVisible();

    // Verify System Resources Widget
    const resourcesWidget = page.locator('.system-resources-card');
    await expect(resourcesWidget).toBeVisible();
    await expect(resourcesWidget).toContainText('RAM');

    // Verify Traffic Telemetry Widget
    const telemetryWidget = page.locator('.traffic-telemetry-widget');
    await expect(telemetryWidget).toBeVisible();

    // Verify Quick Actions Widget (4 action buttons)
    const quickActionsWidget = page.locator('.quick-actions-widget');
    await expect(quickActionsWidget).toBeVisible();
    const qaButtons = quickActionsWidget.locator('.qa-btn');
    await expect(qaButtons).toHaveCount(4);

    // Verify System Info Widget
    const infoWidget = page.locator('.system-info-widget');
    await expect(infoWidget).toBeVisible();
  });

  test('opens SystemAboutModal diagnostics on info button click', async ({ page }) => {
    await page.goto('/#/dashboard');

    const infoWidget = page.locator('.system-info-widget');
    await expect(infoWidget).toBeVisible();

    // Click "О системе" / Details CTA in System Info Widget header
    const aboutBtn = infoWidget.locator('button:has-text("О системе"), button:has-text("About")');
    await expect(aboutBtn).toBeVisible();
    await aboutBtn.click();

    // Verify SystemAboutModal is open
    const modal = page.locator('[data-testid="system-about-modal"]');
    await expect(modal).toBeVisible();
    await expect(modal).toContainText('Go Runtime');
    await expect(modal).toContainText('Goroutines');
    await expect(modal).toContainText('Heap Alloc');
    await expect(modal).toContainText('Keenetic Hopper');

    // Close modal
    const closeBtn = modal.locator('button:has-text("Закрыть"), button:has-text("Close")');
    await closeBtn.click();
    await expect(modal).not.toBeVisible();
  });

  test('prompts confirmation when clicking Reset Sessions quick action', async ({ page }) => {
    await page.goto('/#/dashboard');

    const quickActionsWidget = page.locator('.quick-actions-widget');
    const resetSessionsBtn = quickActionsWidget.locator('.qa-btn-destructive');
    await expect(resetSessionsBtn).toBeVisible();

    // Click Reset Sessions button
    await resetSessionsBtn.click();

    // Confirm dialog should be displayed
    const confirmModal = page.locator('.modal-container');
    await expect(confirmModal).toBeVisible();
    await expect(confirmModal).toContainText('TCP/UDP');

    // Cancel confirmation
    const cancelBtn = confirmModal.locator(
      'button.btn-secondary, button:has-text("Отмена"), button:has-text("Cancel")'
    );
    await cancelBtn.click();
    await expect(confirmModal).not.toBeVisible();
  });
});
