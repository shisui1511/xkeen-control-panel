#!/usr/bin/env node
// scripts/router/history.mjs — постоянная история результатов прогонов на роутерах (D-20).
//
// Отчёты build/router/<ключ> независимы и ротируются (make tidy), а порядку гейта, оценкам
// сессий, тренду набора и отбору тестов для bisect нужны результаты за много прогонов. Этот
// файл переносит из каждого отчёта строки результатов в одну историю, только добавлением.
//
// Файлы (в основной копии, родитель `git rev-parse --path-format=absolute --git-common-dir`;
// одни и те же для основной копии и всех worktree; вне git и вне ротации make tidy):
//   build/router/history.tsv  без заголовка, табуляция, 11 колонок:
//     finished_utc  время окончания (ISO, UTC): mtime summary.md отчёта, иначе самый поздний
//                   results.tsv, иначе время записи
//     sha           40 hex: meta (sha=), иначе «- SHA:» report.md
//     suite         full|changed|smoke|session|harness|rerun|bisect|delta|reboot|- («- Набор:»
//                   report.md; у этапа сессии — session)
//     release       тег («- Релиз:» report.md) или -
//     report        имя каталога отчёта; у этапа сессии — <сессия>/<NN>-<этап>
//     target        цель (каталог results.tsv)
//     core          xray|mihomo|-
//     check         проверка (go:…, pw:…, smoke:… и прочие) — как в results.tsv
//     status        PASS|FAIL|KNOWN|XPASS|SKIP|QPASS|QFAIL
//     class         new|flaky|stand|known|xpass|quarantine|- (из <цель>/classes.tsv)
//     seconds       длительность теста или -: go — Elapsed из <цель>/<ядро>/go-*.jsonl,
//                   pw — сумма duration результатов из <цель>/<ядро>/pw*/report*.json
//   build/router/history.idx  по строке на добавленный отчёт (то же значение, что report):
//                             повторное добавление отчёта ничего не пишет.
//
// Без деталей: деталь results.tsv (4-я колонка) и classes.tsv не читается и в историю не
// попадает — только SHA, имена отчёта и проверок, ядро, статус, класс и секунды (адреса,
// URL, имена узлов в историю не идут, AGENTS.md «Секреты»).
//
// Раскладка отчёта:
//   прогон  <отчёт>/<цель>/results.tsv, <отчёт>/meta, <отчёт>/report.md
//   сессия  <отчёт>/<NN>-<этап>/<цель>/results.tsv и <отчёт>/<NN>-<этап>/meta (каталоги
//           этапов в формате прогона, см. session.mjs); прогоны наборов внутри сессии лежат
//           отдельными отчётами build/router/<сессия>-<NN>-<проход> и идут как прогоны.
//
// CLI (из любой рабочей копии):
//   node scripts/router/history.mjs append <отчёт>   добавить отчёт (путь или имя в
//                                                     build/router основной копии); печатает
//                                                     число добавленных строк (0 — уже был)
//   node scripts/router/history.mjs backfill          добавить все отчёты build/router основной
//                                                     копии по возрастанию имени; итог в stderr
//   node scripts/router/history.mjs durations [--suite s] [--days N] [--core c]
//                                                     TSV check, core, median, runs по убыванию
//                                                     медианы секунд
//   node scripts/router/history.mjs failfreq [--days N]
//                                                     TSV check, fails, runs по убыванию fails
//                                                     (падение — FAIL, XPASS, QFAIL; прогон —
//                                                     любой статус, кроме SKIP)
//   node scripts/router/history.mjs last-full [--green]
//                                                     TSV report, sha, release последнего полного
//                                                     прогона (--green — без FAIL и XPASS);
//                                                     нет — код 1
//   node scripts/router/history.mjs trend <отчёт>     строки тренда для summary.md (D-16)
// Неизвестная команда или аргумент — код 2.
//
// Экспорты ESM:
//   historyPaths()                       { root, file, idx }
//   readHistory({ file }?)               строки истории объектами (seconds — число или null)
//   appendReport(reportDir)              число добавленных строк (0 — отчёт уже в истории)
//   backfill()                           { reports, rows } — добавленные отчёты и строки
//   listReports(dir)                     имена отчётов каталога по возрастанию
//   median(numbers)                      медиана
//   durations(rows, { suite, days, core }?)
//                                        Map `<check>\t<core>` → { median, runs }
//   failFreq(rows, { days }?)            Map check → { fails, runs } — порядок гейта (RTR-03),
//                                        отбор для bisect (D-22), нестабильность (D-19)
//   lastFull(rows, { green, before }?)   { report, sha, release, finished } последнего полного
//                                        прогона (suite=full, не этап сессии) строго раньше
//                                        before (ISO) или null
//   trendLines(rows, reportName)         строки Markdown: «- Тренд: тестов N (было M, +K), время
//                                        тестов X мин (было Y, +Z)» и «- Новые тесты: …» (до 5
//                                        самых долгих проверок, которых не было в прошлом полном
//                                        прогоне); тесты — верхние go: и pw: с ядром
//
// Только встроенные модули Node.

