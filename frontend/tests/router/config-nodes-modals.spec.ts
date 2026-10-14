// e2e-pages: config config/nodes
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import {
  addToDraft,
  api,
  installedKernels,
  layerFlag,
  listSources,
  nodeLink,
  randomPort,
  resetDraft
} from './lib/nodes-api';

// Модали вкладки «Узлы» настоящей панели (фаза 145, план 15): правка узла «Моих узлов» по
// исходному тексту, настройки подписки, диагностика ответа провайдера по ядрам, удаление с
// подтверждением. Ответы сети не подменяются: черновик наполняют настоящие маршруты
// предпросмотра и добавления (данные — ссылки из internal/nodes/testdata/links, подписка —
// адрес зарезервированной зоны .invalid, провайдера не затрагивает). Флаг слоя включается
// на время прогона и возвращается прежним, черновик сбрасывается.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => layer.enable(browser));
test.afterAll(async ({ browser }) => layer.restore(browser));

async function openNodes(page: Page, width = 1440): Promise<void> {
  await page.setViewportSize({ width, height: width > 600 ? 900 : 844 });
  await page.goto('/#/config/nodes', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('nodes-tab').locator('.skeleton')).toHaveCount(0, {
    timeout: T.action
  });
}

async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await resetDraft(page);
}

async function expandCard(page: Page, id: string): Promise<void> {
  const toggle = page.getByTestId(`nodes-source-${id}`).locator('.sc-expand');
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

// Адрес в зарезервированной зоне .invalid: имя не разрешается нигде
const BAD_SUB = 'https://sub.invalid/feed';
const LONG_SUB = `https://sub.invalid/${'a'.repeat(180)}`;

/** Идентификатор подписки из черновика (адрес в зоне .invalid). */
async function subId(page: Page): Promise<string> {
  const found = (await listSources(page)).filter((s) => s.host === 'sub.invalid');
  expect(found.length, 'подписка из черновика одна').toBe(1);
  return found[0].id;
}

const isUpdate = (r: { method(): string; url(): string }) =>
  r.method() === 'POST' && r.url().endsWith('/api/configlayer/sources/update');

/** Загрузка диагностики завершена без ошибки: показан либо отчёт, либо пустое состояние. */
async function diagSettled(page: Page): Promise<void> {
  await expect(page.getByTestId('nodes-diag-loading')).toHaveCount(0, { timeout: T.action });
  await expect(page.getByTestId('nodes-diag-error')).toHaveCount(0);
  await expect(
    page.getByTestId('nodes-diag-meta').or(page.getByTestId('nodes-diag-empty'))
  ).toBeVisible({ timeout: T.action });
}

/** Где фокус относительно диалога: открыт ли он, внутри ли фокус и какой элемент активен. */
async function focusState(
  page: Page,
  testid: string
): Promise<{ dialog: boolean; inside: boolean; active: string }> {
  return page.evaluate((sel) => {
    const dlg = document.querySelector(sel);
    const el = document.activeElement as HTMLElement | null;
    const active = el
      ? el.getAttribute('data-testid') || `${el.tagName.toLowerCase()}.${el.className}`
      : 'нет';
    return { dialog: !!dlg, inside: !!dlg && !!el && dlg.contains(el), active };
  }, `[data-testid="${testid}"]`);
}

/** Все вкладки диагностики открываются; пробный запрос без ответа объяснён в отчёте. */
async function checkTabs(page: Page): Promise<void> {
  const hasRecord = await page.getByTestId('nodes-diag-meta').isVisible();
  for (const [tab, section] of [
    ['report', 'nodes-diag-report'],
    ['headers', 'nodes-diag-headers'],
    ['raw', 'nodes-diag-raw']
  ]) {
    await page.getByTestId(`nodes-diag-tab-${tab}`).click();
    await expect(page.getByTestId(section).or(page.getByTestId('nodes-diag-empty'))).toBeVisible({
      timeout: T.action
    });
  }
  await page.getByTestId('nodes-diag-tab-report').click();
  if (hasRecord) {
    // адрес в зоне .invalid не отвечает: вместо «Все строки разобраны» — объяснение
    await expect(page.getByTestId('nodes-diag-no-response')).toBeVisible();
    await expect(page.getByTestId('nodes-diag-all-ok')).toHaveCount(0);
    // дата попытки в том же виде, что в карточке: ДД.ММ.ГГГГ ЧЧ:ММ:СС, без американского формата
    await expect(page.getByTestId('nodes-diag-meta')).toContainText(
      /\d{2}\.\d{2}\.\d{4} \d{2}:\d{2}:\d{2}/
    );
  }
}

const advanced = (modal: Locator): Locator => modal.getByTestId('nodes-settings-advanced');

test('modals: «Изменить» узел — исходный текст целиком, предпросмотр, nodes/save с ревизией', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  const port = randomPort();
  const port2 = port + 1;
  const link = nodeLink('vless_ws.txt', port, 'EditA');
  await addToDraft(page, link);

  await openNodes(page);
  await expandCard(page, 'my');
  const row = page
    .locator('.node-row')
    .filter({ hasText: `:${port}` })
    .first();
  await expect(row).toBeVisible({ timeout: T.action });
  const edit = row.locator('[data-testid^="nodes-edit-"]');
  await edit.click();

  const modal = page.getByTestId('nodes-edit-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  // предупреждение над текстом и исходный текст целиком, с учётными данными
  await expect(page.getByTestId('nodes-edit-warning')).toBeVisible();
  const text = page.getByTestId('nodes-edit-text');
  await expect(text).toBeVisible({ timeout: T.action });
  const value = await text.inputValue();
  const secret = /^[a-z]+:\/\/([^@]+)@/.exec(link)?.[1] ?? '';
  expect(secret.length, 'в тестовой ссылке есть учётные данные').toBeGreaterThan(0);
  expect(value, 'в поле исходная ссылка целиком').toContain(secret);
  expect(value).toContain(`:${port}`);
  // предпросмотр единственного узла выполнен тем же распознавателем
  await expect(modal.getByTestId('nodes-preview')).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('nodes-preview-my')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('nodes-edit-save')).toBeEnabled({ timeout: T.action });

  // правка имени и порта: после паузы набора предпросмотр пересчитывается
  // язык интерфейса на стенде не фиксирован: поле имени — первый input модали
  await modal.locator('input.input').first().fill('EditRenamed');
  await text.fill(value.replace(`:${port}`, `:${port2}`));
  await expect(page.getByTestId('nodes-edit-save')).toBeEnabled({ timeout: T.action });

  const req = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().endsWith('/api/configlayer/sources/nodes/save')
  );
  await page.getByTestId('nodes-edit-save').click();
  const body = (await req).postDataJSON() as {
    revision?: unknown;
    entry_id?: unknown;
    name?: unknown;
    text?: unknown;
  };
  expect(typeof body.revision, 'в теле save есть ревизия').toBe('number');
  expect(typeof body.entry_id, 'в теле save есть entry_id').toBe('string');
  expect(body.name).toBe('EditRenamed');
  expect(String(body.text)).toContain(`:${port2}`);

  // модаль закрыта, черновик обновлён: строка с новым портом и новым именем
  await expect(modal).toHaveCount(0, { timeout: T.action });
  const updated = page
    .locator('.node-row')
    .filter({ hasText: `:${port2}` })
    .first();
  await expect(updated).toBeVisible({ timeout: T.action });
  await expect(updated.locator('.node-name')).toHaveText('EditRenamed');
  await expect(page.locator('.node-row').filter({ hasText: `:${port}` })).toHaveCount(0);

  // правка записана в черновик на сервере
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const mine = ((view.data?.sources ?? []) as { id: string; nodes: { name: string }[] }[]).find(
    (s) => s.id === 'my'
  );
  expect(
    mine?.nodes.map((n) => n.name),
    'сервер отдаёт узел с новым именем'
  ).toContain('EditRenamed');

  await guard.assertClean();
});

