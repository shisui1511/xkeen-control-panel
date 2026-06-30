import { test, expect, type Page } from '@playwright/test';

// ============================================================
// Console Sweep — автоматический CI-тест отсутствия console.error
// на всех 16 страницах (Mihomo-режим) + 8 страниц (Xray-режим).
// console.warn собирается отдельно и не роняет тест (D-05).
// ============================================================

type KernelMode = 'mihomo' | 'xray';

/**
 * Устанавливает моки API и возвращает page готовую к навигации.
 * authMode управляет ответом /api/auth/me:
 *   'authenticated' — обычный вход (для dashboard и пр.)
 *   'login'         — не аутентифицирован, setup_required=false (страница Login)
 *   'setup'         — не аутентифицирован, setup_required=true  (страница Setup)
 */
async function setupMocks(
  page: Page,
  kernel: KernelMode,
  authMode: 'authenticated' | 'login' | 'setup' = 'authenticated'
) {
  // Отключаем Service Worker, чтобы page.route перехватывал все запросы к API
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  await page.route('**/api/**', async (route) => {
    const url = route.request().url();

    if (url.includes('/api/auth/logout')) {
      const headers = route.request().headers();
      const csrfHeader = headers['x-csrf-token'];
      if (csrfHeader === 'mock-csrf-token') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else {
        await route.fulfill({
          status: 403,
          contentType: 'application/json',
          body: JSON.stringify({
            success: false,
            error: 'Forbidden (CSRF token missing or invalid)'
          })
        });
      }
    } else if (url.includes('/api/auth/me')) {
      if (authMode === 'login') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: false,
            setup_required: false,
            csrf_token: 'mock-csrf-token'
          })
        });
      } else if (authMode === 'setup') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: false,
            setup_required: true,
            csrf_token: 'mock-csrf-token'
          })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: true,
            setup_required: false,
            csrf_token: 'mock-csrf-token'
          })
        });
      }
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
            active_kernel: kernel,
            mihomo: {
              reachable: true,
              process_running: kernel === 'mihomo',
              api_reachable: kernel === 'mihomo',
              api_authenticated: kernel === 'mihomo'
            }
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
              channel: 'stable',
              status: 'idle',
              process_status: kernel === 'xray' ? 'running' : 'stopped',
              message: kernel === 'xray' ? 'running on background' : 'stopped'
            },
            {
              name: 'mihomo',
              display_name: 'Mihomo',
              binary_path: '/opt/bin/mihomo',
              current_version: '1.18.0',
              latest_version: '1.18.0',
              has_update: false,
              channel: 'stable',
              status: 'idle',
              process_status: kernel === 'mihomo' ? 'running' : 'stopped',
              message: kernel === 'mihomo' ? 'running on background' : 'stopped'
            }
          ]
        })
      });
    } else if (url.includes('/api/settings')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: { dev_mode: false }
        })
      });
    } else if (url.includes('/api/version')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: 'v0.16.0'
        })
      });
    } else if (url.includes('/api/system/stats')) {
      // Dashboard.svelte обращается к systemStats.go_runtime.go_version напрямую —
      // нужна полная структура с go_runtime, load и пр.
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
          dns_resolving: true,
          invalid_config: false,
          platform: 'linux',
          kernel_version: '5.15',
          ip_interface: 'eth0',
          timezone: 'Europe/Moscow',
          config_path: '/opt/etc/xray/config.json',
          config_lines: 0,
          boot_time: '2024-01-01T00:00:00Z'
        })
      });
    } else if (url.includes('/api/subscriptions')) {
      // SubscriptionList возвращает сырой JSON-массив (не обёрнутый в {success,data})
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    } else if (url.includes('/api/config/list')) {
      // ConfigList возвращает сырой JSON-массив файлов
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    } else if (url.includes('/api/traffic/quotas')) {
      // TrafficQuotas.svelte: $: activeQuotas = quotas.filter(...) — нужен массив
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    } else if (url.includes('/api/traffic/alerts')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    } else if (url.includes('/api/traffic/stats')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ download: 0, upload: 0, total: 0, reset_time: 0 })
      });
    } else if (url.includes('/api/smart-proxy/profiles')) {
      // SmartProxy.svelte: profiles = await res.json() — нужен массив
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    } else if (url.includes('/api/smart-proxy/status')) {
      // SmartProxy.svelte: status.active.length — нужен объект с полем active
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ active: [], next: [], time: '00:00', day: 0 })
      });
    } else if (url.includes('/api/update/status')) {
      // Settings.svelte: startStatusSSE() вызывается только если status != idle/done/failed
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'idle' })
      });
    } else if (url.includes('/api/dat/list')) {
      // DATManager.svelte: data.sort(...) — нужен массив
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([])
      });
    } else {
      // Fallback-заглушка для всех прочих /api/** (clash api, network tools и т.д.)
      // Возвращает пустой успешный ответ, чтобы страницы не падали с сетевой ошибкой,
      // которая сама по себе генерирует console.error
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    }
  });
}

