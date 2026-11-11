import { execFileSync } from 'node:child_process';
import { appendFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import type { FullConfig, Reporter, Suite, TestCase } from '@playwright/test/reporter';

// Сверка версии стенда на границе спека (D-13, RTR-15). Подключается конфигом
// playwright.router.config.ts только при XCP_PW_VERSION_GUARD: без переменной (ручной запуск,
// `playwright test --list` вне обвязки) репортёра нет.
//
// Когда закончились все тесты спека (в том числе пропущенные), репортёр синхронно вызывает
// `sh <XCP_PW_GUARD_SCRIPT> <цель> pw:<имя файла спека>` — CLI scripts/router/version-guard.sh.
// Сверка идёт в процессе Playwright: часть read остаётся параллельной, отчёты не дробятся.
//
// Переменные (задаёт scripts/router/pw-suite.sh):
//   XCP_PW_VERSION_GUARD  id цели; включает репортёр
//   XCP_PW_GUARD_SCRIPT   абсолютный путь version-guard.sh
//   XCP_PW_OK_SPECS       файл спеков, прошедших границу (`tests/router/<спек>` по строке)
//   XCP_PW_STOPPED        признак остановки: одна строка — граница, на которой сменилась версия
//
// Коды version-guard.sh:
//   0  версия совпала (или переход принят, или сверка выключена) — спек дописывается в
//      XCP_PW_OK_SPECS, прогон идёт дальше;
//   3  нет связи — строка в stderr, прогон идёт (потерю связи разбирает run.sh после этапа);
//   4  версия сменилась (строки stop и FAIL version-guard уже записаны) — сначала пишется
//      XCP_PW_STOPPED, затем процессу Playwright один раз уходит SIGINT: штатная мягкая
//      остановка, идущие тесты прерываются, новые не стартуют, JSON-отчёт пишется;
//   иное  строка в stderr, прогон идёт.
// После остановки сверка больше не вызывается.
//
// Потребитель файлов — pw-suite.sh: по XCP_PW_STOPPED он не начинает следующие запуски и
// выходит с кодом 4, по XCP_PW_OK_SPECS отличает падения проверенных спеков от подозрительных.
// Модуль не импортирует помощники lib/ и не читает ядро цели.

const GUARD_TIMEOUT_MS = 60_000;

/** Путь спека от каталога frontend и имя для границы: `tests/router/x.spec.ts`, `x.spec.ts`. */
function specOf(rootDir: string, file: string): { key: string; name: string } {
  const name = path.relative(rootDir, file).split(path.sep).join('/');
  return { key: `tests/router/${name}`, name };
}

/** Код выхода дочернего процесса из исключения execFileSync; null — процесс не завершился сам. */
function exitCode(err: unknown): number | null {
  if (err && typeof err === 'object' && 'status' in err) {
    const status = (err as { status: unknown }).status;
    return typeof status === 'number' ? status : null;
  }
  return null;
}

export default class VersionReporter implements Reporter {
  private readonly target = process.env.XCP_PW_VERSION_GUARD ?? '';
  private readonly script = process.env.XCP_PW_GUARD_SCRIPT ?? '';
  private readonly okFile = process.env.XCP_PW_OK_SPECS ?? '';
  private readonly stopFile = process.env.XCP_PW_STOPPED ?? '';

  private rootDir = '';
  /** спек → id тестов, ещё не закончившихся */
  private readonly pending = new Map<string, Set<string>>();
  /** спеки, в которых был неожиданный исход */
  private readonly failed = new Set<string>();
  private stopped = false;

  printsToStdio(): boolean {
    return false;
  }

  onBegin(config: FullConfig, suite: Suite): void {
    this.rootDir = config.rootDir;
    for (const test of suite.allTests()) {
      const { key } = specOf(this.rootDir, test.location.file);
      let ids = this.pending.get(key);
      if (!ids) {
        ids = new Set();
        this.pending.set(key, ids);
      }
      ids.add(test.id);
    }
  }

  onTestEnd(test: TestCase): void {
    if (this.stopped) return;
    const { key, name } = specOf(this.rootDir, test.location.file);
    const ids = this.pending.get(key);
    if (!ids || !ids.has(test.id)) return;
    if (test.outcome() === 'unexpected') this.failed.add(key);
    ids.delete(test.id);
    if (ids.size > 0) return;
    this.pending.delete(key);
    this.guard(key, name);
  }

  /** Сверка версии на границе спека и реакция на код version-guard.sh. */
  private guard(key: string, name: string): void {
    const boundary = `pw:${name}`;
    const where = this.failed.has(key) ? ' (в спеке были падения)' : '';
    let code: number | null = 0;
    try {
      execFileSync('sh', [this.script, this.target, boundary], {
        env: process.env,
        stdio: ['ignore', 'pipe', 'pipe'],
        timeout: GUARD_TIMEOUT_MS
      });
    } catch (err) {
      code = exitCode(err);
    }
    switch (code) {
      case 0:
        if (this.okFile) appendFileSync(this.okFile, `${key}\n`);
        return;
      case 3:
        process.stderr.write(`сверка версии: нет связи (граница ${boundary})${where}\n`);
        return;
      case 4:
        this.stopped = true;
        // признак остановки — до сигнала: прерывание во время сверки не даёт зелёного итога
        if (this.stopFile) writeFileSync(this.stopFile, `${boundary}\n`);
        process.stderr.write(
          `сверка версии: версия стенда сменилась (граница ${boundary})${where}, остановка Playwright\n`
        );
        process.kill(process.pid, 'SIGINT');
        return;
      default:
        process.stderr.write(
          `сверка версии: version-guard.sh завершился ${code === null ? 'без кода (тайм-аут или сигнал)' : `с кодом ${code}`} (граница ${boundary})${where}\n`
        );
    }
  }
}