test('modals: настройки — название, фильтр с пустыми строками, интервал 6 ч, HWID: в update только изменённые поля', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await addToDraft(page, BAD_SUB);
  const id = await subId(page);

  await openNodes(page);
  await page.getByTestId(`nodes-settings-${id}`).click();
  const modal = page.getByTestId('nodes-settings-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  // без изменений записывать нечего
  await expect(page.getByTestId('nodes-settings-save')).toBeDisabled();

  await modal.locator('input.input').first().fill('Renamed sub');
  await page.getByTestId('nodes-settings-exclude').fill('alpha\n\n   \nbeta\n');
  await page.getByTestId('nodes-settings-interval').selectOption('6');
  await advanced(modal).locator('summary').click();
  await modal.locator('input[id$="-hwid"]').fill('HWIDTEST01');

  const req = page.waitForRequest(isUpdate);
  await page.getByTestId('nodes-settings-save').click();
  const body = (await req).postDataJSON() as {
    id?: unknown;
    revision?: unknown;
    patch?: Record<string, unknown>;
  };
  expect(body.id).toBe(id);
  expect(typeof body.revision, 'в теле update есть ревизия').toBe('number');
  expect(Object.keys(body.patch ?? {}).sort(), 'в patch только изменённые поля').toEqual([
    'exclude',
    'hwid',
    'interval',
    'name'
  ]);
  expect(body.patch?.name).toBe('Renamed sub');
  expect(body.patch?.exclude, 'пустые строки фильтра отброшены').toEqual(['alpha', 'beta']);
  expect(body.patch?.interval).toEqual({ mode: 'hours', hours: 6 });
  expect(body.patch?.hwid).toBe('HWIDTEST01');
  await expect(modal).toHaveCount(0, { timeout: T.action });

  // сервер хранит значения; адреса и HWID в представлении нет
  const saved = (await listSources(page)).find((x) => x.id === id);
  expect(saved?.name).toBe('Renamed sub');
  expect(saved?.settings?.exclude).toEqual(['alpha', 'beta']);
  expect(saved?.settings?.interval).toEqual({ mode: 'hours', hours: 6 });
  expect(saved?.settings?.hwid_custom).toBe(true);

  // повторное открытие показывает сохранённое, поле HWID пустое; смена одного поля — один ключ
  await page.getByTestId(`nodes-settings-${id}`).click();
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.locator('input.input').first()).toHaveValue('Renamed sub');
  await expect(page.getByTestId('nodes-settings-exclude')).toHaveValue('alpha\nbeta');
  await expect(page.getByTestId('nodes-settings-interval')).toHaveValue('6');
  await advanced(modal).locator('summary').click();
  await expect(modal.locator('input[id$="-hwid"]')).toHaveValue('');
  await expect(page.getByTestId('nodes-settings-hwid-custom')).toBeVisible();
  await expect(page.getByTestId('nodes-settings-save')).toBeDisabled();
  await page.getByTestId('nodes-settings-interval').selectOption('off');
  const req2 = page.waitForRequest(isUpdate);
  await page.getByTestId('nodes-settings-save').click();
  const body2 = (await req2).postDataJSON() as { patch?: Record<string, unknown> };
  expect(body2.patch, 'меняется только интервал').toEqual({ interval: { mode: 'off' } });
  await expect(modal).toHaveCount(0, { timeout: T.action });

  await guard.assertClean();
});

