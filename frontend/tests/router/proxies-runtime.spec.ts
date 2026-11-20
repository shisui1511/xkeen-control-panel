// e2e-pages: proxies config/groups config/cores
// router-area: routing
import { execFile } from 'node:child_process';
import { readFileSync } from 'node:fs';
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
  nodeLink,
  randomPort,
  resetDraft,
  waitApplyIdle,
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

// Страница «Прокси» со слоем «Конфигурация» на настоящей панели (фаза 150, план 12):
// группы панели и строка «Сейчас», выбор узла плиткой без черновика с живым обновлением второй
// вкладки, пинг группы и общей пачки с отменой, звено цепочки и адрес ?group=. Подготовка одна на
// спек: три узла «Моих узлов» с портами, на которых сервер не слушает (замер падает предсказуемо;
// узел TUIC — только для Mihomo), ручная группа «RT Прокси» и вложенная в «RT Внешняя», взятие
// маршрутизации и «Применить». В конце стенд возвращается: группы и узлы удаляются, маршрутизация
// отпускается, если не была взята, черновик сбрасывается. Ответы сети не подменяются. В сообщения
// провалов попадают только testId и коды, без имён узлов. Сценарий «proxies-runtime no-socket»
// (151-09) в конце файла проверяет подсказку для своего профиля Mihomo без Unix-сокета.

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
  // сброс черновика, узлы, две группы, взятие маршрутизации и «Применить» с перезапуском ядра
  test.setTimeout(T.save * 8);
  await withPanel(browser, (page) => resetDraft(page));
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
    await withPanel(browser, async (page) => {
      await waitApplyIdle(page, 'перед сбросом черновика');
      await resetDraft(page);
    });
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
    // Предел проверки: на роутере поток обычно открывается через секунды после первого чтения
    // вида, и событие открытия приходит уже после загрузки, поэтому вид перечитывается и без
    // исправления. Гонку без подмены сети (page.route запрещён) не воспроизвести: зелёная
    // проверка ловит регрессию лишь при случайном порядке и не доказывает исправление.
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
// файла лежат в копии на роутере, путь файла — рядом с копией; файл возвращается до снимка
// слоя следующего прогона и в finally теста. Ответы сети не подменяются.

const run = promisify(execFile);
const REPO_ROOT = fileURLToPath(new URL('../../../', import.meta.url));
/** Файл в каталоге конфигов Xray: только такой путь подставляется в команду на стенде. */
const XRAY_FILE = /^\/opt\/etc\/xray\/configs\/[A-Za-z0-9._-]+$/;
const XRAY_DIR_PREFIX = '/opt/etc/xray/configs/';
const DRIFT_BAK = '/tmp/rtpx-drift.bak';
/** Путь файла с дрейфом теста: пишется до правки файла, чтобы прерванный прогон знал, что вернуть. */
const DRIFT_PATH = `${DRIFT_BAK}.path`;
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

/** Состояние файла path в снимке слоя (нет файла — пустая строка). */
function fileState(s: LayerStateView, path: string): string {
  return s.files.find((x) => x.path === path)?.state ?? '';
}

/**
 * Путь, который допустимо подставить в команду стенда: целиком по шаблону XRAY_FILE (тот же
 * алфавит, ровно один уровень в каталоге конфигов Xray), не «.»/«..» и не файл xcp-groups.
 */
function isDriftPath(path: string): boolean {
  if (!XRAY_FILE.test(path)) return false;
  const base = path.slice(XRAY_DIR_PREFIX.length);
  return base !== '.' && base !== '..' && !base.includes('xcp-groups');
}

/** Проверенный путь в одинарных кавычках для команды стенда; иной путь — ошибка до ssh. */
function quotedDriftPath(path: string): string {
  if (!isDriftPath(path)) throw new Error('фоновый отказ: путь файла Xray вне шаблона каталога');
  return `'${path}'`;
}

/**
 * Функция same команд стенда: побайтовое равенство двух обычных файлов (не ссылок). Есть cmp —
 * сравнение им, нет — по md5sum. Непрочитанный файл, пустая или не шестнадцатеричная сумма в
 * обоих путях дают «не равны».
 */
const SAME_SH = [
  'same() {',
  '  for x in "$1" "$2"; do [ -f "$x" ] && [ ! -L "$x" ] && [ -r "$x" ] || return 1; done',
  '  if command -v cmp >/dev/null 2>&1; then cmp -s "$1" "$2"; return; fi',
  '  s1=$(md5sum < "$1") && s2=$(md5sum < "$2") || return 1',
  '  s1=${s1%% *}; s2=${s2%% *}',
  '  for s in "$s1" "$s2"; do',
  '    case "$s" in "" | *[!0-9a-fA-F]*) return 1 ;; esac',
  '    [ "${#s}" = 32 ] || return 1',
  '  done',
  '  [ "$s1" = "$s2" ]',
  '}'
].join('\n');

