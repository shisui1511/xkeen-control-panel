// router-area: dns
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { ensureDnsTarget, releaseDnsTarget } from './lib/dns-target';
import { CORE_TAG, SLOW_ANNOTATION } from './lib/known';
import {
  api,
  installedKernels,
  resetDraft,
  switchLayer,
  withPanel,
  type Installed
} from './lib/nodes-api';

// Стык со старым перенаправлением DNS XKeen (фаза 148, D-34): при включённом слое
// «Конфигурация» старого переключателя «Перенаправление DNS» нет в DNS-секциях
// конструкторов Xray и Mihomo; при выключенном слое он показан по прежнему условию
// (xkeen_dns === false и есть серверы у Xray, DNS включён у Mihomo). Спек меняет флаг слоя
// настоящим маршрутом и возвращает его прежним в afterAll; правки конструкторов остаются
// в браузере и на роутер не пишутся. Ответы сети не подменяются.
//
// D-36: подтверждение выключения флага слоя при включённом перехвате DNS получает абзац о
// возврате DNS прошивке; без перехвата абзаца нет. Диалог отменяется — флаг не меняется.
// Перехват включается настоящими маршрутами службы; если стенд к нему не готов, секция DNS
// активного ядра берётся панелью и создаётся группа-цель «Прокси» (участник DIRECT). В
// afterAll перехват выключается, подготовка убирается, затем флаг слоя возвращается.

const KERNELS = ['xray', 'mihomo'] as const;
type Kernel = (typeof KERNELS)[number];

const LEGACY = 'dns-legacy-redirect';

let layerWasOn = false;
let installed: Installed = { xray: false, mihomo: false };

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await withPanel(browser, async (page) => {
    const settings = await api(page, 'GET', '/api/settings');
    expect(settings.status, 'GET /api/settings').toBe(200);
    layerWasOn = settings.data?.config_layer === true;
    // Установленные ядра читаются через слой: на время чтения он включён
    await setLayer(page, true);
    installed = await installedKernels(page);
  });
});

test.afterAll(async ({ browser }) => {
  await withPanel(browser, async (page) => {
    if (prep.enabled || prep.groupId || prep.routingTaken || prep.sectionKernel) {
      await setLayer(page, true);
      await cleanupTakeover(page);
    }
    await setLayer(page, layerWasOn);
  });
});

/**
 * Включает или выключает слой «Конфигурация» настоящим маршрутом настроек. Переключение —
 * общим switchLayer: выключение ждёт простоя конвейера, занятый запрос повторяется, включение
 * ждёт фоновую сборку.
 */
async function setLayer(page: Page, on: boolean): Promise<void> {
  const settings = await api(page, 'GET', '/api/settings');
  expect(settings.status, 'GET /api/settings').toBe(200);
  if ((settings.data?.config_layer === true) === on) return;
  await switchLayer(page, on);
}

