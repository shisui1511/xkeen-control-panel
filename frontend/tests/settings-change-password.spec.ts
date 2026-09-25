import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// Неверный текущий пароль при смене пароля: понятная ошибка вместо сырого
// JSON, пользователь остаётся в панели (бэкенд отвечает 403, не 401).

test('неверный текущий пароль не разлогинивает и показывает текст ошибки', async ({ page }) => {
  await setupMocks(page, 'mihomo');
  await page.route('**/api/auth/change-password', async (route) => {
    await route.fulfill({
      status: 403,
      contentType: 'application/json',
      body: JSON.stringify({ success: false, error: 'Текущий пароль неверен' })
    });
  });

  await visitPage(page, '/#/settings');
  await page
    .getByRole('tab', { name: /Безопасность|Security/ })
    .first()
    .click();

  await page.locator('#curr-pwd').fill('wrong-current');
  await page.locator('#new-pwd').fill('new-pass-123');
  await page.locator('#conf-pwd').fill('new-pass-123');
  await page.locator('.card-actions .btn-primary').first().click();

  const error = page.locator('.field-error');
  await expect(error).toHaveText('Текущий пароль неверен');
  await expect(error).not.toContainText('{');
  await expect(page).toHaveURL(/#\/settings/);
  await expect(page.locator('#curr-pwd')).toBeVisible();
});
