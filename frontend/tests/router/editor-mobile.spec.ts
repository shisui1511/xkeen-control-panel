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

interface Fixture {
  dir: string;
  name: string;
  path: string;
  /** последняя записанная версия содержимого */
  current: string;
  /** предыдущая версия (её копия лежит в резервных копиях) */
  previous: string;
}

let fixture: Fixture | null = null;

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
  for (const f of stale) {
    const q = encodeURIComponent(f.path);
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
}

test.beforeAll(async ({ browser }) => {
  test.setTimeout(180_000 * RT.slow);
  const target = fixtureDir();
  if (!target) return;
  await withPanel(browser, async (page) => {
    await sweepFixtures(page, target.dir);
    fixture = await createFixture(page, target.dir, target.ext);
  });
});

test.afterAll(async ({ browser }) => {
  test.setTimeout(120_000 * RT.slow);
  const target = fixtureDir();
  if (!target) return;
  fixture = null;
  await withPanel(browser, (page) => sweepFixtures(page, target.dir));
});

function fx(): Fixture {
  if (!fixture) throw new Error('тестовый файл не создан');
  return fixture;
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

/** Элемент видим целиком внутри окна браузера. */
async function expectInside(page: Page, loc: Locator, what: string): Promise<void> {
  const b = await loc.boundingBox();
  expect(b, `${what}: нет рамки`).not.toBeNull();
  const box = b!;
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

/** Выбор тестового файла в открытой шторке: шторка закрывается, файл открыт во вкладке с кодом. */
async function pickFixtureInSheet(page: Page): Promise<void> {
  const name = fx().name;
  const row = page.locator(SHEET).locator(`.file-row:has(.fr-name[title="${name}"])`).first();
  await row.click({ timeout: T.action });
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(page.locator('.editor-tab', { hasText: name }).first()).toBeVisible({
    timeout: T.page
  });
  await expect(page.locator('.cm-content').first()).toBeVisible({ timeout: T.page });
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

  const box = (await sheet.boundingBox())!;
  expect(box, 'шторка: нет рамки').not.toBeNull();
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

  const bd = (await page.locator(BACKDROP).boundingBox())!;
  expect(bd, 'бэкдроп: нет рамки').not.toBeNull();
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
  const mainBefore = (await page.locator(MAIN_CARD).first().boundingBox())!;
  expect(mainBefore, 'карточка кода: нет рамки').not.toBeNull();

  // открытая шторка отражается в aria-expanded и не сжимает карточку кода
  await openSheetFromToolbar(page);
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  const mainOpen = (await page.locator(MAIN_CARD).first().boundingBox())!;
  expect(
    Math.abs(mainOpen.width - mainBefore.width),
    `ширина карточки кода изменилась: ${mainBefore.width} → ${mainOpen.width}`
  ).toBeLessThanOrEqual(1);
  await expectNoHorizontalScroll(page, 'шторка открыта поверх файла');

  // кнопка «Закрыть»: зона не меньше 44×44, фокус возвращается на кнопку шторки
  const close = page.locator(CLOSE_BTN);
  const cb = (await close.boundingBox())!;
  expect(cb, 'кнопка «Закрыть»: нет рамки').not.toBeNull();
  expect(cb.width, `кнопка «Закрыть»: ширина ${cb.width}`).toBeGreaterThanOrEqual(43.5);
  expect(cb.height, `кнопка «Закрыть»: высота ${cb.height}`).toBeGreaterThanOrEqual(43.5);
  await close.click();
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await expect(toggle).toBeFocused();

  // клавиатура: Enter открывает, Escape закрывает, фокус снова на кнопке
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator(SHEET)).toBeVisible({ timeout: T.action });
  await waitSheetSettled(page);
  await page.keyboard.press('Escape');
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(toggle).toBeFocused();

  // нажатие мышью по фону правее панели шириной 320 px
  await openSheetFromToolbar(page);
  await page.mouse.click(VP_PHONE.width - 16, VP_PHONE.height / 2);
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });

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
  await expect(page.locator(SHEET)).toBeVisible();

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
    await openSheetFromToolbar(page);

    const cdp = await page.context().newCDPSession(page);
    const tabs = page.locator('.editor-tab');
    const tabsBefore = await tabs.count();

    // вертикальный жест (dx +10, dy -300) не закрывает шторку и не открывает файл
    await touchDrag(cdp, { x: 150, y: 600 }, { x: 160, y: 300 });
    await expectSheetStays(page, 'вертикальный жест');
    expect(await tabs.count(), 'вертикальный жест открыл файл').toBe(tabsBefore);

    // диагональный жест (dx -70, dy +80: |dx| < 1.5·|dy|) шторку не закрывает
    await touchDrag(cdp, { x: 200, y: 300 }, { x: 130, y: 380 });
    await expectSheetStays(page, 'диагональный жест');
    expect(await tabs.count(), 'диагональный жест открыл файл').toBe(tabsBefore);

    // горизонтальный свайп влево (dx -120, dy 5) закрывает
    await touchDrag(cdp, { x: 250, y: 400 }, { x: 130, y: 405 });
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });

    // касание по фону правее панели закрывает
    await openSheetFromToolbar(page);
    await page.touchscreen.tap(VP_PHONE.width - 16, VP_PHONE.height / 2);
    await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });

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

  const w0 = (await pane.boundingBox())!.width;
  expect(w0, `ширина колонки ${w0}`).toBeGreaterThanOrEqual(160);
  expect(w0, `ширина колонки ${w0}`).toBeLessThanOrEqual(450);

  // сплиттер тянет колонку: +80 px, но не шире 450 и не шире 42 % рабочей области
  const sb = (await splitter.boundingBox())!;
  expect(sb, 'сплиттер: нет рамки').not.toBeNull();
  const cx = sb.x + sb.width / 2;
  const cy = sb.y + sb.height / 2;
  await page.mouse.move(cx, cy);
  await page.mouse.down();
  await page.mouse.move(cx + 80, cy, { steps: 5 });
  await page.mouse.up();
  const cap = await page.locator('.editor-workspace').evaluate((el) => el.clientWidth * 0.42);
  const want = Math.min(w0 + 80, 450, cap);
  await expect
    .poll(async () => Math.abs((await pane.boundingBox())!.width - want), {
      message: `ширина колонки после перетаскивания, ожидалась ${want}`,
      timeout: T.action
    })
    .toBeLessThanOrEqual(2);
  const w1 = (await pane.boundingBox())!.width;
  await guard.assertClean();

  // ширина сохраняется: вторая страница того же контекста делит localStorage
  const page2 = await page.context().newPage();
  const guard2 = attachConsoleGuard(page2);
  await gotoEditor(page2, VP_DESKTOP);
  const pane2 = page2.locator('.file-tree-pane').first();
  await expect(pane2).toBeVisible({ timeout: T.page });
  expect(
    Math.abs((await pane2.boundingBox())!.width - w1),
    `ширина колонки не сохранилась: было ${w1}`
  ).toBeLessThanOrEqual(1);

  // файл открывается кликом по строке колонки, без шторки; колонка рядом с кодом, не поверх
  const name = fx().name;
  await pane2.locator(`.file-row:has(.fr-name[title="${name}"])`).first().click();
  await expect(page2.locator('.editor-tab', { hasText: name }).first()).toBeVisible({
    timeout: T.page
  });
  await expect(page2.locator('.cm-content').first()).toBeVisible({ timeout: T.page });
  const paneBox = (await pane2.boundingBox())!;
  const cardBox = (await page2.locator(MAIN_CARD).first().boundingBox())!;
  expect(
    paneBox.x + paneBox.width,
    `колонка заходит под карточку кода: правый край ${paneBox.x + paneBox.width}, левый край карточки ${cardBox.x}`
  ).toBeLessThanOrEqual(cardBox.x + 1);
  await expect(page2.locator(SHEET)).toHaveCount(0);

  // кнопка тулбара скрывает и показывает колонку без оверлея
  const toggle = page2.locator(TOGGLE).first();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await toggle.click();
  await expect(page2.locator('.file-tree-pane')).toHaveCount(0, { timeout: T.action });
  await expect(page2.locator(BACKDROP)).toHaveCount(0);
  await toggle.click();
  await expect(page2.locator('.file-tree-pane').first()).toBeVisible({ timeout: T.action });
  await expect(page2.locator('.file-tree-pane').first()).not.toHaveClass(/\boverlay\b/);

  await guard2.assertClean();
  await page2.close();
});
