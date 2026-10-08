// e2e-pages: editor
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';

// Запись в Редакторе настоящей панели и окна, которые появляются только при записи (RT-05):
// окно подтверждения сохранения (SaveConfirm) и предупреждения Preflight после записи. Проверка
// на 390 px в обеих темах: кнопки окна видимы целиком внутри экрана и окна, доступны, берут
// фокус с клавиатуры, нажимаются; ядро принимает файл.
//
// Правка безвредная (D-04): уровень журнала активного ядра. Xray — «loglevel» в файле секции
// log каталога конфигов, Mihomo — «log-level» в config.yaml. Исходное значение возвращается
// вторым сохранением тем же путём; при любом исходе прогона снимок стенда (D-19) вернёт файлы.
//
// Предупреждения Preflight (блок над редактором) приходят не на каждую запись: сервер ищет
// рискованные правила маршрутизации и DNS. Блок проверяется, когда он открылся, а если нет —
// тест пишет об этом в вывод (не падение). Для Xray есть безвредная правка, которая его
// вызывает: файл маршрутизации сервер проверяет на обход частных сетей и порта RDP, поэтому
// пробельная правка (лишняя пустая строка в конце) даёт предупреждение, если в маршрутизации
// стенда этого обхода нет. Для Mihomo то же даёт правка config.yaml без секции rules.

const THEMES = ['light', 'dark'] as const;
const VIEWPORT = { width: 390, height: 844 };

const XRAY_DIR = '/opt/etc/xray/configs';
const MIHOMO_DIR = '/opt/etc/mihomo';
const XRAY_LOG = '01_log.json';
const XRAY_ROUTING = '05_routing.json';
const MIHOMO_CONFIG = 'config.yaml';

/** Безвредная правка: возвращает новое содержимое файла с другим уровнем журнала. */
function mutateLogLevel(core: string, orig: string): string {
  if (core === 'xray') {
    const re = /("loglevel"\s*:\s*")([a-z]+)(")/;
    const m = re.exec(orig);
    if (!m) throw new Error(`в файле ${XRAY_LOG} нет ключа loglevel`);
    const next = m[2] === 'warning' ? 'error' : 'warning';
    return orig.replace(re, `$1${next}$3`);
  }
  const re = /^log-level:\s*([a-z]+)\s*$/m;
  const m = re.exec(orig);
  if (m) return orig.replace(re, `log-level: ${m[1] === 'warning' ? 'error' : 'warning'}`);
  return `log-level: warning\n${orig}`;
}

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** Элемент видим целиком внутри окна браузера и (если задано) внутри рамки окна. */
async function expectInside(page: Page, loc: Locator, what: string, frame?: Box): Promise<Box> {
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
  if (frame) {
    expect(box.x, `${what}: левее окна`).toBeGreaterThanOrEqual(frame.x - 0.5);
    expect(box.x + box.width, `${what}: правее окна`).toBeLessThanOrEqual(
      frame.x + frame.width + 0.5
    );
    expect(box.y, `${what}: выше окна`).toBeGreaterThanOrEqual(frame.y - 0.5);
    expect(box.y + box.height, `${what}: ниже окна`).toBeLessThanOrEqual(
      frame.y + frame.height + 0.5
    );
  }
  return box;
}

/** Нажимает Tab, пока фокус не побывает на каждой из кнопок действий окна. */
async function expectKeyboardReach(page: Page, count: number): Promise<void> {
  const reached = new Set<number>();
  for (let i = 0; i < 12 && reached.size < count; i++) {
    await page.keyboard.press('Tab');
    const idx = await page.evaluate(() => {
      const el = document.activeElement;
      if (!el) return -1;
      const all = Array.from(
        document.querySelectorAll('[role="dialog"] .confirm-modal-actions button')
      );
      return all.indexOf(el as HTMLButtonElement);
    });
    if (idx >= 0) reached.add(idx);
  }
  expect(reached.size, 'клавиатурой достижимы не все кнопки действий окна').toBe(count);
}

