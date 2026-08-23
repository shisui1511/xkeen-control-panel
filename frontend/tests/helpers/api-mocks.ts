import type { Page } from '@playwright/test';

// ============================================================
// Общий хелпер API-моков и навигации для e2e-тестов панели.
// Вынесен из console-sweep.spec.ts (без изменения тел функций),
// чтобы responsive-sweep.spec.ts и editor-design-system.spec.ts
// могли переиспользовать одну и ту же логику моков.
// ============================================================

export type KernelMode = 'mihomo' | 'xray';

/**
 * Устанавливает моки API и возвращает page готовую к навигации.
 * authMode управляет ответом /api/auth/me:
 *   'authenticated' — обычный вход (для dashboard и пр.)
 *   'login'         — не аутентифицирован, setup_required=false (страница Login)
 *   'setup'         — не аутентифицирован, setup_required=true  (страница Setup)
 */
export async function setupMocks(
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
    } else if (url.includes('/api/subscriptions') || url.includes('/api/proxy-providers')) {
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
// Вспомогательная функция обхода страницы с ожиданием загрузки
// ============================================================

export async function visitPage(page: Page, url: string): Promise<void> {
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
