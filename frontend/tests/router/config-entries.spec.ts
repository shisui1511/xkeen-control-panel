// e2e-pages: config/files config/cores config/nodes config/rules config/lists config/setup dashboard proxies rules
// router-area: ui-shell
import { execFile } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard, type ConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, waitApplyIdle } from './lib/nodes-api';

// Входы в настройку (фаза 151, D-09…D-11): экраны, писавшие конфиг ядра мимо слоя, удалены,
// их прежние адреса по одной таблице ведут в подразделы «Конфигурации». Пункта «Редактор» в
// меню нет; адрес #/editor ведёт во вкладку «Файлы» и открывает файл из адреса (запрос
// ?file=/?path= или путь после #/editor/), небезопасный путь не открывается. Перенаправление
// не зависит от порядка параметров запроса; на «Прокси» нет вкладки «Провайдеры», на
// «Правилах» — вкладки «Исключения». Сценарии только читают: файлы открываются без правки,
// черновик не трогается. Ответы сети не подменяются.

import { SLOW_ANNOTATION } from './lib/known';

test.describe.configure({ mode: 'serial' });

// Файл содержит секретный сценарий окна «Панель начала с чистого листа»: ни trace, ни видео, ни
// снимков — с самого начала, до подготовки. Параметры воркера (trace/video/screenshot) нельзя
// задавать внутри describe, поэтому они выключены на весь файл.
test.use({ trace: 'off', video: 'off', screenshot: 'off' });

const VP_DESKTOP = { width: 1280, height: 800 };
const XRAY_CONFIGS = '/opt/etc/xray/configs';

/**
 * Полная загрузка панели с адреса (через about:blank, чтобы вкладки «Файлов» не остались от
 * прошлого адреса). Сборщик консоли подключается до перехода; проверить его — до следующей
 * загрузки.
 */
async function loadFresh(page: Page, hash: string): Promise<ConsoleGuard> {
  await page.goto('about:blank');
  const guard = attachConsoleGuard(page);
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  return guard;
}

/** Файлы каталога по API панели (ответ — массив или конверт data); только простые пути. */
async function listFiles(page: Page, dir: string): Promise<{ name: string; path: string }[]> {
  const res = await api(page, 'GET', `/api/config/list?dir=${encodeURIComponent(dir)}`);
  expect(res.status, 'GET /api/config/list').toBe(200);
  const arr: unknown = res.data;
  if (!Array.isArray(arr)) return [];
  return arr.filter(
    (f): f is { name: string; path: string } =>
      typeof f === 'object' &&
      f !== null &&
      typeof (f as { name?: unknown }).name === 'string' &&
      typeof (f as { path?: unknown }).path === 'string' &&
      /^\/[\w./-]+$/.test((f as { path: string }).path)
  );
}

/** Прежний адрес Редактора привёл во вкладку «Файлы». */
async function expectFilesTab(page: Page): Promise<void> {
  await page.waitForURL((url) => url.hash === '#/config/files', { timeout: T.action });
  await expect(page.getByTestId('files-tab')).toBeVisible({ timeout: T.page });
}

