import { test, expect } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

test.describe('Phase 101: Smart Merge, Remnawave & Custom Rules', () => {
  test.beforeEach(async ({ page }) => {
    await setupMocks(page, 'mihomo');

    await page.route('**/api/rules/custom', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            data: [
              {
                id: 'r1',
                type: 'domain_suffix',
                value: 'instagram.com',
                target: 'proxy',
                comment: 'Instagram',
                enabled: true
              }
            ]
          })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      }
    });

    await page.route('**/api/mihomo/proxy/rules', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          rules: [
            { type: 'DST-PORT', payload: '3389', proxy: 'DIRECT' },
            { type: 'DOMAIN-SUFFIX', payload: 'instagram.com', proxy: 'PROXY' },
            { type: 'MATCH', payload: '', proxy: 'DIRECT' }
          ]
        })
      });
    });

    await page.route('**/api/mihomo/proxy/providers/rules', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ providers: {} })
      });
    });
  });

  test('User Custom Rules tab displays rules and allows adding and toggling', async ({ page }) => {
    await page.goto('/#/rules');

    // Tab buttons must exist
    const customTab = page.locator(
      'button.tab-btn:has-text("Мои правила"), button.tab-btn:has-text("My Rules")'
    );
    await expect(customTab).toBeVisible({ timeout: 5000 });
    await customTab.click();

    // Custom rules table should contain the mocked rule
    const ruleValue = page.locator('td.mono:has-text("instagram.com")');
    await expect(ruleValue).toBeVisible({ timeout: 5000 });

    // Add rule form input
    const input = page.locator('input[placeholder*="Домен"], input[placeholder*="Domain"]');
    await expect(input).toBeVisible();
    await input.fill('facebook.com');

    const addBtn = page.locator(
      'button.btn-primary:has-text("Добавить правило"), button.btn-primary:has-text("Add Rule")'
    );
    await expect(addBtn).toBeEnabled();
    await addBtn.click();

    // Success toast or added rule
    await expect(page.locator('td.mono:has-text("facebook.com")')).toBeVisible({ timeout: 5000 });
  });
});
