// e2e-pages: config/files
// router-area: ui-shell
import { randomInt } from 'node:crypto';
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, withPanel } from './lib/nodes-api';

// Вкладка «Файлы» раздела «Конфигурация» на настоящей панели (фаза 149, CFG-03): встроенный
// Редактор открывает файл по строке списка, на 390 px список выезжает листом слева кнопкой
// «Файлы». Ответы сети не подменяются. Флаг слоя включается на время прогона и возвращается
// прежним. Стенд меняется только временным файлом с префиксом FIXTURE_PREFIX в каталоге Xray
// (пустой объект, ядро не перезапускается): файл создаётся через API панели до сценариев и
// удаляется вместе с копиями после, остатки прерванных прогонов убираются по префиксу.
// Реальные конфиги стенда спек не открывает.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

const XRAY_DIR = '/opt/etc/xray/configs';
// без слов стоп-списка XKeen и без префикса xcp- файлов слоя «Конфигурация»
const FIXTURE_PREFIX = 'zz_e2e_files_';
const VP_DESKTOP = { width: 1280, height: 800 };
const VP_PHONE = { width: 390, height: 844 };
const SHEET = '.file-tree-pane.overlay';
const SHEET_TOGGLE = '.files-sheet-toggle';

let fixtureName = '';

/** Имена файлов каталога по API панели (ответ — массив или конверт data). */
async function listFiles(page: Page, dir: string): Promise<{ name: string; path: string }[]> {
  const res = await page.evaluate(async (d) => {
    const r = await fetch(`/api/config/list?dir=${encodeURIComponent(d)}`);
    return { status: r.status, text: await r.text() };
  }, dir);
  expect(res.status, `GET /api/config/list ${dir}`).toBe(200);
  let payload: unknown = null;
  try {
    payload = JSON.parse(res.text);
  } catch {
    payload = null;
  }
  const arr = Array.isArray(payload)
    ? payload
    : payload && typeof payload === 'object' && Array.isArray((payload as { data?: unknown }).data)
      ? ((payload as { data: unknown[] }).data as unknown[])
      : [];
  return arr.filter(
    (f): f is { name: string; path: string } =>
      typeof f === 'object' &&
      f !== null &&
      typeof (f as { name?: unknown }).name === 'string' &&
      typeof (f as { path?: unknown }).path === 'string'
  );
}

/** Удаляет файл вместе с его резервными копиями; 404 не ошибка. */
async function deleteWithBackups(page: Page, path: string): Promise<void> {
  const q = encodeURIComponent(path);
  const backups = await page.evaluate(async (p) => {
    const r = await fetch(`/api/config/backups?path=${p}`);
    return { status: r.status, text: await r.text() };
  }, q);
  if (backups.status === 200) {
    let list: unknown = [];
    try {
      list = JSON.parse(backups.text);
    } catch {
      list = [];
    }
    for (const b of Array.isArray(list) ? list : []) {
      if (typeof b !== 'string') continue;
      const res = await api(page, 'POST', `/api/config/delete?path=${encodeURIComponent(b)}`);
      expect([200, 404], 'уборка: удаление копии').toContain(res.status);
    }
  }
  const res = await api(page, 'POST', `/api/config/delete?path=${q}`);
  expect([200, 404], 'уборка: удаление файла').toContain(res.status);
}

