// e2e-pages: config/groups config/rules
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { CORE_TAG } from './lib/known';
import { addToDraft, api, layerFlag, nodeLink, randomPort, resetDraft } from './lib/nodes-api';

// Вкладки «Группы» и «Правила» раздела «Конфигурация» на настоящей панели (фаза 146,
// план 17): маршруты #/config/groups и #/config/rules, общая полоса черновика, состояния
// загрузки и пустые состояния, полоса вкладок на 390 px в обеих темах и проход axe.
// Ответы сети не подменяются. Флаг слоя включается на время прогона и возвращается
// прежним, черновик сбрасывается. Ядро не читается и не меняется. В вывод не попадают
// имена узлов и групп.

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

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Вкладка загружена: она видна и скелетона больше нет. */
async function tabReady(page: Page, testId: string): Promise<void> {
  const tab = page.getByTestId(testId);
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
}

interface RoutingProbe {
  groups: number;
  rules: number;
  hasSources: boolean;
}

async function routingProbe(page: Page): Promise<RoutingProbe> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  const groups = view.data?.groups;
  const rules = view.data?.rules;
  return {
    groups: Array.isArray(groups) ? groups.length : 0,
    rules: Array.isArray(rules) ? rules.length : 0,
    hasSources: view.data?.has_sources === true
  };
}

test('routing: вкладки открываются кликом и по маршруту, полоса черновика общая', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);

  await openConfig(page, '#/config');
  await expect(page.getByTestId('config-tab-nodes')).toHaveAttribute('aria-selected', 'true');
  const bar = page.getByTestId('config-draftbar');
  await expect(bar).toBeVisible({ timeout: T.action });
  // метка на элементе полосы: если вкладки пересоздали бы её, метка пропала бы
  await bar.evaluate((el) => {
    (el as HTMLElement & { __kept?: boolean }).__kept = true;
  });
  const kept = () =>
    bar.evaluate((el) => (el as HTMLElement & { __kept?: boolean }).__kept === true);

  await page.getByTestId('config-tab-groups').click();
  await expect(page).toHaveURL(/#\/config\/groups$/);
  await expect(page.getByTestId('config-tab-groups')).toHaveAttribute('aria-selected', 'true');
  await tabReady(page, 'groups-tab');
  await expect(page.getByTestId('nodes-tab')).toHaveCount(0);
  expect(await kept()).toBe(true);

  await page.getByTestId('config-tab-rules').click();
  await expect(page).toHaveURL(/#\/config\/rules$/);
  await expect(page.getByTestId('config-tab-rules')).toHaveAttribute('aria-selected', 'true');
  await tabReady(page, 'rules-tab');
  await expect(page.getByTestId('groups-tab')).toHaveCount(0);
  expect(await kept()).toBe(true);

  await page.getByTestId('config-tab-files').click();
  await expect(page).toHaveURL(/#\/config\/files$/);
  await expect(page.getByTestId('config-files')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('rules-tab')).toHaveCount(0);
  expect(await kept()).toBe(true);

  // порядок вкладок: Узлы, Группы, Правила, Файлы панели
  const order = await page
    .locator('[data-testid="config-tabs"] [role="tab"]')
    .evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')));
  expect(order).toEqual([
    'config-tab-nodes',
    'config-tab-groups',
    'config-tab-rules',
    'config-tab-files'
  ]);

  // прямой переход и перезагрузка открывают ту же вкладку
  for (const [hash, tab, other] of [
    ['#/config/groups', 'groups-tab', 'rules-tab'],
    ['#/config/rules', 'rules-tab', 'groups-tab']
  ] as const) {
    await openConfig(page, hash);
    await page.reload({ timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
    await tabReady(page, tab);
    await expect(page.getByTestId(other)).toHaveCount(0);
  }

  await guard.assertClean();
});

test('routing: пустые состояния и список по данным настоящего представления', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);
  const probe = await routingProbe(page);

  await openConfig(page, '#/config/groups');
  await tabReady(page, 'groups-tab');
  const groupsTab = page.getByTestId('groups-tab');
  if (probe.groups > 0) {
    await expect(groupsTab.locator('[data-testid^="group-row-"]')).toHaveCount(probe.groups);
    await expect(page.getByTestId('groups-count')).toBeVisible();
  } else if (!probe.hasSources) {
    // источников нет: группы по умолчанию ещё не созданы (D-09)
    await expect(
      groupsTab.getByRole('heading', { name: /Групп пока нет|No groups yet/ })
    ).toBeVisible();
    await groupsTab.getByRole('button', { name: /Перейти к узлам|Go to nodes/ }).click();
    await expect(page).toHaveURL(/#\/config\/nodes$/);
    await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.action });
  } else {
    await expect(groupsTab.getByRole('heading', { name: /Групп нет|No groups/ })).toBeVisible();
  }

  await openConfig(page, '#/config/rules');
  await tabReady(page, 'rules-tab');
  const rulesTab = page.getByTestId('rules-tab');
  if (probe.rules === 0) {
    await expect(
      rulesTab.getByRole('heading', { name: /Правил пока нет|No rules yet/ })
    ).toBeVisible();
  } else {
    await expect(rulesTab.locator('[data-testid^="rule-row-"]')).toHaveCount(probe.rules);
    await expect(page.getByTestId('rules-fallback')).toBeVisible();
  }

  await guard.assertClean();
});

