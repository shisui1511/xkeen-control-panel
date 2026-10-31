// e2e-pages: config/cores
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, resetDraft } from './lib/nodes-api';

// Формы секций Xray на настоящей панели (фаза 147, план 22): DNS, «Лог», «Политики» и
// «Входящие» правятся полями без потери ключей, которых формы не знают; входящие, которые
// читает XKeen, сохраняют тег и вид, а смена их порта объявляется до «Применить». Ответы сети
// не подменяются. Флаг слоя включается на время прогона и возвращается прежним. Тесты форм
// пишут только в черновик и сбрасывают его; DNS-тест дополнительно берёт секцию, применяет и
// отпускает её, возвращая исходный файл.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

const XRAY_DIR = '/opt/etc/xray/configs';
const DNS_FILE = `${XRAY_DIR}/02_dns.json`;
const DNS_PANEL_FILE = `${XRAY_DIR}/99_xcp-dns.json`;

async function openConfig(page: Page, hash: string): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Вкладка загружена: она видна и скелетона больше нет. */
async function tabReady(page: Page, testId: string): Promise<void> {
  const tab = page.getByTestId(testId);
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
}

interface SectionBrief {
  id: string;
  owner: string;
  blocked: string;
  files: string[];
  value: unknown;
  form: unknown;
  advanced: string;
}

interface XrayView {
  installed: boolean;
  sections: SectionBrief[];
  forceProxy: { tags: string[]; group: string; editable: boolean } | null;
}

/** Вид секций Xray по настоящему API. */
async function xrayView(page: Page): Promise<XrayView> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=xray');
  expect(view.status, 'GET /api/configlayer/cores?kernel=xray').toBe(200);
  const sections = ((view.data?.sections ?? []) as Record<string, unknown>[]).map((s) => ({
    id: String(s.id ?? ''),
    owner: String(s.owner ?? ''),
    blocked: String(s.blocked ?? ''),
    files: (s.files ?? []) as string[],
    value: s.value,
    form: s.form,
    advanced: String(s.advanced ?? '')
  }));
  const fp = view.data?.force_proxy as XrayView['forceProxy'] | undefined;
  return { installed: view.data?.installed === true, sections, forceProxy: fp ?? null };
}

async function sectionOf(page: Page, id: string): Promise<SectionBrief | undefined> {
  return (await xrayView(page)).sections.find((s) => s.id === id);
}

/** Значение под ключом секции: вид отдаёт значение секции, файл панели — объект с ключом. */
function inner(value: unknown, key: string): Record<string, unknown> {
  const obj = (value ?? {}) as Record<string, unknown>;
  const nested = obj[key];
  const pick = nested !== null && typeof nested === 'object' ? nested : obj;
  return pick as Record<string, unknown>;
}

/** Запрос к API панели из страницы с её сессией; тело уходит строкой, как у Редактора. */
async function rawRequest(
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

/** Содержимое файла конфига; null, если файла нет. */
async function readConfig(page: Page, path: string): Promise<string | null> {
  const res = await rawRequest(page, 'GET', `/api/config/read?path=${encodeURIComponent(path)}`);
  if (res.status === 404) return null;
  expect(res.status, `чтение ${path}`).toBe(200);
  return res.text;
}

/** Записывает файл конфига; нет файла — сначала создаёт. */
async function writeConfig(page: Page, path: string, content: string): Promise<void> {
  const q = encodeURIComponent(path);
  if ((await readConfig(page, path)) === null) {
    const created = await rawRequest(page, 'POST', `/api/config/create?path=${q}`);
    expect(created.status, `создание ${path}: ${created.text.slice(0, 200)}`).toBe(200);
  }
  const saved = await rawRequest(page, 'POST', `/api/config/save?path=${q}`, content);
  expect(saved.status, `запись ${path}: ${saved.text.slice(0, 200)}`).toBe(200);
}

/** Убирает файл конфига; нет файла (404) — не ошибка. */
async function removeConfig(page: Page, path: string): Promise<void> {
  const res = await rawRequest(page, 'POST', `/api/config/delete?path=${encodeURIComponent(path)}`);
  expect([200, 404], `удаление ${path}: ${res.text.slice(0, 200)}`).toContain(res.status);
}

interface LayerProbe {
  revision: number;
  running: boolean;
  changes: number;
  taken: Record<string, boolean>;
}

/** Снимок слоя: ревизия черновика, ход применения и владение секциями Xray. */
async function layerProbe(page: Page): Promise<LayerProbe> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const cores = (st.data?.cores ?? {}) as { xray?: Record<string, { taken?: boolean }> };
  const apply = (st.data?.apply ?? {}) as { running?: boolean };
  const taken: Record<string, boolean> = {};
  for (const [id, snap] of Object.entries(cores.xray ?? {})) taken[id] = snap.taken === true;
  return {
    revision: Number(st.data?.draft_revision ?? 0),
    running: apply.running === true,
    changes: Number(st.data?.draft_changes ?? 0),
    taken
  };
}

/** Применяет черновик настоящим маршрутом и ждёт конца: применение не идёт, изменений нет. */
async function applyAndWait(page: Page): Promise<void> {
  const start = await api(page, 'POST', '/api/configlayer/apply', {});
  expect(start.status, 'запуск применения').toBe(200);
  await expect
    .poll(
      async () => {
        const s = await layerProbe(page);
        return !s.running && s.changes === 0;
      },
      { timeout: T.save * 4, intervals: [2000] }
    )
    .toBe(true);
}

