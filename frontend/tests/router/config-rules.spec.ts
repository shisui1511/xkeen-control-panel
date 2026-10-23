// e2e-pages: config/rules
import { randomInt } from 'node:crypto';
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, resetDraft } from './lib/nodes-api';

// Вкладка «Правила» на настоящей панели (фаза 146, план 20): список правил из настоящего
// представления маршрутизации, порядок кнопками «вверх/вниз», выключение без удаления,
// цель «Всё остальное», перекрытие с кнопкой «Поднять выше», диалог правила и своё правило
// ядра. Правила создаются настоящими маршрутами API; ответы сети не подменяются. Флаг слоя
// включается на время файла и возвращается прежним, черновик сбрасывается до и после
// проверки. Ядро не читается и не меняется. В вывод не попадают имена узлов и групп.

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

interface SeedCond {
  type: string;
  values?: string[];
}

interface SeedRule {
  conds: SeedCond[];
  target?: { kind: string; id?: string };
  /** Своё правило ядра: условия при этом пустые. */
  raw?: { kernel: 'xray' | 'mihomo'; text: string };
}

/** Уникальный домен прогона: значения правил разных тестов не пересекаются. */
function domain(tag: string, n = 0): string {
  return `${tag}-${n}-${randomInt(100000, 999999)}.example.com`;
}

async function routingRevision(page: Page): Promise<number> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  return Number(view.data?.revision ?? 0);
}

/** Создаёт правило настоящим маршрутом сохранения и возвращает его идентификатор. */
async function seedRule(page: Page, seed: SeedRule): Promise<string> {
  const res = await api(page, 'POST', '/api/configlayer/routing/rules/save', {
    revision: await routingRevision(page),
    rule: {
      id: '',
      enabled: true,
      origin: '',
      conds: seed.conds.map((c) => ({ type: c.type, values: c.values ?? [], lists: [] })),
      target: seed.target ?? { kind: 'direct' },
      raw: seed.raw ?? null
    }
  });
  expect(res.status, 'rules/save').toBe(200);
  const id = (res.data?.rule as { id?: string } | undefined)?.id;
  expect(typeof id, 'идентификатор созданного правила').toBe('string');
  return id as string;
}

interface RuleInfo {
  id: string;
  index: number;
  enabled: boolean;
}

async function listRules(page: Page): Promise<RuleInfo[]> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  return (view.data?.rules ?? []) as RuleInfo[];
}

/** Идентификаторы правил в порядке списка. */
async function ruleOrder(page: Page): Promise<string[]> {
  return (await listRules(page)).map((r) => r.id);
}

async function finalKind(page: Page): Promise<string> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  return String((view.data?.final as { kind?: string } | undefined)?.kind ?? '');
}

test('rules: список — номера, чипы условий, цель и «+{n}» при четырёх значениях', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  const values = [domain('lst', 1), domain('lst', 2), domain('lst', 3), domain('lst', 4)];
  const a = await seedRule(page, { conds: [{ type: 'domain', values }] });
  const b = await seedRule(page, {
    conds: [
      { type: 'port', values: ['443'] },
      { type: 'network', values: ['udp'] }
    ],
    target: { kind: 'reject' }
  });
  const rules = await listRules(page);
  const idxA = rules.find((r) => r.id === a)?.index;
  const idxB = rules.find((r) => r.id === b)?.index;

  await openRules(page);
  await expect(page.getByTestId('rules-count')).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('rules-tab').locator('[data-testid^="rule-row-"]')).toHaveCount(
    rules.length
  );

  await expect(page.getByTestId(`rule-num-${a}`)).toHaveText(String(idxA));
  await expect(page.getByTestId(`rule-num-${b}`)).toHaveText(String(idxB));

  // четыре значения: три в чипе, остальное — чип «+1» с полным списком в title
  const conds = page.getByTestId(`rule-conds-${a}`);
  await expect(conds).toContainText(values[0]);
  await expect(conds).toContainText(values[2]);
  await expect(conds).not.toContainText(values[3]);
  const more = conds.locator('.chip', { hasText: '+1' });
  await expect(more).toHaveCount(1);
  expect(await more.getAttribute('title')).toContain(values[3]);
  await expect(page.getByTestId(`rule-row-${a}`).locator('.rr-target-chip')).toHaveText('DIRECT');

  // два условия: между ними «И», цель REJECT — красный чип
  const rowB = page.getByTestId(`rule-row-${b}`);
  await expect(page.getByTestId(`rule-conds-${b}`).locator('.rr-and')).toHaveCount(1);
  await expect(page.getByTestId(`rule-conds-${b}`).locator('.chip')).toHaveCount(2);
  await expect(rowB.locator('.rr-target-chip')).toHaveClass(/chip-danger/);
  await expect(rowB.locator('.rr-target-chip')).toHaveText('REJECT');

  await expect(page.getByTestId('rules-fallback')).toBeVisible();
  await guard.assertClean();
  await resetDraft(page);
});

