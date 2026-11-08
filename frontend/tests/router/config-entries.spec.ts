// e2e-pages: config/files config/cores config/nodes config/rules editor constructor mihomo-gen proxies rules
// router-area: ui-shell
import { test, expect } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { layerFlag } from './lib/nodes-api';

// Входы в настройку при включённом слое «Конфигурация» (фаза 149, D-01, D-16):
// пункт «Редактор» и адрес #/editor перенаправляются во вкладку «Файлы»,
// конструкторы и устаревшие вкладки («Провайдеры», «Исключения») скрыты и
// перенаправлены в соответствующие подразделы «Конфигурации». Ответы сети не подменяются.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

const VP_DESKTOP = { width: 1280, height: 800 };

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

test('config-entries editor-redirect vp:1280', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize(VP_DESKTOP);

  await page.goto('/#/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');

  // Пункт меню «Редактор» имеет href="#/config/files" при включённом флаге
  const editorNavItem = page.locator('nav.sidebar-nav a[href="#/config/files"]');
  await expect(editorNavItem).toBeVisible({ timeout: T.action });

  // Переход на #/editor перенаправляется на #/config/files и открывает files-tab
  await page.goto('/#/editor', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForURL((url) => url.hash.startsWith('#/config/files'), { timeout: T.action });
  await expect(page.getByTestId('files-tab')).toBeVisible({ timeout: T.action });

  await guard.assertClean();
});

test('config-entries legacy-redirects vp:1280', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize(VP_DESKTOP);

  // #/constructor ведёт на #/config/cores
  await page.goto('/#/constructor', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForURL((url) => url.hash.startsWith('#/config/cores'), { timeout: T.action });

  // #/mihomo-gen ведёт на #/config/cores
  await page.goto('/#/mihomo-gen', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForURL((url) => url.hash.startsWith('#/config/cores'), { timeout: T.action });

  // #/proxies?tab=providers ведёт на #/config/nodes
  await page.goto('/#/proxies?tab=providers', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForURL((url) => url.hash.startsWith('#/config/nodes'), { timeout: T.action });

  // #/rules?tab=exceptions ведёт на #/config/rules
  await page.goto('/#/rules?tab=exceptions', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForURL((url) => url.hash.startsWith('#/config/rules'), { timeout: T.action });

  // На #/proxies нет вкладки «Провайдеры»
  await page.goto('/#/proxies', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');
  await expect(
    page.locator('.tabs-bar [data-value="providers"], .tabs [data-value="providers"]')
  ).toHaveCount(0);

  // На #/rules нет вкладки tab-exceptions
  await page.goto('/#/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('tab-exceptions')).toHaveCount(0);

  await guard.assertClean();
});
