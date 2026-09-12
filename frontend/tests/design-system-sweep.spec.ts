import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// ============================================================
// Design System Sweep — Playwright-замер вычисленных стилей на
// маршрутах, уже мигрированных на единый словарь компонентов
// (PageHeader / Select / StatusBadge). Пополняется по мере
// миграции остальных 13 страниц следующими планами фазы 120.
// ============================================================

export const MIGRATED_ROUTES = ['/#/smartproxy'];

test.describe('Design System Sweep — мигрированные маршруты', () => {
  for (const route of MIGRATED_ROUTES) {
    test(`${route}: ровно один h1 с вычисленным font-size 22px`, async ({ page }) => {
      await setupMocks(page, 'mihomo');
      await visitPage(page, route);

      const h1Count = await page.locator('h1').count();
      expect(h1Count, `на ${route} ожидался ровно один h1`).toBe(1);

      const fontSize = await page
        .locator('h1')
        .first()
        .evaluate((el) => {
          return getComputedStyle(el).fontSize;
        });
      expect(fontSize, `h1 на ${route} должен иметь вычисленный font-size 22px`).toBe('22px');
    });

    test(`${route}: все нативные select находятся внутри .xcp-select`, async ({ page }) => {
      await setupMocks(page, 'mihomo');
      await visitPage(page, route);

      const bareSelectCount = await page.evaluate(() => {
        const selects = Array.from(document.querySelectorAll('select'));
        return selects.filter((el) => !el.closest('.xcp-select')).length;
      });
      expect(
        bareSelectCount,
        `на ${route} не должно быть нативных select вне обёртки .xcp-select`
      ).toBe(0);
    });

    test(`${route}: ни один элемент не несёт литеральный цвет в атрибуте style`, async ({
      page
    }) => {
      await setupMocks(page, 'mihomo');
      await visitPage(page, route);

      const literalColorCount = await page.evaluate(() => {
        const HEX_COLOR = /#[0-9a-fA-F]{3,8}\b/;
        const elements = Array.from(document.querySelectorAll('[style]'));
        return elements.filter((el) => HEX_COLOR.test(el.getAttribute('style') || '')).length;
      });
      expect(
        literalColorCount,
        `на ${route} не должно быть элементов с литеральным цветом в style=""`
      ).toBe(0);
    });
  }
});
