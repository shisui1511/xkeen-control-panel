// e2e-pages: config/lists
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { api, layerFlag, resetDraft, withPanel } from './lib/nodes-api';

// Верх вкладки «Списки» (менеджер geodata XKeen, фаза 146.1, план 12): «Обновить сейчас» с
// настоящим xkeen -ug, живой вывод, расписание, задача cron XKeen, блок «Загрузка» и диалог
// записи с diff. Ответы сети не подменяются. Спек изменяющий: расписание, gh_proxy и cron
// возвращаются прежними в afterEach, флаг слоя — в afterAll. Ядро не читается и не меняется.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

const BASE = '/api/configlayer/geodata';
// Предел ожидания настоящего xkeen -ug: скачивание всех баз на роутере идёт минутами
const RUN_LIMIT_MS = 20 * 60_000 * RT.slow;

interface Snapshot {
  xkeen: { installed: boolean; user_geodata: boolean };
  download: { gh_proxy: string; jq: boolean; geofile_update: boolean };
  schedule: {
    enabled: boolean;
    day: number;
    hour: number;
    minute: number;
    suspended: boolean;
    now: string;
    next?: string;
  };
  cron: { found: boolean; adoptable: boolean; two: boolean; kept: boolean; schedule?: string };
  run: { running: boolean; waiting_apply: boolean; job?: string };
  last_run?: { at: string; outcome: string };
  sources: SnapSource[];
  draft?: { file: string; mode: string }[];
}

interface SnapSource {
  id: string;
  file: string;
  origin: string;
  kind: string;
  present: boolean;
  entry: boolean;
  set_id?: string;
  url?: string;
  pending?: boolean;
  dup?: boolean;
  draft?: string;
}

async function openLists(page: Page): Promise<void> {
  await page.goto('/#/config/lists', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  const tab = page.getByTestId('lists-tab');
  await expect(tab).toBeVisible({ timeout: T.page });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
}

async function snapshot(page: Page): Promise<Snapshot> {
  const res = await api(page, 'GET', BASE);
  expect(res.status, `GET ${BASE}`).toBe(200);
  return res.data as unknown as Snapshot;
}

/** Карточка «Обновление баз» после конца запуска не держит «Идёт обновление баз…». */
async function expectRunStatusGone(page: Page): Promise<void> {
  await expect(
    page
      .getByTestId('geo-run-status')
      .filter({ hasText: /Идёт обновление баз|Updating databases/ }),
    'карточка держит «Идёт обновление баз…» после конца запуска'
  ).toHaveCount(0, { timeout: 15_000 });
  await expect(page.getByTestId('geo-run-now')).toBeEnabled({ timeout: 15_000 });
}

test('lists-actions: обновить сейчас', async ({ page }) => {
  test.setTimeout(RUN_LIMIT_MS + 5 * 60_000);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openLists(page);

  const before = await snapshot(page);
  test.skip(!before.xkeen.installed, 'на стенде нет XKeen: xkeen -ug запустить нечем');
  expect(before.run.running, 'на входе в сценарий запуск не идёт').toBe(false);
  const started = Date.now();

  // кнопка просит подтверждение: перезапуск ядра нельзя запустить одним кликом
  await page.getByTestId('geo-run-now').click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible({ timeout: T.action });
  await expect(dialog).toContainText(/xkeen -ug/);
  await dialog.getByRole('button', { name: /Обновить базы|Update databases/ }).click();

  // идёт настоящий запуск: полоса статуса видна, вывод раскрыт и наполняется
  await expect(page.getByTestId('geo-run-status')).toBeVisible({ timeout: T.action });
  const output = page.getByTestId('geo-output');
  await expect(output).toBeVisible({ timeout: T.action });
  await expect(output).not.toHaveText('', { timeout: T.network + T.action });

  // второй запуск и «Применить» во время -ug отклоняются замком менеджера
  const busy = await api(page, 'POST', '/api/configlayer/apply');
  expect(busy.status, 'POST /api/configlayer/apply во время -ug').toBe(409);
  expect(busy.data?.code).toBe('geodata_busy');

  // вывод переживает перезагрузку страницы: блок раскрывается сам и снова непуст
  await page.reload({ timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('lists-tab')).toBeVisible({ timeout: T.page });
  const afterReload = await snapshot(page);
  if (afterReload.run.running) {
    await expect(page.getByTestId('geo-output')).toBeVisible({ timeout: T.page });
  } else {
    // запуск успел закончиться до перезагрузки: вывод открывается кнопкой
    await page.getByTestId('geo-output-toggle').click();
  }
  await expect(page.getByTestId('geo-output')).not.toHaveText('', { timeout: T.page });

  // ждём конца запуска по снимку менеджера
  await expect
    .poll(async () => (await snapshot(page)).run.running, {
      timeout: RUN_LIMIT_MS,
      intervals: [3_000]
    })
    .toBe(false);

  // конец запуска виден на карточке без перезагрузки страницы
  await expectRunStatusGone(page);

  const after = await snapshot(page);
  expect(after.last_run, 'после запуска есть итог последнего запуска').toBeTruthy();
  expect(Date.parse(after.last_run?.at ?? '')).toBeGreaterThanOrEqual(started - 60_000);
  await expect(page.getByTestId('geo-last-run')).toContainText(/Последний запуск|Last run/, {
    timeout: T.action
  });
  await expect(page.getByTestId('geo-run-now')).toBeEnabled({ timeout: T.action });

  await guard.assertClean();
});

type Schedule = Snapshot['schedule'];

// Расписание, найденное на стенде: возвращается после сценария, который его меняет
let savedSchedule: Schedule | null = null;

async function restoreSchedule(browser: Browser): Promise<void> {
  const was = savedSchedule;
  savedSchedule = null;
  if (was === null) return;
  await withPanel(browser, async (page) => {
    const res = await api(page, 'POST', `${BASE}/schedule`, {
      enabled: was.enabled,
      day: was.day,
      hour: was.hour,
      minute: was.minute
    });
    expect(res.status, 'возврат расписания').toBe(200);
  });
}

test.describe('расписание и cron', () => {
  test.afterEach(async ({ browser }) => {
    await restoreSchedule(browser);
  });

  test('lists-actions: расписание', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen: расписание хранить нечем');
    savedSchedule = before.schedule;

    // час запуска — через шесть часов от часов роутера (5 часов занят другими задачами стенда)
    const routerHour = Number(/T(\d{2}):/.exec(before.schedule.now)?.[1]);
    expect(Number.isFinite(routerHour), 'в снимке есть время роутера').toBe(true);
    let hour = (routerHour + 6) % 24;
    if (hour === 5) hour = 3;
    const minute = 10;

    const card = page.getByTestId('geo-update-card');
    const toggle = card.getByTestId('geo-sched-enabled');
    if (!(await toggle.isChecked())) {
      await card.locator('label.toggle-label').click();
    }
    await expect(toggle).toBeChecked();
    await card.getByTestId('geo-sched-day').selectOption('-1');
    await card.getByTestId('geo-sched-hour').locator('input').fill(String(hour));
    await card.getByTestId('geo-sched-minute').locator('input').fill(String(minute));

    const save = card.getByTestId('geo-sched-save');
    await expect(save).toBeEnabled({ timeout: T.action });
    await save.click();
    await expect(page.locator('.toast--success').first()).toBeVisible({ timeout: T.action });
    await expect(card.getByTestId('geo-next-run')).toBeVisible({ timeout: T.action });

    const after = await snapshot(page);
    expect(after.schedule.enabled).toBe(true);
    expect(after.schedule.day).toBe(-1);
    expect(after.schedule.hour).toBe(hour);
    expect(after.schedule.minute).toBe(minute);
    // сохранённое значение — не «изменённое»: кнопка снова неактивна
    await expect(save).toBeDisabled({ timeout: T.action });

    await guard.assertClean();
  });

  test('lists-actions: задача cron XKeen', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(
      !before.cron.found || !before.cron.adoptable,
      'на стенде нет задачи cron XKeen со стандартным расписанием'
    );
    // «Перенять расписание» показано, если нет двух расписаний, а расписание панели приостановлено
    // или выключено и владелец не нажимал «Не сейчас»
    const adoptShown =
      !before.cron.two &&
      (before.schedule.suspended || (!before.cron.kept && !before.schedule.enabled));
    test.skip(!adoptShown, 'на стенде уведомление о перенятии расписания cron XKeen не показано');
    savedSchedule = before.schedule;

    const notice = page.getByTestId('geo-cron-notice');
    await expect(notice).toBeVisible({ timeout: T.page });

    await page.getByTestId('geo-cron-adopt').click();
    await expect(notice).toHaveCount(0, { timeout: T.action });
    await expect
      .poll(async () => (await snapshot(page)).cron.found, { timeout: T.action })
      .toBe(false);

    // вернуть задачу в cron XKeen: расписание отдаётся XKeen
    const give = await api(page, 'POST', `${BASE}/cron`, { action: 'give' });
    expect(give.status, 'POST cron give').toBe(200);
    expect((await snapshot(page)).cron.found).toBe(true);

    await guard.assertClean();
  });
});

