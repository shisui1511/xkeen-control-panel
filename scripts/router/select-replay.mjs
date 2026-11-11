#!/usr/bin/env node
// Прогон выбора гейта по слитым PR с оценкой времени (148.1-09, D-08; снимок — 148.1.1).
//
// Для каждого слияния в first-parent истории origin/main за период запускает
// `select-changed.mjs --base <первый родитель> --head <слияние> --explain` и печатает таблицу:
// вид выбора (полный | обвязка | зависимости | области | смоук), области, число Go-тестов и
// спеков первой ветви, оценку времени гейта в минутах. Нужна, чтобы цели гейта фиксировать по
// факту (p50, p90), а не по ощущению, и чтобы видеть, какие PR ещё уходят в полный выбор.
//
// Снимок входа. Сравнение «до/после» имеет смысл только на одном наборе PR и одних длительностях,
// поэтому вход фиксируется один раз:
//   --capture <файл>   разрешить origin/main в полный SHA, взять слияния за период до этого SHA,
//                      один раз прочитать историю длительностей и записать снимок; выбор не
//                      запускается. Файл — в build/router или во временном каталоге (вне git).
//   --snapshot <файл>  повторить выбор по снимку: живая история и origin/main не читаются.
//   без обоих          снимок строится в памяти и сразу повторяется (как раньше).
// Снимок (JSON, format/version): since и until (приведённые границы или null), originMain,
// history (момент среза, окно в днях, число строк, последняя строка), unknownSec (длительность
// теста без истории), merges — упорядоченный список { pr, base, head } (pr — номер из темы
// слияния или null: строка без номера не теряется, в отчёте её имя — SHA), medians — медианы
// секунд по проверке и ядру, hash — sha256 канонического JSON без поля hash.
//
// Границы дат. Голая дата в `git log --since` берёт текущее время суток, поэтому один и тот же
// вызов в разное время суток выбирал бы разные слияния. Дата `ГГГГ-ММ-ДД` без времени
// приводится: у --since к `ГГГГ-ММ-ДД 00:00:00`, у --until к `ГГГГ-ММ-ДД 23:59:59` (местное
// время); значение со временем передаётся без изменений. По умолчанию --since 2026-10-15.
//
// --selector-root <каталог> — запускать select-changed.mjs из другой рабочей копии того же
// репозитория (например, прежней ревизии выбора для сравнения «до»). Наличие коммитов пар
// проверяется локально: ничего не скачивается, checkout не меняется, панель не собирается и не
// запускается, выбранные тесты не исполняются.
//
// Оценка минут (по медианам снимка):
//   сумма медиан секунд тестов первой ветви (RT_GO_SEQ или тесты пакетов RT_GO_PKGS, спеки
//   RT_SPECS с учётом RT_PW_GREP; спеки только чтения делятся на число воркеров 3) плюс тесты
//   второй ветви (RT_GO_CORE_RUN и тесты спеков RT_SPECS_CORE с тегом ядра) плюс оболочка
//   4,5 мин (деплой, смоук, снимок). Медиана ядер усредняется: ядро ветви зависит от SHA.
//   Тест без истории — unknownSec снимка (30 с). Полный выбор — сумма медиан всех тестов по двум
//   ядрам плюс оболочка.
// Оценка грубая: факты тестов для оценки берутся из рабочей копии этого скрипта, а не из коммита
// слияния, поэтому для давних PR набор тестов мог быть другим.
//
// Две оценки (p50/p90 методом ближайшего ранга). «Все PR» — по каждой строке с оценкой. «Целевая»
// — без строк, у которых селектор назвал обязательное основание полного выбора: строка stderr
// `  - обязательный полный выбор: <основание>`. У прежней ревизии выбора (без этой строки)
// основанием считается только однозначная причина `всё: <файл механизма выбора>: правка самого
// выбора` (select-changed.mjs, core-marks.cjs, import-graph.mjs); прочие полные выборы (правка
// areas.json в прежних правилах, защитный откат, непонятная причина) остаются в целевой выборке
// как диагностика. Цели p50 ≤ 20 и p90 ≤ 45 мин относятся к целевой оценке; исключённые PR
// перечисляются с причинами. Пустая целевая выборка — отсутствие оценки, а не ноль.
//
// Ошибки выбора. Строка PR — ошибка, если селектор завершился не нулём (код 3 — выбор
// остановлен, причины «стоп»), сигналом, не запустился, нет коммита пары или в stdout нет хотя бы
// одного обязательного поля RT_* (пустое значение допустимо, отсутствие — нет). Ошибка остаётся в
// таблице с PR, кодом или сигналом и очищенной диагностикой; оценка незавершена (complete=false),
// вердикт о целях не выносится, код выхода 1.
//
// --json — тот же расчёт одним объектом JSON в stdout: snapshot (хеш снимка), originMain, since,
// until, rows (pr, id, base, head, kind, areas, go, specs, requiredFullReasons, minutes, why,
// error), all и target ({ p50, p90 } или null без оценки), excluded ([{ pr, id, reasons }]),
// errors (число), complete.
//
// Выводится только номер PR из темы слияния (`#<число>`) или SHA слияния; имена веток не
// печатаются. Диагностика селектора очищается: управляющие символы убираются, абсолютные пути
// рабочих копий и домашнего каталога заменяются, число и длина строк ограничены.
//
// Использование (из корня репозитория):
//   node scripts/router/select-replay.mjs [--since <дата>] [--until <дата>] [--selector-root <каталог>] [--json]
//   node scripts/router/select-replay.mjs [--since <дата>] [--until <дата>] --capture <файл>
//   node scripts/router/select-replay.mjs --snapshot <файл> [--selector-root <каталог>] [--json]
// Таблица Markdown: первая ячейка строки PR — `#<номер>` (или SHA слияния); затем итоговые строки.
// Код выхода: 0 — оценка завершена, 1 — ошибка выбора хотя бы одного PR или ошибка входа, 2 — аргументы.

