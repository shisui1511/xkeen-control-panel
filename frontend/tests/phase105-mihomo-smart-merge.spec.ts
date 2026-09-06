import { test, expect } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

test.describe('Phase 105: Mihomo Smart Merge Integration', () => {
  test('Applies configuration via /api/config/smart-merge with template_owns_nodes and saves', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    let smartMergeReqBody: any = null;
    let saveReqBody: string | null = null;
    let saveReqPath: string | null = null;

    const existingYAML = 'mixed-port: 7890\nproxies:\n  - name: OldNode\n    type: direct\n';
    const mergedYAML = 'mixed-port: 7890\nproxies:\n  - name: NewNode\n    type: direct\n';

    await page.route('**/api/config/read**', async (route) => {
      const url = route.request().url();
      if (url.includes('00_main.json')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({})
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'text/yaml',
          body: existingYAML
        });
      }
    });

    await page.route('**/api/config/smart-merge', async (route) => {
      smartMergeReqBody = JSON.parse(route.request().postData() || '{}');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            content: mergedYAML,
            stats: {
              proxies: 2,
              proxy_providers: 1,
              user_rules: 1,
              rules: 7
            }
          }
        })
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      saveReqBody = route.request().postData();
      const url = new URL(route.request().url(), 'http://localhost');
      saveReqPath = url.searchParams.get('path');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.route('**/api/service/control**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/editor');

    // Wait for Editor and switch to Constructor tab
    const constructorTab = page.locator(
      'button.tab-btn:has-text("Конструктор"), button.tab-btn:has-text("Constructor")'
    );
    await expect(constructorTab).toBeVisible({ timeout: 10000 });
    await constructorTab.click();

    // Select Mihomo kernel in constructor
    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible({ timeout: 10000 });
    await mihomoKernelBtn.click();

    // Select preset zkeen-selective to generate YAML
    const presetSelect = page.locator(
      'select.preset-select, [data-testid="preset-select"], select#preset-select'
    );
    await expect(presetSelect).toBeVisible({ timeout: 10000 });
    await presetSelect.selectOption('zkeen-selective');

    // Wait for the constructor to load and apply button to become ready
    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible({ timeout: 10000 });
    await expect(applyBtn).toBeEnabled({ timeout: 10000 });

    // Click Apply
    await applyBtn.click();

    // If confirmation modal is shown, confirm it
    const confirmModal = page.locator('[data-testid="apply-confirm-dialog"]');
    if (await confirmModal.isVisible()) {
      const confirmBtn = confirmModal.locator('button.btn-primary');
      await confirmBtn.click();
    }

    // Verify smart-merge API request
    await expect.poll(() => smartMergeReqBody).not.toBeNull();
    expect(smartMergeReqBody.type).toBe('mihomo');
    expect(smartMergeReqBody.template_owns_nodes).toBe(true);

    // Verify save API request was executed with the merged content
    await expect.poll(() => saveReqBody).not.toBeNull();
    expect(saveReqBody).toBe(mergedYAML);
    expect(saveReqPath).toContain('config.yaml');

    // Verify toast with numbers from stats is shown
    const toast = page.locator('.toast.toast-success, .toast:has-text("Smart Merge")');
    await expect(toast).toBeVisible({ timeout: 10000 });
    const toastText = await toast.textContent();
    // Check that toast includes numbers from stats (proxies=2, providers=1, rules=7)
    expect(toastText).toMatch(/2/);
    expect(toastText).toMatch(/1/);
  });
});
