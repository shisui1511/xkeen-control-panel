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

  test('2a. чип срока на карточке Xray-подписки переводит единицы (дни) при next_update через 26 ч', async ({
    page
  }) => {
    const nextUpdateIso = new Date(Date.now() + 26 * 60 * 60 * 1000).toISOString();
    const mockSubs = [
      {
        id: 'sub-days',
        name: 'Days Sub',
        url: 'https://example.com/days',
        enabled: true,
        enable_xray: true,
        enable_mihomo: false,
        interval: 26,
        next_update: nextUpdateIso,
        refresh_interval_hours: 26
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

    const chip = page.locator('[data-testid="sub-next-update-chip"]');
    await expect(chip).toBeVisible();
    await expect(chip).toContainText('д');
  });

  test('2b. Mihomo-only подписка показывает подпись "Обновляет Mihomo" вместо таймера и не входит в срок шапки', async ({
    page
  }) => {
    const mockSubs = [
      {
        id: 'sub-mihomo-only',
        name: 'Mihomo Only Sub',
        url: 'https://example.com/mihomo-only',
        enabled: true,
        enable_xray: false,
        enable_mihomo: true,
        interval: 12,
        refresh_interval_hours: 12
        // без next_update — Mihomo-only подписки его не получают (D-13)
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

    const mihomoChip = page.locator('[data-testid="sub-mihomo-refresh-chip"]');
    await expect(mihomoChip).toBeVisible();
    await expect(mihomoChip).toContainText('Обновляет Mihomo');
    await expect(mihomoChip).toContainText('12 ч');

    await expect(page.locator('[data-testid="sub-next-update-chip"]')).toHaveCount(0);
    // Mihomo-only подписка без next_update не даёт шапке чип срока (D-13).
    await expect(page.locator('[data-testid="subs-next-update"]')).toHaveCount(0);
  });

  test('2c. без включённых Xray-подписок чип срока в шапке отсутствует (edge empty)', async ({
    page
  }) => {
    const mockSubs = [
      {
        id: 'sub-disabled-xray',
        name: 'Disabled Sub',
        url: 'https://example.com/disabled',
        enabled: false,
        enable_xray: true,
        enable_mihomo: false,
        interval: 24,
        next_update: new Date(Date.now() + 60 * 60 * 1000).toISOString()
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
    await expect(page.locator('#sub-card-sub-disabled-xray')).toBeVisible();
    await expect(page.locator('[data-testid="subs-next-update"]')).toHaveCount(0);
  });

  test('2d. порядок подписок не влияет на текст срока в шапке (edge ordering)', async ({
    page
  }) => {
    const nextUpdateIso = new Date(Date.now() + 45 * 60 * 1000).toISOString();
    const subA = {
      id: 'sub-order-a',
      name: 'Order A',
      url: 'https://example.com/order-a',
      enabled: true,
      enable_xray: true,
      enable_mihomo: false,
      interval: 24,
      next_update: nextUpdateIso
    };
    const subB = {
      id: 'sub-order-b',
      name: 'Order B',
      url: 'https://example.com/order-b',
      enabled: true,
      enable_xray: true,
      enable_mihomo: false,
      interval: 24,
      next_update: nextUpdateIso
    };

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([subA, subB])
      });
    });
    await page.goto('/#/subscriptions');
    const textForward = await page.locator('[data-testid="subs-next-update"]').textContent();

    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([subB, subA])
      });
    });
    await page.reload();
    const textReversed = await page.locator('[data-testid="subs-next-update"]').textContent();

    expect(textForward).toBe(textReversed);
  });

  test('2e. в тексте страницы нет сырых ключей subscr.stats.*', async ({ page }) => {
    const mockSubs = [
      {
        id: 'sub-raw-keys',
        name: 'Raw Keys Sub',
        url: 'https://example.com/raw-keys',
        enabled: true,
        enable_xray: true,
        enable_mihomo: false,
        interval: 26,
        next_update: new Date(Date.now() + 26 * 60 * 60 * 1000).toISOString(),
        refresh_interval_hours: 26
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
    await expect(page.locator('[data-testid="sub-next-update-chip"]')).toBeVisible();

    const bodyText = (await page.locator('body').textContent()) || '';
    expect(bodyText).not.toContain('subscr.stats.');
  });
});