import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import coreMarks from './core-marks.cjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const SELECTOR = 'scripts/router/select-changed.mjs';
const SHELL_MIN = 4.5;
const UNKNOWN_SEC = 30;
const READ_WORKERS = 3;
const HISTORY_DAYS = 60;
const CORES = ['xray', 'mihomo'];
const DEFAULT_SINCE = '2026-10-15';
const KINDS = ['полный', 'обвязка', 'зависимости', 'области', 'смоук'];
const GOALS = { p50: 20, p90: 45, support: 15 };
const SNAPSHOT_FORMAT = 'xcp-select-replay-snapshot';
const SNAPSHOT_VERSION = 1;
const SHA_RE = /^[0-9a-f]{40}$/;
const DAY_RE = /^[0-9]{4}-[0-9]{2}-[0-9]{2}$/;
const DATE_RE = /^[0-9]{4}-[0-9]{2}-[0-9]{2}([ T][0-9:]+)?$/;
// Поля вывода селектора: все обязательны (значение может быть пустым)
const RT_FIELDS = [
  'SELECT_ALL',
  'GO_PKGS',
  'SPECS',
  'PW_GREP',
  'GO_CORE_PKGS',
  'GO_CORE_RUN',
  'SPECS_CORE',
  'KERNEL_TOUCHED',
  'GO_SEQ',
  'SPECS_FIRST',
  'PW_INVERT',
  'HARNESS',
  'AREAS'
];
const RT_FLAGS = ['SELECT_ALL', 'KERNEL_TOUCHED', 'HARNESS'];
// Код выхода селектора «выбор остановлен» (D-24, D-28)
const SELECT_STOP = 3;
// Прежняя ревизия выбора: однозначная причина полного выбора — правка механизма выбора
const LEGACY_SELF = /^всё: (scripts\/router\/(?:select-changed\.mjs|core-marks\.cjs|import-graph\.mjs)): правка самого выбора$/;
const DIAG_LINES = 8;
const DIAG_WIDTH = 300;

const USAGE = [
  'Использование:',
  '  node scripts/router/select-replay.mjs [--since <дата>] [--until <дата>] [--selector-root <каталог>] [--json]',
  '  node scripts/router/select-replay.mjs [--since <дата>] [--until <дата>] --capture <файл>',
  '  node scripts/router/select-replay.mjs --snapshot <файл> [--selector-root <каталог>] [--json]'
].join('\n');

function usage(msg) {
  if (msg) console.error(`select-replay: ${msg}`);
  console.error(USAGE);
  process.exit(2);
}

function fail(msg) {
  console.error(`select-replay: ${msg}`);
  process.exit(1);
}

function parseArgs(argv) {
  const out = { since: null, until: null, capture: null, snapshot: null, selectorRoot: null, json: false };
  const value = (i, name) => {
    const v = argv[i];
    if (v === undefined || v === '') usage(`${name}: нужно значение`);
    return v;
  };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--since') out.since = value(++i, a);
    else if (a === '--until') out.until = value(++i, a);
    else if (a === '--capture') out.capture = value(++i, a);
    else if (a === '--snapshot') out.snapshot = value(++i, a);
    else if (a === '--selector-root') out.selectorRoot = value(++i, a);
    else if (a === '--json') out.json = true;
    else usage(`неизвестный аргумент ${a}`);
  }
  if (out.capture && out.snapshot) usage('--capture и --snapshot взаимоисключающие');
  if (out.capture && out.json) usage('--json относится к повтору выбора, не к --capture');
  if (out.snapshot && (out.since || out.until)) usage('границы периода берутся из снимка: --since и --until с --snapshot не задаются');
  if (out.capture && out.selectorRoot) usage('--selector-root относится к повтору выбора, не к --capture');
  for (const v of [out.since, out.until]) {
    if (v && !DATE_RE.test(v)) usage(`дата ${v}: нужна форма ГГГГ-ММ-ДД`);
  }
  return out;
}

