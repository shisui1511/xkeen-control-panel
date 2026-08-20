import { test, expect } from '@playwright/test';

test.describe('Smart Proxy Wizard and Grid test suite', () => {
  let mockProfiles: any[] = [];
  let lastSavePayload: any = null;

  test.beforeEach(async ({ page }) => {
    mockProfiles = [];
    lastSavePayload = null;

    // Disable Service Worker to intercept API requests and force RU locale
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      window.localStorage.setItem('lang', 'ru');
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
              },
              mihomo: {
                reachable: true
              }
            }
          })
        });
      } else if (
        url.includes('/api/smart-proxy/profiles/add') ||
        url.includes('/api/smart-proxy/profiles/update')
      ) {
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
            day: 1,
            timezone: 'UTC+3'
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

  test('Empty state shows balanced onboarding with 3 template cards and status bar', async ({
    page
  }) => {
    await page.goto('/#/smartproxy');

    // 1. Verify single CTA in page-head and no redundant "Из шаблона" button
    const headerActions = page.locator('.ph-actions button');
    await expect(headerActions).toHaveCount(1);
    await expect(headerActions.first()).toContainText(/Создать профиль/i);

    // 2. Verify Status Banner has neutral inactive indicator and router NTP time
    const statusDot = page.locator('.status-card .status-dot');
    await expect(statusDot).toHaveClass(/inactive/);
    await expect(page.locator('.status-card')).toContainText(/Нет активных правил по расписанию/i);
    await expect(page.locator('.status-card')).toContainText(/Время роутера:\s*12:00/i);

    // 3. Verify Empty State Title & Onboarding Hero
    await expect(page.locator('.empty-state-head h2')).toContainText(/Сценарии автоматизации/i);

    // 4. Verify 3 Template cards with 24h mini-timelines
    const templateCards = page.locator('.template-card');
    await expect(templateCards).toHaveCount(3);

    // Cards titles: "Рабочие часы", "Ночной режим", "Выходные дни"
    await expect(templateCards.nth(0).locator('h3')).toContainText(/Рабочие часы/i);
    await expect(templateCards.nth(1).locator('h3')).toContainText(/Ночной режим/i);
    await expect(templateCards.nth(2).locator('h3')).toContainText(/Выходные дни/i);

    // Each card has a mini-timeline
    await expect(templateCards.nth(0).locator('.template-timeline')).toBeVisible();
    await expect(templateCards.nth(1).locator('.template-timeline')).toBeVisible();
    await expect(templateCards.nth(2).locator('.template-timeline')).toBeVisible();

    // 5. Verify manual create fallback link
    await expect(page.locator('.manual-create-btn')).toContainText(
      /или настройте профиль вручную с нуля/i
    );
  });

  test('Create manually from Wizard walkthrough', async ({ page }) => {
    await page.goto('/#/smartproxy');

    // 1. Open Wizard via Header Primary CTA
    await page.locator('button.btn-primary:has-text("Создать профиль")').first().click();

    // Verify Modal & Step 1 is active
    await expect(page.locator('.modal-container')).toBeVisible();
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
    await expect(proxySelect.locator('option')).toContainText([
      'DIRECT',
      'proxy-node-1',
      'proxy-node-2'
    ]);

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
    await expect(page.locator('.modal-container')).not.toBeVisible();
    await expect(page.locator('.profile-card-name')).toContainText('Manual Profile');
    await expect(page.locator('.profile-card')).toHaveCount(1);

    // Verify the saved payload contains Schedule structure
    expect(lastSavePayload).not.toBeNull();
    expect(lastSavePayload.name).toBe('Manual Profile');
    expect(lastSavePayload.group_name).toBe('PROXY-GROUP-1');
    expect(lastSavePayload.proxy_name).toBe('proxy-node-1');
    expect(lastSavePayload.schedule[1][9]).toBe(true); // Monday 9:00 active
    expect(lastSavePayload.schedule[0][0]).toBe(false); // Sunday 0:00 inactive
  });

  test('Create from a template (clicking entire card) bypasses first step', async ({ page }) => {
    await page.goto('/#/smartproxy');

    // Click on Work Hours template card
    const templateCards = page.locator('.template-card');
    await templateCards.nth(0).click();

    // Modal is opened directly at Step 2
    await expect(page.locator('.modal-container')).toBeVisible();
    await expect(page.locator('.wizard-step-indicator').nth(1)).toHaveClass(/active/);
    await expect(page.locator('#sp-name')).not.toBeVisible(); // Name is at Step 1

    // Select group/proxy
    await page.locator('#sp-group').selectOption('PROXY-GROUP-1');
    await page.locator('#sp-proxy').selectOption('proxy-node-2');

    // Go to Step 3
    await page.locator('button.btn-primary:has-text("Продолжить")').click();
    await expect(page.locator('.wizard-step-indicator').nth(2)).toHaveClass(/active/);

    // Check that grid has active hours prefilled for Work Hours (5 days * 9 hours = 45 active slots)
    const activeCells = page.locator('.grid-cell.active');
    await expect(activeCells).toHaveCount(45);

    // Save
    await page.locator('button.btn-primary:has-text("Сохранить")').click();

    // Profile exists
    await expect(page.locator('.profile-card-name')).toContainText('Рабочие часы');
  });

  test('Drawing with click and drag on scheduling grid works', async ({ page }) => {
    await page.goto('/#/smartproxy');

    // Open add profile wizard via manual link
    await page.locator('.manual-create-btn').click();
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
