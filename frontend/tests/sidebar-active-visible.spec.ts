import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// В невысоком окне нижние пункты меню уходят под подвал сайдбара.
// Активный пункт должен докручиваться в видимую часть списка.

test('активный пункт меню виден в невысоком окне', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 560 });
  await setupMocks(page, 'mihomo');
  await visitPage(page, '/#/settings');

  const nav = page.locator('.sidebar-nav');
  const item = page.locator('.sidebar-nav a[href="#/settings"]');
  await expect(item).toHaveAttribute('aria-current', 'page');

  await expect
    .poll(async () => {
      const navBox = await nav.boundingBox();
      const itemBox = await item.boundingBox();
      if (!navBox || !itemBox) return false;
      return (
        itemBox.y >= navBox.y - 1 && itemBox.y + itemBox.height <= navBox.y + navBox.height + 1
      );
    })
    .toBe(true);
});