test('modals: настройки — ссылка редактируема только после «Показать», запись меняет только url', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await addToDraft(page, BAD_SUB);
  const id = await subId(page);

  await openNodes(page);
  await page.getByTestId(`nodes-settings-${id}`).click();
  const modal = page.getByTestId('nodes-settings-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  // до «Показать» ссылка замаскирована, поля правки нет
  await expect(page.getByTestId('nodes-settings-url')).toBeVisible();
  expect(await page.getByTestId('nodes-settings-url').innerText()).not.toBe(BAD_SUB);
  await expect(page.getByTestId('nodes-settings-url-input')).toHaveCount(0);

  await page.getByTestId('nodes-settings-url-reveal').click();
  const input = page.getByTestId('nodes-settings-url-input');
  await expect(input).toBeVisible({ timeout: T.action });
  await expect(input).toHaveValue(BAD_SUB);
  await expect(page.getByTestId('nodes-settings-save')).toBeDisabled();

  const next = `${BAD_SUB}2`;
  await input.fill(next);
  const req = page.waitForRequest(isUpdate);
  await page.getByTestId('nodes-settings-save').click();
  const body = (await req).postDataJSON() as { patch?: Record<string, unknown> };
  expect(body.patch, 'меняется только ссылка').toEqual({ url: next });
  await expect(modal).toHaveCount(0, { timeout: T.action });

  const revealed = await api(page, 'POST', '/api/configlayer/sources/reveal', { id });
  expect(revealed.data?.url, 'сервер хранит новую ссылку').toBe(next);

  await guard.assertClean();
});

test('modals: настройки — ошибка записи показывается алертом в модали, значения не теряются', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await addToDraft(page, BAD_SUB);
  const id = await subId(page);

  await openNodes(page);
  await page.getByTestId(`nodes-settings-${id}`).click();
  const modal = page.getByTestId('nodes-settings-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await modal.locator('input.input').first().fill('KeepMe');
  await advanced(modal).locator('summary').click();
  // HWID с не-ASCII знаками сервер отклоняет: настоящая ошибка записи в черновик
  await modal.locator('input[id$="-hwid"]').fill('ключ');
  await page.getByTestId('nodes-settings-save').click();

  const alert = page.getByTestId('nodes-settings-error');
  await expect(alert).toBeVisible({ timeout: T.action });
  await expect(alert).toHaveAttribute('role', 'alert');
  await expect(modal).toBeVisible();
  await expect(modal.locator('input.input').first()).toHaveValue('KeepMe');
  await expect(modal.locator('input[id$="-hwid"]')).toHaveValue('ключ');
  // кнопка снова доступна: исправление и повторная запись проходят
  await expect(page.getByTestId('nodes-settings-save')).toBeEnabled();
  await modal.locator('input[id$="-hwid"]').fill('HWIDTEST02');
  const req = page.waitForRequest(isUpdate);
  await page.getByTestId('nodes-settings-save').click();
  await req;
  await expect(modal).toHaveCount(0, { timeout: T.action });

  await guard.assertClean();
});

