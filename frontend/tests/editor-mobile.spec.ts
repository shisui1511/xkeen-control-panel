// e2e-pages: editor
import { test, expect, type Page } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

// Редактор на узком экране (≤768 px): панель файлов — выезжающий лист, действия шапки,
// меню файла, статус-бар, вкладки, модалки и панель бэкапов не выходят за правый край окна.
// Все API замоканы; служб на ПК разработчика тест не трогает.

test.use({ locale: 'ru-RU', viewport: { width: 390, height: 844 }, hasTouch: true });

const THEMES = ['dark', 'light'] as const;
const MIHOMO_DIR = '/opt/etc/mihomo';
const fileContent =
  '# Подписка обновляется каждые шесть часов, адрес https://example.com/api/v1/client/subscribe?token=0123456789abcdef0123456789abcdef\n' +
  'port: 7890\nmode: Rule\nlog-level: info\n';
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
                { name: 'outbounds.json', path: `${MIHOMO_DIR}/outbounds.json`, size: 400 },
                {
                  name: '04_outbounds.sub_provider_nodes.json',
                  path: `${MIHOMO_DIR}/04_outbounds.sub_provider_nodes.json`,
                  size: 2048
                },
                {
                  name: '04_outbounds.zz_xcp_generated.json',
                  path: `${MIHOMO_DIR}/04_outbounds.zz_xcp_generated.json`,
                  size: 1024
                }
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

/** Размер реальной зоны нажатия: идём от центра элемента, пока elementFromPoint возвращает его самого или потомка */
async function hitZone(page: Page, selector: string, index = 0) {
  const loc = page.locator(selector).nth(index);
  await loc.scrollIntoViewIfNeeded();
  return loc.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const cx = r.left + r.width / 2;
    const cy = r.top + r.height / 2;
    const hits = (x: number, y: number) => {
      const top = document.elementFromPoint(x, y);
      return !!top && (top === el || el.contains(top));
    };
    const scan = (dx: number, dy: number) => {
      let n = 0;
      while (n < 60 && hits(cx + dx * (n + 1), cy + dy * (n + 1))) n++;
      return n;
    };
    return {
      width: scan(-1, 0) + scan(1, 0) + 1,
      height: scan(0, -1) + scan(0, 1) + 1
    };
  });
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

      // Статус-бар: однострочный (46 px под зоны нажатия), подсказка Ctrl+S скрыта
      const statusbar = page.locator('.editor-statusbar');
      await expect(statusbar).toBeVisible();
      const statusBox = await statusbar.boundingBox();
      expect(statusBox!.height, 'высота статус-бара').toBeLessThanOrEqual(48);
      const leftBox = (await page.locator('.sb-left').boundingBox())!;
      const rightBox = (await page.locator('.sb-right').boundingBox())!;
      expect(
        Math.abs(leftBox.y + leftBox.height / 2 - (rightBox.y + rightBox.height / 2)),
        'статус-бар в одну строку'
      ).toBeLessThanOrEqual(2);
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

const LONG_NAMES = ['04_outbounds.sub_provider_nodes.json', '04_outbounds.zz_xcp_generated.json'];

