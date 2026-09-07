import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Phase 111: Xray Gaps and Enhancements', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      window.localStorage.setItem('lang', 'ru');
    });

    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      const method = route.request().method();

      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: true,
            setup_required: false,
            csrf_token: 'mock-csrf'
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
                xray: { installed: true, version: '1.8.24', channel: 'stable' }
              },
              active_kernel: 'xray',
              xray: { grpc_ready: true }
            }
          })
        });
      } else if (url.includes('/api/xray/stats')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            status: 'ok',
            data: {
              outbounds: {
                'proxy-out': { uplink: 1024, downlink: 2048 },
                direct: { uplink: 512, downlink: 512 }
              },
              inbounds: {
                'mixed-in': { uplink: 4096, downlink: 8192 }
              },
              users: {
                'user1@domain': { uplink: 1000, downlink: 2000 }
              }
            }
          })
        });
      } else if (url.includes('/api/xray/restart-logger') && method === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'ok', success: true })
        });
      } else if (url.includes('/api/config/read')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            routing: { rules: [] },
            dns: { servers: [] },
            inbounds: [],
            outbounds: [{ tag: 'existing-proxy', protocol: 'vless' }]
          })
        });
      } else if (url.includes('/api/config/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
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

  test('Traffic page: отображает многомерные вкладки статистики Xray (Outbounds, Inbounds, Users)', async ({
    page
  }) => {
    await page.goto('/#/traffic');

    const section = page.locator('[data-testid="xray-stats-section"]');
    await expect(section).toBeVisible({ timeout: 5000 });

    const tabOutbounds = page.locator('[data-testid="xray-tab-outbounds"]');
    const tabInbounds = page.locator('[data-testid="xray-tab-inbounds"]');
    const tabUsers = page.locator('[data-testid="xray-tab-users"]');

    await expect(tabOutbounds).toBeVisible();
    await expect(tabInbounds).toBeVisible();
    await expect(tabUsers).toBeVisible();

    // По умолчанию выбрана вкладка Outbounds
    const tag = page.locator('[data-testid="xray-stats-tag"]');
    await expect(tag.first()).toHaveText('proxy-out');

    // Переключаемся на Inbounds
    await tabInbounds.click();
    await expect(page.locator('[data-testid="xray-stats-tag"]').first()).toHaveText('mixed-in');

    // Переключаемся на Users
    await tabUsers.click();
    await expect(page.locator('[data-testid="xray-stats-tag"]').first()).toHaveText('user1@domain');
  });

  test('Logs page: кнопка перечитывания логов Xray доступна в тулбаре и вызывает эндпоинт', async ({
    page
  }) => {
    let restartCalled = false;
    await page.route('**/api/xray/restart-logger', async (route) => {
      restartCalled = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'ok', success: true })
      });
    });

    await page.goto('/#/logs');

    const restartBtn = page.locator('[data-testid="restart-xray-logger-btn"]');
    await expect(restartBtn).toBeVisible({ timeout: 5000 });

    await restartBtn.click();
    expect(restartCalled).toBe(true);

    // Кнопка переходит в состояние кулдауна (disabled)
    await expect(restartBtn).toBeDisabled();
  });
});
