import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Xray Constructor integration test suite', () => {
  test.beforeEach(async ({ page }) => {
    // Отключаем Service Worker в тестах
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Перехватываем все запросы к API
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();

      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: true,
            setup_required: false,
            csrf_token: 'mock-csrf-token'
          })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              kernels: {
                xray: { installed: true, version: '1.8.4', channel: 'stable' },
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: 'mihomo',
              mihomo: {
                reachable: true,
                process_running: true,
                api_reachable: true,
                api_authenticated: true
              }
            }
          })
        });
      } else if (url.includes('/api/config/list')) {
        const isMihomo = url.includes('mihomo');
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(
            isMihomo
              ? [{ name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 1500 }]
              : [{ name: 'xray-config.json', path: '/opt/etc/xray/configs/xray-config.json', size: 1200 }]
          )
        });
      } else if (url.includes('/api/config/read')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: 'initial config content'
        });
      } else if (url.includes('/api/templates/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  });

  test('deep-link #/constructor automatically opens editor on constructor tab', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Вкладка «Конструктор» должна быть активна
    const constructorTab = page.locator('button.tab-btn:has-text("Конструктор")');
    await expect(constructorTab).toHaveClass(/active/);
    await expect(page).toHaveURL(/#\/constructor/);
  });

  test('Xray Constructor generates valid JSON', async ({ page }) => {
    await page.goto('/#/constructor');

    // Переключиться на Xray-сторону конструктора
    const xrayKernelBtn = page.locator('button:has-text("Xray")').first();
    await expect(xrayKernelBtn).toBeVisible();
    await xrayKernelBtn.click();

    // Заполнить поле сервера
    const serverInput = page.locator('input[placeholder*="example.com"], input[placeholder*="сервер"], input[name="server"]').first();
    await expect(serverInput).toBeVisible();
    await serverInput.fill('my-server.com');

    // Проверить, что preview-панель содержит валидный JSON
    const previewPane = page.locator('.json-preview, .constructor-preview-pane, .xray-preview').first();
    await expect(previewPane).toBeVisible();

    const previewText = await previewPane.textContent();
    expect(() => JSON.parse(previewText || '')).not.toThrow();

    // Кнопка «Открыть в редакторе» не должна быть задизейблена
    const openBtn = page.locator('button:has-text("Открыть в редакторе")').first();
    await expect(openBtn).not.toBeDisabled();
  });
});
