// e2e-pages: subscriptions
import { test, expect, type Page, type Route } from '@playwright/test';

async function disableServiceWorker(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
    window.localStorage.setItem('lang', 'ru');
  });
}

async function mockBaseEndpoints(page: Page) {
  await page.route('**/api/**', async (route: Route) => {
    const url = route.request().url();
    if (url.includes('/api/auth/me')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          authenticated: true,
          setup_required: false,
          csrf_token: 'mock-csrf-token'
        })
      });
    } else if (url.includes('/api/capabilities')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            kernels: {
              xray: { installed: true, version: '1.8.24', channel: 'stable' },
              mihomo: { installed: true, version: '1.18.10', channel: 'stable' }
            },
            active_kernel: 'xray',
            mihomo: { api_reachable: true, process_running: true }
          }
        })
      });
    } else if (url.includes('/api/system/traffic/stats')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ total: { upload: 0, download: 0 } })
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    }
  });
}

test.describe('Subscriptions metadata: имя, срок обновления, Mihomo-only (Phase 133-01)', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await mockBaseEndpoints(page);
  });

  test('1. шапка показывает срок из next_update, а не пересчитанный от interval (кейс B13)', async ({
    page
  }) => {
    // use_provider_interval=true, profile_update_hours=1, interval=24 — до
    // фикса D-12 фронт игнорировал use_provider_interval и считал срок от
    // interval (24ч), показывая «23 ч 59 мин» вместо «~1 ч».
    const nextUpdateIso = new Date(Date.now() + 59 * 60 * 1000).toISOString();
    const mockSubs = [
      {
        id: 'sub-b13',
        name: 'B13 Sub',
        url: 'https://example.com/b13',
        enabled: true,
        enable_xray: true,
        enable_mihomo: false,
        interval: 24,
        use_provider_interval: true,
        profile_update_hours: 1,
        next_update: nextUpdateIso,
        refresh_interval_hours: 1
      }
    ];

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockSubs)
      });
    });

    await page.goto('/#/subscriptions');

    // Чип срока в шапке — единственный .chip--icon в .stats-chips-row,
    // устойчив к появлению per-card чипов срока в Task 2 (другой контейнер).
    const headerChip = page.locator('.stats-chips-row .chip--icon');
    await expect(headerChip).toBeVisible();
    const text = (await headerChip.textContent()) || '';
    expect(text).toContain('мин');
    expect(text).not.toContain('23 ч');
  });
});
