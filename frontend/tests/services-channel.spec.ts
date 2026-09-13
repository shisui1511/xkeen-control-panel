import { test, expect } from '@playwright/test';

// Covers the "Канал и обновления" card on the Services page: a failed
// update check must be visible (not indistinguishable from "up to date"),
// and a channel mismatch between the two kernels must be surfaced rather
// than silently hidden behind whichever kernel happens to be checked first.

async function mockCommonRoutes(page: import('@playwright/test').Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

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
            mihomo: { reachable: true, process_running: false, api_reachable: false }
          }
        })
      });
    } else if (url.includes('/api/settings')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: { dev_mode: false } })
      });
    } else if (url.includes('/api/version')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: 'v0.25.0' })
      });
    } else if (url.includes('/api/service/restart-log')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
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
            uptime: '1h',
            binary_path: '/opt/sbin/xkeen',
            raw: 'Xray-core (running)\nXKeen is running'
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
}

test.describe('Services page — channel & updates card', () => {
  test.beforeEach(async ({ page }) => {
    await mockCommonRoutes(page);
  });

  test('shows a failed-check badge and the error detail instead of "up to date"', async ({
    page
  }) => {
    await page.route('**/api/kernels', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
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
              latest_version: '',
              has_update: false,
              channel: 'stable',
              status: 'failed',
              process_status: 'running',
              message: 'GitHub API error: dial tcp: lookup api.github.com: no such host'
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
              message: ''
            }
          ]
        })
      });
    });

    await page.goto('/#/services');

    const xrayItem = page.locator('.update-item', { hasText: 'Xray' });
    await expect(xrayItem.locator('.status-badge')).toContainText(/error|ошибка/i);
    await expect(xrayItem).not.toContainText(/up to date|актуально/i);
    await expect(xrayItem.locator('.update-hint')).toContainText('GitHub API error');
  });

  test('surfaces a hint when the two kernels have diverging channels', async ({ page }) => {
    await page.route('**/api/kernels', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
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
              message: ''
            },
            {
              name: 'mihomo',
              display_name: 'Mihomo',
              binary_path: '/opt/bin/mihomo',
              current_version: '1.18.0',
              latest_version: '1.18.0',
              has_update: false,
              channel: 'preview',
              status: 'idle',
              process_status: 'stopped',
              message: ''
            }
          ]
        })
      });
    });

    await page.goto('/#/services');

    await expect(page.locator('.channel-mismatch-hint')).toBeVisible();
    // Neither segment should read as the exclusively active one while diverged.
    const channelGroup = page.getByRole('group', { name: /update channel|канал обновлений/i });
    const stableBtn = channelGroup.getByRole('button', { name: /^(stable|стабильный)$/i });
    const previewBtn = channelGroup.getByRole('button', { name: /^(preview|предварительный)$/i });
    await expect(stableBtn).not.toHaveClass(/active/);
    await expect(previewBtn).not.toHaveClass(/active/);
  });

  test('shows an error toast when changing the channel fails on the backend', async ({ page }) => {
    await page.route('**/api/kernels', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
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
              message: ''
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
              message: ''
            }
          ]
        })
      });
    });
    await page.route('**/api/kernels/xray/channel', async (route) => {
      await route.fulfill({ status: 500, contentType: 'text/plain', body: 'internal error' });
    });
    await page.route('**/api/kernels/mihomo/channel', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: { channel: 'preview' } })
      });
    });

    await page.goto('/#/services');

    const channelGroup = page.getByRole('group', { name: /update channel|канал обновлений/i });
    const previewBtn = channelGroup.getByRole('button', { name: /^(preview|предварительный)$/i });
    await previewBtn.click();

    const toast = page.locator('.toast, [role="alert"]');
    await expect(toast.first()).toBeVisible({ timeout: 3000 });
    await expect(toast.first()).toContainText(/канал|channel/i);
  });

  test('shows reinstall, rollback, and upload buttons and does not offer browser download', async ({
    page
  }) => {
    let rollbackTriggered = false;
    await page.route('**/api/kernels', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: [
            {
              name: 'mihomo',
              display_name: 'Mihomo',
              binary_path: '/opt/sbin/mihomo',
              current_version: '1.19.30',
              latest_version: '1.19.30',
              has_update: false,
              has_backup: true,
              channel: 'stable',
              status: 'idle',
              process_status: 'running',
              message: ''
            },
            {
              name: 'xray',
              display_name: 'Xray',
              binary_path: '/opt/sbin/xray',
              current_version: '26.7.28',
              latest_version: '26.7.28',
              has_update: false,
              has_backup: false,
              channel: 'stable',
              status: 'idle',
              process_status: 'stopped',
              message: ''
            }
          ]
        })
      });
    });

    await page.route('**/api/kernels/mihomo/rollback', async (route) => {
      rollbackTriggered = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: { status: 'rolled_back' } })
      });
    });

    page.on('dialog', async (dialog) => {
      await dialog.accept();
    });

    await page.goto('/#/services');

    const mihomoItem = page.locator('.update-item', { hasText: 'Mihomo' });
    const xrayItem = page.locator('.update-item', { hasText: 'Xray' });

    // 1. Browser download links/buttons must NOT exist
    await expect(page.locator('a[href*="/api/kernels/"][href*="/download"]')).toHaveCount(0);

    // 2. Reinstall button is present for up-to-date kernels
    const mihomoReinstallBtn = mihomoItem.locator(
      'button[aria-label*="Переустановить"], button[aria-label*="Reinstall"]'
    );
    await expect(mihomoReinstallBtn).toBeVisible();

    // 3. Rollback button is present when has_backup is true (mihomo), and absent when false (xray)
    const mihomoRollbackBtn = mihomoItem.locator(
      'button[aria-label*="Откатить"], button[aria-label*="Rollback"]'
    );
    await expect(mihomoRollbackBtn).toBeVisible();

    const xrayRollbackBtn = xrayItem.locator(
      'button[aria-label*="Откатить"], button[aria-label*="Rollback"]'
    );
    await expect(xrayRollbackBtn).toHaveCount(0);

    // 4. Upload button is present for both kernels
    const mihomoUploadBtn = mihomoItem.locator(
      'button[aria-label*="Загрузить"], button[aria-label*="Upload"]'
    );
    await expect(mihomoUploadBtn).toBeVisible();

    // 5. Clicking rollback triggers confirmation dialog and POST /api/kernels/mihomo/rollback
    await mihomoRollbackBtn.click();
    await expect.poll(() => rollbackTriggered).toBe(true);
  });
});
