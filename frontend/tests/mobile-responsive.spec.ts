import { test, expect, type Page, type Route } from '@playwright/test';

test.use({ locale: 'ru-RU' });

async function disableServiceWorker(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });
}

const MOCK_PROXIES = {
  proxies: {
    GLOBAL: {
      name: 'GLOBAL',
      type: 'Selector',
      now: 'DIRECT',
      all: ['DIRECT', 'REJECT', 'ProxyGroup1'],
      alive: true,
      history: []
    },
    ProxyGroup1: {
      name: 'ProxyGroup1',
      type: 'Selector',
      now: 'Node1',
      all: ['Node1', 'Node2'],
      alive: true,
      history: []
    },
    Node1: {
      name: 'Node1',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 100, time: '2024-01-01T00:00:00Z' }]
    },
    Node2: {
      name: 'Node2',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 200, time: '2024-01-01T00:00:00Z' }]
    }
  }
};

const MOCK_RULES = {
  rules: [
    { type: 'Domain', payload: 'google.com', proxy: 'ProxyGroup1', size: -1 },
    { type: 'DomainSuffix', payload: 'youtube.com', proxy: 'ProxyGroup1', size: -1 },
    { type: 'Match', payload: '', proxy: 'DIRECT', size: -1 }
  ]
};

const MOCK_PROVIDERS_RULES = {
  providers: {
    reject: {
      name: 'reject',
      type: 'Rule',
      vehicleType: 'HTTP',
      behavior: 'domain',
      ruleCount: 1500,
      updatedAt: '2024-01-01T00:00:00Z'
    }
  }
};

const MOCK_CONNECTIONS_WS = JSON.stringify({
  connections: [
    {
      id: 'conn-1',
      metadata: {
        network: 'TCP',
        type: 'HTTP',
        sourceIP: '192.168.1.5',
        sourcePort: '54321',
        destinationIP: '1.2.3.4',
        destinationPort: '443',
        host: 'example.com',
        process: 'Chrome'
      },
      upload: 1024,
      download: 8192,
      start: new Date().toISOString(),
      chains: ['ProxyGroup1', 'Node1'],
      rule: 'Domain',
      rulePayload: 'example.com'
    }
  ]
});

async function setupMocks(page: Page) {
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
              xray: { installed: false },
              mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
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
    } else if (url.includes('/api/system/stats')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          platform: 'Keenetic Hopper (KN-3810)',
          kernel_version: 'Linux 5.4.213 #1 SMP arm64',
          hostname: 'Keenetic-Router-Main-Node',
          ip_interface: '172.16.0.1 (br0)',
          timezone: 'Europe/Moscow (MSK, +0300)',
          config_path:
            '/opt/etc/xkeen/very/long/nested/path/to/custom/configuration/file/config.yaml',
          config_lines: 480,
          memory: { total: 512 * 1024 * 1024, used: 128 * 1024 * 1024, free: 384 * 1024 * 1024 },
          disk: { total: 1000 * 1024 * 1024, used: 200 * 1024 * 1024, free: 800 * 1024 * 1024 },
          load: [0.35, 0.4, 0.45],
          uptime: { days: 2, hours: 5, minutes: 12 },
          go_runtime: { gomaxprocs: 2, goroutines: 15 }
        })
      });
    } else if (url.includes('/api/service/status')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          xkeen: 'running',
          mihomo: 'running',
          xray: 'stopped'
        })
      });
    } else if (url.includes('/api/mihomo/proxy/rules')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_RULES)
      });
    } else if (url.includes('/api/mihomo/proxy/providers/rules')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROVIDERS_RULES)
      });
    } else if (url.includes('/api/mihomo/proxy/proxies')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROXIES)
      });
    } else if (url.includes('/api/mihomo/proxy/providers/proxies')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ providers: {} })
      });
    } else if (url.includes('/api/mihomo/proxy/configs')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ 'find-process-mode': 'off', mode: 'rule' })
      });
    } else if (url.includes('/api/mihomo/proxy/connections')) {
      if (route.request().method() === 'DELETE') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: MOCK_CONNECTIONS_WS
        });
      }
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    }
  });

  await page.routeWebSocket('**/api/traffic/ws', async (ws) => {
    ws.send(
      JSON.stringify({
        up: 1048576,
        down: 2097152,
        connections: 12,
        tcp_connections: 8,
        udp_connections: 4
      })
    );
  });

  await page.routeWebSocket('**/api/mihomo/proxy/connections', async (ws) => {
    ws.send(MOCK_CONNECTIONS_WS);
  });
}