async function sweepFixtures(page: Page): Promise<void> {
  for (const f of await listFiles(page, XRAY_DIR)) {
    if (f.name.startsWith(FIXTURE_PREFIX)) await deleteWithBackups(page, f.path);
  }
}

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
  await withPanel(browser, async (page) => {
    await sweepFixtures(page);
    fixtureName = `${FIXTURE_PREFIX}${randomInt(100000, 1000000)}.json`;
    const q = encodeURIComponent(`${XRAY_DIR}/${fixtureName}`);
    const created = await api(page, 'POST', `/api/config/create?path=${q}`);
    expect(created.status, 'создание временного файла').toBe(200);
    const saved = await page.evaluate(async (p) => {
      const r = await fetch(`/api/config/save?path=${p}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': localStorage.getItem('csrf_token') ?? ''
        },
        body: '{}\n'
      });
      return r.status;
    }, q);
    expect(saved, 'запись временного файла').toBe(200);
  });
});

test.afterAll(async ({ browser }) => {
  await withPanel(browser, (page) => sweepFixtures(page));
  await layer.restore(browser);
});

/** Открывает вкладку «Файлы» и ждёт загруженный список (без скелетона). */
async function openFilesTab(page: Page, vp: { width: number; height: number }): Promise<Locator> {
  await page.setViewportSize(vp);
  await page.goto('/#/config/files', { timeout: T.page, waitUntil: 'domcontentloaded' });
  const tab = page.getByTestId('files-tab');
  await expect(tab).toBeVisible({ timeout: T.page });
  return tab;
}

/** Строка файла по имени внутри области (колонка или лист). */
function fileRow(scope: Locator, name: string): Locator {
  return scope.locator(`.file-row:has(.fr-name[title="${name}"])`).first();
}

/** Файл открыт во вкладке редактора с кодом. */
async function expectFileOpened(page: Page, name: string): Promise<void> {
  await expect(page.locator('.editor-tab', { hasText: name }).first()).toBeVisible({
    timeout: T.page
  });
  await expect(page.getByTestId('files-tab').locator('.cm-content').first()).toBeVisible({
    timeout: T.page
  });
}

test('config-files open vp:1280', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  const tab = await openFilesTab(page, VP_DESKTOP);

  // на ПК список — колонка рядом с редактором, заголовка страницы Редактора нет (D-01)
  await expect(tab.locator('.page-header')).toHaveCount(0);
  await expect(tab.locator('[data-testid="tab-constructor"]')).toHaveCount(0);
  const row = fileRow(tab, fixtureName);
  await expect(row).toBeVisible({ timeout: T.page });
  await row.click({ timeout: T.action });
  await expectFileOpened(page, fixtureName);

  // смена подвкладки раздела не размонтирует «Файлы»: открытая вкладка файла на месте
  await page.getByTestId('config-tab-nodes').click({ timeout: T.action });
  await expect(tab).toBeHidden({ timeout: T.action });
  await page.getByTestId('config-tab-files').click({ timeout: T.action });
  await expect(tab).toBeVisible({ timeout: T.action });
  await expectFileOpened(page, fixtureName);

  await guard.assertClean();
});

test('config-files owners vp:1280', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  const tab = await openFilesTab(page, VP_DESKTOP);
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
  await expect(tab.locator('.ft-error')).toHaveCount(0);
  await expect(fileRow(tab, fixtureName)).toBeVisible({ timeout: T.page });

  // у каждой строки один бейдж владельца из трёх (D-05); временный файл — пользователя
  const rows = tab.locator('.file-row');
  const count = await rows.count();
  expect(count, 'в списке нет строк').toBeGreaterThan(0);
  const paths: string[] = [];
  for (let i = 0; i < count; i++) {
    const row = rows.nth(i);
    const path = (await row.getAttribute('data-path')) ?? '';
    paths.push(path);
    const badge = row.getByTestId('files-owner-badge');
    await expect(badge, `строка ${path}: бейдж владельца`).toHaveCount(1);
    expect(['panel', 'xkeen', 'user'], `строка ${path}: владелец`).toContain(
      await badge.getAttribute('data-owner')
    );
  }
  await expect(fileRow(tab, fixtureName).getByTestId('files-owner-badge')).toHaveAttribute(
    'data-owner',
    'user'
  );

  // только три каталога: данные панели, логи и бинарники не выводятся (D-04)
  const roots = ['/opt/etc/xray/', '/opt/etc/mihomo/', '/opt/etc/xkeen/'];
  for (const path of paths) {
    expect(
      roots.some((r) => path.startsWith(r)),
      `путь вне трёх каталогов: ${path}`
    ).toBe(true);
    for (const banned of ['/opt/etc/xcp', '/opt/var/log', '/opt/sbin', '/opt/bin']) {
      expect(path.startsWith(banned), `в списке путь ${banned}`).toBe(false);
    }
  }
  // сервер тоже отказывает вкладке в каталоге данных панели
  const denied = await api(page, 'GET', '/api/config/list?dir=%2Fopt%2Fetc%2Fxcp&scope=files');
  expect(denied.status, 'scope=files для каталога панели').toBe(403);

  await guard.assertClean();
});

test('config-files sheet vp:390', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  const tab = await openFilesTab(page, VP_PHONE);

  // кнопка «Файлы» в шапке редактора открывает лист слева шириной не больше min(85vw, 320px)
  const toggle = tab.locator(SHEET_TOGGLE);
  await expect(toggle).toBeVisible({ timeout: T.page });
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await toggle.click({ timeout: T.action });
  const sheet = page.locator(SHEET);
  await expect(sheet).toBeVisible({ timeout: T.action });
  await expect
    .poll(
      () =>
        sheet.evaluate(
          (el) => el.getAnimations().length === 0 && Math.abs(el.getBoundingClientRect().left) <= 1
        ),
      { message: 'лист не встал на место', timeout: T.action }
    )
    .toBe(true);
  const box = await sheet.boundingBox();
  expect(box, 'лист: нет рамки').not.toBeNull();
  const limit = Math.min(0.85 * VP_PHONE.width, 320);
  expect(box!.width, `лист: ширина ${box!.width} при пределе ${limit}`).toBeLessThanOrEqual(
    limit + 1
  );

  // выбор файла закрывает лист и показывает файл во вкладке
  await fileRow(sheet, fixtureName).click({ timeout: T.action });
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expectFileOpened(page, fixtureName);
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');

  await guard.assertClean();
});
