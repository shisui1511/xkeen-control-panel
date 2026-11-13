// e2e-pages: config/cores
// router-area: cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { addToDraft, api, layerFlag, nodeLink, randomPort, resetDraft } from './lib/nodes-api';

// Формы секций Mihomo на настоящей панели (фаза 147, план 23): «Основное», DNS,
// «Входящие/TUN» и Sniffer правят профиль панели через общие блоки форм, ключи вне формы
// сохраняются, секции чужого профиля читаются только для чтения. Ответы сети не
// подменяются. Флаг слоя включается на время прогона и возвращается прежним. Правки идут
// только в черновик и сбрасываются; «Применить» не вызывается, профиль стенда не меняется.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

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

interface MihomoSection {
  id: string;
  owner: string;
  value: unknown;
  form: unknown;
  advanced: string;
}

interface MihomoView {
  installed: boolean;
  profileOwner: string;
  sections: MihomoSection[];
}

/** Вид секций Mihomo по настоящему API. */
async function mihomoView(page: Page): Promise<MihomoView> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=mihomo');
  expect(view.status, 'GET /api/configlayer/cores?kernel=mihomo').toBe(200);
  const profile = (view.data?.profile ?? {}) as { owner?: string };
  return {
    installed: view.data?.installed === true,
    profileOwner: profile.owner ?? '',
    sections: (view.data?.sections ?? []) as MihomoSection[]
  };
}

function sectionOf(view: MihomoView, id: string): MihomoSection {
  const sec = view.sections.find((s) => s.id === id);
  expect(sec, `секция ${id} в виде Mihomo`).toBeTruthy();
  return sec as MihomoSection;
}

/** Ревизия черновика из снимка слоя. */
async function draftRevision(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_revision ?? 0);
}

/**
 * Профиль панели в черновике: если маршрутизацией Mihomo не управляет панель и «взять»
 * ещё не просили, желание «взять» уходит в черновик настоящим маршрутом (файлы меняются
 * только при «Применить», которого тест не вызывает).
 */
async function ensureProfileInDraft(page: Page): Promise<MihomoView> {
  let view = await mihomoView(page);
  if (view.profileOwner !== 'panel' && view.profileOwner !== 'pending') {
    const take = await api(page, 'POST', '/api/configlayer/routing/take', {
      revision: await draftRevision(page)
    });
    expect(take.status, 'желание «взять маршрутизацию» через API').toBe(200);
    view = await mihomoView(page);
  }
  expect(['panel', 'pending'], 'профиль Mihomo у панели или ждёт «Применить»').toContain(
    view.profileOwner
  );
  return view;
}

/** Открывает вкладку ядер и выбирает Mihomo. */
async function openCores(page: Page): Promise<void> {
  await openConfig(page, '#/config/cores');
  await tabReady(page, 'cores-tab');
  await page.getByTestId('kernel-switch-mihomo').click();
  await expect(page.getByTestId('cores-panel-mihomo')).toBeVisible({ timeout: T.page });
}

