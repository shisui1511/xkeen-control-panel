import { test, expect } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// ============================================================
// Editor Design System — разделитель редактора и индикаторы
// (статус сохранения, LED ядра) соответствуют дизайн-системе
// Keenetic/Netcraze: без inline-цветов и без дефолтной рамки
// браузерной кнопки.
// ============================================================

test.describe('Редактор — разделитель и индикаторы дизайн-системы', () => {
  test('статус сохранения — badge с иконкой, без круглого глифа', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    await visitPage(page, '/#/editor');

    const badge = page.locator('.eph-right .badge');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText(/Сохранён|Изменён|Saved|Modified/);

    const dotCount = await page.locator('.eph-right .status-dot').count();
    expect(dotCount).toBe(0);
  });

  test('badge статуса — border 1px, цвет из палитры темы, без inline style с цветом', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');
    await visitPage(page, '/#/editor');

    const badge = page.locator('.eph-right .badge');
    await expect(badge).toBeVisible();

    const styleAttr = await badge.getAttribute('style');
    expect(styleAttr).toBeNull();

    const { borderTopWidth, color } = await badge.evaluate((el) => {
      const cs = getComputedStyle(el);
      return { borderTopWidth: cs.borderTopWidth, color: cs.color };
    });
    expect(borderTopWidth).toBe('1px');
    expect(color).not.toBe('');
    expect(color).not.toBe('rgb(0, 0, 0)');
  });

  test('.editor-splitter не показывает дефолтную рамку/фон кнопки', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    await visitPage(page, '/#/editor');

    const splitter = page.locator('.editor-splitter');
    await expect(splitter).toBeVisible();

    const styles = await splitter.evaluate((el) => {
      const cs = getComputedStyle(el);
      return {
        borderTopWidth: cs.borderTopWidth,
        borderLeftWidth: cs.borderLeftWidth,
        paddingLeft: cs.paddingLeft,
        backgroundColor: cs.backgroundColor
      };
    });

    expect(styles.borderTopWidth).toBe('0px');
    expect(styles.borderLeftWidth).toBe('0px');
    expect(styles.paddingLeft).toBe('0px');
    expect(styles.backgroundColor).toBe('rgba(0, 0, 0, 0)');
  });

  test('.file-tree-pane не шире 42% ширины .editor-workspace на 1024x800', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    await page.setViewportSize({ width: 1024, height: 800 });
    await visitPage(page, '/#/editor');

    const workspace = page.locator('.editor-workspace');
    await expect(workspace).toBeVisible();
    const pane = page.locator('.file-tree-pane');

    if (await pane.count()) {
      const [workspaceWidth, paneWidth] = await Promise.all([
        workspace.evaluate((el) => el.getBoundingClientRect().width),
        pane.evaluate((el) => el.getBoundingClientRect().width)
      ]);
      expect(paneWidth).toBeLessThanOrEqual(workspaceWidth * 0.42 + 1);
    }
  });

  test('LED ядра окрашен токеном темы --success', async ({ page }) => {
    await setupMocks(page, 'mihomo');
    // Виджет ядра рендерится только когда открыта вкладка файла —
    // подкладываем список файлов и содержимое поверх базовых моков.
    await page.route('**/api/config/list**', async (route) => {
      const url = route.request().url();
      const isMihomo = url.includes('mihomo');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          isMihomo ? [{ name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 100 }] : []
        )
      });
    });
    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'port: 7890\nmode: Rule\n'
      });
    });

    await visitPage(page, '/#/editor');

    const fileRow = page.locator('.file-row', { hasText: 'config.yaml' }).first();
    await fileRow.click();

    const led = page.locator('.editor-kernel-widget .led-dot').first();
    await expect(led).toBeVisible();

    const { ledColor, tokenColor } = await page.evaluate(() => {
      const el = document.querySelector('.editor-kernel-widget .led-dot') as HTMLElement;
      const ledColor = getComputedStyle(el).backgroundColor;

      const tokenValue = getComputedStyle(document.documentElement)
        .getPropertyValue('--success')
        .trim();
      const probe = document.createElement('div');
      probe.style.color = tokenValue;
      document.body.appendChild(probe);
      const tokenColor = getComputedStyle(probe).color;
      probe.remove();

      return { ledColor, tokenColor };
    });

    expect(ledColor).toBe(tokenColor);
  });
});