/**
 * Команда дрейфа на стенде: сначала путь файла (атомарно, через временный файл и mv), затем
 * копия исходных байтов (так же), затем дописанный перевод строки. Новые файлы — с umask 077.
 * Остаток прошлого прогона (копия, путь, их временные файлы) — отказ 4 без записи. Параметры
 * meta и append воспроизводят состояния прерванного прогона: без пути и без правки файла.
 */
function driftCmd(path: string, opts: { meta?: boolean; append?: boolean } = {}): string {
  const { meta = true, append = true } = opts;
  return [
    'umask 077',
    `f=${quotedDriftPath(path)}; b='${DRIFT_BAK}'; p='${DRIFT_PATH}'`,
    '[ -f "$f" ] && [ ! -L "$f" ] && [ -r "$f" ] || exit 4',
    'for x in "$b" "$p" "$b.tmp" "$p.tmp"; do [ ! -e "$x" ] && [ ! -L "$x" ] || exit 4; done',
    ...(meta
      ? ['printf "%s" "$f" > "$p.tmp" && mv "$p.tmp" "$p" || { rm -f "$p.tmp"; exit 5; }']
      : []),
    'cp "$f" "$b.tmp" && mv "$b.tmp" "$b" || { rm -f "$b.tmp"; exit 5; }',
    ...(append ? ['printf "\\n" >> "$f" || exit 5'] : []),
    'exit 0'
  ].join('\n');
}

/**
 * Команда возврата на стенде. candidates — управляемые панелью файлы Xray (любое состояние, в
 * том числе drift_modified), каждый проверен isDriftPath до подстановки. Содержимое .path —
 * только данные: читается удалённым shell в переменную и сравнивается с кандидатами целиком,
 * в команду не подставляется. Файл возвращается из копии, только если его байты равны копии
 * или копии плюс ровно перевод строки; копия без .path — только при единственном таком файле.
 * Коды: 0 — вернуто или нечего возвращать; 4 — отказ без записи файлов (ссылка, неверный
 * путь, чужие байты, ноль или несколько совпадений), копия и путь остаются; 5 — сбой записи.
 */
function recoverCmd(candidates: string[]): string {
  const list = candidates.map(quotedDriftPath);
  const alts = list.length > 0 ? list.join(' | ') : "''";
  const words = list.length > 0 ? list.join(' ') : "''";
  return [
    'umask 077',
    `b='${DRIFT_BAK}'; p='${DRIFT_PATH}'; t='${DRIFT_BAK}.chk'`,
    SAME_SH,
    'valid() {',
    `  case "$1" in '${XRAY_DIR_PREFIX}'*) ;; *) return 1 ;; esac`,
    `  nm=\${1#'${XRAY_DIR_PREFIX}'}`,
    '  case "$nm" in "" | . | .. | *[!A-Za-z0-9._-]* | *xcp-groups*) return 1 ;; esac',
    `  case "$1" in ${alts}) ;; *) return 1 ;; esac`,
    '  [ -f "$1" ] && [ ! -L "$1" ]',
    '}',
    'mine() { valid "$1" && { same "$b" "$1" || same "$t" "$1"; }; }',
    'out() { rm -f "$t"; exit "$1"; }',
    'for x in "$b" "$p" "$t"; do [ ! -L "$x" ] || exit 4; done',
    // недописанные временные файлы своего же внесения дрейфа
    'rm -f "$b.tmp" "$p.tmp" || exit 5',
    'if [ ! -e "$b" ]; then',
    // путь без копии: конфиг не трогается, удаляются только свои метаданные
    '  [ ! -e "$p" ] || { [ -f "$p" ] || exit 4; rm -f "$p" || exit 5; }',
    '  exit 0',
    'fi',
    '[ -f "$b" ] && [ -r "$b" ] || exit 4',
    'rm -f "$t" && cp "$b" "$t" && printf "\\n" >> "$t" || out 5',
    'if [ -e "$p" ]; then',
    '  [ -f "$p" ] && [ -r "$p" ] || out 4',
    // точка сохраняет хвостовые переводы строки: путь сравнивается побайтно
    '  f=$(cat "$p" && printf .) || out 4',
    '  f=${f%.}',
    '  mine "$f" || out 4',
    'else',
    '  f=""; k=0',
    `  for c in ${words}; do`,
    '    if mine "$c"; then k=$((k + 1)); f=$c; fi',
    '  done',
    '  [ "$k" = 1 ] || out 4',
    'fi',
    'cp "$b" "$f" && same "$b" "$f" || out 5',
    'rm -f "$b" "$p" || out 5',
    'out 0'
  ].join('\n');
}

/**
 * Возврат файла прерванного прогона: до снимка слоя и выбора файла дрейфа, в finally теста.
 * Реестр управляемых файлов читается отдельным запросом: состояние ok не требуется — файл
 * прерванного прогона в drift_modified. Возвращает код команды стенда (см. recoverCmd).
 */
async function recoverDrift(page: Page): Promise<number> {
  const res = await api(page, 'GET', '/api/configlayer/state');
  expect(res.status, 'реестр файлов слоя для возврата прерванного прогона').toBe(200);
  const files = (res.data?.files ?? []) as LayerStateView['files'];
  const paths = files
    .filter((x) => x.kernel === 'xray' && x.owner === 'panel' && x.state !== 'released')
    .map((x) => x.path ?? '')
    .filter(isDriftPath);
  return standSh(recoverCmd([...new Set(paths)].sort()));
}

