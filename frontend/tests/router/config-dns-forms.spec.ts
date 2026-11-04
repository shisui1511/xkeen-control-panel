// e2e-pages: config/cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { api, layerFlag, randomPort, resetDraft, withPanel } from './lib/nodes-api';

// Поля DNS через прокси формы DNS Mihomo на настоящей панели (фаза 148, DNSF-01):
// порядок полей, проверка адреса слушателя и диапазона fake-ip, запись верных значений в
// черновик, строки списков proxy-server-nameserver и fake-ip-filter, доступность в обеих
// темах и вёрстка на 390 px. Ответы сети не подменяются. Правки идут только в черновик и
// сбрасываются после каждого теста; «Применить» не вызывается, профиль стенда не меняется.
// Флаг слоя включается на время прогона и возвращается прежним.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

test.afterEach(async ({ browser }) => {
  await withPanel(browser, resetDraft);
});

const SEC = 'dns';
const FIELD = (path: string) => `section-field-mihomo-${SEC}-${path}`;

/** Списки формы, которые добавила фаза: путь ключа и тестовое значение строки. */
const LISTS = [
  { path: 'proxy-server-nameserver', value: 'https://1.1.1.1/dns-query' },
  { path: 'fake-ip-filter', value: '+.xcp-dns-forms.example.test' }
] as const;

interface DnsSection {
  id: string;
  owner: string;
  form: Record<string, unknown> | null;
  advanced: string;
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Mihomo установлен на стенде (вид секций ядра). */
async function mihomoInstalled(page: Page): Promise<boolean> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=mihomo');
  expect(view.status, 'GET /api/configlayer/cores?kernel=mihomo').toBe(200);
  return view.data?.installed === true;
}

/** Секция dns из вида Mihomo по настоящему API. */
async function dnsSection(page: Page): Promise<DnsSection> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=mihomo');
  expect(view.status, 'GET /api/configlayer/cores?kernel=mihomo').toBe(200);
  const sections = (view.data?.sections ?? []) as DnsSection[];
  const sec = sections.find((s) => s.id === SEC);
  expect(sec, 'секция dns в виде Mihomo').toBeTruthy();
  return sec as DnsSection;
}

/** Ревизия и число изменений черновика (то, что показывает DraftBar). */
async function draftState(page: Page): Promise<{ revision: number; changes: number }> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return {
    revision: Number(st.data?.draft_revision ?? 0),
    changes: Number(st.data?.draft_changes ?? 0)
  };
}

/**
 * Профиль панели в черновике: если маршрутизацией Mihomo не управляет панель, желание
 * «взять» уходит в черновик настоящим маршрутом (файлы меняются только при «Применить»,
 * которого тест не вызывает). Секция dns после этого правится формой.
 */
async function ensureProfileInDraft(page: Page): Promise<void> {
  const view = await api(page, 'GET', '/api/configlayer/cores?kernel=mihomo');
  const owner = ((view.data?.profile ?? {}) as { owner?: string }).owner ?? '';
  if (owner !== 'panel' && owner !== 'pending') {
    const take = await api(page, 'POST', '/api/configlayer/routing/take', {
      revision: (await draftState(page)).revision
    });
    expect(take.status, 'желание «взять маршрутизацию» через API').toBe(200);
  }
  await expect
    .poll(async () => (await dnsSection(page)).owner, { timeout: T.action })
    .toMatch(/^(panel|pending)$/);
}

/** Вкладка ядер, Mihomo, раскрытая карточка секции dns с формой. */
async function openDns(page: Page): Promise<void> {
  await page.goto('/#/config/cores', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  const tab = page.getByTestId('cores-tab');
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
  await page.getByTestId('kernel-switch-mihomo').click();
  await expect(page.getByTestId('cores-panel-mihomo')).toBeVisible({ timeout: T.page });
  const toggle = page.getByTestId(`section-toggle-mihomo-${SEC}`);
  await expect(toggle).toBeVisible({ timeout: T.page });
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByTestId(`section-form-mihomo-${SEC}`)).toBeVisible({ timeout: T.action });
}