// Прежний gh_proxy, найденный на стенде: возвращается после сценария, который его меняет
let savedProxy: string | null = null;

/** Записывает gh_proxy тем же путём, что диалог: предпросмотр, затем запись по его отпечатку. */
async function writeProxy(page: Page, value: string): Promise<void> {
  const op = { kind: 'gh_proxy', value };
  const prev = await api(page, 'POST', `${BASE}/preview`, { op });
  expect(prev.status, 'предпросмотр gh_proxy').toBe(200);
  const res = await api(page, 'POST', `${BASE}/write`, {
    op,
    base: prev.data?.base,
    accept_comment_loss: true,
    update_now: false
  });
  expect(res.status, 'запись gh_proxy').toBe(200);
}

test.describe('загрузка', () => {
  test.afterEach(async ({ browser }) => {
    const was = savedProxy;
    savedProxy = null;
    if (was === null) return;
    await withPanel(browser, (page) => writeProxy(page, was));
  });

  test('lists-actions: gh_proxy с diff', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(!before.xkeen.user_geodata, 'XKeen старее 2.0.1: панель gh_proxy не меняет');
    savedProxy = before.download.gh_proxy;
    const value = 'https://example.org';
    test.skip(before.download.gh_proxy === value, 'на стенде уже стоит тот же gh_proxy');

    const block = page.getByTestId('geo-download');
    const save = block.getByTestId('geo-ghproxy-save');
    await expect(save).toBeDisabled();
    await block.getByTestId('geo-ghproxy').locator('input').fill(value);
    await expect(save).toBeEnabled({ timeout: T.action });
    await save.click();

    const modal = page.getByTestId('geo-finish-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('geo-finish-diff')).toContainText(value, { timeout: T.action });
    const submit = modal.getByTestId('geo-finish-submit');
    await expect(submit).toBeEnabled({ timeout: T.action });
    await submit.click();
    // в xkeen.json есть комментарии: запись просит подтвердить их потерю
    const lost = page.getByRole('button', {
      name: /Записать без комментариев|Write without comments/
    });
    if (await lost.isVisible({ timeout: T.action }).catch(() => false)) await lost.click();
    await expect(modal).toBeHidden({ timeout: T.action });

    await expect
      .poll(async () => (await snapshot(page)).download.gh_proxy, { timeout: T.action })
      .toBe(value);

    await guard.assertClean();
  });

  test('lists-actions: jq', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    const block = page.getByTestId('geo-download');

    if (before.download.jq) {
      await expect(block).toContainText(/jq установлен|jq is installed/);
      await expect(block.getByTestId('geo-jq-install')).toHaveCount(0);
    } else {
      await block.getByTestId('geo-jq-install').click();
      await expect(block.getByTestId('geo-jq-output')).toBeVisible({ timeout: T.action });
      await expect(block.getByTestId('geo-jq-output')).not.toHaveText('', {
        timeout: T.network + T.action
      });
    }

    await guard.assertClean();
  });
});

// Стандартный набор, включённый сценарием: возвращается после него. Файла на стенде до
// сценария не было, поэтому убирается обычным путём — удалением через черновик и «Применить»
let enabledFile: string | null = null;

// Лежащий стандартный набор без ссылок, убранный самоподготовкой сценария: возвращается после него
let restoreSet: { file: string; set: string } | null = null;

const STANDARD_SETS = ['refilter_geoip', 'refilter_geosite', 'v2fly_geosite'];

async function removeSource(page: Page, file: string): Promise<void> {
  const sources = await api(page, 'GET', '/api/configlayer/sources');
  const drop = await api(page, 'POST', `${BASE}/drop`, {
    rev: Number(sources.data?.draft_revision ?? 0),
    file,
    mode: 'delete',
    actions: []
  });
  expect(drop.status, `удаление ${file} в черновике`).toBe(200);
  const apply = await api(page, 'POST', '/api/configlayer/apply');
  expect(apply.status, 'применение черновика').toBe(200);
}

