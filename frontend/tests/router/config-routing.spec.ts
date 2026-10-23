// e2e-pages: config/groups config/rules
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, resetDraft } from './lib/nodes-api';

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
