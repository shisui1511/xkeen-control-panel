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
              name: 'Xray: Routing — Selective (GeoSite/GeoIP)',
              description: 'Selective routing: GeoSite/GeoIP → PROXY_TAG, private → direct, ads → block',
              type: 'xray',
              filename: 'selective-routing.json'
            },
            {
              name: 'Mihomo: Rule-Based Routing',
              description: 'Selective routing: MetaCubeX rule-sets, fake-ip DNS, proxy-providers',
              type: 'mihomo',
              filename: 'rule-based.yaml'
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

  // Вспомогательная функция: открыть файл и затем модалку шаблонов через kebab-меню
  async function openTemplatesModal(page: any) {
    await page.goto('/#/editor');
    // Ждём загрузки списка файлов
    await page.waitForSelector('.file-row', { timeout: 8000 });
    // Кликнуть на первый файл в списке чтобы появился toolbar с kebab
    const fileRow = page.locator('.file-row').first();
    await expect(fileRow).toBeVisible({ timeout: 5000 });
    await fileRow.click();
    // Ждём появления kebab-кнопки (отображается только когда файл выбран)
    const kebabBtn = page.locator('button[aria-label="Дополнительные действия"], button[title="Дополнительные действия"]').first();
    await expect(kebabBtn).toBeVisible({ timeout: 5000 });
    await kebabBtn.click();
    // Кликаем на кнопку «Шаблоны» в раскрывшемся меню
    const templatesMenuItem = page.locator('.kebab-item:has-text("Шаблоны"), .kebab-dropdown button:has-text("Шаблоны")').first();
    await expect(templatesMenuItem).toBeVisible({ timeout: 3000 });
    await templatesMenuItem.click();
    // Ждём открытия модалки
    await expect(page.locator('.templates-wide-modal')).toBeVisible({ timeout: 3000 });
  }

  test('modal opens and shows Xray/Mihomo tabs', async ({ page }) => {
    await openTemplatesModal(page);

    // Проверяем, что модалка открылась с табами Xray/Mihomo
    const xrayTab = page.locator('.templates-kernel-tabs button:has-text("Xray")').first();
    const mihomoTab = page.locator('.templates-kernel-tabs button:has-text("Mihomo")').first();

    await expect(xrayTab).toBeVisible();
    await expect(mihomoTab).toBeVisible();
  });

  test('selecting template shows preview', async ({ page }) => {
    await openTemplatesModal(page);

    // Кликаем на элемент списка шаблонов
    const templateItem = page.locator('.template-item, .template-list button').first();
    await expect(templateItem).toBeVisible({ timeout: 3000 });
    await templateItem.click();

    // Проверяем, что preview-панель содержит текст
    const preview = page.locator('.template-preview-code, .templates-col-preview').first();
    await expect(preview).toBeVisible();
  });

  test('update button is visible in modal header', async ({ page }) => {
    await openTemplatesModal(page);

    // Кнопка «Обновить шаблоны» видна в хедере модалки
    const updateBtn = page.locator('.templates-update-btn, button:has-text("Обновить")').first();
    await expect(updateBtn).toBeVisible();
  });
});
