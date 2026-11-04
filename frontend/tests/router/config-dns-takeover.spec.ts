// e2e-pages: config/cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, resetDraft, withPanel } from './lib/nodes-api';

// Карточка «Перехват DNS роутера» на настоящей панели (фаза 148, DNSF-02, D-02): одна копия
// перед карточкой DNS выбранного ядра, состояние и владелец порта 53 из API службы,
// блокировка «открыт черновик» с пояснением у кнопки, вёрстка на 390 px и доступность в
// обеих темах. Ответы сети не подменяются. Перехват в этом спеке не включается (148-13,
// 148-14); правки идут только в черновик и сбрасываются после каждого теста. Флаг слоя
// включается на время прогона и возвращается прежним.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

test.afterEach(async ({ browser }) => {
  await withPanel(browser, resetDraft);
});

const KERNELS = ['xray', 'mihomo'] as const;
type Kernel = (typeof KERNELS)[number];

// Строка состояния «выключен» в обоих языках панели
const STATUS_FIRMWARE = /DNS роутера отвечает прошивка\.|Router DNS is served by the firmware\./;

interface DnsState {
  state: string;
  owner: { kind: string };
  blocks: string[];
}

/** Состояние перехвата по настоящему API службы. */
async function dnsState(page: Page): Promise<DnsState> {
  const res = await api(page, 'GET', '/api/configlayer/dns');
  expect(res.status, 'GET /api/configlayer/dns').toBe(200);
  const d = res.data ?? {};
  return {
    state: String(d.state ?? ''),
    owner: (d.owner ?? { kind: '' }) as { kind: string },
    blocks: (d.blocks ?? []) as string[]
  };
}

/** Установленные ядра по виду секций. */
async function installed(page: Page): Promise<Record<Kernel, boolean>> {
  const out = { xray: false, mihomo: false };
  for (const k of KERNELS) {
    const view = await api(page, 'GET', `/api/configlayer/cores?kernel=${k}`);
    expect(view.status, `GET /api/configlayer/cores?kernel=${k}`).toBe(200);
    out[k] = view.data?.installed === true;
  }
  return out;
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Открывает вкладку «Xray │ Mihomo» и выбирает ядро; карточка перехвата загрузилась. */
async function openCores(page: Page, kernel: Kernel): Promise<void> {
  await page.goto('/#/config/cores', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('cores-tab')).toBeVisible({ timeout: T.page });
  await page.getByTestId(`kernel-switch-${kernel}`).click();
  await expect(page.getByTestId(`cores-panel-${kernel}`)).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('dns-state')).toBeVisible({ timeout: T.page });
}

/** testId элемента, который идёт в DOM сразу за карточкой перехвата. */
async function nextAfterCard(page: Page): Promise<string> {
  return page
    .getByTestId('dns-takeover-card')
    .evaluate((el) => el.nextElementSibling?.getAttribute('data-testid') ?? '');
}

/** Ревизия черновика из снимка слоя. */
async function draftRevision(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_revision ?? 0);
}

test('dns-card: одна карточка перед секцией DNS выбранного ядра', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const inst = await installed(page);
  const kernels = KERNELS.filter((k) => inst[k]);
  test.skip(kernels.length === 0, 'ядра не установлены');

  for (const k of kernels) {
    await openCores(page, k);
    const card = page.getByTestId('dns-takeover-card');
    await expect(card, `${k}: одна копия карточки в DOM`).toHaveCount(1);
    await expect(page.getByTestId(`cores-panel-${k}`).getByTestId('dns-takeover-card')).toHaveCount(
      1
    );
    expect(await nextAfterCard(page), `${k}: карточка стоит перед секцией DNS`).toBe(
      `section-card-${k}-dns`
    );
    await expect(card.getByRole('heading', { level: 3 })).toBeVisible();
  }

  // Переключение ядра туда и обратно: карточка остаётся одна и снова на месте
  if (kernels.length === 2) {
    await page.getByTestId('kernel-switch-xray').click();
    await expect(page.getByTestId('cores-panel-xray')).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('dns-takeover-card')).toHaveCount(1);
    expect(await nextAfterCard(page)).toBe('section-card-xray-dns');
    await expect(page.getByTestId('dns-state')).toBeVisible({ timeout: T.action });
  }

  await guard.assertClean();
});

test('dns-card: выключенный перехват — DNS у прошивки', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const inst = await installed(page);
  const kernel = KERNELS.find((k) => inst[k]);
  test.skip(kernel === undefined, 'ядра не установлены');
  const st = await dnsState(page);
  test.skip(st.state !== 'off', `перехват в состоянии ${st.state}`);

  await openCores(page, kernel as Kernel);
  const state = page.getByTestId('dns-state');
  await expect(state).toHaveAttribute('data-state', 'off');
  await expect(state).toHaveAttribute('role', 'status');
  await expect(state).toHaveText(STATUS_FIRMWARE);
  await expect(page.getByTestId('dns-enable')).toBeVisible();
  await expect(page.getByTestId('dns-disable')).toHaveCount(0);
  // в карточке не больше одной основной кнопки
  await expect(page.getByTestId('dns-takeover-card').locator('.btn-primary')).toHaveCount(1);
  if (st.owner.kind === 'firmware') {
    await expect(page.getByTestId('dns-row-owner')).toContainText('dns-proxy');
  }
  await expect(page.getByTestId('dns-row-last-check')).toBeVisible();

  await guard.assertClean();
});

