// e2e-pages: config config/nodes config/files
// router-area: sources
import { execFile } from 'node:child_process';
import { randomInt } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import {
  test,
  expect,
  type Browser,
  type ConsoleMessage,
  type Page,
  type Request,
  type Response
} from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { FULL_TAG, TIER_FULL_ANNOTATION } from './lib/known';

// Каркас раздела «Конфигурация» на настоящей панели (фаза 145, план 12): вкладки «Узлы»
// и «Файлы» с маршрутами #/config/nodes и #/config/files, общая полоса черновика,
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

// --- Сбой обновления окна изменений (G149-01, D-08) ---
// Настоящий отказ чтения: самоссылка с именем файла панели в каталоге конфигураций Xray.
// Сверка сирот берёт её без чтения, предпросмотр читает «было» и получает ELOOP, обработчик
// отвечает HTTP 500 internal. Создаётся и удаляется только вычисленный тестом путь; применение,
// перезапуск ядра и скан запуска ядра, пока самоссылка существует, тест не делает.

const run = promisify(execFile);
const REPO_ROOT = fileURLToPath(new URL('../../../', import.meta.url));
const XRAY_CONFIGS = '/opt/etc/xray/configs';
const ORPHAN_NAME = /^xcp-e2e-preview-\d+\.json$/;
const PREVIEW_PATH = '/api/configlayer/apply/preview';
const APPLY_PATH = '/api/configlayer/apply';
const RESOURCE_500 = /^Failed to load resource: the server responded with a status of 500\b/;
const ELOOP_TEXT = /too many levels of symbolic links|ELOOP/i;

/** Самоссылка, созданная этим спеком и ещё не удалённая: её убирает afterEach при прерывании. */
let ownedOrphan: string | null = null;

/**
 * Команда на стенде arm64 через scripts/router/lib.sh из корня репозитория: алиас и доступы
 * берутся только из локального файла целей. Возвращает код выхода команды; сбой подготовки
 * (файл целей, связь) — ошибка. Вывод ssh (в нём может быть алиас) в отчёт не попадает,
 * поэтому исходная ошибка процесса не прикладывается причиной.
 */
async function standSh(remote: string): Promise<number> {
  const code = await run(
    'sh',
    [
      '-c',
      '. scripts/router/lib.sh || exit 90; rt_load_targets || exit 91; rt_ssh arm64 "$1"',
      'sh',
      remote
    ],
    { cwd: REPO_ROOT, env: { ...process.env, ROUTERS: 'arm64' }, timeout: 60_000 }
  ).then(
    () => 0 as unknown,
    (err: unknown) => (err as { code?: unknown } | null)?.code
  );
  if (code === 3) throw new Error('стенд arm64: нет связи по ssh');
  if (code === 90 || code === 91) throw new Error('стенд arm64: файл целей не прочитан');
  if (typeof code !== 'number') {
    throw new Error('стенд arm64: команда не выполнена (таймаут или запуск sh)');
  }
  return code;
}

function orphanPath(name: string): string {
  if (!ORPHAN_NAME.test(name)) throw new Error('имя сироты вне шаблона теста');
  return `${XRAY_CONFIGS}/${name}`;
}

/** Путь свободен (lstat: ни файла, ни ссылки), ключа xray:<имя> в манифесте нет. */
async function expectOrphanFree(page: Page, name: string): Promise<void> {
  const p = orphanPath(name);
  const state = await api(page, 'GET', '/api/configlayer/state');
  expect(state.status, 'GET /api/configlayer/state').toBe(200);
  const files = Array.isArray(state.data?.files) ? (state.data.files as { key?: unknown }[]) : [];
  expect(
    files.some((f) => f?.key === `xray:${name}`),
    'G149-01 подготовка: ключ сироты уже в манифесте'
  ).toBe(false);
  const code = await standSh(`test ! -e ${p} && test ! -L ${p} || exit 10`);
  if (code !== 0) throw new Error(`G149-01 подготовка: путь сироты уже занят (код ${code})`);
}

/** Создаёт самоссылку name → name; существующий путь не заменяет. */
async function createSelfLink(name: string): Promise<void> {
  const p = orphanPath(name);
  const code = await standSh(
    `test -d ${XRAY_CONFIGS} || exit 11; test ! -e ${p} && test ! -L ${p} || exit 10; ` +
      `ln -s ${name} ${p} || exit 12; test -L ${p} || exit 13`
  );
  // 10 и 11 — до создания: путь чужой или каталога нет, удалять нечего
  if (code !== 10 && code !== 11) ownedOrphan = name;
  if (code !== 0) throw new Error(`G149-01 подготовка: самоссылка не создана (код ${code})`);
}