test('rules: вверх/вниз — порядок в черновике, фокус на той же кнопке, объявление позиции', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  // Четыре правила: второе после переноса вниз не становится последним, и его кнопка
  // «вниз» остаётся активной (у границы фокус по UI-SPEC уходит на парную кнопку)
  const ids: string[] = [];
  for (let i = 1; i <= 4; i++) {
    ids.push(await seedRule(page, { conds: [{ type: 'domain', values: [domain('ord', i)] }] }));
  }
  const [r1, r2, r3] = ids;
  const base = await ruleOrder(page);
  const at = (order: string[], id: string) => order.indexOf(id);
  expect(at(base, r1) < at(base, r2) && at(base, r2) < at(base, r3)).toBe(true);

  await openRules(page);
  const live = page.getByTestId('rules-live');

  // второе правило вниз: оно уходит за третье, фокус остаётся на его кнопке «вниз»
  await page.getByTestId(`rule-down-${r2}`).click();
  await expect
    .poll(async () => at(await ruleOrder(page), r3) < at(await ruleOrder(page), r2), {
      timeout: T.action
    })
    .toBe(true);
  await expect(page.getByTestId(`rule-down-${r2}`)).toBeFocused({ timeout: T.action });
  const posAfterDown = at(await ruleOrder(page), r2) + 1;
  await expect(live).toContainText(String(posAfterDown));
  await expect(page.getByTestId(`rule-num-${r2}`)).toHaveText(String(posAfterDown));

  // и обратно вверх: порядок возвращается, фокус на «вверх» той же строки
  await page.getByTestId(`rule-up-${r2}`).click();
  await expect
    .poll(async () => at(await ruleOrder(page), r2) < at(await ruleOrder(page), r3), {
      timeout: T.action
    })
    .toBe(true);
  await expect(page.getByTestId(`rule-up-${r2}`)).toBeFocused({ timeout: T.action });
  await expect(live).toContainText(String(at(await ruleOrder(page), r2) + 1));

  // у границ кнопки неактивны: «вверх» у первого правила списка, «вниз» у последнего
  const order = await ruleOrder(page);
  await expect(page.getByTestId(`rule-up-${order[0]}`)).toBeDisabled();
  await expect(page.getByTestId(`rule-down-${order[order.length - 1]}`)).toBeDisabled();

  await guard.assertClean();
  await resetDraft(page);
});

