// e2e-pages: proxies config/groups
// router-area: routing
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { CORE_TAG, SLOW_ANNOTATION } from './lib/known';
import {
  addToDraft,
  api,
  installedKernels,
  layerFlag,
  nodeLink,
  randomPort,
  resetDraft,
  withPanel
} from './lib/nodes-api';
import {
  draftChanges,
  myEntryIds,
  restoreRouting,
  routingInfo,
  takeAndApply,
  type RoutingStand
} from './lib/routing-api';

// Страница «Прокси» при включённом слое «Конфигурация» на настоящей панели (фаза 150, план 12):
// группы панели и строка «Сейчас», выбор узла плиткой без черновика с живым обновлением второй
// вкладки, пинг группы и общей пачки с отменой, звено цепочки и адрес ?group=. Подготовка одна на
// спек: три узла «Моих узлов» с портами, на которых сервер не слушает (замер падает предсказуемо;
// узел TUIC — только для Mihomo), ручная группа «RT Прокси» и вложенная в «RT Внешняя», взятие
// маршрутизации и «Применить». В конце стенд возвращается: группы и узлы удаляются, маршрутизация
// отпускается, если не была взята, флаг слоя прежний. Ответы сети не подменяются. В сообщения
// провалов попадают только testId и коды, без имён узлов.

const layer = layerFlag();
const PREFIX = 'RTPX-';

const stand: RoutingStand = { groupIds: [], myBefore: [], wasManaged: false };
let proxyId = '';
let outerId = '';
let autoId = '';
let standRead = false;

test.describe.configure({ mode: 'serial' });

interface RtMember {
  member: string;
  kind: 'group' | 'node' | 'direct' | 'reject';
  ref?: string;
  xray: boolean;
  mihomo: boolean;
}

interface RtGroup {
  id: string;
  mode: string;
  current: string;
  members: RtMember[];
}

/** Группа из вида работающих групп GET /api/configlayer/proxies. */
async function runtimeGroup(page: Page, id: string): Promise<RtGroup> {
  const view = await api(page, 'GET', '/api/configlayer/proxies');
  expect(view.status, 'GET /api/configlayer/proxies').toBe(200);
  const g = ((view.data?.groups ?? []) as RtGroup[]).find((x) => x.id === id);
  expect(g, `группа ${id} в виде /api/configlayer/proxies`).toBeTruthy();
  return { ...g!, current: g!.current ?? '', members: g!.members ?? [] };
}

/** Идентификатор и состояние последней пачки пинга из GET /api/configlayer/proxies/results. */
async function lastBatch(page: Page): Promise<{ id: string; state: string }> {
  const res = await api(page, 'GET', '/api/configlayer/proxies/results');
  expect(res.status, 'GET /api/configlayer/proxies/results').toBe(200);
  const b = (res.data?.batch ?? {}) as { id?: string; state?: string };
  return { id: b.id ?? '', state: b.state ?? '' };
}

/**
 * Запускает пачку пинга кнопкой: после «Применить» и выбора с перезапуском ядро до 60 с
 * отвечает 409 kernel_restarting, и пачка не стартует — кнопка нажимается снова, пока на
 * роутере не появится новая пачка. Возвращает её идентификатор.
 */
async function startBatch(page: Page, testId: string, prevId: string): Promise<string> {
  const button = page.getByTestId(testId);
  let id = '';
  await expect
    .poll(
      async () => {
        const now = await lastBatch(page);
        if (now.id !== '' && now.id !== prevId) {
          id = now.id;
          return 'started';
        }
        if (now.state !== 'running' && (await button.getAttribute('aria-disabled')) !== 'true') {
          await button.click();
        }
        return `пачка не началась (${now.state || 'нет'})`;
      },
      { message: `запуск пачки кнопкой ${testId}`, timeout: T.save * 1.5, intervals: [1000] }
    )
    .toBe('started');
  return id;
}

async function openProxies(page: Page, hash = '#/proxies'): Promise<void> {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('proxies-layer-page')).toBeVisible({ timeout: T.page });
}

/** Карточка группы видна; раскрыта, если просили. */
async function cardOf(page: Page, id: string, expand = false) {
  const card = page.getByTestId(`proxy-group-${id}`);
  await expect(card, `proxy-group-${id}`).toBeVisible({ timeout: T.page });
  const head = card.locator('.gc-head').first();
  if (expand && (await head.getAttribute('aria-expanded')) !== 'true') {
    await card.locator('.pc-name').first().click();
  }
  if (expand) await expect(head).toHaveAttribute('aria-expanded', 'true', { timeout: T.action });
  return card;
}