for (const vp of [
  { label: '390', width: 390, height: 844 },
  { label: '768', width: 768, height: 1024 }
]) {
  test.describe(`Редактор после проверки на роутере (${vp.label} px)`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height }, hasTouch: true });

    test.beforeEach(async ({ page }) => {
      await mockEditor(page, 'light');
    });

    test('шторка файлов — fixed на всю высоту окна с кнопкой «Закрыть»', async ({ page }) => {
      await page.goto('/#/editor');
      await page.locator('.editor-empty-actions .btn-secondary').click();
      const pane = page.locator('.file-tree-pane.overlay');
      await expect(pane).toBeVisible();

      await expect(pane).toHaveCSS('position', 'fixed');
      const paneBox = (await pane.boundingBox())!;
      expect(paneBox.y, 'шторка от верхнего края').toBeLessThanOrEqual(1);
      expect(paneBox.height, 'шторка на всю высоту').toBeGreaterThanOrEqual(vp.height - 1);

      const backdropBox = (await page.locator('.file-tree-backdrop').boundingBox())!;
      expect(Math.abs(backdropBox.x)).toBeLessThanOrEqual(1);
      expect(Math.abs(backdropBox.y)).toBeLessThanOrEqual(1);
      expect(Math.abs(backdropBox.width - vp.width)).toBeLessThanOrEqual(1);
      expect(Math.abs(backdropBox.height - vp.height)).toBeLessThanOrEqual(1);

      // Затемнение накрывает и шапку страницы
      const topClass = await page.evaluate(
        ([x, y]) => document.elementFromPoint(x, y)?.className ?? '',
        [vp.width - 10, 10]
      );
      expect(topClass).toContain('file-tree-backdrop');

      const closeBtn = page.locator('.responsive-sidebar-close');
      const closeBox = (await closeBtn.boundingBox())!;
      expect(closeBox.width).toBeGreaterThanOrEqual(44);
      expect(closeBox.height).toBeGreaterThanOrEqual(44);
      await closeBtn.click();
      await expect(pane).toBeHidden();

      // Escape по-прежнему закрывает
      await page.locator('.editor-empty-actions .btn-secondary').click();
      await expect(pane).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(pane).toBeHidden();
    });

    test('имена файлов переносятся и различимы', async ({ page }) => {
      await page.goto('/#/editor');
      await page.locator('.editor-empty-actions .btn-secondary').click();
      const pane = page.locator('.file-tree-pane.overlay');
      await expect(pane).toBeVisible();

      for (const name of LONG_NAMES) {
        const row = pane.locator(`.file-row:has(.fr-name[title="${name}"])`);
        await expect(row).toBeVisible();
        const m = await row.locator('.fr-name').evaluate((el) => {
          const cs = getComputedStyle(el);
          return {
            clientHeight: el.clientHeight,
            scrollHeight: el.scrollHeight,
            clientWidth: el.clientWidth,
            scrollWidth: el.scrollWidth,
            lineHeight: parseFloat(cs.lineHeight)
          };
        });
        expect(m.clientHeight, `${name}: две строки`).toBeGreaterThanOrEqual(m.lineHeight * 1.6);
        expect(m.scrollHeight, `${name}: высота не обрезана`).toBeLessThanOrEqual(
          m.clientHeight + 1
        );
        expect(m.scrollWidth, `${name}: ширина не обрезана`).toBeLessThanOrEqual(m.clientWidth + 1);

        const meta = row.locator('.fr-meta');
        await expect(meta).toBeVisible();
        const paneBox = (await pane.boundingBox())!;
        const metaBox = (await meta.boundingBox())!;
        expect(metaBox.x + metaBox.width, `${name}: размер внутри панели`).toBeLessThanOrEqual(
          paneBox.x + paneBox.width + 0.5
        );
      }

      const overflowing = await pane
        .locator('.group-path')
        .evaluateAll((els) => els.filter((el) => el.scrollWidth > el.clientWidth + 1).length);
      expect(overflowing, 'путь группы не обрезан').toBe(0);
    });

    test('лист Backups — список, затем сравнение на весь лист', async ({ page }) => {
      await page.goto('/#/editor');
      await openFile(page);

      const stripBox = (await page.locator('.editor-tab-strip').boundingBox())!;
      const stripCenter = [stripBox.x + stripBox.width / 2, stripBox.y + stripBox.height / 2];

      await page.locator('.backups-toggle-btn').click();
      const drawer = page.locator('.editor-bottom-drawer');
      await expect(drawer).toBeVisible();

      // Лист закрывает всю карточку редактора (идёт slide-переход, поэтому poll)
      await expect
        .poll(async () => {
          const d = await drawer.boundingBox();
          const c = await page.locator('.editor-main-card').boundingBox();
          if (!d || !c) return false;
          return Math.abs(d.y - c.y) <= 1 && Math.abs(d.height - c.height) <= 2;
        })
        .toBe(true);
      const inside = await page.evaluate(
        ([x, y]) => !!document.elementFromPoint(x, y)?.closest('.editor-bottom-drawer'),
        stripCenter
      );
      expect(inside, 'полоса вкладок закрыта листом').toBe(true);

      await expect(page.locator('.drawer-sidebar')).toBeVisible();
      await expect(page.locator('.drawer-main')).toBeHidden();
      await expect(page.getByText('Выберите резервную копию слева')).toBeHidden();
      await expect(page.locator('.restore-inline-btn').first()).toBeHidden();

      await page.locator('.backup-select-btn').first().click();
      await expect(page.locator('.drawer-sidebar')).toBeHidden();
      await expect(page.locator('.diff-viewer-container')).toBeVisible();
      await expect(page.locator('.diff-line-removed').first()).toBeVisible();
      const diff = (await page.locator('.diff-body').boundingBox())!;
      expect(diff.height, 'сравнение на весь лист').toBeGreaterThan(250);

      for (const sel of ['.drawer-back-btn', '.diff-restore-btn']) {
        const box = (await page.locator(sel).boundingBox())!;
        expect(box.height, `${sel}: высота`).toBeGreaterThanOrEqual(44);
      }
      await expectNoHorizontalOverflow(page);

      await page.locator('.drawer-back-btn').click();
      await expect(page.locator('.drawer-sidebar')).toBeVisible();
      await expect(page.locator('.drawer-main')).toBeHidden();

      await page.locator('.drawer-close-btn').click();
      await expect(drawer).toBeHidden();
    });

    test('тап-цели вкладок и статус-бара ≥ 44 px, иконки прежнего размера', async ({ page }) => {
      await page.goto('/#/editor');
      await openFile(page, 'config.yaml', true);
      await openFile(page, '04_outbounds.sub_provider_nodes.json');

      const targets: Array<[string, number]> = [
        ['.editor-tab.active .tab-close-btn', 0],
        ['.btn-kebab', 0],
        ['.editor-statusbar .chip-toggle', 0],
        ['.editor-statusbar .chip-toggle', 1],
        ['.backups-toggle-btn', 0]
      ];
      for (const [sel, idx] of targets) {
        const zone = await hitZone(page, sel, idx);
        expect(zone.width, `${sel}[${idx}]: ширина зоны`).toBeGreaterThanOrEqual(44);
        expect(zone.height, `${sel}[${idx}]: высота зоны`).toBeGreaterThanOrEqual(44);
      }

      const svgBox = (await page.locator('.tab-close-btn svg').first().boundingBox())!;
      expect(svgBox.width, 'иконка × прежнего размера').toBeLessThanOrEqual(9);
      const kebabBox = (await page.locator('.btn-kebab').boundingBox())!;
      expect(kebabBox.width).toBeLessThanOrEqual(27);
      expect(kebabBox.height).toBeLessThanOrEqual(27);
      const chipBox = (await page.locator('.editor-statusbar .chip-toggle').first().boundingBox())!;
      expect(chipBox.height, 'чип визуально прежний').toBeLessThanOrEqual(26);
    });

    test('название вкладки — многоточие, активная вкладка видна целиком', async ({ page }) => {
      await page.goto('/#/editor');
      await openFile(page, 'config.yaml', true);
      await openFile(page, 'default.yaml', true);
      await openFile(page, LONG_NAMES[0]);

      const name = page.locator('.editor-tab.active .tab-name');
      await expect(name).toBeVisible();
      await expect(name).toHaveCSS('text-overflow', 'ellipsis');
      await expect(name).toHaveCSS('white-space', 'nowrap');
      const m = await name.evaluate((el) => ({
        scrollWidth: el.scrollWidth,
        clientWidth: el.clientWidth
      }));
      expect(m.scrollWidth, 'имя обрезано').toBeGreaterThan(m.clientWidth);
      expect((await name.boundingBox())!.width).toBeLessThanOrEqual(121);

      await expect
        .poll(async () => {
          const tab = await page.locator('.editor-tab.active').boundingBox();
          const strip = await page.locator('.editor-tab-strip').boundingBox();
          if (!tab || !strip) return false;
          return tab.x >= strip.x - 1 && tab.x + tab.width <= strip.x + strip.width + 1;
        })
        .toBe(true);
    });

    test('«Ещё» в шапке отличается от «⋮» вкладок', async ({ page }) => {
      await page.goto('/#/editor');
      await openFile(page);

      const more = page.locator('.btn-overflow-trigger');
      await expect(more).toContainText('Ещё');
      await expect(more).toHaveAttribute('aria-label', 'Ещё: действия сохранения');
      await expect(more.locator('circle')).toHaveCount(0);

      const kebab = page.locator('.btn-kebab');
      await expect(kebab).toHaveAttribute('aria-label', 'Действия с файлом');
      await expect(kebab.locator('circle')).toHaveCount(3);

      await expectInsideViewport(page, '.btn-overflow-trigger', '«Ещё»');
      await expectInsideViewport(page, '.btn-kebab', '«⋮» вкладок');
      await more.click();
      await expect(page.locator('.overflow-dropdown')).toBeVisible();
    });

    test('шапка без описания, пустое состояние для сенсорного экрана', async ({ page }) => {
      await page.goto('/#/editor');
      await expect(page.locator('.page-header h1')).toBeVisible();
      await expect(page.locator('nav.breadcrumbs')).toBeVisible();
      await expect(page.locator('.page-header-subtitle')).toBeHidden();
      await expect(page.locator('.editor-empty-card')).toContainText(
        'Откройте файл из списка или создайте новый'
      );
      await expect(page.locator('.editor-empty-shortcuts')).toHaveCount(0);
    });

    test('тап-цели переключателя и «На весь экран» ≥ 44 px', async ({ page }) => {
      await page.goto('/#/editor');
      const pill = '.page-header-actions .tabs-pill .tab-btn';
      for (const idx of [0, 1]) {
        const zone = await hitZone(page, pill, idx);
        expect(zone.width, `переключатель ${idx}: ширина`).toBeGreaterThanOrEqual(44);
        expect(zone.height, `переключатель ${idx}: высота`).toBeGreaterThanOrEqual(44);
      }
      expect((await page.locator(pill).first().boundingBox())!.height).toBeLessThanOrEqual(30);

      await openFile(page);
      const zone = await hitZone(page, '.editor-cm-tool-btn');
      expect(zone.width, '«На весь экран»: ширина').toBeGreaterThanOrEqual(44);
      expect(zone.height, '«На весь экран»: высота').toBeGreaterThanOrEqual(44);
      const svg = (await page.locator('.editor-cm-tool-btn svg').boundingBox())!;
      expect(svg.width).toBeLessThanOrEqual(15);
    });

    test('«На весь экран» не накрывает первые строки, перенос по словам', async ({ page }) => {
      await page.goto('/#/editor');
      await openFile(page);
      await expect(page.locator('.cm-content > .cm-line').first()).toBeVisible();

      const res = await page.evaluate(() => {
        const toolbar = document.querySelector('.editor-cm-toolbar')!.getBoundingClientRect();
        const btn = document.querySelector('.editor-cm-tool-btn')!.getBoundingClientRect();
        const cx = btn.left + btn.width / 2;
        const cy = btn.top + btn.height / 2;
        const zone = {
          left: Math.min(toolbar.left, cx - 22),
          right: Math.max(toolbar.right, cx + 22),
          top: Math.min(toolbar.top, cy - 22),
          bottom: Math.max(toolbar.bottom, cy + 22)
        };
        const lines = Array.from(document.querySelectorAll('.cm-content > .cm-line')).slice(0, 2);
        let overlaps = 0;
        for (const line of lines) {
          const range = document.createRange();
          range.selectNodeContents(line);
          for (const r of Array.from(range.getClientRects())) {
            if (r.width === 0 || r.height === 0) continue;
            if (
              r.left < zone.right &&
              r.right > zone.left &&
              r.top < zone.bottom &&
              r.bottom > zone.top
            ) {
              overlaps++;
            }
          }
        }
        // Слова первой строки не разорваны: у каждого слова ровно один client rect
        const first = lines[0];
        const walker = document.createTreeWalker(first, NodeFilter.SHOW_TEXT);
        const words = ['Подписка', 'обновляется', 'каждые', 'шесть', 'часов,'];
        const broken: string[] = [];
        for (const w of words) {
          let found = false;
          walker.currentNode = first;
          for (let n = walker.nextNode(); n; n = walker.nextNode()) {
            const text = n.textContent ?? '';
            const at = text.indexOf(w);
            if (at < 0) continue;
            found = true;
            const rg = document.createRange();
            rg.setStart(n, at);
            rg.setEnd(n, at + w.length);
            if (rg.getClientRects().length !== 1) broken.push(w);
            break;
          }
          if (!found) broken.push(`${w} (не найдено)`);
        }
        return { overlaps, broken };
      });
      expect(res.overlaps, 'кнопка не накрывает текст первых строк').toBe(0);
      expect(res.broken, 'слова не разорваны').toEqual([]);
    });
  });
}

