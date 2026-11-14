// e2e-pages: config/setup config dashboard
// router-area: configlayer
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard, type ConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { SLOW_ANNOTATION, quarantine } from './lib/known';
import {
  addToDraft,
  api,
  layerFlag,
  listSources,
  nodeLink,
  randomPort,
  resetDraft,
  waitApplyIdle,
  withPanel
} from './lib/nodes-api';

// Мастер первого запуска и карточка дашборда «Настроить VPN» на настоящей панели (фаза 149.1,
// CFG-04): карточка вместо карточки Mihomo при флаге слоя, переход в мастер на первый
// невыполненный шаг, «Скрыть» навсегда, «Закрыть» без перехвата входа в раздел. Ответы сети
// не подменяются, ссылки узлов — только из internal/nodes/testdata/links.
//
// Детерминизм при любом состоянии шагов 1–6 стенда: любая проверка, которой нужен статус
// мастера не done, идёт при удержанном черновике (holdDraft) после сброса признака
// (wizardReset) — пока черновик открыт, шаг 5 не выполнен, и наблюдатель сервера (тик 10 с)
// не может поставить «пройдено». Статус мастера, dev_mode, черновик и флаг слоя спек
// возвращает в afterAll; фактический статус после прогона — строка вывода wizard-status:.
// В сообщения проверок попадают только testId, статусы и счётчики.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

const WIZARD = '/api/configlayer/wizard';
// Узел удержания черновика: файл testdata, порт и имя уникальны на каждый вызов
const HOLD_LINK = 'vless_ws.txt';
// Счётчик карточки «N из 6» / «N of 6»
const PROGRESS = /^\s*(\d+)\s+(?:из|of)\s+(\d+)\s*$/;

interface WizardStepView {
  id: string;
  done: boolean;
  partial?: { done: number; total: number };
  taken?: boolean;
  draft_changes?: number;
  applied_empty?: boolean;
  apply_running?: boolean;
}

interface WizardView {
  status: string;
  empty: boolean;
  auto_open: boolean;
  card_visible: boolean;
  done_count: number;
  total: number;
  current: number;
  kernels: { xkeen: boolean; xray: boolean; mihomo: boolean; conflict: boolean };
  steps: WizardStepView[];
}

/** Статус мастера по настоящему API: GET /api/configlayer/wizard. */
async function wizard(page: Page): Promise<WizardView> {
  const res = await api(page, 'GET', WIZARD);
  expect(res.status, `GET ${WIZARD}`).toBe(200);
  return toWizardView(res.data);
}

/** Вид мастера из тела ответа: GET и POST-действия мастера отвечают одним видом. */
function toWizardView(data: Record<string, unknown> | null | undefined): WizardView {
  const d = data ?? {};
  const kernels = (d.kernels ?? {}) as Partial<WizardView['kernels']>;
  return {
    status: String(d.status ?? ''),
    empty: d.empty === true,
    auto_open: d.auto_open === true,
    card_visible: d.card_visible === true,
    done_count: Number(d.done_count ?? 0),
    total: Number(d.total ?? 0),
    current: Number(d.current ?? 0),
    kernels: {
      xkeen: kernels.xkeen === true,
      xray: kernels.xray === true,
      mihomo: kernels.mihomo === true,
      conflict: kernels.conflict === true
    },
    steps: (Array.isArray(d.steps) ? d.steps : []) as WizardStepView[]
  };
}

/** Шаг ответа мастера по id; нет шага — провал с id. */
function stepOf(view: WizardView, id: string): WizardStepView {
  const step = view.steps.find((s) => s.id === id);
  expect(step, `шаг ${id} в ответе мастера`).toBeTruthy();
  return step as WizardStepView;
}

/**
 * Удерживает черновик открытым: кладёт в него узел из testdata и ждёт, пока сервер увидит
 * открытый черновик (шаг 5 не выполнен, изменений больше нуля).
 */
async function holdDraft(page: Page): Promise<void> {
  await addToDraft(page, nodeLink(HOLD_LINK, randomPort(), `RtWizHold${randomPort()}`));
  await expect
    .poll(
      async () => {
        const apply = stepOf(await wizard(page), 'apply');
        return apply.done === false && Number(apply.draft_changes ?? 0) > 0;
      },
      { message: 'удержание черновика: apply.done ложно, draft_changes > 0', timeout: T.action }
    )
    .toBe(true);
}

/**
 * Снимает признак мастера (нужен dev_mode). Сброс и отметка done наблюдателем идут по
 * очереди, а вид в ответе сброса собран до следующего тика: статус в нём обязан быть пустым
 * с первого сброса.
 */
async function wizardReset(page: Page): Promise<WizardView> {
  const res = await api(page, 'POST', `${WIZARD}/reset`);
  expect(res.status, `POST ${WIZARD}/reset (${String(res.data?.code ?? '')})`).toBe(200);
  const view = toWizardView(res.data);
  expect(view.status, 'статус мастера после сброса').toBe('');
  return view;
}

/**
 * Возвращает признак мастера prev при удержанном черновике: сброс (признак монотонен,
 * hidden иначе не опустить до closed), затем «Закрыть» или «Скрыть»; пустой и done — только
 * сброс (done наблюдатель поставит снова, если шаги 1–6 выполнены). Затем черновик
 * отпускается.
 */
async function restoreStatus(page: Page, prev: string): Promise<void> {
  await holdDraft(page);
  try {
    await wizardReset(page);
    if (prev === 'closed' || prev === 'hidden') {
      const action = prev === 'closed' ? 'close' : 'hide';
      const res = await api(page, 'POST', `${WIZARD}/${action}`);
      expect(res.status, `возврат статуса мастера ${prev}`).toBe(200);
    }
  } finally {
    await resetDraft(page);
  }
}

