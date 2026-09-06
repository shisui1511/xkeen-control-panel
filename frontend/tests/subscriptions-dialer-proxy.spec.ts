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

test.describe('Subscriptions Dialer Proxy and Sockopt E2E tests (Plan 104-07)', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);

    // Mock base endpoints with universal fallback to avoid 401 redirects to '/' when real backend is running in CI
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
      } else if (url.includes('/api/proxies/')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ proxies: {} })
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
  });

  test('1. dialerProxy targets dropdown shows items from active subscriptions plus "none"', async ({
    page
  }) => {
    const mockSubs = [
      {
        id: 'sub-1',
        name: 'Primary Xray Sub',
        url: 'https://example.com/xray',
        enabled: true,
        enable_xray: true,
        enable_mihomo: false
      }
    ];

    const mockNodes = [
      {
        tag: 'node-alpha',
        name: 'Alpha Server',
        protocol: 'vless',
        active: false
      }
    ];

    const mockTargets = [
      {
        subscription_id: 'sub-1',
        subscription_name: 'Primary Xray Sub',
        tag: 'target-node-1',
        name: 'Target One'
      },
      {
        subscription_id: 'sub-2',
        subscription_name: 'Backup Xray Sub',
        tag: 'target-node-2',
        name: 'Target Two'
      }
    ];

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockSubs)
      });
    });

    await page.route('**/api/subscriptions/nodes?id=sub-1', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockNodes)
      });
    });

    await page.route('**/api/subscriptions/dialer-proxy-targets**', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: mockTargets
        })
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-1');
    await expect(card).toBeVisible();

    // Expand subscription
    const expandBtn = card.locator('.collapse-toggle');
    await expandBtn.click();

    // Verify select is visible in node row
    const dialerSelect = page.locator('[data-testid="dialer-proxy-select"]');
    await expect(dialerSelect).toBeVisible();

    // Check options
    const options = dialerSelect.locator('option');
    await expect(options).toHaveCount(3); // None + 2 targets
    await expect(options.nth(0)).toHaveText('Без каскада (прямое подключение)');
    await expect(options.nth(1)).toHaveText('Target One');
    await expect(options.nth(2)).toHaveText('Target Two (Backup Xray Sub)');
  });

  test('2. selecting target in dialerProxy dropdown sends POST /api/subscriptions/node-dialer-proxy', async ({
    page
  }) => {
    let capturedBody: any = null;
    let capturedQueryId: string | null = null;

    const mockSubs = [
      {
        id: 'sub-1',
        name: 'Primary Xray Sub',
        url: 'https://example.com/xray',
        enabled: true,
        enable_xray: true,
        enable_mihomo: false
      }
    ];

    const mockNodes = [
      {
        tag: 'node-alpha',
        name: 'Alpha Server',
        protocol: 'vless',
        active: false
      }
    ];

    const mockTargets = [
      {
        subscription_id: 'sub-2',
        subscription_name: 'Backup Xray Sub',
        tag: 'target-node-2',
        name: 'Target Two'
      }
    ];

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockSubs)
      });
    });

    await page.route('**/api/subscriptions/nodes?id=sub-1', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockNodes)
      });
    });

    await page.route('**/api/subscriptions/dialer-proxy-targets**', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: mockTargets
        })
      });
    });

    await page.route('**/api/subscriptions/node-dialer-proxy**', async (route: Route) => {
      const url = new URL(route.request().url());
      capturedQueryId = url.searchParams.get('id');
      capturedBody = route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-1');
    await expect(card).toBeVisible();
    await card.locator('.collapse-toggle').click();

    const dialerSelect = page.locator('[data-testid="dialer-proxy-select"]');
    await expect(dialerSelect).toBeVisible();

    // Select Target Two
    await dialerSelect.selectOption('target-node-2');

    // Verify request
    await expect.poll(() => capturedBody).not.toBeNull();
    expect(capturedQueryId).toBe('sub-1');
    expect(capturedBody).toEqual({
      node_tag: 'node-alpha',
      target_tag: 'target-node-2'
    });
  });

  test('3. conflict 409 from node-dialer-proxy displays chain limit toast error', async ({
    page
  }) => {
    const mockSubs = [
      {
        id: 'sub-1',
        name: 'Primary Xray Sub',
        url: 'https://example.com/xray',
        enabled: true,
        enable_xray: true,
        enable_mihomo: false
      }
    ];

    const mockNodes = [
      {
        tag: 'node-alpha',
        name: 'Alpha Server',
        protocol: 'vless',
        active: false
      }
    ];

    const mockTargets = [
      {
        subscription_id: 'sub-1',
        subscription_name: 'Primary Xray Sub',
        tag: 'target-node-chain',
        name: 'Chain Node'
      }
    ];

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockSubs)
      });
    });

    await page.route('**/api/subscriptions/nodes?id=sub-1', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockNodes)
      });
    });

    await page.route('**/api/subscriptions/dialer-proxy-targets**', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: mockTargets
        })
      });
    });

    await page.route('**/api/subscriptions/node-dialer-proxy**', async (route: Route) => {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'chain limited to one level' })
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-1');
    await expect(card).toBeVisible();
    await card.locator('.collapse-toggle').click();

    const dialerSelect = page.locator('[data-testid="dialer-proxy-select"]');
    await expect(dialerSelect).toBeVisible();

    await dialerSelect.selectOption('target-node-chain');

    // Expect error toast with chain limit message
    const toast = page.locator('.toast.error, .toast-error, [data-testid="toast-error"], .toast');
    await expect(toast.filter({ hasText: 'Превышена максимальная длина цепочки' })).toBeVisible();
  });

  test('4. wireguard node displays not-applicable state and hides health check button', async ({
    page
  }) => {
    const mockSubs = [
      {
        id: 'sub-wg',
        name: 'WireGuard Subscription',
        url: 'https://example.com/wg',
        enabled: true,
        enable_xray: true,
        enable_mihomo: false
      }
    ];

    const mockNodes = [
      {
        tag: 'wg-node-1',
        name: 'WireGuard Tunnel',
        protocol: 'wireguard',
        active: false
      }
    ];

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockSubs)
      });
    });

    await page.route('**/api/subscriptions/nodes?id=sub-wg', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockNodes)
      });
    });

    await page.route('**/api/subscriptions/dialer-proxy-targets**', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: [] })
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-wg');
    await expect(card).toBeVisible();
    await card.locator('.collapse-toggle').click();

    // Node is wireguard protocol
    const naBadge = page.locator('[data-testid="wireguard-check-na"]');
    await expect(naBadge).toBeVisible();
    await expect(naBadge).toHaveText('Проверка неприменима');

    // Ping button must not exist in this node's status container
    const pingBtn = page.locator('.sub-node-status-container .sub-node-ping-btn');
    await expect(pingBtn).not.toBeVisible();
  });

  test('5. edit modal for Xray subscription displays sockopt fields and submits in POST /api/subscriptions/update', async ({
    page
  }) => {
    let capturedUpdateBody: any = null;

    const mockSubs = [
      {
        id: 'sub-xray-sockopt',
        name: 'Xray Sub Sockopt',
        url: 'https://example.com/xray',
        enabled: true,
        enable_xray: true,
        enable_mihomo: false,
        sockopt_mark: 255,
        sockopt_fast_open: false,
        sockopt_mptcp: false
      }
    ];

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockSubs)
      });
    });

    await page.route('**/api/subscriptions/update**', async (route: Route) => {
      capturedUpdateBody = route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-xray-sockopt');
    await expect(card).toBeVisible();

    // Open edit modal directly via gear button
    await card.locator('.action-icon-btn[title="Редактировать"]').click();

    const modal = page.locator('.modal-container');
    await expect(modal).toBeVisible();

    // Sockopt group is present
    const sockoptGroup = modal.locator('[data-testid="sockopt-settings-group"]');
    await expect(sockoptGroup).toBeVisible();

    const markInput = modal.locator('[data-testid="sockopt-mark"]');
    await expect(markInput).toHaveValue('255');

    // Change mark to 500
    await markInput.fill('500');

    // Enable Fast Open and MPTCP
    await modal.locator('label[for="sockopt-fast-open"]').scrollIntoViewIfNeeded();
    await modal.locator('label[for="sockopt-fast-open"]').click();
    await modal.locator('label[for="sockopt-mptcp"]').click();

    // Save
    await modal.locator('button.btn-primary:has-text("Сохранить")').click();

    await expect.poll(() => capturedUpdateBody).not.toBeNull();
    expect(capturedUpdateBody.sockopt_mark).toBe(500);
    expect(capturedUpdateBody.sockopt_fast_open).toBe(true);
    expect(capturedUpdateBody.sockopt_mptcp).toBe(true);
  });

  test('6. subscription form modal with Xray disabled (Mihomo only) hides sockopt group', async ({
    page
  }) => {
    const mockSubs = [
      {
        id: 'sub-mihomo-only',
        name: 'Mihomo Only Sub',
        url: 'https://example.com/mihomo',
        enabled: true,
        enable_xray: false,
        enable_mihomo: true
      }
    ];

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockSubs)
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-mihomo-only');
    await expect(card).toBeVisible();

    await card.locator('.action-icon-btn[title="Редактировать"]').click();

    const modal = page.locator('.modal-container');
    await expect(modal).toBeVisible();

    // Sockopt group should NOT exist in DOM because formEnableXray is false
    const sockoptGroup = modal.locator('[data-testid="sockopt-settings-group"]');
    await expect(sockoptGroup).not.toBeVisible();
  });

  test('7. subscription node list with Xray disabled hides dialerProxy cascade selector', async ({
    page
  }) => {
    const mockSubs = [
      {
        id: 'sub-mihomo-nodes',
        name: 'Mihomo Sub',
        url: 'https://example.com/mihomo',
        enabled: true,
        enable_xray: false,
        enable_mihomo: true,
        mihomo_provider: { name: 'prov1' }
      }
    ];

    const mockNodes = [
      {
        tag: 'mihomo-node-1',
        name: 'Clash Server 1',
        alive: true,
        tested: true,
        delay_ms: 50
      }
    ];

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockSubs)
      });
    });

    await page.route('**/api/proxy-providers/prov1/nodes', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockNodes)
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-mihomo-nodes');
    await expect(card).toBeVisible();
    await card.locator('.collapse-toggle').click();

    // Node is rendered
    await expect(page.locator('.sub-node-row')).toBeVisible();

    // Dialer proxy selector container should NOT exist
    const dialerContainer = page.locator('[data-testid="dialer-proxy-container"]');
    await expect(dialerContainer).not.toBeVisible();
  });
});
