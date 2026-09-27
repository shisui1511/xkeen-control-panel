import { test, expect, type Page } from '@playwright/test';

// Экран входа: чекбокс «Запомнить меня» (D-07), показ/скрытие пароля (D-18)
// и инструкция «Забыли пароль?» с копированием команды (D-23).

test.use({ locale: 'ru-RU' });

async function mockLoginApi(
  page: Page,
  loginResponse: { status: number; body: unknown } = {
    status: 200,
    body: { csrf_token: 'mock-csrf-token' }
  }
) {
  const loginBodies: string[] = [];

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
        body: JSON.stringify({ authenticated: false, setup_required: false })
      });
    } else if (url.includes('/api/auth/login')) {
      loginBodies.push(route.request().postData() ?? '');
      await route.fulfill({
        status: loginResponse.status,
        contentType: 'application/json',
        body: JSON.stringify(loginResponse.body)
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

  return loginBodies;
}

test('«Запомнить меня» отмечен — remember_me: true в теле запроса', async ({ page }) => {
  const loginBodies = await mockLoginApi(page);
  await page.goto('/');

  await page.locator('#password').fill('test-pass-123');
  await page.locator('.remember-me input[type="checkbox"]').check();
  await page.locator('button.btn-primary').click();

  await expect.poll(() => loginBodies.length).toBe(1);
  expect(JSON.parse(loginBodies[0])).toEqual({ password: 'test-pass-123', remember_me: true });
});

test('«Запомнить меня» не отмечен — remember_me: false в теле запроса', async ({ page }) => {
  const loginBodies = await mockLoginApi(page);
  await page.goto('/');

  await page.locator('#password').fill('test-pass-123');
  await page.locator('button.btn-primary').click();

  await expect.poll(() => loginBodies.length).toBe(1);
  expect(JSON.parse(loginBodies[0])).toEqual({ password: 'test-pass-123', remember_me: false });
});

test('кнопка-глаз переключает видимость пароля', async ({ page }) => {
  await mockLoginApi(page);
  await page.goto('/');

  const input = page.locator('#password');
  const toggle = page.locator('.password-toggle');

  await expect(input).toHaveAttribute('type', 'password');
  await expect(toggle).toHaveAttribute('aria-label', 'Показать пароль');

  await toggle.click();

  await expect(input).toHaveAttribute('type', 'text');
  await expect(toggle).toHaveAttribute('aria-label', 'Скрыть пароль');
});

test('пустой пароль — кнопка «Войти» disabled, запрос не уходит', async ({ page }) => {
  const loginBodies = await mockLoginApi(page);
  await page.goto('/');

  const submit = page.locator('button.btn-primary');
  await expect(submit).toBeDisabled();

  await submit.click({ force: true });
  expect(loginBodies).toHaveLength(0);
});

test('неверный пароль — ошибка в .alert-error', async ({ page }) => {
  await mockLoginApi(page, { status: 401, body: { success: false, error: 'Invalid password' } });
  await page.goto('/');

  await page.locator('#password').fill('wrong-pass');
  await page.locator('button.btn-primary').click();

  await expect(page.locator('.alert-error')).toHaveText('Неверный пароль');
});

test('429 с retry_after — ошибка содержит число секунд', async ({ page }) => {
  await mockLoginApi(page, { status: 429, body: { error: 'rate limited', retry_after: 30 } });
  await page.goto('/');

  await page.locator('#password').fill('test-pass-123');
  await page.locator('button.btn-primary').click();

  await expect(page.locator('.alert-error')).toContainText('30');
});