/** Индекс участника группы по виду (testId плитки — proxy-node-<группа>-<индекс>). */
function indexOf(g: RtGroup, pred: (m: RtMember) => boolean): number {
  return g.members.findIndex(pred);
}

async function prepare(browser: Browser): Promise<void> {
  await withPanel(browser, async (page) => {
    await resetDraft(page);
    stand.wasManaged = (await routingInfo(page)).desired;
    stand.myBefore = await myEntryIds(page);
    standRead = true;
    const port = randomPort();
    await addToDraft(
      page,
      [
        nodeLink('vless_ws.txt', port, `${PREFIX}a`),
        nodeLink('trojan_grpc.txt', port + 1, `${PREFIX}b`),
        // TUIC — только для Mihomo (D-06): при Xray он в строке «ещё N … только для Mihomo»
        nodeLink('tuic.txt', port + 2, `${PREFIX}t`)
      ].join('\n')
    );
    const save = async (group: Record<string, unknown>): Promise<string> => {
      const res = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
        revision: (await routingInfo(page)).revision,
        group: {
          id: '',
          mode: 'manual',
          sources: { all: true, ids: [] },
          include: [],
          exclude: [],
          regex: false,
          picks: [],
          members: [],
          reserve: '',
          ...group
        }
      });
      expect(res.status, 'сохранение группы').toBe(200);
      const id = String((res.data?.group as { id?: string } | undefined)?.id ?? '');
      expect(id, 'идентификатор группы').not.toBe('');
      return id;
    };
    // ручная группа: DIRECT, REJECT и узлы «Моих узлов» с префиксом теста
    proxyId = await save({
      name: 'RT Прокси',
      sources: { all: false, ids: ['my'] },
      include: [PREFIX],
      members: [{ kind: 'direct' }, { kind: 'reject' }]
    });
    stand.groupIds = [proxyId];
    // вложенная: единственный участник — «RT Прокси»; внешняя удаляется раньше вложенной
    outerId = await save({
      name: 'RT Внешняя',
      sources: { all: false, ids: [] },
      members: [{ kind: 'group', id: proxyId }]
    });
    // авто-группа: «Сейчас» берётся из опроса ядра и сбрасывается при остановке ядра
    autoId = await save({
      name: 'RT Авто',
      mode: 'auto',
      sources: { all: false, ids: ['my'] },
      include: [PREFIX]
    });
    stand.groupIds = [outerId, autoId, proxyId];
    await takeAndApply(page);
    const applied = (await routingInfo(page)).groups.filter(
      (g) => g.id === proxyId || g.id === outerId
    );
    expect(
      applied.every((g) => g.applied),
      'группы теста применены'
    ).toBe(true);
    // звено цепочки внешней группы: выбрана вложенная (первый и единственный участник)
    const outer = await runtimeGroup(page, outerId);
    const nested = outer.members.find((m) => m.kind === 'group');
    expect(nested, 'участник-группа внешней группы').toBeTruthy();
    if (outer.current !== nested!.member) {
      const sel = await api(page, 'POST', '/api/configlayer/routing/select', {
        group: outerId,
        member: nested!.member
      });
      expect(sel.status, 'выбор вложенной группы во внешней').toBe(200);
    }
  });
}

test.beforeAll(async ({ browser }) => {
  // флаг слоя, узлы, две группы, взятие маршрутизации и «Применить» с перезапуском ядра
  test.setTimeout(T.save * 8);
  await layer.enable(browser);
  await prepare(browser);
});

test.afterAll(async ({ browser }) => {
  test.setTimeout(T.save * 6);
  try {
    // стенд прочитан до первой правки: возвращать есть что
    if (standRead) {
      await withPanel(browser, (page) => restoreRouting(page, stand));
    }
  } finally {
    await layer.restore(browser);
  }
});

