// e2e-pages: config/files config/cores config/nodes config/rules editor constructor mihomo-gen proxies rules
// router-area: ui-shell
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