/** Открывает панель файлов и файл по имени (на узком экране панель выезжает листом). */
async function openFile(page: Page, name: string): Promise<void> {
  const pane = page.locator('.file-tree-pane');
  if (!(await pane.first().isVisible())) {
    const emptyBtn = page.locator('.editor-empty-actions .btn-secondary');
    const toggleBtn = page.locator('.btn-sidebar-toggle');
    await expect(emptyBtn.or(toggleBtn).first()).toBeVisible({ timeout: T.page });
    if (await emptyBtn.first().isVisible()) await emptyBtn.first().click();
    else await toggleBtn.first().click();
  }
  await expect(pane.first()).toBeVisible({ timeout: T.action });
  const row = pane.first().locator(`.file-row:has(.fr-name[title="${name}"])`).first();
  await expect(row).toBeVisible({ timeout: T.page });
  await row.click();
  await expect(page.locator(`.editor-tab:has-text("${name}")`).first()).toBeVisible({
    timeout: T.page
  });
  await expect(page.locator('.cm-content').first()).toBeVisible({ timeout: T.page });
}

/** Заменяет весь текст в редакторе: выделить всё и вставить одним событием. */
async function replaceContent(page: Page, content: string): Promise<void> {
  const cm = page.locator('.cm-content').first();
  await cm.click();
  await page.keyboard.press('Control+a');
  await page.keyboard.insertText(content);
}

/** «Сохранить» из шапки: на узком экране кнопка в меню «Ещё». Возвращает окно подтверждения. */
async function pressSave(page: Page): Promise<Locator> {
  const more = page.locator('.btn-overflow-trigger');
  if (await more.isVisible()) {
    await more.click();
    await page.locator('.overflow-dropdown .dropdown-item').first().click();
  } else {
    await page.locator('.eph-right .btn-desktop-only').first().click();
  }
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible({ timeout: T.action });
  return dialog;
}

/** Проверки окна подтверждения, затем подтверждение. Возвращает число кнопок действий. */
async function confirmSave(page: Page, what: string): Promise<number> {
  const dialog = await pressSave(page);
  // окно появляется с анимацией: ждём, пока рамка остановится
  await page.waitForTimeout(400);
  const frame = await expectInside(page, dialog, `${what}: окно подтверждения`);
  const buttons = dialog.locator('.confirm-modal-actions button');
  const n = await buttons.count();
  expect(n, `${what}: кнопок действий в окне`).toBeGreaterThanOrEqual(2);
  for (let i = 0; i < n; i++) {
    const b = buttons.nth(i);
    await expect(b).toBeVisible();
    await expect(b).toBeEnabled();
    await expectInside(page, b, `${what}: кнопка ${i + 1} окна подтверждения`, frame);
  }
  await expectKeyboardReach(page, n);
  // последняя кнопка — «Сохранить»
  await buttons.nth(n - 1).click();
  await expect(dialog).toBeHidden({ timeout: T.action });
  return n;
}

/** Блок предупреждений Preflight после записи: проверяется, только если он открылся. */
async function checkPreflight(page: Page, what: string): Promise<boolean> {
  const block = page.locator('.preflight-warnings').first();
  const shown = await block.isVisible().catch(() => false);
  if (!shown) return false;
  await expectInside(page, block, `${what}: блок предупреждений Preflight`);
  const close = block.locator('.alert-close-btn');
  if (await close.count()) {
    await expect(close.first()).toBeEnabled();
    await expectInside(page, close.first(), `${what}: кнопка закрытия предупреждений`);
  }
  return true;
}

async function readFile(page: Page, path: string): Promise<string> {
  const res = await page.request.get(`/api/config/read?path=${encodeURIComponent(path)}`);
  expect(res.ok(), 'файл не читается').toBe(true);
  return res.text();
}

/** Закрывает видимые уведомления об успехе: прежний не должен засчитаться за новую запись. */
async function clearSuccessToasts(page: Page): Promise<void> {
  const closers = page.locator('.toast--success .toast__close');
  for (let i = await closers.count(); i > 0; i--) {
    await closers
      .first()
      .click({ timeout: 2000 })
      .catch(() => {});
  }
  await expect(page.locator('.toast--success')).toHaveCount(0, { timeout: T.action });
}

