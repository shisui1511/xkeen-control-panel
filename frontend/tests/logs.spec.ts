import { test, expect } from '@playwright/test';

test.describe('System Logs Console test suite', () => {
  test.beforeEach(async ({ page }) => {
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
      } else if (url.includes('/api/settings')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: { dev_mode: false }
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
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  });

  test('renders logs page toolbar, search filter, and controls', async ({ page }) => {
    await page.goto('/#/logs');

    // Check page title and toolbar elements
    await expect(page.locator('.page-head h1')).toBeVisible();

    const toolbar = page.locator('.logs-toolbar');
    await expect(toolbar).toBeVisible();

    const searchInput = page.locator('.search-input');
    await expect(searchInput).toBeVisible();

    const pauseBtn = page.locator(
      '.logs-toolbar button:has-text("Пауза"), .logs-toolbar button:has-text("Pause")'
    );
    await expect(pauseBtn).toBeVisible();

    const clearBtn = page.locator(
      '.logs-toolbar button:has-text("Очистить"), .logs-toolbar button:has-text("Clear")'
    );
    await expect(clearBtn).toBeVisible();

    // Toggle pause
    await pauseBtn.click();
    const resumeBtn = page.locator(
      '.logs-toolbar button:has-text("Возобновить"), .logs-toolbar button:has-text("Resume")'
    );
    await expect(resumeBtn).toBeVisible();

    // Toggle back
    await resumeBtn.click();
    await expect(pauseBtn).toBeVisible();
  });
});
