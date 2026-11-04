// e2e-pages: config/cores
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { CORE_TAG } from './lib/known';
import {
  addToDraft,
  api,
  layerFlag,
  nodeLink,
  randomPort,
  resetDraft,
  withPanel
} from './lib/nodes-api';

// Карточка «Перехват DNS роутера» на настоящей панели (фаза 148, DNSF-02, D-02): одна копия
// перед карточкой DNS выбранного ядра, состояние и владелец порта 53 из API службы,
// блокировка «открыт черновик» с пояснением у кнопки, вёрстка на 390 px и доступность в
// обеих темах. Ответы сети не подменяются. Тесты `dns-card:` перехват не включают: правки
// идут только в черновик и сбрасываются после каждого теста. Тесты `dns-flow:` (148-13)
// включают перехват на активном ядре, проверяют резолв, выключают перехват и ломают пробу
// группой из одного узла без слушателя; маршрутизация, DNS-секция ядра и параметры
// перехвата возвращаются в afterAll. Флаг слоя включается на время прогона и возвращается
// прежним.

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

const KERNELS = ['xray', 'mihomo'] as const;
type Kernel = (typeof KERNELS)[number];

// Строка состояния «выключен» в обоих языках панели
const STATUS_FIRMWARE = /DNS роутера отвечает прошивка\.|Router DNS is served by the firmware\./;

interface DnsState {
  state: string;
  owner: { kind: string };
  blocks: string[];
}

/** Состояние перехвата по настоящему API службы. */
async function dnsState(page: Page): Promise<DnsState> {
  const res = await api(page, 'GET', '/api/configlayer/dns');
  expect(res.status, 'GET /api/configlayer/dns').toBe(200);
  const d = res.data ?? {};
  return {
    state: String(d.state ?? ''),
    owner: (d.owner ?? { kind: '' }) as { kind: string },
    blocks: (d.blocks ?? []) as string[]
  };
}

/** Установленные ядра по виду секций. */
async function installed(page: Page): Promise<Record<Kernel, boolean>> {
  const out = { xray: false, mihomo: false };
  for (const k of KERNELS) {
    const view = await api(page, 'GET', `/api/configlayer/cores?kernel=${k}`);
    expect(view.status, `GET /api/configlayer/cores?kernel=${k}`).toBe(200);
    out[k] = view.data?.installed === true;
  }
  return out;
}

/** Страница, на которой API панели доступно из браузера (сессия и токен CSRF). */
async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

/** Открывает вкладку «Xray │ Mihomo» и выбирает ядро; карточка перехвата загрузилась. */
async function openCores(page: Page, kernel: Kernel): Promise<void> {
  await page.goto('/#/config/cores', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('cores-tab')).toBeVisible({ timeout: T.page });
  await page.getByTestId(`kernel-switch-${kernel}`).click();
  await expect(page.getByTestId(`cores-panel-${kernel}`)).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId('dns-state')).toBeVisible({ timeout: T.page });
}

/** testId элемента, который идёт в DOM сразу за карточкой перехвата. */
async function nextAfterCard(page: Page): Promise<string> {
  return page
    .getByTestId('dns-takeover-card')
    .evaluate((el) => el.nextElementSibling?.getAttribute('data-testid') ?? '');
}

/** Ревизия черновика из снимка слоя. */
async function draftRevision(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_revision ?? 0);
}

test('dns-card: одна карточка перед секцией DNS выбранного ядра', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const inst = await installed(page);
  const kernels = KERNELS.filter((k) => inst[k]);
  test.skip(kernels.length === 0, 'ядра не установлены');

  for (const k of kernels) {
    await openCores(page, k);
    const card = page.getByTestId('dns-takeover-card');
    await expect(card, `${k}: одна копия карточки в DOM`).toHaveCount(1);
    await expect(page.getByTestId(`cores-panel-${k}`).getByTestId('dns-takeover-card')).toHaveCount(
      1
    );
    expect(await nextAfterCard(page), `${k}: карточка стоит перед секцией DNS`).toBe(
      `section-card-${k}-dns`
    );
    await expect(card.getByRole('heading', { level: 3 })).toBeVisible();
  }

  // Переключение ядра туда и обратно: карточка остаётся одна и снова на месте
  if (kernels.length === 2) {
    await page.getByTestId('kernel-switch-xray').click();
    await expect(page.getByTestId('cores-panel-xray')).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('dns-takeover-card')).toHaveCount(1);
    expect(await nextAfterCard(page)).toBe('section-card-xray-dns');
    await expect(page.getByTestId('dns-state')).toBeVisible({ timeout: T.action });
  }

  await guard.assertClean();
});

