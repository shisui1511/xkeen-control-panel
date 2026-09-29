import { test, expect, type Page } from '@playwright/test';
import { kernelsFixture, systemStatsFixture } from './helpers/api-mocks';

// «Запустить» у карточки XKeen на дашборде и в меню капсулы статуса неактивна,
// пока не установлено ни одно ядро (или нет самого XKeen). Пока capabilities
// не загружены, кнопки не блокируются: null не означает «нет ядер».

interface MockState {
  xkeenInstalled: boolean;
  xrayInstalled: boolean;
  mihomoInstalled: boolean;
  /** Пока промис не решён, ответ /api/capabilities удерживается. */
  capsGate?: Promise<void>;
}

interface Counters {
  control: string[];
}

async function mockRoutes(page: Page, state: MockState): Promise<Counters> {
  const counters: Counters = { control: [] };

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
      if (state.capsGate) await state.capsGate;
      return json({
        success: true,
        data: {
          kernels: {
            xray: { installed: state.xrayInstalled, version: '1.8.4', channel: 'stable' },
            mihomo: { installed: state.mihomoInstalled, version: '1.18.0', channel: 'stable' }
          },
          active_kernel: state.xrayInstalled ? 'xray' : 'none',
          xkeen_installed: state.xkeenInstalled,
          mihomo: { reachable: false, process_running: false, api_reachable: false }
        }
      });
    }
    if (path === '/api/settings') return json({ success: true, data: { dev_mode: false } });
    if (path === '/api/version') {
      return json({ success: true, data: { version: '2.0 Beta', panel_version: 'v0.25.0' } });
    }
    if (path === '/api/service/restart-log') return json([]);
    if (path === '/api/service/status') {
      return json({
        success: true,
        data: { is_running: false, xkeen_installed: state.xkeenInstalled }
      });
    }
    if (path === '/api/service/control') {
      counters.control.push(`${req.method()} ${url.search}`);
      return json({ success: true });
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
    if (path === '/api/system/stats') return json(systemStatsFixture());
    if (path === '/api/subscriptions') return json([]);
    return json({ success: true, data: {} });
  });

  return counters;
}

const NO_KERNEL = 'Сначала установите ядро Xray или Mihomo';
const NO_XKEEN = 'Сначала установите XKeen';

const xkeenStart = (page: Page) =>
  page
    .locator('.service-card')
    .filter({ has: page.locator('.service-name', { hasText: /^XKeen$/ }) })
    .getByRole('button', { name: /Запустить/ });

/** Кнопка запуска службы в меню капсулы — третья в блоке действий. */
async function openCapsuleStart(page: Page) {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.locator('.sidebar .sidebar-kernel-row').click();
  const menu = page.locator('.system-quick-menu');
  await expect(menu).toBeVisible();
  return menu.locator('.action-btn').nth(2);
}

