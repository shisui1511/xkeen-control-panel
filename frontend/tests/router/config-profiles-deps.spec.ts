// e2e-pages: config/nodes settings
// router-area: profiles
import { createHash } from 'node:crypto';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import {
  addToDraft,
  api,
  installedKernels,
  layerFlag,
  listSources,
  resetDraft,
  withPanel
} from './lib/nodes-api';

// Строки о зависимых профилях в диалогах удаления источника и выключения флага слоя (фаза
// 147.1, план 23, D-05): профиль пользователя со ссылкой на proxy_providers/xcp-<id>.yaml
// назван в абзаце диалога, действие не блокируется, файл профиля панель не правит. Ответы
// сети не подменяются. Профиль и файл провайдера кладутся настоящими маршрутами файлов
// конфига (файл провайдера нужен, чтобы mihomo -t принял профиль), убираются в afterAll.
// Сбой чтения профилей не воспроизводится: подмена сети запрещена, поведение следует из
// кода DependentProfilesNote.

const MIHOMO_DIR = '/opt/etc/mihomo';
const PROFILE = 'rt-dep-ui';
const PROFILE_PATH = `${MIHOMO_DIR}/profiles/${PROFILE}.yaml`;
// Адрес в зарезервированной зоне .invalid: имя не разрешается нигде
const BAD_SUB = 'https://sub.invalid/feed';
// Источник, которого в панели нет: нужен только имени файла провайдера для флага слоя
const FLAG_ID = '0badc0de';

const layer = layerFlag();
const providers = new Set<string>();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => layer.enable(browser));

test.afterAll(async ({ browser }) => {
  await withPanel(browser, async (page) => {
    await removeFile(page, PROFILE_PATH);
    for (const id of providers) await removeFile(page, providerPath(id));
  });
  await layer.restore(browser);
});

const providerPath = (id: string): string => `${MIHOMO_DIR}/proxy_providers/xcp-${id}.yaml`;

async function raw(
  page: Page,
  method: string,
  path: string,
  body?: string
): Promise<{ status: number; text: string }> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(path, {
        method,
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': localStorage.getItem('csrf_token') ?? ''
        },
        body
      });
      return { status: res.status, text: await res.text() };
    },
    { method, path, body }
  );
}

async function readFile(page: Page, path: string): Promise<string | null> {
  const res = await raw(page, 'GET', `/api/config/read?path=${encodeURIComponent(path)}`);
  if (res.status === 404) return null;
  expect(res.status, `чтение ${path}`).toBe(200);
  return res.text;
}

async function writeFile(page: Page, path: string, content: string): Promise<void> {
  const q = encodeURIComponent(path);
  if ((await readFile(page, path)) === null) {
    const created = await raw(page, 'POST', `/api/config/create?path=${q}`);
    expect(created.status, `создание ${path}`).toBe(200);
  }
  const saved = await raw(page, 'POST', `/api/config/save?path=${q}`, content);
  expect(saved.status, `запись ${path}`).toBe(200);
}

async function removeFile(page: Page, path: string): Promise<void> {
  await raw(page, 'POST', `/api/config/delete?path=${encodeURIComponent(path)}`);
}

const md5 = (text: string): string => createHash('md5').update(text).digest('hex');

/** Файл провайдера и профиль пользователя со ссылкой на него; возвращает md5 файла профиля. */
async function installProfile(page: Page, id: string): Promise<string> {
  providers.add(id);
  await writeFile(
    page,
    providerPath(id),
    'proxies:\n  - name: rt-dep-node\n    type: socks5\n    server: 127.0.0.1\n    port: 1\n'
  );
  const profile =
    'mixed-port: 7891\nmode: rule\nproxy-providers:\n  own:\n    type: file\n' +
    `    path: ./proxy_providers/xcp-${id}.yaml\n` +
    'proxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n';
  await writeFile(page, PROFILE_PATH, profile);
  const text = await readFile(page, PROFILE_PATH);
  expect(text, 'профиль записан').not.toBeNull();
  return md5(text ?? '');
}

async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
  await resetDraft(page);
}

test('profiles-deps: удаление источника называет профиль', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  test.skip(!(await installedKernels(page)).mihomo, 'Mihomo не установлен на стенде');

  await addToDraft(page, BAD_SUB);
  const found = (await listSources(page)).filter((s) => s.host === 'sub.invalid');
  expect(found.length, 'подписка из черновика одна').toBe(1);
  const id = found[0].id;
  const before = await installProfile(page, id);

  try {
    await page.goto('/#/config/nodes', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('nodes-tab')).toBeVisible({ timeout: T.page });
    const del = page.getByTestId(`nodes-delete-${id}`);
    await expect(del).toBeVisible({ timeout: T.action });
    await del.click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: T.action });
    // кнопка подтверждения доступна, не дожидаясь ответа о профилях
    await expect(dialog.locator('.btn-danger')).toBeEnabled();
    const note = dialog.getByTestId('dep-profiles-note');
    await expect(note).toBeVisible({ timeout: T.action });
    await expect(note.locator('.mono', { hasText: PROFILE })).toHaveCount(1);
    await expect(note).toContainText(`xcp-${id}.yaml`);
    await expect(dialog.locator('.btn-danger')).toBeEnabled();

    // отмена: источник на месте, файл профиля не менялся
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await expect(page.getByTestId(`nodes-source-${id}`)).toBeVisible();
    expect(md5((await readFile(page, PROFILE_PATH)) ?? ''), 'файл профиля не менялся').toBe(before);
  } finally {
    await resetDraft(page);
  }
  await guard.assertClean();
});

test('profiles-deps: выключение флага называет профиль', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  test.skip(!(await installedKernels(page)).mihomo, 'Mihomo не установлен на стенде');

  const before = await installProfile(page, FLAG_ID);

  await page.goto('/#/settings', { timeout: T.page, waitUntil: 'domcontentloaded' });
  const row = page.getByTestId('config-layer-row');
  await expect(row).toBeVisible({ timeout: T.page });
  const toggle = row.getByRole('checkbox');
  await expect(toggle).toBeChecked();
  await row.locator('.toggle-slider').click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible({ timeout: T.action });
  const note = dialog.getByTestId('dep-profiles-note');
  await expect(note).toBeVisible({ timeout: T.action });
  await expect(note.locator('.mono', { hasText: PROFILE })).toHaveCount(1);
  await expect(dialog.getByRole('button').last()).toBeEnabled();

  // отмена: флаг остаётся включённым, профиль не менялся
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(toggle).toBeChecked();
  const settings = await api(page, 'GET', '/api/settings');
  expect(settings.data?.config_layer, 'флаг слоя включён').toBe(true);
  expect(md5((await readFile(page, PROFILE_PATH)) ?? ''), 'файл профиля не менялся').toBe(before);

  await guard.assertClean();
});
