// e2e-pages: config
import { test, expect } from '@playwright/test';
import { visitPage } from './helpers/api-mocks';
import { mockConfigLayer } from './helpers/config-layer-mocks';
import { LAZY_LOAD_TIMEOUT } from './helpers/timeouts';

test.use({ locale: 'ru-RU' });

const okFile = {
  key: 'xray/xcp-routing.json',
  kernel: 'xray',
  path: 'xcp-routing.json',
  owner: 'panel',
  state: 'ok'
};

test.describe('Файлы панели: дрейф вживую (tracer)', () => {
  test('событие files с дрейфом → строка и предупреждение → diff → «Принять правку» → «Отпущен»', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, { snapshot: { files: [okFile], drift_count: 0 } });
    await visitPage(page, '/#/config');

    const files = page.getByTestId('config-files');
    await expect(files).toContainText('xcp-routing.json', { timeout: LAZY_LOAD_TIMEOUT });
    await expect(files).toContainText('Совпадает');
    await expect(page.getByTestId('config-drift')).toHaveCount(0);

    // Файл изменили снаружи: сервер шлёт files с дрейфом
    await mock.waitConnected();
    const drifted = { ...okFile, state: 'drift_modified' };
    mock.snapshot.files = [drifted];
    mock.snapshot.drift_count = 1;
    await mock.emit('files', { files: [drifted], drift_count: 1 });

    const alert = page.getByTestId('config-drift');
    await expect(alert).toBeVisible();
    await expect(alert).toHaveAttribute('role', 'alert');
    await expect(alert).toContainText('Файлы изменены вне панели');
    await expect(files).toContainText('Изменён вручную');

    // Показать отличия → GET /files/diff, строки «−» и «+»
    const toggle = files.getByRole('button', { name: 'Показать отличия' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await toggle.click();
    await expect(files.getByRole('button', { name: 'Скрыть отличия' })).toHaveAttribute(
      'aria-expanded',
      'true'
    );
    await expect(files.locator('.diff-line-removed').first()).toContainText('"a": 2');
    await expect(files.locator('.diff-line-added').first()).toContainText('"a": 1');
    expect(mock.callsTo('GET', '/files/diff')).toHaveLength(1);

    // Принять правку → подтверждение → POST /files/release
    await files.getByRole('button', { name: 'Принять правку' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Принять правку?');
    await dialog.getByRole('button', { name: 'Принять', exact: true }).click();

    await expect(files).toContainText('Отпущен');
    await expect(files).toContainText('Вручную');
    await expect(page.getByTestId('config-drift')).toHaveCount(0);
    await expect(
      files.getByRole('button', { name: 'Вернуть под управление панели' })
    ).toBeVisible();
    await expect(files.getByRole('button', { name: 'Принять правку' })).toHaveCount(0);
    await expect(files.getByRole('button', { name: 'Пересобрать', exact: true })).toHaveCount(0);
    expect(mock.callsTo('POST', '/files/release')[0].body).toEqual({ key: okFile.key });
  });
});
