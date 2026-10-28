// e2e-pages: config/lists
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag } from './lib/nodes-api';

// Вкладка «Списки» раздела «Конфигурация» (менеджер geodata XKeen, фаза 146.1, план 09):
// открытие кликом, перенаправление со старого адреса #/dat при включённом флаге слоя и
// прежнее поведение при выключенном. Ответы сети не подменяются. Флаг слоя включается на
// время прогона и возвращается прежним. Ядро не читается и не меняется.

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

/** Вкладка загружена: она видна и скелетона больше нет. */
async function tabReady(page: Page, testId: string): Promise<void> {
  const tab = page.getByTestId(testId);
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
}

test('lists: вкладка открывается кликом', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openConfig(page, '#/config');

  await page.getByTestId('config-tab-lists').click();
  await expect(page).toHaveURL(/#\/config\/lists$/);
  await expect(page.getByTestId('config-tab-lists')).toHaveAttribute('aria-selected', 'true');
  await tabReady(page, 'lists-tab');
  await expect(page.getByTestId('nodes-tab')).toHaveCount(0);

  // вкладка стоит между «Правила» и «Файлы панели»
  const order = await page
    .getByTestId('config-tabs')
    .locator('[data-testid^="config-tab-"]')
    .evaluateAll((els) => els.map((el) => el.getAttribute('data-testid')));
  expect(order).toEqual([
    'config-tab-nodes',
    'config-tab-groups',
    'config-tab-rules',
    'config-tab-lists',
    'config-tab-files'
  ]);

  // строка сводки совпадает с числом файлов в снимке менеджера
  const view = await api(page, 'GET', '/api/configlayer/geodata');
  expect(view.status, 'GET /api/configlayer/geodata').toBe(200);
  const files = Number(view.data?.files);
  expect(Number.isFinite(files), 'в снимке есть число файлов').toBe(true);
  await expect(page.getByTestId('geo-summary')).toHaveText(
    new RegExp(`(Файлов|Files): ${files}(?!\\d)`),
    { timeout: T.action }
  );

  await guard.assertClean();
});