/** Подготовка изменяющего теста: стенд с Mihomo, профиль у панели, форма dns открыта. */
async function prepare(page: Page, width = 1440): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  await startPage(page);
  test.skip(!(await mihomoInstalled(page)), 'Mihomo не установлен');
  await resetDraft(page);
  await ensureProfileInDraft(page);
  await openDns(page);
}

/** Строки списка формы (поля ввода). */
function rowsOf(page: Page, path: string) {
  return page.locator(`[data-testid^="${FIELD(path)}-"]`);
}

/** Строки списка секции из вида по API. */
async function formList(page: Page, path: string): Promise<string[] | undefined> {
  const v = (await dnsSection(page)).form?.[path];
  return Array.isArray(v) ? v.map((x) => String(x)) : undefined;
}

test('dns-forms: поля DNSF-01 в порядке UI-SPEC', async ({ page }) => {
  test.setTimeout(T.save * 3);
  const guard = attachConsoleGuard(page);
  await prepare(page);

  // Списки представлены кнопкой «Добавить», скаляры — полем: порядок по документу
  const order = [
    FIELD('enable'),
    FIELD('enhanced-mode'),
    FIELD('ipv6'),
    FIELD('listen'),
    `section-add-mihomo-${SEC}-nameserver`,
    `section-add-mihomo-${SEC}-default-nameserver`,
    `section-add-mihomo-${SEC}-proxy-server-nameserver`,
    FIELD('fake-ip-range'),
    `section-add-mihomo-${SEC}-fake-ip-filter`
  ];
  for (const id of order) await expect(page.getByTestId(id).first()).toBeAttached();
  const sorted = await page.evaluate((ids) => {
    const els = ids.map((id) => document.querySelector(`[data-testid="${id}"]`));
    return els.every(
      (el, i) =>
        i === 0 ||
        (el !== null &&
          els[i - 1] !== null &&
          (els[i - 1] as Element).compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING)
    );
  }, order);
  expect(sorted, `порядок полей: ${order.join(', ')}`).toBe(true);

  // Новые поля идут до «Закреплено» и «Дополнительно»
  const before = await page.evaluate(
    ([last, adv]) => {
      const a = document.querySelector(`[data-testid="${last}"]`);
      const b = document.querySelector(`[data-testid="${adv}"]`);
      return a !== null && b !== null && !!(a.compareDocumentPosition(b) & 4);
    },
    [`section-add-mihomo-${SEC}-fake-ip-filter`, `section-advanced-block-mihomo-${SEC}`]
  );
  expect(before, 'fake-ip-filter стоит перед «Дополнительно»').toBe(true);

  // Ключа нет — у диапазона fake-ip подсказка с умолчанием ядра
  const range = page.getByTestId(FIELD('fake-ip-range'));
  if ((await range.inputValue()) === '') {
    const hintId = (await range.getAttribute('aria-describedby')) ?? '';
    expect(hintId, 'подсказка диапазона связана с полем').not.toBe('');
    await expect(page.locator(`[id="${hintId}"]`)).toContainText('198.18.0.1/16');
  }

  await guard.assertClean();
});