import { execFileSync } from 'node:child_process';
import {
  appendFileSync,
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  realpathSync,
  statSync
} from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const SCRIPT = fileURLToPath(import.meta.url);
const SCRIPT_DIR = path.dirname(SCRIPT);

// имя отчёта (как _TL_KEY_RE в tidy-lib.sh) и этапа сессии
const REPORT_RE = /^\d{8}-\d{6}-[0-9a-f]{7,40}(-dirty)?(-\d{2}-.+)?$/;
const STAGE_RE = /^\d{2}-[A-Za-z0-9._-]+$/;
const SHA_RE = /^[0-9a-f]{40}$/;
const SUITES = new Set([
  'full',
  'changed',
  'smoke',
  'session',
  'harness',
  'rerun',
  'bisect',
  'delta',
  'reboot'
]);
const STATUSES = new Set(['PASS', 'FAIL', 'KNOWN', 'XPASS', 'SKIP', 'QPASS', 'QFAIL']);
const CLASSES = new Set(['new', 'flaky', 'stand', 'known', 'xpass', 'quarantine']);
const CORES = new Set(['xray', 'mihomo']);
const COLUMNS = [
  'finished_utc',
  'sha',
  'suite',
  'release',
  'report',
  'target',
  'core',
  'check',
  'status',
  'class',
  'seconds'
];

// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;]*m/g;
// то же, что cleanName в branch-report.mjs и clean в pw-parse.js: имя проверки pw:
function cleanName(text) {
  const flat = String(text || '')
    .replace(ANSI, '')
    .replace(/\s+/g, ' ')
    .trim();
  return flat.length > 300 ? flat.slice(0, 300) + '…' : flat;
}

function git(args, cwd) {
  return execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim();
}

let pathsCache = null;
export function historyPaths() {
  if (pathsCache) return pathsCache;
  const common = git(['rev-parse', '--path-format=absolute', '--git-common-dir'], SCRIPT_DIR);
  const root = path.dirname(common);
  const dir = path.join(root, 'build', 'router');
  pathsCache = {
    root,
    dir,
    file: path.join(dir, 'history.tsv'),
    idx: path.join(dir, 'history.idx')
  };
  return pathsCache;
}

function readText(file) {
  try {
    return readFileSync(file, 'utf8');
  } catch {
    return '';
  }
}

function isDir(p) {
  try {
    return statSync(p).isDirectory();
  } catch {
    return false;
  }
}

function mtime(p) {
  try {
    return statSync(p).mtime;
  } catch {
    return null;
  }
}

function iso(date) {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

// поле истории: без табуляций и переводов строки
function field(v) {
  const s = String(v ?? '').replace(/[\t\r\n]+/g, ' ').trim();
  return s === '' ? '-' : s;
}

export function readHistory(opts = {}) {
  const file = opts.file || historyPaths().file;
  const rows = [];
  for (const line of readText(file).split('\n')) {
    if (!line) continue;
    const parts = line.split('\t');
    if (parts.length !== COLUMNS.length) continue;
    const row = {};
    COLUMNS.forEach((c, i) => {
      row[c] = parts[i];
    });
    const sec = Number(row.seconds);
    row.seconds = row.seconds !== '-' && Number.isFinite(sec) ? sec : null;
    rows.push(row);
  }
  return rows;
}

function readIdx(idx) {
  return new Set(
    readText(idx)
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean)
  );
}

export function listReports(dir) {
  let names = [];
  try {
    names = readdirSync(dir);
  } catch {
    return [];
  }
  return names.filter((n) => REPORT_RE.test(n) && isDir(path.join(dir, n))).sort();
}