test('dns-card: выключенный перехват — DNS у прошивки', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const inst = await installed(page);
  const kernel = KERNELS.find((k) => inst[k]);
  test.skip(kernel === undefined, 'ядра не установлены');
  const st = await dnsState(page);
  test.skip(st.state !== 'off', `перехват в состоянии ${st.state}`);

  await openCores(page, kernel as Kernel);
  const state = page.getByTestId('dns-state');
  await expect(state).toHaveAttribute('data-state', 'off');
  await expect(state).toHaveAttribute('role', 'status');
  await expect(state).toHaveText(STATUS_FIRMWARE);
  await expect(page.getByTestId('dns-enable')).toBeVisible();
  await expect(page.getByTestId('dns-disable')).toHaveCount(0);
  // в карточке не больше одной основной кнопки
  await expect(page.getByTestId('dns-takeover-card').locator('.btn-primary')).toHaveCount(1);
  if (st.owner.kind === 'firmware') {
    await expect(page.getByTestId('dns-row-owner')).toContainText('dns-proxy');
  }
  await expect(page.getByTestId('dns-row-last-check')).toBeVisible();

  await guard.assertClean();
});

test('dns-card: открытый черновик блокирует включение', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await startPage(page);
  const inst = await installed(page);
  const kernel = KERNELS.find((k) => inst[k]);
  test.skip(kernel === undefined, 'ядра не установлены');
  const st = await dnsState(page);
  test.skip(st.state !== 'off', `перехват в состоянии ${st.state}`);

  await openCores(page, kernel as Kernel);
  await expect(page.getByTestId('dns-block-draft')).toHaveCount(0);

  // Правка параметров перехвата открывает черновик (секция dns_takeover)
  const edit = await api(page, 'POST', '/api/configlayer/draft', {
    revision: await draftRevision(page),
    section: 'dns_takeover',
    value: { group: '', zones: ['xcp-e2e-dns-card.lan'] }
  });
  expect(edit.status, 'правка черновика').toBe(200);

  const block = page.getByTestId('dns-block-draft');
  await expect(block).toBeVisible({ timeout: T.action });
  const enable = page.getByTestId('dns-enable');
  await expect(enable).toBeDisabled();
  const blockId = await block.getAttribute('id');
  expect(blockId, 'у строки блокировки есть id').toBeTruthy();
  const describedBy = (await enable.getAttribute('aria-describedby')) ?? '';
  expect(describedBy.split(' '), 'кнопка связана со строкой блокировки').toContain(blockId);

  // Сброс черновика снимает блокировку
  await resetDraft(page);
  await expect(page.getByTestId('dns-block-draft')).toHaveCount(0, { timeout: T.action });

  await guard.assertClean();
});