// ============================================================
// Сбор console-сообщений: навешивать listeners ДО page.goto
// ============================================================

interface ConsoleRecord {
  level: string;
  text: string;
}

/**
 * Создаёт collectors до навигации, возвращает геттеры.
 * Listeners навешиваются ДО goto, чтобы ранние ошибки не терялись.
 */
function attachConsoleCollectors(page: Page) {
  const errors: ConsoleRecord[] = [];
  const warnings: ConsoleRecord[] = [];

  page.on('console', (msg) => {
    const level = msg.type();
    const text = msg.text();
    if (level === 'error') {
      errors.push({ level, text });
    } else if (level === 'warning') {
      warnings.push({ level, text });
    }
  });

  page.on('pageerror', (err) => {
    errors.push({ level: 'pageerror', text: err.message });
  });

  return { errors, warnings };
}

// ============================================================
// Вспомогательная функция обхода страницы с ожиданием загрузки
// ============================================================

async function visitPage(page: Page, url: string): Promise<void> {
  await page.goto(url);
  try {
    // networkidle может не наступить из-за WS-соединений — используем таймаут
    await page.waitForLoadState('networkidle', { timeout: 3000 });
  } catch {
    // Если networkidle не достигнут за 3с — достаточно domcontentloaded
    await page.waitForLoadState('domcontentloaded');
    // Даём время на обработку оставшихся промисов
    await page.waitForTimeout(500);
  }
}

// ============================================================
// MIHOMO-проход: 16 страниц
// ============================================================

test.describe('Mihomo mode — console sweep (16 pages)', () => {
  // Страницы аутентификации проверяются с особыми auth-моками

  test('mihomo:login has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo', 'login');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:login warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:login: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:setup has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo', 'setup');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:setup warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:setup: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:dashboard has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/dashboard');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:dashboard warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:dashboard: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:services has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/services');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:services warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:services: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:connections has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/connections');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:connections warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:connections: ${JSON.stringify(errors)}`).toHaveLength(
      0
    );
  });

  test('mihomo:proxies has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/proxies');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:proxies warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:proxies: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:rules has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/rules');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:rules warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:rules: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:smartproxy has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/smartproxy');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:smartproxy warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:smartproxy: ${JSON.stringify(errors)}`).toHaveLength(
      0
    );
  });

  test('mihomo:traffic has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/traffic');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:traffic warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:traffic: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:trafficquotas has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/trafficquotas');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:trafficquotas warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(
      errors,
      `console errors on mihomo:trafficquotas: ${JSON.stringify(errors)}`
    ).toHaveLength(0);
  });

  test('mihomo:logs has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/logs');
    if (warnings.length > 0) {
      console.log('[console-sweep] mihomo:logs warnings:', warnings.map((w) => w.text).join(' | '));
    }
    expect(errors, `console errors on mihomo:logs: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:editor has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/editor');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:editor warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:editor: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:dat has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/dat');
    if (warnings.length > 0) {
      console.log('[console-sweep] mihomo:dat warnings:', warnings.map((w) => w.text).join(' | '));
    }
    expect(errors, `console errors on mihomo:dat: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:network has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/network');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:network warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:network: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('mihomo:subscriptions has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/subscriptions');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:subscriptions warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(
      errors,
      `console errors on mihomo:subscriptions: ${JSON.stringify(errors)}`
    ).toHaveLength(0);
  });

  test('mihomo:settings has no console errors', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/settings');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] mihomo:settings warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on mihomo:settings: ${JSON.stringify(errors)}`).toHaveLength(0);
  });
});

