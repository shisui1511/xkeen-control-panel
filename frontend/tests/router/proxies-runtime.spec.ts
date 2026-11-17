// e2e-pages: proxies config/groups
// router-area: routing
import { execFile } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { CORE_TAG, FULL_TAG, SLOW_ANNOTATION, TIER_FULL_ANNOTATION } from './lib/known';
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
  switching?: boolean;
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
  const head = card.locator('.pc-toggle').first();
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
          // окно не висит без якоря: если статус узла пропал, окно закрыто (при замерах пачки
          // статус остаётся, и окно вправе остаться открытым)
          await expect
            .poll(
              async () => (await pop.count()) === 0 || (await reasonStatus.first().isVisible()),
              { message: 'IN-05: окно причины без якоря', timeout: T.action }
            )
            .toBe(true);
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
        second.getByTestId(`proxy-group-${proxyId}`).locator('.pc-toggle').first()
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

      // τ — время перечитывания вида (GET /api/configlayer/proxies), наибольшее из трёх замеров
      let tau = 0;
      for (let i = 0; i < 3; i++) {
        const t0 = Date.now();
        const r = await page.request.get('/api/configlayer/proxies');
        expect(r.status(), 'GET /api/configlayer/proxies (замер τ)').toBe(200);
        tau = Math.max(tau, Date.now() - t0);
      }

      // Наблюдатель во второй вкладке (D-17): идущий на сервере выбор (switching) блокирует
      // плитки группы и в той вкладке, где выбора не нажимали — aria-disabled и подсказка
      // «Идёт переключение группы. Дождитесь завершения.» (proxies.rt.locked_title)
      await second.evaluate((gid) => {
        const locked = /Идёт переключение группы|The group is switching/;
        const w = window as unknown as {
          __rtSwitching?: { seen: boolean };
          __rtSwitchingObs?: MutationObserver;
        };
        const rec = { seen: false };
        w.__rtSwitching = rec;
        const check = () => {
          if (rec.seen) return;
          const tiles = document.querySelectorAll(`[data-testid^="proxy-node-${gid}-"]`);
          for (const el of tiles) {
            if (
              el.getAttribute('aria-disabled') === 'true' &&
              locked.test(el.getAttribute('title') ?? '')
            ) {
              rec.seen = true;
              return;
            }
          }
        };
        const obs = new MutationObserver(check);
        obs.observe(document.body, {
          subtree: true,
          childList: true,
          attributes: true,
          attributeFilter: ['aria-disabled', 'title']
        });
        w.__rtSwitchingObs = obs;
        check();
      }, proxyId);

      const selectResp = page.waitForResponse(
        (r) =>
          r.request().method() === 'POST' && r.url().includes('/api/configlayer/routing/select'),
        { timeout: T.save }
      );
      const clickedAt = Date.now();
      await tile.click();
      const resp = await selectResp;
      const d = Date.now() - clickedAt;
      expect(resp.status(), 'POST routing/select').toBe(200);
      const seen = await second.evaluate(
        () =>
          (window as unknown as { __rtSwitching?: { seen: boolean } }).__rtSwitching?.seen ?? false
      );
      test.info().annotations.push({
        type: 'proxies-switching',
        description: `proxies-switching: d=${d} tau=${tau} blocked_seen=${seen}`
      });
      // Окно выбора короче четырёх перечитываний вида: событие начала, перечитывание и отрисовка
      // могут не успеть — не регрессия D-17, только аннотация с числами
      if (d >= 4 * tau) {
        expect(
          seen,
          `D-17: во второй вкладке плитки группы не были заблокированы за выбор (d=${d} мс, τ=${tau} мс)`
        ).toBe(true);
      }

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
      await second
        .evaluate(() =>
          (
            window as unknown as { __rtSwitchingObs?: MutationObserver }
          ).__rtSwitchingObs?.disconnect()
        )
        .catch(() => undefined);
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
    // Первое открытие потока событий слоя: события до подписки не доставляются, поэтому вид
    // и результаты пинга, прочитанные страницей при загрузке, перечитываются после открытия.
    // Иначе пачка, закончившаяся до подписки (поток открывается после чтения состояния слоя,
    // на роутере — через секунды), оставалась бы «Проверяется 0 из 3». Считаются запросы
    // результатов после ответа потока (открытие) — до первых вызовов api теста.
    // Вид тоже: событие открытия могло прийти до ответа первого чтения вида, и тогда
    // перечитывание ждёт конца первой загрузки — считается чтение вида, начатое после
    // открытия потока и после первого ответа вида.
    let streamOpen = false;
    let viewLoaded = false;
    let resultsAfterOpen = 0;
    let viewAfterOpen = 0;
    const isView = (u: string) => new URL(u).pathname === '/api/configlayer/proxies';
    page.on('response', (r) => {
      if (r.url().includes('/api/configlayer/events')) streamOpen = true;
      if (r.request().method() === 'GET' && isView(r.url())) viewLoaded = true;
    });
    page.on('request', (r) => {
      if (
        streamOpen &&
        r.method() === 'GET' &&
        r.url().includes('/api/configlayer/proxies/results')
      )
        resultsAfterOpen++;
      if (streamOpen && viewLoaded && r.method() === 'GET' && isView(r.url())) viewAfterOpen++;
    });
    await openProxies(page, `#/proxies?group=${proxyId}`);
    const card = await cardOf(page, proxyId, true);
    const status = page.getByTestId('proxies-ping-status');
    await expect(status).toHaveAttribute('aria-live', 'polite');
    await expect
      .poll(() => (streamOpen ? resultsAfterOpen : -1), {
        message: 'перечитывание результатов пинга при первом открытии потока событий слоя',
        timeout: T.page
      })
      .toBeGreaterThan(0);
    await expect
      .poll(() => (streamOpen ? viewAfterOpen : -1), {
        message: 'перечитывание вида «Прокси» после первого открытия потока и первой загрузки',
        timeout: T.page
      })
      .toBeGreaterThan(0);

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
    // нажатие внутри окна (на текст причины) его не закрывает: фокус уходит на body
    await pop.locator('.pns-reason').click();
    await expect(pop).toBeVisible();
    const pressedAfter = await tiles.evaluateAll((els) =>
      els.map((el) => el.getAttribute('aria-pressed'))
    );
    expect(pressedAfter, 'нажатие на статус не меняет выбор').toEqual(pressedBefore);
    await page.keyboard.press('Escape');
    await expect(pop).toHaveCount(0, { timeout: T.action });
    await expect(nodeStatus).toBeFocused();

    // с клавиатуры: Enter открывает окно, Tab закрывает его и возвращает фокус на статус
    await page.keyboard.press('Enter');
    await expect(pop).toBeVisible({ timeout: T.action });
    await page.keyboard.press('Tab');
    await expect(pop).toHaveCount(0, { timeout: T.action });
    await expect(nodeStatus).toBeFocused();

    // «Диагностика узла» открывает окно источника, а Esc возвращает фокус на статус
    await page.keyboard.press('Enter');
    await expect(pop).toBeVisible({ timeout: T.action });
    await pop.getByRole('button', { name: /Диагностика узла|Node diagnostics/ }).click();
    await expect(page.getByRole('dialog').first()).toBeVisible({ timeout: T.action });
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog')).toHaveCount(0, { timeout: T.action });
    await expect(nodeStatus).toBeFocused();

    // «Проверить все» и сразу «Отменить проверку»: итог «Отменено, проверено N из M»
    await startBatch(page, 'proxies-ping-all', groupBatch);
    // пока пачка идёт, «Проверить все» с aria-disabled выглядит неактивной (не как обычная)
    if ((await lastBatch(page)).state === 'running') {
      const all = page.getByTestId('proxies-ping-all');
      await expect(all).toHaveAttribute('aria-disabled', 'true', { timeout: T.action });
      await expect(all).toHaveCSS('opacity', '0.45');
    }
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
    const innerHead = page.getByTestId(`proxy-group-${proxyId}`).locator('.pc-toggle').first();
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
    await expect(outerCard.locator('.pc-toggle').first()).toHaveAttribute('aria-expanded', 'false');

    // адрес ?group= раскрывает внешнюю карточку
    await page.goto(`/#/proxies?group=${outerId}`, {
      timeout: T.page,
      waitUntil: 'domcontentloaded'
    });
    const outerHead = page.getByTestId(`proxy-group-${outerId}`).locator('.pc-toggle').first();
    await expect(outerHead).toHaveAttribute('aria-expanded', 'true', { timeout: T.page });
    await expect(outerHead).toBeFocused({ timeout: T.action });
    await guard.assertClean();
  }
);