async function setDevMode(page: Page, on: boolean): Promise<void> {
  const res = await api(page, 'POST', '/api/settings/dev-mode', { enabled: on });
  expect(res.status, `dev_mode → ${on ? 'вкл' : 'выкл'}`).toBe(200);
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Переход по маршруту оболочки. */
async function gotoHash(page: Page, hash: string): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Хэш адреса страницы. */
function hashOf(page: Page): string {
  return new URL(page.url()).hash;
}

// --- Путь мастера: шаги, применение, проверка IP (задача 2) ---

// Порядок шагов 1–6 ответа мастера (шаг 7 — проверка IP — клиентский)
const STEP_IDS = ['kernel', 'sections', 'nodes', 'routing', 'apply', 'start'] as const;
// Итог проверки IP в шаге 7 (IpCheckResult data-state); значение итога тест не сверяет
const IP_RESULT = /^(proxied|direct_ok|direct_warn|failed)$/;

/** Ветка сценария в аннотации отчёта: план-сессия разбирает, каким путём прошёл тест. */
function branch(description: string): void {
  test.info().annotations.push({ type: 'branch', description });
}

/** Ожидаемый вид строки шага n по ответу сервера: выполнен, текущий или ожидает. */
function expectedStatus(view: WizardView, n: number): string[] {
  const step = view.steps.find((s) => s.id === STEP_IDS[n - 1]);
  if (step?.done) return ['done', 'partial'];
  if (view.current === n) return ['current'];
  return ['pending'];
}

/** Статусы строк шагов 1–6 совпадают с ответом сервера (не с ожидаемым состоянием стенда). */
async function expectStepsMatchServer(page: Page): Promise<WizardView> {
  let last: WizardView | null = null;
  await expect
    .poll(
      async () => {
        const view = await wizard(page);
        last = view;
        const diffs: string[] = [];
        for (let n = 1; n <= 6; n++) {
          const shown =
            (await page.getByTestId(`setup-step-${n}`).getAttribute('data-status')) ?? '';
          const want = expectedStatus(view, n);
          if (!want.includes(shown))
            diffs.push(`setup-step-${n}: ${shown} вместо ${want.join('|')}`);
        }
        return diffs.length === 0 ? 'ok' : diffs.join('; ');
      },
      { message: 'статусы шагов 1–6 против ответа мастера', timeout: T.action }
    )
    .toBe('ok');
  return last as unknown as WizardView;
}

/** Шаги 5 и 6 выполнены: проверка IP доступна. */
function ipReady(view: WizardView): boolean {
  return stepOf(view, 'apply').done && stepOf(view, 'start').done;
}

/** Раскрывает шаг n (если свёрнут) и ждёт видимое тело. */
async function expandStep(page: Page, n: number): Promise<void> {
  const toggle = page.getByTestId(`setup-step-${n}-toggle`);
  await expect(toggle).toBeVisible({ timeout: T.action });
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(page.getByTestId(`setup-step-${n}-body`)).toBeVisible({ timeout: T.action });
}

/** Число изменений черновика по снимку слоя. */
async function draftChanges(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_changes ?? 0);
}

/** Свежая ревизия черновика: каждая правка подготовки идёт со своей. */
async function draftRevision(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_revision ?? 0);
}

/**
 * Итог проверки IP шага 7: при выполненных шагах 5 и 6 — один из четырёх итогов без нажатия
 * (страница проверяет сама, D-09), иначе шаг заперт. Возвращает ветку для аннотации.
 */
async function expectIpOutcome(page: Page, ready: boolean, timeout: number): Promise<string> {
  const box = page.getByTestId('setup-ip-result');
  if (ready) {
    await expect(box, 'setup-ip-result: итог проверки без нажатия').toHaveAttribute(
      'data-state',
      IP_RESULT,
      { timeout }
    );
    return 'result';
  }
  await expect(box, 'setup-ip-result: шаг 7 заперт').toHaveAttribute('data-state', 'locked', {
    timeout: T.action
  });
  return 'locked';
}

// Статус строки шага 7 по итогу проверки IP: выполнен только через VPN или при ожидаемом
// прямом трафике; прямой трафик при пресете «всё через прокси» и сбой — предупреждение (P1)
const IP_STEP_STATUS: Record<string, string> = {
  proxied: 'done',
  direct_ok: 'done',
  direct_warn: 'warning',
  failed: 'warning'
};

/** Строка шага 7 показывает статус, соответствующий итогу проверки IP (data-state). */
async function expectIpStepStatus(page: Page): Promise<void> {
  const state = (await page.getByTestId('setup-ip-result').getAttribute('data-state')) ?? '';
  const want = IP_STEP_STATUS[state];
  expect(want, `setup-ip-result: итог ${state}`).toBeDefined();
  await expect(
    page.getByTestId('setup-step-7'),
    `setup-step-7 при итоге ${state}: ${want}`
  ).toHaveAttribute('data-status', want, { timeout: T.action });
}

/** На экране мастера не больше одной видимой главной кнопки. */
async function expectSinglePrimary(page: Page, where: string): Promise<void> {
  const n = await page.getByTestId('config-setup').locator('.btn-primary:visible').count();
  expect(n, `видимых .btn-primary в мастере (${where})`).toBeLessThanOrEqual(1);
}

/** Кнопка testId — главная (.btn-primary) ровно тогда, когда current ответа мастера равен n. */
async function expectPrimaryWhen(page: Page, testId: string, n: number): Promise<void> {
  await expect
    .poll(
      async () => {
        const current = (await wizard(page)).current;
        const cls = (await page.getByTestId(testId).getAttribute('class')) ?? '';
        const primary = /\bbtn-primary\b/.test(cls);
        return primary === (current === n)
          ? 'ok'
          : `${testId}: primary ${primary}, current ${current}`;
      },
      { message: `${testId}: primary только при current ${n}`, timeout: T.action }
    )
    .toBe('ok');
}

/** Все видимые кнопки .btn внутри testId — не ниже 44 px (цель касания на ≤768 px). */
async function expectTouchTargets(page: Page, testId: string, where: string): Promise<void> {
  const low = await page
    .getByTestId(testId)
    .locator('.btn')
    .evaluateAll((els) =>
      els
        .filter((el) => (el as HTMLElement).offsetParent !== null)
        .map((el, i) => ({
          id: el.getAttribute('data-testid') ?? `btn#${i}`,
          h: Math.round(el.getBoundingClientRect().height)
        }))
        .filter((b) => b.h < 43.5)
    );
  expect(low, `${testId}: кнопки ниже 44 px (${where})`).toEqual([]);
}

/** Каркас страницы мастера: h2 шагов, скрытые маркеры, живые области, зазор шапки. */
async function expectWizardShell(page: Page): Promise<void> {
  const steps = page.getByTestId('setup-steps');
  await expect(steps.getByRole('heading', { level: 2 }), 'заголовки шагов h2').toHaveCount(7);
  await expect(
    steps.locator('.setup-marker:not([aria-hidden="true"])'),
    'маркеры шагов без aria-hidden'
  ).toHaveCount(0);
  await expect(page.getByTestId('setup-live-running')).toHaveAttribute('aria-live', 'polite');
  const ip = page.getByTestId('setup-ip-result');
  expect(await ip.getAttribute('aria-live'), 'setup-ip-result без aria-live').toBeNull();
  const status = page.getByTestId('setup-ip-status');
  await expect(status).toHaveAttribute('role', 'status');
  await expect(status).toHaveAttribute('aria-live', 'polite');
  await expect(
    status.getByTestId('setup-ip-check'),
    'setup-ip-check вне живой области'
  ).toHaveCount(0);
  const gap = await page.evaluate(() => {
    const header = document.querySelector('[data-testid="config-setup"] > .page-header');
    const list = document.querySelector('[data-testid="setup-steps"]');
    if (!header || !list) return -1;
    return list.getBoundingClientRect().top - header.getBoundingClientRect().bottom;
  });
  expect(gap, 'зазор шапка → setup-steps, px').toBeGreaterThanOrEqual(15);
  expect(gap, 'зазор шапка → setup-steps, px').toBeLessThanOrEqual(17);
}

/** Ждёт, пока шаг 6 станет выполнен (ядро после перезапуска), не дольше срока; итог — ответ мастера. */
async function startSettles(page: Page): Promise<WizardView> {
  let view = await wizard(page);
  const until = Date.now() + T.action;
  while (stepOf(view, 'apply').done && !stepOf(view, 'start').done && Date.now() < until) {
    await page.waitForTimeout(1000);
    view = await wizard(page);
  }
  return view;
}

