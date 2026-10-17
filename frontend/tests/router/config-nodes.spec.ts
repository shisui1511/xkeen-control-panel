// e2e-pages: config config/nodes config/files
import { randomInt } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';

// Каркас раздела «Конфигурация» на настоящей панели (фаза 145, план 12): вкладки «Узлы»
// и «Файлы панели» с маршрутами #/config/nodes и #/config/files, общая полоса черновика,
// бейдж меню, полоса вкладок на 390 px. Ответы сети не подменяются: черновик создаёт
// настоящий маршрут добавления узла (данные — ссылка из internal/nodes/testdata/links).
// Флаг слоя включается на время прогона и возвращается прежним; черновик сбрасывается.
// В вывод не попадают имена узлов и содержимое ссылок.

const LINK = fileURLToPath(
  new URL('../../../internal/nodes/testdata/links/socks.txt', import.meta.url)
);

/**
 * Ссылка узла из тестовых данных с уникальным портом: предпросмотр не пометит её как «уже
 * есть», даже если такой же узел уже лежит в «Моих узлах» стенда.
 */
function uniqueLink(): string {
  const port = randomInt(20000, 60000);
  return readFileSync(LINK, 'utf8')
    .trim()
    .replace(/(@[^:/?#]+:)\d+/, `$1${port}`);
}

interface ApiResult {
  status: number;
  data: Record<string, unknown> | null;
}

/** Запрос к API панели из страницы с её сессией и токеном CSRF; тело ответа — конверт data. */
async function api(page: Page, method: string, path: string, body?: unknown): Promise<ApiResult> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(path, {
        method,
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': localStorage.getItem('csrf_token') ?? ''
        },
        body: body === undefined ? undefined : JSON.stringify(body)
      });
      let data: Record<string, unknown> | null = null;
      try {
        const payload = await res.json();
        data = payload && typeof payload === 'object' ? (payload.data ?? payload) : null;
      } catch {
        data = null;
      }
      return { status: res.status, data };
    },
    { method, path, body }
  );
}

async function withPanel<T>(browser: Browser, fn: (page: Page) => Promise<T>): Promise<T> {
  const context = await browser.newContext({
    baseURL: RT.url,
    ignoreHTTPSErrors: true,
    storageState: process.env.XCP_PW_STATE
  });
  try {
    const page = await context.newPage();
    await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
    return await fn(page);
  } finally {
    await context.close();
  }
}

async function resetDraft(page: Page): Promise<void> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const revision = Number(view.data?.draft_revision ?? 0);
  const res = await api(page, 'POST', '/api/configlayer/draft/reset', { revision });
  expect(res.status, 'сброс черновика').toBe(200);
}

async function sourcesCount(page: Page): Promise<number> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  expect(view.status, 'GET /api/configlayer/sources').toBe(200);
  const list = view.data?.sources;
  return Array.isArray(list) ? list.length : 0;
}

/** Кладёт в черновик один узел «Моих узлов» настоящим маршрутом добавления. */
async function addDraftNode(page: Page): Promise<string> {
  const text = uniqueLink();
  const preview = await api(page, 'POST', '/api/configlayer/sources/preview', { text, files: [] });
  expect(preview.status, 'предпросмотр узла').toBe(200);
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const add = await api(page, 'POST', '/api/configlayer/sources/add', {
    revision: Number(view.data?.draft_revision ?? 0),
    token: preview.data?.token
  });
  expect(add.status, 'добавление узла в черновик').toBe(200);
  return text;
}

let layerWasOn = false;

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await withPanel(browser, async (page) => {
    const settings = await api(page, 'GET', '/api/settings');
    expect(settings.status, 'GET /api/settings').toBe(200);
    layerWasOn = settings.data?.config_layer === true;
    if (!layerWasOn) {
      const res = await api(page, 'POST', '/api/settings/config-layer', { enabled: true });
      expect(res.status, 'включение слоя').toBe(200);
    }
    await resetDraft(page);
  });
});

