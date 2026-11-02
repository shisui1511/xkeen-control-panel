// e2e-pages: config/cores, config/rules, services
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { RT, T } from './lib/env';
import { CORE_TAG } from './lib/known';
import {
  addToDraft,
  api,
  layerFlag,
  nodeLink,
  randomPort,
  resetDraft,
  withPanel
} from './lib/nodes-api';

// Карточка «Профили» во вкладке Mihomo (фаза 147.1, план 21): список с владельцем, «Сделать
// активным» панель → свой профиль и обратно, блокировка открытым черновиком, цель возврата,
// имена, удаление, профиль без API Mihomo, доступность и 390 px. Ответы сети не подменяются.
// Сценарии изменяющие: они создают профили rt-ui-* настоящим API, переводят Mihomo между
// панелью и своими профилями и после каждого теста возвращают прежнее владение и убирают
// свои профили. Флаг слоя включается на время прогона и возвращается прежним. Если Mihomo
// был обычным файлом config.yaml (профили не включены), сценарии с переходом пропускаются:
// усыновление этого файла необратимо.

const PREFIX = 'rt-ui-';
const layer = layerFlag();

const LOCALES = ['ru', 'en'].map((lang) =>
  fileURLToPath(new URL(`../../src/locales/${lang}.json`, import.meta.url))
);

/** Тексты ключа на обоих языках интерфейса: язык панели стенда заранее неизвестен. */
function texts(key: string): string[] {
  return LOCALES.map(
    (file) => (JSON.parse(readFileSync(file, 'utf8')) as Record<string, string>)[key]
  );
}

const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Начало текста ключа до первой подстановки: так находится абзац с именем профиля внутри. */
function startOf(key: string, len = 28): RegExp {
  return new RegExp(
    texts(key)
      .map((s) => escapeRe(s.split('{')[0].trim().slice(0, len)))
      .join('|')
  );
}

/** Текст кнопки целиком, на любом языке интерфейса. */
function exactOf(key: string): RegExp {
  return new RegExp(`^(${texts(key).map(escapeRe).join('|')})$`);
}

// --- Состояние стенда ---

interface Stand {
  /** Mihomo ведёт панель (профиль xcp-main). */
  taken: boolean;
  /** Значения секций Mihomo сохранены при прошлом отпускании. */
  saved: boolean;
  changes: number;
  running: boolean;
  revision: number;
  /** Активный профиль при включённых профилях, иначе пусто. */
  active: string;
  /** config.yaml — обычный файл XKeen. */
  plain: boolean;
  returnTarget: string;
}

async function standState(page: Page): Promise<Stand> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const routing = (st.data?.routing ?? {}) as {
    mihomo_saved?: boolean;
    mihomo?: { taken?: boolean } | null;
  };
  const apply = (st.data?.apply ?? {}) as { running?: boolean };
  const list = await api(page, 'GET', '/api/mihomo/profiles');
  expect(list.status, 'GET /api/mihomo/profiles').toBe(200);
  const p = (list.data ?? {}) as {
    managed?: boolean;
    active?: string;
    plain_config?: unknown;
    return_target?: string;
  };
  return {
    taken: routing.mihomo?.taken === true,
    saved: routing.mihomo_saved === true,
    changes: Number(st.data?.draft_changes ?? 0),
    running: apply.running === true,
    revision: Number(st.data?.draft_revision ?? 0),
    active: p.managed === true ? (p.active ?? '') : '',
    plain: p.plain_config != null,
    returnTarget: p.return_target ?? ''
  };
}

/** Ждёт конца применения: оно не идёт, в черновике нет изменений. */
async function settled(page: Page): Promise<void> {
  await expect
    .poll(
      async () => {
        const s = await standState(page);
        return !s.running && s.changes === 0;
      },
      { timeout: T.save * 4, intervals: [2000] }
    )
    .toBe(true);
}

/** Результат перехода Mihomo без ошибки: запрос принят и ядро запустилось (или не требовалось). */
function expectSwitched(
  res: { status: number; data: Record<string, unknown> | null },
  what: string
) {
  expect(res.status, what).toBe(200);
  expect(res.data?.error, `${what}: ошибка в ответе`).toBeUndefined();
}

