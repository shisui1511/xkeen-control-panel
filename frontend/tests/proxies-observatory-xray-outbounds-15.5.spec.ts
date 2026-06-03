import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

// Mock data structures
const MOCK_CAPABILITIES_ONLINE = {
  kernels: {
    xray: { installed: true, version: '1.8.24', channel: 'stable' },
    mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
  },
  active_kernel: 'mihomo',
  mihomo: {
    reachable: true,
    process_running: true,
    api_reachable: true,
    api_authenticated: true
  }
};

const MOCK_CAPABILITIES_OFFLINE = {
  kernels: {
    xray: { installed: true, version: '1.8.24', channel: 'stable' },
    mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
  },
  active_kernel: 'mihomo',
  mihomo: {
    reachable: false,
    process_running: false,
    api_reachable: false,
    api_authenticated: false
  }
};

const MOCK_PROXIES_WITH_GROUPS = {
  proxies: {
    'SelectorGroup': {
      name: 'SelectorGroup',
      type: 'Selector',
      now: 'ss-node',
      all: ['ss-node', 'direct', 'block'],
      alive: true
    },
    'ss-node': {
      name: 'ss-node',
      type: 'Shadowsocks',
      alive: true,
      history: [{ delay: 100, time: '2026-06-04T00:00:00Z' }]
    }
  }
};

const MOCK_PROXIES_EMPTY = {
  proxies: {}
};

test.describe('Phase 15.5 Observatory & Xray Outbounds', () => {
  // Helper to disable service worker
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      window.localStorage.setItem('lang', 'ru');
    });
  });

  test('Observatory card is hidden when there are no proxy groups', async ({ page }) => {
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf' })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true, data: MOCK_CAPABILITIES_ONLINE }) });
      } else if (url.includes('/api/mihomo/proxy/proxies')) {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_PROXIES_EMPTY) });
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
      }
    });

    await page.goto('/#/proxies');
    await page.waitForLoadState('networkidle');

    // Observatory statistics header should NOT be visible
    const observatoryHeader = page.locator('h2.card-title').filter({ hasText: 'OBSERVATORY' });
    await expect(observatoryHeader).not.toBeVisible();
  });

  test('Observatory card is hidden when Mihomo is offline', async ({ page }) => {
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf' })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true, data: MOCK_CAPABILITIES_OFFLINE }) });
      } else if (url.includes('/api/mihomo/proxy/proxies')) {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_PROXIES_WITH_GROUPS) });
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
      }
    });

    await page.goto('/#/proxies');
    await page.waitForLoadState('networkidle');

    // Observatory statistics header should NOT be visible, and EmptyState for offline should be shown
    const observatoryHeader = page.locator('h2.card-title').filter({ hasText: 'OBSERVATORY' });
    await expect(observatoryHeader).not.toBeVisible();

    const offlineText = page.locator('text=Ядро Mihomo остановлено');
    await expect(offlineText).toBeVisible();
  });

  test('Observatory card is visible when there are groups and Mihomo is online', async ({ page }) => {
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf' })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true, data: MOCK_CAPABILITIES_ONLINE }) });
      } else if (url.includes('/api/mihomo/proxy/proxies')) {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_PROXIES_WITH_GROUPS) });
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
      }
    });

    await page.goto('/#/proxies');
    await page.waitForLoadState('networkidle');

    // Observatory statistics header should be visible
    const observatoryHeader = page.locator('h2.card-title').filter({ hasText: 'OBSERVATORY' });
    await expect(observatoryHeader).toBeVisible();
  });

  test('Xray select "Основной прокси-выход" is populated dynamically from config/list', async ({ page }) => {
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      const method = route.request().method();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf' })
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
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: 'xray'
            }
          })
        });
      } else if (url.includes('/api/config/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            { name: '04_outbounds.json', path: '/opt/etc/xray/configs/04_outbounds.json', size: 100 },
            { name: '04_outbounds.manual.json', path: '/opt/etc/xray/configs/04_outbounds.manual.json', size: 100 }
          ])
        });
      } else if (url.includes('/api/config/read')) {
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        if (path.includes('04_outbounds.manual.json')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              outbounds: [{ tag: 'manual-vless', protocol: 'vless' }]
            })
          });
        } else if (path.includes('04_outbounds.json')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              outbounds: [{ tag: 'auto-trojan', protocol: 'trojan' }]
            })
          });
        } else if (path.includes('05_routing.json')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              routing: {
                domainStrategy: 'IPIfNonMatch',
                rules: [
                  { type: 'field', network: 'tcp,udp', outboundTag: 'auto-trojan' }
                ]
              }
            })
          });
        } else {
          // Empty other config files
          await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
        }
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
      }
    });

    await page.goto('/#/constructor');
    // Switch to Xray tab in Constructor
    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Select tag options check
    const select = page.locator('#proxy-tag-select');
    await expect(select).toBeVisible();

    const options = select.locator('option');
    await expect(options).toHaveCount(2); // manual-vless and auto-trojan (system outbounds like 'direct', 'block', 'dns-out' are filtered out)
    
    const optionTexts = await options.allInnerTexts();
    expect(optionTexts).toContain('auto-trojan');
    expect(optionTexts).toContain('manual-vless');
  });

  test('Warning toast is displayed when invalid proxyTag is saved', async ({ page }) => {
    const savedFiles: Record<string, any> = {};

    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      const method = route.request().method();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf' })
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
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: 'xray'
            }
          })
        });
      } else if (url.includes('/api/config/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            { name: '04_outbounds.json', path: '/opt/etc/xray/configs/04_outbounds.json', size: 100 }
          ])
        });
      } else if (url.includes('/api/config/read')) {
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        if (path.includes('04_outbounds.json')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              outbounds: [{ tag: 'ss-node', protocol: 'shadowsocks' }]
            })
          });
        } else if (path.includes('05_routing.json')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              routing: {
                domainStrategy: 'IPIfNonMatch',
                rules: [
                  { type: 'field', network: 'tcp,udp', outboundTag: 'non-existent-tag' } // Invalid by default!
                ]
              }
            })
          });
        } else {
          await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
        }
      } else if (url.includes('/api/config/save') && method === 'POST') {
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        const body = route.request().postDataJSON();
        savedFiles[path] = body;
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
      }
    });

    await page.goto('/#/constructor');
    // Switch to Xray tab in Constructor
    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Trigger Apply Changes
    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible({ timeout: 5000 });
    await applyBtn.click();

    // Confirm dialog appears - click the primary button (Apply and Restart)
    const confirmBtn = page.locator('[data-testid="apply-confirm-dialog"] .btn-primary').first();
    await expect(confirmBtn).toBeVisible({ timeout: 3000 });
    await confirmBtn.click();

    // A warning toast should be shown
    const toast = page.locator('.toast.toast--warning');
    await expect(toast).toBeVisible({ timeout: 5000 });
    const toastText = await toast.innerText();
    expect(toastText).toContain('не найден в списке исходящих подключений');

    // But the configuration should still be saved
    expect(Object.keys(savedFiles).length).toBeGreaterThan(0);
  });
});