test('dns-forms: неверные адрес слушателя и CIDR не попадают в черновик', async ({ page }) => {
  test.setTimeout(T.save * 3);
  const guard = attachConsoleGuard(page);
  await prepare(page);
  const was = await draftState(page);
  const sectionBefore = await dnsSection(page);

  for (const [path, bad] of [
    ['listen', 'xcp-not-an-address'],
    ['listen', '127.0.0.1:70000'],
    ['fake-ip-range', '198.18.0.1/40'],
    ['fake-ip-range', '300.18.0.1/16']
  ] as const) {
    const field = page.getByTestId(FIELD(path));
    await field.fill(bad);
    await field.blur();
    const error = page.getByTestId(`section-error-mihomo-${SEC}-${path}`);
    await expect(error, `${path}: текст ошибки для ${bad}`).toBeVisible({ timeout: T.action });
    await expect(error).toHaveAttribute('role', 'alert');
    await expect(field).toHaveClass(/input-error/);
    await expect(field).toHaveAttribute('aria-invalid', 'true');
    await expect(field).toHaveAttribute('aria-describedby', (await error.getAttribute('id')) ?? '');
    await expect(field, 'текст остаётся в поле').toHaveValue(bad);
  }

  // Черновик не изменился: ни ревизии, ни счётчика DraftBar, форма секции прежняя
  await page.waitForTimeout(1000);
  expect(await draftState(page), 'черновик после неверного ввода').toEqual(was);
  expect((await dnsSection(page)).form, 'форма секции dns').toEqual(sectionBefore.form);

  await guard.assertClean();
});

test('dns-forms: верные адрес слушателя и диапазон уходят в черновик', async ({ page }) => {
  test.setTimeout(T.save * 3);
  const guard = attachConsoleGuard(page);
  await prepare(page);

  const listen = `127.0.0.1:${randomPort()}`;
  const range = '198.18.0.1/15';
  for (const [path, value] of [
    ['listen', listen],
    ['fake-ip-range', range]
  ] as const) {
    const field = page.getByTestId(FIELD(path));
    await field.fill(value);
    await field.blur();
    await expect(page.getByTestId(`section-error-mihomo-${SEC}-${path}`)).toHaveCount(0);
    await expect
      .poll(async () => (await dnsSection(page)).form?.[path], { timeout: T.action })
      .toBe(value);
  }

  // Ошибка снимается верным значением: неверный ввод, затем исправление
  const field = page.getByTestId(FIELD('listen'));
  await field.fill('xcp-not-an-address');
  await field.blur();
  await expect(page.getByTestId(`section-error-mihomo-${SEC}-listen`)).toBeVisible();
  await field.fill(':53');
  await field.blur();
  await expect(page.getByTestId(`section-error-mihomo-${SEC}-listen`)).toHaveCount(0);
  await expect
    .poll(async () => (await dnsSection(page)).form?.listen, { timeout: T.action })
    .toBe(':53');

  // Поля формы не дублируются в «Дополнительно»
  const adv = (await dnsSection(page)).advanced;
  expect(adv, '«Дополнительно» без listen и fake-ip-range').not.toMatch(
    /^(listen|fake-ip-range):/m
  );

  await guard.assertClean();
});

