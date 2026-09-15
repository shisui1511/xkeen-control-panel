import { test, expect } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

test.describe('Phase 123: Routing Center & Exception Rules', () => {
  let customRulesStore: any[] = [];

  test.beforeEach(async ({ page }) => {
    customRulesStore = [
      {
        id: 'r1',
        type: 'domain_suffix',
        value: 'rutracker.org',
        target: 'proxy',
        group: 'PROXY',
        comment: 'Tracker',
        enabled: true
      },
      {
        id: 'r2',
        type: 'domain',
        value: 'special.internal',
        target: 'direct',
        comment: 'Local direct',
        enabled: true
      }
    ];

    await setupMocks(page, 'mihomo');

    // Mock custom rules endpoint
    await page.route('**/api/rules/custom', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: customRulesStore })
        });
      } else if (route.request().method() === 'POST') {
        const body = JSON.parse(route.request().postData() || '{}');
        if (body.rules) {
          customRulesStore = body.rules;
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: { applied: true, reloaded: true, count: customRulesStore.length }
          })
        });
      } else {
        await route.continue();
      }
    });

    // Mock route test endpoint
    await page.route('**/api/rules/test', async (route) => {
      const body = JSON.parse(route.request().postData() || '{}');
      const target = body.target || 'rutracker.org';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            target: target,
            matched: true,
            rule_type: 'DOMAIN-SUFFIX',
            rule_payload: 'rutracker.org',
            target_action: 'PROXY',
            target_group: 'PROXY',
            selected_proxy: 'NL-Amsterdam-01',
            proxy_type: 'Vmess',
            trace_time_ms: 1.45,
            source: 'user'
          }
        })
      });
    });

    // Mock kernel rules endpoint
    await page.route('**/api/mihomo/proxy/rules', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          rules: [
            { type: 'DOMAIN-SUFFIX', payload: 'rutracker.org', proxy: 'PROXY' },
            { type: 'IP-CIDR', payload: '192.168.1.0/24', proxy: 'DIRECT' },
            { type: 'GEOIP', payload: 'telegram', proxy: 'PROXY' },
            { type: 'MATCH', payload: '', proxy: 'DIRECT' }
          ]
        })
      });
    });

    // Mock rule providers endpoint
    await page.route('**/api/mihomo/proxy/providers/rules', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          providers: {
            antifilter: {
              name: 'antifilter',
              type: 'http',
              behavior: 'domain',
              ruleCount: 12500,
              updatedAt: new Date().toISOString(),
              vehicleType: 'HTTP'
            }
          }
        })
      });
    });

    // Mock fakeip flush endpoint
    await page.route('**/api/mihomo/cache/fakeip/flush', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    // Mock proxies for groups
    await page.route('**/api/mihomo/proxy/proxies', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          proxies: {
            GLOBAL: { type: 'Selector', all: ['PROXY', 'DIRECT'] },
            PROXY: { type: 'Selector', all: ['NL-Amsterdam-01', 'DE-Frankfurt-02'] }
          }
        })
      });
    });
  });

  test('Tab navigation renders all 4 tabs correctly', async ({ page }) => {
    await page.goto('/#/rules');

    // 1. Exceptions tab is active by default
    await expect(page.locator('[data-testid="tab-exceptions"]')).toHaveClass(/active/);
    await expect(page.locator('td.col-value:has-text("rutracker.org")')).toBeVisible({
      timeout: 5000
    });

    // 2. Providers tab
    await page.locator('[data-testid="tab-providers"]').click();
    await expect(page.locator('.provider-name:has-text("antifilter")')).toBeVisible({
      timeout: 5000
    });

    // 3. Diagnostic tab
    await page.locator('[data-testid="tab-diagnostic"]').click();
    await expect(page.locator('.diagnostic-input')).toBeVisible({ timeout: 5000 });

    // 4. All Kernel Rules tab
    await page.locator('[data-testid="tab-all_rules"]').click();
    await expect(page.locator('td.col-payload:has-text("192.168.1.0/24")')).toBeVisible({
      timeout: 5000
    });
  });

  test('Adding new custom rule via quick add form', async ({ page }) => {
    await page.goto('/#/rules');

    // Fill quick add input
    const input = page.locator('input[placeholder*="Домен"], input[placeholder*="Domain"]');
    await expect(input).toBeVisible();
    await input.fill('youtube.com');

    // Click quick add submit inside the form
    const submitBtn = page.locator('.quick-add-form button[type="submit"]');
    await expect(submitBtn).toBeEnabled();
    await submitBtn.click();

    // Verify new rule appears in table
    await expect(page.locator('td.col-value:has-text("youtube.com")')).toBeVisible({
      timeout: 5000
    });
  });

  test('Toggling rule activity switch updates rule state', async ({ page }) => {
    await page.goto('/#/rules');

    // Find the toggle checkbox and label for first rule
    const toggleLabel = page.locator('.toggle-switch').first();
    await expect(toggleLabel).toBeVisible();

    // Toggle off by clicking the toggle switch
    await toggleLabel.click();

    // Verify customRulesStore updated
    await expect.poll(() => customRulesStore[0]?.enabled).toBe(false);
  });

  test('Reordering rules using priority buttons', async ({ page }) => {
    await page.goto('/#/rules');

    // Wait for rules to load
    await expect(page.locator('td.col-value:has-text("rutracker.org")')).toBeVisible({
      timeout: 5000
    });

    // Click down arrow on the first rule
    const downArrow = page.locator('button.order-btn:has-text("▼")').first();
    await downArrow.click();

    // Second rule should now be first (special.internal)
    const firstRowValue = page.locator('tbody tr:first-child td.col-value');
    await expect(firstRowValue).toHaveText('special.internal');
  });

  test('Route Diagnostic tab traces route and displays 4-step flow', async ({ page }) => {
    await page.goto('/#/rules');

    // Switch to diagnostic tab
    await page.locator('[data-testid="tab-diagnostic"]').click();

    // Enter target host
    const input = page.locator('.diagnostic-input');
    await input.fill('rutracker.org');

    // Click run button
    const runBtn = page.locator('.diagnostic-form button[type="submit"]');
    await runBtn.click();

    // Verify 4-step flow
    await expect(page.locator('.flow-container')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('.result-header .target-highlight')).toHaveText('rutracker.org');
    await expect(page.locator('.node-name')).toHaveText('NL-Amsterdam-01');
    await expect(page.locator('.trace-latency')).toContainText('ms');
  });

  test('Flush Fake-IP button triggers cache flush with toast', async ({ page }) => {
    await page.goto('/#/rules');

    // Find Fake-IP flush button in page header
    const flushBtn = page.locator('button:has-text("Fake-IP")');
    await expect(flushBtn).toBeVisible();
    await flushBtn.click();

    // Toast should show success message
    const toast = page
      .locator('.toast, [role="status"], [role="alert"]')
      .filter({ hasText: /Fake-IP/i });
    await expect(toast).toBeVisible({ timeout: 5000 });
  });
});
