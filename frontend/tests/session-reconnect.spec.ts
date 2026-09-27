import { test, expect, type Page } from '@playwright/test';
import { setupMocks, visitPage } from './helpers/api-mocks';

// e2e-pages: settings, dashboard

test.use({ locale: 'ru-RU' });

// ============================================================
// D-19: 401 с причиной → тост → вход на месте → тот же маршрут.
// ============================================================

async function triggerReasonedLogout(page: Page, reason?: string): Promise<void> {
  await setupMocks(page, 'mihomo');

  let trigger401 = false;
  await page.route('**/api/system/stats', async (route) => {
    if (!trigger401) {
      await route.fallback();
      return;
    }
    const body: Record<string, unknown> = { success: false, error: 'Unauthorized' };
    if (reason) body.reason = reason;
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify(body)
    });
  });

  await page.route('**/api/auth/login', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ csrf_token: 'mock-csrf-token-2' })
    });
  });

  await visitPage(page, '/#/settings?tab=security');
  await expect(page.getByRole('tab', { name: /Безопасность|Security/ }).first()).toBeVisible();

  // Помечает страницу, чтобы проверить позже, что реального reload не было —
  // 401 должен переключить экран «на месте» (D-19), не через navigation.
  await page.evaluate(() => {
    (window as unknown as { __noReload?: boolean }).__noReload = true;
  });

  trigger401 = true;
  await page.waitForResponse(
    (response) => response.url().includes('/api/system/stats') && response.status() === 401
  );
}

test('401 password_changed: тост с причиной и вход возвращает на тот же маршрут без reload', async ({
  page
}) => {
  await triggerReasonedLogout(page, 'password_changed');

  await expect(page.locator('.toast__message')).toContainText('Пароль изменён');
  await expect(page.locator('#password')).toBeVisible();
  await expect(page).toHaveURL(/#\/settings/);

  await page.locator('#password').fill('new-password-123');
  await page.getByRole('button', { name: /Войти|Login/ }).click();

  await expect(page.getByRole('tab', { name: /Безопасность|Security/ }).first()).toBeVisible();
  await expect(page).toHaveURL(/#\/settings/);
  expect(
    await page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload)
  ).toBe(true);
});

test('401 terminated_elsewhere: тост «Сессия завершена с другого устройства»', async ({ page }) => {
  await triggerReasonedLogout(page, 'terminated_elsewhere');

  await expect(page.locator('.toast__message')).toContainText(
    'Сессия завершена с другого устройства'
  );
  await expect(page.locator('#password')).toBeVisible();
});

test('401 без reason: запасной тост «Сессия истекла, выполните повторный вход»', async ({
  page
}) => {
  await triggerReasonedLogout(page, undefined);

  await expect(page.locator('.toast__message')).toContainText(
    'Сессия истекла, выполните повторный вход'
  );
  await expect(page.locator('#password')).toBeVisible();
});

// ============================================================
// D-20: плашка «Панель недоступна, переподключение…» вместо лавины
// тостов при обрыве сети, тихое исчезновение и предложение перезагрузки
// при смене версии панели.
//
// Реализовано через один page.route('**/api/**', ...) с флагами в замыкании
// (а не через page.route/page.unroute на том же паттерне) — unroute() на
// '**/api/**' снял бы заодно и обработчик setupMocks, зарегистрированный
// на тот же паттерн, оставив все запросы без мока после «восстановления».
// ============================================================

async function setupPanelHealthMocks(
  page: Page
): Promise<{ setOutage: (v: boolean) => void; setPanelVersion: (v: string) => void }> {
  await setupMocks(page, 'mihomo');

  let outageActive = false;
  let panelVersion = 'v1.0.0';

  await page.route('**/api/**', async (route) => {
    if (outageActive) {
      await route.abort('connectionrefused');
      return;
    }
    const url = route.request().url();
    if (url.includes('/api/version')) {
      // Реальный контракт бэкенда — плоский объект, без {success,data}
      // (в отличие от устаревшего мока setupMocks для этого эндпоинта).
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ version: 'test-build', panel_version: panelVersion })
      });
      return;
    }
    await route.fallback();
  });

  return {
    setOutage: (v: boolean) => {
      outageActive = v;
    },
    setPanelVersion: (v: string) => {
      panelVersion = v;
    }
  };
}

