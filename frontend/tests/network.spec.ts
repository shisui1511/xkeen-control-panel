import { test, expect, type Page, type Route } from '@playwright/test';

async function disableServiceWorker(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });
}

async function setupRestMocks(page: Page) {
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
    } else if (url.includes('/api/mihomo/proxy/proxies')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          proxies: {
            'US-01': { type: 'Shadowsocks' },
            'HK-02': { type: 'Vless' },
            'PROXIES-GROUP': { type: 'Selector' }
          }
        })
      });
    } else if (url.includes('/api/network/proxy-test')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          delay: 150,
          output: 'Proxy: US-01\nTarget URL: https://www.google.com\nDelay: 150 ms\nStatus: Reachable'
        })
      });
    } else if (url.includes('/api/network/port-check')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          rtt_ms: 15,
          output: 'Host: vpn.server.com\nPort: 443\nStatus: Open\nRTT: 15 ms'
        })
      });
    } else if (url.includes('/api/network/ip')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          ip: '8.8.8.8'
        })
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    }
  });
}

test.describe('Network Tools E2E suite', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page);
    await page.goto('/#/network');
  });

  test('displays all 6 diagnostic cards in grid', async ({ page }) => {
    // Should display at least 6 tool cards
    const cards = page.locator('.nt-card');
    await expect(cards).toHaveCount(6);

    // Verify presence of original ones and new ones
    await expect(page.locator('h3:has-text("Ping")')).toBeVisible();
    await expect(page.locator('h3:has-text("Traceroute")')).toBeVisible();
    await expect(page.locator('h3:has-text("DNS")')).toBeVisible();
    await expect(page.locator('h3:has-text("HTTP Test")')).toBeVisible();
    await expect(page.locator('h3:has-text("Proxy Test")')).toBeVisible();
    await expect(page.locator('h3:has-text("Port Checker")')).toBeVisible();
  });

  test('executes Proxy Test successfully and displays result', async ({ page }) => {
    // Select proxy from dropdown
    const proxySelect = page.locator('#proxy-select');
    await expect(proxySelect).toBeVisible();
    await proxySelect.selectOption('US-01');

    // Select preset target URL
    const targetSelect = page.locator('#proxy-target');
    await expect(targetSelect).toBeVisible();
    await targetSelect.selectOption('https://www.google.com');

    // Trigger proxy delay test
    const runBtn = page.locator('.nt-card:has(h3:has-text("Proxy Test")) button.btn-primary');
    await expect(runBtn).toBeVisible();
    await runBtn.click();

    // Verify result card is shown with success and mock output
    const resultCard = page.locator('.card-tight');
    await expect(resultCard).toBeVisible({ timeout: 5000 });
    await expect(resultCard).toContainText('Proxy: US-01');
    await expect(resultCard).toContainText('Delay: 150 ms');
  });

  test('executes Port Checker successfully and displays RTT', async ({ page }) => {
    // Enter host
    const hostInput = page.locator('#port-host');
    await expect(hostInput).toBeVisible();
    await hostInput.fill('vpn.server.com');

    // Select port preset (e.g. 443)
    const preset443 = page.locator('button.chip:has-text("443 (HTTPS)")');
    await expect(preset443).toBeVisible();
    await preset443.click();

    // Verify input gets filled
    const portInput = page.locator('#port-number');
    await expect(portInput).toHaveValue('443');

    // Run port checker
    const runBtn = page.locator('.nt-card:has(h3:has-text("Port Checker")) button.btn-primary');
    await expect(runBtn).toBeVisible();
    await runBtn.click();

    // Verify result card displays open port and RTT
    const resultCard = page.locator('.card-tight');
    await expect(resultCard).toBeVisible({ timeout: 5000 });
    await expect(resultCard).toContainText('Host: vpn.server.com');
    await expect(resultCard).toContainText('RTT: 15 ms');
  });

  test('maintains localStorage history and refills form on click', async ({ page }) => {
    // Form filler helper: Port Checker
    await page.locator('#port-host').fill('my.server.org');
    await page.locator('#port-number').fill('80');
    await page.locator('.nt-card:has(h3:has-text("Port Checker")) button.btn-primary').click();

    // Verify History card is visible
    const historyBlock = page.locator('.card:has(h3:has-text("Test History"))');
    await expect(historyBlock).toBeVisible();

    // Confirm the test was appended to history list
    const historyRow = page.locator('.history-row');
    await expect(historyRow).toHaveCount(1);
    await expect(historyRow).toContainText('Port 80');

    // Clear form inputs manually
    await page.locator('#port-host').fill('');
    await page.locator('#port-number').fill('');

    // Click the history item to restore values
    await historyRow.click();
    await expect(page.locator('#port-host')).toHaveValue('my.server.org');
    await expect(page.locator('#port-number')).toHaveValue('80');
  });
});