for (const theme of ['dark', 'light'] as const) {
  test(`routing: 390 px, тема ${theme} — без горизонтальной прокрутки, полоса вкладок прокручивается внутри себя`, async ({
    page
  }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 390, height: 844 });
    await startPage(page);

    for (const [hash, tab] of [
      ['#/config/groups', 'groups-tab'],
      ['#/config/rules', 'rules-tab']
    ] as const) {
      await openConfig(page, hash);
      await tabReady(page, tab);
      expect(
        await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
        `${hash}: применена тема`
      ).toBe(theme);

      const metrics = await page.evaluate(() => {
        const strip = document.querySelector(
          '[data-testid="config-tabs"] .tabs'
        ) as HTMLElement | null;
        const root = document.documentElement;
        return {
          overflowX: strip ? getComputedStyle(strip).overflowX : '',
          wrap: strip ? getComputedStyle(strip).flexWrap : '',
          pageScroll: root.scrollWidth - root.clientWidth
        };
      });
      expect(metrics.overflowX, `${hash}: прокрутка полосы`).toBe('auto');
      expect(metrics.wrap, `${hash}: вкладки не переносятся`).toBe('nowrap');
      expect(metrics.pageScroll, `${hash}: горизонтальная прокрутка страницы`).toBeLessThanOrEqual(
        0
      );
    }

    await guard.assertClean();
  });
}

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри заданных областей. Вывод — правило, селекторы и числа контраста: HTML узлов
 * может содержать имена узлов.
 */
async function axeSevere(page: Page, scopes: string[]): Promise<string[]> {
  // переход между вкладками и смена ширины идут с затуханием: axe не должен видеть
  // полупрозрачный кадр
  await page.waitForTimeout(500);
  let builder = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);
  for (const scope of scopes) builder = builder.include(scope);
  const scan = await builder.analyze();
  return scan.violations
    .filter((v) => v.impact === 'critical' || v.impact === 'serious')
    .map((v) => {
      const nodes = v.nodes.slice(0, 3).map((n) => {
        const msg = (n.any[0]?.message ?? '')
          .replace(/^Element has insufficient color contrast of /, 'контраст ')
          .replace(/, font size:.*?\)/, ')')
          .replace(/\. Expected contrast ratio of /, ', нужно ');
        return `${n.target.join(' ')} [${msg}]`;
      });
      return `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${nodes.join(' | ')}`;
    });
}