/**
 * Выбор пресета в открытом диалоге: «Выборочно по спискам» (основной трафик стенда остаётся
 * прямым), если карточка неактивна — «Начать с пустого» (только в режиме взятия).
 */
async function pickPreset(page: Page): Promise<string> {
  const modal = page.getByTestId('preset-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('preset-loading')).toHaveCount(0, { timeout: T.page });
  const selective = modal.getByTestId('preset-card-selective');
  const id = (await selective.count()) > 0 && (await selective.isEnabled()) ? 'selective' : 'empty';
  const radio = modal.getByTestId(`preset-card-${id}`);
  await expect(radio, `preset-card-${id}`).toBeEnabled({ timeout: T.action });
  // радио скрыто визуально: нажимается подпись карточки
  await modal.locator('label.pm-card', { has: page.getByTestId(`preset-card-${id}`) }).click();
  await expect(radio).toBeChecked();
  const create = modal.getByTestId('preset-create');
  await expect(create).toBeEnabled({ timeout: T.action });
  await create.click();
  await expect(modal).toBeHidden({ timeout: T.action });
  return id;
}

/**
 * «Применить» шага 5: тот же путь, что в разделе — перехват взятия маршрутизации (если
 * маршрутизацией управляет XKeen и в черновике есть группы или правила), затем окно изменений
 * и «Применить изменения». Проверка черновика нашла проблемы — провал с их кодами.
 */
async function applyFromStep(page: Page): Promise<void> {
  await expandStep(page, 5);
  const btn = page.getByTestId('setup-apply');
  await expect(btn, 'setup-apply активна').toBeEnabled({ timeout: T.action });
  await btn.click();
  const modal = page.getByTestId('apply-preview-modal');
  const nodesOnly = page.getByTestId('take-routing-nodes-only');
  await expect(modal.or(nodesOnly)).toBeVisible({ timeout: T.action });
  if (await nodesOnly.isVisible()) await nodesOnly.click();
  await expect(modal).toBeVisible({ timeout: T.action });
  const confirm = modal.getByTestId('apply-preview-confirm');
  try {
    await expect(confirm).toBeEnabled({ timeout: T.action });
  } catch {
    const pv = await api(page, 'GET', '/api/configlayer/apply/preview');
    const codes = ((pv.data?.issues ?? []) as { code?: string }[]).map((i) => i.code ?? '?');
    throw new Error(
      `apply-preview-confirm неактивна, проблемы проверки черновика: [${codes.join(', ')}]`
    );
  }
  await confirm.click();
  await expect(modal).toHaveCount(0, { timeout: T.save * 2 });
  await waitApplyIdle(page, 'применение из шага 5 мастера');
}

interface MyNode {
  name?: string;
  entry_id?: string;
}

/**
 * Уборка узла теста: черновик сбрасывается, узел «Моих узлов» с именем теста (если он попал
 * в применённое состояние) удаляется и изменение применяется. Имя узла в вывод не попадает.
 */
async function removeMyNode(page: Page, name: string): Promise<void> {
  await waitApplyIdle(page, 'перед уборкой узла теста');
  await resetDraft(page);
  const view = await api(page, 'GET', '/api/configlayer/sources');
  expect(view.status, 'GET /api/configlayer/sources').toBe(200);
  const my = ((view.data?.sources ?? []) as { id: string; nodes?: MyNode[] }[]).find(
    (s) => s.id === 'my'
  );
  const entries = new Set(
    (my?.nodes ?? []).filter((n) => n.name === name && n.entry_id).map((n) => n.entry_id as string)
  );
  for (const entry of entries) {
    const res = await api(page, 'POST', '/api/configlayer/sources/nodes/delete', {
      revision: await draftRevision(page),
      entry_id: entry
    });
    expect(res.status, 'удаление узла теста').toBe(200);
  }
  if ((await draftChanges(page)) > 0) {
    const start = await api(page, 'POST', '/api/configlayer/apply', {});
    expect(start.status, `применение уборки узла теста (${String(start.data?.code ?? '')})`).toBe(
      200
    );
    await waitApplyIdle(page, 'применение уборки узла теста');
    expect(await draftChanges(page), 'после уборки черновик чист').toBe(0);
  }
  await resetDraft(page);
}

/** Ввод узла в поле «Добавить» шага 3 и добавление в черновик. */
async function addNodeInStep(page: Page, link: string): Promise<void> {
  await expandStep(page, 3);
  const body = page.getByTestId('setup-step-3-body');
  await body.locator('#nodes-add-input').fill(link);
  await expect(body.getByTestId('nodes-preview')).toBeVisible({ timeout: T.action });
  const submit = body.getByTestId('nodes-add-submit');
  await expect(submit).toBeEnabled({ timeout: T.action });
  await submit.click();
}

// --- Проход с нуля под снимком панели (clean-install) ---

const PREP = 'подготовка clean-install:';
const SNAP_LABEL = 'xcp-rt-wizard-clean';
// Снимок теста clean-install, ещё не восстановленный: его восстанавливает afterAll, если
// тест упал по тайм-ауту (фикстура page снята, запросы finally не дошли)
let cleanSnap: string | null = null;
// Тест сам восстановил снимок (строка clean-snapshot: restored-in-test уже выведена)
let cleanRestored = false;

interface XraySectionSnap {
  desired: boolean;
  taken: boolean;
}

/** Секции Xray снимка состояния слоя: id → «в черновике» (desired) и «ведёт панель» (taken). */
async function xrayCores(page: Page): Promise<Record<string, XraySectionSnap>> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const cores = (st.data?.cores ?? {}) as {
    xray?: Record<string, { desired?: boolean; taken?: boolean }> | null;
  };
  const out: Record<string, XraySectionSnap> = {};
  for (const [id, s] of Object.entries(cores.xray ?? {})) {
    out[id] = { desired: s?.desired === true, taken: s?.taken === true };
  }
  return out;
}

/** Правка подготовки настоящим маршрутом раздела; отказ — провал с префиксом подготовки. */
async function prepCall(page: Page, what: string, path: string, body: unknown): Promise<void> {
  const res = await api(page, 'POST', path, body);
  if (res.status !== 200) {
    throw new Error(`${PREP} ${what}: HTTP ${res.status} ${String(res.data?.code ?? '')}`);
  }
}

async function routingView(page: Page): Promise<Record<string, unknown>> {
  const res = await api(page, 'GET', '/api/configlayer/routing');
  if (res.status !== 200) throw new Error(`${PREP} GET routing: HTTP ${res.status}`);
  return res.data ?? {};
}

/**
 * Доводит стенд до «чистой установки» (ядра и XKeen стоят, черновик и применённое состояние
 * пусты) при любом исходном владении: правила, «Остальное» → DIRECT, группы, источники,
 * секции Xray в черновике (при любом владельце), маршрутизация — и применение очищенного
 * черновика. Значения секций Mihomo, сохранённые при отпускании маршрутизации, остаются
 * (поведение продукта).
 */