/** recoverDrift с ошибкой на любой код, кроме 0. */
async function recoverStand(page: Page): Promise<void> {
  const code = await recoverDrift(page);
  if (code === 4) {
    throw new Error(
      'фоновый отказ: копия прерванного прогона не сопоставлена однозначно с файлом Xray — ' +
        'чужие байты не перезаписаны, копия и путь оставлены (код 4)'
    );
  }
  if (code !== 0) throw new Error(`фоновый отказ: файл Xray не возвращён (код ${code})`);
}

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * body, затем back в любом случае. Сбой back не прячет сбой body и наоборот: при обоих —
 * одна ошибка с обоими сообщениями; при одном — он сам.
 */
async function withStandReturn(
  body: () => Promise<void>,
  back: () => Promise<void>
): Promise<void> {
  let failed = false;
  let failure: unknown;
  try {
    await body();
  } catch (e) {
    failed = true;
    failure = e;
  }
  try {
    await back();
  } catch (e) {
    throw new Error(
      failed
        ? `${errText(e)}\nисходный сбой сценария: ${errText(failure)}`
        : `возврат стенда: ${errText(e)}`,
      { cause: e }
    );
  }
  if (failed) throw failure;
}

/**
 * Управляемый панелью файл Xray в каталоге конфигов, кроме xcp-groups, в состоянии ok (как
 * rtRtDriftFile Go-сценария). Путь проверяется шаблоном каталога Xray до подстановки в команду.
 */
function driftFile(s: LayerStateView): string {
  const f = s.files.find(
    (x) =>
      x.kernel === 'xray' && x.owner === 'panel' && x.state === 'ok' && isDriftPath(x.path ?? '')
  );
  if (!f) {
    throw new Error('фоновый отказ: нет управляемого файла Xray вне xcp-groups в состоянии ok');
  }
  const path = f.path ?? '';
  if (!XRAY_FILE.test(path)) {
    throw new Error('фоновый отказ: путь файла Xray вне шаблона каталога');
  }
  return path;
}

// Команды проверки состояний прерванного прогона: 0 — наблюдение подтверждено.
/** Путь записан рядом с копией: обычный файл, побайтно равный пути файла с дрейфом. */
function metaKeptCmd(path: string): string {
  return [
    `f=${quotedDriftPath(path)}; b='${DRIFT_BAK}'; p='${DRIFT_PATH}'`,
    '[ -f "$b" ] && [ ! -L "$b" ] && [ -f "$p" ] && [ ! -L "$p" ] || exit 1',
    'm=$(cat "$p" && printf .) || exit 1',
    '[ "$m" = "$f." ]'
  ].join('\n');
}

/** Ни копии, ни пути, ни их временных файлов. */
const NO_LEFTOVERS_CMD = [
  `b='${DRIFT_BAK}'; p='${DRIFT_PATH}'`,
  'for x in "$b" "$p" "$b.chk" "$b.tmp" "$p.tmp"; do [ ! -e "$x" ] && [ ! -L "$x" ] || exit 1; done'
].join('\n');

/** Чужая правка: к файлу дописаны два перевода строки — не копия и не копия плюс один. */
function foreignCmd(path: string): string {
  return `f=${quotedDriftPath(path)}; [ -f "$f" ] && [ ! -L "$f" ] || exit 4; printf "\\n\\n" >> "$f"`;
}

/** Отказ ничего не записал: копия (и путь, если meta) на месте, файл — копия плюс два LF. */
function foreignKeptCmd(path: string, meta: boolean): string {
  return [
    'umask 077',
    `f=${quotedDriftPath(path)}; b='${DRIFT_BAK}'; p='${DRIFT_PATH}'; e='${DRIFT_BAK}.exp'`,
    SAME_SH,
    '[ -f "$b" ] || exit 1',
    meta ? '[ -f "$p" ] || exit 1' : '[ ! -e "$p" ] || exit 1',
    'rm -f "$e" && cp "$b" "$e" && printf "\\n\\n" >> "$e" || { rm -f "$e"; exit 5; }',
    'same "$e" "$f"; r=$?; rm -f "$e"; exit "$r"'
  ].join('\n');
}

/**
 * Возврат известного исходника после сознательной чужой правки: копию сделал сам сценарий
 * перед правкой, поэтому байты файла не сверяются. Копии нет — файл не трогается.
 */