test('rules: перетаскивание — линия вставки, порядок в черновике, объявление позиции', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  const ids: string[] = [];
  for (let i = 1; i <= 3; i++) {
    ids.push(await seedRule(page, { conds: [{ type: 'domain', values: [domain('dnd', i)] }] }));
  }
  const [r1, r2, r3] = ids;
  /** Порядок только тестовых правил: в черновике могут быть и другие. */
  const mine = async () => (await ruleOrder(page)).filter((id) => ids.includes(id));
  expect(await mine()).toEqual([r1, r2, r3]);

  await openRules(page);
  const live = page.getByTestId('rules-live');
  const row = (id: string) => page.getByTestId(`rule-row-${id}`);
  const grip = (id: string) => page.getByTestId(`rule-grip-${id}`);
  const lines = page.locator('[data-testid^="rule-row-"][class*="rr-drop-"]');

  // ручка видна при точном указателе, подпись с номером, в порядок фокуса не входит
  await expect(grip(r3)).toBeVisible({ timeout: T.action });
  await expect(grip(r3)).toHaveAttribute('aria-label', /.+/);
  await expect(grip(r3)).toHaveAttribute('draggable', 'true');

  // третье правило несут на верхнюю половину первого: линия над первым, источник приглушён
  await grip(r3).hover();
  await page.mouse.down();
  await row(r3).hover({ position: { x: 60, y: 4 } });
  // над собой линии нет
  await expect(lines).toHaveCount(0);
  await row(r1).hover({ position: { x: 60, y: 3 } });
  await row(r1).hover({ position: { x: 62, y: 3 } });
  await expect(row(r1)).toHaveClass(/rr-drop-before/, { timeout: T.action });
  await expect(row(r3)).toHaveClass(/rr-dragging/);
  const line = await row(r1).evaluate((el) => getComputedStyle(el).boxShadow);
  expect(line, 'линия вставки 2px').toMatch(/\b2px\b/);
  await page.mouse.up();

  await expect.poll(mine, { timeout: T.action }).toEqual([r3, r1, r2]);
  await expect(lines).toHaveCount(0);
  await expect(row(r3)).not.toHaveClass(/rr-dragging/);
  const pos3 = (await ruleOrder(page)).indexOf(r3) + 1;
  await expect(live).toContainText(String(pos3), { timeout: T.action });
  await expect(page.getByTestId(`rule-num-${r3}`)).toHaveText(String(pos3), {
    timeout: T.action
  });

  // и вниз: нижняя половина последнего правила — перенос в конец
  await grip(r3).hover();
  await page.mouse.down();
  await row(r2).hover({ position: { x: 60, y: 6 } });
  await row(r2).hover({ position: { x: 62, y: 6 } });
  const box = await row(r2).boundingBox();
  expect(box, 'строка последнего правила').not.toBeNull();
  await row(r2).hover({ position: { x: 60, y: (box?.height ?? 40) - 3 } });
  await row(r2).hover({ position: { x: 62, y: (box?.height ?? 40) - 3 } });
  await expect(row(r2)).toHaveClass(/rr-drop-after/, { timeout: T.action });
  await page.mouse.up();
  await expect.poll(mine, { timeout: T.action }).toEqual([r1, r2, r3]);

  // кнопки продолжают работать после перетаскивания; кнопка берёт позицию из списка
  // на экране, поэтому сначала ждём, пока он покажет новый порядок
  await expect(page.getByTestId(`rule-num-${r1}`)).toHaveText(
    String((await ruleOrder(page)).indexOf(r1) + 1),
    { timeout: T.action }
  );
  await page.getByTestId(`rule-down-${r1}`).click();
  await expect.poll(mine, { timeout: T.action }).toEqual([r2, r1, r3]);

  await guard.assertClean();
  await resetDraft(page);
});

test.describe('касание', () => {
  test.use({ hasTouch: true });

  test('rules: перетаскивание — на касании ручка скрыта, кнопки «вверх / вниз» работают', async ({
    page
  }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    await resetDraft(page);

    const a = await seedRule(page, { conds: [{ type: 'domain', values: [domain('tch', 1)] }] });
    const b = await seedRule(page, { conds: [{ type: 'domain', values: [domain('tch', 2)] }] });
    await openRules(page);
    await expect(page.getByTestId(`rule-row-${a}`)).toBeVisible({ timeout: T.action });

    const coarse = await page.evaluate(() => matchMedia('(pointer: coarse)').matches);
    expect(coarse, 'эмуляция касания включает (pointer: coarse)').toBe(true);
    await expect(page.getByTestId(`rule-grip-${a}`)).toBeHidden();
    await expect(page.getByTestId(`rule-grip-${b}`)).toBeHidden();

    // порядок меняется кнопкой
    const before = (await ruleOrder(page)).filter((id) => id === a || id === b);
    expect(before).toEqual([a, b]);
    await page.getByTestId(`rule-down-${a}`).tap();
    await expect
      .poll(async () => (await ruleOrder(page)).filter((id) => id === a || id === b), {
        timeout: T.action
      })
      .toEqual([b, a]);

    await guard.assertClean();
    await resetDraft(page);
  });
});

