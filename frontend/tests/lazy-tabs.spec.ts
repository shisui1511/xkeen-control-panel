import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Lazy tab loading (Logs)', () => {
  test.beforeEach(async ({ page }) => {
    // Отключаем Service Worker в тестах, чтобы запросы к API перехватывались через page.route
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
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
              mihomo: {
                installed: true,
                reachable: true,
                api_reachable: true,
                process_running: true
              },
              kernels: { xray: { installed: false }, mihomo: { installed: true } }
            }
          })
        });
      } else if (url.includes('/api/system/stats')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            memory: { total: 524288000, used: 131072000, free: 393216000 },
            disk: { total: 536870912, used: 209715200, free: 327155712 },
            ssl_cert_days: 10,
            load: [0.5, 0.4, 0.3],
            uptime: { seconds: 3600, days: 0, hours: 1, minutes: 0 },
            go_runtime: {
              goroutines: 10,
              heap_alloc: 4194304,
              heap_sys: 8388608,
              num_gc: 5,
              go_version: 'go1.21.0',
              gomaxprocs: 4,
              goarch: 'arm64'
            },
            router_model: 'Keenetic',
            hostname: 'keenetic',
            wan_status: 'connected',
            default_gateway: '192.168.1.1',
            dns_servers: ['8.8.8.8'],
            dns_resolving: true
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
  });

  test('ленивый чанк вкладки логов грузится по клику', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.dashboard-layout')).toBeVisible();

    await page.locator('a[href="#/logs"]').click();

    await expect(page.locator('a[href="#/logs"]')).toHaveAttribute('aria-current', 'page');
    // Дожидаемся отрисовки контента вкладки логов (не Skeleton)
    await expect(page.locator('.skeleton-card')).not.toBeVisible({ timeout: 10000 });
  });

  test('ошибка загрузки чанка показывает тост с кнопкой повтора', async ({ page }) => {
    // Матчим точный путь к ленивому компоненту вкладки: dev-режим отдаёт
    // модуль как `/src/Logs.svelte`, production-сборка — как
    // `/assets/Logs-[hash].js`. Паттерн намеренно НЕ матчит
    // `src/lib/components/icons/Logs.svelte` (статическая иконка сайдбара,
    // грузится при старте приложения и не должна быть перехвачена).
    const logsChunkPattern = /\/(src\/Logs\.svelte|assets\/Logs-[^/]+\.js)(\?.*)?$/;
    await page.route(logsChunkPattern, async (route) => {
      await route.abort();
    });

    await page.goto('/');
    await expect(page.locator('.dashboard-layout')).toBeVisible();

    await page.locator('a[href="#/logs"]').click();

    await expect(page.getByRole('heading', { name: 'Не удалось загрузить раздел' })).toBeVisible({
      timeout: 10000
    });
    const retryButton = page.getByRole('button', { name: 'Повторить' }).first();
    await expect(retryButton).toBeVisible();

    // Снимаем перехват, чтобы повторная попытка успешно загрузила чанк
    await page.unroute(logsChunkPattern);
    await retryButton.click();

    await expect(page.locator('.skeleton-card')).not.toBeVisible({ timeout: 10000 });
    await expect(
      page.getByRole('heading', { name: 'Не удалось загрузить раздел' })
    ).not.toBeVisible();
  });

  const lazyTabs = ['proxies', 'settings', 'dat', 'console'];

  for (const tab of lazyTabs) {
    test(`ленивый чанк вкладки «${tab}» грузится по клику и отрисовывается`, async ({ page }) => {
      await page.goto('/');
      await expect(page.locator('.dashboard-layout')).toBeVisible();

      const navItem = page.locator(`a[href="#/${tab}"]`);
      await navItem.click();

      await expect(navItem).toHaveAttribute('aria-current', 'page');
      // Дожидаемся исчезновения Skeleton (чанк успешно загружен) и появления
      // реального содержимого вкладки (не пустой контейнер).
      await expect(page.locator('.skeleton-card')).toHaveCount(0, { timeout: 10000 });
      await expect(page.locator('.main-content')).not.toBeEmpty();
    });
  }
});
