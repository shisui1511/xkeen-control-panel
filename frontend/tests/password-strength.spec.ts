// e2e-pages: setup
import { test, expect, type Page } from '@playwright/test';

// Индикатор надёжности пароля (D-18): библиотека zxcvbn грузится лениво,
// только внутри формы Setup, никогда на экране входа; индикатор не блокирует
// отправку формы даже при слабом, но допустимом политикой пароле.

test.use({ locale: 'ru-RU' });

async function mockCommonApi(page: Page, meBody: unknown, setupBodies?: string[]) {
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
        body: JSON.stringify(meBody)
      });
    } else if (url.includes('/api/auth/setup')) {
      setupBodies?.push(route.request().postData() ?? '');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    } else if (url.includes('/api/auth/login')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ csrf_token: 'test-csrf' })
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
}

test('на экране входа zxcvbn не загружается', async ({ page }) => {
  const requestUrls: string[] = [];
  page.on('request', (req) => requestUrls.push(req.url()));

  await mockCommonApi(page, { authenticated: false, setup_required: false });
  await page.goto('/');

  await expect(page.locator('#password')).toBeVisible();
  // Даём время на то, чтобы случайный ленивый импорт успел бы себя проявить.
  await page.waitForTimeout(1000);

  expect(requestUrls.some((url) => url.includes('zxcvbn'))).toBe(false);
});

test('на экране setup индикатор появляется лениво в течение 5 секунд', async ({ page }) => {
  const requestUrls: string[] = [];
  page.on('request', (req) => requestUrls.push(req.url()));

  await mockCommonApi(page, { authenticated: false, setup_required: true });
  await page.goto('/');

  await page.locator('#password').fill('abcdefgh');

  await expect(async () => {
    const text = await page.locator('.strength-label').textContent();
    expect(['Очень слабый', 'Слабый']).toContain(text?.trim());
  }).toPass({ timeout: 5000 });

  expect(requestUrls.some((url) => url.includes('zxcvbn'))).toBe(true);
});

test('слабый, но допустимый политикой пароль всё равно отправляется', async ({ page }) => {
  const setupBodies: string[] = [];
  await mockCommonApi(page, { authenticated: false, setup_required: true }, setupBodies);
  await page.goto('/');

  await page.locator('#setup-code').fill('A1B2C3D4');
  await page.locator('#password').fill('abcdefgh');
  await page.locator('#confirm').fill('abcdefgh');
  await page.locator('button[type="submit"]').click();

  await expect.poll(() => setupBodies.length).toBe(1);
  expect(JSON.parse(setupBodies[0])).toEqual({
    password: 'abcdefgh',
    setup_code: 'A1B2C3D4'
  });
});

test('надёжный пароль показывает «Хороший» или «Отличный»', async ({ page }) => {
  await mockCommonApi(page, { authenticated: false, setup_required: true });
  await page.goto('/');

  await page.locator('#password').fill('correct-horse-battery-staple-42');

  await expect(async () => {
    const text = await page.locator('.strength-label').textContent();
    expect(['Хороший', 'Отличный']).toContain(text?.trim());
  }).toPass({ timeout: 5000 });
});
