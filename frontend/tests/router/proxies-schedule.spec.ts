// e2e-pages: proxies traffic
// router-area: routing
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { test, expect, type Browser, type Page } from '@playwright/test';
import { axeSevere } from './lib/axe';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { SLOW_ANNOTATION } from './lib/known';
import { escapeRe } from './lib/regex';
import {
  addToDraft,
  api,
  nodeLink,
  randomPort,
  resetDraft,
  waitApplyIdle,
  withPanel
} from './lib/nodes-api';
import {
  myEntryIds,
  restoreRouting,
  routingInfo,
  takeAndApply,
  type RoutingStand
} from './lib/routing-api';

// Расписание в карточке группы на «Прокси» (фаза 152, SPX-02, D-04, D-08, D-10, D-11, D-17):
// пустое состояние блока «Расписание», окно, покрывающее время роутера, строка «По расписанию
// до», конфликт окон, подсказка окна через полночь, удаление с подтверждением, перехват выбором
// другого узла и «Вернуть расписание», переход «Задать лимит» на «Трафик → Лимиты» и axe блока и
// модалки в обеих темах. Подготовка одна на спек: два узла «Моих узлов» на портах, где сервер не
// слушает, ручная группа и группа «авто», взятие маршрутизации и «Применить». Время окон — по
// часам роутера из вида «Прокси». В конце стенд возвращается: окна удаляются, группы и узлы
// убираются, маршрутизация отпускается, если не была взята, черновик сбрасывается. Ответы сети
// не подменяются; в сообщения провалов попадают только testId и коды, без имён узлов.

const PREFIX = 'RTSC-';

const stand: RoutingStand = { groupIds: [], myBefore: [], wasManaged: false };
let manualId = '';
let autoId = '';
let standRead = false;
// Окна, созданные сценариями: удаляются в конце теста и ещё раз в afterAll
const windows = new Set<string>();

test.describe.configure({ mode: 'serial' });

interface RtMember {
  member: string;
  kind: 'group' | 'node' | 'direct' | 'reject';
}

interface RtWindow {
  id: string;
  member: string;
  active: boolean;
}

interface RtGroup {
  id: string;
  current: string;
  members: RtMember[];
  schedule: RtWindow[];
  hold?: { kind?: string; window?: string };
}

interface RouterClock {
  /** Unix, секунды. */
  now: number;
  /** Смещение пояса роутера от UTC, секунды. */
  offset: number;
}

const LOCALES = ['ru', 'en'].map(
  (lang) =>
    JSON.parse(
      readFileSync(
        fileURLToPath(new URL(`../../src/locales/${lang}.json`, import.meta.url)),
        'utf8'
      )
    ) as Record<string, string>
);

/**
 * Текст ключа на обоих языках (язык панели стенда заранее неизвестен); параметры {…} —
 * любой текст. exact — строка целиком.
 */
function say(key: string, exact = false): RegExp {
  const parts = LOCALES.map((l) => {
    const text = l[key];
    if (!text) throw new Error(`ключа ${key} нет в словаре`);
    return escapeRe(text).replace(/\\\{\w+\\\}/g, '.*?');
  });
  const body = `(${parts.join('|')})`;
  return new RegExp(exact ? `^${body}$` : body);
}

/** Вид «Прокси»: группа и часы роутера из GET /api/configlayer/proxies. */
async function runtimeView(page: Page, id: string): Promise<{ g: RtGroup; clock: RouterClock }> {
  const view = await api(page, 'GET', '/api/configlayer/proxies');
  expect(view.status, 'GET /api/configlayer/proxies').toBe(200);
  const g = ((view.data?.groups ?? []) as RtGroup[]).find((x) => x.id === id);
  expect(g, `группа ${id} в виде /api/configlayer/proxies`).toBeTruthy();
  const c = (view.data?.clock ?? {}) as { now?: number; offset?: number };
  return {
    g: { ...g!, current: g!.current ?? '', members: g!.members ?? [], schedule: g!.schedule ?? [] },
    clock: { now: Number(c.now ?? 0), offset: Number(c.offset ?? 0) }
  };
}

/** Индексы узлов группы по виду (testId плитки — proxy-node-<группа>-<индекс>). */
function nodeIndexes(g: RtGroup): number[] {
  return g.members.flatMap((m, i) => (m.kind === 'node' ? [i] : []));
}

const hhmm = (min: number) => {
  const m = ((min % 1440) + 1440) % 1440;
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
};

/**
 * Окно, покрывающее текущее время роутера: сегодня (0 — понедельник), «С» на час раньше, «По»
 * на час позже. У полуночи окно идёт через полночь, и оно ставится на все дни: так оно
 * покрывает время роутера и вчерашним продолжением.
 */
