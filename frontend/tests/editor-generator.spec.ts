import { test, expect } from '@playwright/test';

// Принудительно задаем русский язык для тестов интерфейса
test.use({ locale: 'ru-RU' });

test.describe('Editor & Constructor integration test suite', () => {
  let fileContent = 'initial config content';

  test.beforeEach(async ({ page }) => {
    fileContent = 'initial config content';

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
        // Возвращаем список файлов раздельно для Xray и Mihomo, чтобы избежать strict mode violation в Playwright
        const isMihomo = url.includes('mihomo');
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(
            isMihomo
              ? [
                {
                  name: 'config.yaml',
                  path: '/opt/etc/mihomo/config.yaml',
                  size: 1500
                }
              ]
              : [
                {
                  name: 'xray-config.json',
                  path: '/opt/etc/xray/configs/xray-config.json',
                  size: 1200
                }
              ]
          )
        });
      } else if (url.includes('/api/config/read')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: fileContent
        });
      } else if (url.includes('/api/templates/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/assets/definition')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({})
        });
      } else if (url.includes('/api/config/validate')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ valid: true })
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

  test('successfully displays editor tabs and switches between files and constructor', async ({
    page
  }) => {
    await page.goto('/#/editor');

    // Проверяем наличие верхних вкладок в редакторе
    const filesTab = page.locator('button.tab-btn:has-text("Файлы")');
    const constructorTab = page.locator('button.tab-btn:has-text("Конструктор")');

    await expect(filesTab).toBeVisible();
    await expect(filesTab).toHaveClass(/active/);
    await expect(constructorTab).toBeVisible();
    await expect(constructorTab).not.toHaveClass(/active/);

    // Кликаем по вкладке Конструктор
    await constructorTab.click();

    // Проверяем переключение вкладок и изменение URL
    await expect(constructorTab).toHaveClass(/active/);
    await expect(filesTab).not.toHaveClass(/active/);
    await expect(page).toHaveURL(/#\/constructor/);
  });

  test('deep-link #/constructor automatically opens editor on constructor tab', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Проверяем, что вкладка Конструктор активна при переходе по диплинку
    const filesTab = page.locator('button.tab-btn:has-text("Файлы")');
    const constructorTab = page.locator('button.tab-btn:has-text("Конструктор")');

    await expect(constructorTab).toHaveClass(/active/);
    await expect(filesTab).not.toHaveClass(/active/);
    await expect(page).toHaveURL(/#\/constructor/);
  });

  test('warns and redirects when trying to insert config with no active file selected', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Кнопка должна называться "Открыть в редакторе", так как файл не выбран
    const actionBtn = page.locator('button.btn-secondary:has-text("Открыть в редакторе")');
    await expect(actionBtn).toBeVisible();

    // Кликаем по кнопке
    await actionBtn.click();

    // Ожидаем переключения на вкладку Файлы
    const filesTab = page.locator('button.tab-btn:has-text("Файлы")');
    await expect(filesTab).toHaveClass(/active/);
    await expect(page).toHaveURL(/#\/editor/);
  });

  test('successfully inserts generated YAML into active editor file', async ({ page }) => {
    await page.goto('/#/editor');

    // Открываем тестовый файл config.yaml из боковой панели
    const fileRow = page.locator('.file-row:has-text("config.yaml")');
    await expect(fileRow).toBeVisible();
    await fileRow.click();

    // Проверяем, что файл открылся
    await expect(page.locator('.file-name:has-text("config.yaml")')).toBeVisible();

    // Переходим на вкладку Конструктор
    const constructorTab = page.locator('button.tab-btn:has-text("Конструктор")');
    await constructorTab.click();

    // Переключаемся на Mihomo-сторону конструктора (по умолчанию открыт Xray)
    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible();
    await mihomoKernelBtn.click();

    // Добавляем прокси через интерфейс генератора
    await page.locator('button.add-btn:has-text("Добавить прокси")').click();
    await page.locator('input.form-input[placeholder="my-proxy"]').fill('test-reality-proxy');
    await page.locator('input.form-input[placeholder="example.com"]').fill('reality-server.com');
    await page.locator('button.btn-primary:has-text("Добавить")').click();

    // Кнопка должна называться "Вставить в редактор", так как файл открыт
    const actionBtn = page.locator('button.btn-secondary:has-text("Вставить в редактор")');
    await expect(actionBtn).toBeVisible();

    // Нажимаем вставить в редактор
    await actionBtn.click();

    // Должно произойти переключение на вкладку Файлы
    const filesTab = page.locator('button.tab-btn:has-text("Файлы")');
    await expect(filesTab).toHaveClass(/active/);
    await expect(page).toHaveURL(/#\/editor/);

    // Проверяем, что статус файла изменился на "Изменён" (isDirty)
    await expect(page.locator('.status-dirty')).toBeVisible();
  });

  test('metacubex rule-provider selector displays checkbox picker with categories and meta-rules-dat URL', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Переключаемся на Mihomo-сторону конструктора
    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible({ timeout: 5000 });
    await mihomoKernelBtn.click();

    // Находим select для выбора rule-provider и выбираем "metacubex"
    const rpSelect = page.locator('select.rp-select, #rp-select');
    await expect(rpSelect).toBeVisible();
    await rpSelect.selectOption('metacubex');

    // Проверяем, что отображается checkbox-пикер с категориями
    const picker = page.locator('[data-testid="rulesets-picker"], .rulesets-picker');
    await expect(picker).toBeVisible({ timeout: 5000 });

    // Проверяем наличие категорий
    await expect(picker).toContainText('Социальные сети');

    // Находим чекбокс с YouTube или другим правилом и отмечаем его
    const youtubeCheckbox = page
      .locator(
        'input[type="checkbox"][value="youtube|geosite"], input[type="checkbox"]#ruleset-geosite-youtube'
      )
      .first();
    await expect(youtubeCheckbox).toBeVisible();
    await youtubeCheckbox.check();

    // Проверяем, что в YAML-превью генерируется rule-provider с URL meta-rules-dat
    const previewPane = page
      .locator(
        '.constructor-preview-panel, pre.constructor-preview, textarea[readonly], .yaml-preview'
      )
      .first();
    await expect(previewPane).toBeVisible();
    await expect(previewPane).toContainText(
      'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo'
    );
    await expect(previewPane).toContainText('format: mrs');
  });
});

