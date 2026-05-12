import { test, expect } from '@playwright/test';

test('has title', async ({ page }) => {
  await page.goto('/');
  // Expect a title "to contain" a substring.
  await expect(page).toHaveTitle(/XKeen Control Panel/);
});

test('shows login form', async ({ page }) => {
  await page.goto('/');
  // Expect login form elements to be visible
  await expect(page.locator('#password')).toBeVisible();
  await expect(page.locator('button', { hasText: /Войти|Login/ })).toBeVisible();
});