test('rules: выключение — чип «выключено» и приглушённый цвет текста, правило остаётся', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  const id = await seedRule(page, { conds: [{ type: 'domain', values: [domain('off', 1)] }] });
  await openRules(page);
  const row = page.getByTestId(`rule-row-${id}`);
  const sw = page.getByTestId(`rule-enabled-${id}`);
  await expect(sw).toBeChecked({ timeout: T.action });
  await expect(row.locator('[data-chip="disabled"]')).toHaveCount(0);

  // сам input скрыт (opacity 0): клик по ползунку
  await row.locator('.toggle-slider').click();
  await expect
    .poll(async () => (await listRules(page)).find((r) => r.id === id)?.enabled, {
      timeout: T.action
    })
    .toBe(false);
  await expect(sw).not.toBeChecked();
  await expect(row.locator('[data-chip="disabled"]')).toBeVisible({ timeout: T.action });

  // цвет текста строки — --fg-dim, прозрачность не используется
  const dim = await row.evaluate((el) => {
    const probe = document.createElement('span');
    probe.style.color = 'var(--fg-dim)';
    document.body.appendChild(probe);
    const want = getComputedStyle(probe).color;
    probe.remove();
    return {
      want,
      color: getComputedStyle(el).color,
      opacity: getComputedStyle(el).opacity
    };
  });
  expect(dim.color, 'цвет выключенного правила').toBe(dim.want);
  expect(dim.opacity).toBe('1');

  // включение возвращает всё как было
  await row.locator('.toggle-slider').click();
  await expect
    .poll(async () => (await listRules(page)).find((r) => r.id === id)?.enabled, {
      timeout: T.action
    })
    .toBe(true);
  await expect(row.locator('[data-chip="disabled"]')).toHaveCount(0, { timeout: T.action });

  await guard.assertClean();
  await resetDraft(page);
});

test('rules: «Всё остальное» — смена цели через Select записана в черновик', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  await seedRule(page, { conds: [{ type: 'domain', values: [domain('fin', 1)] }] });
  await openRules(page);
  const fallback = page.getByTestId('rules-fallback');
  await expect(fallback).toBeVisible({ timeout: T.action });
  const select = page.getByTestId('rules-fallback-select');

  await select.selectOption('reject');
  await expect.poll(() => finalKind(page), { timeout: T.action }).toBe('reject');
  await expect(select).toHaveValue('reject');

  await select.selectOption('direct');
  await expect.poll(() => finalKind(page), { timeout: T.action }).toBe('direct');
  await expect(select).toHaveValue('direct');

  // «Всё остальное» остаётся последней строкой и не имеет ни кнопок, ни переключателя
  await expect(fallback.locator('button')).toHaveCount(0);
  await expect(fallback.locator('input[type="checkbox"]')).toHaveCount(0);

  await guard.assertClean();
  await resetDraft(page);
});

test('rules: перекрытие — chip-warning и «Поднять выше» меняет порядок', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  // один домен и разные цели: второе правило никогда не сработает
  const same = domain('ovl', 1);
  const first = await seedRule(page, { conds: [{ type: 'domain', values: [same] }] });
  const second = await seedRule(page, {
    conds: [{ type: 'domain', values: [same] }],
    target: { kind: 'reject' }
  });

  await openRules(page);
  const rowSecond = page.getByTestId(`rule-row-${second}`);
  await expect(rowSecond.locator('[data-chip="overlap"]')).toBeVisible({ timeout: T.action });
  await expect(rowSecond.locator('[data-chip="overlap"]')).toHaveClass(/chip-warning/);
  await expect(page.getByTestId(`rule-raise-${second}`)).toBeVisible();
  // у верхнего правила перекрытия нет
  await expect(page.getByTestId(`rule-row-${first}`).locator('[data-chip="overlap"]')).toHaveCount(
    0
  );

  await page.getByTestId(`rule-raise-${second}`).click();
  await expect
    .poll(
      async () => {
        const order = await ruleOrder(page);
        return order.indexOf(second) < order.indexOf(first);
      },
      { timeout: T.action }
    )
    .toBe(true);
  // теперь перекрыто уже бывшее верхнее правило
  await expect(page.getByTestId(`rule-row-${first}`).locator('[data-chip="overlap"]')).toBeVisible({
    timeout: T.action
  });
  await expect(page.getByTestId(`rule-row-${second}`).locator('[data-chip="overlap"]')).toHaveCount(
    0
  );

  await guard.assertClean();
  await resetDraft(page);
});

