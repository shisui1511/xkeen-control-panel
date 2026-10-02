import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// e2e-pages: #/dashboard

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