async function prepareClean(page: Page): Promise<void> {
  const base = '/api/configlayer/routing';
  // (1) правила и «Остальное»
  let view = await routingView(page);
  for (const rule of (view.rules ?? []) as { id: string }[]) {
    await prepCall(page, 'routing/rules/delete', `${base}/rules/delete`, {
      revision: await draftRevision(page),
      id: rule.id
    });
  }
  view = await routingView(page);
  if ((view.final as { kind?: string } | undefined)?.kind !== 'direct') {
    await prepCall(page, 'routing/final', `${base}/final`, {
      revision: await draftRevision(page),
      target: { kind: 'direct' }
    });
  }
  // (2) группы
  for (const group of (view.groups ?? []) as { id: string }[]) {
    await prepCall(page, 'routing/groups/delete', `${base}/groups/delete`, {
      revision: await draftRevision(page),
      id: group.id,
      cascade: { mode: 'drop' }
    });
  }
  // (3) источники: подписки удаляются целиком, «Мои узлы» — по узлу
  for (const source of await listSources(page)) {
    if (source.id !== 'my') {
      await prepCall(page, 'sources/delete', '/api/configlayer/sources/delete', {
        revision: await draftRevision(page),
        id: source.id
      });
    }
  }
  const sources = await api(page, 'GET', '/api/configlayer/sources');
  const my = ((sources.data?.sources ?? []) as { id: string; nodes?: MyNode[] }[]).find(
    (s) => s.id === 'my'
  );
  const entries = new Set((my?.nodes ?? []).map((n) => n.entry_id).filter((e): e is string => !!e));
  for (const entry of entries) {
    await prepCall(page, 'sources/nodes/delete', '/api/configlayer/sources/nodes/delete', {
      revision: await draftRevision(page),
      entry_id: entry
    });
  }
  // (4) секции Xray в черновике — по признаку desired, не по владельцу (drift тоже)
  for (const [id, s] of Object.entries(await xrayCores(page))) {
    if (!s.desired) continue;
    await prepCall(page, `cores/release ${id}`, '/api/configlayer/cores/release', {
      revision: await draftRevision(page),
      kernel: 'xray',
      section: id
    });
  }
  const stillDesired = Object.entries(await xrayCores(page))
    .filter(([, s]) => s.desired)
    .map(([id]) => id);
  if (stillDesired.length > 0) {
    throw new Error(`${PREP} секции Xray остались в черновике: ${stillDesired.join(', ')}`);
  }
  // (5) маршрутизация у панели хотя бы для одного ядра
  const st = await api(page, 'GET', '/api/configlayer/state');
  if ((st.data?.routing as { desired?: boolean } | undefined)?.desired === true) {
    await prepCall(page, 'routing/release', `${base}/release`, {
      revision: await draftRevision(page)
    });
  }
  // (6) применение очищенного черновика
  const start = await api(page, 'POST', '/api/configlayer/apply', {});
  if (start.status !== 200) {
    const issues = ((start.data?.issues ?? []) as { code?: string }[]).map((i) => i.code ?? '?');
    throw new Error(
      `${PREP} apply: HTTP ${start.status} ${String(start.data?.code ?? '')}` +
        (issues.length > 0 ? ` [${issues.join(', ')}]` : '')
    );
  }
  await waitApplyIdle(page, 'применение очищенного черновика');
  const after = await api(page, 'GET', '/api/configlayer/state');
  const result = ((after.data?.apply ?? {}) as { result?: { ok?: boolean; code?: string } }).result;
  const changes = Number(after.data?.draft_changes ?? -1);
  if (changes !== 0 || result?.ok !== true) {
    throw new Error(
      `${PREP} применение очищенного черновика: draft_changes ${changes}, result.code ${result?.code ?? ''}`
    );
  }
}

/**
 * Восстанавливает снимок панели: под замком жизненного цикла, повтор при 409
 * kernel_op_in_progress (до трёх попыток через 5 с). Неудача — исключение, снимок остаётся.
 * Успех — простой конвейера, сброс черновика и удаление снимка.
 */
async function restoreSnapshot(page: Page, id: string): Promise<void> {
  await waitApplyIdle(page, 'перед восстановлением снимка');
  let res = await api(page, 'POST', `/api/snapshots/${id}/restore`, {});
  for (
    let attempt = 1;
    attempt < 3 && res.status === 409 && res.data?.code === 'kernel_op_in_progress';
    attempt++
  ) {
    await page.waitForTimeout(5000);
    res = await api(page, 'POST', `/api/snapshots/${id}/restore`, {});
  }
  if (res.status !== 200) {
    throw new Error(
      `снимок ${id} не восстановлен (HTTP ${res.status} ${String(res.data?.code ?? '')}), восстановить вручную`
    );
  }
  await waitApplyIdle(page, 'после восстановления снимка');
  await resetDraft(page);
  const del = await api(page, 'POST', `/api/snapshots/${id}/delete`, {});
  expect(del.status, `удаление снимка ${id}`).toBe(200);
}

/**
 * Перезагрузка страницы на маршруте с заменой сборщика консоли: нарушения проверяются до
 * перезагрузки (она обрывает запросы прежнего документа), новый сборщик — после ответа мастера.
 */
async function reloadAt(page: Page, guard: ConsoleGuard, hash: string): Promise<ConsoleGuard> {
  await guard.assertClean();
  await gotoHash(page, hash);
  const loaded = page.waitForResponse(
    (r) => r.request().method() === 'GET' && new URL(r.url()).pathname === WIZARD,
    { timeout: T.page }
  );
  await page.reload({ timeout: T.page, waitUntil: 'domcontentloaded' });
  await loaded;
  return attachConsoleGuard(page);
}

// Прежние значения стенда: null — ещё не прочитаны (beforeAll не дошёл), возвращать нечего
let prevStatus: string | null = null;
let devWasOn: boolean | null = null;

test.beforeAll(async ({ browser }) => {
  test.setTimeout(T.save * 4);
  await layer.enable(browser);
  await withPanel(browser, async (page) => {
    const settings = await api(page, 'GET', '/api/settings');
    expect(settings.status, 'GET /api/settings').toBe(200);
    devWasOn = settings.data?.dev_mode === true;
    prevStatus = (await wizard(page)).status;
    // сброс признака мастера разрешён только в режиме разработчика
    if (!devWasOn) await setDevMode(page, true);
  });
});

test.afterAll(async ({ browser }) => {
  // Уцелевший снимок clean-install восстанавливается первым, до прочей уборки
  test.setTimeout(cleanSnap !== null ? T.save * 8 : T.save * 4);
  // Уборка идёт до конца при любом сбое шага: первая ошибка — после всех шагов
  const errors: unknown[] = [];
  const attempt = async (fn: () => Promise<void>): Promise<void> => {
    try {
      await fn();
    } catch (err) {
      errors.push(err);
    }
  };
  let snapLeft: Error | null = null;
  if (cleanSnap !== null) {
    // тест упал по тайм-ауту: фикстура page снята, восстановление — свежим контекстом
    const id = cleanSnap;
    await attempt(() =>
      withPanel(browser, async (page) => {
        await restoreSnapshot(page, id);
        cleanSnap = null;
        console.log('clean-snapshot: restored-in-hook');
      })
    );
    if (cleanSnap !== null) {
      console.log(`clean-snapshot: left id=${id}`);
      snapLeft = new Error(
        `clean-snapshot: left id=${id} — снимок не восстановлен, восстановить вручную`
      );
    }
  } else if (!cleanRestored) {
    console.log('clean-snapshot: none');
  }
  await attempt(() =>
    withPanel(browser, async (page) => {
      await waitApplyIdle(page, 'перед возвратом статуса мастера');
      if (prevStatus !== null) await restoreStatus(page, prevStatus);
      const status = (await wizard(page)).status;
      console.log(`wizard-status: before=${prevStatus ?? ''} after=${status}`);
    })
  );
  await attempt(async () => {
    if (devWasOn === false) await withPanel(browser, (page) => setDevMode(page, false));
  });
  await attempt(() => layer.restore(browser));
  if (snapLeft !== null) throw snapLeft;
  if (errors.length > 0) throw errors[0];
});

