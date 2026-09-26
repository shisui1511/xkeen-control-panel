import { test, expect } from '@playwright/test';

// DAT Manager без баз: «Обновить все» (xkeen -ug) обновляет только
// установленные файлы, поэтому базы ставятся стандартным набором.

test.describe('DAT Manager — empty state', () => {
  test('installs a standard set and disables "update all" while empty', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    const installed: string[] = [];
    const posted: Array<{ file?: string; type?: string }> = [];

    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          json: { authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' }
        });
      } else if (url.includes('/api/dat/update')) {
        const body = route.request().postDataJSON() as { file?: string; type?: string };
        posted.push(body);
        if (body.file) installed.push(body.file);
        await route.fulfill({ json: { success: true } });
      } else if (url.includes('/api/dat/list')) {
        await route.fulfill({
          json: installed.map((name) => ({
            name,
            path: `/opt/etc/xray/dat/${name}`,
            size: 1024,
            last_update: Math.floor(Date.now() / 1000),
            exists: true,
            type: 'xray'
          }))
        });
      } else {
        await route.fulfill({ json: { success: true, data: {} } });
      }
    });

    await page.goto('/#/dat');
    const sets = page.getByTestId('dat-standard-sets');
    await expect(sets).toBeVisible();
    await expect(page.getByRole('button', { name: /Обновить все|Update all/ })).toBeDisabled();

    await sets.getByRole('button', { name: 'ZKeen' }).click();
    await expect(sets).toHaveCount(0);
    expect(posted).toEqual([
      { file: 'geosite_zkeen.dat', type: 'xray' },
      { file: 'geoip_zkeenip.dat', type: 'xray' }
    ]);
    await expect(page.getByText('geosite_zkeen.dat').first()).toBeVisible();
  });
});

test('lists databases when no kernel is active', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });
  await page.route('**/api/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/api/auth/me')) {
      await route.fulfill({
        json: { authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' }
      });
    } else if (url.includes('/api/capabilities')) {
      await route.fulfill({ json: { success: true, data: { active_kernel: 'none' } } });
    } else if (url.includes('/api/dat/list')) {
      await route.fulfill({
        json: ['geosite_v2fly.dat', 'geoip_v2fly.dat'].map((name) => ({
          name,
          path: `/opt/etc/xray/dat/${name}`,
          size: 1024,
          last_update: Math.floor(Date.now() / 1000),
          exists: true,
          type: 'xray'
        }))
      });
    } else {
      await route.fulfill({ json: { success: true, data: {} } });
    }
  });

  await page.goto('/#/dat');
  await expect(page.locator('.db-card-item')).toHaveCount(2);
});