test(
  'proxies-runtime: страница при слое — группы панели, «Сейчас», плитки и строка несовместимых',
  { tag: CORE_TAG },
  async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await openProxies(page);
    await expect(page.locator('.api-offline')).toHaveCount(0);
    await expect(page.locator('.sidebar-nav a[href="#/proxies"]')).toBeVisible({
      timeout: T.action
    });

    await cardOf(page, outerId);
    const card = await cardOf(page, proxyId, true);
    await expect(page.getByTestId(`proxy-group-now-${proxyId}`)).toContainText(/Сейчас|Now/);
    await expect(page.getByTestId(`proxy-group-now-${outerId}`)).toContainText(/Сейчас|Now/);
    await expect(card.locator(`[data-testid^="proxy-node-${proxyId}-"]`).first()).toBeVisible({
      timeout: T.action
    });

    // D-06: узел, который активное ядро не умеет, — в свёрнутой строке, а не в сетке
    const inst = await installedKernels(page);
    const hidden = page.getByTestId(`proxy-group-hidden-${proxyId}`);
    if (RT.core === 'xray' && inst.mihomo) {
      await expect(hidden).toBeVisible();
      await expect(hidden).toContainText('Mihomo');
      const g = await runtimeGroup(page, proxyId);
      const tuic = indexOf(g, (m) => m.kind === 'node' && !m.xray && m.mihomo);
      expect(tuic, 'узел только для Mihomo в составе группы').toBeGreaterThanOrEqual(0);
      await expect(page.getByTestId(`proxy-node-${proxyId}-${tuic}`)).toHaveCount(0);
    } else if (RT.core === 'mihomo') {
      await expect(hidden).toHaveCount(0);
    }
    await guard.assertClean();
  }
);

test(
  'proxies-runtime: выбор плиткой без черновика, вторая вкладка видит выбор по событию',
  {
    tag: CORE_TAG,
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'выбор узла с возможным перезапуском Xray и ожиданием события во второй вкладке'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 3);
    const guard = attachConsoleGuard(page);
    const second = await page.context().newPage();
    const guard2 = attachConsoleGuard(second);
    try {
      await openProxies(page);
      // вторая вкладка: карточку раскрывает адрес ?group=
      await openProxies(second, `#/proxies?group=${proxyId}`);
      await expect(
        second.getByTestId(`proxy-group-${proxyId}`).locator('.gc-head').first()
      ).toHaveAttribute('aria-expanded', 'true', { timeout: T.action });
      await cardOf(page, proxyId, true);

      // цель — DIRECT или REJECT, который сейчас не выбран
      const g = await runtimeGroup(page, proxyId);
      const currentKind = g.members.find((m) => m.member === g.current)?.kind;
      const targetKind = currentKind === 'direct' ? 'reject' : 'direct';
      const idx = indexOf(g, (m) => m.kind === targetKind);
      expect(idx, `участник ${targetKind} в составе группы`).toBeGreaterThanOrEqual(0);
      const target = g.members[idx].member;
      const tile = page.getByTestId(`proxy-node-${proxyId}-${idx}`);
      const tile2 = second.getByTestId(`proxy-node-${proxyId}-${idx}`);
      await expect(tile).toHaveAttribute('aria-pressed', 'false', { timeout: T.action });
      await expect(tile2).toHaveAttribute('aria-pressed', 'false', { timeout: T.action });

      const draftBefore = await draftChanges(page);
      await tile.click();
      await expect(
        page
          .locator('.toast--success')
          .filter({ hasText: /Выбрано|Selected|Узел выбран|Node selected/ })
      ).toBeVisible({ timeout: T.save });
      await expect(page.getByRole('dialog')).toHaveCount(0);
      expect(await draftChanges(page), 'выбор не создаёт изменений черновика').toBe(draftBefore);
      await expect(tile).toHaveAttribute('aria-pressed', 'true', { timeout: T.action });

      // вторая вкладка без перезагрузки: событие общего SSE перечитывает вид
      await expect(tile2).toHaveAttribute('aria-pressed', 'true', { timeout: T.action });
      await expect
        .poll(async () => (await runtimeGroup(page, proxyId)).current, { timeout: T.action })
        .toBe(target);
      await guard.assertClean();
      await guard2.assertClean();
    } finally {
      await second.close();
    }
  }
);

