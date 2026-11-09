// e2e-pages: config/files config/cores config/nodes config/rules editor constructor mihomo-gen proxies rules
// router-area: ui-shell
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, withPanel } from './lib/nodes-api';

// Входы в настройку при включённом слое «Конфигурация» (фаза 149, D-01, D-16):
// пункт «Редактор» и адрес #/editor перенаправляются во вкладку «Файлы»,
// конструкторы и устаревшие вкладки («Провайдеры», «Исключения») скрыты и
// перенаправлены в соответствующие подразделы «Конфигурации». Без флага вкладка
// «Исключения» и её адрес прежние: legacy-redirects на время проверки выключает слой
// и возвращает его. Ответы сети не подменяются.

const layer = layerFlag();

/** Переключает флаг слоя настоящим маршрутом настроек (сессия и CSRF панели). */
async function setLayer(browser: Browser, on: boolean): Promise<void> {
  await withPanel(browser, async (panel) => {
    const res = await api(panel, 'POST', '/api/settings/config-layer', { enabled: on });
    expect(res.status, `флаг слоя → ${on ? 'вкл' : 'выкл'} (${String(res.data?.code ?? '')})`).toBe(
      200
    );
    expect(res.data?.config_layer, 'флаг слоя в ответе').toBe(on);
  });
}

/** Перезагружает страницу и ждёт, что панель сама прочитала флаг слоя с ожидаемым значением. */
async function reloadWithFlag(page: Page, on: boolean): Promise<void> {
  const [res] = await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === 'GET' && new URL(r.url()).pathname === '/api/settings',
      { timeout: T.page }
    ),
    page.reload({ timeout: T.page, waitUntil: 'domcontentloaded' })
  ]);
  expect(res.status(), 'GET /api/settings после перезагрузки').toBe(200);
  const body = (await res.json()) as { data?: { config_layer?: unknown } };
  expect(body.data?.config_layer, 'флаг слоя, прочитанный панелью').toBe(on);
}

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

test('config-entries legacy-redirects vp:1280', async ({ page, browser }) => {
  // выключение слоя переносит файлы и перезапускает ядро, включение пересобирает их
  test.setTimeout(T.save * 3);
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

  // Без флага всё по-старому (D-16): слой временно выключается настоящим маршрутом, вкладка
  // «Исключения» на месте и её адрес не перенаправляется. Флаг возвращается в finally, затем
  // повторно проверяются перенаправление и отсутствие вкладки. Черновик не применяется.
  const exceptions = page.getByTestId('tab-exceptions');
  try {
    await setLayer(browser, false);
    const offGuard = attachConsoleGuard(page);
    // сначала перезагрузка на #/rules: панель перечитывает флаг, иначе прежнее значение в
    // памяти перенаправило бы адрес исключений
    await reloadWithFlag(page, false);
    await page.goto('/#/rules?tab=exceptions', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await expect(exceptions).toBeVisible({ timeout: T.page });
    await expect(exceptions).toBeEnabled();
    await exceptions.click();
    await expect(exceptions).toHaveAttribute('aria-selected', 'true');
    await expect(page, 'адрес исключений без флага не перенаправляется').toHaveURL(
      /#\/rules\?tab=exceptions$/
    );
    await offGuard.assertClean();
  } finally {
    await setLayer(browser, true);
  }

  const onGuard = attachConsoleGuard(page);
  // перезагрузка на #/rules?tab=exceptions: при включённом флаге адрес снова ведёт в «Конфигурацию»
  await reloadWithFlag(page, true);
  await page.waitForURL((url) => url.hash.startsWith('#/config/rules'), { timeout: T.action });
  await page.goto('/#/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');
  await expect(exceptions).toHaveCount(0);
  await onGuard.assertClean();
});
