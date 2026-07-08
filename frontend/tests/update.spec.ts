import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Templates updates Settings tab test suite', () => {
  let mockStatus = {
    templates_repo_url: 'https://example.com/templates',
    current_version: '1.0.0',
    last_updated: '2026-06-13T12:00:00Z',
    last_check: '2026-06-13T12:00:00Z',
    has_update: false,
    incompatible: false,
    warning_message: ''
  };

  test.beforeEach(async ({ page }) => {
    // Disable service worker in tests
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Reset default mock state
    mockStatus = {
      templates_repo_url: 'https://example.com/templates',
      current_version: '1.0.0',
      last_updated: '2026-06-13T12:00:00Z',
      last_check: '2026-06-13T12:00:00Z',
      has_update: false,
      incompatible: false,
      warning_message: ''
    };

    // Route API calls
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      const method = route.request().method();

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
      } else if (url.includes('/api/templates/status')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockStatus)
        });
      } else if (url.includes('/api/templates/check') && method === 'POST') {
        mockStatus.has_update = true;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ has_update: true })
        });
      } else if (url.includes('/api/templates/update') && method === 'POST') {
        mockStatus.has_update = false;
        mockStatus.current_version = '1.0.1';
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ updated: 2 })
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

  test('scenario 1: shows updates available and installs successfully', async ({ page }) => {
    // Go to settings page
    await page.goto('/#/settings');

    // Click Updates tab
    const updatesTab = page
      .locator('.stab:has-text("Обновления"), .stab:has-text("Updates")')
      .first();
    await expect(updatesTab).toBeVisible({ timeout: 5000 });
    await updatesTab.click();

    // Check updates section is visible
    const updatesTitle = page
      .locator('.card-label:has-text("Шаблоны"), .card-label:has-text("Templates")')
      .first();
    await expect(updatesTitle).toBeVisible();

    // Verify initial version is shown
    await expect(page.locator('.card:has-text("Шаблоны")').first()).toContainText('1.0.0');

    // Scope to templates card
    const templatesCard = page
      .locator('.card:has-text("Шаблоны"), .card:has-text("Templates")')
      .first();

    // Trigger update check
    const checkBtn = templatesCard
      .locator('button:has-text("Проверить обновления"), button:has-text("Check for updates")')
      .first();
    await expect(checkBtn).toBeVisible();
    await checkBtn.click();

    // "Update available" text should appear
    const updateAvailable = templatesCard.locator('text=Доступно обновление').first();
    await expect(updateAvailable).toBeVisible();

    // "Install Updates" button should be visible
    const installBtn = templatesCard
      .locator('button:has-text("Установить обновления"), button:has-text("Install updates")')
      .first();
    await expect(installBtn).toBeVisible();
    await installBtn.click();

    // Wait for update success toast (templates_updated)
    const successToast = page.locator('.toast, :has-text("Шаблоны обновлены")').first();
    await expect(successToast).toBeVisible({ timeout: 5000 });

    // Version should be updated to 1.0.1
    await expect(templatesCard).toContainText('1.0.1');
  });

  test('scenario 2: shows warning alert if remote schema version is incompatible', async ({
    page
  }) => {
    // Set incompatible status before routing
    mockStatus.has_update = true;
    mockStatus.incompatible = true;
    mockStatus.warning_message =
      'incompatible schema version: remote major version 2 is greater than supported local major version 1';

    // Go to settings page
    await page.goto('/#/settings');

    // Click Updates tab
    const updatesTab = page
      .locator('.stab:has-text("Обновления"), .stab:has-text("Updates")')
      .first();
    await expect(updatesTab).toBeVisible({ timeout: 5000 });
    await updatesTab.click();

    // Verify that the alert warning banner is visible
    const alertBanner = page.locator('.alert-warning').first();
    await expect(alertBanner).toBeVisible();
    await expect(alertBanner).toContainText('Версия схемы не поддерживается');
    await expect(alertBanner).toContainText('incompatible schema version');
  });
});
