// e2e-pages: config/cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, randomPort, resetDraft } from './lib/nodes-api';

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

/** Открывает вкладку ядер и выбирает Xray. */
async function openCores(page: Page): Promise<void> {
  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await page.getByTestId('kernel-switch-xray').click();
  await expect(page.getByTestId('cores-panel-xray')).toBeVisible({ timeout: T.page });
}

/** Раскрывает карточку секции Xray на открытой вкладке ядер. */
async function expandSection(page: Page, id: string): Promise<void> {
  const toggle = page.getByTestId(`section-toggle-xray-${id}`);
  await expect(toggle).toBeVisible({ timeout: T.page });
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

/** Открывает вкладку ядер, выбирает Xray и раскрывает карточку секции. */
async function openSection(page: Page, id: string): Promise<void> {
  await openCores(page);
  await expandSection(page, id);
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

// --- «Лог» и «Политики» ---------------------------------------------------------------------

/** Открывает раскрытый блок «Дополнительно» секции и возвращает его поле. */
async function openAdvanced(page: Page, id: string) {
  const block = page.getByTestId(`section-advanced-block-xray-${id}`);
  await expect(block).toBeVisible({ timeout: T.action });
  if ((await block.getAttribute('open')) === null) await block.locator('summary').click();
  const area = page.getByTestId(`section-advanced-xray-${id}`);
  await expect(area).toBeVisible({ timeout: T.action });
  return area;
}

test('cores: Xray лог и политики — поля вне формы и закреплённые значения', async ({ page }) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await xrayView(page);
  test.skip(!before.installed, 'Xray не установлен');
  for (const id of ['log', 'policy']) {
    const s = before.sections.find((x) => x.id === id);
    test.skip(s === undefined || s.blocked !== '', `секция ${id} не берётся`);
  }
  await resetDraft(page);

  try {
    // «Лог»: ключ вне формы переживает правку поля формы
    await openSection(page, 'log');
    await takeViaDialog(page, 'log');
    await expect(page.getByTestId('section-form-xray-log')).toBeVisible({ timeout: T.action });
    const advanced = await openAdvanced(page, 'log');
    await advanced.fill('{\n  "maskAddress": "quarter"\n}');
    await advanced.blur();
    await expect
      .poll(async () => inner((await sectionOf(page, 'log'))?.value, 'log').maskAddress, {
        timeout: T.action
      })
      .toBe('quarter');

    const access = '/opt/var/log/xray/access.log';
    const field = page.getByTestId('section-field-xray-log-access');
    await field.fill(access);
    await field.blur();
    await expect
      .poll(
        async () => {
          const log = inner((await sectionOf(page, 'log'))?.value, 'log');
          return log.access === access && log.maskAddress === 'quarter';
        },
        { timeout: T.action }
      )
      .toBe(true);
    // поле формы не дублируется в «Дополнительно»
    expect(await advanced.inputValue()).not.toContain('"access"');

    // две правки подряд, пока первая ещё пишется: вторая не теряется и ждёт очереди
    const access2 = '/opt/var/log/xray/access2.log';
    const errorLog = '/opt/var/log/xray/error2.log';
    const errorField = page.getByTestId('section-field-xray-log-error');
    await field.fill(access2);
    await field.blur();
    await errorField.fill(errorLog);
    await errorField.blur();
    await expect
      .poll(
        async () => {
          const log = inner((await sectionOf(page, 'log'))?.value, 'log');
          return log.access === access2 && log.error === errorLog && log.maskAddress === 'quarter';
        },
        { timeout: T.action }
      )
      .toBe(true);
    await expect(field).toHaveValue(access2);
    await expect(errorField).toHaveValue(errorLog);

    // «Политики»: закреплённые счётчики видны, но не правятся
    await openSection(page, 'policy');
    await takeViaDialog(page, 'policy');
    const pinned = page.getByTestId('section-pinned-xray-policy');
    await expect(pinned).toBeVisible({ timeout: T.action });
    const summary = pinned.locator('summary');
    if ((await summary.count()) > 0) await summary.click();
    await expect(pinned.locator('.pv-row')).toHaveCount(4);
    await expect(pinned.locator('.pv-badge')).toHaveCount(4);
    for (const key of [
      'statsInboundUplink',
      'statsInboundDownlink',
      'statsOutboundUplink',
      'statsOutboundDownlink'
    ]) {
      await expect(pinned).toContainText(`system.${key}`);
    }
    const counters = inner((await sectionOf(page, 'policy'))?.value, 'policy').system;

    const handshake = page.getByTestId('section-field-xray-policy-levels-0-handshake');
    await handshake.fill('5');
    await handshake.blur();
    await expect
      .poll(
        async () => {
          const levels = inner((await sectionOf(page, 'policy'))?.value, 'policy').levels as
            Record<string, { handshake?: number }> | undefined;
          return levels?.['0']?.handshake;
        },
        { timeout: T.action }
      )
      .toBe(5);
    // правка поля формы закреплённые счётчики не трогает
    expect(inner((await sectionOf(page, 'policy'))?.value, 'policy').system).toEqual(counters);

    // неверное значение: ошибка под полем, в черновик ничего не уходит
    const revision = (await layerProbe(page)).revision;
    await handshake.fill('-1');
    await handshake.blur();
    await expect(handshake).toHaveAttribute('aria-invalid', 'true');
    await expect(handshake).toHaveClass(/input-error/);
    const describedBy = await handshake.getAttribute('aria-describedby');
    expect(describedBy, 'ошибка связана с полем').toBeTruthy();
    await expect(page.locator(`[id="${describedBy}"]`)).toBeVisible();
    await page.waitForTimeout(1000);
    expect((await layerProbe(page)).revision, 'неверное значение не меняет черновик').toBe(
      revision
    );

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

// --- «Входящие» -----------------------------------------------------------------------------

const INBOUND_ROWS = '[data-testid^="section-inbound-row-"]';

test('cores: Xray входящие — закреплённые строки и объявление перезапуска', async ({ page }) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await xrayView(page);
  test.skip(!before.installed, 'Xray не установлен');
  const inbounds = before.sections.find((s) => s.id === 'inbounds');
  test.skip(inbounds === undefined, 'в виде нет секции «Входящие»');
  await resetDraft(page);

  try {
    await openSection(page, 'inbounds');

    // входящие лежат в файле с другими ключами или XKeen старый: секция не берётся
    if (inbounds?.blocked) {
      await expect(page.getByTestId('section-blocked-xray-inbounds')).toBeVisible({
        timeout: T.action
      });
      await expect(page.getByTestId('section-take-xray-inbounds')).toBeDisabled();
      await guard.assertClean();
      return;
    }

    await takeViaDialog(page, 'inbounds');
    await expect(page.getByTestId('section-form-xray-inbounds')).toBeVisible({
      timeout: T.action
    });
    const pinnedRows = page.locator(`${INBOUND_ROWS}[data-pinned="true"]`);
    test.skip((await pinnedRows.count()) === 0, 'на стенде нет прозрачных входящих XKeen');

    // закреплённая строка: бейдж, нет поля тега, правится только порт
    const row = pinnedRows.first();
    await expect(row.locator('[data-testid^="section-inbound-pinned-"]')).toBeVisible();
    await expect(row.locator('[data-testid^="section-field-xray-inbounds-tag-"]')).toHaveCount(0);
    await expect(row.locator('[data-testid^="section-field-xray-inbounds-remove-"]')).toHaveCount(
      0
    );
    const port = row.locator('[data-testid^="section-field-xray-inbounds-port-"]');
    const note = page.getByTestId('section-restart-note-xray-inbounds');
    await expect(note).toHaveCount(0);

    // смена порта объявляется под полем и в полосе черновика
    const oldPort = await port.inputValue();
    let newPort = String(randomPort());
    if (newPort === oldPort) newPort = String(Number(newPort) + 1);
    await port.fill(newPort);
    await port.blur();
    await expect(note).toBeVisible({ timeout: T.action });
    // объявление в полосе черновика ждём всегда, в том числе когда «Входящие» берутся в этом же
    // черновике: взятие сравнивается с действующим значением configs/ (G-147-3, D-16)
    await expect(
      page.locator('[data-testid="draft-restart-note"][data-kernel="xray"][data-reason="port"]')
    ).toBeVisible({ timeout: T.action });

    // вернули записанное значение: объявление под полем исчезло
    await port.fill(oldPort);
    await port.blur();
    await expect(note).toHaveCount(0, { timeout: T.action });

    // новая строка без тега: ошибка, в черновик ничего не уходит
    const revision = (await layerProbe(page)).revision;
    await page.getByTestId('section-field-xray-inbounds-add').click();
    const error = page
      .locator('.error-text')
      .filter({ hasText: /Задайте тег|Set a tag/ })
      .first();
    await expect(error).toBeVisible({ timeout: T.action });
    await page.waitForTimeout(1000);
    expect((await layerProbe(page)).revision, 'пустой тег не меняет черновик').toBe(revision);

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

test('cores: Xray входящие — redirect и tproxy на одном порту, остаток входящего без тега', async ({
  page
}) => {
  test.setTimeout(T.save * 6);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await xrayView(page);
  test.skip(!before.installed, 'Xray не установлен');
  const inbounds = before.sections.find((s) => s.id === 'inbounds');
  test.skip(inbounds === undefined, 'в виде нет секции «Входящие»');
  test.skip(!!inbounds?.blocked, `секция «Входящие» не берётся: ${inbounds?.blocked}`);
  await resetDraft(page);

  const ADD_TAG = 'xcp-review-tagged';
  const NEW_TAG = 'xcp-review-named';
  type Inbound = Record<string, unknown>;

  /** Строки формы и остаток «Дополнительно» секции «Входящие» по настоящему API. */
  const current = async (): Promise<{ form: Inbound[]; adv: Record<string, unknown> }> => {
    const s = await sectionOf(page, 'inbounds');
    const form = Array.isArray(s?.form) ? (s.form as Inbound[]) : [];
    const adv = JSON.parse(s?.advanced?.trim() ? s.advanced : '{}') as Record<string, unknown>;
    return { form, adv };
  };

  try {
    await openSection(page, 'inbounds');
    await takeViaDialog(page, 'inbounds');
    await expect(page.getByTestId('section-form-xray-inbounds')).toBeVisible({
      timeout: T.action
    });

    // 1. Пара прозрачных входящих XKeen на одном порту (redirect — tcp, tproxy — udp) не повтор:
    // у закреплённых строк нет ошибки, а один новый порт обоим записывается
    const pinnedRows = page.locator(`${INBOUND_ROWS}[data-pinned="true"]`);
    const portOf = (i: number) =>
      pinnedRows.nth(i).locator('[data-testid^="section-field-xray-inbounds-port-"]');
    if ((await pinnedRows.count()) >= 2) {
      const first = await portOf(0).inputValue();
      if (first === (await portOf(1).inputValue())) {
        await expect(pinnedRows.locator('.error-text')).toHaveCount(0);
        const tags = [
          (
            await pinnedRows.nth(0).locator('[data-testid^="section-inbound-tag-"]').innerText()
          ).trim(),
          (
            await pinnedRows.nth(1).locator('[data-testid^="section-inbound-tag-"]').innerText()
          ).trim()
        ];
        let shared = String(randomPort());
        if (shared === first) shared = String(Number(shared) + 1);
        for (const i of [0, 1]) {
          await portOf(i).fill(shared);
          await portOf(i).blur();
          await expect(pinnedRows.locator('.error-text')).toHaveCount(0);
        }
        await expect
          .poll(
            async () => {
              const { form } = await current();
              return tags.map((tag) => String(form.find((r) => r.tag === tag)?.port ?? ''));
            },
            { timeout: T.action }
          )
          .toEqual([shared, shared]);
      }
    }

    // 2. Входящий без тега: удаление строки выше переносит его остаток под новый номер
    const base = await current();
    const idx = base.form.length;
    const udpSettings = { auth: 'noauth', udp: true };
    const seeded = await api(page, 'POST', '/api/configlayer/cores/save', {
      revision: (await layerProbe(page)).revision,
      kernel: 'xray',
      section: 'inbounds',
      form: [
        ...base.form,
        { tag: ADD_TAG, protocol: 'socks', listen: '127.0.0.1', port: randomPort() },
        { protocol: 'socks', listen: '127.0.0.1', port: randomPort() }
      ],
      advanced: JSON.stringify({ ...base.adv, [`#${idx + 1}`]: { settings: udpSettings } })
    });
    expect(seeded.status, 'запись входящих с входящим без тега').toBe(200);
    await openSection(page, 'inbounds');
    await expect(page.getByTestId(`section-field-xray-inbounds-remove-${idx}`)).toBeVisible({
      timeout: T.action
    });
    await page.getByTestId(`section-field-xray-inbounds-remove-${idx}`).click();
    await expect
      .poll(
        async () => {
          const { form, adv } = await current();
          const moved = adv[`#${idx}`] as { settings?: { udp?: boolean } } | undefined;
          return (
            form.length === idx + 1 && moved?.settings?.udp === true && !(`#${idx + 1}` in adv)
          );
        },
        { timeout: T.action }
      )
      .toBe(true);

    // 3. Тег входящему без тега: остаток переезжает под тег и не теряется
    const tagInput = page.getByTestId(`section-field-xray-inbounds-tag-${idx}`);
    await expect(tagInput).toBeVisible({ timeout: T.action });
    await tagInput.fill(NEW_TAG);
    await tagInput.blur();
    await expect
      .poll(
        async () => {
          const { form, adv } = await current();
          const named = adv[NEW_TAG] as { settings?: { udp?: boolean } } | undefined;
          return (
            form.some((r) => r.tag === NEW_TAG) &&
            named?.settings?.udp === true &&
            !(`#${idx}` in adv)
          );
        },
        { timeout: T.action }
      )
      .toBe(true);

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

test('cores: Xray входящие — группа force-proxy', async ({ page }) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await xrayView(page);
  test.skip(!before.installed, 'Xray не установлен');
  const inbounds = before.sections.find((s) => s.id === 'inbounds');
  test.skip(inbounds === undefined, 'в виде нет секции «Входящие»');
  const hasTag = (before.forceProxy?.tags.length ?? 0) > 0;
  test.skip(!hasTag && !!inbounds?.blocked, `секция «Входящие» не берётся: ${inbounds?.blocked}`);
  await resetDraft(page);

  try {
    await openSection(page, 'inbounds');

    if (!hasTag) {
      // тега force-proxy на стенде нет: создаём его в черновике сами, чтобы проводка
      // «вид → карточка → форма» проверялась всегда
      await takeViaDialog(page, 'inbounds');
      const rows = page.locator(INBOUND_ROWS);
      // Вид секции после взятия приходит отдельным обновлением и сбрасывает строки формы,
      // ещё не ушедшие в черновик: строку добавляем заново, пока она не удержится
      await expect(async () => {
        const count = await rows.count();
        await page.getByTestId('section-field-xray-inbounds-add').click();
        await expect(rows).toHaveCount(count + 1, { timeout: 3000 });
        const tag = page.getByTestId(`section-field-xray-inbounds-tag-${count}`);
        await tag.fill('force-proxy', { timeout: 3000 });
        await tag.blur();
        const listen = page.getByTestId(`section-field-xray-inbounds-listen-${count}`);
        await listen.fill('127.0.0.1', { timeout: 3000 });
        await listen.blur();
        const port = page.getByTestId(`section-field-xray-inbounds-port-${count}`);
        await port.fill(String(randomPort()), { timeout: 3000 });
        await port.blur();
      }).toPass({ timeout: T.action });
      await expect
        .poll(async () => (await xrayView(page)).forceProxy?.tags ?? [], { timeout: T.action })
        .toContain('force-proxy');
    }

    // блок группы есть ровно один и объясняет режим speed balancer
    const block = page.getByTestId('section-force-proxy-xray');
    await expect(block).toHaveCount(1, { timeout: T.action });
    await expect(block).toBeVisible();
    await expect(block).toContainText('xkeen -sb');

    const view = await xrayView(page);
    const select = page.getByTestId('section-force-proxy-select-xray');
    if (view.forceProxy?.editable === false) {
      // маршрутизацией управляет не панель: выбор недоступен, причина названа
      await expect(select).toBeDisabled();
      await expect(page.getByTestId('section-force-proxy-locked-xray')).toBeVisible();
    } else {
      await expect(select).toBeEnabled({ timeout: T.action });
      const options = await select
        .locator('option')
        .evaluateAll((els) => els.map((e) => (e as HTMLOptionElement).value));
      const current = await select.inputValue();
      const other = options.find((v) => v !== '' && v !== current);
      if (other !== undefined) {
        await select.selectOption(other);
        await expect
          .poll(async () => (await xrayView(page)).forceProxy?.group, { timeout: T.action })
          .toBe(other);
      }
    }

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри заданных областей. Вывод — правило, селекторы и числа контраста.
 */
async function axeSevere(page: Page, scopes: string[]): Promise<string[]> {
  // раскрытие карточки идёт с затуханием: axe не должен видеть полупрозрачный кадр
  await page.waitForTimeout(500);
  let builder = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);
  for (const scope of scopes) builder = builder.include(scope);
  const scan = await builder.analyze();
  return scan.violations
    .filter((v) => v.impact === 'critical' || v.impact === 'serious')
    .map((v) => {
      const nodes = v.nodes.slice(0, 3).map((n) => {
        const msg = (n.any[0]?.message ?? '')
          .replace(/^Element has insufficient color contrast of /, 'контраст ')
          .replace(/, font size:.*?\)/, ')')
          .replace(/\. Expected contrast ratio of /, ', нужно ');
        return `${n.target.join(' ')} [${msg}]`;
      });
      return `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${nodes.join(' | ')}`;
    });
}

const FORM_SECTIONS = ['dns', 'inbounds', 'policy'] as const;

for (const theme of ['dark', 'light'] as const) {
  test(`sections axe: формы Xray, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    test.skip(!(await xrayView(page)).installed, 'Xray не установлен');

    await openCores(page);
    for (const id of FORM_SECTIONS) await expandSection(page, id);
    for (const id of FORM_SECTIONS) {
      await expect(page.getByTestId(`section-form-xray-${id}`)).toBeVisible({ timeout: T.action });
    }

    const problems: string[] = [];
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
    const scopes = FORM_SECTIONS.map((id) => `[data-testid="section-card-xray-${id}"]`);
    for (const v of await axeSevere(page, scopes)) problems.push(`axe ${v}`);
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

for (const theme of ['dark', 'light'] as const) {
  test(`cores: 390 px формы Xray, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 390, height: 844 });
    await startPage(page);
    test.skip(!(await xrayView(page)).installed, 'Xray не установлен');

    await openCores(page);
    for (const id of FORM_SECTIONS) await expandSection(page, id);
    await expect(page.getByTestId('section-form-xray-inbounds')).toBeVisible({
      timeout: T.action
    });
    await page.waitForTimeout(500);

    const pageScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(pageScroll, 'горизонтальная прокрутка страницы с формами').toBeLessThanOrEqual(0);

    // кнопки строк и полей не ниже 44 px и в пределах экрана
    const buttons = page.locator(
      [
        '[data-testid^="section-field-xray-inbounds-remove-"]',
        '[data-testid="section-field-xray-inbounds-add"]',
        '[data-testid^="section-field-xray-dns-servers-remove-"]',
        '[data-testid="section-field-xray-dns-servers-add"]'
      ].join(', ')
    );
    for (let i = 0; i < (await buttons.count()); i++) {
      const box = await buttons.nth(i).boundingBox();
      expect(box, 'кнопка формы видна').not.toBeNull();
      expect(box!.height, 'высота кнопки формы').toBeGreaterThanOrEqual(44);
      expect(box!.x + box!.width, 'кнопка формы в пределах экрана').toBeLessThanOrEqual(390);
    }

    await guard.assertClean();
  });
}
