// e2e-pages: config/rules
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, resetDraft } from './lib/nodes-api';

// Списки и пресеты на вкладке «Правила» на настоящей панели (фаза 146, план 22): выбор списка
// для условия geosite из каталога роутера и сохранение правила со ссылкой «файл · тег». Каталог
// читается настоящим маршрутом (GET /api/configlayer/routing/lists), ответы сети не
// подменяются. Флаг слоя включается на время файла и возвращается прежним, черновик
// сбрасывается до и после проверки. Ядро не читается и не меняется. В вывод не попадают имена
// узлов и групп.

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
