import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Mihomo Generator fallback hidden rendering', () => {
  test.beforeEach(async ({ page }) => {
    // Disable Service Worker in tests
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
  });

  test('assert zkeen-selective preset contains hidden and max-failed-times in generated yaml fallback.*hidden', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Make sure we are in constructor
    const heading = page.locator('h1, h2, .constructor-header').first();
    await expect(heading).toBeVisible({ timeout: 5000 });

    // Since default_assets.json will be loaded by backend or mock if loaded from app,
    // let's click to load the preset if present, and check generated YAML.
    // In our test, the actual MihomoGenerator is rendered, and it loads assets schema from backend /api/assets/definition.
    // Wait, let's mock /api/assets/definition to return default assets!
    // But since the actual test runs on the running app (dev server or preview),
    // it will fetch definition from the Go backend.
    
    // Find preview container for yaml
    const yamlPreview = page.locator('.mihomo-yaml-preview, .yaml-preview, .constructor-preview-pane').first();
    await expect(yamlPreview).toBeVisible({ timeout: 8000 });
    const text = await yamlPreview.textContent();

    // Verify it contains Fallback and Fastest with hidden: true and max-failed-times: 3
    expect(text).toContain('Fallback');
    expect(text).toContain('Fastest');
    expect(text).toContain('hidden: true');
    expect(text).toContain('max-failed-times: 3');
  });

  test('assert UI terminology uses Пресеты instead of Сценарии preset.*terminology', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    // Wait for constructor layout to load
    await page.waitForSelector('.gen-layout', { timeout: 8000 });

    // Assert that the page does not contain «Сценарии» or «Сценарий» in UI labels (display values)
    // and contains «Пресет» or «Пресеты»
    const bodyText = await page.innerText('body');
    expect(bodyText).not.toContain('Сценарии Xray');
    expect(bodyText).not.toContain('Сценарий');
    expect(bodyText).toContain('Пресет');
  });
});
