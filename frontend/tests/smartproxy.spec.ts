import { test, expect } from '@playwright/test';

test.describe('Smart Proxy Wizard and Grid test suite', () => {
  let mockProfiles: any[] = [];
  let lastSavePayload: any = null;

  test.beforeEach(async ({ page }) => {
    mockProfiles = [];
    lastSavePayload = null;

    // Disable Service Worker to intercept API requests
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Mock API requests
    await page.route('**/api/**', async (route) => {
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
              active_kernel: 'mihomo',
              kernels: {
                xray: { installed: true },
                mihomo: { installed: true }
              }
            }
          })
        });
      } else if (url.includes('/api/smart-proxy/profiles/add') || url.includes('/api/smart-proxy/profiles/update')) {
        lastSavePayload = route.request().postDataJSON();
        const newProfile = {
          id: 'profile_' + Date.now(),
          ...lastSavePayload,
          last_applied: 0,
          apply_count: 0
        };
        mockProfiles.push(newProfile);
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(newProfile)
        });
      } else if (url.includes('/api/smart-proxy/profiles')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockProfiles)
        });
      } else if (url.includes('/api/smart-proxy/status')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            active: [],
            next: [],
            time: '12:00',
            day: 1
          })
        });
      } else if (url.includes('/api/mihomo/proxy/proxies')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            proxies: {
              'PROXY-GROUP-1': { type: 'Selector', all: ['proxy-node-1', 'proxy-node-2'] },
              'proxy-node-1': { type: 'Shadowsocks' },
              'proxy-node-2': { type: 'Vless' }
            }
          })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      }
    });
  });

  test('Empty state shows 3 template cards and CTA', async ({ page }) => {
    await page.goto('/#/smartproxy');

    // 1. Verify Empty State Title
    await expect(page.locator('.empty-state-head h2')).toContainText(/Нет профилей умного прокси/i);

    // 2. Verify 3 Template cards
    const templateCards = page.locator('.template-card');
    await expect(templateCards).toHaveCount(3);

    // Cards titles: "Ночной VPN", "Будни 9-18", "Круглосуточный VPN" (depending on language)
    await expect(templateCards.nth(0).locator('h3')).toContainText(/Ночной VPN/i);
    await expect(templateCards.nth(1).locator('h3')).toContainText(/Будни 9-18/i);
    await expect(templateCards.nth(2).locator('h3')).toContainText(/Круглосуточный/i);
  });

  test('Create manually from Wizard walkthrough', async ({ page }) => {
    await page.goto('/#/smartproxy');

    // 1. Open Wizard
    await page.locator('button.btn-primary:has-text("Добавить")').first().click();

    // Verify Modal & Step 1 is active
    await expect(page.locator('.modal-card')).toBeVisible();
    await expect(page.locator('.wizard-step-indicator').nth(0)).toHaveClass(/active/);

    // Fill profile name
    await page.locator('#sp-name').fill('Manual Profile');

    // Go to Step 2
    await page.locator('button.btn-primary:has-text("Продолжить")').click();
    await expect(page.locator('.wizard-step-indicator').nth(1)).toHaveClass(/active/);

    // 2. Verify Proxy Groups and Proxies Autocomplete options are populated
    const groupSelect = page.locator('#sp-group');
    const proxySelect = page.locator('#sp-proxy');

    await expect(groupSelect.locator('option')).toContainText(['PROXY-GROUP-1']);
    await expect(proxySelect.locator('option')).toContainText(['proxy-node-1', 'proxy-node-2', 'DIRECT']);

    // Select target options
    await groupSelect.selectOption('PROXY-GROUP-1');
    await proxySelect.selectOption('proxy-node-1');

    // Go to Step 3
    await page.locator('button.btn-primary:has-text("Продолжить")').click();
    await expect(page.locator('.wizard-step-indicator').nth(2)).toHaveClass(/active/);

    // 3. Verify schedule grid is present
    const gridCell = page.locator('.grid-cell');
    await expect(gridCell).toHaveCount(168); // 7 * 24 cells

    // Click "Будни 9-18" preset
    await page.locator('button:has-text("Будни 9-18")').click();

    // Verify active cells (colored ones)
    const activeCells = page.locator('.grid-cell.active');
    await expect(activeCells).toHaveCount(5 * 9); // 5 days * 9 hours (9:00 - 17:59)

    // Save profile
    await page.locator('button.btn-primary:has-text("Сохранить")').click();

    // Modal should close and the created profile should appear in the list
    await expect(page.locator('.modal-card')).not.toBeVisible();
    await expect(page.locator('.profile-card-name')).toContainText('Manual Profile');
    await expect(page.locator('.profile-card')).toHaveCount(1);

    // Verify the saved payload contains Schedule structure
    expect(lastSavePayload).not.toBeNull();
    expect(lastSavePayload.name).toBe('Manual Profile');
    expect(lastSavePayload.group_name).toBe('PROXY-GROUP-1');
    expect(lastSavePayload.proxy_name).toBe('proxy-node-1');
    expect(lastSavePayload.schedule[1][9]).toBe(true);  // Monday 9:00 active
    expect(lastSavePayload.schedule[0][0]).toBe(false); // Sunday 0:00 inactive
  });

  test('Create from a template bypasses first step', async ({ page }) => {
    await page.goto('/#/smartproxy');

    // Click "Select" on Night VPN template
    const templateCards = page.locator('.template-card');
    await templateCards.nth(0).locator('button').click();

    // Modal is opened directly at Step 2
    await expect(page.locator('.modal-card')).toBeVisible();
    await expect(page.locator('.wizard-step-indicator').nth(1)).toHaveClass(/active/);
    await expect(page.locator('#sp-name')).not.toBeVisible(); // Name is at Step 1

    // Select group/proxy
    await page.locator('#sp-group').selectOption('PROXY-GROUP-1');
    await page.locator('#sp-proxy').selectOption('proxy-node-2');

    // Go to Step 3
    await page.locator('button.btn-primary:has-text("Продолжить")').click();
    await expect(page.locator('.wizard-step-indicator').nth(2)).toHaveClass(/active/);

    // Check that grid has active hours prefilled for Night VPN (hours 23, 0..7)
    // 7 days * 9 active hours = 63 active slots
    const activeCells = page.locator('.grid-cell.active');
    await expect(activeCells).toHaveCount(63);

    // Save
    await page.locator('button.btn-primary:has-text("Сохранить")').click();

    // Profile exists
    await expect(page.locator('.profile-card-name')).toContainText('Ночной VPN');
  });

  test('Drawing with click and drag on scheduling grid works', async ({ page }) => {
    await page.goto('/#/smartproxy');

    // Open add profile wizard
    await page.locator('button.btn-primary:has-text("Добавить")').first().click();
    await page.locator('#sp-name').fill('Drag Test');
    await page.locator('button.btn-primary:has-text("Продолжить")').click();
    await page.locator('#sp-group').selectOption('PROXY-GROUP-1');
    await page.locator('#sp-proxy').selectOption('proxy-node-1');
    await page.locator('button.btn-primary:has-text("Продолжить")').click();

    // Verify grid has 0 active cells initially
    await expect(page.locator('.grid-cell.active')).toHaveCount(0);

    const cells = page.locator('.grid-cell');

    // Simulate clicking and drawing over cells:
    // Hover first cell, mousedown, hover next cells, mouseup
    await cells.nth(0).hover();
    await page.mouse.down();
    await cells.nth(1).hover();
    await cells.nth(2).hover();
    await page.mouse.up();

    // Verify 3 cells are active
    await expect(page.locator('.grid-cell.active')).toHaveCount(3);
  });
});