test.describe('Редактор на широком экране не меняется', () => {
  // Мышь, а не касание: иначе (pointer: coarse) включит сенсорную раскладку
  test.use({ viewport: { width: 1280, height: 800 }, hasTouch: false });

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
    await expect(page.locator('.drawer-topbar')).toBeHidden();
    // Список копий и подсказка рядом
    await expect(page.locator('.drawer-sidebar')).toBeVisible();
    await expect(page.locator('.drawer-main')).toBeVisible();
    await expect(page.locator('.drawer-empty-state')).toBeVisible();
  });

  test('длинные имена файлов в одну строку', async ({ page }) => {
    await mockEditor(page, 'dark');
    await page.goto('/#/editor');
    const name = page.locator(`.fr-name[title="${LONG_NAMES[0]}"]`);
    await expect(name).toBeVisible();
    await expect(name).toHaveCSS('white-space', 'nowrap');
  });

  test('на ПК шапка и пустое состояние прежние', async ({ page }) => {
    await mockEditor(page, 'dark');
    await page.goto('/#/editor');
    await expect(page.locator('.page-header-subtitle')).toBeVisible();
    await expect(page.locator('.editor-empty-card')).toContainText(
      'Откройте файл из боковой панели слева'
    );
    await expect(page.locator('.editor-empty-shortcuts')).toBeVisible();
  });

  test('на ПК статус-бар остаётся компактным', async ({ page }) => {
    await mockEditor(page, 'dark');
    await page.goto('/#/editor');
    await page.locator('.file-row:has-text("config.yaml")').click();
    const statusbar = page.locator('.editor-statusbar');
    await expect(statusbar).toBeVisible();
    expect((await statusbar.boundingBox())!.height).toBeLessThanOrEqual(30);
  });
});
