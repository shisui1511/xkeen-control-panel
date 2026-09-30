import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// На телефоне закрытое меню смещено за левый край. Оно не должно ни отбрасывать
// тень на экран, ни получать фокус; у открытого меню тень остаётся.

const THEMES = ['light', 'dark'] as const;

for (const theme of THEMES) {
  test.describe(`Закрытое мобильное меню (393x852, ${theme})`, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize({ width: 393, height: 852 });
      await setupMocks(page, 'mihomo');
      await page.addInitScript((t) => {
        localStorage.setItem('theme', t);
      }, theme);
      await visitPage(page, '/#/dashboard');
      await expect(page.locator('.mobile-header')).toBeVisible({ timeout: 15000 });
    });

    const sidebarShadow = (page: import('@playwright/test').Page) =>
      page.locator('.sidebar').evaluate((el) => getComputedStyle(el).boxShadow);

    test('закрытое меню без тени и с inert, открытое с тенью и без inert', async ({ page }) => {
      const sidebar = page.locator('.sidebar');

      await expect(sidebar).not.toHaveClass(/sidebar-open/);
      expect(await sidebarShadow(page)).toBe('none');
      await expect(sidebar).toHaveAttribute('inert', '');

      await page.locator('.burger-btn').click();
      await expect(sidebar).toHaveClass(/sidebar-open/);
      await expect(sidebar).not.toHaveAttribute('inert', '');
      expect(await sidebarShadow(page)).not.toBe('none');

      await page.keyboard.press('Escape');
      await expect(sidebar).not.toHaveClass(/sidebar-open/);
      await expect(sidebar).toHaveAttribute('inert', '');
      // Тень гаснет вместе с переходом transform: ждём итоговое значение
      await expect.poll(() => sidebarShadow(page)).toBe('none');
    });

    test('Tab с начала страницы не попадает в пункты закрытого меню', async ({ page }) => {
      await page.evaluate(() => {
        (document.activeElement as HTMLElement | null)?.blur();
        window.scrollTo(0, 0);
      });

      for (let i = 0; i < 25; i++) {
        await page.keyboard.press('Tab');
        const inSidebar = await page.evaluate(
          () => !!document.activeElement?.closest('.sidebar-nav, .sidebar')
        );
        expect(inSidebar, `шаг Tab ${i + 1}: фокус попал в закрытое меню`).toBe(false);
      }
    });
  });
}
