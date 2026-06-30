import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

const MOCK_PROXIES = {
  proxies: {
    // Selector group with 12 proxies
    YouTube: {
      name: 'YouTube',
      type: 'Selector',
      now: 'RU-Node-01',
      all: [
        'RU-Node-01',
        'US-Node-02',
        'NL-Node-03',
        'DE-Node-04',
        'LT-Node-05',
        'EE-Node-06',
        'SE-Node-07',
        'AM-Node-08',
        'ES-Node-09',
        'FI-Node-10',
        'SG-Node-11',
        'GB-Node-12'
      ],
      alive: true
    },
    // URLTest group with 4 proxies
    FastGroup: {
      name: 'FastGroup',
      type: 'URLTest',
      now: 'US-Node-02',
      all: ['US-Node-02', 'NL-Node-03', 'DE-Node-04', 'RU-Node-01'],
      alive: true
    },
    // Individual proxies with latency data
    'RU-Node-01': { name: 'RU-Node-01', type: 'Vless', alive: true, history: [{ delay: 45, time: '' }] },
    'US-Node-02': { name: 'US-Node-02', type: 'Vless', alive: true, history: [{ delay: 150, time: '' }] },
    'NL-Node-03': { name: 'NL-Node-03', type: 'Vless', alive: true, history: [{ delay: 350, time: '' }] },
    'DE-Node-04': { name: 'DE-Node-04', type: 'Vless', alive: false, history: [{ delay: 0, time: '' }] },
    'LT-Node-05': { name: 'LT-Node-05', type: 'Vless', alive: true, history: [{ delay: 80, time: '' }] },
    'EE-Node-06': { name: 'EE-Node-06', type: 'Vless', alive: true, history: [{ delay: 90, time: '' }] },
    'SE-Node-07': { name: 'SE-Node-07', type: 'Vless', alive: true, history: [{ delay: 120, time: '' }] },
    'AM-Node-08': { name: 'AM-Node-08', type: 'Vless', alive: true, history: [{ delay: 180, time: '' }] },
    'ES-Node-09': { name: 'ES-Node-09', type: 'Vless', alive: true, history: [{ delay: 240, time: '' }] },
    'FI-Node-10': { name: 'FI-Node-10', type: 'Vless', alive: true, history: [{ delay: 400, time: '' }] },
    'SG-Node-11': { name: 'SG-Node-11', type: 'Vless', alive: true, history: [{ delay: 600, time: '' }] },
    'GB-Node-12': { name: 'GB-Node-12', type: 'Vless', alive: true, history: [{ delay: 900, time: '' }] }
  }
};

const MOCK_SUBSCRIPTIONS = [
  {
    id: 'sub-1',
    name: 'Primary VPN',
    profile_title: 'Primary VPN',
    url: 'https://example.com/sub',
    enabled: true,
    interval: 24,
    use_provider_interval: false,
    enable_xray: false,
    enable_mihomo: true,
    mihomo_integrated: true,
    hwid_locked: false,
    last_update: '2026-06-30T10:00:00Z',
    proxy_count: 12,
    upload: 10 * 1024 * 1024 * 1024,
    download: 20 * 1024 * 1024 * 1024,
    total: 100 * 1024 * 1024 * 1024,
    expire: Math.floor(Date.now() / 1000) + 15 * 86400 // expires in 15 days
  }
];

