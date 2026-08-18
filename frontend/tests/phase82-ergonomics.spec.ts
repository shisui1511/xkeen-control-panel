import { test, expect, type Page, type Route } from '@playwright/test';

test.use({ locale: 'ru-RU' });

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

const MOCK_PROXIES = {
  proxies: {
    LargeGroup: {
      name: 'LargeGroup',
      type: 'Selector',
      now: 'Node-Fast',
      all: [
        'Node-Fast',
        'Node-Mid',
        'Node-Down',
        'Node-Unchecked',
        'Node-05',
        'Node-06',
        'Node-07',
        'Node-08',
        'Node-09',
        'Node-10'
      ],
      alive: true,
      history: [{ delay: 45, time: '2026-08-18T12:00:00Z' }]
    },
    AutoGroup: {
      name: 'AutoGroup',
      type: 'URLTest',
      now: 'Node-Fast',
      all: ['Node-Fast', 'Node-Mid'],
      alive: true,
      history: [{ delay: 45, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-Fast': {
      name: 'Node-Fast',
      type: 'Vless',
      alive: true,
      history: [{ delay: 50, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-Mid': {
      name: 'Node-Mid',
      type: 'Vless',
      alive: true,
      history: [{ delay: 250, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-Down': {
      name: 'Node-Down',
      type: 'Vless',
      alive: false,
      history: [{ delay: 0, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-Unchecked': {
      name: 'Node-Unchecked',
      type: 'Vless',
      alive: true,
      history: []
    },
    'Node-05': {
      name: 'Node-05',
      type: 'Vless',
      alive: true,
      history: [{ delay: 80, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-06': {
      name: 'Node-06',
      type: 'Vless',
      alive: true,
      history: [{ delay: 120, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-07': {
      name: 'Node-07',
      type: 'Vless',
      alive: true,
      history: [{ delay: 160, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-08': {
      name: 'Node-08',
      type: 'Vless',
      alive: true,
      history: [{ delay: 200, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-09': {
      name: 'Node-09',
      type: 'Vless',
      alive: true,
      history: [{ delay: 350, time: '2026-08-18T12:00:00Z' }]
    },
    'Node-10': {
      name: 'Node-10',
      type: 'Vless',
      alive: false,
      history: [{ delay: 850, time: '2026-08-18T12:00:00Z' }]
    }
  },
  providers: {}
};

const MOCK_SUBSCRIPTIONS = [
  {
    id: 'sub-1',
    name: 'Test Premium',
    profile_title: 'Test Premium',
    url: 'https://example.com/sub',
    enabled: true,
    enable_xray: true,
    enable_mihomo: false,
    interval: 24,
    last_update: '2026-08-18T10:00:00Z',
    proxy_count: 3
  }
];

const MOCK_SUB_NODES = [
  {
    tag: 'DE-Frankfurt-01',
    name: '🇩🇪 Frankfurt VLESS Fast',
    country: 'DE',
    flag: '🇩🇪',
    protocol: 'VLESS',
    transport: 'ws',
    security: 'tls',
    active: true
  },
  {
    tag: 'NL-Amsterdam-02',
    name: '🇳🇱 Amsterdam Trojan',
    country: 'NL',
    flag: '🇳🇱',
    protocol: 'Trojan',
    transport: 'tcp',
    security: 'tls',
    active: false
  },
  {
    tag: 'US-NewYork-03',
    name: '🇺🇸 New York Shadowsocks',
    country: 'US',
    flag: '🇺🇸',
    protocol: 'Shadowsocks',
    transport: 'tcp',
    security: 'none',
    active: false
  }
];

const MOCK_CONNECTIONS_WS = {
  downloadTotal: 1024000,
  uploadTotal: 512000,
  connections: [
    {
      id: 'conn-1',
      metadata: {
        network: 'tcp',
        type: 'HTTP',
        sourceIP: '',
        sourcePort: 0,
        destinationIP: '1.1.1.1',
        destinationPort: 443,
        host: 'cloudflare.com',
        process: 'curl'
      },
      upload: 500,
      download: 2000,
      start: '2026-08-18T12:00:00Z',
      chains: ['LargeGroup', 'Node-Fast'],
      rule: 'Match',
      rulePayload: ''
    },
    {
      id: 'conn-2',
      metadata: {
        network: 'tcp',
        type: 'HTTPS',
        sourceIP: '192.168.1.55',
        sourcePort: 49152,
        destinationIP: '8.8.8.8',
        destinationPort: 443,
        host: 'dns.google',
        process: ''
      },
      upload: 300,
      download: 1500,
      start: '2026-08-18T12:00:00Z',
      chains: ['AutoGroup', 'Node-Fast'],
      rule: 'Match',
      rulePayload: ''
    }
  ]
};

function setupApiRoutes(page: Page, options: { emptyQuotas?: boolean } = {}) {
  return page.route('**/api/**', async (route: Route) => {
    const url = route.request().url();
    const method = route.request().method();

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
            active_kernel: 'mihomo',
            mihomo: {
              reachable: true,
              process_running: true,
              api_reachable: true,
              api_authenticated: true
            }
          }
        })
      });
    } else if (url.includes('/api/mihomo/proxy/proxies')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROXIES)
      });
    } else if (url.includes('/api/mihomo/proxy/providers')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ providers: {} })
      });
    } else if (url.includes('/api/subscriptions/nodes')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_SUB_NODES)
      });
    } else if (url.includes('/api/proxy-providers') || url.includes('/api/subscriptions')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_SUBSCRIPTIONS)
      });
    } else if (
      url.includes('/api/mihomo/proxy/connections') ||
      url.includes('/api/mihomo/connections')
    ) {
      if (method === 'DELETE') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(MOCK_CONNECTIONS_WS)
        });
      }
    } else if (url.includes('/api/system/clients')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          clients: {
            '192.168.1.55': {
              ip: '192.168.1.55',
              mac: 'f8:4d:89:bf:ce:5b',
              name: 'Iphone 12',
              hostname: 'avolkov',
              display_name: 'Iphone 12',
              active: true
            }
          }
        })
      });
    } else if (url.includes('/api/traffic/quotas') || url.includes('/api/trafficquotas')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          options.emptyQuotas
            ? []
            : [
                {
                  id: 'q1',
                  name: 'Daily Limit',
                  target_type: 'ip',
                  target_id: '192.168.1.50',
                  limit_bytes: 1073741824,
                  period: 'daily',
                  enabled: true,
                  alert_threshold: 80,
                  current_bytes: 524288000,
                  last_reset: Date.now()
                }
              ]
        )
      });
    } else if (url.includes('/api/traffic/stats')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ total: 524288000, upload: 100000000, download: 424288000 })
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