test('dns-card: 390 px без горизонтальной прокрутки, цели не меньше 44 px', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await startPage(page);
  const inst = await installed(page);
  const kernel = KERNELS.find((k) => inst[k]);
  test.skip(kernel === undefined, 'ядра не установлены');

  await openCores(page, kernel as Kernel);
  const card = page.getByTestId('dns-takeover-card');
  await card.scrollIntoViewIfNeeded();

  const pageScroll = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

  const cardBox = await card.boundingBox();
  expect(cardBox, 'карточка видна').toBeTruthy();
  expect(
    (cardBox?.x ?? 0) + (cardBox?.width ?? 0),
    'карточка в пределах экрана'
  ).toBeLessThanOrEqual(390);

  const boxes = await card.locator(':is(button, a)').evaluateAll((els) =>
    els
      .filter((e) => (e as HTMLElement).offsetParent !== null)
      .map((e) => {
        const r = e.getBoundingClientRect();
        return {
          id: e.getAttribute('data-testid') ?? e.textContent?.trim() ?? e.tagName,
          right: r.right,
          height: r.height
        };
      })
  );
  for (const b of boxes) {
    expect(b.right, `${b.id} в пределах экрана`).toBeLessThanOrEqual(390);
    expect(b.height, `${b.id}: высота цели нажатия`).toBeGreaterThanOrEqual(44);
  }

  // Строки сетки на узком экране — в одну колонку: значение под подписью
  const row = page.getByTestId('dns-row-owner');
  const [dt, dd] = await Promise.all([
    row.locator('dt').boundingBox(),
    row.locator('dd').boundingBox()
  ]);
  expect(dt && dd && dd.y >= dt.y + dt.height - 1, 'значение под подписью').toBeTruthy();

  await guard.assertClean();
});

/**
 * Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA, в том числе color-contrast)
 * внутри карточки. Вывод — правило, селекторы и числа контраста.
 */
