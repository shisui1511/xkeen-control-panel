import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { expect, type Browser, type Page } from '@playwright/test';
import { RT, T } from './env';

// Помощники роутерных сценариев вкладки «Узлы»: вызовы API панели из страницы с её сессией,
// наполнение черновика настоящими маршрутами предпросмотра и добавления, включение флага слоя
// на время прогона. Сеть не подменяется. В вывод не попадают имена узлов и содержимое ссылок.

const LINKS = fileURLToPath(new URL('../../../../internal/nodes/testdata/links/', import.meta.url));

/** Ссылка узла из тестовых данных с заданным портом и именем (идентичность узла уникальна). */
export function nodeLink(file: string, port: number, name: string): string {
  const raw = readFileSync(`${LINKS}${file}`, 'utf8').trim();
  return raw.replace(/(@[^:/?#]+:)\d+/, `$1${port}`).replace(/#.*$/, '') + `#${name}`;
}

export const randomPort = (): number => 20000 + Math.floor(Math.random() * 20000);

export interface ApiResult {
  status: number;
  data: Record<string, unknown> | null;
}

/** Запрос к API панели из страницы с её сессией и токеном CSRF; тело ответа — конверт data. */
export async function api(
  page: Page,
  method: string,
  path: string,
  body?: unknown
): Promise<ApiResult> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(path, {
        method,
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': localStorage.getItem('csrf_token') ?? ''
        },
        body: body === undefined ? undefined : JSON.stringify(body)
      });
      let data: Record<string, unknown> | null = null;
      try {
        const payload = await res.json();
        data = payload && typeof payload === 'object' ? (payload.data ?? payload) : null;
      } catch {
        data = null;
      }
      return { status: res.status, data };
    },
    { method, path, body }
  );
}

export async function withPanel<R>(browser: Browser, fn: (page: Page) => Promise<R>): Promise<R> {
  const context = await browser.newContext({
    baseURL: RT.url,
    ignoreHTTPSErrors: true,
    storageState: process.env.XCP_PW_STATE
  });
  try {
    const page = await context.newPage();
    await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
    return await fn(page);
  } finally {
    await context.close();
  }
}

export async function resetDraft(page: Page): Promise<void> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const revision = Number(view.data?.draft_revision ?? 0);
  const res = await api(page, 'POST', '/api/configlayer/draft/reset', { revision });
  expect(res.status, 'сброс черновика').toBe(200);
}

/** Кладёт ввод в черновик настоящими маршрутами предпросмотра и добавления. */
export async function addToDraft(page: Page, text: string): Promise<void> {
  const preview = await api(page, 'POST', '/api/configlayer/sources/preview', { text, files: [] });
  expect(preview.status, 'предпросмотр ввода').toBe(200);
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const add = await api(page, 'POST', '/api/configlayer/sources/add', {
    revision: Number(view.data?.draft_revision ?? 0),
    token: preview.data?.token
  });
  expect(add.status, 'добавление в черновик').toBe(200);
}

export interface Installed {
  xray: boolean;
  mihomo: boolean;
}

export async function installedKernels(page: Page): Promise<Installed> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  expect(view.status, 'GET /api/configlayer/sources').toBe(200);
  const inst = (view.data?.installed ?? {}) as Partial<Installed>;
  return { xray: inst.xray === true, mihomo: inst.mihomo === true };
}

export interface SourceInfo {
  id: string;
  kind: string;
  name: string;
  host?: string;
  enabled: boolean;
  settings?: {
    exclude: string[];
    interval: { mode: string; hours?: number };
    hwid_custom: boolean;
  };
}

export async function listSources(page: Page): Promise<SourceInfo[]> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  expect(view.status, 'GET /api/configlayer/sources').toBe(200);
  return (view.data?.sources ?? []) as SourceInfo[];
}

/** Состояние флага слоя до прогона: включается на время сценариев и возвращается прежним. */
export function layerFlag() {
  let wasOn = false;
  return {
    async enable(browser: Browser): Promise<void> {
      await withPanel(browser, async (page) => {
        const settings = await api(page, 'GET', '/api/settings');
        expect(settings.status, 'GET /api/settings').toBe(200);
        wasOn = settings.data?.config_layer === true;
        if (!wasOn) {
          const res = await api(page, 'POST', '/api/settings/config-layer', { enabled: true });
          expect(res.status, 'включение слоя').toBe(200);
        }
        await resetDraft(page);
      });
    },
    async restore(browser: Browser): Promise<void> {
      await withPanel(browser, async (page) => {
        await resetDraft(page);
        if (!wasOn) {
          const res = await api(page, 'POST', '/api/settings/config-layer', { enabled: false });
          expect(res.status, 'возврат флага слоя').toBe(200);
        }
      });
    }
  };
}
