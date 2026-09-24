import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

const FILES: Record<string, string> = {
  '01_log': JSON.stringify({ log: { loglevel: 'warning' } }),
  '02_dns': JSON.stringify({
    dns: { tag: 'dns-in', servers: ['8.8.8.8'], queryStrategy: 'UseIP', hosts: {} }
  }),
  '03_inbounds': JSON.stringify({ inbounds: [] }),
  '04_outbounds': JSON.stringify({
    outbounds: [
      { tag: 'direct', protocol: 'freedom' },
      { tag: 'block', protocol: 'blackhole' },
      { tag: 'vless-a', protocol: 'vless' }
    ]
  }),
  // JSONC with a comment, a balancer rule and fields the form does not edit.
  '05_routing':
    '{\n  // managed by hand\n  "routing": {\n    "domainStrategy": "IPIfNonMatch",\n    "domainMatcher": "hybrid",\n' +
    '    "rules": [\n      { "type": "field", "domain": ["geosite:youtube"], "balancerTag": "best", "ruleTag": "yt" },\n' +
    '      { "type": "field", "source": ["172.16.0.5"], "user": ["kid@home"], "outboundTag": "block" },\n' +
    '      { "type": "field", "network": "tcp,udp", "outboundTag": "direct" }\n    ],\n' +
    '    "balancers": [{ "tag": "best", "selector": ["vless-"], "strategy": { "type": "leastPing" } }]\n  }\n}\n',
  '06_policy': JSON.stringify({ policy: { levels: { '0': { handshake: 4 } }, system: {} } })
};

test('Xray constructor keeps rule fields, stores disabled rules and saves only changed files', async ({
  page
}) => {
  const saved: Record<string, any> = {};
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      configurable: true
    });
    window.localStorage.setItem('lang', 'ru');
  });
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url());
    const method = route.request().method();
    if (url.pathname === '/api/auth/me') {
      return route.fulfill({
        json: { authenticated: true, setup_required: false, csrf_token: 't' }
      });
    }
    if (url.pathname === '/api/capabilities') {
      return route.fulfill({
        json: {
          success: true,
          data: {
            kernels: { xray: { installed: true }, mihomo: { installed: true } },
            active_kernel: 'xray'
          }
        }
      });
    }
    if (url.pathname === '/api/config/read') {
      const path = url.searchParams.get('path') || '';
      const key = Object.keys(FILES).find((k) => path.includes(k));
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: key ? FILES[key] : '{}'
      });
    }
    if (url.pathname === '/api/config/save' && method === 'POST') {
      const name = (url.searchParams.get('path') || '').split('/').pop() || '';
      saved[name] = JSON.parse(route.request().postData() || '{}');
      return route.fulfill({ json: { success: true } });
    }
    if (url.pathname === '/api/config/list' || url.pathname === '/api/templates/list') {
      return route.fulfill({ json: [] });
    }
    return route.fulfill({ json: { success: true, data: {} } });
  });

  await page.goto('/#/constructor');
  await page.locator('.constructor-kernel-toggle button:has-text("Xray")').click();
  const list = page.locator('[data-testid="routing-rules-list"]');
  await expect(list.locator('.rule-card')).toHaveCount(3);

  // Switch off the second rule (source/user -> block).
  await list.locator('.rule-card').nth(1).getByRole('switch').click();

  await page.locator('[data-testid="apply-changes-btn"]').click();
  const dialog = page.locator('[data-testid="apply-confirm-dialog"]');
  await expect(dialog.getByText('05_routing.json')).toBeVisible();
  await expect(dialog.getByText(/комментарии/)).toBeVisible();
  await dialog.locator('button.btn-primary').click();

  // Unchanged files are not written (log/dns get normalized defaults and
  // legitimately differ from the minimal mocks).
  await expect.poll(() => Object.keys(saved)).toContain('05_routing.json');
  for (const untouched of ['03_inbounds.json', '04_outbounds.json', '06_policy.json']) {
    expect(Object.keys(saved)).not.toContain(untouched);
  }
  const routing = saved['05_routing.json'].routing;
  expect(routing.domainMatcher).toBe('hybrid');
  expect(routing.rules).toEqual([
    { type: 'field', domain: ['geosite:youtube'], balancerTag: 'best', ruleTag: 'yt' },
    { type: 'field', network: 'tcp,udp', outboundTag: 'direct' }
  ]);
  expect(routing.xcpDisabledRules).toEqual([
    {
      type: 'field',
      source: ['172.16.0.5'],
      user: ['kid@home'],
      outboundTag: 'block',
      xcpPosition: 1
    }
  ]);
  expect(routing.balancers).toEqual([
    { tag: 'best', selector: ['vless-'], strategy: { type: 'leastPing' } }
  ]);
  expect(saved['05_routing.json'].observatory.subjectSelector).toEqual(['vless-']);
});

