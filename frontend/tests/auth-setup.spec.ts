import { test, expect, type Page } from '@playwright/test';

// Экран первичной настройки пароля: общий с формой входа каркас,
// отправка по Enter и разбор JSON-ошибок бэкенда.

async function mockSetupApi(page: Page, setupResponse: { status: number; body: unknown }) {
  const setupBodies: string[] = [];

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
        body: JSON.stringify({ authenticated: false, setup_required: true })
      });
    } else if (url.includes('/api/auth/setup')) {
      setupBodies.push(route.request().postData() ?? '');
      await route.fulfill({
        status: setupResponse.status,
        contentType: 'application/json',
        body: JSON.stringify(setupResponse.body)
      });
    } else if (url.includes('/api/version')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ panel_version: 'v9.9.9' })
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    }
  });

  return setupBodies;
}

test('экран настройки использует каркас формы входа без радиального фона', async ({ page }) => {
  await mockSetupApi(page, { status: 200, body: { success: true } });
  await page.goto('/');

  await expect(page.locator('.login-card .login-brand')).toBeVisible();
  await expect(page.locator('.login-footer')).toContainText('v9.9.9');
  await expect(page.locator('#password')).toBeFocused();

  const bgImage = await page
    .locator('.login-screen')
    .evaluate((el) => getComputedStyle(el).backgroundImage);
  expect(bgImage).toBe('none');
});

test('Enter в поле подтверждения отправляет форму', async ({ page }) => {
  const setupBodies = await mockSetupApi(page, {
    status: 400,
    body: { error: 'Password must be at least 8 characters' }
  });
  await page.goto('/');

  await page.locator('#password').fill('test-pass-123');
  await page.locator('#confirm').fill('test-pass-123');
  await page.locator('#confirm').press('Enter');

  await expect.poll(() => setupBodies.length).toBe(1);
  expect(JSON.parse(setupBodies[0])).toEqual({ password: 'test-pass-123' });
});

test('ошибка бэкенда показывается текстом, а не сырым JSON', async ({ page }) => {
  await mockSetupApi(page, {
    status: 400,
    body: { error: 'Password must be at least 8 characters' }
  });
  await page.goto('/');

  await page.locator('#password').fill('test-pass-123');
  await page.locator('#confirm').fill('test-pass-123');
  await page.locator('button[type="submit"]').click();

  const alert = page.locator('.alert-error');
  await expect(alert).toHaveText('Password must be at least 8 characters');
  await expect(alert).not.toContainText('{');
});

test('несовпадающие пароли не отправляются на сервер', async ({ page }) => {
  const setupBodies = await mockSetupApi(page, { status: 200, body: { success: true } });
  await page.goto('/');

  await page.locator('#password').fill('test-pass-123');
  await page.locator('#confirm').fill('test-pass-456');
  await page.locator('button[type="submit"]').click();

  await expect(page.locator('.alert-error')).toBeVisible();
  expect(setupBodies).toHaveLength(0);
});
