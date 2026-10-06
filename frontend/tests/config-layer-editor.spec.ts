// e2e-pages: editor
import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { visitPage } from './helpers/api-mocks';
import { mockConfigLayer, type ConfigLayerMockOptions } from './helpers/config-layer-mocks';
import { LAZY_LOAD_TIMEOUT } from './helpers/timeouts';

// Редактор и файлы панели (D-12): файл под управлением слоя открывается только для чтения,
// «Отпустить управление» делает его ручным. Все API замоканы; служб на ПК не трогаем.

test.use({ locale: 'ru-RU' });

const XRAY_DIR = '/opt/etc/xray/configs';
const MIHOMO_DIR = '/opt/etc/mihomo';

const MANAGED_PATH = `${XRAY_DIR}/04_outbounds.xcp-diag.tail.json`;
const MANAGED_KEY = 'xray/04_outbounds.xcp-diag.tail.json';
const MANUAL_PATH = `${XRAY_DIR}/05_routing.json`;
const FILE_BODY = '{\n  "outbounds": []\n}';

const managedFile = {
  key: MANAGED_KEY,
  kernel: 'xray',
  path: MANAGED_PATH,
  owner: 'panel',
  state: 'ok'
};

const releasedFile = {
  key: 'xray/05_routing.json',
  kernel: 'xray',
  path: MANUAL_PATH,
  owner: 'manual',
  state: 'released'
};

interface EditorMock {
  saves: string[];
}

/** Моки Редактора поверх слоя «Конфигурация»: список и чтение файлов, сохранение. */
async function openEditor(page: Page, opts: ConfigLayerMockOptions = {}) {
  // Мок release меняет объекты файлов на месте: у каждого теста своя копия
  const files = structuredClone(opts.snapshot?.files ?? [managedFile]);
  const mock = await mockConfigLayer(page, {
    ...opts,
    snapshot: { ...opts.snapshot, files }
  });
  const rec: EditorMock = { saves: [] };

  await page.route('**/api/config/**', async (route) => {
    const url = new URL(route.request().url());
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (url.pathname === '/api/config/list') {
      const dir = url.searchParams.get('dir') || '';
      await json(
        dir.includes('mihomo')
          ? [{ name: 'config.yaml', path: `${MIHOMO_DIR}/config.yaml`, size: 1500 }]
          : [
              {
                name: '04_outbounds.xcp-diag.tail.json',
                path: MANAGED_PATH,
                size: 900
              },
              { name: '05_routing.json', path: MANUAL_PATH, size: 400 }
            ]
      );
    } else if (url.pathname === '/api/config/read') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: FILE_BODY });
    } else if (url.pathname === '/api/config/backups') {
      await json([]);
    } else if (url.pathname === '/api/config/save') {
      rec.saves.push(url.searchParams.get('path') || '');
      await json({ success: true });
    } else {
      await json({ success: true });
    }
  });

  await visitPage(page, '/#/editor');
  return { mock, rec };
}

