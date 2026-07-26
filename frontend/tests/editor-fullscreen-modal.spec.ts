import { test, expect } from '@playwright/test';

// Принудительно задаем русский язык для тестов интерфейса
test.use({ locale: 'ru-RU' });

test.describe('Editor Fullscreen + Modal Regression (G-71-29)', () => {
  const fileContent = `port: 7890
socks-port: 7891
allow-lan: true
mode: Rule
log-level: info
proxies:
  - name: "HK-1"
    type: ss
    server: hk.server.com
    port: 443
    uuid: test-uuid
`;

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

    // Гермитичный стаб нативного Fullscreen API — headless Chromium требует user
    // activation и не отражает ограничение отрисовки в DOM, поэтому стабим вызовы
    // и проверяем сами факты вызова, а не визуальный эффект.
    await page.addInitScript(() => {
      let current: Element | null = null;
      (window as any).__exitFullscreenCalls = 0;

      Object.defineProperty(document, 'fullscreenElement', {
        get: () => current,
        configurable: true
      });

      Element.prototype.requestFullscreen = function () {
        current = this;
        document.dispatchEvent(new Event('fullscreenchange'));
        return Promise.resolve();
      } as any;

      (document as any).exitFullscreen = function () {
        (window as any).__exitFullscreenCalls += 1;
        current = null;
        document.dispatchEvent(new Event('fullscreenchange'));
        return Promise.resolve();
      };
    });

    // Перехватываем все запросы к API
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
              active_kernel: 'mihomo',
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
        const isMihomo = url.includes('mihomo');
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(
            isMihomo
              ? [
                  {
                    name: 'config.yaml',
                    path: '/opt/etc/mihomo/config.yaml',
                    size: 1500
                  }
                ]
              : [
                  {
                    name: 'xray-config.json',
                    path: '/opt/etc/xray/configs/xray-config.json',
                    size: 1200
                  }
                ]
          )
        });
      } else if (url.includes('/api/config/read')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: fileContent
        });
      } else if (url.includes('/api/config/backups')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/config/save')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else if (url.includes('/api/templates/list')) {
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

  async function openFile(page: import('@playwright/test').Page) {
    await page.goto('/#/editor');
    const fileRow = page.locator('.file-row:has-text("config.yaml")');
    await expect(fileRow).toBeVisible();
    await fileRow.click();
    const tab = page.locator('.editor-tab:has-text("config.yaml")');
    await expect(tab).toBeVisible();
  }

  test('enters native fullscreen via toolbar button', async ({ page }) => {
    await openFile(page);

    const fullscreenBtn = page.locator('.editor-cm-tool-btn');
    await expect(fullscreenBtn).toBeVisible();
    await fullscreenBtn.click();

    const wrapper = page.locator('.editor-cm-wrapper');
    await expect(wrapper).toHaveClass(/is-fullscreen/);
    const isFullscreenElement = await page.evaluate(() => {
      const wrapperEl = document.querySelector('.editor-cm-wrapper');
      return document.fullscreenElement === wrapperEl;
    });
    expect(isFullscreenElement).toBe(true);
  });

  test('G-71-29: SaveConfirm dialog leaves fullscreen and becomes visible on Ctrl+S', async ({
    page
  }) => {
    await openFile(page);

    await page.locator('.editor-cm-tool-btn').click();
    await expect(page.locator('.editor-cm-wrapper')).toHaveClass(/is-fullscreen/);

    const cmContent = page.locator('.cm-content');
    await expect(cmContent).toBeVisible();
    await cmContent.focus();
    await page.keyboard.type('\n# edited line\n');

    await page.keyboard.press('Control+s');

    const dialog = page.locator('[role="dialog"][aria-modal="true"]');
    await expect(dialog).toBeVisible();

    const exitCalls = await page.evaluate(() => (window as any).__exitFullscreenCalls);
    expect(exitCalls).toBe(1);

    const fullscreenElement = await page.evaluate(() => document.fullscreenElement);
    expect(fullscreenElement).toBeNull();

    await expect(page.locator('.editor-cm-wrapper')).not.toHaveClass(/is-fullscreen/);
  });

  test('Escape closes the open dialog instead of only leaving fullscreen', async ({ page }) => {
    await openFile(page);

    await page.locator('.editor-cm-tool-btn').click();
    const cmContent = page.locator('.cm-content');
    await cmContent.focus();
    await page.keyboard.type('\n# edited line\n');
    await page.keyboard.press('Control+s');

    const dialog = page.locator('[role="dialog"][aria-modal="true"]');
    await expect(dialog).toBeVisible();

    await page.keyboard.press('Escape');

    await expect(dialog).toHaveCount(0);

    const exitCalls = await page.evaluate(() => (window as any).__exitFullscreenCalls);
    expect(exitCalls).toBe(1);
  });

  test('normal path unchanged: no fullscreen call when nothing is fullscreened', async ({
    page
  }) => {
    await openFile(page);

    const cmContent = page.locator('.cm-content');
    await expect(cmContent).toBeVisible();
    await cmContent.focus();
    await page.keyboard.type('\n# edited line\n');

    await page.keyboard.press('Control+s');

    const dialog = page.locator('[role="dialog"][aria-modal="true"]');
    await expect(dialog).toBeVisible();

    const exitCalls = await page.evaluate(() => (window as any).__exitFullscreenCalls);
    expect(exitCalls).toBe(0);
  });
});