// ---------------------------------------------------------------------------
// RED-тест D-13: zkeen-selective generateYAML создаёт 16 групп + 15 rule-providers + rules
// Падает до реализации пресета в Plan 15.2-05
// ---------------------------------------------------------------------------
test.describe('zkeen-selective generateYAML (D-13)', () => {
  test.beforeEach(async ({ page }) => {
    // Отключаем Service Worker
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      window.localStorage.setItem('lang', 'ru');
    });

    // Перехватываем API запросы
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
              ? [{ name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 100 }]
              : [
                {
                  name: 'xray-config.json',
                  path: '/opt/etc/xray/configs/xray-config.json',
                  size: 100
                }
              ]
          )
        });
      } else if (url.includes('/api/config/read') && route.request().method() === 'GET') {
        // Возвращаем минимальный config.yaml для Mihomo
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: 'mixed-port: 7890\nallow-lan: false\n'
        });
      } else if (url.includes('/api/templates/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/subscriptions')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/assets/definition')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({})
        });
      } else if (url.includes('/api/config/validate')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ valid: true })
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

  test('пресет zkeen-selective генерирует ровно 18 proxy-groups и 43 rule-providers (D-13)', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Переключиться на Mihomo-конструктор
    const mihomoBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoBtn).toBeVisible({ timeout: 5000 });
    await mihomoBtn.click();

    // Выбрать пресет zkeen-selective
    const presetSelect = page.locator
      (
        'select.preset-select, [data-testid="preset-select"], select#preset-select'
      );
    await expect(presetSelect).toBeVisible({ timeout: 5000 });
    await presetSelect.selectOption('zkeen-selective');

    // Получить сгенерированный YAML из превью
    const previewPane = page
      .locator(
        '.constructor-preview-panel, pre.constructor-preview, textarea[readonly], .yaml-preview'
      )
      .first();
    await expect(previewPane).toBeVisible({ timeout: 3000 });
    const yamlText = (await previewPane.textContent()) || '';

    // Считаем proxy-groups — каждая группа начинается с '  - name:'
    const proxyGroupMatches = yamlText.match(/^ {2}- name:/gm);
    const proxyGroupCount = proxyGroupMatches ? proxyGroupMatches.length : 0;

    // Считаем rule-providers — каждый провайдер — строка вида '  name@type:'
    // (или считаем вхождения 'type: http' в секции rule-providers)
    const ruleProviderMatches = yamlText.match(/\n {2}[a-z][^:\n]+@[a-z]+:/g);
    const ruleProviderCount = ruleProviderMatches ? ruleProviderMatches.length : 0;

    // D-13: 18 групп
    expect(proxyGroupCount).toBe(18);

    // D-13: 43 rule-providers
    expect(ruleProviderCount).toBe(43);

    // Ключевые правила должны присутствовать
    expect(yamlText).toContain('RULE-SET');
    expect(yamlText).toContain('MATCH');
  });

  test('применение изменений Mihomo (Apply) требует подтверждения и отправляет запрос на merge + restart (D-05, D-19)', async ({
    page
  }) => {
    const postRequests: string[] = [];
    page.on('request', (request) => {
      if (request.method() === 'POST') {
        postRequests.push(request.url());
      }
    });

    page.on('dialog', async (dialog) => {
      await dialog.accept();
    });

    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")').first();
    await expect(fileRow).toBeVisible();
    await fileRow.click();

    await expect(page.locator('.file-name:has-text("config.yaml")').first()).toBeVisible();

    const constructorTab = page.locator('button.tab-btn:has-text("Конструктор")');
    await constructorTab.click();

    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible();
    await mihomoKernelBtn.click();

    // Выбираем пресет zkeen-selective, чтобы сгенерировать YAML и активировать кнопку применить
    const presetSelect = page.locator(
      'select.preset-select, [data-testid="preset-select"], select#preset-select'
    );
    await expect(presetSelect).toBeVisible();
    await presetSelect.selectOption('zkeen-selective');

    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible();
    await applyBtn.click();

    const confirmDialog = page.locator('[data-testid="apply-confirm-dialog"]');
    await expect(confirmDialog).toBeVisible();

    const confirmActionBtn = confirmDialog.locator('button.btn-primary');
    await expect(confirmActionBtn).toBeVisible();
    await confirmActionBtn.click();

    await expect(confirmDialog).not.toBeVisible();

    // Wait for the save flow to finish
    await expect(page.locator('[data-testid="apply-changes-btn"]')).toBeEnabled({ timeout: 5000 });

    const mergeCall = postRequests.some((url) => url.includes('/api/config/mihomo-merge'));
    const restartCall = postRequests.some(
      (url) => url.includes('/api/service/control') && url.includes('action=restart')
    );

    expect(mergeCall).toBe(true);
    expect(restartCall).toBe(true);
  });

  test('displays warning banner listing preserved non-managed keys and sends 6 sections on merge', async ({
    page
  }) => {
    // We override config content to contain some custom keys
    await page.route('**/api/config/read*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'mixed-port: 7890\ncustom-key: value\nproxies:\n  - name: test-p\n    type: ss\n    server: 1.1.1.1\n    port: 8388\n'
      });
    });

    // Capture the payload sent to merge
    let mergePayload: any = null;
    await page.route('**/api/config/mihomo-merge', async (route) => {
      mergePayload = route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")').first();
    await expect(fileRow).toBeVisible();
    await fileRow.click();

    const constructorTab = page.locator('button.tab-btn:has-text("Конструктор")');
    await constructorTab.click();

    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible();
    await mihomoKernelBtn.click();

    // Check that warning banner is visible and lists the preserved keys
    const warningBanner = page.locator('.alert-warning');
    await expect(warningBanner).toBeVisible();
    await expect(warningBanner).toContainText('mixed-port, custom-key');

    // Click Apply
    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible();
    await applyBtn.click();

    const confirmDialog = page.locator('[data-testid="apply-confirm-dialog"]');
    await expect(confirmDialog).toBeVisible();

    const confirmActionBtn = confirmDialog.locator('button.btn-primary');
    await expect(confirmActionBtn).toBeVisible();
    await confirmActionBtn.click();

    // Wait for the save flow to finish
    await expect(page.locator('[data-testid="apply-changes-btn"]')).toBeEnabled({ timeout: 5000 });

    expect(mergePayload).not.toBeNull();
    expect(mergePayload.sections).toBeDefined();
    expect(mergePayload.sections['proxies']).toContain('test-p');
    expect(mergePayload.sections['dns']).toBeDefined();
    expect(mergePayload.sections['tun']).toBeDefined();
  });
});