/** Дата без времени — к началу (since) или концу (until) суток; со временем — без изменений. */
function normalizeBound(value, end) {
  if (!value) return null;
  return DAY_RE.test(value) ? `${value} ${end ? '23:59:59' : '00:00:00'}` : value;
}

function git(args, cwd = ROOT, input) {
  const r = spawnSync('git', args, { cwd, encoding: 'utf8', input, maxBuffer: 64 * 1024 * 1024 });
  if (r.status !== 0) throw new Error(`git ${args[0]}: ${(r.stderr || '').split('\n', 1)[0] || `код ${r.status}`}`);
  return r.stdout;
}

function resolveOriginMain() {
  const sha = git(['rev-parse', '--verify', '--quiet', 'origin/main^{commit}']).trim();
  if (!SHA_RE.test(sha)) throw new Error(`origin/main не разрешён в полный SHA (${sha || 'пусто'})`);
  return sha;
}

/** Слияния first-parent от tip за период: по возрастанию времени, строки без номера PR сохраняются. */
function gitLog({ since, until, tip }) {
  const args = ['log', '--first-parent', '--merges', tip, `--since=${since}`];
  if (until) args.push(`--until=${until}`);
  args.push('--format=%H %P\t%s');
  const rows = [];
  for (const line of git(args).split('\n').filter(Boolean)) {
    const tab = line.indexOf('\t');
    const head = tab < 0 ? line : line.slice(0, tab);
    const subject = tab < 0 ? '' : line.slice(tab + 1);
    const [merge, first] = head.split(' ');
    if (!SHA_RE.test(merge || '') || !SHA_RE.test(first || '')) {
      throw new Error(`git log: строка без полного SHA слияния и первого родителя: ${head}`);
    }
    const m = /#(\d+)/.exec(subject);
    rows.push({ pr: m ? m[1] : null, base: first, head: merge });
  }
  // по возрастанию времени: старые слияния первыми
  return rows.reverse();
}