function coveringWindow(clock: RouterClock): { days: number[]; from: string; to: string } {
  const local = new Date((clock.now + clock.offset) * 1000);
  const min = local.getUTCHours() * 60 + local.getUTCMinutes();
  const today = (local.getUTCDay() + 6) % 7;
  const nearMidnight = min < 60 || min >= 23 * 60;
  return {
    days: nearMidnight ? [0, 1, 2, 3, 4, 5, 6] : [today],
    from: hhmm(min - 60),
    to: hhmm(min + 60)
  };
}

/** Окно через API (как модалка): возвращает его ID. */
async function saveWindow(
  page: Page,
  member: string,
  times: { days: number[]; from: string; to: string }
): Promise<string> {
  const res = await api(page, 'POST', '/api/configlayer/routing/schedule/save', {
    group: manualId,
    window: { id: '', member, enabled: true, ...times }
  });
  expect(res.status, 'POST routing/schedule/save').toBe(200);
  const id = String(res.data?.id ?? '');
  expect(id, 'ID окна').not.toBe('');
  windows.add(id);
  return id;
}

/** Удаляет окна сценариев; ошибки уборки не роняют тест. */
async function dropWindows(page: Page): Promise<void> {
  for (const id of [...windows]) {
    const res = await api(page, 'POST', '/api/configlayer/routing/schedule/delete', {
      group: manualId,
      id
    }).catch(() => null);
    if (res && (res.status === 200 || res.status === 404)) windows.delete(id);
  }
}

async function openProxies(page: Page, hash = '#/proxies', width = 1440): Promise<void> {
  await page.setViewportSize({ width, height: width <= 480 ? 844 : 900 });
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('proxies-layer-page')).toBeVisible({ timeout: T.page });
}

/** Карточка группы раскрыта. */
async function expandCard(page: Page, id: string) {
  const card = page.getByTestId(`proxy-group-${id}`);
  await expect(card, `proxy-group-${id}`).toBeVisible({ timeout: T.page });
  const head = card.locator('.pc-toggle').first();
  if ((await head.getAttribute('aria-expanded')) !== 'true') {
    await card.locator('.pc-name').first().click();
  }
  await expect(head).toHaveAttribute('aria-expanded', 'true', { timeout: T.action });
  return card;
}

/** Дни модалки окна: чипы нажимаются, пока набор не совпадёт с нужным. */
async function setDays(page: Page, days: number[]): Promise<void> {
  for (let d = 0; d < 7; d++) {
    const chip = page.getByTestId(`schedule-window-day-${d}`);
    const pressed = (await chip.getAttribute('aria-pressed')) === 'true';
    if (pressed !== days.includes(d)) await chip.click();
    await expect(chip).toHaveAttribute('aria-pressed', String(days.includes(d)));
  }
}

async function prepare(browser: Browser): Promise<void> {
  await withPanel(browser, async (page) => {
    await resetDraft(page);
    stand.wasManaged = (await routingInfo(page)).desired;
    stand.myBefore = await myEntryIds(page);
    standRead = true;
    const port = randomPort();
    await addToDraft(
      page,
      [
        nodeLink('vless_ws.txt', port, `${PREFIX}a`),
        nodeLink('trojan_grpc.txt', port + 1, `${PREFIX}b`)
      ].join('\n')
    );
    const save = async (group: Record<string, unknown>): Promise<string> => {
      const res = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
        revision: (await routingInfo(page)).revision,
        group: {
          id: '',
          mode: 'manual',
          sources: { all: false, ids: ['my'] },
          include: [PREFIX],
          exclude: [],
          regex: false,
          picks: [],
          members: [],
          reserve: '',
          ...group
        }
      });
      expect(res.status, 'сохранение группы').toBe(200);
      const id = String((res.data?.group as { id?: string } | undefined)?.id ?? '');
      expect(id, 'идентификатор группы').not.toBe('');
      return id;
    };
    manualId = await save({ name: 'RT Расписание' });
    stand.groupIds = [manualId];
    autoId = await save({ name: 'RT Расписание авто', mode: 'auto' });
    stand.groupIds = [autoId, manualId];
    await takeAndApply(page);
    // признак «применена» бэкенд ставит только ручным группам; «авто» должна быть в списке
    const groups = (await routingInfo(page)).groups;
    expect(groups.find((g) => g.id === manualId)?.applied, 'ручная группа теста применена').toBe(
      true
    );
    expect(
      groups.some((g) => g.id === autoId),
      'группа «авто» теста есть в списке'
    ).toBe(true);
    const { g } = await runtimeView(page, manualId);
    expect(nodeIndexes(g).length, 'два узла в ручной группе').toBe(2);
  });
}

