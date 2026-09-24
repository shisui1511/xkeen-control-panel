import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

test.describe('Mihomo profiles card', () => {
  test('lists, creates and activates profiles, opens one in the editor', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    let profiles = [
      { name: 'default', size: 12609, mtime: 1790150000, active: true },
      { name: 'travel', size: 900, mtime: 1790100000, active: false }
    ];
    const calls: { action: string; body: any }[] = [];

    await page.route('**/api/capabilities', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            kernels: {
              mihomo: { installed: true, version: '1.19.31' },
              xray: { installed: false }
            },
            active_kernel: 'mihomo',
            mihomo: {
              reachable: true,
              process_running: true,
              api_reachable: true,
              api_authenticated: true
            }
          }
        }
      })
    );
    await page.route('**/api/mihomo/profiles**', async (route) => {
      const url = new URL(route.request().url());
      const action = url.pathname.split('/').pop() || '';
      if (action === 'profiles') {
        return route.fulfill({
          json: { success: true, data: { managed: true, active: 'default', profiles } }
        });
      }
      const body = route.request().postDataJSON();
      calls.push({ action, body });
      if (action === 'create') {
        profiles = [...profiles, { name: body.name, size: 100, mtime: 1790200000, active: false }];
        return route.fulfill({
          json: { success: true, data: { managed: true, active: 'default', profiles } }
        });
      }
      if (action === 'activate') {
        profiles = profiles.map((p) => ({ ...p, active: p.name === body.name }));
        return route.fulfill({
          json: { success: true, data: { active: body.name, restarted: true, rolled_back: false } }
        });
      }
      return route.fulfill({ status: 404, json: { success: false, error: 'unexpected' } });
    });

    await visitPage(page, '/#/services');
    const card = page.locator('.profiles-card');
    await expect(card.getByText('default')).toBeVisible();
    await expect(card.locator('.pc-row.is-active')).toContainText('default');

    await card.getByRole('textbox', { name: /Имя нового профиля|New profile name/ }).fill('work');
    await card.getByRole('button', { name: /Создать копию активного|Copy the active one/ }).click();
    await expect(card.getByText('work', { exact: true })).toBeVisible();
    expect(calls[0]).toEqual({ action: 'create', body: { name: 'work', empty: false } });

    const travel = card.locator('.pc-row', { hasText: 'travel' });
    await travel.getByRole('button', { name: /Активировать|Activate/ }).click();
    await page.locator('.confirm-actions').getByRole('button').last().click();
    await expect(card.locator('.pc-row.is-active')).toContainText('travel');
    expect(calls.at(-1)).toEqual({ action: 'activate', body: { name: 'travel' } });

    await card
      .locator('.pc-row', { hasText: 'work' })
      .getByRole('button', { name: /Править|Edit/ })
      .click();
    await expect(page).toHaveURL(/#\/editor/);
  });
});
