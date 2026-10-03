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
    await expect(page.locator('h1')).toBeVisible();

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
  // Запись B15 этапа 10: полный лог скачивается через запрос, а ошибка сервера
  // показывается уведомлением и не уводит со страницы на JSON ответа.
  test('полный лог: ошибка 404 показывает уведомление и не уводит со страницы (B15)', async ({
    page
  }) => {
    await page.route('**/api/logs/download', async (route) => {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ success: false, error: 'Log file does not exist' })
      });
    });
    await page.goto('/#/logs');

    const fullBtn = page.locator('.export-split button[aria-label]').first();
    await expect(fullBtn).toBeVisible();
    await fullBtn.click();

    await expect(page.locator('.toast--error')).toBeVisible();
    await expect(page).toHaveURL(/#\/logs/);
    await expect(page.locator('.logs-toolbar')).toBeVisible();
  });

  test('полный лог: успешный ответ скачивается файлом с именем из заголовка (B15)', async ({
    page
  }) => {
    await page.route('**/api/logs/download', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain; charset=utf-8',
        headers: { 'Content-Disposition': 'attachment; filename=xcp_logs_full_test.txt' },
        body: '===== /opt/var/log/xcp.log =====\nstarted\n'
      });
    });
    await page.goto('/#/logs');

    const fullBtn = page.locator('.export-split button[aria-label]').first();
    await expect(fullBtn).toBeVisible();
    const [download] = await Promise.all([page.waitForEvent('download'), fullBtn.click()]);
    expect(download.suggestedFilename()).toBe('xcp_logs_full_test.txt');
  });
});