test.beforeAll(async ({ browser }) => {
  // сброс черновика, узлы, две группы, взятие маршрутизации и «Применить» с перезапуском ядра
  test.setTimeout(T.save * 8);
  await withPanel(browser, (page) => resetDraft(page));
  await prepare(browser);
});

test.afterAll(async ({ browser }) => {
  test.setTimeout(T.save * 6);
  try {
    if (standRead) {
      await withPanel(browser, async (page) => {
        await dropWindows(page);
        await restoreRouting(page, stand);
      });
    }
  } finally {
    await withPanel(browser, async (page) => {
      await waitApplyIdle(page, 'перед сбросом черновика');
      await resetDraft(page);
    });
  }
});

test(
  'proxies-schedule: окно в карточке — добавление, пересечение, через полночь, удаление',
  {
    annotation: {
      type: SLOW_ANNOTATION,
      description:
        'выбор узла арбитром при входе в окно и возврат при удалении: Xray перезапускается'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 4);
    const guard = attachConsoleGuard(page);
    try {
      await openProxies(page, `#/proxies?group=${manualId}`);
      await expandCard(page, manualId);
      const block = page.getByTestId(`proxy-group-schedule-${manualId}`);
      await expect(block).toBeVisible({ timeout: T.page });
      await expect(block).toContainText(say('proxies.rt.sched.empty'), { timeout: T.action });
      await expect(page.getByTestId(`proxy-group-schedule-clock-${manualId}`)).toContainText(
        /\d{2}:\d{2}/
      );
      await expect(page.getByTestId(`proxy-group-state-${manualId}`)).toHaveCount(0);

      const { g, clock } = await runtimeView(page, manualId);
      const [, idxB] = nodeIndexes(g);
      const memberB = g.members[idxB].member;
      const cover = coveringWindow(clock);

      // «Добавить окно»: узел B, день и время роутера ±1 ч
      await page.getByTestId(`proxy-group-schedule-add-${manualId}`).click();
      const modal = page.getByTestId('schedule-window-modal');
      await expect(modal).toBeVisible({ timeout: T.action });
      await page.getByTestId('schedule-window-member').selectOption(memberB);
      await setDays(page, cover.days);
      await page.getByTestId('schedule-window-from').fill(cover.from);
      await page.getByTestId('schedule-window-to').fill(cover.to);
      await expect(page.getByTestId('schedule-window-overlap')).toHaveCount(0);
      const saveResp = page.waitForResponse(
        (r) =>
          r.request().method() === 'POST' &&
          r.url().includes('/api/configlayer/routing/schedule/save'),
        { timeout: T.save }
      );
      await page.getByTestId('schedule-window-save').click();
      expect((await saveResp).status(), 'POST routing/schedule/save').toBe(200);
      await expect(modal).toHaveCount(0, { timeout: T.action });

      const saved = (await runtimeView(page, manualId)).g.schedule;
      expect(saved.length, 'окно в виде группы').toBe(1);
      const first = saved[0].id;
      windows.add(first);
      await expect(page.getByTestId(`proxy-group-schedule-window-${first}`)).toBeVisible({
        timeout: T.action
      });

      // Окно покрывает время роутера: арбитр держит группу, строка «По расписанию до HH:MM»
      const state = page.getByTestId(`proxy-group-state-${manualId}`);
      await expect(state).toContainText(say('proxies.rt.sched.state_active'), {
        timeout: T.save
      });
      await expect(state).toContainText(/\d{2}:\d{2}/);
      await expect
        .poll(async () => (await runtimeView(page, manualId)).g.current, {
          message: 'выбран узел окна',
          timeout: T.save
        })
        .toBe(memberB);

      // Второе окно на те же дни и время: конфликт блокирует «Сохранить окно»
      await page.getByTestId(`proxy-group-schedule-add-${manualId}`).click();
      await expect(modal).toBeVisible({ timeout: T.action });
      await setDays(page, cover.days);
      await page.getByTestId('schedule-window-from').fill(cover.from);
      await page.getByTestId('schedule-window-to').fill(cover.to);
      const overlap = page.getByTestId('schedule-window-overlap');
      await expect(overlap).toBeVisible({ timeout: T.action });
      await expect(overlap).toHaveAttribute('role', 'alert');
      await expect(overlap).toContainText(say('proxies.rt.sched.err_overlap'));
      await expect(page.getByTestId('schedule-window-save')).toBeDisabled();

      // «По» не позже «С»: окно закончится на следующий день
      await page.getByTestId('schedule-window-from').fill('23:00');
      await page.getByTestId('schedule-window-to').fill('01:00');
      await expect(modal).toContainText(say('proxies.rt.sched.hint_overnight'), {
        timeout: T.action
      });

      // Закрыть без сохранения: второго окна нет
      await modal.getByRole('button', { name: say('proxies.rt.sched.cancel', true) }).click();
      await expect(modal).toHaveCount(0, { timeout: T.action });
      expect((await runtimeView(page, manualId)).g.schedule.length, 'окно одно').toBe(1);

      // Удаление через подтверждение: «Оставить окно» ничего не меняет
      const dialog = page.getByRole('dialog');
      await page.getByTestId(`proxy-group-schedule-delete-${first}`).click();
      await expect(
        dialog.getByRole('button', { name: say('proxies.rt.sched.delete_confirm', true) })
      ).toBeVisible({ timeout: T.action });
      await dialog
        .getByRole('button', { name: say('proxies.rt.sched.delete_cancel', true) })
        .click();
      await expect(dialog).toHaveCount(0, { timeout: T.action });
      await expect(page.getByTestId(`proxy-group-schedule-window-${first}`)).toBeVisible();

      await page.getByTestId(`proxy-group-schedule-delete-${first}`).click();
      await dialog
        .getByRole('button', { name: say('proxies.rt.sched.delete_confirm', true) })
        .click();
      await expect(dialog).toHaveCount(0, { timeout: T.save });
      windows.delete(first);
      await expect(page.getByTestId(`proxy-group-schedule-window-${first}`)).toHaveCount(0, {
        timeout: T.action
      });
      await expect(block).toContainText(say('proxies.rt.sched.empty'));
      // Удержания больше нет: строка состояния исчезает
      await expect(state).toHaveCount(0, { timeout: T.save });

      // D-10: у группы «авто» узел выбирает ядро, блока «Расписание» нет
      await expandCard(page, autoId);
      await expect(page.getByTestId(`proxy-group-schedule-${autoId}`)).toHaveCount(0);
      await guard.assertClean();
    } finally {
      await dropWindows(page);
    }
  }
);

