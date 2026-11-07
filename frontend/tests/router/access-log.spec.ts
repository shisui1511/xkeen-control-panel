// e2e-pages: logs
// router-area: access-log
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { CORE_TAG, SLOW_ANNOTATION } from './lib/known';
import { api, layerFlag, resetDraft, withPanel } from './lib/nodes-api';

// Блок access-лога Xray на «Логах» (фаза 147.1, план 20): секция у панели включается и выключается
// кнопками с подтверждением и честным исходом, чужая секция берётся диалогом одним шагом (при
// открытом черновике диалог блокирует взятие), путь вне разрешённых объясняется и ведёт к секции,
// доступность в обеих темах и 390 px. Ответы сети не подменяются. Флаг слоя включается на время
// прогона и возвращается прежним; секция «Лог» берётся через API и в afterAll возвращается:
// файлы секции сверяются с исходными побайтно. Ядро не запускается и не останавливается.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

const XRAY_DIR = '/opt/etc/xray/configs';
const DENIED_PATH = '/opt/etc/xray/rt.log';

const RE_ENABLE = /^(Включить access-лог|Turn on access log)$/;
const RE_DISABLE = /^(Выключить access-лог|Turn off access log)$/;
const RE_RESTART = /перезапустится|will restart/i;
const RE_STOPPED = /Вступит в силу при запуске|Takes effect when Xray starts/i;
const RE_TOAST_ON = /Access-лог Xray включён|Xray access log is on/;
const RE_TOAST_OFF = /Access-лог Xray выключен|Xray access log is off/;

interface LogBefore {
  installed: boolean;
  blocked: string;
  owner: string;
  files: Record<string, string | null>;
}

let before: LogBefore = { installed: false, blocked: '', owner: '', files: {} };

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

function configPath(name: string): string {
  return name.startsWith('/') ? name : `${XRAY_DIR}/${name}`;
}

/** Содержимое файла конфига; null, если файла нет. */
async function readConfig(page: Page, path: string): Promise<string | null> {
  const res = await rawRequest(page, 'GET', `/api/config/read?path=${encodeURIComponent(path)}`);
  if (res.status === 404) return null;
  expect(res.status, `чтение ${path}`).toBe(200);
  return res.text;
}

/** Записывает файл конфига; нет файла — сначала создаёт; null — убирает файл. */
async function putConfig(page: Page, path: string, content: string | null): Promise<void> {
  const q = encodeURIComponent(path);
  if (content === null) {
    await rawRequest(page, 'POST', `/api/config/delete?path=${q}`);
    return;
  }
  if ((await readConfig(page, path)) === null) {
    const created = await rawRequest(page, 'POST', `/api/config/create?path=${q}`);
    expect(created.status, `создание ${path}`).toBe(200);
  }
  const saved = await rawRequest(page, 'POST', `/api/config/save?path=${q}`, content);
  expect(saved.status, `запись ${path}`).toBe(200);
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

interface LogSection {
  installed: boolean;
  owner: string;
  blocked: string;
  files: string[];
  form: Record<string, unknown>;
  advanced: string;
}

async function logSection(page: Page): Promise<LogSection> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=xray');
  expect(view.status, 'GET /api/configlayer/cores?kernel=xray').toBe(200);
  const sections = (view.data?.sections ?? []) as Record<string, unknown>[];
  const s = sections.find((x) => x.id === 'log') ?? {};
  return {
    installed: view.data?.installed === true,
    owner: String(s.owner ?? ''),
    blocked: String(s.blocked ?? ''),
    files: (s.files ?? []) as string[],
    form: (s.form ?? {}) as Record<string, unknown>,
    advanced: String(s.advanced ?? '')
  };
}

