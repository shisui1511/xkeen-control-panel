// e2e-pages: config/lists
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, withPanel } from './lib/nodes-api';

// Вкладка «Списки» раздела «Конфигурация» (менеджер geodata XKeen, фаза 146.1, план 09):
// открытие кликом, перенаправление со старого адреса #/dat при включённом флаге слоя и
// прежнее поведение при выключенном. Ответы сети не подменяются. Флаг слоя включается на
// время прогона и возвращается прежним. Ядро не читается и не меняется.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

async function openConfig(page: Page, hash: string): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
}

/** Включает или выключает флаг слоя на время одного сценария (прежнее состояние вернёт layer.restore). */
async function setLayerFlag(browser: Browser, enabled: boolean): Promise<void> {
  await withPanel(browser, async (page) => {
    const res = await api(page, 'POST', '/api/settings/config-layer', { enabled });
    expect(res.status, enabled ? 'включение слоя' : 'выключение слоя').toBe(200);
  });
}

/** Вкладка загружена: она видна и скелетона больше нет. */
async function tabReady(page: Page, testId: string): Promise<void> {
  const tab = page.getByTestId(testId);
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
}

test('lists: вкладка открывается кликом', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openConfig(page, '#/config');

  await page.getByTestId('config-tab-lists').click();
  await expect(page).toHaveURL(/#\/config\/lists$/);
  await expect(page.getByTestId('config-tab-lists')).toHaveAttribute('aria-selected', 'true');
  await tabReady(page, 'lists-tab');
  await expect(page.getByTestId('nodes-tab')).toHaveCount(0);

  // вкладка стоит между «Правила» и «Файлы панели»
  const order = await page
    .getByTestId('config-tabs')
    .locator('[data-testid^="config-tab-"]')
    .evaluateAll((els) => els.map((el) => el.getAttribute('data-testid')));
  expect(order).toEqual([
    'config-tab-nodes',
    'config-tab-groups',
    'config-tab-rules',
    'config-tab-lists',
    'config-tab-files'
  ]);

  // строка сводки совпадает с числом файлов в снимке менеджера
  const view = await api(page, 'GET', '/api/configlayer/geodata');
  expect(view.status, 'GET /api/configlayer/geodata').toBe(200);
  const files = Number(view.data?.files);
  expect(Number.isFinite(files), 'в снимке есть число файлов').toBe(true);
  await expect(page.getByTestId('geo-summary')).toHaveText(
    new RegExp(`(Файлов|Files): ${files}(?!\\d)`),
    { timeout: T.action }
  );

  await guard.assertClean();
});

test('lists: #/dat ведёт на вкладку при флаге', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/#/dat', { timeout: T.page, waitUntil: 'domcontentloaded' });

  await expect(page).toHaveURL(/#\/config\/lists$/, { timeout: T.page });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  await tabReady(page, 'lists-tab');
  await expect(page.getByTestId('config-tab-lists')).toHaveAttribute('aria-selected', 'true');
  // пункта меню старой страницы нет
  await expect(page.locator('a[href="#/dat"]')).toHaveCount(0);

  await guard.assertClean();
});

/** Источник менеджера из снимка: файл на диске, вид geosite или geoip. */
interface GeoSource {
  file: string;
  kind: string;
  present: boolean;
}

async function presentSources(page: Page): Promise<GeoSource[]> {
  const view = await api(page, 'GET', '/api/configlayer/geodata');
  expect(view.status, 'GET /api/configlayer/geodata').toBe(200);
  const sources = (view.data?.sources ?? []) as GeoSource[];
  return sources.filter((s) => s.present);
}

interface TagRow {
  tag: string;
  count: number;
}

async function tagsOf(page: Page, file: string): Promise<TagRow[]> {
  const res = await api(
    page,
    'GET',
    `/api/configlayer/geodata/tags?${new URLSearchParams({ file }).toString()}`
  );
  expect(res.status, 'GET /api/configlayer/geodata/tags').toBe(200);
  return (res.data?.tags ?? []) as TagRow[];
}

async function entriesOf(
  page: Page,
  file: string,
  tag: string,
  limit: number
): Promise<{ total: number; entries: string[] }> {
  const q = new URLSearchParams({ file, tag, q: '', offset: '0', limit: String(limit) });
  const res = await api(page, 'GET', `/api/configlayer/geodata/entries?${q.toString()}`);
  expect(res.status, 'GET /api/configlayer/geodata/entries').toBe(200);
  return {
    total: Number(res.data?.total ?? 0),
    entries: (res.data?.entries ?? []) as string[]
  };
}

