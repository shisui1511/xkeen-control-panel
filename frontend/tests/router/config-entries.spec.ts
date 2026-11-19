// e2e-pages: config/files config/cores config/nodes config/rules config/setup dashboard editor constructor mihomo-gen proxies rules
// router-area: ui-shell
import { execFile } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, switchLayer, waitApplyIdle, withPanel } from './lib/nodes-api';

// Входы в настройку при включённом слое «Конфигурация» (фаза 149, D-01, D-16):
// пункт «Редактор» и адрес #/editor перенаправляются во вкладку «Файлы»,
// конструкторы и устаревшие вкладки («Провайдеры», «Исключения») скрыты и
// перенаправлены в соответствующие подразделы «Конфигурации». Без флага вкладка
// «Исключения» и её адрес прежние: legacy-redirects на время проверки выключает слой
// и возвращает его, только если слой ничего не держит (иначе skip-part).
// После цикла стенд сверяется со снимком. Ответы сети не подменяются.

import { SLOW_ANNOTATION } from './lib/known';

const layer = layerFlag();

/** Переключает флаг слоя настоящим маршрутом настроек (сессия и CSRF панели). */
async function setLayer(browser: Browser, on: boolean): Promise<void> {
  await withPanel(browser, (panel) => switchLayer(panel, on));
}

interface SideState {
  routing: {
    desired: boolean;
    desired_xray: boolean;
    desired_mihomo: boolean;
    xray: boolean | null;
    mihomo: boolean | null;
    mihomo_active: string;
  };
  cores: string[];
  files: string[];
  drift_count: number;
  running: { xray: boolean; mihomo: boolean };
  dns: {
    state: string;
    desired: boolean;
    window: boolean;
    proxy_dns: boolean;
    owner: string;
  } | null;
  geodata: {
    enabled: boolean;
    day: number;
    hour: number;
    minute: number;
    suspended: boolean;
    cron_found: boolean;
    cron_adoptable: boolean;
    cron_two: boolean;
    cron_kept: boolean;
    cron_taken: boolean;
    cron_schedule: string | null;
  } | null;
}

/** Только устойчивые поля настоящих снимков: без адресов, узлов и времени опроса. */
async function layerSideState(page: Page): Promise<SideState> {
  const state = await api(page, 'GET', '/api/configlayer/state');
  expect(state.status, '/api/configlayer/state для снимка стенда').toBe(200);
  const routing = state.data?.routing as {
    desired: boolean;
    desired_xray: boolean;
    desired_mihomo: boolean;
    xray: { taken: boolean } | null;
    mihomo: { taken: boolean } | null;
    mihomo_active: string;
  };
  const cores = state.data?.cores as { xray: Record<string, { taken: boolean; desired: boolean }> };
  const files = state.data?.files as { key: string; state: string }[];
  const sources = await api(page, 'GET', '/api/configlayer/sources');
  expect(sources.status, '/api/configlayer/sources для снимка стенда').toBe(200);
  const running = sources.data?.running as { xray: boolean; mihomo: boolean };
  const dns = await api(page, 'GET', '/api/configlayer/dns');
  let dnsState: SideState['dns'] = null;
  if (!(dns.status === 404 && dns.data?.code === 'not_found')) {
    expect(dns.status, '/api/configlayer/dns для снимка стенда').toBe(200);
    const d = dns.data as {
      state: string;
      desired: boolean;
      window?: unknown;
      legacy: { proxy_dns: boolean };
      owner: { kind: string };
    };
    dnsState = {
      state: d.state,
      desired: d.desired,
      window: d.window != null,
      proxy_dns: d.legacy.proxy_dns,
      owner: d.owner.kind
    };
  }
  const geo = await api(page, 'GET', '/api/configlayer/geodata');
  let geoState: SideState['geodata'] = null;
  if (!(geo.status === 404 && geo.data?.code === 'config_layer_disabled')) {
    expect(geo.status, '/api/configlayer/geodata для снимка стенда').toBe(200);
    const g = geo.data as {
      schedule: { enabled: boolean; day: number; hour: number; minute: number; suspended: boolean };
      cron: {
        found: boolean;
        adoptable: boolean;
        two: boolean;
        kept: boolean;
        taken: boolean;
        schedule?: string;
      };
    };
    expect(typeof g.cron.taken, 'cron.taken в снимке стенда').toBe('boolean');
    const { enabled, day, hour, minute, suspended } = g.schedule;
    geoState = {
      enabled,
      day,
      hour,
      minute,
      suspended,
      cron_found: g.cron.found,
      cron_adoptable: g.cron.adoptable,
      cron_two: g.cron.two,
      cron_kept: g.cron.kept,
      cron_taken: g.cron.taken,
      cron_schedule: g.cron.schedule ?? null
    };
  }
  return {
    routing: {
      desired: routing.desired,
      desired_xray: routing.desired_xray,
      desired_mihomo: routing.desired_mihomo,
      xray: routing.xray?.taken ?? null,
      mihomo: routing.mihomo?.taken ?? null,
      mihomo_active: routing.mihomo_active
    },
    cores: Object.entries(cores.xray)
      .filter(([, v]) => v.taken || v.desired)
      .map(([id]) => id)
      .sort(),
    files: files.map(({ key, state }) => `${key}:${state}`).sort(),
    drift_count: Number(state.data?.drift_count),
    running: { xray: running.xray, mihomo: running.mihomo },
    dns: dnsState,
    geodata: geoState
  };
}