// Публичный адрес без такого пути отвечает кодом 404: настоящий ответ HTTP с ошибкой (не сбой связи)
const NOT_FOUND_SUB = 'https://github.com/xcp-router-test-no-such-feed';

test('modals: диагностика при ответе провайдера с ошибкой HTTP не говорит «все строки разобраны»', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await addToDraft(page, NOT_FOUND_SUB);
  const found = (await listSources(page)).filter((s) => s.host === 'github.com');
  expect(found.length, 'подписка из черновика одна').toBe(1);

  await openNodes(page);
  await page.getByTestId(`nodes-diag-${found[0].id}`).click();
  await expect(page.getByTestId('nodes-diag-modal')).toBeVisible({ timeout: T.action });
  await diagSettled(page);
  await expect(page.getByTestId('nodes-diag-meta')).toContainText(/HTTP 4\d\d/);
  await expect(page.getByTestId('nodes-diag-http-error')).toBeVisible();
  await expect(page.getByTestId('nodes-diag-all-ok')).toHaveCount(0);
  await expect(page.getByTestId('nodes-diag-no-response')).toHaveCount(0);

  await guard.assertClean();
});

test('modals: диагностика — сегмент по установленным ядрам, пустое состояние, запрос с нужным ядром', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  const inst = await installedKernels(page);
  await addToDraft(page, BAD_SUB);
  const id = await subId(page);

  await openNodes(page);
  const first = page.waitForRequest(
    (r) => r.url().includes('/sources/diagnostics') && r.url().includes(`id=${id}`)
  );
  await page.getByTestId(`nodes-diag-${id}`).click();
  const modal = page.getByTestId('nodes-diag-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await first;
  // пробный запрос к адресу в зоне .invalid не дошёл до провайдера: запись диагностики есть
  // (без ответа) или пуста; вкладки показывают разделы либо пустое состояние
  await diagSettled(page);
  await checkTabs(page);

  const segment = page.getByTestId('nodes-diag-kernel');
  if (inst.xray && inst.mihomo) {
    await expect(segment.getByRole('button', { name: 'Xray' })).toBeVisible();
    await expect(segment.getByRole('button', { name: 'Mihomo' })).toBeVisible();
    for (const kernel of ['Xray', 'Mihomo']) {
      const want = kernel.toLowerCase();
      const req = page.waitForRequest(
        (r) => r.url().includes('/sources/diagnostics') && r.url().includes(`kernel=${want}`)
      );
      // активный сегмент уже выбран: второй клик по нему запроса не даёт, поэтому
      // сначала уводим выбор на другое ядро
      const other = kernel === 'Xray' ? 'Mihomo' : 'Xray';
      const pressed = await segment
        .getByRole('button', { name: kernel })
        .getAttribute('aria-pressed');
      if (pressed === 'true') await segment.getByRole('button', { name: other }).click();
      await segment.getByRole('button', { name: kernel }).click();
      await req;
      await expect(segment.getByRole('button', { name: kernel })).toHaveAttribute(
        'aria-pressed',
        'true'
      );
      await diagSettled(page);
    }
  } else {
    // одно ядро — сегмента нет, есть только его имя
    await expect(segment).toBeVisible();
    await expect(segment.getByRole('button')).toHaveCount(0);
  }

  await guard.assertClean();
});