test('dns-forms: строки proxy-server-nameserver и fake-ip-filter, включая последнюю', async ({
  page
}) => {
  test.setTimeout(T.save * 4);
  const guard = attachConsoleGuard(page);
  await prepare(page);

  for (const list of LISTS) {
    const rows = rowsOf(page, list.path);
    const start = await rows.count();

    // ноль, одна и много строк: добавляются одинаково
    for (let n = 0; n < 2; n++) {
      await page.getByTestId(`section-add-mihomo-${SEC}-${list.path}`).click();
      const row = page.getByTestId(`${FIELD(list.path)}-${start + n}`);
      await expect(row, `${list.path}: новая строка в фокусе`).toBeFocused();
      await row.fill(`${list.value}${n === 0 ? '' : `#${n}`}`);
      await row.blur();
      // следующая строка — после того, как вид секции вернулся с записанной
      await expect
        .poll(async () => (await formList(page, list.path))?.length ?? 0, { timeout: T.action })
        .toBe(start + n + 1);
      await expect(rows).toHaveCount(start + n + 1);
    }
    expect(await formList(page, list.path)).toContain(list.value);

    // удаляются все строки, последняя — тоже; опустевший список снимает ключ
    for (let left = start + 2; left > 0; left--) {
      await page.getByTestId(`section-remove-mihomo-${SEC}-${list.path}-0`).click();
      await expect(rows).toHaveCount(left - 1, { timeout: T.action });
    }
    await expect(page.getByTestId(`section-list-empty-mihomo-${SEC}-${list.path}`)).toBeVisible();
    await expect.poll(async () => formList(page, list.path), { timeout: T.action }).toBeUndefined();
  }

  await guard.assertClean();
});

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри карточки секции dns. Вывод — правило и селекторы.
 */
async function axeSevere(page: Page): Promise<string[]> {
  // раскрытие карточки идёт с затуханием: axe не должен видеть полупрозрачный кадр
  await page.waitForTimeout(500);
  const scan = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .include(`[data-testid="section-card-mihomo-${SEC}"]`)
    .analyze();
  return scan.violations
    .filter((v) => v.impact === 'critical' || v.impact === 'serious')
    .map(
      (v) =>
        `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${v.nodes
          .slice(0, 3)
          .map((n) => n.target.join(' '))
          .join(' | ')}`
    );
}

/** Строки списков и ошибка поля: состояние формы для axe и вёрстки. */
async function fillStates(page: Page): Promise<void> {
  for (const list of LISTS) {
    await page.getByTestId(`section-add-mihomo-${SEC}-${list.path}`).click();
    const row = rowsOf(page, list.path).last();
    // длинное значение без пробелов: проверка переноса
    const value = `${list.value}${'-long'.repeat(20)}`;
    await row.fill(value);
    await row.blur();
    await expect
      .poll(async () => (await formList(page, list.path)) ?? [], { timeout: T.action })
      .toContain(value);
  }
  const range = page.getByTestId(FIELD('fake-ip-range'));
  await range.fill('198.18.0.1/40');
  await range.blur();
  await expect(page.getByTestId(`section-error-mihomo-${SEC}-fake-ip-range`)).toBeVisible();
}

for (const theme of ['dark', 'light'] as const) {
  test(`dns-forms: dns axe: форма DNS Mihomo, тема ${theme}`, async ({ page }) => {
    test.setTimeout(T.save * 3);
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await prepare(page, 1280);
    await fillStates(page);

    const problems: string[] = [];
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
    for (const v of await axeSevere(page)) problems.push(`axe ${v}`);
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

test('dns-forms: 390 px — без горизонтальной прокрутки, цели нажатия от 44 px', async ({
  page
}) => {
  test.setTimeout(T.save * 3);
  const guard = attachConsoleGuard(page);
  await prepare(page, 390);
  await fillStates(page);

  const pageScroll = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

  const boxes = await page
    .locator(`[data-testid="section-form-mihomo-${SEC}"] :is(input, select, button)`)
    .evaluateAll((els) =>
      els
        .filter((e) => (e as HTMLElement).offsetParent !== null)
        .map((e) => {
          const r = e.getBoundingClientRect();
          return {
            id: e.getAttribute('data-testid') ?? e.tagName,
            right: r.right,
            height: r.height,
            target:
              (e.tagName === 'BUTTON' && e.getAttribute('type') === 'button') ||
              (e.tagName === 'INPUT' && (e as HTMLInputElement).type === 'text')
          };
        })
    );
  for (const b of boxes) {
    expect(b.right, `${b.id} в пределах экрана`).toBeLessThanOrEqual(390);
    if (b.target) expect(b.height, `${b.id}: высота цели нажатия`).toBeGreaterThanOrEqual(44);
  }

  // ошибка поля и подсказки переносятся, а не выходят за край
  const msgRight = await page
    .locator(`[data-testid="section-form-mihomo-${SEC}"] :is(.error-text, .mf-hint)`)
    .evaluateAll((els) => Math.max(0, ...els.map((e) => e.getBoundingClientRect().right)));
  expect(msgRight, 'ошибки и подсказки в пределах экрана').toBeLessThanOrEqual(390);

  await guard.assertClean();
});
