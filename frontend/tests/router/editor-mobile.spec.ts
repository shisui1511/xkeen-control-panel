// e2e-pages: editor
import { test, expect, type CDPSession, type Locator, type Page } from '@playwright/test';
import { randomInt } from 'node:crypto';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { withPanel } from './lib/nodes-api';

// Сценарии EDT-01 на настоящей панели: шторка файлов на ≤768 px, действия и лист Backups на
// 390 px в обеих темах, колонка со сплиттером на ПК. Сеть не подменяется. Стенд меняется только
// временным файлом с префиксом FIXTURE_PREFIX и его резервными копиями: файл создаётся через API
// панели до сценариев и удаляется вместе с копиями после (afterAll), остатки прерванных прогонов
// убираются по префиксу перед созданием. Реальные конфиги стенда спек не открывает.

const VP_PHONE = { width: 390, height: 844 };
const VP_TABLET = { width: 768, height: 1024 };
const VP_DESKTOP = { width: 1280, height: 800 };
const THEMES = ['light', 'dark'] as const;
type Theme = (typeof THEMES)[number];
type Viewport = { width: number; height: number };

const XRAY_DIR = '/opt/etc/xray/configs';
const MIHOMO_DIR = '/opt/etc/mihomo';
// без слов стоп-списка XKeen и без префикса xcp- файлов слоя «Конфигурация»
const FIXTURE_PREFIX = 'zz_e2e_editor_';

const SHEET = '.file-tree-pane.overlay';
const BACKDROP = '.responsive-sidebar-backdrop';
const CLOSE_BTN = '.responsive-sidebar-close';
const TOGGLE = '.btn-sidebar-toggle';
const SHOW_FILES = '.editor-empty-actions .btn-secondary';
const MAIN_CARD = '.editor-main-card';
// синтетическая строка без пробелов, шире листа на 390 px; вводится в редактор и на диск не сохраняется
// текст временного файла отличается от обеих копий fixture (`{}` и `{}` с переводом строки),
// иначе подстановку копии не видно по признаку несохранённых правок
const SCRATCH_TEXT = '{}\n\n\n';
const LONG_DIFF_LINE = 'zz_e2e_long_' + '0123456789'.repeat(15);

interface Fixture {
  dir: string;
  name: string;
  path: string;
  /** последняя записанная версия содержимого */
  current: string;
  /** предыдущая версия (её копия лежит в резервных копиях) */
  previous: string;
}

/** Второй тестовый файл: пустой, без копий; нужен сценарию выбора файла при несохранённых правках. */
interface AltFixture {
  name: string;
  path: string;
}

let fixture: Fixture | null = null;
let altFixture: AltFixture | null = null;

/** Каталог и расширение тестового файла: каталог активного ядра, его проверка точно установлена. */
function fixtureDir(): { dir: string; ext: string } | null {
  if (RT.core === 'xray') return { dir: XRAY_DIR, ext: '.json' };
  if (RT.core === 'mihomo') return { dir: MIHOMO_DIR, ext: '.yaml' };
  return null;
}

test.skip(() => fixtureDir() === null, 'активное ядро стенда не определено');

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

/** Массив из ответа панели: сам массив либо поле data конверта. */
function asArray(text: string): unknown[] {
  try {
    const payload: unknown = JSON.parse(text);
    if (Array.isArray(payload)) return payload;
    if (payload && typeof payload === 'object') {
      const data = (payload as { data?: unknown }).data;
      if (Array.isArray(data)) return data;
    }
  } catch {
    // не JSON: пустой список
  }
  return [];
}

function expectOk(res: { status: number; text: string }, step: string): void {
  if (res.status !== 200) {
    throw new Error(`тестовый файл: ${step} HTTP ${res.status} ${res.text.slice(0, 300)}`);
  }
}

/** Тестовый файл с двумя резервными копиями. Содержимое одинаково для Xray и Mihomo. */
async function createFixture(page: Page, dir: string, ext: string): Promise<Fixture> {
  const name = `${FIXTURE_PREFIX}${randomInt(100000, 1000000)}${ext}`;
  const path = `${dir}/${name}`;
  const q = encodeURIComponent(path);
  expectOk(await rawRequest(page, 'POST', `/api/config/create?path=${q}`), 'create');

  // пустой объект с одним и двумя переводами строки: версии различаются строкой
  const previous = '{}\n';
  const current = '{}\n\n';
  expectOk(await rawRequest(page, 'POST', `/api/config/save?path=${q}`, previous), 'save 1');
  // имя копии с точностью до секунды: вторая запись должна попасть в другую секунду
  await page.waitForTimeout(1100);
  expectOk(await rawRequest(page, 'POST', `/api/config/save?path=${q}`, current), 'save 2');

  const backups = await rawRequest(page, 'GET', `/api/config/backups?path=${q}`);
  expectOk(backups, 'backups');
  if (asArray(backups.text).length < 2) {
    throw new Error(`тестовый файл: копий ${asArray(backups.text).length}, ожидалось не меньше 2`);
  }
  return { dir, name, path, current, previous };
}

/** Второй тестовый файл с другим именем, чем `taken`; содержимое задаёт сам create. */
async function createAltFixture(
  page: Page,
  dir: string,
  ext: string,
  taken: string
): Promise<AltFixture> {
  let name = taken;
  while (name === taken) {
    name = `${FIXTURE_PREFIX}${randomInt(100000, 1000000)}${ext}`;
  }
  const path = `${dir}/${name}`;
  expectOk(
    await rawRequest(page, 'POST', `/api/config/create?path=${encodeURIComponent(path)}`),
    'create alt'
  );
  return { name, path };
}

/** Удаляет файл вместе со всеми его резервными копиями; 404 не ошибка. */
async function deleteFileWithBackups(page: Page, path: string): Promise<void> {
  const q = encodeURIComponent(path);
  const backups = await rawRequest(page, 'GET', `/api/config/backups?path=${q}`);
  if (backups.status === 200) {
    for (const b of asArray(backups.text)) {
      if (typeof b !== 'string') continue;
      const res = await rawRequest(
        page,
        'POST',
        `/api/config/delete?path=${encodeURIComponent(b)}`
      );
      if (res.status !== 200 && res.status !== 404) {
        throw new Error(`уборка: удаление копии HTTP ${res.status}`);
      }
    }
  }
  const res = await rawRequest(page, 'POST', `/api/config/delete?path=${q}`);
  if (res.status !== 200 && res.status !== 404) {
    throw new Error(`уборка: удаление файла HTTP ${res.status}`);
  }
}

/** Удаляет тестовые файлы с префиксом вместе со всеми их резервными копиями. */
async function sweepFixtures(page: Page, dir: string): Promise<void> {
  const list = await rawRequest(page, 'GET', `/api/config/list?dir=${encodeURIComponent(dir)}`);
  expectOk(list, 'list');
  const stale = asArray(list.text).filter(
    (f): f is { name: string; path: string } =>
      typeof f === 'object' &&
      f !== null &&
      typeof (f as { name?: unknown }).name === 'string' &&
      typeof (f as { path?: unknown }).path === 'string' &&
      (f as { name: string }).name.startsWith(FIXTURE_PREFIX)
  );
  for (const f of stale) await deleteFileWithBackups(page, f.path);
}

/**
 * Временный файл теста: пустой, без копий. Удаляется самим тестом, остатки прерванных прогонов
 * убирает уборка по префиксу. Имя не из `taken` и не совпадает с общими тестовыми файлами.
 */
async function createScratchFile(page: Page, taken: string[]): Promise<AltFixture> {
  const target = fixtureDir();
  if (!target) throw new Error('активное ядро стенда не определено');
  let name = '';
  while (name === '' || taken.includes(name) || name === fx().name || name === altFx().name) {
    name = `${FIXTURE_PREFIX}${randomInt(100000, 1000000)}${target.ext}`;
  }
  const path = `${target.dir}/${name}`;
  expectOk(
    await rawRequest(page, 'POST', `/api/config/create?path=${encodeURIComponent(path)}`),
    'create scratch'
  );
  return { name, path };
}

/** Содержимое файла на диске, как его видит API. */
async function readOnDisk(page: Page, path: string): Promise<string> {
  const res = await rawRequest(page, 'GET', `/api/config/read?path=${encodeURIComponent(path)}`);
  expectOk(res, 'read');
  return res.text;
}

/** Имена файлов каталога по API панели. */
async function listNames(page: Page, dir: string): Promise<string[]> {
  const res = await rawRequest(page, 'GET', `/api/config/list?dir=${encodeURIComponent(dir)}`);
  expectOk(res, 'list');
  return asArray(res.text)
    .map((f) => (typeof f === 'object' && f !== null ? (f as { name?: unknown }).name : undefined))
    .filter((n): n is string => typeof n === 'string');
}

test.beforeAll(async ({ browser }) => {
  test.setTimeout(180_000 * RT.slow);
  const target = fixtureDir();
  if (!target) return;
  await withPanel(browser, async (page) => {
    await sweepFixtures(page, target.dir);
    fixture = await createFixture(page, target.dir, target.ext);
    altFixture = await createAltFixture(page, target.dir, target.ext, fixture.name);
  });
});

test.afterAll(async ({ browser }) => {
  test.setTimeout(120_000 * RT.slow);
  const target = fixtureDir();
  if (!target) return;
  fixture = null;
  altFixture = null;
  await withPanel(browser, (page) => sweepFixtures(page, target.dir));
});

function fx(): Fixture {
  if (!fixture) throw new Error('тестовый файл не создан');
  return fixture;
}

function altFx(): AltFixture {
  if (!altFixture) throw new Error('второй тестовый файл не создан');
  return altFixture;
}

/** Открывает Редактор в заданном окне и теме и дожидается заголовка страницы. */
async function gotoEditor(page: Page, vp: Viewport, theme: Theme = 'light'): Promise<void> {
  await page.setViewportSize(vp);
  await page.addInitScript((th) => {
    localStorage.setItem('theme', th);
  }, theme);
  await page.goto('/#/editor');
  await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
    timeout: T.page
  });
  expect(await page.evaluate(() => document.documentElement.getAttribute('data-theme'))).toBe(
    theme
  );
}

/** Рамка элемента; без неё падает с понятным сообщением, а не с TypeError на разыменовании. */
async function boxOf(
  loc: Locator,
  what: string
): Promise<{ x: number; y: number; width: number; height: number }> {
  const b = await loc.boundingBox();
  expect(b, `${what}: нет рамки`).not.toBeNull();
  if (!b) throw new Error(`${what}: нет рамки`);
  return b;
}

/** Значение document.body.style.overflow: блокировка прокрутки страницы под шторкой (D-14). */
async function bodyOverflow(page: Page): Promise<string> {
  return page.evaluate(() => document.body.style.overflow);
}

