// e2e-pages: config config/nodes
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { test, expect, type Browser, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';

// Список источников и узлов на вкладке «Узлы» настоящей панели (фаза 145, план 14): карточки,
// сегмент «Xray │ Mihomo», пометки «только <ядро>», включатель источника, замаскированный адрес
// подписки, статусы по установленным ядрам, поиск и «Показать ещё 50». Ответы сети не
// подменяются: черновик наполняют настоящие маршруты добавления (данные — ссылки из
// internal/nodes/testdata/links, подписка — адрес зарезервированной зоны .invalid, провайдера
// не затрагивает). Флаг слоя включается на время прогона и возвращается прежним, черновик
// сбрасывается. В вывод не попадают имена узлов и содержимое ссылок.
//
// Не покрыто этим спеком (нужны ответы провайдера, которые нельзя получить без подмены сети):
// метаданные подписки (трафик, срок, объявление, ссылки поддержки и кабинета), статусы
// rejected/deferred/network/refused и итоги ручного «Обновить». Разметка, схемы ссылок и
// таблица статусов закреплены статически: scripts/check-config-ui.js.

const LINKS = fileURLToPath(new URL('../../../internal/nodes/testdata/links/', import.meta.url));
// Адрес в зарезервированной зоне .invalid: имя не разрешается нигде
const BAD_SUB = 'https://sub.invalid/feed';

/** Ссылка узла из тестовых данных с заданным портом и именем (идентичность узла уникальна). */
function nodeLink(file: string, port: number, name: string): string {
  const raw = readFileSync(`${LINKS}${file}`, 'utf8').trim();
  return raw.replace(/(@[^:/?#]+:)\d+/, `$1${port}`).replace(/#.*$/, '') + `#${name}`;
}

const randomPort = (): number => 20000 + Math.floor(Math.random() * 20000);

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

async function withPanel<R>(browser: Browser, fn: (page: Page) => Promise<R>): Promise<R> {
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

/** Кладёт ввод в черновик настоящими маршрутами предпросмотра и добавления. */
async function addToDraft(page: Page, text: string): Promise<void> {
  const preview = await api(page, 'POST', '/api/configlayer/sources/preview', { text, files: [] });
  expect(preview.status, 'предпросмотр ввода').toBe(200);
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const add = await api(page, 'POST', '/api/configlayer/sources/add', {
    revision: Number(view.data?.draft_revision ?? 0),
    token: preview.data?.token
  });
  expect(add.status, 'добавление в черновик').toBe(200);
}

interface Installed {
  xray: boolean;
  mihomo: boolean;
}

async function installedKernels(page: Page): Promise<Installed> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  expect(view.status, 'GET /api/configlayer/sources').toBe(200);
  const inst = (view.data?.installed ?? {}) as Partial<Installed>;
  return { xray: inst.xray === true, mihomo: inst.mihomo === true };
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

async function openNodes(page: Page, width = 1440): Promise<void> {
  await page.setViewportSize({ width, height: width > 600 ? 900 : 844 });
  await page.goto('/#/config/nodes', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('nodes-tab').locator('.skeleton')).toHaveCount(0, {
    timeout: T.action
  });
}

const cards = (page: Page): Locator => page.locator('article.source-card');

async function expandCard(card: Locator): Promise<void> {
  const toggle = card.locator('.sc-expand');
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

test('nodes-list: «Мои узлы» последней карточкой, сводка по установленным ядрам, раскрытие со строками', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await resetDraft(page);
  const inst = await installedKernels(page);
  const port = randomPort();
  await addToDraft(
    page,
    [
      nodeLink('vless_ws.txt', port, 'ListA'),
      nodeLink('tuic.txt', port + 1, 'ListB'),
      nodeLink('socks.txt', port + 2, 'ListC')
    ].join('\n')
  );

  await openNodes(page);
  const my = page.getByTestId('nodes-source-my');
  await expect(my).toBeVisible({ timeout: T.action });
  // порядок задаёт сервер: «Мои узлы» — всегда последняя карточка
  await expect(cards(page).last()).toHaveAttribute('data-testid', 'nodes-source-my');
  // счётчик источников виден, сводка называет только установленные ядра
  await expect(page.getByTestId('nodes-sources-count')).toContainText(/\d/);
  const summary = page.getByTestId('nodes-source-counts-my');
  await expect(summary).toContainText(/\d/);
  if (inst.xray) await expect(summary).toContainText('Xray');
  else await expect(summary).not.toContainText('Xray');
  if (inst.mihomo) await expect(summary).toContainText('Mihomo');
  else await expect(summary).not.toContainText('Mihomo');

  // строки статуса: по одной на установленное ядро, неустановленному строки нет
  await expect(page.getByTestId('nodes-status-my-xray')).toHaveCount(inst.xray ? 1 : 0);
  await expect(page.getByTestId('nodes-status-my-mihomo')).toHaveCount(inst.mihomo ? 1 : 0);

  // список свёрнут и раскрывается кнопкой с aria-expanded
  await expect(page.getByTestId('nodes-list-my')).toHaveCount(0);
  await expandCard(my);
  await expect(page.getByTestId('nodes-list-my')).toBeVisible();
  await expect(page.getByTestId('nodes-list-my').locator('.node-row').first()).toBeVisible();
  // учётные данные в строках не показываются: адрес без логина и пароля
  const rowText = await page.getByTestId('nodes-list-my').locator('.node-row').allInnerTexts();
  expect(
    rowText.some((t) => t.includes('@')),
    'в строке узла нет учётных данных'
  ).toBe(false);

  // раскрытие не сбрасывается сменой вкладки
  await page.getByTestId('config-tab-files').click();
  await expect(page.getByTestId('config-files')).toBeVisible({ timeout: T.action });
  await page.getByTestId('config-tab-nodes').click();
  await expect(page.getByTestId('nodes-source-my').locator('.sc-expand')).toHaveAttribute(
    'aria-expanded',
    'true',
    { timeout: T.action }
  );

  await guard.assertClean();
});

test('nodes-list: сегмент ядра меняет набор, узел чужого ядра приглушён цветом и помечен', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await resetDraft(page);
  const inst = await installedKernels(page);
  const port = randomPort();
  // tuic выражается только в Mihomo, vless по ws — в обоих ядрах
  await addToDraft(
    page,
    [nodeLink('tuic.txt', port, 'SegT'), nodeLink('vless_ws.txt', port + 1, 'SegV')].join('\n')
  );

  await openNodes(page);
  const my = page.getByTestId('nodes-source-my');
  await expandCard(my);
  const segment = page.getByTestId('nodes-kernel-segment');

  if (!(inst.xray && inst.mihomo)) {
    // одно ядро — сегмента нет, показан один набор
    await expect(segment).toHaveCount(0);
    await guard.assertClean();
    return;
  }

  await expect(segment).toBeVisible();
  await expect(segment.getByRole('button', { name: 'Xray' })).toBeVisible();
  await expect(segment.getByRole('button', { name: 'Mihomo' })).toBeVisible();

  const tuic = page
    .locator('.node-row')
    .filter({ hasText: `:${port}` })
    .first();
  const vless = page
    .locator('.node-row')
    .filter({ hasText: `:${port + 1}` })
    .first();
  const dimState = (row: Locator) =>
    row.evaluate((el) => {
      const name = el.querySelector('.node-name') as HTMLElement;
      const probe = document.createElement('span');
      probe.style.color = 'var(--fg-dim)';
      document.body.appendChild(probe);
      const dim = getComputedStyle(probe).color;
      probe.remove();
      return {
        dimmed: getComputedStyle(name).color === dim,
        opacity: getComputedStyle(name).opacity,
        mark: el.querySelector('.node-mark')?.getAttribute('data-mark') ?? null
      };
    });

  await segment.getByRole('button', { name: 'Mihomo' }).click();
  await expect(segment.getByRole('button', { name: 'Mihomo' })).toHaveAttribute(
    'aria-pressed',
    'true'
  );
  await expect(tuic).toBeVisible({ timeout: T.action });
  expect(await dimState(tuic), 'в наборе Mihomo tuic без приглушения и пометки').toEqual({
    dimmed: false,
    opacity: '1',
    mark: null
  });

  await segment.getByRole('button', { name: 'Xray' }).click();
  await expect(segment.getByRole('button', { name: 'Xray' })).toHaveAttribute(
    'aria-pressed',
    'true'
  );
  // в наборе Xray tuic остаётся в списке: цвет --fg-dim без opacity и чип «только Mihomo»
  await expect(tuic.locator('.node-mark')).toHaveAttribute('data-mark', 'only_mihomo', {
    timeout: T.action
  });
  expect(await dimState(tuic), 'в наборе Xray tuic приглушён цветом').toEqual({
    dimmed: true,
    opacity: '1',
    mark: 'only_mihomo'
  });
  // у приглушённого узла в title чипа причина
  await expect(tuic.locator('.node-mark')).toHaveAttribute('title', /\S/);
  // узел, который умеют оба ядра, остаётся обычным в обоих наборах
  expect((await dimState(vless)).dimmed, 'vless в наборе Xray без приглушения').toBe(false);
  // у узла нет переключателей и флажков ядер
  await expect(tuic.locator('input, [role="switch"]')).toHaveCount(0);

  await guard.assertClean();
});

test('nodes-list: включатель источника идёт в черновик с ревизией, выключенный помечен', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await resetDraft(page);
  await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'SwitchA'));

  await openNodes(page);
  const my = page.getByTestId('nodes-source-my');
  const sw = page.getByTestId('nodes-enabled-my');
  await expect(sw).toBeChecked({ timeout: T.action });
  await expect(page.getByTestId('nodes-source-off-my')).toHaveCount(0);

  const req = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().endsWith('/api/configlayer/sources/update')
  );
  // сам input скрыт (opacity 0): клик по ползунку
  await my.locator('.toggle-slider').click();
  const body = (await req).postDataJSON() as {
    id?: unknown;
    revision?: unknown;
    patch?: { enabled?: unknown };
  };
  expect(body.id).toBe('my');
  expect(typeof body.revision, 'в теле update есть ревизия').toBe('number');
  expect(body.patch?.enabled).toBe(false);

  await expect(page.getByTestId('nodes-source-off-my')).toBeVisible({ timeout: T.action });
  await expect(sw).not.toBeChecked();
  // источник по-прежнему в списке, имя и сводка приглушены цветом, не прозрачностью
  const dim = await my.evaluate((el) => {
    const probe = document.createElement('span');
    probe.style.color = 'var(--fg-dim)';
    document.body.appendChild(probe);
    const want = getComputedStyle(probe).color;
    probe.remove();
    const name = el.querySelector('.sc-name') as HTMLElement;
    const sum = el.querySelector('.sc-summary') as HTMLElement;
    return {
      name: getComputedStyle(name).color === want,
      summary: getComputedStyle(sum).color === want,
      opacity: getComputedStyle(el).opacity
    };
  });
  expect(dim).toEqual({ name: true, summary: true, opacity: '1' });

  // обратно: источник снова включён
  await my.locator('.toggle-slider').click();
  await expect(sw).toBeChecked({ timeout: T.action });
  await expect(page.getByTestId('nodes-source-off-my')).toHaveCount(0);

  await guard.assertClean();
});

