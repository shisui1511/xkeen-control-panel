#!/usr/bin/env node
// scripts/router/estimate-full.mjs — бегущая оценка полного прогона на одном стенде (D-38).
//
// Считает время `make router-test SUITE=full` по тому же составу, что исполняет run.sh: план
// run-plan.mjs набора full (обе ветви, все тесты; тесты с меткой «одно ядро» — только на ядре --b1,
// ветвь другого ядра их не запускает, D-01, D-03) и модель оценки select-replay.mjs (buildModel,
// planSec: оболочка прогона, переключение и смоук ветви, окно наблюдения параллельно спекам read,
// затем Go и изменяющие спеки). Поэтому оценка не расходится с прогоном (RTR-14 фазы 148.1.1), а тест
// «одно ядро» считается один раз.
//
// Медианы:
//   «до»     — строки build/router/history.tsv основной копии (history.mjs readHistory) за --days дней
//            до --until, кроме отчётов, чьё имя начинается с любого --exclude (по умолчанию прогон
//            20261107-004952-2b2b0afb, D-06 фазы 148.1.1) и строк набора coverage (сборка с -cover,
//            D-24); ключ — проверка и ядро, медиана секунд (history.mjs durations).
//   «после»  — строки отчётов, чьё имя начинается с любого --after (отчёты сессий), без окна по
//            времени (select-replay.mjs afterMedians); для проверки и ядра с «после» берётся «после»
//            — фактическое время переделанных тестов идёт в оценку (D-38, D-39).
// Медианы ядер в модели усредняются (как в повторе выбора); тест без медианы — UNKNOWN_SEC (30 с).
//
// Вывод (две строки):
//   Оценка полного прогона: N мин (Go X, Playwright Y, оболочка Z); заменено сессиями: K; одно ядро: M
//   - медианы: <начало окна>…<until>, без: <exclude>; после: <after или —>
// Минуты — с одной цифрой после запятой; Go и Playwright — суммы этапов ветвей (спеки read — с учётом
// трёх воркеров), оболочка — остаток (сборка, установка, снимок, переключения, смоуки, окна
// наблюдения сверх спеков read). K — пар «проверка-ядро», чьё время взято из сессий; M — тестов с
// меткой «одно ядро» (Go-имена и спек-тесты).
//
// Использование (из любого каталога рабочей копии):
//   node scripts/router/estimate-full.mjs [--until <ISO>] [--days N] [--exclude <отчёт>]…
//     [--after <отчёт>]… [--b1 <xray|mihomo>] [--json]
// --until — момент среза (ISO, по умолчанию сейчас); --days — окно «до» (целое 1…3650, по умолчанию
// 60); --exclude — префикс имени отчёта (повторяемый; заданный список заменяет значение по
// умолчанию); --after — префикс имени отчёта сессии (повторяемый; у этапа сессии имя в history.tsv —
// <сессия>/<NN>-<этап>); --b1 — ядро «одно ядро» (по умолчанию xray: медианы ядер усредняются, на
// итог ядро не влияет); --json — объект { total_min, go_min, pw_min, shell_min, replaced, one_core,
// until, days, exclude, after }.
// Код 0 — оценка напечатана; 1 — история не прочитана или пуста в окне, план состава не получен;
// 2 — неверный вызов. Только чтение: стенд, история и отчёты не меняются.
// Без зависимостей: только встроенные модули Node и модули scripts/router.

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { durations, readHistory } from './history.mjs';
import { runPlan } from './run-plan.mjs';
import { afterMedians, buildModel, goStageSec, HISTORY_DAYS, planSec, pwStageSec, UNKNOWN_SEC } from './select-replay.mjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const CORES = ['xray', 'mihomo'];
const DEFAULT_EXCLUDE = ['20261107-004952-2b2b0afb'];
// префикс имени отчёта в history.tsv: каталог отчёта и, у этапа сессии, /<NN>-<этап>
const REPORT_PREFIX_RE = /^[A-Za-z0-9._-]+(\/[A-Za-z0-9._-]*)?$/;
const USAGE =
  'usage: estimate-full.mjs [--until <ISO>] [--days N] [--exclude <отчёт>]… [--after <отчёт>]… [--b1 <xray|mihomo>] [--json]';

class UsageError extends Error {}

function parseArgs(argv) {
  const out = { until: null, days: HISTORY_DAYS, exclude: [], after: [], b1: 'xray', json: false };
  const value = (i, name) => {
    const v = argv[i];
    if (v === undefined || v === '') throw new UsageError(`${name}: нужно значение`);
    return v;
  };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--until') out.until = value(++i, a);
    else if (a === '--days') {
      const v = value(++i, a);
      if (!/^[0-9]{1,4}$/.test(v) || Number(v) < 1 || Number(v) > 3650) throw new UsageError(`--days: нужно целое 1…3650, получено ${v.slice(0, 20)}`);
      out.days = Number(v);
    } else if (a === '--exclude' || a === '--after') {
      const v = value(++i, a);
      if (!REPORT_PREFIX_RE.test(v)) throw new UsageError(`${a}: недопустимое имя отчёта ${v.slice(0, 80)}`);
      (a === '--exclude' ? out.exclude : out.after).push(v);
    } else if (a === '--b1') {
      const v = value(++i, a);
      if (!CORES.includes(v)) throw new UsageError('--b1: xray или mihomo');
      out.b1 = v;
    } else if (a === '--json') out.json = true;
    else throw new UsageError(`неизвестный аргумент ${String(a).slice(0, 40)}`);
  }
  if (out.exclude.length === 0) out.exclude = [...DEFAULT_EXCLUDE];
  let at = Date.now();
  if (out.until !== null) {
    at = Date.parse(out.until);
    if (!Number.isFinite(at)) throw new UsageError(`--until: нужна дата ISO, получено ${out.until.slice(0, 40)}`);
  }
  out.at = at;
  return out;
}