async function createProfile(
  page: Page,
  name: string,
  how: 'template' | 'empty' = 'template'
): Promise<void> {
  const res = await api(
    page,
    'POST',
    '/api/mihomo/profiles/create',
    how === 'template' ? { name, template: 'rule-based.yaml' } : { name, empty: true }
  );
  expect(res.status, `создание профиля ${name}`).toBe(200);
}

async function activateViaApi(page: Page, name: string): Promise<void> {
  const res = await api(page, 'POST', '/api/mihomo/profiles/activate', { name });
  expectSwitched(res, `активация профиля ${name}`);
}

/** Mihomo у панели: возврат при сохранённых значениях секций, иначе взятие и «Применить». */
async function ensurePanelOwns(page: Page): Promise<void> {
  const s = await standState(page);
  if (s.taken) return;
  await resetDraft(page);
  if (s.saved) {
    expectSwitched(
      await api(page, 'POST', '/api/configlayer/routing/mihomo', { action: 'return' }),
      'возврат Mihomo панели'
    );
  } else {
    const take = await api(page, 'POST', '/api/configlayer/routing/take', {
      revision: (await standState(page)).revision,
      scope: 'mihomo'
    });
    expect(take.status, 'желание «взять Mihomo» через API').toBe(200);
    const start = await api(page, 'POST', '/api/configlayer/apply', {});
    expect(start.status, 'применение взятия').toBe(200);
    await settled(page);
  }
  await expect.poll(async () => (await standState(page)).taken, { timeout: T.save }).toBe(true);
}

async function releaseViaApi(page: Page): Promise<void> {
  expectSwitched(
    await api(page, 'POST', '/api/configlayer/routing/mihomo', { action: 'release' }),
    'отпускание Mihomo'
  );
}

async function userProfiles(page: Page): Promise<string[]> {
  const list = await api(page, 'GET', '/api/mihomo/profiles');
  expect(list.status, 'GET /api/mihomo/profiles').toBe(200);
  return ((list.data?.profiles ?? []) as { name: string; owner: string }[])
    .filter((p) => p.owner === 'user')
    .map((p) => p.name);
}

let initial: Stand | null = null;

/**
 * Возвращает прежнее владение Mihomo. Цель возврата не должна остаться на профиле теста:
 * иначе его нельзя удалить. Профиль, на котором стенд стоял до прогона, берётся из цели
 * возврата; не нашёлся — любой профиль пользователя вне rt-ui-*.
 */
async function restoreStand(page: Page, before: Stand): Promise<void> {
  await resetDraft(page);
  let cur = await standState(page);
  if (cur.taken && cur.returnTarget.startsWith(PREFIX)) {
    await releaseViaApi(page);
    cur = await standState(page);
  }
  const names = await userProfiles(page);
  if (before.taken) {
    if (!cur.taken) {
      const want = before.returnTarget;
      const park = names.includes(want) ? want : (names.find((n) => !n.startsWith(PREFIX)) ?? '');
      if (park !== '' && cur.active !== park) await activateViaApi(page, park);
      await ensurePanelOwns(page);
    }
    return;
  }
  if (cur.taken) {
    await releaseViaApi(page);
    cur = await standState(page);
  }
  if (before.active !== '' && !before.plain && cur.active !== before.active) {
    await activateViaApi(page, before.active);
  }
}

/** Убирает профили теста. */
async function removeTestProfiles(page: Page): Promise<void> {
  for (const name of await userProfiles(page)) {
    if (!name.startsWith(PREFIX)) continue;
    const res = await api(page, 'POST', '/api/mihomo/profiles/delete', { name });
    expect(res.status, `удаление профиля ${name}`).toBe(200);
  }
}

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
  initial = await withPanel(browser, async (page) => {
    const view = await api(page, 'GET', '/api/configlayer/cores?kernel=mihomo');
    if (view.status !== 200 || view.data?.installed !== true) return null;
    return standState(page);
  });
});

