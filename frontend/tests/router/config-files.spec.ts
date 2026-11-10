// e2e-pages: config/files
// router-area: ui-shell
import { execFile } from 'node:child_process';
import { randomInt } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { test, expect, type Locator, type Page, type Request } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { CORE_TAG, FULL_TAG, TIER_FULL_ANNOTATION } from './lib/known';
import { api, layerFlag, resetDraft, withPanel } from './lib/nodes-api';

// Вкладка «Файлы» раздела «Конфигурация» на настоящей панели (фаза 149, CFG-03): встроенный
// Редактор открывает файл по строке списка, на 390 px список выезжает листом слева кнопкой
// «Файлы». Ответы сети не подменяются. Флаг слоя включается на время прогона и возвращается
// прежним. Стенд меняется только временным файлом с префиксом FIXTURE_PREFIX в каталоге Xray
// (пустой объект, ядро не перезапускается): файл создаётся через API панели до сценариев и
// удаляется вместе с копиями после, остатки прерванных прогонов убираются по префиксу.
// Реальные конфиги стенда спек не открывает, кроме сценария истории файла панели (ниже):
// он применяет секцию «Лог» Xray и возвращает её владельца, форму и файлы.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

const XRAY_DIR = '/opt/etc/xray/configs';
// без слов стоп-списка XKeen и без префикса xcp- файлов слоя «Конфигурация»
const FIXTURE_PREFIX = 'zz_e2e_files_';
const VP_DESKTOP = { width: 1280, height: 800 };
const VP_PHONE = { width: 390, height: 844 };
const SHEET = '.file-tree-pane.overlay';
const SHEET_TOGGLE = '.files-sheet-toggle';
const SHOW_FILES = '.editor-empty-actions .btn-secondary';

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

test.afterEach(async () => {
  // Прерванный сценарий истории: копии, переименованные или подменённые FIFO, возвращаются
  if (copiesDirty) await restoreCopies(LOG_KEY);
});