test('плашка недоступности при обрыве сети и тихое исчезновение после восстановления', async ({
  page
}) => {
  test.setTimeout(90000);
  const { setOutage } = await setupPanelHealthMocks(page);

  await visitPage(page, '/#/dashboard');
  await expect(page.locator('.panel-reconnect-banner')).toHaveCount(0);

  setOutage(true);
  await expect(page.locator('.panel-reconnect-banner')).toBeVisible({ timeout: 10000 });
  await expect(page.locator('.panel-reconnect-banner')).toContainText(
    'Панель недоступна, переподключение'
  );
  await expect(page.locator('.toast--error')).toHaveCount(0);

  setOutage(false);
  await expect(page.locator('.panel-reconnect-banner')).toBeHidden({ timeout: 40000 });
  // D-20: возврат панели проходит тихо — без тоста «соединение восстановлено».
  await expect(page.locator('.toast--success')).toHaveCount(0);
});

test('плашка предлагает обновить страницу, когда версия панели изменилась за время недоступности', async ({
  page
}) => {
  test.setTimeout(90000);
  const { setOutage, setPanelVersion } = await setupPanelHealthMocks(page);

  await visitPage(page, '/#/dashboard');

  await page.evaluate(() => {
    (window as unknown as { __noReload?: boolean }).__noReload = true;
  });

  setOutage(true);
  await expect(page.locator('.panel-reconnect-banner')).toBeVisible({ timeout: 10000 });

  setPanelVersion('v1.0.1');
  setOutage(false);

  const banner = page.locator('.panel-reconnect-banner');
  await expect(banner).toContainText('Доступна новая версия панели', { timeout: 40000 });
  const reloadButton = page.getByRole('button', { name: /Обновить страницу|Reload page/ });
  await expect(reloadButton).toBeVisible();

  // Перезагрузки не было, пока не нажали кнопку.
  expect(
    await page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload)
  ).toBe(true);

  await reloadButton.click();
  await page.waitForLoadState('domcontentloaded');
  expect(
    await page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload)
  ).toBeUndefined();
});

test('overflow: плашка новой версии не создаёт горизонтальный скролл на 360px', async ({
  page
}) => {
  test.setTimeout(90000);
  await page.setViewportSize({ width: 360, height: 740 });
  const { setOutage, setPanelVersion } = await setupPanelHealthMocks(page);

  await visitPage(page, '/#/dashboard');

  setOutage(true);
  await expect(page.locator('.panel-reconnect-banner')).toBeVisible({ timeout: 10000 });

  setPanelVersion('v1.0.1');
  setOutage(false);

  const banner = page.locator('.panel-reconnect-banner');
  await expect(banner).toContainText('Доступна новая версия панели', { timeout: 40000 });

  const noOverflow = await banner.evaluate((el) => el.scrollWidth <= el.clientWidth + 1);
  expect(noOverflow).toBe(true);
});

test('401 во время недоступности панели: плашка исчезает, показывается тост причины', async ({
  page
}) => {
  test.setTimeout(60000);
  await setupMocks(page, 'mihomo');

  let mode: 'ok' | 'outage' | 'unauthorized' = 'ok';
  await page.route('**/api/**', async (route) => {
    if (mode === 'outage') {
      await route.abort('connectionrefused');
      return;
    }
    const url = route.request().url();
    if (mode === 'unauthorized' && url.includes('/api/system/stats')) {
      await route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ success: false, error: 'Unauthorized', reason: 'session_expired' })
      });
      return;
    }
    await route.fallback();
  });

  await visitPage(page, '/#/dashboard');

  mode = 'outage';
  await expect(page.locator('.panel-reconnect-banner')).toBeVisible({ timeout: 10000 });

  mode = 'unauthorized';
  await page.waitForResponse(
    (response) => response.url().includes('/api/system/stats') && response.status() === 401
  );

  await expect(page.locator('.panel-reconnect-banner')).toHaveCount(0);
  await expect(page.locator('.toast__message')).toContainText(
    'Сессия истекла, выполните повторный вход'
  );
});