test('nodes-list: поиск при больше 12 узлах, «Показать ещё 50» после 100 строк', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await resetDraft(page);
  const base = randomPort();
  const lines: string[] = [];
  for (let i = 0; i < 130; i++) lines.push(nodeLink('vless_ws.txt', base + i, `Many${i}`));
  await addToDraft(page, lines.join('\n'));

  await openNodes(page);
  const my = page.getByTestId('nodes-source-my');
  await expandCard(my);
  const rows = page.getByTestId('nodes-list-my').locator('.node-row');
  const search = page.getByTestId('nodes-search-my');
  await expect(search).toBeVisible({ timeout: T.action });

  // рисуются первые 100 строк и кнопка «Показать ещё 50»
  await expect(rows).toHaveCount(100, { timeout: T.action });
  const more = page.getByTestId('nodes-more-my');
  await expect(more).toBeVisible();
  const before = await rows.count();
  await more.click();
  await expect.poll(() => rows.count(), { timeout: T.action }).toBeGreaterThan(before);
  expect(await rows.count()).toBeLessThanOrEqual(150);

  // фильтр по имени без учёта регистра, на клиенте
  await search.fill('many12');
  await expect.poll(() => rows.count(), { timeout: T.action }).toBeLessThan(15);
  expect(await rows.count()).toBeGreaterThan(0);
  await expect(page.getByTestId('nodes-more-my')).toHaveCount(0);
  // без совпадений — строка «Ничего не найдено», узлов нет
  await search.fill('zzzz-no-such-node');
  await expect(page.getByTestId('nodes-search-empty-my')).toBeVisible({ timeout: T.action });
  await expect(rows).toHaveCount(0);

  await guard.assertClean();
});