test.afterAll(async ({ browser }) => {
  await withPanel(browser, async (page) => {
    await resetDraft(page);
    if (!layerWasOn) {
      const res = await api(page, 'POST', '/api/settings/config-layer', { enabled: false });
      expect(res.status, 'возврат флага слоя').toBe(200);
    }
  });
});

async function openConfig(page: Page, hash: string): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
}

test('config-tabs: #/config открывает Узлы, вкладки переключают маршрут', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  const total = await sourcesCount(page);

  await openConfig(page, '#/config');
  await expect(page.getByTestId('config-tab-nodes')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByTestId('config-tab-files')).toHaveAttribute('aria-selected', 'false');
  const nodesTab = page.getByTestId('nodes-tab');
  await expect(nodesTab).toBeVisible({ timeout: T.action });
  // загрузка закончилась: скелетона нет, строки ошибки загрузки нет
  await expect(nodesTab.locator('.skeleton')).toHaveCount(0, { timeout: T.action });
  await expect(page.getByTestId('nodes-load-error')).toHaveCount(0);
  // пусто — пустое состояние; на стенде с источниками его быть не должно
  const emptyState = nodesTab.locator('.empty-state');
  if (total === 0) {
    await expect(emptyState).toHaveCount(1);
  } else {
    await expect(emptyState).toHaveCount(0);
  }
  await expect(page.getByTestId('config-files')).toHaveCount(0);

  await page.getByTestId('config-tab-files').click();
  await expect(page).toHaveURL(/#\/config\/files$/);
  await expect(page.getByTestId('config-files')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('config-tab-files')).toHaveAttribute('aria-selected', 'true');
  await expect(nodesTab).toHaveCount(0);

  await page.getByTestId('config-tab-nodes').click();
  await expect(page).toHaveURL(/#\/config\/nodes$/);
  await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('config-files')).toHaveCount(0);

  await guard.assertClean();
});

test('config-tabs: #/config/files напрямую открывает Файлы панели', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openConfig(page, '#/config/files');
  await page.reload({ timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('config-tab-files')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByTestId('config-files')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('nodes-tab')).toHaveCount(0);
  await guard.assertClean();
});

test('config-tabs: полоса черновика общая, смена вкладки её не пересоздаёт, бейдж меню считает', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await addDraftNode(page);

  await openConfig(page, '#/config/nodes');
  const bar = page.getByTestId('config-draftbar');
  await expect(bar).toBeVisible({ timeout: T.action });
  // метка на элементе полосы: если вкладки пересоздали бы её, метка пропала бы
  await bar.evaluate((el) => {
    (el as HTMLElement & { __kept?: boolean }).__kept = true;
  });

  await page.getByTestId('config-tab-files').click();
  await expect(page.getByTestId('config-files')).toBeVisible({ timeout: T.action });
  await expect(bar).toBeVisible();
  expect(await bar.evaluate((el) => (el as HTMLElement & { __kept?: boolean }).__kept)).toBe(true);

  await page.getByTestId('config-tab-nodes').click();
  await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.action });
  expect(await bar.evaluate((el) => (el as HTMLElement & { __kept?: boolean }).__kept)).toBe(true);

  // бейдж меню = изменения черновика + видимые уведомления слоя
  const state = await api(page, 'GET', '/api/configlayer/state');
  const changes = Number(state.data?.draft_changes ?? 0);
  expect(changes, 'в черновике есть изменение').toBeGreaterThan(0);
  const notices = await page.locator('[data-testid="config-notices"] [data-notice]').count();
  const badge = page.locator('.nav-badge-config');
  await expect(badge).toBeVisible({ timeout: T.action });
  const expected = changes + notices;
  await expect(badge).toHaveText(expected > 99 ? '99+' : String(expected));

  await guard.assertClean();
});