const ALL_ROWS = '[data-testid^="rule-row-"]';

/** Открывает диалог нового правила и возвращает его. */
async function openNewRule(page: Page) {
  await page.getByTestId('rules-add').click();
  const modal = page.getByTestId('rule-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  return modal;
}

test('rules: диалог — создание и правка, пустое значение, «И», неверный CIDR, подсказка о дубле', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  // правило, значение которого потом повторится в диалоге
  const taken = domain('dlg', 0);
  const seeded = await seedRule(page, { conds: [{ type: 'domain', values: [taken] }] });
  const seededIndex = (await listRules(page)).find((r) => r.id === seeded)?.index;

  await openRules(page);
  const rows = page.locator(ALL_ROWS);
  const before = await rows.count();

  // пустое значение: ошибка у условия и неактивная кнопка
  let modal = await openNewRule(page);
  const save = modal.getByTestId('rule-save');
  await expect(save).toBeDisabled();
  await modal.getByTestId('rule-cond-values-0').focus();
  await modal.getByTestId('rule-cond-values-0').blur();
  await expect(modal.getByTestId('rule-cond-error-0')).toBeVisible({ timeout: T.action });
  await expect(save).toBeDisabled();

  // домен → DIRECT: строка с чипом домена и целью
  const created = domain('dlg', 1);
  await modal.getByTestId('rule-cond-values-0').fill(created);
  await expect(modal.getByTestId('rule-cond-error-0')).toHaveCount(0);
  await expect(save).toBeEnabled();
  await save.click();
  await expect(modal).toBeHidden({ timeout: T.action });
  await expect(rows).toHaveCount(before + 1, { timeout: T.action });
  const row = rows.filter({ hasText: created });
  await expect(row).toHaveCount(1);
  await expect(row.locator('.rr-target-chip')).toHaveText('DIRECT');

  // правка: условия и цель на месте, цель меняется на REJECT
  const createdId = (await listRules(page)).find((r) => r.index === before + 1)?.id as string;
  await page.getByTestId(`rule-edit-${createdId}`).click();
  modal = page.getByTestId('rule-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('rule-cond-values-0')).toHaveValue(created);
  await expect(modal.getByTestId('rule-target')).toHaveValue('direct');
  await modal.getByTestId('rule-target').selectOption('reject');
  await modal.getByTestId('rule-save').click();
  await expect(modal).toBeHidden({ timeout: T.action });
  await expect(page.getByTestId(`rule-row-${createdId}`).locator('.rr-target-chip')).toHaveText(
    'REJECT',
    { timeout: T.action }
  );
  await expect(rows).toHaveCount(before + 1);

  // два условия, домен и порт: между ними «И», сохраняется одна строка
  modal = await openNewRule(page);
  const second = domain('dlg', 2);
  await modal.getByTestId('rule-cond-values-0').fill(second);
  await modal.getByTestId('rule-add-cond').click();
  await expect(modal.locator('.re-and')).toHaveCount(1);
  await modal.getByTestId('rule-cond-type-1').selectOption('port');
  await modal.getByTestId('rule-cond-values-1').fill('443');
  await modal.getByTestId('rule-save').click();
  await expect(modal).toBeHidden({ timeout: T.action });
  await expect(rows).toHaveCount(before + 2, { timeout: T.action });
  const two = rows.filter({ hasText: second });
  await expect(two).toHaveCount(1);
  await expect(two.locator('.rr-and')).toHaveCount(1);

  // неверный CIDR: ошибка под условием, кнопка неактивна
  modal = await openNewRule(page);
  await modal.getByTestId('rule-cond-type-0').selectOption('ip');
  await modal.getByTestId('rule-cond-values-0').fill('10.0.0.999/8');
  await expect(modal.getByTestId('rule-cond-error-0')).toContainText('10.0.0.999/8', {
    timeout: T.action
  });
  await expect(modal.getByTestId('rule-save')).toBeDisabled();
  await modal.getByTestId('rule-cancel').click();
  await expect(modal).toBeHidden({ timeout: T.action });

  // значение уже есть в другом правиле: подсказка с номером, сохранить можно
  modal = await openNewRule(page);
  await modal.getByTestId('rule-cond-values-0').fill(taken);
  const hint = modal.getByTestId('rule-cond-dup-0');
  await expect(hint).toBeVisible({ timeout: T.action });
  await expect(hint).toContainText(String(seededIndex));
  await expect(modal.getByTestId('rule-save')).toBeEnabled();
  await modal.getByTestId('rule-cancel').click();
  await expect(modal).toBeHidden({ timeout: T.action });
  await expect(rows).toHaveCount(before + 2);

  await guard.assertClean();
  await resetDraft(page);
});

test('rules: своё правило ядра — Mihomo, отказ ядра, BitTorrent «только Xray»', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  await openRules(page);
  const rows = page.locator(ALL_ROWS);
  const rawTab = /Своё правило ядра|Custom kernel rule/;

  /** Включает «Эксперт» и открывает сегмент «Своё правило ядра». */
  async function openRaw() {
    const modal = await openNewRule(page);
    await modal.locator('.toggle-slider').click();
    await modal.getByRole('button', { name: rawTab }).click();
    await expect(modal.getByTestId('rule-raw')).toBeVisible({ timeout: T.action });
    return modal;
  }

  // строка Mihomo принимается: строка списка с чипом «только Mihomo»
  let modal = await openRaw();
  await modal.getByTestId('rule-raw-kernel').selectOption('mihomo');
  await modal.getByTestId('rule-raw-text').fill('PROCESS-NAME,example');
  await modal.getByTestId('rule-save').click();
  await expect(modal).toBeHidden({ timeout: T.action });
  const mihomoRule = (await ruleOrder(page)).pop() as string;
  await expect(
    page.getByTestId(`rule-row-${mihomoRule}`).locator('[data-chip="only-mihomo"]')
  ).toBeVisible({ timeout: T.action });

  // MATCH ядро не принимает: ошибка сервера под полем, диалог остаётся открытым
  const count = await rows.count();
  modal = await openRaw();
  await modal.getByTestId('rule-raw-kernel').selectOption('mihomo');
  await modal.getByTestId('rule-raw-text').fill('MATCH,DIRECT');
  await modal.getByTestId('rule-save').click();
  await expect(modal.getByTestId('rule-raw-error')).toContainText(
    /Ядро не приняло правило|did not accept the rule/,
    { timeout: T.action }
  );
  await expect(modal).toBeVisible();
  await modal.getByTestId('rule-cancel').click();
  await expect(modal).toBeHidden({ timeout: T.action });
  await expect(rows).toHaveCount(count);

  // BitTorrent: условие без значений, строка помечена «только Xray»
  modal = await openNewRule(page);
  await modal.getByTestId('rule-cond-type-0').selectOption('bittorrent');
  await expect(modal.getByTestId('rule-save')).toBeEnabled();
  await modal.getByTestId('rule-save').click();
  await expect(modal).toBeHidden({ timeout: T.action });
  const bt = (await ruleOrder(page)).pop() as string;
  await expect(page.getByTestId(`rule-row-${bt}`).locator('[data-chip="only-xray"]')).toBeVisible({
    timeout: T.action
  });

  await guard.assertClean();
  await resetDraft(page);
});

