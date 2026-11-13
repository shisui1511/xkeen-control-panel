import { appendFileSync } from 'node:fs';
import type { FullConfig, Reporter, Suite, TestCase, TestResult } from '@playwright/test/reporter';

// Ход прогона для смотрящего (scripts/router/progress.mjs: make router-watch, панель хода в
// приложении). Подключается конфигом playwright.router.config.ts только при XCP_PW_PROGRESS:
// без переменной (ручной запуск, `--list`) репортёра нет.
//
// Строки дописываются в журнал хода прогона <отчёт>/progress.tsv (формат — rt_progress в
// scripts/router/lib.sh: «секунды<TAB>вид<TAB>поля»), одна строка — одна запись:
//   pw   <цель> <ядро> <тестов в запуске>          начало запуска Playwright
//   pwt+ <цель> <ядро> <тест>                      тест начался
//   pwt  <цель> <ядро> <статус> <тест>             тест закончился
// <тест> — имя проверки без «pw:», как его строит scripts/router/pw-parse.js: названия
// describe и теста через « > » (без файла спека), чтобы живая строка совпала с записанной.
// Статус: PASS, FAIL, KNOWN (ожидаемое падение по метке knownFailure), SKIP. Итоговые строки
// результатов пишет pw-suite.sh по JSON-отчёту, как прежде: этот журнал только для смотрящего.
//
// Переменные (задаёт scripts/router/pw-suite.sh):
//   XCP_PW_PROGRESS  файл журнала (<отчёт>/progress.tsv); включает репортёр
//   XCP_PW_TARGET    id цели
//   XCP_CORE         ядро ветви («-» вне ветви)
// Сбой записи прогон не меняет.

/** Поле журнала: без табуляций и переводов строки. */
function field(v: string): string {
  return v.replace(/[\t\r\n]+/g, ' ');
}

/** Имя теста для журнала — как в pw-parse.js: describe и тест через « > ». */
function titleOf(test: TestCase): string {
  return test.titlePath().slice(3).join(' > ');
}

/** Статус по исходу: ожидаемое падение (test.fail по метке) — KNOWN. */
function statusOf(test: TestCase, result: TestResult): string {
  const outcome = test.outcome();
  if (outcome === 'skipped' || result.status === 'skipped') return 'SKIP';
  if (outcome === 'unexpected') return 'FAIL';
  return test.expectedStatus === 'failed' ? 'KNOWN' : 'PASS';
}

export default class ProgressReporter implements Reporter {
  private readonly file = process.env.XCP_PW_PROGRESS ?? '';
  private readonly target = field(process.env.XCP_PW_TARGET || '-');
  private readonly core = field(process.env.XCP_CORE || '-');

  printsToStdio(): boolean {
    return false;
  }

  private write(kind: string, ...fields: string[]): void {
    if (!this.file) return;
    const line = [
      String(Math.floor(Date.now() / 1000)),
      kind,
      this.target,
      this.core,
      ...fields.map(field)
    ].join('\t');
    try {
      appendFileSync(this.file, `${line}\n`);
    } catch {
      // журнал только для смотрящего: сбой записи прогон не меняет
    }
  }

  onBegin(_config: FullConfig, suite: Suite): void {
    this.write('pw', String(suite.allTests().length));
  }

  onTestBegin(test: TestCase): void {
    this.write('pwt+', titleOf(test));
  }

  onTestEnd(test: TestCase, result: TestResult): void {
    this.write('pwt', statusOf(test, result), titleOf(test));
  }
}
