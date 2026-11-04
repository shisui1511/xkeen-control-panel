import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, installedKernels, withPanel, type Installed } from './lib/nodes-api';

// Стык со старым перенаправлением DNS XKeen (фаза 148, D-34): при включённом слое
// «Конфигурация» старого переключателя «Перенаправление DNS» нет в DNS-секциях
// конструкторов Xray и Mihomo; при выключенном слое он показан по прежнему условию
// (xkeen_dns === false и есть серверы у Xray, DNS включён у Mihomo). Спек меняет флаг слоя
// настоящим маршрутом и возвращает его прежним в afterAll; правки конструкторов остаются
// в браузере и на роутер не пишутся. Ответы сети не подменяются.

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
  await withPanel(browser, (page) => setLayer(page, layerWasOn));
});

/** Включает или выключает слой «Конфигурация» настоящим маршрутом настроек. */
async function setLayer(page: Page, on: boolean): Promise<void> {
  const settings = await api(page, 'GET', '/api/settings');
  expect(settings.status, 'GET /api/settings').toBe(200);
  if ((settings.data?.config_layer === true) === on) return;
  const res = await api(page, 'POST', '/api/settings/config-layer', { enabled: on });
  expect(res.status, `флаг слоя → ${on ? 'вкл' : 'выкл'}`).toBe(200);
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

    await openConstructorDns(page, kernel);
    if (kernel === 'xray') await ensureXrayServer(page);
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