// --- 390 / 768 / 1280 px, обе темы и проход axe (фаза 146, план 29) ---

interface Showcase {
  /** Правило с четырьмя доменами, один из них длинный: чип «+1» и перенос значения. */
  many: string;
  /** Два условия через «И», цель REJECT. */
  pair: string;
  disabled: string;
  /** Второе из двух правил с одним доменом и разными целями: чип перекрытия. */
  overlapped: string;
  mihomo: string;
}

/** Строки всех видов: чип «+{n}», выключенное, перекрытое, «только Mihomo» (и «Всё остальное»). */
async function seedShowcase(page: Page): Promise<Showcase> {
  const long = `${'x'.repeat(55)}-${randomInt(100000, 999999)}.example.com`;
  const many = await seedRule(page, {
    conds: [
      { type: 'domain', values: [long, domain('shw', 2), domain('shw', 3), domain('shw', 4)] }
    ]
  });
  const pair = await seedRule(page, {
    conds: [
      { type: 'port', values: ['443'] },
      { type: 'network', values: ['udp'] }
    ],
    target: { kind: 'reject' }
  });
  const off = await seedRule(page, { conds: [{ type: 'domain', values: [domain('shw', 5)] }] });
  const res = await api(page, 'POST', '/api/configlayer/routing/rules/enable', {
    revision: await routingRevision(page),
    id: off,
    enabled: false
  });
  expect(res.status, 'rules/enable').toBe(200);
  const same = domain('shw', 6);
  await seedRule(page, { conds: [{ type: 'domain', values: [same] }] });
  const overlapped = await seedRule(page, {
    conds: [{ type: 'domain', values: [same] }],
    target: { kind: 'reject' }
  });
  const mihomo = await seedRule(page, {
    conds: [],
    raw: { kernel: 'mihomo', text: 'PROCESS-NAME,example' }
  });
  return { many, pair, disabled: off, overlapped, mihomo };
}