function knownBackCmd(path: string): string {
  return [
    `f=${quotedDriftPath(path)}; b='${DRIFT_BAK}'; p='${DRIFT_PATH}'`,
    '[ -e "$b" ] || { rm -f "$p"; exit 0; }',
    '[ -f "$b" ] && [ ! -L "$b" ] && [ -f "$f" ] && [ ! -L "$f" ] || exit 4',
    'cp "$b" "$f" && rm -f "$b" "$p"'
  ].join('\n');
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

    // (0) WR-01: файл прерванного прогона возвращается по пути из метаданных копии до снимка
    // слоя и выбора файла дрейфа — иначе свой файл в drift_modified не выбирается и стенд
    // остаётся с дрейфом. Чужие байты и неоднозначная старая копия — отказ без записи.
    await recoverStand(page);

    // (1) файл дрейфа и исходное число расхождений
    const before = await layerState(page);
    expect(selectNoticeIds(before, proxyId), `уведомления выбора группы ${proxyId}`).toEqual([]);
    const path = driftFile(before);
    // возврат стенда — один помощник: файл из копии, затем исходное число расхождений
    const returnStand = async (): Promise<void> => {
      await recoverStand(page);
      await expect
        .poll(async () => (await layerState(page)).drift, {
          message: 'число расхождений вернулось к исходному',
          timeout: T.save,
          intervals: [1000]
        })
        .toBe(before.drift);
    };

    // (1а)–(1г) состояния прерванного прогона на настоящих файлах стенда: каждое заканчивается
    // файлом в ok и исходным числом расхождений, проверяется тот же recoverDrift, что в (0)
    const sh = async (cmd: string, what: string): Promise<void> => {
      expect(await standSh(cmd), what).toBe(0);
    };
    const settled = async (what: string): Promise<void> => {
      await expect
        .poll(
          async () => {
            const s = await layerState(page);
            return `${fileState(s, path)}/${s.drift}`;
          },
          { message: `${what}: файл в ok, число расхождений исходное`, timeout: T.save }
        )
        .toBe(`ok/${before.drift}`);
    };
    const driftSeen = async (what: string): Promise<void> => {
      await expect
        .poll(async () => fileState(await layerState(page), path), {
          message: `${what}: слой видит дрейф файла`,
          timeout: T.save
        })
        .toBe('drift_modified');
    };
    await withStandReturn(async () => {
      // (1а) прерванный прогон: копия, путь и перевод строки — файл в drift_modified,
      // возврат по сохранённому пути
      await sh(driftCmd(path), '(1а) дрейф внесён');
      await sh(metaKeptCmd(path), '(1а) путь записан рядом с копией');
      await driftSeen('(1а)');
      expect(await recoverDrift(page), '(1а) возврат по сохранённому пути').toBe(0);
      await sh(NO_LEFTOVERS_CMD, '(1а) копия и путь удалены');
      await settled('(1а)');

      // (1б) файл уже с исходными байтами, копия и путь остались — остаток удаляется
      await sh(driftCmd(path, { append: false }), '(1б) копия и путь без правки файла');
      expect(await recoverDrift(page), '(1б) остаток при уже возвращённом файле').toBe(0);
      await sh(NO_LEFTOVERS_CMD, '(1б) остаток удалён');
      await settled('(1б)');

      // (1в) старая копия без пути: единственный управляемый файл с её байтами плюс LF
      await sh(driftCmd(path, { meta: false }), '(1в) старая копия без пути и дрейф');
      await driftSeen('(1в)');
      expect(await recoverDrift(page), '(1в) возврат по единственному совпадению').toBe(0);
      await sh(NO_LEFTOVERS_CMD, '(1в) копия удалена');
      await settled('(1в)');

      // (1г) чужие байты: отказ 4 без записи — с путём и без него (ни одного совпадения);
      // известный исходник возвращается в finally самого состояния
      await withStandReturn(
        async () => {
          await sh(driftCmd(path, { append: false }), '(1г) копия и путь');
          await sh(foreignCmd(path), '(1г) чужая правка файла');
          expect(await recoverDrift(page), '(1г) отказ при чужих байтах').toBe(4);
          await sh(foreignKeptCmd(path, true), '(1г) байты, копия и путь сохранены');
          await sh(`rm -f '${DRIFT_PATH}'`, '(1г) путь убран: старая копия');
          expect(await recoverDrift(page), '(1г) отказ без совпадений').toBe(4);
          await sh(foreignKeptCmd(path, false), '(1г) байты и копия сохранены');
        },
        async () => {
          const code = await standSh(knownBackCmd(path));
          if (code !== 0) throw new Error(`(1г) известный исходник не возвращён (код ${code})`);
        }
      );
      await settled('(1г)');
    }, returnStand);
    // снимок страницы — после возврата файла: бейдж и уведомления без следов (1а)–(1г)
    await openProxies(page);

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

    // стенд возвращается и после падения: исходные байты файла Xray, копия и путь удалены,
    // число расхождений исходное; сбой возврата виден вместе со сбоем сценария
    await withStandReturn(async () => {
      // (3) дрейф: путь файла и копия исходных байтов, затем дописанный перевод строки
      const code = await standSh(driftCmd(path));
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
      await returnStand();
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
    }, returnStand);
    await guard.assertClean();
  }
);