test('config-tabs: на 390 px полоса вкладок прокручивается внутри себя', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 390, height: 844 });
  for (const hash of ['#/config/nodes', '#/config/files']) {
    await openConfig(page, hash);
    const strip = page.getByTestId('config-tabs');
    await expect(strip).toBeVisible({ timeout: T.action });

    const metrics = await page.evaluate(() => {
      const el = document.querySelector('[data-testid="config-tabs"] .tabs') as HTMLElement | null;
      const btns = [...document.querySelectorAll('[data-testid="config-tabs"] .tab-btn')];
      const root = document.documentElement;
      return {
        overflowX: el ? getComputedStyle(el).overflowX : '',
        scrollbarWidth: el ? getComputedStyle(el).scrollbarWidth : '',
        wrap: el ? getComputedStyle(el).flexWrap : '',
        nowrap: btns.every((b) => getComputedStyle(b).whiteSpace === 'nowrap'),
        oneLine: btns.every((b) => (b as HTMLElement).offsetHeight < 60),
        pageScroll: root.scrollWidth - root.clientWidth
      };
    });
    expect(metrics.overflowX, `${hash}: прокрутка полосы`).toBe('auto');
    expect(metrics.scrollbarWidth, `${hash}: скроллбар полосы скрыт`).toBe('none');
    expect(metrics.wrap, `${hash}: вкладки не переносятся`).toBe('nowrap');
    expect(metrics.nowrap, `${hash}: подписи в одну строку`).toBe(true);
    expect(metrics.oneLine, `${hash}: подписи не разрослись по высоте`).toBe(true);
    expect(metrics.pageScroll, `${hash}: горизонтальная прокрутка страницы`).toBeLessThanOrEqual(0);
  }
  await guard.assertClean();
});

test('config-add: вставка ссылки → предпросмотр → Добавить → поле пусто и в фокусе', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await resetDraft(page);
  const before = Number(
    (await api(page, 'GET', '/api/configlayer/state')).data?.draft_changes ?? 0
  );

  await openConfig(page, '#/config/nodes');
  const card = page.getByTestId('nodes-add');
  await expect(card).toBeVisible({ timeout: T.action });
  const input = page.locator('#nodes-add-input');
  const submit = page.getByTestId('nodes-add-submit');
  // пустое поле: предпросмотра нет, «Добавить» неактивна
  await expect(page.getByTestId('nodes-preview')).toHaveCount(0);
  await expect(submit).toBeDisabled();

  await input.fill(uniqueLink());
  // предпросмотр ничего не пишет в черновик: строка «Мои узлы» и активная кнопка
  await expect(page.getByTestId('nodes-preview-my')).toBeVisible({ timeout: T.action });
  await expect(submit).toBeEnabled();
  const mid = Number((await api(page, 'GET', '/api/configlayer/state')).data?.draft_changes ?? 0);
  expect(mid, 'предпросмотр не меняет черновик').toBe(before);

  const added = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().endsWith('/api/configlayer/sources/add')
  );
  await submit.click();
  const body = (await added).postDataJSON() as { revision?: unknown; token?: unknown };
  expect(typeof body.revision, 'в теле add есть ревизия').toBe('number');
  expect(typeof body.token === 'string' && body.token !== '', 'в теле add есть токен').toBe(true);

  await expect(input).toHaveValue('', { timeout: T.action });
  await expect(input).toBeFocused();
  await expect(page.getByTestId('nodes-preview')).toHaveCount(0);
  await expect(page.getByTestId('nodes-add-announce')).toContainText(/\S/);
  const after = Number((await api(page, 'GET', '/api/configlayer/state')).data?.draft_changes ?? 0);
  expect(after, 'узел попал в черновик').toBeGreaterThan(before);

  await guard.assertClean();
});

// --- Поле «Добавить»: состояния предпросмотра, файлы, перетаскивание (план 13) ---

const PREVIEW_URL = '/api/configlayer/sources/preview';
// Адрес в зарезервированной зоне .invalid: имя не разрешается нигде, провайдер не затрагивается
const BAD_SUB = 'https://sub.invalid/feed';

function isPreview(r: { method(): string; url(): string }): boolean {
  return r.method() === 'POST' && r.url().endsWith(PREVIEW_URL);
}