test.afterEach(async ({ browser }) => {
  if (initial === null) return;
  const before = initial;
  await withPanel(browser, async (page) => {
    await restoreStand(page, before);
    await removeTestProfiles(page);
  });
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

// --- Страница ---

async function openConfig(page: Page, hash: string): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Вкладка Mihomo открыта, карточка «Профили» загрузила список. */
async function openCores(page: Page): Promise<void> {
  await openConfig(page, '#/config/cores');
  const tab = page.getByTestId('cores-tab');
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
  await page.getByTestId('kernel-switch-mihomo').click();
  await expect(page.getByTestId('cores-panel-mihomo')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('profiles-card')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('profiles-list')).toBeVisible({ timeout: T.page });
}

/** Диалог подтверждения панели (переход, возврат, удаление). */
function confirmDialog(page: Page) {
  return page.getByRole('dialog');
}

test('profiles: активация и возврат', { tag: CORE_TAG }, async ({ page }) => {
  test.setTimeout(T.save * 16);
  test.skip(initial === null, 'Mihomo не установлен');
  test.skip(initial?.plain === true, 'config.yaml — обычный файл: переход его усыновит');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);
  await createProfile(page, `${PREFIX}a`);
  await ensurePanelOwns(page);

  // Mihomo у панели: строка панели активна, у своего профиля есть «Сделать активным»
  await openCores(page);
  await expect(page.getByTestId('profile-panel-row')).toHaveAttribute('data-active', 'true');
  const row = page.getByTestId(`profile-row-${PREFIX}a`);
  await expect(row).toBeVisible({ timeout: T.page });
  await expect(row).toHaveAttribute('data-owner', 'user');
  await expect(row).toHaveAttribute('data-active', 'false');
  await page.getByTestId(`profile-activate-${PREFIX}a`).click();

  // подтверждение перехода панель → свой профиль называет перезапуск и обрыв соединений
  const dialog = confirmDialog(page);
  await expect(dialog).toBeVisible({ timeout: T.action });
  await expect(dialog).toContainText(startOf('cfg.prof.to_user_body'));
  await dialog.getByRole('button', { name: exactOf('cfg.prof.to_user_confirm') }).click();

  await expect(page.locator('.toast').first()).toBeVisible({ timeout: T.save * 2 });
  await expect(row).toHaveAttribute('data-active', 'true', { timeout: T.save * 2 });
  await expect(page.getByTestId('profile-panel-row')).toHaveAttribute('data-active', 'false');
  await expect(row.getByTestId(`profile-active-${PREFIX}a`)).toBeVisible();

  // «Группы» и «Правила» сообщают, что для профиля пользователя они не действуют
  await openConfig(page, '#/config/rules');
  await expect(page.getByTestId('routing-mihomo-own-profile')).toBeVisible({ timeout: T.page });

  // возврат строкой панели: подтверждение возврата и панель снова активна
  await openCores(page);
  await page.getByTestId('profile-panel-take').click();
  await expect(dialog).toBeVisible({ timeout: T.action });
  await expect(dialog).toContainText(startOf('cfg.prof.to_panel_body'));
  await dialog.getByRole('button', { name: exactOf('cfg.prof.to_panel_confirm') }).click();
  await expect(page.getByTestId('profile-panel-row')).toHaveAttribute('data-active', 'true', {
    timeout: T.save * 2
  });
  await expect(page.getByTestId(`profile-row-${PREFIX}a`)).toHaveAttribute('data-active', 'false');

  await guard.assertClean();
});

test('profiles: черновик блокирует переход', async ({ page }) => {
  test.setTimeout(T.save * 16);
  test.skip(initial === null, 'Mihomo не установлен');
  test.skip(initial?.plain === true, 'config.yaml — обычный файл: переход его усыновит');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);
  await createProfile(page, `${PREFIX}b`);
  await createProfile(page, `${PREFIX}c`);
  await ensurePanelOwns(page);

  // открытый черновик при управлении панели: переход недоступен, причина написана строкой
  await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'RtUiDraft'));
  await openCores(page);
  const hint = page.getByTestId('profiles-draft-hint');
  await expect(hint).toBeVisible({ timeout: T.page });
  const activate = page.getByTestId(`profile-activate-${PREFIX}b`);
  await expect(activate).toBeDisabled();
  await expect(activate).toHaveAttribute('aria-describedby', /.+/);

  // сброс черновика снимает блокировку
  await resetDraft(page);
  await expect(hint).toBeHidden({ timeout: T.page });
  await expect(activate).toBeEnabled();

  // смена своего профиля на другой своего профиля черновика не касается и не блокируется
  await activateViaApi(page, `${PREFIX}b`);
  await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'RtUiDraftTwo'));
  await page.reload();
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  await page.getByTestId('kernel-switch-mihomo').click();
  const other = page.getByTestId(`profile-activate-${PREFIX}c`);
  await expect(other).toBeEnabled({ timeout: T.page });
  await resetDraft(page);

  await guard.assertClean();
});