test(
  'proxies-runtime: пинг — «Проверить все», статусы плиток, отмена общей пачки',
  {
    tag: CORE_TAG,
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'пачка пинга группы до итога с таймаутами узлов и отмена общей пачки'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 4);
    const guard = attachConsoleGuard(page);
    await openProxies(page, `#/proxies?group=${proxyId}`);
    const card = await cardOf(page, proxyId, true);
    const status = page.getByTestId('proxies-ping-status');
    await expect(status).toHaveAttribute('aria-live', 'polite');

    // «Проверить» группы: пачка проходит до итога «Проверено N …»
    const groupBatch = await startBatch(
      page,
      `proxy-group-ping-${proxyId}`,
      (await lastBatch(page)).id
    );
    await expect
      .poll(async () => (await lastBatch(page)).state, {
        message: 'пачка группы закончилась',
        timeout: 40_000 * RT.slow,
        intervals: [1000]
      })
      .toBe('done');
    expect((await lastBatch(page)).id, 'итог у той же пачки').toBe(groupBatch);
    await expect(status).toContainText(/Проверено|Checked/, { timeout: T.action });

    // узлы с портами вне сервиса: «Таймаут» или «Ошибка»
    const g = await runtimeGroup(page, proxyId);
    const node = indexOf(g, (m) => m.kind === 'node' && (RT.core === 'mihomo' ? m.mihomo : m.xray));
    expect(node, 'узел активного ядра в составе группы').toBeGreaterThanOrEqual(0);
    const nodeStatus = page.getByTestId(`proxy-node-status-${proxyId}-${node}`);
    await expect(nodeStatus).toHaveText(/Таймаут|Timeout|Ошибка|Error/, { timeout: T.action });
    await expect(page.getByTestId(`proxy-group-noreply-${proxyId}`)).toBeVisible({
      timeout: T.action
    });
    await expect(page.getByTestId(`proxy-group-noreply-${proxyId}`)).toContainText(
      /Ни один узел не ответил|No nodes responded/
    );
    await expect(card.getByRole('button', { name: /Проверить ещё раз|Check again/ })).toBeVisible();

    // статус открывает причину и диагностику, но не выбирает узел (D-25)
    const tiles = card.locator(`[data-testid^="proxy-node-${proxyId}-"]`);
    const pressedBefore = await tiles.evaluateAll((els) =>
      els.map((el) => el.getAttribute('aria-pressed'))
    );
    await nodeStatus.click();
    const pop = page.getByRole('dialog').filter({ hasText: /Причина|Reason/ });
    await expect(pop).toBeVisible({ timeout: T.action });
    await expect(
      pop.getByRole('button', { name: /Диагностика узла|Node diagnostics/ })
    ).toBeVisible();
    const pressedAfter = await tiles.evaluateAll((els) =>
      els.map((el) => el.getAttribute('aria-pressed'))
    );
    expect(pressedAfter, 'нажатие на статус не меняет выбор').toEqual(pressedBefore);
    await page.keyboard.press('Escape');
    await expect(pop).toHaveCount(0, { timeout: T.action });
    await expect(nodeStatus).toBeFocused();

    // «Проверить все» и сразу «Отменить проверку»: итог «Отменено, проверено N из M»
    await startBatch(page, 'proxies-ping-all', groupBatch);
    const cancel = page.getByTestId('proxies-ping-cancel');
    const cancelled = await cancel
      .click({ timeout: T.action })
      .then(() => true)
      .catch(() => false);
    await expect
      .poll(async () => (await lastBatch(page)).state, {
        message: 'общая пачка закончилась',
        timeout: T.save,
        intervals: [1000]
      })
      .not.toBe('running');
    const final = await lastBatch(page);
    if (cancelled && final.state === 'cancelled') {
      await expect(status).toContainText(/Отменено|Cancelled/, { timeout: T.action });
    } else {
      // пачка успела закончиться до нажатия: итог «Проверено», отмена не проверена
      expect(final.state, 'итог общей пачки').toBe('done');
      await expect(status).toContainText(/Проверено|Checked/, { timeout: T.action });
      test.info().annotations.push({
        type: 'skip-part',
        description: 'общая пачка закончилась до нажатия «Отменить проверку»: отмена не проверена'
      });
    }
    await guard.assertClean();
  }
);

