// e2e-pages: config/cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag } from './lib/nodes-api';

// Вкладка «Xray │ Mihomo» раздела «Конфигурация» на настоящей панели (фаза 147, план 09):
// маршрут #/config/cores, переключатель ядра, строка состояния, пустое состояние
// неустановленного ядра, вкладка на 390 px и проход axe в обеих темах. Ответы сети не
// подменяются. Флаг слоя включается на время прогона и возвращается прежним. Ядро не
// запускается, не останавливается и не меняется: вкладка только читает.

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

type Kernel = 'xray' | 'mihomo';
const KERNELS: readonly Kernel[] = ['xray', 'mihomo'];

/** Установлено ли ядро — по виду секций настоящего API. */
async function installedOf(page: Page, kernel: Kernel): Promise<boolean> {
  const view = await api(page, 'GET', `/api/configlayer/cores?kernel=${kernel}`);
  expect(view.status, `GET /api/configlayer/cores?kernel=${kernel}`).toBe(200);
  return view.data?.installed === true;
}

test('cores: вкладка и переключатель ядра', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const installed: Record<Kernel, boolean> = {
    xray: await installedOf(page, 'xray'),
    mihomo: await installedOf(page, 'mihomo')
  };

  await openConfig(page, '#/config');
  await page.getByTestId('config-tab-cores').click();
  await expect(page).toHaveURL(/#\/config\/cores$/);
  await expect(page.getByTestId('config-tab-cores')).toHaveAttribute('aria-selected', 'true');
  await tabReady(page, 'cores-tab');

  // вкладка стоит между «Списками» и «Файлами панели»
  const order = await page
    .locator('[data-testid="config-tabs"] [role="tab"]')
    .evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')));
  expect(order.indexOf('config-tab-cores')).toBe(order.indexOf('config-tab-lists') + 1);
  expect(order.indexOf('config-tab-files')).toBe(order.indexOf('config-tab-cores') + 1);

  // переключатель: два сегмента, ровно один выбран, строка состояния не пуста
  const sw = page.getByTestId('kernel-switch');
  await expect(sw).toBeVisible();
  await expect(sw.getByRole('group', { name: /Ядро|Kernel/ })).toBeVisible();
  await expect(page.getByTestId('kernel-switch-xray')).toBeVisible();
  await expect(page.getByTestId('kernel-switch-mihomo')).toBeVisible();
  await expect(sw.locator('[aria-pressed="true"]')).toHaveCount(1);
  const state = page.getByTestId('kernel-state');
  await expect(state).toHaveText(
    /Запущено|Остановлено|Не установлено|Running|Stopped|Not installed/
  );

  // оба представления в DOM; метка на окне показывает, что страница не перезагружалась
  await page.evaluate(() => {
    (window as Window & { __kept?: boolean }).__kept = true;
  });
  const first = await page
    .getByTestId('kernel-switch-xray')
    .evaluate((el) => el.getAttribute('aria-pressed') === 'true')
    .then((xray): Kernel => (xray ? 'xray' : 'mihomo'));
  const second: Kernel = first === 'xray' ? 'mihomo' : 'xray';
  await expect(page.getByTestId(`cores-panel-${first}`)).toBeVisible();
  await expect(page.getByTestId(`cores-panel-${second}`)).toBeHidden();
  await expect(page.getByTestId(`cores-panel-${second}`)).toHaveCount(1);

  await page.getByTestId(`kernel-switch-${second}`).click();
  await expect(page.getByTestId(`kernel-switch-${second}`)).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByTestId(`cores-panel-${second}`)).toBeVisible();
  await expect(page.getByTestId(`cores-panel-${first}`)).toBeHidden();
  expect(
    await page.evaluate(() => (window as Window & { __kept?: boolean }).__kept === true),
    'страница не перезагружалась'
  ).toBe(true);

  // пустое состояние неустановленного ядра и набор секций установленного
  for (const kernel of KERNELS) {
    await page.getByTestId(`kernel-switch-${kernel}`).click();
    const panel = page.getByTestId(`cores-panel-${kernel}`);
    await expect(panel).toBeVisible();
    if (installed[kernel]) {
      await expect(page.getByTestId(`cores-sections-${kernel}`)).toBeVisible();
      await expect(panel.locator('.empty-state')).toHaveCount(0);
    } else {
      await expect(panel.locator('.empty-state')).toBeVisible();
      await expect(panel.getByRole('button')).toHaveCount(0);
      await expect(state).toHaveText(/Не установлено|Not installed/);
    }
  }

  await guard.assertClean();
});

for (const theme of ['dark', 'light'] as const) {
  test(`cores: 390 px, тема ${theme} — без горизонтальной прокрутки, цели нажатия не меньше 44 px`, async ({
    page
  }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 390, height: 844 });
    await startPage(page);

    await openConfig(page, '#/config/cores');
    await tabReady(page, 'cores-tab');
    expect(
      await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
      'применена тема'
    ).toBe(theme);

    const pageScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

    for (const kernel of KERNELS) {
      const box = await page.getByTestId(`kernel-switch-${kernel}`).boundingBox();
      expect(box, `сегмент ${kernel} виден`).not.toBeNull();
      expect(box!.height, `высота сегмента ${kernel}`).toBeGreaterThanOrEqual(44);
      expect(box!.x + box!.width, `сегмент ${kernel} в пределах экрана`).toBeLessThanOrEqual(390);
    }

    await guard.assertClean();
  });
}

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри заданных областей. Вывод — правило, селекторы и числа контраста.
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
  test(`sections axe: вкладка, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);

    const problems: string[] = [];
    await openConfig(page, '#/config/cores');
    await tabReady(page, 'cores-tab');
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);

    for (const kernel of KERNELS) {
      for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
        await page.getByTestId(`kernel-switch-${kernel}`).click();
        // области должны быть видимы: include без совпадений axe считает ошибкой
        await expect(page.locator('[role="tablist"]').first()).toBeVisible({ timeout: T.action });
        await expect(page.getByTestId('cores-tab')).toBeVisible({ timeout: T.action });
        await expect(page.getByTestId(`cores-panel-${kernel}`)).toBeVisible({ timeout: T.action });
        for (const v of await axeSevere(page, ['[role="tablist"]', '[data-testid="cores-tab"]'])) {
          problems.push(`${kernel} ${width}px: axe ${v}`);
        }
      }
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}
