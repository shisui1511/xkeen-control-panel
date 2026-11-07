// e2e-pages: config/cores
// router-area: cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, resetDraft } from './lib/nodes-api';

// Общие блоки форм секций ядер на настоящей панели (фаза 147, план 19): «Прочие ключи»
// обоих ядер правятся в «Дополнительно» с проверкой на сервере до записи в черновик, у
// чужой секции блок только для чтения, раскрытая карточка проходит axe и умещается в 390 px.
// Ответы сети не подменяются. Флаг слоя включается на время прогона и возвращается прежним.
// Ядра не запускаются и не меняются; изменения идут только в черновик и сбрасываются.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

type Kernel = 'xray' | 'mihomo';
const KERNELS: readonly Kernel[] = ['xray', 'mihomo'];

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

interface OtherView {
  installed: boolean;
  owner: string;
  blocked: string;
  value: unknown;
  advanced: string;
}

/** Секция «Прочие ключи» ядра по виду настоящего API. */
async function otherOf(page: Page, kernel: Kernel): Promise<OtherView> {
  const view = await api(page, 'GET', `/api/configlayer/cores?kernel=${kernel}`);
  expect(view.status, `GET /api/configlayer/cores?kernel=${kernel}`).toBe(200);
  const sections = (view.data?.sections ?? []) as {
    id: string;
    owner: string;
    blocked?: string;
    value?: unknown;
    advanced?: string;
  }[];
  const other = sections.find((s) => s.id === 'other');
  return {
    installed: view.data?.installed === true,
    owner: other?.owner ?? '',
    blocked: other?.blocked ?? '',
    value: other?.value,
    advanced: other?.advanced ?? ''
  };
}

/** Ревизия черновика из снимка слоя. */
async function draftRevision(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_revision ?? 0);
}