/** Элемент видим целиком внутри окна браузера. */
async function expectInside(page: Page, loc: Locator, what: string): Promise<void> {
  const box = await boxOf(loc, what);
  const vp = page.viewportSize()!;
  expect(box.width, `${what}: нулевая ширина`).toBeGreaterThan(0);
  expect(box.height, `${what}: нулевая высота`).toBeGreaterThan(0);
  expect(box.x, `${what}: левый край ${box.x}`).toBeGreaterThanOrEqual(-0.5);
  expect(
    box.x + box.width,
    `${what}: правый край ${box.x + box.width} из ${vp.width}`
  ).toBeLessThanOrEqual(vp.width + 0.5);
  expect(box.y, `${what}: верхний край ${box.y}`).toBeGreaterThanOrEqual(-0.5);
  expect(
    box.y + box.height,
    `${what}: нижний край ${box.y + box.height} из ${vp.height}`
  ).toBeLessThanOrEqual(vp.height + 0.5);
}

/** Страница не прокручивается по горизонтали. */
async function expectNoHorizontalScroll(page: Page, what: string): Promise<void> {
  const m = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    inner: window.innerWidth
  }));
  expect(
    m.scroll,
    `${what}: горизонтальная прокрутка, ширина ${m.scroll} при окне ${m.inner}`
  ).toBeLessThanOrEqual(m.inner + 1);
}

/** Ждёт, пока шторка встанет на место: без анимаций и с левым краем у границы окна. */
async function waitSheetSettled(page: Page): Promise<void> {
  await expect
    .poll(
      async () =>
        page.locator(SHEET).evaluate((el) => {
          return el.getAnimations().length === 0 && Math.abs(el.getBoundingClientRect().left) <= 1;
        }),
      { message: 'шторка не встала на место', timeout: T.action }
    )
    .toBe(true);
}

/** Пустое состояние Редактора: кнопка «Показать файлы» открывает шторку. */
async function openSheetFromEmpty(page: Page): Promise<void> {
  await page.locator(SHOW_FILES).first().click({ timeout: T.page });
  await expect(page.locator(SHEET)).toBeVisible({ timeout: T.action });
  await waitSheetSettled(page);
}

/** Кнопка шторки в тулбаре при открытом файле. */
async function openSheetFromToolbar(page: Page): Promise<void> {
  await page.locator(TOGGLE).first().click({ timeout: T.action });
  await expect(page.locator(SHEET)).toBeVisible({ timeout: T.action });
  await waitSheetSettled(page);
}

/** Выбор файла по имени в открытой шторке: шторка закрывается, файл открыт во вкладке с кодом. */
async function pickFileInSheet(page: Page, name: string): Promise<void> {
  const row = page.locator(SHEET).locator(`.file-row:has(.fr-name[title="${name}"])`).first();
  await row.click({ timeout: T.action });
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(page.locator('.editor-tab', { hasText: name }).first()).toBeVisible({
    timeout: T.page
  });
  await expect(page.locator('.cm-content').first()).toBeVisible({ timeout: T.page });
}

/** Выбор тестового файла в открытой шторке. */
async function pickFixtureInSheet(page: Page): Promise<void> {
  await pickFileInSheet(page, fx().name);
}

async function openFixtureFile(page: Page): Promise<void> {
  await openSheetFromEmpty(page);
  await pickFixtureInSheet(page);
}

/** Геометрия и семантика шторки: fixed на всю высоту, в body, диалог, бэкдроп на всё окно. */
async function expectSheetGeometry(page: Page, vp: Viewport): Promise<void> {
  const sheet = page.locator(SHEET);
  await expect(sheet).toHaveAttribute('role', 'dialog');
  await expect(sheet).toHaveAttribute('aria-modal', 'true');
  const info = await sheet.evaluate((el) => ({
    position: getComputedStyle(el).position,
    inMain: el.closest('.main-content') !== null
  }));
  expect(info.position, 'шторка: position').toBe('fixed');
  expect(info.inMain, 'шторка внутри .main-content, а не в body').toBe(false);

  const box = await boxOf(sheet, 'шторка');
  expect(Math.abs(box.y), `шторка: верхний край ${box.y}`).toBeLessThanOrEqual(1);
  expect(
    Math.abs(box.height - vp.height),
    `шторка: высота ${box.height} при окне ${vp.height}`
  ).toBeLessThanOrEqual(1);
  const want = Math.min(0.85 * vp.width, 320);
  expect(
    Math.abs(box.width - want),
    `шторка: ширина ${box.width}, ожидалась ${want}`
  ).toBeLessThanOrEqual(1);

  const bd = await boxOf(page.locator(BACKDROP), 'бэкдроп');
  expect(Math.abs(bd.x), `бэкдроп: левый край ${bd.x}`).toBeLessThanOrEqual(1);
  expect(Math.abs(bd.y), `бэкдроп: верхний край ${bd.y}`).toBeLessThanOrEqual(1);
  expect(Math.abs(bd.width - vp.width), `бэкдроп: ширина ${bd.width}`).toBeLessThanOrEqual(1);
  expect(Math.abs(bd.height - vp.height), `бэкдроп: высота ${bd.height}`).toBeLessThanOrEqual(1);
}

for (const theme of THEMES) {
  test(`editor-mobile sheet-open-file theme:${theme} vp:390`, async ({ page }) => {
    test.setTimeout(90_000 * RT.slow);
    const guard = attachConsoleGuard(page);
    await gotoEditor(page, VP_PHONE, theme);

    await openSheetFromEmpty(page);
    await expectSheetGeometry(page, VP_PHONE);
    await expectNoHorizontalScroll(page, 'шторка открыта');

    // D-16: выбор файла закрывает шторку и открывает вкладку с кодом
    await pickFixtureInSheet(page);
    await expect(page.locator(TOGGLE).first()).toHaveAttribute('aria-expanded', 'false');
    await expectNoHorizontalScroll(page, 'файл открыт');

    await guard.assertClean();
  });
}

test('editor-mobile sheet-close vp:390', async ({ page }) => {
  test.setTimeout(90_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_PHONE);
  await openFixtureFile(page);

  const toggle = page.locator(TOGGLE).first();
  const mainBefore = await boxOf(page.locator(MAIN_CARD).first(), 'карточка кода');
  const overflow0 = await bodyOverflow(page);
  expect(overflow0, 'прокрутка body заблокирована до открытия шторки').not.toBe('hidden');

  // открытая шторка отражается в aria-expanded и не сжимает карточку кода
  await openSheetFromToolbar(page);
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  expect(await bodyOverflow(page), 'шторка открыта, прокрутка body не заблокирована (D-14)').toBe(
    'hidden'
  );
  const mainOpen = await boxOf(page.locator(MAIN_CARD).first(), 'карточка кода при шторке');
  expect(
    Math.abs(mainOpen.width - mainBefore.width),
    `ширина карточки кода изменилась: ${mainBefore.width} → ${mainOpen.width}`
  ).toBeLessThanOrEqual(1);
  await expectNoHorizontalScroll(page, 'шторка открыта поверх файла');

  // кнопка «Закрыть»: зона не меньше 44×44, фокус возвращается на кнопку шторки
  const close = page.locator(CLOSE_BTN);
  const cb = await boxOf(close, 'кнопка «Закрыть»');
  expect(cb.width, `кнопка «Закрыть»: ширина ${cb.width}`).toBeGreaterThanOrEqual(43.5);
  expect(cb.height, `кнопка «Закрыть»: высота ${cb.height}`).toBeGreaterThanOrEqual(43.5);
  await close.click();
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await expect(toggle).toBeFocused();
  expect(await bodyOverflow(page), 'после «Закрыть» прокрутка body не восстановлена').toBe(
    overflow0
  );

  // клавиатура: Enter открывает, Escape закрывает, фокус снова на кнопке
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator(SHEET)).toBeVisible({ timeout: T.action });
  await waitSheetSettled(page);
  expect(await bodyOverflow(page), 'открытие по Enter: прокрутка body не заблокирована').toBe(
    'hidden'
  );
  await page.keyboard.press('Escape');
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(toggle).toBeFocused();
  expect(await bodyOverflow(page), 'после Escape прокрутка body не восстановлена').toBe(overflow0);

  // нажатие мышью по фону правее панели шириной 320 px
  await openSheetFromToolbar(page);
  await page.mouse.click(VP_PHONE.width - 16, VP_PHONE.height / 2);
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  expect(await bodyOverflow(page), 'после нажатия по фону прокрутка body не восстановлена').toBe(
    overflow0
  );

  await guard.assertClean();
});

test('editor-mobile sheet-pick-unsaved vp:390', async ({ page }) => {
  test.setTimeout(90_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_PHONE);
  await openFixtureFile(page);

  // правка закрепляет вкладку: превью-вкладка с несохранёнными правками недостижима
  await page.locator('.cm-content').first().click({ timeout: T.action });
  await page.keyboard.press('Control+End');
  await page.keyboard.insertText(' ');
  const firstTab = page.locator('.editor-tab', {
    has: page.locator(`.tab-name[title="${fx().name}"]`)
  });
  await expect(firstTab.locator('.tab-dirty-dot')).toBeVisible({ timeout: T.action });
  await expect(
    firstTab,
    'правка не закрепила вкладку — подтверждение замены превью стало достижимым, сценарий нужно расширить'
  ).not.toHaveClass(/\bpreview\b/);

  // D-16: шторка закрывается только после того, как второй файл показан во вкладке
  await openSheetFromToolbar(page);
  const order = await observePickOrder(page, altFx().name);
  expect(order.tabSeq, 'второй файл не показан во вкладке').not.toBeNull();
  expect(
    order.closedSeq,
    'шторка не закрылась после выбора файла — возможно, появилось подтверждение'
  ).not.toBeNull();
  expect(
    order.tabSeq!,
    `D-16: шторка закрылась раньше, чем файл показан: шаг закрытия ${order.closedSeq}, шаг вкладки ${order.tabSeq}`
  ).toBeLessThanOrEqual(order.closedSeq!);

  // правки не потеряны: первая вкладка осталась с точкой, второй файл открыт в своей вкладке
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(page.locator('.modal-backdrop')).toHaveCount(0);
  await expect(page.locator('.editor-tab')).toHaveCount(2, { timeout: T.action });
  await expect(firstTab.locator('.tab-dirty-dot')).toBeVisible({ timeout: T.action });
  await expect(
    page.locator('.editor-tab.active', {
      has: page.locator(`.tab-name[title="${altFx().name}"]`)
    })
  ).toHaveCount(1, { timeout: T.action });
  await expect(page.locator(TOGGLE).first()).toHaveAttribute('aria-expanded', 'false');

  // «Сохранить» не нажималось: файл на диске прежний
  expect(await readFixtureOnDisk(page), 'правка попала на диск').toBe(fx().current);
  await expectNoHorizontalScroll(page, 'второй файл открыт');
  await guard.assertClean();
});

