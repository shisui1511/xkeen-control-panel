// e2e-pages: services
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { withPanel } from './lib/nodes-api';

// Управление службой ядра через интерфейс настоящей панели (RT-05, D-04): остановка, запуск и
// перезапуск кнопками страницы «Службы». После каждого действия статус в интерфейсе (значок
// в шапке карточки) и ответ /api/service/status должны совпадать и отражать настоящее состояние
// устройства: процесс ядра остановлен, запущен, перезапущен (PID сменился).
//
// Сценарий разрушающий: пока идёт остановка, перехват трафика на стенде снят. Службу при
// падении возвращает хук afterEach (запускает остановленное ядро и ждёт устойчивого состояния);
// снимок стенда есть только в полном прогоне. В конце удачного теста ядро тоже дожидается
// устойчивого состояния, чтобы следующие спеки начинали с рабочей панели.

// действие службы на медленном устройстве: остановка, запуск и перезапуск идут минуты
const ACTION = 120_000 * RT.slow;

interface Status {
  running: boolean;
  kernels: string[];
  pid: number;
  conflict: boolean;
  active: string;
}

async function readStatus(page: Page): Promise<Status | string> {
  const res = await page.request.get('/api/service/status');
  if (!res.ok()) return `http ${res.status()}`;
  const body = (await res.json()) as {
    data?: {
      is_running?: boolean;
      running_kernels?: string[];
      pid?: number;
      kernel_conflict?: boolean;
      active_kernel?: string;
    };
  };
  const d = body.data ?? {};
  return {
    running: d.is_running === true,
    kernels: d.running_kernels ?? [],
    pid: d.pid ?? 0,
    conflict: d.kernel_conflict === true,
    active: d.active_kernel ?? ''
  };
}

/** Краткая подпись состояния для сообщений об ошибках. */
function describe(s: Status | string): string {
  if (typeof s === 'string') return s;
  return `running=${s.running} kernels=[${s.kernels.join(',')}] pid=${s.pid ? 'есть' : 'нет'} conflict=${s.conflict}`;
}

/**
 * Ждёт устойчивого состояния: ядро работает, PID не меняется `hold` мс. XKeen возвращает
 * управление раньше, чем закончил запуск (ядро поднимается, ставятся правила, на медленных
 * устройствах процесс ядра заменяется), а команда, пришедшая в это окно, молча не
 * выполняется: поэтому следующее действие начинается только после устойчивого состояния.
 */
async function waitStable(page: Page, core: string, what: string): Promise<void> {
  const hold = 20_000 * RT.slow;
  const deadline = Date.now() + 2 * 120_000 * RT.slow;
  let pid = 0;
  let since = Date.now();
  for (;;) {
    const s = await readStatus(page);
    const ok = typeof s !== 'string' && s.running && s.kernels.join(',') === core;
    const cur = ok ? (s as Status).pid : 0;
    if (!ok || cur !== pid) {
      pid = cur;
      since = Date.now();
    }
    if (ok && Date.now() - since >= hold) return;
    expect(Date.now(), `${what}: ${describe(s)}`).toBeLessThan(deadline);
    await page.waitForTimeout(3000);
  }
}

/**
 * Шлюз запуска: перед `xkeen -start` панель опрашивает Preflight (не дольше 3 с) и, если в
 * конфиге есть ошибки, просит подтверждение («Исправить» / «Запустить всё равно»). У Mihomo без
 * external-controller (на стенде нет API ядра) Preflight сообщает именно такую ошибку, поэтому
 * окно — штатная часть запуска, а не сбой. Если окно появилось, проверяется его геометрия и
 * запуск подтверждается последней кнопкой; если ядро поднялось без вопроса, ничего не делается.
 */
