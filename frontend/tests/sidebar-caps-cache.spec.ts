import { test, expect, type Page } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

// Боковое меню рисуется по сохранённому срезу capabilities (localStorage
// `xcp_nav_caps`): первый кадр уже итоговый, группы не мигают после ответа
// сервера; на холодном входе вместо переменной части меню — скелетон.

const NAV_KEY = 'xcp_nav_caps';
const SECRET = 'mock-secret-value-9f3a';
const HWID = 'mock-hwid-77c1';

interface CapsState {
  activeKernel: string;
  /** Пока промис не разрешён, ответ /api/capabilities удерживается. */
  gate?: Promise<void>;
  /** Если задан, /api/capabilities отвечает этим кодом ошибки вместо среза. */
  failStatus?: number;
  calls: number;
}

/** Ответ /api/capabilities из изменяемого состояния; читается при каждом запросе. */
async function mockCapabilities(page: Page, state: CapsState) {
  await setupMocks(page, 'xray');
  // Маршрут, добавленный позже, срабатывает раньше маршрута из setupMocks
  await page.route('**/api/capabilities', async (route) => {
    state.calls++;
    if (state.gate) await state.gate;
    if (state.failStatus) {
      await route.fulfill({
        status: state.failStatus,
        contentType: 'application/json',
        body: JSON.stringify({ success: false, error: 'mock failure' })
      });
      return;
    }
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
          active_kernel: state.activeKernel,
          xkeen_installed: true,
          global_hwid: HWID,
          mihomo: {
            reachable: true,
            process_running: state.activeKernel === 'mihomo',
            api_reachable: state.activeKernel === 'mihomo',
            api_authenticated: state.activeKernel === 'mihomo',
            discovered_secret: SECRET
          }
        }
      })
    });
  });
}

/** Кладёт значение в localStorage до загрузки приложения (один раз на вкладку). */
async function seedNavCaps(page: Page, value: string) {
  await page.addInitScript(
    ([key, val]) => {
      try {
        if (!sessionStorage.getItem('__seeded')) {
          sessionStorage.setItem('__seeded', '1');
          localStorage.setItem(key, val);
        }
      } catch {
        // хранилище недоступно
      }
    },
    [NAV_KEY, value]
  );
}

/** Записывает, какие сочетания «число групп : есть скелетон» видел документ. */
async function recordNavFrames(page: Page) {
  await page.addInitScript(() => {
    const seen: string[] = [];
    (window as unknown as { __navSeen: string[] }).__navSeen = seen;
    const rec = () => {
      if (!document.querySelector('.sidebar-nav')) return;
      const n = document.querySelectorAll('.sidebar-nav .nav-group').length;
      const sk = !!document.querySelector('[data-testid="nav-skeleton"]');
      const key = `${n}:${sk}`;
      if (!seen.includes(key)) seen.push(key);
    };
    new MutationObserver(rec).observe(document, { childList: true, subtree: true });
  });
}

async function navFrames(page: Page): Promise<string[]> {
  return page.evaluate(() => (window as unknown as { __navSeen: string[] }).__navSeen);
}

function makeGate() {
  let open!: () => void;
  const gate = new Promise<void>((resolve) => {
    open = resolve;
  });
  return { gate, open };
}

// Число групп при Xray: обзор, наблюдение, инструменты, система
const XRAY_GROUPS = 4;

