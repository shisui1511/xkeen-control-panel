// e2e-pages: config config/nodes config/files
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
async function addDraftNode(page: Page): Promise<void> {
  const text = readFileSync(LINK, 'utf8').trim();
  const preview = await api(page, 'POST', '/api/configlayer/sources/preview', { text, files: [] });
  expect(preview.status, 'предпросмотр узла').toBe(200);
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const add = await api(page, 'POST', '/api/configlayer/sources/add', {
    revision: Number(view.data?.draft_revision ?? 0),
    token: preview.data?.token
  });
  expect(add.status, 'добавление узла в черновик').toBe(200);
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
