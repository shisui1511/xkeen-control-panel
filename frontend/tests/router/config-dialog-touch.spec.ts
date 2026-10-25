// e2e-pages: config/rules config/groups config/nodes
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import {
  addToDraft,
  api,
  layerFlag,
  listSources,
  nodeLink,
  randomPort,
  resetDraft
} from './lib/nodes-api';

// Цели касания в подвалах диалогов раздела «Конфигурация» на настоящей панели: на узком
// экране и на касании (там плотность compact сжимает .btn до 32 px) кнопки подвала не ниже
// 44 px. Ответы сети не подменяются: черновик наполняют настоящие маршруты предпросмотра и
// добавления, подписка — адрес зарезервированной зоны .invalid. Флаг слоя включается на время
// прогона и возвращается прежним, черновик сбрасывается; диалоги закрываются без сохранения.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => layer.enable(browser));
test.afterAll(async ({ browser }) => layer.restore(browser));

const BAD_SUB = 'https://sub.invalid/feed';

async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

async function openTab(page: Page, route: string, tab: string): Promise<void> {
  await page.goto(`/#/config/${route}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId(tab)).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId(tab).locator('.skeleton')).toHaveCount(0, { timeout: T.page });
}

/** Кнопки подвала ниже 44 px: «подпись ширина×высота». */
async function shortButtons(modal: Locator, footer: string): Promise<string[]> {
  await expect(modal.locator(`${footer} .btn`).first()).toBeVisible({ timeout: T.action });
  return modal.locator(`${footer} .btn`).evaluateAll((els) =>
    els
      .map((el) => ({ el, b: el.getBoundingClientRect() }))
      .filter(({ b }) => b.width > 0 && b.height < 43.5)
      .map(
        ({ el, b }) => `«${el.textContent?.trim()}» ${Math.round(b.width)}x${Math.round(b.height)}`
      )
  );
}

/** Открывает диалоги вкладок по очереди и собирает кнопки подвала ниже 44 px. */
async function footerProblems(page: Page, width: number, height: number): Promise<string[]> {
  await page.setViewportSize({ width, height });
  await startPage(page);
  await resetDraft(page);
  const port = randomPort();
  await addToDraft(page, [BAD_SUB, nodeLink('vless_ws.txt', port, 'TouchA')].join('\n'));
  const sub = (await listSources(page)).find((s) => s.host === 'sub.invalid');
  expect(sub, 'подписка из черновика').toBeTruthy();

  const density = await page.evaluate(() => document.documentElement.dataset.density);
  expect(density, 'на этой ширине авто-плотность compact').toBe('compact');

  const problems: string[] = [];
  const check = async (name: string, modal: Locator, footer: string) => {
    await expect(modal, name).toBeVisible({ timeout: T.action });
    for (const v of await shortButtons(modal, footer)) problems.push(`${name}: ${v}`);
  };
  const close = async (modal: Locator) => {
    await page.keyboard.press('Escape');
    await expect(modal).toHaveCount(0, { timeout: T.action });
  };

  // правила: диалог правила, выбор списка поверх него, пресеты
  await openTab(page, 'rules', 'rules-tab');
  await page.getByTestId('rules-add').click();
  const rule = page.getByTestId('rule-modal');
  await check('правило', rule, '.re-footer');
  await rule.getByTestId('rule-cond-type-0').selectOption('geosite');
  await rule.getByTestId('rule-cond-pick-0').click();
  const picker = page.getByTestId('list-picker');
  await expect(picker.getByTestId('list-picker-loading')).toHaveCount(0, { timeout: T.page });
  await check('выбор списка', picker, '.lp-footer');
  await close(picker);
  await close(rule);
  await page.getByTestId('rules-preset').click();
  const preset = page.getByTestId('preset-modal');
  await expect(preset.getByTestId('preset-loading')).toHaveCount(0, { timeout: T.page });
  await check('пресет', preset, '.pm-footer');
  await close(preset);

  // группы: диалог группы и удаление существующей группы
  await openTab(page, 'groups', 'groups-tab');
  await page.getByTestId('groups-add').first().click();
  const group = page.getByTestId('group-modal');
  await check('группа', group, '.gm-footer');
  await close(group);
  const routing = await api(page, 'GET', '/api/configlayer/routing');
  expect(routing.status, 'GET /api/configlayer/routing').toBe(200);
  const groups = (routing.data?.groups ?? []) as { id: string }[];
  if (groups.length > 0) {
    await page.getByTestId(`group-delete-${groups[0].id}`).click();
    const del = page.getByTestId('group-delete-modal');
    await check('удаление группы', del, '.gd-footer');
    await close(del);
  } else {
    test.info().annotations.push({ type: 'note', description: 'групп нет: удаление не проверено' });
  }

  // узлы: настройки подписки и правка узла
  await openTab(page, 'nodes', 'nodes-tab');
  await page.getByTestId(`nodes-settings-${sub!.id}`).click();
  const settings = page.getByTestId('nodes-settings-modal');
  await check('настройки подписки', settings, '.ss-footer');
  await close(settings);
  const toggle = page.getByTestId('nodes-source-my').locator('.sc-expand');
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await page
    .locator('.node-row')
    .filter({ hasText: `:${port}` })
    .first()
    .locator('[data-testid^="nodes-edit-"]')
    .click();
  const edit = page.getByTestId('nodes-edit-modal');
  await expect(edit.getByTestId('nodes-edit-text')).toBeVisible({ timeout: T.action });
  // предпросмотр исходного текста закончился: закрытие раньше обрывает его запрос
  await expect(edit.getByTestId('nodes-edit-save')).toBeEnabled({ timeout: T.action });
  await check('правка узла', edit, '.ne-footer');
  await close(edit);

  await resetDraft(page);
  return problems;
}

test('dialogs: 390 px — кнопки подвала не ниже 44 px', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  const problems = await footerProblems(page, 390, 844);
  expect(problems, problems.join('\n')).toEqual([]);
  await guard.assertClean();
});

test.describe('касание', () => {
  test.use({ hasTouch: true });

  test('dialogs: касание на 1024 px — кнопки подвала не ниже 44 px', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1024, height: 768 });
    await startPage(page);
    const coarse = await page.evaluate(() => matchMedia('(pointer: coarse)').matches);
    expect(coarse, 'эмуляция касания включает (pointer: coarse)').toBe(true);
    const problems = await footerProblems(page, 1024, 768);
    expect(problems, problems.join('\n')).toEqual([]);
    await guard.assertClean();
  });
});