test.describe('Кнопка «Запустить» без ядер', () => {
  test('нет ядер: карточка XKeen и капсула неактивны, POST не уходит', async ({ page }) => {
    const counters = await mockRoutes(page, {
      xkeenInstalled: true,
      xrayInstalled: false,
      mihomoInstalled: false
    });
    await page.goto('/#/dashboard');

    const cardBtn = xkeenStart(page);
    await expect(cardBtn).toBeDisabled();
    await expect(cardBtn).toHaveAttribute('title', NO_KERNEL);

    const capsuleBtn = await openCapsuleStart(page);
    await expect(capsuleBtn).toBeDisabled();
    await expect(capsuleBtn).toHaveAttribute('title', NO_KERNEL);

    await cardBtn.dispatchEvent('click');
    await capsuleBtn.dispatchEvent('click');
    await page.waitForTimeout(300);
    expect(counters.control).toEqual([]);
  });

  test('одно ядро установлено: обе кнопки активны и запускают службу', async ({ page }) => {
    const counters = await mockRoutes(page, {
      xkeenInstalled: true,
      xrayInstalled: true,
      mihomoInstalled: false
    });
    await page.goto('/#/dashboard');

    const cardBtn = xkeenStart(page);
    await expect(cardBtn).toBeEnabled();
    const capsuleBtn = await openCapsuleStart(page);
    await expect(capsuleBtn).toBeEnabled();

    await capsuleBtn.click();
    await expect.poll(() => counters.control.length).toBe(1);
    expect(counters.control[0]).toContain('action=start');
  });

  test('XKeen не установлен: капсула неактивна с подсказкой про XKeen', async ({ page }) => {
    const counters = await mockRoutes(page, {
      xkeenInstalled: false,
      xrayInstalled: false,
      mihomoInstalled: false
    });
    await page.goto('/#/dashboard');

    const capsuleBtn = await openCapsuleStart(page);
    await expect(capsuleBtn).toBeDisabled();
    await expect(capsuleBtn).toHaveAttribute('title', NO_XKEEN);
    await capsuleBtn.dispatchEvent('click');
    await page.waitForTimeout(300);
    expect(counters.control).toEqual([]);
  });

  test('capabilities ещё не загружены: кнопки не блокируются', async ({ page }) => {
    let release: () => void = () => {};
    const capsGate = new Promise<void>((resolve) => {
      release = resolve;
    });
    await mockRoutes(page, {
      xkeenInstalled: true,
      xrayInstalled: false,
      mihomoInstalled: false,
      capsGate
    });
    await page.goto('/#/dashboard');

    // Карточка XKeen отрисована по статусу, ответа capabilities ещё нет
    const cardBtn = xkeenStart(page);
    await expect(cardBtn).toBeVisible();
    await expect(cardBtn).toBeEnabled();
    await expect(cardBtn).not.toHaveAttribute('title', NO_KERNEL);

    release();
    // После загрузки capabilities без ядер кнопка блокируется
    await expect(cardBtn).toBeDisabled();
  });
});

test.describe('Сервисы: «Запустить» без ядер', () => {
  const heroStart = (page: Page) => page.getByTestId('hero-start');

  test('нет ядер: кнопка неактивна с подсказкой, POST не уходит', async ({ page }) => {
    const counters = await mockRoutes(page, {
      xkeenInstalled: true,
      xrayInstalled: false,
      mihomoInstalled: false
    });
    await page.goto('/#/services');

    await expect(heroStart(page)).toBeDisabled();
    await expect(heroStart(page)).toHaveAttribute('title', NO_KERNEL);
    await expect(heroStart(page)).toHaveAttribute('aria-label', new RegExp(NO_KERNEL));
    await heroStart(page).dispatchEvent('click');
    await page.waitForTimeout(300);
    expect(counters.control).toEqual([]);
  });

  test('одно ядро установлено: кнопка активна и запускает службу', async ({ page }) => {
    const counters = await mockRoutes(page, {
      xkeenInstalled: true,
      xrayInstalled: false,
      mihomoInstalled: true
    });
    await page.goto('/#/services');

    await expect(heroStart(page)).toBeEnabled();
    await heroStart(page).click();
    await expect.poll(() => counters.control.length).toBe(1);
    expect(counters.control[0]).toContain('action=start');
  });

  test('XKeen не установлен: подсказка про XKeen', async ({ page }) => {
    const counters = await mockRoutes(page, {
      xkeenInstalled: false,
      xrayInstalled: false,
      mihomoInstalled: false
    });
    await page.goto('/#/services');

    await expect(heroStart(page)).toBeDisabled();
    await expect(heroStart(page)).toHaveAttribute('title', NO_XKEEN);
    await heroStart(page).dispatchEvent('click');
    await page.waitForTimeout(300);
    expect(counters.control).toEqual([]);
  });

  test('capabilities ещё не загружены: кнопка не блокируется', async ({ page }) => {
    let release: () => void = () => {};
    const capsGate = new Promise<void>((resolve) => {
      release = resolve;
    });
    await mockRoutes(page, {
      xkeenInstalled: true,
      xrayInstalled: false,
      mihomoInstalled: false,
      capsGate
    });
    await page.goto('/#/services');

    await expect(heroStart(page)).toBeVisible();
    await expect(heroStart(page)).toBeEnabled();
    await expect(heroStart(page)).not.toHaveAttribute('title', NO_KERNEL);

    release();
    await expect(heroStart(page)).toBeDisabled();
  });
});