test('profiles: цель возврата', async ({ page }) => {
  test.setTimeout(T.save * 16);
  test.skip(initial === null, 'Mihomo не установлен');
  test.skip(initial?.plain === true, 'config.yaml — обычный файл: переход его усыновит');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  // профиль, на котором стоял Mihomo перед возвратом панели, становится целью возврата
  await createProfile(page, `${PREFIX}prev`);
  await activateViaApi(page, `${PREFIX}prev`);
  await ensurePanelOwns(page);
  expect((await standState(page)).returnTarget, 'цель возврата').toBe(`${PREFIX}prev`);

  await openCores(page);
  const row = page.getByTestId(`profile-row-${PREFIX}prev`);
  await expect(row).toBeVisible({ timeout: T.page });
  const remove = page.getByTestId(`profile-delete-${PREFIX}prev`);
  await expect(remove).toBeDisabled();
  await expect(remove).toHaveAttribute('aria-describedby', /.+/);
  await expect(page.getByTestId(`profile-prev-target-${PREFIX}prev`)).toBeVisible();

  // переименование цели возврата — отказ под полем; Esc закрывает поле, фокус возвращается
  const rename = page.getByTestId(`profile-rename-${PREFIX}prev`);
  await rename.click();
  const input = page.getByTestId(`profile-rename-input-${PREFIX}prev`);
  await expect(input).toBeFocused();
  await input.fill(`${PREFIX}prev-new`);
  await input.press('Enter');
  const error = page.getByTestId(`profile-rename-error-${PREFIX}prev`);
  await expect(error).toBeVisible({ timeout: T.action });
  await expect(error).toHaveText(startOf('cfg.prof.err_prev_target'));
  await expect(error).toHaveAttribute('role', 'alert');
  await input.press('Escape');
  await expect(input).toBeHidden();
  await expect(rename).toBeFocused();

  await guard.assertClean();
});

test('profiles: имя и удаление', async ({ page }) => {
  test.setTimeout(T.save * 4);
  test.skip(initial === null, 'Mihomo не установлен');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await createProfile(page, `${PREFIX}del`, 'empty');
  await openCores(page);

  // у профиля панели нет «Переименовать» и «Удалить»
  await expect(page.getByTestId('profile-panel-row')).toBeVisible();
  await expect(page.getByTestId('profile-rename-xcp-main')).toHaveCount(0);
  await expect(page.getByTestId('profile-delete-xcp-main')).toHaveCount(0);

  // префикс xcp- отклоняется
  await page.getByTestId(`profile-rename-${PREFIX}del`).click();
  const input = page.getByTestId(`profile-rename-input-${PREFIX}del`);
  await input.fill('xcp-x');
  await input.press('Enter');
  await expect(page.getByTestId(`profile-rename-error-${PREFIX}del`)).toHaveText(
    startOf('cfg.prof.err_reserved')
  );
  await input.press('Escape');

  // удаление: подтверждение danger, строка исчезает
  await page.getByTestId(`profile-delete-${PREFIX}del`).click();
  const dialog = confirmDialog(page);
  await expect(dialog).toBeVisible({ timeout: T.action });
  await dialog.getByRole('button', { name: exactOf('cfg.prof.delete_confirm') }).click();
  await expect(page.getByTestId(`profile-row-${PREFIX}del`)).toHaveCount(0, { timeout: T.page });
  expect(await userProfiles(page), 'профиль удалён с диска').not.toContain(`${PREFIX}del`);

  await guard.assertClean();
});

