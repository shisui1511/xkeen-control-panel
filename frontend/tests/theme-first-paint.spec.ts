import { test, expect, type Page } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

// e2e-pages: #/
// Тема и акцент должны ставиться блокирующим скриптом /theme-init.js в <head>
// до выполнения модуля приложения, чтобы первый кадр не мигал тёмной темой.

test.use({ locale: 'ru-RU' });

async function seedStorage(page: Page, values: Record<string, string>) {
  await page.addInitScript((entries) => {
    for (const [key, value] of Object.entries(entries)) {
      localStorage.setItem(key, value);
    }
  }, values);
}

// Модуль приложения не выполняется: атрибуты ставит только head-скрипт.
async function openWithoutAppModule(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });
  await page.route('**/src/main.ts', (route) => route.abort());
  await page.route('**/api/**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
  );
  await page.goto('/');
}

const html = (page: Page) => page.locator('html');

test.describe('theme-init.js: первый кадр без модуля приложения', () => {
  test.use({ colorScheme: 'dark' });

  test('theme=light из localStorage ставит data-theme=light', async ({ page }) => {
    await seedStorage(page, { theme: 'light' });
    await openWithoutAppModule(page);
    await expect(html(page)).toHaveAttribute('data-theme', 'light');
  });

  test('theme=dark из localStorage ставит data-theme=dark', async ({ page }) => {
    await seedStorage(page, { theme: 'dark' });
    await openWithoutAppModule(page);
    await expect(html(page)).toHaveAttribute('data-theme', 'dark');
  });

  test('accent=steel ставит data-accent=steel', async ({ page }) => {
    await seedStorage(page, { theme: 'light', accent: 'steel' });
    await openWithoutAppModule(page);
    await expect(html(page)).toHaveAttribute('data-accent', 'steel');
  });

  test('accent=red вне белого списка не ставит data-accent', async ({ page }) => {
    await seedStorage(page, { theme: 'light', accent: 'red' });
    await openWithoutAppModule(page);
    await expect(html(page)).toHaveAttribute('data-theme', 'light');
    await expect(html(page)).not.toHaveAttribute('data-accent', /.*/);
  });

  test('без сохранённого выбора и colorScheme=dark тема тёмная', async ({ page }) => {
    await openWithoutAppModule(page);
    await expect(html(page)).toHaveAttribute('data-theme', 'dark');
  });
});

test.describe('theme-init.js: системная тема и белый список', () => {
  test.use({ colorScheme: 'light' });

  test('без сохранённого выбора и colorScheme=light тема светлая', async ({ page }) => {
    await openWithoutAppModule(page);
    await expect(html(page)).toHaveAttribute('data-theme', 'light');
  });

  test('theme=foo вне белого списка падает на системную тему', async ({ page }) => {
    await seedStorage(page, { theme: 'foo' });
    await openWithoutAppModule(page);
    await expect(html(page)).toHaveAttribute('data-theme', 'light');
  });
});

test.describe('theme-init.js: согласованность с приложением', () => {
  test.use({ colorScheme: 'dark' });

  test('theme=light сохраняется после загрузки приложения', async ({ page }) => {
    await seedStorage(page, { theme: 'light' });
    await setupMocks(page, 'xray');
    await page.goto('/');
    await expect(page.locator('#app')).not.toBeEmpty();
    await expect(html(page)).toHaveAttribute('data-theme', 'light');
  });
});