/** Возвращает убранный подготовкой набор: файл снова лежит, записи в xkeen.json нет. */
async function restoreStandardSet(page: Page, r: { file: string; set: string }): Promise<void> {
  // запуск -ug или удаление, начатые сценарием, должны закончиться: замок держит запись
  await expect
    .poll(async () => (await snapshot(page)).run.running, {
      timeout: RUN_LIMIT_MS,
      intervals: [3_000]
    })
    .toBe(false);
  let snap = await snapshot(page);
  if ((snap.draft ?? []).length > 0) {
    await resetDraft(page);
    snap = await snapshot(page);
  }
  const row = snap.sources.find((x) => x.file === r.file);
  if (row !== undefined && row.present && !row.entry && row.pending !== true) return;
  if (row === undefined || (!row.present && !row.entry && row.pending !== true)) {
    // ни файла, ни записи: набор включается обычным путём, XKeen скачивает файл
    const op = { kind: 'enable_set', set: r.set };
    const prev = await api(page, 'POST', `${BASE}/preview`, { op });
    expect(prev.status, 'предпросмотр возврата набора').toBe(200);
    const write = await api(page, 'POST', `${BASE}/write`, {
      op,
      base: String(prev.data?.base ?? ''),
      accept_comment_loss: true,
      update_now: true
    });
    if (write.status === 502 && write.data?.code === 'update_failed') {
      // запись сделана, а запуск -ug не начался: запускаем его отдельно
      const rerun = await api(page, 'POST', `${BASE}/run`);
      expect(rerun.status, 'запуск -ug после возврата набора').toBe(200);
    } else {
      expect(write.status, 'запись при возврате набора').toBe(200);
    }
  } else {
    // файла нет, а запись или ожидание есть: сценарий упал после записи
    const rerun = await api(page, 'POST', `${BASE}/run`);
    expect(rerun.status, 'запуск -ug после возврата набора').toBe(200);
  }
  await expect
    .poll(
      async () => {
        const now = (await snapshot(page)).sources.find((x) => x.file === r.file);
        return now !== undefined && now.present && !now.entry && now.pending !== true;
      },
      { timeout: RUN_LIMIT_MS, intervals: [3_000] }
    )
    .toBe(true);
}

/** Убирает набор, включённый сценарием: файла на стенде до него не было. */
async function removeEnabledSet(page: Page, file: string): Promise<void> {
  // запуск -ug, начатый сценарием, должен закончиться: замок держит удаление
  await expect
    .poll(async () => (await snapshot(page)).run.running, {
      timeout: RUN_LIMIT_MS,
      intervals: [3_000]
    })
    .toBe(false);
  const now = (await snapshot(page)).sources.find((x) => x.file === file);
  if (now === undefined || (!now.present && !now.entry)) return;
  await removeSource(page, file);
  await expect
    .poll(
      async () => {
        const row = (await snapshot(page)).sources.find((x) => x.file === file);
        return row !== undefined && (row.present || row.entry);
      },
      { timeout: RUN_LIMIT_MS, intervals: [3_000] }
    )
    .toBe(false);
}

test.describe('включение набора', () => {
  test.afterEach(async ({ browser }) => {
    const file = enabledFile;
    enabledFile = null;
    const restore = restoreSet;
    restoreSet = null;
    if (file === null && restore === null) return;
    await withPanel(browser, async (page) => {
      if (file !== null) await removeEnabledSet(page, file);
      if (restore !== null) await restoreStandardSet(page, restore);
    });
  });

  test('lists-actions: включить стандартный набор', async ({ page }) => {
    // подготовка (убрать лежащий набор) и возможный возврат в afterEach — по запуску на каждую
    test.setTimeout(3 * RUN_LIMIT_MS + 10 * 60_000);
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    let before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(!before.xkeen.user_geodata, 'XKeen старее 2.0.1: панель записи не делает');
    test.skip(!before.download.jq, 'на стенде нет jq: XKeen не читает записи geodata');
    test.skip(!before.download.geofile_update, 'на стенде выключено обновление баз XKeen');
    expect(before.run.running, 'на входе в сценарий запуск не идёт').toBe(false);
    const isFree = (x: SnapSource): boolean =>
      x.origin === 'standard' && !x.present && !x.entry && STANDARD_SETS.includes(x.set_id ?? '');
    let found = before.sources.find(isFree);

    if (found === undefined) {
      // Самоподготовка: все три набора лежат. Убирается лежащий набор без ссылок (ни правил
      // панели, ни сырых правил, ни чужих конфигов), чтобы ни один конфиг ядра не указывал на
      // отсутствующий dat; в конце сценария набор снова лежит
      let spare: SnapSource | undefined;
      for (const c of before.sources.filter(
        (x) =>
          x.origin === 'standard' &&
          x.present &&
          !x.entry &&
          x.pending !== true &&
          !x.draft &&
          STANDARD_SETS.includes(x.set_id ?? '')
      )) {
        const imp = await api(page, 'GET', `${BASE}/impact?file=${encodeURIComponent(c.file)}`);
        const refs = (k: string): number => ((imp.data?.[k] as unknown[] | null) ?? []).length;
        if (
          imp.status === 200 &&
          refs('lists') === 0 &&
          refs('raw') === 0 &&
          refs('foreign') === 0
        ) {
          spare = c;
          break;
        }
      }
      test.skip(
        spare === undefined,
        'на стенде нет стандартного набора без ссылок: самоподготовка невозможна'
      );
      if (spare === undefined) return;
      const spareFile = spare.file;
      restoreSet = { file: spareFile, set: spare.set_id ?? spare.id };
      await removeSource(page, spareFile);
      await expect
        .poll(
          async () => {
            const row = (await snapshot(page)).sources.find((x) => x.file === spareFile);
            return row !== undefined && (row.present || row.entry);
          },
          { timeout: RUN_LIMIT_MS, intervals: [3_000] }
        )
        .toBe(false);
      before = await snapshot(page);
      await openLists(page);
      found = before.sources.find((x) => x.file === spareFile);
      expect(found, 'набор после подготовки не лежит на диске').toBeDefined();
      if (found === undefined) return;
    } else {
      // файла на стенде до сценария не было: сценарий убирает его сам
      enabledFile = found.file;
    }
    const target: SnapSource = found;
    const setId = target.set_id ?? target.id;

    // переключатель выключен; клик открывает диалог с записью geodata в diff
    const row = page.getByTestId(`geo-source-${target.id}`);
    const sw = page.getByTestId(`geo-set-switch-${setId}`);
    await expect(sw).not.toBeChecked();
    await row.locator('label.toggle-label').click();
    const modal = page.getByTestId('geodata-add-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('geodata-add-loading')).toHaveCount(0, { timeout: T.page });
    const diff = modal.getByTestId('geodata-add-diff');
    await expect(diff).toContainText(target.file, { timeout: T.action });
    await expect(diff.locator('.diff-line-added').first()).toBeVisible();
    // клик не включил набор сам: до подтверждения переключатель выключен
    await expect(sw).not.toBeChecked();

    // «Обновить сейчас»: запись и настоящий xkeen -ug
    await modal.getByTestId('geodata-update-now').check();
    const confirm = modal.getByTestId('geodata-add-confirm');
    await expect(confirm).toBeEnabled({ timeout: T.action });
    await confirm.click();
    // в xkeen.json есть комментарии: запись просит подтвердить их потерю
    const lost = page.getByRole('button', {
      name: /Записать без комментариев|Write without comments/
    });
    if (await lost.isVisible({ timeout: T.action }).catch(() => false)) await lost.click();
    await expect(modal).toBeHidden({ timeout: T.network + T.action });

    // файла ещё нет: на строке «ждёт скачивания», переключатель включён
    await expect(page.getByTestId(`geo-source-pending-${target.id}`)).toBeVisible({
      timeout: T.action
    });
    await expect(sw).toBeChecked();

    // XKeen скачал файл, панель убрала временную запись: чип пропал без перезагрузки страницы
    await expect(page.getByTestId(`geo-source-pending-${target.id}`)).toHaveCount(0, {
      timeout: RUN_LIMIT_MS
    });
    await expect
      .poll(
        async () => {
          const row = (await snapshot(page)).sources.find((x) => x.file === target.file);
          return row !== undefined && row.present && !row.entry && row.pending !== true;
        },
        { timeout: RUN_LIMIT_MS, intervals: [3_000] }
      )
      .toBe(true);
    // запуск закончился: карточка «Обновление баз» снимает «Идёт обновление баз…» сама,
    // страница не перезагружалась
    await expect
      .poll(async () => (await snapshot(page)).run.running, {
        timeout: RUN_LIMIT_MS,
        intervals: [3_000]
      })
      .toBe(false);
    await expectRunStatusGone(page);
    await expect(sw).toBeChecked();
    await expect(page.getByTestId(`geo-source-open-${target.id}`)).toBeVisible({
      timeout: T.action
    });

    // в xkeen.json записи с этим файлом не осталось: XKeen дальше обновляет файл сам
    expect(
      (await snapshot(page)).sources.some((x) => x.file === target.file && x.entry),
      'временная запись убрана'
    ).toBe(false);

    await guard.assertClean();
  });
});

test.describe('группы источников', () => {
  test('lists-actions: дубль и группы', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');

    // три группы источников видны: «Стандартные XKeen», «Наборы панели», «Свои»
    const standard = page.getByTestId('geo-group-standard');
    await expect(standard).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('geo-group-panel')).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('geo-group-own')).toBeVisible({ timeout: T.action });
    // шесть переключателей стандартных наборов, по одному на файл
    await expect(standard.locator('fieldset')).toHaveCount(3);
    await expect(standard.locator('input[type="checkbox"]')).toHaveCount(6);
    // строка источника не интерактивна как целое: у <li> нет role
    await expect(standard.locator('li[role]')).toHaveCount(0);

    const dup = before.sources.find((x) => x.dup === true);
    test.skip(dup === undefined, 'на стенде нет записи geodata, дублирующей стандартный набор');
    if (dup === undefined) return;

    // «Убрать дубль» открывает диалог с diff; «Отмена» ничего не пишет
    await page.getByTestId(`geo-source-dedup-${dup.id}`).click();
    const modal = page.getByTestId('geo-finish-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('geo-finish-diff')).toBeVisible({ timeout: T.action });
    await modal.getByTestId('geo-finish-cancel').click();
    await expect(modal).toBeHidden({ timeout: T.action });
    const after = await snapshot(page);
    expect(after.sources.find((x) => x.id === dup.id)?.dup, 'запись не тронута').toBe(true);

    await guard.assertClean();
  });
});

