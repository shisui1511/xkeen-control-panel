import { test, expect, type Page } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

// Редактор на узком экране (≤768 px): панель файлов — выезжающий лист, действия шапки,
// меню файла, статус-бар и панель бэкапов не выходят за правый край окна.
// Все API замоканы; служб на ПК разработчика тест не трогает.

test.use({ locale: 'ru-RU', viewport: { width: 390, height: 844 } });

const THEMES = ['dark', 'light'] as const;
const MIHOMO_DIR = '/opt/etc/mihomo';
const fileContent = 'port: 7890\nmode: Rule\nlog-level: info\n';
const backupContent = 'port: 7890\nmode: Global\nlog-level: debug\n';

async function mockEditor(page: Page, theme: (typeof THEMES)[number]) {
  await setupMocks(page, 'mihomo');
  await page.addInitScript((t) => {
    localStorage.setItem('theme', t);
  }, theme);

  // Более поздний route приоритетнее: подменяем файловые эндпоинты
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
                { name: 'default.yaml', path: `${MIHOMO_DIR}/default.yaml`, size: 800 }
              ]
            : []
        )
      });
    } else if (url.includes('/api/config/read')) {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: url.includes('backup') || url.includes('bak.1') ? backupContent : fileContent
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

async function openFile(page: Page) {
  await page.locator('.editor-empty-actions .btn-secondary').click();
  const pane = page.locator('.file-tree-pane.overlay');
  await expect(pane).toBeVisible();
  await pane.locator('.file-row:has-text("config.yaml")').click();
  await expect(pane).toBeHidden();
  await expect(page.locator('.editor-tab:has-text("config.yaml")')).toBeVisible();
}

for (const theme of THEMES) {
  test.describe(`Редактор на 390 px (${theme})`, () => {
    test.beforeEach(async ({ page }) => {
      await mockEditor(page, theme);
    });

    test('панель файлов выезжает листом, выбор файла закрывает её', async ({ page }) => {
      await page.goto('/#/editor');
      await expect(page.locator('.editor-empty-card')).toBeVisible();
      // Колонки с деревом файлов рядом с редактором нет
      await expect(page.locator('.file-tree-pane')).toHaveCount(0);

      await page.locator('.editor-empty-actions .btn-secondary').click();
      const pane = page.locator('.file-tree-pane.overlay');
      await expect(pane).toBeVisible();
      await expectInsideViewport(page, '.file-tree-pane.overlay', 'лист файлов');

      // Клик по подложке закрывает лист
      await page.locator('.file-tree-backdrop').click({ position: { x: 345, y: 300 } });
      await expect(pane).toBeHidden();

      await openFile(page);
      await expectNoHorizontalOverflow(page);
    });

    test('действия шапки, меню файла и статус-бар не за краем окна', async ({ page }) => {
      await page.goto('/#/editor');
      await openFile(page);

      await expectInsideViewport(
        page,
        '.page-header-actions .btn-accent',
        '«Сохранить и применить»'
      );
      await expectInsideViewport(page, '.btn-kebab', 'меню действий файла');
      await expectInsideViewport(page, '.editor-statusbar .chip-toggle >> nth=1', '«Эксперт»');
      await expectInsideViewport(page, '.backups-toggle-btn', '«Backups»');
      await expectNoHorizontalOverflow(page);

      // Меню действий открывается и тоже помещается в окно
      await page.locator('.btn-kebab').click();
      await expectInsideViewport(page, '.kebab-dropdown', 'выпадающее меню');
      // Редактор не схлопнут
      const cm = await page.locator('.cm-editor').boundingBox();
      expect(cm!.height).toBeGreaterThan(120);
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
