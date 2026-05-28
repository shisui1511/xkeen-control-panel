import { test, expect } from '@playwright/test';

// Принудительно задаем русский язык для тестов интерфейса
test.use({ locale: 'ru-RU' });

test.describe('Editor & Mihomo Generator integration test suite', () => {
  let fileContent = 'initial config content';
  let insertCallbackCalled = false;

  test.beforeEach(async ({ page }) => {
    fileContent = 'initial config content';
    insertCallbackCalled = false;

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
          body: JSON.stringify(isMihomo ? [
            {
              name: 'config.yaml',
              path: '/opt/etc/mihomo/config.yaml',
              size: 1500
            }
          ] : [
            {
              name: 'xray-config.json',
              path: '/opt/etc/xray/configs/xray-config.json',
              size: 1200
            }
          ])
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
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  });

  test('successfully displays editor tabs and switches between files and generator', async ({ page }) => {
    await page.goto('/#/editor');

    // Проверяем наличие верхних вкладок в редакторе
    const filesTab = page.locator('button.tab-btn:has-text("Файлы")');
    const generatorTab = page.locator('button.tab-btn:has-text("Mihomo Generator")');

    await expect(filesTab).toBeVisible();
    await expect(filesTab).toHaveClass(/active/);
    await expect(generatorTab).toBeVisible();
    await expect(generatorTab).not.toHaveClass(/active/);

    // Кликаем по вкладке Mihomo Generator
    await generatorTab.click();

    // Проверяем переключение вкладок и изменение URL
    await expect(generatorTab).toHaveClass(/active/);
    await expect(filesTab).not.toHaveClass(/active/);
    await expect(page).toHaveURL(/#\/mihomo-gen/);

    // Должен отображаться заголовок визуального генератора
    await expect(page.locator('h1').filter({ hasText: 'Визуальный генератор' })).toBeVisible();
  });

  test('deep-link #/mihomo-gen automatically opens editor on generator tab', async ({ page }) => {
    await page.goto('/#/mihomo-gen');

    // Проверяем, что вкладка Mihomo Generator активна при переходе по диплинку
    const filesTab = page.locator('button.tab-btn:has-text("Файлы")');
    const generatorTab = page.locator('button.tab-btn:has-text("Mihomo Generator")');

    await expect(generatorTab).toHaveClass(/active/);
    await expect(filesTab).not.toHaveClass(/active/);
    await expect(page.locator('h1').filter({ hasText: 'Визуальный генератор' })).toBeVisible();
  });

  test('warns and redirects when trying to insert config with no active file selected', async ({ page }) => {
    await page.goto('/#/mihomo-gen');

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

    // Переходим на вкладку Mihomo Generator
    const generatorTab = page.locator('button.tab-btn:has-text("Mihomo Generator")');
    await generatorTab.click();

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
});
