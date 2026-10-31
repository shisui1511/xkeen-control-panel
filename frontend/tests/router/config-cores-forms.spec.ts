// e2e-pages: config/cores
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
