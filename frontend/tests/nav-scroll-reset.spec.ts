import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// Переход на другую страницу через меню открывает её сверху, а не с
// прокруткой предыдущей страницы.

test('переход по меню сбрасывает прокрутку окна', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 700 });
  await setupMocks(page, 'mihomo');
  await visitPage(page, '/#/proxies');

  // Гарантируем, что обе страницы длиннее окна, независимо от моков
  await page.addStyleTag({ content: 'body { min-height: 5000px; }' });
  await page.evaluate(() => window.scrollTo(0, 800));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(0);

  await page.locator('.sidebar a[href="#/dashboard"]').click();
  await expect(page).toHaveURL(/#\/dashboard/);
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
});

test('смена параметров внутри страницы не сбрасывает прокрутку', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 700 });
  await setupMocks(page, 'mihomo');
  await visitPage(page, '/#/proxies');

  await page.addStyleTag({ content: 'body { min-height: 5000px; }' });
  await page.evaluate(() => window.scrollTo(0, 800));
  await page.evaluate(() => (window.location.hash = '#/proxies?tab=providers'));
  await page.waitForTimeout(300);
  expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(0);
});