/** Раскрывает карточку секции Mihomo на открытой вкладке ядер. */
async function expandSection(page: Page, id: string): Promise<void> {
  const toggle = page.getByTestId(`section-toggle-mihomo-${id}`);
  await expect(toggle).toBeVisible({ timeout: T.page });
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

/** Открывает вкладку ядер, выбирает Mihomo и раскрывает карточку секции. */
async function openSection(page: Page, id: string): Promise<void> {
  await openCores(page);
  await expandSection(page, id);
}

/** Раскрывает «Дополнительно» секции и возвращает его поле. */
async function openAdvanced(page: Page, id: string) {
  const block = page.getByTestId(`section-advanced-block-mihomo-${id}`);
  await expect(block).toBeVisible({ timeout: T.action });
  if ((await block.getAttribute('open')) === null) await block.locator('summary').click();
  const area = page.getByTestId(`section-advanced-mihomo-${id}`);
  await expect(area).toBeVisible({ timeout: T.action });
  return area;
}

test('cores: Mihomo — правка формы сохраняет ключи вне формы', async ({ page }) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await mihomoView(page);
  test.skip(!before.installed, 'Mihomo не установлен');
  await resetDraft(page);

  try {
    await ensureProfileInDraft(page);
    await openSection(page, 'main');
    await expect(page.getByTestId('section-owner-mihomo-main')).toHaveAttribute(
      'data-owner',
      /^(pending|panel)$/,
      { timeout: T.action }
    );

    // подключ profile вне формы: уходит в черновик из «Дополнительно» (D-26)
    const area = await openAdvanced(page, 'main');
    const current = await area.inputValue();
    const revision = await draftRevision(page);
    const withProfile = /^profile:/m.test(current)
      ? /^\s+store-fake-ip:/m.test(current)
        ? current
        : current.replace(/^profile:[^\S\n]*$/m, 'profile:\n  store-fake-ip: true')
      : `${current.trimEnd()}\nprofile:\n  store-fake-ip: true\n`.trimStart();
    await area.fill(withProfile);
    await area.blur();
    await expect(page.getByTestId('section-raw-error-mihomo-main')).toHaveCount(0);
    if (withProfile !== current) {
      await expect.poll(() => draftRevision(page), { timeout: T.action }).toBeGreaterThan(revision);
    }

    // порт формой: после записи в черновик остальные ключи секции на месте
    const port = randomPort();
    const field = page.getByTestId('section-field-mihomo-main-mixed-port');
    await expect(field).toBeVisible({ timeout: T.action });
    await expect(field).not.toHaveAttribute('readonly', /.*/);
    await field.fill(String(port));
    await field.blur();
    await expect
      .poll(
        async () => {
          const value = sectionOf(await mihomoView(page), 'main').value as {
            'mixed-port'?: unknown;
            profile?: Record<string, unknown>;
          } | null;
          return (
            Number(value?.['mixed-port']) === port && value?.profile?.['store-fake-ip'] === true
          );
        },
        { timeout: T.action }
      )
      .toBe(true);

    // закреплённое показано текстом, полей для него нет
    await expect(page.getByTestId('section-pinned-mihomo-main')).toContainText('routing-mark');

    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

test('cores: Mihomo — секции чужого профиля только для чтения', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const view = await mihomoView(page);
  test.skip(!view.installed, 'Mihomo не установлен');
  await resetDraft(page);

  const foreign = view.profileOwner === 'xkeen' || view.profileOwner === 'user';
  const dns = sectionOf(view, 'dns').value as Record<string, unknown> | null;

  await openCores(page);
  for (const id of ['main', 'dns', 'sniffer']) await expandSection(page, id);
  const revision = await draftRevision(page);

  if (foreign) {
    // строка владельца: секция принадлежит профилю, кнопки взятия у секции нет (D-03)
    for (const id of ['main', 'dns', 'sniffer']) {
      await expect(page.getByTestId(`section-profile-note-mihomo-${id}`)).toContainText(
        /Секция принадлежит профилю|section belongs to the profile/
      );
      await expect(page.getByTestId(`section-take-mihomo-${id}`)).toHaveCount(0);
    }

    // поля только для чтения, значения — действующие значения профиля
    const port = page.getByTestId('section-field-mihomo-main-mixed-port');
    await expect(port).toHaveAttribute('aria-readonly', 'true');
    await expect(port).toHaveAttribute('readonly', /.*/);
    await expect(page.getByTestId('section-field-mihomo-main-mode')).toHaveAttribute(
      'aria-readonly',
      'true'
    );

    const enable = page.getByTestId('section-field-mihomo-dns-enable');
    await expect(enable).toHaveAttribute('aria-readonly', 'true');
    await expect(enable).toHaveValue(
      dns?.enable === true ? /^(Включено|On)$/ : /^(Выключено|Off)$/
    );
    const enhanced = page.getByTestId('section-field-mihomo-dns-enhanced-mode');
    await expect(enhanced).toHaveAttribute('aria-readonly', 'true');
    await expect(enhanced).toHaveValue(String(dns?.['enhanced-mode'] ?? ''));
    await expect(page.getByTestId('section-field-mihomo-sniffer-enable')).toHaveAttribute(
      'aria-readonly',
      'true'
    );

    // ввод с клавиатуры ничего не меняет
    const text = await port.inputValue();
    await port.click();
    await page.keyboard.type('77');
    await page.keyboard.press('Tab');
    await expect(port).toHaveValue(text);
  } else if (view.profileOwner === 'panel' || view.profileOwner === 'pending') {
    // профиль у панели или ждёт «Применить»: те же поля доступны для правки
    await expect(page.getByTestId('section-field-mihomo-main-mixed-port')).not.toHaveAttribute(
      'readonly',
      /.*/
    );
    await expect(page.getByTestId('section-field-mihomo-dns-enable')).toBeEnabled();
    await expect(page.getByTestId('section-field-mihomo-sniffer-enable')).toBeEnabled();
  }
  expect(await draftRevision(page), 'просмотр секций не пишет в черновик').toBe(revision);

  await guard.assertClean();
});

interface ListenerRow {
  name?: string;
  type?: string;
  port?: number;
}

/** Listeners и tun секции «Входящие/TUN» по виду настоящего API. */
function inboundsOf(view: MihomoView): { listeners: ListenerRow[]; stack: string } {
  const value = (sectionOf(view, 'inbounds').value ?? {}) as {
    listeners?: ListenerRow[];
    tun?: { stack?: string };
  };
  return { listeners: value.listeners ?? [], stack: value.tun?.stack ?? '' };
}

test('cores: Mihomo — listener force-proxy переживает правку формы', async ({ page }) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const before = await mihomoView(page);
  test.skip(!before.installed, 'Mihomo не установлен');
  await resetDraft(page);

  try {
    // узел теста в черновике: без узлов группы по умолчанию пусты и «Применить» недоступно
    // (group_empty) — проверка окна изменений не зависит от источников стенда
    await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), 'RtListenerA'));
    await ensureProfileInDraft(page);
    await openSection(page, 'inbounds');

    // listener force-proxy: если профиль его ещё не знает, он добавляется в «Дополнительно»
    // (тег создаёт сам тест, поэтому блок группы виден на любом стенде)
    let name = inboundsOf(await mihomoView(page)).listeners.find((l) =>
      (l.name ?? '').startsWith('force-proxy')
    )?.name;
    let port = 0;
    if (name === undefined) {
      name = 'force-proxy';
      port = randomPort();
      const area = await openAdvanced(page, 'inbounds');
      const current = await area.inputValue();
      const entry = `  ${name}:\n    type: redir\n    port: ${port}`;
      const next = /^listeners:[^\S\n]*$/m.test(current)
        ? current.replace(/^listeners:[^\S\n]*$/m, `listeners:\n${entry}`)
        : `${current.trimEnd()}\nlisteners:\n${entry}\n`.trimStart();
      await area.fill(next);
      await area.blur();
      await expect(page.getByTestId('section-raw-error-mihomo-inbounds')).toHaveCount(0);
    }
    const names = page.locator('[data-testid^="section-listener-name-mihomo-"]');
    const nameValues = async (): Promise<string[]> =>
      names.evaluateAll((els) => els.map((e) => (e as HTMLInputElement).value));
    await expect.poll(nameValues, { timeout: T.action }).toContain(name);
    if (port === 0) {
      port = Number(
        inboundsOf(await mihomoView(page)).listeners.find((l) => l.name === name)?.port ?? 0
      );
    }

    // группа для входящих force-proxy: блок один, виден пояснение про xkeen -sb
    const block = page.getByTestId('section-force-proxy-mihomo');
    await expect(block).toHaveCount(1, { timeout: T.action });
    await expect(block).toBeVisible();
    await expect(block).toContainText(/speed balancer/);

    // правка TUN: строка force-proxy сохраняет имя и порт
    const stackSelect = page.getByTestId('section-field-mihomo-inbounds-tun-stack');
    await expect(stackSelect).toBeVisible({ timeout: T.action });
    const stacks = ['system', 'gvisor', 'mixed'];
    const current = await stackSelect.inputValue();
    const wanted = stacks.find((s) => s !== current) ?? 'gvisor';
    await stackSelect.selectOption(wanted);
    await expect
      .poll(async () => inboundsOf(await mihomoView(page)).stack, { timeout: T.action })
      .toBe(wanted);

    const after = inboundsOf(await mihomoView(page));
    const kept = after.listeners.filter((l) => l.name === name);
    expect(kept, 'listener force-proxy после правки tun').toHaveLength(1);
    expect(kept[0].port, 'порт listener не изменился').toBe(port);
    await expect.poll(nameValues, { timeout: T.action }).toContain(name);

    // смена tun объявляет полный перезапуск (D-16) и тогда, когда профиль взят в этом же
    // черновике: взятие сравнивается с активным профилем (G-147-3)
    await expect(page.getByTestId('section-restart-note-mihomo-inbounds')).toBeVisible({
      timeout: T.action
    });
    await page.getByTestId('config-apply').click();
    const modal = page.getByTestId('apply-preview-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(
      modal.locator('[data-testid="draft-restart-note"][data-kernel="mihomo"]')
    ).toHaveAttribute('data-reason', 'tun', { timeout: T.action });
    await page.getByTestId('apply-preview-cancel').click();
    await expect(modal).toHaveCount(0);

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

const FORM_SECTIONS = ['main', 'dns', 'inbounds', 'sniffer'];

for (const theme of ['dark', 'light'] as const) {
  test(`sections axe: формы Mihomo, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    test.skip(!(await mihomoView(page)).installed, 'Mihomo не установлен');

    await openCores(page);
    for (const id of FORM_SECTIONS) await expandSection(page, id);
    for (const id of FORM_SECTIONS) {
      await expect(page.getByTestId(`section-form-mihomo-${id}`)).toBeVisible({
        timeout: T.action
      });
    }

    const problems: string[] = [];
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
    const scopes = FORM_SECTIONS.map((id) => `[data-testid="section-card-mihomo-${id}"]`);
    for (const v of await axeSevere(page, scopes)) problems.push(`axe ${v}`);
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

for (const theme of ['dark', 'light'] as const) {
  test(`cores: 390 px формы Mihomo, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 390, height: 844 });
    await startPage(page);
    test.skip(!(await mihomoView(page)).installed, 'Mihomo не установлен');

    await openCores(page);
    for (const id of FORM_SECTIONS) await expandSection(page, id);
    for (const id of FORM_SECTIONS) {
      await expect(page.getByTestId(`section-form-mihomo-${id}`)).toBeVisible({
        timeout: T.action
      });
    }

    const pageScroll = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

    // поля и кнопки умещаются в экран, кнопки строк не ниже 44 px
    const boxes = await page
      .locator('[data-testid^="section-form-mihomo-"] :is(input, select, textarea, button)')
      .evaluateAll((els) =>
        els
          .filter((e) => (e as HTMLElement).offsetParent !== null)
          .map((e) => {
            const r = e.getBoundingClientRect();
            return {
              id: e.getAttribute('data-testid') ?? e.tagName,
              right: r.right,
              height: r.height,
              button: e.tagName === 'BUTTON' && e.getAttribute('type') === 'button'
            };
          })
      );
    for (const b of boxes) {
      expect(b.right, `${b.id} в пределах экрана`).toBeLessThanOrEqual(390);
      if (b.button) expect(b.height, `${b.id}: высота кнопки`).toBeGreaterThanOrEqual(44);
    }

    await guard.assertClean();
  });
}
