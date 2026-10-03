import { test, expect, type Page } from '@playwright/test';
import { fulfillServiceControl, kernelsFixture, setupMocks, visitPage } from './helpers/api-mocks';

// e2e-pages: #/ #/services #/proxies #/connections

// UPDUI-03: пока API Mihomo не отвечает (api_reachable: false), фронтенд не опрашивает
// /api/mihomo/proxy/*. Каждый такой запрос при остановленном ядре — 502 и строка в логе
// на слабом роутере. Реальное ожидание вместо page.clock: интервалы опросов — 10 и 30 с.

test.use({ locale: 'ru-RU' });
test.setTimeout(120_000);

const PROXY_URL = '/api/mihomo/proxy/';
const QUIET_WINDOW_MS = 26_000;

interface CapsFlags {
  apiReachable: boolean;
  processRunning: boolean;
}

/**
 * Поверх setupMocks: capabilities с изменяемыми флагами. Маршрут, зарегистрированный
 * позже, перехватывает раньше (формат ответа тот же, что в api-mocks).
 */
async function mockCapabilities(page: Page, flags: CapsFlags, delayMs = 0) {
  await page.route('**/api/capabilities', async (route) => {
    if (delayMs > 0) await new Promise((r) => setTimeout(r, delayMs));
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        success: true,
        data: {
          kernels: {
            xray: { installed: true, version: '1.8.4', channel: 'stable' },
            mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
          },
          active_kernel: 'mihomo',
          xkeen_installed: true,
          mihomo: {
            reachable: flags.apiReachable,
            process_running: flags.processRunning,
            api_reachable: flags.apiReachable,
            api_authenticated: flags.apiReachable
          }
        }
      })
    });
  });
}

/** Счётчик запросов к прокси ядра: массив меток времени (мс от старта теста). */
function trackProxyRequests(page: Page) {
  const hits: { at: number; url: string }[] = [];
  page.on('request', (req) => {
    if (req.url().includes(PROXY_URL)) hits.push({ at: Date.now(), url: req.url() });
  });
  return { hits };
}