test.afterAll(async ({ browser }) => {
  // Прерванный сценарий истории: секция «Лог» и её файлы — до возврата флага слоя
  const before = logBefore;
  if (before !== null) {
    await withPanel(browser, (page) => restoreLog(page, before));
    logBefore = null;
  }
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

/**
 * Обход фокуса внутри области по стандартным признакам HTML, без классов и атрибутов панели:
 * видимые элементы с tabIndex ≥ 0, не disabled, не внутри inert или aria-hidden. `first`/`last`
 * фокусируют край обхода и возвращают число элементов; `where` — где сейчас фокус.
 */
async function focusEdge(
  scope: Locator,
  action: 'first' | 'last' | 'where'
): Promise<{ count: number; at: 'first' | 'last' | 'inside' | 'outside' }> {
  return scope.evaluate((root, act) => {
    const sel =
      'a[href], button, input:not([type="hidden"]), select, textarea, summary, [contenteditable="true"], [tabindex]';
    const list = Array.from(root.querySelectorAll<HTMLElement>(sel)).filter(
      (el) =>
        el.tabIndex >= 0 &&
        !(el as HTMLButtonElement).disabled &&
        !el.closest('[inert], [aria-hidden="true"]') &&
        el.getClientRects().length > 0 &&
        getComputedStyle(el).visibility !== 'hidden'
    );
    if (act === 'first') list[0]?.focus();
    if (act === 'last') list[list.length - 1]?.focus();
    const active = document.activeElement;
    const at =
      active === list[0]
        ? 'first'
        : active === list[list.length - 1]
          ? 'last'
          : active && root.contains(active)
            ? 'inside'
            : 'outside';
    return { count: list.length, at };
  }, action);
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
  const sheet = page.locator(SHEET);
  const settled = async (): Promise<void> => {
    await expect
      .poll(
        () =>
          sheet.evaluate(
            (el) =>
              el.getAnimations().length === 0 && Math.abs(el.getBoundingClientRect().left) <= 1
          ),
        { message: 'лист не встал на место', timeout: T.action }
      )
      .toBe(true);
  };
  const focusInSheet = (): Promise<boolean> =>
    sheet.evaluate((el) => el.contains(document.activeElement));
  const showFiles = tab.locator(SHOW_FILES).first();
  await expect(showFiles).toBeVisible({ timeout: T.page });
  await showFiles.click();
  await expect(sheet).toBeVisible({ timeout: T.action });
  await settled();
  await expect
    .poll(focusInSheet, {
      message: 'G149-04 initial focus из пустого состояния',
      timeout: T.action
    })
    .toBe(true);

  const newFile = sheet.locator('.btn-new-file');
  await newFile.click({ timeout: T.action });
  const modal = page.locator('.modal-container').filter({ has: page.locator('#new-file-name') });
  const close = modal.locator('.modal-close-btn');
  await expect(close).toBeFocused({ timeout: T.action });
  for (const key of ['Tab', 'Shift+Tab']) {
    const edge = key === 'Tab' ? 'first' : 'last';
    await modal.locator('.modal-title').click();
    await expect(modal, 'G149-10: клик по заголовку фокусирует контейнер').toBeFocused();
    await page.keyboard.press(key);
    expect((await focusEdge(modal, 'where')).at, `G149-10: ${key} с контейнера`).toBe(edge);

    await close.focus();
    await close.evaluate((button) => (button as HTMLElement).blur());
    expect(await page.evaluate(() => document.activeElement === document.body)).toBe(true);
    await page.keyboard.press(key);
    expect((await focusEdge(modal, 'where')).at, `G149-10: ${key} с body`).toBe(edge);
  }
  await page.keyboard.press('Escape');
  await expect(modal).toHaveCount(0, { timeout: T.action });
  await expect(sheet).toBeVisible();
  await expect(newFile, 'G149-10: первый Escape возвращает фокус в лист').toBeFocused();

  await page.keyboard.press('Escape');
  await expect(sheet).toHaveCount(0, { timeout: T.action });
  await expect(
    showFiles,
    'WR-05: фокус после закрытия листа, открытого из пустого состояния'
  ).toBeFocused();
  await expect(showFiles).toHaveAttribute('aria-expanded', 'false');

  await toggle.click({ timeout: T.action });
  await expect(sheet).toBeVisible({ timeout: T.action });
  await settled();
  const box = await sheet.boundingBox();
  expect(box, 'лист: нет рамки').not.toBeNull();
  const limit = Math.min(0.85 * VP_PHONE.width, 320);
  expect(box!.width, `лист: ширина ${box!.width} при пределе ${limit}`).toBeLessThanOrEqual(
    limit + 1
  );

  // G149-04: лист — модальный диалог. Обычный клик переносит фокус внутрь листа
  await expect
    .poll(focusInSheet, { message: 'G149-04 initial focus', timeout: T.action })
    .toBe(true);
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');

  // Tab с последнего элемента и Shift+Tab с первого не выводят фокус на страницу под листом
  const edges = await focusEdge(sheet, 'last');
  expect(edges.count, 'G149-04: в листе меньше двух доступных элементов').toBeGreaterThan(1);
  expect(edges.at, 'G149-04: последний элемент листа не принял фокус').toBe('last');
  await page.keyboard.press('Tab');
  expect((await focusEdge(sheet, 'where')).at, 'G149-04: Tab с последнего элемента').toBe('first');
  expect((await focusEdge(sheet, 'first')).at, 'G149-04: первый элемент листа').toBe('first');
  await page.keyboard.press('Shift+Tab');
  expect((await focusEdge(sheet, 'where')).at, 'G149-04: Shift+Tab с первого элемента').toBe(
    'last'
  );

  // фон не удерживает фокус: фокус, поставленный на кнопку под листом, возвращается в лист
  await toggle.evaluate((el) => (el as HTMLElement).focus());
  await expect
    .poll(focusInSheet, {
      message: 'G149-04: фокус остался на кнопке под листом',
      timeout: T.action
    })
    .toBe(true);

  // Escape закрывает лист и возвращает фокус на кнопку «Файлы»
  await page.keyboard.press('Escape');
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await expect(toggle).toBeFocused();

  // повторное открытие с клавиатуры: Enter по кнопке, фокус снова внутри листа
  await page.keyboard.press('Enter');
  await expect(sheet).toBeVisible({ timeout: T.action });
  await settled();
  await expect
    .poll(focusInSheet, { message: 'G149-04 initial focus после Enter', timeout: T.action })
    .toBe(true);
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');

  // выбор файла закрывает лист и показывает файл во вкладке
  await fileRow(sheet, fixtureName).click({ timeout: T.action });
  await expect(page.locator(SHEET)).toHaveCount(0, { timeout: T.action });
  await expectFileOpened(page, fixtureName);
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');

  await guard.assertClean();
});

test('config-files owner-banners vp:1280', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  const tab = await openFilesTab(page, VP_DESKTOP);
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });

  // 1. Открыть файл панели (data-owner="panel")
  const panelRow = tab.locator('.file-row:has([data-owner="panel"])').first();
  if ((await panelRow.count()) > 0) {
    await panelRow.click({ timeout: T.action });
    await expect(tab.getByTestId('managed-file-banner')).toBeVisible({ timeout: T.page });

    // Кнопка «Сохранить» неактивна
    const saveBtn = tab.locator('button:has-text("Сохранить")').first();
    if ((await saveBtn.count()) > 0) {
      await expect(saveBtn).toBeDisabled();
    }

    // В листе копий кнопка «Восстановить» неактивна (регрессия editor-restore-ignores-managed-readonly)
    const restoreBtn = tab.locator('.restore-inline-btn, .diff-restore-btn').first();
    if ((await restoreBtn.count()) > 0) {
      await expect(restoreBtn).toBeDisabled();
    }
  }

  // 2. Открыть файл пользователя или XKeen (наш fixtureName — user)
  const userRow = fileRow(tab, fixtureName);
  await expect(userRow).toBeVisible({ timeout: T.page });
  await userRow.click({ timeout: T.action });
  await expect(tab.getByTestId('files-immediate-note')).toBeVisible({ timeout: T.page });
  // пометка «мимо черновика» одна — у «Сохранить» (в шапке или строкой под ней на телефоне);
  // строка files-immediate-note говорит то же текстом и бейдж не повторяет
  await expect(tab.locator('[data-testid="immediate-mark"]:visible').first()).toBeVisible({
    timeout: T.page
  });
  await expect(tab.getByTestId('files-immediate-note').getByTestId('immediate-mark')).toHaveCount(
    0
  );

  await guard.assertClean();
});