test('editor-mobile sheet-pick-busy vp:390', async ({ page }) => {
  test.setTimeout(90_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_PHONE);
  await openSheetFromEmpty(page);

  // G-142-4-WR02: шторка закрывается без ожидания списка копий, строка сразу помечена занятой
  const r = await observeSheetPick(page, [fx().name], fx());
  expect(r.closedSeq, 'шторка не закрылась после выбора файла').not.toBeNull();
  expect(r.backupsEnd, 'ответ списка копий файла не пойман').not.toBeNull();
  // список, показанный позже конца трассы, тоже показан после закрытия; что он показан вообще,
  // проверяет число в кнопке Backups ниже
  expect(
    r.backupsSeq === null || r.closedSeq! < r.backupsSeq,
    `G-142-4-WR02: шторка закрылась только после показа списка копий: шаг закрытия ${r.closedSeq}, шаг списка ${r.backupsSeq}`
  ).toBe(true);
  expect(r.tabSeq, 'файл не показан во вкладке').not.toBeNull();
  expect(
    r.tabSeq!,
    `D-16: шторка закрылась раньше, чем файл показан: шаг закрытия ${r.closedSeq}, шаг вкладки ${r.tabSeq}`
  ).toBeLessThanOrEqual(r.closedSeq!);
  expect(r.busySeq, 'строка файла не получила aria-busy во время чтения').not.toBeNull();
  expect(
    r.busySeq!,
    `aria-busy появился не раньше вкладки: шаг занятости ${r.busySeq}, шаг вкладки ${r.tabSeq}`
  ).toBeLessThan(r.tabSeq!);

  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(
    page.locator('.editor-tab.active', {
      has: page.locator(`.tab-name[title="${fx().name}"]`)
    })
  ).toHaveCount(1, { timeout: T.action });
  await expect(page.locator('.cm-content').first()).toBeVisible({ timeout: T.page });

  // список копий догрузился после закрытия: число в кнопке Backups совпадает с API
  const res = await rawRequest(
    page,
    'GET',
    `/api/config/backups?path=${encodeURIComponent(fx().path)}`
  );
  expectOk(res, 'backups');
  const n = asArray(res.text).length;
  expect(n, `копий файла ${n}`).toBeGreaterThanOrEqual(2);
  await expect(page.locator('.backups-toggle-btn').first()).toContainText(`(${n})`, {
    timeout: T.page
  });

  // занятость снята, строка открытого файла подсвечена выбором
  await openSheetFromToolbar(page);
  await expect(
    page.locator(SHEET).locator('button.file-row[aria-busy="true"]'),
    'занятость строки не снята после загрузки'
  ).toHaveCount(0);
  await expect(
    page.locator(SHEET).locator(`.file-row.active:has(.fr-name[title="${fx().name}"])`)
  ).toHaveCount(1, { timeout: T.action });
  await page.locator(CLOSE_BTN).click();
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });

  await expectNoHorizontalScroll(page, 'файл открыт');
  await guard.assertClean();
});

test('editor-mobile sheet-pick-twice vp:390', async ({ page }) => {
  test.setTimeout(90_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_PHONE);
  await openSheetFromEmpty(page);

  // G-142-4-WR02: два нажатия в одной задаче; первое ставит чтение, второе приходится на его время
  const r = await observeSheetPick(page, [fx().name, altFx().name], altFx());

  // последний выбор побеждает: открыт второй файл, первого во вкладках нет
  await expect(
    page.locator('.editor-tab.active', {
      has: page.locator(`.tab-name[title="${altFx().name}"]`)
    }),
    'G-142-4-WR02: второе нажатие потеряно — открыт не последний выбранный файл'
  ).toHaveCount(1, { timeout: T.action });
  await expect(
    page.locator('.editor-tab', { has: page.locator(`.tab-name[title="${fx().name}"]`) }),
    'первый файл открыт, хотя последним выбран второй'
  ).toHaveCount(0);
  await expect(page.locator('.editor-tab')).toHaveCount(1);

  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(page.locator('.modal-backdrop')).toHaveCount(0);
  await expect(page.locator(TOGGLE).first()).toHaveAttribute('aria-expanded', 'false');

  expect(r.busySeq, 'строка второго файла не получила aria-busy после нажатия').not.toBeNull();
  expect(r.tabSeq, 'второй файл не показан во вкладке').not.toBeNull();
  expect(
    r.busySeq!,
    `aria-busy появился не раньше вкладки: шаг занятости ${r.busySeq}, шаг вкладки ${r.tabSeq}`
  ).toBeLessThan(r.tabSeq!);
  expect(r.closedSeq, 'шторка не закрылась после выбора файла').not.toBeNull();
  expect(
    r.tabSeq!,
    `D-16: шторка закрылась раньше, чем показан последний выбранный файл: шаг закрытия ${r.closedSeq}, шаг вкладки ${r.tabSeq}`
  ).toBeLessThanOrEqual(r.closedSeq!);

  await expectNoHorizontalScroll(page, 'второй файл открыт');
  await guard.assertClean();
});

test('editor-mobile sheet-escape-modal vp:390', async ({ page }) => {
  test.setTimeout(90_000 * RT.slow);
  const dir = fx().dir;
  const filesCount = async (): Promise<number> => {
    const res = await rawRequest(page, 'GET', `/api/config/list?dir=${encodeURIComponent(dir)}`);
    expectOk(res, 'list');
    return asArray(res.text).length;
  };
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_PHONE);
  const before = await filesCount();

  await openFixtureFile(page);
  await openSheetFromToolbar(page);

  // модалка создания открывается поверх шторки; фокус уходит внутрь модалки
  await page.locator(SHEET).locator('.btn-new-file').first().click({ timeout: T.action });
  await expect(page.locator('.modal-backdrop').first()).toBeVisible({ timeout: T.action });
  await expect(page.locator('.modal-backdrop :focus')).toHaveCount(1, { timeout: T.action });

  // первый Escape закрывает только модалку; второй не нажимаем, пока она не ушла полностью
  await page.keyboard.press('Escape');
  await expect(page.locator('.modal-backdrop')).toHaveCount(0, { timeout: T.action });
  // шторка в документе и не inert: уходящая шторка (анимация 200 мс) не считается оставшейся
  await expectSheetStays(page, 'первый Escape');

  // второй Escape закрывает шторку
  await page.keyboard.press('Escape');
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });

  expect(await filesCount(), 'число файлов каталога изменилось').toBe(before);
  await guard.assertClean();
});

test('editor-mobile sheet vp:768', async ({ page }) => {
  test.setTimeout(90_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_TABLET);

  await openSheetFromEmpty(page);
  await expectSheetGeometry(page, VP_TABLET);
  await expectNoHorizontalScroll(page, 'шторка на 768 px');
  await pickFixtureInSheet(page);

  await guard.assertClean();
});

/**
 * Нажимает строку файла в открытой шторке и фиксирует порядок изменений DOM: номер вызова
 * наблюдателя, на котором появилась вкладка файла, и номер, на котором шторка закрылась (ушла из
 * документа или стала inert). Важен порядок изменений, а не кадры: закрытие шторки до ответа
 * сервера видно при любой скорости сети.
 */
async function observePickOrder(
  page: Page,
  fileName: string
): Promise<{ tabSeq: number | null; closedSeq: number | null }> {
  return page.evaluate(
    ({ sheetSel, fileName, capMs }) =>
      new Promise<{ tabSeq: number | null; closedSeq: number | null }>((resolve, reject) => {
        const sheet = document.querySelector<HTMLElement>(sheetSel);
        const row = Array.from(sheet?.querySelectorAll<HTMLButtonElement>('button.file-row') ?? [])
          .filter((b) => b.querySelector(`.fr-name[title="${fileName}"]`) !== null)
          .at(0);
        if (!sheet || !row) {
          reject(
            new Error(`${sheetSel} button.file-row .fr-name[title="${fileName}"]: строки нет`)
          );
          return;
        }
        let seq = 0;
        let tabSeq: number | null = null;
        let closedSeq: number | null = null;
        const finish = () => {
          observer.disconnect();
          clearTimeout(timer);
          resolve({ tabSeq, closedSeq });
        };
        const observer = new MutationObserver(() => {
          seq++;
          if (
            tabSeq === null &&
            document.querySelector(`.editor-tab .tab-name[title="${fileName}"]`) !== null
          ) {
            tabSeq = seq;
          }
          if (closedSeq === null && (!sheet.isConnected || sheet.inert)) {
            closedSeq = seq;
          }
          if (tabSeq !== null && closedSeq !== null) finish();
        });
        const timer = setTimeout(finish, capMs);
        observer.observe(document.body, {
          subtree: true,
          childList: true,
          attributes: true,
          attributeFilter: ['inert']
        });
        row.click();
      }),
    { sheetSel: SHEET, fileName, capMs: T.page }
  );
}

/** Трасса выбора файла в шторке: порядок изменений DOM. */
interface PickTrace {
  /** номер вызова наблюдателя, на котором строка цели получила aria-busy="true" */
  busySeq: number | null;
  /** номер вызова, на котором появилась вкладка цели */
  tabSeq: number | null;
  /** номер вызова, на котором шторка закрылась (ушла из документа или стала inert) */
  closedSeq: number | null;
  /** номер вызова, на котором список копий показан (кнопка Backups); null — не до конца трассы */
  backupsSeq: number | null;
  /** responseEnd запроса списка копий цели (performance.now()-шкала) */
  backupsEnd: number | null;
}

/**
 * Нажимает строки `clicks` в открытой шторке по порядку и фиксирует трассу выбора цели `target`:
 * aria-busy у её строки, появление вкладки, закрытие шторки, показ списка копий и конец ответа
 * списка копий. Первое нажатие синхронно ставит загрузку, поэтому второе в той же задаче всегда
 * приходится на время чтения при любой скорости сети; важен порядок изменений DOM, а не кадры.
 * Сетевой responseEnd с закрытием не сравнивается: ответ может прийти, пока главный поток
 * отрисовывает файл в той же задаче, что и закрытие, хотя Редактор его ещё не обработал.
 * Из страницы возвращаются только номера шагов и числа.
 */
async function observeSheetPick(
  page: Page,
  clicks: string[],
  target: { name: string; path: string }
): Promise<PickTrace> {
  return page.evaluate(
    ({ sheetSel, clicks, targetName, backupsUrl, capMs }) =>
      new Promise<PickTrace>((resolve, reject) => {
        const sheet = document.querySelector<HTMLElement>(sheetSel);
        const rowOf = (name: string): HTMLButtonElement | undefined =>
          Array.from(sheet?.querySelectorAll<HTMLButtonElement>('button.file-row') ?? [])
            .filter((b) => b.querySelector(`.fr-name[title="${name}"]`) !== null)
            .shift();
        const rows = clicks.map(rowOf);
        const targetRow = rowOf(targetName);
        if (!sheet || !targetRow || rows.some((r) => r === undefined)) {
          reject(new Error(`${sheetSel} button.file-row .fr-name[title]: нужной строки нет`));
          return;
        }
        let seq = 0;
        const trace: PickTrace = {
          busySeq: null,
          tabSeq: null,
          closedSeq: null,
          backupsSeq: null,
          backupsEnd: null
        };
        const finish = () => {
          observer.disconnect();
          perf.disconnect();
          clearTimeout(timer);
          resolve(trace);
        };
        const check = () => {
          if (trace.tabSeq !== null && trace.closedSeq !== null && trace.backupsEnd !== null) {
            finish();
          }
        };
        const observer = new MutationObserver(() => {
          seq++;
          if (trace.busySeq === null && targetRow.getAttribute('aria-busy') === 'true') {
            trace.busySeq = seq;
          }
          if (
            trace.tabSeq === null &&
            document.querySelector(`.editor-tab .tab-name[title="${targetName}"]`) !== null
          ) {
            trace.tabSeq = seq;
          }
          if (trace.closedSeq === null && (!sheet.isConnected || sheet.inert)) {
            trace.closedSeq = seq;
          }
          // кнопка Backups появляется, когда Редактор применил список копий открытого файла
          if (
            trace.backupsSeq === null &&
            trace.tabSeq !== null &&
            document.querySelector('.backups-toggle-btn') !== null
          ) {
            trace.backupsSeq = seq;
          }
          check();
        });
        const perf = new PerformanceObserver((list) => {
          for (const entry of list.getEntries()) {
            if (trace.backupsEnd === null && entry.name.includes(backupsUrl)) {
              trace.backupsEnd = (entry as PerformanceResourceTiming).responseEnd;
            }
          }
          check();
        });
        const timer = setTimeout(finish, capMs);
        observer.observe(document.body, {
          subtree: true,
          childList: true,
          attributes: true,
          attributeFilter: ['inert', 'aria-busy']
        });
        perf.observe({ type: 'resource' });
        // все нажатия в одной синхронной задаче
        for (const row of rows) row?.click();
      }),
    {
      sheetSel: SHEET,
      clicks,
      targetName: target.name,
      backupsUrl: '/api/config/backups?path=' + encodeURIComponent(target.path),
      capMs: T.page
    }
  );
}

