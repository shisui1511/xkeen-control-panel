// e2e-pages: config/cores
// router-area: dns
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { ensureDnsTarget, releaseDnsTarget } from './lib/dns-target';
import { CORE_TAG, SLOW_ANNOTATION, knownFailure } from './lib/known';
import { api, installedKernels, resetDraft, withPanel, type Installed } from './lib/nodes-api';

// Стык со старым перенаправлением DNS XKeen (фаза 148, D-33…D-35). Старого переключателя
// «Перенаправление DNS» в панели нет: прежний адрес конструктора #/constructor ведёт на вкладку
// секций ядер, и в DNS-карточке ядра нет ни блока, ни заглушки вместо него.
//
// Настоящий proxy_dns прошивки читается из вида перехвата (GET /api/configlayer/dns, поле
// legacy.proxy_dns): карточка перехвата показывает строку «будет выключено» при выключенном
// перехвате и предупреждение «Выключить старое перенаправление» при включённом перехвате ровно
// тогда, когда proxy_dns включён. Включение перехвата выключает proxy_dns в той же транзакции:
// после включения предупреждения нет. Перехват включается настоящими маршрутами службы; если
// стенд к нему не готов, секция DNS активного ядра берётся панелью и создаётся группа-цель
// «Прокси» (участник DIRECT). В afterAll перехват выключается и подготовка убирается. Ответы
// сети не подменяются.

const KERNELS = ['xray', 'mihomo'] as const;
type Kernel = (typeof KERNELS)[number];

const LEGACY = 'dns-legacy-redirect';

let installed: Installed = { xray: false, mihomo: false };

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await withPanel(browser, async (page) => {
    installed = await installedKernels(page);
  });
});

test.afterAll(async ({ browser }) => {
  await withPanel(browser, async (page) => {
    if (prep.enabled || prep.groupId || prep.routingTaken || prep.sectionKernel) {
      await cleanupTakeover(page);
    }
  });
});

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/**
 * Старых конструкторов нет: прежний адрес #/constructor ведёт на вкладку секций ядер
 * «Xray │ Mihomo» (таблица адресов D-10). Открывается карточка DNS ядра.
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
  // Карточка могла быть раскрыта заранее (openDnsForm): клик по раскрытой её свернёт
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true', { timeout: T.action });
  // Тело рендерится только у раскрытой карточки. Форма DNS ядра — положительная опора: без неё
  // проверки «нет» ниже проходят мгновенно, даже пока содержимое не отрисовано (WR-06)
  const body = page.getByTestId(`section-body-${kernel}-dns`);
  await expect(body).toBeVisible({ timeout: T.action });
  await expect(body.getByTestId(`section-form-${kernel}-dns`)).toBeVisible({ timeout: T.action });
}

for (const kernel of KERNELS) {
  test(`dns-legacy: старого переключателя нет (${kernel})`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    test.skip(!installed[kernel], `${kernel} не установлен на стенде`);
    await startPage(page);

    await openCoresDns(page, kernel);
    // Ни блока, ни заглушки вместо него (D-34)
    await expect(page.getByTestId(LEGACY)).toHaveCount(0);
    await expect(
      page.getByRole('button', { name: /^(Включить перехват DNS|Enable DNS Interception)$/ })
    ).toHaveCount(0);

    await guard.assertClean();
  });
}

// --- D-33, D-35: настоящий proxy_dns и предупреждение «Выключить старое перенаправление» ---

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
  proxyDns: boolean;
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
    unavailableReason: String(d.unavailable_reason ?? ''),
    proxyDns: (d.legacy as { proxy_dns?: boolean } | undefined)?.proxy_dns === true
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

/** Выключает перехват и убирает подготовку стенда. */
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

/** Открывает карточку перехвата на вкладке секций активного ядра; вид загрузился. */
async function openTakeoverCard(page: Page, kernel: Kernel): Promise<Locator> {
  await page.goto('/#/config/cores', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('cores-tab')).toBeVisible({ timeout: T.page });
  await page.getByTestId(`kernel-switch-${kernel}`).click();
  await expect(page.getByTestId(`cores-panel-${kernel}`)).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('dns-state')).toBeVisible({ timeout: T.page });
  return page.getByTestId('dns-takeover-card');
}

/**
 * Строки стыка в карточке соответствуют настоящему proxy_dns из вида перехвата: «будет
 * выключено» — перехват выключен и proxy_dns включён; предупреждение с кнопкой — перехват
 * включён (или ждёт подтверждения) и proxy_dns включён; иначе ни того, ни другого.
 */
async function expectLegacyNotes(card: Locator, v: DnsBrief): Promise<void> {
  const willOff = v.state === 'off' && v.proxyDns;
  const drift = (v.state === 'on' || v.state === 'window') && v.proxyDns;
  await expect(card.getByTestId('dns-legacy-note')).toHaveCount(willOff ? 1 : 0, {
    timeout: T.action
  });
  await expect(card.getByTestId('dns-legacy-drift')).toHaveCount(drift ? 1 : 0, {
    timeout: T.action
  });
  await expect(card.getByTestId('dns-legacy-off')).toHaveCount(drift ? 1 : 0);
}

test(
  'dns-legacy: строки стыка следуют настоящему proxy_dns',
  { tag: CORE_TAG },
  async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    const v = await dnsView(page);
    test.skip(v.state === 'unavailable', `перехват недоступен на стенде: ${v.unavailableReason}`);
    test.skip(
      v.activeKernel !== 'xray' && v.activeKernel !== 'mihomo',
      `активное ядро стенда: ${v.activeKernel || 'нет'}`
    );
    test.skip(v.state === 'applying', 'перехват в переходном состоянии');

    const card = await openTakeoverCard(page, v.activeKernel as Kernel);
    await expectLegacyNotes(card, await dnsView(page));

    await guard.assertClean();
  }
);

test(
  'dns-legacy: включение перехвата выключает proxy_dns без предупреждения',
  {
    tag: CORE_TAG,
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'включение перехвата DNS при proxy_dns on с перезапуском ядра'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 12);
    knownFailure('*', 'dns-legacy-takeover-probe-external-no-working-node');
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    const skip = await enableTakeover(page);
    test.skip(skip !== null, skip ?? '');

    const v = await dnsView(page);
    expect(takeoverActive(v), 'перехват включён').toBe(true);
    // Транзакция включения выключила proxy_dns (D-33): дрейфа и предупреждения нет. Перехват,
    // включённый на стенде до спека, мог получить proxy_dns мимо панели — тогда строки стыка
    // проверяются по фактическому значению
    if (prep.enabled) expect(v.proxyDns, 'proxy_dns после включения перехвата').toBe(false);
    const card = await openTakeoverCard(page, v.activeKernel as Kernel);
    await expectLegacyNotes(card, v);

    await guard.assertClean();
  }
);
