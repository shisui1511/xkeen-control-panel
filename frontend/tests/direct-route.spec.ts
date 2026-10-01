import { test, expect, type Page } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

// Прямой переход на маршрут сразу показывает нужную страницу: дашборд не
// появляется в DOM даже на один кадр (иначе монтируются его виджеты и уходят
// лишние запросы).

async function watchDashboard(page: Page) {
  await page.addInitScript(() => {
    (window as unknown as { __dashboardSeen: boolean }).__dashboardSeen = false;
    const mark = () => {
      (window as unknown as { __dashboardSeen: boolean }).__dashboardSeen = true;
    };
    const observer = new MutationObserver(() => {
      if (document.querySelector('[data-testid="dashboard-page"]')) mark();
    });
    observer.observe(document, { subtree: true, childList: true });
  });
}

async function dashboardSeen(page: Page): Promise<boolean> {
  return page.evaluate(
    () => (window as unknown as { __dashboardSeen: boolean }).__dashboardSeen === true
  );
}

test.describe('прямой переход на маршрут', () => {
  test.beforeEach(async ({ page }) => {
    await setupMocks(page, 'mihomo');
    await watchDashboard(page);
  });

  test('#/settings: дашборд не появляется', async ({ page }) => {
    await page.goto('/#/settings');
    await expect(page.getByRole('tablist').first()).toBeVisible();
    await page.waitForTimeout(500);
    expect(await dashboardSeen(page)).toBe(false);
  });

  test('#/services: дашборд не появляется', async ({ page }) => {
    await page.goto('/#/services');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await page.waitForTimeout(500);
    expect(await dashboardSeen(page)).toBe(false);
  });

  test('#/subscriptions: переход на прокси без дашборда', async ({ page }) => {
    await page.goto('/#/subscriptions');
    await expect
      .poll(() => page.evaluate(() => window.location.hash))
      .toBe('#/proxies?tab=providers');
    await page.waitForTimeout(500);
    expect(await dashboardSeen(page)).toBe(false);
  });

  test('#/ — контроль наблюдателя: дашборд появляется', async ({ page }) => {
    await page.goto('/#/');
    await expect(page.locator('[data-testid="dashboard-page"]')).toBeVisible();
    expect(await dashboardSeen(page)).toBe(true);
  });
});