async function layerProbe(
  page: Page
): Promise<{ revision: number; running: boolean; changes: number }> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const apply = (st.data?.apply ?? {}) as { running?: boolean };
  return {
    revision: Number(st.data?.draft_revision ?? 0),
    running: apply.running === true,
    changes: Number(st.data?.draft_changes ?? 0)
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

/** Берёт секцию «Лог» в черновик настоящим маршрутом и применяет; секция уже у панели — ничего. */
async function takeLog(page: Page): Promise<void> {
  if ((await logSection(page)).owner === 'panel') return;
  const res = await api(page, 'POST', '/api/configlayer/cores/take', {
    revision: (await layerProbe(page)).revision,
    kernel: 'xray',
    sections: ['log'],
    ack_comments: true
  });
  expect(res.status, 'взятие секции «Лог» через API').toBe(200);
  await applyAndWait(page);
}

/** Возвращает секцию прежнему владельцу и применяет (уборка). */
async function releaseLog(page: Page): Promise<void> {
  if ((await logSection(page)).owner !== 'panel') return;
  const res = await api(page, 'POST', '/api/configlayer/cores/release', {
    revision: (await layerProbe(page)).revision,
    kernel: 'xray',
    section: 'log'
  });
  expect(res.status, 'возврат секции «Лог» через API').toBe(200);
  await applyAndWait(page);
}

/** Возвращает секцию и файлы к состоянию до прогона. */
async function restoreLog(page: Page): Promise<void> {
  await resetDraft(page);
  if (before.owner !== 'panel') await releaseLog(page);
  for (const [path, content] of Object.entries(before.files)) {
    if ((await readConfig(page, path)) !== content) await putConfig(page, path, content);
  }
}

async function isXrayRunning(page: Page): Promise<boolean> {
  const caps = await api(page, 'GET', '/api/capabilities');
  expect(caps.status, 'GET /api/capabilities').toBe(200);
  const running = (caps.data?.running_kernels ?? []) as string[];
  return running.includes('xray');
}

/** Выключает access-лог настоящим маршрутом (исходная точка сценария). */
async function accessOffViaApi(page: Page): Promise<void> {
  const res = await api(page, 'POST', '/api/configlayer/cores/access-log', { enabled: false });
  expect(res.status, 'POST /api/configlayer/cores/access-log {enabled:false}').toBe(200);
}

/** Открывает «Логи» в виде «Xray по устройствам». */
async function openDevices(page: Page): Promise<void> {
  await page.goto('/#/logs', { timeout: T.page, waitUntil: 'domcontentloaded' });
  const seg = page.locator('.seg-item[data-value="xray-devices"]');
  await expect(seg).toBeVisible({ timeout: T.page });
  await seg.click();
  await expect(page.getByTestId('xlog-control')).toBeVisible({ timeout: T.page });
}

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
  await withPanel(browser, async (page) => {
    const s = await logSection(page);
    const files: Record<string, string | null> = {};
    for (const name of s.files) files[configPath(name)] = await readConfig(page, configPath(name));
    files[DENIED_PATH] = await readConfig(page, DENIED_PATH);
    before = { installed: s.installed, blocked: s.blocked, owner: s.owner, files };
  });
});

test.afterAll(async ({ browser }: { browser: Browser }) => {
  await withPanel(browser, restoreLog);
  await layer.restore(browser);
});

// --- Чужая секция и черновик ----------------------------------------------------------------