/** Канонический JSON: ключи объектов по алфавиту, массивы в исходном порядке. */
function canonical(v) {
  if (Array.isArray(v)) return `[${v.map(canonical).join(',')}]`;
  if (v && typeof v === 'object') {
    return `{${Object.keys(v)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${canonical(v[k])}`)
      .join(',')}}`;
  }
  return JSON.stringify(v);
}

function snapshotHash(snap) {
  const rest = { ...snap };
  delete rest.hash;
  return createHash('sha256').update(canonical(rest)).digest('hex');
}

/** Медианы длительностей на зафиксированный момент: окно считается от at, а не от момента повтора. */
async function captureMedians(at) {
  const hist = await import(pathToFileURL(path.join(ROOT, 'scripts/router/history.mjs')));
  const span = HISTORY_DAYS * 86400 * 1000;
  const rows = hist.readHistory().filter((r) => {
    const t = Date.parse(r.finished_utc);
    return Number.isFinite(t) && t <= at && at - t <= span;
  });
  const dur = hist.durations(rows);
  const medians = {};
  for (const [key, { median }] of dur) {
    const [check, core] = key.split('\t');
    if (!CORES.includes(core) || typeof median !== 'number' || !Number.isFinite(median)) continue;
    if (!medians[check]) medians[check] = {};
    medians[check][core] = median;
  }
  const latest = rows.reduce((m, r) => (r.finished_utc > m ? r.finished_utc : m), '');
  return { medians, rows: rows.length, latest: latest || null };
}

async function buildSnapshot(args) {
  const since = normalizeBound(args.since || DEFAULT_SINCE, false);
  const until = normalizeBound(args.until, true);
  const originMain = resolveOriginMain();
  const merges = gitLog({ since, until, tip: originMain });
  if (merges.length === 0) throw new Error(`слияний за период ${since} … ${until || originMain} не найдено`);
  const at = Date.now();
  const hist = await captureMedians(at);
  if (Object.keys(hist.medians).length === 0) {
    throw new Error(`история длительностей за ${HISTORY_DAYS} дней пуста: оценка без медиан не имеет смысла`);
  }
  const snap = {
    format: SNAPSHOT_FORMAT,
    version: SNAPSHOT_VERSION,
    since,
    until,
    originMain,
    history: {
      at: new Date(at).toISOString(),
      days: HISTORY_DAYS,
      rows: hist.rows,
      latest: hist.latest
    },
    unknownSec: UNKNOWN_SEC,
    merges,
    medians: hist.medians
  };
  snap.hash = snapshotHash(snap);
  return snap;
}

/** Проверка снимка: версия, поля, SHA, состав и контрольный хеш; ошибка — исключение с причиной. */
function validateSnapshot(snap) {
  const isObj = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
  if (!isObj(snap)) throw new Error('не объект JSON');
  if (snap.format !== SNAPSHOT_FORMAT) throw new Error(`формат ${JSON.stringify(snap.format)}, ожидается ${SNAPSHOT_FORMAT}`);
  if (snap.version !== SNAPSHOT_VERSION) throw new Error(`версия ${JSON.stringify(snap.version)}, поддерживается ${SNAPSHOT_VERSION}`);
  const keys = ['format', 'version', 'since', 'until', 'originMain', 'history', 'unknownSec', 'merges', 'medians', 'hash'];
  const extra = Object.keys(snap).filter((k) => !keys.includes(k));
  if (extra.length > 0) throw new Error(`лишние поля: ${extra.join(', ')}`);
  if (typeof snap.hash !== 'string' || !/^[0-9a-f]{64}$/.test(snap.hash)) throw new Error('нет контрольного хеша');
  if (snapshotHash(snap) !== snap.hash) throw new Error('контрольный хеш не совпадает: снимок изменён');
  if (typeof snap.since !== 'string' || !DATE_RE.test(snap.since)) throw new Error('since: нужна дата');
  if (snap.until !== null && (typeof snap.until !== 'string' || !DATE_RE.test(snap.until))) {
    throw new Error('until: нужна дата или null');
  }
  if (!SHA_RE.test(snap.originMain || '')) throw new Error('originMain: нужен полный SHA');
  const h = snap.history;
  if (!isObj(h) || !Number.isFinite(Date.parse(h.at)) || !Number.isInteger(h.days) || h.days <= 0) {
    throw new Error('history: нужны момент среза at и окно days');
  }
  if (!Number.isInteger(h.rows) || h.rows < 0 || (h.latest !== null && typeof h.latest !== 'string')) {
    throw new Error('history: нужны rows и latest');
  }
  if (typeof snap.unknownSec !== 'number' || !Number.isFinite(snap.unknownSec) || snap.unknownSec < 0) {
    throw new Error('unknownSec: нужно неотрицательное число');
  }
  if (!Array.isArray(snap.merges) || snap.merges.length === 0) throw new Error('merges: пустой список слияний');
  const heads = new Set();
  snap.merges.forEach((m, i) => {
    if (!isObj(m) || Object.keys(m).sort().join(',') !== 'base,head,pr') throw new Error(`merges[${i}]: нужны поля pr, base, head`);
    if (m.pr !== null && (typeof m.pr !== 'string' || !/^[0-9]+$/.test(m.pr))) throw new Error(`merges[${i}].pr: номер или null`);
    if (!SHA_RE.test(m.base) || !SHA_RE.test(m.head)) throw new Error(`merges[${i}]: base и head — полные SHA`);
    if (heads.has(m.head)) throw new Error(`merges[${i}]: слияние ${m.head} повторяется`);
    heads.add(m.head);
  });
  if (!isObj(snap.medians) || Object.keys(snap.medians).length === 0) throw new Error('medians: пусто');
  for (const [check, byCore] of Object.entries(snap.medians)) {
    const ok =
      isObj(byCore) &&
      Object.keys(byCore).length > 0 &&
      Object.entries(byCore).every(([c, v]) => CORES.includes(c) && typeof v === 'number' && Number.isFinite(v) && v >= 0);
    if (!ok) throw new Error(`medians[${JSON.stringify(check)}]: нужны секунды по ядрам ${CORES.join(', ')}`);
  }
  return snap;
}

function loadSnapshot(file) {
  let text;
  try {
    text = readFileSync(file, 'utf8');
  } catch (e) {
    fail(`снимок ${file}: не прочитан (${e.code || e.message})`);
  }
  if (!text.trim()) fail(`снимок ${file}: пустой файл`);
  try {
    return validateSnapshot(JSON.parse(text));
  } catch (e) {
    fail(`снимок ${file}: ${e.message}`);
  }
  return null;
}

/** Снимок не должен попасть в git: внутри рабочей копии — только игнорируемый путь (build/router). */
function writeSnapshot(file, snap) {
  const abs = path.resolve(file);
  const rel = path.relative(ROOT, abs);
  if (!rel.startsWith('..') && !path.isAbsolute(rel)) {
    const r = spawnSync('git', ['check-ignore', '-q', '--', rel], { cwd: ROOT });
    if (r.status !== 0) fail(`снимок ${file}: путь в рабочей копии не игнорируется git — используйте build/router или временный каталог`);
  }
  mkdirSync(path.dirname(abs), { recursive: true });
  writeFileSync(abs, `${JSON.stringify(snap, null, 2)}\n`);
}

/** Рабочая копия выбора: корень git с select-changed.mjs; коммиты пар должны быть локально. */
function checkSelectorRoot(dir, merges) {
  const abs = path.resolve(dir);
  if (!existsSync(path.join(abs, SELECTOR))) throw new Error(`${dir}: нет ${SELECTOR}`);
  let top;
  try {
    top = git(['rev-parse', '--show-toplevel'], abs).trim();
  } catch (e) {
    throw new Error(`${dir}: не рабочая копия git (${e.message})`);
  }
  if (realpathSync(top) !== realpathSync(abs)) throw new Error(`${dir}: не корень рабочей копии (корень — ${top})`);
  const shas = [...new Set(merges.flatMap((m) => [m.base, m.head]))];
  const out = git(['cat-file', '--batch-check=%(objectname) %(objecttype)'], abs, shas.map((s) => `${s}^{commit}`).join('\n') + '\n');
  const missing = new Set();
  out
    .split('\n')
    .filter(Boolean)
    .forEach((line, i) => {
      if (!/^[0-9a-f]{40} commit$/.test(line)) missing.add(shas[i]);
    });
  return { root: abs, missing };
}

function parseSelect(stdout, stderr) {
  const vars = {};
  for (const line of stdout.split('\n')) {
    const m = /^RT_([A-Z_]+)=(.*)$/.exec(line);
    if (m) vars[m[1]] = m[2].replace(/^'(.*)'$/, '$1');
  }
  // причины --explain: строки «  - <причина>» после «причины:»
  const reasons = [];
  for (const line of stderr.split('\n')) {
    const m = /^ {2}- (.+)$/.exec(line);
    if (m) reasons.push(m[1].trim());
  }
  const after = (prefix) => reasons.filter((r) => r.startsWith(prefix)).map((r) => r.slice(prefix.length).trim());
  const kind = after('вид выбора:')[0] || null;
  const why = after('всё:')[0] || '';
  // контракт селектора: явное основание обязательного полного выбора
  const required = after('обязательный полный выбор:').filter(Boolean);
  const legacy = reasons.map((r) => LEGACY_SELF.exec(r)?.[1]).filter(Boolean).map((f) => `правка механизма выбора: ${f}`);
  const stops = reasons.filter((r) => /^стоп\b/.test(r));
  return { vars, kind, why, reasons, required, legacy, stops };
}

/** Диагностика без управляющих символов и абсолютных путей рабочих копий; ограничена по объёму. */
function cleanDiag(lines, roots, max = DIAG_LINES) {
  const home = process.env.HOME || '';
  const subs = [...new Set(roots.filter(Boolean))].sort((a, b) => b.length - a.length);
  const out = [];
  for (const raw of lines) {
    // eslint-disable-next-line no-control-regex
    let s = String(raw).replace(/\u001b\[[0-9;]*m/g, '').replace(/[\u0000-\u001f\u007f]+/g, ' ');
    for (const r of subs) s = s.split(r).join('.');
    if (home.length > 1) s = s.split(home).join('~');
    s = s.replace(/\s+/g, ' ').trim();
    if (!s) continue;
    out.push(s.length > DIAG_WIDTH ? `${s.slice(0, DIAG_WIDTH)}…` : s);
    if (out.length >= max) break;
  }
  return out;
}

/**
 * Строка PR по результату селектора: ошибка или выбор с оценкой. Ошибка — не нулевой код (3 —
 * остановка), сигнал, сбой запуска, нет обязательного поля RT_*, недопустимый флаг или
 * противоречие «обязательный полный выбор» при RT_SELECT_ALL=0.
 */
function selectRow(r, out, roots) {
  const stderrLines = (r.stderr || '').split('\n');
  const err = (message, extra = {}) => ({
    code: r.status ?? null,
    signal: r.signal ?? null,
    message,
    diagnostic: cleanDiag(extra.lines ?? (out.reasons.length > 0 ? out.reasons : stderrLines), roots),
    stops: extra.stops ?? []
  });
  if (r.error) return { error: err(`селектор не запущен: ${r.error.code || r.error.message}`) };
  if (r.signal) return { error: err(`селектор прерван сигналом ${r.signal}`) };
  if (r.status === SELECT_STOP) {
    const stops = cleanDiag(out.stops.length > 0 ? out.stops : out.reasons.length > 0 ? out.reasons : stderrLines, roots);
    return { error: err('стоп: выбор остановлен селектором', { stops, lines: stops }) };
  }
  if (r.status !== 0) {
    // без причин --explain — строки ошибки (без исходного кода и стека), иначе хвост stderr
    const errLines = stderrLines.filter((l) => /^(\w*Error\b|select-changed:)/.test(l.trim()));
    const tail = errLines.length > 0 ? errLines : stderrLines.filter((l) => l.trim()).slice(-DIAG_LINES);
    return { error: err(`селектор завершился с кодом ${r.status}`, { lines: out.reasons.length > 0 ? out.reasons : tail }) };
  }
  const missing = RT_FIELDS.filter((f) => !Object.prototype.hasOwnProperty.call(out.vars, f));
  if (missing.length > 0) return { error: err(`в выводе селектора нет полей ${missing.map((f) => `RT_${f}`).join(', ')}`) };
  const badFlag = RT_FLAGS.find((f) => out.vars[f] !== '0' && out.vars[f] !== '1');
  if (badFlag) return { error: err(`RT_${badFlag}=${JSON.stringify(out.vars[badFlag].slice(0, 20))}: ожидается 0 или 1`) };
  const full = out.vars.SELECT_ALL === '1';
  if (out.required.length > 0 && !full) {
    return { error: err('противоречие: обязательный полный выбор при RT_SELECT_ALL=0') };
  }
  // основание исключения из целевой выборки: контракт селектора, иначе однозначная прежняя причина
  const requiredFullReasons = full ? (out.required.length > 0 ? out.required : out.legacy) : [];
  return { error: null, requiredFullReasons: cleanDiag(requiredFullReasons, roots, Infinity) };
}

const median = (list) => {
  const s = [...list].sort((a, b) => a - b);
  if (s.length === 0) return null;
  const m = Math.floor(s.length / 2);
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
};

/** Процентиль по методу ближайшего ранга. */
const percentile = (list, p) => {
  const s = [...list].sort((a, b) => a - b);
  if (s.length === 0) return null;
  return s[Math.min(s.length - 1, Math.max(0, Math.ceil((p / 100) * s.length) - 1))];
};

const fmt = (n) => (n === null || n === undefined ? '-' : n.toFixed(1));

/** Модель оценки: секунды Go-тестов и спеков по медианам снимка. */
function buildModel(snap) {
  const unknown = snap.unknownSec;
  const secs = (check) => {
    const byCore = snap.medians[check];
    const vals = byCore ? CORES.map((c) => byCore[c]).filter((v) => typeof v === 'number') : [];
    return vals.length > 0 ? vals.reduce((a, b) => a + b, 0) / vals.length : null;
  };
  const facts = coreMarks.testFacts(ROOT);
  // Go: тесты пакета
  const goByPkg = new Map();
  for (const r of facts) {
    if (r.kind !== 'go') continue;
    if (!goByPkg.has(r.pkg)) goByPkg.set(r.pkg, []);
    goByPkg.get(r.pkg).push(r.test);
  }
  // Playwright: проверки истории по спекам (название теста → спек)
  const literal = new Map();
  const templates = [];
  for (const r of facts) {
    if (r.kind !== 'pw') continue;
    if (r.test) literal.set(r.test, r);
    else if (r.titleTemplate) templates.push(r);
  }
  const tplRe = templates.map((r) => ({
    row: r,
    re: new RegExp('^' + r.titleTemplate.replace(/[.*+?^()|[\]\\]/g, '\\$&').replace(/\$\{[^}]*\}/g, '.*') + '$')
  }));
  const pwBySpec = new Map(); // спек → [{ title, sec, core }]
  const pwAll = [];
  for (const check of Object.keys(snap.medians).sort()) {
    if (!check.startsWith('pw:')) continue;
    const title = check.slice(3);
    const row = literal.get(title) || tplRe.find((t) => t.re.test(title))?.row;
    const sec = secs(check);
    if (sec === null) continue;
    pwAll.push(sec);
    if (!row) continue;
    if (!pwBySpec.has(row.spec)) pwBySpec.set(row.spec, []);
    pwBySpec.get(row.spec).push({ title, sec, coreMarked: row.coreMarked });
  }
  const goAll = [...new Set(facts.filter((r) => r.kind === 'go').map((r) => r.test))].map((t) => secs(`go:${t}`) ?? unknown);
  const specKnown = (spec) => pwBySpec.has(spec);
  const isRead = (spec) => {
    const file = path.join(ROOT, 'frontend', spec);
    if (!existsSync(file)) return false;
    return readFileSync(file, 'utf8').split('\n', 5).some((l) => /^\/\/\s*e2e-kind:\s*read\b/.test(l));
  };
  return { secs, unknown, goByPkg, pwBySpec, goAll, pwAll, specKnown, isRead };
}

/** Оценка минут по выбору. */
function estimate(model, v) {
  if (v.SELECT_ALL === '1') {
    // две ветви: сумма медиан всех тестов обоих ядер (каждое ядро — полный набор)
    const one = model.goAll.reduce((a, b) => a + b, 0) + model.pwAll.reduce((a, b) => a + b, 0);
    return (2 * one) / 60 + SHELL_MIN;
  }
  const words = (s) => (s ? s.split(/\s+/).filter(Boolean) : []);
  let sec = 0;
  // первая ветвь: Go
  const seq = words(v.GO_SEQ);
  if (seq.length > 0) {
    for (const e of seq) sec += model.secs(`go:${e.slice(e.indexOf(':') + 1)}`) ?? model.unknown;
  } else {
    for (const pkg of words(v.GO_PKGS)) {
      for (const t of model.goByPkg.get(pkg.replace(/^\.\//, '')) || []) sec += model.secs(`go:${t}`) ?? model.unknown;
    }
  }
  // первая ветвь: Playwright
  let grep = null;
  try {
    grep = v.PW_GREP ? new RegExp(v.PW_GREP) : null;
  } catch {
    grep = null;
  }
  const specSec = (spec, onlyCore) => {
    if (!model.specKnown(spec)) return model.unknown;
    let s = 0;
    for (const t of model.pwBySpec.get(spec)) {
      if (grep && !grep.test(t.title)) continue;
      if (onlyCore && !t.coreMarked) continue;
      s += t.sec;
    }
    return model.isRead(spec) ? s / READ_WORKERS : s;
  };
  for (const spec of words(v.SPECS)) sec += specSec(spec, false);
  // вторая ветвь: тесты с меткой ядра
  const coreRun = (v.GO_CORE_RUN || '').replace(/^\^\(|\)\$$/g, '');
  for (const name of coreRun ? coreRun.split('|') : []) sec += model.secs(`go:${name}`) ?? model.unknown;
  for (const spec of words(v.SPECS_CORE)) sec += specSec(spec, true);
  return sec / 60 + SHELL_MIN;
}

const label = (m) => (m.pr ? `#${m.pr}` : m.head.slice(0, 12));

