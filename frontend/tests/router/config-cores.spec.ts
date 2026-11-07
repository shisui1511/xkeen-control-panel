// e2e-pages: config/cores
// router-area: cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { settleMotion } from './lib/motion';
import { api, layerFlag, resetDraft } from './lib/nodes-api';
import { SLOW_ANNOTATION } from './lib/known';

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
  await expect(sw.getByRole('group', { name: /Ядро|Core/ })).toBeVisible();
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
      // пустое состояние «не установлено» — прямой потомок панели; внутри карточек
      // вкладки свои пустые состояния (например, список профилей без файлов)
      await expect(panel.locator(':scope > .empty-state')).toHaveCount(0);
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

// --- Взятие секции в панель (фаза 147, план 15) ---------------------------------------------

const XRAY_DIR = '/opt/etc/xray/configs';
const DNS_FILE = `${XRAY_DIR}/02_dns.json`;
// первая строка — комментарий JSONC: панель его не хранит, нужно согласие на потерю
const DNS_CONTENT = '// rt-cores-comment\n{\n  "dns": {\n    "servers": ["127.0.0.1"]\n  }\n}\n';

/** Запрос к API панели из страницы с её сессией; тело уходит строкой, как у Редактора. */
async function rawRequest(
  page: Page,
  method: string,
  path: string,
  body?: string
): Promise<{ status: number; text: string }> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(path, {
        method,
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': localStorage.getItem('csrf_token') ?? ''
        },
        body
      });
      return { status: res.status, text: await res.text() };
    },
    { method, path, body }
  );
}

/** Содержимое файла конфига; null, если файла нет. */
async function readConfig(page: Page, path: string): Promise<string | null> {
  const res = await rawRequest(page, 'GET', `/api/config/read?path=${encodeURIComponent(path)}`);
  if (res.status === 404) return null;
  expect(res.status, `чтение ${path}`).toBe(200);
  return res.text;
}

/** Записывает файл конфига; нет файла — сначала создаёт. */
async function writeConfig(page: Page, path: string, content: string): Promise<void> {
  const q = encodeURIComponent(path);
  if ((await readConfig(page, path)) === null) {
    const created = await rawRequest(page, 'POST', `/api/config/create?path=${q}`);
    expect(created.status, `создание ${path}: ${created.text.slice(0, 200)}`).toBe(200);
  }
  const saved = await rawRequest(page, 'POST', `/api/config/save?path=${q}`, content);
  expect(saved.status, `запись ${path}: ${saved.text.slice(0, 200)}`).toBe(200);
}

/** Убирает файл конфига; нет файла (404) — не ошибка. */
async function removeConfig(page: Page, path: string): Promise<void> {
  const res = await rawRequest(page, 'POST', `/api/config/delete?path=${encodeURIComponent(path)}`);
  expect([200, 404], `удаление ${path}: ${res.text.slice(0, 200)}`).toContain(res.status);
}

interface LayerProbe {
  revision: number;
  running: boolean;
  changes: number;
  dnsTaken: boolean;
}

/** Снимок слоя: ревизия черновика, ход применения и владение секцией DNS Xray. */
async function layerProbe(page: Page): Promise<LayerProbe> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const cores = (st.data?.cores ?? {}) as { xray?: Record<string, { taken?: boolean }> };
  const apply = (st.data?.apply ?? {}) as { running?: boolean };
  return {
    revision: Number(st.data?.draft_revision ?? 0),
    running: apply.running === true,
    changes: Number(st.data?.draft_changes ?? 0),
    dnsTaken: cores.xray?.dns?.taken === true
  };
}

/** Ждёт конца применения: оно не идёт, в черновике нет изменений. */
async function settled(page: Page): Promise<void> {
  await expect
    .poll(
      async () => {
        const s = await layerProbe(page);
        return !s.running && s.changes === 0;
      },
      { timeout: T.save * 4, intervals: [2000] }
    )
    .toBe(true);
}

