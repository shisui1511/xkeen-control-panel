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

test.describe('Subscriptions: видимость выбора узла (Phase 133-06)', () => {
  let subs: Array<Record<string, unknown>> = [];
  let nodeLists: Record<string, Array<Record<string, unknown>>> = {};

  test.beforeEach(async ({ page }) => {
    subs = [];
    nodeLists = {};
    await disableServiceWorker(page);
    await mockBaseEndpoints(page);
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(subs)
      });
    });
    await page.route('**/api/subscriptions/nodes?id=*', async (route: Route) => {
      const id = new URL(route.request().url()).searchParams.get('id') || '';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(nodeLists[id] || [])
      });
    });
  });

  async function openCard(page: Page, id: string) {
    await page.goto('/#/subscriptions');
    const card = page.locator(`#sub-card-${id}`);
    await expect(card).toBeVisible();
    await card.locator('.collapse-toggle').click();
    return card;
  }

  test('дефолтный узел помечен «По умолчанию», «Снять выбор» шлёт POST и обновляет список', async ({
    page
  }) => {
    subs = [{ ...sub, is_default: true, selected_tag: 'node-1', stable_tag: 'xcp-sub-1' }];
    nodeLists['sub-1'] = [
      { tag: 'node-1', name: 'Узел 1', protocol: 'vless', active: true },
      { tag: 'node-2', name: 'Узел 2', protocol: 'vless', active: false }
    ];
    const requests: Array<{ method: string; url: string }> = [];
    await page.route('**/api/subscriptions/active/clear**', async (route: Route) => {
      const req = route.request();
      requests.push({ method: req.method(), url: req.url() });
      subs = [{ ...sub, is_default: false, selected_tag: '', stable_tag: '' }];
      nodeLists['sub-1'] = nodeLists['sub-1'].map((n) => ({ ...n, active: false }));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: { is_default: false } })
      });
    });

    const card = await openCard(page, 'sub-1');
    const badge = card.getByTestId('node-default-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('По умолчанию');
    await expect(badge).toHaveAttribute('title', /xcp-sub-1/);
    await expect(card.getByTestId('node-by-tag-badge')).toHaveCount(0);

    await card.getByTestId('node-clear-selection').click();

    await expect.poll(() => requests.length).toBe(1);
    expect(requests[0].method).toBe('POST');
    expect(requests[0].url).toContain('/api/subscriptions/active/clear?id=sub-1');
    await expect(page.locator('.toast').filter({ hasText: 'Выбор снят' })).toBeVisible();
    await expect(card.getByTestId('node-default-badge')).toHaveCount(0);
    await expect(card.getByTestId('node-clear-selection')).toHaveCount(0);
  });

  test('выбранный узел недефолтной подписки помечен «Только по тегу», кнопки снятия нет', async ({
    page
  }) => {
    subs = [
      {
        ...sub,
        id: 'sub-2',
        name: 'Второй',
        is_default: false,
        selected_tag: 'node-1',
        stable_tag: 'xcp-sub-2'
      }
    ];
    nodeLists['sub-2'] = [{ tag: 'node-1', name: 'Узел 1', protocol: 'vless', active: true }];

    const card = await openCard(page, 'sub-2');
    const badge = card.getByTestId('node-by-tag-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('xcp-sub-2');
    await expect(badge).toContainText('Только по тегу');
    await expect(card.getByTestId('node-default-badge')).toHaveCount(0);
    await expect(card.getByTestId('node-clear-selection')).toHaveCount(0);
  });

  test('last_warning=selected_node_lost показывает строку предупреждения на карточке', async ({
    page
  }) => {
    subs = [
      { ...sub, is_default: true, stable_tag: 'xcp-sub-1', last_warning: 'selected_node_lost' }
    ];
    nodeLists['sub-1'] = nodes;

    await page.goto('/#/subscriptions');
    const warning = page.locator('#sub-card-sub-1').getByTestId('sub-last-warning');
    await expect(warning).toBeVisible();
    await expect(warning).toContainText('Выбранный узел пропал');
  });

  test('без last_warning строки предупреждения нет', async ({ page }) => {
    subs = [{ ...sub }];
    nodeLists['sub-1'] = nodes;

    await page.goto('/#/subscriptions');
    await expect(page.locator('#sub-card-sub-1')).toBeVisible();
    await expect(page.getByTestId('sub-last-warning')).toHaveCount(0);
  });

  test('proxy_published=false в ответе выбора даёт тост «Тег proxy занят», не успех', async ({
    page
  }) => {
    subs = [{ ...sub }];
    nodeLists['sub-1'] = nodes.map((n) => ({ ...n }));
    await page.route('**/api/subscriptions/active?**', async (route: Route) => {
      subs = [{ ...sub, is_default: true, selected_tag: 'node-2', stable_tag: 'xcp-sub-1' }];
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            active_node: 'node-2',
            stable_tag: 'xcp-sub-1',
            is_default: true,
            proxy_published: false
          }
        })
      });
    });

    const card = await openCard(page, 'sub-1');
    await card.locator('.sub-node-row').nth(1).locator('.sub-node-select-btn').click();

    const toast = page.locator('.toast').filter({ hasText: 'Тег proxy занят' });
    await expect(toast).toBeVisible();
    await expect(toast).toContainText('xcp-sub-1');
  });

  test('proxy_tag_taken у дефолтной подписки показывает предупреждение на карточке', async ({
    page
  }) => {
    subs = [{ ...sub, is_default: true, stable_tag: 'xcp-sub-1', proxy_tag_taken: true }];
    nodeLists['sub-1'] = nodes;

    await page.goto('/#/subscriptions');
    const warning = page.locator('#sub-card-sub-1').getByTestId('sub-proxy-tag-taken');
    await expect(warning).toBeVisible();
    await expect(warning).toContainText('xcp-sub-1');
  });

  test('refresh с пропавшим выбранным узлом показывает тост-предупреждение', async ({ page }) => {
    subs = [{ ...sub, is_default: true, selected_tag: 'node-1', stable_tag: 'xcp-sub-1' }];
    nodeLists['sub-1'] = nodes;
    let refreshCalls = 0;
    await page.route('**/api/subscriptions/refresh?id=sub-1', async (route: Route) => {
      refreshCalls++;
      subs = [
        {
          ...sub,
          is_default: true,
          selected_tag: 'node-2',
          stable_tag: 'xcp-sub-1',
          last_warning: 'selected_node_lost'
        }
      ];
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-1');
    await expect(card).toBeVisible();
    await expect(page.getByTestId('sub-last-warning')).toHaveCount(0);
    await card.locator('button.action-icon-btn[title="Обновить"]').click();

    await expect.poll(() => refreshCalls).toBe(1);
    await expect(page.locator('.toast').filter({ hasText: 'Выбранный узел пропал' })).toBeVisible();
    await expect(page.getByTestId('sub-last-warning')).toBeVisible();
  });
});