for (const theme of ['dark', 'light'] as const) {
  test(`routing axe: вкладки, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    await resetDraft(page);

    const problems: string[] = [];
    for (const [hash, tab] of [
      ['#/config/groups', 'groups-tab'],
      ['#/config/rules', 'rules-tab']
    ] as const) {
      await openConfig(page, hash);
      await tabReady(page, tab);
      const applied = await page.evaluate(() =>
        document.documentElement.getAttribute('data-theme')
      );
      if (applied !== theme)
        problems.push(`${hash}: тема применена ${applied}, ожидалась ${theme}`);

      for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
        // области должны быть видимы: include без совпадений axe считает ошибкой
        await expect(page.locator('[role="tablist"]').first()).toBeVisible({ timeout: T.action });
        await expect(page.getByTestId(tab)).toBeVisible({ timeout: T.action });
        for (const v of await axeSevere(page, ['[role="tablist"]', `[data-testid="${tab}"]`])) {
          problems.push(`${hash} ${width}px: axe ${v}`);
        }
      }
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

// --- Владение маршрутизацией (фаза 146, план 18) ---

interface OwnerSnap {
  taken: boolean;
  desired: boolean;
  xray: boolean;
  mihomo: boolean;
  checks: number;
  revision: number;
  changes: number;
  running: boolean;
}

/** Снимок слоя: кто владеет маршрутизацией, какие ядра установлены, проблемы проверки. */
async function ownerSnap(page: Page): Promise<OwnerSnap> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const routing = (st.data?.routing ?? {}) as {
    desired?: boolean;
    xray?: { taken?: boolean } | null;
    mihomo?: { taken?: boolean } | null;
  };
  const apply = (st.data?.apply ?? {}) as { running?: boolean };
  return {
    taken: routing.xray?.taken === true || routing.mihomo?.taken === true,
    desired: routing.desired === true,
    xray: !!routing.xray,
    mihomo: !!routing.mihomo,
    checks: Array.isArray(st.data?.checks) ? st.data.checks.length : 0,
    revision: Number(st.data?.draft_revision ?? 0),
    changes: Number(st.data?.draft_changes ?? 0),
    running: apply.running === true
  };
}

/** Запускает «Применить» и ждёт конца: применение не идёт, в черновике нет изменений. */
async function applyAndWait(page: Page): Promise<void> {
  const start = await api(page, 'POST', '/api/configlayer/apply', {});
  expect(start.status, 'запуск применения').toBe(200);
  await expect
    .poll(
      async () => {
        const s = await ownerSnap(page);
        return !s.running && s.changes === 0;
      },
      { timeout: T.save * 4, intervals: [2000] }
    )
    .toBe(true);
}

/** Берёт или возвращает маршрутизацию настоящими маршрутами и применяет. */
async function setOwnership(page: Page, take: boolean): Promise<void> {
  const s = await ownerSnap(page);
  if (s.taken === take && s.desired === take) return;
  const res = await api(page, 'POST', `/api/configlayer/routing/${take ? 'take' : 'release'}`, {
    revision: s.revision
  });
  expect(res.status, take ? 'взятие через API' : 'возврат через API').toBe(200);
  await applyAndWait(page);
}

/**
 * Приводит стенд к состоянию «маршрутизацией управляет XKeen» и возвращает функцию
 * отката: сбрасывает черновик, убирает добавленные узлы, возвращает прежнего владельца.
 */
async function prepareXKeen(page: Page): Promise<() => Promise<void>> {
  await resetDraft(page);
  const before = await ownerSnap(page);
  if (before.taken) await setOwnership(page, false);
  return async () => {
    await resetDraft(page);
    // узлы теста могли быть применены: убираем их и применяем
    const view = await api(page, 'GET', '/api/configlayer/sources');
    const mine = (
      (view.data?.sources ?? []) as { id: string; nodes?: { name: string; entry_id?: string }[] }[]
    ).find((x) => x.id === 'my');
    for (const n of mine?.nodes ?? []) {
      if (!n.name.startsWith('RtOwn') || !n.entry_id) continue;
      const cur = await ownerSnap(page);
      const del = await api(page, 'POST', '/api/configlayer/sources/nodes/delete', {
        revision: cur.revision,
        entry_id: n.entry_id
      });
      expect(del.status, 'удаление узла теста').toBe(200);
    }
    const after = await ownerSnap(page);
    if (after.changes > 0) await applyAndWait(page);
    await setOwnership(page, before.taken);
  };
}

test('routing: взятие и отпускание', { tag: CORE_TAG }, async ({ page }) => {
  test.setTimeout(T.save * 10);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const snap = await ownerSnap(page);
  test.skip(!snap.xray && !snap.mihomo, 'на стенде нет установленных ядер');
  const restore = await prepareXKeen(page);
  try {
    await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'RtOwnA'));
    await openConfig(page, '#/config/rules');
    await tabReady(page, 'rules-tab');

    // управляет XKeen: плашка и кнопка взятия
    const notice = page.getByTestId('routing-notice');
    await expect(notice).toHaveAttribute('data-owner', 'xkeen', { timeout: T.action });
    await page.getByTestId('routing-take').click();

    // диалог показывает последствия до любого запроса записи
    const modal = page.getByTestId('take-routing-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('take-routing-consequence')).toBeVisible({
      timeout: T.action
    });
    if (snap.xray) {
      await expect(
        modal.getByTestId('take-routing-move').or(modal.getByTestId('take-routing-none'))
      ).toBeVisible();
    }
    if (snap.mihomo) await expect(modal.getByTestId('take-routing-mihomo')).toBeVisible();
    expect((await ownerSnap(page)).desired, 'диалог сам ничего не записал').toBe(false);

    // «Взять» пишет желание в черновик: файлы меняются при «Применить»
    await modal.getByTestId('take-routing-confirm').click();
    await expect(modal).toHaveCount(0, { timeout: T.action });
    await expect(notice).toHaveAttribute('data-owner', 'pending', { timeout: T.action });
    await expect(page.getByTestId('routing-take-pending')).toBeVisible();
    expect((await ownerSnap(page)).taken, 'до «Применить» файлы не тронуты').toBe(false);

    await page.getByTestId('config-apply').click();
    await expect(notice).toHaveAttribute('data-owner', 'panel', { timeout: T.save * 3 });
    await expect(
      page
        .locator('.toast--success')
        .filter({ hasText: /Маршрутизацию взяла панель|managed by the panel/ })
    ).toBeVisible({ timeout: T.action });
    expect((await ownerSnap(page)).taken).toBe(true);

    // «Отпустить»: подтверждение, возврат и применение без ожидания
    await page.getByTestId('routing-release').click();
    const confirm = page.getByRole('dialog');
    await expect(confirm).toBeVisible({ timeout: T.action });
    await confirm.getByRole('button', { name: /^(Отпустить|Release)$/ }).click();
    await expect(notice).toHaveAttribute('data-owner', 'xkeen', { timeout: T.save * 3 });
    await expect(
      page
        .locator('.toast--success')
        .filter({ hasText: /Маршрутизацию вернули XKeen|returned to XKeen/ })
    ).toBeVisible({ timeout: T.action });
    expect((await ownerSnap(page)).taken).toBe(false);

    await guard.assertClean();
  } finally {
    await restore();
  }
});

/** Узел и группа, пустая для всех ядер: проверка черновика отказывает «Применить» (GRP-05). */
async function seedBlockedDraft(page: Page): Promise<void> {
  await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'RtOwnB'));
  const s = await ownerSnap(page);
  const take = await api(page, 'POST', '/api/configlayer/routing/take', { revision: s.revision });
  expect(take.status, 'желание «взять» через API').toBe(200);
  const rev = await ownerSnap(page);
  const save = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
    revision: rev.revision,
    group: {
      name: 'RtEmpty',
      mode: 'auto',
      sources: { all: true, ids: [] },
      include: [`rtnone${randomPort()}`],
      exclude: [],
      regex: false,
      picks: [],
      members: [],
      reserve: ''
    }
  });
  expect(save.status, 'группа без узлов через API').toBe(200);
  await expect
    .poll(async () => (await ownerSnap(page)).checks, { timeout: T.action })
    .toBeGreaterThan(0);
}

test('routing: блок «Применить»', async ({ page }) => {
  test.setTimeout(T.save * 6);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const snap = await ownerSnap(page);
  test.skip(!snap.xray && !snap.mihomo, 'на стенде нет установленных ядер');
  const restore = await prepareXKeen(page);
  try {
    await seedBlockedDraft(page);
    await openConfig(page, '#/config/rules');
    await tabReady(page, 'rules-tab');

    const blocked = page.getByTestId('config-apply-blocked');
    await expect(blocked).toBeVisible({ timeout: T.action });
    await expect(blocked).toContainText(/Применить недоступно|Apply unavailable/);
    expect(await blocked.getAttribute('title'), 'в подсказке причины').toBeTruthy();
    const apply = page.getByTestId('config-apply');
    await expect(apply).toBeDisabled();
    expect(await apply.getAttribute('title'), 'кнопка объясняет отказ').toBeTruthy();
    // отказ не даёт открыть диалог взятия: «Применить» неактивно
    await expect(page.getByTestId('take-routing-modal')).toHaveCount(0);

    await guard.assertClean();
  } finally {
    await restore();
  }
});

test('routing: 390 px — «Применить недоступно» переносится, кнопки полосы не обрезаны', async ({
  page
}) => {
  test.setTimeout(T.save * 6);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await startPage(page);
  const snap = await ownerSnap(page);
  test.skip(!snap.xray && !snap.mihomo, 'на стенде нет установленных ядер');
  const restore = await prepareXKeen(page);
  try {
    await seedBlockedDraft(page);
    await openConfig(page, '#/config/rules');
    await tabReady(page, 'rules-tab');
    await expect(page.getByTestId('config-apply-blocked')).toBeVisible({ timeout: T.action });

    const m = await page.evaluate(() => {
      const bar = document.querySelector('[data-testid="config-draftbar"]') as HTMLElement;
      const sub = bar.querySelector('[data-testid="config-apply-blocked"]') as HTMLElement;
      const root = document.documentElement;
      const btns = Array.from(bar.querySelectorAll('button')).map((b) => {
        const r = b.getBoundingClientRect();
        return { left: r.left, right: r.right, clipped: b.scrollWidth > b.clientWidth + 1 };
      });
      return {
        subClipped: sub.scrollWidth > sub.clientWidth + 1,
        subRight: sub.getBoundingClientRect().right,
        btns,
        pageScroll: root.scrollWidth - root.clientWidth
      };
    });
    expect(m.subClipped, 'текст не обрезан, а переносится').toBe(false);
    expect(m.subRight, 'текст в пределах экрана').toBeLessThanOrEqual(390);
    expect(m.btns.length, 'в полосе две кнопки').toBe(2);
    for (const b of m.btns) {
      expect(b.left, 'кнопка не уходит влево').toBeGreaterThanOrEqual(0);
      expect(b.right, 'кнопка не уходит вправо').toBeLessThanOrEqual(390);
      expect(b.clipped, 'подпись кнопки не обрезана').toBe(false);
    }
    expect(m.pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

    await guard.assertClean();
  } finally {
    await restore();
  }
});

test('routing: перехват «Применить»', { tag: CORE_TAG }, async ({ page }) => {
  test.setTimeout(T.save * 10);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const snap = await ownerSnap(page);
  test.skip(!snap.xray && !snap.mihomo, 'на стенде нет установленных ядер');
  const restore = await prepareXKeen(page);
  try {
    await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'RtOwnC'));
    // группы по умолчанию создаются вместе с первым источником (D-09)
    const probe = await routingProbe(page);
    expect(probe.groups, 'группы по умолчанию в черновике').toBeGreaterThan(0);

    await openConfig(page, '#/config/nodes');
    const apply = page.getByTestId('config-apply');
    await expect(apply).toBeEnabled({ timeout: T.action });
    await apply.click();

    // три исхода: взять, только узлы, отмена
    const modal = page.getByTestId('take-routing-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('take-routing-confirm')).toBeEnabled({ timeout: T.action });
    await expect(modal.getByTestId('take-routing-nodes-only')).toBeVisible();
    await expect(modal.getByTestId('take-routing-cancel')).toBeVisible();

    // «Отмена»: ничего не записано и не применено
    await modal.getByTestId('take-routing-cancel').click();
    await expect(modal).toHaveCount(0, { timeout: T.action });
    const afterCancel = await ownerSnap(page);
    expect(afterCancel.desired, 'отмена не пишет желание «взять»').toBe(false);
    expect(afterCancel.changes, 'отмена не применяет черновик').toBeGreaterThan(0);

    // «Применить только узлы»: применение без взятия, плашка XKeen прежняя
    await apply.click();
    await expect(modal).toBeVisible({ timeout: T.action });
    await modal.getByTestId('take-routing-nodes-only').click();
    await expect(modal).toHaveCount(0, { timeout: T.action });
    await expect
      .poll(
        async () => {
          const s = await ownerSnap(page);
          return !s.running && s.changes === 0;
        },
        { timeout: T.save * 4, intervals: [2000] }
      )
      .toBe(true);
    expect((await ownerSnap(page)).taken, 'только узлы: маршрутизация не взята').toBe(false);

    await openConfig(page, '#/config/rules');
    await tabReady(page, 'rules-tab');
    await expect(page.getByTestId('routing-notice')).toHaveAttribute('data-owner', 'xkeen', {
      timeout: T.action
    });

    await guard.assertClean();
  } finally {
    await restore();
  }
});

for (const theme of ['dark', 'light'] as const) {
  test(`routing axe: владение и взятие, тема ${theme}`, async ({ page }) => {
    test.setTimeout(T.save * 10);
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    const snap = await ownerSnap(page);
    test.skip(!snap.xray && !snap.mihomo, 'на стенде нет установленных ядер');
    // маршрутизация в тесте не берётся: диалог зависит от установленных ядер, не от активного
    const restore = await prepareXKeen(page);
    const problems: string[] = [];
    try {
      await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'RtOwnD'));
      for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
        const at = `${width}px`;

        // плашка владения на «Группах» и «Правилах»
        for (const [hash, tab] of [
          ['#/config/groups', 'groups-tab'],
          ['#/config/rules', 'rules-tab']
        ] as const) {
          await openConfig(page, hash);
          await tabReady(page, tab);
          const applied = await page.evaluate(() =>
            document.documentElement.getAttribute('data-theme')
          );
          if (applied !== theme)
            problems.push(`${hash} ${at}: тема применена ${applied}, ожидалась ${theme}`);
          await expect(page.getByTestId('routing-notice')).toBeVisible({ timeout: T.action });
          for (const v of await axeSevere(page, ['[data-testid="routing-notice"]'])) {
            problems.push(`${hash} ${at}: плашка, axe ${v}`);
          }
        }

        // диалог взятия, режим take (последняя открытая вкладка — «Правила»)
        await page.getByTestId('routing-take').click();
        const modal = page.getByTestId('take-routing-modal');
        await expect(modal).toBeVisible({ timeout: T.action });
        await expect(modal.getByTestId('take-routing-consequence')).toBeVisible({
          timeout: T.action
        });
        for (const v of await axeSevere(page, ['[data-testid="take-routing-modal"]'])) {
          problems.push(`диалог take ${at}: axe ${v}`);
        }
        await modal.getByTestId('take-routing-cancel').click();
        await expect(modal).toHaveCount(0, { timeout: T.action });

        // диалог «Применить» при группах по умолчанию, режим intercept
        await page.getByTestId('config-apply').click();
        await expect(modal).toBeVisible({ timeout: T.action });
        await expect(modal.getByTestId('take-routing-consequence')).toBeVisible({
          timeout: T.action
        });
        await expect(modal.getByTestId('take-routing-nodes-only')).toBeVisible();
        for (const v of await axeSevere(page, ['[data-testid="take-routing-modal"]'])) {
          problems.push(`диалог intercept ${at}: axe ${v}`);
        }
        await modal.getByTestId('take-routing-cancel').click();
        await expect(modal).toHaveCount(0, { timeout: T.action });
      }
      expect(problems, problems.join('\n')).toEqual([]);

      await guard.assertClean();
    } finally {
      await restore();
    }
  });
}