async function axeSevere(page: Page, scope: string): Promise<string[]> {
  await page.waitForTimeout(500);
  const scan = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .include(scope)
    .analyze();
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

for (const theme of ['dark', 'light'] as const) {
  test(`dns axe: карточка перехвата, тема ${theme}`, async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await startPage(page);
    const inst = await installed(page);
    const kernel = KERNELS.find((k) => inst[k]);
    test.skip(kernel === undefined, 'ядра не установлены');

    await openCores(page, kernel as Kernel);
    // Черновик открыт: блокировки (--warning) попадают в проверку контраста
    const st = await dnsState(page);
    if (st.state === 'off') {
      const edit = await api(page, 'POST', '/api/configlayer/draft', {
        revision: await draftRevision(page),
        section: 'dns_takeover',
        value: { group: '', zones: ['xcp-e2e-dns-axe.lan'] }
      });
      expect(edit.status, 'правка черновика').toBe(200);
      await expect(page.getByTestId('dns-block-draft')).toBeVisible({ timeout: T.action });
    }

    const problems: string[] = [];
    const applied = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
    for (const v of await axeSevere(page, '[data-testid="dns-takeover-card"]')) {
      problems.push(`axe ${v}`);
    }
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

// --- Включение, проверка резолва, выключение, сбой пробы (148-13) --------------------------
//
// Перехват ведёт активное ядро, поэтому тесты помечены тегом ядра. Подготовка настоящими
// маршрутами: маршрутизация и DNS-секция активного ядра у панели (группа «Прокси» с узлами
// подписки стенда — цель DNS-запросов по умолчанию), черновик применён. Сбой пробы
// воспроизводится группой из одного узла без слушателя; её состояние (сбой и откат) остаётся
// для проверок доступности и вёрстки, параметры перехвата, группа, узел, секция и
// маршрутизация возвращаются в afterAll.

const RE_RESOLVED = /^(Разрешилось|Resolved)$/;
const RE_DISABLE_BTN = /^(Выключить перехват|Disable interception)$/;
const RE_TOAST_DISABLED = /Перехват DNS выключен\.|DNS interception disabled\./;
const RE_PROBE_EXTERNAL = /не разрешилось через группу|did not resolve through the/;
const RE_ROLLED_BACK =
  /возвращены к состоянию до включения|returned to their state before enabling/;

interface DnsParamsValue {
  group: string;
  zones: string[];
}

interface DnsFull {
  state: string;
  blocks: string[];
  owner: { kind: string; kernel?: string };
  default_name: string;
  zones_builtin: string[];
  group?: { id: string; name: string; missing: boolean };
  failure?: { code: string; rolled_back: boolean };
  last_check?: { ok: boolean; source: string };
  draft_params?: DnsParamsValue;
}

/** Полный вид перехвата по настоящему API службы. */
async function dnsFull(page: Page): Promise<DnsFull> {
  const res = await api(page, 'GET', '/api/configlayer/dns');
  expect(res.status, 'GET /api/configlayer/dns').toBe(200);
  const d = (res.data ?? {}) as Partial<DnsFull>;
  return {
    state: String(d.state ?? ''),
    blocks: d.blocks ?? [],
    owner: d.owner ?? { kind: '' },
    default_name: String(d.default_name ?? ''),
    zones_builtin: d.zones_builtin ?? [],
    group: d.group,
    failure: d.failure,
    last_check: d.last_check,
    draft_params: d.draft_params
  };
}

/** Ждёт одно из состояний перехвата; возвращает достигнутое. */
async function waitDnsState(page: Page, states: string[], timeout = T.save * 3): Promise<string> {
  let last = '';
  await expect
    .poll(
      async () => {
        last = (await dnsFull(page)).state;
        return states.includes(last);
      },
      { timeout, intervals: [1000] }
    )
    .toBe(true);
  return last;
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

async function routingState(page: Page): Promise<{ revision: number; desired: boolean }> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  return { revision: Number(view.data?.revision ?? 0), desired: view.data?.desired === true };
}

/** Единственное запущенное ядро; оба или ни одного — пусто. */
async function runningKernel(page: Page): Promise<Kernel | ''> {
  const caps = await api(page, 'GET', '/api/capabilities');
  expect(caps.status, 'GET /api/capabilities').toBe(200);
  const running = ((caps.data?.running_kernels ?? []) as string[]).filter((k): k is Kernel =>
    (KERNELS as readonly string[]).includes(k)
  );
  return running.length === 1 ? running[0] : '';
}

/** Владелец DNS-секции ядра: panel, xkeen, user или none. */
async function dnsSectionOwner(page: Page, kernel: Kernel): Promise<string> {
  const view = await api(page, 'GET', `/api/configlayer/cores?kernel=${kernel}`);
  expect(view.status, `GET /api/configlayer/cores?kernel=${kernel}`).toBe(200);
  const sections = (view.data?.sections ?? []) as { id?: string; owner?: string }[];
  return String(sections.find((s) => s.id === 'dns')?.owner ?? '');
}

/** Идентификаторы записей «Моих узлов» в черновике. */
async function myEntryIds(page: Page): Promise<string[]> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const sources = (view.data?.sources ?? []) as { id: string; nodes?: { entry_id?: string }[] }[];
  const my = sources.find((s) => s.id === 'my');
  return (my?.nodes ?? []).map((n) => n.entry_id ?? '').filter((id) => id !== '');
}

const kernelTitle = (k: Kernel): string => (k === 'xray' ? 'Xray' : 'Mihomo');

/** Выключает перехват настоящими маршрутами: окно — откат, включён или ждёт ядро — выключение. */
async function dnsOff(page: Page): Promise<void> {
  let st = (await dnsFull(page)).state;
  if (st === 'applying') st = await waitDnsState(page, ['off', 'window', 'on', 'waiting']);
  if (st === 'off' || st === 'unavailable') return;
  const action = st === 'window' ? 'rollback' : 'disable';
  const res = await api(page, 'POST', `/api/configlayer/dns/${action}`, {});
  expect(res.status, `POST /api/configlayer/dns/${action}`).toBe(200);
  await waitDnsState(page, ['off']);
}

const flow = {
  skip: 'подготовка не выполнялась',
  prepared: false,
  kernel: 'xray' as Kernel,
  routingWasDesired: false,
  sectionWasPanel: false,
  params: { group: '', zones: [] } as DnsParamsValue,
  myBefore: [] as string[],
  deadGroup: '',
  deadGroupName: '',
  failureReady: false
};

test.describe('dns-flow', () => {
  test.beforeAll(async ({ browser }) => {
    test.setTimeout(T.save * 8);
    await withPanel(browser, async (page) => {
      await resetDraft(page);
      const kernel = await runningKernel(page);
      if (kernel === '') {
        flow.skip = 'нет единственного запущенного ядра';
        return;
      }
      flow.kernel = kernel;
      const first = await dnsFull(page);
      if (first.state !== 'off') {
        flow.skip = `перехват в состоянии ${first.state}`;
        return;
      }
      flow.params = first.draft_params ?? { group: '', zones: [] };
      flow.myBefore = await myEntryIds(page);
      flow.routingWasDesired = (await routingState(page)).desired;
      flow.sectionWasPanel = (await dnsSectionOwner(page, kernel)) === 'panel';
      flow.prepared = true;

      if (!flow.routingWasDesired) {
        const take = await api(page, 'POST', '/api/configlayer/routing/take', {
          revision: (await routingState(page)).revision
        });
        if (take.status !== 200) {
          flow.skip = 'маршрутизация не берётся в панель';
          return;
        }
      }
      if (!flow.sectionWasPanel) {
        const take = await api(page, 'POST', '/api/configlayer/cores/take', {
          revision: (await layerProbe(page)).revision,
          kernel,
          sections: ['dns'],
          ack_comments: true
        });
        if (take.status !== 200) {
          flow.skip = 'DNS-секция ядра не берётся в панель';
          return;
        }
      }
      if ((await layerProbe(page)).changes > 0) await applyAndWait(page);

      const ready = await dnsFull(page);
      flow.skip =
        ready.blocks.length > 0 ? `включение заблокировано: ${ready.blocks.join(', ')}` : '';
    });
  });

  test.afterAll(async ({ browser }) => {
    test.setTimeout(T.save * 10);
    await withPanel(browser, async (page) => {
      await dnsOff(page);
      await resetDraft(page);
      if (!flow.prepared) return;
      const cur = (await dnsFull(page)).draft_params ?? { group: '', zones: [] };
      if (JSON.stringify(cur) !== JSON.stringify(flow.params)) {
        const res = await api(page, 'POST', '/api/configlayer/draft', {
          revision: (await layerProbe(page)).revision,
          section: 'dns_takeover',
          value: flow.params
        });
        expect(res.status, 'возврат параметров перехвата').toBe(200);
      }
      if (flow.deadGroup) {
        await api(page, 'POST', '/api/configlayer/routing/groups/delete', {
          revision: (await routingState(page)).revision,
          id: flow.deadGroup,
          cascade: { mode: '' }
        });
      }
      for (const entryId of await myEntryIds(page)) {
        if (flow.myBefore.includes(entryId)) continue;
        await api(page, 'POST', '/api/configlayer/sources/nodes/delete', {
          revision: (await layerProbe(page)).revision,
          entry_id: entryId
        });
      }
      if (!flow.sectionWasPanel && (await dnsSectionOwner(page, flow.kernel)) === 'panel') {
        await api(page, 'POST', '/api/configlayer/cores/release', {
          revision: (await layerProbe(page)).revision,
          kernel: flow.kernel,
          section: 'dns'
        });
      }
      if (!flow.routingWasDesired) {
        await api(page, 'POST', '/api/configlayer/routing/release', {
          revision: (await routingState(page)).revision
        });
      }
      if ((await layerProbe(page)).changes > 0) await applyAndWait(page);
      await resetDraft(page);
    });
  });

  test(
    'dns-flow: включение — шаги транзакции, окно, DNS у ядра',
    { tag: CORE_TAG },
    async ({ page }) => {
      test.setTimeout(T.save * 5);
      test.skip(flow.skip !== '', flow.skip);
      const guard = attachConsoleGuard(page);
      await page.setViewportSize({ width: 1440, height: 900 });
      await startPage(page);
      await openCores(page, flow.kernel);

      const state = page.getByTestId('dns-state');
      await expect(state).toHaveAttribute('data-state', 'off');
      const enable = page.getByTestId('dns-enable');
      await expect(enable).toBeEnabled({ timeout: T.action });
      await enable.click();

      // Ход транзакции: список шагов с сервера, пока идёт — aria-busy и никаких действий
      const steps = page.getByTestId('dns-steps');
      await expect(steps).toBeVisible({ timeout: T.action });
      await expect(steps).toHaveAttribute('aria-live', 'polite');
      await expect(steps).toHaveAttribute('aria-busy', 'true');
      await expect(page.getByTestId('dns-step-restart')).toBeVisible();
      await expect(page.getByTestId('dns-enable')).toHaveCount(0);

      // Итог: окно подтверждения (баннер проверяет 148-14 — здесь «Оставить» через API) или
      // сразу «включён»; возврат в «выключен» — сбой включения
      const reached = await waitDnsState(page, ['window', 'on', 'off']);
      const after = await dnsFull(page);
      expect(reached, `включение не прошло: ${after.failure?.code ?? '?'}`).not.toBe('off');
      if (reached === 'window') {
        const keep = await api(page, 'POST', '/api/configlayer/dns/keep', {});
        expect(keep.status, 'POST /api/configlayer/dns/keep').toBe(200);
      }

      await expect(state).toHaveAttribute('data-state', 'on', { timeout: T.action });
      // после успеха список шагов скрыт: состояние показывает строка карточки
      await expect(page.getByTestId('dns-steps')).toHaveCount(0);
      await expect(page.getByTestId('dns-fail')).toHaveCount(0);
      await expect(page.getByTestId('dns-kernel-badge')).toHaveText(kernelTitle(flow.kernel));
      await expect(page.getByTestId('dns-row-owner')).toContainText(kernelTitle(flow.kernel));
      await expect(page.getByTestId('dns-disable')).toBeVisible();

      await guard.assertClean();
    }
  );

  test(
    'dns-flow: проверка резолва — имя по умолчанию через группу, служебный домен через зону',
    {
      tag: CORE_TAG
    },
    async ({ page }) => {
      test.setTimeout(T.save * 3);
      test.skip(flow.skip !== '', flow.skip);
      const guard = attachConsoleGuard(page);
      await page.setViewportSize({ width: 1440, height: 900 });
      await startPage(page);
      const view = await dnsFull(page);
      test.skip(view.state !== 'on', `перехват в состоянии ${view.state}`);
      await openCores(page, flow.kernel);

      const name = page.getByTestId('dns-check-name');
      await expect(name).toHaveValue(view.default_name);
      await expect(name).toHaveAttribute('spellcheck', 'false');
      await expect(page.getByTestId('dns-check-off-hint')).toHaveCount(0);
      const btn = page.getByTestId('dns-check-btn');
      await expect(btn).toBeEnabled();
      await btn.click();

      const result = page.getByTestId('dns-check-result');
      await expect(result).toHaveAttribute('data-source', 'manual', { timeout: T.save });
      await expect(result).toHaveAttribute('role', 'status');
      await expect(result).toContainText(view.default_name);
      await expect(result.getByTestId('dns-check-outcome').first()).toHaveText(RE_RESOLVED);
      const path = result.getByTestId('dns-check-path').first();
      await expect(path).toHaveAttribute('data-path', 'group');
      if (view.group?.name) await expect(path).toContainText(view.group.name);

      // Имя веб-интерфейса роутера в служебном домене: ответ из локальной зоны, мимо группы
      const zone =
        view.zones_builtin.find(
          (z) => !['lan', 'local', 'home.arpa'].includes(z) && !z.endsWith('.arpa')
        ) ?? 'lan';
      const own = `my.${zone}`;
      await name.fill(own);
      await btn.click();
      await expect(result).toContainText(own, { timeout: T.save });
      const zonePath = result.getByTestId('dns-check-path').first();
      await expect(zonePath).toHaveAttribute('data-path', 'zone');
      await expect(zonePath).toContainText(zone);

      await guard.assertClean();
    }
  );

  test(
    'dns-flow: выключение — подтверждение, DNS у прошивки, тост',
    { tag: CORE_TAG },
    async ({ page }) => {
      test.setTimeout(T.save * 4);
      test.skip(flow.skip !== '', flow.skip);
      const guard = attachConsoleGuard(page);
      await page.setViewportSize({ width: 1440, height: 900 });
      await startPage(page);
      const view = await dnsFull(page);
      test.skip(view.state !== 'on', `перехват в состоянии ${view.state}`);
      await openCores(page, flow.kernel);

      await page.getByTestId('dns-disable').click();
      const dialog = page.getByRole('dialog');
      await expect(dialog).toBeVisible({ timeout: T.action });
      await dialog.getByRole('button', { name: RE_DISABLE_BTN }).click();
      await expect(dialog).toHaveCount(0, { timeout: T.action });

      await expect(page.getByTestId('dns-state')).toHaveAttribute('data-state', 'off', {
        timeout: T.save * 2
      });
      await expect(page.locator('.toast').filter({ hasText: RE_TOAST_DISABLED })).toBeVisible({
        timeout: T.save
      });
      if ((await dnsFull(page)).owner.kind === 'firmware') {
        await expect(page.getByTestId('dns-row-owner')).toContainText('dns-proxy');
      }
      // проверка резолва без перехвата недоступна, причина — постоянной строкой
      await expect(page.getByTestId('dns-check-btn')).toBeDisabled();
      await expect(page.getByTestId('dns-check-off-hint')).toBeVisible();

      await guard.assertClean();
    }
  );

  test(
    'dns-flow: сбой пробы — причина, откат, DNS у прошивки',
    { tag: CORE_TAG },
    async ({ page }) => {
      test.setTimeout(T.save * 8);
      test.skip(flow.skip !== '', flow.skip);
      const guard = attachConsoleGuard(page);
      await page.setViewportSize({ width: 1440, height: 900 });
      await startPage(page);
      await resetDraft(page);
      test.skip((await dnsFull(page)).state !== 'off', 'перехват не выключен');

      // Группа из одного узла без слушателя: внешнее имя через неё не разрешится
      const port = randomPort();
      const uniq = `DnsDead${port}`;
      await addToDraft(page, nodeLink('vless_ws.txt', port, uniq));
      flow.deadGroupName = `RT DNS ${port}`;
      const saved = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
        revision: (await routingState(page)).revision,
        group: {
          id: '',
          name: flow.deadGroupName,
          mode: 'auto',
          sources: { all: false, ids: ['my'] },
          include: [uniq],
          exclude: [],
          regex: false,
          picks: [],
          members: [],
          reserve: ''
        }
      });
      expect(saved.status, 'сохранение группы').toBe(200);
      flow.deadGroup = String((saved.data?.group as { id?: string } | undefined)?.id ?? '');
      expect(flow.deadGroup, 'идентификатор группы').not.toBe('');
      const params = await api(page, 'POST', '/api/configlayer/draft', {
        revision: (await layerProbe(page)).revision,
        section: 'dns_takeover',
        value: { group: flow.deadGroup, zones: flow.params.zones }
      });
      expect(params.status, 'группа-цель в параметрах перехвата').toBe(200);
      await applyAndWait(page);

      await openCores(page, flow.kernel);
      await expect(page.getByTestId('dns-row-group')).toContainText(flow.deadGroupName, {
        timeout: T.action
      });
      const enable = page.getByTestId('dns-enable');
      await expect(enable).toBeEnabled({ timeout: T.action });
      await enable.click();
      await expect(page.getByTestId('dns-steps')).toBeVisible({ timeout: T.action });

      const fail = page.getByTestId('dns-fail');
      await expect(fail).toBeVisible({ timeout: T.save * 3 });
      await expect(fail).toHaveAttribute('role', 'alert');
      await expect(fail).toContainText(RE_PROBE_EXTERNAL);
      await expect(fail).toContainText(flow.deadGroupName);
      await expect(fail).toContainText(RE_ROLLED_BACK);
      // пройденные шаги — готово, упавший — сбой, список больше не «идёт»
      await expect(page.getByTestId('dns-step-probe')).toHaveAttribute('data-status', 'failed');
      await expect(page.getByTestId('dns-step-restart')).toHaveAttribute('data-status', 'done');
      await expect(page.getByTestId('dns-steps')).toHaveAttribute('aria-busy', 'false');
      await expect(page.getByTestId('dns-state')).toHaveAttribute('data-state', 'off');

      const after = await dnsFull(page);
      expect(after.failure?.code, 'код сбоя').toBe('probe_external');
      expect(after.failure?.rolled_back, 'откат выполнен').toBe(true);
      expect(after.owner.kind, 'порт 53 у прошивки').toBe('firmware');
      await expect(page.getByTestId('dns-row-owner')).toContainText('dns-proxy');
      flow.failureReady = true;

      await guard.assertClean();
    }
  );

  for (const theme of ['dark', 'light'] as const) {
    test(
      `dns axe: шаги, сбой и результат проверки, тема ${theme}`,
      { tag: CORE_TAG },
      async ({ page }) => {
        test.skip(!flow.failureReady, 'сбой пробы не воспроизведён');
        const guard = attachConsoleGuard(page);
        await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
        await page.setViewportSize({ width: 1280, height: 900 });
        await startPage(page);
        await openCores(page, flow.kernel);

        await expect(page.getByTestId('dns-fail')).toBeVisible({ timeout: T.action });
        await expect(page.getByTestId('dns-steps')).toBeVisible();
        const output = page.getByTestId('dns-fail-output');
        if ((await output.count()) > 0) await output.locator('summary').click();
        if ((await dnsFull(page)).last_check) {
          await expect(page.getByTestId('dns-check-result')).toBeVisible();
        }

        const problems: string[] = [];
        const applied = await page.evaluate(() =>
          document.documentElement.getAttribute('data-theme')
        );
        if (applied !== theme) problems.push(`тема применена ${applied}, ожидалась ${theme}`);
        for (const v of await axeSevere(page, '[data-testid="dns-takeover-card"]')) {
          problems.push(`axe ${v}`);
        }
        expect(problems, problems.join('\n')).toEqual([]);

        await guard.assertClean();
      }
    );
  }

  test(
    'dns-flow: 390 px — шаги, сбой и проверка без горизонтальной прокрутки',
    {
      tag: CORE_TAG
    },
    async ({ page }) => {
      test.skip(!flow.failureReady, 'сбой пробы не воспроизведён');
      const guard = attachConsoleGuard(page);
      await page.setViewportSize({ width: 390, height: 844 });
      await startPage(page);
      await openCores(page, flow.kernel);

      const fail = page.getByTestId('dns-fail');
      await expect(fail).toBeVisible({ timeout: T.action });
      const output = page.getByTestId('dns-fail-output');
      if ((await output.count()) > 0) await output.locator('summary').click();
      await page.getByTestId('dns-check').scrollIntoViewIfNeeded();

      const pageScroll = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth
      );
      expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);
      for (const id of ['dns-fail', 'dns-steps', 'dns-check']) {
        const box = await page.getByTestId(id).boundingBox();
        expect(box, `${id} виден`).toBeTruthy();
        expect((box?.x ?? 0) + (box?.width ?? 0), `${id} в пределах экрана`).toBeLessThanOrEqual(
          390
        );
      }

      // ≤480 px: кнопка проверки под полем на всю его ширину
      const [input, btn] = await Promise.all([
        page.getByTestId('dns-check-name').boundingBox(),
        page.getByTestId('dns-check-btn').boundingBox()
      ]);
      expect(input && btn, 'поле и кнопка видны').toBeTruthy();
      expect((btn?.y ?? 0) >= (input?.y ?? 0) + (input?.height ?? 0) - 1, 'кнопка под полем').toBe(
        true
      );
      expect(
        Math.abs((btn?.width ?? 0) - (input?.width ?? 0)),
        'кнопка на всю ширину'
      ).toBeLessThan(2);
      expect(btn?.height ?? 0, 'высота кнопки').toBeGreaterThanOrEqual(44);

      await guard.assertClean();
    }
  );
});