// sha, набор и релиз отчёта (каталога прогона или этапа)
function reportInfo(dir) {
  const meta = readText(path.join(dir, 'meta'));
  const md = readText(path.join(dir, 'report.md'));
  let sha = (meta.match(/^sha=([0-9a-f]{40})\s*$/m) || [])[1];
  if (!sha) sha = (md.match(/^- SHA: ([0-9a-f]{40})\b/m) || [])[1];
  let suite = (md.match(/^- Набор: (\S+)/m) || [])[1] || '-';
  if (!SUITES.has(suite)) suite = '-';
  let release = (md.match(/^- Релиз: (\S+)/m) || [])[1] || '-';
  if (!/^[A-Za-z0-9._+-]+$/.test(release)) release = '-';
  return { sha: sha && SHA_RE.test(sha) ? sha : null, suite, release };
}

function targetsOf(dir) {
  let names = [];
  try {
    names = readdirSync(dir);
  } catch {
    return [];
  }
  return names
    .filter((n) => /^[a-z0-9]+$/.test(n) && existsSync(path.join(dir, n, 'results.tsv')))
    .sort();
}

function list(dir, re) {
  try {
    return readdirSync(dir).filter((n) => re.test(n));
  } catch {
    return [];
  }
}

// секунды проверок ядра: Map проверка → секунды (go: последнее событие, pw: сумма)
function secondsOf(coreDir) {
  const out = new Map();
  for (const f of list(coreDir, /^go-.*\.jsonl$/)) {
    for (const line of readText(path.join(coreDir, f)).split('\n')) {
      if (!line.includes('"Elapsed"')) continue;
      let ev;
      try {
        ev = JSON.parse(line);
      } catch {
        continue;
      }
      if (!['pass', 'fail', 'skip'].includes(ev.Action)) continue;
      if (!ev.Test || typeof ev.Elapsed !== 'number') continue;
      out.set(`go:${ev.Test}`, ev.Elapsed);
    }
  }
  const pw = new Map();
  for (const sub of list(coreDir, /^pw/)) {
    const base = path.join(coreDir, sub);
    if (!isDir(base)) continue;
    for (const f of list(base, /^report.*\.json$/)) {
      let rep;
      try {
        rep = JSON.parse(readFileSync(path.join(base, f), 'utf8'));
      } catch {
        continue;
      }
      // имя — как в pw-parse.js: titles через « > », верхний набор с title, равным file, пропускается
      const visit = (node, titles) => {
        for (const spec of node.specs || []) {
          const name = `pw:${cleanName([...titles, spec.title].join(' > '))}`;
          for (const test of spec.tests || []) {
            let ms = 0;
            for (const r of test.results || []) ms += Number(r.duration) || 0;
            pw.set(name, (pw.get(name) || 0) + ms);
          }
        }
        for (const child of node.suites || []) visit(child, [...titles, child.title]);
      };
      for (const top of rep.suites || []) visit(top, top.file === top.title ? [] : [top.title]);
    }
  }
  for (const [name, ms] of pw) out.set(name, ms / 1000);
  return out;
}

// класс проверки из <цель>/classes.tsv: класс, ядро, проверка, деталь (деталь не читается)
function classesOf(targetDir) {
  const out = new Map();
  for (const line of readText(path.join(targetDir, 'classes.tsv')).split('\n')) {
    const [cls, core, check] = line.split('\t');
    if (!check || !CLASSES.has(cls)) continue;
    out.set(`${core}\t${check}`, cls);
  }
  return out;
}

function fmtSeconds(sec) {
  if (typeof sec !== 'number' || !Number.isFinite(sec)) return '-';
  return String(Math.round(sec * 100) / 100);
}

