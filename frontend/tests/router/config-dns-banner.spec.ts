// e2e-pages: config/cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Browser, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { CORE_TAG } from './lib/known';
import { api, layerFlag, resetDraft, withPanel } from './lib/nodes-api';

// Общий баннер окна подтверждения перехвата DNS на настоящей панели (фаза 148, DNSF-05,
// D-05…D-08, D-25): перехват включается настоящим API службы, баннер виден на разных
// страницах оболочки, «Оставить» и «Откатить сейчас» решают окно, решение в одной вкладке
// снимает баннер в другой, вёрстка на 390 px под мобильной шапкой и над полосой черновика,
// доступность в обеих темах. Ответы сети не подменяются. После каждого теста окно
// закрывается и перехват выключается; флаг слоя возвращается прежним.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

// Включение — транзакция с перезапуском ядра; окно длится 60 с
const ENABLE_TIMEOUT = T.save;
const TEST_TIMEOUT = T.save * 4;

const TOAST_KEPT = /Перехват DNS подтверждён\.|DNS interception confirmed\./;
const TOAST_ROLLED = /Перехват DNS откачен\.|DNS interception rolled back\./;

interface DnsView {
  state: string;
  blocks: string[];
  owner: { kind: string };
  window?: { kind: string; deadline_ms: number; total_s: number };
}

/** Состояние перехвата по настоящему API службы. */
async function dnsView(page: Page): Promise<DnsView> {
  const res = await api(page, 'GET', '/api/configlayer/dns');
  expect(res.status, 'GET /api/configlayer/dns').toBe(200);
  const d = res.data ?? {};
  return {
    state: String(d.state ?? ''),
    blocks: (d.blocks ?? []) as string[],
    owner: (d.owner ?? { kind: '' }) as { kind: string },
    window: d.window as DnsView['window']
  };
}

/** Ждёт состояние перехвата из списка. */
async function waitState(page: Page, states: string[], timeout = ENABLE_TIMEOUT): Promise<void> {
  await expect
    .poll(async () => (await dnsView(page)).state, { timeout, intervals: [500, 1000, 2000] })
    .toMatch(new RegExp(`^(${states.join('|')})$`));
}

/**
 * Закрывает окно (откат) и выключает перехват: стенд возвращается к DNS прошивки. Ждёт
 * конца транзакций, чтобы следующий тест начинал с состояния off.
 */
async function restoreDns(page: Page): Promise<void> {
  let view = await dnsView(page);
  if (view.state === 'applying') {
    await waitState(page, ['off', 'window', 'on', 'waiting', 'unavailable']);
    view = await dnsView(page);
  }
  if (view.state === 'window') {
    const res = await api(page, 'POST', '/api/configlayer/dns/rollback', {});
    expect([200, 409], 'откат окна при уборке').toContain(res.status);
    await waitState(page, ['off', 'on', 'waiting', 'unavailable']);
    view = await dnsView(page);
  }
  if (view.state === 'on' || view.state === 'waiting') {
    const res = await api(page, 'POST', '/api/configlayer/dns/disable', {});
    expect(res.status, 'выключение перехвата при уборке').toBe(200);
    await waitState(page, ['off', 'unavailable']);
  }
}

async function restoreAll(browser: Browser): Promise<void> {
  await withPanel(browser, async (page) => {
    await restoreDns(page);
    await resetDraft(page);
  });
}

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
  await restoreAll(browser);
});

test.afterEach(async ({ browser }) => {
  await restoreAll(browser);
});

