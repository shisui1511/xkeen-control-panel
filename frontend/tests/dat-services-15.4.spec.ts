import { test, expect } from '@playwright/test';

test.describe('Phase 15.4 Visual and Logic Fixes', () => {
  test.beforeEach(async ({ page }) => {
    // Отключаем Service Worker в тестах, чтобы запросы к API перехватывались через page.route
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
    });
  });

  test('Services page: restart log has ct-actions wrapper and kernels do not duplicate versions', async ({
    page
  }) => {
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
      } else if (url.includes('/api/service/status')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              is_running: true,
              active_kernel: 'xray',
              pid: 1234,
              uptime: '2h',
              binary_path: '/opt/sbin/xkeen'
            }
          })
        });
      } else if (url.includes('/api/kernels')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: [
              {
                name: 'xray',
                display_name: 'Xray-core',
                binary_path: '/opt/bin/xray',
                current_version: '1.8.4',
                latest_version: '1.8.4',
                has_update: false,
                status: 'idle',
                process_status: 'running',
                message: 'running'
              },
              {
                name: 'mihomo',
                display_name: 'Mihomo',
                binary_path: '/opt/bin/mihomo',
                current_version: '1.18.0',
                latest_version: '1.18.0',
                has_update: false,
                status: 'idle',
                process_status: 'stopped',
                message: 'stopped'
              }
            ]
          })
        });
      } else if (url.includes('/api/service/restart-log')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              action: 'restart',
              success: true,
              timestamp: Math.floor(Date.now() / 1000) - 60,
              output: 'log output'
            }
          ])
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });

    await page.goto('/#/services');
    await page.waitForLoadState('networkidle');

    // Проверяем наличие .ct-actions обертки вокруг кнопки в заголовке истории перезапусков
    const restartHeader = page.locator('.restart-card .card-head-row');
    await expect(restartHeader).toBeVisible();
    const ctActions = restartHeader.locator('.ct-actions');
    await expect(ctActions).toBeVisible();
    await expect(ctActions.locator('button')).toBeVisible();

    // Проверяем, что в k-meta описании Xray нет дублирования версии v1.8.4
    const xrayMeta = page.locator('.kernel-card:has-text("Xray") .k-body .k-meta');
    await expect(xrayMeta).toBeVisible();
    const xrayText = await xrayMeta.innerText();
    expect(xrayText).not.toContain('v1.8.4');
    expect(xrayText).toContain('PID');

    // Проверяем, что в k-meta описании Mihomo нет дублирования clash-meta и версии v1.18.0
    const mihomoMeta = page.locator('.kernel-card:has-text("Mihomo") .k-body .k-meta');
    await expect(mihomoMeta).toBeVisible();
    const mihomoText = await mihomoMeta.innerText();
    expect(mihomoText).not.toContain('v1.18.0');
    expect(mihomoText).not.toContain('clash-meta');
  });

  test('DAT Manager: dynamically filters files and counts stats correctly based on active kernel', async ({
    page
  }) => {
    let currentActiveKernel = 'xray';
    page.on('console', (msg) => console.log('PAGE LOG:', msg.text()));

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
        console.log(
          `[mock] /api/capabilities requested, returning active_kernel = ${currentActiveKernel}`
        );
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
              active_kernel: currentActiveKernel
            }
          })
        });
      } else if (url.includes('/api/dat/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              name: 'geoip.dat',
              path: '/xray/geoip.dat',
              size: 1024 * 1024,
              last_update: Math.floor(Date.now() / 1000) - 3600,
              exists: true,
              type: 'xray'
            },
            {
              name: 'geosite.dat',
              path: '/xray/geosite.dat',
              size: 2 * 1024 * 1024,
              last_update: Math.floor(Date.now() / 1000) - 3600,
              exists: true,
              type: 'xray'
            },
            {
              name: 'geoip.metadb',
              path: '/mihomo/geoip.metadb',
              size: 5 * 1024 * 1024,
              last_update: Math.floor(Date.now() / 1000) - 86400 * 45,
              exists: true,
              type: 'mihomo'
            }, // outdated
            {
              name: 'custom.dat',
              path: '/custom.dat',
              size: 512,
              last_update: Math.floor(Date.now() / 1000) - 3600,
              exists: true,
              type: 'other'
            }
          ])
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });

    // 1. Загружаем DAT Manager с активным ядром xray
    await page.goto('/#/dat');
    await page.waitForLoadState('networkidle');

    // Для Xray должны показываться: geoip.dat, geosite.dat и custom.dat (displayedFiles.length = 3)
    // Все 3 файла актуальны (не outdated).
    const statsTextXray = await page.locator('.stats').innerText();
    expect(statsTextXray).toMatch(/3\s+(Files|Файлов)/i);
    expect(statsTextXray).toMatch(/3\s+(active|актуальных)/i);
    expect(statsTextXray).not.toContain('отсутствует');

    // 2. Меняем активное ядро на mihomo и перезагружаем страницу
    currentActiveKernel = 'mihomo';
    await page.reload();
    await page.waitForLoadState('networkidle');

    // Для Mihomo должны показываться: geoip.metadb и custom.dat (displayedFiles.length = 2)
    // geoip.metadb устарел (>30 дней), custom.dat актуален. Итого: 1 актуальный.
    const statsTextMihomo = await page.locator('.stats').innerText();
    expect(statsTextMihomo).toMatch(/2\s+(Files|Файлов)/i);
    expect(statsTextMihomo).toMatch(/1\s+(active|актуальных)/i);
  });

  test('DAT Manager: tag browser correctly unpacks tags and GeoScan modal performs lookup', async ({
    page
  }) => {
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
                xray: { installed: true, version: '1.8.4', channel: 'stable' }
              },
              active_kernel: 'xray'
            }
          })
        });
      } else if (url.includes('/api/dat/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              name: 'geosite.dat',
              path: '/xray/geosite.dat',
              size: 2048,
              last_update: Math.floor(Date.now() / 1000) - 3600,
              exists: true,
              type: 'xray',
              geo_type: 'geosite',
              tag_count: 5
            },
            {
              name: 'zkeen.dat',
              path: '/xray/zkeen.dat',
              size: 1024,
              last_update: Math.floor(Date.now() / 1000) - 3600,
              exists: true,
              type: 'xray',
              geo_type: 'geosite',
              tag_count: 2
            }
          ])
        });
      } else if (url.includes('/api/dat/tags?name=geosite.dat')) {
        // apiFetchJSON unpacks response.data or raw array
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: [
              { tag: 'google', count: 120 },
              { tag: 'youtube', count: 45 }
            ]
          })
        });
      } else if (url.includes('/api/dat/lookup')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: [
              {
                file: 'geosite.dat',
                type: 'geosite',
                tag: 'youtube',
                rule: 'geosite:youtube',
                match_count: 1,
                sample_matches: ['youtube.com']
              },
              {
                file: 'zkeen.dat',
                type: 'geosite',
                tag: 'antizapret',
                rule: 'ext:zkeen.dat:antizapret',
                match_count: 1,
                sample_matches: ['youtube.com']
              }
            ]
          })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });

    await page.goto('/#/dat');
    await page.waitForLoadState('networkidle');

    // 1. Проверяем открытие Tag Browser и распаковку тегов
    const tagsBtn = page
      .locator(
        '.db-card-item:has-text("geosite.dat"), .dat-row:has-text("geosite.dat") button:has-text("Tags"), .dat-row:has-text("geosite.dat") button:has-text("Теги")'
      )
      .first();
    await tagsBtn.click();

    // Теги должны отрендериться (не пустой список)
    await expect(page.locator('.td-tag-name:has-text("google")')).toBeVisible();
    await expect(page.locator('.td-tag-name:has-text("youtube")')).toBeVisible();

    // Закрываем Tag Browser
    await page.locator('.td-close').first().click();

    // 2. Открываем GeoScan
    const scanBtn = page.locator('button:has-text("GeoScan"), button:has-text("Скан геофайлов")');
    await scanBtn.click();

    const scanInput = page.locator('.geoscan-input');
    await expect(scanInput).toBeVisible();

    await scanInput.fill('youtube.com');
    await page
      .locator(
        '.geoscan-search-bar button:has-text("Search"), .geoscan-search-bar button:has-text("Поиск")'
      )
      .click();

    // Проверяем результаты
    await expect(page.locator('.geoscan-result-card')).toHaveCount(2);
    await expect(page.locator('.geoscan-rule-code:has-text("geosite:youtube")')).toBeVisible();
    await expect(
      page.locator('.geoscan-rule-code:has-text("ext:zkeen.dat:antizapret")')
    ).toBeVisible();
  });
});
