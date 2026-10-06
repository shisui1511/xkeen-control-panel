// e2e-pages: editor
import { test, expect, type Page } from '@playwright/test';
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

interface EditorMock {
  saves: string[];
}

/** Моки Редактора поверх слоя «Конфигурация»: список и чтение файлов, сохранение. */
async function openEditor(page: Page, opts: ConfigLayerMockOptions = {}) {
  const mock = await mockConfigLayer(page, {
    snapshot: { files: [managedFile] },
    ...opts
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