// --- Отказ фонового прохода выбора (150-29, D-11) ---
// При запущенном Mihomo и остановленном Xray выбор узла применяется к Mihomo сразу, а файл
// Xray пишется фоновым проходом. Сбой вносится на самом роутере: управляемый панелью файл Xray
// (не xcp-groups) получает дописанный перевод строки — дрейф, на котором фоновый проход
// отказывает (drift_blocked, класс rejected), выбор возвращается к прежнему узлу и сервер
// публикует уведомление routing_select:<gid>:<n> с новым n на каждый отказ. Исходные байты
// файла лежат в копии на роутере и возвращаются в finally теста. Ответы сети не подменяются.

const run = promisify(execFile);
const REPO_ROOT = fileURLToPath(new URL('../../../', import.meta.url));
/** Файл в каталоге конфигов Xray: только такой путь подставляется в команду на стенде. */
const XRAY_FILE = /^\/opt\/etc\/xray\/configs\/[A-Za-z0-9._-]+$/;
const XRAY_DIR_PREFIX = '/opt/etc/xray/configs/';
const DRIFT_BAK = '/tmp/rtpx-drift.bak';
const REJECTED = /файл Xray не принят|the Xray file was rejected/;
const RESTORED = /Прежний узел возвращён|previous node has been restored/;

