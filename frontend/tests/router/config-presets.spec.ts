// e2e-pages: config/rules
import { randomInt } from 'node:crypto';
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { CORE_TAG } from './lib/known';
import { addToDraft, api, layerFlag, nodeLink, randomPort, resetDraft } from './lib/nodes-api';

// Списки и пресеты на вкладке «Правила» на настоящей панели (фаза 146, план 22): выбор списка
// для условия geosite из каталога роутера и сохранение правила со ссылкой «файл · тег»; пресет
// на «Правилах» с планом «Будет создано» и «Уйдёт / Придёт»; взятие маршрутизации через
// диалог пресета. Каталог и пресеты читаются настоящими маршрутами, ответы сети не
// подменяются. Флаг слоя включается на время файла и возвращается прежним, черновик
// сбрасывается до и после проверки. Ядро читает и меняет только «взятие с пресетом» (оно
// размечено). В вывод не попадают имена узлов и групп.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

async function openRules(page: Page): Promise<void> {
  await page.goto('/#/config/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  const tab = page.getByTestId('rules-tab');
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
}

interface CatalogList {
  file: string;
  tag: string;
  kind: string;
  count: number;
}

/** Списки доменов (geosite) каталога роутера; пусто, если dat на стенде нет. */
async function siteLists(page: Page): Promise<CatalogList[]> {
  const res = await api(page, 'GET', '/api/configlayer/routing/lists');
  if (res.status !== 200) return [];
  const all = (res.data?.lists ?? []) as CatalogList[];
  return all.filter((l) => l.kind === 'site');
}

test('presets: выбор списка', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  const lists = await siteLists(page);
  test.skip(lists.length === 0, 'на стенде нет установленных geosite-баз: каталог пуст');
  // Самый длинный тег: подстрока в поиске почти не даёт чужих совпадений
  const target = [...lists].sort((a, b) => b.tag.length - a.tag.length)[0];
  const label = `${target.file} · ${target.tag}`;

  await openRules(page);
  await page.getByTestId('rules-add').click();
  const modal = page.getByTestId('rule-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await modal.getByTestId('rule-cond-type-0').selectOption('geosite');
  await expect(modal.getByTestId('rule-save')).toBeDisabled();

  // «Выбрать список» открывает диалог выбора поверх диалога правила
  await modal.getByTestId('rule-cond-pick-0').click();
  const picker = page.getByTestId('list-picker');
  await expect(picker).toBeVisible({ timeout: T.action });
  await expect(picker.getByTestId('list-picker-pick')).toBeDisabled();
  await expect(picker.getByTestId('list-picker-loading')).toHaveCount(0, { timeout: T.page });

  // «Все списки»: поиск по тегу из каталога, строка «файл · тег»
  await picker.getByTestId('list-picker-seg').locator('[data-value="all"]').click();
  await picker.getByTestId('list-picker-search').fill(target.tag);
  const row = picker.getByTestId(`list-row-${target.file}:${target.tag}`);
  await expect(row).toBeVisible({ timeout: T.action });
  await expect(picker.getByTestId('list-picker-all')).toContainText(label);
  await row.check();
  await expect(picker.getByTestId('list-picker-pick')).toBeEnabled();
  await picker.getByTestId('list-picker-pick').click();
  await expect(picker).toBeHidden({ timeout: T.action });

  // чип «файл · тег» в условии, сохранение активно
  const chips = modal.getByTestId('rule-cond-lists-0');
  await expect(chips).toContainText(label);
  await expect(modal.getByTestId('rule-save')).toBeEnabled();

  // повторное открытие помнит выбор; «Отмена» ничего не меняет
  await modal.getByTestId('rule-cond-pick-0').click();
  await expect(picker).toBeVisible({ timeout: T.action });
  await picker.getByTestId('list-picker-seg').locator('[data-value="all"]').click();
  await picker.getByTestId('list-picker-search').fill(target.tag);
  await expect(picker.getByTestId(`list-row-${target.file}:${target.tag}`)).toBeChecked({
    timeout: T.action
  });
  await picker.getByTestId('list-picker-cancel').click();
  await expect(picker).toBeHidden({ timeout: T.action });
  await expect(chips).toContainText(label);

  // сохранение: строка правила со ссылкой «файл · тег»
  await modal.getByTestId('rule-save').click();
  await expect(modal).toBeHidden({ timeout: T.action });
  await expect(
    page.getByTestId('rules-tab').locator('[data-testid^="rule-row-"]').filter({ hasText: label })
  ).toHaveCount(1, { timeout: T.action });

  await guard.assertClean();
  await resetDraft(page);
});

interface ExtendedApi {
  id: string;
  file: string;
  kind: string;
  in_xkeen: boolean;
}

/** Расширенные наборы каталога (geosite), которых ещё нет в xkeen.json стенда. */
async function freeExtendedSets(page: Page): Promise<ExtendedApi[]> {
  const res = await api(page, 'GET', '/api/configlayer/routing/lists');
  if (res.status !== 200) return [];
  return ((res.data?.extended ?? []) as ExtendedApi[]).filter(
    (e) => e.kind === 'site' && !e.in_xkeen
  );
}

/** Содержимое xkeen.json стенда в виде «до» из предпросмотра: запись не выполняется. */
async function xkeenJsonNow(page: Page, set: string): Promise<string> {
  const res = await api(page, 'POST', '/api/configlayer/routing/lists/geodata/preview', { set });
  expect(res.status, 'предпросмотр записи geodata').toBe(200);
  return String(res.data?.before ?? '');
}

test('presets: добавить в XKeen', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  const free = await freeExtendedSets(page);
  test.skip(
    free.length === 0,
    'на стенде все наборы geosite уже в xkeen.json или каталог не прочитан'
  );
  const target = free[0];
  const before = await xkeenJsonNow(page, target.id);

  await openRules(page);
  await page.getByTestId('rules-add').click();
  const rule = page.getByTestId('rule-modal');
  await expect(rule).toBeVisible({ timeout: T.action });
  await rule.getByTestId('rule-cond-type-0').selectOption('geosite');
  await rule.getByTestId('rule-cond-pick-0').click();
  const picker = page.getByTestId('list-picker');
  await expect(picker).toBeVisible({ timeout: T.action });
  await expect(picker.getByTestId('list-picker-loading')).toHaveCount(0, { timeout: T.page });

  // блок расширенных наборов под списком; у набора вне xkeen.json есть «Добавить в XKeen»
  const block = picker.getByTestId('list-picker-extended');
  await expect(block).toBeVisible({ timeout: T.action });
  await expect(block.getByTestId(`list-ext-${target.id}`)).toBeVisible();
  await block.getByTestId(`list-ext-add-${target.id}`).click();

  // диалог: запись geodata с набором в diff, по умолчанию «дождаться ночного обновления»
  const modal = page.getByTestId('geodata-add-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('geodata-add-loading')).toHaveCount(0, { timeout: T.page });
  const diff = modal.getByTestId('geodata-add-diff');
  await expect(diff).toContainText(target.file, { timeout: T.action });
  await expect(diff.locator('.diff-line-added').first()).toBeVisible();
  await expect(modal.getByTestId('geodata-update-later')).toBeChecked();
  await expect(modal.getByTestId('geodata-update-now')).not.toBeChecked();
  // адрес набора в интерфейсе не вводится
  await expect(modal.locator('input[type="url"], input[type="text"], textarea')).toHaveCount(0);

  // «Отмена»: диалог закрыт, xkeen.json не менялся (запись в сценарии не выполняется)
  await modal.getByTestId('geodata-add-cancel').click();
  await expect(modal).toBeHidden({ timeout: T.action });
  await expect(picker).toBeVisible();
  expect(await xkeenJsonNow(page, target.id), 'xkeen.json стенда не изменён').toBe(before);

  await picker.getByTestId('list-picker-cancel').click();
  await expect(picker).toBeHidden({ timeout: T.action });
  await rule.getByTestId('rule-cancel').click();
  await expect(rule).toBeHidden({ timeout: T.action });

  await guard.assertClean();
  await resetDraft(page);
});

// --- Пресеты ---

interface PresetInfoApi {
  id: string;
  needs_country: boolean;
  countries: string[];
}

interface RuleApi {
  id: string;
  origin: string;
}

async function presetInfo(page: Page, id: string): Promise<PresetInfoApi | undefined> {
  const res = await api(page, 'GET', '/api/configlayer/routing/presets');
  if (res.status !== 200) return undefined;
  return ((res.data?.presets ?? []) as PresetInfoApi[]).find((p) => p.id === id);
}

async function routingNow(page: Page): Promise<{
  revision: number;
  rules: RuleApi[];
  final: { kind?: string; id?: string };
}> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  return {
    revision: Number(view.data?.revision ?? 0),
    rules: (view.data?.rules ?? []) as RuleApi[],
    final: (view.data?.final ?? {}) as { kind?: string; id?: string }
  };
}

