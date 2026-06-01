import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  // Отключаем Service Worker в тестах, чтобы запросы к API перехватывались через page.route
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  // Перехватываем все запросы к API
  await page.route('**/api/**', async (route) => {
    const url = route.request().url();

    if (url.includes('/api/auth/me')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          authenticated: false,
          setup_required: false,
          csrf_token: 'mock-csrf-token'
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

test('has title', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveTitle(/XKeen Control Panel/);
});

test('shows auth form', async ({ page }) => {
  await page.goto('/');
  // Password field is present on both Login and Setup screens
  await expect(page.locator('#password')).toBeVisible();
  // Button text depends on whether setup is required or login is shown
  // Setup: "Установить пароль" / "Set Password"
  // Login: "Войти" / "Login"
  await expect(page.locator('button.btn-primary')).toBeVisible();
});
