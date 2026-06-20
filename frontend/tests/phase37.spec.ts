import { test, expect } from '@playwright/test';

test.describe('Phase 37 integration tests (DAT Deep Search & Port Collision Warning)', () => {
  test.beforeEach(async ({ page }) => {
    // Disable Service Worker in tests
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });

    // Mock general endpoints
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
              active_kernel: 'xray'
            }
          })
        });
      } else if (url.includes('/api/config/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              name: '04_outbounds.manual.json',
              path: '/opt/etc/xray/configs/04_outbounds.manual.json',
              size: 800
            }
          ])
        });
      } else if (url.includes('/api/subscriptions/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/dat/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              name: 'geosite.dat',
              path: '/xray/geosite.dat',
              size: 2 * 1024 * 1024,
              last_update: Math.floor(Date.now() / 1000) - 3600,
              exists: true,
              type: 'xray',
              tag_count: 2
            }
          ])
        });
      } else if (url.includes('/api/dat/tags')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            tags: [
              { tag: 'google', count: 4 },
              { tag: 'direct', count: 1 }
            ]
          })
        });
      } else if (url.includes('/api/dat/search')) {
        const query = new URL(url).searchParams.get('query') || '';
        const pageNum = new URL(url).searchParams.get('page') || '0';

        if (query === 'ru') {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              entries: ['google.ru'],
              total: 1,
              has_more: false
            })
          });
        } else if (pageNum === '1') {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              entries: ['google.co.uk', 'google.de'],
              total: 4,
              has_more: false
            })
          });
        } else {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              entries: ['google.com', 'google.ru'],
              total: 4,
              has_more: true
            })
          });
        }
      } else if (url.includes('/api/config/read') && url.includes('03_inbounds.json')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            inbounds: [
              { port: 1182, protocol: 'socks', tag: 'socks-inbound' }
            ]
          })
        });
      } else if (url.includes('/api/config/read') && url.includes('config.yaml')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/yaml',
          body: `
port: 7890
socks-port: 7891
redir-port: 1182
mixed-port: 7893
proxies:
  - name: dummy-proxy
    type: ss
    server: 127.0.0.1
    port: 8388
    cipher: aes-256-gcm
    password: password
`
        });
      } else if (url.includes('/api/config/read') && url.includes('04_outbounds.manual.json')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ outbounds: [{ tag: 'my-proxy', protocol: 'vless' }] })
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

  test('DAT Manager: Deep Search, Debounce and Pagination (D-02, D-03)', async ({ page }) => {
    // 1. Go to DAT manager page
    await page.goto('/#/dat');
    await page.waitForLoadState('networkidle');

    // 2. Click Tags button for geosite.dat
    const tagsButton = page.locator('.dat-row:has-text("geosite.dat") button:has-text("Теги"), .dat-row:has-text("geosite.dat") button:has-text("Tags")').first();
    await expect(tagsButton).toBeVisible();
    await tagsButton.click();

    // 3. Verify tag drawer opens and shows the tags list
    const googleTag = page.locator('.tag-item-name:has-text("google"), button:has-text("google")').first();
    await expect(googleTag).toBeVisible({ timeout: 5000 });
    await googleTag.click();

    // 4. Verify entry drawer opens and shows page 0 entries
    await expect(page.locator('text=google.com')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('text=google.ru')).toBeVisible();

    // 5. Click "Load More" button to get page 1 entries
    const loadMoreBtn = page.locator('button:has-text("Загрузить еще"), button:has-text("Load More")');
    await expect(loadMoreBtn).toBeVisible();
    await loadMoreBtn.click();

    // 6. Verify page 1 entries loaded alongside page 0 entries
    await expect(page.locator('text=google.co.uk')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('text=google.de')).toBeVisible();

    // 7. Verify search input filtering with debounce
    const searchInput = page.locator('.td-search-input');
    await expect(searchInput).toBeVisible();
    await searchInput.fill('ru');
    await page.waitForTimeout(400); // Wait for debounce timer (300ms) to fire

    // 8. Verify only "google.ru" matches the search
    await expect(page.locator('text=google.ru')).toBeVisible();
    await expect(page.locator('text=google.com')).not.toBeVisible();
  });

  test('Mihomo Generator: Port Collision Warning (D-07)', async ({ page }) => {
    // Intercept browser confirm dialog
    let dialogTriggered = false;
    let dialogMsg = '';
    page.on('dialog', async (dialog) => {
      dialogTriggered = true;
      dialogMsg = dialog.message();
      await dialog.dismiss(); // Simulate clicking 'Cancel'
    });

    // 1. Go to Constructor page
    await page.goto('/#/constructor');
    await page.waitForLoadState('networkidle');

    // Make sure we switch to Mihomo Generator tab
    const mihomoTab = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")').first();
    await expect(mihomoTab).toBeVisible({ timeout: 5000 });
    await mihomoTab.click();

    // 2. Locate the Save/Apply button on the generator page
    const applyBtn = page.locator('button[data-testid="apply-changes-btn"]').first();
    await expect(applyBtn).toBeVisible({ timeout: 5000 });
    await applyBtn.click();

    // Wait for the Svelte confirmation modal to appear
    const confirmModal = page.locator('[data-testid="apply-confirm-dialog"]');
    await expect(confirmModal).toBeVisible({ timeout: 5000 });

    // Click the "Apply and Restart" button in the modal to trigger the collision check
    const confirmBtn = confirmModal.locator('button:has-text("Apply and Restart"), button:has-text("Применить и перезапустить")').first();
    await expect(confirmBtn).toBeVisible();
    await confirmBtn.click();

    // 3. Verify dialog popped up with the port collision details
    await page.waitForTimeout(500);
    expect(dialogTriggered).toBe(true);
    expect(dialogMsg).toContain('1182');
    expect(dialogMsg).toContain('mihomo');
    expect(dialogMsg).toContain('xray');
  });
});