test.afterAll(async ({ browser }) => {
  await restoreAll(browser);
  await layer.restore(browser);
});

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Переход на страницу оболочки по маршруту. */
async function gotoRoute(page: Page, route: string): Promise<void> {
  await page.goto(`/#/${route}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/**
 * Включает перехват через API и ждёт окно подтверждения. Перехват недоступен на стенде
 * (не off или есть блокировки) — тест пропускается с причиной.
 */
async function openWindow(page: Page): Promise<void> {
  const before = await dnsView(page);
  test.skip(before.state !== 'off', `перехват в состоянии ${before.state}`);
  test.skip(before.blocks.length > 0, `включение заблокировано: ${before.blocks.join(', ')}`);
  const res = await api(page, 'POST', '/api/configlayer/dns/enable', {});
  expect(res.status, 'POST /api/configlayer/dns/enable').toBe(200);
  await waitState(page, ['window']);
}

function banner(page: Page): Locator {
  return page.getByTestId('dns-confirm-banner');
}

/** Число секунд отсчёта из баннера. */
async function bannerSeconds(page: Page): Promise<number> {
  return Number(await page.getByTestId('dns-confirm-seconds').getAttribute('data-seconds'));
}

test(
  'dns-banner: окно видно на разных страницах, «Оставить» подтверждает',
  { tag: CORE_TAG },
  async ({ page }) => {
    test.setTimeout(TEST_TIMEOUT);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    await openWindow(page);

    await gotoRoute(page, 'config/cores');
    await expect(banner(page)).toBeVisible({ timeout: T.action });
    await expect(banner(page)).toHaveAttribute('data-kind', 'enable');
    await expect(banner(page)).toHaveAttribute('role', 'alert');
    await expect(page.getByTestId('dns-confirm-seconds')).toHaveAttribute('role', 'timer');
    // В окне единственная основная кнопка на странице — «Оставить» в баннере: карточка
    // своих кнопок не показывает, кнопка предупреждения о старом перенаправлении вторичная
    const card = page.getByTestId('dns-takeover-card');
    await expect(card).toBeVisible({ timeout: T.action });
    await expect(card.locator('.btn-primary')).toHaveCount(0);
    await expect(banner(page).locator('.btn-primary')).toHaveCount(1);

    // Отсчёт идёт по сроку сервера: число уменьшается
    const first = await bannerSeconds(page);
    expect(first, 'остаток окна в секундах').toBeGreaterThan(0);
    expect(first, 'остаток окна не больше 60 с').toBeLessThanOrEqual(60);
    await expect.poll(() => bannerSeconds(page), { timeout: T.action }).toBeLessThan(first);

    // Баннер в оболочке, а не на странице карточки
    for (const route of ['services', 'logs']) {
      await gotoRoute(page, route);
      await expect(banner(page), `баннер на #/${route}`).toBeVisible({ timeout: T.action });
      await expect(banner(page)).toHaveCount(1);
    }

    await gotoRoute(page, 'config/cores');
    await expect(banner(page)).toBeVisible({ timeout: T.action });
    await page.getByTestId('dns-confirm-keep').click();
    await expect(banner(page)).toHaveCount(0, { timeout: T.action });
    await expect(page.locator('.toast--success').filter({ hasText: TOAST_KEPT })).toBeVisible({
      timeout: T.action
    });
    await expect(page.getByTestId('dns-state')).toHaveAttribute('data-state', 'on', {
      timeout: T.action
    });
    expect((await dnsView(page)).state, 'перехват подтверждён').toBe('on');

    await guard.assertClean();
  }
);

test(
  'dns-banner: «Откатить сейчас» возвращает DNS прошивке',
  { tag: CORE_TAG },
  async ({ page }) => {
    test.setTimeout(TEST_TIMEOUT);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    await openWindow(page);

    await gotoRoute(page, 'config/cores');
    await expect(banner(page)).toBeVisible({ timeout: T.action });
    await page.getByTestId('dns-confirm-rollback').click();
    await expect(page.locator('.toast--info').filter({ hasText: TOAST_ROLLED })).toBeVisible({
      timeout: ENABLE_TIMEOUT
    });
    await expect(banner(page)).toHaveCount(0);
    await expect(page.getByTestId('dns-state')).toHaveAttribute('data-state', 'off', {
      timeout: T.action
    });
    await expect
      .poll(async () => (await dnsView(page)).owner.kind, { timeout: T.action })
      .toBe('firmware');

    await guard.assertClean();
  }
);