/** Возвращает секцию DNS Xray прежнему владельцу настоящими маршрутами (уборка). */
async function releaseDnsViaApi(page: Page): Promise<void> {
  const s = await layerProbe(page);
  if (!s.dnsTaken) return;
  const res = await api(page, 'POST', '/api/configlayer/cores/release', {
    revision: s.revision,
    kernel: 'xray',
    section: 'dns'
  });
  expect(res.status, 'возврат секции DNS через API').toBe(200);
  const start = await api(page, 'POST', '/api/configlayer/apply', {});
  expect(start.status, 'применение возврата').toBe(200);
  await settled(page);
}

interface SectionBrief {
  id: string;
  owner: string;
}

async function xraySections(page: Page): Promise<SectionBrief[]> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=xray');
  expect(view.status, 'GET /api/configlayer/cores?kernel=xray').toBe(200);
  return ((view.data?.sections ?? []) as SectionBrief[]).map((s) => ({
    id: s.id,
    owner: s.owner
  }));
}

test(
  'cores: взять секцию DNS Xray с комментариями — подтверждение, «Применить», отпустить',
  {
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'взятие, «Применить» и отпускание секции DNS Xray: серия перезапусков Xray'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 12);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    test.skip(!(await installedOf(page, 'xray')), 'Xray не установлен');
    await resetDraft(page);
    const dns = (await xraySections(page)).find((s) => s.id === 'dns');
    test.skip(dns?.owner === 'panel' || dns?.owner === 'pending', 'секция DNS уже у панели');

    const original = await readConfig(page, DNS_FILE);
    try {
      await writeConfig(page, DNS_FILE, DNS_CONTENT);

      await openConfig(page, '#/config/cores');
      await tabReady(page, 'cores-tab');
      await page.getByTestId('kernel-switch-xray').click();
      await page.getByTestId('section-toggle-xray-dns').click();

      // секцию ведёт XKeen или пользователь: «Управлять в панели»
      const owner = page.getByTestId('section-owner-xray-dns');
      await expect(owner).toHaveAttribute('data-owner', /^(xkeen|user)$/, { timeout: T.action });
      await page.getByTestId('section-take-xray-dns').click();

      // диалог: список файлов, комментарии JSONC, кнопка неактивна без согласия
      const modal = page.getByTestId('take-section-modal');
      await expect(modal).toBeVisible({ timeout: T.action });
      await expect(modal.getByTestId('take-section-files')).toContainText('02_dns.json', {
        timeout: T.action
      });
      await expect(modal.getByTestId('take-section-comments')).toBeVisible();
      const confirm = modal.getByTestId('take-section-confirm');
      await expect(confirm).toBeDisabled();
      const before = await layerProbe(page);
      expect(before.dnsTaken, 'диалог сам секцию не взял').toBe(false);

      // согласие снимает блокировку; диалог до подтверждения в черновик ничего не записал
      await modal.getByTestId('take-section-comments-ack').check();
      await expect(confirm).toBeEnabled();
      expect((await layerProbe(page)).revision, 'диалог ничего не записал (draft_revision)').toBe(
        before.revision
      );
      await confirm.click();
      await expect(modal).toHaveCount(0, { timeout: T.action });
      await expect(owner).toHaveAttribute('data-owner', 'pending', { timeout: T.action });
      expect((await layerProbe(page)).dnsTaken, 'до «Применить» файлы не тронуты').toBe(false);
      expect(await readConfig(page, DNS_FILE), 'файл секции не тронут до «Применить»').toBe(
        DNS_CONTENT
      );

      // «Применить»: секция у панели
      await page.getByTestId('config-apply').click();
      await expect(owner).toHaveAttribute('data-owner', 'panel', { timeout: T.save * 3 });
      expect((await layerProbe(page)).dnsTaken).toBe(true);

      // «Отпустить»: подтверждение, затем «Применить» возвращает исходный файл
      await page.getByTestId('section-release-xray-dns').click();
      const dialog = page.getByRole('dialog');
      await expect(dialog).toBeVisible({ timeout: T.action });
      await dialog.getByRole('button', { name: /^(Отпустить секцию|Release section)$/ }).click();

      // отпускание ждёт «Применить»: секция показана только для чтения со значениями панели,
      // правки закрыты, а «Управлять» отменяет отпускание
      await expect(owner).toHaveAttribute('data-owner', 'releasing', { timeout: T.action });
      await expect(page.getByTestId('section-releasing-xray-dns')).toBeVisible();
      await expect(page.getByTestId('section-advanced-xray-dns')).toHaveAttribute('readonly', /.*/);
      await expect(page.getByTestId('section-preset-xray-dns')).toHaveCount(0);
      await page.getByTestId('section-manage-xray-dns').click();
      await expect(owner).toHaveAttribute('data-owner', 'panel', { timeout: T.action });
      expect((await layerProbe(page)).dnsTaken, 'отмена отпускания файлов не трогает').toBe(true);

      await page.getByTestId('section-release-xray-dns').click();
      await expect(dialog).toBeVisible({ timeout: T.action });
      await dialog.getByRole('button', { name: /^(Отпустить секцию|Release section)$/ }).click();
      await expect(owner).toHaveAttribute('data-owner', 'releasing', { timeout: T.action });
      await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: T.action });
      await page.getByTestId('config-apply').click();
      await expect(owner).toHaveAttribute('data-owner', /^(xkeen|user)$/, { timeout: T.save * 3 });
      expect((await layerProbe(page)).dnsTaken).toBe(false);
      expect(await readConfig(page, DNS_FILE), 'исходный файл вернулся побайтно').toBe(DNS_CONTENT);

      await guard.assertClean();
    } finally {
      await resetDraft(page);
      await releaseDnsViaApi(page);
      if (original === null) await removeConfig(page, DNS_FILE);
      else await writeConfig(page, DNS_FILE, original);
    }
  }
);

