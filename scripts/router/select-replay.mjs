#!/usr/bin/env node
// Прогон выбора гейта по слитым PR с оценкой времени (148.1-09, D-08).
//
// Для каждого слияния в first-parent истории origin/main за период запускает
// `select-changed.mjs --base <первый родитель> --head <слияние> --explain` и печатает таблицу:
// вид выбора (полный | обвязка | зависимости | области | смоук), области, число Go-тестов и
// спеков первой ветви, оценку времени гейта в минутах. Нужна, чтобы цели гейта фиксировать по
// факту (p50, p90), а не по ощущению, и чтобы видеть, какие PR ещё уходят в полный выбор.
//
// Оценка минут (по истории build/router/history.tsv, окно 60 дней, history.mjs durations):
//   сумма медиан секунд тестов первой ветви (RT_GO_SEQ или тесты пакетов RT_GO_PKGS, спеки
//   RT_SPECS с учётом RT_PW_GREP; спеки только чтения делятся на число воркеров 3) плюс тесты
//   второй ветви (RT_GO_CORE_RUN и тесты спеков RT_SPECS_CORE с тегом ядра) плюс оболочка
//   4,5 мин (деплой, смоук, снимок). Медиана ядер усредняется: ядро ветви зависит от SHA.
//   Тест без истории — 30 с. Полный выбор — сумма медиан всех тестов по двум ядрам плюс оболочка.
// Оценка грубая: карта областей, факты тестов и граф импортов берутся из рабочей копии, а не из
// коммита слияния, поэтому для давних PR набор тестов мог быть другим.
//
// Выводится только номер PR из темы слияния (`#<число>`); имена веток не печатаются.
//
// Использование (из корня репозитория):
//   node scripts/router/select-replay.mjs --since <дата> [--until <дата>]
// Таблица Markdown: первая ячейка строки PR — `#<номер>`; затем итоговые строки.

import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import coreMarks from './core-marks.cjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const SHELL_MIN = 4.5;
const UNKNOWN_SEC = 30;
const READ_WORKERS = 3;
const KINDS = ['полный', 'обвязка', 'зависимости', 'области', 'смоук'];
const GOALS = { p50: 20, p90: 45, support: 15 };

function usage(msg) {
  if (msg) console.error(`select-replay: ${msg}`);
  console.error('Использование: node scripts/router/select-replay.mjs --since <дата> [--until <дата>]');
  process.exit(2);
}

function parseArgs(argv) {
  const out = { since: null, until: null };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--since') out.since = argv[++i];
    else if (a === '--until') out.until = argv[++i];
    else usage(`неизвестный аргумент ${a}`);
  }
  if (!out.since) usage('нужен --since');
  for (const v of [out.since, out.until]) {
    if (v && !/^[0-9]{4}-[0-9]{2}-[0-9]{2}([ T][0-9:]+)?$/.test(v)) usage(`дата ${v}: нужна форма ГГГГ-ММ-ДД`);
  }
  return out;
}