/** Число изменений черновика слоя: диалог правила без «Сохранить» его не меняет. */
async function draftChanges(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_changes ?? 0);
}

/** Тег с наибольшим числом записей: у него записи не умещаются в одну страницу. */
function biggestTag(tags: TagRow[]): TagRow | undefined {
  return tags.reduce<TagRow | undefined>(
    (best, x) => (best === undefined || x.count > best.count ? x : best),
    undefined
  );
}

test('lists: инспектор', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openConfig(page, '#/config/lists');
  await tabReady(page, 'lists-tab');

  const sources = await presentSources(page);
  test.skip(sources.length === 0, 'на стенде нет ни одного файла баз');
  const file = sources[0].file;
  const big = biggestTag(await tagsOf(page, file));
  test.skip(big === undefined, 'в первом файле баз нет тегов');
  const tag = big?.tag ?? '';
  const draftBefore = await draftChanges(page);

  // ступень «Теги»: настоящие теги файла с роутера
  await page.getByTestId(`geo-source-open-${file}`).click();
  const inspector = page.getByTestId('geo-inspector');
  await expect(inspector).toBeVisible({ timeout: T.action });
  const rows = inspector.getByTestId('geo-tag-list').locator('li');
  await expect(rows.first()).toBeVisible({ timeout: T.page });

  // поиск тега: в списке не больше 300 строк, нужный тег находится запросом
  await inspector.getByTestId('geo-tag-search').fill(tag);
  await expect(inspector.getByTestId(`geo-tag-${tag}`)).toBeVisible({ timeout: T.action });

  // ступень «Записи»: не больше 100 строк, «Показать ещё записи» добавляет следующие
  await inspector.getByTestId(`geo-tag-${tag}`).getByTestId('geo-open-entries').click();
  await expect(inspector.getByTestId('geo-entry-search')).toBeVisible({ timeout: T.action });
  const entryRows = inspector.getByTestId('geo-entry');
  await expect(entryRows.first()).toBeVisible({ timeout: T.page });
  const { total } = await entriesOf(page, file, tag, 1);
  expect(await entryRows.count(), 'первая страница записей').toBeLessThanOrEqual(100);
  if (total > 100) {
    const more = inspector.getByTestId('geo-entry-more');
    await expect(more).toBeVisible({ timeout: T.action });
    await more.click();
    await expect(entryRows).toHaveCount(Math.min(total, 200), { timeout: T.page });
  }

  // «Назад к тегам»: список тегов на месте
  await inspector.getByTestId('geo-inspector-back').click();
  await expect(inspector.getByTestId('geo-tag-list')).toBeVisible({ timeout: T.action });

  // «Создать правило в черновике» закрывает инспектор и открывает диалог правила со списком
  await inspector.getByTestId(`geo-tag-${tag}`).getByTestId('geo-make-rule').click();
  await expect(inspector).toHaveCount(0, { timeout: T.action });
  const ruleModal = page.getByTestId('rule-modal');
  await expect(ruleModal).toBeVisible({ timeout: T.action });
  await expect(ruleModal.getByTestId('rule-cond-lists-0')).toContainText(`${file} · ${tag}`, {
    timeout: T.action
  });
  await ruleModal.getByTestId('rule-cancel').click();
  await expect(ruleModal).toHaveCount(0, { timeout: T.action });
  expect(await draftChanges(page), 'черновик не изменился').toBe(draftBefore);

  await guard.assertClean();
});

test.describe('флаг выключен', () => {
  // флаг возвращается включённым для остальных сценариев; исходное состояние вернёт afterAll
  test.afterEach(async ({ browser }) => {
    await setLayerFlag(browser, true);
  });

  test('lists: с выключенным флагом всё как раньше', async ({ page, browser }) => {
    await setLayerFlag(browser, false);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });

    // старая страница DAT на месте, пункт меню есть
    await page.goto('/#/dat', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
      timeout: T.page
    });
    await expect(page).toHaveURL(/#\/dat$/);
    await expect(page.locator('a[href="#/dat"]').first()).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('lists-tab')).toHaveCount(0);

    // раздела «Конфигурация» нет: адрес вкладки уводит на обзор по общему правилу раздела
    await page.goto('/#/config/lists', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page).toHaveURL(/#\/dashboard$/, { timeout: T.page });
    await expect(page.getByTestId('lists-tab')).toHaveCount(0);

    await guard.assertClean();
  });
});
