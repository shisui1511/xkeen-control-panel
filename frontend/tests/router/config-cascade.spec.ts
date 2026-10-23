// e2e-pages: config/groups
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import {
  addToDraft,
  api,
  installedKernels,
  layerFlag,
  nodeLink,
  randomPort,
  resetDraft
} from './lib/nodes-api';

// Каскад удаления на настоящей панели (фаза 146, план 21): удаление группы со ссылками
// (сводка, зависимости, перенаправление, последствия по ядрам), группы без ссылок и источника
// или узла, после которых группа остаётся пустой. Ответы сети не подменяются. Флаг слоя
// включается на время прогона и возвращается прежним, черновик сбрасывается; ядро не читается
// и не меняется. В вывод не попадают имена узлов.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

const AUTO_NAME = 'RT Каскад Авто';
const TARGET_NAME = 'RT Каскад Цель';

async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

async function openGroups(page: Page, width = 1440): Promise<void> {
  await page.setViewportSize({ width, height: width > 600 ? 900 : 844 });
  await page.goto('/#/config/groups', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  const tab = page.getByTestId('groups-tab');
  await expect(tab).toBeVisible({ timeout: T.action });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.action });
}

interface RoutingInfo {
  revision: number;
  groups: { id: string; name: string; members: { kind: string; id?: string }[] }[];
  rules: { id: string; target: { kind: string; id?: string } }[];
  final: { kind: string; id?: string };
  issues: { code: string; params?: Record<string, string> }[];
}

async function routingInfo(page: Page): Promise<RoutingInfo> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  const d = view.data ?? {};
  return {
    revision: Number(d.revision ?? 0),
    groups: (d.groups ?? []) as RoutingInfo['groups'],
    rules: (d.rules ?? []) as RoutingInfo['rules'],
    final: (d.final ?? { kind: 'direct' }) as RoutingInfo['final'],
    issues: (d.issues ?? []) as RoutingInfo['issues']
  };
}

/** Создаёт группу настоящим маршрутом сохранения и возвращает её идентификатор. */
async function createGroup(
  page: Page,
  group: { name: string; mode: string; members?: { kind: string; id?: string }[] }
): Promise<string> {
  const res = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
    revision: (await routingInfo(page)).revision,
    group: {
      id: '',
      sources: { all: true, ids: [] },
      include: [],
      exclude: [],
      regex: false,
      picks: [],
      members: [],
      reserve: '',
      ...group
    }
  });
  expect(res.status, 'сохранение группы через API').toBe(200);
  const id = String((res.data?.group as { id?: string } | undefined)?.id ?? '');
  expect(id, 'идентификатор группы').not.toBe('');
  return id;
}

interface Seeded {
  autoId: string;
  targetId: string;
  ruleId: string;
}

/**
 * Группа «Авто» (все источники) со ссылками: ручная группа-цель с ней в составе, правило с
 * целью на неё и «Всё остальное» на неё. Узел «Моих узлов» даёт Xray-балансиру состав.
 */
async function seedReferenced(page: Page, tag: string): Promise<Seeded> {
  await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), `Cas${tag}`));
  const autoId = await createGroup(page, { name: AUTO_NAME, mode: 'auto' });
  const targetId = await createGroup(page, {
    name: TARGET_NAME,
    mode: 'manual',
    members: [{ kind: 'group', id: autoId }, { kind: 'direct' }]
  });
  const rule = await api(page, 'POST', '/api/configlayer/routing/rules/save', {
    revision: (await routingInfo(page)).revision,
    rule: {
      id: '',
      enabled: true,
      origin: '',
      conds: [{ type: 'domain', values: ['rt-cascade.example'], lists: [] }],
      target: { kind: 'group', id: autoId },
      raw: null
    }
  });
  expect(rule.status, 'rules/save').toBe(200);
  const ruleId = String((rule.data?.rule as { id?: string } | undefined)?.id ?? '');
  expect(ruleId, 'идентификатор правила').not.toBe('');
  const fin = await api(page, 'POST', '/api/configlayer/routing/final', {
    revision: (await routingInfo(page)).revision,
    target: { kind: 'group', id: autoId }
  });
  expect(fin.status, 'routing/final').toBe(200);
  return { autoId, targetId, ruleId };
}