test('modals: диагностика — ошибка загрузки показывает алерт, «Повторить» загружает снова', async ({
  page
}) => {
  // Обрыв связи эмулируется на уровне браузера (ответы сети не подменяются). Сборщик
  // консоли не подключается: обрыв порождает ожидаемые ошибки запросов и потока событий.
  await startPage(page);
  await addToDraft(page, BAD_SUB);
  const id = await subId(page);

  await openNodes(page);
  await expect(page.getByTestId(`nodes-diag-${id}`)).toBeVisible({ timeout: T.action });
  await page.context().setOffline(true);
  try {
    await page.getByTestId(`nodes-diag-${id}`).click();
    const modal = page.getByTestId('nodes-diag-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('nodes-diag-error')).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('nodes-diag-error')).toHaveAttribute('role', 'alert');
    // модаль открыта, вкладки доступны
    await expect(page.getByTestId('nodes-diag-tab-report')).toBeEnabled();
  } finally {
    await page.context().setOffline(false);
  }
  const retry = page.waitForRequest((r) => r.url().includes('/sources/diagnostics'));
  await page.getByTestId('nodes-diag-retry').click();
  await retry;
  await diagSettled(page);
});

test('modals: «Изменить» — текст из двух ссылок даёт ошибку, «Сохранить» неактивна', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  const port = randomPort();
  const link = nodeLink('vless_ws.txt', port, 'EditTwo');
  await addToDraft(page, link);

  await openNodes(page);
  await expandCard(page, 'my');
  const row = page
    .locator('.node-row')
    .filter({ hasText: `:${port}` })
    .first();
  await row.locator('[data-testid^="nodes-edit-"]').click();
  const modal = page.getByTestId('nodes-edit-modal');
  const text = page.getByTestId('nodes-edit-text');
  await expect(text).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('nodes-edit-save')).toBeEnabled({ timeout: T.action });

  const original = await text.inputValue();
  await text.fill(`${original}\n${nodeLink('tuic.txt', port + 1, 'EditTwoB')}`);
  const err = modal.getByTestId('nodes-preview-error');
  await expect(err).toBeVisible({ timeout: T.action });
  expect(await err.textContent(), 'в подробностях код ошибки').toContain('edit_single_node');
  await expect(page.getByTestId('nodes-edit-save')).toBeDisabled();

  // нераспознанная строка: под общей ошибкой видна причина по строке
  await text.fill(`${original}\nfoo://broken`);
  await expect(err).toBeVisible({ timeout: T.action });
  const lineErr = modal.getByTestId('nodes-preview-line-error');
  await expect(lineErr).toHaveCount(1, { timeout: T.action });
  await expect(lineErr).toContainText('foo://');
  await expect(page.getByTestId('nodes-edit-save')).toBeDisabled();

  // пустой текст: предпросмотр скрыт, запись невозможна
  await text.fill('');
  await expect(modal.getByTestId('nodes-preview')).toHaveCount(0);
  await expect(page.getByTestId('nodes-edit-save')).toBeDisabled();

  // вернули одну ссылку: запись снова доступна
  await text.fill(original);
  await expect(page.getByTestId('nodes-edit-save')).toBeEnabled({ timeout: T.action });

  await guard.assertClean();
});

test('modals: «Изменить» — узел только для Mihomo помечается, сохранение разрешено', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  const port = randomPort();
  // tuic выражается только в Mihomo
  await addToDraft(page, nodeLink('tuic.txt', port, 'EditTuic'));

  await openNodes(page);
  await expandCard(page, 'my');
  const row = page
    .locator('.node-row')
    .filter({ hasText: `:${port}` })
    .first();
  await row.locator('[data-testid^="nodes-edit-"]').click();
  const modal = page.getByTestId('nodes-edit-modal');
  await expect(modal.getByTestId('nodes-preview-nodes')).toBeVisible({ timeout: T.action });
  await expect(modal.locator('.chip-info', { hasText: 'Mihomo' })).toBeVisible();
  await expect(page.getByTestId('nodes-edit-save')).toBeEnabled();

  const req = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().endsWith('/api/configlayer/sources/nodes/save')
  );
  await page.getByTestId('nodes-edit-save').click();
  await req;
  await expect(modal).toHaveCount(0, { timeout: T.action });

  await guard.assertClean();
});

test('modals: удаление подписки — подтверждение с именем и числом узлов, POST delete с ревизией, отмена ничего не пишет', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await addToDraft(page, BAD_SUB);
  const id = await subId(page);

  await openNodes(page);
  const card = page.getByTestId(`nodes-source-${id}`);
  await expect(card).toBeVisible({ timeout: T.action });

  // отмена Esc: запроса нет, источник на месте
  let deletes = 0;
  const onRequest = (r: { method(): string; url(): string }) => {
    if (r.method() === 'POST' && r.url().endsWith('/api/configlayer/sources/delete')) deletes++;
  };
  page.on('request', onRequest);
  await page.getByTestId(`nodes-delete-${id}`).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible({ timeout: T.action });
  await expect(dialog.getByRole('heading')).toContainText('sub.invalid');
  await expect(dialog).toContainText('0');
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(page.getByTestId(`nodes-delete-${id}`)).toBeFocused();
  expect(deletes, 'после отмены запросов delete нет').toBe(0);
  await expect(card).toBeVisible();

  // подтверждение: запрос с ревизией, источник исчезает после ответа сервера
  await page.getByTestId(`nodes-delete-${id}`).click();
  await expect(dialog).toBeVisible({ timeout: T.action });
  const req = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().endsWith('/api/configlayer/sources/delete')
  );
  await dialog.locator('.btn-danger').click();
  const body = (await req).postDataJSON() as { id?: unknown; revision?: unknown };
  page.off('request', onRequest);
  expect(body.id).toBe(id);
  expect(typeof body.revision, 'в теле delete есть ревизия').toBe('number');
  await expect(dialog).toHaveCount(0, { timeout: T.action });
  await expect(card).toHaveCount(0, { timeout: T.action });
  expect((await listSources(page)).some((s) => s.id === id)).toBe(false);

  await guard.assertClean();
});