test('access-log: чужая секция и черновик', { tag: CORE_TAG }, async ({ page }) => {
  test.setTimeout(T.save * 8);
  test.skip(!before.installed, 'Xray не установлен');
  test.skip(before.owner === 'panel', 'секция «Лог» уже у панели');
  test.skip(before.blocked !== '', 'секция «Лог» не берётся');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  await resetDraft(page);

  // открытый черновик: другая секция взята желанием и не применена
  const policy = await api(page, 'POST', '/api/configlayer/cores/take', {
    revision: (await layerProbe(page)).revision,
    kernel: 'xray',
    sections: ['policy'],
    ack_comments: true
  });
  test.skip(policy.status !== 200, 'секция «Политики» не берётся: черновик не открыть');
  expect((await layerProbe(page)).changes, 'черновик открыт').toBeGreaterThan(0);

  await openDevices(page);
  const enableBtn = page.getByTestId('xlog-enable');
  const wantEnable = await enableBtn.isVisible();
  const trigger = wantEnable ? enableBtn : page.getByTestId('xlog-disable');
  await expect(page.getByTestId('xlog-owner')).toBeVisible();
  await trigger.click();

  const modal = page.getByTestId('xlog-take-modal');
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('xlog-draft-block')).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('take-section-confirm')).toBeDisabled();
  await modal.getByTestId('take-section-cancel').click();
  await expect(modal).toHaveCount(0, { timeout: T.action });

  // черновик сброшен: диалог показывает файлы, взятие и включение идут одним шагом
  await resetDraft(page);
  await openDevices(page);
  await page.getByTestId(wantEnable ? 'xlog-enable' : 'xlog-disable').click();
  await expect(modal).toBeVisible({ timeout: T.action });
  await expect(modal.getByTestId('take-section-loading')).toHaveCount(0, { timeout: T.action });
  await expect(modal.getByTestId('xlog-draft-block')).toHaveCount(0);
  await expect(
    modal.getByTestId('take-section-files').or(modal.getByTestId('take-section-nofile'))
  ).toBeVisible({ timeout: T.action });
  const ack = modal.getByTestId('take-section-comments-ack');
  if (await ack.isVisible()) await ack.check();
  await modal.getByTestId('take-section-confirm').click();
  await expect(modal).toHaveCount(0, { timeout: T.save * 2 });
  await expect(
    page.locator('.toast').filter({ hasText: wantEnable ? RE_TOAST_ON : RE_TOAST_OFF })
  ).toBeVisible({ timeout: T.save });
  await expect(page.getByTestId('xlog-owner')).toHaveText(/^(Панель|Panel)$/, {
    timeout: T.action
  });

  await guard.assertClean();
});

// --- Секция у панели -------------------------------------------------------------------------

test(
  'access-log: включение и выключение на секции панели',
  {
    tag: CORE_TAG,
    annotation: {
      type: SLOW_ANNOTATION,
      description:
        'включение и выключение журнала доступа: «Применить» с перезапуском Xray на каждом шаге'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 10);
    test.skip(!before.installed, 'Xray не установлен');
    test.skip(before.blocked !== '' && before.owner !== 'panel', 'секция «Лог» не берётся');
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    await resetDraft(page);
    await takeLog(page);
    await accessOffViaApi(page);
    const running = await isXrayRunning(page);

    await openDevices(page);
    await expect(page.getByTestId('xlog-off')).toBeVisible({ timeout: T.page });
    await expect(page.getByTestId('xlog-will-write')).toContainText('/');
    await expect(page.getByTestId('xlog-owner')).toHaveText(/^(Панель|Panel)$/);

    // включение: подтверждение с путём записи; при запущенном Xray — с перезапуском
    await page.getByTestId('xlog-enable').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: T.action });
    await expect(dialog).toContainText(running ? RE_RESTART : RE_STOPPED);
    await expect(dialog).toContainText('/opt/var/log');
    await dialog.getByRole('button', { name: RE_ENABLE }).click();
    await expect(page.getByTestId('xlog-path')).toBeVisible({ timeout: T.save * 2 });
    await expect(
      page
        .locator('.toast')
        .filter({ hasText: running ? RE_TOAST_ON : RE_STOPPED })
        .last()
    ).toBeVisible({ timeout: T.save });

    // выключение: Xray запущен — подтверждение, остановлен — сразу
    await page.getByTestId('xlog-disable').click();
    if (running) {
      await expect(dialog).toBeVisible({ timeout: T.action });
      await expect(dialog).toContainText(RE_RESTART);
      await dialog.getByRole('button', { name: RE_DISABLE }).click();
    } else {
      await expect(dialog).toHaveCount(0);
    }
    await expect(page.getByTestId('xlog-enable')).toBeVisible({ timeout: T.save * 2 });
    await expect(
      // при остановленном Xray текст тот же, что после включения: прежний тост ещё на экране
      page
        .locator('.toast')
        .filter({ hasText: running ? RE_TOAST_OFF : RE_STOPPED })
        .last()
    ).toBeVisible({ timeout: T.save });

    await guard.assertClean();
  }
);

// --- Путь вне разрешённых --------------------------------------------------------------------

