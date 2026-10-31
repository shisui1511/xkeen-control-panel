// e2e-pages: config/cores
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag } from './lib/nodes-api';

// Плашка профиля Mihomo над секциями ядра (фаза 147, план 20): владелец профиля, «Управлять
// в панели» через диалог взятия маршрутизации со строкой о переносе секций, причина
// выключенного блока api Xray ниже минимальной версии, доступность и 390 px. Ответы сети не
// подменяются. Флаг слоя включается на время прогона и возвращается прежним; сами тесты
// только открывают вкладку и диалог и закрывают его «Отменой» — в черновик и на диск не
// пишут. Ядро не запускается, не останавливается и не меняется.

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

/** Вид Mihomo настоящего API: установлено ли ядро и кто владеет профилем. */
async function mihomoView(page: Page): Promise<{ installed: boolean; owner: string }> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=mihomo');
  expect(view.status, 'GET /api/configlayer/cores?kernel=mihomo').toBe(200);
  const profile = (view.data?.profile ?? {}) as { owner?: string };
  return { installed: view.data?.installed === true, owner: profile.owner ?? '' };
}

/** Ревизия черновика по снимку слоя. */
async function draftRevision(page: Page): Promise<number> {
  const snap = await api(page, 'GET', '/api/configlayer/state');
  expect(snap.status, 'GET /api/configlayer/state').toBe(200);
  return Number(snap.data?.draft_revision);
}

test('cores: плашка профиля Mihomo', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const mihomo = await mihomoView(page);
  test.skip(!mihomo.installed, 'Mihomo не установлен');
  const revision = await draftRevision(page);

  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await page.getByTestId('kernel-switch-mihomo').click();
  await expect(page.getByTestId('cores-panel-mihomo')).toBeVisible();

  const notice = page.getByTestId('mihomo-profile-notice');
  await expect(notice).toBeVisible({ timeout: T.page });
  await expect(notice).toHaveAttribute('data-owner', mihomo.owner);

  if (mihomo.owner === 'panel') {
    // профиль у панели: кнопка возврата есть, не нажимается
    await expect(page.getByTestId('mihomo-profile-release')).toBeVisible();
  } else if (mihomo.owner === 'pending') {
    // взятие ждёт «Применить»: бейдж без действий
    await expect(page.getByTestId('mihomo-profile-pending')).toBeVisible();
    await expect(notice.getByRole('button')).toHaveCount(0);
  } else {
    // xkeen, user или нет файла: «Управлять в панели» открывает диалог взятия маршрутизации
    await page.getByTestId('mihomo-profile-take').click();
    const modal = page.getByTestId('take-routing-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('take-routing-sections')).toBeVisible({ timeout: T.page });
    await page.getByTestId('take-routing-cancel').click();
    await expect(modal).toBeHidden({ timeout: T.action });
    expect(await draftRevision(page), 'черновик не изменился').toBe(revision);
  }

  await guard.assertClean();
});