/**
 * Один цикл: открыть файл, записать правку через окно подтверждения, убедиться, что запись
 * принята, затем вернуть исходное содержимое вторым сохранением. Результат — было ли окно Preflight.
 */
async function writeAndRestore(
  page: Page,
  core: string,
  dir: string,
  name: string,
  edit: (orig: string) => string,
  what: string
): Promise<boolean> {
  const path = `${dir}/${name}`;
  // исходный текст файла — из того же API, которым пользуется панель
  const orig = await readFile(page, path);
  const edited = edit(orig);
  expect(edited).not.toBe(orig);

  await openFile(page, name);
  await clearSuccessToasts(page);

  // --- правка и запись ----------------------------------------------------------
  await replaceContent(page, edited);
  const buttons = await confirmSave(page, `${what}, запись`);
  await expect(page.locator('.toast--success').first()).toBeVisible({ timeout: T.action });
  await expect(page.locator('.toast--error')).toHaveCount(0);
  const preflight = await checkPreflight(page, `${what}, запись`);
  console.log(
    `editor-save ${what}: окно подтверждения проверено (кнопок ${buttons}); окно Preflight ${
      preflight ? 'открылось и проверено' : 'не появлялось (предупреждений на этой записи нет)'
    }`
  );

  // ядро приняло запись: файл прочитывается обратно, служба работает
  expect((await readFile(page, path)).trimEnd()).toBe(edited.trimEnd());
  const st = await page.request.get('/api/service/status');
  expect(st.ok(), 'статус службы недоступен после записи').toBe(true);
  const stBody = (await st.json()) as {
    data?: { running_kernels?: string[]; kernel_conflict?: boolean };
  };
  expect(stBody.data?.running_kernels ?? [], 'ядро не работает после записи').toContain(core);
  expect(stBody.data?.kernel_conflict ?? false).toBe(false);

  // --- возврат исходного значения вторым сохранением тем же путём -------------------
  await clearSuccessToasts(page);
  await replaceContent(page, orig);
  await confirmSave(page, `${what}, возврат`);
  await expect(page.locator('.toast--success').first()).toBeVisible({ timeout: T.action });
  expect((await readFile(page, path)).trimEnd()).toBe(orig.trimEnd());
  return preflight;
}

for (const theme of THEMES) {
  test(`editor-save theme:${theme} vp:390 core:${RT.core || 'unknown'}`, async ({ page }) => {
    test.setTimeout(300_000 * RT.slow);
    const core = RT.core;
    test.skip(core !== 'xray' && core !== 'mihomo', 'активное ядро стенда не определено');

    const guard = attachConsoleGuard(page);
    await page.setViewportSize(VIEWPORT);
    await page.addInitScript((th) => {
      localStorage.setItem('theme', th);
    }, theme);
    await page.goto('/#/editor');
    expect(await page.evaluate(() => document.documentElement.getAttribute('data-theme'))).toBe(
      theme
    );

    // Основной сценарий: уровень журнала активного ядра
    if (core === 'xray') {
      await writeAndRestore(
        page,
        core,
        XRAY_DIR,
        XRAY_LOG,
        (o) => mutateLogLevel('xray', o),
        'журнал Xray'
      );
    } else {
      await writeAndRestore(
        page,
        core,
        MIHOMO_DIR,
        MIHOMO_CONFIG,
        (o) => mutateLogLevel('mihomo', o),
        'журнал Mihomo'
      );
    }

    // Предупреждения Preflight для Xray: безвредная пробельная правка файла маршрутизации
    if (core === 'xray') {
      const list = await page.request.get(`/api/config/list?dir=${encodeURIComponent(XRAY_DIR)}`);
      const files = list.ok() ? ((await list.json()) as { name: string }[]) : [];
      if (files.some((f) => f.name === XRAY_ROUTING)) {
        await writeAndRestore(
          page,
          core,
          XRAY_DIR,
          XRAY_ROUTING,
          (o) => `${o}\n`,
          'маршрутизация Xray'
        );
      } else {
        console.log(
          `editor-save: файла ${XRAY_ROUTING} на стенде нет, проверка Preflight для Xray пропущена`
        );
      }
    }

    await guard.assertClean();
  });
}