// --- Профиль Mihomo без сокета (151-09, D-13) ---
// Свой профиль пользователя без external-controller-unix: панель его не правит и к Clash API по
// TCP не ходит, а «Прокси», «Подключения» и квоты показывают подсказку «У профиля нет доступа к
// API» с переходом к профилям. Профили rtpx-nosock-* создаются настоящим API из шаблона, строки
// контроллера правятся записью файла (config/save): у одного контроллера нет совсем, у другого
// только TCP — его слушает сам Mihomo, свой сервер тест не поднимает. Подключения к TCP-порту
// считаются на роутере по /proc/net/tcp. Остановленное ядро и профиль панели подсказки не дают.
// В конце Mihomo возвращается прежнему владельцу, профили теста удаляются. В сообщения провалов
// попадают только testId, коды и имена профилей теста — не содержимое YAML.

const NOSOCK_PREFIX = 'rtpx-nosock-';
const NOSOCK_NONE = `${NOSOCK_PREFIX}none`;
const NOSOCK_TCP = `${NOSOCK_PREFIX}tcp`;
const NOSOCK_PARK = `${NOSOCK_PREFIX}park`;
const MIHOMO_PROFILES_DIR = '/opt/etc/mihomo/profiles';
/** Строки контроллера верхнего уровня: TCP и Unix. */
const CONTROLLER_LINES = /^external-controller(?:-unix)?[ \t]*:.*(?:\r?\n|$)/gm;

/** Тексты ключа на обоих языках интерфейса: язык панели стенда заранее неизвестен. */
function localeRe(key: string): RegExp {
  const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const all = ['ru', 'en'].map(
    (lang) =>
      (
        JSON.parse(
          readFileSync(
            fileURLToPath(new URL(`../../src/locales/${lang}.json`, import.meta.url)),
            'utf8'
          )
        ) as Record<string, string>
      )[key]
  );
  return new RegExp(`^(${all.map(esc).join('|')})$`);
}

interface MihomoOwner {
  /** Mihomo ведёт панель (профиль xcp-main). */
  taken: boolean;
  /** Значения секций Mihomo сохранены при отпускании: возврат без сборки заново. */
  saved: boolean;
  /** Активный профиль при включённых профилях, иначе пусто. */
  active: string;
  /** config.yaml — обычный файл XKeen: переход его усыновит. */
  plain: boolean;
  returnTarget: string;
  /** Профили пользователя. */
  users: string[];
}

async function mihomoOwner(page: Page): Promise<MihomoOwner> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const routing = (st.data?.routing ?? {}) as {
    mihomo_saved?: boolean;
    mihomo?: { taken?: boolean } | null;
  };
  const list = await api(page, 'GET', '/api/mihomo/profiles');
  expect(list.status, 'GET /api/mihomo/profiles').toBe(200);
  const p = (list.data ?? {}) as {
    managed?: boolean;
    active?: string;
    plain_config?: unknown;
    return_target?: string;
    profiles?: { name: string; owner: string }[];
  };
  return {
    taken: routing.mihomo?.taken === true,
    saved: routing.mihomo_saved === true,
    active: p.managed === true ? (p.active ?? '') : '',
    plain: p.plain_config != null,
    returnTarget: p.return_target ?? '',
    users: (p.profiles ?? []).filter((x) => x.owner === 'user').map((x) => x.name)
  };
}

/** Признак «нет сокета» обоих источников: capabilities и вид страницы «Прокси». */
async function noSocketState(page: Page): Promise<string> {
  const caps = await api(page, 'GET', '/api/capabilities');
  const view = await api(page, 'GET', '/api/configlayer/proxies');
  const m = (caps.data?.mihomo ?? {}) as { no_socket?: boolean };
  return [
    `caps ${caps.status} no_socket=${m.no_socket === true}`,
    `view ${view.status} ${String(view.data?.state ?? '')} no_socket=${view.data?.no_socket === true}`
  ].join(' | ');
}

const NO_SOCKET_ON = 'caps 200 no_socket=true | view 200 user_profile no_socket=true';

async function expectNoSocketState(page: Page, want: string | RegExp, what: string): Promise<void> {
  await expect
    .poll(() => noSocketState(page), { message: what, timeout: T.save * 2, intervals: [2000] })
    .toMatch(want);
}