test('profiles: профиль без API', { tag: CORE_TAG }, async ({ page }) => {
  test.setTimeout(T.save * 16);
  test.skip(initial === null, 'Mihomo не установлен');
  test.skip(RT.core !== 'mihomo', 'пометка «API не настроен» видна, пока работает ядро Mihomo');
  test.skip(initial?.plain === true, 'config.yaml — обычный файл: переход его усыновит');
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  // профиль без контроллера: активен, ядро работает, API недоступен
  await createProfile(page, `${PREFIX}noapi`, 'empty');
  await activateViaApi(page, `${PREFIX}noapi`);

  // сам переход уже сделан API; консоль проверяется на странице «Службы»
  const guard = attachConsoleGuard(page);
  await page.goto('/#/services', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('mihomo-no-api-note')).toBeVisible({ timeout: T.save * 2 });

  await guard.assertClean();
});

// --- Диалоги «Новый профиль» и «Заполнить из профиля» ---

const MIHOMO_DIR = '/opt/etc/mihomo';

/** Текст файла роутера через API панели; null — файла нет. */
async function readText(page: Page, path: string): Promise<string | null> {
  const res = await page.evaluate(async (p) => {
    const r = await fetch(`/api/config/read?path=${encodeURIComponent(p)}`, {
      headers: { 'X-CSRF-Token': localStorage.getItem('csrf_token') ?? '' }
    });
    return { status: r.status, text: await r.text() };
  }, path);
  if (res.status === 404) return null;
  expect(res.status, `чтение ${path}`).toBe(200);
  return res.text;
}

/** «Новый профиль» открыт кнопкой шапки, сводка пришла с сервера. */
async function openNewDialog(page: Page) {
  await page.getByTestId('profiles-new').click();
  const dialog = page.getByTestId('new-profile-modal');
  await expect(dialog).toBeVisible({ timeout: T.action });
  await expect(page.getByTestId('new-profile-summary-loading')).toHaveCount(0, {
    timeout: T.page
  });
  await expect(page.getByTestId('new-profile-summary')).toBeVisible();
  return dialog;
}

test('profiles: новый профиль из шаблона', async ({ page }) => {
  test.setTimeout(T.save * 4);
  test.skip(initial === null, 'Mihomo не установлен');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);
  await openCores(page);
  const before = await standState(page);

  const dialog = await openNewDialog(page);
  // восемь шаблонов, по умолчанию Rule-Based
  await expect(dialog.locator('[data-testid^="new-profile-template-"]')).toHaveCount(8);
  await expect(page.getByTestId('new-profile-template-rule-based')).toBeChecked();
  await page.getByTestId('new-profile-template-zkeen').check();
  await expect(page.getByTestId('new-profile-template-zkeen')).toBeChecked();

  // сводка видна до создания; без имени создание недоступно
  const summary = page.getByTestId('new-profile-summary');
  await expect(summary).toContainText(startOf('cfg.prof.sum_check'));
  await expect(summary).toContainText(startOf('cfg.prof.sum_added'));
  const confirm = page.getByTestId('new-profile-confirm');
  await expect(confirm).toBeDisabled();

  await page.getByTestId('new-profile-name').fill(`${PREFIX}tpl`);
  await expect(confirm).toBeEnabled();
  await confirm.click();

  // диалог закрылся, строка в списке под фокусом, профиль не активировался
  await expect(dialog).toBeHidden({ timeout: T.save });
  const row = page.getByTestId(`profile-row-${PREFIX}tpl`);
  await expect(row).toBeVisible({ timeout: T.page });
  await expect(row).toHaveAttribute('data-active', 'false');
  await expect(row).toBeFocused();
  const toast = page.locator('.toast').filter({ hasText: `${PREFIX}tpl` });
  await expect(toast).toBeVisible({ timeout: T.action });
  await expect(toast.getByRole('button', { name: exactOf('cfg.prof.activate') })).toBeVisible();
  const after = await standState(page);
  expect(after.active, 'профиль сам не активируется').toBe(before.active);
  expect(after.taken, 'владелец Mihomo не менялся').toBe(before.taken);

  // файл профиля: панель дописала контроллер, подписка шаблона не попала в профиль
  const text = await readText(page, `${MIHOMO_DIR}/profiles/${PREFIX}tpl.yaml`);
  expect(text, 'файл профиля создан').not.toBeNull();
  expect(text).toContain('external-controller-unix');
  expect(text).not.toContain('example.com');

  await guard.assertClean();
});