/** Удаляет только ссылку по вычисленному пути и проверяет, что пути больше нет. */
async function removeSelfLink(name: string): Promise<void> {
  const p = orphanPath(name);
  const code = await standSh(
    `if test -L ${p}; then rm -f ${p}; fi; test ! -e ${p} && test ! -L ${p} || exit 10`
  );
  if (code !== 0) throw new Error(`G149-01 уборка: путь сироты остался (код ${code})`);
  if (ownedOrphan === name) ownedOrphan = null;
}

function urlPath(url: string): string {
  try {
    return new URL(url).pathname;
  } catch {
    return url;
  }
}

function isPreviewGet(req: Request): boolean {
  return req.method() === 'GET' && urlPath(req.url()) === PREVIEW_PATH;
}

interface FaultWindow {
  violations: string[];
  /** Ответы HTTP 500 предпросмотра: тело сверяется после окна. */
  failed: Response[];
  /** location.url ресурсных строк console.error про 500. */
  resourceLines: string[];
  applyPosts: number;
  detach(): void;
}

/**
 * Строгий сборщик узкого окна сбоя. Допустимы только HTTP 500 GET предпросмотра и ресурсная
 * строка console.error с адресом этого же запроса; любые другие 5xx, requestfailed, pageerror
 * и console.error — нарушения. ERR_ABORTED запроса, на который ответ уже пришёл, отказом не
 * считается, как и в attachConsoleGuard.
 */
function openFaultWindow(page: Page): FaultWindow {
  const answered = new WeakSet<Request>();
  const w: FaultWindow = {
    violations: [],
    failed: [],
    resourceLines: [],
    applyPosts: 0,
    detach: () => {
      page.off('console', onConsole);
      page.off('pageerror', onPageError);
      page.off('response', onResponse);
      page.off('requestfailed', onRequestFailed);
      page.off('request', onRequest);
    }
  };
  const onConsole = (msg: ConsoleMessage) => {
    if (msg.type() !== 'error') return;
    if (RESOURCE_500.test(msg.text()) && urlPath(msg.location().url) === PREVIEW_PATH) {
      w.resourceLines.push(msg.location().url);
      return;
    }
    w.violations.push(`console.error: ${msg.text()}`);
  };
  const onPageError = (err: Error) => {
    w.violations.push(`pageerror: ${err.message}`);
  };
  const onResponse = (res: Response) => {
    answered.add(res.request());
    if (res.status() < 500) return;
    if (res.status() === 500 && isPreviewGet(res.request())) {
      w.failed.push(res);
      return;
    }
    w.violations.push(`HTTP ${res.status()} ${res.request().method()} ${urlPath(res.url())}`);
  };
  const onRequestFailed = (req: Request) => {
    const why = req.failure()?.errorText ?? 'без причины';
    if (why === 'net::ERR_ABORTED' && answered.has(req)) return;
    w.violations.push(`requestfailed ${req.method()} ${urlPath(req.url())}: ${why}`);
  };
  const onRequest = (req: Request) => {
    if (req.method() === 'POST' && urlPath(req.url()) === APPLY_PATH) w.applyPosts++;
  };
  page.on('console', onConsole);
  page.on('pageerror', onPageError);
  page.on('response', onResponse);
  page.on('requestfailed', onRequestFailed);
  page.on('request', onRequest);
  return w;
}

/** Тело HTTP 500 предпросмотра — отказ чтения именно этой сироты. */
function isOrphanEloop(body: unknown, name: string): boolean {
  const b = body as { code?: unknown; detail?: unknown } | null;
  return (
    b?.code === 'internal' &&
    typeof b.detail === 'string' &&
    b.detail.includes(name) &&
    ELOOP_TEXT.test(b.detail)
  );
}

/** Нарушения окна сбоя после его закрытия: посторонние ошибки и непарные строки консоли. */
async function faultProblems(w: FaultWindow, name: string): Promise<string[]> {
  const out = [...w.violations];
  for (const res of w.failed) {
    const body = await res.json().catch(() => null);
    if (!isOrphanEloop(body, name)) out.push('HTTP 500 предпросмотра не от ELOOP сироты теста');
  }
  const seen = w.failed.map((r) => r.url());
  for (const url of w.resourceLines) {
    const i = seen.indexOf(url);
    if (i < 0) out.push(`строка консоли о 500 без наблюдённого ответа: ${urlPath(url)}`);
    else seen.splice(i, 1);
  }
  if (w.applyPosts > 0) out.push(`POST ${APPLY_PATH} отправлен ${w.applyPosts} раз`);
  return out;
}