test(
  'dns-banner: решение в одной вкладке снимает баннер в другой',
  { tag: CORE_TAG },
  async ({ page }) => {
    test.setTimeout(TEST_TIMEOUT);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    await openWindow(page);

    const second = await page.context().newPage();
    const guard2 = attachConsoleGuard(second);
    try {
      await second.setViewportSize({ width: 1440, height: 900 });
      await gotoRoute(page, 'services');
      await gotoRoute(second, 'logs');
      await expect(banner(page), 'баннер в первой вкладке').toBeVisible({ timeout: T.action });
      await expect(banner(second), 'баннер во второй вкладке').toBeVisible({
        timeout: T.action
      });

      // Обе вкладки видят одно окно: срок одинаковый с точностью до секунды отсчёта
      const [a, b] = await Promise.all([bannerSeconds(page), bannerSeconds(second)]);
      expect(Math.abs(a - b), 'одно окно в двух вкладках').toBeLessThanOrEqual(2);

      await second.getByTestId('dns-confirm-keep').click();
      await expect(banner(second)).toHaveCount(0, { timeout: T.action });
      await expect(banner(page), 'событие снимает баннер в первой вкладке').toHaveCount(0, {
        timeout: 3000
      });

      await guard2.assertClean();
    } finally {
      await second.close();
    }
    await guard.assertClean();
  }
);

test(
  'dns-banner: 390 px — под мобильной шапкой, над полосой черновика, цели 44 px',
  { tag: CORE_TAG },
  async ({ page }) => {
    test.setTimeout(TEST_TIMEOUT);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 390, height: 844 });
    await startPage(page);
    await openWindow(page);

    await gotoRoute(page, 'config/cores');
    const b = banner(page);
    await expect(b).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('config-draftbar')).toBeVisible({ timeout: T.page });

    // Прокрутка вниз: оба липких блока приклеиваются
    await page.evaluate(() => {
      window.scrollTo(0, document.documentElement.scrollHeight);
      for (const el of document.querySelectorAll('.main-content')) {
        el.scrollTop = el.scrollHeight;
      }
    });
    await page.waitForTimeout(300);

    const pageScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

    const header = await page.locator('.mobile-header').boundingBox();
    const bannerBox = await b.boundingBox();
    expect(bannerBox, 'баннер виден').toBeTruthy();
    if (header && bannerBox) {
      expect(bannerBox.y, 'баннер под мобильной шапкой').toBeGreaterThanOrEqual(
        header.y + header.height - 1
      );
    }
    expect(
      (bannerBox?.x ?? 0) + (bannerBox?.width ?? 0),
      'баннер в пределах экрана'
    ).toBeLessThanOrEqual(390);

    // Высота баннера опубликована для смещения липких блоков
    const published = await page.evaluate(() =>
      parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--dns-banner-h'))
    );
    expect(published, '--dns-banner-h').toBeGreaterThan(0);

    const draftBox = await page.getByTestId('config-draftbar').boundingBox();
    if (draftBox && bannerBox) {
      expect(draftBox.y, 'полоса черновика не перекрыта баннером').toBeGreaterThanOrEqual(
        bannerBox.y + bannerBox.height - 1
      );
    }

    const boxes = await b.locator('button').evaluateAll((els) =>
      els.map((e) => {
        const r = e.getBoundingClientRect();
        return { id: e.getAttribute('data-testid') ?? e.tagName, right: r.right, height: r.height };
      })
    );
    expect(boxes.length, 'кнопки баннера').toBe(2);
    for (const box of boxes) {
      expect(box.right, `${box.id} в пределах экрана`).toBeLessThanOrEqual(390);
      expect(box.height, `${box.id}: высота цели нажатия`).toBeGreaterThanOrEqual(44);
    }

    // Решение окна снимает смещение
    await page.getByTestId('dns-confirm-keep').click();
    await expect(b).toHaveCount(0, { timeout: T.action });
    await expect
      .poll(() =>
        page.evaluate(() =>
          parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--dns-banner-h'))
        )
      )
      .toBe(0);

    await guard.assertClean();
  }
);

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри баннера. Вывод — правило, селекторы и числа контраста.
 */
async function axeSevere(page: Page, scope: string): Promise<string[]> {
  await page.waitForTimeout(500);
  const scan = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .include(scope)
    .analyze();
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
  test(`dns-banner: dns axe: баннер окна, тема ${theme}`, { tag: CORE_TAG }, async ({ page }) => {
    test.setTimeout(TEST_TIMEOUT);
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    await openWindow(page);

    await gotoRoute(page, 'services');
    await expect(banner(page)).toBeVisible({ timeout: T.action });

    const problems: string[] = [];
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
    for (const v of await axeSevere(page, '[data-testid="dns-confirm-banner"]')) {
      problems.push(`axe ${v}`);
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}