test('config-setup dashboard-card vp:1280', async ({ page, browser }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await startPage(page);
  // Черновик открыт, шаг 5 не выполнен: карточка видна при любом состоянии шагов 1–6
  await holdDraft(page);
  try {
    await wizardReset(page);
    const guard = attachConsoleGuard(page);

    await gotoHash(page, '#/dashboard');
    const card = page.getByTestId('setup-checklist-card');
    await expect(card).toBeVisible({ timeout: T.page });
    // карточка Mihomo при флаге слоя не показывается
    await expect(page.locator('.quickstart')).toHaveCount(0);
    await expect(card.locator('[data-testid^="setup-card-step-"]')).toHaveCount(6);
    for (let n = 1; n <= 6; n++) {
      await expect(page.getByTestId(`setup-card-step-${n}`)).toBeVisible();
    }
    // счётчик «N из 6» совпадает с done_count ответа сервера (карточка перечитывает GET по событию)
    await expect
      .poll(
        async () => {
          const text = (await page.getByTestId('setup-card-progress').textContent()) ?? '';
          const m = PROGRESS.exec(text);
          if (!m) return 'setup-card-progress: формат не «N из M»';
          const view = await wizard(page);
          const shown = `${m[1]}/${m[2]}`;
          const server = `${view.done_count}/${view.total}`;
          return shown === server ? 'ok' : `карточка ${shown}, сервер ${server}`;
        },
        { message: 'setup-card-progress против done_count', timeout: T.action }
      )
      .toBe('ok');

    // «Продолжить настройку» открывает мастер на первом невыполненном шаге
    await page.getByTestId('setup-card-continue').click();
    await expect(page).toHaveURL(/#\/config\/setup$/, { timeout: T.action });
    await expect(page.getByTestId('config-setup')).toBeVisible({ timeout: T.page });
    const current = (await wizard(page)).current;
    expect(current, 'current ответа мастера при открытом черновике').toBeGreaterThan(0);
    await expect(page.getByTestId(`setup-step-${current}`)).toHaveAttribute(
      'data-status',
      'current',
      { timeout: T.action }
    );
    await expect(page.getByTestId(`setup-step-${current}-body`)).toBeVisible();

    // 390 px: без горизонтальной прокрутки, счётчик в одну строку (допущение E9 UI-SPEC)
    await page.setViewportSize({ width: 390, height: 844 });
    await gotoHash(page, '#/dashboard');
    await expect(card).toBeVisible({ timeout: T.page });
    const pageScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(pageScroll, 'горизонтальная прокрутка дашборда на 390 px').toBeLessThanOrEqual(0);
    const counter = await page.getByTestId('setup-card-progress').evaluate((el) => {
      const cs = getComputedStyle(el);
      const lh = parseFloat(cs.lineHeight);
      return {
        height: el.getBoundingClientRect().height,
        line: Number.isFinite(lh) ? lh : parseFloat(cs.fontSize) * 1.2
      };
    });
    expect(counter.height, 'setup-card-progress в одну строку на 390 px').toBeLessThanOrEqual(
      counter.line * 1.5
    );
    // Кнопки карточки — цель касания 44 px; кольцо невыполненного шага 16 px, как в мастере
    await expectTouchTargets(page, 'setup-checklist-card', 'карточка 390');
    if ((await card.locator('.sc-pending').count()) > 0) {
      const box = (await card.locator('.sc-pending').first().boundingBox()) ?? {
        width: 0,
        height: 0
      };
      for (const side of [box.width, box.height]) {
        expect(side, '.sc-pending: сторона кольца, px').toBeGreaterThanOrEqual(15.5);
        expect(side, '.sc-pending: сторона кольца, px').toBeLessThanOrEqual(16.5);
      }
    }

    // «Скрыть»: подтверждение, карточки нет ни здесь, ни во второй вкладке, статус hidden
    await page.getByTestId('setup-card-hide').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: T.action });
    await dialog.locator('.confirm-actions button').last().click();
    await expect(card).toHaveCount(0, { timeout: T.action });
    await withPanel(browser, async (second) => {
      await gotoHash(second, '#/dashboard');
      const loaded = second.waitForResponse(
        (r) => r.request().method() === 'GET' && new URL(r.url()).pathname === WIZARD,
        { timeout: T.page }
      );
      await second.reload({ timeout: T.page, waitUntil: 'domcontentloaded' });
      await loaded;
      await second.waitForTimeout(500);
      await expect(second.getByTestId('setup-checklist-card')).toHaveCount(0);
    });
    expect((await wizard(page)).status, 'статус мастера после «Скрыть»').toBe('hidden');

    await guard.assertClean();
  } finally {
    // черновик ещё открыт: сброс признака не даст наблюдателю поставить done
    await wizardReset(page);
    await resetDraft(page);
  }
});

test('config-setup close-no-auto-open vp:1280', async ({ page, browser }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await startPage(page);
  await holdDraft(page);
  try {
    const view = await wizardReset(page);
    // в черновике есть источник: мастер сам не открывается (149 D-22, D-12)
    expect(
      { empty: view.empty, auto_open: view.auto_open },
      'предусловие: непустой черновик'
    ).toEqual({ empty: false, auto_open: false });
    const guard = attachConsoleGuard(page);

    await gotoHash(page, '#/config');
    await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.page });
    await page.waitForTimeout(3000);
    await expect(page.getByTestId('config-setup')).toHaveCount(0);
    expect(hashOf(page), 'вход в раздел не уводит в мастер').not.toBe('#/config/setup');

    // ссылка «Мастер настройки» в шапке раздела (D-13)
    const link = page.getByTestId('config-setup-link');
    await expect(link).toBeVisible({ timeout: T.action });
    await link.click();
    await expect(page).toHaveURL(/#\/config\/setup$/, { timeout: T.action });
    await expect(page.getByTestId('config-setup')).toBeVisible({ timeout: T.page });
    // шаг 5 не выполнен — проверки IP нет
    await expect(page.getByTestId('setup-step-7')).toHaveAttribute('data-status', 'locked', {
      timeout: T.page
    });
    await expectWizardShell(page);
    // запертый шаг 7 называет причину: черновик удержан — шаг 5 не выполнен
    await expect(page.getByTestId('setup-ip-result')).toHaveAttribute('data-lock', 'apply');
    // шаги 1–6 не выполнены — «Продолжить позже» не главная
    await expect(page.getByTestId('setup-finish')).not.toHaveClass(/\bbtn-primary\b/);
    await expandStep(page, 5);
    // «Применить» — главная только у текущего шага (current ответа мастера равен 5)
    if ((await page.getByTestId('setup-apply').count()) > 0) {
      await expectPrimaryWhen(page, 'setup-apply', 5);
    }
    await expectSinglePrimary(page, 'шаг 5 раскрыт');

    // Цели касания 44 px на 768 и 390 px, без горизонтальной прокрутки на телефоне
    await page.setViewportSize({ width: 768, height: 1024 });
    await expectTouchTargets(page, 'config-setup', '768');
    await page.setViewportSize({ width: 390, height: 844 });
    await expectTouchTargets(page, 'config-setup', '390');
    const setupScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(setupScroll, 'горизонтальная прокрутка мастера на 390 px').toBeLessThanOrEqual(0);
    await page.setViewportSize({ width: 1280, height: 900 });

    // «Закрыть» уводит в «Узлы» и ставит closed на сервере (D-10)
    await page.getByTestId('setup-close').click();
    await expect(page).toHaveURL(/#\/config\/nodes$/, { timeout: T.action });
    expect((await wizard(page)).status, 'статус мастера после «Закрыть»').toBe('closed');
    await withPanel(browser, async (second) => {
      await gotoHash(second, '#/config');
      await expect(second.getByTestId('nodes-tab')).toBeVisible({ timeout: T.page });
      await second.waitForTimeout(3000);
      await expect(second.getByTestId('config-setup')).toHaveCount(0);
      expect(hashOf(second), 'вторая вкладка не уводит в мастер').not.toBe('#/config/setup');
    });

    await guard.assertClean();
  } finally {
    await wizardReset(page);
    await resetDraft(page);
  }
});

