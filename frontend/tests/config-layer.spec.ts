// e2e-pages: config settings
import { test, expect } from '@playwright/test';
import { visitPage } from './helpers/api-mocks';
import { mockConfigLayer } from './helpers/config-layer-mocks';
import { LAZY_LOAD_TIMEOUT } from './helpers/timeouts';

test.use({ locale: 'ru-RU' });

test.describe('Слой «Конфигурация»: черновик с сервера (tracer)', () => {
  test('SSE draft обновляет полосу черновика, значение переживает перезагрузку', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page);
    await visitPage(page, '/#/config');

    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Черновик пуст', { timeout: LAZY_LOAD_TIMEOUT });

    await mock.waitConnected();
    await mock.emit('draft', { draft_revision: 2, draft_changes: 3 });
    await expect(bar).toContainText('Неприменённых изменений: 3');

    // Сервер помнит черновик: после F5 состояние приходит из GET /state
    mock.snapshot.draft_revision = 2;
    mock.snapshot.draft_changes = 3;
    await page.reload();
    await expect(page.getByTestId('config-draftbar')).toContainText('Неприменённых изменений: 3', {
      timeout: LAZY_LOAD_TIMEOUT
    });
    expect(mock.callsTo('GET', '/state').length).toBeGreaterThanOrEqual(2);
  });
});
