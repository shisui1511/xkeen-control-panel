// e2e-pages: config/cores
import AxeBuilder from '@axe-core/playwright';
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

test('cores: причина блока api Xray', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);

  const xrayView = await api(page, 'GET', '/api/configlayer/cores?kernel=xray');
  expect(xrayView.status, 'GET /api/configlayer/cores?kernel=xray').toBe(200);
  const snap = await api(page, 'GET', '/api/configlayer/state');
  expect(snap.status, 'GET /api/configlayer/state').toBe(200);
  const features = (snap.data?.features ?? {}) as Record<string, string>;
  const routing = (snap.data?.routing ?? {}) as {
    desired?: boolean;
    xray?: { taken?: boolean } | null;
  };
  const expected =
    xrayView.data?.installed === true &&
    features.xray_hot_swap_api === 'unavailable' &&
    (routing.desired === true || routing.xray?.taken === true)
      ? 1
      : 0;

  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await expect(page.getByTestId('xray-api-unavailable')).toHaveCount(expected, {
    timeout: T.page
  });

  await guard.assertClean();
});

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри заданных областей.
 */
async function axeSevere(page: Page, scopes: string[]): Promise<string[]> {
  // переход между вкладками идёт с затуханием: axe не должен видеть полупрозрачный кадр
  await page.waitForTimeout(500);
  let builder = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);
  for (const scope of scopes) builder = builder.include(scope);
  const scan = await builder.analyze();
  return scan.violations
    .filter((v) => v.impact === 'critical' || v.impact === 'serious')
    .map((v) => {
      const nodes = v.nodes
        .slice(0, 3)
        .map((n) => `${n.target.join(' ')} [${n.any[0]?.message ?? ''}]`);
      return `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${nodes.join(' | ')}`;
    });
}

/** Открывает вид Mihomo с плашкой профиля; без Mihomo тест пропускается. */
async function openMihomoNotice(page: Page, theme: 'dark' | 'light', width: number) {
  await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
  await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
  await startPage(page);
  const mihomo = await mihomoView(page);
  test.skip(!mihomo.installed, 'Mihomo не установлен');

  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  expect(
    await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
    'применена тема'
  ).toBe(theme);
  await page.getByTestId('kernel-switch-mihomo').click();
  const notice = page.getByTestId('mihomo-profile-notice');
  await expect(notice).toBeVisible({ timeout: T.page });
  return notice;
}

for (const theme of ['dark', 'light'] as const) {
  test(`sections axe: плашка профиля, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await openMihomoNotice(page, theme, 1280);

    const problems: string[] = [];
    for (const v of await axeSevere(page, ['[data-testid="mihomo-profile-notice"]'])) {
      problems.push(`axe ${v}`);
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

for (const theme of ['dark', 'light'] as const) {
  test(`cores: 390 px плашка профиля, тема ${theme} — без горизонтальной прокрутки, кнопка не меньше 44 px`, async ({
    page
  }) => {
    const guard = attachConsoleGuard(page);
    const notice = await openMihomoNotice(page, theme, 390);

    const pageScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

    // когда взятие ждёт «Применить», кнопок в плашке нет
    const buttons = notice.getByRole('button');
    const count = await buttons.count();
    for (let i = 0; i < count; i++) {
      const box = await buttons.nth(i).boundingBox();
      expect(box, `кнопка ${i} видна`).not.toBeNull();
      expect(box!.height, `высота кнопки ${i}`).toBeGreaterThanOrEqual(44);
      expect(box!.x + box!.width, `кнопка ${i} в пределах экрана`).toBeLessThanOrEqual(390);
    }

    await guard.assertClean();
  });
}
