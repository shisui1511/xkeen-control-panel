// e2e-pages: dashboard
import { test, expect } from '@playwright/test';
import { T } from './lib/env';

// Трассер: одна страница настоящей панели цели. Обход всех страниц — отдельным планом.
test('page:dashboard theme:light vp:1440', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => {
    localStorage.setItem('theme', 'light');
  });

  await page.goto('/#/dashboard');
  await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
    timeout: T.page
  });
});
