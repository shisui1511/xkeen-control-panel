import { test, expect, type Page } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// e2e-pages: #/ #/services #/proxies

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
          mihomo: {
            reachable: true,
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
  const t0 = Date.now();
  page.on('request', (req) => {
    if (req.url().includes(PROXY_URL)) hits.push({ at: Date.now() - t0, url: req.url() });
  });
  return { hits, t0 };
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
});