/** Трасса переключения файла: что видно при активной вкладке цели до ответа списка копий. */
interface SwitchTrace {
  /** номер вызова наблюдателя, на котором вкладка цели стала активной */
  tabSeq: number | null;
  /** performance.now() первого состояния, когда при активной вкладке цели видно прежнее */
  staleAt: number | null;
  /** что именно показало копии прежнего файла */
  staleWhat: 'кнопка Backups' | 'лист Backups' | null;
  /** responseEnd запроса списка копий цели (шкала performance.now()) */
  backupsEnd: number | null;
}

/**
 * Нажимает в `scopeSel` строку файла `target` и следит за DOM: пока активна вкладка цели, кнопка
 * Backups не должна показывать число копий прежнего файла (`staleCount`), а лист Backups прежнего
 * файла не должен быть живым. Важны порядок изменений DOM и одна шкала времени: окно между
 * показом вкладки и ответом списка копий наблюдается при любой скорости сети, потому что запрос
 * стартует только после показа вкладки. Состояние после ответа (у цели могут быть свои копии)
 * отсекается сравнением staleAt < backupsEnd. Уходящий лист (inert, анимация 200 мс) устаревшим
 * не считается: Svelte ставит ему inert синхронно, в том же обновлении, где сменилась вкладка.
 * Из страницы возвращаются только числа, null и одна из двух фиксированных строк.
 */
async function observeBackupsSwitch(
  page: Page,
  scopeSel: string,
  target: { name: string; path: string },
  staleCount: number
): Promise<SwitchTrace> {
  return page.evaluate(
    ({ scopeSel, targetName, backupsUrl, staleText, capMs }) =>
      new Promise<SwitchTrace>((resolve, reject) => {
        const scope = document.querySelector<HTMLElement>(scopeSel);
        const row = Array.from(scope?.querySelectorAll<HTMLButtonElement>('button.file-row') ?? [])
          .filter((b) => b.querySelector(`.fr-name[title="${targetName}"]`) !== null)
          .shift();
        if (!scope || !row) {
          reject(
            new Error(`${scopeSel} button.file-row .fr-name[title="${targetName}"]: строки нет`)
          );
          return;
        }
        let seq = 0;
        const trace: SwitchTrace = {
          tabSeq: null,
          staleAt: null,
          staleWhat: null,
          backupsEnd: null
        };
        let finished = false;
        const finish = () => {
          if (finished) return;
          finished = true;
          observer.disconnect();
          perf.disconnect();
          clearTimeout(timer);
          resolve(trace);
        };
        const inspect = () => {
          const active =
            document.querySelector(`.editor-tab.active .tab-name[title="${targetName}"]`) !== null;
          if (!active) return;
          if (trace.tabSeq === null) trace.tabSeq = seq;
          if (trace.staleAt !== null) return;
          const btn = document.querySelector('.backups-toggle-btn');
          if ((btn?.textContent ?? '').includes(staleText)) {
            trace.staleAt = performance.now();
            trace.staleWhat = 'кнопка Backups';
            return;
          }
          const drawer = document.querySelector<HTMLElement>('.editor-bottom-drawer');
          if (drawer !== null && !drawer.inert) {
            trace.staleAt = performance.now();
            trace.staleWhat = 'лист Backups';
          }
        };
        let settling = false;
        const check = () => {
          if (trace.tabSeq === null || trace.backupsEnd === null || settling) return;
          settling = true;
          requestAnimationFrame(() =>
            requestAnimationFrame(() => {
              inspect();
              finish();
            })
          );
        };
        const observer = new MutationObserver(() => {
          seq++;
          inspect();
          check();
        });
        const perf = new PerformanceObserver((list) => {
          for (const entry of list.getEntries()) {
            if (trace.backupsEnd === null && entry.name.includes(backupsUrl)) {
              trace.backupsEnd = (entry as PerformanceResourceTiming).responseEnd;
            }
          }
          check();
        });
        const timer = setTimeout(finish, capMs);
        observer.observe(document.body, {
          subtree: true,
          childList: true,
          characterData: true,
          attributes: true
        });
        perf.observe({ type: 'resource' });
        row.click();
      }),
    {
      scopeSel,
      targetName: target.name,
      backupsUrl: '/api/config/backups?path=' + encodeURIComponent(target.path),
      staleText: `(${staleCount})`,
      capMs: T.page
    }
  );
}

/** Число копий файла по API панели. */
async function countBackups(page: Page, path: string): Promise<number> {
  const res = await rawRequest(page, 'GET', `/api/config/backups?path=${encodeURIComponent(path)}`);
  expectOk(res, 'backups');
  return asArray(res.text).length;
}

// Порядок пунктов контекстного меню файла: переименовать, дублировать, скачать, «История
// бэкапов», удалить (последний — .ctx-item.ctx-danger). openFileMenu проверяет их число.
const CTX_RENAME = 0;
const CTX_BACKUPS = 3;

/** Правый клик по строке файла в открытой шторке: меню файла с заголовком и пятью пунктами. */
async function openFileMenu(page: Page, name: string): Promise<Locator> {
  const row = page.locator(SHEET).locator(`button.file-row:has(.fr-name[title="${name}"])`).first();
  await row.click({ button: 'right', timeout: T.action });
  const menu = page.locator('.file-context-menu');
  await expect(menu).toBeVisible({ timeout: T.action });
  await expect(menu.locator('.ctx-header')).toContainText(name);
  expect(
    await menu.locator('.ctx-item').count(),
    'порядок пунктов меню файла изменился — сценарий выбирает пункт по номеру'
  ).toBe(5);
  return menu;
}

/** «Сохранить» активной вкладки: меню «Ещё» на узком экране, окно подтверждения, запись. */
async function saveActiveFile(page: Page): Promise<void> {
  const more = page.locator('.btn-overflow-trigger');
  if (await more.isVisible()) {
    await more.click();
    await page.locator('.overflow-dropdown .dropdown-item').first().click();
  } else {
    await page.locator('.eph-right .btn-desktop-only').first().click();
  }
  const dialog = page.locator('.modal-backdrop:has(.confirm-modal-actions)');
  await expect(dialog).toBeVisible({ timeout: T.action });
  await waitAnimationsDone(dialog, 'окно сохранения');
  await dialog.locator('.confirm-modal-actions button').last().click();
  await expect(page.locator('.modal-backdrop')).toHaveCount(0, { timeout: T.save });
  await expect(page.locator('.toast--success').first()).toBeVisible({ timeout: T.action });
}

interface Point {
  x: number;
  y: number;
}

/** Жест пальцем настоящим вводом браузера (CDP): касание, равные шаги движения, отпускание. */
async function touchDrag(cdp: CDPSession, from: Point, to: Point, steps = 8): Promise<void> {
  const send = (type: 'touchStart' | 'touchMove' | 'touchEnd', touchPoints: Point[]) =>
    cdp.send('Input.dispatchTouchEvent', { type, touchPoints });
  await send('touchStart', [from]);
  for (let i = 1; i <= steps; i++) {
    await send('touchMove', [
      { x: from.x + ((to.x - from.x) * i) / steps, y: from.y + ((to.y - from.y) * i) / steps }
    ]);
  }
  await send('touchEnd', []);
}

/** Шторка на месте и не уходит: обработчик touchend и обновление Svelte синхронны с жестом. */
async function expectSheetStays(page: Page, what: string): Promise<void> {
  const sheet = page.locator(SHEET);
  await expect(sheet, `${what}: шторка закрылась`).toHaveCount(1);
  const alive = await sheet.evaluate((el) => el.isConnected && !(el as HTMLElement).inert);
  expect(alive, `${what}: шторка уходит`).toBe(true);
}

test.describe('touch', () => {
  test.use({ hasTouch: true });

  test('editor-mobile sheet-gestures vp:390', async ({ page }) => {
    test.setTimeout(90_000 * RT.slow);
    const guard = attachConsoleGuard(page);
    await gotoEditor(page, VP_PHONE);
    await openFixtureFile(page);
    const overflow0 = await bodyOverflow(page);
    await openSheetFromToolbar(page);
    expect(await bodyOverflow(page), 'шторка открыта, прокрутка body не заблокирована').toBe(
      'hidden'
    );

    const cdp = await page.context().newCDPSession(page);
    const tabs = page.locator('.editor-tab');
    const tabsBefore = await tabs.count();

    // вертикальный жест (dx +10, dy -300) не закрывает шторку и не открывает файл
    await touchDrag(cdp, { x: 150, y: 600 }, { x: 160, y: 300 });
    await expectSheetStays(page, 'вертикальный жест');
    expect(await tabs.count(), 'вертикальный жест открыл файл').toBe(tabsBefore);
    expect(await bodyOverflow(page), 'вертикальный жест снял блокировку прокрутки body').toBe(
      'hidden'
    );

    // диагональный жест (dx -70, dy +80: |dx| < 1.5·|dy|) шторку не закрывает
    await touchDrag(cdp, { x: 200, y: 300 }, { x: 130, y: 380 });
    await expectSheetStays(page, 'диагональный жест');
    expect(await tabs.count(), 'диагональный жест открыл файл').toBe(tabsBefore);
    expect(await bodyOverflow(page), 'диагональный жест снял блокировку прокрутки body').toBe(
      'hidden'
    );

    // горизонтальный свайп влево (dx -120, dy 5) закрывает
    await touchDrag(cdp, { x: 250, y: 400 }, { x: 130, y: 405 });
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
    expect(await bodyOverflow(page), 'после свайпа влево прокрутка body не восстановлена').toBe(
      overflow0
    );

    // касание по фону правее панели закрывает
    await openSheetFromToolbar(page);
    await page.touchscreen.tap(VP_PHONE.width - 16, VP_PHONE.height / 2);
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
    expect(await bodyOverflow(page), 'после касания по фону прокрутка body не восстановлена').toBe(
      overflow0
    );

    await cdp.detach();
    await guard.assertClean();
  });
});

