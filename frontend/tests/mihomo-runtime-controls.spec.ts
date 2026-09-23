import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

test.describe('Mihomo runtime controls', () => {
  test('mode switch asks for confirmation and patches the running core', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const patches: unknown[] = [];

    await page.route('**/api/mihomo/proxy/configs', async (route) => {
      if (route.request().method() === 'PATCH') {
        patches.push(route.request().postDataJSON());
        await route.fulfill({ status: 204, body: '' });
      } else {
        await route.fulfill({ json: { mode: 'rule', 'log-level': 'info' } });
      }
    });

    await visitPage(page, '/#/proxies');
    const direct = page.locator('.mode-switch').getByRole('button', { name: /Напрямую|Direct/ });
    const rule = page.locator('.mode-switch').getByRole('button', { name: /Правила|Rules/ });
    await expect(page.locator('.mode-switch')).toBeVisible();

    // Cancelling keeps the current mode and sends nothing.
    await direct.click();
    await page.locator('.confirm-actions').getByRole('button').first().click();
    await expect(rule).toHaveAttribute('aria-pressed', 'true');
    expect(patches).toHaveLength(0);

    // Confirming switches the core.
    await direct.click();
    await page.locator('.confirm-actions').getByRole('button').last().click();
    await expect.poll(() => patches).toEqual([{ mode: 'direct' }]);
    await expect(direct).toHaveAttribute('aria-pressed', 'true');

    // Back to rules does not need confirmation.
    await rule.click();
    await expect.poll(() => patches).toEqual([{ mode: 'direct' }, { mode: 'rule' }]);
  });

  test('rules page flushes the DNS cache', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    let flushed = 0;
    await page.route('**/api/mihomo/proxy/cache/dns/flush', async (route) => {
      flushed++;
      await route.fulfill({ status: 204, body: '' });
    });

    await visitPage(page, '/#/rules');
    await page.getByRole('button', { name: /Сбросить DNS-кэш|Flush DNS cache/ }).click();
    await expect.poll(() => flushed).toBe(1);
  });
});