// --- Последние применения файла панели (D-06, G149-02) ---
// Подготовка обязательна: секция «Лог» Xray берётся и применяется настоящими маршрутами три
// раза подряд (loglevel warning, error, none при access=none), поэтому у файла панели
// 99_xcp-log.json появляются минимум две копии с разным содержимым: A (warning) и B (error).
// Сбой и задержка чтения вносятся на самом роутере только в копию набора, созданного этим
// сценарием: копия переименовывается (настоящий 404) или подменяется именованным FIFO
// (чтение блокируется, пока тест не отдаст исходные байты). Сеть и ответы не подменяются.
// В finally копии, секция и файлы возвращаются; остатки прерванного прогона убирает afterEach.

const run = promisify(execFile);
const REPO_ROOT = fileURLToPath(new URL('../../../', import.meta.url));
const APPLIES_PATH = '/api/configlayer/files/applies';
const LOG_PANEL_NAME = '99_xcp-log.json';
const LOG_PANEL_PATH = `${XRAY_DIR}/${LOG_PANEL_NAME}`;
const LOG_KEY = `xray:${LOG_PANEL_NAME}`;
// <data_dir>/backup/config-layer: наборы apply-<число>, копия файла — <набор>/<ядро>/<путь>
const LAYER_BACKUP_ROOT = '/opt/etc/xcp/backup/config-layer';
const APPLY_ID = /^apply-[0-9]{1,20}$/;
const COPY_REL = /^[A-Za-z0-9._-]+$/;
const ORIG_SUFFIX = '.zz_e2e_orig';
const RETRY_NAME = /^\s*(Повторить|Retry)\s*$/;

/** Наборы копий, созданные подготовкой сценария: только их копии тест трогает на диске. */
const createdSets = new Set<string>();
/** Наборы, копия которых сейчас переименована или подменена FIFO. */
const touchedSets = new Set<string>();
/** Копии могли остаться изменёнными (тест прерван): afterEach возвращает их. */
let copiesDirty = false;

interface LogSection {
  installed: boolean;
  owner: string;
  blocked: string;
  files: string[];
  form: Record<string, unknown>;
  advanced: string;
}

interface LogBefore {
  owner: string;
  form: Record<string, unknown>;
  advanced: string;
  files: Record<string, string | null>;
}

/** Секция «Лог» до сценария: её владелец, форма и файлы возвращаются после. */
let logBefore: LogBefore | null = null;

/**
 * Команда на стенде arm64 через scripts/router/lib.sh из корня репозитория: алиас и доступы
 * берутся только из локального файла целей. Возвращает код выхода; сбой подготовки — ошибка.
 * Вывод ssh (в нём может быть алиас) в отчёт не попадает.
 */
async function standSh(remote: string): Promise<number> {
  const code = await run(
    'sh',
    [
      '-c',
      '. scripts/router/lib.sh || exit 90; rt_load_targets || exit 91; rt_ssh arm64 "$1"',
      'sh',
      remote
    ],
    { cwd: REPO_ROOT, env: { ...process.env, ROUTERS: 'arm64' }, timeout: 60_000 }
  ).then(
    () => 0 as unknown,
    (err: unknown) => (err as { code?: unknown } | null)?.code
  );
  if (code === 3) throw new Error('стенд arm64: нет связи по ssh');
  if (code === 90 || code === 91) throw new Error('стенд arm64: файл целей не прочитан');
  if (typeof code !== 'number') {
    throw new Error('стенд arm64: команда не выполнена (таймаут или запуск sh)');
  }
  return code;
}

