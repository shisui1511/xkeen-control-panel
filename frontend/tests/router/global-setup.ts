import { chromium, type FullConfig } from '@playwright/test';
import { RT, T } from './lib/env';

// Однократный вход в панель цели: форма входа, затем storageState (cookie и
// localStorage с CSRF-токеном) переиспользуется всеми тестами. Лимит панели —
// 5 неверных попыток за 5 минут, поэтому тесты пароль не вводят вовсе.
export default async function globalSetup(_config: FullConfig) {
  const url = process.env.XCP_URL;
  const password = process.env.XCP_PASSWORD;
  const statePath = process.env.XCP_PW_STATE;
  const missing = [
    ['XCP_URL', url],
    ['XCP_PASSWORD', password],
    ['XCP_PW_STATE', statePath]
  ]
    .filter(([, v]) => !v)
    .map(([k]) => k);
  if (missing.length > 0) {
    throw new Error(`роутерный Playwright: не заданы переменные окружения: ${missing.join(', ')}`);
  }

  const who = `цель ${RT.arch || '?'}`;
  const browser = await chromium.launch();
  try {
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    const page = await context.newPage();

    try {
      await page.goto(url!, { timeout: T.login, waitUntil: 'domcontentloaded' });
      await page.locator('#password').waitFor({ state: 'visible', timeout: T.login });
    } catch (e) {
      throw new Error(`${who}: панель не отвечает (${reason(e)})`, { cause: e });
    }

    await page.locator('#password').fill(password!);
    await page.locator('.login-btn').click();

    try {
      await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.login });
    } catch (e) {
      const alert = await page
        .locator('.alert-error')
        .first()
        .textContent({ timeout: 1000 })
        .catch(() => null);
      throw new Error(`${who}: вход в панель не удался (${alert ? alert.trim() : reason(e)})`, {
        cause: e
      });
    }

    await context.storageState({ path: statePath! });
  } finally {
    await browser.close();
  }
}

function reason(e: unknown): string {
  const msg = e instanceof Error ? e.message : String(e);
  // первая строка без журнала вызовов Playwright
  return msg.split('\n')[0].trim();
}