test(
  'config-setup wizard-flow vp:1280',
  {
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'применение с перезапуском ядра и два ожидания итога проверки IP из браузера'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 6);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    await waitApplyIdle(page, 'перед проходом мастера');
    await resetDraft(page);
    const nodeName = `RtWizFlow${randomPort()}`;
    try {
      const w0 = await wizard(page);
      const readyAtOpen = ipReady(w0);
      const guard = attachConsoleGuard(page);

      await gotoHash(page, '#/config/setup');
      await expect(page.getByTestId('config-setup')).toBeVisible({ timeout: T.page });
      const rows = page
        .getByTestId('setup-steps')
        .locator('[data-testid^="setup-step-"][data-status]');
      await expect(rows).toHaveCount(7, {
        timeout: T.page
      });
      await expectStepsMatchServer(page);

      // Шаг 7 при открытии (D-09): при выполненных шагах 5 и 6 итог без нажатия, иначе заперт
      const ipOpen = await expectIpOutcome(page, readyAtOpen, 20_000);
      branch(`ip-open: ${ipOpen}`);
      if (ipOpen === 'result') await expectIpStepStatus(page);

      // Шаг 2: диалоги взятия открываются из шага и закрываются без изменений черновика
      await expandStep(page, 2);
      const before = await draftChanges(page);
      if ((await page.getByTestId('setup-take-xray').count()) > 0) {
        await page.getByTestId('setup-take-xray').click();
        await expect(page.getByTestId('take-section-content')).toBeVisible({ timeout: T.action });
        await page.getByTestId('take-section-cancel').click();
        await expect(page.getByTestId('take-section-content')).toHaveCount(0, {
          timeout: T.action
        });
        expect(await draftChanges(page), 'отмена диалога секций Xray: черновик прежний').toBe(
          before
        );
      }
      await expandStep(page, 2);
      if ((await page.getByTestId('setup-take-mihomo').count()) > 0) {
        await page.getByTestId('setup-take-mihomo').click();
        await expect(page.getByTestId('take-routing-content')).toBeVisible({ timeout: T.action });
        await page.getByTestId('take-routing-cancel').click();
        await expect(page.getByTestId('take-routing-content')).toHaveCount(0, {
          timeout: T.action
        });
        expect(await draftChanges(page), 'отмена диалога профиля Mihomo: черновик прежний').toBe(
          before
        );
      }
      // Шаг 4: диалог пресета — только в режиме пресета (маршрутизация взята): закрытие ничего не пишет
      if (stepOf(await wizard(page), 'routing').taken === true) {
        await expandStep(page, 4);
        await page.getByTestId('setup-preset-open').click();
        const preset = page.getByTestId('preset-modal');
        await expect(preset).toBeVisible({ timeout: T.action });
        await preset.getByTestId('preset-cancel').click();
        await expect(preset).toBeHidden({ timeout: T.action });
        expect(await draftChanges(page), 'отмена диалога пресета: черновик прежний').toBe(before);
      }

      // Шаг 3: «Добавить» одним полем
      await addNodeInStep(page, nodeLink('socks.txt', randomPort(), nodeName));
      await expect
        .poll(async () => stepOf(await wizard(page), 'nodes').done, {
          message: 'шаг nodes выполнен после «Добавить»',
          timeout: T.action
        })
        .toBe(true);
      await expandStep(page, 3);
      await expect(page.getByTestId('setup-nodes-count')).toBeVisible({ timeout: T.action });
      // число узлов — моноширинным, отдельно от подписи
      const nodesMono = page.getByTestId('setup-nodes-count').locator('.setup-mono');
      await expect(nodesMono, 'setup-nodes-count: .setup-mono').toHaveCount(1);
      const fonts = await page.getByTestId('setup-nodes-count').evaluate((el) => ({
        text: getComputedStyle(el).fontFamily,
        value: getComputedStyle(el.querySelector('.setup-mono') as Element).fontFamily
      }));
      expect(fonts.value, 'setup-nodes-count: шрифт числа отличается от подписи').not.toBe(
        fonts.text
      );
      await expect(page.getByTestId('setup-step-5')).not.toHaveAttribute('data-status', 'done', {
        timeout: T.action
      });

      // Новая жизнь страницы мастера без перезагрузки документа: шаг 5 не выполнен — проверки нет
      await page.evaluate(() => {
        (window as Window & { __wizLife?: boolean }).__wizLife = true;
        location.hash = '#/config/nodes';
      });
      await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.page });
      await expect(page.getByTestId('config-setup')).toHaveCount(0);
      await page.evaluate(() => {
        location.hash = '#/config/setup';
      });
      await expect(page.getByTestId('config-setup')).toBeVisible({ timeout: T.page });
      expect(
        await page.evaluate(() => (window as Window & { __wizLife?: boolean }).__wizLife === true),
        'документ не перезагружался'
      ).toBe(true);
      await expect(page.getByTestId('setup-ip-result')).toHaveAttribute('data-state', 'locked', {
        timeout: T.action
      });
      await expect(page.getByTestId('setup-ip-result')).toHaveAttribute('data-lock', 'apply');

      // Шаг 5: то же окно изменений, что в разделе
      await applyFromStep(page);
      await expect
        .poll(
          async () => {
            const shown = await page.getByTestId('setup-step-5').getAttribute('data-status');
            const done = stepOf(await wizard(page), 'apply').done;
            return `setup-step-5 ${shown}, apply.done ${done}`;
          },
          { message: 'шаг 5 выполнен после применения', timeout: T.save }
        )
        .toBe('setup-step-5 done, apply.done true');
      await expect(page.getByTestId('config-draftbar')).toHaveCount(0);

      // Шаг 6: статус по серверу, запуск не нажимается (FND-05)
      const after = await startSettles(page);
      await expectStepsMatchServer(page);

      // Шаг 7 после применения: переход, пауза 3–6 с и опрос до трёх сервисов — до 45 с
      const ipApply = await expectIpOutcome(page, ipReady(after), 45_000);
      branch(`ip-apply: ${ipApply}`);
      if (ipApply === 'result') await expectIpStepStatus(page);
      await expectSinglePrimary(page, 'после применения');
      // «Готово» — главная только при выполненных шагах 1–6 (current 0) и свёрнутом шаге 3
      if (
        (await page.getByTestId('setup-step-3-toggle').getAttribute('aria-expanded')) !== 'true'
      ) {
        await expectPrimaryWhen(page, 'setup-finish', 0);
      }

      await expect(page.getByTestId('setup-finish')).toBeVisible();
      await expect(page.getByTestId('setup-finish')).toBeEnabled();
      await guard.assertClean();
    } finally {
      await removeMyNode(page, nodeName);
    }
  }
);

