import { test, expect, type Page, type Route } from '@playwright/test';

async function setupRestMocks(page: Page) {
  await page.route('**/api/**', async (route: Route) => {
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
            kernels: { xray: { installed: true, version: '1.8.24', channel: 'stable' } },
            active_kernel: 'xray'
          }
        })
      });
    } else if (url.includes('/api/subscriptions')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    } else if (url.includes('/api/outbound/parse') && method === 'POST') {
      const body = route.request().postDataJSON();
      const link = body.links?.[0] || '';

      if (link.includes('invalid')) {
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify({ success: false, error: 'Не удалось распознать ссылку' })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: [
              {
                link: link,
                outbound: {
                  tag: 'test-parsed-tag',
                  protocol: 'vless',
                  settings: {
                    vnext: [
                      {
                        address: 'server.example.com',
                        port: 443
                      }
                    ]
                  }
                }
              }
            ]
          })
        });
      }
    } else if (url.includes('/api/outbound/import') && method === 'POST') {
      const body = route.request().postDataJSON();

      if (body.link.includes('error-import')) {
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify({ success: false, error: 'Ошибка импорта' })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      }
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    }
  });
}

async function disableServiceWorker(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
    window.localStorage.setItem('lang', 'ru');
  });
}

test.describe('Import Proxy Node E2E test suite', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page);
    await page.route('**/api/config/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ outbounds: [] })
      });
    });
    await page.goto('/#/constructor');
    // Переключаемся на вкладку Исходящие (outbounds)
    await page.locator('.sec-tab[data-tab="outbounds"]').click();
  });

  test('successfully imports proxy node with custom tag', async ({ page }) => {
    // 1. Verify button exists and click it
    const importBtn = page.locator('button:has-text("Импорт узла")');
    await expect(importBtn).toBeVisible();
    await importBtn.click();

    // 2. Modal opens, step 1
    const modal = page.locator('.modal-card');
    await expect(modal).toBeVisible();
    await expect(modal.locator('h2')).toContainText('Импорт прокси-узла');

    // Parse button should be disabled when empty
    const parseBtn = modal.locator('button:has-text("Распознать")');
    await expect(parseBtn).toBeDisabled();

    // Fill valid link
    await modal.locator('textarea').fill('vless://test-link-data#some-tag');
    await expect(parseBtn).toBeEnabled();

    // Click Parse
    await parseBtn.click();

    // 3. Step 2: preview should be visible
    await expect(modal.locator('.preview-section')).toBeVisible();
    await expect(modal.locator('.preview-row:has-text("Протокол")')).toContainText('vless');
    await expect(modal.locator('.preview-row:has-text("Сервер")')).toContainText(
      'server.example.com'
    );
    await expect(modal.locator('.preview-row:has-text("Порт")')).toContainText('443');

    // Custom tag input should be pre-filled with original tag
    const tagInput = modal.locator('input#import-tag');
    await expect(tagInput).toHaveValue('test-parsed-tag');

    // Change tag to custom
    await tagInput.fill('my-custom-node-tag');

    // Click Import
    const confirmBtn = modal.locator('button:has-text("Импортировать")');
    await confirmBtn.click();

    // 4. Modal closes, success toast should appear
    await expect(modal).not.toBeVisible();

    // In our app layout, showToast adds a toast to the screen
    const toast = page.locator('.toast--success');
    await expect(toast).toBeVisible();
    await expect(toast).toContainText('Узел успешно импортирован');
  });

  test('shows parse error message on invalid link', async ({ page }) => {
    const importBtn = page.locator('button:has-text("Импорт узла")');
    await importBtn.click();

    const modal = page.locator('.modal-card');
    await modal.locator('textarea').fill('invalid-link-format');
    await modal.locator('button:has-text("Распознать")').click();

    // Error message should be visible inside modal
    const errorMsg = modal.locator('.error-msg');
    await expect(errorMsg).toBeVisible();
    await expect(errorMsg).toContainText('Не удалось распознать ссылку');
  });

  test('shows import error message on backend failure', async ({ page }) => {
    const importBtn = page.locator('button:has-text("Импорт узла")');
    await importBtn.click();

    const modal = page.locator('.modal-card');
    await modal.locator('textarea').fill('vless://error-import#tag');
    await modal.locator('button:has-text("Распознать")').click();

    await expect(modal.locator('.preview-section')).toBeVisible();

    // Click Import
    await modal.locator('button:has-text("Импортировать")').click();

    // Error message should be visible inside modal
    const errorMsg = modal.locator('.error-msg');
    await expect(errorMsg).toBeVisible();
  });

  test('shows client-side validation error when entering multiple links', async ({ page }) => {
    const importBtn = page.locator('button:has-text("Импорт узла")');
    await importBtn.click();

    const modal = page.locator('.modal-card');
    await modal.locator('textarea').fill('vless://link1#tag1\nvless://link2#tag2');
    await modal.locator('button:has-text("Распознать")').click();

    const errorMsg = modal.locator('.error-msg');
    await expect(errorMsg).toBeVisible();
    await expect(errorMsg).toContainText('Пожалуйста, введите только одну ссылку за раз');
  });
});

// RED-тесты: D-15, D-16, D-17 — падают до реализации (Wave 2/3)
test.describe('Import Node из конструкторов (D-15, D-16, D-17)', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page);
    // Дополнительный мок для config/read и config/list в конструкторе
    await page.route('**/api/config/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ outbounds: [] })
      });
    });
    await page.goto('/#/constructor');
    await page.locator('.constructor-kernel-toggle button:has-text("Xray")').click();
    await page.locator('.sec-tab[data-tab="outbounds"]').click();
  });

  test('кнопка «Импорт узла» присутствует в Xray-конструкторе (D-15)', async ({ page }) => {
    await expect(page.locator('button:has-text("Импорт узла")')).toBeVisible();
  });

  test('кнопка «Импорт узла» присутствует в Mihomo-конструкторе (D-15)', async ({ page }) => {
    await page.locator('.constructor-kernel-toggle button:has-text("Mihomo")').click();
    await expect(page.locator('button:has-text("Импорт узла")')).toBeVisible();
  });

  test('кнопка «Импорт узла» отсутствует в Subscriptions (D-16)', async ({ page }) => {
    await page.goto('/#/subscriptions');
    await expect(page.locator('button:has-text("Импорт узла")')).not.toBeVisible();
  });

  test('импорт в Xray-конструкторе вызывает POST /api/outbound/import (D-17)', async ({ page }) => {
    let importCalled = false;
    await page.route('**/api/outbound/import', async (route) => {
      importCalled = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.locator('button:has-text("Импорт узла")').click();
    const modal = page.locator('.modal-card');
    await modal.locator('textarea').fill('vless://test-link#tag');
    await modal.locator('button:has-text("Распознать")').click();
    await expect(modal.locator('.preview-section')).toBeVisible();
    await modal.locator('button:has-text("Импортировать")').click();
    expect(importCalled).toBe(true);
  });
});