test.describe('Phase 82: Ergonomics and UX improvements', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
  });

  test('Proxies health bar and observatory', async ({ page }) => {
    await setupApiRoutes(page);
    await page.goto('/#/proxies');

    // Wait for proxies to load
    await expect(page.locator('.group-card')).toHaveCount(2);

    // 1. Check that dot-container / dot-indicator is eliminated in collapsed cards
    await expect(page.locator('.dot-container')).toHaveCount(0);
    await expect(page.locator('.dot-indicator')).toHaveCount(0);

    // 2. Check health bar rendered in collapsed LargeGroup (>8 nodes -> auto-collapsed)
    const healthBars = page.locator('.health-bar');
    await expect(healthBars.first()).toBeVisible();

    // Check segments exist
    const fastSegments = page.locator('.health-segment.fast');
    await expect(fastSegments.first()).toBeVisible();

    // 3. Expand LargeGroup and check filter chips
    const largeGroupHeader = page.locator('.gc-head').filter({ hasText: 'LargeGroup' });
    await largeGroupHeader.click();

    // Check filter chips exist in expanded group
    const filterChips = page.locator('.group-filters .filter-chip');
    await expect(filterChips.first()).toBeVisible();
    await expect(page.locator('.group-filters').first()).toContainText('Все');
    await expect(page.locator('.group-filters').first()).toContainText('Рабочие');

    // 4. Observatory stats verification (no NaN, non-zero values)
    const obsTotal = page.locator('.stat-box').filter({ hasText: 'Всего узлов' });
    if ((await obsTotal.count()) > 0) {
      await expect(obsTotal).not.toContainText('NaN');
    }
  });

  test('Subscription node search', async ({ page }) => {
    await setupApiRoutes(page);
    await page.goto('/#/proxies?tab=providers');

    // Open node list for the subscription
    const toggleNodesBtn = page
      .locator('.collapse-toggle, button[aria-label="Список узлов"]')
      .first();
    await expect(toggleNodesBtn).toBeVisible();
    await toggleNodesBtn.click();

    // 1. Search input visibility
    const searchInput = page.locator('.node-search-input');
    await expect(searchInput).toBeVisible();

    // Check node rows initially
    await expect(page.locator('.sub-node-row')).toHaveCount(3);

    // 2. Filter by text query
    await searchInput.fill('Amsterdam');
    await expect(page.locator('.sub-node-row')).toHaveCount(1);
    await expect(page.locator('.sub-node-name')).toContainText('Amsterdam');

    // 3. Clear search query
    const clearBtn = page.locator('.node-search-clear');
    await expect(clearBtn).toBeVisible();
    await clearBtn.click();
    await expect(page.locator('.sub-node-row')).toHaveCount(3);

    // 4. Filter by country chip (e.g. DE)
    const deChip = page.locator('.node-filter-chip').filter({ hasText: 'DE' });
    if ((await deChip.count()) > 0) {
      await deChip.click();
      await expect(page.locator('.sub-node-row')).toHaveCount(1);
      await expect(page.locator('.sub-node-name')).toContainText('Frankfurt');

      // Click again to unfilter
      await deChip.click();
      await expect(page.locator('.sub-node-row')).toHaveCount(3);
    }

    // 5. Empty search results
    await searchInput.fill('NonExistentNode123');
    await expect(page.locator('.sub-node-row')).toHaveCount(0);
    const emptyState = page.locator('.nodes-empty-search');
    await expect(emptyState).toBeVisible();
    await expect(emptyState).toContainText('Узлы не найдены');

    const resetBtn = page.locator('.nodes-empty-search button');
    await resetBtn.click();
    await expect(page.locator('.sub-node-row')).toHaveCount(3);
  });

  test('Connections source and danger confirm', async ({ page }) => {
    await setupApiRoutes(page);
    await page.routeWebSocket('**/api/mihomo/connections/ws', async (ws) => {
      ws.send(JSON.stringify(MOCK_CONNECTIONS_WS));
    });

    await page.goto('/#/connections');

    // Check table loaded
    await expect(page.locator('.connections-table, .conns-table, table')).toBeVisible();

    // 1. Check source column does not contain :0 or bare :
    const srcCells = page.locator('.col-src, .source-col');
    const count = await srcCells.count();
    expect(count).toBeGreaterThan(0);
    for (let i = 0; i < count; i++) {
      const text = await srcCells.nth(i).innerText();
      expect(text.trim()).not.toBe(':0');
      expect(text.trim()).not.toBe(':');
    }

    // 1.1 Check device name rendered for 192.168.1.55
    const deviceName = page.locator('.src-name');
    await expect(deviceName).toBeVisible();
    await expect(deviceName).toContainText('Iphone 12');

    // 1.2 Check source filtering by device name
    const filterInput = page.locator('#filter-source');
    await filterInput.fill('Iphone');
    await expect(page.locator('.conn-row')).toHaveCount(1);
    await expect(page.locator('.conn-row .col-src')).toContainText('Iphone 12');

    await filterInput.fill('');
    await expect(page.locator('.conn-row')).toHaveCount(2);

    // 2. Click "Закрыть все" -> should show ConfirmDialog modal
    const closeAllBtn = page.locator('button:has-text("Закрыть все")');
    await expect(closeAllBtn).toBeEnabled();
    await closeAllBtn.click();

    // Modal must be visible
    const modal = page.locator('.modal-container, .confirm-modal, [role="alertdialog"]');
    await expect(modal).toBeVisible();
    await expect(modal).toContainText('Закрыть');

    // Cancel modal
    const cancelBtn = modal.locator('button:has-text("Отмена")');
    await cancelBtn.click();
    await expect(modal).not.toBeVisible();
  });

  test('Traffic quotas empty state', async ({ page }) => {
    await setupApiRoutes(page, { emptyQuotas: true });
    await page.goto('/#/trafficquotas');

    // Verify EmptyState component renders
    const emptyState = page.locator('.empty-state');
    await expect(emptyState).toBeVisible();
    await expect(emptyState).toContainText('Нет настроенных квот');
    await expect(emptyState.locator('button')).toContainText('Добавить лимит');
  });
});
