// e2e-pages: config/lists
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, withPanel } from './lib/nodes-api';

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

/** Включает или выключает флаг слоя на время одного сценария (прежнее состояние вернёт layer.restore). */
async function setLayerFlag(browser: Browser, enabled: boolean): Promise<void> {
  await withPanel(browser, async (page) => {
    const res = await api(page, 'POST', '/api/settings/config-layer', { enabled });
    expect(res.status, enabled ? 'включение слоя' : 'выключение слоя').toBe(200);
  });
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

test('lists: #/dat ведёт на вкладку при флаге', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/#/dat', { timeout: T.page, waitUntil: 'domcontentloaded' });

  await expect(page).toHaveURL(/#\/config\/lists$/, { timeout: T.page });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  await tabReady(page, 'lists-tab');
  await expect(page.getByTestId('config-tab-lists')).toHaveAttribute('aria-selected', 'true');
  // пункта меню старой страницы нет
  await expect(page.locator('a[href="#/dat"]')).toHaveCount(0);

  await guard.assertClean();
});

test.describe('флаг выключен', () => {
  // флаг возвращается включённым для остальных сценариев; исходное состояние вернёт afterAll
  test.afterEach(async ({ browser }) => {
    await setLayerFlag(browser, true);
  });

  test('lists: с выключенным флагом всё как раньше', async ({ page, browser }) => {
    await setLayerFlag(browser, false);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });

    // старая страница DAT на месте, пункт меню есть
    await page.goto('/#/dat', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
      timeout: T.page
    });
    await expect(page).toHaveURL(/#\/dat$/);
    await expect(page.locator('a[href="#/dat"]').first()).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('lists-tab')).toHaveCount(0);

    // раздела «Конфигурация» нет: адрес вкладки уводит на обзор по общему правилу раздела
    await page.goto('/#/config/lists', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page).toHaveURL(/#\/dashboard$/, { timeout: T.page });
    await expect(page.getByTestId('lists-tab')).toHaveCount(0);

    await guard.assertClean();
  });
});