/**
 * Команда на стенде arm64 через scripts/router/lib.sh из корня репозитория: алиас и доступы
 * берутся только из локального файла целей. Возвращает код выхода; сбой подготовки — ошибка.
 * Вывод ssh (в нём может быть алиас) в отчёт не попадает.
 */
async function standSh(remote: string): Promise<number> {
  const code = await run(
    'sh',
    [
      '-c',
      '. scripts/router/lib.sh || exit 90; rt_load_targets || exit 91; rt_ssh arm64 "$1"',
      'sh',
      remote
    ],
    { cwd: REPO_ROOT, env: { ...process.env, ROUTERS: 'arm64' }, timeout: 60_000 }
  ).then(
    () => 0 as unknown,
    (err: unknown) => (err as { code?: unknown } | null)?.code
  );
  if (code === 3) throw new Error('стенд arm64: нет связи по ssh');
  if (code === 90 || code === 91) throw new Error('стенд arm64: файл целей не прочитан');
  if (typeof code !== 'number') {
    throw new Error('стенд arm64: команда не выполнена (таймаут или запуск sh)');
  }
  return code;
}

interface LayerStateView {
  files: { kernel?: string; owner?: string; state?: string; path?: string }[];
  notices: { id: string; params?: Record<string, string> }[];
  drift: number;
}

/** Снимок слоя GET /api/configlayer/state: файлы, уведомления и число расхождений. */
async function layerState(page: Page): Promise<LayerStateView> {
  const res = await api(page, 'GET', '/api/configlayer/state');
  expect(res.status, 'GET /api/configlayer/state').toBe(200);
  const d = res.data ?? {};
  return {
    files: (d.files ?? []) as LayerStateView['files'],
    notices: (d.notices ?? []) as LayerStateView['notices'],
    drift: Number(d.drift_count ?? 0)
  };
}

/** Идентификаторы уведомлений отказа выбора группы gid. */
function selectNoticeIds(s: LayerStateView, gid: string): string[] {
  return s.notices.filter((n) => n.id.startsWith(`routing_select:${gid}:`)).map((n) => n.id);
}

/**
 * Управляемый панелью файл Xray в каталоге конфигов, кроме xcp-groups, в состоянии ok (как
 * rtRtDriftFile Go-сценария). Путь проверяется шаблоном каталога Xray до подстановки в команду.
 */
function driftFile(s: LayerStateView): string {
  const f = s.files.find((x) => {
    const p = x.path ?? '';
    const base = p.slice(XRAY_DIR_PREFIX.length);
    return (
      x.kernel === 'xray' &&
      x.owner === 'panel' &&
      x.state === 'ok' &&
      p.startsWith(XRAY_DIR_PREFIX) &&
      !base.includes('/') &&
      !base.includes('xcp-groups')
    );
  });
  if (!f) {
    throw new Error('фоновый отказ: нет управляемого файла Xray вне xcp-groups в состоянии ok');
  }
  const path = f.path ?? '';
  if (!XRAY_FILE.test(path)) {
    throw new Error('фоновый отказ: путь файла Xray вне шаблона каталога');
  }
  return path;
}

