// e2e-pages: editor
import { test, expect, type Page } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

// Редактор на узком экране (≤768 px): панель файлов — выезжающий лист, действия шапки,
// меню файла, статус-бар, вкладки, модалки и панель бэкапов не выходят за правый край окна.
// Все API замоканы; служб на ПК разработчика тест не трогает.

test.use({ locale: 'ru-RU', viewport: { width: 390, height: 844 }, hasTouch: true });

const THEMES = ['dark', 'light'] as const;
const MIHOMO_DIR = '/opt/etc/mihomo';
const fileContent = 'port: 7890\nmode: Rule\nlog-level: info\n';
const backupContent = 'port: 7890\nmode: Global\nlog-level: debug\n';

async function mockEditor(page: Page, theme: (typeof THEMES)[number]) {
  await setupMocks(page, 'mihomo');
  await page.addInitScript((t) => {
    localStorage.setItem('theme', t);
  }, theme);

  // Более поздний route приоритетнее: подменяем файловые и шаблонные эндпоинты
  await page.route('**/api/config/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/api/config/list')) {
      const isMihomo = url.includes('mihomo');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          isMihomo
            ? [
                { name: 'config.yaml', path: `${MIHOMO_DIR}/config.yaml`, size: 1500 },
                { name: 'default.yaml', path: `${MIHOMO_DIR}/default.yaml`, size: 800 },
                { name: 'outbounds.json', path: `${MIHOMO_DIR}/outbounds.json`, size: 400 }
              ]
            : []
        )
      });
    } else if (url.includes('/api/config/read')) {
      const isJson = url.includes('.json');
      await route.fulfill({
        status: 200,
        contentType: isJson ? 'application/json' : 'text/plain',
        body:
          url.includes('backup') || url.includes('bak.1')
            ? backupContent
            : isJson
              ? '{\n  "outbounds": []\n}'
              : fileContent
      });
    } else if (url.includes('/api/config/backups')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([`${MIHOMO_DIR}/config.yaml.bak.1`, `${MIHOMO_DIR}/config.yaml.bak.2`])
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    }
  });

  await page.route('**/api/templates/list', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          name: 'Basic Mihomo',
          type: 'mihomo',
          description: 'Базовый шаблон для теста',
          content: 'port: 7890\nmode: Rule\n'
        }
      ])
    });
  });
}

/** Элемент целиком в пределах окна по горизонтали */
async function expectInsideViewport(page: Page, selector: string, label: string) {
  const loc = page.locator(selector).first();
  await expect(loc, `${label} виден`).toBeVisible();
  const box = await loc.boundingBox();
  const width = page.viewportSize()!.width;
  expect(box, `${label}: boundingBox`).not.toBeNull();
  expect(box!.x, `${label}: левый край`).toBeGreaterThanOrEqual(-0.5);
  expect(box!.x + box!.width, `${label}: правый край`).toBeLessThanOrEqual(width + 0.5);
}

async function expectNoHorizontalOverflow(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow, 'страница не шире окна').toBeLessThanOrEqual(0);
}

async function openFile(page: Page, fileName = 'config.yaml', isPermanent = false) {
  const emptyBtn = page.locator('.editor-empty-actions .btn-secondary');
  const toggleBtn = page.locator('.btn-sidebar-toggle');
  const pane = page.locator('.file-tree-pane.overlay');

  if (!(await pane.isVisible())) {
    await expect(emptyBtn.or(toggleBtn)).toBeVisible();
    if (await emptyBtn.isVisible()) {
      await emptyBtn.click();
    } else {
      await toggleBtn.click();
    }
  }

  await expect(pane).toBeVisible();
  const fileRow = pane.locator(`.file-row:has-text("${fileName}")`);
  if (isPermanent) {
    await fileRow.dblclick();
  } else {
    await fileRow.click();
  }
  await expect(pane).toBeHidden();
  await expect(page.locator(`.editor-tab:has-text("${fileName}")`)).toBeVisible();
}