/** Путь копии внутри набора: ядро и путь берутся из ключа манифеста и проверяются. */
function copyRel(key: string): string {
  const [kernel, rel] = key.split(':');
  if (kernel !== 'xray' || rel === undefined || !COPY_REL.test(rel)) {
    throw new Error('G149-02: ключ файла вне шаблона сценария');
  }
  return `${kernel}/${rel}`;
}

// Отдать читателю FIFO исходные байты: открытие на чтение-запись не блокируется, читатель
// получает данные и конец файла после закрытия; без читателя данные просто пропадают.
const UNBLOCK = 'exec 3<>"$P"; cat "$O" >&3; exec 3>&-; rm -f "$P"';

/**
 * Команда над копией файла key в наборе id. id — из наборов сценария, meta.json набора
 * обязан содержать запись этого ключа (как у сервера, путь копии — из записи метаданных).
 */
function copyCmd(id: string, key: string, body: string): string {
  if (!APPLY_ID.test(id) || !createdSets.has(id)) {
    throw new Error('G149-02: набор копий не создан сценарием');
  }
  return (
    `cd ${LAYER_BACKUP_ROOT}/${id} || exit 11; test -f meta.json || exit 12; ` +
    `grep -qF '"key": "${key}"' meta.json || exit 13; ` +
    `P=${copyRel(key)}; O="$P${ORIG_SUFFIX}"; ${body}`
  );
}

/** Убирает копию: чтение содержимого этого набора получает настоящий 404. */
async function hideCopy(id: string, key: string): Promise<void> {
  copiesDirty = true;
  touchedSets.add(id);
  const code = await standSh(
    copyCmd(
      id,
      key,
      'test -f "$P" && test ! -p "$P" || exit 14; test ! -e "$O" || exit 15; mv "$P" "$O" || exit 16'
    )
  );
  if (code !== 0) throw new Error(`G149-02 подготовка: копия не убрана (код ${code})`);
}

/** Подменяет копию именованным FIFO: чтение содержимого блокируется до releaseCopy. */
async function blockCopy(id: string, key: string): Promise<void> {
  copiesDirty = true;
  touchedSets.add(id);
  const code = await standSh(
    copyCmd(
      id,
      key,
      'test -f "$P" && test ! -p "$P" || exit 14; test ! -e "$O" || exit 15; ' +
        'mv "$P" "$O" || exit 16; mkfifo -m 600 "$P" || { mv "$O" "$P"; exit 17; }'
    )
  );
  if (code !== 0) throw new Error(`G149-02 подготовка: чтение копии не задержано (код ${code})`);
}

/** Возвращает исходную копию: FIFO отдаёт исходные байты и удаляется, оригинал на месте. */
async function releaseCopy(id: string, key: string): Promise<void> {
  const code = await standSh(
    copyCmd(
      id,
      key,
      `test -e "$O" || exit 0; if test -p "$P"; then ${UNBLOCK}; fi; ` +
        'test ! -e "$P" || exit 21; mv "$O" "$P" || exit 22; test -f "$P" && test ! -p "$P" || exit 23'
    )
  );
  if (code !== 0) throw new Error(`G149-02 уборка: копия не возвращена (код ${code})`);
  touchedSets.delete(id);
}

/** Уборка после прерывания: все копии файла панели «Лога» с меткой сценария возвращаются. */
async function sweepCopies(): Promise<void> {
  const code = await standSh(
    `for O in ${LAYER_BACKUP_ROOT}/apply-*/xray/${LOG_PANEL_NAME}${ORIG_SUFFIX}; do ` +
      `test -e "$O" || continue; P="\${O%${ORIG_SUFFIX}}"; ` +
      `if test -p "$P"; then ${UNBLOCK}; fi; test -e "$P" || mv "$O" "$P" || exit 31; done; exit 0`
  );
  if (code !== 0) throw new Error(`G149-02 уборка: копии не возвращены (код ${code})`);
}

async function restoreCopies(key: string): Promise<void> {
  let failure: unknown;
  for (const id of [...touchedSets]) {
    try {
      await releaseCopy(id, key);
    } catch (err) {
      failure ??= err;
    }
  }
  try {
    await sweepCopies();
  } catch (err) {
    failure ??= err;
  }
  if (failure !== undefined) throw failure;
  copiesDirty = false;
}

/** Запрос к API панели со строковым телом (как у Редактора). */
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

function configPath(name: string): string {
  return name.startsWith('/') ? name : `${XRAY_DIR}/${name}`;
}

async function readConfig(page: Page, path: string): Promise<string | null> {
  const res = await rawRequest(page, 'GET', `/api/config/read?path=${encodeURIComponent(path)}`);
  if (res.status === 404) return null;
  expect(res.status, `чтение ${path}`).toBe(200);
  return res.text;
}