test('nodes-list: подписка — замаскированный адрес, «Показать»/«Скопировать», статусы по ядрам, выключение', async ({
  browser
}) => {
  const context = await browser.newContext({
    baseURL: RT.url,
    ignoreHTTPSErrors: true,
    storageState: process.env.XCP_PW_STATE,
    permissions: ['clipboard-read', 'clipboard-write']
  });
  try {
    const page = await context.newPage();
    const guard = attachConsoleGuard(page);
    await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
    await resetDraft(page);
    const inst = await installedKernels(page);
    await addToDraft(page, BAD_SUB);

    await openNodes(page);
    // подписка из черновика находится по домену; на стенде могут быть и другие подписки
    const view = await api(page, 'GET', '/api/configlayer/sources');
    const list = (view.data?.sources ?? []) as { id: string; host?: string }[];
    const found = list.filter((s) => s.host === 'sub.invalid');
    expect(found.length, 'подписка из черновика одна').toBe(1);
    const id = found[0].id;
    const sub = page.getByTestId(`nodes-source-${id}`);
    await expect(sub).toBeVisible({ timeout: T.action });

    const url = page.getByTestId(`nodes-url-${id}`);
    const hasFull = async () => (await url.innerText()).includes('/feed');
    await expect(url).toBeVisible();
    expect(await hasFull(), 'до «Показать» полного адреса нет').toBe(false);

    const reveal = page.getByTestId(`nodes-url-reveal-${id}`);
    await reveal.click();
    await expect.poll(hasFull, { timeout: T.action }).toBe(true);
    await expect(reveal).toHaveAttribute('aria-pressed', 'true');
    await reveal.click();
    await expect.poll(hasFull, { timeout: T.action }).toBe(false);

    // «Скопировать» кладёт полный адрес в буфер и показывает уведомление
    await page.getByTestId(`nodes-url-copy-${id}`).click();
    await expect(page.locator('.toast').first()).toBeVisible({ timeout: T.action });
    const clip = await page.evaluate(() => navigator.clipboard.readText());
    expect(clip === BAD_SUB, 'в буфере полный адрес подписки').toBe(true);

    // строки статуса: по одной на установленное ядро; ответа провайдера ещё не было — idle
    for (const k of ['xray', 'mihomo'] as const) {
      const row = page.getByTestId(`nodes-status-${id}-${k}`);
      await expect(row).toHaveCount(inst[k] ? 1 : 0);
      if (inst[k]) await expect(row.locator('.status-badge.idle')).toHaveCount(1);
    }

    // «Обновить» доходит до сервера, после ответа кнопка снова доступна, итог — уведомление
    const refreshBtn = page.getByTestId(`nodes-refresh-${id}`);
    await expect(refreshBtn).toBeEnabled();
    const done = page.waitForResponse(
      (r) =>
        r.request().method() === 'POST' && r.url().endsWith('/api/configlayer/sources/refresh'),
      { timeout: T.save }
    );
    await refreshBtn.click();
    await done;
    await expect(refreshBtn).toBeEnabled({ timeout: T.save });

    // выключение: чип «Выключен», кнопки «Обновить» нет, статусы — «Выключен»
    await sub.locator('.toggle-slider').click();
    await expect(page.getByTestId(`nodes-source-off-${id}`)).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId(`nodes-refresh-${id}`)).toHaveCount(0);
    for (const k of ['xray', 'mihomo'] as const) {
      if (inst[k]) {
        await expect(
          page.getByTestId(`nodes-status-${id}-${k}`).locator('.status-badge.idle')
        ).toHaveCount(1);
      }
    }

    await guard.assertClean();
  } finally {
    await context.close();
  }
});

