// e2e-pages: config settings
import { test, expect } from '@playwright/test';
import { visitPage } from './helpers/api-mocks';
import { mockConfigLayer, defaultSnapshot } from './helpers/config-layer-mocks';
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

test.describe('Слой «Конфигурация»: состояния полосы черновика', () => {
  test('dirty + drift: применение заблокировано, сброс доступен', async ({ page }) => {
    await mockConfigLayer(page, { snapshot: { draft_changes: 3, drift_count: 2 } });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Неприменённых изменений: 3', { timeout: LAZY_LOAD_TIMEOUT });
    await expect(bar).toContainText('Применение заблокировано: есть расхождения файлов (2)');
    const apply = page.getByTestId('config-apply');
    await expect(apply).toBeDisabled();
    await expect(apply).toHaveAttribute('title', 'Сначала решите расхождения в списке файлов');
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeEnabled();
  });

  test('clean: обе кнопки недоступны', async ({ page }) => {
    await mockConfigLayer(page);
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Черновик пуст — всё применено', {
      timeout: LAZY_LOAD_TIMEOUT
    });
    await expect(page.getByTestId('config-apply')).toBeDisabled();
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeDisabled();
  });

  test('до загрузки состояния кнопки недоступны', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    await page.route('**/api/configlayer/state', async (route) => {
      await gate;
      await route.fallback();
    });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-draftbar')).toBeVisible({ timeout: LAZY_LOAD_TIMEOUT });
    await expect(page.getByTestId('config-apply')).toBeDisabled();
    release();
    await expect(page.getByTestId('config-apply')).toBeEnabled();
    expect(mock.callsTo('GET', '/state').length).toBeGreaterThanOrEqual(1);
  });

  test('сбой первой загрузки: заглушка с кнопкой «Повторить»', async ({ page }) => {
    const mock = await mockConfigLayer(page, { failState: true });
    await visitPage(page, '/#/config');
    await expect(page.getByText('Не удалось загрузить состояние')).toBeVisible({
      timeout: LAZY_LOAD_TIMEOUT
    });
    mock.failState = false;
    await page.getByRole('button', { name: 'Повторить' }).click();
    await expect(page.getByTestId('config-draftbar')).toContainText('Черновик пуст');
  });

  test('применение из этой вкладки: POST apply, затем «Применяется…»', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    await visitPage(page, '/#/config');
    const apply = page.getByTestId('config-apply');
    await expect(apply).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await mock.waitConnected();
    await apply.click();
    await expect.poll(() => mock.callsTo('POST', '/configlayer/apply').length).toBe(1);
    await mock.emit('apply_step', { id: 'build', state: 'running' });
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Применяется…');
    await expect(apply).toBeDisabled();
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeDisabled();
  });

  test('неудачное применение: полоса снова «есть изменения», черновик цел', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await mock.waitConnected();
    await page.getByTestId('config-apply').click();
    await mock.emit('apply_step', { id: 'build', state: 'running' });
    await mock.emit('apply_done', {
      running: false,
      steps: [{ id: 'build', state: 'failed', message: 'boom' }],
      result: { ok: false, code: 'build_failed', written: 0 }
    });
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Неприменённых изменений: 3');
    await expect(page.getByTestId('config-apply')).toBeEnabled();
  });

  test('применение из другой вкладки: подпись и недоступные кнопки', async ({ page }) => {
    await mockConfigLayer(page, {
      snapshot: {
        draft_changes: 3,
        apply: { running: true, steps: [{ id: 'build', state: 'running' }] }
      }
    });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Применение запущено в другой вкладке', {
      timeout: LAZY_LOAD_TIMEOUT
    });
    await expect(page.getByTestId('config-apply')).toBeDisabled();
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeDisabled();
  });

  test('обрыв SSE: «Нет связи…», после восстановления состояние читается заново', async ({
    page
  }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar).toContainText('Неприменённых изменений: 3', { timeout: LAZY_LOAD_TIMEOUT });
    await mock.waitConnected();
    const before = mock.callsTo('GET', '/state').length;
    await mock.dropConnection();
    await expect(bar).toContainText('Нет связи с панелью. Переподключаемся…');
    await mock.reopen();
    await expect
      .poll(() => mock.callsTo('GET', '/state').length, { timeout: 10_000 })
      .toBeGreaterThan(before);
    await expect(bar).not.toContainText('Нет связи с панелью');
  });

  test('«Сбросить»: подтверждение и POST draft/reset с ревизией', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3, draft_revision: 5 } });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeEnabled({
      timeout: LAZY_LOAD_TIMEOUT
    });
    await bar.getByRole('button', { name: 'Сбросить' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Сбросить черновик?');
    await expect(dialog).toContainText('Неприменённые изменения (3) будут потеряны.');
    await expect(dialog).toContainText('Файлы на диске не изменятся.');
    await dialog.getByRole('button', { name: 'Сбросить' }).click();
    await expect.poll(() => mock.callsTo('POST', '/draft/reset').length).toBe(1);
    expect(mock.callsTo('POST', '/draft/reset')[0].body).toEqual({ revision: 5 });
    await expect(bar).toContainText('Черновик пуст — всё применено');
  });

  test('«Применить» не просит подтверждения', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 1 } });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await page.getByTestId('config-apply').click();
    await expect.poll(() => mock.callsTo('POST', '/configlayer/apply').length).toBe(1);
    await expect(page.getByRole('dialog')).toHaveCount(0);
  });

  test('409 draft_conflict на сбросе: тост и повторный GET state', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 } });
    await visitPage(page, '/#/config');
    const bar = page.getByTestId('config-draftbar');
    await expect(bar.getByRole('button', { name: 'Сбросить' })).toBeEnabled({
      timeout: LAZY_LOAD_TIMEOUT
    });
    mock.draftConflict = true;
    const before = mock.callsTo('GET', '/state').length;
    await bar.getByRole('button', { name: 'Сбросить' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Сбросить' }).click();
    await expect(
      page.getByText('Черновик изменён в другой вкладке. Показана свежая версия')
    ).toBeVisible();
    await expect.poll(() => mock.callsTo('GET', '/state').length).toBeGreaterThan(before);
  });

  test('409 apply_busy на применении: тост «Применение уже идёт»', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: { draft_changes: 3 }, applyBusy: true });
    await visitPage(page, '/#/config');
    await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: LAZY_LOAD_TIMEOUT });
    await page.getByTestId('config-apply').click();
    await expect(page.getByText('Применение уже идёт')).toBeVisible();
    await expect(page.getByTestId('config-draftbar')).toContainText('Неприменённых изменений: 3');
    expect(mock.callsTo('POST', '/configlayer/apply')).toHaveLength(1);
  });
});