let layerWasOn = false;

test.describe.configure({ mode: 'serial' });

test.afterEach(async () => {
  // Прерванный тест: убрать только свою самоссылку по известному имени
  if (ownedOrphan !== null) await removeSelfLink(ownedOrphan);
});

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
  await expect(page.getByTestId('files-tab')).toHaveCount(0);

  await page.getByTestId('config-tab-files').click();
  await expect(page).toHaveURL(/#\/config\/files$/);
  await expect(page.getByTestId('files-tab')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('config-tab-files')).toHaveAttribute('aria-selected', 'true');
  await expect(nodesTab).toHaveCount(0);

  await page.getByTestId('config-tab-nodes').click();
  await expect(page).toHaveURL(/#\/config\/nodes$/);
  await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('files-tab')).toHaveCount(0);

  await guard.assertClean();
});

test('config-tabs: #/config/files напрямую открывает Файлы', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openConfig(page, '#/config/files');
  await page.reload({ timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('config-tab-files')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByTestId('files-tab')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('nodes-tab')).toHaveCount(0);
  await guard.assertClean();
});

test(
  'config-tabs: полоса черновика общая, смена вкладки её не пересоздаёт, бейдж меню считает',
  {
    tag: FULL_TAG,
    annotation: {
      type: TIER_FULL_ANNOTATION,
      description: 'внесение сбоя: ELOOP при чтении сироты preview'
    }
  },
  async ({ page }) => {
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
    await expect(page.getByTestId('files-tab')).toBeVisible({ timeout: T.action });
    await expect(bar).toBeVisible();
    expect(await bar.evaluate((el) => (el as HTMLElement & { __kept?: boolean }).__kept)).toBe(
      true
    );

    await page.getByTestId('config-tab-nodes').click();
    await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.action });
    expect(await bar.evaluate((el) => (el as HTMLElement & { __kept?: boolean }).__kept)).toBe(
      true
    );

    // бейдж меню = изменения черновика + видимые уведомления слоя
    const state = await api(page, 'GET', '/api/configlayer/state');
    const changes = Number(state.data?.draft_changes ?? 0);
    expect(changes, 'в черновике есть изменение').toBeGreaterThan(0);
    const notices = await page.locator('[data-testid="config-notices"] [data-notice]').count();
    const badge = page.locator('.nav-badge-config');
    await expect(badge).toBeVisible({ timeout: T.action });
    const expected = changes + notices;
    await expect(badge).toHaveText(expected > 99 ? '99+' : String(expected));

    // Окно изменений: сводка apply-preview-count равна draft_changes и числу строк apply-preview-list
    await page.getByTestId('config-apply').click();
    const modal = page.getByTestId('apply-preview-modal');
    // Если маршрутизацию ещё не принимала панель, сначала открывается диалог «Взять маршрутизацию»:
    // «Применить только узлы» ведёт в то же окно изменений без взятия
    const nodesOnly = page.getByTestId('take-routing-nodes-only');
    await expect(modal.or(nodesOnly)).toBeVisible({ timeout: T.action });
    if (await nodesOnly.isVisible()) await nodesOnly.click();
    await expect(modal).toBeVisible({ timeout: T.action });
    const previewCountText = (await page.getByTestId('apply-preview-count').textContent()) ?? '';
    const previewCount = Number(previewCountText.trim());
    const rowsCount = await page.locator('[data-testid="apply-preview-list"] li').count();
    expect(previewCount, 'сводка в окне равна draft_changes').toBe(changes);
    expect(rowsCount, 'число строк в окне равно draft_changes').toBe(changes);
    const confirmBtn = page.getByTestId('apply-preview-confirm');
    await expect(confirmBtn, 'исходный предпросмотр разрешает подтверждение').toBeEnabled();

    // Сбой обновления окна (G149-01, D-08): прежний успешный список остаётся на экране, но
    // подтвердить его нельзя, пока повтор не прочитает предпросмотр заново. Чистый начальный
    // участок проверен и отключён; на окно сбоя — свой строгий сборщик.
    await guard.assertClean();
    const orphan = `xcp-e2e-preview-${randomInt(100_000_000, 1_000_000_000)}.json`;
    await expectOrphanFree(page, orphan);
    const fault = openFaultWindow(page);
    let primary: unknown;
    let cleanupError: unknown;
    try {
      await createSelfLink(orphan);
      // Вторая правка черновика настоящим API: событие draft перечитывает открытое окно
      const failedPreview = page
        .waitForResponse((r) => isPreviewGet(r.request()) && r.status() === 500, {
          timeout: T.action
        })
        .catch(() => null);
      await addDraftNode(page);
      const res500 = await failedPreview;
      if (res500 === null) {
        throw new Error(
          'G149-01 подготовка: окно не получило HTTP 500 предпросмотра — сбой не внесён'
        );
      }
      expect(
        isOrphanEloop(await res500.json().catch(() => null), orphan),
        'G149-01 подготовка: HTTP 500 internal от ELOOP сироты теста'
      ).toBe(true);
      await expect(page.getByTestId('apply-preview-error')).toBeVisible({ timeout: T.action });
      await expect(confirmBtn, 'G149-01 preview failure blocks confirmation').toBeDisabled();
      expect(fault.applyPosts, 'G149-01: применение не отправлялось').toBe(0);

      // Сирота убрана до штатного повтора: успешное чтение новой ревизии снимает блокировку
      await removeSelfLink(orphan);
      const rev = Number(
        (await api(page, 'GET', '/api/configlayer/sources')).data?.draft_revision ?? 0
      );
      const okPreview = page
        .waitForResponse((r) => isPreviewGet(r.request()) && r.status() === 200, {
          timeout: T.action
        })
        .catch(() => null);
      await page.getByTestId('apply-preview-retry').click();
      const res200 = await okPreview;
      if (res200 === null) throw new Error('G149-01: повтор не получил HTTP 200 предпросмотра');
      const okBody = (await res200.json().catch(() => null)) as {
        data?: { draft_revision?: unknown };
      } | null;
      expect(Number(okBody?.data?.draft_revision), 'повтор читает новую ревизию').toBe(rev);
      await expect(page.getByTestId('apply-preview-error')).toHaveCount(0);
      await expect(confirmBtn, 'G149-01: успешный повтор снимает блокировку').toBeEnabled();
    } catch (err) {
      primary = err;
    } finally {
      if (ownedOrphan !== null) {
        try {
          await removeSelfLink(ownedOrphan);
        } catch (err) {
          cleanupError = err;
        }
      }
      fault.detach();
    }
    const problems = await faultProblems(fault, orphan);
    if (primary !== undefined) {
      if (problems.length > 0 && primary instanceof Error) {
        primary.message += `\n\nнарушения окна сбоя:\n- ${problems.join('\n- ')}`;
      }
      throw primary;
    }
    if (cleanupError !== undefined) throw cleanupError;
    expect(problems, 'G149-01: в окне сбоя нет посторонних ошибок').toEqual([]);

    // Остаток сценария — под новым сборщиком консоли
    const rest = attachConsoleGuard(page);
    const changesNow = Number(
      (await api(page, 'GET', '/api/configlayer/state')).data?.draft_changes ?? 0
    );
    expect(changesNow, 'вторая правка в черновике').toBeGreaterThanOrEqual(changes);

    // «Закрыть без применения» не меняет черновик
    await page.getByTestId('apply-preview-cancel').click();
    await expect(modal).toHaveCount(0);
    const stateAfterCancel = await api(page, 'GET', '/api/configlayer/state');
    expect(
      Number(stateAfterCancel.data?.draft_changes ?? 0),
      'после отмены draft_changes прежний'
    ).toBe(changesNow);

    // «Сбросить» отменяет весь черновик
    await resetDraft(page);
    const stateAfterReset = await api(page, 'GET', '/api/configlayer/state');
    expect(
      Number(stateAfterReset.data?.draft_changes ?? 0),
      'после сброса draft_changes равен 0'
    ).toBe(0);

    await rest.assertClean();
  }
);

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
  expect(after, 'узел попал в черновик').toBe(before + 1);

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
  await expect(page.getByTestId('nodes-preview-nodes').locator('.badge-neutral')).toHaveCount(1);
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
  const sources = await sourcesCount(page);
  test.skip(
    sources > 0,
    `на стенде есть источники (${sources}): пустого состояния нет — сценарий проверяется на стенде без источников`
  );
  await openAddCard(page);
  await page.getByTestId('nodes-tab').locator('.empty-state button').click();
  await expect(page.locator('#nodes-add-input')).toBeFocused();
  await guard.assertClean();
});