test('modals: удаление подписки — ошибка записи даёт тост, диалог закрыт', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await addToDraft(page, BAD_SUB);
  const id = await subId(page);

  await openNodes(page);
  const card = page.getByTestId(`nodes-source-${id}`);
  await expect(card).toBeVisible({ timeout: T.action });
  await page.getByTestId(`nodes-delete-${id}`).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible({ timeout: T.action });

  // Источник убран из черновика другим путём (настоящий запрос): запись из подтверждения
  // получит отказ сервера «источник не найден» — реальная ошибка записи. Обрыв связи для
  // этого не годится: при недоступной панели тосты ошибок подавляются баннером.
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const gone = await api(page, 'POST', '/api/configlayer/sources/delete', {
    revision: Number(view.data?.draft_revision ?? 0),
    id
  });
  expect(gone.status, 'удаление другим путём').toBe(200);
  await expect(dialog).toBeVisible();

  await dialog.locator('.btn-danger').click();
  // диалог закрывается сразу, итог ошибки — тост
  await expect(dialog).toHaveCount(0, { timeout: T.action });
  await expect(page.locator('.toast--error').first()).toBeVisible({ timeout: T.action });
  // карточка при ошибке записи не убирается: исчезает только после успешного ответа
  await expect(card).toBeVisible();

  await guard.assertClean();
});

test('modals: удаление узла — подтверждение и POST nodes/delete с ревизией и entry_id', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  const port = randomPort();
  await addToDraft(
    page,
    [nodeLink('vless_ws.txt', port, 'DelA'), nodeLink('vless_ws.txt', port + 1, 'DelB')].join('\n')
  );

  await openNodes(page);
  await expandCard(page, 'my');
  const rowA = page
    .locator('.node-row')
    .filter({ hasText: `:${port}` })
    .first();
  const rowB = page
    .locator('.node-row')
    .filter({ hasText: `:${port + 1}` })
    .first();
  await expect(rowA).toBeVisible({ timeout: T.action });
  await expect(rowB).toBeVisible();

  await rowA.locator('[data-testid^="nodes-delete-"]').click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible({ timeout: T.action });
  await expect(dialog.getByRole('heading')).toContainText('DelA');
  const req = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().endsWith('/api/configlayer/sources/nodes/delete')
  );
  await dialog.locator('.btn-danger').click();
  const body = (await req).postDataJSON() as { revision?: unknown; entry_id?: unknown };
  expect(typeof body.revision, 'в теле delete есть ревизия').toBe('number');
  expect(typeof body.entry_id, 'в теле delete есть entry_id').toBe('string');
  await expect(dialog).toHaveCount(0, { timeout: T.action });
  await expect(page.locator('.node-row').filter({ hasText: `:${port}` })).toHaveCount(0, {
    timeout: T.action
  });
  await expect(page.locator('.node-row').filter({ hasText: `:${port + 1}` })).toHaveCount(1);
  // у узлов подписки кнопок «Изменить» и «Удалить» нет: только у «Моих узлов»
  await expect(
    page.getByTestId('nodes-source-my').locator('[data-testid^="nodes-edit-"]')
  ).toHaveCount(1);

  await guard.assertClean();
});

test('modals: «Сбросить» черновик на вкладке «Узлы» сразу убирает добавленные источник и узел из списка', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  const port = randomPort();
  await addToDraft(page, [BAD_SUB, nodeLink('vless_ws.txt', port, 'ResetA')].join('\n'));
  const id = await subId(page);

  await openNodes(page);
  await expect(page.getByTestId(`nodes-source-${id}`)).toBeVisible({ timeout: T.action });
  await expandCard(page, 'my');
  const row = page.locator('.node-row').filter({ hasText: `:${port}` });
  await expect(row).toHaveCount(1, { timeout: T.action });

  // сброс из полосы черновика: подтверждение, затем POST draft/reset
  const req = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().endsWith('/api/configlayer/draft/reset')
  );
  await page.getByTestId('config-draftbar').locator('.draft-bar-actions button').first().click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible({ timeout: T.action });
  await dialog.locator('.confirm-actions button').last().click();
  await req;
  await expect(dialog).toHaveCount(0, { timeout: T.action });

  // список обновился без перезагрузки страницы: источник и узел черновика исчезли
  await expect(page.getByTestId(`nodes-source-${id}`)).toHaveCount(0, { timeout: T.action });
  await expect(row).toHaveCount(0, { timeout: T.action });
  expect((await listSources(page)).some((s) => s.id === id)).toBe(false);

  await guard.assertClean();
});