test(
  'access-log: путь вне разрешённых',
  {
    tag: CORE_TAG,
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'серия действий через интерфейс с «Применить» и перезапуском Xray'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 12);
    test.skip(!before.installed, 'Xray не установлен');
    test.skip(before.blocked !== '' && before.owner !== 'panel', 'секция «Лог» не берётся');
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await startPage(page);
    await resetDraft(page);
    await takeLog(page);

    const section = await logSection(page);
    const original = String(section.form.access ?? '');
    const save = async (access: string): Promise<void> => {
      const res = await api(page, 'POST', '/api/configlayer/cores/save', {
        revision: (await layerProbe(page)).revision,
        kernel: 'xray',
        section: 'log',
        form: { ...section.form, access },
        advanced: section.advanced
      });
      expect(res.status, `сохранение секции «Лог» с путём ${access}`).toBe(200);
      await applyAndWait(page);
    };

    try {
      await save(DENIED_PATH);
      await openDevices(page);
      const denied = page.getByTestId('xlog-path-denied');
      await expect(denied).toBeVisible({ timeout: T.page });
      await expect(denied).toContainText(DENIED_PATH);
      await expect(page.getByTestId('xlog-disable')).toBeVisible();
      await expect(denied.getByRole('link')).toHaveAttribute('href', '#/config/cores');

      // форма секции предупреждает без блокировки сохранения
      await denied.getByRole('link').click();
      await expect(page.getByTestId('cores-tab')).toBeVisible({ timeout: T.page });
      await page.getByTestId('kernel-switch-xray').click();
      const toggle = page.getByTestId('section-toggle-xray-log');
      await expect(toggle).toBeVisible({ timeout: T.page });
      if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
      await expect(page.getByTestId('section-field-xray-log-access')).toHaveValue(DENIED_PATH);
      await expect(page.getByTestId('section-field-xray-log-access-denied')).toBeVisible();
      await expect(page.getByTestId('section-field-xray-log-access')).toBeEditable();
    } finally {
      await save(original === '' ? 'none' : original);
      await putConfig(page, DENIED_PATH, before.files[DENIED_PATH] ?? null);
    }

    await guard.assertClean();
  }
);

// --- Доступность и 390 px --------------------------------------------------------------------

/** Критичные и серьёзные нарушения axe (WCAG A и AA, в том числе color-contrast) в области. */
async function axeSevere(page: Page, scopes: string[]): Promise<string[]> {
  await page.waitForTimeout(500);
  let builder = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);
  for (const scope of scopes) builder = builder.include(scope);
  const scan = await builder.analyze();
  return scan.violations
    .filter((v) => v.impact === 'critical' || v.impact === 'serious')
    .map((v) => {
      const nodes = v.nodes
        .slice(0, 3)
        .map((n) => `${n.target.join(' ')} [${n.any[0]?.message ?? ''}]`);
      return `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${nodes.join(' | ')}`;
    });
}

async function openThemed(page: Page, theme: 'dark' | 'light', width: number): Promise<void> {
  await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
  await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
  await startPage(page);
  const probe = await logSection(page);
  test.skip(!probe.installed, 'Xray не установлен');
  await openDevices(page);
  expect(
    await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
    'применена тема'
  ).toBe(theme);
}

for (const theme of ['dark', 'light'] as const) {
  test(`access-log axe: тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await openThemed(page, theme, 1280);

    const problems = (await axeSevere(page, ['[data-testid="xlog-control"]'])).map(
      (v) => `axe ${v}`
    );
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

test('access-log: 390 px — без горизонтальной прокрутки, цели не меньше 44 px', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await openThemed(page, 'dark', 390);

  const pageScroll = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

  const control = page.getByTestId('xlog-control');
  const buttons = control.getByRole('button');
  const count = await buttons.count();
  expect(count, 'в блоке есть кнопка действия').toBeGreaterThan(0);
  for (let i = 0; i < count; i++) {
    const box = await buttons.nth(i).boundingBox();
    expect(box, `кнопка ${i} видна`).not.toBeNull();
    expect(box!.height, `высота кнопки ${i}`).toBeGreaterThanOrEqual(44);
    expect(box!.x + box!.width, `кнопка ${i} в пределах экрана`).toBeLessThanOrEqual(390);
  }

  await guard.assertClean();
});