// строки истории одного каталога в формате прогона (отчёт или этап сессии)
function rowsOf(dir, name, info) {
  const targets = targetsOf(dir);
  let finished = mtime(path.join(dir, 'summary.md'));
  if (!finished) {
    for (const t of targets) {
      const m = mtime(path.join(dir, t, 'results.tsv'));
      if (m && (!finished || m > finished)) finished = m;
    }
  }
  const when = iso(finished || new Date());
  const lines = [];
  for (const target of targets) {
    const tdir = path.join(dir, target);
    const classes = classesOf(tdir);
    const secs = new Map();
    for (const line of readText(path.join(tdir, 'results.tsv')).split('\n')) {
      const parts = line.split('\t');
      if (parts.length < 3) continue;
      // деталь (parts[3]) не читается
      const [status, rawCore, check] = parts;
      if (!STATUSES.has(status)) continue;
      if (!check || /[\r\n]/.test(check)) continue;
      const core = CORES.has(rawCore) ? rawCore : '-';
      let sec;
      if (core !== '-' && (check.startsWith('go:') || check.startsWith('pw:'))) {
        if (!secs.has(core)) secs.set(core, secondsOf(path.join(tdir, core)));
        sec = secs.get(core).get(check);
      }
      const row = [
        when,
        info.sha,
        info.suite,
        info.release,
        name,
        target,
        core,
        check,
        status,
        classes.get(`${core}\t${check}`) || '-',
        fmtSeconds(sec)
      ].map(field);
      lines.push(row.join('\t'));
    }
  }
  return lines;
}

// путь отчёта: абсолютный, относительный от текущего каталога или имя в build/router основной
// копии; допустим только каталог верхнего уровня build/router основной копии или текущей
// рабочей копии
function resolveReport(reportDir) {
  const { dir: mainDir } = historyPaths();
  let p = path.resolve(String(reportDir || ''));
  if (!existsSync(p) && REPORT_RE.test(String(reportDir)) && !String(reportDir).includes('/')) {
    p = path.join(mainDir, String(reportDir));
  }
  if (!isDir(p)) throw new Error(`отчёт не найден: ${reportDir}`);
  const real = realpathSync(p);
  const bases = [mainDir];
  try {
    bases.push(path.join(git(['rev-parse', '--show-toplevel'], process.cwd()), 'build', 'router'));
  } catch {
    // вне рабочей копии — только основная
  }
  for (const b of bases) {
    let rb;
    try {
      rb = realpathSync(b);
    } catch {
      continue;
    }
    const rel = path.relative(rb, real);
    if (rel && !rel.startsWith('..') && !path.isAbsolute(rel) && !rel.includes(path.sep)) {
      if (!REPORT_RE.test(rel)) throw new Error(`не имя отчёта: ${rel}`);
      return { dir: real, name: rel };
    }
  }
  throw new Error(`отчёт вне build/router: ${reportDir}`);
}

export function appendReport(reportDir) {
  const { dir, name } = resolveReport(reportDir);
  const { file, idx } = historyPaths();
  mkdirSync(path.dirname(file), { recursive: true });
  const done = readIdx(idx);
  const stages = list(dir, STAGE_RE)
    .filter((n) => isDir(path.join(dir, n)))
    .sort();
  // сессия — каталоги этапов <NN>-<этап>; прогон — цели в самом отчёте
  const units =
    stages.length > 0
      ? stages.map((s) => ({ dir: path.join(dir, s), name: `${name}/${s}`, session: true }))
      : [{ dir, name, session: false }];
  const top = reportInfo(dir);
  let added = 0;
  for (const u of units) {
    if (done.has(u.name)) continue;
    const info = u.session ? reportInfo(u.dir) : top;
    if (!info.sha) info.sha = top.sha;
    if (u.session) info.suite = 'session';
    if (!info.sha) {
      // без SHA строки бесполезны: отчёт не отмечается, ошибка видна в stderr
      console.error(`history: нет SHA в отчёте ${u.name}, пропущен`);
      continue;
    }
    const lines = rowsOf(u.dir, u.name, info);
    if (lines.length > 0) appendFileSync(file, lines.join('\n') + '\n');
    appendFileSync(idx, `${u.name}\n`);
    done.add(u.name);
    added += lines.length;
  }
  return added;
}

export function backfill() {
  const { dir } = historyPaths();
  let reports = 0;
  let rows = 0;
  for (const name of listReports(dir)) {
    const n = appendReport(path.join(dir, name));
    reports += 1;
    rows += n;
  }
  return { reports, rows };
}