test.describe('Гейт опросов Mihomo (UPDUI-03)', () => {
  test('дашборд при api_reachable:false не опрашивает /api/mihomo/proxy/* 26 с', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');
    await mockCapabilities(page, { apiReachable: false, processRunning: false });
    const { hits } = trackProxyRequests(page);

    await visitPage(page, '/#/');
    await expect(page.locator('main, #app').first()).toBeVisible();
    hits.length = 0; // считаем только после первой отрисовки
    await page.waitForTimeout(QUIET_WINDOW_MS);

    expect(hits.map((h) => h.url)).toEqual([]);
  });

  test('«Службы» при api_reachable:false не опрашивают /api/mihomo/proxy/* 26 с', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');
    await mockCapabilities(page, { apiReachable: false, processRunning: false });
    const { hits } = trackProxyRequests(page);

    await visitPage(page, '/#/services');
    await expect(page.locator('main, #app').first()).toBeVisible();
    hits.length = 0;
    await page.waitForTimeout(QUIET_WINDOW_MS);

    expect(hits.map((h) => h.url)).toEqual([]);
  });
  test('#/proxies до ответа capabilities: опроса нет, заглушки «оффлайн» нет; после ответа запрос уходит сразу', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');
    const flags: CapsFlags = { apiReachable: true, processRunning: true };
    await mockCapabilities(page, flags, 3000);
    const { hits } = trackProxyRequests(page);
    // Момент, когда браузер получил ответ capabilities (не обработчик маршрута)
    let capsResponseAt = 0;
    page.on('response', (res) => {
      if (!capsResponseAt && res.url().includes('/api/capabilities')) capsResponseAt = Date.now();
    });

    await page.goto('/#/proxies');
    await page.waitForTimeout(2500);

    // Ответа capabilities ещё нет: состояние unknown
    expect(capsResponseAt).toBe(0);
    // Одноразовая загрузка режима при открытии страницы (/proxy/configs) не опрос и не гейтуется;
    // опросы /proxy/proxies и /proxy/providers/proxies до ответа capabilities не идут
    expect(hits.map((h) => h.url).filter((u) => !u.endsWith('/api/mihomo/proxy/configs'))).toEqual(
      []
    );
    await expect(page.getByText('Mihomo API недоступен')).toHaveCount(0);

    const isPoll = (h: { url: string }) => h.url.endsWith('/api/mihomo/proxy/proxies');
    await expect.poll(() => hits.filter(isPoll).length, { timeout: 8000 }).toBeGreaterThan(0);
    expect(capsResponseAt).toBeGreaterThan(0);
    expect(hits.filter(isPoll)[0].at - capsResponseAt).toBeLessThan(3000);
  });

  test('#/proxies при api_reachable:false: 12 с тишины, запуск возобновляет опрос раньше такта 10 с', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');
    const flags: CapsFlags = { apiReachable: false, processRunning: false };
    await mockCapabilities(page, flags);
    let launchedAt = 0;
    await page.route('**/api/service/control**', async (route) => {
      if (route.request().method() === 'POST') {
        flags.apiReachable = true;
        flags.processRunning = true;
        launchedAt = Date.now();
      }
      // «Запустить Mihomo» = switch_kernel: клиент требует типизированный исход
      await fulfillServiceControl(route);
    });
    const { hits } = trackProxyRequests(page);

    await page.goto('/#/proxies');
    await expect(page.getByText('Mihomo API недоступен')).toBeVisible();
    hits.length = 0;
    await page.waitForTimeout(12_000);
    expect(hits.map((h) => h.url)).toEqual([]);

    await page.getByRole('button', { name: 'Запустить Mihomo' }).click();
    await expect.poll(() => hits.length, { timeout: 8000 }).toBeGreaterThan(0);
    expect(launchedAt).toBeGreaterThan(0);
    expect(hits.map((h) => h.url).some((u) => u.endsWith('/api/mihomo/proxy/proxies'))).toBe(true);
    expect(hits[0].at - launchedAt).toBeLessThan(5000);
  });

  test('«Службы»: после «Запустить» capabilities перечитываются сразу (D-10)', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const flags: CapsFlags = { apiReachable: false, processRunning: false };
    await mockCapabilities(page, flags);
    await page.route('**/api/kernels**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: kernelsFixture('mihomo', {
            mihomo: { process_status: 'stopped' }
          })
        })
      });
    });
    await page.route('**/api/service/status**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: { is_running: false, xkeen_installed: true, active_kernel: 'mihomo' }
        })
      });
    });
    let postDoneAt = 0;
    let capsAfterPostAt = 0;
    page.on('request', (req) => {
      if (postDoneAt && !capsAfterPostAt && req.url().includes('/api/capabilities')) {
        capsAfterPostAt = Date.now();
      }
    });
    await page.route('**/api/service/control**', async (route) => {
      if (route.request().method() === 'POST') {
        flags.apiReachable = true;
        flags.processRunning = true;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
      if (route.request().method() === 'POST') postDoneAt = Date.now();
    });

    await page.goto('/#/services');
    const start = page.getByTestId('hero-start');
    await expect(start).toBeEnabled();
    await start.click();

    await expect.poll(() => capsAfterPostAt, { timeout: 8000 }).toBeGreaterThan(0);
    expect(capsAfterPostAt - postDoneAt).toBeLessThan(2000);
  });

  test('connections tile shows a dash and the reason', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const flags: CapsFlags = { apiReachable: false, processRunning: false };
    await mockCapabilities(page, flags);

    await visitPage(page, '/#/');
    const value = page.getByTestId('dash-connections-value');
    const reason = page.getByTestId('dash-connections-offline');
    await expect(value).toContainText('—');
    await expect(reason).toHaveText('Mihomo не запущен');

    // Процесс поднялся, API ещё нет: подпись меняется на следующем опросе capabilities
    flags.processRunning = true;
    await expect(reason).toHaveText('Mihomo запущен, API не отвечает', { timeout: 14_000 });
    await expect(value).toContainText('—');
  });

  test('quick actions are disabled while the API is down', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const flags: CapsFlags = { apiReachable: false, processRunning: false };
    await mockCapabilities(page, flags);

    await visitPage(page, '/#/');
    const qa = page.locator('.quick-actions-widget .qa-btn');
    const latency = qa.filter({ hasText: 'Тест задержки' });
    const reset = qa.filter({ hasText: 'Сбросить сессии' });
    const refreshSubs = qa.filter({ hasText: 'Обновить подписки' });

    await expect(latency).toBeDisabled();
    await expect(reset).toBeDisabled();
    await expect(refreshSubs).toBeEnabled();
    await expect(latency).toHaveAttribute('title', 'Mihomo не запущен');
    await expect(page.getByTestId('qs-step3-reason')).toHaveText('Mihomo не запущен');

    // API поднялся: на следующем опросе capabilities кнопки оживают, подпись шага уходит
    flags.apiReachable = true;
    flags.processRunning = true;
    await expect(latency).toBeEnabled({ timeout: 14_000 });
    await expect(reset).toBeEnabled();
    await expect(page.getByTestId('qs-step3-reason')).toHaveCount(0);
  });

  test('connections websocket waits for the API', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const flags: CapsFlags = { apiReachable: false, processRunning: false };
    await mockCapabilities(page, flags);
    // Перехваченный routeWebSocket сокет не попадает в page.on('websocket'): считаем в обработчике
    const sockets: { closed: boolean }[] = [];
    await page.routeWebSocket('**/api/mihomo/connections/ws', (ws) => {
      const entry = { closed: false };
      sockets.push(entry);
      ws.onClose(() => (entry.closed = true));
      ws.send(JSON.stringify({ connections: [] }));
    });

    await visitPage(page, '/#/connections');
    await page.waitForTimeout(5000);
    expect(sockets).toHaveLength(0);

    // API ответил: сокет открывается сам на следующем опросе capabilities
    flags.apiReachable = true;
    flags.processRunning = true;
    await expect.poll(() => sockets.length, { timeout: 14_000 }).toBe(1);

    // API пропал: сокет закрывается, цикл переподключений не запускается (T-137-27)
    flags.apiReachable = false;
    flags.processRunning = false;
    await expect.poll(() => sockets[0].closed, { timeout: 14_000 }).toBe(true);
    await page.waitForTimeout(5000);
    expect(sockets).toHaveLength(1);
  });
});