for (const theme of ['dark', 'light'] as const) {
  test(`nodes-list: 390 px, тема ${theme} — без горизонтальной прокрутки, кнопки не обрезаны`, async ({
    page
  }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
    await resetDraft(page);
    const base = randomPort();
    await addToDraft(
      page,
      [
        BAD_SUB,
        nodeLink('tuic.txt', base, 'NarrowLongNameNarrowLongNameNarrowLongNameNarrowLongName'),
        nodeLink('vless_ws.txt', base + 1, 'Narrow')
      ].join('\n')
    );

    await openNodes(page, 390);
    await expect(cards(page).first()).toBeVisible({ timeout: T.action });
    for (let i = 0; i < (await cards(page).count()); i++) {
      await expandCard(cards(page).nth(i));
    }
    const metrics = await page.evaluate(() => {
      const root = document.documentElement;
      const theme = root.getAttribute('data-theme');
      const small = [...document.querySelectorAll('article.source-card button')]
        .map((el) => (el as HTMLElement).getBoundingClientRect())
        .filter((r) => r.width > 0 && r.height > 0 && (r.height < 32 || r.width < 32)).length;
      return { theme, pageScroll: root.scrollWidth - root.clientWidth, small };
    });
    expect(metrics.theme, 'применена тема').toBe(theme);
    expect(metrics.pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);
    expect(metrics.small, 'кнопки карточки не крохотные').toBe(0);

    await guard.assertClean();
  });
}