async function passStartGate(page: Page, badge: Locator): Promise<void> {
  const dialog = page.getByRole('dialog');
  const wait = 10_000 * RT.slow;
  const shown = await Promise.race([
    dialog.waitFor({ state: 'visible', timeout: wait }).then(() => true),
    expect(badge)
      .toHaveClass(/\brunning\b/, { timeout: wait })
      .then(() => false)
  ]).catch(() => false);
  if (!shown) return;
  // окно появляется с анимацией: ждём, пока рамка остановится
  await page.waitForTimeout(400);
  const vp = page.viewportSize();
  const buttons = dialog.locator('.confirm-actions button');
  const n = await buttons.count();
  expect(n, 'шлюз запуска: кнопок действий в окне').toBeGreaterThanOrEqual(2);
  for (let i = 0; i < n; i++) {
    const b = buttons.nth(i);
    await expect(b).toBeVisible();
    await expect(b).toBeEnabled();
    const box = await b.boundingBox();
    expect(box, `шлюз запуска: кнопка ${i + 1} без рамки`).not.toBeNull();
    if (box && vp) {
      expect(box.x, `шлюз запуска: кнопка ${i + 1} левее экрана`).toBeGreaterThanOrEqual(-0.5);
      expect(box.x + box.width, `шлюз запуска: кнопка ${i + 1} правее экрана`).toBeLessThanOrEqual(
        vp.width + 0.5
      );
      expect(box.y + box.height, `шлюз запуска: кнопка ${i + 1} ниже экрана`).toBeLessThanOrEqual(
        vp.height + 0.5
      );
    }
  }
  test.info().annotations.push({
    type: 'start-gate',
    description: 'Preflight запуска показал окно подтверждения: запуск подтверждён'
  });
  // последняя кнопка — «Запустить всё равно»
  await buttons.nth(n - 1).click();
  await expect(dialog).toBeHidden({ timeout: T.action });
}

/** Запрос к API панели из страницы с её сессией и токеном CSRF. */
async function rawRequest(page: Page, method: string, path: string): Promise<number> {
  return page.evaluate(
    async ({ method, path }) => {
      const res = await fetch(path, {
        method,
        headers: { 'X-CSRF-Token': localStorage.getItem('csrf_token') ?? '' }
      });
      return res.status;
    },
    { method, path }
  );
}

/**
 * Упавший тест мог остановиться посреди перезапуска, а XKeen возвращает управление раньше
 * конца действия: статус «работает» в этот момент ещё не значит, что ядро устойчиво. Ждёт, пока
 * PID не продержится неизменным `hold` мс. false — ядро оказалось остановленным (запоздавшая
 * остановка или перезапуск в разгаре): его надо запустить.
 */
async function holdsRunning(page: Page, core: string): Promise<boolean> {
  const hold = 20_000 * RT.slow;
  let pid = 0;
  let since = Date.now();
  for (;;) {
    const s = await readStatus(page);
    if (typeof s === 'string') throw new Error(`уборка: статус службы недоступен (${describe(s)})`);
    if (s.conflict) throw new Error(`уборка: конфликт двух ядер (${describe(s)})`);
    if (!(s.running && s.kernels.length === 1 && s.kernels[0] === core)) return false;
    if (s.pid !== pid) {
      pid = s.pid;
      since = Date.now();
    }
    if (Date.now() - since >= hold) return true;
    await page.waitForTimeout(3000);
  }
}

/**
 * Уборка после падения: остановленное ядро запускается и дожидается устойчивого состояния.
 * Работающее ядро после упавшего теста тоже дожидается устойчивого состояния: если оно в ходе
 * ожидания остановилось, его запускают. Удачный тест уже дождался сам, ничего не делается.
 * Конфликт двух ядер — ошибка без вмешательства. Ошибка уборки добавляется к падению теста, не
 * заменяя его.
 */