/** Открывает вкладку ядер, выбирает ядро и раскрывает карточку «Прочие ключи». */
async function openOther(page: Page, kernel: Kernel): Promise<void> {
  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await page.getByTestId(`kernel-switch-${kernel}`).click();
  await expect(page.getByTestId(`cores-panel-${kernel}`)).toBeVisible({ timeout: T.page });
  const toggle = page.getByTestId(`section-toggle-${kernel}-other`);
  await expect(toggle).toBeVisible({ timeout: T.page });
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

test('cores: «Прочие ключи» Xray — правка в «Дополнительно»', async ({ page }) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await otherOf(page, 'xray');
  test.skip(!before.installed, 'Xray не установлен');
  test.skip(before.blocked !== '', 'секция «Прочие ключи» Xray не берётся');
  await resetDraft(page);

  try {
    await openOther(page, 'xray');
    const card = page.getByTestId('section-card-xray-other');

    // секцию ведёт не панель: берём её в черновик через диалог
    if ((await card.getAttribute('data-owner')) !== 'panel') {
      await page.getByTestId('section-take-xray-other').click();
      const modal = page.getByTestId('take-section-modal');
      await expect(modal).toBeVisible({ timeout: T.action });
      await expect(modal.getByTestId('take-section-loading')).toHaveCount(0, {
        timeout: T.action
      });
      const ack = modal.getByTestId('take-section-comments-ack');
      if (await ack.isVisible()) await ack.check();
      await modal.getByTestId('take-section-confirm').click();
      await expect(modal).toHaveCount(0, { timeout: T.action });
      await expect(card).toHaveAttribute('data-owner', /^(pending|panel)$/, { timeout: T.action });
    }

    const area = page.getByTestId('section-advanced-xray-other');
    await expect(area).toBeVisible({ timeout: T.action });
    await expect(area).not.toHaveAttribute('readonly', /.*/);

    // верный объект уходит в черновик по уходу фокуса, ключи не теряются
    const revision = await draftRevision(page);
    await area.fill('{\n  "stats": {},\n  "metrics": { "tag": "metrics-test" }\n}');
    await area.blur();
    await expect(page.getByTestId('section-raw-error-xray-other')).toHaveCount(0);
    await expect.poll(() => draftRevision(page), { timeout: T.action }).toBeGreaterThan(revision);
    await expect
      .poll(
        async () => {
          const view = await otherOf(page, 'xray');
          const value = view.value as { metrics?: { tag?: string }; stats?: unknown } | null;
          return value?.metrics?.tag === 'metrics-test' && value?.stats !== undefined;
        },
        { timeout: T.action }
      )
      .toBe(true);

    // перечитывание вида с тем же текстом секции посреди ввода не подменяет набранное:
    // запись прежнего текста поднимает ревизию черновика, панель перечитывает оба вида
    const settled = (await otherOf(page, 'xray')).advanced;
    const typing = '{ "stats": {}, "typed": 1 }';
    await area.fill(typing);
    const same = await api(page, 'POST', '/api/configlayer/cores/save', {
      revision: await draftRevision(page),
      kernel: 'xray',
      section: 'other',
      form: {},
      advanced: settled
    });
    expect(same.status, 'повторная запись прежнего текста секции').toBe(200);
    // перечитывание идёт не раньше чем через 300 мс после события черновика; не пришло
    // (запись ничего не изменила) — проверка просто не имеет предмета
    await page
      .waitForResponse(
        (r) => r.url().includes('/api/configlayer/cores?kernel=xray') && r.status() === 200,
        { timeout: T.action }
      )
      .catch(() => null);
    await page.waitForTimeout(500);
    await expect(area).toHaveValue(typing);

    // панельный ключ отклоняется: ошибка у блока, черновик не меняется
    const accepted = await draftRevision(page);
    await area.fill('{ "routing": {} }');
    await area.blur();
    const error = page.getByTestId('section-raw-error-xray-other');
    await expect(error).toBeVisible({ timeout: T.action });
    await expect(error).toContainText('routing');
    await expect(error).toHaveAttribute('role', 'alert');
    await expect(area).toHaveAttribute('aria-invalid', 'true');
    expect(await draftRevision(page), 'отклонённый текст не меняет черновик').toBe(accepted);

    // синтаксическая ошибка: номер строки из разборщика
    await area.fill('{\n  "stats": {\n');
    await area.blur();
    await expect(error).toBeVisible({ timeout: T.action });
    await expect(error).toContainText(/(строке|line) \d+/);
    expect(await draftRevision(page), 'неверный синтаксис не меняет черновик').toBe(accepted);

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

test('cores: чужие «Прочие ключи» — только для чтения', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  let checked = 0;
  for (const kernel of KERNELS) {
    const other = await otherOf(page, kernel);
    if (!other.installed) continue;
    if (other.owner === '' || other.owner === 'panel' || other.owner === 'pending') continue;
    checked++;

    await openOther(page, kernel);
    const area = page.getByTestId(`section-advanced-${kernel}-other`);
    await expect(area).toBeVisible({ timeout: T.action });
    await expect(area).toHaveAttribute('readonly', /.*/);
    await expect(area).toHaveAttribute('aria-readonly', 'true');

    // текст показывает действующие ключи секции
    const keys =
      other.value !== null && typeof other.value === 'object' && !Array.isArray(other.value)
        ? Object.keys(other.value as Record<string, unknown>).slice(0, 5)
        : [];
    for (const key of keys) await expect(area).toHaveValue(new RegExp(`\\b${key}\\b`));

    // ввод с клавиатуры ничего не меняет и ничего не пишет в черновик
    const revision = await draftRevision(page);
    const text = await area.inputValue();
    await area.click();
    await page.keyboard.type('zzz');
    await page.keyboard.press('Tab');
    await expect(area).toHaveValue(text);
    await expect(page.getByTestId(`section-raw-error-${kernel}-other`)).toHaveCount(0);
    expect(await draftRevision(page), 'чужая секция не пишет в черновик').toBe(revision);
  }
  test.skip(checked === 0, 'на стенде нет чужой секции «Прочие ключи»');

  await guard.assertClean();
});

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри заданных областей. Вывод — правило, селекторы и числа контраста.
 */
async function axeSevere(page: Page, scopes: string[]): Promise<string[]> {
  // раскрытие карточки идёт с затуханием: axe не должен видеть полупрозрачный кадр
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
  test(`sections axe: «Дополнительно», тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    test.skip(!(await otherOf(page, 'xray')).installed, 'Xray не установлен');

    await openOther(page, 'xray');
    await expect(page.getByTestId('section-advanced-xray-other')).toBeVisible({
      timeout: T.action
    });

    const problems: string[] = [];
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
    for (const v of await axeSevere(page, ['[data-testid="section-card-xray-other"]'])) {
      problems.push(`axe ${v}`);
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

for (const theme of ['dark', 'light'] as const) {
  test(`cores: 390 px «Дополнительно», тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 390, height: 844 });
    await startPage(page);
    test.skip(!(await otherOf(page, 'xray')).installed, 'Xray не установлен');

    await openOther(page, 'xray');
    const area = page.getByTestId('section-advanced-xray-other');
    await expect(area).toBeVisible({ timeout: T.action });

    const pageScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

    const head = await page.getByTestId('section-toggle-xray-other').boundingBox();
    expect(head, 'заголовок карточки виден').not.toBeNull();
    expect(head!.height, 'высота заголовка карточки').toBeGreaterThanOrEqual(44);

    const box = await area.boundingBox();
    expect(box, 'поле «Дополнительно» видно').not.toBeNull();
    expect(box!.x + box!.width, 'поле в пределах экрана').toBeLessThanOrEqual(390);

    await guard.assertClean();
  });
}