test('editor-mobile desktop-column vp:1280', async ({ page }) => {
  test.setTimeout(120_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_DESKTOP);

  // на ПК панель файлов — колонка со сплиттером: без оверлея, бэкдропа и роли диалога
  const pane = page.locator('.file-tree-pane').first();
  await expect(pane).toBeVisible({ timeout: T.page });
  await expect(pane).not.toHaveClass(/\boverlay\b/);
  expect(await pane.evaluate((el) => el.hasAttribute('role')), 'у колонки есть role').toBe(false);
  await expect(page.locator(BACKDROP)).toHaveCount(0);
  const splitter = page.locator('.editor-splitter').first();
  await expect(splitter).toBeVisible();

  const w0 = (await boxOf(pane, 'колонка файлов')).width;
  expect(w0, `ширина колонки ${w0}`).toBeGreaterThanOrEqual(160);
  expect(w0, `ширина колонки ${w0}`).toBeLessThanOrEqual(450);

  // сплиттер тянет колонку: +80 px, но не шире 450 и не шире 42 % рабочей области
  const sb = await boxOf(splitter, 'сплиттер');
  const cx = sb.x + sb.width / 2;
  const cy = sb.y + sb.height / 2;
  await page.mouse.move(cx, cy);
  await page.mouse.down();
  await page.mouse.move(cx + 80, cy, { steps: 5 });
  await page.mouse.up();
  const cap = await page.locator('.editor-workspace').evaluate((el) => el.clientWidth * 0.42);
  const want = Math.min(w0 + 80, 450, cap);
  await expect
    .poll(async () => Math.abs(((await pane.boundingBox())?.width ?? Number.NaN) - want), {
      message: `ширина колонки после перетаскивания, ожидалась ${want}`,
      timeout: T.action
    })
    .toBeLessThanOrEqual(2);
  const w1 = (await boxOf(pane, 'колонка файлов после перетаскивания')).width;
  await guard.assertClean();

  // ширина сохраняется: вторая страница того же контекста делит localStorage
  const page2 = await page.context().newPage();
  const guard2 = attachConsoleGuard(page2);
  await gotoEditor(page2, VP_DESKTOP);
  const pane2 = page2.locator('.file-tree-pane').first();
  await expect(pane2).toBeVisible({ timeout: T.page });
  expect(
    Math.abs((await boxOf(pane2, 'колонка файлов на второй странице')).width - w1),
    `ширина колонки не сохранилась: было ${w1}`
  ).toBeLessThanOrEqual(1);

  // файл открывается кликом по строке колонки, без шторки; колонка рядом с кодом, не поверх
  const name = fx().name;
  await pane2.locator(`.file-row:has(.fr-name[title="${name}"])`).first().click();
  await expect(page2.locator('.editor-tab', { hasText: name }).first()).toBeVisible({
    timeout: T.page
  });
  await expect(page2.locator('.cm-content').first()).toBeVisible({ timeout: T.page });
  const paneBox = await boxOf(pane2, 'колонка файлов рядом с кодом');
  const cardBox = await boxOf(page2.locator(MAIN_CARD).first(), 'карточка кода на ПК');
  expect(
    paneBox.x + paneBox.width,
    `колонка заходит под карточку кода: правый край ${paneBox.x + paneBox.width}, левый край карточки ${cardBox.x}`
  ).toBeLessThanOrEqual(cardBox.x + 1);
  await expect(page2.locator(SHEET)).toHaveCount(0);

  // кнопка тулбара скрывает и показывает колонку без оверлея
  const toggle = page2.locator(TOGGLE).first();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  // нажатие и один кадр внутри страницы: на ПК колонка исчезает и появляется без анимации
  const clickToggle = () =>
    page2.evaluate(
      (sel) =>
        new Promise<{ panes: number; animations: number }>((resolve, reject) => {
          const btn = document.querySelector<HTMLElement>(sel);
          if (!btn) {
            reject(new Error(`${sel}: кнопки нет`));
            return;
          }
          btn.click();
          requestAnimationFrame(() => {
            const pane = document.querySelector('.file-tree-pane');
            resolve({
              panes: document.querySelectorAll('.file-tree-pane').length,
              animations: pane ? pane.getAnimations().length : 0
            });
          });
        }),
      TOGGLE
    );
  const hidden = await clickToggle();
  expect(
    hidden.panes,
    'колонка ПК уходит с анимацией: через кадр после нажатия она ещё в документе'
  ).toBe(0);
  await expect(page2.locator('.file-tree-pane')).toHaveCount(0, { timeout: T.action });
  await expect(page2.locator(BACKDROP)).toHaveCount(0);
  const shown = await clickToggle();
  expect(shown.panes, 'колонка ПК не появилась через кадр после нажатия').toBeGreaterThanOrEqual(1);
  expect(shown.animations, 'колонка ПК появляется с анимацией').toBe(0);
  await expect(page2.locator('.file-tree-pane').first()).toBeVisible({ timeout: T.action });
  await expect(page2.locator('.file-tree-pane').first()).not.toHaveClass(/\boverlay\b/);

  await guard2.assertClean();
  await page2.close();
});

/** Ждёт конца всех анимаций элемента и его потомков: рамка перестаёт меняться. */
async function waitAnimationsDone(loc: Locator, what: string): Promise<void> {
  await expect
    .poll(async () => loc.evaluate((el) => el.getAnimations({ subtree: true }).length), {
      message: `${what}: анимации не закончились`,
      timeout: T.action
    })
    .toBe(0);
}

for (const theme of THEMES) {
  test(`editor-mobile actions theme:${theme} vp:390`, async ({ page }) => {
    test.setTimeout(120_000 * RT.slow);
    const guard = attachConsoleGuard(page);
    await gotoEditor(page, VP_PHONE, theme);
    await openFixtureFile(page);
    await expectNoHorizontalScroll(page, 'файл открыт');

    // шапка (D-20): «Сохранить и применить» и «Ещё» видны, кнопки для ПК скрыты
    const apply = page.locator('.eph-right .btn-accent').first();
    await expect(apply).toBeVisible({ timeout: T.page });
    await expectInside(page, apply, '«Сохранить и применить»');
    const more = page.locator('.btn-overflow-trigger').first();
    await expect(more).toBeVisible();
    await expectInside(page, more, '«Ещё»');
    const desktopOnly = page.locator('.eph-right .btn-desktop-only');
    const desktopCount = await desktopOnly.count();
    for (let i = 0; i < desktopCount; i++) {
      await expect(desktopOnly.nth(i), `кнопка для ПК №${i} видна на 390 px`).toBeHidden();
    }

    // две кнопки «⋮»/«Ещё» различимы по aria-label (§7 D-24)
    const kebab = page.locator('.btn-kebab').first();
    await expect(kebab).toBeVisible();
    const moreLabel = (await more.getAttribute('aria-label')) ?? '';
    const kebabLabel = (await kebab.getAttribute('aria-label')) ?? '';
    expect(moreLabel, 'у «Ещё» нет aria-label').not.toBe('');
    expect(kebabLabel, 'у «⋮» нет aria-label').not.toBe('');
    expect(moreLabel, `aria-label «Ещё» и «⋮» совпадают: ${moreLabel}`).not.toBe(kebabLabel);

    // меню «Ещё» (D-21): два пункта, целиком внутри окна, Escape и клик вне закрывают.
    // Пункты «Сохранить» и «Перезагрузить» не нажимаются: считаются и измеряются
    await more.click();
    const menu = page.locator('.overflow-dropdown[role="menu"]');
    await expect(menu).toBeVisible({ timeout: T.action });
    const items = menu.locator('.dropdown-item');
    expect(await items.count(), 'пунктов в меню «Ещё»').toBe(2);
    await expectInside(page, menu, 'меню «Ещё»');
    for (let i = 0; i < 2; i++) {
      await expectInside(page, items.nth(i), `пункт меню «Ещё» №${i + 1}`);
    }
    await expectNoHorizontalScroll(page, 'меню «Ещё» открыто');
    await page.keyboard.press('Escape');
    await expect(page.locator('.overflow-dropdown')).toHaveCount(0, { timeout: T.action });

    await more.click();
    await expect(menu).toBeVisible({ timeout: T.action });
    await page.locator('.cm-content').first().click();
    await expect(page.locator('.overflow-dropdown')).toHaveCount(0, { timeout: T.action });

    // меню файла «⋮»: появляется с затуханием, после него рамка стабильна
    await kebab.click();
    const kebabMenu = page.locator('.kebab-dropdown');
    await expect(kebabMenu).toBeVisible({ timeout: T.action });
    await waitAnimationsDone(kebabMenu, 'меню файла «⋮»');
    await expectInside(page, kebabMenu, 'меню файла «⋮»');
    await expectNoHorizontalScroll(page, 'меню файла открыто');
    await kebab.click();
    await expect(page.locator('.kebab-dropdown')).toHaveCount(0, { timeout: T.action });

    // статус-бар (D-22, §7 D-23): одна строка, чипы с подписью, «Backups (N)» с числом из API
    const bar = page.locator('.editor-statusbar').first();
    await expect(bar).toBeVisible({ timeout: T.page });
    await expectInside(page, bar, 'статус-бар');
    const barBox = await boxOf(bar, 'статус-бар');
    expect(
      barBox.height,
      `статус-бар не в одну строку: высота ${barBox.height}`
    ).toBeLessThanOrEqual(48);
    const chips = bar.locator('.chip-toggle');
    expect(await chips.count(), 'чипов в статус-баре').toBe(2);
    for (let i = 0; i < 2; i++) {
      const chip = chips.nth(i);
      await expect(chip).toBeVisible();
      const label = (await chip.getAttribute('aria-label')) ?? '';
      expect(label, `у чипа №${i + 1} нет aria-label`).not.toBe('');
      await expectInside(page, chip, `чип статус-бара №${i + 1}`);
    }
    const res = await rawRequest(
      page,
      'GET',
      `/api/config/backups?path=${encodeURIComponent(fx().path)}`
    );
    expectOk(res, 'backups');
    const n = asArray(res.text).length;
    expect(n, `копий файла ${n}`).toBeGreaterThanOrEqual(2);
    const backupsBtn = bar.locator('.backups-toggle-btn');
    await expect(backupsBtn).toBeVisible({ timeout: T.page });
    await expect(backupsBtn).toContainText(`(${n})`, { timeout: T.page });
    await expectInside(page, backupsBtn, 'кнопка Backups');

    expect(await page.evaluate(() => document.documentElement.getAttribute('data-theme'))).toBe(
      theme
    );
    await expectNoHorizontalScroll(page, 'действия: итог');
    await guard.assertClean();
  });
}

/** Содержимое общего тестового файла на диске, как его видит API. */
async function readFixtureOnDisk(page: Page): Promise<string> {
  return readOnDisk(page, fx().path);
}

/** Высота рамки кнопки не меньше сенсорной зоны 44 px. */
async function expectTapZone(loc: Locator, what: string): Promise<void> {
  const b = await boxOf(loc, what);
  expect(b.height, `${what}: высота ${b.height}`).toBeGreaterThanOrEqual(43.5);
}