async function openAddCard(page: Page, width = 1440): Promise<void> {
  await page.setViewportSize({ width, height: width > 600 ? 900 : 844 });
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await openConfig(page, '#/config/nodes');
  await expect(page.getByTestId('nodes-add')).toBeVisible({ timeout: T.action });
}

test('config-add: подписка с ошибкой первого запроса — строка подписки, причина из трёх частей, Добавить доступна', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await openAddCard(page);
  const input = page.locator('#nodes-add-input');
  const submit = page.getByTestId('nodes-add-submit');

  await input.fill(BAD_SUB);
  // идёт распознавание: индикатор в регионе aria-live, «Добавить» неактивна
  const region = page.getByTestId('nodes-preview');
  await expect(region).toHaveAttribute('aria-live', 'polite');
  await expect(page.getByTestId('nodes-preview-loading')).toBeVisible({ timeout: T.action });
  await expect(submit).toBeDisabled();

  const row = page.getByTestId('nodes-preview-sub');
  await expect(row).toBeVisible({ timeout: T.save });
  await expect(page.getByTestId('nodes-preview-loading')).toHaveCount(0);
  // ошибка подписки — заголовок, совет и свёрнутые «Подробности» с кодом
  const err = page.getByTestId('nodes-preview-sub-error');
  await expect(err).toBeVisible();
  await expect(err.locator('strong')).toHaveText(/\S/);
  const details = err.locator('details');
  await expect(details).toHaveCount(1);
  expect(await details.evaluate((el) => (el as HTMLDetailsElement).open)).toBe(false);
  await details.locator('summary').click();
  await expect(details.locator('li').first()).toContainText(/\S/);
  // подписку можно добавить и при ошибке (D-18); сам адрес в строке замаскирован
  await expect(submit).toBeEnabled();
  await expect(row).not.toContainText(BAD_SUB);

  await guard.assertClean();
});

test('config-add: смешанная вставка — подписка, «Мои узлы», нераспознанная строка с номером', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await openAddCard(page);
  const input = page.locator('#nodes-add-input');

  await input.fill(`${BAD_SUB}\n${uniqueLink()}\nhello world`);
  const sub = page.getByTestId('nodes-preview-sub');
  const my = page.getByTestId('nodes-preview-my');
  const bad = page.getByTestId('nodes-preview-errors');
  await expect(bad).toBeVisible({ timeout: T.save });
  await expect(sub).toBeVisible();
  await expect(my).toBeVisible();
  // порядок строк: подписки, затем «Мои узлы», затем нераспознанные
  const [ySub, yMy, yBad] = await Promise.all(
    [sub, my, bad].map(async (l) => (await l.boundingBox())?.y ?? -1)
  );
  expect(ySub).toBeGreaterThan(0);
  expect(yMy).toBeGreaterThan(ySub);
  expect(yBad).toBeGreaterThan(yMy);
  // у нераспознанной строки виден её номер (третья строка ввода)
  const lineErr = page.getByTestId('nodes-preview-line-error');
  await expect(lineErr).toHaveCount(1);
  await expect(lineErr).toContainText('3');
  await expect(page.getByTestId('nodes-add-submit')).toBeEnabled();

  // список узлов свёрнут и раскрывается кнопкой с aria-expanded
  const toggle = page.getByTestId('nodes-preview-my-toggle');
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByTestId('nodes-preview-nodes')).toHaveCount(0);
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByTestId('nodes-preview-nodes')).toBeVisible();

  await guard.assertClean();
});