async function putConfig(page: Page, path: string, content: string | null): Promise<void> {
  const q = encodeURIComponent(path);
  if (content === null) {
    const res = await rawRequest(page, 'POST', `/api/config/delete?path=${q}`);
    expect([200, 404], `удаление ${path}`).toContain(res.status);
    return;
  }
  if ((await readConfig(page, path)) === null) {
    const created = await rawRequest(page, 'POST', `/api/config/create?path=${q}`);
    expect(created.status, `создание ${path}`).toBe(200);
  }
  const saved = await rawRequest(page, 'POST', `/api/config/save?path=${q}`, content);
  expect(saved.status, `запись ${path}`).toBe(200);
}

async function logSection(page: Page): Promise<LogSection> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=xray');
  expect(view.status, 'GET /api/configlayer/cores?kernel=xray').toBe(200);
  const sections = (view.data?.sections ?? []) as Record<string, unknown>[];
  const s = sections.find((x) => x.id === 'log') ?? {};
  return {
    installed: view.data?.installed === true,
    owner: String(s.owner ?? ''),
    blocked: String(s.blocked ?? ''),
    files: (s.files ?? []) as string[],
    form: (s.form ?? {}) as Record<string, unknown>,
    advanced: String(s.advanced ?? '')
  };
}

async function layerProbe(
  page: Page
): Promise<{ revision: number; running: boolean; changes: number }> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const apply = (st.data?.apply ?? {}) as { running?: boolean };
  return {
    revision: Number(st.data?.draft_revision ?? 0),
    running: apply.running === true,
    changes: Number(st.data?.draft_changes ?? 0)
  };
}

/** Применяет черновик настоящим маршрутом и ждёт конца: применение не идёт, изменений нет. */
async function applyAndWait(page: Page): Promise<void> {
  const start = await api(page, 'POST', '/api/configlayer/apply', {});
  expect(start.status, 'запуск применения').toBe(200);
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

/** Записывает форму и «Дополнительно» секции «Лог» в черновик и применяет, если есть правка. */
async function saveLogAndApply(
  page: Page,
  form: Record<string, unknown>,
  advanced: string
): Promise<void> {
  const res = await api(page, 'POST', '/api/configlayer/cores/save', {
    revision: (await layerProbe(page)).revision,
    kernel: 'xray',
    section: 'log',
    form,
    advanced
  });
  expect(res.status, 'запись секции «Лог» в черновик').toBe(200);
  if ((await layerProbe(page)).changes > 0) await applyAndWait(page);
}

/** Возвращает секцию «Лог», её файлы и пустой черновик к состоянию до сценария. */
async function restoreLog(page: Page, before: LogBefore): Promise<void> {
  await resetDraft(page);
  const now = await logSection(page);
  if (before.owner === 'panel') {
    await saveLogAndApply(page, before.form, before.advanced);
  } else if (now.owner === 'panel') {
    const res = await api(page, 'POST', '/api/configlayer/cores/release', {
      revision: (await layerProbe(page)).revision,
      kernel: 'xray',
      section: 'log'
    });
    expect(res.status, 'возврат секции «Лог» через API').toBe(200);
    await applyAndWait(page);
  }
  // чужие файлы секции — побайтно как до сценария; файл панели пересобран применением
  for (const [path, content] of Object.entries(before.files)) {
    if (path === LOG_PANEL_PATH) continue;
    if ((await readConfig(page, path)) !== content) await putConfig(page, path, content);
  }
}

/** Ключ файла панели «Лога» в манифесте слоя. */
async function logPanelKey(page: Page): Promise<string> {
  const state = await api(page, 'GET', '/api/configlayer/state');
  expect(state.status, 'GET /api/configlayer/state').toBe(200);
  const files = (Array.isArray(state.data?.files) ? state.data.files : []) as {
    key?: unknown;
    path?: unknown;
  }[];
  const entry = files.find((f) => f?.path === LOG_PANEL_PATH);
  expect(entry?.key, 'G149-02 подготовка: файл панели «Лога» в манифесте').toBe(LOG_KEY);
  return LOG_KEY;
}

/** Идентификаторы копий файла по API, в порядке списка; файла в манифесте нет — пусто. */
async function applyIds(page: Page, key: string): Promise<string[]> {
  const res = await api(page, 'GET', `${APPLIES_PATH}?key=${encodeURIComponent(key)}`);
  if (res.status === 404) return [];
  expect(res.status, 'GET files/applies').toBe(200);
  return ((res.data?.applies ?? []) as { id?: unknown }[]).map((a) => String(a.id ?? ''));
}

async function applyContent(page: Page, key: string, id: string): Promise<string> {
  const res = await api(
    page,
    'GET',
    `${APPLIES_PATH}?key=${encodeURIComponent(key)}&id=${encodeURIComponent(id)}`
  );
  expect(res.status, `содержимое копии ${id}`).toBe(200);
  return String(res.data?.content ?? '');
}

/** key и id запроса истории файла; запрос не к истории — null. */
function appliesQuery(url: string): { key: string | null; id: string | null } | null {
  try {
    const u = new URL(url);
    if (u.pathname !== APPLIES_PATH) return null;
    return { key: u.searchParams.get('key'), id: u.searchParams.get('id') };
  } catch {
    return null;
  }
}

function isListReq(req: Request, key: string): boolean {
  const q = appliesQuery(req.url());
  return req.method() === 'GET' && q !== null && q.key === key && q.id === null;
}

function isContentReq(req: Request, key: string, id: string): boolean {
  const q = appliesQuery(req.url());
  return req.method() === 'GET' && q !== null && q.key === key && q.id === id;
}

function lineSet(text: string): Set<string> {
  return new Set(
    text
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l !== '')
  );
}

