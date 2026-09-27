import { test, expect, type Page } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// e2e-pages: settings, dashboard

test.use({ locale: 'ru-RU' });

// ============================================================
// D-19: 401 с причиной → тост → вход на месте → тот же маршрут.
// ============================================================

async function triggerReasonedLogout(page: Page, reason?: string): Promise<void> {
  await setupMocks(page, 'mihomo');

  let trigger401 = false;
  await page.route('**/api/system/stats', async (route) => {
    if (!trigger401) {
      await route.fallback();
      return;
    }
    const body: Record<string, unknown> = { success: false, error: 'Unauthorized' };
    if (reason) body.reason = reason;
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify(body)
    });
  });

  await page.route('**/api/auth/login', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ csrf_token: 'mock-csrf-token-2' })
    });
  });

  await visitPage(page, '/#/settings?tab=security');
  await expect(page.getByRole('tab', { name: /Безопасность|Security/ }).first()).toBeVisible();

  // Помечает страницу, чтобы проверить позже, что реального reload не было —
  // 401 должен переключить экран «на месте» (D-19), не через navigation.
  await page.evaluate(() => {
    (window as unknown as { __noReload?: boolean }).__noReload = true;
  });

  trigger401 = true;
  await page.waitForResponse(
    (response) => response.url().includes('/api/system/stats') && response.status() === 401
  );
}

test('401 password_changed: тост с причиной и вход возвращает на тот же маршрут без reload', async ({
  page
}) => {
  await triggerReasonedLogout(page, 'password_changed');

  await expect(page.locator('.toast__message')).toContainText('Пароль изменён');
  await expect(page.locator('#password')).toBeVisible();
  await expect(page).toHaveURL(/#\/settings/);

  await page.locator('#password').fill('new-password-123');
  await page.getByRole('button', { name: /Войти|Login/ }).click();

  await expect(page.getByRole('tab', { name: /Безопасность|Security/ }).first()).toBeVisible();
  await expect(page).toHaveURL(/#\/settings/);
  expect(
    await page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload)
  ).toBe(true);
});

test('401 terminated_elsewhere: тост «Сессия завершена с другого устройства»', async ({ page }) => {
  await triggerReasonedLogout(page, 'terminated_elsewhere');

  await expect(page.locator('.toast__message')).toContainText(
    'Сессия завершена с другого устройства'
  );
  await expect(page.locator('#password')).toBeVisible();
});

test('401 без reason: запасной тост «Сессия истекла, выполните повторный вход»', async ({
  page
}) => {
  await triggerReasonedLogout(page, undefined);

  await expect(page.locator('.toast__message')).toContainText(
    'Сессия истекла, выполните повторный вход'
  );
  await expect(page.locator('#password')).toBeVisible();
});