/** Открыть файл кликом по строке дерева (одиночный клик — предпросмотр). */
async function openFileRow(page: Page, name: string) {
  const row = page.locator('.file-row', { hasText: name }).first();
  await expect(row).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
  await row.click();
  await expect(page.locator('.cm-content')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
}

const saveButton = (page: Page) => page.getByRole('button', { name: 'Сохранить', exact: true });
const saveAndApplyButton = (page: Page) =>
  page.getByRole('button', { name: 'Сохранить и применить', exact: true });

test.describe('Редактор: файл панели только для чтения (tracer)', () => {
  test('открыть файл панели → плашка и только чтение → «Отпустить управление» → POST release → файл редактируется', async ({
    page
  }) => {
    const { mock } = await openEditor(page);
    await openFileRow(page, '04_outbounds.xcp-diag.tail.json');

    // Плашка выводится вместе с файлом, без отдельного запроса
    const banner = page.getByTestId('managed-file-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText(
      'Файл панели — только чтение. Панель пересоздаёт его при каждом применении.'
    );
    expect(mock.callsTo('GET', '/files')).toHaveLength(0);

    // Клавиатура не меняет документ, «Сохранить» и «Сохранить и применить» недоступны
    const content = page.locator('.cm-content');
    await expect(content).toHaveAttribute('contenteditable', 'false');
    await content.click();
    await page.keyboard.type('zzz');
    await expect(content).not.toContainText('zzz');
    await expect(saveButton(page)).toBeDisabled();
    await expect(saveAndApplyButton(page)).toBeDisabled();
    await expect(saveButton(page)).toHaveAttribute('title', 'Файл панели — только чтение');

    // Отпустить управление → подтверждение → POST /files/release {key}
    await banner.getByRole('button', { name: 'Отпустить управление' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Отпустить управление файлом?');
    await expect(dialog).toContainText(MANAGED_PATH);
    await dialog.getByRole('button', { name: 'Отпустить', exact: true }).click();

    await expect(banner).toHaveCount(0);
    const calls = mock.callsTo('POST', '/files/release');
    expect(calls).toHaveLength(1);
    expect(calls[0].body).toEqual({ key: MANAGED_KEY });

    // Редактор стал редактируемым без перезагрузки
    await expect(content).toHaveAttribute('contenteditable', 'true');
    await content.click();
    await page.keyboard.type('zzz');
    await expect(content).toContainText('zzz');
    await expect(saveButton(page)).toBeEnabled();
  });
});

const kebab = (page: Page) => page.getByRole('button', { name: 'Действия с файлом' });

async function openKebab(page: Page) {
  await kebab(page).click();
  await expect(page.locator('.kebab-dropdown')).toBeVisible();
}

/** Пункты меню файла, которые меняют файл: у файла панели недоступны. */
const EDIT_ITEMS = ['Переименовать', 'Шаблоны', 'Генератор', 'Быстрые исправления', 'Удалить'];

test.describe('Редактор: бейджи «панель» и «ручной»', () => {
  test('в дереве и во вкладке: «панель» у файла панели, «ручной» у отпущенного, у чужого файла нет', async ({
    page
  }) => {
    await openEditor(page, { snapshot: { files: [managedFile, releasedFile] } });

    const managedRow = page.locator('.file-row', { hasText: '04_outbounds.xcp-diag.tail.json' });
    const manualRow = page.locator('.file-row', { hasText: '05_routing.json' });
    await expect(managedRow.getByTestId('layer-badge')).toHaveText('панель', {
      timeout: LAZY_LOAD_TIMEOUT
    });
    await expect(managedRow.getByTestId('layer-badge')).toHaveClass(/badge-info/);
    await expect(manualRow.getByTestId('layer-badge')).toHaveText('ручной');
    await expect(manualRow.getByTestId('layer-badge')).not.toHaveClass(/badge-info/);
    // Файл вне манифеста: бейджа нет
    await expect(
      page.locator('.file-row', { hasText: 'config.yaml' }).getByTestId('layer-badge')
    ).toHaveCount(0);

    // Имя сохраняет многоточие, бейдж не сжимается
    await expect(managedRow.locator('.fr-name')).toHaveCSS('text-overflow', 'ellipsis');
    await expect(managedRow.getByTestId('layer-badge')).toHaveCSS('flex-shrink', '0');

    await managedRow.click();
    await manualRow.dblclick();
    const tabs = page.locator('.editor-tab');
    await expect(tabs).toHaveCount(2);
    await expect(
      tabs.filter({ hasText: '04_outbounds.xcp-diag.tail.json' }).getByTestId('layer-badge')
    ).toHaveText('панель');
    await expect(tabs.filter({ hasText: '05_routing.json' }).getByTestId('layer-badge')).toHaveText(
      'ручной'
    );
    await expect(tabs.first().locator('.tab-name')).toHaveAttribute('title', /\.json$/);
  });

  test('отпускание меняет бейдж «панель» на «ручной» в дереве и во вкладке', async ({ page }) => {
    await openEditor(page);
    await openFileRow(page, '04_outbounds.xcp-diag.tail.json');
    const badges = page.getByTestId('layer-badge');
    await expect(badges).toHaveText(['панель', 'панель']);

    await page.getByTestId('managed-file-banner').getByRole('button').click();
    await page.getByRole('dialog').getByRole('button', { name: 'Отпустить', exact: true }).click();
    await expect(page.getByTestId('managed-file-banner')).toHaveCount(0);
    await expect(badges).toHaveText(['ручной', 'ручной']);
  });
});

test.describe('Редактор: правящие действия панели инструментов', () => {
  test('у файла панели меню файла блокирует правку, скачивание доступно; у отпущенного всё доступно', async ({
    page
  }) => {
    await openEditor(page, { snapshot: { files: [managedFile, releasedFile] } });
    await openFileRow(page, '04_outbounds.xcp-diag.tail.json');

    await openKebab(page);
    const menu = page.locator('.kebab-dropdown');
    for (const name of EDIT_ITEMS) {
      const item = menu.getByRole('button', { name, exact: true });
      await expect(item, name).toBeDisabled();
      await expect(item, name).toHaveAttribute('title', 'Файл панели — только чтение');
    }
    await expect(menu.getByRole('button', { name: 'Скачать', exact: false })).toBeEnabled();
    await page.keyboard.press('Escape');
    await page.locator('.editor-subhead-bar').click({ position: { x: 150, y: 5 } });

    // Отпущенный файл — как обычный
    await page.locator('.file-row', { hasText: '05_routing.json' }).dblclick();
    await expect(page.getByTestId('managed-file-banner')).toHaveCount(0);
    await expect(saveButton(page)).toBeEnabled();
    await expect(saveAndApplyButton(page)).toBeEnabled();
    await openKebab(page);
    for (const name of EDIT_ITEMS) {
      await expect(
        page.locator('.kebab-dropdown').getByRole('button', { name, exact: true }),
        name
      ).toBeEnabled();
    }
  });

  test('Ctrl+S на файле панели ничего не сохраняет', async ({ page }) => {
    const { rec } = await openEditor(page);
    await openFileRow(page, '04_outbounds.xcp-diag.tail.json');
    await page.keyboard.press('Control+s');
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(rec.saves).toHaveLength(0);
  });
});

test.describe('Редактор: флаг выключен — как до включения слоя', () => {
  test('ни плашки, ни бейджей, ни блокировок; сохранение доступно', async ({ page }) => {
    const { mock } = await openEditor(page, { flag: false });
    await openFileRow(page, '04_outbounds.xcp-diag.tail.json');

    await expect(page.getByTestId('managed-file-banner')).toHaveCount(0);
    await expect(page.getByTestId('layer-badge')).toHaveCount(0);
    const content = page.locator('.cm-content');
    await expect(content).toHaveAttribute('contenteditable', 'true');
    await content.click();
    await page.keyboard.type('zzz');
    await expect(content).toContainText('zzz');
    await expect(saveButton(page)).toBeEnabled();
    await expect(saveAndApplyButton(page)).toBeEnabled();
    await openKebab(page);
    for (const name of EDIT_ITEMS) {
      await expect(
        page.locator('.kebab-dropdown').getByRole('button', { name, exact: true }),
        name
      ).toBeEnabled();
    }
    // Слой не опрашивался
    expect(mock.callsTo('GET', '/state')).toHaveLength(0);
  });
});

test.describe('Редактор: сбой отпускания управления', () => {
  test('409 apply_busy → тост, плашка остаётся, Редактор только для чтения', async ({ page }) => {
    const { mock } = await openEditor(page);
    await page.route('**/api/configlayer/files/release', async (route) => {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ success: false, code: 'apply_busy', error: 'Применение идёт' })
      });
    });
    await openFileRow(page, '04_outbounds.xcp-diag.tail.json');

    const banner = page.getByTestId('managed-file-banner');
    await banner.getByRole('button', { name: 'Отпустить управление' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Отпустить', exact: true }).click();

    await expect(page.getByText('Применение уже идёт')).toBeVisible();
    await expect(banner).toBeVisible();
    await expect(banner.getByRole('button', { name: 'Отпустить управление' })).toBeEnabled();
    await expect(page.locator('.cm-content')).toHaveAttribute('contenteditable', 'false');
    await expect(saveButton(page)).toBeDisabled();
    expect(mock.callsTo('POST', '/files/release')).toHaveLength(0); // перехвачено прицельным route
  });

  test('отказ в подтверждении: запроса нет', async ({ page }) => {
    const { mock } = await openEditor(page);
    await openFileRow(page, '04_outbounds.xcp-diag.tail.json');
    await page.getByTestId('managed-file-banner').getByRole('button').click();
    await page.getByRole('dialog').getByRole('button', { name: 'Отмена' }).click();
    await expect(page.getByTestId('managed-file-banner')).toBeVisible();
    expect(mock.callsTo('POST', '/files/release')).toHaveLength(0);
  });
});

test.describe('Редактор: файл панели на узком экране и доступность', () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`390 px (${theme}): плашка сложена, кнопка 44 px, без горизонтальной прокрутки`, async ({
      page
    }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
      await openEditor(page);

      // Дерево файлов — выезжающий лист
      await page.locator('.editor-empty-actions .btn-secondary').click();
      const pane = page.locator('.file-tree-pane.overlay');
      await expect(pane).toBeVisible();
      await expect(
        pane
          .locator('.file-row', { hasText: '04_outbounds.xcp-diag.tail.json' })
          .getByTestId('layer-badge')
      ).toBeVisible();
      await pane.locator('.file-row', { hasText: '04_outbounds.xcp-diag.tail.json' }).click();
      await expect(pane).toBeHidden();

      const banner = page.getByTestId('managed-file-banner');
      await expect(banner).toBeVisible();
      const button = banner.getByRole('button', { name: 'Отпустить управление' });
      const text = banner.locator('.managed-text');
      const [bb, tb, bt] = [
        await banner.boundingBox(),
        await text.boundingBox(),
        await button.boundingBox()
      ];
      expect(bt!.y, 'кнопка под текстом').toBeGreaterThanOrEqual(tb!.y + tb!.height - 1);
      expect(bt!.height).toBeGreaterThanOrEqual(43.5);
      expect(bt!.width, 'кнопка во всю ширину плашки').toBeGreaterThan(bb!.width * 0.8);
      expect(bb!.x + bb!.width).toBeLessThanOrEqual(390.5);

      // Вкладка: многоточие и зона нажатия не потеряны
      const tab = page.locator('.editor-tab').first();
      await expect(tab.getByTestId('layer-badge')).toBeVisible();
      await expect(tab.locator('.tab-name')).toHaveCSS('text-overflow', 'ellipsis');
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth
      );
      expect(overflow, 'страница не шире окна').toBeLessThanOrEqual(0);
    });
  }

  for (const theme of ['dark', 'light'] as const) {
    test(`axe: Редактор с открытым файлом панели без серьёзных нарушений (${theme})`, async ({
      page
    }) => {
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
      await openEditor(page, { snapshot: { files: [managedFile, releasedFile] } });
      await openFileRow(page, '04_outbounds.xcp-diag.tail.json');
      await expect(page.getByTestId('managed-file-banner')).toBeVisible();
      await page.waitForLoadState('networkidle');
      // Скан ограничен тем, что меняет этот план: у самого Редактора (вкладки role=tablist,
      // бейджи формата, чипы строки состояния) есть прежние замечания, их здесь не проверяем.
      // Fade страницы даёт ложные contrast-нарушения, пока анимация не кончилась
      await page.waitForFunction(() =>
        document.getAnimations().every((a) => a.effect?.getTiming().iterations === Infinity)
      );
      const results = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .include('[data-testid="managed-file-banner"]')
        .include('[data-testid="layer-badge"]')
        .analyze();
      const severe = results.violations.filter(
        (v) => v.impact === 'critical' || v.impact === 'serious'
      );
      expect(
        severe.map((v) => ({ id: v.id, nodes: v.nodes.map((n) => n.target) })),
        `#/editor (${theme})`
      ).toEqual([]);
    });
  }
});