for (const theme of THEMES) {
  test(`editor-mobile backups theme:${theme} vp:390`, async ({ page }) => {
    test.setTimeout(150_000 * RT.slow);
    const guard = attachConsoleGuard(page);
    await gotoEditor(page, VP_PHONE, theme);
    await openFixtureFile(page);

    const listRes = await rawRequest(
      page,
      'GET',
      `/api/config/backups?path=${encodeURIComponent(fx().path)}`
    );
    expectOk(listRes, 'backups');
    const n = asArray(listRes.text).length;
    expect(n, `копий файла ${n}`).toBeGreaterThanOrEqual(2);
    expect(await readFixtureOnDisk(page), 'файл на диске до сценария').toBe(fx().current);

    // длинная строка вводится только в редактор: сравнение строится против текущего документа,
    // поэтому она попадает в diff как добавленная; «Сохранить» не нажимается
    const fixtureTab = page.locator('.editor-tab', {
      has: page.locator(`.tab-name[title="${fx().name}"]`)
    });
    await page.locator('.cm-content').first().click({ timeout: T.action });
    await page.keyboard.press('Control+End');
    await page.keyboard.insertText(LONG_DIFF_LINE);
    await page.keyboard.press('Escape');
    await expect(fixtureTab.locator('.tab-dirty-dot')).toBeVisible({ timeout: T.action });
    await expect(
      page.locator('.cm-content').first(),
      'длинная строка не попала в редактор'
    ).toContainText(LONG_DIFF_LINE, { timeout: T.action });

    // лист открывается на всю карточку Редактора
    const toggle = page.locator('.backups-toggle-btn');
    const drawer = page.locator('.editor-bottom-drawer');
    await toggle.click({ timeout: T.page });
    await expect(drawer).toBeVisible({ timeout: T.action });
    await waitAnimationsDone(drawer, 'лист Backups');
    // inset: 0 отсчитывается от внутренней области карточки: её рамка остаётся вокруг листа
    const card = page.locator(MAIN_CARD).first();
    const cardOuter = await boxOf(card, 'карточка кода');
    const inner = await card.evaluate((el) => ({
      left: el.clientLeft,
      top: el.clientTop,
      width: el.clientWidth,
      height: el.clientHeight
    }));
    const cardBox = {
      x: cardOuter.x + inner.left,
      y: cardOuter.y + inner.top,
      width: inner.width,
      height: inner.height
    };
    const drawerBox = await boxOf(drawer, 'лист Backups');
    for (const k of ['x', 'y', 'width', 'height'] as const) {
      expect(
        Math.abs(drawerBox[k] - cardBox[k]),
        `лист не на всю карточку: ${k} листа ${drawerBox[k]}, карточки внутри рамки ${cardBox[k]}`
      ).toBeLessThanOrEqual(1);
    }
    await expectInside(page, drawer, 'лист Backups');
    await expectNoHorizontalScroll(page, 'лист Backups открыт');

    // экран списка: только копии, без подсказки, «Назад» и «Восстановить»
    const items = page.locator('.backup-select-btn');
    expect(await items.count(), 'копий в списке листа').toBe(n);
    for (let i = 0; i < n; i++) {
      await expect(items.nth(i)).toBeVisible();
      await expectInside(page, items.nth(i), `копия №${i + 1}`);
    }
    await expect(page.locator('.drawer-back-btn')).toHaveCount(0);
    await expect(page.locator('.drawer-empty-state')).toBeHidden();
    await expect(page.locator('.restore-inline-btn').first()).toBeHidden();
    const close = page.locator('.drawer-close-btn');
    await expect(close).toBeVisible();
    await expectInside(page, close, 'кнопка «Закрыть» листа');
    await expectTapZone(close, 'кнопка «Закрыть» листа');

    // экран сравнения: на весь лист, со строками различий, кнопки не меньше 44 px
    await items.first().click();
    const diff = page.locator('.diff-viewer-container');
    await expect(diff).toBeVisible({ timeout: T.action });
    await expect(page.locator('.diff-body .spinner')).toHaveCount(0, { timeout: T.page });
    expect(
      await page.locator('.diff-line-added, .diff-line-removed').count(),
      'строк различий в сравнении'
    ).toBeGreaterThanOrEqual(1);
    // G-142-2, D-25, UI-SPEC E5: длинная строка не переносится, а прокручивается по горизонтали
    await expect(
      page.locator('.diff-line-added', { hasText: LONG_DIFF_LINE }),
      'длинная строка не попала в сравнение'
    ).toHaveCount(1, { timeout: T.action });
    const longLine = await page.locator('.diff-body').evaluate((el, text) => {
      const row = Array.from(el.querySelectorAll<HTMLElement>('.diff-line-added')).find((r) =>
        (r.textContent ?? '').includes(text)
      );
      if (!row) throw new Error('строка с длинным текстом не найдена');
      const clipped = Array.from(
        el.querySelectorAll<HTMLElement>('.diff-line-added, .diff-line-removed')
      ).filter((r) => r.scrollWidth > r.clientWidth + 1).length;
      el.scrollLeft = el.scrollWidth;
      const moved = el.scrollLeft > 0;
      el.scrollLeft = 0;
      return {
        scrollWidth: el.scrollWidth,
        clientWidth: el.clientWidth,
        height: row.getBoundingClientRect().height,
        lineHeight: parseFloat(getComputedStyle(row).lineHeight),
        clipped,
        moved
      };
    }, LONG_DIFF_LINE);
    expect(
      longLine.height,
      `G-142-2: длинная строка diff переносится: высота ${longLine.height} при высоте строки ${longLine.lineHeight}`
    ).toBeLessThan(1.5 * longLine.lineHeight);
    expect(
      longLine.scrollWidth,
      `.diff-body не прокручивается по горизонтали: scrollWidth ${longLine.scrollWidth}, clientWidth ${longLine.clientWidth}`
    ).toBeGreaterThan(longLine.clientWidth + 1);
    expect(longLine.moved, '.diff-body не сдвигается по горизонтали').toBe(true);
    expect(
      longLine.clipped,
      `подсветка строк различий не тянется на ширину прокрутки: строк с текстом за фоном ${longLine.clipped}`
    ).toBe(0);
    await expect(page.locator('.drawer-sidebar')).toBeHidden();
    const back = page.locator('.drawer-back-btn');
    const restore = page.locator('.diff-restore-btn');
    for (const [loc, what] of [
      [back, '«Назад»'],
      [restore, '«Восстановить» в сравнении']
    ] as const) {
      await expect(loc).toBeVisible();
      await expectInside(page, loc, what);
      await expectTapZone(loc, what);
    }
    expect(
      await page.locator('.diff-body').evaluate((el) => getComputedStyle(el).overflowX),
      'diff-body: overflow-x'
    ).toBe('auto');
    await expectNoHorizontalScroll(page, 'сравнение открыто');

    // «Назад» возвращает к списку
    await back.click();
    await expect(items.first()).toBeVisible({ timeout: T.action });
    await expect(diff).toBeHidden();

    // «Восстановить»: окно подтверждения, подстановка в редактор без записи на диск
    await items.first().click();
    await expect(diff).toBeVisible({ timeout: T.action });
    await page.locator('.diff-restore-btn').click();
    const dialog = page.locator('[role="dialog"]:has(.confirm-actions)');
    await expect(dialog).toBeVisible({ timeout: T.action });
    await waitAnimationsDone(dialog, 'окно подтверждения');
    await expectInside(page, dialog, 'окно подтверждения');
    const confirmButtons = dialog.locator('.confirm-actions button');
    const confirmCount = await confirmButtons.count();
    expect(confirmCount, 'кнопок в окне подтверждения').toBeGreaterThanOrEqual(2);
    for (let i = 0; i < confirmCount; i++) {
      await expectInside(page, confirmButtons.nth(i), `кнопка окна подтверждения №${i + 1}`);
    }
    await expectNoHorizontalScroll(page, 'окно подтверждения открыто');
    await confirmButtons.last().click();
    await expect(page.locator('[role="dialog"]:has(.confirm-actions)')).toHaveCount(0, {
      timeout: T.action
    });
    await expect(drawer).toHaveCount(0, { timeout: T.action });
    await expect(page.locator('.toast--success').first()).toBeVisible({ timeout: T.action });
    await expect(
      page.locator('.editor-tab', { hasText: fx().name }).locator('.tab-dirty-dot')
    ).toBeVisible({ timeout: T.action });
    // копия создана до ввода длинной строки: после подстановки её в редакторе быть не должно
    await expect(
      page.locator('.cm-content').first(),
      'Восстановить не подставило копию в редактор: длинная строка осталась'
    ).not.toContainText(LONG_DIFF_LINE, { timeout: T.action });
    expect(await readFixtureOnDisk(page), 'файл на диске после «Восстановить»').toBe(fx().current);

    // «Закрыть» закрывает лист
    await toggle.click();
    await expect(drawer).toBeVisible({ timeout: T.action });
    await page.locator('.drawer-close-btn').click();
    await expect(drawer).toHaveCount(0, { timeout: T.action });

    expect(await page.evaluate(() => document.documentElement.getAttribute('data-theme'))).toBe(
      theme
    );
    await guard.assertClean();
  });
}

test('editor-mobile backups-switch vp:390', async ({ page }) => {
  test.setTimeout(120_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_PHONE);
  await openFixtureFile(page);

  const n = await countBackups(page, fx().path);
  const m = await countBackups(page, altFx().path);
  expect(n, `копий первого файла ${n}`).toBeGreaterThanOrEqual(2);
  expect(m, 'сценарий не различает списки: у файлов одинаковое число копий').not.toBe(n);
  const toggle = page.locator('.backups-toggle-btn');
  await expect(toggle).toContainText(`(${n})`, { timeout: T.page });

  await openSheetFromToolbar(page);
  const r = await observeBackupsSwitch(page, SHEET, altFx(), n);
  expect(r.tabSeq, 'второй файл не показан во вкладке').not.toBeNull();
  expect(r.backupsEnd, 'ответ списка копий второго файла не пойман').not.toBeNull();
  expect(
    r.staleAt !== null && r.staleAt < r.backupsEnd!,
    `WR-01: после переключения на второй файл ${r.staleWhat} показывает копии прежнего файла (${n}) до ответа списка копий`
  ).toBe(false);

  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  if (m === 0) {
    await expect(toggle).toHaveCount(0, { timeout: T.page });
  } else {
    await expect(toggle).toContainText(`(${m})`, { timeout: T.page });
  }
  await expectNoHorizontalScroll(page, 'второй файл открыт');
  await guard.assertClean();
});

