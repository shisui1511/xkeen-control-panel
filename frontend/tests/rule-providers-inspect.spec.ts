import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

test.describe('Rule providers: contents and diagnostics', () => {
  test('shows empty lists, checks a dead link and browses contents', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const contentQueries: string[] = [];

    await page.route('**/api/mihomo/proxy/providers/rules', (route) =>
      route.fulfill({
        json: {
          providers: {
            'ads@domain': {
              name: 'ads@domain',
              behavior: 'Domain',
              type: 'Rule',
              vehicleType: 'HTTP',
              ruleCount: 3,
              updatedAt: new Date().toISOString()
            },
            'dead@domain': {
              name: 'dead@domain',
              behavior: 'Domain',
              type: 'Rule',
              vehicleType: 'HTTP',
              ruleCount: 0,
              updatedAt: '0001-01-01T00:00:00Z'
            }
          }
        }
      })
    );
    await page.route('**/api/rule-providers/info', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: [
            {
              name: 'ads@domain',
              type: 'http',
              behavior: 'domain',
              format: 'mrs',
              url: 'https://example.com/ads.mrs',
              file_exists: true
            },
            {
              name: 'dead@domain',
              type: 'http',
              behavior: 'domain',
              format: 'mrs',
              url: 'https://example.com/dead.mrs',
              file_exists: false
            }
          ]
        }
      })
    );
    await page.route('**/api/rule-providers/check-url**', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            name: 'dead@domain',
            url: 'https://example.com/dead.mrs',
            status_code: 404,
            ok: false,
            duration_ms: 12
          }
        }
      })
    );
    await page.route('**/api/rule-providers/content**', (route) => {
      const q = new URL(route.request().url()).searchParams.get('q') || '';
      contentQueries.push(q);
      const all = ['+.ads.test', '+.tracker.test', '+.banner.test'];
      const entries = all.filter((e) => e.includes(q));
      return route.fulfill({
        json: {
          success: true,
          data: { name: 'ads@domain', total: 3, matched: entries.length, offset: 0, entries }
        }
      });
    });

    await visitPage(page, '/#/rules');
    await page.getByTestId('tab-providers').click();

    await expect(page.getByText(/Пустых списков: 1|Empty lists: 1/)).toBeVisible();
    const dead = page.locator('.provider-card', { hasText: 'dead@domain' });
    await expect(dead.getByText(/файл не скачан|not downloaded/)).toBeVisible();
    await expect(dead.getByRole('button', { name: /Содержимое|Contents/ })).toBeDisabled();

    await dead.getByRole('button', { name: /Проверить ссылку|Check link/ }).click();
    await expect(dead.getByText(/HTTP 404/)).toBeVisible();

    const ads = page.locator('.provider-card', { hasText: 'ads@domain' });
    await ads.getByRole('button', { name: /Содержимое|Contents/ }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByText('+.tracker.test')).toBeVisible();
    await dialog.getByRole('searchbox').fill('banner');
    await expect(dialog.getByText('+.banner.test')).toBeVisible();
    await expect(dialog.getByText('+.tracker.test')).toHaveCount(0);
    expect(contentQueries).toContain('banner');
  });
});
