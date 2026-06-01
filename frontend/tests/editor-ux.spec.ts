import { test, expect } from '@playwright/test';

// Принудительно задаем русский язык для тестов интерфейса
test.use({ locale: 'ru-RU' });

test.describe('Editor UX (Multi-tab, Breadcrumbs, Drawer, Save & Apply) Test Suite', () => {
  const fileContent = `port: 7890
socks-port: 7891
allow-lan: true
mode: Rule
log-level: info
proxies:
  - name: "HK-1"
    type: ss
    server: hk.server.com
    port: 443
    uuid: test-uuid
`;

  const backupContent = `port: 7890
socks-port: 7891
allow-lan: false
mode: Global
log-level: debug
proxies:
  - name: "HK-1"
    type: ss
    server: hk.server.com
    port: 443
    uuid: test-uuid
`;

  let statusChecksCount = 0;

  test.beforeEach(async ({ page }) => {
    statusChecksCount = 0;

    // Включаем логирование консоли браузера для отладки
    page.on('console', (msg) => {
      console.log(`BROWSER CONSOLE [${msg.type()}]: ${msg.text()}`);
    });
    page.on('pageerror', (err) => {
      console.log(`BROWSER PAGE ERROR: ${err.message}\nStack: ${err.stack}`);
    });

    // Безопасно мокаем Service Worker с пустым методом register, чтобы избежать JS ошибок в index.html
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: {
          register: () => Promise.resolve({}),
          addEventListener: () => {},
          removeEventListener: () => {},
          getRegistrations: () => Promise.resolve([])
        },
        writable: false,
        configurable: true
      });
    });

    // Перехватываем все запросы к API
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      console.log(`MOCK API REQUEST: ${url}`);

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
              ? [
                  {
                    name: 'config.yaml',
                    path: '/opt/etc/mihomo/config.yaml',
                    size: 1500
                  },
                  {
                    name: 'default.yaml',
                    path: '/opt/etc/mihomo/default.yaml',
                    size: 800
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
        if (url.includes('bak.1')) {
          await route.fulfill({
            status: 200,
            contentType: 'text/plain',
            body: backupContent
          });
        } else {
          await route.fulfill({
            status: 200,
            contentType: 'text/plain',
            body: fileContent
          });
        }
      } else if (url.includes('/api/config/backups')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            '/opt/etc/mihomo/config.yaml.bak.1',
            '/opt/etc/mihomo/config.yaml.bak.2'
          ])
        });
      } else if (url.includes('/api/config/save')) {
        // Добавляем задержку 300мс для стабильности тестирования состояния loading кнопки
        await new Promise((resolve) => setTimeout(resolve, 300));
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else if (url.includes('/api/config/validate')) {
        // Добавляем искусственную задержку 500мс для надежного тестирования состояния loading
        await new Promise((resolve) => setTimeout(resolve, 500));
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ valid: true })
        });
      } else if (url.includes('/api/service/control') && url.includes('action=restart')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: 'OK'
        });
      } else if (url.includes('/api/service/status')) {
        statusChecksCount++;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              is_running: statusChecksCount >= 2,
              active_kernel: 'mihomo',
              pid: 4567,
              uptime: '30s',
              binary_path: '/usr/bin/mihomo',
              raw: 'active and running'
            }
          })
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

  test('successfully manages multiple tabs (Multi-tab)', async ({ page }) => {
    await page.goto('/#/editor');

    // 1. Открываем первый файл config.yaml двойным кликом, чтобы закрепить вкладку (pinned)
    const mihomoFileRow = page.locator('.file-row:has-text("config.yaml")');
    await expect(mihomoFileRow).toBeVisible();
    await mihomoFileRow.dblclick();

    // Проверяем наличие первой вкладки и ждем ее появления
    const tab1 = page.locator('.editor-tab:has-text("config.yaml")');
    await expect(tab1).toBeVisible();

    // 2. Открываем второй файл default.yaml
    const defaultFileRow = page.locator('.file-row:has-text("default.yaml")');
    await expect(defaultFileRow).toBeVisible();
    await defaultFileRow.click();

    // Должно быть 2 вкладки, и вторая активна
    const tab2 = page.locator('.editor-tab:has-text("default.yaml")');
    await expect(tab2).toBeVisible();
    await expect(tab2).toHaveClass(/active/);
    await expect(tab1).not.toHaveClass(/active/);

    // 3. Кликаем по первой вкладке для переключения назад
    await tab1.click();
    await expect(tab1).toHaveClass(/active/);
    await expect(tab2).not.toHaveClass(/active/);

    // 4. Проверяем клавиатурную навигацию (Ctrl+Tab)
    await page.keyboard.press('Control+Tab');
    await expect(tab2).toHaveClass(/active/);

    // 5. Закрываем вторую вкладку через кнопку закрытия
    const closeBtn = tab2.locator('.tab-close-btn');
    await expect(closeBtn).toBeVisible();
    await closeBtn.click();

    // Вкладка default.yaml должна исчезнуть, а config.yaml снова стать активной
    await expect(tab2).not.toBeVisible();
    await expect(tab1).toHaveClass(/active/);
  });

  test('correctly shows breadcrumbs for JSON/YAML paths', async ({ page }) => {
    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")');
    await fileRow.click();

    // Ждем открытия файла
    const tab = page.locator('.editor-tab:has-text("config.yaml")');
    await expect(tab).toBeVisible();

    // Симулируем клик по строке с YAML-парой (например, socks-port: 7891 на 2-й строке), чтобы активировать курсор и крошки
    const cmLine = page.locator('.cm-line').nth(1); // 2-я строка
    await expect(cmLine).toBeVisible();
    await cmLine.click();

    // Проверяем наличие хлебных крошек
    const breadcrumbsBar = page.locator('.editor-breadcrumbs');
    await expect(breadcrumbsBar).toBeVisible();
  });

  test('successfully displays backup bottom drawer and diff viewer', async ({ page }) => {
    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")');
    await fileRow.click();

    // Ждем открытия файла
    const tab = page.locator('.editor-tab:has-text("config.yaml")');
    await expect(tab).toBeVisible();

    // Ищем кнопку раскрытия Drawer бэкапов в статус-баре
    const backupsBtn = page.locator('.backups-toggle-btn');
    await expect(backupsBtn).toBeVisible();
    await expect(backupsBtn).toContainText('(2)');

    // Открываем панель
    await backupsBtn.click();

    const drawer = page.locator('.editor-bottom-drawer');
    await expect(drawer).toBeVisible();

    // Выбираем первый бэкап в списке
    const backupItem = page.locator('.backup-item').first();
    await expect(backupItem).toBeVisible();
    await backupItem.click();

    // Проверяем, что отображается Diff-Viewer
    const diffContainer = page.locator('.diff-viewer-container');
    await expect(diffContainer).toBeVisible();

    // Должны быть удаленные и добавленные строки с подсветкой
    await expect(page.locator('.diff-line-removed').first()).toBeVisible();
    await expect(page.locator('.diff-line-added').first()).toBeVisible();
  });

  test('successfully executes "Save & Apply" flow with background polling', async ({ page }) => {
    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")');
    await fileRow.click();

    // Ждем открытия файла
    const tab = page.locator('.editor-tab:has-text("config.yaml")');
    await expect(tab).toBeVisible();

    // Вводим текст в редактор, чтобы сделать файл грязным (isDirty)
    const cmContent = page.locator('.cm-content');
    await expect(cmContent).toBeVisible();
    await cmContent.focus();
    await page.keyboard.type('\n# edited line\n');

    // Проверяем, что dirty-индикатор появился
    await expect(page.locator('.status-dirty')).toBeVisible();

    // Ищем кнопку "Сохранить и применить" по title, так как ее текст меняется при блокировке
    const applyBtn = page.locator('button.btn-accent[title="Сохранить и применить"]');
    await expect(applyBtn).toBeVisible();

    // Кликаем "Сохранить и применить"
    await applyBtn.click();

    // Кнопка должна стать заблокированной (loading)
    await expect(applyBtn).toBeDisabled();

    // В статус-баре должен отображаться текущий статус применения
    const statusText = page.locator('.status-apply-indicator');
    await expect(statusText).toBeVisible();

    // Ожидаем завершения фонового перезапуска и опроса статуса
    // Наш мок возвращает is_running: true на второй попытке опроса
    // Ждем, пока кнопка разблокируется и исчезнет индикатор применения
    await expect(applyBtn).toBeEnabled();
    await expect(statusText).not.toBeVisible();
  });
});