function sideStateTaken(s: SideState): string[] {
  const reasons: string[] = [];
  if (
    s.routing.desired ||
    s.routing.desired_xray ||
    s.routing.desired_mihomo ||
    s.routing.xray ||
    s.routing.mihomo
  )
    reasons.push('маршрутизация у панели');
  if (s.cores.length) reasons.push('взятые секции Xray');
  if (s.drift_count > 0 || s.files.some((f) => !f.endsWith(':ok') && !f.endsWith(':released')))
    reasons.push('файлы слоя не в покое');
  if (s.dns && (s.dns.desired || s.dns.window || !['off', 'unavailable'].includes(s.dns.state)))
    reasons.push('перехват DNS');
  if (s.geodata && (s.geodata.enabled || s.geodata.suspended || s.geodata.cron_taken))
    reasons.push('расписание баз или перенятый cron');
  return reasons;
}

/** Перезагружает страницу и ждёт, что панель сама прочитала флаг слоя с ожидаемым значением. */
async function reloadWithFlag(page: Page, on: boolean): Promise<void> {
  const [res] = await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === 'GET' && new URL(r.url()).pathname === '/api/settings',
      { timeout: T.page }
    ),
    page.reload({ timeout: T.page, waitUntil: 'domcontentloaded' })
  ]);
  expect(res.status(), 'GET /api/settings после перезагрузки').toBe(200);
  const body = (await res.json()) as { data?: { config_layer?: unknown } };
  expect(body.data?.config_layer, 'флаг слоя, прочитанный панелью').toBe(on);
}

test.describe.configure({ mode: 'serial' });

const VP_DESKTOP = { width: 1280, height: 800 };

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

test('config-entries editor-redirect vp:1280', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize(VP_DESKTOP);

  await page.goto('/#/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');

  // Пункт меню «Редактор» имеет href="#/config/files" при включённом флаге
  const editorNavItem = page.locator('nav.sidebar-nav a[href="#/config/files"]');
  await expect(editorNavItem).toBeVisible({ timeout: T.action });

  // Переход на #/editor перенаправляется на #/config/files и открывает files-tab
  await page.goto('/#/editor', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.waitForURL((url) => url.hash.startsWith('#/config/files'), { timeout: T.action });
  await expect(page.getByTestId('files-tab')).toBeVisible({ timeout: T.action });

  await guard.assertClean();
});

