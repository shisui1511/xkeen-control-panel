import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

// Открытие конструктора Mihomo читает файл конфигурации ровно один раз:
// и при прямом переходе на #/constructor, и при переходе из Редактора
// с выбранным файлом.

const CONFIG_PATH = '/opt/etc/mihomo/config.yaml';

test.describe('конструктор Mihomo: одно чтение конфига', () => {
  let readCount = 0;

  test.beforeEach(async ({ page }) => {
    readCount = 0;

    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    page.on('request', (req) => {
      const url = req.url();
      if (url.includes('/api/config/read') && url.includes('config.yaml')) readCount++;
    });

    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      const json = (body: unknown) =>
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(body)
        });

      if (url.includes('/api/auth/me')) {
        await json({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' });
      } else if (url.includes('/api/capabilities')) {
        await json({
          success: true,
          data: {
            kernels: {
              xray: { installed: true, version: '1.8.4', channel: 'stable' },
              mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
            },
            active_kernel: 'mihomo'
          }
        });
      } else if (url.includes('/api/config/list')) {
        await json(
          url.includes('mihomo') ? [{ name: 'config.yaml', path: CONFIG_PATH, size: 1500 }] : []
        );
      } else if (url.includes('/api/config/read')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: 'proxies: []\nproxy-groups: []\nrules: []\n'
        });
      } else if (url.includes('/api/assets/definition')) {
        await json({});
      } else {
        await json({ success: true, data: {} });
      }
    });
  });

  test('прямой #/constructor: ровно одно чтение', async ({ page }) => {
    await page.goto('/#/constructor');
    await page.waitForSelector('.gen-layout', { timeout: 8000 });
    await page.waitForTimeout(2000);
    expect(readCount).toBe(1);
  });

  test('переход из Редактора с выбранным файлом: ровно одно чтение', async ({ page }) => {
    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")');
    await expect(fileRow).toBeVisible();
    await fileRow.click();
    await expect(page.locator('.file-name:has-text("config.yaml")')).toBeVisible();
    // Содержимое файла прочитано редактором — счётчик отсчитывает только конструктор
    await expect.poll(() => readCount).toBeGreaterThan(0);
    await page.waitForTimeout(500);
    readCount = 0;

    await page.locator('button.tab-btn:has-text("Конструктор")').click();
    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible();
    if ((await mihomoKernelBtn.getAttribute('aria-pressed')) !== 'true') {
      await mihomoKernelBtn.click();
    }
    await page.waitForSelector('.gen-layout', { timeout: 8000 });
    await page.waitForTimeout(3000);
    expect(readCount).toBe(1);
  });
});