function gitLog({ since, until }) {
  const args = ['log', '--first-parent', '--merges', 'origin/main', `--since=${since}`];
  if (until) args.push(`--until=${until}`);
  args.push('--format=%H %P\t%s');
  const r = spawnSync('git', args, { cwd: ROOT, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
  if (r.status !== 0) throw new Error(`git log: ${(r.stderr || '').split('\n', 1)[0]}`);
  const rows = [];
  for (const line of r.stdout.split('\n').filter(Boolean)) {
    const [head, subject] = line.split('\t');
    const [merge, first] = head.split(' ');
    const m = /#(\d+)/.exec(subject || '');
    rows.push({ merge, first, pr: m ? m[1] : '?' });
  }
  // по возрастанию времени: старые слияния первыми
  return rows.reverse();
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

/** Модель оценки: секунды Go-тестов и спеков по истории. */
async function buildModel() {
  const hist = await import(pathToFileURL(path.join(ROOT, 'scripts/router/history.mjs')));
  const dur = hist.durations(hist.readHistory(), { days: 60 });
  const cores = ['xray', 'mihomo'];
  const secs = (check) => {
    const vals = cores.map((c) => dur.get(`${check}\t${c}`)?.median).filter((v) => typeof v === 'number');
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
  const seen = new Set();
  for (const key of dur.keys()) {
    const [check] = key.split('\t');
    if (!check.startsWith('pw:') || seen.has(check)) continue;
    seen.add(check);
    const title = check.slice(3);
    const row = literal.get(title) || tplRe.find((t) => t.re.test(title))?.row;
    const sec = secs(check);
    if (sec === null) continue;
    pwAll.push(sec);
    if (!row) continue;
    if (!pwBySpec.has(row.spec)) pwBySpec.set(row.spec, []);
    pwBySpec.get(row.spec).push({ title, sec, coreMarked: row.coreMarked });
  }
  const goAll = [...new Set(facts.filter((r) => r.kind === 'go').map((r) => r.test))].map((t) => secs(`go:${t}`) ?? UNKNOWN_SEC);
  const specKnown = (spec) => pwBySpec.has(spec);
  const isRead = (spec) => {
    const file = path.join(ROOT, 'frontend', spec);
    if (!existsSync(file)) return false;
    return readFileSync(file, 'utf8').split('\n', 5).some((l) => /^\/\/\s*e2e-kind:\s*read\b/.test(l));
  };
  return { secs, goByPkg, pwBySpec, goAll, pwAll, specKnown, isRead };
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
    for (const e of seq) sec += model.secs(`go:${e.slice(e.indexOf(':') + 1)}`) ?? UNKNOWN_SEC;
  } else {
    for (const pkg of words(v.GO_PKGS)) {
      for (const t of model.goByPkg.get(pkg.replace(/^\.\//, '')) || []) sec += model.secs(`go:${t}`) ?? UNKNOWN_SEC;
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
    if (!model.specKnown(spec)) return UNKNOWN_SEC;
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
  for (const name of coreRun ? coreRun.split('|') : []) sec += model.secs(`go:${name}`) ?? UNKNOWN_SEC;
  for (const spec of words(v.SPECS_CORE)) sec += specSec(spec, true);
  return sec / 60 + SHELL_MIN;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const merges = gitLog(args);
  if (merges.length === 0) {
    console.error('select-replay: слияний за период не найдено');
    process.exit(1);
  }
  const model = await buildModel();
  const rows = [];
  for (const m of merges) {
    const r = spawnSync(
      process.execPath,
      [path.join(ROOT, 'scripts/router/select-changed.mjs'), '--base', m.first, '--head', m.merge, '--explain'],
      { cwd: ROOT, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }
    );
    const sel = parseSelect(r.stdout || '', r.stderr || '');
    if (r.status !== 0 || !sel.vars.SELECT_ALL) {
      rows.push({ pr: m.pr, kind: 'ошибка', areas: '-', go: '-', specs: '-', min: null, why: 'выбор не получен' });
      continue;
    }
    const v = sel.vars;
    const words = (s) => (s ? s.split(/\s+/).filter(Boolean).length : 0);
    let goCount;
    if (v.SELECT_ALL === '1') goCount = model.goAll.length;
    else if (v.GO_SEQ) goCount = words(v.GO_SEQ);
    else goCount = (v.GO_PKGS ? v.GO_PKGS.split(/\s+/).filter(Boolean) : []).reduce((n, p) => n + (model.goByPkg.get(p.replace(/^\.\//, '')) || []).length, 0);
    rows.push({
      pr: m.pr,
      kind: sel.kind || (v.SELECT_ALL === '1' ? 'полный' : 'смоук'),
      areas: v.AREAS || '-',
      go: goCount,
      specs: v.SELECT_ALL === '1' ? 'все' : words(v.SPECS),
      min: estimate(model, v),
      why: sel.why
    });
  }

  console.log(`Прогон выбора гейта по слитым PR: ${args.since} … ${args.until || 'сейчас'}, слияний ${rows.length}.`);
  console.log();
  console.log('| PR | Вид выбора | Области | Go-тестов | Спеков | Оценка, мин |');
  console.log('|---|---|---|---|---|---|');
  for (const r of rows) {
    console.log(`| #${r.pr} | ${r.kind} | ${r.areas} | ${r.go} | ${r.specs} | ${fmt(r.min)} |`);
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
    for (const r of fulls) console.log(`- #${r.pr}: ${r.why || 'причина не получена'}`);
  }
}

await main();