test('cores: взять все секции Xray — диалог по секциям', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  test.skip(!(await installedOf(page, 'xray')), 'Xray не установлен');
  await resetDraft(page);
  const sections = (await xraySections(page)).filter(
    (s) => s.owner !== 'panel' && s.owner !== 'pending'
  );
  test.skip(sections.length === 0, 'все секции Xray уже у панели');

  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await page.getByTestId('kernel-switch-xray').click();
  await page.getByTestId('cores-take-all').click();

  const modal = page.getByTestId('take-section-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('take-section-loading')).toHaveCount(0, { timeout: T.action });
  await expect(modal.getByTestId('take-section-consequence')).toBeVisible({ timeout: T.action });

  // диалог показывает то же, что предпросмотр настоящего API: файлы по секциям и блокировки
  const ids = sections.map((s) => s.id).join(',');
  const preview = await api(
    page,
    'GET',
    `/api/configlayer/cores/takeover?kernel=xray&sections=${ids}`
  );
  expect(preview.status, 'GET /api/configlayer/cores/takeover').toBe(200);
  const rows = (preview.data?.sections ?? []) as {
    id: string;
    move: string[];
    blocked?: string;
  }[];
  for (const r of rows) {
    if (r.blocked) {
      await expect(modal.getByTestId(`take-section-blocked-${r.id}`)).toContainText(
        /Не будет взята|Will not be taken/
      );
    } else if (r.move.length > 0) {
      const item = modal.getByTestId('take-section-files').locator(`li[data-section="${r.id}"]`);
      await expect(item).toBeVisible();
      for (const file of r.move) await expect(item).toContainText(file);
    }
  }

  // «Отмена» ничего не записала
  const before = await layerProbe(page);
  await modal.getByTestId('take-section-cancel').click();
  await expect(modal).toHaveCount(0, { timeout: T.action });
  const after = await layerProbe(page);
  expect(after.revision, 'отмена диалога не меняет черновик (draft_revision)').toBe(
    before.revision
  );

  await guard.assertClean();
});

// --- 390 px и доступность карточек и диалога взятия (фаза 147, план 15) ---------------------

/**
 * Открывает диалог «Взять все секции ядра». Ничего не записывает. Возвращает false, если
 * Xray не установлен или все его секции уже у панели (кнопки нет).
 */
