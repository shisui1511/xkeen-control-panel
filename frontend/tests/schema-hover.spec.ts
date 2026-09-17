import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Editor Schema Tooltip UI/UX', () => {
  test.beforeEach(async ({ page }) => {
    // Безопасно мокаем Service Worker с пустым методом register, чтобы избежать JS ошибок в index.html
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: {
          register: () => Promise.resolve({}),
          addEventListener: () => {},
          removeEventListener: () => {},
          getRegistrations: () => Promise.resolve([])
        },
        writable: false,
        configurable: true
      });
    });

    // Mock API
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ authenticated: true, csrf_token: 'token' })
        });
      } else if (url.includes('/api/version')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ version: '0.25.0' })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              kernels: {
                xray: { installed: true, version: '1.8.4' },
                mihomo: { installed: true, version: '1.18.0' }
              },
              active_kernel: 'mihomo'
            }
          })
        });
      } else if (url.includes('/api/config/list')) {
        const isMihomo = url.includes('mihomo');
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(
            isMihomo
              ? [{ name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 1000 }]
              : [
                  {
                    name: '05_routing.json',
                    path: '/opt/etc/xray/configs/05_routing.json',
                    size: 500
                  }
                ]
          )
        });
      } else if (url.includes('/api/config/read')) {
        const isMihomo = url.includes('mihomo');
        const content = isMihomo
          ? `port: 7890\nmode: rule\nlog-level: info\n`
          : `{\n  "routing": {\n    "domainStrategy": "IPIfNonMatch"\n  }\n}\n`;
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: content
        });
      } else if (url.includes('/api/config/backups')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
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

  test('displays enhanced schema hover tooltip on config keys', async ({ page }) => {
    await page.goto('/#/editor');

    // Open config.yaml
    const fileRow = page.locator('.file-row:has-text("config.yaml")');
    await expect(fileRow).toBeVisible();
    await fileRow.click();

    // Wait for editor line with log-level to appear
    const line = page.locator('.cm-line:has-text("log-level")');
    await expect(line).toBeVisible();

    // Hover over the word log-level
    await line.hover({ position: { x: 30, y: 10 } });

    // Wait for tooltip
    const tooltip = page.locator('.cm-schema-tooltip');
    await expect(tooltip).toBeVisible({ timeout: 5000 });

    // Verify header structure
    await expect(tooltip.locator('.cm-schema-tooltip-name')).toHaveText('log-level');
    await expect(tooltip.locator('.cm-schema-badge-type')).toBeVisible();

    // Verify enum pills
    const enumPills = tooltip.locator('.cm-schema-enum-pill');
    await expect(enumPills.first()).toBeVisible();

    // Take a screenshot of the tooltip
    await page.screenshot({ path: 'schema-tooltip-hover.png' });
  });

  test('displays breadcrumbs and nested property schema for Xray JSON', async ({ page }) => {
    await page.goto('/#/editor');

    // Open 05_routing.json
    const fileRow = page.locator('.file-row:has-text("05_routing.json")');
    await expect(fileRow).toBeVisible();
    await fileRow.click();

    // Hover over domainStrategy
    const line = page.locator('.cm-line:has-text("domainStrategy")');
    await expect(line).toBeVisible();

    await line.hover({ position: { x: 50, y: 10 } });

    // Wait for tooltip
    const tooltip = page.locator('.cm-schema-tooltip');
    await expect(tooltip).toBeVisible({ timeout: 5000 });

    // Verify breadcrumb
    await expect(tooltip.locator('.cm-schema-tooltip-path')).toHaveText('routing › ');
    await expect(tooltip.locator('.cm-schema-tooltip-name')).toHaveText('domainStrategy');
    await expect(tooltip.locator('.cm-schema-badge-type')).toHaveText('string');

    // Verify enums
    await expect(tooltip.locator('.cm-schema-tooltip-enums')).toBeVisible();
    await expect(tooltip.locator('.cm-schema-enum-pill:has-text("AsIs")')).toBeVisible();
    await expect(tooltip.locator('.cm-schema-enum-pill:has-text("IPIfNonMatch")')).toBeVisible();

    // Screenshot
    await page.screenshot({ path: 'schema-tooltip-xray-hover.png' });
  });
});