test(
  'config-setup clean-install vp:1280',
  {
    annotation: {
      type: SLOW_ANNOTATION,
      description:
        'снимок панели, применение очищенного черновика, проход мастера с нуля с применением и восстановление снимка — три перезапуска ядра'
    }
  },
  async ({ page, browser }) => {
    // Три прогона в сессии 149.1-09: поток прошёл целиком, затем 10-секундные ожидания
    // упёрлись в медленную панель на разных шагах («Закрыть», отпускание секции).
    quarantine('wizard-clean-install-slow-panel-timeouts');
    test.setTimeout(T.save * 8);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    const w0 = await wizard(page);
    expect(stepOf(w0, 'kernel').done, 'предусловие: XKeen и ядро стоят на стенде').toBe(true);
    await waitApplyIdle(page, 'перед снимком');
    await resetDraft(page);
    const prev = (await wizard(page)).status;

    const snap = await api(page, 'POST', '/api/snapshots/create', { label: SNAP_LABEL });
    expect(snap.status, 'POST /api/snapshots/create').toBe(200);
    const id = String(snap.data?.id ?? '');
    expect(id, 'id снимка').not.toBe('');
    cleanSnap = id;

    let primary: unknown;
    try {
      await cleanInstallPass(page, browser);
    } catch (err) {
      primary = err;
    }

    // Восстановление снимка при любом исходе; неудача оставляет снимок для afterAll
    let restoreError: unknown;
    try {
      await restoreSnapshot(page, id);
      cleanSnap = null;
      cleanRestored = true;
      console.log('clean-snapshot: restored-in-test');
    } catch (err) {
      restoreError = err;
    }
    let statusError: unknown;
    if (restoreError === undefined) {
      // Снимок хранит и признак мастера; при пустом исходном и выполненных 1–6 наблюдатель
      // ставит done между восстановлением и проверкой
      try {
        await expect
          .poll(
            async () => {
              const v = await wizard(page);
              const ok =
                v.status === prev || (prev === '' && v.done_count === 6 && v.status === 'done');
              return ok ? 'ok' : `статус ${v.status}, было ${prev}, выполнено ${v.done_count}`;
            },
            { message: 'статус мастера после восстановления снимка', timeout: T.action }
          )
          .toBe('ok');
      } catch (err) {
        statusError = err;
      }
    }
    if (primary !== undefined) throw primary;
    if (restoreError !== undefined) throw restoreError;
    if (statusError !== undefined) throw statusError;
  }
);

/**
 * Тело clean-install под снимком: подготовка до чистой установки, затем проход мастера с нуля
 * (автооткрытие, «Закрыть», возврат ссылкой, взятие, «Добавить», пресет, «Применить», итог IP,
 * «пройдено») и «Взять остальные» для отпущенной в черновике секции Xray.
 */