export function median(numbers) {
  const s = [...numbers].sort((a, b) => a - b);
  if (s.length === 0) return null;
  const m = Math.floor(s.length / 2);
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

function inWindow(row, days, now = Date.now()) {
  if (!days) return true;
  const t = Date.parse(row.finished_utc);
  return Number.isFinite(t) && now - t <= days * 86400 * 1000;
}

export function durations(rows, opts = {}) {
  const { suite, days, core } = opts;
  const acc = new Map();
  for (const r of rows) {
    if (r.seconds === null || r.seconds === undefined) continue;
    if (suite && r.suite !== suite) continue;
    if (core && r.core !== core) continue;
    if (!inWindow(r, days)) continue;
    const key = `${r.check}\t${r.core}`;
    if (!acc.has(key)) acc.set(key, []);
    acc.get(key).push(r.seconds);
  }
  const out = new Map();
  for (const [key, list] of acc) out.set(key, { median: median(list), runs: list.length });
  return out;
}

// падение — FAIL, XPASS и QFAIL; прогон — любой статус, кроме SKIP
const FAIL_STATUSES = new Set(['FAIL', 'XPASS', 'QFAIL']);

export function failFreq(rows, opts = {}) {
  const { days } = opts;
  const out = new Map();
  for (const r of rows) {
    if (r.status === 'SKIP') continue;
    if (!inWindow(r, days)) continue;
    const v = out.get(r.check) || { fails: 0, runs: 0 };
    v.runs += 1;
    if (FAIL_STATUSES.has(r.status)) v.fails += 1;
    out.set(r.check, v);
  }
  return out;
}

// отчёты истории: Map имя → { report, sha, release, suite, finished, rows }
function byReport(rows) {
  const out = new Map();
  for (const r of rows) {
    let v = out.get(r.report);
    if (!v) {
      v = {
        report: r.report,
        sha: r.sha,
        release: r.release,
        suite: r.suite,
        finished: r.finished_utc,
        rows: []
      };
      out.set(r.report, v);
    }
    if (r.finished_utc > v.finished) v.finished = r.finished_utc;
    v.rows.push(r);
  }
  return out;
}

// последний полный прогон (suite=full, не этап сессии) по finished_utc; before — строго раньше
// (ISO); green — без строк FAIL и XPASS
export function lastFull(rows, opts = {}) {
  const { green, before } = opts;
  let best = null;
  for (const v of byReport(rows).values()) {
    if (v.suite !== 'full' || v.report.includes('/')) continue;
    if (before && !(v.finished < before)) continue;
    if (green && v.rows.some((r) => r.status === 'FAIL' || r.status === 'XPASS')) continue;
    if (!best || v.finished > best.finished || (v.finished === best.finished && v.report > best.report))
      best = v;
  }
  return best
    ? { report: best.report, sha: best.sha, release: best.release, finished: best.finished }
    : null;
}

// тесты отчёта: верхние go: (без подтестов) и pw:; Map check → секунды по всем ядрам
function testsOf(reportRows) {
  const keys = new Set();
  const secs = new Map();
  let timed = false;
  for (const r of reportRows) {
    const top = (r.check.startsWith('go:') && !r.check.includes('/')) || r.check.startsWith('pw:');
    if (!top || r.core === '-') continue;
    keys.add(`${r.check}\t${r.core}`);
    if (!secs.has(r.check)) secs.set(r.check, null);
    if (r.seconds !== null) {
      secs.set(r.check, (secs.get(r.check) || 0) + r.seconds);
      timed = true;
    }
  }
  let total = 0;
  for (const s of secs.values()) total += s || 0;
  // без секунд (в старом отчёте нет go-*.jsonl и report*.json) время неизвестно
  return { count: keys.size, secs, total: timed ? total : null };
}

function signed(n, digits = 0) {
  const v = digits ? Number(n.toFixed(digits)) : Math.round(n);
  const s = digits ? v.toFixed(digits) : String(v);
  return v >= 0 ? `+${s}` : s;
}

// строки Markdown тренда отчёта против прошлого полного прогона (D-16)
export function trendLines(rows, reportName) {
  const reports = byReport(rows);
  const cur = reports.get(reportName);
  if (!cur) return [`- Тренд: отчёта ${reportName} в истории нет`];
  const prevRef = lastFull(rows, { before: cur.finished });
  if (!prevRef) return ['- Тренд: прошлого полного прогона в истории нет'];
  const prev = reports.get(prevRef.report);
  const a = testsOf(cur.rows);
  const b = testsOf(prev.rows);
  const mins = (t) => (t === null ? 'н/д' : `${(t / 60).toFixed(1)} мин`);
  const delta = a.total !== null && b.total !== null ? `, ${signed((a.total - b.total) / 60, 1)}` : '';
  const lines = [
    `- Тренд: тестов ${a.count} (было ${b.count}, ${signed(a.count - b.count)}), ` +
      `время тестов ${mins(a.total)} (было ${mins(b.total)}${delta}); ` +
      `прошлый полный — ${prevRef.report}`
  ];
  const fresh = [...a.secs]
    .filter(([check]) => !b.secs.has(check))
    .sort((x, y) => (y[1] ?? -1) - (x[1] ?? -1))
    .slice(0, 5)
    .map(([check, s]) => (s === null ? check : `${check} (${Math.round(s)} с)`));
  lines.push(`- Новые тесты: ${fresh.length > 0 ? fresh.join(', ') : 'нет'}`);
  return lines;
}

// --- CLI ---------------------------------------------------------------------------------

function usage(msg) {
  if (msg) console.error(`history: ${msg}`);
  console.error(
    'usage: history.mjs append <отчёт> | backfill | durations [--suite s] [--days N] [--core c]' +
      ' | failfreq [--days N] | last-full [--green] | trend <отчёт>'
  );
  process.exit(2);
}

// allowed — опции со значением, flags — без значения
function parseOpts(args, allowed, flags = []) {
  const opts = {};
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    const key = a.replace(/^--/, '');
    if (a.startsWith('--') && flags.includes(key)) {
      opts[key] = true;
      continue;
    }
    if (!a.startsWith('--') || !allowed.includes(key)) usage(`неизвестный аргумент: ${a}`);
    const v = args[i + 1];
    if (v === undefined) usage(`нет значения у ${a}`);
    i += 1;
    if (key === 'days') {
      if (!/^\d+$/.test(v)) usage(`--days: целое число: ${v}`);
      opts.days = Number(v);
    } else {
      opts[key] = v;
    }
  }
  return opts;
}