async function openTakeDialog(page: Page): Promise<boolean> {
  if (!(await installedOf(page, 'xray'))) return false;
  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await page.getByTestId('kernel-switch-xray').click();
  const all = page.getByTestId('cores-take-all');
  if (!(await all.isVisible())) return false;
  await all.click();
  const modal = page.getByTestId('take-section-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('take-section-loading')).toHaveCount(0, { timeout: T.action });
  await expect(modal.getByTestId('take-section-consequence')).toBeVisible({ timeout: T.action });
  return true;
}

for (const theme of ['dark', 'light'] as const) {
  test(`cores: 390 px карточки и диалог, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 390, height: 844 });
    await startPage(page);
    const kernel: Kernel = (await installedOf(page, 'xray')) ? 'xray' : 'mihomo';
    test.skip(kernel === 'mihomo' && !(await installedOf(page, 'mihomo')), 'нет ядер');

    await openConfig(page, '#/config/cores');
    await tabReady(page, 'cores-tab');
    await page.getByTestId(`kernel-switch-${kernel}`).click();
    const panel = page.getByTestId(`cores-panel-${kernel}`);

    // пять карточек, раскрываем первую
    const toggles = panel.locator('[data-testid^="section-toggle-"]');
    await expect(toggles).toHaveCount(5, { timeout: T.page });
    const first = toggles.first();
    const box = await first.boundingBox();
    expect(box, 'заголовок карточки виден').not.toBeNull();
    expect(box!.height, 'высота заголовка карточки').toBeGreaterThanOrEqual(44);
    await first.click();
    await expect(first).toHaveAttribute('aria-expanded', 'true');

    const pageScroll = () =>
      page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth
      );
    expect(
      await pageScroll(),
      'горизонтальная прокрутка страницы с карточками'
    ).toBeLessThanOrEqual(0);

    // кнопки строки владельца раскрытой карточки не ниже 44 px
    const actions = panel.locator('.so-actions .btn');
    for (let i = 0; i < (await actions.count()); i++) {
      const b = await actions.nth(i).boundingBox();
      expect(b, 'кнопка владельца видна').not.toBeNull();
      expect(b!.height, 'высота кнопки владельца').toBeGreaterThanOrEqual(44);
      expect(b!.x + b!.width, 'кнопка владельца в пределах экрана').toBeLessThanOrEqual(390);
    }

    // диалог взятия: без горизонтальной прокрутки, кнопки не ниже 44 px
    if (await openTakeDialog(page)) {
      const modal = page.getByTestId('take-section-modal');
      expect(
        await pageScroll(),
        'горизонтальная прокрутка страницы с диалогом'
      ).toBeLessThanOrEqual(0);
      const overflow = await modal
        .getByTestId('take-section-content')
        .evaluate((el) => el.scrollWidth - el.clientWidth);
      expect(overflow, 'горизонтальная прокрутка тела диалога').toBeLessThanOrEqual(0);
      await settleMotion(page);
      for (const id of ['take-section-confirm', 'take-section-cancel']) {
        const b = await modal.getByTestId(id).boundingBox();
        expect(b, `${id} виден`).not.toBeNull();
        expect(b!.height, `высота ${id}`).toBeGreaterThanOrEqual(44);
        expect(b!.x + b!.width, `${id} в пределах экрана`).toBeLessThanOrEqual(390);
      }
      await modal.getByTestId('take-section-cancel').click();
      await expect(modal).toHaveCount(0, { timeout: T.action });
    }

    await guard.assertClean();
  });
}

for (const theme of ['dark', 'light'] as const) {
  test(`sections axe: диалог взятия, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);

    const opened = await openTakeDialog(page);
    test.skip(!opened, 'на стенде нечего брать: диалог не открывается');

    const problems: string[] = [];
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
    for (const v of await axeSevere(page, ['[data-testid="take-section-modal"]'])) {
      problems.push(`axe ${v}`);
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await page.getByTestId('take-section-cancel').click();
    await expect(page.getByTestId('take-section-modal')).toHaveCount(0, { timeout: T.action });
    await guard.assertClean();
  });
}