test(
  'proxies-runtime: ссылка-звено цепочки и адрес ?group= раскрывают карточку',
  { tag: CORE_TAG },
  async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await openProxies(page);
    const outerCard = await cardOf(page, outerId);
    const innerHead = page.getByTestId(`proxy-group-${proxyId}`).locator('.gc-head').first();
    await expect(innerHead).toHaveAttribute('aria-expanded', 'false', { timeout: T.action });

    // звено «Сейчас» внешней группы ведёт к вложенной: раскрыть и перевести фокус на шапку
    const link = page
      .getByTestId(`proxy-group-now-${outerId}`)
      .getByRole('button', { name: /^(Перейти к группе|Go to group)/ })
      .first();
    await expect(link).toBeVisible({ timeout: T.action });
    await link.click();
    await expect(innerHead).toHaveAttribute('aria-expanded', 'true', { timeout: T.action });
    await expect(innerHead).toBeFocused({ timeout: T.action });
    // нажатие на звено не сворачивает и не раскрывает внешнюю карточку
    await expect(outerCard.locator('.gc-head').first()).toHaveAttribute('aria-expanded', 'false');

    // адрес ?group= раскрывает внешнюю карточку
    await page.goto(`/#/proxies?group=${outerId}`, {
      timeout: T.page,
      waitUntil: 'domcontentloaded'
    });
    const outerHead = page.getByTestId(`proxy-group-${outerId}`).locator('.gc-head').first();
    await expect(outerHead).toHaveAttribute('aria-expanded', 'true', { timeout: T.page });
    await expect(outerHead).toBeFocused({ timeout: T.action });
    await guard.assertClean();
  }
);

/** Состояние службы ядра: работающие ядра по GET /api/service/status. */
async function runningKernels(page: Page): Promise<string[]> {
  const res = await page.request.get('/api/service/status');
  if (!res.ok()) return ['http ' + res.status()];
  const body = (await res.json()) as { data?: { running_kernels?: string[] } };
  return body.data?.running_kernels ?? [];
}

test(
  'proxies-runtime: «Сейчас» авто-группы и окно причины снимаются при остановке ядра',
  {
    tag: CORE_TAG,
    annotation: {
      type: SLOW_ANNOTATION,
      description: 'остановка и запуск службы ядра с ожиданием устойчивого состояния'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 8);
    const guard = attachConsoleGuard(page);
    await openProxies(page);
    const now = page.getByTestId(`proxy-group-now-${autoId}`);
    await expect(now).toBeVisible({ timeout: T.page });
    const unknown = /неизвестен|unknown/i;
    // ядро отдало «Сейчас» авто-группы: строка не «неизвестен» (Mihomo — узел, Xray — быстрый)
    const known = await expect
      .poll(async () => !unknown.test((await now.textContent()) ?? ''), { timeout: T.save })
      .toBe(true)
      .then(() => true)
      .catch(() => false);
    const core = RT.core;
    try {
      if (known) {
        const before = ((await now.textContent()) ?? '').trim();
        // IN-05: окно причины у «Таймаут»/«Ошибка» узла авто-группы закрывается, когда замер
        // узла пропадает (после остановки ядра история задержек сбрасывается)
        const autoCard = await cardOf(page, autoId, true);
        const reasonStatus = autoCard.locator(`[data-testid^="proxy-node-status-${autoId}-"]`);
        const pop = page.getByRole('dialog').filter({ hasText: /Причина|Reason/ });
        const popOpened =
          (await reasonStatus
            .first()
            .waitFor({ state: 'visible', timeout: T.action })
            .then(() => true)
            .catch(() => false)) &&
          (await reasonStatus
            .first()
            .click()
            .then(() => pop.isVisible())
            .catch(() => false));
        const stop = await api(page, 'POST', '/api/service/control?action=stop');
        expect(stop.status, 'остановка службы').toBe(200);
        await expect
          .poll(async () => (await runningKernels(page)).length, {
            message: 'ядро остановлено',
            timeout: T.save * 3,
            intervals: [2000]
          })
          .toBe(0);
        // WR-07: опрос остановлен, прежний «Сейчас» не показывается (D-26)
        await expect(now, `строка «Сейчас» была «${before}»`).toContainText(unknown, {
          timeout: T.save
        });
        if (popOpened) {
          await expect(pop, 'IN-05: окно причины закрыто').toHaveCount(0, { timeout: T.action });
        } else {
          test.info().annotations.push({
            type: 'skip-part',
            description:
              'у узлов авто-группы нет статуса «Таймаут»/«Ошибка»: окно причины не проверено'
          });
        }
      } else {
        test.info().annotations.push({
          type: 'skip-part',
          description: `ядро ${core} не отдаёт «Сейчас» авто-группы без замеров: сброс не проверен`
        });
      }
    } finally {
      // служба возвращается и после падения: следующие спеки начинают с рабочего ядра
      if ((await runningKernels(page)).length === 0) {
        await api(page, 'POST', '/api/service/control?action=start');
      }
      await expect
        .poll(async () => (await runningKernels(page)).join(','), {
          message: 'ядро вернулось в рабочее состояние',
          timeout: T.save * 4,
          intervals: [3000]
        })
        .toBe(core);
    }
    await guard.assertClean();
  }
);