for (const theme of THEMES) {
  test.describe(`Редактор на 390 px (${theme})`, () => {
    test.beforeEach(async ({ page }) => {
      await mockEditor(page, theme);
    });

    test('панель файлов выезжает листом, закрывается по подложке, Escape, свайпу и выбору файла', async ({
      page
    }) => {
      await page.goto('/#/editor');
      await expect(page.locator('.editor-empty-card')).toBeVisible();
      // Колонки с деревом файлов рядом с редактором нет
      await expect(page.locator('.file-tree-pane')).toHaveCount(0);

      // 1. Открытие шторки
      await page.locator('.editor-empty-actions .btn-secondary').click();
      const pane = page.locator('.file-tree-pane.overlay');
      await expect(pane).toBeVisible();
      await expectInsideViewport(page, '.file-tree-pane.overlay', 'лист файлов');

      // 2. Закрытие по клику на подложку
      await page.locator('.file-tree-backdrop').click({ position: { x: 345, y: 300 } });
      await expect(pane).toBeHidden();

      // 3. Открытие и закрытие по Escape
      await page.locator('.editor-empty-actions .btn-secondary').click();
      await expect(pane).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(pane).toBeHidden();

      // 4. Открытие и закрытие по свайпу влево (dx <= -60px)
      await page.locator('.editor-empty-actions .btn-secondary').click();
      await expect(pane).toBeVisible();
      await page.evaluate(() => {
        const target = document.querySelector('.file-tree-pane.overlay')!;
        const touchStart = new Touch({
          identifier: 1,
          target,
          clientX: 250,
          clientY: 300
        });
        const touchEnd = new Touch({
          identifier: 1,
          target,
          clientX: 150,
          clientY: 300
        });

        target.dispatchEvent(
          new TouchEvent('touchstart', { touches: [touchStart], bubbles: true, cancelable: true })
        );
        target.dispatchEvent(
          new TouchEvent('touchmove', { touches: [touchEnd], bubbles: true, cancelable: true })
        );
        target.dispatchEvent(
          new TouchEvent('touchend', {
            touches: [],
            changedTouches: [touchEnd],
            bubbles: true,
            cancelable: true
          })
        );
      });
      await expect(pane).toBeHidden();

      // 5. Выбор файла открывает файл и закрывает шторку
      await openFile(page, 'config.yaml');
      await expectNoHorizontalOverflow(page);
    });

    test('действия шапки, меню файла и статус-бар не за краем окна', async ({ page }) => {
      await page.goto('/#/editor');
      await openFile(page);

      // В шапке на 390px видна кнопка «Применить» и меню «⋮», текстовые кнопки скрыты
      await expectInsideViewport(
        page,
        '.page-header-actions .btn-accent',
        '«Сохранить и применить»'
      );
      await expect(page.locator('.page-header-actions .btn-desktop-only').first()).toBeHidden();
      await expect(page.locator('.page-header-actions .btn-desktop-only').nth(1)).toBeHidden();
      await expectInsideViewport(page, '.btn-overflow-trigger', 'кнопка меню «⋮»');

      // Клик по меню «⋮» раскрывает выпадающий список
      await page.locator('.btn-overflow-trigger').click();
      await expectInsideViewport(page, '.overflow-dropdown', 'выпадающее меню шапки');
      // Закрытие по Escape
      await page.keyboard.press('Escape');
      await expect(page.locator('.overflow-dropdown')).toBeHidden();

      // Меню действий файла (кебаб)
      await expectInsideViewport(page, '.btn-kebab', 'меню действий файла');
      await page.locator('.btn-kebab').click();
      await expectInsideViewport(page, '.kebab-dropdown', 'выпадающее меню файла');
      await page.keyboard.press('Escape');

      // Статус-бар: компактный, однострочный (высота <= 30px), подсказка Ctrl+S скрыта
      const statusbar = page.locator('.editor-statusbar');
      await expect(statusbar).toBeVisible();
      const statusBox = await statusbar.boundingBox();
      expect(statusBox!.height, 'высота статус-бара').toBeLessThanOrEqual(30);
      await expect(page.locator('.esb-tip')).toBeHidden();
      await expectInsideViewport(page, '.editor-statusbar .chip-toggle >> nth=1', '«Эксперт»');
      await expectInsideViewport(page, '.backups-toggle-btn', '«Backups»');

      await expectNoHorizontalOverflow(page);

      // Редактор не схлопнут
      const cm = await page.locator('.cm-editor').boundingBox();
      expect(cm!.height).toBeGreaterThan(120);
    });

    test('вкладки файлов: скролл, обрезка названий до 120px и закрытие по тапу', async ({
      page
    }) => {
      await page.goto('/#/editor');
      await openFile(page, 'config.yaml');
      // Закрепляем первую вкладку, чтобы открытие второго файла добавило новую вкладку
      await page.locator('.editor-tab:has-text("config.yaml")').dblclick();
      // Открываем второй файл
      await openFile(page, 'default.yaml');

      const tabs = page.locator('.editor-tab');
      await expect(tabs).toHaveCount(2);

      // Проверка ограничения ширины названия вкладки (120px)
      const tabName = page.locator('.tab-name').first();
      await expect(tabName).toBeVisible();
      const tabBox = await tabName.boundingBox();
      expect(tabBox!.width, 'ширина названия вкладки <= 120px').toBeLessThanOrEqual(121);

      // Закрываем вторую вкладку
      const closeBtn = page.locator('.editor-tab:has-text("default.yaml") .tab-close-btn');
      await expect(closeBtn).toBeVisible();
      await closeBtn.click();

      // Осталась только первая вкладка
      await expect(page.locator('.editor-tab')).toHaveCount(1);
      await expect(page.locator('.editor-tab:has-text("config.yaml")')).toBeVisible();

      await expectNoHorizontalOverflow(page);
    });

    test('панель бэкапов — лист поверх редактора: список виден, сравнение читается, закрывается', async ({
      page
    }) => {
      await page.goto('/#/editor');
      await openFile(page);

      await page.locator('.backups-toggle-btn').click();
      const drawer = page.locator('.editor-bottom-drawer');
      await expect(drawer).toBeVisible();
      await expectInsideViewport(page, '.editor-bottom-drawer', 'панель бэкапов');

      // Список копий виден, «Восстановить» доступна без наведения
      const firstItem = page.locator('.backup-item').first();
      await expect(firstItem).toBeVisible();
      await expectInsideViewport(page, '.backup-item .restore-inline-btn', '«Восстановить»');
      await expect(page.locator('.backup-item .restore-inline-btn').first()).toHaveCSS(
        'opacity',
        '1'
      );

      await firstItem.locator('.backup-select-btn').click();
      await expect(page.locator('.diff-viewer-container')).toBeVisible();
      const diff = await page.locator('.diff-body').boundingBox();
      expect(diff!.height, 'область сравнения читаема').toBeGreaterThan(80);
      await expect(page.locator('.diff-line-removed').first()).toBeVisible();

      await expectNoHorizontalOverflow(page);

      await page.locator('.drawer-close-btn').click();
      await expect(drawer).toBeHidden();
    });

    test('модальные окна шаблонов и генератора адаптированы под 390px', async ({ page }) => {
      await page.goto('/#/editor');
      await openFile(page, 'config.yaml');

      // 1. Шаблоны через кебаб-меню
      await page.locator('.btn-kebab').click();
      await page.locator('.kebab-item:has-text("Шаблоны")').click();
      const templatesModal = page.locator('.templates-wide-modal');
      await expect(templatesModal).toBeVisible();
      await expectInsideViewport(page, '.templates-body-grid', 'сетка шаблонов');
      await expectNoHorizontalOverflow(page);

      // Закрываем модалку шаблонов
      await page.locator('.templates-wide-modal .modal-close-btn').click();
      await expect(templatesModal).toBeHidden();

      // 2. Генератор через открытие JSON-файла
      await openFile(page, 'outbounds.json');
      await page.locator('.btn-kebab').click();
      const genItem = page.locator('.kebab-item:has-text("Генератор")');
      await expect(genItem).toBeVisible();
      await genItem.click();

      // Модалка генератора
      const genGrid = page.locator('.gen-form-grid').first();
      await expect(genGrid).toBeVisible();
      await expectInsideViewport(page, '.gen-form-grid', 'сетка полей генератора');
      await expectNoHorizontalOverflow(page);

      // Закрываем генератор
      await page.locator('.confirm-modal-actions .btn-secondary').click();
      await expect(genGrid).toBeHidden();
    });
  });
}

test.describe('Редактор на широком экране не меняется', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test('дерево файлов — колонка с разделителем, а не лист', async ({ page }) => {
    await mockEditor(page, 'dark');
    await page.goto('/#/editor');
    await expect(page.locator('.file-tree-pane')).toBeVisible();
    await expect(page.locator('.file-tree-pane.overlay')).toHaveCount(0);
    await expect(page.locator('.editor-splitter')).toHaveCount(1);
    await expect(page.locator('.file-tree-backdrop')).toHaveCount(0);

    await page.locator('.file-row:has-text("config.yaml")').click();
    await page.locator('.backups-toggle-btn').click();
    await expect(page.locator('.editor-bottom-drawer')).toBeVisible();
    // Кнопка «Закрыть» нужна только на узком экране
    await expect(page.locator('.drawer-close-btn')).toBeHidden();
  });
});