test('config-add: ничего не распознано — ошибка с «Подробностями», Добавить неактивна', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await openAddCard(page);
  await page.locator('#nodes-add-input').fill('hello world');

  const err = page.getByTestId('nodes-preview-error');
  await expect(err).toBeVisible({ timeout: T.action });
  await expect(err.locator('strong')).toHaveText(/\S/);
  await expect(page.getByTestId('nodes-add-submit')).toBeDisabled();
  // пустой рамки нет: ни строк подписок, ни «Моих узлов»
  await expect(page.getByTestId('nodes-preview-sub')).toHaveCount(0);
  await expect(page.getByTestId('nodes-preview-my')).toHaveCount(0);
  const details = err.locator('details');
  await details.locator('summary').click();
  await expect(details).toContainText('unrecognized');

  // «Очистить» убирает ввод и предпросмотр, фокус возвращается в поле
  await page.getByTestId('nodes-add-clear').click();
  await expect(page.locator('#nodes-add-input')).toHaveValue('');
  await expect(page.locator('#nodes-add-input')).toBeFocused();
  await expect(page.getByTestId('nodes-preview')).toHaveCount(0);

  await guard.assertClean();
});

test('config-add: выбор файла — предпросмотр получает файл с именем и содержимым', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await openAddCard(page);
  const link = uniqueLink();
  const sent = page.waitForRequest(isPreview);
  await page.getByTestId('nodes-add-file').setInputFiles({
    name: 'nodes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from(link)
  });
  const body = (await sent).postDataJSON() as {
    text: string;
    files: { name: string; content: string }[];
  };
  expect(body.files).toEqual([{ name: 'nodes.txt', content: link }]);
  await expect(page.getByTestId('nodes-preview-my')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('nodes-add-submit')).toBeEnabled();

  // файл снимается чипом — ввод и предпросмотр исчезают
  await page.getByTestId('nodes-add-files').getByRole('button').click();
  await expect(page.getByTestId('nodes-add-files')).toHaveCount(0);
  await expect(page.getByTestId('nodes-preview')).toHaveCount(0);

  await guard.assertClean();
});

test('config-add: файл больше 1 МиБ отклоняется на клиенте без запроса', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await openAddCard(page);
  let requests = 0;
  page.on('request', (r) => {
    if (isPreview(r)) requests++;
  });
  await page.getByTestId('nodes-add-file').setInputFiles({
    name: 'big.txt',
    mimeType: 'text/plain',
    buffer: Buffer.alloc(1024 * 1024 + 1, 'a')
  });
  const err = page.getByTestId('nodes-preview-error');
  await expect(err).toBeVisible({ timeout: T.action });
  await err.locator('details summary').click();
  await expect(err.locator('details')).toContainText('too_large');
  await expect(page.getByTestId('nodes-add-submit')).toBeDisabled();
  await expect(page.getByTestId('nodes-add-files')).toHaveCount(0);
  expect(requests, 'запрос предпросмотра не отправлялся').toBe(0);

  await guard.assertClean();
});

for (const theme of ['dark', 'light'] as const) {
  test(`config-add: перетаскивание файла (${theme}) — рамка и фон --accent, подпись, drop отправляет файл`, async ({
    page
  }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => {
      localStorage.setItem('theme', th);
    }, theme);
    await openAddCard(page);
    const card = page.getByTestId('nodes-add');
    const hint = page.getByTestId('nodes-add-hint');
    const idle = (await hint.textContent()) ?? '';

    const colors = () =>
      card.evaluate((el) => {
        const probe = document.createElement('i');
        probe.style.color = 'var(--accent)';
        probe.style.backgroundColor = 'var(--accent-soft)';
        document.body.append(probe);
        const p = getComputedStyle(probe);
        const out = { accent: p.color, soft: p.backgroundColor };
        probe.remove();
        const c = getComputedStyle(el);
        return { ...out, border: c.borderTopColor, bg: c.backgroundColor };
      });

    const before = await colors();
    expect(before.border, 'в покое рамка не акцентная').not.toBe(before.accent);

    const link = uniqueLink();
    const dt = await page.evaluateHandle((content) => {
      const d = new DataTransfer();
      d.items.add(new File([content], 'drop.conf', { type: 'text/plain' }));
      return d;
    }, link);

    await card.dispatchEvent('dragover', { dataTransfer: dt });
    await expect(card).toHaveClass(/add-dragging/);
    await expect
      .poll(async () => {
        const c = await colors();
        return c.border === c.accent && c.bg === c.soft;
      })
      .toBe(true);
    expect(((await hint.textContent()) ?? '').trim()).not.toBe(idle.trim());

    await card.dispatchEvent('dragleave', { dataTransfer: dt });
    await expect(card).not.toHaveClass(/add-dragging/);
    expect(((await hint.textContent()) ?? '').trim()).toBe(idle.trim());

    await card.dispatchEvent('dragover', { dataTransfer: dt });
    const sent = page.waitForRequest(isPreview);
    await card.dispatchEvent('drop', { dataTransfer: dt });
    const body = (await sent).postDataJSON() as { files: { name: string }[] };
    expect(body.files.map((f) => f.name)).toEqual(['drop.conf']);
    await expect(card).not.toHaveClass(/add-dragging/);
    await expect(page.getByTestId('nodes-preview-my')).toBeVisible({ timeout: T.action });

    await guard.assertClean();
  });
}

