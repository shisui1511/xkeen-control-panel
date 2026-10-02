// e2e-pages: editor
import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// Изоляция ядер (140.1-07): страницы без жизненного цикла читают состояние ядра из стора.
// Редактор в конфликте: «Сохранить и применить» неактивна, «Сохранить» доступна.

test.use({ locale: 'ru-RU' });

const CONFIG_PATH = '/opt/etc/mihomo/config.yaml';
const CONFIG_BODY = 'port: 7890\nmode: Rule\n';

/** Мок списка и чтения одного конфига: перекрывает пустой список из setupMocks. */
async function mockEditorFiles(page: Page): Promise<void> {
  await page.route('**/api/config/list**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      // Редактор запрашивает список по каталогам обоих ядер — файл отдаём только для Mihomo
      body: JSON.stringify(
        route.request().url().includes('mihomo')
          ? [{ name: 'config.yaml', path: CONFIG_PATH, size: 24 }]
          : []
      )
    });
  });
  await page.route('**/api/config/read**', async (route) => {
    await route.fulfill({ status: 200, contentType: 'text/plain', body: CONFIG_BODY });
  });
  await page.route('**/api/config/backups**', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });
}

async function openConfig(page: Page): Promise<void> {
  await visitPage(page, '/#/editor');
  const row = page.locator('.file-row:has-text("config.yaml")');
  await expect(row).toBeVisible();
  await row.click();
  await expect(page.locator('.editor-tab:has-text("config.yaml")')).toBeVisible();
}

test.describe('Редактор при конфликте ядер', () => {
  test('«Сохранить и применить» неактивна с подсказкой, «Сохранить» доступна', async ({ page }) => {
    await setupMocks(page, 'conflict');
    await mockEditorFiles(page);
    await openConfig(page);

    const apply = page.getByRole('button', { name: 'Недоступно, пока запущены оба ядра' });
    await expect(apply).toBeDisabled();
    await expect(apply).toHaveAttribute('title', 'Недоступно, пока запущены оба ядра');
    await expect(apply).toHaveAttribute('aria-label', 'Недоступно, пока запущены оба ядра');

    await expect(page.getByRole('button', { name: 'Сохранить', exact: true })).toBeEnabled();
  });

  test('с одним активным ядром «Сохранить и применить» доступна', async ({ page }) => {
    await setupMocks(page, 'xray');
    await mockEditorFiles(page);
    await openConfig(page);

    const apply = page.getByRole('button', { name: 'Сохранить и применить' });
    await expect(apply).toBeEnabled();
    await expect(apply).toHaveAttribute('title', 'Сохранить и применить');
  });

  test('409 гейта при применении показывает понятный текст, а не «рестарт не удался»', async ({
    page
  }) => {
    await setupMocks(page, 'xray');
    await mockEditorFiles(page);
    await page.route('**/api/service/control?action=apply**', async (route) => {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({
          success: false,
          error: 'kernel is not active',
          code: 'kernel_inactive',
          required: 'mihomo',
          active: 'xray'
        })
      });
    });
    await openConfig(page);

    await page.getByRole('button', { name: 'Сохранить и применить' }).click();

    await expect(
      page.getByText('Действие недоступно: сейчас активно Xray, а нужно Mihomo.')
    ).toBeVisible();
    await expect(page.getByText('рестарт', { exact: false })).toHaveCount(0);
  });
});
