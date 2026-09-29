import { test, expect, type Page } from '@playwright/test';
import { kernelsFixture, systemStatsFixture } from './helpers/api-mocks';

// Дашборд: понятное состояние XKeen и одна подсказка следующего шага
// («лестница») в панели проблем. Состояние мока меняется между кейсами.

interface MockState {
  /** capabilities.xkeen_installed */
  xkeenInstalled: boolean;
  /** kernels.xray.installed / kernels.mihomo.installed */
  xrayInstalled: boolean;
  mihomoInstalled: boolean;
  activeKernel: 'xray' | 'mihomo' | 'none';
  /** Поля data ответа /api/service/status */
  service: Record<string, unknown>;
  /** Ответ /api/config/preflight (без конверта) */
  preflight: Record<string, unknown>;
  /** Ответ /api/system/stats целиком */
  stats: unknown;
  /** Ответ /api/version (поле data) */
  version: unknown;
}

interface Counters {
  preflight: string[];
}

function baseState(overrides: Partial<MockState> = {}): MockState {
  return {
    xkeenInstalled: true,
    xrayInstalled: true,
    mihomoInstalled: false,
    activeKernel: 'xray',
    service: {
      is_running: false,
      xkeen_installed: true,
      xkeen_setup_incomplete: false
    },
    preflight: { valid: true, errors: [], warnings: [] },
    stats: systemStatsFixture(),
    version: '2.0 Beta',
    ...overrides
  };
}

async function mockRoutes(page: Page, state: MockState): Promise<Counters> {
  const counters: Counters = { preflight: [] };

  await page.addInitScript(() => {
    window.localStorage.setItem('lang', 'ru');
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
  });

  await page.route('**/api/**', async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname;
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
            xray: { installed: state.xrayInstalled, version: '1.8.4', channel: 'stable' },
            mihomo: { installed: state.mihomoInstalled, version: '1.18.0', channel: 'stable' }
          },
          active_kernel: state.activeKernel,
          xkeen_installed: state.xkeenInstalled,
          mihomo: { reachable: false, process_running: false, api_reachable: false }
        }
      });
    }
    if (path === '/api/settings') return json({ success: true, data: { dev_mode: false } });
    if (path === '/api/version') return json({ success: true, data: state.version });
    if (path === '/api/service/restart-log') return json([]);
    if (path === '/api/service/status') return json({ success: true, data: state.service });
    if (path === '/api/config/preflight') {
      counters.preflight.push(url.searchParams.get('kernel') ?? '');
      return json(state.preflight);
    }
    if (path === '/api/kernels' && req.method() === 'GET') {
      return json({
        success: true,
        data: kernelsFixture('xray', {
          xray: { process_status: 'stopped' },
          mihomo: { process_status: 'stopped' }
        })
      });
    }
    if (path === '/api/system/stats') return json(state.stats);
    if (path === '/api/subscriptions') return json([]);
    return json({ success: true, data: {} });
  });

  return counters;
}

const step = (page: Page) => page.locator('[data-step]');
const xkeenCard = (page: Page) =>
  page
    .locator('.service-card')
    .filter({ has: page.locator('.service-name', { hasText: /^XKeen$/ }) });

test.describe('Dashboard — лестница следующего шага', () => {
  test('нет XKeen: шаг «XKeen не установлен», карточка «Не установлено»', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({
        xkeenInstalled: false,
        xrayInstalled: false,
        service: { is_running: false, xkeen_installed: false }
      })
    );
    await page.goto('/#/dashboard');

    const missing = page.getByTestId('problem-xkeen-missing');
    await expect(missing).toBeVisible();
    await expect(missing).toHaveAttribute('data-step', 'install_xkeen');
    await expect(missing).toContainText('XKeen не установлен');
    await expect(step(page)).toHaveCount(1);
    await expect(xkeenCard(page)).toContainText('Не установлено');
    await expect(xkeenCard(page)).not.toContainText('Неизвестно');
  });

  test('прерванная настройка: шаг «Установка XKeen не завершена»', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({
        service: { is_running: false, xkeen_installed: true, xkeen_setup_incomplete: true }
      })
    );
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toBeVisible();
    await expect(item).toHaveAttribute('data-step', 'finish_xkeen_setup');
    await expect(item).toContainText('Установка XKeen не завершена');
    await expect(step(page)).toHaveCount(1);
  });

  test('нет ядер: шаг «Прокси ядра не установлены»', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({ xrayInstalled: false, mihomoInstalled: false, activeKernel: 'none' })
    );
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toBeVisible();
    await expect(item).toHaveAttribute('data-step', 'install_kernel');
    await expect(item).toContainText('Прокси ядра не установлены');
    await expect(step(page)).toHaveCount(1);
  });

  test('нет подключений в конфиге xray: шаг «Настройте конфигурацию ядра»', async ({ page }) => {
    const counters = await mockRoutes(
      page,
      baseState({
        preflight: {
          valid: true,
          errors: [],
          warnings: [{ code: 'no_real_outbounds', message: 'no outbounds' }]
        }
      })
    );
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toBeVisible();
    await expect(item).toHaveAttribute('data-step', 'configure');
    await expect(item).toContainText('Настройте конфигурацию ядра');
    await expect(item).toContainText('Xray');
    await expect(step(page)).toHaveCount(1);
    expect(counters.preflight).toEqual(['xray']);

    await item.getByRole('button').click();
    await expect(page).toHaveURL(/#\/constructor/);
  });

  test('всё готово, остановлено: шаг «Запустите XKeen», карточка «Остановлено»', async ({
    page
  }) => {
    await mockRoutes(page, baseState());
    await page.goto('/#/dashboard');

    const item = page.getByTestId('problem-next-step');
    await expect(item).toBeVisible();
    await expect(item).toHaveAttribute('data-step', 'start');
    await expect(item).toContainText('Запустите XKeen');
    await expect(step(page)).toHaveCount(1);
    await expect(xkeenCard(page)).toContainText('Остановлено');
    await expect(xkeenCard(page)).not.toContainText('Неизвестно');

    await item.getByRole('button').click();
    await expect(page).toHaveURL(/#\/services/);
  });

  test('запущено: шага нет', async ({ page }) => {
    await mockRoutes(
      page,
      baseState({
        service: { is_running: true, xkeen_installed: true, xkeen_setup_incomplete: false }
      })
    );
    await page.goto('/#/dashboard');

    await expect(xkeenCard(page)).toContainText('Работает');
    await expect(step(page)).toHaveCount(0);
    await expect(page.getByTestId('problem-next-step')).toHaveCount(0);
  });

  test('холодный кэш статуса: «Неизвестно», шагов «Настройте»/«Запустите» нет', async ({
    page
  }) => {
    const counters = await mockRoutes(
      page,
      baseState({
        service: { is_running: false, stale: true, xkeen_installed: true }
      })
    );
    await page.goto('/#/dashboard');

    await expect(xkeenCard(page)).toContainText('Неизвестно');
    await expect(step(page)).toHaveCount(0);
    expect(counters.preflight).toEqual([]);
  });
});
