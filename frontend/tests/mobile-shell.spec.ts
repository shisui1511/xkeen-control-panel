import { test, expect } from '@playwright/test';

test.describe('Mobile shell (Pixel 5)', () => {
  test.beforeEach(async ({ page }) => {
    // Отключаем Service Worker в тестах, чтобы запросы к API перехватывались через page.route
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Перехватываем все запросы к API
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();

      if (url.includes('/api/auth/me')) {
        // Мобильный каркас (шапка/drawer) живёт только внутри Dashboard, за auth-гейтом
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: true,
            setup_required: false,
            csrf_token: 'mock-csrf-token'
          })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  });

  test('geometry smoke: header is a full-width top strip, content is full-width, no horizontal scroll', async ({
    page
  }) => {
    await page.goto('/');
    await expect(page.locator('.mobile-header')).toBeVisible();

    const viewport = page.viewportSize();
    expect(viewport).not.toBeNull();
    const viewportWidth = viewport!.width;

    const headerBox = await page.locator('.mobile-header').boundingBox();
    expect(headerBox).not.toBeNull();
    expect(headerBox!.y).toBeLessThan(2);
    expect(headerBox!.x).toBeLessThan(2);
    expect(headerBox!.width).toBeGreaterThanOrEqual(viewportWidth - 2);
    expect(headerBox!.height).toBeLessThan(80);

    const contentBox = await page.locator('.main-content').boundingBox();
    expect(contentBox).not.toBeNull();
    expect(contentBox!.x).toBeLessThan(2);
    expect(contentBox!.width).toBeGreaterThanOrEqual(viewportWidth - 2);

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth
    );
    expect(overflow).toBeLessThanOrEqual(1);
  });

  test('drawer: burger opens sidebar, Escape closes it', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.mobile-header')).toBeVisible();

    const sidebar = page.locator('.sidebar');
    const overlay = page.locator('.sidebar-overlay');

    await expect(sidebar).not.toHaveClass(/sidebar-open/);

    await page.locator('.burger-btn').click();

    await expect(sidebar).toHaveClass(/sidebar-open/);
    await expect(sidebar).toBeInViewport();
    await expect(overlay).not.toHaveClass(/hidden/);

    await page.keyboard.press('Escape');

    await expect(sidebar).not.toHaveClass(/sidebar-open/);
    await expect(overlay).toHaveClass(/hidden/);
  });

  test('input no iOS-zoom: #password computed font-size is >= 16px', async ({ page }) => {
    // Переопределяем /api/auth/me на unauthenticated, чтобы отрендерился Login с #password
    await page.route('**/api/auth/me', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          authenticated: false,
          setup_required: false,
          csrf_token: 'mock-csrf-token'
        })
      });
    });

    await page.goto('/');
    await expect(page.locator('#password')).toBeVisible();

    const fontSize = await page
      .locator('#password')
      .evaluate((el) => parseFloat(getComputedStyle(el).fontSize));

    expect(fontSize).toBeGreaterThanOrEqual(16);
  });
});
