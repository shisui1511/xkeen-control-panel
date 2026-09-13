import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Settings updates tab test suite', () => {
  test.beforeEach(async ({ page }) => {
    // Disable service worker in tests
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Route API calls
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
      } else if (url.includes('/api/update/channel')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ channel: 'stable' })
        });
      } else if (url.includes('/api/version')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ panel_version: '1.0.0' })
        });
      } else if (url.includes('/api/update/status')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'idle', message: '', progress: 0 })
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

  test('settings updates tab renders without templates card', async ({ page }) => {
    await page.goto('/#/settings');

    const updatesTab = page
      .locator(
        '[role="tab"]:has-text("Обновления"), [role="tab"]:has-text("Updates"), .tab-btn:has-text("Обновления"), .tab-btn:has-text("Updates"), .stab:has-text("Обновления"), .stab:has-text("Updates")'
      )
      .first();
    await expect(updatesTab).toBeVisible({ timeout: 5000 });
    await updatesTab.click();

    // Карточка обновления панели управления отображается
    const panelUpdateTitle = page
      .locator('.card-label:has-text("Обновление"), .card-label:has-text("Update")')
      .first();
    await expect(panelUpdateTitle).toBeVisible();

    // Карточка «Шаблоны» отсутствует
    const templatesTitle = page.locator(
      '.card-label:has-text("Шаблоны"), .card-label:has-text("Templates")'
    );
    await expect(templatesTitle).toHaveCount(0);
  });
});