/** Страница с темой, заданной до загрузки. */
async function startThemed(page: Page, theme: 'dark' | 'light'): Promise<void> {
  await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
  await page.setViewportSize({ width: 1280, height: 900 });
  await startPage(page);
}

async function appliedTheme(page: Page): Promise<string | null> {
  return page.evaluate(() => document.documentElement.getAttribute('data-theme'));
}

for (const theme of ['dark', 'light'] as const) {
  // Один тест на тему проходит три ширины: 390, 768 и 1280 px
  test(`rules: 390 px, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await startThemed(page, theme);
    await resetDraft(page);
    const shw = await seedShowcase(page);
    await openRules(page);
    expect(await appliedTheme(page), 'тема применена').toBe(theme);

    const problems: string[] = [];
    for (const width of [390, 768, 1280]) {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
      await expect(page.getByTestId(`rule-row-${shw.many}`)).toBeVisible({ timeout: T.action });
      const narrow = width <= 768;

      const found = await page.evaluate(
        ({ id, narrow }) => {
          const out: string[] = [];
          const root = document.documentElement;
          if (root.scrollWidth - root.clientWidth > 0)
            out.push(`прокрутка страницы ${root.scrollWidth - root.clientWidth}px`);
          const tab = document.querySelector('[data-testid="rules-tab"]') as HTMLElement;
          const tabBox = tab.getBoundingClientRect();
          const rows = [...tab.querySelectorAll<HTMLElement>('[data-testid^="rule-row-"]')];
          rows.forEach((row, i) => {
            const rb = row.getBoundingClientRect();
            if (row.scrollWidth - row.clientWidth > 0)
              out.push(`строка ${i + 1}: шире своей области`);
            for (const el of row.querySelectorAll<HTMLElement>('.chip, .rr-actions, button')) {
              const b = el.getBoundingClientRect();
              if (b.width > 0 && (b.right > rb.right + 1 || b.left < rb.left - 1))
                out.push(`строка ${i + 1}: ${el.className.split(' ')[0] || el.tagName} вне строки`);
            }
            if (narrow) {
              for (const el of row.querySelectorAll<HTMLElement>('.rr-act, .rr-raise')) {
                const b = el.getBoundingClientRect();
                if (b.width < 43.5 || b.height < 43.5)
                  out.push(
                    `строка ${i + 1}: кнопка ${Math.round(b.width)}x${Math.round(b.height)}`
                  );
              }
            }
          });
          const fb = document.querySelector('[data-testid="rules-fallback"]') as HTMLElement;
          if (fb.getBoundingClientRect().right > tabBox.right + 1)
            out.push('«Всё остальное» шире вкладки');

          // раскладка строки с несколькими значениями: три ряда на узком, одна строка на широком
          const row = document.querySelector(`[data-testid="rule-row-${id}"]`) as HTMLElement;
          const box = (sel: string) =>
            (row.querySelector(sel) as HTMLElement).getBoundingClientRect();
          const conds = box('.rr-conds');
          const actions = box('.rr-actions');
          const target = box('.rr-target');
          const gripShown =
            getComputedStyle(row.querySelector('.rr-grip') as HTMLElement).display !== 'none';
          if (narrow) {
            if (conds.top < actions.bottom - 1) out.push('условия не ниже кнопок');
            if (target.top < conds.bottom - 1) out.push('цель не ниже условий');
            if (gripShown) out.push('ручка видна на узком экране');
          } else {
            if (conds.top >= actions.bottom) out.push('условия и кнопки не в одной строке');
            if (target.top >= conds.bottom) out.push('цель и условия не в одной строке');
          }
          return out;
        },
        { id: shw.many, narrow }
      );
      for (const v of found) problems.push(`${width}px: ${v}`);
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
    await resetDraft(page);
  });
}

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри заданных областей. Вывод — правило, селекторы и числа контраста: HTML узлов
 * может содержать имена узлов и групп.
 */
async function axeSevere(page: Page, scopes: string[]): Promise<string[]> {
  // смена ширины и открытие диалога идут с затуханием: axe не должен видеть полупрозрачный кадр
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
  test(`rules axe: строки и диалог, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await startThemed(page, theme);
    await resetDraft(page);
    const shw = await seedShowcase(page);
    await openRules(page);

    const problems: string[] = [];
    const applied = await appliedTheme(page);
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);

    // строки всех видов: вкладка целиком на 1280 и 390 px
    for (const width of [1280, 390]) {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
      // области должны быть видимы: include без совпадений axe считает ошибкой
      await expect(page.getByTestId(`rule-row-${shw.mihomo}`)).toBeVisible({ timeout: T.action });
      await expect(page.getByTestId(`rule-row-${shw.overlapped}`)).toBeVisible();
      await expect(page.getByTestId(`rule-row-${shw.pair}`)).toBeVisible();
      await expect(page.getByTestId(`rule-row-${shw.disabled}`)).toBeVisible();
      await expect(page.getByTestId('rules-fallback')).toBeVisible();
      for (const v of await axeSevere(page, ['[data-testid="rules-tab"]'])) {
        problems.push(`строки ${width}px: axe ${v}`);
      }
    }

    // диалог правила: два условия, затем «Эксперт» и сегмент «Своё правило ядра»
    await page.setViewportSize({ width: 1280, height: 900 });
    const modal = await openNewRule(page);
    await modal.getByTestId('rule-cond-values-0').fill(domain('axe', 1));
    await modal.getByTestId('rule-add-cond').click();
    await modal.getByTestId('rule-cond-type-1').selectOption('port');
    await modal.getByTestId('rule-cond-values-1').fill('443');
    await expect(modal.locator('.re-and')).toHaveCount(1);
    const scope = ['[data-testid="rule-modal"]'];

    for (const v of await axeSevere(page, scope)) problems.push(`диалог 1280px: axe ${v}`);

    await modal.locator('.toggle-slider').click();
    await modal.getByRole('button', { name: /Своё правило ядра|Custom kernel rule/ }).click();
    await expect(modal.getByTestId('rule-raw')).toBeVisible({ timeout: T.action });
    await modal.getByTestId('rule-raw-kernel').selectOption('mihomo');
    await modal.getByTestId('rule-raw-text').fill('PROCESS-NAME,example');
    for (const v of await axeSevere(page, scope)) {
      problems.push(`диалог, своё правило 1280px: axe ${v}`);
    }

    await page.setViewportSize({ width: 390, height: 844 });
    await expect(modal).toBeVisible();
    for (const v of await axeSevere(page, scope)) {
      problems.push(`диалог, своё правило 390px: axe ${v}`);
    }

    await modal.getByRole('button', { name: /^(Условия|Conditions)$/ }).click();
    await expect(modal.getByTestId('rule-cond-values-0')).toBeVisible({ timeout: T.action });
    for (const v of await axeSevere(page, scope)) problems.push(`диалог, условия 390px: axe ${v}`);

    // Escape закрывает диалог, в черновик ничего не попадает
    await page.keyboard.press('Escape');
    await expect(modal).toBeHidden({ timeout: T.action });
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
    await resetDraft(page);
  });
}
