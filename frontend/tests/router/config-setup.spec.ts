// e2e-pages: config/setup config dashboard
// router-area: configlayer
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import {
  addToDraft,
  api,
  layerFlag,
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
  const d = res.data ?? {};
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
 * Снимает признак мастера (нужен dev_mode). Тик наблюдателя, прочитавший факты до правки
 * черновика, мог успеть записать done — тогда сброс повторяется один раз; после него статус
 * обязан быть пустым.
 */
async function wizardReset(page: Page): Promise<WizardView> {
  for (let attempt = 0; ; attempt++) {
    const res = await api(page, 'POST', `${WIZARD}/reset`);
    expect(res.status, `POST ${WIZARD}/reset (${String(res.data?.code ?? '')})`).toBe(200);
    const view = await wizard(page);
    if (view.status === '' || attempt >= 1) {
      expect(view.status, 'статус мастера после сброса').toBe('');
      return view;
    }
  }
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
  test.setTimeout(T.save * 4);
  // Уборка идёт до конца при любом сбое шага: первая ошибка — после всех шагов
  const errors: unknown[] = [];
  const attempt = async (fn: () => Promise<void>): Promise<void> => {
    try {
      await fn();
    } catch (err) {
      errors.push(err);
    }
  };
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