/** Ответ текстом: файлы профилей читаются и пишутся без конверта JSON. */
async function rawText(
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

function profilePath(name: string): string {
  return `${MIHOMO_PROFILES_DIR}/${name}.yaml`;
}

async function readProfile(page: Page, name: string): Promise<string> {
  const res = await rawText(
    page,
    'GET',
    `/api/config/read?path=${encodeURIComponent(profilePath(name))}`
  );
  expect(res.status, `чтение профиля ${name}`).toBe(200);
  return res.text;
}

/**
 * Профиль из шаблона с заменёнными строками контроллера: tcpPort — только TCP на loopback,
 * null — контроллера нет. Файл пишется обычной записью «Файлов» с проверкой ядром.
 */
async function createNoSocketProfile(
  page: Page,
  name: string,
  tcpPort: number | null
): Promise<string> {
  const created = await api(page, 'POST', '/api/mihomo/profiles/create', {
    name,
    template: 'rule-based.yaml'
  });
  expect(created.status, `создание профиля ${name}`).toBe(200);
  let text = (await readProfile(page, name)).replace(CONTROLLER_LINES, '');
  if (!text.endsWith('\n')) text += '\n';
  if (tcpPort !== null) text += `external-controller: 127.0.0.1:${tcpPort}\n`;
  const q = encodeURIComponent(profilePath(name));
  const saved = await rawText(page, 'POST', `/api/config/save?path=${q}`, text);
  expect(saved.status, `запись профиля ${name}`).toBe(200);
  const back = await readProfile(page, name);
  expect(back.includes('external-controller-unix'), `у профиля ${name} нет сокета`).toBe(false);
  return back;
}

/** «Сделать активным» настоящим API; занятость ядра и конвейера пережидается. */
async function activateProfile(page: Page, name: string, restoring = false): Promise<void> {
  await resetDraft(page);
  await waitApplyIdle(page, `перед активацией ${name}`);
  const transient = restoring
    ? ['kernel_op_in_progress', 'apply_busy', 'draft_open']
    : ['kernel_op_in_progress', 'apply_busy'];
  const busy = (r: { status: number; data: Record<string, unknown> | null }) =>
    r.status === 409 && transient.includes(String(r.data?.code ?? ''));
  let res = await api(page, 'POST', '/api/mihomo/profiles/activate', { name });
  for (let i = 0; i < 30 && busy(res); i++) {
    await page.waitForTimeout(2000);
    res = await api(page, 'POST', '/api/mihomo/profiles/activate', { name });
  }
  expect(res.status, `активация ${name}: код ${String(res.data?.code ?? '')}`).toBe(200);
  expect(res.data?.error, `активация ${name}: ошибка в ответе`).toBeUndefined();
}

/** Mihomo снова у панели: возврат при сохранённых значениях секций, иначе взятие и «Применить». */
async function returnMihomoToPanel(page: Page): Promise<void> {
  await resetDraft(page);
  await waitApplyIdle(page, 'перед возвратом Mihomo панели');
  const cur = await mihomoOwner(page);
  if (cur.taken) return;
  if (cur.saved) {
    const res = await api(page, 'POST', '/api/configlayer/routing/mihomo', { action: 'return' });
    expect(res.status, `возврат Mihomo панели: код ${String(res.data?.code ?? '')}`).toBe(200);
    expect(res.data?.error, 'возврат Mihomo панели: ошибка в ответе').toBeUndefined();
  } else {
    const take = await api(page, 'POST', '/api/configlayer/routing/take', {
      revision: (await routingInfo(page)).revision,
      scope: 'mihomo'
    });
    expect(take.status, 'взятие Mihomo').toBe(200);
    const start = await api(page, 'POST', '/api/configlayer/apply', {});
    expect(start.status, 'применение взятия Mihomo').toBe(200);
  }
  await waitApplyIdle(page, 'после возврата Mihomo панели');
  await expect
    .poll(async () => (await mihomoOwner(page)).taken, {
      message: 'Mihomo у панели',
      timeout: T.save
    })
    .toBe(true);
}

/** Удаляет профили теста; strict — каждый обязан удалиться. */
async function removeNoSocketProfiles(page: Page, strict: boolean): Promise<void> {
  for (const name of (await mihomoOwner(page)).users) {
    // Профиль возврата остаётся на стенде: он цель возврата Mihomo, его удалить нельзя
    if (!name.startsWith(NOSOCK_PREFIX) || name === NOSOCK_PARK) continue;
    const res = await api(page, 'POST', '/api/mihomo/profiles/delete', { name });
    if (strict) {
      expect(res.status, `удаление профиля ${name}: ${String(res.data?.code ?? '')}`).toBe(200);
    }
  }
}

/**
 * Команда стенда по сокетам TCP роутера (IPv4 и IPv6): awk получает строки таблиц и порт
 * контроллера в шестнадцатеричном виде (s = ":XXXX"), program решает код выхода.
 */
function tcpPortCmd(port: number, program: string): string {
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error('порт TCP-контроллера вне диапазона');
  }
  const hex = port.toString(16).toUpperCase().padStart(4, '0');
  return [
    'for f in /proc/net/tcp /proc/net/tcp6; do [ -r "$f" ] && cat "$f"; done |',
    `awk -v s=':${hex}' '${program}'`
  ].join(' ');
}

/** 0 — Mihomo слушает TCP-порт контроллера (локальный адрес, состояние LISTEN). */
function listeningCmd(port: number): string {
  return tcpPortCmd(
    port,
    '$1 ~ /^[0-9]+:$/ && $4 == "0A" && substr($2, length($2) - 4) == s { f = 1 } END { exit f ? 0 : 1 }'
  );
}

/** 0 — ни одного клиентского сокета к порту контроллера (TIME_WAIT тоже считается). */
function noClientsCmd(port: number): string {
  return tcpPortCmd(
    port,
    '$1 ~ /^[0-9]+:$/ && substr($3, length($3) - 4) == s { n++ } END { exit n ? 1 : 0 }'
  );
}

