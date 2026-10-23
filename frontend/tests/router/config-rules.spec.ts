// e2e-pages: config/rules
import { randomInt } from 'node:crypto';
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
      raw: null
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

  const ids: string[] = [];
  for (let i = 1; i <= 3; i++) {
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