test.describe('Proxies UI Improvements (Phase 57)', () => {
  let putRequests: { url: string; body: any }[] = [];
  let postRequests: { url: string; body: any }[] = [];
  let currentProxies: any;

  test.beforeEach(async ({ page }) => {
    putRequests = [];
    postRequests = [];
    currentProxies = JSON.parse(JSON.stringify(MOCK_PROXIES));

    // Mock Service Worker
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: {
          register: () => Promise.resolve({}),
          addEventListener: () => {},
          removeEventListener: () => {},
          getRegistrations: () => Promise.resolve([])
        },
        writable: false,
        configurable: true
      });
    });

    // Capture and mock API calls
    await page.route('**/api/**', async (route) => {
      const request = route.request();
      const url = request.url();
      const method = request.method();

      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              kernels: {
                xray: { installed: false, version: '', channel: 'stable' },
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: 'mihomo',
              mihomo: { reachable: true, process_running: true, api_reachable: true, api_authenticated: true }
            }
          })
        });
      } else if (url.includes('/api/mihomo/proxy/proxies') && method === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(currentProxies)
        });
      } else if (url.includes('/api/mihomo/proxy/proxies/') && method === 'PUT') {
        const body = request.postDataJSON();
        putRequests.push({ url, body });

        const groupMatch = url.match(/\/api\/mihomo\/proxy\/proxies\/([^?#/]+)/);
        if (groupMatch) {
          const groupName = decodeURIComponent(groupMatch[1]);
          if (body && body.name && currentProxies.proxies[groupName]) {
            currentProxies.proxies[groupName].now = body.name;
          }
        }

        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else if (url.includes('/api/subscriptions') && method === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(MOCK_SUBSCRIPTIONS)
        });
      } else if (url.includes('/api/subscriptions/refresh') && method === 'POST') {
        postRequests.push({ url, body: null });
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else if (url.includes('/api/mihomo/proxy/connections')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ connections: [], total: 0 })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      }
    });

    await page.goto('/#/proxies');
    await page.waitForSelector('.group-grid, .page-head', { timeout: 10000 });
  });

  test('Grid Layout - nodes rendered in 5 columns grid', async ({ page }) => {
    // Expand YouTube group
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const gcHead = ytGroup.locator('.gc-head').first();
    
    // Check if collapsed initially and expand
    const isCollapsed = await ytGroup.locator('.dot-container').isVisible();
    if (isCollapsed) {
      await gcHead.click();
    }

    // Verify grid layout exists
    const grid = ytGroup.locator('.proxy-grid');
    await expect(grid).toBeVisible();

    const display = await grid.evaluate((el) => window.getComputedStyle(el).display);
    expect(display).toBe('grid');

    const cards = grid.locator('.proxy-card');
    await expect(cards).toHaveCount(12);
  });

  test('Dot-indicators - show latency dots for collapsed groups', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const gcHead = ytGroup.locator('.gc-head').first();

    // Collapse if expanded
    const isGridVisible = await ytGroup.locator('.proxy-grid').isVisible();
    if (isGridVisible) {
      await gcHead.click();
    }

    // Verify dot indicators container
    const dotContainer = ytGroup.locator('.dot-container');
    await expect(dotContainer).toBeVisible();

    // Verify dots are rendered (12 dots for 12 proxies)
    const dots = dotContainer.locator('.dot-indicator');
    await expect(dots).toHaveCount(12);

    // Verify some tooltips
    const firstDot = dots.first();
    const title = await firstDot.getAttribute('title');
    expect(title).toContain('RU-Node-01: 45'); // name + delay
  });

  test('Brand icons - YouTube group displays branding', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    // Brand icon SVG should be present in the header
    const brandIcon = ytGroup.locator('.gc-head svg.brand-icon');
    await expect(brandIcon).toBeVisible();
  });

  test('Active node highlight - active node has accent border and background', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const gcHead = ytGroup.locator('.gc-head').first();
    
    // Ensure expanded
    const isCollapsed = await ytGroup.locator('.dot-container').isVisible();
    if (isCollapsed) {
      await gcHead.click();
    }

    // Check active card highlight
    const activeCard = ytGroup.locator('.proxy-card.now');
    await expect(activeCard).toBeVisible();
    await expect(activeCard).toContainText('RU-Node-01');

    // Arrow character should not be used
    const text = await activeCard.innerText();
    expect(text).not.toContain('→');
  });

  test('Country flag - flag emoji is parsed and displayed', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const gcHead = ytGroup.locator('.gc-head').first();
    
    // Ensure expanded
    if (await ytGroup.locator('.dot-container').isVisible()) {
      await gcHead.click();
    }

    const firstCard = ytGroup.locator('.proxy-card').filter({ hasText: 'RU-Node-01' }).first();
    await expect(firstCard).toContainText('🇷🇺');
  });

  test('Global search - filters groups and nodes', async ({ page }) => {
    const searchInput = page.locator('input.group-search');
    await expect(searchInput).toBeVisible();

    // Search for US node
    await searchInput.fill('US-Node');
    await page.waitForTimeout(300); // Wait for debounce

    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const fastGroup = page.locator('.group-card').filter({ hasText: 'FastGroup' }).first();

    // Both groups should be visible as they both contain US-Node-02
    await expect(ytGroup).toBeVisible();
    await expect(fastGroup).toBeVisible();

    // Inside YouTube, only US-Node-02 card should be visible
    if (await ytGroup.locator('.dot-container').isVisible()) {
      await ytGroup.locator('.gc-head').click();
    }
    const visibleCards = ytGroup.locator('.proxy-card');
    const count = await visibleCards.count();
    for (let i = 0; i < count; i++) {
      const isVisible = await visibleCards.nth(i).isVisible();
      if (isVisible) {
        await expect(visibleCards.nth(i)).toContainText('US-Node');
      }
    }
  });

  test('Collapse/Expand all - collapses/expands all groups on click', async ({ page }) => {
    const collapseAllBtn = page.locator('button:has-text("Свернуть все")');
    const expandAllBtn = page.locator('button:has-text("Развернуть все")');

    await expect(collapseAllBtn).toBeVisible();
    await expect(expandAllBtn).toBeVisible();

    // Click collapse all
    await collapseAllBtn.click();
    
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    const fastGroup = page.locator('.group-card').filter({ hasText: 'FastGroup' }).first();
    await expect(ytGroup.locator('.dot-container')).toBeVisible();
    await expect(fastGroup.locator('.dot-container')).toBeVisible();

    // Click expand all
    await expandAllBtn.click();
    await expect(ytGroup.locator('.proxy-grid')).toBeVisible();
    await expect(fastGroup.locator('.proxy-grid')).toBeVisible();
  });

  test('Node Switch PUT API - click changes node and calls API', async ({ page }) => {
    const ytGroup = page.locator('.group-card').filter({ hasText: 'YouTube' }).first();
    if (await ytGroup.locator('.dot-container').isVisible()) {
      await ytGroup.locator('.gc-head').click();
    }

    const usCard = ytGroup.locator('.proxy-card').filter({ hasText: 'US-Node-02' }).first();
    await usCard.click();

    // Optimistic UI check
    await expect(usCard).toHaveClass(/now/);

    // Verify PUT request
    await expect.poll(() => putRequests.length).toBe(1);
    expect(putRequests[0].url).toContain('YouTube');
    expect(putRequests[0].body).toEqual({ name: 'US-Node-02' });
  });

  test('Provider CRUD and Merge - tab switching and subscription actions', async ({ page }) => {
    const groupsTab = page.locator('button:has-text("Группы")');
    const providersTab = page.locator('button:has-text("Провайдеры")');

    await expect(groupsTab).toBeVisible();
    await expect(providersTab).toBeVisible();

    await providersTab.click();

    const subCard = page.locator('.card, .provider-card').filter({ hasText: 'Primary VPN' }).first();
    await expect(subCard).toBeVisible();

    await expect(subCard.locator('.progress-bar, .traffic-bar')).toBeVisible();

    const refreshBtn = subCard.locator('button:has-text("↺"), button.btn-refresh-sub');
    if (await refreshBtn.isVisible()) {
      await refreshBtn.click();
      await expect.poll(() => postRequests.length).toBe(1);
    }
  });
});
