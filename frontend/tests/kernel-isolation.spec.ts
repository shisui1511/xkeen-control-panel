import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { fulfillServiceControl, setupMocks, visitPage } from './helpers/api-mocks';

// e2e-pages: #/dashboard #/proxies #/services #/connections #/rules #/traffic #/smartproxy #/trafficquotas #/dat #/constructor #/logs #/settings #/editor

// Изоляция ядер (140.1): при двух запущенных ядрах над содержимым любой вкладки
// виден баннер конфликта; без конфликта баннера нет.

test.use({ locale: 'ru-RU' });

test.describe('Конфликт ядер: баннер', () => {
  test('конфликт: баннер виден и объявлен как alert', async ({ page }) => {
    await setupMocks(page, 'conflict');
    await visitPage(page, '/#/dashboard');

    const banner = page.getByTestId('kernel-conflict-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toHaveAttribute('role', 'alert');
    await expect(banner).toContainText('Запущены оба ядра: Xray и Mihomo');
  });

  test('без конфликта баннера нет', async ({ page }) => {
    await setupMocks(page, 'xray');
    await visitPage(page, '/#/dashboard');

    await expect(page.getByTestId('dashboard-page')).toBeVisible();
    await expect(page.getByTestId('kernel-conflict-banner')).toHaveCount(0);
  });
});

// Вкладки ядра: в конфликте вместо страницы заглушка, сама страница не монтируется.
const KERNEL_TABS = [
  '#/proxies',
  '#/connections',
  '#/rules',
  '#/traffic',
  '#/smartproxy',
  '#/trafficquotas',
  '#/dat',
  '#/constructor'
];
// Нейтральные вкладки: открываются со своим содержимым, баннер виден над ними.
const NEUTRAL_TABS = ['#/dashboard', '#/services', '#/logs', '#/settings', '#/editor'];

test.describe('Конфликт ядер: все вкладки', () => {
  for (const tab of KERNEL_TABS) {
    test(`вкладка ядра ${tab}: баннер и заглушка «Раздел недоступен»`, async ({ page }) => {
      await setupMocks(page, 'conflict');
      await visitPage(page, `/${tab}`);

      await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();
      const gate = page.getByTestId('kernel-conflict-gate');
      await expect(gate).toBeVisible();
      await expect(gate).toContainText('Раздел недоступен: запущены оба ядра');
    });
  }

  for (const tab of NEUTRAL_TABS) {
    test(`нейтральная вкладка ${tab}: баннер есть, заглушки нет`, async ({ page }) => {
      await setupMocks(page, 'conflict');
      await visitPage(page, `/${tab}`);

      await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();
      await expect(page.getByTestId('kernel-conflict-gate')).toHaveCount(0);
      // страница действительно отрисована: в области содержимого есть не только баннер
      await expect(
        page.locator('.main-content .container, .main-content .page-header').first()
      ).toBeVisible();
    });
  }

  test('вкладка ядра в конфликте не опрашивает API Mihomo', async ({ page }) => {
    const proxyRequests: string[] = [];
    page.on('request', (req) => {
      if (req.url().includes('/api/mihomo/proxy/')) proxyRequests.push(req.url());
    });
    await setupMocks(page, 'conflict');
    await visitPage(page, '/#/proxies');
    await expect(page.getByTestId('kernel-conflict-gate')).toBeVisible();
    await page.waitForTimeout(5000);

    expect(proxyRequests).toEqual([]);
  });
});

test.describe('Конфликт ядер: гейты оболочки', () => {
  test('вкладка Прокси: заглушка под баннером, без ApiOffline', async ({ page }) => {
    await setupMocks(page, 'conflict');
    await visitPage(page, '/#/proxies');

    const gate = page.getByTestId('kernel-conflict-gate');
    await expect(gate).toBeVisible();
    await expect(gate).toContainText('Раздел недоступен: запущены оба ядра');
    await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();
    await expect(page.getByText('127.0.0.1:9090')).toHaveCount(0);
  });

  test('боковое меню: группы Mihomo скрыты, Сервисы доступны', async ({ page }) => {
    await setupMocks(page, 'conflict');
    await visitPage(page, '/#/dashboard');
    await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();

    await expect(page.locator('a[href="#/proxies"]')).toHaveCount(0);
    await expect(page.locator('a[href="#/connections"]')).toHaveCount(0);
    await expect(page.locator('a[href="#/services"]')).toBeVisible();
  });

  test('Сервисы в конфликте открывают страницу, а не заглушку', async ({ page }) => {
    await setupMocks(page, 'conflict');
    await visitPage(page, '/#/services');

    await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();
    await expect(page.getByTestId('kernel-conflict-gate')).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Сервисы и ядра' }).first()).toBeVisible();
  });

  test('конфликт не попадает в localStorage', async ({ page }) => {
    await setupMocks(page, 'conflict');
    await visitPage(page, '/#/dashboard');
    await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();

    const stored = await page.evaluate(() => window.localStorage.getItem('xcp_nav_caps'));
    expect(stored === null || !stored.includes('both')).toBe(true);
  });

  test('при активном Xray WebSocket трафика не открывается', async ({ page }) => {
    const wsUrls: string[] = [];
    page.on('websocket', (ws) => wsUrls.push(ws.url()));
    await setupMocks(page, 'xray');
    await visitPage(page, '/#/dashboard');
    await expect(page.getByTestId('dashboard-page')).toBeVisible();
    await page.waitForTimeout(1500);

    expect(wsUrls.filter((u) => u.includes('/api/traffic/ws'))).toHaveLength(0);
  });
});

// Остановка выбранного ядра из баннера (D-02): подтверждение, один запрос stop,
// затем capabilities без конфликта.
interface StopHarness {
  stopRequests: string[];
  allRequests: string[];
}

async function setupStopHarness(
  page: Page,
  opts: { stopOutcome: 'stopped' | 'still_running'; conflictCleared: boolean }
): Promise<StopHarness> {
  const harness: StopHarness = { stopRequests: [], allRequests: [] };
  let stopped = false;

  await setupMocks(page, 'conflict');
  await page.route('**/api/capabilities**', async (route) => {
    // до остановки (и когда она не помогает) отвечает мок режима conflict
    if (!stopped || !opts.conflictCleared) return route.fallback();
    await route.fulfill({
      json: {
        success: true,
        data: {
          kernels: {
            xray: { installed: true, version: '1.8.4', channel: 'stable' },
            mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
          },
          active_kernel: 'xray',
          kernel_conflict: false,
          running_kernels: ['xray'],
          mihomo: {
            reachable: true,
            process_running: false,
            api_reachable: false,
            api_authenticated: false
          }
        }
      }
    });
  });
  await page.route('**/api/service/control**', async (route) => {
    const url = route.request().url();
    harness.allRequests.push(url);
    const params = new URL(url).searchParams;
    if (params.get('action') === 'stop') {
      harness.stopRequests.push(`${params.get('action')}&kernel=${params.get('kernel')}`);
      stopped = true;
      if (opts.stopOutcome === 'still_running') {
        await route.fulfill({
          json: {
            success: true,
            data: { kernel: params.get('kernel'), outcome: 'still_running', method: 'signal' }
          }
        });
        return;
      }
    }
    await fulfillServiceControl(route);
  });
  return harness;
}

test.describe('Конфликт ядер: остановка из баннера', () => {
  test('остановка Mihomo из баннера снимает конфликт', async ({ page }) => {
    const harness = await setupStopHarness(page, { stopOutcome: 'stopped', conflictCleared: true });
    await visitPage(page, '/#/dashboard');

    const banner = page.getByTestId('kernel-conflict-banner');
    await expect(banner.getByRole('button', { name: /Остановить Xray/ })).toBeVisible();
    await expect(banner.getByRole('button', { name: /Остановить Mihomo/ })).toBeVisible();

    await page.getByTestId('kernel-conflict-stop-mihomo').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Остановить Mihomo?');
    await dialog.getByRole('button', { name: 'Остановить Mihomo' }).click();

    await expect.poll(() => harness.stopRequests).toEqual(['stop&kernel=mihomo']);
    await expect(
      page.locator('.toast', { hasText: 'Конфликт снят. Активное ядро: Xray' })
    ).toBeVisible();
    await expect(banner).toHaveCount(0);
  });

  test('отмена в подтверждении не отправляет остановку', async ({ page }) => {
    const harness = await setupStopHarness(page, { stopOutcome: 'stopped', conflictCleared: true });
    await visitPage(page, '/#/dashboard');

    await page.getByTestId('kernel-conflict-stop-xray').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Остановить Xray?');
    await dialog.getByRole('button', { name: 'Отмена' }).click();

    await expect(dialog).toHaveCount(0);
    expect(harness.stopRequests).toHaveLength(0);
    await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();
  });

  test('ядро не остановилось: error-тост, кнопки снова активны, баннер остаётся', async ({
    page
  }) => {
    const harness = await setupStopHarness(page, {
      stopOutcome: 'still_running',
      conflictCleared: false
    });
    await visitPage(page, '/#/dashboard');

    await page.getByTestId('kernel-conflict-stop-mihomo').click();
    await page.getByRole('dialog').getByRole('button', { name: 'Остановить Mihomo' }).click();

    await expect(
      page.locator('.toast--error', { hasText: 'Mihomo не остановился за 10 секунд' })
    ).toBeVisible();
    expect(harness.stopRequests).toEqual(['stop&kernel=mihomo']);
    await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();
    await expect(page.getByTestId('kernel-conflict-stop-mihomo')).toBeEnabled();
    await expect(page.getByTestId('kernel-conflict-stop-xray')).toBeEnabled();
  });
});

// Карточки служб на дашборде (D-02): в конфликте запуск и перезапуск неактивны,
// «Остановить» на карточке ядра гасит именно это ядро.
function serviceCard(page: Page, name: 'XKeen' | 'Mihomo' | 'Xray') {
  return page.locator('.service-card').filter({
    has: page.locator('.service-name', { hasText: new RegExp(`^${name}$`) })
  });
}

test.describe('Конфликт ядер: карточки служб на дашборде', () => {
  test('конфликт: «Перезапустить» неактивна, «Остановить Mihomo» шлёт stop&kernel=mihomo', async ({
    page
  }) => {
    const harness = await setupStopHarness(page, { stopOutcome: 'stopped', conflictCleared: true });
    await visitPage(page, '/#/dashboard');
    await expect(page.getByTestId('kernel-conflict-banner')).toBeVisible();

    for (const name of ['Xray', 'Mihomo'] as const) {
      const restart = serviceCard(page, name).getByRole('button', { name: /Перезапустить/ });
      await expect(restart).toBeDisabled();
      await expect(restart).toHaveAttribute('title', 'Недоступно, пока запущены оба ядра');
    }

    await serviceCard(page, 'Mihomo').getByRole('button', { name: 'Остановить' }).click();

    await expect.poll(() => harness.stopRequests).toEqual(['stop&kernel=mihomo']);
    expect(harness.allRequests.filter((u) => u.includes('action=restart'))).toHaveLength(0);
  });

  test('без конфликта «Перезапустить» на Xray активна и шлёт restart', async ({ page }) => {
    const requests: string[] = [];
    await setupMocks(page, 'xray');
    await page.route('**/api/service/control**', async (route) => {
      requests.push(new URL(route.request().url()).searchParams.get('action') ?? '');
      await fulfillServiceControl(route);
    });
    await visitPage(page, '/#/dashboard');

    const restart = serviceCard(page, 'Xray').getByRole('button', { name: /Перезапустить/ });
    await expect(restart).toBeEnabled();
    await restart.click();

    await expect.poll(() => requests).toEqual(['restart']);
  });
});