/** Правило пользователя настоящим маршрутом сохранения; возвращает его идентификатор. */
async function seedUserRule(page: Page): Promise<string> {
  const value = `own-${randomInt(100000, 999999)}.example.com`;
  const res = await api(page, 'POST', '/api/configlayer/routing/rules/save', {
    revision: (await routingNow(page)).revision,
    rule: {
      id: '',
      enabled: true,
      origin: '',
      conds: [{ type: 'domain', values: [value], lists: [] }],
      target: { kind: 'direct' },
      raw: null
    }
  });
  expect(res.status, 'rules/save').toBe(200);
  const id = (res.data?.rule as { id?: string } | undefined)?.id;
  expect(typeof id, 'идентификатор правила пользователя').toBe('string');
  return id as string;
}

/** Выбирает карточку пресета: радио скрыто визуально, щёлкаем по его подписи. */
async function pickCard(page: Page, id: string): Promise<void> {
  const modal = page.getByTestId('preset-modal');
  const radio = modal.getByTestId(`preset-card-${id}`);
  await expect(radio).toBeEnabled({ timeout: T.action });
  await modal.locator('label.pm-card', { has: radio }).click();
  await expect(radio).toBeChecked();
}

test('presets: пресет на «Правилах»', async ({ page }) => {
  test.setTimeout(T.save * 3);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  const except = await presetInfo(page, 'except_country');
  test.skip(
    !except || except.countries.length === 0,
    'на стенде нет данных стран: «Всё кроме страны» недоступно'
  );
  expect((await routingNow(page)).rules, 'после сброса черновика правил нет').toHaveLength(0);

  await openRules(page);
  await page.getByTestId('rules-preset').click();
  const modal = page.getByTestId('preset-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('preset-loading')).toHaveCount(0, { timeout: T.page });
  // «Начать с пустого» — только при взятии
  await expect(modal.getByTestId('preset-card-empty')).toHaveCount(0);
  await expect(modal.getByTestId('preset-create')).toBeDisabled();

  // первый пресет: страна по умолчанию, план «Будет создано» до записи
  await pickCard(page, 'except_country');
  await expect(modal.getByTestId('preset-country')).toBeVisible();
  await expect(modal.getByTestId('preset-plan-first')).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('preset-plan-first')).toContainText(/\d/);
  expect((await routingNow(page)).rules, 'план ничего не пишет').toHaveLength(0);
  await expect(modal.getByTestId('preset-create')).toBeEnabled();
  await modal.getByTestId('preset-create').click();
  await expect(modal).toBeHidden({ timeout: T.action });

  // строки с чипом «из пресета», «Всё остальное → Прокси»
  const first = await routingNow(page);
  expect(first.rules.length, 'правила пресета в черновике').toBeGreaterThan(0);
  expect(first.rules.every((r) => r.origin === 'preset')).toBe(true);
  expect(first.final.kind, 'Всё остальное идёт в группу').toBe('group');
  const rows = page.getByTestId('rules-tab').locator('[data-testid^="rule-row-"]');
  await expect(rows).toHaveCount(first.rules.length, { timeout: T.action });
  await expect(rows.first().locator('[data-chip="preset"]')).toBeVisible();
  await expect(page.getByTestId('rules-fallback-select').locator('option:checked')).toHaveText(
    'Прокси'
  );

  // замена: правило пользователя остаётся выше, план «Уйдёт / Придёт»
  const own = await seedUserRule(page);
  await openRules(page);
  await page.getByTestId('rules-preset').click();
  await expect(modal).toBeVisible({ timeout: T.action });
  await pickCard(page, 'all_proxy');
  await expect(modal.getByTestId('preset-plan-leave')).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('preset-plan-come')).toBeVisible();
  await expect(modal.getByTestId('preset-plan-note')).toBeVisible();
  await modal.getByTestId('preset-create').click();
  await expect(modal).toBeHidden({ timeout: T.action });

  const after = await routingNow(page);
  expect(after.rules[0]?.id, 'правило пользователя первое').toBe(own);
  expect(after.rules.slice(1).every((r) => r.origin === 'preset')).toBe(true);
  await expect(rows.first()).toHaveAttribute('data-testid', `rule-row-${own}`, {
    timeout: T.action
  });

  await guard.assertClean();
  await resetDraft(page);
});

