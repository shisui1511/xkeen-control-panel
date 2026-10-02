import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// e2e-pages: #/dashboard #/proxies #/services

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