test('config-add: новый ввод заменяет прошлый — показан результат последнего', async ({ page }) => {
  // Отмена прошлого запроса (AbortController) намеренна и в сети выглядит как оборванный
  // запрос, который сборщик консоли считает нарушением, поэтому здесь он не подключается
  await openAddCard(page);
  const input = page.locator('#nodes-add-input');
  const first = page.waitForRequest(isPreview);
  await input.fill(uniqueLink());
  await first;
  await input.fill('hello world');
  await expect(page.getByTestId('nodes-preview-error')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('nodes-preview-my')).toHaveCount(0);
  await expect(page.getByTestId('nodes-add-submit')).toBeDisabled();
});

test('config-add: уже добавленный узел помечен, Добавить неактивна', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await resetDraft(page);
  const link = await addDraftNode(page);

  await openAddCard(page);
  await page.locator('#nodes-add-input').fill(link);
  await expect(page.getByTestId('nodes-preview-my')).toBeVisible({ timeout: T.action });
  await page.getByTestId('nodes-preview-my-toggle').click();
  await expect(page.getByTestId('nodes-preview-nodes').locator('.chip-default')).toHaveCount(1);
  await expect(page.getByTestId('nodes-add-submit')).toBeDisabled();

  await guard.assertClean();
});

test('config-add: на 390 px длинный ввод и все строки предпросмотра не дают горизонтальной прокрутки', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await openAddCard(page, 390);
  const input = page.locator('#nodes-add-input');
  await input.fill(`${BAD_SUB}\n${uniqueLink()}\nvless://${'a'.repeat(1500)}\nhello world`);
  await expect(page.getByTestId('nodes-preview-errors')).toBeVisible({ timeout: T.save });
  await expect(page.getByTestId('nodes-preview-my')).toBeVisible();
  await page.getByTestId('nodes-preview-my-toggle').click();
  await expect(page.getByTestId('nodes-preview-nodes')).toBeVisible();

  const m = await page.evaluate(() => {
    const root = document.documentElement;
    const card = document.querySelector('[data-testid="nodes-add"]') as HTMLElement;
    const field = document.getElementById('nodes-add-input') as HTMLElement;
    return {
      page: root.scrollWidth - root.clientWidth,
      card: card.scrollWidth - card.clientWidth,
      fieldH: field.getBoundingClientRect().height,
      fieldMax: parseFloat(getComputedStyle(field).maxHeight)
    };
  });
  expect(m.page, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);
  expect(m.card, 'содержимое карточки шире неё').toBeLessThanOrEqual(0);
  expect(m.fieldH, 'поле не выше восьми строк').toBeLessThanOrEqual(m.fieldMax + 1);

  await guard.assertClean();
});

test('config-add: пустой список источников — CTA переводит фокус в поле', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  // Пустое состояние зависит от стенда: при непустом списке его нет, сценарий не применим
  test.skip((await sourcesCount(page)) > 0, 'на стенде есть источники: пустого состояния нет');
  await openAddCard(page);
  await page.getByTestId('nodes-tab').locator('.empty-state button').click();
  await expect(page.locator('#nodes-add-input')).toBeFocused();
  await guard.assertClean();
});
