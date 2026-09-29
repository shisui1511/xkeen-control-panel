import { test, expect, type Page, type Route } from '@playwright/test';

const XRAY_DIR = '/opt/etc/xray/configs';

const FIRST_NODE = {
  tag: 'first-node',
  protocol: 'vless',
  settings: {
    vnext: [
      { address: 'first.example.com', port: 443, users: [{ id: 'first-id', encryption: 'none' }] }
    ]
  }
};

const SUB_NODE = {
  tag: 'sub-node',
  protocol: 'vless',
  settings: {
    vnext: [
      { address: 'sub.example.com', port: 443, users: [{ id: 'sub-id', encryption: 'none' }] }
    ]
  }
};

// Файлы Xray конструктора: ручной узел в 04_outbounds.json и узел фрагмента подписки.
const FILES: Record<string, unknown> = {
  '01_log.json': { log: { loglevel: 'warning' } },
  '02_dns.json': {
    dns: { tag: 'dns-in', servers: ['8.8.8.8'], queryStrategy: 'UseIP', hosts: {} }
  },
  '03_inbounds.json': { inbounds: [] },
  '04_outbounds.json': { outbounds: [FIRST_NODE] },
  '04_outbounds.sub_1.tail.json': { outbounds: [SUB_NODE] },
  '05_routing.json': {
    routing: {
      domainStrategy: 'IPIfNonMatch',
      rules: [{ type: 'field', network: 'tcp,udp', outboundTag: 'direct' }]
    }
  },
  '06_policy.json': { policy: { levels: { '0': { handshake: 4 } }, system: {} } }
};

type Saved = Record<string, any>;

async function setupRestMocks(page: Page, saved: Saved = {}, outboundRequests: string[] = []) {
  await page.route('**/api/**', async (route: Route) => {
    const url = new URL(route.request().url());
    const method = route.request().method();

    if (url.pathname.startsWith('/api/outbound/')) {
      outboundRequests.push(url.pathname);
    }

    if (url.pathname === '/api/auth/me') {
      await route.fulfill({
        json: { authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' }
      });
    } else if (url.pathname === '/api/capabilities') {
      await route.fulfill({
        json: {
          success: true,
          data: {
            kernels: {
              xray: { installed: true, version: '1.8.24', channel: 'stable' },
              mihomo: { installed: true }
            },
            active_kernel: 'xray'
          }
        }
      });
    } else if (url.pathname === '/api/subscriptions') {
      await route.fulfill({ json: [] });
    } else if (url.pathname === '/api/config/list') {
      const files = Object.keys(FILES)
        .filter((name) => name.startsWith('04_outbounds'))
        .map((name) => ({ name, path: `${XRAY_DIR}/${name}`, size: 100 }));
      await route.fulfill({ json: files });
    } else if (url.pathname === '/api/config/read') {
      const name = (url.searchParams.get('path') || '').split('/').pop() || '';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(FILES[name] ?? {})
      });
    } else if (url.pathname === '/api/config/save' && method === 'POST') {
      const name = (url.searchParams.get('path') || '').split('/').pop() || '';
      saved[name] = JSON.parse(route.request().postData() || '{}');
      await route.fulfill({ json: { success: true } });
    } else if (url.pathname === '/api/outbound/parse' && method === 'POST') {
      const body = route.request().postDataJSON();
      const links: string[] = body.links || [];
      const hasInvalid = links.some((link) => link.includes('invalid'));

      if (hasInvalid || links.length === 0) {
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify({ success: false, error: 'Не удалось распознать ссылку' })
        });
      } else {
        const parsedData = links.map((link, idx) => ({
          link,
          outbound: {
            tag: idx === 0 ? 'test-parsed-tag' : `test-parsed-tag-${idx}`,
            protocol: 'vless',
            settings: {
              vnext: [
                {
                  address: `server${idx}.example.com`,
                  port: 443,
                  users: [{ id: `id-${idx}`, encryption: 'none' }]
                }
              ]
            }
          }
        }));
        await route.fulfill({ json: { success: true, data: parsedData } });
      }
    } else if (url.pathname === '/api/templates/list') {
      await route.fulfill({ json: [] });
    } else {
      await route.fulfill({ json: { success: true, data: {} } });
    }
  });
}

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