test(
  'proxies-schedule: перехват и «Вернуть расписание»',
  {
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'вход в окно, выбор плиткой и возврат расписания: Xray перезапускается трижды'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 5);
    const guard = attachConsoleGuard(page);
    try {
      await openProxies(page, `#/proxies?group=${manualId}`);
      const card = await expandCard(page, manualId);
      const { g, clock } = await runtimeView(page, manualId);
      const [idxA, idxB] = nodeIndexes(g);
      const memberA = g.members[idxA].member;
      const memberB = g.members[idxB].member;

      // Окно на B действует сейчас
      await saveWindow(page, memberB, coveringWindow(clock));
      const state = page.getByTestId(`proxy-group-state-${manualId}`);
      await expect(state).toContainText(say('proxies.rt.sched.state_active'), {
        timeout: T.save
      });
      const tileA = card.getByTestId(`proxy-node-${manualId}-${idxA}`);
      const tileB = card.getByTestId(`proxy-node-${manualId}-${idxB}`);
      await expect(tileB).toHaveAttribute('aria-pressed', 'true', { timeout: T.save });

      // Выбор другого узла внутри окна — перехват до конца окна (D-04)
      const selectResp = page.waitForResponse(
        (r) =>
          r.request().method() === 'POST' && r.url().includes('/api/configlayer/routing/select'),
        { timeout: T.save }
      );
      await tileA.click();
      expect((await selectResp).status(), 'POST routing/select').toBe(200);
      await expect(
        page.locator('.toast').filter({ hasText: say('proxies.rt.sched.toast_override') })
      ).toBeVisible({ timeout: T.save });
      await expect(state).toContainText(say('proxies.rt.sched.state_override'), {
        timeout: T.save
      });
      await expect(state).toContainText(/\d{2}:\d{2}/);
      await expect(tileA).toHaveAttribute('aria-pressed', 'true', { timeout: T.save });
      expect((await runtimeView(page, manualId)).g.hold?.kind, 'удержание — перехват').toBe(
        'override'
      );

      // «Вернуть расписание»: группа снова на узле окна
      const restore = page.getByTestId(`proxy-group-restore-${manualId}`);
      await expect(restore).toBeVisible();
      await expect(restore).toHaveText(say('proxies.rt.sched.restore'));
      await restore.click();
      await expect(
        page.locator('.toast').filter({ hasText: say('proxies.rt.sched.toast_restored') })
      ).toBeVisible({ timeout: T.save });
      await expect(state).toContainText(say('proxies.rt.sched.state_active'), {
        timeout: T.save
      });
      await expect(restore).toHaveCount(0);
      await expect
        .poll(async () => (await runtimeView(page, manualId)).g.current, {
          message: 'группа вернулась на узел окна',
          timeout: T.save
        })
        .toBe(memberB);
      expect(memberA, 'узлы окна и перехвата разные').not.toBe(memberB);
      await guard.assertClean();
    } finally {
      await dropWindows(page);
    }
  }
);