/** Запускает применение и ждёт его конца: идёт ли применение, видно в снимке слоя. */
async function applyAndWait(page: Page): Promise<void> {
  const start = await api(page, 'POST', '/api/configlayer/apply', {});
  expect(start.status, 'запуск применения').toBe(200);
  await expect
    .poll(
      async () => {
        const st = await api(page, 'GET', '/api/configlayer/state');
        const apply = (st.data?.apply ?? {}) as { running?: boolean };
        return apply.running === false && Number(st.data?.draft_changes ?? -1) === 0;
      },
      { timeout: T.save * 3, intervals: [2000] }
    )
    .toBe(true);
}

test('notices: сбой связи у нового источника — одно уведомление без упоминания рабочего снимка', async ({
  page
}) => {
  test.setTimeout(T.save * 8);
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await addToDraft(page, BAD_SUB);
  const id = await subId(page);
  try {
    await applyAndWait(page);
    await page.goto('/#/config/nodes', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
    const notice = page.locator(`[data-notice^="source_network:${id}:"]`);
    // уведомление по ядру приходит с сервера отдельно для каждого установленного ядра,
    // а на экране оно одно: в тексте ядра нет
    await expect(notice).toHaveCount(1, { timeout: T.action });
    // у источника, который ни разу не отвечал, рабочего снимка нет
    await expect(notice).not.toContainText(/снимок|snapshot/i);
  } finally {
    // возврат стенда: источник убран из применённого состояния
    const view = await api(page, 'GET', '/api/configlayer/sources');
    await api(page, 'POST', '/api/configlayer/sources/delete', {
      revision: Number(view.data?.draft_revision ?? 0),
      id
    });
    await applyAndWait(page);
  }
  await guard.assertClean();
});

/** Критичные и серьёзные нарушения axe внутри открытого диалога. */
async function dialogViolations(page: Page): Promise<string[]> {
  // модаль появляется с затуханием: axe не должен видеть полупрозрачный кадр
  await page.waitForTimeout(500);
  const scan = await new AxeBuilder({ page })
    .include('[role="dialog"]')
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  return scan.violations
    .filter((v) => v.impact === 'critical' || v.impact === 'serious')
    .map(
      (v) =>
        `${v.id} (${v.impact}): ${v.nodes
          .slice(0, 3)
          .map((n) => n.target.join(' '))
          .join(' | ')}`
    );
}

test('modals: Esc закрывает, фокус заперт и возвращается на вызвавшую кнопку, axe без нарушений', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  const port = randomPort();
  await addToDraft(page, [BAD_SUB, nodeLink('vless_ws.txt', port, 'FocusA')].join('\n'));
  const id = await subId(page);

  await openNodes(page);
  await expandCard(page, 'my');
  const row = page
    .locator('.node-row')
    .filter({ hasText: `:${port}` })
    .first();
  const editBtn = row.locator('[data-testid^="nodes-edit-"]');

  const cases: { name: string; opener: Locator; modal: string; ready: Locator }[] = [
    {
      name: 'настройки',
      opener: page.getByTestId(`nodes-settings-${id}`),
      modal: 'nodes-settings-modal',
      ready: page.getByTestId('nodes-settings-exclude')
    },
    {
      name: 'диагностика',
      opener: page.getByTestId(`nodes-diag-${id}`),
      modal: 'nodes-diag-modal',
      ready: page.getByTestId('nodes-diag-kernel')
    },
    {
      name: 'правка узла',
      opener: editBtn,
      modal: 'nodes-edit-modal',
      ready: page.getByTestId('nodes-edit-text')
    }
  ];

  const problems: string[] = [];
  for (const c of cases) {
    await c.opener.focus();
    await c.opener.click();
    const modal = page.getByTestId(c.modal);
    await expect(modal, c.name).toBeVisible({ timeout: T.action });
    await expect(c.ready, c.name).toBeVisible({ timeout: T.action });

    // при открытии фокус уходит в диалог
    await expect
      .poll(async () => (await focusState(page, c.modal)).inside, {
        message: `${c.name}: начальный фокус в диалоге`,
        timeout: T.action
      })
      .toBe(true);

    // Tab, пришедший пока фокус ещё вне диалога (на вызвавшей кнопке, раньше начального
    // фокуса), не должен уводить его на страницу под модалью: фокус уводится наружу
    // намеренно, чтобы окно гонки воспроизводилось детерминированно.
    for (const key of ['Tab', 'Shift+Tab']) {
      await c.opener.focus();
      const before = await focusState(page, c.modal);
      if (!before.dialog) {
        problems.push(`${c.name}: диалог закрылся при возврате фокуса на кнопку`);
        break;
      }
      await page.keyboard.press(key);
      const after = await focusState(page, c.modal);
      if (!after.dialog) problems.push(`${c.name}: диалог закрылся после ${key} с кнопки`);
      else if (!after.inside)
        problems.push(`${c.name}: фокус вне диалога на ${after.active} после ${key} с кнопки`);
    }
    // дальше Escape и обход Tab имеют смысл только при фокусе в диалоге
    expect(problems, problems.join('\n')).toEqual([]);

    // фокус заперт: Tab по кругу не выводит его из диалога
    for (let i = 0; i < 14; i++) {
      await page.keyboard.press('Tab');
      const inside = await page.evaluate(
        (sel) => document.querySelector(sel)?.contains(document.activeElement) ?? false,
        `[data-testid="${c.modal}"]`
      );
      if (!inside) {
        problems.push(`${c.name}: фокус вышел из диалога после ${i + 1} Tab`);
        break;
      }
    }

    for (const v of await dialogViolations(page)) problems.push(`${c.name}: axe ${v}`);

    await page.keyboard.press('Escape');
    await expect(modal, c.name).toHaveCount(0, { timeout: T.action });
    // фокус вернулся на вызвавшую кнопку (если её строка не пересоздана)
    if (c.name !== 'правка узла') await expect(c.opener, c.name).toBeFocused();
  }
  expect(problems, problems.join('\n')).toEqual([]);

  await guard.assertClean();
});