async function openXrayOutbounds(page: Page) {
  await page.goto('/#/constructor');
  await page.locator('.constructor-kernel-toggle button:has-text("Xray")').click();
  await page.locator('.sec-tab[data-tab="outbounds"]').click();
  // Конструктор подтянул ручной узел и узел фрагмента подписки
  await expect(page.locator('.tag-card', { hasText: 'first-node' })).toBeVisible();
  await expect(page.locator('.tag-card', { hasText: 'sub-node' })).toBeVisible();
}

async function parseLinks(page: Page, links: string) {
  await page.locator('button:has-text("Импорт узла")').click();
  const modal = page.locator('.modal-container');
  await expect(modal).toBeVisible();
  await modal.locator('textarea').fill(links);
  await modal.locator('button:has-text("Распознать")').click();
  await expect(modal.locator('.preview-section')).toBeVisible();
  return modal;
}

test.describe('Импорт узла в конструкторе Xray (APPLY-04, D-18, D-19)', () => {
  let saved: Saved;
  let outboundRequests: string[];

  test.beforeEach(async ({ page }) => {
    saved = {};
    outboundRequests = [];
    await disableServiceWorker(page);
    await setupRestMocks(page, saved, outboundRequests);
    await openXrayOutbounds(page);
  });

  test('импортированный узел попадает в список ручных узлов и черновик без запросов на сервер', async ({
    page
  }) => {
    const modal = await parseLinks(page, 'vless://test-link-data#some-tag');
    await expect(modal.locator('.preview-item-card')).toContainText('vless');
    await expect(modal.locator('.preview-item-card')).toContainText('server0.example.com:443');

    const tagInput = modal.locator('input#import-tag-0');
    await expect(tagInput).toHaveValue('test-parsed-tag');
    await tagInput.fill('my-custom-node-tag');
    await modal.locator('button:has-text("Импортировать")').click();

    await expect(modal).not.toBeVisible();
    const toast = page.locator('.toast--success');
    await expect(toast).toBeVisible();
    await expect(toast).toContainText('Добавлено узлов: 1');
    await expect(toast).toContainText('Применить');

    const card = page.locator('.tag-card', { hasText: 'my-custom-node-tag' });
    await expect(card).toHaveCount(1);
    await expect(card.locator('button[title="Редактировать"]')).toBeVisible();
    await expect(card.locator('button[title="Удалить"]')).toBeVisible();
    await expect(card).not.toContainText('Подписка');

    // Узел фрагмента подписки по-прежнему только для чтения
    await expect(page.locator('.tag-card', { hasText: 'sub-node' })).toContainText('Подписка');

    // Ничего не записано на сервер, эндпоинтов импорта нет: только разбор ссылки
    expect(Object.keys(saved)).toHaveLength(0);
    expect(new Set(outboundRequests)).toEqual(new Set(['/api/outbound/parse']));
  });

  test('импортированный узел удаляется из черновика кнопкой удаления', async ({ page }) => {
    const modal = await parseLinks(page, 'vless://test-link-data#some-tag');
    await modal.locator('button:has-text("Импортировать")').click();
    await expect(modal).not.toBeVisible();

    const card = page.locator('.tag-card', { hasText: 'test-parsed-tag' });
    await expect(card).toHaveCount(1);
    await card.locator('button[title="Удалить"]').click();
    await expect(page.locator('.tag-card', { hasText: 'test-parsed-tag' })).toHaveCount(0);
    await expect(page.locator('.tag-card', { hasText: 'first-node' })).toBeVisible();
    expect(Object.keys(saved)).toHaveLength(0);
  });

  test('«Применить» пишет импортированные узлы в конец 04_outbounds.json в порядке мастера', async ({
    page
  }) => {
    const modal = await parseLinks(page, 'vless://link1#tag1\nvless://link2#tag2');
    await modal.locator('input#import-tag-0').fill('imported-a');
    await modal.locator('input#import-tag-1').fill('imported-b');
    await modal.locator('button:has-text("Импортировать (2)")').click();
    await expect(modal).not.toBeVisible();
    await expect(page.locator('.toast--success')).toContainText('Добавлено узлов: 2');

    // До «Применить» на диск ничего не ушло
    expect(Object.keys(saved)).toHaveLength(0);

    await page.locator('[data-testid="apply-changes-btn"]').click();
    const dialog = page.locator('[data-testid="apply-confirm-dialog"]');
    await expect(dialog.getByText('04_outbounds.json')).toBeVisible();
    await dialog.locator('button.btn-primary').click();

    await expect.poll(() => Object.keys(saved), { timeout: 15000 }).toContain('04_outbounds.json');
    const tags = saved['04_outbounds.json'].outbounds.map((o: any) => o.tag);
    // Первый outbound файла (маршрут по умолчанию) не меняется, новые узлы в конце
    expect(tags).toEqual(['first-node', 'imported-a', 'imported-b']);
    expect(saved['04_outbounds.json'].outbounds[1].settings.vnext[0].address).toBe(
      'server0.example.com'
    );
    expect(saved['04_outbounds.json'].outbounds[2].settings.vnext[0].address).toBe(
      'server1.example.com'
    );
    expect(new Set(outboundRequests)).toEqual(new Set(['/api/outbound/parse']));
  });

  test('занятый и пустой теги подсвечиваются ошибкой и блокируют кнопку импорта', async ({
    page
  }) => {
    const modal = await parseLinks(page, 'vless://link1#tag1\nvless://link2#tag2');
    const confirmBtn = modal.locator('button:has-text("Импортировать (2)")');
    const tagInput = modal.locator('input#import-tag-0');
    await expect(confirmBtn).toBeEnabled();

    // Тег узла фрагмента подписки
    await tagInput.fill('sub-node');
    await expect(modal.locator('#import-tag-error-0')).toContainText('Тег «sub-node» уже занят');
    await expect(confirmBtn).toBeDisabled();

    // Тег ручного узла
    await tagInput.fill('first-node');
    await expect(modal.locator('#import-tag-error-0')).toContainText('Тег «first-node» уже занят');
    await expect(confirmBtn).toBeDisabled();

    // Пустой тег
    await tagInput.fill('   ');
    await expect(modal.locator('#import-tag-error-0')).toContainText('Укажите тег');
    await expect(confirmBtn).toBeDisabled();

    // Тег другой строки импорта
    await tagInput.fill('test-parsed-tag-1');
    await expect(modal.locator('#import-tag-error-0')).toContainText(
      'Тег «test-parsed-tag-1» уже занят'
    );
    await expect(modal.locator('#import-tag-error-1')).toContainText(
      'Тег «test-parsed-tag-1» уже занят'
    );
    await expect(confirmBtn).toBeDisabled();

    // Свободный тег снимает ошибки
    await tagInput.fill('free-tag');
    await expect(modal.locator('#import-tag-error-0')).toHaveCount(0);
    await expect(modal.locator('#import-tag-error-1')).toHaveCount(0);
    await expect(confirmBtn).toBeEnabled();
  });

  test('ошибка разбора ссылки показывает сообщение в окне импорта', async ({ page }) => {
    await page.locator('button:has-text("Импорт узла")').click();
    const modal = page.locator('.modal-container');
    await modal.locator('textarea').fill('invalid-link-format');
    await modal.locator('button:has-text("Распознать")').click();

    const errorMsg = modal.locator('.error-msg');
    await expect(errorMsg).toBeVisible();
    await expect(errorMsg).toContainText('Не удалось распознать ссылку');
  });

  test('кнопка «Импорт узла» присутствует в конструкторе Xray', async ({ page }) => {
    await expect(page.locator('button:has-text("Импорт узла")')).toBeVisible();
  });

  test('кнопка «Импорт узла» присутствует в конструкторе Mihomo', async ({ page }) => {
    await page.locator('.constructor-kernel-toggle button:has-text("Mihomo")').click();
    await expect(page.locator('button:has-text("Импорт узла")')).toBeVisible();
  });
});

test.describe('Импорт узла в подписках (D-16)', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupRestMocks(page);
  });

  test('кнопка «Импорт узла» отсутствует в подписках', async ({ page }) => {
    await page.goto('/#/proxies?tab=providers');
    await expect(page.locator('button:has-text("Импорт узла")')).not.toBeVisible();
  });
});
