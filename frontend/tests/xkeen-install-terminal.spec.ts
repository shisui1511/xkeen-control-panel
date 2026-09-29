// e2e-pages: services, console
import { test, expect, type Page } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

// Окно установщика XKeen: терминал вмещает вывод (строк не меньше порога на
// трёх разрешениях, без горизонтальной прокрутки на узком экране), а обычная
// консоль остаётся прежней. Размер терминала задаёт клиент (fitAddon.fit()),
// число строк читается из data-rows корня .terminal-card.

/** Изменяемое состояние моков: читается при каждом запросе. */
interface MockState {
  /** active_kernel в ответах /api/capabilities */
  activeKernel: string;
}

async function mockInstallerPage(page: Page, state: MockState = { activeKernel: 'xray' }) {
  await setupMocks(page, 'xray');
  // Маршруты, добавленные позже, срабатывают раньше маршрута из setupMocks
  await page.route('**/api/capabilities', async (route) => {
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
          xkeen_installed: false,
          mihomo: {
            reachable: true,
            process_running: state.activeKernel === 'mihomo',
            api_reachable: state.activeKernel === 'mihomo',
            api_authenticated: state.activeKernel === 'mihomo'
          }
        }
      })
    });
  });
  await page.route('**/api/service/restart-log', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });
  await page.route('**/api/service/status', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        success: true,
        data: {
          is_running: false,
          active_kernel: state.activeKernel,
          pid: 0,
          uptime: '',
          binary_path: '',
          raw: '',
          xkeen_installed: false,
          xkeen_installer_available: true,
          xkeen_setup_incomplete: false
        }
      })
    });
  });
  // Сервер терминала не нужен: сокет принимается и молчит
  await page.routeWebSocket(/\/api\/terminal\/ws/, () => {});
}

async function readRows(page: Page): Promise<number> {
  const raw = await page.locator('.terminal-card[data-rows]').first().getAttribute('data-rows');
  return Number(raw);
}

/** Ждёт, пока число строк перестанет меняться (подгонка размера с задержкой). */
async function settledRows(page: Page): Promise<number> {
  await expect(page.locator('.terminal-card[data-rows] .xterm-screen')).toBeVisible();
  let last = -1;
  let stableSince = Date.now();
  await expect
    .poll(
      async () => {
        const rows = await readRows(page);
        if (rows !== last) {
          last = rows;
          stableSince = Date.now();
        }
        return Date.now() - stableSince >= 600;
      },
      { timeout: 10_000 }
    )
    .toBe(true);
  return last;
}

async function openInstaller(page: Page) {
  await page.goto('/#/services');
  await expect(page.locator('.hero-card')).toBeVisible();
  await page.getByTestId('xkeen-install-start').click();
  await expect(page.getByTestId('xkeen-install-modal')).toBeVisible();
}

test.describe('терминал установщика XKeen', () => {
  async function rowsAt(page: Page, width: number, height: number): Promise<number> {
    await page.setViewportSize({ width, height });
    await mockInstallerPage(page);
    await openInstaller(page);
    return settledRows(page);
  }

  test('вмещает не меньше 20 строк при 1366x768', async ({ page }) => {
    // До правки: 16 строк
    const rows = await rowsAt(page, 1366, 768);
    expect(rows).toBeGreaterThanOrEqual(20);
  });

  test('вмещает не меньше 30 строк при 1920x1080', async ({ page }) => {
    // До правки: 19 строк
    const rows = await rowsAt(page, 1920, 1080);
    expect(rows).toBeGreaterThanOrEqual(30);
  });

  test('вмещает не меньше 20 строк при 390x844', async ({ page }) => {
    // До правки: 14 строк
    const rows = await rowsAt(page, 390, 844);
    expect(rows).toBeGreaterThanOrEqual(20);
  });

  test('на 390 px нет горизонтальной прокрутки, «Во весь экран» доступна', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await mockInstallerPage(page);
    await openInstaller(page);
    await settledRows(page);

    const modal = page.getByTestId('xkeen-install-modal');
    const overflow = await modal.locator('.modal-content').evaluate((el) => ({
      scrollWidth: el.scrollWidth,
      clientWidth: el.clientWidth
    }));
    expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth);

    const fullscreen = modal.getByRole('button', { name: /Во весь экран|Fullscreen/ });
    await expect(fullscreen).toBeVisible();
    await expect(fullscreen).toBeInViewport();
  });

  test('в окне установщика нет подсказок горячих клавиш и бейджа размера', async ({ page }) => {
    await page.setViewportSize({ width: 1366, height: 768 });
    await mockInstallerPage(page);
    await openInstaller(page);
    await settledRows(page);

    const modal = page.getByTestId('xkeen-install-modal');
    await expect(modal.locator('.footer-hints')).toHaveCount(0);
    await expect(modal.locator('.geo-badge')).toHaveCount(0);
  });
});

test.describe('обычная консоль', () => {
  test('подсказки горячих клавиш и бейдж размера на месте', async ({ page }) => {
    await page.setViewportSize({ width: 1366, height: 768 });
    await mockInstallerPage(page);
    await page.goto('/#/console');

    const card = page.locator('.terminal-card[data-rows]');
    await expect(card).toBeVisible();
    await expect(card.locator('.footer-hints')).toBeVisible();
    await expect(card.locator('.footer-hints kbd')).toHaveCount(3);
    await expect(card.locator('.geo-badge')).toBeVisible();
    await expect(card.locator('.geo-badge')).toContainText('×');
  });
});