/** Признак xkeen_dns из настоящих capabilities панели. */
async function xkeenDns(page: Page): Promise<boolean | undefined> {
  const res = await api(page, 'GET', '/api/capabilities');
  expect(res.status, 'GET /api/capabilities').toBe(200);
  const v = res.data?.xkeen_dns;
  return typeof v === 'boolean' ? v : undefined;
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Открывает конструктор, выбирает ядро и его DNS-секцию; секция отрисована. */
async function openConstructorDns(page: Page, kernel: Kernel): Promise<void> {
  await page.goto('/#/constructor', { timeout: T.page, waitUntil: 'domcontentloaded' });
  const workspace = page.locator('.main-content');
  const tab = workspace.getByRole('button', {
    name: kernel === 'xray' ? 'Xray' : 'Mihomo',
    exact: true
  });
  await expect(tab).toBeVisible({ timeout: T.page });
  if ((await tab.getAttribute('aria-pressed')) !== 'true') {
    await tab.click();
    await expect(tab).toHaveAttribute('aria-pressed', 'true');
  }
  if (kernel === 'xray') {
    const tabs = page.getByTestId('xray-section-tabs');
    await expect(tabs).toBeVisible({ timeout: T.page });
    await tabs.locator('button[data-tab="dns"]').click();
    await expect(page.getByTestId('xray-section-dns')).toBeVisible({ timeout: T.action });
    await expect(page.locator('#dns-query-strategy')).toBeVisible();
  } else {
    await expect(workspace.locator('.loading-state-block')).toHaveCount(0, { timeout: T.page });
    await workspace
      .locator('.sec-tabs .sec-tab', { hasText: /^\s*DNS/ })
      .first()
      .click();
    const section = page.getByTestId('mihomo-section-dns');
    await expect(section).toBeVisible({ timeout: T.action });
    // Блок старого перенаправления живёт внутри включённого DNS: включение только в браузере
    const enable = section.locator('.toggle-row input[type="checkbox"]');
    if (!(await enable.isChecked())) await enable.check();
    await expect(page.locator('#mihomo-dns-enhanced-mode')).toBeVisible({ timeout: T.action });
  }
}

/**
 * При включённом слое старых конструкторов нет: #/constructor ведёт на вкладку секций ядер
 * «Xray │ Mihomo» (constructorHash в lib/editorRoute.ts). Открывается карточка DNS ядра.
 */
async function openCoresDns(page: Page, kernel: Kernel): Promise<void> {
  await page.goto('/#/constructor', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect
    .poll(() => page.evaluate(() => window.location.hash), { timeout: T.page })
    .toBe('#/config/cores');
  const tab = page.getByTestId('cores-tab');
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
  await page.getByTestId(`kernel-switch-${kernel}`).click();
  await expect(page.getByTestId(`cores-panel-${kernel}`)).toBeVisible({ timeout: T.action });
  const toggle = page.getByTestId(`section-toggle-${kernel}-dns`);
  await expect(toggle).toBeVisible({ timeout: T.action });
  await toggle.click();
}

/** У Xray блок показывается только при серверах DNS: без них в браузер добавляется один. */
async function ensureXrayServer(page: Page): Promise<void> {
  const section = page.getByTestId('xray-section-dns');
  const rows = section.locator('.dns-servers-list .item-row');
  if ((await rows.count()) > 0) return;
  await section.locator('.add-btn').first().click();
  await page.locator('#xray-new-dns-address').fill('9.9.9.9');
  await section.locator('.form-actions .btn-primary').first().click();
  await expect(rows).toHaveCount(1, { timeout: T.action });
}

for (const kernel of KERNELS) {
  test(`dns-legacy: слой включён — старого переключателя нет (${kernel})`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    test.skip(!installed[kernel], `${kernel} не установлен на стенде`);
    await startPage(page);
    await setLayer(page, true);
    // Флаг слоя панель читает при загрузке: страница перечитывается
    await page.reload({ waitUntil: 'domcontentloaded' });

    await openCoresDns(page, kernel);
    // Ни блока, ни заглушки вместо него (D-34) — при любом значении xkeen_dns
    await expect(page.getByTestId(LEGACY)).toHaveCount(0);
    await expect(
      page.getByRole('button', { name: /^(Включить перехват DNS|Enable DNS Interception)$/ })
    ).toHaveCount(0);

    await guard.assertClean();
  });

  test(`dns-legacy: слой выключен — переключатель по прежнему условию (${kernel})`, async ({
    page
  }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    test.skip(!installed[kernel], `${kernel} не установлен на стенде`);
    await startPage(page);
    await setLayer(page, false);
    await page.reload({ waitUntil: 'domcontentloaded' });
    const dnsRedirected = await xkeenDns(page);

    await openConstructorDns(page, kernel);
    if (kernel === 'xray') await ensureXrayServer(page);
    // Прежнее условие показа: XKeen не перенаправляет DNS в ядро
    await expect(page.getByTestId(LEGACY)).toHaveCount(dnsRedirected === false ? 1 : 0, {
      timeout: T.action
    });

    await guard.assertClean();
  });
}

// --- D-36: подтверждение выключения флага слоя при перехвате ---

const FLAG_NOTE = /Перехват DNS будет выключен|DNS interception will be disabled/;
const ACTIVE_STATES = ['on', 'window', 'waiting'];

/** Что спек сам поменял на стенде ради перехвата (убирается в afterAll). */
const prep = { enabled: false, groupId: '', routingTaken: false, sectionKernel: '', nodeName: '' };

interface DnsBrief {
  state: string;
  desired: boolean;
  activeKernel: string;
  blocks: string[];
  failure: string;
  unavailableReason: string;
}

/** Состояние перехвата по настоящему API службы. */
async function dnsView(page: Page): Promise<DnsBrief> {
  const res = await api(page, 'GET', '/api/configlayer/dns');
  expect(res.status, 'GET /api/configlayer/dns').toBe(200);
  const d = res.data ?? {};
  const failure = (d.failure ?? null) as { code?: string } | null;
  return {
    state: String(d.state ?? ''),
    desired: d.desired === true,
    activeKernel: String(d.active_kernel ?? ''),
    blocks: (d.blocks ?? []) as string[],
    failure: failure?.code ?? '',
    unavailableReason: String(d.unavailable_reason ?? '')
  };
}

const takeoverActive = (v: DnsBrief): boolean => v.desired || ACTIVE_STATES.includes(v.state);

/**
 * Включает перехват настоящими маршрутами. Возвращает причину пропуска, если стенд не может
 * держать перехват (служба недоступна, ядро остановлено или конфликт ядер), иначе null.
 */
async function enableTakeover(page: Page): Promise<string | null> {
  let v = await dnsView(page);
  if (takeoverActive(v)) return null;
  if (v.state === 'unavailable') return `перехват недоступен на стенде: ${v.unavailableReason}`;
  if (v.activeKernel !== 'xray' && v.activeKernel !== 'mihomo') {
    return `активное ядро стенда: ${v.activeKernel || 'нет'}`;
  }
  if (v.blocks.includes('stopped') || v.blocks.includes('conflict')) {
    return `ядро не готово к перехвату: ${v.blocks.join(', ')}`;
  }
  if (v.blocks.includes('draft')) await resetDraft(page);

  await ensureDnsTarget(page, prep);

  v = await dnsView(page);
  expect(v.blocks, 'блокировки включения после подготовки').toEqual([]);
  const res = await api(page, 'POST', '/api/configlayer/dns/enable', {});
  expect(res.status, `включение перехвата (${String(res.data?.code ?? '')})`).toBe(200);
  prep.enabled = true;

  await expect
    .poll(
      async () => {
        const now = await dnsView(page);
        if (now.state === 'off' && now.failure) return `сбой: ${now.failure}`;
        return now.state;
      },
      { timeout: T.save * 4, intervals: [2000] }
    )
    .toMatch(/^(on|window)$/);
  if ((await dnsView(page)).state === 'window') {
    const keep = await api(page, 'POST', '/api/configlayer/dns/keep', {});
    expect(keep.status, 'оставить перехват').toBe(200);
  }
  return null;
}

/** Выключает перехват и убирает подготовку стенда (флаг слоя включён). */
async function cleanupTakeover(page: Page): Promise<void> {
  if (prep.enabled) {
    const res = await api(page, 'POST', '/api/configlayer/dns/disable', {});
    expect(res.status, 'выключение перехвата').toBe(200);
    await expect
      .poll(async () => (await dnsView(page)).state, { timeout: T.save * 4, intervals: [2000] })
      .toBe('off');
  }
  await releaseDnsTarget(page, prep);
  prep.enabled = false;
}

/** Открывает Настройки и снимает флаг слоя: диалог подтверждения на экране. */
async function openFlagOffDialog(page: Page): Promise<{ dialog: Locator; toggle: Locator }> {
  await page.goto('/#/settings', { timeout: T.page, waitUntil: 'domcontentloaded' });
  const row = page.getByTestId('config-layer-row');
  await expect(row).toBeVisible({ timeout: T.page });
  const toggle = row.getByRole('switch');
  await expect(toggle).toBeChecked();
  await row.locator('.toggle-slider').click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible({ timeout: T.action });
  return { dialog, toggle };
}

/** Отмена диалога: флаг слоя остаётся включённым. */
async function cancelFlagOff(page: Page, dialog: Locator, toggle: Locator): Promise<void> {
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(toggle).toBeChecked();
  const settings = await api(page, 'GET', '/api/settings');
  expect(settings.data?.config_layer, 'флаг слоя включён').toBe(true);
}

test('dns-legacy: без перехвата абзаца о DNS в выключении флага нет', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await setLayer(page, true);
  await page.reload({ waitUntil: 'domcontentloaded' });
  const v = await dnsView(page);
  test.skip(takeoverActive(v), `перехват на стенде уже в состоянии ${v.state}`);

  const { dialog, toggle } = await openFlagOffDialog(page);
  // Решение об абзаце принято до показа диалога: прежний вид без строки о перехвате
  await expect(dialog.getByTestId('dns-flag-off-note')).toHaveCount(0);
  await expect(dialog).not.toContainText(FLAG_NOTE);
  await cancelFlagOff(page, dialog, toggle);

  await guard.assertClean();
});

test(
  'dns-legacy: абзац о перехвате при выключении флага',
  {
    tag: CORE_TAG,
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'включение перехвата DNS при proxy_dns on с перезапуском ядра'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 12);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    await setLayer(page, true);
    await page.reload({ waitUntil: 'domcontentloaded' });
    const skip = await enableTakeover(page);
    test.skip(skip !== null, skip ?? '');

    const { dialog, toggle } = await openFlagOffDialog(page);
    const note = dialog.getByTestId('dns-flag-off-note');
    await expect(note).toBeVisible({ timeout: T.action });
    await expect(note).toHaveClass(/warning-text/);
    await expect(note).toContainText(FLAG_NOTE);
    // Действие не блокируется: кнопка подтверждения доступна
    await expect(dialog.getByRole('button').last()).toBeEnabled();
    await cancelFlagOff(page, dialog, toggle);
    expect(takeoverActive(await dnsView(page)), 'перехват остался включённым').toBe(true);

    await guard.assertClean();
  }
);