/** Число бейджа раздела «Конфигурация» по его aria-label (бейджа нет — 0). */
async function configBadge(page: Page): Promise<number> {
  const badge = page.locator('.nav-badge-config');
  if ((await badge.count()) === 0) return 0;
  const label = (await badge.first().getAttribute('aria-label')) ?? '';
  const m = /(\d+)\s*$/.exec(label);
  return m ? Number(m[1]) : Number.NaN;
}

/** Конец переключения группы: switching в виде «Прокси» снят (фоновый проход закончен). */
async function waitSettled(page: Page, gid: string): Promise<void> {
  await expect
    .poll(async () => (await runtimeGroup(page, gid)).switching === true, {
      message: `группа ${gid} вышла из переключения`,
      timeout: 90_000 * RT.slow,
      intervals: [500]
    })
    .toBe(false);
}

test(
  'proxies-runtime: отказ фонового прохода выбора — тост на каждый отказ, уведомление на «Конфигурации», без тоста при загрузке',
  {
    tag: [CORE_TAG, FULL_TAG],
    annotation: {
      type: TIER_FULL_ANNOTATION,
      description: 'внесение сбоя: дрейф управляемого файла Xray через ssh стенда'
    }
  },
  async ({ page }) => {
    test.skip(RT.core !== 'mihomo', 'фоновый проход файла Xray идёт только при ядре Mihomo');
    test.setTimeout(T.save * 8);
    const guard = attachConsoleGuard(page);
    await openProxies(page);
    const inst = await installedKernels(page);
    test.skip(!inst.xray, 'Xray не установлен: фонового прохода файла Xray нет');

    // (1) файл дрейфа и исходное число расхождений
    const before = await layerState(page);
    expect(selectNoticeIds(before, proxyId), `уведомления выбора группы ${proxyId}`).toEqual([]);
    const path = driftFile(before);
    const driftCmd =
      `f='${path}'; b=${DRIFT_BAK}; [ ! -e "$b" ] || exit 4; ` +
      `cp -p "$f" "$b" && printf '\\n' >> "$f"`;
    const restoreCmd =
      `f='${path}'; b=${DRIFT_BAK}; [ -e "$b" ] || exit 0; ` + `cp -p "$b" "$f" && rm -f "$b"`;
    const restore = async (): Promise<void> => {
      const code = await standSh(restoreCmd);
      if (code !== 0) throw new Error(`фоновый отказ: файл Xray не возвращён (код ${code})`);
    };

    // цель — узел Mihomo, отличный от текущего выбора группы
    const card = await cardOf(page, proxyId, true);
    const g = await runtimeGroup(page, proxyId);
    const idx = indexOf(g, (m) => m.kind === 'node' && m.mihomo && m.member !== g.current);
    expect(idx, `узел Mihomo вне текущего выбора в группе ${proxyId}`).toBeGreaterThanOrEqual(0);
    const target = g.members[idx].member;
    const tile = card.getByTestId(`proxy-node-${proxyId}-${idx}`);
    await expect(tile).toHaveAttribute('aria-pressed', 'false', { timeout: T.action });

    // (2) исходное число бейджа «Конфигурации»: снимок слоя страницы уже загружен
    const badgeBefore = await configBadge(page);
    expect(Number.isNaN(badgeBefore), 'число бейджа .nav-badge-config').toBe(false);

    const errToast = page.locator('.toast.toast--error').filter({ hasText: 'RT Прокси' });
    const failOnce = async (n: number): Promise<string> => {
      await expect(errToast, `тост прошлого отказа группы ${proxyId} ушёл`).toHaveCount(0, {
        timeout: T.action
      });
      await expect(tile).toHaveAttribute('aria-pressed', 'false', { timeout: T.action });
      await expect(tile).not.toHaveAttribute('aria-disabled', 'true', { timeout: T.save });
      const selectResp = page.waitForResponse(
        (r) =>
          r.request().method() === 'POST' && r.url().includes('/api/configlayer/routing/select'),
        { timeout: T.save }
      );
      await tile.click();
      expect((await selectResp).status(), `POST routing/select, отказ №${n}`).toBe(200);
      // ровно один тост ошибки на новый идентификатор уведомления
      await expect(errToast, `тост отказа №${n} группы ${proxyId}`).toHaveCount(1, {
        timeout: T.save
      });
      await expect(errToast, `причина rejected в тосте №${n}`).toContainText(REJECTED);
      await expect(errToast, `возврат прежнего узла в тосте №${n}`).toContainText(RESTORED);
      await waitSettled(page, proxyId);
      let ids: string[] = [];
      await expect
        .poll(
          async () => {
            ids = selectNoticeIds(await layerState(page), proxyId);
            return ids.length;
          },
          { message: `уведомление выбора группы ${proxyId}, отказ №${n}`, timeout: T.action }
        )
        .toBe(1);
      // выбор вернулся к прежнему узлу
      expect((await runtimeGroup(page, proxyId)).current, `current группы ${proxyId}`).toBe(
        g.current
      );
      return ids[0];
    };

    let drifted = false;
    try {
      // (3) дрейф: копия исходных байтов с правами и дописанный перевод строки
      drifted = true;
      const code = await standSh(driftCmd);
      if (code === 4) {
        drifted = false;
        throw new Error('фоновый отказ: на стенде осталась копия прерванного прогона');
      }
      if (code !== 0) throw new Error(`фоновый отказ: дрейф не внесён (код ${code})`);

      // (4) и (5) два отказа подряд: два тоста, идентификаторы уведомлений различаются (D-11)
      const firstId = await failOnce(1);
      const secondId = await failOnce(2);
      expect(secondId, `D-11: новый идентификатор уведомления группы ${proxyId}`).not.toBe(firstId);
      await expect(errToast).toHaveCount(0, { timeout: T.action });

      // (6) загрузка «Прокси» при уже существующем уведомлении: тоста нет
      await page.reload({ timeout: T.page, waitUntil: 'domcontentloaded' });
      await expect(page.getByTestId('proxies-layer-page')).toBeVisible({ timeout: T.page });
      await cardOf(page, proxyId);
      await page.waitForTimeout(3000);
      await expect(errToast, `тост при загрузке с уведомлением группы ${proxyId}`).toHaveCount(0);

      // (7) «Конфигурация»: уведомление с группой и причиной
      await page.goto('/#/config', { timeout: T.page, waitUntil: 'domcontentloaded' });
      const notice = page
        .getByTestId('config-notices')
        .locator(`[data-notice^="routing_select:${proxyId}:"]`);
      await expect(notice, `уведомление выбора группы ${proxyId}`).toHaveCount(1, {
        timeout: T.page
      });
      await expect(notice).toContainText('RT Прокси');
      await expect(notice).toContainText(REJECTED);

      // (8) возврат файла: дрейф снят, уведомление ещё считается в бейдже
      await restore();
      drifted = false;
      await expect
        .poll(async () => (await layerState(page)).drift, {
          message: 'число расхождений вернулось к исходному',
          timeout: T.save,
          intervals: [1000]
        })
        .toBe(before.drift);
      await expect
        .poll(() => configBadge(page), {
          message: `бейдж .nav-badge-config с уведомлением группы ${proxyId}`,
          timeout: T.action
        })
        .toBe(badgeBefore + 1);

      // успешный выбор той же цели снимает уведомление группы и возвращает бейдж
      const sel = await api(page, 'POST', '/api/configlayer/routing/select', {
        group: proxyId,
        member: target
      });
      expect(sel.status, 'выбор после возврата файла').toBe(200);
      await waitSettled(page, proxyId);
      await expect
        .poll(async () => selectNoticeIds(await layerState(page), proxyId).length, {
          message: `уведомление выбора группы ${proxyId} снято`,
          timeout: T.save,
          intervals: [1000]
        })
        .toBe(0);
      expect((await runtimeGroup(page, proxyId)).current, `current группы ${proxyId}`).toBe(target);
      await expect(notice).toHaveCount(0, { timeout: T.action });
      await expect
        .poll(() => configBadge(page), {
          message: 'бейдж .nav-badge-config вернулся к исходному',
          timeout: T.action
        })
        .toBe(badgeBefore);
    } finally {
      // стенд возвращается и после падения: исходные байты файла Xray, копия удалена
      if (drifted) await restore();
    }
    await guard.assertClean();
  }
);
