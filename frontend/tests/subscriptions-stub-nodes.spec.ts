// e2e-pages: subscriptions
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

async function mockBaseEndpoints(page: Page) {
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
    } else if (url.includes('/api/system/traffic/stats')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ total: { upload: 0, download: 0 } })
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    }
  });
}

const baseSub = {
  id: 'sub-1',
  name: 'Провайдер',
  url: 'https://example.com/sub',
  enabled: true,
  enable_xray: true,
  enable_mihomo: false
};

// Фикстура аудита: провайдер отклонил устройство и вернул заглушки vless 0.0.0.0:1.
const stubNodes = [
  {
    tag: 'stub-1',
    name: 'Превышен лимит устройств',
    protocol: 'vless',
    active: false,
    stub: true,
    stub_reason: 'address'
  },
  {
    tag: 'stub-2',
    name: 'Превышен лимит устройств',
    protocol: 'vless',
    active: false,
    stub: true,
    stub_reason: 'address'
  }
];

const workingNode = {
  tag: 'node-ok',
  name: 'Рабочий узел',
  protocol: 'vless',
  active: false
};

async function mockSubscription(
  page: Page,
  sub: Record<string, unknown>,
  nodes: Array<Record<string, unknown>>
) {
  await page.route('**/api/proxy-providers', async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([sub])
    });
  });
  await page.route('**/api/subscriptions/nodes?id=sub-1', async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(nodes)
    });
  });
}

async function expandCard(page: Page) {
  await page.goto('/#/subscriptions');
  const card = page.locator('#sub-card-sub-1');
  await expect(card).toBeVisible();
  await card.locator('.collapse-toggle').click();
  return card;
}