// --- Доступность и 390 px ---

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

/** Тема и акцент задаются до загрузки страницы. */
async function setLook(page: Page, theme: 'dark' | 'light', accent: string): Promise<void> {
  await page.addInitScript(
    ([th, ac]) => {
      localStorage.setItem('theme', th);
      if (ac !== '') localStorage.setItem('accent', ac);
      else localStorage.removeItem('accent');
    },
    [theme, accent]
  );
}

for (const theme of ['dark', 'light'] as const) {
  for (const accent of ['indigo', 'steel', 'graphite']) {
    test(`profiles axe: тема ${theme}, акцент ${accent}`, async ({ page }) => {
      test.setTimeout(T.save * 4);
      test.skip(initial === null, 'Mihomo не установлен');
      const guard = attachConsoleGuard(page);
      await setLook(page, theme, accent);
      await page.setViewportSize({ width: 1280, height: 900 });
      await startPage(page);
      await createProfile(page, `${PREFIX}axe`, 'empty');
      await openCores(page);
      expect(
        await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
        'применена тема'
      ).toBe(theme);
      expect(
        await page.evaluate(() => document.documentElement.getAttribute('data-accent')),
        'применён акцент'
      ).toBe(accent);
      await expect(page.getByTestId(`profile-row-${PREFIX}axe`)).toBeVisible();

      const problems = (await axeSevere(page, ['[data-testid="profiles-card"]'])).map(
        (v) => `axe ${v}`
      );
      expect(problems, problems.join('\n')).toEqual([]);

      await guard.assertClean();
    });
  }
}

test('profiles: 390 px — без горизонтальной прокрутки, кнопки не меньше 44 px', async ({
  page
}) => {
  test.setTimeout(T.save * 4);
  test.skip(initial === null, 'Mihomo не установлен');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await startPage(page);
  // единицы профилей и имя максимальной длины (63 символа): строки растут вертикально
  const longName = `${PREFIX}${'x'.repeat(63 - PREFIX.length)}`;
  for (const name of [`${PREFIX}m1`, `${PREFIX}m2`, `${PREFIX}m3`, `${PREFIX}m4`, longName]) {
    await createProfile(page, name, 'empty');
  }
  await openCores(page);
  await expect(page.getByTestId(`profile-row-${longName}`)).toBeVisible({ timeout: T.page });

  const pageScroll = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

  const buttons = page.getByTestId('profiles-card').getByRole('button');
  const count = await buttons.count();
  expect(count, 'в карточке есть кнопки').toBeGreaterThan(0);
  for (let i = 0; i < count; i++) {
    const box = await buttons.nth(i).boundingBox();
    expect(box, `кнопка ${i} видна`).not.toBeNull();
    expect(box!.height, `высота кнопки ${i}`).toBeGreaterThanOrEqual(44);
    expect(box!.x + box!.width, `кнопка ${i} в пределах экрана`).toBeLessThanOrEqual(390);
  }

  // действия строки идут сеткой в две колонки: «Править» и «Копировать» стоят рядом
  const edit = await page.getByTestId(`profile-edit-${PREFIX}m1`).boundingBox();
  const copy = await page.getByTestId(`profile-copy-${PREFIX}m1`).boundingBox();
  expect(edit, 'кнопка «Править»').not.toBeNull();
  expect(copy, 'кнопка «Копировать»').not.toBeNull();
  expect(Math.abs(edit!.y - copy!.y), 'кнопки в одном ряду').toBeLessThanOrEqual(2);
  expect(copy!.x, 'вторая колонка').toBeGreaterThan(edit!.x);

  await guard.assertClean();
});