test('cascade: группа со ссылками — зависимости, последствия, перенаправление без висячих ссылок', async ({
  page
}) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await resetDraft(page);
  const inst = await installedKernels(page);
  try {
    const { autoId, targetId, ruleId } = await seedReferenced(page, 'A');

    await openGroups(page);
    await page.getByTestId(`group-delete-${autoId}`).click();
    const modal = page.getByTestId('group-delete-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('group-delete-summary')).toContainText(/\d/);
    // строка правила и строка группы-участника
    const deps = modal.getByTestId('group-delete-deps');
    await expect(deps.locator('li')).toHaveCount(2);
    await expect(deps).toContainText('rt-cascade.example');
    await expect(deps).toContainText(TARGET_NAME);
    // «Всё остальное» ссылается на группу: «Удалить эти правила» неактивно, замена обязательна
    await expect(modal.getByTestId('group-delete-drop')).toBeDisabled();
    await expect(modal.getByTestId('group-delete-final-required')).toBeVisible();
    await expect(modal.getByTestId('group-delete-redirect')).toBeChecked();
    // последствия по ядрам
    // (без установленных ядер последствий по ядрам нет)
    if (inst.xray || inst.mihomo) {
      await expect(modal.getByTestId('group-delete-effects')).toBeVisible();
      await expect(modal.getByTestId('group-delete-effects')).toContainText(AUTO_NAME);
    }
    if (inst.xray) {
      await expect(modal.getByTestId('group-delete-effects')).toContainText('Xray');
    }

    // перенаправление на группу-цель: правило и «Остальное» уходят на неё
    // замена по умолчанию — группа, в состав которой входила удаляемая (в наборе по умолчанию «Прокси»)
    await expect(modal.getByTestId('group-delete-target')).toHaveValue(`group:${targetId}`);
    await modal.getByTestId('group-delete-target').selectOption({ label: TARGET_NAME });
    await modal.getByTestId('group-delete-confirm').click();
    await expect(modal).toHaveCount(0, { timeout: T.action });
    await expect(
      page.locator('.toast--success').filter({ hasText: /удален|deleted/i })
    ).toBeVisible({ timeout: T.action });

    const after = await routingInfo(page);
    expect(
      after.groups.some((g) => g.id === autoId),
      'группа удалена'
    ).toBe(false);
    expect(after.rules.find((r) => r.id === ruleId)?.target, 'цель правила').toEqual({
      kind: 'group',
      id: targetId
    });
    expect(after.final, 'цель «Всё остальное»').toEqual({ kind: 'group', id: targetId });
    const target = after.groups.find((g) => g.id === targetId);
    expect(
      target?.members.some((m) => m.kind === 'group' && m.id === autoId),
      'в составе цели нет удалённой группы'
    ).toBe(false);
    expect(
      after.issues.some((i) => i.code === 'target_missing'),
      'висячих ссылок на цель нет'
    ).toBe(false);

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

test('cascade: группа без ссылок — простое подтверждение и тост', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await resetDraft(page);
  try {
    const id = await createGroup(page, {
      name: 'RT Без ссылок',
      mode: 'manual',
      members: [{ kind: 'direct' }]
    });

    await openGroups(page);
    await page.getByTestId(`group-delete-${id}`).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('group-delete-modal')).toHaveCount(0);
    await expect(dialog).toContainText(/Ссылок на неё нет|Nothing refers to it/);

    // отмена: группа остаётся
    await dialog.getByRole('button', { name: /Отмена|Cancel/ }).click();
    await expect(dialog).toHaveCount(0, { timeout: T.action });
    expect((await routingInfo(page)).groups.some((g) => g.id === id)).toBe(true);

    await page.getByTestId(`group-delete-${id}`).click();
    await expect(dialog).toBeVisible({ timeout: T.action });
    await dialog.locator('.btn-danger').click();
    await expect(dialog).toHaveCount(0, { timeout: T.action });
    await expect(
      page.locator('.toast--success').filter({ hasText: /удален|deleted/i })
    ).toBeVisible({ timeout: T.action });
    expect((await routingInfo(page)).groups.some((g) => g.id === id)).toBe(false);
    await expect(page.getByTestId(`group-card-${id}`)).toHaveCount(0);

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

/**
 * Нарушения critical и serious WCAG 2.0/2.1 A/AA в областях страницы: правило, id, число
 * узлов, цели и контраст. HTML узлов не выводится: он может содержать имена узлов.
 */
async function axeSevere(page: Page, scopes: string[]): Promise<string[]> {
  // диалог появляется с затуханием: axe не должен видеть полупрозрачный кадр
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
  test(`cascade axe: диалог удаления, тема ${theme}`, async ({ page }) => {
    test.setTimeout(T.save * 4);
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    await resetDraft(page);
    const inst = await installedKernels(page);
    const problems: string[] = [];
    try {
      const { autoId } = await seedReferenced(page, `X${theme}`);

      for (const width of [1280, 390]) {
        await openGroups(page, width);
        const at = `${width}px`;
        const applied = await page.evaluate(() =>
          document.documentElement.getAttribute('data-theme')
        );
        if (applied !== theme)
          problems.push(`${at}: тема применена ${applied}, ожидалась ${theme}`);

        await page.getByTestId(`group-delete-${autoId}`).click();
        const modal = page.getByTestId('group-delete-modal');
        await expect(modal).toBeVisible({ timeout: T.action });
        await expect(modal.getByTestId('group-delete-drop')).toBeDisabled();
        if (inst.xray || inst.mihomo) {
          await expect(modal.getByTestId('group-delete-effects')).toBeVisible();
        }
        for (const v of await axeSevere(page, ['[data-testid="group-delete-modal"]'])) {
          problems.push(`диалог удаления ${at}: axe ${v}`);
        }

        // «Отмена»: группа остаётся, черновик не меняется
        await modal.getByTestId('group-delete-cancel').click();
        await expect(modal).toHaveCount(0, { timeout: T.action });
        expect((await routingInfo(page)).groups.some((g) => g.id === autoId)).toBe(true);
      }
      expect(problems, problems.join('\n')).toEqual([]);

      await guard.assertClean();
    } finally {
      await resetDraft(page);
    }
  });
}