async function cleanInstallPass(page: Page, browser: Browser): Promise<void> {
  // --- подготовка: стенд доводится до чистой установки ---
  await prepareClean(page);
  const w = await wizardReset(page);
  const applyStep = stepOf(w, 'apply');
  if (!w.empty || !w.auto_open || applyStep.applied_empty !== true) {
    throw new Error(
      `${PREP} состояние не пустое: empty ${w.empty}, auto_open ${w.auto_open}, applied_empty ${String(applyStep.applied_empty)}, выполнено шагов ${w.done_count}`
    );
  }
  const held = Object.entries(await xrayCores(page))
    .filter(([, s]) => s.desired || s.taken)
    .map(([sid]) => sid);
  if (held.length > 0) {
    throw new Error(`${PREP} секции Xray под панелью или в черновике: ${held.join(', ')}`);
  }
  if (w.kernels.xray) {
    const cores = await api(page, 'GET', '/api/configlayer/cores?kernel=xray');
    if (cores.status !== 200 || cores.data?.take_all !== true) {
      throw new Error(
        `${PREP} у Xray нет секции, которую можно взять: HTTP ${cores.status}, take_all ${String(cores.data?.take_all)}`
      );
    }
  }

  // --- проход мастера с нуля ---
  let guard = attachConsoleGuard(page);
  await gotoHash(page, '#/config');
  // пустое состояние: мастер открывается сам при входе в раздел (149 D-22)
  await expect(page).toHaveURL(/#\/config\/setup$/, { timeout: T.page });
  await expect(page.getByTestId('config-setup')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('setup-step-2')).toHaveAttribute('data-status', 'current', {
    timeout: T.page
  });
  for (const n of [3, 4, 5]) {
    await expect(page.getByTestId(`setup-step-${n}`)).toHaveAttribute('data-status', 'pending');
  }
  await expect(page.getByTestId('setup-step-7')).toHaveAttribute('data-status', 'locked');
  await expect(page.getByTestId('setup-ip-result')).toHaveAttribute('data-state', 'locked');

  // «Закрыть» отменяет автооткрытие на том же пустом черновике (D-10)
  await page.getByTestId('setup-close').click();
  await expect(page).toHaveURL(/#\/config\/nodes$/, { timeout: T.action });
  const closed = await wizard(page);
  expect(
    { status: closed.status, empty: closed.empty, auto_open: closed.auto_open },
    'мастер после «Закрыть»'
  ).toEqual({ status: 'closed', empty: true, auto_open: false });
  await withPanel(browser, async (second) => {
    await gotoHash(second, '#/config');
    await expect(second.getByTestId('nodes-tab')).toBeVisible({ timeout: T.page });
    await second.waitForTimeout(3000);
    await expect(second.getByTestId('config-setup')).toHaveCount(0);
    expect(hashOf(second), 'вторая вкладка остаётся в «Узлах»').not.toBe('#/config/setup');
  });
  // возврат ссылкой раздела (D-13)
  await page.getByTestId('config-setup-link').click();
  await expect(page).toHaveURL(/#\/config\/setup$/, { timeout: T.action });
  await expect(page.getByTestId('config-setup')).toBeVisible({ timeout: T.page });

  // Шаг 2: те же диалоги взятия, что в разделе (147 D-01)
  if (w.kernels.xray) {
    await expandStep(page, 2);
    await page.getByTestId('setup-take-xray').click();
    const content = page.getByTestId('take-section-content');
    await expect(content).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('take-section-loading')).toHaveCount(0, { timeout: T.page });
    const ack = page.getByTestId('take-section-comments-ack');
    if (await ack.isVisible()) await ack.check();
    await expect(page.getByTestId('take-section-confirm')).toBeEnabled({ timeout: T.action });
    await page.getByTestId('take-section-confirm').click();
    await expect(content).toHaveCount(0, { timeout: T.action });
  }
  if (w.kernels.mihomo) {
    await expandStep(page, 2);
    await page.getByTestId('setup-take-mihomo').click();
    const content = page.getByTestId('take-routing-content');
    await expect(content).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('take-routing-confirm')).toBeEnabled({ timeout: T.action });
    await page.getByTestId('take-routing-confirm').click();
    // правил после подготовки нет: диалог сам переходит в пресет (B3)
    const preset = page.getByTestId('preset-modal');
    await expect
      .poll(
        async () => {
          if (await preset.isVisible()) return 'preset';
          return (await content.count()) === 0 ? 'closed' : 'wait';
        },
        { message: 'take-routing-confirm: пресет или закрытие', timeout: T.action }
      )
      .not.toBe('wait');
    if (await preset.isVisible()) branch(`mihomo-preset: ${await pickPreset(page)}`);
    await expect(content).toHaveCount(0, { timeout: T.action });
  }
  await expect
    .poll(async () => stepOf(await wizard(page), 'sections').done, {
      message: 'шаг sections выполнен',
      timeout: T.action
    })
    .toBe(true);

  // Шаг 3: узел одним полем
  await addNodeInStep(page, nodeLink('vless_ws.txt', randomPort(), `RtWizClean${randomPort()}`));
  await expect
    .poll(async () => stepOf(await wizard(page), 'nodes').done, {
      message: 'шаг nodes выполнен',
      timeout: T.action
    })
    .toBe(true);

  // Шаг 4, если ещё не выполнен: пресет
  if (!stepOf(await wizard(page), 'routing').done) {
    await expandStep(page, 4);
    await page.getByTestId('setup-preset-open').click();
    branch(`routing-preset: ${await pickPreset(page)}`);
    await expect
      .poll(async () => stepOf(await wizard(page), 'routing').done, {
        message: 'шаг routing выполнен',
        timeout: T.action
      })
      .toBe(true);
  }

  // Шаг 5: в черновике только то, что собрал мастер; провал проверки — находка мастера
  await applyFromStep(page);
  const st = await api(page, 'GET', '/api/configlayer/state');
  const result = ((st.data?.apply ?? {}) as { result?: { ok?: boolean; code?: string } }).result;
  expect(result?.ok, `итог применения мастера (${result?.code ?? ''})`).toBe(true);
  await expect
    .poll(async () => stepOf(await wizard(page), 'apply').done, {
      message: 'шаг apply выполнен',
      timeout: T.save
    })
    .toBe(true);

  // Шаги 6 и 7 — как в wizard-flow после применения
  const after = await startSettles(page);
  await expectStepsMatchServer(page);
  branch(`ip-apply: ${await expectIpOutcome(page, ipReady(after), 45_000)}`);

  // «Пройдено» ставит сервер при выполненных 1–6 (D-06): карточка исчезает
  const doneCount = (await wizard(page)).done_count;
  if (doneCount === 6) {
    await expect
      .poll(async () => (await wizard(page)).status, {
        message: 'статус мастера при выполненных шагах 1–6',
        timeout: 25_000
      })
      .toBe('done');
    guard = await reloadAt(page, guard, '#/dashboard');
    await page.waitForTimeout(500);
    await expect(page.getByTestId('setup-checklist-card')).toHaveCount(0);
    branch('done: stamped');
  } else {
    branch(`done: no, ${doneCount} of 6`);
  }

  // Отпущенная в черновике секция берётся снова «Взять остальные» (план 149.1-07)
  if (w.kernels.xray) {
    await takeRestReleasing(page);
  } else {
    branch('releasing: no-xray');
  }

  await guard.assertClean();
}

/**
 * Ветка отпущенной секции: мастер взял все секции Xray; первая взятая и применённая секция
 * отпускается в черновике, «Взять остальные» открывает непустой диалог со строкой
 * take-section-releasing без обещания пресета и берёт её снова. В сообщениях — id секции,
 * признаки desired/taken и счётчики.
 */
async function takeRestReleasing(page: Page): Promise<void> {
  const p0 = stepOf(await wizard(page), 'sections').partial;
  expect(!p0 || p0.done >= p0.total, 'шаг 2 без «частично»: мастер взял все секции').toBe(true);
  const cores = await xrayCores(page);
  const sid = Object.keys(cores).find((k) => cores[k].desired && cores[k].taken) ?? '';
  expect(sid, 'секция Xray, взятая мастером и применённая').not.toBe('');
  const rel = await api(page, 'POST', '/api/configlayer/cores/release', {
    revision: await draftRevision(page),
    kernel: 'xray',
    section: sid
  });
  expect(rel.status, `cores/release ${sid}`).toBe(200);
  await expect
    .poll(
      async () => {
        const c = (await xrayCores(page))[sid];
        const p = stepOf(await wizard(page), 'sections').partial;
        return `desired ${String(c?.desired)}, taken ${String(c?.taken)}, не взято ${p ? p.total - p.done : 0}`;
      },
      { message: `секция ${sid} отпущена в черновике`, timeout: T.action }
    )
    .toBe('desired false, taken true, не взято 1');

  await gotoHash(page, '#/config/setup');
  await expect(page.getByTestId('config-setup')).toBeVisible({ timeout: T.page });
  // кнопка в строке шага видна и в свёрнутом виде
  const rest = page.getByTestId('setup-take-rest');
  await expect(rest).toBeVisible({ timeout: T.page });
  await rest.click();
  const content = page.getByTestId('take-section-content');
  await expect(content).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('take-section-loading')).toHaveCount(0, { timeout: T.page });
  const releasing = content.getByTestId('take-section-releasing');
  await expect(releasing, 'take-section-releasing: одна строка').toHaveCount(1);
  await expect(releasing).toHaveAttribute('data-section', sid);
  await expect(releasing).toBeVisible();
  expect(
    ((await releasing.textContent()) ?? '').trim().length,
    'take-section-releasing: тело диалога не пустое'
  ).toBeGreaterThan(0);
  // предпросмотр не обещает отпущенной секции пресет
  await expect(content.getByTestId('take-section-nofile')).toHaveCount(0);
  await expect(page.getByTestId('take-section-confirm')).toBeEnabled({ timeout: T.action });
  await page.getByTestId('take-section-confirm').click();
  await expect(content).toHaveCount(0, { timeout: T.action });
  await expect
    .poll(
      async () => {
        const c = (await xrayCores(page))[sid];
        const p = stepOf(await wizard(page), 'sections').partial;
        return `desired ${String(c?.desired)}, не взято ${p ? p.total - p.done : 0}`;
      },
      { message: `секция ${sid} снова в черновике`, timeout: T.action }
    )
    .toBe('desired true, не взято 0');
  await resetDraft(page);
}
