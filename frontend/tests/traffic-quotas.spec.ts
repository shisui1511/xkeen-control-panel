import { test, expect, type Page, type Route } from '@playwright/test';

async function setupRestMocks(page: Page) {
  let mockQuotas = [
    {
      id: 'quota-1',
      name: 'Global Limit',
      target_type: 'global',
      target_id: '',
      limit_bytes: 10485760, // 10 MB
      period: 'monthly',
      enabled: true,
      alert_threshold: 80,
      action: 'block',
      current_bytes: 5242880, // 5 MB
      last_reset: Math.floor(Date.now() / 1000) - 3600
    }
  ];

  let mockStats = {
    proxies: [],
    total_upload: 1048576, // 1 MB
    total_download: 4194304, // 4 MB
    total: 5242880, // 5 MB
    reset_time: Math.floor(Date.now() / 1000) - 1200 // 20 mins ago (so forecast calculation works)
  };

  let mockAlerts: any[] = [];

  await page.route('**/api/**', async (route: Route) => {
    const url = route.request().url();
    const method = route.request().method();

    if (url.includes('/api/auth/me')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ authenticated: true, setup_required: false, csrf_token: 'mock' })
      });
    } else if (url.includes('/api/capabilities')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            kernels: { mihomo: { installed: true, version: '1.18.0', channel: 'stable' } },
            active_kernel: 'mihomo',
            mihomo: { reachable: true, process_running: true, api_reachable: true, api_authenticated: true }
          }
        })
      });
    } else if (url.includes('/api/traffic/quotas/add') && method === 'POST') {
      const q = route.request().postDataJSON();
      q.id = 'quota-added';
      mockQuotas.push(q);
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(q) });
    } else if (url.includes('/api/traffic/quotas/delete') && method === 'POST') {
      const id = new URL(url).searchParams.get('id');
      mockQuotas = mockQuotas.filter(q => q.id !== id);
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
    } else if (url.includes('/api/traffic/quotas/enabled') && method === 'POST') {
      const id = new URL(url).searchParams.get('id');
      const enabled = new URL(url).searchParams.get('enabled') === 'true';
      mockQuotas = mockQuotas.map(q => q.id === id ? { ...q, enabled } : q);
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
    } else if (url.includes('/api/traffic/quotas/reset') && method === 'POST') {
      const id = new URL(url).searchParams.get('id');
      mockQuotas = mockQuotas.map(q => q.id === id ? { ...q, current_bytes: 0 } : q);
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
    } else if (url.includes('/api/traffic/quotas')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mockQuotas) });
    } else if (url.includes('/api/traffic/stats')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mockStats) });
    } else if (url.includes('/api/traffic/alerts/clear') && method === 'POST') {
      mockAlerts = [];
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
    } else if (url.includes('/api/traffic/alerts')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mockAlerts) });
    } else {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) });
    }
  });
}

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

test.describe('Traffic Quotas page test suite', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page);
    await page.goto('/#/trafficquotas');
    await page.waitForSelector('h1:has-text("Лимиты трафика")');
  });

  test('displays banner and allows closing it', async ({ page }) => {
    const banner = page.locator('.info-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('Полезная информация');

    const closeBtn = page.locator('.info-banner-close');
    await closeBtn.click();
    await expect(banner).not.toBeVisible();
  });

  test('displays forecast card and values', async ({ page }) => {
    const forecastBox = page.locator('.stat-box:has-text("Прогноз на месяц")');
    await expect(forecastBox).toBeVisible();
    // 5 MB consumed in 20 minutes (1200 seconds) -> extrapolated to 30 days:
    // (5 MB / 1200) * 2592000 = 10800 MB = 10.55 GB
    await expect(forecastBox).toContainText(/10\.\d+\s*GB/);
  });

  test('displays quota table and allows toggle enabled', async ({ page }) => {
    const table = page.locator('table');
    await expect(table).toBeVisible();
    await expect(table).toContainText('Global Limit');
    await expect(table).toContainText('БЛОКИРОВАТЬ');

    const toggle = page.locator('.toggle-switch input');
    await expect(toggle).toBeChecked();
    await page.locator('.toggle-slider').first().click();
    await expect(toggle).not.toBeChecked();
    // After unchecking, status badge should show 'выключен' or similar
    await expect(page.locator('table')).toContainText('выключен');
  });

  test('allows creating a new quota', async ({ page }) => {
    const addBtn = page.locator('button:has-text("Добавить лимит")');
    await addBtn.click();

    const modal = page.locator('.modal-card');
    await expect(modal).toBeVisible();

    await page.fill('#form-name', 'New Proxy Limit');
    await page.selectOption('#form-type', 'proxy');
    await page.fill('#form-target', 'HK-Group');
    await page.fill('#form-limit', '25');
    await page.selectOption('#form-unit', 'GB');
    await page.selectOption('#form-action', 'redirect_direct');

    const saveBtn = page.locator('button:has-text("Сохранить")');
    await saveBtn.click();

    await expect(modal).not.toBeVisible();
    await expect(page.locator('table')).toContainText('New Proxy Limit');
    await expect(page.locator('table')).toContainText('НА DIRECT');
  });
});