const iso = (ms) => new Date(ms).toISOString().replace(/\.\d{3}Z$/, 'Z');
const minutes = (sec) => Math.round((sec / 60) * 10) / 10;
const ru = (min) => min.toFixed(1).replace('.', ',');

/** Медианы «до»: окно days до at, без отчётов exclude и набора coverage — { [проверка]: { [ядро]: с } }. */
function beforeMedians(rows, { at, days, exclude }) {
  const span = days * 86400 * 1000;
  const picked = rows.filter((r) => {
    const t = Date.parse(r.finished_utc);
    if (!Number.isFinite(t) || t > at || at - t > span) return false;
    if (r.suite === 'coverage') return false;
    return !exclude.some((p) => typeof r.report === 'string' && r.report.startsWith(p));
  });
  const medians = {};
  for (const [key, { median }] of durations(picked)) {
    const [check, core] = key.split('\t');
    if (!CORES.includes(core) || typeof median !== 'number' || !Number.isFinite(median)) continue;
    if (!medians[check]) medians[check] = {};
    medians[check][core] = median;
  }
  return { medians, rows: picked.length };
}

export function estimate(opts, rows = readHistory()) {
  const before = beforeMedians(rows, opts);
  if (before.rows === 0) throw new Error(`в истории нет строк за ${opts.days} дн. до ${iso(opts.at)} (без ${opts.exclude.join(', ')})`);
  const medians = before.medians;
  let replaced = 0;
  for (const [check, byCore] of Object.entries(afterMedians(rows, opts.after))) {
    if (!medians[check]) medians[check] = {};
    for (const [core, sec] of Object.entries(byCore)) {
      medians[check][core] = sec;
      replaced++;
    }
  }
  const model = buildModel({ medians, unknownSec: UNKNOWN_SEC }, ROOT);
  const plan = runPlan({ suite: 'full', b1: opts.b1, root: ROOT });
  const total = planSec(model, ROOT, plan);
  let go = 0;
  let pw = 0;
  for (const b of plan.branches) {
    for (const st of b.stages) {
      if (st.stage === 'go' || st.stage === 'go-harness') go += goStageSec(model, st);
      else if (st.stage === 'pw-read' || st.stage === 'pw-harness-read') pw += pwStageSec(model, ROOT, st, true);
      else if (st.stage === 'pw' || st.stage === 'pw-harness-state') pw += pwStageSec(model, ROOT, st, false);
    }
  }
  const oneCore = plan.oneCore ? plan.oneCore.go.length + plan.oneCore.pw : 0;
  return {
    total_min: minutes(total),
    go_min: minutes(go),
    pw_min: minutes(pw),
    shell_min: minutes(total - go - pw),
    replaced,
    one_core: oneCore,
    until: iso(opts.at),
    days: opts.days,
    exclude: [...opts.exclude],
    after: [...opts.after]
  };
}

function main(argv) {
  let opts;
  try {
    opts = parseArgs(argv);
  } catch (e) {
    process.stderr.write(`estimate-full: ${e.message}\n${USAGE}\n`);
    process.exit(2);
  }
  let r;
  try {
    r = estimate(opts);
  } catch (e) {
    process.stderr.write(`estimate-full: ${e && e.message ? e.message : String(e)}\n`);
    process.exit(1);
  }
  if (opts.json) {
    process.stdout.write(JSON.stringify(r, null, 2) + '\n');
    return;
  }
  const since = iso(opts.at - opts.days * 86400 * 1000);
  process.stdout.write(
    `Оценка полного прогона: ${ru(r.total_min)} мин (Go ${ru(r.go_min)}, Playwright ${ru(r.pw_min)}, оболочка ${ru(r.shell_min)}); заменено сессиями: ${r.replaced}; одно ядро: ${r.one_core}\n`
  );
  process.stdout.write(`- медианы: ${since}…${r.until}, без: ${r.exclude.join(', ')}; после: ${r.after.length ? r.after.join(', ') : '—'}\n`);
}

// Запуск — только как CLI: import (однострочник node -e, другие скрипты) оценку не печатает.
function isMain() {
  if (typeof import.meta.main === 'boolean') return import.meta.main;
  if (process.execArgv.some((a) => /^(-e|--eval|-p|--print)(=|$)/.test(a))) return false;
  return Boolean(process.argv[1]) && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
}

if (isMain()) main(process.argv.slice(2));
