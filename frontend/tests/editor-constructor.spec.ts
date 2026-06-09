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
              active_kernel: 'mihomo'
            }
          })
        });
      } else if (url.includes('/api/config/list')) {
        // Возвращаем файл с outbound-тегами для XrayRoutingConstructor
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              name: '04_outbounds.manual.json',
              path: '/opt/etc/xray/configs/04_outbounds.manual.json',
              size: 800
            }
          ])
        });
      } else if (url.includes('/api/config/read') && route.request().method() === 'GET') {
        // Mock возвращает конфиг с тегами outbounds (исправление бага Phase 15.1 — проверяем GET)
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ outbounds: [{ tag: 'my-proxy', protocol: 'vless' }] })
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

  test('tab navigation: switching to Конструктор activates the tab', async ({ page }) => {
    await page.goto('/#/editor');

    const filesTab = page.locator('button.tab-btn:has-text("Файлы")');
    const constructorTab = page.locator('button.tab-btn:has-text("Конструктор")');

    await expect(filesTab).toBeVisible();
    await expect(filesTab).toHaveClass(/active/);
    await expect(constructorTab).toBeVisible();
    await expect(constructorTab).not.toHaveClass(/active/);

    await constructorTab.click();

    await expect(constructorTab).toHaveClass(/active/);
    await expect(filesTab).not.toHaveClass(/active/);
    await expect(page).toHaveURL(/#\/constructor/);
  });

  test('adding a routing rule with domain and outboundTag shows it in the rules list', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Переключаемся на Xray-конструктор
    const xrayKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayKernelBtn).toBeVisible({ timeout: 5000 });
    await xrayKernelBtn.click();

    // Добавляем новое правило маршрутизации
    const addRuleBtn = page
      .locator('[data-testid="add-routing-rule"], button:has-text("Добавить правило")')
      .first();
    await expect(addRuleBtn).toBeVisible({ timeout: 5000 });
    await addRuleBtn.click();

    // Заполняем домены в новом правиле
    const domainInput = page
      .locator(
        '[data-testid="rule-domain-input"], input[placeholder*="домен"], input[placeholder*="domain"]'
      )
      .first();
    await expect(domainInput).toBeVisible({ timeout: 3000 });
    await domainInput.fill('geosite:youtube');

    // Выбираем outbound tag из dropdown
    const outboundSelect = page
      .locator(
        '[data-testid="rule-outbound-select"], select[data-testid="outbound-tag"], .rule-outbound-select'
      )
      .first();
    await expect(outboundSelect).toBeVisible({ timeout: 3000 });
    await outboundSelect.selectOption('my-proxy');

    // Кликаем по кнопке Создать для подтверждения добавления правила
    const saveRuleBtn = page
      .locator(
        '.form-card button.btn-primary:has-text("Создать"), .form-card button:has-text("Create")'
      )
      .first();
    await expect(saveRuleBtn).toBeVisible({ timeout: 3000 });
    await saveRuleBtn.click();

    // Правило должно появиться в списке с выбранным тегом
    const rulesList = page.locator('[data-testid="routing-rules-list"], .routing-rules-list');
    await expect(rulesList).toBeVisible({ timeout: 3000 });
    await expect(rulesList).toContainText('my-proxy');
  });

  test('generated JSON contains my-proxy and does NOT contain real outbound parameters (server/uuid)', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Переключаемся на Xray-конструктор
    const xrayKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayKernelBtn).toBeVisible({ timeout: 5000 });
    await xrayKernelBtn.click();

    // Находим JSON preview-панель
    const previewPane = page
      .locator(
        '[data-testid="xray-json-preview"], .xray-routing-preview, .json-preview, .constructor-preview-pane'
      )
      .first();
    await expect(previewPane).toBeVisible({ timeout: 5000 });

    const previewText = await previewPane.textContent();

    // JSON должен содержать my-proxy
    expect(previewText).toContain('my-proxy');

    // JSON не должен содержать реальных параметров outbound (server, uuid, address)
    expect(previewText).not.toMatch(/"server"\s*:/);
    expect(previewText).not.toMatch(/"uuid"\s*:/);

    // Preview должен быть валидным JSON
    expect(() => JSON.parse(previewText || '')).not.toThrow();
  });

  test('dropdown outbound tags shows my-proxy, direct and block options', async ({ page }) => {
    await page.goto('/#/constructor');

    // Переключаемся на Xray-конструктор
    const xrayKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayKernelBtn).toBeVisible({ timeout: 5000 });
    await xrayKernelBtn.click();

    // Добавляем правило, чтобы dropdown появился
    const addRuleBtn = page
      .locator('[data-testid="add-routing-rule"], button:has-text("Добавить правило")')
      .first();
    await expect(addRuleBtn).toBeVisible({ timeout: 5000 });
    await addRuleBtn.click();

    // Проверяем, что dropdown содержит теги из mock-файла + системные теги
    const outboundSelect = page
      .locator(
        '[data-testid="rule-outbound-select"], select[data-testid="outbound-tag"], .rule-outbound-select'
      )
      .first();
    await expect(outboundSelect).toBeVisible({ timeout: 3000 });

    // Должны присутствовать: my-proxy (из mock), direct, block
    const options = await outboundSelect.locator('option').allTextContents();
    expect(options).toContain('my-proxy');
    expect(options).toContain('direct');
    expect(options).toContain('block');
  });
});