for (const theme of ['dark', 'light'] as const) {
  test(`modals: 390 px, тема ${theme} — модали без горизонтальной прокрутки и в пределах экрана`, async ({
    page
  }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await startPage(page);
    const port = randomPort();
    await addToDraft(
      page,
      [LONG_SUB, nodeLink('vless_ws.txt', port, 'NarrowLongNameNarrowLongNameNarrowLongName')].join(
        '\n'
      )
    );
    const id = await subId(page);

    await openNodes(page, 390);
    await expandCard(page, 'my');

    const fits = async (name: string) => {
      const m = await page.evaluate(() => {
        const d = document.querySelector('[role="dialog"]') as HTMLElement | null;
        const r = d?.getBoundingClientRect();
        return {
          page: document.documentElement.scrollWidth,
          inner: window.innerWidth,
          dialogScroll: d ? d.scrollWidth - d.clientWidth : 0,
          left: r?.left ?? 0,
          right: r?.right ?? 0
        };
      });
      expect(m.page, `${name}: страница шире окна`).toBeLessThanOrEqual(m.inner + 1);
      expect(m.dialogScroll, `${name}: диалог прокручивается по горизонтали`).toBeLessThanOrEqual(
        1
      );
      expect(m.left, `${name}: диалог левее экрана`).toBeGreaterThanOrEqual(-1);
      expect(m.right, `${name}: диалог правее экрана`).toBeLessThanOrEqual(m.inner + 1);
    };

    // настройки: полный длинный адрес и длинный HWID переносятся
    await page.getByTestId(`nodes-settings-${id}`).click();
    const settings = page.getByTestId('nodes-settings-modal');
    await expect(settings).toBeVisible({ timeout: T.action });
    await page.getByTestId('nodes-settings-url-reveal').click();
    await expect(page.getByTestId('nodes-settings-url-input')).toBeVisible({ timeout: T.action });
    await advanced(settings).locator('summary').click();
    await settings.locator('input[id$="-hwid"]').fill('H'.repeat(120));
    await fits('настройки');
    await page.keyboard.press('Escape');
    await expect(settings).toHaveCount(0);

    // диагностика
    await page.getByTestId(`nodes-diag-${id}`).click();
    await expect(page.getByTestId('nodes-diag-modal')).toBeVisible({ timeout: T.action });
    await diagSettled(page);
    await fits('диагностика');
    await page.keyboard.press('Escape');
    await expect(page.getByTestId('nodes-diag-modal')).toHaveCount(0);

    // правка узла: длинная ссылка без пробелов переносится внутри поля
    await page
      .locator('.node-row')
      .filter({ hasText: `:${port}` })
      .first()
      .locator('[data-testid^="nodes-edit-"]')
      .click();
    await expect(page.getByTestId('nodes-edit-text')).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('nodes-edit-save')).toBeEnabled({ timeout: T.action });
    await fits('правка узла');

    await guard.assertClean();
  });
}