test('editor-mobile backups-switch vp:1280', async ({ page, browser }) => {
  test.setTimeout(150_000 * RT.slow);
  const created: AltFixture[] = [];
  try {
    // временный файл X для шага «Восстановить во время смены файла»: создаётся до перехода,
    // чтобы строка X попала в колонку при загрузке страницы
    const x = await withPanel(browser, async (pg) => {
      const file = await createScratchFile(pg, []);
      created.push(file);
      expectOk(
        await rawRequest(
          pg,
          'POST',
          `/api/config/save?path=${encodeURIComponent(file.path)}`,
          SCRATCH_TEXT
        ),
        'save scratch'
      );
      return file;
    });

    const guard = attachConsoleGuard(page);
    await gotoEditor(page, VP_DESKTOP);
    const column = page.locator('.file-tree-pane');
    const rowOf = (name: string) =>
      column.locator(`.file-row:has(.fr-name[title="${name}"])`).first();
    await rowOf(fx().name).click({ timeout: T.page });
    await expect(page.locator('.editor-tab', { hasText: fx().name }).first()).toBeVisible({
      timeout: T.page
    });
    await expect(page.locator('.cm-content').first()).toBeVisible({ timeout: T.page });

    const n = await countBackups(page, fx().path);
    const m = await countBackups(page, altFx().path);
    expect(n, `копий первого файла ${n}`).toBeGreaterThanOrEqual(2);
    expect(m, 'сценарий не различает списки: у файлов одинаковое число копий').not.toBe(n);
    const toggle = page.locator('.backups-toggle-btn');
    await expect(toggle).toContainText(`(${n})`, { timeout: T.page });

    const drawer = page.locator('.editor-bottom-drawer');
    await toggle.click();
    await expect(drawer).toBeVisible({ timeout: T.action });
    await page.locator('.backup-select-btn').first().click();
    await expect(page.locator('.diff-viewer-container')).toBeVisible({ timeout: T.action });
    await expect(page.locator('.diff-body .spinner')).toHaveCount(0, { timeout: T.page });

    const r = await observeBackupsSwitch(page, '.file-tree-pane', altFx(), n);
    expect(r.tabSeq, 'второй файл не показан во вкладке').not.toBeNull();
    expect(r.backupsEnd, 'ответ списка копий второго файла не пойман').not.toBeNull();
    expect(
      r.staleAt !== null && r.staleAt < r.backupsEnd!,
      `WR-01: после переключения на второй файл ${r.staleWhat} показывает копии прежнего файла (${n}) до ответа списка копий`
    ).toBe(false);

    // узел листа уходит 200 мс: ожидание с автоповтором, без фиксированных пауз
    await expect(drawer).toHaveCount(0, { timeout: T.page });
    if (m === 0) {
      await expect(toggle).toHaveCount(0, { timeout: T.page });
    } else {
      await expect(toggle).toContainText(`(${m})`, { timeout: T.page });
    }

    // лист на ПК остаётся открытым при смене файла (прежнее поведение)
    await rowOf(fx().name).click({ timeout: T.action });
    await expect(toggle).toContainText(`(${n})`, { timeout: T.page });
    await expect(drawer).toBeVisible({ timeout: T.action });
    await expect(
      page.locator('.diff-viewer-container'),
      'WR-01: выбор копии и сравнение не сброшены после смены файла'
    ).toHaveCount(0);
    await expect(page.locator('.drawer-empty-state')).toBeVisible({ timeout: T.action });

    await toggle.click();
    await expect(drawer).toHaveCount(0, { timeout: T.action });

    // WR-01 ревью 19.10: «Восстановить» во время смены файла на ПК. Лист копий fixture открыт,
    // пользователь нажимает файл X в колонке и, пока X читается, «Восстановить» у копии fixture.
    // Подтверждение приходит уже после показа X: копия fixture не должна попасть в X.
    const copies = asArray(
      (await rawRequest(page, 'GET', `/api/config/backups?path=${encodeURIComponent(fx().path)}`))
        .text
    ).filter((b): b is string => typeof b === 'string');
    expect(copies.length, 'у файла fixture нет копий').toBeGreaterThanOrEqual(1);
    const xText = await readOnDisk(page, x.path);
    for (const b of copies) {
      expect(
        await readOnDisk(page, b),
        'сценарий не различит подстановку: копия fixture совпадает с текстом временного файла'
      ).not.toBe(xText);
    }
    await expect(
      page.locator(`.editor-tab .tab-name[title="${x.name}"]`),
      'у временного файла уже есть вкладка: loadFile не пойдёт в сеть, окна гонки нет'
    ).toHaveCount(0);
    await expect(column.locator(`.file-row:has(.fr-name[title="${x.name}"])`).first()).toBeVisible({
      timeout: T.page
    });

    await toggle.click();
    await expect(drawer).toBeVisible({ timeout: T.action });
    await expect(page.locator('.backup-item')).toHaveCount(n, { timeout: T.action });

    // оба нажатия в одном такте страницы: чтение X уже идёт, а «Восстановить» проверяет копию
    // до смены файла. Так сценарий ревью воспроизводится детерминированно, без пауз и подмены сети
    await page.evaluate((name) => {
      const nameEl = document.querySelector(`.file-tree-pane .fr-name[title="${name}"]`);
      const row = nameEl?.closest('button.file-row');
      if (!(row instanceof HTMLElement)) {
        throw new Error(`не найдена строка файла: .file-tree-pane button.file-row ${name}`);
      }
      const restore = document.querySelector(
        '.editor-bottom-drawer .backup-item .restore-inline-btn'
      );
      if (!(restore instanceof HTMLElement)) {
        throw new Error(
          'не найдена кнопка: .editor-bottom-drawer .backup-item .restore-inline-btn'
        );
      }
      row.click();
      restore.click();
    }, x.name);

    const confirm = page.locator('[role="dialog"]:has(.confirm-actions)');
    await expect(confirm, 'Восстановить не открыло подтверждение').toBeVisible({
      timeout: T.action
    });
    await expect(
      page.locator(`.editor-tab.active .tab-name[title="${x.name}"]`),
      'второй файл не показан, пока открыто подтверждение'
    ).toHaveCount(1, { timeout: T.page });
    await expect(page.locator('.cm-content').first()).toBeVisible({ timeout: T.page });

    await waitAnimationsDone(confirm, 'окно подтверждения');
    await confirm.locator('.confirm-actions button').last().click();
    await expect(confirm).toHaveCount(0, { timeout: T.action });

    const staleToast = page.locator('.toast--info', {
      hasText: /Бэкап не восстановлен|Backup not restored/
    });
    const restoredToast = page.locator('.toast--success', {
      hasText: /Backup восстановлен|Backup restored/
    });
    await expect(
      staleToast.or(restoredToast).first(),
      'Восстановить не завершилось: нет ни уведомления об отмене, ни уведомления о восстановлении'
    ).toBeVisible({ timeout: T.action });
    // исход определён: проверки ниже не проходят вхолостую
    await expect(
      page.locator(`.editor-tab:has(.tab-name[title="${x.name}"]) .tab-dirty-dot`),
      'WR-01 restore: копия прежнего файла подставлена во второй файл после подтверждения — вкладка второго файла с несохранёнными правками'
    ).toHaveCount(0);
    await expect(
      restoredToast,
      'WR-01 restore: показано «Backup восстановлен» для копии прежнего файла'
    ).toHaveCount(0);
    await expect(staleToast.first()).toBeVisible({ timeout: T.action });
    await expect(page.locator(`.editor-tab.active .tab-name[title="${x.name}"]`)).toHaveCount(1);
    expect(await readOnDisk(page, x.path), 'WR-01 restore: временный файл на диске изменился').toBe(
      xText
    );

    await guard.assertClean();
  } finally {
    await withPanel(browser, async (pg) => {
      for (const f of created) await deleteFileWithBackups(pg, f.path);
    });
  }
});

test('editor-mobile menu-save-target vp:390', async ({ page, browser }) => {
  test.setTimeout(240_000 * RT.slow);
  const dir = fixtureDir()!.dir;
  const created: AltFixture[] = [];
  try {
    // A открыт в редакторе, F — цель отменяемых действий, D — удаляется
    const origin = await withPanel(browser, async (pg) => {
      const x = await createScratchFile(pg, []);
      created.push(x);
      const y = await createScratchFile(pg, [x.name]);
      created.push(y);
      const z = await createScratchFile(pg, [x.name, y.name]);
      created.push(z);
      return { files: [x, y, z], a: await readOnDisk(pg, x.path), f: await readOnDisk(pg, y.path) };
    });
    const [a, f, d] = origin.files;

    const guard = attachConsoleGuard(page);
    await gotoEditor(page, VP_PHONE);
    await openSheetFromEmpty(page);
    await pickFileInSheet(page, a.name);
    const tabA = page.locator('.editor-tab', { has: page.locator(`.tab-name[title="${a.name}"]`) });

    // правка вкладки A: пробел в конце документа, ядро принимает запись
    let edits = 0;
    const editA = async () => {
      edits++;
      await page.locator('.cm-content').first().click({ timeout: T.action });
      await page.keyboard.press('Control+End');
      await page.keyboard.insertText(' ');
      await expect(tabA.locator('.tab-dirty-dot')).toBeVisible({ timeout: T.action });
    };
    const expectDisk = async (what: string) => {
      const got = await readOnDisk(page, a.path);
      expect(got, `CR-01: ${what}: открытая вкладка не сохранена в свой файл`).toBe(
        origin.a + ' '.repeat(edits)
      );
    };

    // «Переименовать → Отмена»
    await editA();
    await openSheetFromToolbar(page);
    const menu1 = await openFileMenu(page, f.name);
    await menu1.locator('.ctx-item').nth(CTX_RENAME).click();
    await expect(page.locator('.modal-backdrop')).toBeVisible({ timeout: T.action });
    await expect(
      page.locator('#rename-target'),
      'цель переименования — не файл из меню'
    ).toHaveValue(f.name);
    await page.locator('.modal-backdrop .confirm-modal-actions .btn-secondary').click();
    await expect(page.locator('.modal-backdrop')).toHaveCount(0, { timeout: T.action });
    await page.locator(CLOSE_BTN).click();
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
    await saveActiveFile(page);
    expect(
      await readOnDisk(page, f.path),
      'CR-01: после «Переименовать → Отмена» «Сохранить» записало открытую вкладку в файл из меню'
    ).toBe(origin.f);
    await expectDisk('после «Переименовать → Отмена»');
    await openSheetFromToolbar(page);
    await expect(
      page.locator(SHEET).locator('.file-row.active'),
      'CR-01: в дереве выбран файл из меню, а не открытый'
    ).toHaveCount(1);
    await expect(
      page.locator(SHEET).locator(`.file-row.active:has(.fr-name[title="${a.name}"])`)
    ).toHaveCount(1);
    await page.locator(CLOSE_BTN).click();
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });

    // «Удалить → Отмена»
    await editA();
    await openSheetFromToolbar(page);
    const menu2 = await openFileMenu(page, f.name);
    await menu2.locator('.ctx-item.ctx-danger').click();
    await expect(page.locator('.modal-backdrop')).toContainText(f.name, { timeout: T.action });
    await page.locator('.modal-backdrop .confirm-modal-actions .btn-secondary').click();
    await expect(page.locator('.modal-backdrop')).toHaveCount(0, { timeout: T.action });
    await page.locator(CLOSE_BTN).click();
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
    await saveActiveFile(page);
    expect(
      await readOnDisk(page, f.path),
      'CR-01: после «Удалить → Отмена» «Сохранить» записало открытую вкладку в файл из меню'
    ).toBe(origin.f);
    await expectDisk('после «Удалить → Отмена»');

    // «Удалить → подтвердить» по неоткрытому файлу D
    await editA();
    await openSheetFromToolbar(page);
    const menu3 = await openFileMenu(page, d.name);
    await menu3.locator('.ctx-item.ctx-danger').click();
    await expect(page.locator('.modal-backdrop')).toContainText(d.name, { timeout: T.action });
    await page.locator('.modal-backdrop .confirm-modal-actions .btn-danger').click();
    await expect(page.locator('.modal-backdrop')).toHaveCount(0, { timeout: T.action });
    await expect(
      page.locator(SHEET).locator(`button.file-row:has(.fr-name[title="${d.name}"])`)
    ).toHaveCount(0, { timeout: T.action });
    await expect(
      page.locator(`.editor-tab.active .tab-name[title="${a.name}"]`),
      'CR-01: после удаления файла меню активна не вкладка открытого файла'
    ).toHaveCount(1);
    await page.locator(CLOSE_BTN).click();
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
    await saveActiveFile(page);
    expect(
      await listNames(page, dir),
      'CR-01: «Сохранить» создало заново удалённый файл'
    ).not.toContain(d.name);
    await expectDisk('после «Удалить → подтвердить»');

    // «История бэкапов» по F: файл открывается во вкладке, F на диске прежний
    await editA();
    await openSheetFromToolbar(page);
    const menu4 = await openFileMenu(page, f.name);
    await menu4.locator('.ctx-item').nth(CTX_BACKUPS).click();
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
    await expect(
      page.locator(`.editor-tab.active .tab-name[title="${f.name}"]`),
      'CR-01: «История бэкапов» не открыла файл из меню — «Сохранить» нацелено не на открытую вкладку'
    ).toHaveCount(1, { timeout: T.page });
    await expect(page.locator('.editor-bottom-drawer')).toHaveCount(0);
    await tabA.click({ timeout: T.action });
    await expect(page.locator(`.editor-tab.active .tab-name[title="${a.name}"]`)).toHaveCount(1, {
      timeout: T.page
    });
    await saveActiveFile(page);
    expect(
      await readOnDisk(page, f.path),
      'CR-01: после «История бэкапов» «Сохранить» записало открытую вкладку в файл из меню'
    ).toBe(origin.f);
    await expectDisk('после «История бэкапов»');

    await expectNoHorizontalScroll(page, 'действия меню выполнены');
    await guard.assertClean();
  } finally {
    await withPanel(browser, async (pg) => {
      for (const x of created) await deleteFileWithBackups(pg, x.path);
    });
  }
});

