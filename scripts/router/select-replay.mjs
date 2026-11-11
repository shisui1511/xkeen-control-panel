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
// Выводится только номер PR из темы слияния (`#<число>`) или SHA слияния; имена веток не
// печатаются.
//
// Использование (из корня репозитория):
//   node scripts/router/select-replay.mjs [--since <дата>] [--until <дата>] [--selector-root <каталог>]
//   node scripts/router/select-replay.mjs [--since <дата>] [--until <дата>] --capture <файл>
//   node scripts/router/select-replay.mjs --snapshot <файл> [--selector-root <каталог>]
// Таблица Markdown: первая ячейка строки PR — `#<номер>` (или SHA слияния); затем итоговые строки.

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

const USAGE = [
  'Использование:',
  '  node scripts/router/select-replay.mjs [--since <дата>] [--until <дата>] [--selector-root <каталог>]',
  '  node scripts/router/select-replay.mjs [--since <дата>] [--until <дата>] --capture <файл>',
  '  node scripts/router/select-replay.mjs --snapshot <файл> [--selector-root <каталог>]'
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
  const out = { since: null, until: null, capture: null, snapshot: null, selectorRoot: null };
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
    else usage(`неизвестный аргумент ${a}`);
  }
  if (out.capture && out.snapshot) usage('--capture и --snapshot взаимоисключающие');
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
  const kind = (/^ {2}- вид выбора: (.+)$/m.exec(stderr) || [])[1] || null;
  const why = (/^ {2}- всё: (.+)$/m.exec(stderr) || [])[1] || '';
  return { vars, kind, why };
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
  const rows = [];
  for (const m of snap.merges) {
    const base = { pr: m.pr, id: label(m), base: m.base, head: m.head };
    if (sel.missing.has(m.base) || sel.missing.has(m.head)) {
      rows.push({ ...base, kind: 'ошибка', areas: '-', go: '-', specs: '-', min: null, why: 'нет локального коммита пары' });
      continue;
    }
    const r = spawnSync(process.execPath, [path.join(sel.root, SELECTOR), '--base', m.base, '--head', m.head, '--explain'], {
      cwd: sel.root,
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024
    });
    const out = parseSelect(r.stdout || '', r.stderr || '');
    if (r.status !== 0 || !out.vars.SELECT_ALL) {
      rows.push({ ...base, kind: 'ошибка', areas: '-', go: '-', specs: '-', min: null, why: 'выбор не получен' });
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
      why: out.why
    });
  }

  const period = `${snap.since} … ${snap.until || `origin/main ${snap.originMain.slice(0, 12)}`}`;
  console.log(`Прогон выбора гейта по слитым PR: ${period}, слияний ${rows.length}.`);
  console.log(
    `Снимок ${snap.hash.slice(0, 12)}: origin/main ${snap.originMain.slice(0, 12)}, история на ${snap.history.at} (окно ${snap.history.days} дн., строк ${snap.history.rows}), тест без истории — ${snap.unknownSec} с.`
  );
  console.log();
  console.log('| PR | Вид выбора | Области | Go-тестов | Спеков | Оценка, мин |');
  console.log('|---|---|---|---|---|---|');
  for (const r of rows) {
    console.log(`| ${r.id} | ${r.kind} | ${r.areas} | ${r.go} | ${r.specs} | ${fmt(r.min)} |`);
  }
  console.log();
  const counts = KINDS.map((k) => `${k} ${rows.filter((r) => r.kind === k).length}`);
  console.log(`Вид выбора: ${counts.join(', ')}.`);
  const mins = rows.filter((r) => r.min !== null).map((r) => r.min);
  const p50 = percentile(mins, 50);
  const p90 = percentile(mins, 90);
  console.log(`p50: ${fmt(p50)} мин (цель ≤ ${GOALS.p50})`);
  console.log(`p90: ${fmt(p90)} мин (цель ≤ ${GOALS.p90})`);
  const nonFull = rows.filter((r) => r.kind !== 'полный' && r.min !== null).map((r) => r.min);
  console.log(`p50 без полного выбора: ${fmt(percentile(nonFull, 50))} мин, p90: ${fmt(percentile(nonFull, 90))} мин`);
  for (const k of ['зависимости', 'обвязка']) {
    const m = rows.filter((r) => r.kind === k && r.min !== null).map((r) => r.min);
    console.log(`${k}: PR ${m.length}, максимум ${fmt(m.length ? Math.max(...m) : null)} мин (цель ≤ ${GOALS.support})`);
  }
  const med = median(mins);
  console.log(`медиана оценок (для сверки): ${fmt(med)} мин`);
  const fulls = rows.filter((r) => r.kind === 'полный');
  if (fulls.length > 0) {
    console.log();
    console.log('Полный выбор:');
    for (const r of fulls) console.log(`- ${r.id}: ${r.why || 'причина не получена'}`);
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