test.afterEach(async ({ browser }, testInfo) => {
  const core = RT.core;
  if (testInfo.status === 'skipped' || (core !== 'xray' && core !== 'mihomo')) return;
  await withPanel(browser, async (page) => {
    const s = await readStatus(page);
    if (typeof s === 'string') throw new Error(`уборка: статус службы недоступен (${describe(s)})`);
    if (s.conflict) throw new Error(`уборка: конфликт двух ядер (${describe(s)})`);
    const running = s.running && s.kernels.length === 1 && s.kernels[0] === core;
    if (running && testInfo.status === 'passed') return;
    testInfo.setTimeout(testInfo.timeout + 4 * ACTION);
    if (running && (await holdsRunning(page, core))) {
      console.log('уборка: ядро после падения теста устойчиво работает');
      return;
    }
    const code = await rawRequest(page, 'POST', '/api/service/control?action=start');
    console.log(`уборка: запуск службы после падения теста, http ${code}`);
    await waitStable(page, core, 'уборка: ядро не вернулось в рабочее состояние');
    console.log('уборка: служба запущена после падения теста');
  });
});

test(`service-control core:${RT.core || 'unknown'}`, async ({ page }) => {
  const core = RT.core;
  test.skip(core !== 'xray' && core !== 'mihomo', 'активное ядро стенда не определено');
  test.setTimeout(6 * ACTION);

  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/#/services');

  const expectStopped = 'running=false kernels=[] pid=нет conflict=false';
  const heroStatus = page.locator('.hero-status .status-badge').first();
  const stopBtn = page.locator('.hero-actions .btn-danger-soft');
  const restartBtn = page.locator('.hero-actions > button.btn-secondary').first();
  const startBtn = page.getByTestId('hero-start');

  /** Интерфейс и API говорят одно и то же о состоянии службы. */
  async function expectState(running: boolean, what: string): Promise<void> {
    await expect(heroStatus, `${what}: значок статуса в интерфейсе`).toHaveClass(
      running ? /\brunning\b/ : /\bstopped\b/,
      { timeout: ACTION }
    );
    await expect
      .poll(async () => describe(await readStatus(page)), {
        message: `${what}: /api/service/status не совпал с интерфейсом`,
        timeout: ACTION,
        intervals: [1000, 3000, 5000]
      })
      .toBe(running ? `running=true kernels=[${core}] pid=есть conflict=false` : expectStopped);
  }
  // --- исходное состояние: ядро работает (стенд пришёл в рабочее состояние) -----------
  await expect(heroStatus).toBeVisible({ timeout: T.page });
  await expectState(true, 'исходное состояние');
  const first = (await readStatus(page)) as Status;
  expect(first.active, 'активное ядро в статусе').toBe(core);

  // --- остановка ------------------------------------------------------------------
  await expect(stopBtn).toBeEnabled({ timeout: T.action });
  await stopBtn.click();
  await expectState(false, 'после остановки');
  await expect(startBtn).toBeVisible({ timeout: T.action });
  await expect(stopBtn).toHaveCount(0);

  // --- запуск ----------------------------------------------------------------------
  await expect(startBtn).toBeEnabled({ timeout: ACTION });
  await startBtn.click();
  await passStartGate(page, heroStatus);
  await expectState(true, 'после запуска');
  const started = (await readStatus(page)) as Status;
  expect(started.active, 'ядро после запуска').toBe(core);

  // --- перезапуск: процесс ядра заменён ------------------------------------------------
  await waitStable(page, core, 'после запуска ядро не пришло в устойчивое состояние');
  await expect(restartBtn).toBeEnabled({ timeout: ACTION });
  // процесс ядра мог смениться за время ожидания: сравнение идёт с PID устойчивого состояния
  const pidBefore = ((await readStatus(page)) as Status).pid;
  await restartBtn.click();
  await expect
    .poll(
      async () => {
        const s = await readStatus(page);
        if (typeof s === 'string') return s;
        return s.running && s.kernels.join(',') === core && s.pid !== 0 && s.pid !== pidBefore
          ? 'перезапущено'
          : describe(s);
      },
      {
        message: 'перезапуск не сменил процесс ядра',
        timeout: ACTION,
        intervals: [1000, 3000, 5000]
      }
    )
    .toBe('перезапущено');
  await expectState(true, 'после перезапуска');

  // --- устойчивость: после перезапуска ядро держится, PID не меняется ------------------
  await waitStable(page, core, 'ядро не пришло в устойчивое состояние');

  await guard.assertClean();
});