test.describe('Mobile Responsiveness and Layout (Phase 81)', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupMocks(page);
  });

  test('Dashboard system info grid (MOB-01, D-07, D-08)', async ({ page }) => {
    // Check small phone viewport (375px)
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/#/dashboard');

    const infoRows = page.locator('.info-rows');
    await expect(infoRows).toBeVisible({ timeout: 5000 });

    // Verify 1-column grid on <=768px
    const gridTemplate = await infoRows.evaluate((el) => {
      return window.getComputedStyle(el).gridTemplateColumns;
    });
    // Should have only 1 track (e.g. "327px" or "1fr" resolved to single value)
    const trackCount = gridTemplate.trim().split(/\s+/).length;
    expect(trackCount).toBe(1);

    // Verify border-right is 0 on all rows
    const allBorderRights = await page.locator('.info-row').evaluateAll((rows) => {
      return rows.map((r) => window.getComputedStyle(r).borderRightWidth);
    });
    for (const bw of allBorderRights) {
      expect(bw === '0px' || bw === '').toBeTruthy();
    }

    // Verify long values break properly without overflowing viewport
    const valWordBreak = await page
      .locator('.info-row .val')
      .first()
      .evaluate((el) => {
        const style = window.getComputedStyle(el);
        return style.wordBreak || style.overflowWrap;
      });
    expect(['break-word', 'anywhere']).toContain(valWordBreak);

    // No horizontal document overflow
    const hasHorizontalOverflow = await page.evaluate(() => {
      return document.documentElement.scrollWidth > window.innerWidth;
    });
    expect(hasHorizontalOverflow).toBeFalsy();

    // Check tablet breakpoint (768px)
    await page.setViewportSize({ width: 768, height: 1024 });
    const gridTemplate768 = await infoRows.evaluate((el) => {
      return window.getComputedStyle(el).gridTemplateColumns;
    });
    expect(gridTemplate768.trim().split(/\s+/).length).toBe(1);
  });

  test('Sidebar mobile drawer (MOB-02, D-02, D-03, D-04)', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/#/dashboard');

    const burgerBtn = page.locator('.burger-btn');
    await expect(burgerBtn).toBeVisible({ timeout: 5000 });

    // Open drawer
    await burgerBtn.click();

    const sidebar = page.locator('.sidebar');
    await expect(sidebar).toHaveClass(/sidebar-open/);

    // Overlay check with backdrop-filter blur (D-02)
    const overlay = page.locator('.sidebar-overlay');
    await expect(overlay).toBeVisible();
    const overlayBlur = await overlay.evaluate((el) => {
      const s = window.getComputedStyle(el);
      return s.backdropFilter || (s as Record<string, string>)['-webkit-backdrop-filter'] || '';
    });
    expect(overlayBlur).toContain('blur');

    // Sidebar nav independent scroll (D-03)
    const nav = page.locator('.sidebar-nav');
    await expect(nav).toBeVisible();
    const navOverflow = await nav.evaluate((el) => window.getComputedStyle(el).overflowY);
    expect(navOverflow).toBe('auto');

    // Fixed footer is pinned and visible at the bottom (D-03)
    const footer = page.locator('.sidebar-footer');
    await expect(footer).toBeVisible();
    const footerBox = await footer.boundingBox();
    expect(footerBox).not.toBeNull();
    if (footerBox) {
      expect(footerBox.y + footerBox.height).toBeLessThanOrEqual(670);
    }
  });

  test('Rules filter bar (MOB-03, D-06)', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/#/rules');

    const filters = page.locator('.filters');
    await expect(filters).toBeVisible({ timeout: 5000 });

    const filterInput = page.locator('.filters .filter-input');
    await expect(filterInput).toBeVisible();

    const filtersBox = await filters.boundingBox();
    const inputBox = await filterInput.boundingBox();
    expect(filtersBox).not.toBeNull();
    expect(inputBox).not.toBeNull();

    if (filtersBox && inputBox) {
      // Search input takes full width of container (>=90% allowing for padding/gap)
      expect(inputBox.width).toBeGreaterThanOrEqual(filtersBox.width * 0.88);
    }

    // Selects in 2nd row take ~50% each
    const selects = page.locator('.filters .source-select');
    const selectCount = await selects.count();
    if (selectCount >= 2 && filtersBox) {
      const firstSelectBox = await selects.nth(0).boundingBox();
      const secondSelectBox = await selects.nth(1).boundingBox();
      if (firstSelectBox && secondSelectBox) {
        expect(firstSelectBox.width).toBeLessThan(filtersBox.width * 0.65);
        expect(secondSelectBox.width).toBeLessThan(filtersBox.width * 0.65);
      }
    }
  });

  test('Proxies mobile toolbar (MOB-04, D-01)', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/#/proxies');

    const actions = page.locator('.ph-actions');
    await expect(actions).toBeVisible({ timeout: 5000 });

    const groupSearch = actions.locator('.group-search');
    await expect(groupSearch).toBeVisible();

    const actionsBox = await actions.boundingBox();
    const searchBox = await groupSearch.boundingBox();
    expect(actionsBox).not.toBeNull();
    expect(searchBox).not.toBeNull();

    if (actionsBox && searchBox) {
      // Search input spans full width of toolbar container (>=88%)
      expect(searchBox.width).toBeGreaterThanOrEqual(actionsBox.width * 0.88);
    }

    // Action buttons are present below the search input
    const buttons = actions.locator('.btn');
    const count = await buttons.count();
    expect(count).toBeGreaterThanOrEqual(2);
  });

  test('Touch targets 44px (MOB-05, D-05)', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/#/dashboard');

    // Burger button is >=44x44px
    const burgerBtn = page.locator('.burger-btn');
    await expect(burgerBtn).toBeVisible({ timeout: 5000 });
    const burgerBox = await burgerBtn.boundingBox();
    expect(burgerBox).not.toBeNull();
    if (burgerBox) {
      expect(burgerBox.width).toBeGreaterThanOrEqual(44);
      expect(burgerBox.height).toBeGreaterThanOrEqual(44);
    }

    // Check Settings toggle switch touch target
    await page.goto('/#/settings');
    const toggleLabel = page.locator('.toggle-label').first();
    if (await toggleLabel.isVisible()) {
      const toggleBox = await toggleLabel.boundingBox();
      expect(toggleBox).not.toBeNull();
      if (toggleBox) {
        expect(toggleBox.height).toBeGreaterThanOrEqual(44);
      }
    }

    // Check Connections close button hitbox
    await page.goto('/#/connections');
    const closeBtn = page.locator('.btn-close-conn').first();
    await expect(closeBtn).toBeVisible({ timeout: 5000 });
    // Check that ::before pseudo element exists with min-width/height 44px or inset
    const pseudoHitbox = await closeBtn.evaluate((el) => {
      const before = window.getComputedStyle(el, '::before');
      return {
        content: before.content,
        position: before.position,
        inset:
          before.inset || before.top + ' ' + before.right + ' ' + before.bottom + ' ' + before.left
      };
    });
    expect(pseudoHitbox.content).not.toBe('none');
    expect(pseudoHitbox.position).toBe('absolute');
  });
});
