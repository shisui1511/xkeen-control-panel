/**
 * overflow-gate.spec.ts — Постоянный E2E-гейт контроля горизонтального скролла (overflow)
 * и доступности действий модальных диалогов конструктора на всех ключевых вьюпортах (AUDIT-06).
 */

import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Adaptive & Overflow Gate (AUDIT-06)', () => {
  const setupRoutes = async (page: any, activeKernel = 'mihomo') => {
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      window.localStorage.setItem('lang', 'ru');
    });

    await page.route('**/api/**', async (route: any) => {
      const url = route.request().url();
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: true,
            setup_required: false,
            csrf_token: 'mock-csrf'
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
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: activeKernel,
              mihomo: {
                reachable: true,
                process_running: true,
                api_reachable: true,
                api_authenticated: true
              }
            }
          })
        });
      } else if (url.includes('/api/config/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(
            activeKernel === 'mihomo'
              ? [{ name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 1200 }]
              : [
                  {
                    name: 'xray-config.json',
                    path: '/opt/etc/xray/configs/xray-config.json',
                    size: 1000
                  }
                ]
          )
        });
      } else if (url.includes('/api/config/read')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body:
            activeKernel === 'mihomo'
              ? 'port: 7890\nproxies: []\nproxy-groups: []\nrules: []\n'
              : JSON.stringify({ routing: { rules: [] }, outbounds: [], inbounds: [] })
        });
      } else if (url.includes('/api/templates/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/config/validate')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ valid: true })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  };

  const viewports = [
    { name: '1280x800 Desktop', width: 1280, height: 800 },
    { name: '1024x768 Tablet Landscape', width: 1024, height: 768 },
    { name: '768x1024 Tablet Portrait', width: 768, height: 1024 },
    { name: '393x852 Mobile iPhone', width: 393, height: 852 }
  ];

  for (const vp of viewports) {
    test(`Mihomo Constructor Modal reachability & overflow at ${vp.name}`, async ({ page }) => {
      await page.setViewportSize({ width: vp.width, height: vp.height });
      await setupRoutes(page, 'mihomo');
      await page.goto('/#/constructor');

      // Check horizontal overflow on constructor page
      const hasHScroll = await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 2
      );
      expect(hasHScroll, `Page has horizontal overflow on ${vp.name}`).toBe(false);

      // Open manual add node modal
      const manualBtn = page.locator('button.btn-secondary:has-text("+ Добавить вручную")');
      await manualBtn.click();

      const modalContainer = page.locator('.modal-container');
      await expect(modalContainer).toBeVisible();

      // Check wireguard + AWG (longest form state)
      const typeSelect = page.locator('#proxy-type');
      await typeSelect.selectOption('wireguard');
      const awgToggle = page.locator('#proxy-awg-enabled');
      await awgToggle.check();

      // Check action buttons visibility/reachability in viewport
      const saveBtn = page.locator('.modal-container button:has-text("Создать")');
      const cancelBtn = page.locator('.modal-container button:has-text("Отмена")');
      await expect(saveBtn).toBeVisible();
      await expect(cancelBtn).toBeVisible();

      const isSaveInViewport = await saveBtn.evaluate((el) => {
        const rect = el.getBoundingClientRect();
        return (
          rect.top >= 0 &&
          rect.bottom <= window.innerHeight &&
          rect.left >= 0 &&
          rect.right <= window.innerWidth
        );
      });
      expect(isSaveInViewport, `Save button must be inside viewport on ${vp.name}`).toBe(true);

      // Close modal
      await cancelBtn.click();
      await expect(modalContainer).not.toBeVisible();
    });
  }

  for (const vp of viewports) {
    test(`Xray Constructor Modal reachability & overflow at ${vp.name}`, async ({ page }) => {
      await page.setViewportSize({ width: vp.width, height: vp.height });
      await setupRoutes(page, 'xray');
      await page.goto('/#/constructor');

      // Check horizontal overflow on constructor page
      const hasHScroll = await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 2
      );
      expect(hasHScroll, `Page has horizontal overflow on ${vp.name}`).toBe(false);

      // Switch to outbounds
      const outboundsTab = page.locator('button.sec-tab[data-tab="outbounds"]');
      await outboundsTab.click();

      // Open manual add outbound modal
      const manualBtn = page.locator('.constructor-outbounds-header button.btn-secondary');
      await manualBtn.click();

      const modalContainer = page.locator('.modal-container');
      await expect(modalContainer).toBeVisible();

      // Check action buttons visibility/reachability
      const addBtn = page.locator('.modal-container button:has-text("Добавить")');
      const cancelBtn = page.locator('.modal-container button:has-text("Отмена")');
      await expect(addBtn).toBeVisible();
      await expect(cancelBtn).toBeVisible();

      const isAddInViewport = await addBtn.evaluate((el) => {
        const rect = el.getBoundingClientRect();
        return (
          rect.top >= 0 &&
          rect.bottom <= window.innerHeight &&
          rect.left >= 0 &&
          rect.right <= window.innerWidth
        );
      });
      expect(isAddInViewport, `Add button must be inside viewport on ${vp.name}`).toBe(true);

      // Close modal
      await cancelBtn.click();
      await expect(modalContainer).not.toBeVisible();
    });
  }
});