test('config-entries editor-redirect vp:1280', async ({ page }) => {
  await page.setViewportSize(VP_DESKTOP);

  // Пункта «Редактор» в меню нет: файлы живут только в «Конфигурация → Файлы» (D-11)
  let guard = await loadFresh(page, '#/');
  await page.waitForLoadState('networkidle');
  const nav = page.locator('nav.sidebar-nav');
  await expect(nav).toBeVisible({ timeout: T.page });
  await expect(nav.locator('a[href="#/config/files"], a[href^="#/editor"]')).toHaveCount(0);

  // Адрес #/editor без файла ведёт во вкладку «Файлы»
  await page.goto('/#/editor', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expectFilesTab(page);
  const files = await listFiles(page, XRAY_CONFIGS);
  await guard.assertClean();

  // Небезопасный путь: перенаправление есть, файл не открывается
  guard = await loadFresh(page, `#/editor?file=${encodeURIComponent('/opt/../etc/passwd')}`);
  await expectFilesTab(page);
  await page.waitForLoadState('networkidle');
  await expect(page.locator('.editor-tab')).toHaveCount(0);
  await guard.assertClean();

  if (files.length === 0) {
    test.info().annotations.push({
      type: 'skip-part',
      description: 'в каталоге конфигураций Xray нет файлов — открытие файла из адреса не проверено'
    });
    return;
  }

  // Путь файла в запросе (?file= и ?path=, с другими параметрами) и в адресе #/editor/<путь>
  // открывает файл во вкладке «Файлы»; адрес после перенаправления — без пути
  const variants = [
    (p: string) => `#/editor?file=${encodeURIComponent(p)}`,
    (p: string) => `#/editor?view=1&path=${encodeURIComponent(p)}`,
    (p: string) => `#/editor/${encodeURIComponent(p)}`
  ];
  for (const [i, variant] of variants.entries()) {
    const file = files[i % files.length];
    guard = await loadFresh(page, variant(file.path));
    await expectFilesTab(page);
    await expect(
      page.locator(`.editor-tab.active[data-path="${file.path}"]`),
      `вариант адреса ${i + 1}: файл открыт во вкладке «Файлы»`
    ).toBeVisible({ timeout: T.page });
    await expect(page.getByTestId('files-tab').locator('.cm-content').first()).toBeVisible({
      timeout: T.page
    });
    await guard.assertClean();
  }
});

interface LegacyAddress {
  /** Прежний адрес удалённого экрана или вкладки. */
  from: string;
  /** Подраздел «Конфигурации», куда он ведёт. */
  target: string;
  /** Вкладка подраздела, видимая после перехода. */
  tab: string;
}

// Таблица прежних адресов D-10 с разными формами запроса: перенаправление решают путь и
// значение параметра, порядок и лишние параметры на него не влияют
const LEGACY: readonly LegacyAddress[] = [
  { from: '#/constructor', target: '#/config/cores', tab: 'cores-tab' },
  { from: '#/constructor?tab=routing', target: '#/config/cores', tab: 'cores-tab' },
  { from: '#/mihomo-gen', target: '#/config/cores', tab: 'cores-tab' },
  { from: '#/editor?tab=constructor', target: '#/config/cores', tab: 'cores-tab' },
  { from: '#/editor?file=x&tab=constructor', target: '#/config/cores', tab: 'cores-tab' },
  { from: '#/subscriptions', target: '#/config/nodes', tab: 'nodes-tab' },
  { from: '#/subscriptions/rt-legacy-sub-1', target: '#/config/nodes', tab: 'nodes-tab' },
  { from: '#/proxies?tab=providers', target: '#/config/nodes', tab: 'nodes-tab' },
  { from: '#/proxies?view=list&tab=providers', target: '#/config/nodes', tab: 'nodes-tab' },
  { from: '#/dat', target: '#/config/lists', tab: 'lists-tab' },
  { from: '#/rules?tab=exceptions', target: '#/config/rules', tab: 'rules-tab' },
  { from: '#/rules?q=1&tab=exceptions', target: '#/config/rules', tab: 'rules-tab' }
];

test('config-entries legacy-redirects vp:1280', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize(VP_DESKTOP);
  await page.goto('/#/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');

  for (const { from, target, tab } of LEGACY) {
    await page.goto(`/${from}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForURL((url) => url.hash === target, { timeout: T.action });
    await expect(page.getByTestId(tab), `${from} → ${target}`).toBeVisible({ timeout: T.page });
  }

  // Действующие страницы с прежними вкладками остаются на месте и прежних вкладок не несут
  await page.goto('/#/proxies', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');
  await expect(page, '#/proxies не перенаправляется').toHaveURL(/#\/proxies$/);
  await expect(
    page.locator('.tabs-bar [data-value="providers"], .tabs [data-value="providers"]')
  ).toHaveCount(0);

  await page.goto('/#/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');
  await expect(page, '#/rules не перенаправляется').toHaveURL(/#\/rules$/);
  await expect(page.getByTestId('tab-exceptions')).toHaveCount(0);

  await guard.assertClean();
});

// --- Окно чистого старта и следы старой панели (фаза 151, D-04…D-06) ---
// Состояние готовит настоящая панель: при остановленной панели кладётся реестр старых
// подписок, штатный старт складывает его в архив и открывает окно. Адреса синтетические
// (домен .invalid, Unicode и percent-encoding), но обращаются с ними как с секретом: trace,
// видео и снимки выключены до подготовки, в сообщениях проверок — только коды и счёт, а
// сравнение адресов идёт без вывода значений. Исходные архив и файл состояния слоя
// сохраняются до подготовки и возвращаются в finally.

const run = promisify(execFile);
const REPO_ROOT = fileURLToPath(new URL('../../../', import.meta.url));
const XCP_DATA = '/opt/etc/xcp';
const XCP_INIT = '/opt/etc/init.d/S99xcp';
const XCP_STATE = `${XCP_DATA}/config_layer_state.json`;
const LEGACY_BACKUP = '/opt/tmp/xcp-e2e-legacy-reset';
// Файл выбора узла старой панели: панель узнаёт его по точному имени
const LEFTOVER_FILE = `${XRAY_CONFIGS}/04_outbounds.zz_xcp_default.json`;
const VP_PHONE = { width: 390, height: 844 };

/**
 * Команда на стенде arm64 через scripts/router/lib.sh из корня репозитория: алиас и доступы
 * берутся только из локального файла целей. Возвращает код выхода; сбой подготовки (файл
 * целей, связь) — ошибка. Вывод ssh в отчёт не попадает.
 */
async function standSh(remote: string, timeout = 60_000): Promise<number> {
  const code = await run(
    'sh',
    [
      '-c',
      '. scripts/router/lib.sh || exit 90; rt_load_targets || exit 91; rt_ssh arm64 "$1"',
      'sh',
      remote
    ],
    { cwd: REPO_ROOT, env: { ...process.env, ROUTERS: 'arm64' }, timeout }
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

// Остановка панели init-скриптом и ожидание выхода процесса (до 60 с)
const STOP_PANEL =
  `${XCP_INIT} stop >/dev/null 2>&1; i=0; ` +
  `while pidof xcp >/dev/null 2>&1; do i=$((i+1)); [ $i -gt 60 ] && exit 20; sleep 1; done`;
const START_PANEL = `${XCP_INIT} start >/dev/null 2>&1 || exit 29`;

/** Ждёт, что панель после старта отвечает снимком слоя той же сессией. */
async function waitPanelUp(page: Page): Promise<void> {
  await expect
    .poll(
      async () => {
        try {
          return (await api(page, 'GET', '/api/configlayer/state')).status;
        } catch {
          return 0;
        }
      },
      { message: 'панель отвечает после перезапуска', timeout: T.save * 2, intervals: [2000] }
    )
    .toBe(200);
}

/**
 * Сохраняет исходные архив старых данных и файл состояния слоя. Копия от прерванного
 * прогона не перезаписывается: она и есть исходное состояние.
 */
async function backupLegacy(): Promise<void> {
  const code = await standSh(
    `B=${LEGACY_BACKUP}; [ -f "$B/.complete" ] && exit 0; rm -rf "$B"; ` +
      `mkdir -p "$B" && chmod 700 "$B" || exit 30; ` +
      `if [ -f ${XCP_STATE} ]; then cp -p ${XCP_STATE} "$B/state" || exit 31; fi; ` +
      `if [ -d ${XCP_DATA}/legacy ]; then cp -a ${XCP_DATA}/legacy "$B/legacy" || exit 32; fi; ` +
      `touch "$B/.complete"`
  );
  if (code !== 0) throw new Error(`legacy-backup: копия исходных данных не сделана (код ${code})`);
}

/** Возвращает исходные архив и файл состояния при остановленной панели и запускает её. */
async function restoreLegacy(page: Page): Promise<void> {
  const code = await standSh(
    `B=${LEGACY_BACKUP}; [ -f "$B/.complete" ] || exit 40; ${STOP_PANEL}; ` +
      `rm -rf ${XCP_DATA}/legacy ${XCP_DATA}/subscriptions.json || exit 41; ` +
      `if [ -d "$B/legacy" ]; then cp -a "$B/legacy" ${XCP_DATA}/legacy || exit 42; fi; ` +
      `if [ -f "$B/state" ]; then cp -p "$B/state" ${XCP_STATE} || exit 43; fi; ` +
      `rm -rf "$B"; ${START_PANEL}`,
    150_000
  );
  if (code !== 0) throw new Error(`legacy-restore: исходные данные не возвращены (код ${code})`);
  await waitPanelUp(page);
}

/**
 * Перезапуск панели с подготовкой окна: прежний архив и признак закрытия снимаются (нулевое
 * время закрытия файл состояния читает как «не закрыто»), registry — JSON реестра старых
 * подписок; null — перезапуск без подготовки.
 */
async function restartPanel(page: Page, registry: string | null): Promise<void> {
  if (registry !== null && registry.includes("'")) throw new Error('реестр с апострофом');
  const seed =
    registry === null
      ? ''
      : `rm -rf ${XCP_DATA}/legacy || exit 21; ` +
        `if [ -f ${XCP_STATE} ]; then sed -i 's/"closed_at"[[:space:]]*:[[:space:]]*"[^"]*"/"closed_at": "0001-01-01T00:00:00Z"/' ${XCP_STATE} || exit 22; fi; ` +
        `(umask 077; printf '%s' '${registry}' > ${XCP_DATA}/subscriptions.json) || exit 23; `;
  const code = await standSh(`${STOP_PANEL}; ${seed}${START_PANEL}`, 150_000);
  if (code !== 0) throw new Error(`legacy-seed: панель не перезапущена (код ${code})`);
  await waitPanelUp(page);
  // Страница со старым снимком слоя не должна пережить перезапуск: переход на тот же адрес с
  // другим фрагментом не перезагружает приложение, окно читало бы устаревший признак
  await page.goto('about:blank');
}

/** Синтетические адреса: длинный, Unicode в пути и percent-encoding в запросе. */
function legacyUrls(n: number): string[] {
  const tag = Date.now().toString(36);
  return Array.from({ length: n }, (_, i) =>
    i === 1
      ? `https://rt-legacy-${tag}-${i}.invalid/подписка/ключ?name=%D0%BF%D1%80%20x&q=ü`
      : `https://rt-legacy-${tag}-${i}.invalid/sub/${'a'.repeat(48)}?token=${tag}${i}%2F%3D`
  );
}

function registryOf(urls: string[]): string {
  return JSON.stringify(urls.map((url, i) => ({ id: `rt-legacy-${i}`, url })));
}

interface LegacyApi {
  pending: boolean;
  urls: number;
}

/** Состояние окна на сервере без адресов: признак и число адресов. */
async function legacyState(page: Page): Promise<LegacyApi> {
  const res = await api(page, 'GET', '/api/configlayer/legacy');
  expect(res.status, 'GET /api/configlayer/legacy').toBe(200);
  const urls = res.data?.urls;
  return { pending: res.data?.pending === true, urls: Array.isArray(urls) ? urls.length : -1 };
}

/** Сколько элементов страницы несут адрес в атрибуте (title, aria-*, data-* и т. п.). */
async function urlsInAttributes(page: Page, urls: string[]): Promise<number> {
  return page.evaluate((list) => {
    let n = 0;
    for (const el of Array.from(document.querySelectorAll('*'))) {
      for (const attr of Array.from(el.attributes)) {
        if (list.some((u) => attr.value.includes(u))) n++;
      }
    }
    return n;
  }, urls);
}

async function openAt(page: Page, hash: string): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');
}

/** Кнопки окна целиком в ширине экрана. */
async function expectButtonsFit(page: Page, width: number): Promise<void> {
  for (const id of ['legacy-reset-setup', 'legacy-reset-close']) {
    const box = await page.getByTestId(id).boundingBox();
    expect(box, `${id}: кнопка видна`).not.toBeNull();
    if (!box) continue;
    expect(box.x >= 0 && box.x + box.width <= width, `${id}: кнопка не обрезана`).toBe(true);
  }
}

test.describe('config-entries legacy-reset', () => {
  test(
    'window vp:1280',
    {
      annotation: {
        type: SLOW_ANNOTATION,
        description:
          'четыре перезапуска панели init-скриптом с подготовкой окна и возврат исходных данных'
      }
    },
    async ({ page }) => {
      test.setTimeout(T.save * 12);
      await page.setViewportSize(VP_DESKTOP);
      await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
      await openAt(page, '#/');
      // Панель останавливается init-скриптом: конвейер применения не должен быть занят
      await waitApplyIdle(page, 'до перезапусков панели');
      await backupLegacy();
      const win = page.getByTestId('legacy-reset-window');
      try {
        // Длинный список: прокрутка внутри окна, фокус, адреса только текстом, копия целиком
        const urls = legacyUrls(8);
        await restartPanel(page, registryOf(urls));
        const guard = attachConsoleGuard(page);
        await openAt(page, '#/');
        await expect(win).toBeVisible({ timeout: T.page });
        await expect(page.getByTestId('legacy-reset-setup')).toBeFocused({ timeout: T.action });
        const copies = win.getByTestId('legacy-reset-copy');
        await expect(copies).toHaveCount(urls.length, { timeout: T.page });
        const shown = await win.locator('code').evaluateAll((els) => els.map((e) => e.textContent));
        expect(
          shown.length === urls.length && shown.every((u, i) => u === urls[i]),
          'адреса показаны текстом без изменений'
        ).toBe(true);
        expect(await urlsInAttributes(page, urls), 'адреса в атрибутах разметки').toBe(0);
        const scrolls = await win
          .getByTestId('legacy-reset-urls')
          .evaluate((el) => el.scrollHeight > el.clientHeight);
        expect(scrolls, 'длинный список прокручивается внутри окна').toBe(true);
        await copies.nth(1).click();
        await expect(win.locator('[aria-live="polite"]')).not.toBeEmpty({ timeout: T.action });
        const copied = await page.evaluate(
          (u) => navigator.clipboard.readText().then((got) => got === u),
          urls[1]
        );
        expect(copied, 'в буфере полный адрес с Unicode и percent-encoding').toBe(true);

        // Вторая вкладка видит окно; Esc в первой закрывает его на сервере, и окно уходит
        // из второй по событию legacy
        const second = await page.context().newPage();
        try {
          const secondGuard = attachConsoleGuard(second);
          await openAt(second, '#/');
          const secondWin = second.getByTestId('legacy-reset-window');
          await expect(secondWin).toBeVisible({ timeout: T.page });
          await page.bringToFront();
          await page.keyboard.press('Escape');
          await expect(win).toHaveCount(0, { timeout: T.action });
          await expect(secondWin).toHaveCount(0, { timeout: T.page });
          await secondGuard.assertClean();
        } finally {
          await second.close();
        }
        await expect
          .poll(() => legacyState(page), {
            message: 'окно закрыто на сервере, адреса убраны',
            timeout: T.action
          })
          .toEqual({ pending: false, urls: 0 });
        await openAt(page, '#/');
        await expect(win).toHaveCount(0);
        await guard.assertClean();

        // Закрытие переживает перезапуск панели
        await restartPanel(page, null);
        const restartGuard = attachConsoleGuard(page);
        await openAt(page, '#/');
        await expect(win).toHaveCount(0);
        expect((await legacyState(page)).pending, 'окно после перезапуска').toBe(false);
        await restartGuard.assertClean();

        // Ноль адресов: отдельный текст, «Настроить VPN» ведёт в мастер и закрывает окно
        await restartPanel(page, '[]');
        const emptyGuard = attachConsoleGuard(page);
        await openAt(page, '#/');
        await expect(win).toBeVisible({ timeout: T.page });
        await expect(win.getByTestId('legacy-reset-empty')).toBeVisible({ timeout: T.page });
        await expect(win.getByTestId('legacy-reset-unavailable')).toHaveCount(0);
        await page.getByTestId('legacy-reset-setup').click();
        await page.waitForURL((url) => url.hash.startsWith('#/config/setup'), {
          timeout: T.action
        });
        await expect(win).toHaveCount(0);
        expect((await legacyState(page)).pending, 'окно после «Настроить VPN»').toBe(false);
        await emptyGuard.assertClean();

        // Список недоступен: путь архива вместо списка, закрытие работает и на телефоне
        await restartPanel(page, registryOf(legacyUrls(1)));
        // Телефон со списком: зоны нажатия не ниже 44px, строка адреса той же высоты, порядок
        // кнопок в разметке совпадает с видимым (закрыть выше главной)
        await page.setViewportSize(VP_PHONE);
        await openAt(page, '#/');
        await expect(win).toBeVisible({ timeout: T.page });
        await expect(win.getByTestId('legacy-reset-copy')).toHaveCount(1, { timeout: T.page });
        const boxes = await win.evaluate((el) => {
          const box = (sel: string) => {
            const r = el.querySelector(sel)?.getBoundingClientRect();
            return r ? { y: r.y, h: r.height, w: r.width } : null;
          };
          return {
            copy: box('[data-testid="legacy-reset-copy"]'),
            row: box('.legacy-url'),
            close: box('[data-testid="legacy-reset-close"]'),
            setup: box('[data-testid="legacy-reset-setup"]')
          };
        });
        expect(boxes.copy && boxes.copy.h >= 43.5, '«Копировать» не ниже 44px').toBe(true);
        expect(boxes.close && boxes.close.h >= 43.5, '«Закрыть» не ниже 44px').toBe(true);
        expect(boxes.setup && boxes.setup.h >= 43.5, '«Настроить VPN» не ниже 44px').toBe(true);
        expect(
          boxes.row && boxes.copy && Math.abs(boxes.row.h - boxes.copy.h) <= 1,
          'строка адреса той же высоты, что «Копировать»'
        ).toBe(true);
        expect(
          boxes.close && boxes.setup && boxes.close.y < boxes.setup.y,
          'порядок кнопок в разметке совпадает с видимым'
        ).toBe(true);
        const rm = await standSh(`rm -f ${XCP_DATA}/legacy/urls.json`);
        expect(rm, 'список адресов убран с диска').toBe(0);
        const phoneGuard = attachConsoleGuard(page);
        await openAt(page, '#/');
        await expect(win).toBeVisible({ timeout: T.page });
        await expect(win.getByTestId('legacy-reset-unavailable')).toBeVisible({
          timeout: T.page
        });
        await expect(win.getByTestId('legacy-reset-empty')).toHaveCount(0);
        await expectButtonsFit(page, VP_PHONE.width);
        await page.getByTestId('legacy-reset-close').click();
        await expect(win).toHaveCount(0, { timeout: T.action });
        await expect
          .poll(async () => (await legacyState(page)).pending, {
            message: 'закрытие записано на сервер',
            timeout: T.action
          })
          .toBe(false);
        await phoneGuard.assertClean();
      } finally {
        await page.setViewportSize(VP_DESKTOP);
        await restoreLegacy(page);
      }
    }
  );

  test(
    'leftovers vp:1280',
    {
      annotation: {
        type: SLOW_ANNOTATION,
        description: 'уведомление появляется плановой сверкой слоя (раз в минуту)'
      }
    },
    async ({ page }) => {
      test.setTimeout(T.save * 6);
      await page.setViewportSize(VP_DESKTOP);
      const seeded = await standSh(
        `[ -d ${XRAY_CONFIGS} ] || exit 10; [ -e ${LEFTOVER_FILE} ] && exit 11; ` +
          `(umask 077; printf '%s' '{"outbounds":[]}' > ${LEFTOVER_FILE}) || exit 12`
      );
      if (seeded === 10 || seeded === 11) {
        test.info().annotations.push({
          type: 'skip-part',
          description:
            seeded === 10
              ? 'нет каталога конфигураций Xray — следы не подготовить'
              : 'на стенде уже есть файл выбора старой панели — тест его не трогает'
        });
        return;
      }
      expect(seeded, 'файл выбора старой панели подготовлен').toBe(0);
      try {
        const guard = attachConsoleGuard(page);
        await openAt(page, '#/config/nodes');
        const notice = page.locator('[data-notice="legacy_leftovers"]');
        await expect(notice).toBeVisible({ timeout: T.save * 2 });
        await expect(notice.locator('.alert-close-btn')).toHaveCount(0);
        await expect(notice.getByTestId('legacy-leftovers-files')).toContainText(
          '04_outbounds.zz_xcp_default.json'
        );
        expect(
          await notice.getByTestId('legacy-leftovers-files').locator('li').count(),
          'строк списка не больше пяти и строки «и ещё»'
        ).toBeLessThanOrEqual(6);
        await expect(notice.getByTestId('legacy-leftovers-files-link')).toHaveAttribute(
          'href',
          '#/config/files'
        );
        // «Применить» уведомление не блокирует: проверок черновика о следах нет
        const state = await api(page, 'GET', '/api/configlayer/state');
        expect(state.status, 'GET /api/configlayer/state').toBe(200);
        const checks = (state.data?.checks ?? []) as { code?: string }[];
        expect(
          checks.filter((c) => String(c.code ?? '').includes('legacy')).length,
          'проверки черновика о следах старой панели'
        ).toBe(0);
        await guard.assertClean();

        // В мастере уведомление скрыто
        const setupGuard = attachConsoleGuard(page);
        await openAt(page, '#/config/setup');
        await expect(page.locator('[data-notice="legacy_leftovers"]')).toHaveCount(0);
        await setupGuard.assertClean();

        // «Убрать»: подтверждение, кнопка в загрузке, уведомление уходит само, файла нет
        const cleanGuard = attachConsoleGuard(page);
        await openAt(page, '#/config/nodes');
        await expect(notice).toBeVisible({ timeout: T.page });
        await notice.getByTestId('legacy-leftovers-clean').click();
        const dialog = page.locator('[role="dialog"]:has(.confirm-actions)');
        await expect(dialog).toBeVisible({ timeout: T.action });
        await dialog.locator('.confirm-actions button').last().click();
        await expect(notice).toHaveCount(0, { timeout: T.save * 2 });
        expect(await standSh(`test ! -e ${LEFTOVER_FILE}`), 'файл выбора убран').toBe(0);
        await cleanGuard.assertClean();
      } finally {
        await standSh(`rm -f ${LEFTOVER_FILE}`);
      }
    }
  );
});
