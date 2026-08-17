import { test, expect, type Page, type Route } from '@playwright/test';

async function disableServiceWorker(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });
}

async function setupMocks(page: Page) {
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
              xray: { installed: true, version: '1.8.4', channel: 'stable' },
              mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
            },
            active_kernel: 'mihomo',
            mihomo: {
              reachable: true,
              process_running: true,
              api_reachable: true,
              api_authenticated: true
            }
          }
        })
      });
    } else if (url.includes('/api/system/stats')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          load: [0.35, 0.4, 0.45],
          memory: {
            total: 512 * 1024 * 1024,
            used: 128 * 1024 * 1024,
            free: 384 * 1024 * 1024
          },
          disk: {
            total: 1000 * 1024 * 1024,
            used: 200 * 1024 * 1024,
            free: 800 * 1024 * 1024
          },
          uptime: { days: 2, hours: 5, minutes: 12 },
          go_runtime: { gomaxprocs: 2, goroutines: 15 }
        })
      });
    } else if (url.includes('/api/service/status')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          xkeen: 'running',
          mihomo: 'running',
          xray: 'stopped'
        })
      });
    } else if (url.includes('/api/service/control')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    } else if (url.includes('/api/config/list')) {
      const isMihomo = url.includes('mihomo');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          isMihomo
            ? [
                {
                  name: 'config.yaml',
                  path: '/opt/etc/mihomo/config.yaml',
                  size: 1500
                }
              ]
            : [
                {
                  name: 'xray-config.json',
                  path: '/opt/etc/xray/configs/xray-config.json',
                  size: 1200
                }
              ]
        )
      });
    } else if (url.includes('/api/config/read')) {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'port: 7890\nmode: rule\n'
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    }
  });

  await page.routeWebSocket('**/api/traffic/ws', async (ws) => {
    ws.send(
      JSON.stringify({
        up: 1048576,
        down: 2097152,
        connections: 12,
        tcp_connections: 8,
        udp_connections: 4
      })
    );
  });
}

test.describe('System Status Capsule and Quick Actions', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupMocks(page);
  });

  test('displays segmented status capsule in desktop topbar', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto('/#/dashboard');

    const capsule = page.locator('.topbar .system-status-capsule');
    await expect(capsule).toBeVisible({ timeout: 5000 });

    // Check kernel name
    await expect(page.locator('.topbar .kernel-name')).toContainText('Mihomo');

    // Check resources segment
    await expect(page.locator('.topbar .resource-segment')).toContainText('CPU');
    await expect(page.locator('.topbar .resource-segment')).toContainText('RAM');

    // Check traffic segment
    await expect(page.locator('.topbar .traffic-segment')).toBeVisible();
    await expect(page.locator('.topbar .traffic-segment')).toContainText('MB/s');
  });

  test('opens quick actions popover menu on kernel click', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto('/#/dashboard');

    const kernelBtn = page.locator('.topbar .kernel-segment');
    await expect(kernelBtn).toBeVisible({ timeout: 5000 });
    await kernelBtn.click();

    const quickMenu = page.locator('.system-quick-menu');
    await expect(quickMenu).toBeVisible();
    await expect(quickMenu).toContainText(/Mihomo/i);

    // Verify quick action buttons exist
    const actionBtns = quickMenu.locator('.action-btn');
    await expect(actionBtns).toHaveCount(3);
  });

  test('navigates to traffic and dashboard upon segment clicks', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto('/#/dashboard');

    const trafficSegment = page.locator('.topbar .traffic-segment');
    await expect(trafficSegment).toBeVisible({ timeout: 5000 });
    await trafficSegment.click();

    await expect(page).toHaveURL(/#\/traffic/);

    const resSegment = page.locator('.topbar .resource-segment');
    await expect(resSegment).toBeVisible({ timeout: 5000 });
    await resSegment.click();

    await expect(page).toHaveURL(/#\/dashboard/);
  });

  test('displays mobile capsule in mobile header on small screens', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/#/dashboard');

    const mobileCapsule = page.locator('.mobile-header .capsule-mobile');
    await expect(mobileCapsule).toBeVisible({ timeout: 5000 });
    await expect(mobileCapsule).toContainText('Mihomo');

    // Tap opens quick actions popover
    await mobileCapsule.click();
    const quickMenu = page.locator('.mobile-header .system-quick-menu');
    await expect(quickMenu).toBeVisible();
  });

  test('displays EditorKernelWidget inside editor toolbar', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")');
    await expect(fileRow).toBeVisible({ timeout: 5000 });
    await fileRow.click();

    const editorWidget = page.locator('.editor-kernel-widget');
    await expect(editorWidget).toBeVisible({ timeout: 5000 });
    await expect(editorWidget).toContainText('Mihomo');

    const restartBtn = editorWidget.locator('.widget-restart-btn');
    await expect(restartBtn).toBeVisible();
  });

  test('settings page toggles capsule visibility', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto('/#/settings');

    const capsuleSection = page
      .locator('.card', { hasText: 'Капсула статуса системы' })
      .or(page.locator('.card', { hasText: 'System Status Capsule' }));
    await expect(capsuleSection).toBeVisible({ timeout: 5000 });

    const toggles = capsuleSection.locator('input[type="checkbox"]');
    await expect(toggles.first()).toBeChecked();
  });
});
