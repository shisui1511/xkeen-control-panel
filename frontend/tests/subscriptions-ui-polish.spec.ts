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

  test('displays format error badge and detailed message if subscription has last_error', async ({
    page
  }) => {
    // Mock subscriptions with one format error subscription
    await page.route('**/api/proxy-providers', async (route: Route) => {
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
            last_error:
              'данная подписка не имеет формата Clash/Mihomo YAML и не поддерживается ядром Mihomo. Пожалуйста, убедитесь, что подписка возвращает Clash YAML формат',
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

  test('MihomoGenerator import button is disabled if there are no Xray subscriptions', async ({
    page
  }) => {
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

    const importBtn = page
      .locator('.constructor-proxy-list button')
      .filter({ hasText: 'Импортировать из Xray-подписок' });
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

    const importBtn = page
      .locator('.constructor-proxy-list button')
      .filter({ hasText: 'Импортировать из Xray-подписок' });
    await expect(importBtn).toBeVisible();
    await expect(importBtn).toBeEnabled();
    await expect(importBtn).toHaveAttribute(
      'title',
      /Импортировать прокси-серверы из существующих/
    );
  });

  test('displays provider chip and node count badge correctly', async ({ page }) => {
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_mihomo_pp',
            name: 'Mihomo Provider Sub',
            url: 'https://example.com/mihomo.yaml',
            enable_mihomo: true,
            enable_xray: false,
            enabled: true,
            mihomo_provider: {
              name: 'mihomo-provider-sub',
              vehicle_type: 'HTTP',
              updated_at: new Date().toISOString(),
              node_count: 7
            }
          }
        ])
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub_mihomo_pp');
    await expect(card).toBeVisible();

    const providerChip = card.locator('.mihomo-provider-chip');
    await expect(providerChip).toBeVisible();
    await expect(providerChip).toContainText('HTTP');

    const nodesCount = card.locator('.nodes-count-badge');
    await expect(nodesCount).toBeVisible();
    await expect(nodesCount).toContainText('7');
  });

  test('displays fallback Mihomo label when mihomo_provider is null', async ({ page }) => {
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_mihomo_null',
            name: 'Mihomo Null Sub',
            url: 'https://example.com/mihomo2.yaml',
            enable_mihomo: true,
            enable_xray: false,
            enabled: true,
            mihomo_provider: null
          }
        ])
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub_mihomo_null');
    await expect(card).toBeVisible();

    const integratedBadge = card.locator('.mihomo-integrated-badge');
    await expect(integratedBadge).toBeVisible();
    await expect(integratedBadge).toContainText('Mihomo —');
  });

  test('applies ellipsis and max-width on long vehicle_type in provider chip', async ({ page }) => {
    const longVehicleType = 'VERY_LONG_VEHICLE_TYPE_'.repeat(10);
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_mihomo_long',
            name: 'Mihomo Long Sub',
            url: 'https://example.com/mihomo3.yaml',
            enable_mihomo: true,
            enable_xray: false,
            enabled: true,
            mihomo_provider: {
              name: 'mihomo-provider-long',
              vehicle_type: longVehicleType,
              updated_at: new Date().toISOString(),
              node_count: 5
            }
          }
        ])
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub_mihomo_long');
    await expect(card).toBeVisible();

    const providerChip = card.locator('.mihomo-provider-chip');
    await expect(providerChip).toBeVisible();

    const textOverflow = await providerChip.evaluate(
      (el) => window.getComputedStyle(el).textOverflow
    );
    expect(textOverflow).toBe('ellipsis');

    const maxWidth = await providerChip.evaluate((el) => window.getComputedStyle(el).maxWidth);
    expect(maxWidth).toBe('220px');
  });

  test('dual-kernel refresh triggers both endpoints and shows separate toasts', async ({
    page
  }) => {
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_dual',
            name: 'Dual Sub',
            url: 'https://example.com/dual',
            enable_mihomo: true,
            enable_xray: true,
            enabled: true,
            mihomo_provider: {
              name: 'dual-provider',
              vehicle_type: 'HTTP',
              updated_at: new Date().toISOString(),
              node_count: 10
            }
          }
        ])
      });
    });

    let xrayRefreshed = false;
    let mihomoRefreshed = false;

    await page.route('**/api/subscriptions/refresh*', async (route: Route) => {
      xrayRefreshed = true;
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Xray update error details' })
      });
    });

    await page.route('**/api/proxy-providers/*/refresh', async (route: Route) => {
      mihomoRefreshed = true;
      await route.fulfill({
        status: 204
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub_dual');
    await expect(card).toBeVisible();

    const refreshBtn = card.locator('button.action-icon-btn').first();
    await expect(refreshBtn).toBeVisible();
    await refreshBtn.click();

    // Verify both requests were triggered
    await expect.poll(() => xrayRefreshed).toBe(true);
    await expect.poll(() => mihomoRefreshed).toBe(true);

    // Verify both toasts are displayed
    const successToast = page.locator('.toast.success, .toast:has-text("Mihomo:")');
    const errorToast = page.locator('.toast.error, .toast:has-text("Xray: ошибка")');
    await expect(successToast).toBeVisible();
    await expect(errorToast).toBeVisible();
  });

  test('displays error message and retry button when nodes load fails for Mihomo provider', async ({
    page
  }) => {
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_fail',
            name: 'Failed Sub',
            url: 'https://example.com/fail.yaml',
            enable_mihomo: true,
            enable_xray: false,
            enabled: true,
            mihomo_provider: {
              name: 'fail-provider',
              vehicle_type: 'HTTP',
              updated_at: new Date().toISOString(),
              node_count: 5
            }
          }
        ])
      });
    });

    let loadAttempts = 0;
    await page.route('**/api/proxy-providers/fail-provider/nodes', async (route: Route) => {
      loadAttempts++;
      if (loadAttempts === 1) {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'Internal Server Error' })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              tag: 'node-ok',
              name: 'node-ok',
              alive: true,
              tested: true,
              delay_ms: 90
            }
          ])
        });
      }
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub_fail');
    await expect(card).toBeVisible();

    // Toggle expand to trigger load
    const countBadge = card.locator('.nodes-count-badge');
    await countBadge.click();

    // Verify error and retry button are visible
    const errorDetails = card.locator('.sub-error-details');
    await expect(errorDetails).toBeVisible();
    await expect(errorDetails).toContainText('Не удалось загрузить узлы Mihomo');

    const retryBtn = errorDetails.locator('button');
    await expect(retryBtn).toBeVisible();
    await expect(retryBtn).toContainText('Повторить');

    // Click retry
    await retryBtn.click();

    // Verify nodes are loaded now
    await expect(card.locator('.sub-node-row')).toBeVisible();
    await expect(card.locator('.sub-node-name')).toContainText('node-ok');
    expect(loadAttempts).toBe(2);
  });

  test('renders neutral dash for untested Mihomo node instead of default-ok checkmark', async ({
    page
  }) => {
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'sub_untested',
            name: 'Untested Sub',
            url: 'https://example.com/untested.yaml',
            enable_mihomo: true,
            enable_xray: false,
            enabled: true,
            mihomo_provider: {
              name: 'untested-provider',
              vehicle_type: 'HTTP',
              updated_at: new Date().toISOString(),
              node_count: 1
            }
          }
        ])
      });
    });

    await page.route('**/api/proxy-providers/untested-provider/nodes', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            tag: 'node-untested',
            name: 'node-untested',
            alive: true,
            tested: false,
            delay_ms: 0
          }
        ])
      });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub_untested');
    await expect(card).toBeVisible();

    const countBadge = card.locator('.nodes-count-badge');
    await countBadge.click();

    // Verify untested node renders neutral dash instead of green checkmark
    const nodeRow = card.locator('.sub-node-row');
    await expect(nodeRow).toBeVisible();

    const pingVal = nodeRow.locator('.sub-node-ping-btn');
    await expect(pingVal).toBeVisible();
    await expect(pingVal).toContainText('—');
    await expect(nodeRow.locator('.sub-node-status-icon.default-ok')).not.toBeVisible();
  });
});