test('proxies-schedule: «Задать лимит» открывает форму на «Трафике»', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await openProxies(page, `#/proxies?group=${manualId}`);
  // Ссылка есть, только пока на группу не действует ни один лимит (свой или общий)
  const limits = await api(page, 'GET', '/api/traffic/limits');
  expect(limits.status, 'GET /api/traffic/limits').toBe(200);
  const list = (limits.data?.limits ?? []) as { target_type: string; target_id: string }[];
  const before = list.length;
  test.skip(
    list.some((l) => l.target_type === 'all' || l.target_id === manualId),
    'на стенде есть общий лимит: у группы нет ссылки «Задать лимит»'
  );
  await expandCard(page, manualId);
  const set = page.getByTestId(`proxy-group-quota-set-${manualId}`);
  await expect(set).toBeVisible({ timeout: T.page });
  await expect(set).toHaveText(say('proxies.rt.quota.set'));
  // уход со страницы обрывает опрос «Прокси» самой панелью: «Прокси» проверяется до перехода,
  // «Трафик» — своим сборщиком после него
  await guard.assertClean();
  await set.click();
  const trafficGuard = attachConsoleGuard(page);
  await expect
    .poll(() => page.evaluate(() => window.location.hash), { timeout: T.page })
    .toBe(`#/traffic?tab=limits&new=1&group=${encodeURIComponent(manualId)}`);
  await expect(page.getByTestId('traffic-tab-limits')).toHaveAttribute('aria-selected', 'true', {
    timeout: T.page
  });
  const modal = page.getByTestId('traffic-limit-modal');
  await expect(modal).toBeVisible({ timeout: T.page });
  // Цель формы — группа карточки (D-17)
  await expect(page.getByTestId('traffic-limit-field-target')).toHaveValue(manualId, {
    timeout: T.action
  });
  await modal.getByRole('button', { name: say('traffic.limits.cancel', true) }).click();
  await expect(modal).toHaveCount(0, { timeout: T.action });
  const after = await api(page, 'GET', '/api/traffic/limits');
  expect(((after.data?.limits ?? []) as unknown[]).length, 'лимит не создан').toBe(before);
  await trafficGuard.assertClean();
});

for (const theme of ['light', 'dark'] as const) {
  test(`proxies-schedule axe: блок расписания и модалка, тема ${theme}`, async ({ page }) => {
    test.setTimeout(T.page * 3);
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    const problems: string[] = [];
    for (const width of [390, 1280]) {
      await openProxies(page, `#/proxies?group=${manualId}`, width);
      expect(
        await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
        'применена тема'
      ).toBe(theme);
      await expandCard(page, manualId);
      const block = page.getByTestId(`proxy-group-schedule-${manualId}`);
      await expect(block).toBeVisible({ timeout: T.page });
      await expect(block.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
      for (const v of await axeSevere(page, `[data-testid="proxy-group-schedule-${manualId}"]`)) {
        problems.push(`${width} px, блок: ${v}`);
      }
      await page.getByTestId(`proxy-group-schedule-add-${manualId}`).click();
      const modal = page.getByTestId('schedule-window-modal');
      await expect(modal).toBeVisible({ timeout: T.action });
      for (const v of await axeSevere(page, '.modal-container[role="dialog"]')) {
        problems.push(`${width} px, модалка: ${v}`);
      }
      await modal.getByRole('button', { name: say('proxies.rt.sched.cancel', true) }).click();
      await expect(modal).toHaveCount(0, { timeout: T.action });
      // на 390 px страница не шире окна
      if (width === 390) {
        const wide = await page.evaluate(() => ({
          scroll: document.documentElement.scrollWidth,
          inner: window.innerWidth
        }));
        if (wide.scroll > wide.inner + 1) {
          problems.push(`390 px: scrollWidth ${wide.scroll} шире окна ${wide.inner}`);
        }
      }
    }
    expect(problems, problems.join('\n')).toEqual([]);
    await guard.assertClean();
  });
}
