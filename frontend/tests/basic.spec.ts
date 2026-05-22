import { test, expect } from '@playwright/test';

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