test('editor-mobile menu-backups vp:390', async ({ page }) => {
  test.setTimeout(150_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  await gotoEditor(page, VP_PHONE);
  // Редактор пуст: вкладок нет
  await openSheetFromEmpty(page);

  const n = await countBackups(page, fx().path);
  const m = await countBackups(page, altFx().path);
  expect(n, `копий первого файла ${n}`).toBeGreaterThanOrEqual(2);
  expect(m, 'у второго тестового файла есть копии — сценарий пустого списка не проверяется').toBe(
    0
  );

  const drawer = page.locator('.editor-bottom-drawer');
  const activeTab = (name: string) => page.locator(`.editor-tab.active .tab-name[title="${name}"]`);

  // файл с копиями: файл открыт, лист виден со всеми копиями из API
  const menu1 = await openFileMenu(page, fx().name);
  await menu1.locator('.ctx-item').nth(CTX_BACKUPS).click();
  await expect(
    activeTab(fx().name),
    'WR-03: «История бэкапов» из шторки при пустом Редакторе не открыли файл'
  ).toHaveCount(1, { timeout: T.page });
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(drawer, 'WR-03: лист копий не открылся').toBeVisible({ timeout: T.page });
  await expect(page.locator('.backup-select-btn'), 'WR-03: в листе не все копии файла').toHaveCount(
    n,
    { timeout: T.action }
  );
  // Путь ошибки списка копий на настоящем стенде без подмены сети не воспроизводится: сервер
  // отвечает не 2xx только при отказе валидатора пути или ошибке шаблона, а для прочитанного
  // файла оба уже пройдены. Сценарий держит оба успешных пути: точный текст E5 empty ниже и
  // отсутствие уведомления об ошибке у обоих.
  await expect(
    page.locator('.toast--error'),
    'WR-03: при успешном списке копий показано уведомление об ошибке'
  ).toHaveCount(0);
  await page.locator('.drawer-close-btn').click();
  await expect(drawer).toHaveCount(0, { timeout: T.action });

  // файл без копий: файл открыт, листа нет, есть уведомление
  await openSheetFromToolbar(page);
  const menu2 = await openFileMenu(page, altFx().name);
  await menu2.locator('.ctx-item').nth(CTX_BACKUPS).click();
  await expect(activeTab(altFx().name)).toHaveCount(1, { timeout: T.page });
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(
    page.locator('.toast--info', { hasText: /Нет сохранённых бэкапов|No saved backups/ }).first(),
    'WR-03: нет уведомления «Нет сохранённых бэкапов» при успешном пустом списке'
  ).toBeVisible({ timeout: T.action });
  await expect(
    page.locator('.toast--error'),
    'WR-03: при успешном пустом списке показано уведомление об ошибке'
  ).toHaveCount(0);
  await expect(drawer).toHaveCount(0);

  // флаг листа не остался включённым: следующий файл открывается без листа
  await openSheetFromToolbar(page);
  await pickFileInSheet(page, fx().name);
  await expect(page.locator('.backups-toggle-btn')).toContainText(`(${n})`, { timeout: T.page });
  await expect(
    drawer,
    'WR-03: лист копий открылся сам после выбора файла: флаг листа остался от действия меню'
  ).toHaveCount(0);

  await expectNoHorizontalScroll(page, 'история бэкапов из меню');
  await guard.assertClean();
});

test('editor-mobile backups-collapsed vp:390', async ({ page, browser }) => {
  test.setTimeout(150_000 * RT.slow);
  let scratch: AltFixture | null = null;
  try {
    // комментарии и пустой объект — верный JSONC (Xray) и YAML (Mihomo): ядро запись принимает
    const mark = fixtureDir()!.ext === '.json' ? '//' : '#';
    const lines = ['{}'];
    for (let i = 1; i <= 12; i++) lines.push(`${mark} zz_e2e_line_${String(i).padStart(2, '0')}`);
    const previous = lines.join('\n') + '\n';
    const current = [...lines, `${mark} ${LONG_DIFF_LINE}`].join('\n') + '\n';

    const idx = await withPanel(browser, async (pg) => {
      const made = await createScratchFile(pg, []);
      scratch = made;
      const q = encodeURIComponent(made.path);
      expectOk(await rawRequest(pg, 'POST', `/api/config/save?path=${q}`, previous), 'save 1');
      // имя копии с точностью до секунды: вторая запись должна попасть в другую секунду
      await pg.waitForTimeout(1100);
      expectOk(await rawRequest(pg, 'POST', `/api/config/save?path=${q}`, current), 'save 2');

      const list = await rawRequest(pg, 'GET', `/api/config/backups?path=${q}`);
      expectOk(list, 'backups');
      const paths = asArray(list.text).filter((b): b is string => typeof b === 'string');
      expect(paths.length, `копий файла ${paths.length}`).toBeGreaterThanOrEqual(2);
      const texts: string[] = [];
      for (const b of paths) texts.push(await readOnDisk(pg, b));
      const found = texts.indexOf(previous);
      expect(
        found,
        'нет копии с исходным текстом — сравнение не даст свёрнутой группы'
      ).toBeGreaterThanOrEqual(0);
      return { k: paths.length, found };
    });
    const file = scratch as AltFixture | null;
    if (!file) throw new Error('временный файл не создан');

    const guard = attachConsoleGuard(page);
    await gotoEditor(page, VP_PHONE);
    await openSheetFromEmpty(page);
    await pickFileInSheet(page, file.name);

    const toggle = page.locator('.backups-toggle-btn');
    await expect(toggle).toContainText(`(${idx.k})`, { timeout: T.page });
    await toggle.click({ timeout: T.page });
    const drawer = page.locator('.editor-bottom-drawer');
    await expect(drawer).toBeVisible({ timeout: T.action });
    await waitAnimationsDone(drawer, 'лист Backups');
    await page.locator('.backup-select-btn').nth(idx.found).click();
    await expect(page.locator('.diff-viewer-container')).toBeVisible({ timeout: T.action });
    await expect(page.locator('.diff-body .spinner')).toHaveCount(0, { timeout: T.page });

    expect(
      await page.locator('.diff-line-collapsed').count(),
      'в сравнении нет свёрнутых строк — сценарий не проверяет подпись'
    ).toBeGreaterThanOrEqual(1);
    await expect(
      page.locator('.diff-line-added', { hasText: LONG_DIFF_LINE }),
      'длинная строка не попала в сравнение'
    ).toHaveCount(1, { timeout: T.action });

    // прямоугольник текста подписи берётся по содержимому строки: от разметки не зависит
    const m = await page.locator('.diff-body').evaluate((el) => {
      const row = el.querySelector<HTMLElement>('.diff-line-collapsed');
      if (!row) throw new Error('свёрнутая строка не найдена');
      const box = el.getBoundingClientRect();
      const left = box.left + el.clientLeft;
      const label = () => {
        const range = document.createRange();
        range.selectNodeContents(row);
        const r = range.getBoundingClientRect();
        return { left: r.left, right: r.right };
      };
      el.scrollLeft = 0;
      const atStart = label();
      el.scrollLeft = el.scrollWidth;
      const scrolled = el.scrollLeft;
      const atEnd = label();
      el.scrollLeft = 0;
      return {
        areaLeft: left,
        areaRight: left + el.clientWidth,
        scrollWidth: el.scrollWidth,
        clientWidth: el.clientWidth,
        scrolled,
        atStart,
        atEnd
      };
    });
    expect(
      m.scrollWidth,
      'длинная строка не дала горизонтальной прокрутки — сценарий не проверяет подпись'
    ).toBeGreaterThan(m.clientWidth + 1);
    expect(m.scrolled, '.diff-body не сдвигается по горизонтали').toBeGreaterThan(0);
    const eps = 0.5;
    const area = `видимая область ${m.areaLeft}–${m.areaRight}`;
    expect(
      m.atStart.left >= m.areaLeft - eps && m.atStart.right <= m.areaRight + eps,
      `WR-02: подпись свёрнутых строк за краем листа при scrollLeft = 0: подпись ${m.atStart.left}–${m.atStart.right}, ${area}`
    ).toBe(true);
    expect(
      m.atEnd.left >= m.areaLeft - eps && m.atEnd.right <= m.areaRight + eps,
      `WR-02: подпись свёрнутых строк уходит из вида при горизонтальной прокрутке: подпись ${m.atEnd.left}–${m.atEnd.right}, ${area}`
    ).toBe(true);

    await expectNoHorizontalScroll(page, 'сравнение со свёрнутыми строками');
    await page.locator('.drawer-close-btn').click();
    await expect(drawer).toHaveCount(0, { timeout: T.action });
    await guard.assertClean();
  } finally {
    const left = scratch as AltFixture | null;
    if (left) {
      await withPanel(browser, async (pg) => {
        await deleteFileWithBackups(pg, left.path);
      });
    }
  }
});