/** Строка сравнения без знака «-»/«+» и отступов. */
function diffLine(text: string): string {
  return text.replace(/^[-+] /, '').trim();
}

test(
  'config-files applies vp:1280',
  {
    tag: [FULL_TAG, CORE_TAG],
    annotation: {
      type: TIER_FULL_ANNOTATION,
      description: 'внесение сбоя: ошибка и задержка чтения копии на диске'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 12);
    await page.setViewportSize(VP_DESKTOP);
    await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });

    // Подготовка: остатки прерванного прогона, пустой черновик, секция «Лог» берётся
    await sweepCopies();
    const start = await logSection(page);
    expect(start.installed, 'G149-02 подготовка: Xray установлен').toBe(true);
    expect(start.blocked, 'G149-02 подготовка: секция «Лог» берётся').toBe('');
    expect(start.owner, 'G149-02 подготовка: секция «Лог» не в черновике').not.toBe('pending');
    // чужой черновик не применяется вместе с правками сценария
    expect((await layerProbe(page)).changes, 'G149-02 подготовка: черновик пуст').toBe(0);
    const files: Record<string, string | null> = {};
    for (const name of start.files)
      files[configPath(name)] = await readConfig(page, configPath(name));
    logBefore = { owner: start.owner, form: start.form, advanced: start.advanced, files };

    let primary: unknown;
    try {
      // наборы с копией этого файла до подготовки (в том числе от прежнего взятия секции)
      const idsBefore = new Set(await applyIds(page, LOG_KEY));
      if (start.owner !== 'panel') {
        const took = await api(page, 'POST', '/api/configlayer/cores/take', {
          revision: (await layerProbe(page)).revision,
          kernel: 'xray',
          sections: ['log'],
          ack_comments: true
        });
        expect(took.status, 'G149-02 подготовка: взятие секции «Лог»').toBe(200);
      }
      for (const loglevel of ['warning', 'error', 'none']) {
        const s = await logSection(page);
        await saveLogAndApply(page, { ...s.form, loglevel, access: 'none' }, s.advanced);
      }
      const key = await logPanelKey(page);
      // применения сценария — самые новые: две первые копии списка созданы его вторым и
      // третьим применением (до них файл уже существовал); B — до последнего, A — до предпоследнего
      const ids = await applyIds(page, key);
      const idB = ids[0] ?? '';
      const idA = ids[1] ?? '';
      for (const id of [idB, idA]) {
        expect(
          APPLY_ID.test(id) && !idsBefore.has(id),
          'G149-02 подготовка: применения создали две новые копии файла панели'
        ).toBe(true);
        createdSets.add(id);
      }
      const contentA = await applyContent(page, key, idA);
      const contentB = await applyContent(page, key, idB);
      expect(contentA, 'G149-02 подготовка: копии A и B различаются').not.toBe(contentB);
      const diffRes = await api(
        page,
        'GET',
        `/api/configlayer/files/diff?key=${encodeURIComponent(key)}`
      );
      expect(diffRes.status, 'текущий файл панели').toBe(200);
      const linesA = lineSet(contentA);
      const linesB = lineSet(contentB);
      const linesNow = lineSet(String(diffRes.data?.actual ?? ''));
      const markB = [...linesB].filter((l) => !linesA.has(l) && !linesNow.has(l));
      const markA = [...linesA].filter((l) => !linesB.has(l) && !linesNow.has(l));
      expect(markB.length, 'G149-02 подготовка: у B есть свои строки').toBeGreaterThan(0);
      expect(markA.length, 'G149-02 подготовка: у A есть свои строки').toBeGreaterThan(0);
      const level = (marks: string[], value: string) =>
        marks.some((l) => l.includes('"loglevel"') && l.includes(`"${value}"`));
      expect(level(markB, 'error'), 'G149-02 подготовка: B — версия с loglevel error').toBe(true);
      expect(level(markA, 'warning'), 'G149-02 подготовка: A — версия с loglevel warning').toBe(
        true
      );

      const guard = attachConsoleGuard(page);
      const tab = await openFilesTab(page, VP_DESKTOP);
      await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });

      // Файл панели: баннер управления, история по кнопке строки состояния
      const panelRow = fileRow(tab, LOG_PANEL_NAME);
      await expect(panelRow).toBeVisible({ timeout: T.page });
      await panelRow.click({ timeout: T.action });
      await expect(tab.getByTestId('managed-file-banner')).toBeVisible({ timeout: T.page });
      const listRes = page.waitForResponse((r) => isListReq(r.request(), key), {
        timeout: T.page
      });
      await tab.locator('.backups-toggle-btn').first().click({ timeout: T.action });
      const listed = await listRes;
      expect(listed.status(), 'GET files/applies для файла панели').toBe(200);
      const listBody = (await listed.json()) as { data?: { applies?: { id?: unknown }[] } };
      const uiIds = (listBody.data?.applies ?? []).map((a) => String(a.id ?? ''));
      const iA = uiIds.indexOf(idA);
      const iB = uiIds.indexOf(idB);
      expect(iA >= 0 && iA < 5 && iB >= 0 && iB < 5, 'A и B среди первых пяти строк').toBe(true);

      const list = tab.getByTestId('files-applies-list');
      await expect(list).toBeVisible({ timeout: T.action });
      await expect(list.locator('.apply-row')).toHaveCount(Math.min(5, uiIds.length), {
        timeout: T.action
      });
      // отката к применению нет (D-06)
      await expect(list.locator('.restore-inline-btn, .diff-restore-btn')).toHaveCount(0);
      const rowA = list.locator('.apply-row').nth(iA);
      const rowB = list.locator('.apply-row').nth(iB);
      const expanded = list.locator('.apply-row-btn[aria-expanded="true"]');

      /** Нажимает строку и возвращает фактический запрос содержимого этого id. */
      const select = async (row: Locator, id: string): Promise<Request> => {
        const sent = page.waitForRequest((r) => isContentReq(r, key, id), { timeout: T.action });
        await row.locator('.apply-row-btn').click({ timeout: T.action });
        return sent;
      };
      /** Ждёт полного ответа на запрос: статус и тело дошли до страницы. */
      const answered = async (req: Request, status: number, what: string): Promise<void> => {
        // response() отдаёт и уже пришедший ответ; ожидание ограничено
        const res = await Promise.race([
          req.response(),
          new Promise<null>((r) => setTimeout(() => r(null), T.action))
        ]);
        if (res === null) throw new Error(`${what}: ответа нет`);
        expect(res.status(), what).toBe(status);
        await res.finished();
        // обработка ответа компонентом успевает отрисоваться
        await page.evaluate(() => new Promise<void>((r) => setTimeout(r, 300)));
      };
      /** Раскрыта только строка B, её сравнение — содержимое B, строк A нигде нет. */
      const expectShowsB = async (message: string): Promise<void> => {
        await expect(expanded, message).toHaveCount(1);
        await expect(rowB.locator('.apply-row-btn'), message).toHaveAttribute(
          'aria-expanded',
          'true'
        );
        await expect(rowB.locator('.apply-diff-container .diff-line').first(), message).toBeVisible(
          { timeout: T.action }
        );
        await expect(rowB.locator('.diff-loading'), message).toHaveCount(0);
        await expect(rowB.getByRole('alert'), message).toHaveCount(0);
        await expect
          .poll(
            async () => {
              const removed = (
                await rowB.locator('.apply-diff-container .diff-line-removed').allTextContents()
              ).map(diffLine);
              return markB.filter((m) => !removed.includes(m));
            },
            { message, timeout: T.action }
          )
          .toEqual([]);
        const shown = (await list.locator('.diff-line').allTextContents()).map(diffLine);
        expect(
          markA.filter((m) => shown.includes(m)),
          `${message}: строк копии A нет`
        ).toEqual([]);
      };
      /** Ни раскрытой строки, ни сравнения, ни ошибки, ни спиннера прошлого выбора. */
      const expectNothingSelected = async (scope: Locator, message: string): Promise<void> => {
        await expect(scope.locator('.apply-row-btn[aria-expanded="true"]'), message).toHaveCount(0);
        await expect(scope.locator('.apply-diff-container'), message).toHaveCount(0);
        await expect(scope.locator('.apply-row .diff-loading'), message).toHaveCount(0);
        await expect(scope.locator('.apply-row [role="alert"]'), message).toHaveCount(0);
      };

      // 1. A, сразу B: чтение A задержано на диске и завершается после ответа B
      await blockCopy(idA, key);
      const reqA1 = await select(rowA, idA);
      const reqB1 = await select(rowB, idB);
      await answered(reqB1, 200, 'содержимое B');
      await releaseCopy(idA, key);
      await answered(reqA1, 200, 'позднее содержимое A');
      await expectShowsB('G149-02 selected B content');

      // 2. A, B, свернуть B: поздний ответ A не раскрывает и не показывает ничего
      await rowB.locator('.apply-row-btn').click({ timeout: T.action });
      await expectNothingSelected(list, 'свёрнутая B');
      await blockCopy(idA, key);
      const reqA2 = await select(rowA, idA);
      const reqB2 = await select(rowB, idB);
      await answered(reqB2, 200, 'содержимое B');
      await rowB.locator('.apply-row-btn').click({ timeout: T.action });
      await releaseCopy(idA, key);
      await answered(reqA2, 200, 'позднее содержимое A');
      await expectNothingSelected(list, 'G149-02 collapse drops late A');

      // 3. Настоящий 404 и клавиатурный повтор чтения той же копии.
      const rowButton = rowB.locator('.apply-row-btn');
      const retry = rowB.getByRole('alert').getByRole('button', { name: RETRY_NAME });
      const waitForB = () =>
        page.waitForRequest((r) => isContentReq(r, key, idB), { timeout: T.action });
      for (const activation of ['Enter', 'Space']) {
        const message = `G149-09: ${activation}`;
        await hideCopy(idB, key);
        await list
          .locator('.apply-row-btn')
          .nth(iB + 1)
          .focus();
        await page.keyboard.press('Shift+Tab');
        await expect(rowButton, message).toBeFocused();
        const [missing] = await Promise.all([waitForB(), page.keyboard.press(activation)]);
        await answered(missing, 404, `${message}: копии B нет на диске`);
        await expect(rowButton).toHaveAttribute('aria-expanded', 'true');
        await expect(retry).toBeVisible({ timeout: T.action });
        await page.keyboard.press('Tab');
        await expect(retry, message).toBeFocused();

        await releaseCopy(idB, key);
        await blockCopy(idB, key);
        const [repeated] = await Promise.all([waitForB(), page.keyboard.press(activation)]);
        await expect(rowB.locator('.diff-loading')).toBeVisible({ timeout: T.action });
        await expect(rowButton, `${message}: фокус во время чтения`).toBeFocused();
        await releaseCopy(idB, key);
        await answered(repeated, 200, `${message}: повтор читает B`);
        await expectShowsB(message);
        await expect(rowButton, `${message}: фокус после показа сравнения`).toBeFocused();
        await page.keyboard.press('Tab');
        await expect(rowB.locator('.diff-body'), message).toBeFocused();
        await page.keyboard.press('Shift+Tab');
        await expect(rowButton, message).toBeFocused();
        await page.keyboard.press(activation);
        await expectNothingSelected(list, message);
      }

      // 4. A, сменить файл: поздний ответ A не оставляет следов выбора
      await blockCopy(idA, key);
      const reqA5 = await select(rowA, idA);
      const userRow = fileRow(tab, fixtureName);
      await expect(userRow).toBeVisible({ timeout: T.page });
      await userRow.click({ timeout: T.action });
      await expectFileOpened(page, fixtureName);
      await releaseCopy(idA, key);
      await answered(reqA5, 200, 'позднее содержимое A');
      await expectNothingSelected(tab, 'G149-02 file switch drops late A');

      // файл пользователя: рядом с «Сохранить» пометка ImmediateMark
      await expect(tab.locator('.eph-right').getByTestId('immediate-mark').first()).toBeVisible({
        timeout: T.page
      });

      // обратно к файлу панели: история новая, прошлый выбор не вернулся
      await panelRow.click({ timeout: T.action });
      await expect(tab.getByTestId('managed-file-banner')).toBeVisible({ timeout: T.page });
      // лист остался открытым: файл панели показывает историю сам, без повторного нажатия
      await expect(list.locator('.apply-row').first()).toBeVisible({ timeout: T.page });
      await expectNothingSelected(list, 'G149-02 file switch drops late A');

      await guard.assertClean();
    } catch (err) {
      primary = err;
    } finally {
      // копии возвращаются первыми: применение уборки пишет новые наборы и чистит старые
      try {
        await restoreCopies(LOG_KEY);
      } catch (err) {
        primary ??= err;
      }
      const before = logBefore;
      if (before !== null) {
        try {
          await restoreLog(page, before);
          logBefore = null;
        } catch (err) {
          primary ??= err;
        }
      }
      // невозвращённые копии остаются в списке сценария для afterEach
      if (touchedSets.size === 0) createdSets.clear();
    }
    if (primary !== undefined) throw primary;
  }
);