/** Заголовок «Профили» после перехода из подсказки: ниже липкой полосы черновика, но не далеко. */
async function expectProfilesTitleBelowDraftBar(page: Page, where: string): Promise<void> {
  const titleTop = await page
    .getByTestId('profiles-card')
    .locator('.pc-title')
    .evaluate((el) => el.getBoundingClientRect().top);
  const barBottom = await page
    .getByTestId('config-draftbar')
    .evaluate((el) => el.getBoundingClientRect().bottom);
  expect(titleTop, `${where}: заголовок «Профили» ниже полосы черновика`).toBeGreaterThanOrEqual(
    barBottom
  );
  expect(
    titleTop - barBottom,
    `${where}: запас под полосой — шаг, а не лишний экран`
  ).toBeLessThanOrEqual(40);
}

/**
 * Подсказка S3 на странице: заголовок и ссылка «Управлять в панели». На «Квотах» она — плашка
 * над работающей страницей, без заголовка-раздела.
 */
async function expectNoSocketHint(page: Page, hash: string): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  const hint = page.getByTestId('mihomo-no-socket');
  await expect(hint, `подсказка на ${hash}`).toBeVisible({ timeout: T.page });
  if (hash === '#/trafficquotas') {
    await expect(hint.locator('strong')).toHaveText(localeRe('proxies.rt.no_socket_title'));
  } else {
    await expect(hint.getByRole('heading')).toHaveText(localeRe('proxies.rt.no_socket_title'));
  }
  await expect(
    hint.getByRole('link', { name: localeRe('proxies.rt.no_socket_cta') })
  ).toBeVisible();
}

