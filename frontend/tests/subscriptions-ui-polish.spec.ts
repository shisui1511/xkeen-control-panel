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

test.describe('Subscriptions UI Polish E2E tests', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);

    // Mock generic API endpoints
    await page.route('**/api/**', async (route: Route) => {
      const url = route.request().url();
      const method = route.request().method();

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
      } else if (url.includes('/api/config/read')) {
        // Mock config.yaml with proxy-groups
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            content: `
port: 7890
socks-port: 7891
mode: rule
proxy-groups:
  - name: Selective
    type: select
    proxies:
      - DIRECT
      - REJECT
  - name: Proxy
    type: select
    proxies:
      - DIRECT
`
          })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      }
    });
  });

  test('modal contains kernel checkboxes and hides/shows advanced settings', async ({ page }) => {
    // 1. Visit subscriptions page and click add
    await page.goto('/#/subscriptions');
    const addBtn = page.locator('button:has-text("Добавить")').first();
    await expect(addBtn).toBeVisible();
    await addBtn.click();

    const modal = page.locator('.modal-card');
    await expect(modal).toBeVisible();

    // 2. Verify checkboxes exist
    const xrayCheckbox = modal.locator('label:has-text("XRay (JSON / Base64)")');
    const mihomoCheckbox = modal.locator('label:has-text("Mihomo (Clash YAML)")');
    await expect(xrayCheckbox).toBeVisible();
    await expect(mihomoCheckbox).toBeVisible();

    // Default should have xray checked, so advanced settings are toggleable
    const advancedToggle = modal.locator('button:has-text("Дополнительные параметры")');
    await expect(advancedToggle).toBeVisible();

    // Toggle advanced to make filters visible
    await advancedToggle.click();
    await expect(modal.locator('input#form-tag-prefix')).toBeVisible();

    // Check Mihomo integration
    await mihomoCheckbox.click();

    // Mihomo groups selection panel should be displayed with parsed groups
    await expect(modal.locator('label:has-text("Интегрировать в группы Mihomo")')).toBeVisible();
    const selectiveCheckbox = modal.locator('input[type="checkbox"] + span:has-text("Selective")');
    const proxyCheckbox = modal.locator('input[type="checkbox"] + span:has-text("Proxy")');
    await expect(selectiveCheckbox).toBeVisible();
    await expect(proxyCheckbox).toBeVisible();

    // Uncheck XRay integration
    await xrayCheckbox.click();

    // Advanced toggle and tag prefix should now be hidden
    await expect(advancedToggle).not.toBeVisible();
    await expect(modal.locator('input#form-tag-prefix')).not.toBeVisible();
  });

  test('saves subscription with selected kernel flags and mihomo groups', async ({ page }) => {
    let savedPayload: any = null;
    await page.route('**/api/subscriptions/add', async (route: Route) => {
      savedPayload = route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/subscriptions');
    await page.locator('button:has-text("Добавить")').first().click();

    const modal = page.locator('.modal-card');
    await modal.locator('input#form-name').fill('My test sub');
    await modal.locator('input#form-url').fill('https://example.com/sub.yaml');

    // Enable Mihomo
    await modal.locator('label:has-text("Mihomo (Clash YAML)")').click();

    // Select Selective group
    await modal.locator('input[type="checkbox"] ~ span:has-text("Selective")').click();

    // Disable XRay
    await modal.locator('label:has-text("XRay (JSON / Base64)")').click();

    // Save
    await modal.locator('button:has-text("Сохранить")').click();

    // Verify correct payload was sent
    expect(savedPayload).not.toBeNull();
    expect(savedPayload.name).toBe('My test sub');
    expect(savedPayload.enable_xray).toBe(false);
    expect(savedPayload.enable_mihomo).toBe(true);
    expect(savedPayload.mihomo_groups).toContain('Selective');
    expect(savedPayload.tag_prefix).toBe('');
  });

  test('displays format error badge and detailed message if subscription has last_error', async ({ page }) => {
    // Mock subscriptions with one format error subscription
    await page.route('**/api/subscriptions', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_error',
            name: 'Broken Sub',
            url: 'https://example.com/bad',
            enable_mihomo: true,
            enable_xray: false,
            enabled: true,
            last_error: 'данная подписка не имеет формата Clash/Mihomo YAML и не поддерживается ядром Mihomo. Пожалуйста, убедитесь, что подписка возвращает Clash YAML формат',
            proxy_count: 0
          }
        ])
      });
    });

    await page.goto('/#/subscriptions');

    // Sub card with error class should be present
    const card = page.locator('#sub-card-sub_error');
    await expect(card).toBeVisible();

    // Should display red badge "Ошибка формата"
    const errorBadge = card.locator('.badge-error');
    await expect(errorBadge).toBeVisible();
    await expect(errorBadge).toContainText('Ошибка формата');

    // Detailed error message should be displayed under title
    const errorDesc = card.locator('.sub-error-details');
    await expect(errorDesc).toBeVisible();
    await expect(errorDesc).toContainText('данная подписка не имеет формата Clash/Mihomo YAML');
  });

  test('MihomoGenerator import button is disabled if there are no Xray subscriptions', async ({ page }) => {
    // Mock subscription to return only a Mihomo subscription, meaning no Xray subscriptions exist
    await page.route('**/api/subscriptions', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_mihomo',
            name: 'Mihomo Sub',
            url: 'https://example.com/mihomo',
            enable_mihomo: true,
            enable_xray: false,
            enabled: true
          }
        ])
      });
    });

    await page.goto('/#/constructor');
    await page.locator('.constructor-kernel-toggle button:has-text("Mihomo")').click();

    const importBtn = page.locator('.constructor-proxy-list button').filter({ hasText: 'Импортировать из Xray-подписок' });
    await expect(importBtn).toBeVisible();
    await expect(importBtn).toBeDisabled();
    await expect(importBtn).toHaveAttribute('title', /Нет доступных активных Xray-подписок/);
  });

  test('MihomoGenerator import button is enabled if Xray subscriptions exist', async ({ page }) => {
    // Mock subscription to return an Xray subscription
    await page.route('**/api/subscriptions', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_xray',
            name: 'Xray Sub',
            url: 'https://example.com/xray',
            enable_xray: true,
            enable_mihomo: false,
            enabled: true
          }
        ])
      });
    });

    await page.goto('/#/constructor');
    await page.locator('.constructor-kernel-toggle button:has-text("Mihomo")').click();

    const importBtn = page.locator('.constructor-proxy-list button').filter({ hasText: 'Импортировать из Xray-подписок' });
    await expect(importBtn).toBeVisible();
    await expect(importBtn).toBeEnabled();
    await expect(importBtn).toHaveAttribute('title', /Импортировать прокси-серверы из существующих/);
  });
});
