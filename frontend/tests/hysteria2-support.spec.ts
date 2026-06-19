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

async function setupMocks(page: Page) {
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
              xray: { installed: true, version: '1.8.4', channel: 'stable' },
              mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
            },
            active_kernel: 'mihomo'
          }
        })
      });
    } else if (url.includes('/api/config/list')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            name: 'config.yaml',
            path: '/opt/etc/mihomo/config.yaml',
            size: 1500
          }
        ])
      });
    } else if (url.includes('/api/config/read')) {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'proxies: []\nproxy-groups: []\n'
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    }
  });
}

test.describe('Mihomo Generator Hysteria 2 Support and Validation', () => {
  test.beforeEach(async ({ page }) => {
    await disableServiceWorker(page);
    await setupMocks(page);
    await page.goto('/#/constructor');
    await page.waitForSelector('.gen-layout', { timeout: 8000 });
  });

  test('should render hysteria2 fields, trigger validation, and generate correct YAML', async ({ page }) => {
    // 1. Open Add proxy form
    const addProxyBtn = page.locator('button:has-text("Добавить прокси")');
    await expect(addProxyBtn).toBeVisible();
    await addProxyBtn.click();

    // 2. Select hysteria2 type
    const typeSelect = page.locator('.form-card select').first();
    await typeSelect.selectOption('hysteria2');

    // 3. Confirm hysteria2 specific fields are rendered
    await expect(page.locator('input[placeholder="password"]')).toBeVisible();
    await expect(page.locator('input[placeholder="example.com"]').first()).toBeVisible(); // Server/SNI
    
    // Obfs Select and Skip Cert Checkbox
    const obfsSelect = page.locator('div.form-row:has(label:has-text("Тип обфускации")), div.form-row:has(label:has-text("Obfuscation type"))').locator('select');
    await expect(obfsSelect).toBeVisible();
    
    const skipCertCheckbox = page.locator('input[type="checkbox"]');
    await expect(skipCertCheckbox).toBeVisible();

    // 4. Fill common fields
    await page.locator('input[placeholder="my-proxy"]').fill('hy2-test-node');
    await page.locator('input[placeholder="example.com"]').first().fill('my-hy2-server.com');
    await page.locator('input[type="number"]').fill('8443');
    await page.locator('input[placeholder="password"]').fill('mypass123');
    await page.locator('input[placeholder="example.com"]').nth(1).fill('sni-server.com'); // SNI

    // 5. Test validation for obfsType == 'simple' with empty password
    await obfsSelect.selectOption('simple');
    
    // Obfs password input should now be visible
    const obfsPasswordInput = page.locator('div.form-row:has(label:has-text("Пароль обфускации")), div.form-row:has(label:has-text("Obfuscation password"))').locator('input');
    await expect(obfsPasswordInput).toBeVisible();
    await obfsPasswordInput.fill(''); // Clear it

    // Try to save
    const saveBtn = page.locator('button:has-text("Добавить")');
    await saveBtn.click();

    // Toast error should appear
    const toast = page.locator('.toast--error');
    await expect(toast).toBeVisible();
    await expect(toast).toContainText('Пароль обфускации обязателен при типе simple');

    // 6. Complete form and save successfully
    await obfsPasswordInput.fill('obfs-secret-pass');
    await skipCertCheckbox.check();

    // Save again
    await saveBtn.click();

    // Form should close
    await expect(page.locator('.form-card')).not.toBeVisible();

    // 7. Verify node in list
    const itemRow = page.locator('.item-row:has-text("hy2-test-node")');
    await expect(itemRow).toBeVisible();
    await expect(itemRow.locator('.type-hysteria2')).toBeVisible();

    // 8. Verify generated YAML contains all details
    const yamlPreview = page.locator('.mihomo-yaml-preview, .yaml-preview, .constructor-preview-pane').first();
    await expect(yamlPreview).toBeVisible();
    const yamlText = await yamlPreview.textContent();

    expect(yamlText).toContain('type: hysteria2');
    expect(yamlText).toContain('password: "mypass123"');
    expect(yamlText).toContain('sni: "sni-server.com"');
    expect(yamlText).toContain('skip-cert-verify: true');
    expect(yamlText).toContain('obfs:');
    expect(yamlText).toContain('type: simple');
    expect(yamlText).toContain('password: "obfs-secret-pass"');
  });
});