/** Возвращает секцию Xray прежнему владельцу настоящими маршрутами (уборка). */
async function releaseViaApi(page: Page, id: string): Promise<void> {
  const s = await layerProbe(page);
  if (!s.taken[id]) return;
  const res = await api(page, 'POST', '/api/configlayer/cores/release', {
    revision: s.revision,
    kernel: 'xray',
    section: id
  });
  expect(res.status, `возврат секции ${id} через API`).toBe(200);
  await applyAndWait(page);
}

/** Открывает вкладку ядер, выбирает Xray и раскрывает карточку секции. */
async function openSection(page: Page, id: string): Promise<void> {
  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await page.getByTestId('kernel-switch-xray').click();
  await expect(page.getByTestId('cores-panel-xray')).toBeVisible({ timeout: T.page });
  const toggle = page.getByTestId(`section-toggle-xray-${id}`);
  await expect(toggle).toBeVisible({ timeout: T.page });
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

/** Берёт секцию в черновик через диалог; секция уже у панели — ничего не делает. */
async function takeViaDialog(page: Page, id: string): Promise<void> {
  const card = page.getByTestId(`section-card-xray-${id}`);
  if (/^(panel|pending)$/.test((await card.getAttribute('data-owner')) ?? '')) return;
  await page.getByTestId(`section-take-xray-${id}`).click();
  const modal = page.getByTestId('take-section-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('take-section-loading')).toHaveCount(0, { timeout: T.action });
  const ack = modal.getByTestId('take-section-comments-ack');
  if (await ack.isVisible()) await ack.check();
  await modal.getByTestId('take-section-confirm').click();
  await expect(modal).toHaveCount(0, { timeout: T.action });
  await expect(card).toHaveAttribute('data-owner', /^(pending|panel)$/, { timeout: T.action });
}

// --- DNS ------------------------------------------------------------------------------------

test('cores: Xray DNS — серверы списком, сложный сервер в «Дополнительно»', async ({ page }) => {
  test.setTimeout(T.save * 14);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await xrayView(page);
  test.skip(!before.installed, 'Xray не установлен');
  const dns = before.sections.find((s) => s.id === 'dns');
  test.skip(dns?.owner === 'panel' || dns?.owner === 'pending', 'секция DNS уже у панели');
  await resetDraft(page);

  const complexServer = { address: '127.0.0.2', domains: ['domain:example.test'] };
  const fixture = `${JSON.stringify(
    { dns: { servers: ['127.0.0.1', complexServer], queryStrategy: 'UseIP' } },
    null,
    2
  )}\n`;
  const original = await readConfig(page, DNS_FILE);
  try {
    await writeConfig(page, DNS_FILE, fixture);

    // секция читается только из 02_dns.json: другие файлы с ключом dns меняют раскладку
    await expect
      .poll(async () => (await sectionOf(page, 'dns'))?.files.length ?? 0, { timeout: T.action })
      .toBeGreaterThan(0);
    const files = (await sectionOf(page, 'dns'))?.files ?? [];
    const foreign = files.filter((f) => !f.endsWith('02_dns.json'));
    expect(
      foreign,
      `раскладка стенда не та, что у шаблонов XKeen: ключ dns и в файлах ${foreign.join(', ')}`
    ).toEqual([]);

    await openSection(page, 'dns');
    await takeViaDialog(page, 'dns');

    // форма: серверы-объекты списком не показываются, текст сервера в форму не попадает
    const form = page.getByTestId('section-form-xray-dns');
    await expect(form).toBeVisible({ timeout: T.action });
    const complex = page.getByTestId('section-field-xray-dns-servers-complex');
    await expect(complex).toBeVisible({ timeout: T.action });
    await expect(complex).not.toContainText('example.test');
    await expect(page.getByTestId('section-field-xray-dns-servers-0')).toHaveCount(0);

    // смена стратегии пишет в черновик; сложный сервер остаётся
    const revision = (await layerProbe(page)).revision;
    await page.getByTestId('section-field-xray-dns-querystrategy').selectOption('UseIPv4');
    await expect
      .poll(async () => (await layerProbe(page)).revision, { timeout: T.action })
      .toBeGreaterThan(revision);
    await expect
      .poll(
        async () => {
          const value = inner((await sectionOf(page, 'dns'))?.value, 'dns');
          const servers = (value.servers ?? []) as unknown[];
          const kept = servers.some(
            (s) =>
              s !== null &&
              typeof s === 'object' &&
              ((s as { domains?: string[] }).domains ?? []).includes('domain:example.test')
          );
          return kept && value.queryStrategy === 'UseIPv4';
        },
        { timeout: T.action }
      )
      .toBe(true);

    // «Применить»: файл панели хранит сложный сервер и новую стратегию
    await applyAndWait(page);
    const written = await readConfig(page, DNS_PANEL_FILE);
    expect(written, `${DNS_PANEL_FILE} после «Применить»`).not.toBeNull();
    const panel = inner(JSON.parse(written ?? '{}'), 'dns');
    expect(panel.queryStrategy, 'queryStrategy в файле панели').toBe('UseIPv4');
    expect(JSON.stringify(panel.servers), 'сложный сервер сохранён в файле панели').toContain(
      'domain:example.test'
    );

    // «Отпустить»: подтверждение, затем «Применить» возвращает исходный файл побайтно
    await page.getByTestId('section-release-xray-dns').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: T.action });
    await dialog.getByRole('button', { name: /^(Отпустить секцию|Release section)$/ }).click();
    await expect(page.getByTestId('config-apply')).toBeEnabled({ timeout: T.action });
    await applyAndWait(page);
    expect(await readConfig(page, DNS_FILE), '02_dns.json вернулся побайтно').toBe(fixture);

    await guard.assertClean();
  } finally {
    await resetDraft(page);
    await releaseViaApi(page, 'dns');
    if (original === null) await removeConfig(page, DNS_FILE);
    else await writeConfig(page, DNS_FILE, original);
  }
});
