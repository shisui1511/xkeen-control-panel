// e2e-pages: dashboard
import { test, expect } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';

// Трассер: одна страница настоящей панели цели. Обход всех страниц — отдельным планом.
test('page:dashboard theme:light vp:1440', async ({ page }) => {
  // сборщики консоли и сети подключаются до перехода, чтобы ранние ошибки не терялись
  const guard = attachConsoleGuard(page);

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => {
    localStorage.setItem('theme', 'light');
  });

  await page.goto('/#/dashboard');
  await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
    timeout: T.page
  });
  // Панель опрашивает API постоянно, полной тишины сети не будет: ждём затишья
  // ограниченное время и в любом случае проверяем консоль
  await page.waitForLoadState('networkidle', { timeout: T.network }).catch(() => {});

  await guard.assertClean();
});
