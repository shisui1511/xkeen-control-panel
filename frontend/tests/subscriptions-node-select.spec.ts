// e2e-pages: subscriptions
import { test, expect, type Page, type Route } from '@playwright/test';

async function disableServiceWorker(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
    window.localStorage.setItem('lang', 'ru');
  });
}

async function mockBaseEndpoints(page: Page) {
  await page.route('**/api/**', async (route: Route) => {
    const url = route.request().url();
    if (url.includes('/api/auth/me')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          authenticated: true,
          setup_required: false,
          csrf_token: 'mock-csrf-token'
        })
      });
    } else if (url.includes('/api/capabilities')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            kernels: {
              xray: { installed: true, version: '1.8.24', channel: 'stable' },
              mihomo: { installed: true, version: '1.18.10', channel: 'stable' }
            },
            active_kernel: 'xray',
            mihomo: { api_reachable: true, process_running: true }
          }
        })
      });
    } else if (url.includes('/api/system/traffic/stats')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ total: { upload: 0, download: 0 } })
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    }
  });
}

const sub = {
  id: 'sub-1',
  name: 'Провайдер',
  url: 'https://example.com/sub',
  enabled: true,
  enable_xray: true,
  enable_mihomo: false,
  routing_mode: 'manual',
  proxy_count: 2
};

const nodes = [
  { tag: 'node-1', name: 'Узел 1', protocol: 'vless', active: false },
  { tag: 'node-2', name: 'Узел 2', protocol: 'vless', active: false }
];

test.describe('Subscriptions: выбор узла отправляет JSON-тело (Phase 133-04)', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await mockBaseEndpoints(page);
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([sub])
      });
    });
    await page.route('**/api/subscriptions/nodes?id=sub-1', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(nodes)
      });
    });
  });

  test('клик по узлу шлёт POST /api/subscriptions/active?id=sub-1 с телом {node_tag}', async ({
    page
  }) => {
    const requests: Array<{ method: string; url: string; body: unknown }> = [];
    await page.route('**/api/subscriptions/active**', async (route: Route) => {
      const req = route.request();
      requests.push({ method: req.method(), url: req.url(), body: req.postDataJSON() });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: { active_node: 'node-2' } })
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-1');
    await expect(card).toBeVisible();
    await card.locator('.collapse-toggle').click();

    const rows = card.locator('.sub-node-row');
    await expect(rows).toHaveCount(2);
    await rows.nth(1).locator('.sub-node-select-btn').click();

    await expect.poll(() => requests.length).toBe(1);
    expect(requests[0].method).toBe('POST');
    expect(requests[0].url).toContain('/api/subscriptions/active?id=sub-1');
    expect(requests[0].url).not.toContain('tag=');
    expect(requests[0].body).toEqual({ node_tag: 'node-2' });
  });

  test('конфликт 409 от сервера показывается ошибкой, а не успехом', async ({ page }) => {
    await page.route('**/api/subscriptions/active**', async (route: Route) => {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ success: false, error: 'cannot select a provider stub node' })
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-1');
    await expect(card).toBeVisible();
    await card.locator('.collapse-toggle').click();
    await card.locator('.sub-node-row').nth(0).locator('.sub-node-select-btn').click();

    await expect(
      page.locator('.toast').filter({ hasText: 'cannot select a provider stub node' })
    ).toBeVisible();
  });
});
