// e2e-pages: config/lists
import { test, expect, type Browser, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { api, layerFlag, withPanel } from './lib/nodes-api';

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