function main(argv) {
  const [cmd, ...args] = argv;
  switch (cmd) {
    case 'append': {
      if (args.length !== 1) usage('append: нужен один отчёт');
      console.log(appendReport(args[0]));
      return;
    }
    case 'backfill': {
      if (args.length !== 0) usage('backfill: аргументов нет');
      const { reports, rows } = backfill();
      console.error(`history: отчётов просмотрено ${reports}, строк добавлено ${rows}`);
      return;
    }
    case 'durations': {
      const opts = parseOpts(args, ['suite', 'days', 'core']);
      const d = [...durations(readHistory(), opts)].sort((a, b) => b[1].median - a[1].median);
      const out = d.map(([key, v]) => `${key}\t${fmtSeconds(v.median)}\t${v.runs}`);
      if (out.length > 0) process.stdout.write(out.join('\n') + '\n');
      return;
    }
    case 'failfreq': {
      const opts = parseOpts(args, ['days']);
      const f = [...failFreq(readHistory(), opts)].sort(
        (a, b) => b[1].fails - a[1].fails || b[1].runs - a[1].runs || (a[0] < b[0] ? -1 : 1)
      );
      const out = f.map(([check, v]) => `${check}\t${v.fails}\t${v.runs}`);
      if (out.length > 0) process.stdout.write(out.join('\n') + '\n');
      return;
    }
    case 'last-full': {
      const opts = parseOpts(args, [], ['green']);
      const r = lastFull(readHistory(), opts);
      if (!r) {
        console.error('history: полного прогона в истории нет');
        process.exit(1);
      }
      console.log([r.report, r.sha, r.release].join('\t'));
      return;
    }
    case 'trend': {
      if (args.length !== 1) usage('trend: нужно имя отчёта');
      console.log(trendLines(readHistory(), args[0]).join('\n'));
      return;
    }
    default:
      usage(cmd ? `неизвестная команда: ${cmd}` : 'нет команды');
  }
}

function isMain() {
  try {
    return realpathSync(process.argv[1] || '') === realpathSync(SCRIPT);
  } catch {
    return false;
  }
}

if (isMain()) {
  // вывод в head и подобное: закрытый канал — не ошибка
  process.stdout.on('error', (e) => {
    if (e.code === 'EPIPE') process.exit(0);
    throw e;
  });
  try {
    main(process.argv.slice(2));
  } catch (e) {
    console.error(`history: ${e.message}`);
    process.exit(1);
  }
}