test('dns-card: открытый черновик блокирует включение', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const inst = await installed(page);
  const kernel = KERNELS.find((k) => inst[k]);
  test.skip(kernel === undefined, 'ядра не установлены');
  const st = await dnsState(page);
  test.skip(st.state !== 'off', `перехват в состоянии ${st.state}`);

  await openCores(page, kernel as Kernel);
  await expect(page.getByTestId('dns-block-draft')).toHaveCount(0);

  // Правка параметров перехвата открывает черновик (секция dns_takeover)
  const edit = await api(page, 'POST', '/api/configlayer/draft', {
    revision: await draftRevision(page),
    section: 'dns_takeover',
    value: { group: '', zones: ['xcp-e2e-dns-card.lan'] }
  });
  expect(edit.status, 'правка черновика').toBe(200);

  const block = page.getByTestId('dns-block-draft');
  await expect(block).toBeVisible({ timeout: T.action });
  const enable = page.getByTestId('dns-enable');
  await expect(enable).toBeDisabled();
  const blockId = await block.getAttribute('id');
  expect(blockId, 'у строки блокировки есть id').toBeTruthy();
  const describedBy = (await enable.getAttribute('aria-describedby')) ?? '';
  expect(describedBy.split(' '), 'кнопка связана со строкой блокировки').toContain(blockId);

  // Сброс черновика снимает блокировку
  await resetDraft(page);
  await expect(page.getByTestId('dns-block-draft')).toHaveCount(0, { timeout: T.action });

  await guard.assertClean();
});

test('dns-card: 390 px без горизонтальной прокрутки, цели не меньше 44 px', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await startPage(page);
  const inst = await installed(page);
  const kernel = KERNELS.find((k) => inst[k]);
  test.skip(kernel === undefined, 'ядра не установлены');

  await openCores(page, kernel as Kernel);
  const card = page.getByTestId('dns-takeover-card');
  await card.scrollIntoViewIfNeeded();

  const pageScroll = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

  const cardBox = await card.boundingBox();
  expect(cardBox, 'карточка видна').toBeTruthy();
  expect(
    (cardBox?.x ?? 0) + (cardBox?.width ?? 0),
    'карточка в пределах экрана'
  ).toBeLessThanOrEqual(390);

  const boxes = await card.locator(':is(button, a)').evaluateAll((els) =>
    els
      .filter((e) => (e as HTMLElement).offsetParent !== null)
      .map((e) => {
        const r = e.getBoundingClientRect();
        return {
          id: e.getAttribute('data-testid') ?? e.textContent?.trim() ?? e.tagName,
          right: r.right,
          height: r.height
        };
      })
  );
  for (const b of boxes) {
    expect(b.right, `${b.id} в пределах экрана`).toBeLessThanOrEqual(390);
    expect(b.height, `${b.id}: высота цели нажатия`).toBeGreaterThanOrEqual(44);
  }

  // Строки сетки на узком экране — в одну колонку: значение под подписью
  const row = page.getByTestId('dns-row-owner');
  const [dt, dd] = await Promise.all([
    row.locator('dt').boundingBox(),
    row.locator('dd').boundingBox()
  ]);
  expect(dt && dd && dd.y >= dt.y + dt.height - 1, 'значение под подписью').toBeTruthy();

  await guard.assertClean();
});

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри карточки. Вывод — правило, селекторы и числа контраста.
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
  test(`dns axe: карточка перехвата, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    const inst = await installed(page);
    const kernel = KERNELS.find((k) => inst[k]);
    test.skip(kernel === undefined, 'ядра не установлены');

    await openCores(page, kernel as Kernel);
    // Черновик открыт: блокировки (--warning) попадают в проверку контраста
    const st = await dnsState(page);
    if (st.state === 'off') {
      const edit = await api(page, 'POST', '/api/configlayer/draft', {
        revision: await draftRevision(page),
        section: 'dns_takeover',
        value: { group: '', zones: ['xcp-e2e-dns-axe.lan'] }
      });
      expect(edit.status, 'правка черновика').toBe(200);
      await expect(page.getByTestId('dns-block-draft')).toBeVisible({ timeout: T.action });
    }

    const problems: string[] = [];
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
    for (const v of await axeSevere(page, '[data-testid="dns-takeover-card"]')) {
      problems.push(`axe ${v}`);
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}