test(
  'proxies-runtime no-socket: свой профиль без сокета — подсказка на трёх страницах, без TCP, возврат профиля',
  {
    tag: CORE_TAG,
    annotation: {
      type: SLOW_ANNOTATION,
      description:
        'активации профилей Mihomo, остановка и запуск ядра и возврат панели: перезапуск Mihomo на каждом шаге'
    }
  },
  async ({ page }) => {
    test.setTimeout(T.save * 20);
    test.skip(RT.core !== 'mihomo', 'подсказка относится к работающему Mihomo');
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
    test.skip(!(await installedKernels(page)).mihomo, 'Mihomo не установлен');
    await removeNoSocketProfiles(page, false);
    const before = await mihomoOwner(page);
    test.skip(before.plain, 'config.yaml — обычный файл: переход его усыновит');
    // Профиль, на который Mihomo встанет перед возвратом панели: иначе целью возврата
    // останется профиль теста, и его нельзя удалить
    // Своего профиля на стенде может не быть: тогда тест заводит такой сам и оставляет, потому
    // что он становится целью возврата Mihomo
    let park = before.users.includes(before.returnTarget)
      ? before.returnTarget
      : (before.users[0] ?? '');
    const initial = await noSocketState(page);

    // Профиль панели (или прежний свой профиль с сокетом) подсказки не даёт
    if (!initial.includes('no_socket=true')) {
      await page.goto('/#/proxies', { timeout: T.page, waitUntil: 'domcontentloaded' });
      await expect(page.getByTestId('proxies-layer-page')).toBeVisible({ timeout: T.page });
      await expect(page.locator('.main-content .skeleton-card')).toHaveCount(0, {
        timeout: T.page
      });
      await expect(page.getByTestId('mihomo-no-socket')).toHaveCount(0);
    }

    const tcpPort = randomPort();
    await withStandReturn(
      async () => {
        if (before.taken && park === '') {
          const parked = await api(page, 'POST', '/api/mihomo/profiles/create', {
            name: NOSOCK_PARK,
            template: 'rule-based.yaml'
          });
          expect(parked.status, `создание профиля ${NOSOCK_PARK}`).toBe(200);
          park = NOSOCK_PARK;
        }
        const noneBytes = await createNoSocketProfile(page, NOSOCK_NONE, null);
        const tcpBytes = await createNoSocketProfile(page, NOSOCK_TCP, tcpPort);

        // Контроллера нет совсем: признак на сервере и подсказка на «Прокси»
        await activateProfile(page, NOSOCK_NONE);
        await expectNoSocketState(page, NO_SOCKET_ON, `${NOSOCK_NONE}: признак no_socket`);
        expect(await readProfile(page, NOSOCK_NONE), 'активация не правит профиль').toBe(noneBytes);
        let guard = attachConsoleGuard(page);
        await expectNoSocketHint(page, '#/proxies');
        await guard.assertClean();

        // Только TCP: Mihomo слушает порт, панель к нему не подключается
        await activateProfile(page, NOSOCK_TCP);
        await expectNoSocketState(page, NO_SOCKET_ON, `${NOSOCK_TCP}: признак no_socket`);
        expect(await readProfile(page, NOSOCK_TCP), 'активация не правит профиль').toBe(tcpBytes);
        await expect
          .poll(() => standSh(listeningCmd(tcpPort)), {
            message: 'Mihomo слушает TCP-контроллер профиля',
            timeout: T.save,
            intervals: [2000]
          })
          .toBe(0);
        expect(await standSh(noClientsCmd(tcpPort)), 'к TCP-контроллеру нет подключений').toBe(0);

        guard = attachConsoleGuard(page);
        await expectNoSocketHint(page, '#/proxies');
        await expect(page.getByTestId('proxies-runtime-notice')).toHaveCount(0);
        await expectNoSocketHint(page, '#/connections');
        await expect(page.locator('.main-content .conn-toolbar')).toHaveCount(0);
        await expectNoSocketHint(page, '#/trafficquotas');
        // Ссылка ведёт к профилям: вкладка ядер «Конфигурации», активен профиль теста
        await page
          .getByTestId('mihomo-no-socket')
          .getByRole('link', { name: localeRe('proxies.rt.no_socket_cta') })
          .click();
        await expect
          .poll(() => page.evaluate(() => window.location.hash), { timeout: T.page })
          .toBe('#/config/cores?focus=profiles');
        await expect(page.getByTestId('profiles-card')).toBeVisible({ timeout: T.page });
        await expectProfilesTitleBelowDraftBar(page, 'десктоп');
        await expect(page.getByTestId(`profile-row-${NOSOCK_TCP}`)).toHaveAttribute(
          'data-active',
          'true',
          { timeout: T.page }
        );
        // Телефон: подсказка на «Прокси», переход к «Профилям», заголовок так же ниже полосы
        await page.setViewportSize({ width: 390, height: 844 });
        await expectNoSocketHint(page, '#/proxies');
        await page
          .getByTestId('mihomo-no-socket')
          .getByRole('link', { name: localeRe('proxies.rt.no_socket_cta') })
          .click();
        await expect
          .poll(() => page.evaluate(() => window.location.hash), { timeout: T.page })
          .toBe('#/config/cores?focus=profiles');
        await expect(page.getByTestId('profiles-card')).toBeVisible({ timeout: T.page });
        await expectProfilesTitleBelowDraftBar(page, 'телефон');
        await page.setViewportSize({ width: 1280, height: 900 });
        await guard.assertClean();
        // Страницы постояли открытыми: за это время ни одного подключения по TCP
        expect(
          await standSh(noClientsCmd(tcpPort)),
          'после обхода страниц к TCP-контроллеру нет подключений'
        ).toBe(0);

        // Остановленное ядро — свой вид «Прокси», не подсказка
        const stop = await api(page, 'POST', '/api/service/control?action=stop');
        expect(stop.status, 'остановка службы').toBe(200);
        await expect
          .poll(async () => (await runningKernels(page)).length, {
            message: 'ядро остановлено',
            timeout: T.save * 3,
            intervals: [2000]
          })
          .toBe(0);
        await expectNoSocketState(
          page,
          /no_socket=false \| view 200 stopped no_socket=false$/,
          'остановленное ядро снимает признак'
        );
        guard = attachConsoleGuard(page);
        await page.goto('/#/proxies', { timeout: T.page, waitUntil: 'domcontentloaded' });
        await expect(page.getByTestId('proxies-runtime-notice')).toHaveAttribute(
          'data-kind',
          'stopped',
          { timeout: T.page }
        );
        await expect(page.getByTestId('mihomo-no-socket')).toHaveCount(0);
        await page.goto('/#/connections', { timeout: T.page, waitUntil: 'domcontentloaded' });
        await expect(page.locator('.main-content .empty-state').first()).toBeVisible({
          timeout: T.page
        });
        // Признаки ядер страница берёт из capabilities, их опрос идёт раз в 10 с
        await expect(page.getByTestId('mihomo-no-socket')).toHaveCount(0, { timeout: T.page });
        await guard.assertClean();
        const start = await api(page, 'POST', '/api/service/control?action=start');
        expect(start.status, 'запуск службы').toBe(200);
        await expectNoSocketState(page, NO_SOCKET_ON, 'после запуска признак вернулся');
      },
      async () => {
        await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
        if ((await runningKernels(page)).length === 0) {
          await api(page, 'POST', '/api/service/control?action=start');
        }
        const cur = await mihomoOwner(page);
        if (before.taken) {
          if (!cur.taken) {
            if (cur.active !== park) await activateProfile(page, park, true);
            await returnMihomoToPanel(page);
          }
        } else if (before.active !== '' && cur.active !== before.active) {
          await activateProfile(page, before.active, true);
        }
        await removeNoSocketProfiles(page, true);
      }
    );

    // Профиль вернулся: признак прежний, на «Прокси» подсказки нет
    await expectNoSocketState(
      page,
      new RegExp(`^${initial.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`),
      'признак вернулся к исходному'
    );
    await expect
      .poll(async () => (await runningKernels(page)).join(','), {
        message: 'ядро вернулось в рабочее состояние',
        timeout: T.save * 4,
        intervals: [3000]
      })
      .toBe('mihomo');
    if (!initial.includes('no_socket=true')) {
      const guard = attachConsoleGuard(page);
      await page.goto('/#/proxies', { timeout: T.page, waitUntil: 'domcontentloaded' });
      await expect(page.getByTestId('proxies-layer-page')).toBeVisible({ timeout: T.page });
      await expect(page.locator('.main-content .skeleton-card')).toHaveCount(0, {
        timeout: T.page
      });
      await expect(page.getByTestId('mihomo-no-socket')).toHaveCount(0);
      await guard.assertClean();
    }
  }
);