test('lists-actions: выбор списка предлагает включить набор', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openLists(page);
  const before = await snapshot(page);
  test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
  const missing = before.sources.filter(
    (x) => x.origin === 'standard' && x.kind === 'geosite' && !x.present && !x.pending && !x.entry
  );
  const catalog = await api(page, 'GET', '/api/configlayer/routing/lists');
  const extended = (catalog.data?.extended ?? []) as { kind: string; in_xkeen: boolean }[];
  const free = extended.filter((e) => e.kind === 'site' && !e.in_xkeen);

  // вкладка «Правила» → диалог правила → выбор списка для условия geosite
  await page.goto('/#/config/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
  const rules = page.getByTestId('rules-tab');
  await expect(rules).toBeVisible({ timeout: T.page });
  await expect(rules.locator('.skeleton')).toHaveCount(0, { timeout: T.page });
  await page.getByTestId('rules-add').click();
  const rule = page.getByTestId('rule-modal');
  await expect(rule).toBeVisible({ timeout: T.action });
  await rule.getByTestId('rule-cond-type-0').selectOption('geosite');
  await rule.getByTestId('rule-cond-pick-0').click();
  const picker = page.getByTestId('list-picker');
  await expect(picker).toBeVisible({ timeout: T.action });
  await expect(picker.getByTestId('list-picker-loading')).toHaveCount(0, { timeout: T.page });

  // блок «Расширенные наборы»: стандартные наборы XKeen, которых нет на роутере, и ссылка
  const block = picker.getByTestId('list-picker-extended');
  await expect(block).toBeVisible({ timeout: T.action });
  for (const m of missing) {
    await expect(block.getByTestId(`list-std-add-${m.set_id ?? m.id}`)).toBeVisible({
      timeout: T.action
    });
  }
  if (missing.length === 0 && free.length === 0) {
    await expect(block.getByTestId('list-picker-all-enabled')).toBeVisible({ timeout: T.action });
  }
  const link = block.getByTestId('list-picker-manage');
  await expect(link).toBeVisible();
  await expect(link).toHaveAttribute('href', '#/config/lists');

  // ссылка ведёт на вкладку «Списки»
  await link.click();
  await expect(page).toHaveURL(/#\/config\/lists/, { timeout: T.action });
  await expect(page.getByTestId('lists-tab')).toBeVisible({ timeout: T.page });

  await guard.assertClean();
});

// --- Свой источник: «Добавить источник» и «Изменить ссылку» (план 146.1-16) ---

const XKEEN_SETTINGS = '/api/xkeen/settings';
const XKEEN_JSON_KIND = 'xkeen_json';
const OWN_FILE = 'xcp-ui-test.dat';

/** Содержимое xkeen.json стенда; null — файла нет или он не прочитан. */
async function readXkeenJson(page: Page): Promise<string | null> {
  const res = await api(page, 'GET', XKEEN_SETTINGS);
  const list = (Array.isArray(res.data) ? (res.data as unknown) : []) as {
    kind: string;
    content?: string;
  }[];
  return list.find((f) => f.kind === XKEEN_JSON_KIND)?.content ?? null;
}

/** Каталог расширенных наборов: адрес настоящего небольшого файла GitHub (около 68 КБ). */
async function catalogUrl(page: Page): Promise<string> {
  const res = await api(page, 'GET', '/api/configlayer/routing/lists');
  expect(res.status, 'GET /api/configlayer/routing/lists').toBe(200);
  const extended = (res.data?.extended ?? []) as { id: string; url: string }[];
  const set = extended.find((e) => e.id === 'roscomvpn_geosite');
  expect(set?.url, 'в каталоге есть roscomvpn_geosite').toBeTruthy();
  return set?.url ?? '';
}

/**
 * Адрес с неподдерживаемой схемой: проверка закрывает запись блоком scheme_bad без обращения
 * в сеть. Блок html (страница вместо базы) проверяет TestRouterGeodataProbe на сервере
 * loopback роутера: адрес самой панели для этого не годится, она не отдаёт страницу
 * запросу с роутера (403).
 */
const BLOCKED_URL = 'ftp://example.com/xcp-own.dat';

/** Просит подтвердить потерю комментариев в xkeen.json, если запись их стирает. */
async function acceptCommentLoss(page: Page): Promise<void> {
  const lost = page.getByRole('button', {
    name: /Записать без комментариев|Write without comments/
  });
  if (await lost.isVisible({ timeout: T.action }).catch(() => false)) await lost.click();
}

test.describe('свой источник', () => {
  // xkeen.json стенда запоминается до сценария и возвращается после каждого: запись своего
  // источника меняет файл, остальные сценарии должны видеть прежний
  let xkeenJsonBefore: string | null = null;

  test.beforeAll(async ({ browser }) => {
    xkeenJsonBefore = await withPanel(browser, readXkeenJson);
  });

  test.afterEach(async ({ browser }) => {
    const was = xkeenJsonBefore;
    if (was === null) return;
    await withPanel(browser, async (page) => {
      if ((await readXkeenJson(page)) === was) return;
      const res = await api(page, 'POST', `${XKEEN_SETTINGS}/save`, {
        kind: XKEEN_JSON_KIND,
        content: was
      });
      expect(res.status, 'возврат xkeen.json').toBe(200);
    });
  });

  test('lists-actions: свой источник — проверка и запись', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(!before.xkeen.user_geodata, 'XKeen старее 2.0.1: панель записи не делает');
    test.skip(!before.download.jq, 'на стенде нет jq: XKeen не читает записи geodata');
    test.skip(!before.download.geofile_update, 'на стенде выключено обновление баз XKeen');
    expect(
      before.sources.some((x) => x.file === OWN_FILE),
      `на стенде нет записи ${OWN_FILE}`
    ).toBe(false);

    await page.getByTestId('geo-add-source').click();
    const modal = page.getByTestId('geo-add-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    const file = modal.getByTestId('geo-add-file').locator('input');
    const url = modal.getByTestId('geo-add-url').locator('input');
    const check = modal.getByTestId('geo-add-check');
    const results = modal.getByTestId('geo-add-results');
    const submit = modal.getByTestId('geo-add-submit');

    // пока поля пусты, проверка и добавление неактивны; проверка подсказывает, что заполнить
    await expect(check).toBeDisabled();
    await expect(check).toHaveAttribute('title', /.+/);
    await expect(submit).toBeDisabled();
    await file.fill(OWN_FILE);
    await expect(check).toBeDisabled();
    await url.fill(BLOCKED_URL);
    await expect(check).toBeEnabled({ timeout: T.action });
    // без проверки добавить нельзя
    await expect(submit).toBeDisabled();

    // неподдерживаемая схема: блок, запись закрыта
    await check.click();
    const html = results.locator('[data-code="scheme_bad"]');
    await expect(html).toBeVisible({ timeout: T.network + T.action });
    await expect(html).toHaveAttribute('data-level', 'block');
    await expect(submit).toBeDisabled();
    await expect(modal.getByTestId('geo-add-diff')).toHaveCount(0);

    // закрытый порт на loopback роутера: адрес не ответил — предупреждение, не блок
    await url.fill('http://127.0.0.1:9/x.dat');
    // правка поля сбросила результат
    await expect(results.locator('li')).toHaveCount(0);
    await expect(submit).toBeDisabled();
    await check.click();
    const fail = results.locator('[data-code="reach_fail"]');
    await expect(fail).toBeVisible({ timeout: T.network + T.action });
    await expect(fail).toHaveAttribute('data-level', 'warn');
    await expect(fail).toContainText(/Адрес не ответил|did not respond/);
    await expect(results.locator('[data-level="block"]')).toHaveCount(0);
    // блоков нет: запись разрешена, diff показан
    await expect(modal.getByTestId('geo-add-diff')).toBeVisible({ timeout: T.action });
    await expect(submit).toBeEnabled({ timeout: T.action });
    // любая правка полей снова закрывает запись до новой проверки
    await file.fill(`x-${OWN_FILE}`);
    await expect(results.locator('li')).toHaveCount(0);
    await expect(submit).toBeDisabled();
    await file.fill(OWN_FILE);

    // настоящий небольшой файл GitHub из каталога: без блоков, записывается
    await url.fill(await catalogUrl(page));
    await check.click();
    await expect(results.locator('li').first()).toBeVisible({ timeout: T.network + T.action });
    test.skip(
      (await results.locator('[data-code="reach_fail"]').count()) > 0,
      'сеть стенда недоступна: адрес GitHub не ответил'
    );
    await expect(results.locator('[data-code="kind"]')).toContainText(/geosite/, {
      timeout: T.action
    });
    await expect(results.locator('[data-level="block"]')).toHaveCount(0);
    const diff = modal.getByTestId('geo-add-diff');
    await expect(diff).toContainText(OWN_FILE, { timeout: T.action });
    await expect(diff.locator('.diff-line-added').first()).toBeVisible();
    await modal.getByTestId('geo-add-update-later').check();
    await expect(submit).toBeEnabled({ timeout: T.action });
    await submit.click();
    await acceptCommentLoss(page);
    await expect(modal).toBeHidden({ timeout: T.action });

    // в группе «Свои» появилась строка; файла ещё нет, его скачает XKeen по расписанию
    const row = page.getByTestId('geo-group-own').getByTestId(`geo-source-${OWN_FILE}`);
    await expect(row).toBeVisible({ timeout: T.action });
    await expect(row.getByTestId(`geo-source-nofile-${OWN_FILE}`)).toBeVisible();
    const after = await snapshot(page);
    const added = after.sources.find((x) => x.file === OWN_FILE);
    expect(added?.entry, 'запись geodata появилась в xkeen.json').toBe(true);
    expect(added?.origin).toBe('own');

    await guard.assertClean();
  });

  test('lists-actions: изменить ссылку', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(!before.xkeen.user_geodata, 'XKeen старее 2.0.1: панель записи не делает');
    test.skip(!before.download.jq, 'на стенде нет jq: XKeen не читает записи geodata');
    test.skip(!before.download.geofile_update, 'на стенде выключено обновление баз XKeen');
    expect(
      before.sources.some((x) => x.file === OWN_FILE),
      `на стенде нет записи ${OWN_FILE}`
    ).toBe(false);

    // собственная запись тем же путём, что диалог: предпросмотр, затем запись по его отпечатку;
    // адрес — закрытый порт на loopback роутера, проверка его не блокирует
    const startUrl = 'http://127.0.0.1:9/x.dat';
    const op = { kind: 'add', file: OWN_FILE, url: startUrl };
    const prev = await api(page, 'POST', `${BASE}/preview`, { op });
    expect(prev.status, 'предпросмотр добавления').toBe(200);
    const written = await api(page, 'POST', `${BASE}/write`, {
      op,
      base: prev.data?.base,
      accept_comment_loss: true,
      update_now: false
    });
    expect(written.status, 'запись своего источника').toBe(200);
    await openLists(page);

    // у записи в группе «Свои» есть кнопка правки ссылки
    const row = page.getByTestId('geo-group-own').getByTestId(`geo-source-${OWN_FILE}`);
    await expect(row).toBeVisible({ timeout: T.action });
    const edit = row.getByTestId(`geo-source-edit-${OWN_FILE}`);
    await expect(edit).toHaveAttribute('aria-label', /.+/);
    await edit.click();

    // имя файла неактивно, ссылка — прежняя
    const modal = page.getByTestId('geo-url-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId('geo-url-file').locator('input')).toBeDisabled();
    await expect(modal.getByTestId('geo-url-file').locator('input')).toHaveValue(OWN_FILE);
    const url = modal.getByTestId('geo-url-url').locator('input');
    await expect(url).toHaveValue(startUrl);
    const check = modal.getByTestId('geo-url-check');
    const results = modal.getByTestId('geo-add-results');
    const submit = modal.getByTestId('geo-url-submit');
    await expect(submit).toBeDisabled();

    // неподдерживаемая схема: блок, сохранить нельзя
    await url.fill(BLOCKED_URL);
    await expect(results.locator('li')).toHaveCount(0);
    await check.click();
    const html = results.locator('[data-code="scheme_bad"]');
    await expect(html).toBeVisible({ timeout: T.network + T.action });
    await expect(html).toHaveAttribute('data-level', 'block');
    await expect(submit).toBeDisabled();
    await expect(modal.getByTestId('geo-url-diff')).toHaveCount(0);

    // адрес из каталога: проверка без блоков, diff, сохранение
    const catalog = await catalogUrl(page);
    await url.fill(catalog);
    await expect(results.locator('li')).toHaveCount(0);
    await check.click();
    await expect(results.locator('li').first()).toBeVisible({ timeout: T.network + T.action });
    test.skip(
      (await results.locator('[data-code="reach_fail"]').count()) > 0,
      'сеть стенда недоступна: адрес GitHub не ответил'
    );
    await expect(results.locator('[data-level="block"]')).toHaveCount(0);
    const diff = modal.getByTestId('geo-url-diff');
    await expect(diff).toContainText(OWN_FILE, { timeout: T.action });
    await expect(diff.locator('.diff-line-added').first()).toBeVisible();
    await modal.getByTestId('geo-url-update-later').check();
    await expect(submit).toBeEnabled({ timeout: T.action });
    await submit.click();
    await acceptCommentLoss(page);
    await expect(modal).toBeHidden({ timeout: T.action });

    // у записи теперь ссылка каталога
    await expect
      .poll(async () => (await snapshot(page)).sources.find((x) => x.file === OWN_FILE)?.url, {
        timeout: T.action
      })
      .toBe(catalog);

    await guard.assertClean();
  });
});

// --- Удаление и выключение источника через черновик (план 146.1-18) ---
// Применение пометки (запуск -ug, уборка файла) здесь не выполняется: оно стирает файлы
// стенда, его проверяют Go-сценарии и сессия стенда. Сценарии доходят до черновика и
// «Сбросить»; xkeen.json возвращается прежним, черновик — пустым.

const DEL_FILE = 'xcp-del-test.dat';

/** Своя запись geodata тем же путём, что диалог: предпросмотр, затем запись без запуска -ug. */
async function addOwnEntry(page: Page, file: string, url: string): Promise<void> {
  const op = { kind: 'add', file, url };
  const prev = await api(page, 'POST', `${BASE}/preview`, { op });
  expect(prev.status, 'предпросмотр добавления').toBe(200);
  const written = await api(page, 'POST', `${BASE}/write`, {
    op,
    base: prev.data?.base,
    accept_comment_loss: true,
    update_now: false
  });
  expect(written.status, 'запись своего источника').toBe(200);
}

const XRAY_CONF_DIR = '/opt/etc/xray/configs';
const FOREIGN_FILE = '98_rt_foreign.json';

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

/** Убирает чужой конфиг сценария; нет файла (404) — не ошибка. */
async function removeForeignConfig(page: Page): Promise<void> {
  const res = await rawRequest(
    page,
    'POST',
    `/api/config/delete?path=${encodeURIComponent(`${XRAY_CONF_DIR}/${FOREIGN_FILE}`)}`
  );
  expect([200, 404], `уборка ${FOREIGN_FILE}: ${res.text.slice(0, 200)}`).toContain(res.status);
}

test.describe('удаление источника', () => {
  let xkeenJsonBefore: string | null = null;

  test.beforeAll(async ({ browser }) => {
    xkeenJsonBefore = await withPanel(browser, readXkeenJson);
  });

  test.afterEach(async ({ browser }) => {
    const was = xkeenJsonBefore;
    await withPanel(browser, async (page) => {
      await resetDraft(page);
      await removeForeignConfig(page);
      if (was === null || (await readXkeenJson(page)) === was) return;
      const res = await api(page, 'POST', `${XKEEN_SETTINGS}/save`, {
        kind: XKEEN_JSON_KIND,
        content: was
      });
      expect(res.status, 'возврат xkeen.json').toBe(200);
    });
  });

  test('lists-actions: удалить свой источник в черновик', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(!before.xkeen.user_geodata, 'XKeen старее 2.0.1: панель записи не делает');
    test.skip(!before.download.jq, 'на стенде нет jq: XKeen не читает записи geodata');
    expect(
      before.sources.some((x) => x.file === DEL_FILE),
      `на стенде нет записи ${DEL_FILE}`
    ).toBe(false);

    await addOwnEntry(page, DEL_FILE, await catalogUrl(page));
    await openLists(page);

    const row = page.getByTestId('geo-group-own').getByTestId(`geo-source-${DEL_FILE}`);
    const del = row.getByTestId(`geo-source-delete-${DEL_FILE}`);
    const restore = row.getByTestId(`geo-source-restore-${DEL_FILE}`);
    const chip = row.getByTestId(`geo-source-draft-${DEL_FILE}`);
    const geoRow = page.getByTestId('config-draft-geodata');
    await expect(row).toBeVisible({ timeout: T.action });
    await expect(del).toHaveAttribute('aria-label', /.+/);
    await expect(restore).toHaveCount(0);

    // ссылок нет: подтверждение, а не диалог каскада; отмена ничего не помечает
    await del.click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('geo-delete-modal')).toHaveCount(0);
    await expect(dialog).toContainText(/Ссылок на файл нет|Nothing references the file/);
    await dialog.getByRole('button', { name: /Отмена|Cancel/ }).click();
    await expect(dialog).toHaveCount(0, { timeout: T.action });
    await expect(chip).toHaveCount(0);

    // подтверждение: пометка в черновике, чип, «Вернуть источник» вместо правки и удаления
    await del.click();
    await expect(dialog).toBeVisible({ timeout: T.action });
    await dialog.locator('.btn-danger').click();
    await expect(dialog).toHaveCount(0, { timeout: T.action });
    await expect(chip).toContainText(/удалится после|will be deleted after/, {
      timeout: T.action
    });
    await expect(row.getByTestId(`geo-source-edit-${DEL_FILE}`)).toHaveCount(0);
    await expect(del).toHaveCount(0);
    await expect(restore).toBeVisible();
    await expect(geoRow).toContainText(DEL_FILE, { timeout: T.action });
    const marked = await snapshot(page);
    expect(marked.draft?.some((d) => d.file === DEL_FILE && d.mode === 'delete')).toBe(true);
    // запись и файл на месте: удаление ждёт «Применить»
    expect(marked.sources.find((x) => x.file === DEL_FILE)?.entry).toBe(true);

    // «Вернуть источник» снимает только пометку
    await restore.click();
    await expect(chip).toHaveCount(0, { timeout: T.action });
    await expect(geoRow).toHaveCount(0, { timeout: T.action });
    await expect(del).toBeVisible();

    // снова пометить и сбросить черновик из полосы
    await del.click();
    await expect(dialog).toBeVisible({ timeout: T.action });
    await dialog.locator('.btn-danger').click();
    await expect(chip).toBeVisible({ timeout: T.action });
    await page.getByTestId('config-draftbar').locator('.draft-bar-actions button').first().click();
    await expect(dialog).toBeVisible({ timeout: T.action });
    await dialog.locator('.confirm-actions button').last().click();
    await expect(dialog).toHaveCount(0, { timeout: T.action });
    await expect(chip).toHaveCount(0, { timeout: T.action });
    await expect(geoRow).toHaveCount(0, { timeout: T.action });
    expect((await snapshot(page)).draft ?? []).toHaveLength(0);

    await guard.assertClean();
  });

  test('lists-actions: каскад правил', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(!before.xkeen.user_geodata, 'XKeen старее 2.0.1: панель записи не делает');

    // лежащий источник «Свои» или «Наборы панели» без чужих ссылок и сырых правил
    const candidates = before.sources.filter(
      (x) =>
        x.present &&
        !x.draft &&
        ['own', 'file', 'panel'].includes(x.origin) &&
        ['geosite', 'geoip'].includes(x.kind)
    );
    let source: SnapSource | null = null;
    for (const c of candidates) {
      const imp = await api(page, 'GET', `${BASE}/impact?file=${encodeURIComponent(c.file)}`);
      const foreign = (imp.data?.foreign as unknown[] | undefined) ?? [];
      const raw = (imp.data?.raw as unknown[] | undefined) ?? [];
      if (imp.status === 200 && foreign.length === 0 && raw.length === 0) {
        source = c;
        break;
      }
    }
    test.skip(source === null, 'на стенде нет лежащего источника «Свои» или «Наборы панели»');
    const src = source as SnapSource;

    const tagsRes = await api(page, 'GET', `${BASE}/tags?file=${encodeURIComponent(src.file)}`);
    expect(tagsRes.status, 'GET tags').toBe(200);
    const tag = ((tagsRes.data?.tags ?? []) as { tag: string }[])[0]?.tag;
    test.skip(!tag, 'в файле источника нет тегов');
    const tagName = tag as string;

    // правило в черновике на (файл, тег) источника: других условий нет, правило погибнет
    const routing = await api(page, 'GET', '/api/configlayer/routing');
    expect(routing.status, 'GET routing').toBe(200);
    const ruleSave = await api(page, 'POST', '/api/configlayer/routing/rules/save', {
      revision: Number(routing.data?.revision ?? 0),
      rule: {
        id: '',
        enabled: true,
        origin: '',
        conds: [
          {
            type: src.kind,
            values: [],
            lists: [{ file: src.file, tag: tagName, kind: src.kind === 'geoip' ? 'ip' : 'site' }]
          }
        ],
        target: { kind: 'direct' },
        raw: null
      }
    });
    expect(ruleSave.status, 'rules/save').toBe(200);
    const ruleId = String((ruleSave.data?.rule as { id?: string } | undefined)?.id ?? '');
    expect(ruleId, 'идентификатор правила').not.toBe('');

    await openLists(page);
    await startDrop(page, src);
    const modal = page.getByTestId('geo-delete-modal');
    await expect(modal).toBeVisible({ timeout: T.action });
    await expect(modal.getByTestId(`geo-delete-row-${tagName}`)).toContainText(src.file);
    await expect(modal.getByTestId('geo-delete-effects')).toBeVisible();
    const confirm = modal.getByTestId('geo-delete-confirm');
    // «заменить» без выбранной замены: записать нельзя
    await expect(confirm).toBeDisabled();
    await modal.getByTestId(`geo-delete-with-${tagName}`).selectOption('other');
    const picker = page.getByTestId('list-picker');
    await expect(picker).toBeVisible({ timeout: T.action });
    // списки удаляемого файла в выборе замены скрыты
    await expect(picker.locator(`[data-testid^="list-row-${src.file}:"]`)).toHaveCount(0);
    await picker.getByTestId('list-picker-cancel').click();
    await expect(picker).toHaveCount(0, { timeout: T.action });
    await expect(confirm).toBeDisabled();

    // убрать список из условий: других условий у правила нет, его удаление видно заранее
    await modal.getByTestId(`geo-delete-action-${tagName}`).selectOption('remove');
    await expect(modal.getByTestId(`geo-delete-dies-${tagName}`)).toBeVisible({
      timeout: T.action
    });
    await expect(confirm).toBeEnabled();
    await confirm.click();
    await expect(modal).toHaveCount(0, { timeout: T.action });

    const row = page.getByTestId(groupOf(src.origin)).getByTestId(`geo-source-${src.id}`);
    await expect(row.getByTestId(`geo-source-draft-${src.id}`)).toBeVisible({
      timeout: T.action
    });
    await expect(page.getByTestId('config-draft-geodata')).toContainText(src.file, {
      timeout: T.action
    });
    const marked = await api(page, 'GET', '/api/configlayer/routing');
    const ruleIds = ((marked.data?.rules ?? []) as { id: string }[]).map((r) => r.id);
    expect(ruleIds, 'правило без условий убрано каскадом').not.toContain(ruleId);

    // «Сбросить» возвращает черновик к применённому: пометки нет
    await page.getByTestId('config-draftbar').locator('.draft-bar-actions button').first().click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: T.action });
    await dialog.locator('.confirm-actions button').last().click();
    await expect(dialog).toHaveCount(0, { timeout: T.action });
    await expect(row.getByTestId(`geo-source-draft-${src.id}`)).toHaveCount(0, {
      timeout: T.action
    });
    await expect(page.getByTestId('config-draft-geodata')).toHaveCount(0);
    expect((await snapshot(page)).draft ?? []).toHaveLength(0);

    await guard.assertClean();
  });

  test('lists-actions: ссылка на удаляемый файл', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(!before.xkeen.user_geodata, 'XKeen старее 2.0.1: панель записи не делает');

    // лежащий стандартный набор без ссылок: его выключение ничего не каскадирует
    let source: SnapSource | null = null;
    for (const c of before.sources.filter(
      (x) =>
        x.present && !x.draft && x.origin === 'standard' && ['geosite', 'geoip'].includes(x.kind)
    )) {
      const imp = await api(page, 'GET', `${BASE}/impact?file=${encodeURIComponent(c.file)}`);
      const noLists = ((imp.data?.lists as unknown[] | undefined) ?? []).length === 0;
      const noForeign = ((imp.data?.foreign as unknown[] | undefined) ?? []).length === 0;
      const noRaw = ((imp.data?.raw as unknown[] | undefined) ?? []).length === 0;
      if (imp.status === 200 && noLists && noForeign && noRaw) {
        source = c;
        break;
      }
    }
    test.skip(source === null, 'на стенде нет лежащего стандартного набора без ссылок');
    const src = source as SnapSource;

    const tagsRes = await api(page, 'GET', `${BASE}/tags?file=${encodeURIComponent(src.file)}`);
    expect(tagsRes.status, 'GET tags').toBe(200);
    const tag = ((tagsRes.data?.tags ?? []) as { tag: string }[])[0]?.tag;
    test.skip(!tag, 'в файле набора нет тегов');
    const tagName = tag as string;

    // пометка выключения в черновике; правил на файл ещё нет, поэтому действия пусты
    const sources = await api(page, 'GET', '/api/configlayer/sources');
    const drop = await api(page, 'POST', `${BASE}/drop`, {
      rev: Number(sources.data?.draft_revision ?? 0),
      file: src.file,
      mode: 'off',
      actions: []
    });
    expect(drop.status, `выключение ${src.file} в черновике`).toBe(200);

    // правило на (файл, тег) выключаемого набора
    const routing = await api(page, 'GET', '/api/configlayer/routing');
    expect(routing.status, 'GET routing').toBe(200);
    const ruleSave = await api(page, 'POST', '/api/configlayer/routing/rules/save', {
      revision: Number(routing.data?.revision ?? 0),
      rule: {
        id: '',
        enabled: true,
        origin: '',
        conds: [
          {
            type: src.kind,
            values: [],
            lists: [{ file: src.file, tag: tagName, kind: src.kind === 'geoip' ? 'ip' : 'site' }]
          }
        ],
        target: { kind: 'direct' },
        raw: null
      }
    });
    expect(ruleSave.status, 'rules/save').toBe(200);
    const ruleId = String((ruleSave.data?.rule as { id?: string } | undefined)?.id ?? '');
    expect(ruleId, 'идентификатор правила').not.toBe('');

    // маршрутизация под управлением панели: проблема слоя блокирует «Применить»
    const after = await api(page, 'GET', '/api/configlayer/routing');
    expect(after.status, 'GET routing после правила').toBe(200);
    const take = await api(page, 'POST', '/api/configlayer/routing/take', {
      revision: Number(after.data?.revision ?? 0)
    });
    expect(take.status, 'взятие маршрутизации в черновике').toBe(200);
    await expect
      .poll(
        async () => {
          const st = await api(page, 'GET', '/api/configlayer/state');
          return ((st.data?.checks ?? []) as { code: string }[]).map((c) => c.code);
        },
        { timeout: T.action }
      )
      .toContain('geodata_ref_remains');

    await page.goto('/#/config/rules', { timeout: T.page, waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
    const chip = page
      .getByTestId(`rule-status-${ruleId}`)
      .locator('[data-chip="geodata_ref_remains"]');
    await expect(chip).toBeVisible({ timeout: T.action });
    await expect(chip, 'чип называет удаляемый файл').toContainText(src.file);
    await expect(chip, 'чип называет тег списка').toContainText(tagName);
    await expect(chip).not.toContainText(/Не удалось проверить черновик|could not be checked/);
    await expect(chip).toHaveClass(/chip-danger/);

    await guard.assertClean();
  });

  test('lists-actions: чужая ссылка блокирует', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openLists(page);

    const before = await snapshot(page);
    test.skip(!before.xkeen.installed, 'на стенде нет XKeen');
    test.skip(!before.xkeen.user_geodata, 'XKeen старее 2.0.1: панель записи не делает');

    // лежащий источник без своих ссылок: чужую ссылку на него сценарий кладёт сам
    let source: SnapSource | null = null;
    for (const c of before.sources.filter(
      (x) => x.present && !x.draft && ['geosite', 'geoip'].includes(x.kind)
    )) {
      const imp = await api(page, 'GET', `${BASE}/impact?file=${encodeURIComponent(c.file)}`);
      const foreignNow = ((imp.data?.foreign as unknown[] | undefined) ?? []).length;
      const rawNow = ((imp.data?.raw as unknown[] | undefined) ?? []).length;
      if (imp.status === 200 && foreignNow === 0 && rawNow === 0) {
        source = c;
        break;
      }
    }
    test.skip(source === null, 'на стенде нет лежащего источника без ссылок');
    const src = source as SnapSource;

    const tagsRes = await api(page, 'GET', `${BASE}/tags?file=${encodeURIComponent(src.file)}`);
    expect(tagsRes.status, 'GET tags').toBe(200);
    const tag = ((tagsRes.data?.tags ?? []) as { tag: string }[])[0]?.tag;
    test.skip(!tag, 'в файле источника нет тегов');
    const tagName = tag as string;

    // чужой конфиг Xray: пустой объект и комментарий со ссылкой ext:<файл>:<тег>
    const foreignPath = `${XRAY_CONF_DIR}/${FOREIGN_FILE}`;
    const foreignQuery = encodeURIComponent(foreignPath);
    try {
      const saved = await rawRequest(
        page,
        'POST',
        `/api/config/save?path=${foreignQuery}`,
        `{\n  // ext:${src.file}:${tagName}\n}\n`
      );
      expect(saved.status, `запись чужого конфига: ${saved.text.slice(0, 200)}`).toBe(200);

      await openLists(page);
      await startDrop(page, src);
      const modal = page.getByTestId('geo-delete-modal');
      await expect(modal).toBeVisible({ timeout: T.action });
      const foreign = modal.getByTestId('geo-delete-foreign');
      await expect(foreign).toBeVisible({ timeout: T.action });
      await expect(foreign).toHaveAttribute('role', 'alert');
      await expect(foreign.getByRole('link')).toHaveAttribute('href', '#/editor');
      const text = (await foreign.textContent()) ?? '';
      expect(text, 'место чужой ссылки с номером строки').toMatch(/98_rt_foreign\.json:\d+\./);
      expect(text, 'нет пробела перед точкой').not.toMatch(/\s\./);
      await expect(modal.getByTestId('geo-delete-confirm')).toBeDisabled();
      await modal.getByTestId('geo-delete-cancel').click();
      await expect(modal).toHaveCount(0, { timeout: T.action });
      expect((await snapshot(page)).draft ?? []).toHaveLength(0);

      await guard.assertClean();
    } finally {
      await removeForeignConfig(page);
    }
  });
});

function groupOf(origin: string): string {
  if (origin === 'panel') return 'geo-group-panel';
  if (origin === 'standard') return 'geo-group-standard';
  return 'geo-group-own';
}

/** Начинает удаление источника или выключение набора из его строки. */
async function startDrop(page: Page, s: SnapSource): Promise<void> {
  const row = page.getByTestId(groupOf(s.origin)).getByTestId(`geo-source-${s.id}`);
  await expect(row).toBeVisible({ timeout: T.action });
  if (s.origin === 'own' || s.origin === 'file') {
    await row.getByTestId(`geo-source-delete-${s.id}`).click();
  } else {
    await row.locator('label.toggle-label').click();
  }
}