test(
  'config-entries legacy-redirects vp:1280',
  {
    annotation: {
      type: SLOW_ANNOTATION,
      description:
        'цикл флага слоя: выключение переносит файлы и перезапускает ядро, включение пересобирает файлы и снова перезапускает ядро; ожидание простоя конвейера'
    }
  },
  async ({ page, browser }) => {
    // выключение слоя переносит файлы и перезапускает ядро, включение пересобирает их
    test.setTimeout(T.save * 5);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize(VP_DESKTOP);

    // #/constructor ведёт на #/config/cores
    await page.goto('/#/constructor', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForURL((url) => url.hash.startsWith('#/config/cores'), { timeout: T.action });

    // #/mihomo-gen ведёт на #/config/cores
    await page.goto('/#/mihomo-gen', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForURL((url) => url.hash.startsWith('#/config/cores'), { timeout: T.action });

    // #/proxies?tab=providers ведёт на #/config/nodes
    await page.goto('/#/proxies?tab=providers', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForURL((url) => url.hash.startsWith('#/config/nodes'), { timeout: T.action });

    // #/rules?tab=exceptions ведёт на #/config/rules
    await page.goto('/#/rules?tab=exceptions', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForURL((url) => url.hash.startsWith('#/config/rules'), { timeout: T.action });

    // На #/proxies нет вкладки «Провайдеры»
    await page.goto('/#/proxies', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await expect(
      page.locator('.tabs-bar [data-value="providers"], .tabs [data-value="providers"]')
    ).toHaveCount(0);

    // На #/rules нет вкладки tab-exceptions
    await page.goto('/#/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await expect(page.getByTestId('tab-exceptions')).toHaveCount(0);

    await guard.assertClean();

    // Без флага всё по-старому (D-16): слой временно выключается настоящим маршрутом, вкладка
    // «Исключения» на месте и её адрес не перенаправляется. Флаг возвращается в finally, затем
    // повторно проверяются перенаправление и отсутствие вкладки. Черновик не применяется.
    await waitApplyIdle(page, 'до снимка');
    const before = await layerSideState(page);
    const taken = sideStateTaken(before);
    if (taken.length) {
      test.info().annotations.push({
        type: 'skip-part',
        description: `слой держит: ${taken.join(', ')} — выключение флага необратимо, режим без флага не проверен`
      });
      return;
    }
    const exceptions = page.getByTestId('tab-exceptions');
    try {
      await setLayer(browser, false);
      // сначала перезагрузка на #/rules: панель перечитывает флаг, иначе прежнее значение в
      // памяти перенаправило бы адрес исключений
      await reloadWithFlag(page, false);
      const offGuard = attachConsoleGuard(page);
      await page.goto('/#/rules?tab=exceptions', {
        timeout: T.page,
        waitUntil: 'domcontentloaded'
      });
      await page.waitForLoadState('networkidle');
      await expect(exceptions).toBeVisible({ timeout: T.page });
      await expect(exceptions).toBeEnabled();
      await exceptions.click();
      await expect(exceptions).toHaveAttribute('aria-selected', 'true');
      await expect(page, 'адрес исключений без флага не перенаправляется').toHaveURL(
        /#\/rules\?tab=exceptions$/
      );
      await offGuard.assertClean();
    } finally {
      await setLayer(browser, true);
    }

    // перезагрузка на #/rules?tab=exceptions: при включённом флаге адрес снова ведёт в «Конфигурацию»
    await reloadWithFlag(page, true);
    const onGuard = attachConsoleGuard(page);
    await page.waitForURL((url) => url.hash.startsWith('#/config/rules'), { timeout: T.action });
    await page.goto('/#/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await expect(exceptions).toHaveCount(0);
    await onGuard.assertClean();
    await expect
      .poll(() => layerSideState(page), {
        message: 'G149-06: стенд после цикла флага слоя',
        timeout: T.save,
        intervals: [2000]
      })
      .toEqual(before);
  }
);

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
const XRAY_CONFIGS = '/opt/etc/xray/configs';
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
  // Секретный сценарий: ни trace, ни видео, ни снимков — с самого начала, до подготовки
  test.use({ trace: 'off', video: 'off', screenshot: 'off' });

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
        const rm = await standSh(`rm -f ${XCP_DATA}/legacy/urls.json`);
        expect(rm, 'список адресов убран с диска').toBe(0);
        await page.setViewportSize(VP_PHONE);
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
        const dialog = page.locator('.modal-backdrop:has(.confirm-modal-actions)');
        await expect(dialog).toBeVisible({ timeout: T.action });
        await dialog.locator('.confirm-modal-actions button').last().click();
        await expect(notice).toHaveCount(0, { timeout: T.save * 2 });
        expect(await standSh(`test ! -e ${LEFTOVER_FILE}`), 'файл выбора убран').toBe(0);
        await cleanGuard.assertClean();
      } finally {
        await standSh(`rm -f ${LEFTOVER_FILE}`);
      }
    }
  );
});
