import { existsSync, readFileSync } from 'node:fs';
import { request, type FullConfig } from '@playwright/test';
import { T } from './lib/env';

// Выход из сессии панели, открытой global-setup: без него сессии копятся до лимита панели (20)
// и вытесняют ручную сессию владельца, а cookie из трасс `retain-on-failure` остаются живыми.
// Выход идёт тем же storageState, что у тестов: cookie сессии и токен CSRF из localStorage.
// Сбой выхода прогон не роняет и ничего, кроме фиксированной строки, не печатает: в тексте
// исключения может оказаться адрес панели.
export default async function globalTeardown(_config: FullConfig) {
  const url = process.env.XCP_URL;
  const statePath = process.env.XCP_PW_STATE;
  // запуск `--list` и этап без входа: сессии нет, выходить не из чего
  if (!url || !statePath || !existsSync(statePath)) return;

  try {
    const csrf = readCsrf(statePath);
    const ctx = await request.newContext({
      baseURL: url,
      ignoreHTTPSErrors: true,
      storageState: statePath
    });
    try {
      const res = await ctx.post('/api/auth/logout', {
        headers: { 'X-CSRF-Token': csrf },
        timeout: T.action
      });
      console.log(`global-teardown: выход из панели, http ${res.status()}`);
    } finally {
      await ctx.dispose().catch(() => {});
    }
  } catch {
    console.log('global-teardown: выход из панели не удался');
  }
}

/** Токен CSRF сессии: запись csrf_token в localStorage любого источника состояния. */
function readCsrf(statePath: string): string {
  const state = JSON.parse(readFileSync(statePath, 'utf8')) as {
    origins?: { localStorage?: { name: string; value: string }[] }[];
  };
  for (const origin of state.origins ?? []) {
    for (const item of origin.localStorage ?? []) {
      if (item.name === 'csrf_token') return item.value;
    }
  }
  return '';
}