// ============================================================
// XRAY-проход: 8 общих страниц (без Mihomo-специфичных вкладок)
// Страницы connections/proxies/rules/smartproxy/traffic/trafficquotas
// скрыты в Xray-режиме через context-aware UI (Phase 9.10) — не проверяются.
// ============================================================

test.describe('Xray mode — console sweep (8 pages)', () => {
  test('xray:dashboard has no console errors', async ({ page }) => {
    await setupMocks(page, 'xray');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/dashboard');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] xray:dashboard warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on xray:dashboard: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('xray:services has no console errors', async ({ page }) => {
    await setupMocks(page, 'xray');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/services');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] xray:services warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on xray:services: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('xray:logs has no console errors', async ({ page }) => {
    await setupMocks(page, 'xray');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/logs');
    if (warnings.length > 0) {
      console.log('[console-sweep] xray:logs warnings:', warnings.map((w) => w.text).join(' | '));
    }
    expect(errors, `console errors on xray:logs: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('xray:editor has no console errors', async ({ page }) => {
    await setupMocks(page, 'xray');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/editor');
    if (warnings.length > 0) {
      console.log('[console-sweep] xray:editor warnings:', warnings.map((w) => w.text).join(' | '));
    }
    expect(errors, `console errors on xray:editor: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('xray:dat has no console errors', async ({ page }) => {
    await setupMocks(page, 'xray');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/dat');
    if (warnings.length > 0) {
      console.log('[console-sweep] xray:dat warnings:', warnings.map((w) => w.text).join(' | '));
    }
    expect(errors, `console errors on xray:dat: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('xray:network has no console errors', async ({ page }) => {
    await setupMocks(page, 'xray');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/network');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] xray:network warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on xray:network: ${JSON.stringify(errors)}`).toHaveLength(0);
  });

  test('xray:subscriptions has no console errors', async ({ page }) => {
    await setupMocks(page, 'xray');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/subscriptions');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] xray:subscriptions warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on xray:subscriptions: ${JSON.stringify(errors)}`).toHaveLength(
      0
    );
  });

  test('xray:settings has no console errors', async ({ page }) => {
    await setupMocks(page, 'xray');
    const { errors, warnings } = attachConsoleCollectors(page);
    await visitPage(page, '/#/settings');
    if (warnings.length > 0) {
      console.log(
        '[console-sweep] xray:settings warnings:',
        warnings.map((w) => w.text).join(' | ')
      );
    }
    expect(errors, `console errors on xray:settings: ${JSON.stringify(errors)}`).toHaveLength(0);
  });
});

test.describe('Logout CSRF Verification', () => {
  test('logout request sends X-CSRF-Token and succeeds', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    await visitPage(page, '/#/dashboard');
    const token = await page.evaluate(() => localStorage.getItem('csrf_token'));
    expect(token).toBe('mock-csrf-token');
    const logoutBtn = page.locator('button.nav-item', { hasText: /Выйти|Logout/ });
    await expect(logoutBtn).toBeVisible();
    const logoutResponsePromise = page.waitForResponse('**/api/auth/logout');
    await logoutBtn.click();
    const logoutResponse = await logoutResponsePromise;
    expect(logoutResponse.status()).toBe(200);
  });
});
