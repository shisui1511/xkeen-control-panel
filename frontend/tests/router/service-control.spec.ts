// e2e-pages: services
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';

// Управление службой ядра через интерфейс настоящей панели (RT-05, D-04): остановка, запуск и
// перезапуск кнопками страницы «Службы». После каждого действия статус в интерфейсе (значок
// в шапке карточки) и ответ /api/service/status должны совпадать и отражать настоящее состояние
// устройства: процесс ядра остановлен, запущен, перезапущен (PID сменился).
//
// Сценарий разрушающий: пока идёт остановка, перехват трафика на стенде снят. Исходное
// состояние возвращает снимок стенда (D-19); в конце теста ядро дожидается устойчивого
// состояния, чтобы следующие спеки начинали с рабочей панели.

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

test(`service-control core:${RT.core || 'unknown'}`, async ({ page }) => {
  const core = RT.core;
  test.skip(core !== 'xray' && core !== 'mihomo', 'активное ядро стенда не определено');
  // действие службы на медленном устройстве: остановка, запуск и перезапуск идут минуты
  const ACTION = 120_000 * RT.slow;
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
  await expect(restartBtn).toBeEnabled({ timeout: ACTION });
  const pidBefore = started.pid;
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
  // XKeen на медленных устройствах заменяет процесс ядра спустя время после запуска
  const hold = 20_000 * RT.slow;
  const deadline = Date.now() + 2 * ACTION;
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
    if (ok && Date.now() - since >= hold) break;
    expect(Date.now(), `ядро не пришло в устойчивое состояние: ${describe(s)}`).toBeLessThan(
      deadline
    );
    await page.waitForTimeout(3000);
  }

  await guard.assertClean();
});