test.describe('Слой «Конфигурация»: пункт меню и бейдж', () => {
  test('пункт «Конфигурация» стоит сразу после «Дашборда» в группе «Обзор»', async ({ page }) => {
    await mockConfigLayer(page);
    await visitPage(page, '/#/dashboard');
    const group = page.locator('details.nav-group').first();
    const items = group.locator('a.nav-item');
    await expect(items.nth(1)).toHaveAttribute('href', '#/config', { timeout: LAZY_LOAD_TIMEOUT });
    await expect(items.nth(0)).toHaveAttribute('href', '#/dashboard');
    await expect(items.nth(1)).toContainText('Конфигурация');
  });

  for (const [count, label] of [
    [3, '3'],
    [120, '99+']
  ] as const) {
    test(`бейдж меню: ${count} → «${label}», с aria-label`, async ({ page }) => {
      await mockConfigLayer(page, { snapshot: { draft_changes: count } });
      await visitPage(page, '/#/dashboard');
      const badge = page.locator('.nav-badge-config');
      await expect(badge).toHaveText(label, { timeout: LAZY_LOAD_TIMEOUT });
      await expect(badge).toHaveAttribute('role', 'img');
      await expect(badge).toHaveAttribute('aria-label', `Неприменённых изменений: ${count}`);
    });
  }

  test('бейдж скрыт при нуле и обновляется событием draft на другой странице', async ({ page }) => {
    const mock = await mockConfigLayer(page, { snapshot: defaultSnapshot() });
    await visitPage(page, '/#/dashboard');
    await expect(page.locator('a.nav-item[href="#/config"]')).toBeVisible({
      timeout: LAZY_LOAD_TIMEOUT
    });
    await expect(page.locator('.nav-badge-config')).toHaveCount(0);
    await mock.waitConnected();
    await mock.emit('draft', { draft_revision: 2, draft_changes: 4 });
    await expect(page.locator('.nav-badge-config')).toHaveText('4');
  });
});