test('rule editor retargets a rule to a new balancer', async ({ page }) => {
  const saved: Record<string, any> = {};
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      configurable: true
    });
    window.localStorage.setItem('lang', 'ru');
  });
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url());
    const method = route.request().method();
    if (url.pathname === '/api/auth/me') {
      return route.fulfill({
        json: { authenticated: true, setup_required: false, csrf_token: 't' }
      });
    }
    if (url.pathname === '/api/capabilities') {
      return route.fulfill({
        json: {
          success: true,
          data: {
            kernels: { xray: { installed: true }, mihomo: { installed: true } },
            active_kernel: 'xray'
          }
        }
      });
    }
    if (url.pathname === '/api/config/read') {
      const path = url.searchParams.get('path') || '';
      const key = Object.keys(FILES).find((k) => path.includes(k));
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: key ? FILES[key] : '{}'
      });
    }
    if (url.pathname === '/api/config/save' && method === 'POST') {
      const name = (url.searchParams.get('path') || '').split('/').pop() || '';
      saved[name] = JSON.parse(route.request().postData() || '{}');
      return route.fulfill({ json: { success: true } });
    }
    if (url.pathname === '/api/config/list' || url.pathname === '/api/templates/list') {
      return route.fulfill({ json: [] });
    }
    return route.fulfill({ json: { success: true, data: {} } });
  });

  await page.goto('/#/constructor');
  await page.locator('.constructor-kernel-toggle button:has-text("Xray")').click();
  const list = page.locator('[data-testid="routing-rules-list"]');
  await expect(list.locator('.rule-card')).toHaveCount(3);

  // Add a round-robin balancer over vless- outbounds.
  await page.getByRole('button', { name: 'Добавить балансировщик' }).click();
  const card = page.locator('[data-testid="balancer-card"]').last();
  await card.getByLabel('Тег', { exact: true }).fill('rr');
  await card.getByLabel(/Селектор outbound/).fill('vless-');
  await card.getByLabel(/Селектор outbound/).blur();
  await card.getByLabel('Стратегия').selectOption('roundRobin');
  await expect(card.getByText('Попадают: vless-a')).toBeVisible();

  // Edit the third rule (network -> direct) to point to the balancer.
  await list.locator('.rule-card').nth(2).getByTestId('edit-routing-rule').click();
  const editor = page.getByTestId('xray-rule-editor');
  await editor.getByRole('button', { name: 'Балансировщик' }).click();
  await editor.getByLabel('Балансировщик', { exact: true }).selectOption('rr');
  await editor.getByRole('button', { name: 'Сохранить' }).click();
  await expect(list.locator('.rule-card').nth(2)).toContainText('⚖ rr');

  await page.locator('[data-testid="apply-changes-btn"]').click();
  await page.locator('[data-testid="apply-confirm-dialog"] button.btn-primary').click();
  await expect.poll(() => Object.keys(saved)).toContain('05_routing.json');
  const routing = saved['05_routing.json'].routing;
  expect(routing.rules[2]).toEqual({ type: 'field', network: 'tcp,udp', balancerTag: 'rr' });
  expect(routing.balancers).toContainEqual({
    tag: 'rr',
    selector: ['vless-'],
    strategy: { type: 'roundRobin' }
  });
});