test.describe('Subscriptions: узлы-заглушки провайдера (Phase 133-02)', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await mockBaseEndpoints(page);
  });

  test('1. у заглушки бейдж вместо пинга и зелёной галочки, выбрать её нельзя', async ({
    page
  }) => {
    await mockSubscription(page, baseSub, [...stubNodes, workingNode]);
    const card = await expandCard(page);

    const rows = card.locator('.sub-node-row');
    await expect(rows).toHaveCount(3);

    const stubRow = rows.nth(0);
    await expect(stubRow.locator('[data-testid="stub-node-badge"]')).toBeVisible();
    await expect(stubRow.locator('[data-testid="stub-node-badge"]')).toContainText(
      'Провайдер отклонил устройство'
    );
    await expect(stubRow.locator('.sub-node-status-icon.success')).toHaveCount(0);
    await expect(stubRow.locator('.sub-node-ping-btn')).toHaveCount(0);
    await expect(stubRow.locator('.sub-node-select-btn')).toBeDisabled();
  });

  test('2. клик по заглушке не отправляет запрос выбора узла', async ({ page }) => {
    let setActiveCalls = 0;
    await page.route('**/api/subscriptions/active**', async (route: Route) => {
      setActiveCalls++;
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
    });
    await mockSubscription(page, baseSub, [...stubNodes, workingNode]);
    const card = await expandCard(page);

    await card
      .locator('.sub-node-row')
      .nth(0)
      .locator('.sub-node-select-btn')
      .click({ force: true });
    expect(setActiveCalls).toBe(0);
  });

  test('3. у рабочего узла кнопка пинга есть, бейджа заглушки нет', async ({ page }) => {
    await mockSubscription(page, baseSub, [...stubNodes, workingNode]);
    const card = await expandCard(page);

    const workingRow = card.locator('.sub-node-row').nth(2);
    await expect(workingRow.locator('.sub-node-ping-btn')).toBeVisible();
    await expect(workingRow.locator('[data-testid="stub-node-badge"]')).toHaveCount(0);
    await expect(workingRow.locator('.sub-node-select-btn')).toBeEnabled();
  });

  test('4. баннер отказа провайдера показывает HWID, сохранённые узлы и ссылку на кабинет', async ({
    page
  }) => {
    await mockSubscription(
      page,
      {
        ...baseSub,
        device_rejected: true,
        hwid_token: 'D49C531AE27F',
        profile_web_page_url: 'https://cabinet.example.org',
        proxy_count: 2
      },
      [...stubNodes]
    );
    await page.goto('/#/subscriptions');

    const banner = page.locator('[data-testid="device-rejected-banner"]');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('Провайдер отклонил устройство');
    await expect(banner).toContainText('D49C531AE27F');
    await expect(banner).toContainText('Прежние рабочие узлы сохранены');

    const link = banner.locator('[data-testid="device-rejected-cabinet-link"]');
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('href', 'https://cabinet.example.org/');
    await expect(link).toHaveAttribute('rel', /noopener/);
  });

  test('5. без profile_web_page_url ссылки на кабинет нет, без сохранённых узлов строки о них нет', async ({
    page
  }) => {
    await mockSubscription(
      page,
      { ...baseSub, device_rejected: true, hwid_token: 'D49C531AE27F', proxy_count: 0 },
      [...stubNodes]
    );
    await page.goto('/#/subscriptions');

    const banner = page.locator('[data-testid="device-rejected-banner"]');
    await expect(banner).toBeVisible();
    await expect(banner.locator('[data-testid="device-rejected-cabinet-link"]')).toHaveCount(0);
    await expect(banner).not.toContainText('Прежние рабочие узлы сохранены');
  });

  test('6. ссылка javascript: в profile_web_page_url не выводится', async ({ page }) => {
    await mockSubscription(
      page,
      {
        ...baseSub,
        device_rejected: true,
        hwid_token: 'D49C531AE27F',
        profile_web_page_url: 'javascript:alert(1)',
        proxy_count: 1
      },
      [...stubNodes]
    );
    await page.goto('/#/subscriptions');

    await expect(page.locator('[data-testid="device-rejected-banner"]')).toBeVisible();
    await expect(page.locator('[data-testid="device-rejected-cabinet-link"]')).toHaveCount(0);
  });

  test('7. подписка без отказа не показывает баннер', async ({ page }) => {
    await mockSubscription(page, { ...baseSub, proxy_count: 1 }, [workingNode]);
    await page.goto('/#/subscriptions');
    await expect(page.locator('#sub-card-sub-1')).toBeVisible();
    await expect(page.locator('[data-testid="device-rejected-banner"]')).toHaveCount(0);
  });

  test('8. обновление с отказом провайдера даёт тост-предупреждение, а не успех', async ({
    page
  }) => {
    let refreshed = false;
    await page.route('**/api/proxy-providers', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          refreshed
            ? { ...baseSub, device_rejected: true, hwid_token: 'D49C531AE27F', proxy_count: 1 }
            : { ...baseSub, proxy_count: 1 }
        ])
      });
    });
    await page.route('**/api/subscriptions/nodes?id=sub-1', async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([workingNode])
      });
    });
    await page.route('**/api/subscriptions/refresh*', async (route: Route) => {
      refreshed = true;
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-1');
    await expect(card).toBeVisible();
    await card.locator('button[title="Обновить"]').click();

    const toasts = page.locator('.toast');
    await expect(toasts.filter({ hasText: 'Провайдер отклонил устройство' })).toBeVisible();
    await expect(toasts.filter({ hasText: 'обновление подписки запущено' })).toHaveCount(0);
  });

  test('9. обычное обновление по-прежнему показывает успех', async ({ page }) => {
    await mockSubscription(page, { ...baseSub, proxy_count: 1 }, [workingNode]);
    await page.route('**/api/subscriptions/refresh*', async (route: Route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
    });

    await page.goto('/#/subscriptions');
    const card = page.locator('#sub-card-sub-1');
    await expect(card).toBeVisible();
    await card.locator('button[title="Обновить"]').click();

    await expect(
      page.locator('.toast').filter({ hasText: 'обновление подписки запущено' })
    ).toBeVisible();
  });
});