// --- Взятие маршрутизации с пресетом ---

interface OwnerSnap {
  taken: boolean;
  desired: boolean;
  xray: boolean;
  mihomo: boolean;
  revision: number;
  changes: number;
  running: boolean;
}

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
    revision: Number(st.data?.draft_revision ?? 0),
    changes: Number(st.data?.draft_changes ?? 0),
    running: apply.running === true
  };
}

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

async function setOwnership(page: Page, take: boolean): Promise<void> {
  const s = await ownerSnap(page);
  if (s.taken === take && s.desired === take) return;
  const res = await api(page, 'POST', `/api/configlayer/routing/${take ? 'take' : 'release'}`, {
    revision: s.revision
  });
  expect(res.status, take ? 'взятие через API' : 'возврат через API').toBe(200);
  await applyAndWait(page);
}

/** Возвращает стенд к «управляет XKeen» и функцию отката: черновик, узлы теста, владелец. */
async function prepareXKeen(page: Page): Promise<() => Promise<void>> {
  await resetDraft(page);
  const before = await ownerSnap(page);
  if (before.taken) await setOwnership(page, false);
  return async () => {
    await resetDraft(page);
    const view = await api(page, 'GET', '/api/configlayer/sources');
    const mine = (
      (view.data?.sources ?? []) as { id: string; nodes?: { name: string; entry_id?: string }[] }[]
    ).find((x) => x.id === 'my');
    for (const n of mine?.nodes ?? []) {
      if (!n.name.startsWith('RtPreset') || !n.entry_id) continue;
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

test('presets: взятие с пресетом', { tag: CORE_TAG }, async ({ page }) => {
  test.setTimeout(T.save * 10);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const snap = await ownerSnap(page);
  test.skip(!snap.xray && !snap.mihomo, 'на стенде нет установленных ядер');
  const restore = await prepareXKeen(page);
  try {
    await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'RtPresetA'));
    test.skip(
      (await routingNow(page)).rules.length > 0,
      'в черновике уже есть правила: взятие не идёт через пресет'
    );

    await page.goto('/#/config/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
    const notice = page.getByTestId('routing-notice');
    await expect(notice).toHaveAttribute('data-owner', 'xkeen', { timeout: T.action });
    await page.getByTestId('routing-take').click();

    // список правил пуст: «Взять» открывает диалог пресета в режиме взятия
    const take = page.getByTestId('take-routing-modal');
    await expect(take).toBeVisible({ timeout: T.action });
    await take.getByTestId('take-routing-confirm').click();
    const modal = page.getByTestId('preset-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('preset-loading')).toHaveCount(0, { timeout: T.page });
    await expect(modal.getByTestId('preset-card-empty')).toBeVisible();
    await expect(modal.getByTestId('preset-create')).toBeDisabled();
    expect((await ownerSnap(page)).desired, 'диалог сам ничего не записал').toBe(false);

    // «Всё через прокси»: одна правка черновика — желание «взять» и правила пресета
    await pickCard(page, 'all_proxy');
    await modal.getByTestId('preset-create').click();
    await expect(modal).toBeHidden({ timeout: T.action });
    await expect(take).toHaveCount(0, { timeout: T.action });
    await expect(notice).toHaveAttribute('data-owner', 'pending', { timeout: T.action });
    expect((await ownerSnap(page)).desired).toBe(true);
    const drafted = await routingNow(page);
    expect(drafted.rules.length, 'правила пресета в черновике').toBeGreaterThan(0);
    expect(drafted.rules.every((r) => r.origin === 'preset')).toBe(true);
    expect((await ownerSnap(page)).taken, 'до «Применить» файлы не тронуты').toBe(false);

    await page.getByTestId('config-apply').click();
    await expect(notice).toHaveAttribute('data-owner', 'panel', { timeout: T.save * 3 });
    expect((await ownerSnap(page)).taken).toBe(true);

    await guard.assertClean();
  } finally {
    await restore();
  }
});

// --- Доступность диалогов (axe) ---

/**
 * Серьёзные и критические нарушения WCAG 2.0/2.1 A и AA (включая контраст) внутри заданных
 * областей. Вывод — правило, селекторы и числа контраста: HTML узлов может содержать имена
 * узлов и групп.
 */
async function axeSevere(page: Page, scopes: string[]): Promise<string[]> {
  // диалоги появляются с затуханием: axe не должен видеть полупрозрачный кадр
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

const WIDTHS = [1280, 390] as const;

async function setWidth(page: Page, width: number): Promise<void> {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
}

for (const theme of ['dark', 'light'] as const) {
  test(`presets axe: пресет, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await setWidth(page, 1280);
    await startPage(page);
    await resetDraft(page);

    const problems: string[] = [];
    for (const width of WIDTHS) {
      await setWidth(page, width);
      await openRules(page);
      const applied = await page.evaluate(() =>
        document.documentElement.getAttribute('data-theme')
      );
      if (applied !== theme)
        problems.push(`${width}px: тема применена ${applied}, ожидалась ${theme}`);

      // диалог пресета: выбрана карточка «Всё через прокси», план «Будет создано» показан
      await page.getByTestId('rules-preset').click();
      const modal = page.getByTestId('preset-modal');
      await expect(modal).toBeVisible({ timeout: T.action });
      await expect(modal.getByTestId('preset-loading')).toHaveCount(0, { timeout: T.page });
      await pickCard(page, 'all_proxy');
      await expect(modal.getByTestId('preset-plan-first')).toBeVisible({ timeout: T.action });
      for (const v of await axeSevere(page, ['[data-testid="preset-modal"]'])) {
        problems.push(`пресет ${width}px: axe ${v}`);
      }

      // «Отмена»: в черновике ничего не создано
      await modal.getByTestId('preset-cancel').click();
      await expect(modal).toBeHidden({ timeout: T.action });
      expect((await routingNow(page)).rules, 'отмена ничего не создаёт').toHaveLength(0);
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
    await resetDraft(page);
  });
}

for (const theme of ['dark', 'light'] as const) {
  test(`presets axe: списки и XKeen, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await setWidth(page, 1280);
    await startPage(page);
    await resetDraft(page);
    test.skip(
      (await siteLists(page)).length === 0,
      'на стенде нет установленных geosite-баз: каталог пуст'
    );
    const free = await freeExtendedSets(page);

    const problems: string[] = [];
    for (const width of WIDTHS) {
      await setWidth(page, width);
      await openRules(page);
      const applied = await page.evaluate(() =>
        document.documentElement.getAttribute('data-theme')
      );
      if (applied !== theme)
        problems.push(`${width}px: тема применена ${applied}, ожидалась ${theme}`);

      // диалог выбора списка лежит поверх диалога правила: область — по testid
      await page.getByTestId('rules-add').click();
      const rule = page.getByTestId('rule-modal');
      await expect(rule).toBeVisible({ timeout: T.action });
      await rule.getByTestId('rule-cond-type-0').selectOption('geosite');
      await rule.getByTestId('rule-cond-pick-0').click();
      const picker = page.getByTestId('list-picker');
      await expect(picker).toBeVisible({ timeout: T.action });
      await expect(picker.getByTestId('list-picker-loading')).toHaveCount(0, { timeout: T.page });
      await expect(picker.getByTestId('list-picker-extended')).toBeVisible({ timeout: T.action });

      for (const seg of ['services', 'all'] as const) {
        await picker.getByTestId('list-picker-seg').locator(`[data-value="${seg}"]`).click();
        for (const v of await axeSevere(page, ['[data-testid="list-picker"]'])) {
          problems.push(`список (${seg}) ${width}px: axe ${v}`);
        }
      }

      // «Добавить в XKeen» у набора, которого нет в xkeen.json; запись не выполняется
      if (free.length > 0) {
        await picker.getByTestId(`list-ext-add-${free[0].id}`).click();
        const add = page.getByTestId('geodata-add-modal');
        await expect(add).toBeVisible({ timeout: T.action });
        await expect(add.getByTestId('geodata-add-diff')).toBeVisible({ timeout: T.page });
        for (const v of await axeSevere(page, ['[data-testid="geodata-add-modal"]'])) {
          problems.push(`добавить в XKeen ${width}px: axe ${v}`);
        }
        await add.getByTestId('geodata-add-cancel').click();
        await expect(add).toBeHidden({ timeout: T.action });
      }

      await picker.getByTestId('list-picker-cancel').click();
      await expect(picker).toBeHidden({ timeout: T.action });
      await rule.getByTestId('rule-cancel').click();
      await expect(rule).toBeHidden({ timeout: T.action });
    }
    expect(problems, problems.join('\n')).toEqual([]);
    expect((await routingNow(page)).rules, 'черновик не изменён').toHaveLength(0);

    await guard.assertClean();
    await resetDraft(page);
  });
}
