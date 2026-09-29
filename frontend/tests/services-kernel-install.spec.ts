import { test, expect, type Page } from '@playwright/test';
import { kernelsFixture, systemStatsFixture } from './helpers/api-mocks';

// Страница «Сервисы»: опрос статуса ядер не должен зацикливаться после
// установки или ошибки, пустой ответ списка ядер не ломает страницу,
// а возраст статуса XKeen показывается тихим бейджем.

interface MockState {
  kernels: unknown;
  /** Поля data ответа /api/service/status поверх базовых. */
  serviceStatus?: Record<string, unknown>;
}

interface Counters {
  kernelsList: number;
  kernelStatus: Record<string, number>;
}

async function mockRoutes(page: Page, state: MockState): Promise<Counters> {
  const counters: Counters = { kernelsList: 0, kernelStatus: {} };

  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  await page.route('**/api/**', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const json = (body: unknown) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });

    if (path === '/api/auth/me') {
      return json({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf-token' });
    }
    if (path === '/api/capabilities') {
      return json({
        success: true,
        data: {
          kernels: {
            xray: { installed: true, version: '1.8.4', channel: 'stable' },
            mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
          },
          active_kernel: 'xray',
          xkeen_installed: true,
          mihomo: { reachable: true, process_running: false, api_reachable: false }
        }
      });
    }
    if (path === '/api/settings') return json({ success: true, data: { dev_mode: false } });
    if (path === '/api/version') return json({ success: true, data: 'v0.25.0' });
    if (path === '/api/service/restart-log') return json([]);
    if (path === '/api/service/status') {
      return json({
        success: true,
        data: {
          is_running: true,
          active_kernel: 'xray',
          pid: 1234,
          uptime: '1h',
          binary_path: '/opt/sbin/xkeen',
          raw: 'Xray-core (running)\nXKeen is running',
          xkeen_installed: true,
          xkeen_installer_available: true,
          xkeen_setup_incomplete: false,
          ...state.serviceStatus
        }
      });
    }
    if (path === '/api/system/stats') return json(systemStatsFixture());

    const statusMatch = path.match(/^\/api\/kernels\/([^/]+)\/status$/);
    if (statusMatch && req.method() === 'GET') {
      const name = statusMatch[1];
      counters.kernelStatus[name] = (counters.kernelStatus[name] ?? 0) + 1;
      const list = Array.isArray(state.kernels) ? (state.kernels as { name: string }[]) : [];
      return json({ success: true, data: list.find((k) => k.name === name) ?? {} });
    }
    if (path === '/api/kernels' && req.method() === 'GET') {
      counters.kernelsList++;
      return json({ success: true, data: state.kernels });
    }
    return json({ success: true, data: {} });
  });

  return counters;
}

test.describe('Services page — kernel status polling', () => {
  test('не зацикливает опрос для ядер в статусах done и failed', async ({ page }) => {
    const counters = await mockRoutes(page, {
      kernels: kernelsFixture('xray', {
        mihomo: { status: 'done', message: 'Updated to 1.18.0' },
        xray: { status: 'failed', message: 'download failed' }
      })
    });

    await page.clock.install();
    await page.goto('/#/services');
    await expect(page.locator('.updates-card')).toBeVisible();
    await expect(
      page.locator('.update-item', { hasText: 'Xray' }).locator('.status-badge')
    ).toBeVisible();

    await page.clock.runFor(30_000);
    // Короткая реальная пауза: ответы на запросы последнего тика доходят в реальном времени
    await page.waitForTimeout(300);

    expect(counters.kernelsList).toBeLessThanOrEqual(10);
    const statusCalls = Object.values(counters.kernelStatus).reduce((a, b) => a + b, 0);
    expect(statusCalls).toBeLessThanOrEqual(2);
  });

  for (const [label, data] of [
    ['пустой объект', {}],
    ['null', null]
  ] as const) {
    test(`пустой ответ ядер (${label}) не ломает страницу`, async ({ page }) => {
      const errors: string[] = [];
      page.on('pageerror', (e) => errors.push(e.message));
      page.on('console', (m) => {
        if (m.type() === 'error') errors.push(m.text());
      });
      await mockRoutes(page, { kernels: data });

      await page.goto('/#/services');
      await expect(page.locator('.updates-card')).toBeVisible();
      await expect(page.locator('.update-item')).toHaveCount(2);
      await expect.poll(() => errors.filter((e) => e.includes('forEach'))).toEqual([]);
    });
  }

  test('статус checking запускает опрос и останавливает его после idle', async ({ page }) => {
    const state: MockState = {
      kernels: kernelsFixture('xray', { xray: { status: 'checking' } })
    };
    const counters = await mockRoutes(page, state);
    // Первый ответ статуса сообщает, что проверка завершилась
    await page.route('**/api/kernels/xray/status', async (route) => {
      counters.kernelStatus['xray'] = (counters.kernelStatus['xray'] ?? 0) + 1;
      state.kernels = kernelsFixture('xray');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: (state.kernels as { name: string }[])[0] })
      });
    });

    await page.clock.install();
    await page.goto('/#/services');
    await expect(page.locator('.updates-card')).toBeVisible();
    await expect.poll(() => counters.kernelStatus['xray'] ?? 0).toBeGreaterThanOrEqual(1);

    await page.clock.runFor(10_000);
    await page.waitForTimeout(300);
    expect(counters.kernelStatus['xray']).toBeLessThanOrEqual(2);
  });
});
