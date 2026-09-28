import { test, expect, type Page } from '@playwright/test';

// Экран первичной настройки пароля: общий с формой входа каркас,
// код настройки, отправка по Enter и разбор JSON-ошибок бэкенда.

test.use({ locale: 'ru-RU' });

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
  await expect(page.locator('#setup-code')).toBeFocused();

  const bgImage = await page
    .locator('.login-screen')
    .evaluate((el) => getComputedStyle(el).backgroundImage);
  expect(bgImage).toBe('none');
});

test('экран настройки подсказывает команды роутера для кода и сброса пароля', async ({ page }) => {
  await mockSetupApi(page, { status: 200, body: { success: true } });
  await page.goto('/');

  await expect(page.locator('code', { hasText: 'xcp --setup-code' })).toBeVisible();
  await expect(page.locator('code', { hasText: 'xcp --reset-password' })).toBeVisible();
});

test('Enter в поле подтверждения отправляет форму с кодом настройки', async ({ page }) => {
  const setupBodies = await mockSetupApi(page, {
    status: 400,
    body: { error: 'Password must be at least 8 characters' }
  });
  await page.goto('/');

  await page.locator('#setup-code').fill('A1B2C3D4');
  await page.locator('#password').fill('test-pass-123');
  await page.locator('#confirm').fill('test-pass-123');
  await page.locator('#confirm').press('Enter');

  await expect.poll(() => setupBodies.length).toBe(1);
  expect(JSON.parse(setupBodies[0])).toEqual({
    password: 'test-pass-123',
    setup_code: 'A1B2C3D4'
  });
});

test('неверный код настройки — ошибка текстом, без сырого JSON', async ({ page }) => {
  const setupBodies = await mockSetupApi(page, {
    status: 400,
    body: { success: false, error: 'invalid setup code', code: 'setup_code_invalid' }
  });
  await page.goto('/');

  await page.locator('#setup-code').fill('WRONGCOD');
  await page.locator('#password').fill('test-pass-123');
  await page.locator('#confirm').fill('test-pass-123');
  await page.locator('button[type="submit"]').click();

  await expect.poll(() => setupBodies.length).toBe(1);
  const alert = page.locator('.alert-error');
  await expect(alert).toHaveText('Неверный код настройки');
  await expect(alert).not.toContainText('{');
});

test('пустой код настройки — «Заполните все поля», запрос не уходит', async ({ page }) => {
  const setupBodies = await mockSetupApi(page, { status: 200, body: { success: true } });
  await page.goto('/');

  await page.locator('#password').fill('test-pass-123');
  await page.locator('#confirm').fill('test-pass-123');
  await page.locator('button[type="submit"]').click();

  await expect(page.locator('.alert-error')).toHaveText('Заполните все поля');
  expect(setupBodies).toHaveLength(0);
});

test('ошибка бэкенда показывается текстом, а не сырым JSON', async ({ page }) => {
  await mockSetupApi(page, {
    status: 400,
    body: { error: 'Password must be at least 8 characters' }
  });
  await page.goto('/');

  await page.locator('#setup-code').fill('A1B2C3D4');
  await page.locator('#password').fill('test-pass-123');
  await page.locator('#confirm').fill('test-pass-123');
  await page.locator('button[type="submit"]').click();

  const alert = page.locator('.alert-error');
  await expect(alert).toHaveText('Password must be at least 8 characters');
  await expect(alert).not.toContainText('{');
});

test('пароль из чёрного списка — ошибка политики, запрос не уходит', async ({ page }) => {
  const setupBodies = await mockSetupApi(page, { status: 200, body: { success: true } });
  await page.goto('/');

  await page.locator('#setup-code').fill('A1B2C3D4');
  await page.locator('#password').fill('password123');
  await page.locator('#confirm').fill('password123');
  await page.locator('button[type="submit"]').click();

  await expect(page.locator('.alert-error')).toHaveText('Пароль слишком простой — выберите другой');
  expect(setupBodies).toHaveLength(0);
});

test('пароль из одного повторяющегося символа — ошибка политики, запрос не уходит', async ({
  page
}) => {
  const setupBodies = await mockSetupApi(page, { status: 200, body: { success: true } });
  await page.goto('/');

  await page.locator('#setup-code').fill('A1B2C3D4');
  await page.locator('#password').fill('aaaaaaaa');
  await page.locator('#confirm').fill('aaaaaaaa');
  await page.locator('button[type="submit"]').click();

  await expect(page.locator('.alert-error')).toHaveText(
    'Пароль не может состоять из одного повторяющегося символа'
  );
  expect(setupBodies).toHaveLength(0);
});

test('ответ бэкенда password_too_long — переведённый текст, а не сырой код', async ({ page }) => {
  const setupBodies = await mockSetupApi(page, {
    status: 400,
    body: { success: false, error: 'password too long', code: 'password_too_long' }
  });
  await page.goto('/');

  // Валидный по клиентской политике пароль (<=72 байта), чтобы запрос дошёл
  // до мокнутого бэкенда — сервер здесь источник истины по факту длины.
  await page.locator('#setup-code').fill('A1B2C3D4');
  await page.locator('#password').fill('valid-password-1');
  await page.locator('#confirm').fill('valid-password-1');
  await page.locator('button[type="submit"]').click();

  await expect.poll(() => setupBodies.length).toBe(1);
  const alert = page.locator('.alert-error');
  await expect(alert).toHaveText('Пароль не должен превышать 72 байта');
  await expect(alert).not.toContainText('{');
});

test('несовпадающие пароли не отправляются на сервер', async ({ page }) => {
  const setupBodies = await mockSetupApi(page, { status: 200, body: { success: true } });
  await page.goto('/');

  await page.locator('#setup-code').fill('A1B2C3D4');
  await page.locator('#password').fill('test-pass-123');
  await page.locator('#confirm').fill('test-pass-456');
  await page.locator('button[type="submit"]').click();

  await expect(page.locator('.alert-error')).toBeVisible();
  expect(setupBodies).toHaveLength(0);
});