test.describe('Sidebar: кэш capabilities и скелетон', () => {
  test('с кэшем первый кадр итоговый и число групп не меняется после ответа', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(e.message));

    const { gate, open } = makeGate();
    const state: CapsState = { activeKernel: 'xray', gate, calls: 0 };
    await mockCapabilities(page, state);
    await seedNavCaps(
      page,
      JSON.stringify({ v: 1, active_kernel: 'xray', kernels: { xray: true, mihomo: true } })
    );
    await recordNavFrames(page);

    await page.goto('/#/dashboard');
    await expect(page.locator('.sidebar-nav')).toBeVisible();
    // Ответ capabilities ещё удержан: меню уже итоговое, скелетона нет
    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);
    await expect(page.getByTestId('nav-skeleton')).toHaveCount(0);

    const responded = page.waitForResponse('**/api/capabilities');
    open();
    await responded;
    await page.evaluate(
      () => new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r())))
    );

    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);
    await expect(page.getByTestId('nav-skeleton')).toHaveCount(0);
    // За всё время меню не показывало ни скелетон, ни другое число групп
    expect(await navFrames(page)).toEqual([`${XRAY_GROUPS}:false`]);
    expect(errors).toEqual([]);
  });

  test('без кэша до ответа виден скелетон, потом итоговые группы без Mihomo', async ({ page }) => {
    const { gate, open } = makeGate();
    const state: CapsState = { activeKernel: 'xray', gate, calls: 0 };
    await mockCapabilities(page, state);
    await recordNavFrames(page);

    await page.goto('/#/dashboard');
    const skeleton = page.getByTestId('nav-skeleton');
    await expect(skeleton).toBeVisible();
    await expect(skeleton).toHaveAttribute('role', 'status');
    await expect(skeleton).toHaveAttribute('aria-label', /.+/);
    // Статические группы видны сразу, группы Mihomo не рисуются
    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);

    open();
    await expect(skeleton).toHaveCount(0);
    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);
    // Группы Mihomo не вспыхивали ни на один кадр
    const frames = await navFrames(page);
    expect(frames.every((f) => f.startsWith(`${XRAY_GROUPS}:`))).toBe(true);
  });

  test('без кэша и при 500 от capabilities меню не висит скелетоном', async ({ page }) => {
    // Два опроса capabilities с интервалом 10 с: дефолт ставится на втором сбое
    test.setTimeout(60_000);
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(e.message));

    const state: CapsState = { activeKernel: 'xray', failStatus: 500, calls: 0 };
    await mockCapabilities(page, state);

    await page.goto('/#/dashboard');
    await expect(page.getByTestId('nav-skeleton')).toBeVisible();

    await expect(page.getByTestId('nav-skeleton')).toHaveCount(0, { timeout: 25_000 });
    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);
    expect(state.calls).toBeGreaterThanOrEqual(2);
    // Дефолт меню не записывается в кэш: он не подтверждён сервером
    expect(await page.evaluate((k) => localStorage.getItem(k), NAV_KEY)).toBeNull();
    expect(errors).toEqual([]);
  });

  for (const [title, raw] of [
    ['битый JSON', '{bad'],
    ['чужая версия формата', JSON.stringify({ v: 2, active_kernel: 'xray', kernels: {} })],
    ['неверная форма', JSON.stringify({ v: 1, active_kernel: 'weird', kernels: 5 })]
  ] as const) {
    test(`мусор в хранилище (${title}) — как без кэша, без ошибок`, async ({ page }) => {
      const errors: string[] = [];
      page.on('pageerror', (e) => errors.push(e.message));

      const { gate, open } = makeGate();
      await mockCapabilities(page, { activeKernel: 'xray', gate, calls: 0 });
      await seedNavCaps(page, raw);

      await page.goto('/#/dashboard');
      await expect(page.getByTestId('nav-skeleton')).toBeVisible();
      await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);

      open();
      await expect(page.getByTestId('nav-skeleton')).toHaveCount(0);
      await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);
      // Мусор заменён валидным срезом
      await expect
        .poll(() => page.evaluate((k) => localStorage.getItem(k), NAV_KEY))
        .toContain('"v":1');
      expect(errors).toEqual([]);
    });
  }

  test('в хранилище попадает только срез без секретов', async ({ page }) => {
    await mockCapabilities(page, { activeKernel: 'xray', calls: 0 });

    await page.goto('/#/dashboard');
    await expect(page.getByTestId('nav-skeleton')).toHaveCount(0);
    await expect.poll(() => page.evaluate((k) => localStorage.getItem(k), NAV_KEY)).not.toBeNull();

    const raw = (await page.evaluate((k) => localStorage.getItem(k), NAV_KEY)) as string;
    expect(raw).not.toContain('discovered_secret');
    expect(raw).not.toContain('global_hwid');
    expect(raw).not.toContain(SECRET);
    expect(raw).not.toContain(HWID);
    const parsed = JSON.parse(raw);
    expect(Object.keys(parsed).sort()).toEqual([
      'active_kernel',
      'kernels',
      'v',
      'xkeen_installed'
    ]);
    expect(parsed).toEqual({
      v: 1,
      active_kernel: 'xray',
      kernels: { xray: true, mihomo: true },
      xkeen_installed: true
    });
  });

  test('none при известном кэше не меняет меню', async ({ page }) => {
    const { gate, open } = makeGate();
    await mockCapabilities(page, { activeKernel: 'none', gate, calls: 0 });
    await seedNavCaps(
      page,
      JSON.stringify({ v: 1, active_kernel: 'xray', kernels: { xray: true, mihomo: true } })
    );
    await recordNavFrames(page);

    await page.goto('/#/dashboard');
    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);

    const responded = page.waitForResponse('**/api/capabilities');
    open();
    await responded;
    await page.evaluate(
      () => new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r())))
    );

    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);
    expect(await navFrames(page)).toEqual([`${XRAY_GROUPS}:false`]);
  });

  test('none без кэша показывает группы Mihomo, как на чистой системе', async ({ page }) => {
    const { gate, open } = makeGate();
    await mockCapabilities(page, { activeKernel: 'none', gate, calls: 0 });

    await page.goto('/#/dashboard');
    await expect(page.getByTestId('nav-skeleton')).toBeVisible();

    open();
    await expect(page.getByTestId('nav-skeleton')).toHaveCount(0);
    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS + 2);
    await expect(page.locator('a[href="#/proxies"]')).toBeVisible();
  });

  // Базовая линия для замка меню: без замка настоящая смена ядра доходит до меню
  // со следующим опросом (Dashboard опрашивает capabilities каждые 10 с).
  test('без замка смена ядра xray → mihomo меняет меню после опроса', async ({ page }) => {
    const state: CapsState = { activeKernel: 'xray', calls: 0 };
    await mockCapabilities(page, state);
    await page.clock.install();

    await page.goto('/#/dashboard');
    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS);
    await expect(page.locator('a[href="#/proxies"]')).toHaveCount(0);

    state.activeKernel = 'mihomo';
    await page.clock.runFor(10_500);

    await expect(page.locator('.sidebar-nav .nav-group')).toHaveCount(XRAY_GROUPS + 2);
    await expect(page.locator('a[href="#/proxies"]')).toBeVisible();
  });
});
