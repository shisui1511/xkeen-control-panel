import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Templates modal integration test suite', () => {
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
      const method = route.request().method();

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
          body: JSON.stringify([
            {
              name: 'Xray: VLESS + Reality',
              description: 'VLESS + XTLS Vision Reality — обходит DPI без SNI-индикатора',
              type: 'xray',
              filename: 'vless-reality.json'
            },
            {
              name: 'Mihomo: RU Bypass (ZKeen)',
              description: 'CIS-oriented: GEOSITE/GEOIP правила для заблокированных сервисов РФ',
              type: 'mihomo',
              filename: 'ru-bypass.yaml'
            }
          ])
        });
      } else if (url.includes('/api/templates/fetch')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ content: '# template content\nline2\nline3' })
        });
      } else if (url.includes('/api/templates/update') && method === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ updated: 2 })
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

  test('modal opens and shows Xray/Mihomo tabs', async ({ page }) => {
    await page.goto('/#/editor');

    // Открываем модалку шаблонов через кнопку в тулбаре
    const templatesBtn = page.locator('button:has-text("Шаблоны"), button[title*="шаблон"], button[aria-label*="шаблон"]').first();
    await expect(templatesBtn).toBeVisible();
    await templatesBtn.click();

    // Проверяем, что модалка открылась с табами Xray/Mihomo
    const xrayTab = page.locator('.templates-tabs button:has-text("Xray"), [role="tab"]:has-text("Xray")').first();
    const mihomoTab = page.locator('.templates-tabs button:has-text("Mihomo"), [role="tab"]:has-text("Mihomo")').first();

    await expect(xrayTab).toBeVisible();
    await expect(mihomoTab).toBeVisible();
  });

  test('selecting template shows preview', async ({ page }) => {
    await page.goto('/#/editor');

    // Открываем модалку шаблонов
    const templatesBtn = page.locator('button:has-text("Шаблоны"), button[title*="шаблон"], button[aria-label*="шаблон"]').first();
    await expect(templatesBtn).toBeVisible();
    await templatesBtn.click();

    // Кликаем на элемент списка шаблонов
    const templateItem = page.locator('.template-item, .template-list button').first();
    await expect(templateItem).toBeVisible();
    await templateItem.click();

    // Проверяем, что preview-панель содержит текст
    const preview = page.locator('.template-preview-code, .templates-col-preview, .template-preview').first();
    await expect(preview).toBeVisible();
  });

  test('update button is visible in modal header', async ({ page }) => {
    await page.goto('/#/editor');

    // Открываем модалку шаблонов
    const templatesBtn = page.locator('button:has-text("Шаблоны"), button[title*="шаблон"], button[aria-label*="шаблон"]').first();
    await expect(templatesBtn).toBeVisible();
    await templatesBtn.click();

    // Кнопка «Обновить шаблоны» видна в хедере модалки
    const updateBtn = page.locator('button:has-text("Обновить")').first();
    await expect(updateBtn).toBeVisible();
  });
});
