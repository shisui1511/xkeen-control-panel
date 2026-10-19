// e2e-pages: editor
import { test, expect, type Page } from '@playwright/test';
import { randomInt } from 'node:crypto';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { withPanel } from './lib/nodes-api';

// CR-02 (todo editor-json-extra-tokens-console-error): текст после JSON-значения в Редакторе не
// должен попадать в console.error. Разбор неверного JSON идёт через best-effort-json-parser, чей
// обработчик лишних токенов по умолчанию писал в консоль весь текст документа. Сценарий открывает
// временный .json в каталоге активного ядра (Xray: разбор через jsoncDocumentParser, Mihomo:
// jsonSchemaLinter по умолчанию; оба пути доходят до библиотеки), вводит хвост после `{}` и
// требует, чтобы линтер отметил ошибку разбора, а консоль осталась чистой. Сеть не подменяется,
// «Сохранить» не нажимается. Стенд меняется только временным файлом с префиксом TAIL_PREFIX:
// файл создаётся через API панели до сценария и удаляется после (afterAll), остатки прерванных
// прогонов убираются по префиксу перед созданием.

const VP_DESKTOP = { width: 1280, height: 800 };

const XRAY_DIR = '/opt/etc/xray/configs';
const MIHOMO_DIR = '/opt/etc/mihomo';
// отличается от префикса editor-mobile, поэтому уборка спеков не пересекается; без слов
// стоп-списка XKeen и без префикса xcp- файлов слоя «Конфигурация»
const TAIL_PREFIX = 'zz_e2e_jsontail_';
// синтетический текст после JSON-значения; на диск не сохраняется
const TAIL = 'zz_e2e_tail_after_json';

let tailFile: { name: string; path: string } | null = null;

/** Каталог тестового файла: каталог активного ядра. Расширение всегда .json. */
function tailDir(): string | null {
  if (RT.core === 'xray') return XRAY_DIR;
  if (RT.core === 'mihomo') return MIHOMO_DIR;
  return null;
}

test.skip(() => tailDir() === null, 'активное ядро стенда не определено');

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

/** Удаляет тестовые файлы с префиксом вместе со всеми их резервными копиями. */
async function sweepTail(page: Page, dir: string): Promise<void> {
  const list = await rawRequest(page, 'GET', `/api/config/list?dir=${encodeURIComponent(dir)}`);
  expectOk(list, 'list');
  const stale = asArray(list.text).filter(
    (f): f is { name: string; path: string } =>
      typeof f === 'object' &&
      f !== null &&
      typeof (f as { name?: unknown }).name === 'string' &&
      typeof (f as { path?: unknown }).path === 'string' &&
      (f as { name: string }).name.startsWith(TAIL_PREFIX)
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
  test.setTimeout(120_000 * RT.slow);
  const dir = tailDir();
  if (!dir) return;
  await withPanel(browser, async (page) => {
    await sweepTail(page, dir);
    const name = `${TAIL_PREFIX}${randomInt(100000, 1000000)}.json`;
    const path = `${dir}/${name}`;
    expectOk(
      await rawRequest(page, 'POST', `/api/config/create?path=${encodeURIComponent(path)}`),
      'create'
    );
    tailFile = { name, path };
  });
});

test.afterAll(async ({ browser }) => {
  test.setTimeout(120_000 * RT.slow);
  const dir = tailDir();
  if (!dir) return;
  tailFile = null;
  await withPanel(browser, (page) => sweepTail(page, dir));
});

function tf(): { name: string; path: string } {
  if (!tailFile) throw new Error('тестовый файл не создан');
  return tailFile;
}

/** Текст файла на диске через API панели. */
async function readOnDisk(page: Page, path: string): Promise<string> {
  const res = await rawRequest(page, 'GET', `/api/config/read?path=${encodeURIComponent(path)}`);
  expectOk(res, 'read');
  return res.text;
}

test('editor-json-tail extra-tokens vp:1280', async ({ page }) => {
  test.setTimeout(90_000 * RT.slow);
  const guard = attachConsoleGuard(page);
  const { name, path } = tf();

  await page.setViewportSize(VP_DESKTOP);
  await page.goto('/#/editor');
  await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
    timeout: T.page
  });

  // файл открывается кликом по строке колонки файлов
  await page
    .locator('.file-tree-pane')
    .locator(`.file-row:has(.fr-name[title="${name}"])`)
    .first()
    .click({ timeout: T.page });
  const tab = page.locator('.editor-tab', {
    has: page.locator(`.tab-name[title="${name}"]`)
  });
  await expect(tab.first()).toBeVisible({ timeout: T.page });
  await expect(page.locator('.cm-content').first()).toBeVisible({ timeout: T.page });

  // сценарий не должен пройти вхолостую: схема включена, экспертный режим выключен
  const chips = page.locator('.editor-statusbar .chip-toggle');
  await expect(
    chips.nth(0),
    'проверка схемы выключена: сценарий не дойдёт до разбора схемы'
  ).toHaveClass(/(^|\s)active(\s|$)/, { timeout: T.action });
  await expect(chips.nth(1), 'включён экспертный режим: линтер схемы не подключён').not.toHaveClass(
    /(^|\s)active(\s|$)/,
    { timeout: T.action }
  );

  const original = await readOnDisk(page, path);

  // хвост вводится только в редактор: документ становится `{} zz_e2e_tail_after_json`
  await page.locator('.cm-content').first().click({ timeout: T.action });
  await page.keyboard.press('Control+End');
  await page.keyboard.insertText(' ' + TAIL);
  await page.keyboard.press('Escape');
  await expect(tab.first().locator('.tab-dirty-dot')).toBeVisible({ timeout: T.action });

  // оба линтера идут одним проходом @codemirror/lint: диагностика ставится после ответа всех
  // источников, поэтому к её появлению линтер схемы уже разобрал документ, и console.error,
  // если бы он был, уже прозвучал
  await expect
    .poll(
      () => page.locator('.cm-editor .cm-lintPoint-error, .cm-editor .cm-lintRange-error').count(),
      {
        message: 'линтер не отметил текст после JSON: разбор документа не запускался',
        timeout: T.action
      }
    )
    .toBeGreaterThan(0);

  expect(await readOnDisk(page, path), 'правка попала на диск без нажатия «Сохранить»').toBe(
    original
  );

  await guard.assertClean();
});
