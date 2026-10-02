import { test, expect, type Page } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

async function mockProfiles(
  page: Page,
  activation: Record<string, unknown> = {},
  capsOverride: Record<string, unknown> = {}
) {
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
          },
          ...capsOverride
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
        json: {
          success: true,
          data: { active: body.name, restarted: true, rolled_back: false, ...activation }
        }
      });
    }
    return route.fulfill({ status: 404, json: { success: false, error: 'unexpected' } });
  });
  return calls;
}

test.describe('Mihomo profiles card', () => {
  test('lists, creates and activates profiles, opens one in the editor', async ({ page }) => {
    const calls = await mockProfiles(page);
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

  test('activation without restart shows the outcome toast instead of "activated"', async ({
    page
  }) => {
    await mockProfiles(page, { restarted: false, outcome: 'saved_kernel_stopped' });
    await visitPage(page, '/#/services');
    const card = page.locator('.profiles-card');
    await expect(card.getByText('default')).toBeVisible();

    const travel = card.locator('.pc-row', { hasText: 'travel' });
    await travel.getByRole('button', { name: /Активировать|Activate/ }).click();
    await page.locator('.confirm-actions').getByRole('button').last().click();

    const toast = page.locator('.toast', {
      hasText: /вступят в силу при запуске|take effect when it starts/
    });
    await expect(toast).toBeVisible({ timeout: 5000 });
    await expect(toast.getByRole('button', { name: /Запустить сейчас|Start now/ })).toBeVisible();
    await expect(card.locator('.pc-row.is-active')).toContainText('travel');
  });

  test('activation with both kernels running shows the conflict outcome toast', async ({
    page
  }) => {
    await mockProfiles(page, { restarted: false, outcome: 'saved_kernel_conflict' });
    await visitPage(page, '/#/services');
    const card = page.locator('.profiles-card');
    await expect(card.getByText('default')).toBeVisible();

    const travel = card.locator('.pc-row', { hasText: 'travel' });
    await travel.getByRole('button', { name: /Активировать|Activate/ }).click();
    await page.locator('.confirm-actions').getByRole('button').last().click();

    await expect(
      page.locator('.toast', {
        hasText: /Сохранено, но не применено: запущены оба ядра|Saved but not applied: both kernels/
      })
    ).toBeVisible({ timeout: 5000 });
  });

  test('activate is disabled while both kernels are running', async ({ page }) => {
    await mockProfiles(
      page,
      {},
      { active_kernel: 'both', kernel_conflict: true, running_kernels: ['xray', 'mihomo'] }
    );
    await visitPage(page, '/#/services');
    const card = page.locator('.profiles-card');
    const activate = card
      .locator('.pc-row', { hasText: 'travel' })
      .getByRole('button', { name: /Активировать|Activate/ });
    await expect(activate).toBeDisabled();
    await expect(activate).toHaveAttribute(
      'title',
      /Недоступно, пока запущены оба ядра|Unavailable while both kernels are running/
    );
  });
});