function replay(snap, args) {
  const model = buildModel(snap);
  let sel;
  try {
    sel = checkSelectorRoot(args.selectorRoot || ROOT, snap.merges);
  } catch (e) {
    fail(`--selector-root ${e.message}`);
  }
  const roots = [sel.root, ROOT];
  const rows = [];
  const errorRow = (base, error) => ({
    ...base,
    kind: 'ошибка',
    areas: '-',
    go: '-',
    specs: '-',
    min: null,
    why: error.message,
    requiredFullReasons: [],
    error
  });
  for (const m of snap.merges) {
    const base = { pr: m.pr, id: label(m), base: m.base, head: m.head };
    const lost = [m.base, m.head].filter((s) => sel.missing.has(s));
    if (lost.length > 0) {
      rows.push(
        errorRow(base, {
          code: null,
          signal: null,
          message: `нет локального коммита ${lost.map((s) => s.slice(0, 12)).join(', ')} в рабочей копии выбора`,
          diagnostic: [],
          stops: []
        })
      );
      continue;
    }
    const r = spawnSync(process.execPath, [path.join(sel.root, SELECTOR), '--base', m.base, '--head', m.head, '--explain'], {
      cwd: sel.root,
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024
    });
    const out = parseSelect(r.stdout || '', r.stderr || '');
    const res = selectRow(r, out, roots);
    if (res.error) {
      rows.push(errorRow(base, res.error));
      continue;
    }
    const v = out.vars;
    const words = (s) => (s ? s.split(/\s+/).filter(Boolean).length : 0);
    let goCount;
    if (v.SELECT_ALL === '1') goCount = model.goAll.length;
    else if (v.GO_SEQ) goCount = words(v.GO_SEQ);
    else goCount = (v.GO_PKGS ? v.GO_PKGS.split(/\s+/).filter(Boolean) : []).reduce((n, p) => n + (model.goByPkg.get(p.replace(/^\.\//, '')) || []).length, 0);
    rows.push({
      ...base,
      kind: out.kind || (v.SELECT_ALL === '1' ? 'полный' : 'смоук'),
      areas: v.AREAS || '-',
      go: goCount,
      specs: v.SELECT_ALL === '1' ? 'все' : words(v.SPECS),
      min: estimate(model, v),
      why: out.why,
      requiredFullReasons: res.requiredFullReasons,
      error: null
    });
  }

  const ok = rows.filter((r) => !r.error);
  const errors = rows.filter((r) => r.error);
  const excluded = ok.filter((r) => r.requiredFullReasons.length > 0);
  const group = (list) => {
    const mins = list.map((r) => r.min).filter((n) => typeof n === 'number');
    return mins.length > 0 ? { p50: percentile(mins, 50), p90: percentile(mins, 90), n: mins.length } : null;
  };
  const all = group(ok);
  const target = group(ok.filter((r) => r.requiredFullReasons.length === 0));
  const complete = errors.length === 0;

  if (args.json) {
    const pick = (g) => (g ? { p50: g.p50, p90: g.p90 } : null);
    const report = {
      snapshot: snap.hash,
      originMain: snap.originMain,
      since: snap.since,
      until: snap.until,
      rows: rows.map((r) => ({
        pr: r.pr,
        id: r.id,
        base: r.base,
        head: r.head,
        kind: r.kind,
        areas: r.areas === '-' ? [] : r.areas.split(/\s+/).filter(Boolean),
        go: typeof r.go === 'number' ? r.go : null,
        specs: typeof r.specs === 'number' || r.specs === 'все' ? r.specs : null,
        requiredFullReasons: r.requiredFullReasons,
        minutes: r.min,
        why: r.why || null,
        error: r.error
      })),
      all: pick(all),
      target: pick(target),
      excluded: excluded.map((r) => ({ pr: r.pr, id: r.id, reasons: r.requiredFullReasons })),
      errors: errors.length,
      complete
    };
    console.log(JSON.stringify(report, null, 2));
    if (!complete) process.exitCode = 1;
    return;
  }

  const period = `${snap.since} … ${snap.until || `origin/main ${snap.originMain.slice(0, 12)}`}`;
  console.log(`Прогон выбора гейта по слитым PR: ${period}, слияний ${rows.length}.`);
  console.log(
    `Снимок ${snap.hash.slice(0, 12)}: origin/main ${snap.originMain.slice(0, 12)}, история на ${snap.history.at} (окно ${snap.history.days} дн., строк ${snap.history.rows}), тест без истории — ${snap.unknownSec} с.`
  );
  console.log();
  console.log('| PR | Вид выбора | Области | Go-тестов | Спеков | Оценка, мин | Целевая |');
  console.log('|---|---|---|---|---|---|---|');
  for (const r of rows) {
    const inTarget = r.error ? '-' : r.requiredFullReasons.length > 0 ? 'нет' : 'да';
    console.log(`| ${r.id} | ${r.kind} | ${r.areas} | ${r.go} | ${r.specs} | ${fmt(r.min)} | ${inTarget} |`);
  }
  console.log();
  const counts = KINDS.map((k) => `${k} ${rows.filter((r) => r.kind === k).length}`);
  console.log(`Вид выбора: ${counts.join(', ')}, ошибка ${errors.length}.`);
  const line = (g) => (g ? `p50 ${fmt(g.p50)} мин, p90 ${fmt(g.p90)} мин (PR ${g.n})` : 'нет оценки');
  console.log(`Все PR: ${line(all)}.`);
  console.log(
    `Целевая выборка (без обязательных полных выборов): ${line(target)}; цели p50 ≤ ${GOALS.p50}, p90 ≤ ${GOALS.p90}.`
  );
  for (const k of ['зависимости', 'обвязка']) {
    const m = ok.filter((r) => r.kind === k).map((r) => r.min);
    console.log(`${k}: PR ${m.length}, максимум ${fmt(m.length ? Math.max(...m) : null)} мин (цель ≤ ${GOALS.support})`);
  }
  console.log(`медиана оценок (для сверки): ${fmt(median(ok.map((r) => r.min)))} мин`);
  if (excluded.length > 0) {
    console.log();
    console.log('Исключены из целевой выборки (обязательный полный выбор):');
    for (const r of excluded) console.log(`- ${r.id}: ${r.requiredFullReasons.join('; ')}`);
  }
  const diagFull = ok.filter((r) => r.kind === 'полный' && r.requiredFullReasons.length === 0);
  if (diagFull.length > 0) {
    console.log();
    console.log('Полный выбор без обязательного основания (в целевой выборке):');
    for (const r of diagFull) console.log(`- ${r.id}: ${r.why || 'причина не получена'}`);
  }
  if (errors.length > 0) {
    console.log();
    console.log('Ошибки выбора:');
    for (const r of errors) {
      const e = r.error;
      const how = e.signal ? `сигнал ${e.signal}` : e.code !== null ? `код ${e.code}` : 'без запуска';
      console.log(`- ${r.id} (${r.head.slice(0, 12)}, ${how}): ${e.message}`);
      for (const d of e.diagnostic) console.log(`  - ${d}`);
    }
  }
  console.log();
  if (!complete) {
    console.log(`Оценка незавершена: ошибок выбора ${errors.length}. Вердикт о целях не выносится до разбора ошибок.`);
    process.exitCode = 1;
  } else if (!target) {
    console.log('Целевая выборка пуста: оценки нет, вердикт о целях не выносится.');
  } else {
    const met = target.p50 <= GOALS.p50 && target.p90 <= GOALS.p90;
    console.log(
      met
        ? `Цели достигнуты: p50 ${fmt(target.p50)} ≤ ${GOALS.p50}, p90 ${fmt(target.p90)} ≤ ${GOALS.p90}.`
        : `Цели не достигнуты: p50 ${fmt(target.p50)} (цель ≤ ${GOALS.p50}), p90 ${fmt(target.p90)} (цель ≤ ${GOALS.p90}).`
    );
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.snapshot) {
    replay(loadSnapshot(args.snapshot), args);
    return;
  }
  let snap;
  try {
    snap = await buildSnapshot(args);
  } catch (e) {
    fail(e.message);
  }
  if (args.capture) {
    writeSnapshot(args.capture, snap);
    console.log(
      `Снимок записан: ${args.capture} (слияний ${snap.merges.length}, период ${snap.since} … ${snap.until || 'origin/main'}, origin/main ${snap.originMain.slice(0, 12)}, хеш ${snap.hash.slice(0, 12)}).`
    );
    return;
  }
  replay(snap, args);
}

await main();
