#!/usr/bin/env node
// scripts/router/progress.mjs — ход прогона на стенде для человека: что идёт сейчас, сколько
// пройдено, что упало, сколько осталось. Только читает отчёт, стенд не трогает.
//
// Источник — журнал <отчёт>/progress.tsv (rt_progress в lib.sh и репортёр Playwright
// frontend/tests/router/lib/progress-reporter.ts), строки «секунды<TAB>вид<TAB>поля»:
//   run <набор> <pid>                     начало прогона (run.sh); pid — признак, что он ещё идёт
//   est <секунды>                         оценка длительности (полный прогон, сессия)
//   stage+ <цель> <ядро> <этап>           этап начался (rt_stage)
//   stage- <цель> <ядро> <этап> <код> <с> этап закончился
//   go <цель> <ядро> <запуск> <n> <поток> Go-запуск: n тестов верхнего уровня, сырой поток test2json
//   pw <цель> <ядро> <n>                  запуск Playwright на n тестов
//   pwt+ <цель> <ядро> <тест>             тест начался
//   pwt <цель> <ядро> <статус> <тест>     тест закончился (PASS, FAIL, KNOWN, SKIP)
// Журналы вложенных отчётов (этапы сессии <NN>-<этап>/) читаются вместе с корневым. Записанные
// итоги — строки results.tsv всех целей отчёта и вложенных отчётов; идущие Go- и Playwright-
// запуски считаются отдельно («сейчас»), пока их строки не попали в results.tsv. Вердикт
// законченного прогона — строки «- Итог:» summary.md.
//
// Вне отчёта: очередь на стенд — файлы <git common dir>/router-test.queue.<pid> (rt_lock в lib.sh:
// набор и начало ожидания), живые по PID; ночной прогон, ждущий старта, — build/router/unattended/
// state (фаза «ожидание», время at), живой по PID и командной строке «unattended.sh run».
//
// Использование (из любого каталога рабочей копии; отчёт по умолчанию — build/router/last
// основной копии, то есть последний начатый прогон или сессия):
//   node scripts/router/progress.mjs [<отчёт>]            снимок хода
//   node scripts/router/progress.mjs --watch [<отчёт>]    обновление раз в 2 с до конца прогона
//   node scripts/router/progress.mjs --json [<отчёт>]     состояние JSON (панель хода в приложении)
//   node scripts/router/progress.mjs --line [<отчёт>]     одна строка (строка состояния)
// <отчёт> — путь или имя каталога под build/router основной копии.
// Код выхода: 0 — снимок напечатан (в том числе «прогонов нет»); 2 — неверные аргументы.

import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

const WATCH_MS = 2000;
const FAILS_SHOWN = 12;
// без событий и роста сырого потока дольше — пометка «нет событий»
const IDLE_WARN_S = 300;

function commonDir() {
  return execFileSync('git', ['rev-parse', '--path-format=absolute', '--git-common-dir'], {
    encoding: 'utf8'
  }).trim();
}

function mainRoot() {
  return path.dirname(commonDir());
}

/** Прогоны, ждущие блокировку стенда: [{ suite, sinceSec }]. */
function queueOf(now) {
  const dir = commonDir();
  const out = [];
  let names = [];
  try {
    names = fs.readdirSync(dir);
  } catch {
    return out;
  }
  for (const n of names) {
    const m = /^router-test\.queue\.(\d+)$/.exec(n);
    if (!m || !alive(Number(m[1]))) continue;
    const [suite = 'full', t = '0'] = readText(path.join(dir, n)).trim().split('\t');
    out.push({ suite, sinceSec: now - Number(t) });
  }
  return out.sort((a, b) => b.sinceSec - a.sinceSec);
}

/** Ночной прогон, ждущий старта: { at } или null. */
function unattendedWaiting(root) {
  const st = readText(path.join(root, 'build', 'router', 'unattended', 'state'));
  const kv = Object.fromEntries(
    st
      .split('\n')
      .filter((l) => l.includes('='))
      .map((l) => [l.slice(0, l.indexOf('=')), l.slice(l.indexOf('=') + 1)])
  );
  const pid = Number(kv.pid);
  if (!pid || !alive(pid)) return null;
  if (!readText(`/proc/${pid}/cmdline`).split('\0').join(' ').includes('unattended.sh run')) return null;
  if (kv.phase !== 'ожидание' && kv.phase !== 'запуск') return null;
  return { at: kv.at && kv.at !== '-' ? kv.at : '' };
}

function parseArgs(argv) {
  const out = { mode: 'text', report: '' };
  for (const a of argv) {
    if (a === '--watch') out.mode = 'watch';
    else if (a === '--json') out.mode = 'json';
    else if (a === '--line') out.mode = 'line';
    else if (a.startsWith('-')) {
      process.stderr.write(`progress: неизвестный ключ ${a}\n`);
      process.exit(2);
    } else out.report = a;
  }
  return out;
}

function resolveReport(root, arg) {
  const base = path.join(root, 'build', 'router');
  if (!arg) {
    const last = path.join(base, 'last');
    try {
      return fs.realpathSync(last);
    } catch {
      return '';
    }
  }
  const cand = path.isAbsolute(arg) ? arg : fs.existsSync(arg) ? path.resolve(arg) : path.join(base, arg);
  try {
    return fs.realpathSync(cand);
  } catch {
    return '';
  }
}

function readText(file) {
  try {
    return fs.readFileSync(file, 'utf8');
  } catch {
    return '';
  }
}

function mtimeS(file) {
  try {
    return Math.floor(fs.statSync(file).mtimeMs / 1000);
  } catch {
    return 0;
  }
}

/** Файлы с именем name в отчёте и вложенных отчётах (до глубины 3). */
function findFiles(dir, name, depth = 3) {
  const out = [];
  let entries = [];
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const e of entries) {
    const p = path.join(dir, e.name);
    if (e.isFile() && e.name === name) out.push(p);
    else if (e.isDirectory() && depth > 0 && !e.name.startsWith('.') && e.name !== 'artifacts') {
      out.push(...findFiles(p, name, depth - 1));
    }
  }
  return out;
}

/**
 * Файлы итогов прогона: results.tsv целей и вложенных отчётов; у перепрогона (rerun дописывает
 * отчёт FROM) — только rerun-<N>.tsv последнего перепрогона, итоги исходного прогона не в счёт.
 */
function resultFiles(report, suite) {
  if (suite !== 'rerun') return findFiles(report, 'results.tsv');
  let n = 0;
  for (const f of fs.readdirSync(report)) {
    const m = /^rerun-(\d+)\.meta$/.exec(f);
    if (m) n = Math.max(n, Number(m[1]));
  }
  if (!n) return [];
  const out = [];
  for (const e of fs.readdirSync(report, { withFileTypes: true })) {
    const f = path.join(report, e.name, `rerun-${n}.tsv`);
    if (e.isDirectory() && fs.existsSync(f)) out.push(f);
  }
  return out;
}

function alive(pid) {
  if (!pid) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (e) {
    return e.code === 'EPERM';
  }
}

/** События журнала: { t, kind, f: [...], file }. */
function readEvents(file) {
  const ev = [];
  for (const line of readText(file).split('\n')) {
    if (!line) continue;
    const [t, kind, ...f] = line.split('\t');
    const n = Number(t);
    if (!Number.isFinite(n) || !kind) continue;
    ev.push({ t: n, kind, f, file });
  }
  return ev;
}

/** Разбор сырого потока test2json: тесты верхнего уровня — закончившиеся и идущий. */
function goRaw(file) {
  const txt = readText(file);
  const res = [];
  let current = '';
  for (const line of txt.split('\n')) {
    const l = line.replace(/^\x16/, '');
    let m = /^=== RUN\s+(TestRouter[^\s/]+)\s*$/.exec(l);
    if (m) {
      current = m[1];
      continue;
    }
    m = /^--- (PASS|FAIL|SKIP): (TestRouter[^\s/]+) /.exec(l);
    if (m) {
      res.push({ status: m[1], name: m[2] });
      if (m[2] === current) current = '';
    }
  }
  return { res, current, mtime: mtimeS(file) };
}

/** Оставшееся время запуска по среднему темпу; меньше трёх законченных тестов — null. */
function blockEta(elapsed, done, total) {
  if (done < 3 || total <= done) return null;
  return Math.round((elapsed / done) * (total - done));
}

function fmtDur(sec) {
  if (sec == null || !Number.isFinite(sec)) return '—';
  sec = Math.max(0, Math.round(sec));
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (h) return `${h} ч ${String(m).padStart(2, '0')} мин`;
  if (m) return `${m} мин ${String(s).padStart(2, '0')} с`;
  return `${s} с`;
}

function collect(report) {
  const now = Math.floor(Date.now() / 1000);
  const state = {
    report,
    name: path.basename(report),
    suite: '',
    state: 'none',
    verdict: '',
    started: 0,
    elapsedSec: 0,
    estSec: null,
    etaSec: null,
    percent: null,
    stages: [],
    block: null,
    counts: { PASS: 0, FAIL: 0, KNOWN: 0, SKIP: 0 },
    fails: [],
    session: '',
    idleSec: 0,
    holdUntil: 0,
    holdLeftSec: 0,
    overSec: 0,
    queue: [],
    unattended: null,
    line: ''
  };
  state.queue = queueOf(now);
  state.unattended = unattendedWaiting(mainRoot());
  if (!report || !fs.existsSync(report)) {
    state.line = oneLine(state);
    return state;
  }
  const rootLog = path.join(report, 'progress.tsv');
  const logs = findFiles(report, 'progress.tsv');
  let events = logs.flatMap(readEvents);
  // старт прогона — последнее событие run корневого журнала (rerun дописывает тот же отчёт)
  const runs = events.filter((e) => e.kind === 'run' && e.file === rootLog);
  const run = runs[runs.length - 1];
  const meta = readText(path.join(report, 'meta'));
  const metaStart = /started=(\S+)/.exec(meta);
  state.started = run ? run.t : metaStart ? Math.floor(Date.parse(metaStart[1]) / 1000) : 0;
  if (run) events = events.filter((e) => e.t >= run.t);
  events.sort((a, b) => a.t - b.t);
  state.suite =
    (run && run.f[0]) ||
    /^- Набор: (\S+)/m.exec(readText(path.join(report, 'report.md')))?.[1] ||
    (fs.existsSync(path.join(report, 'session.log')) ? 'session' : '');
  const pid = run ? Number(run.f[1]) : 0;
  const summary = readText(path.join(report, 'summary.md'));
  const verdicts = [...summary.matchAll(/^- Итог: (.+)$/gm)].map((m) => m[1].trim());
  const running = alive(pid);
  state.state = running ? 'running' : verdicts.length ? 'done' : run ? 'stopped' : 'done';
  state.verdict = verdicts.join('; ');
  // удержание (RT_HOLD_MINUTES): прогон закончен, стенд держится для ручной сессии; этапы
  // go-suite.sh и pw-suite.sh, запущенные вручную, идут в тот же отчёт
  const hold = events.filter((e) => e.kind === 'hold' && e.file === rootLog).pop();
  const holding = running && hold !== undefined;
  if (holding) {
    state.state = 'hold';
    state.holdUntil = Number(hold.f[0]) || 0;
    state.holdLeftSec = Math.max(0, state.holdUntil - now);
  }
  const lastT = events.length ? events[events.length - 1].t : state.started;
  const endT = holding
    ? hold.t
    : running
      ? now
      : Math.max(lastT, mtimeS(path.join(report, 'summary.md')) || 0);
  state.elapsedSec = state.started ? endT - state.started : 0;

  // оценка — только корневого журнала: вложенный прогон сессии пишет свою в свой журнал
  const est = events.filter((e) => e.kind === 'est' && e.file === rootLog).pop();
  if (est) {
    state.estSec = Number(est.f[0]) || null;
    if (state.estSec) {
      const going = running && !holding;
      state.percent = Math.min(going ? 99 : 100, Math.floor((state.elapsedSec / state.estSec) * 100));
      state.etaSec = going ? Math.max(0, state.estSec - state.elapsedSec) : 0;
      state.overSec = going ? Math.max(0, state.elapsedSec - state.estSec) : 0;
    }
  }

  // открытые этапы: stage+ без парного stage-
  const open = new Map();
  for (const e of events) {
    const key = `${e.file}\t${e.f[0]}\t${e.f[1]}\t${e.f[2]}`;
    if (e.kind === 'stage+') open.set(key, e);
    else if (e.kind === 'stage-') open.delete(key);
  }
  if (running) {
    state.stages = [...open.values()]
      .sort((a, b) => a.t - b.t)
      .map((e) => ({ target: e.f[0], core: e.f[1], name: e.f[2], sinceSec: now - e.t }));
  }

  // записанные итоги: события res журнала (последний исход каждой проверки; при удержании —
  // только ручной сессии); отчёты без них — по файлам результатов
  const recorded = new Set();
  const resEvents = events.filter((e) => e.kind === 'res' && (!holding || e.t >= hold.t));
  const addRecorded = (st, core, check, detail) => {
    if (st in state.counts) state.counts[st]++;
    else if (st === 'XPASS') state.counts.FAIL++;
    else if (st === 'QPASS') state.counts.PASS++;
    recorded.add(`${core}\t${check}`);
    if (st === 'FAIL' || st === 'XPASS') state.fails.push({ core, name: check, detail, live: false });
  };
  const hasRes = events.some((e) => e.kind === 'res');
  if (hasRes) {
    const latest = new Map();
    for (const e of resEvents) latest.set(`${e.f[0]}\t${e.f[1]}\t${e.f[3]}`, e);
    for (const e of latest.values()) addRecorded(e.f[2], e.f[1], e.f[3], '');
  }
  for (const file of hasRes ? [] : resultFiles(report, state.suite)) {
    for (const line of readText(file).split('\n')) {
      if (!line) continue;
      const [st, core, check, detail = ''] = line.split('\t');
      addRecorded(st, core, check, detail.slice(0, 200));
    }
  }

  // запуск Playwright идёт, пока закончились не все его тесты и после него не было stage- этапа pw
  const pwOpen = (pw) => {
    const ends = events.filter((e) => e.file === pw.file && e.t >= pw.t && e.kind === 'pwt').length;
    const closed = events.some(
      (e) => e.file === pw.file && e.t > pw.t && e.kind === 'stage-' && /^pw/.test(e.f[2])
    );
    return ends < (Number(pw.f[2]) || 0) && !closed;
  };

  // идущий запуск: последний go или pw, если его этап открыт
  if (running) {
    const lastGo = events.filter((e) => e.kind === 'go').pop();
    const lastPw = events.filter((e) => e.kind === 'pw').pop();
    const goOpen =
      lastGo && [...open.values()].some((s) => s.file === lastGo.file && s.f[2] === `go-run-${lastGo.f[2]}`);
    let block = null;
    if (goOpen && (!lastPw || lastGo.t >= lastPw.t || !pwOpen(lastPw))) {
      const raw = goRaw(lastGo.f[4]);
      const counts = { PASS: 0, FAIL: 0, SKIP: 0 };
      for (const r of raw.res) {
        counts[r.status]++;
        if (r.status === 'FAIL' && !recorded.has(`${lastGo.f[1]}\tgo:${r.name}`)) {
          state.fails.push({ core: lastGo.f[1], name: `go:${r.name}`, detail: '', live: true });
        }
      }
      block = {
        kind: 'go',
        target: lastGo.f[0],
        core: lastGo.f[1],
        name: `go ${lastGo.f[2]}`,
        done: raw.res.length,
        total: Math.max(Number(lastGo.f[3]) || 0, raw.res.length),
        counts,
        current: raw.current,
        etaSec: blockEta(now - lastGo.t, raw.res.length, Math.max(Number(lastGo.f[3]) || 0, raw.res.length))
      };
      state.idleSec = now - Math.max(raw.mtime, lastT);
    } else if (lastPw && pwOpen(lastPw)) {
      const after = events.filter((e) => e.file === lastPw.file && e.t >= lastPw.t && (e.kind === 'pwt' || e.kind === 'pwt+'));
      const ended = new Set();
      const counts = { PASS: 0, FAIL: 0, KNOWN: 0, SKIP: 0 };
      for (const e of after) {
        if (e.kind !== 'pwt') continue;
        ended.add(e.f[3]);
        if (e.f[2] in counts) counts[e.f[2]]++;
        if (e.f[2] === 'FAIL' && !recorded.has(`${e.f[1]}\tpw:${e.f[3]}`)) {
          state.fails.push({ core: e.f[1], name: `pw:${e.f[3]}`, detail: '', live: true });
        }
      }
      const started = after.filter((e) => e.kind === 'pwt+' && !ended.has(e.f[2]));
      block = {
        kind: 'pw',
        target: lastPw.f[0],
        core: lastPw.f[1],
        name: 'Playwright',
        done: ended.size,
        total: Number(lastPw.f[2]) || 0,
        counts,
        current: started.length ? started[started.length - 1].f[2] : '',
        currentSec: started.length ? now - started[started.length - 1].t : null,
        etaSec: blockEta(now - lastPw.t, ended.size, Number(lastPw.f[2]) || 0)
      };
      state.idleSec = now - lastT;
    } else {
      state.idleSec = now - lastT;
    }
    state.block = block;

  }

  // сессия: последняя строка журнала сессии
  const slog = readText(path.join(report, 'session.log')).split('\n');
  const sline = slog.filter((l) => l.startsWith('session: ')).pop() || '';
  state.session = sline.slice('session: '.length, 'session: '.length + 200);

  state.line = oneLine(state);
  return state;
}

function oneLine(s) {
  const base = oneLineRun(s);
  const extra = [];
  if (s.queue.length && s.state !== 'running') {
    extra.push(`в очереди: ${s.queue.map((q) => q.suite).join(', ')}`);
  }
  if (s.unattended) extra.push(`ночной прогон ждёт${s.unattended.at ? ` ${s.unattended.at}` : ''}`);
  return extra.length ? `${base} · ${extra.join(' · ')}` : base;
}

function oneLineRun(s) {
  if (s.state === 'none') return 'стенд: прогонов нет';
  const suite = s.suite || 'прогон';
  if (s.state === 'hold') {
    const parts = [`стенд удержан до ${clock(s.holdUntil)}`, `${suite}: ${s.verdict || 'закончен'}`];
    if (s.block) parts.push(`${s.block.core} ${s.block.kind} ${s.block.done}/${s.block.total}`);
    if (s.fails.length) parts.push(`✗${s.fails.length}`);
    return parts.join(' · ');
  }
  if (s.state !== 'running') {
    const v = s.state === 'stopped' ? 'оборван' : s.verdict || 'закончен';
    return `стенд: ${suite} — ${v}`;
  }
  const parts = [`стенд: ${suite}`];
  const st = s.stages[s.stages.length - 1];
  if (s.block) {
    parts.push(`${s.block.core} ${s.block.kind} ${s.block.done}/${s.block.total}`);
  } else if (st) {
    parts.push(`${st.core !== '-' ? `${st.core} ` : ''}${st.name}`);
  }
  if (s.counts.FAIL || s.fails.some((f) => f.live)) parts.push(`✗${s.fails.length}`);
  if (s.overSec > 0) parts.push(`дольше оценки на ${fmtDur(s.overSec)}`);
  else if (s.etaSec != null) parts.push(`осталось ~${fmtDur(s.etaSec)}`);
  else parts.push(fmtDur(s.elapsedSec));
  return parts.join(' · ');
}

/** Местное время ЧЧ:ММ по секундам эпохи. */
function clock(sec) {
  const d = new Date(sec * 1000);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

function bar(frac, width) {
  const n = Math.max(0, Math.min(width, Math.round(frac * width)));
  return `[${'█'.repeat(n)}${'░'.repeat(width - n)}]`;
}

function render(s, color) {
  const c = (code, t) => (color ? `\x1b[${code}m${t}\x1b[0m` : t);
  const width = Math.max(20, Math.min(50, (process.stdout.columns || 80) - 30));
  const out = [];
  const pre = [];
  for (const q of s.queue) {
    pre.push(c('33', `В очереди на стенд: ${q.suite} — ждёт ${fmtDur(q.sinceSec)}`));
  }
  if (s.unattended) {
    pre.push(c('36', `Ночной прогон ждёт старта${s.unattended.at ? ` в ${s.unattended.at}` : ''}`));
  }
  if (s.state === 'none') {
    return [...pre, 'Прогонов на стенде нет (build/router/last не найден).'].join('\n') + '\n';
  }
  out.push(...pre);
  const stateRu = {
    running: c('36', 'идёт'),
    hold: 'закончен',
    done: 'закончен',
    stopped: c('33', 'оборван (процесс прогона не найден)')
  }[s.state];
  out.push(`${c('1', `Прогон ${s.suite || ''}`)} — ${stateRu}   ${c('2', s.name)}`);
  if (s.state !== 'running' && s.verdict) {
    const red = /красный/.test(s.verdict);
    out.push(`Итог: ${c(red ? '31' : '32', s.verdict)}`);
  }
  const times = [`прошло ${fmtDur(s.elapsedSec)}`];
  if (s.estSec) times.push(`оценка ${fmtDur(s.estSec)}`);
  if (s.state === 'running' && s.overSec > 0) times.push(c('33', `дольше оценки на ${fmtDur(s.overSec)}`));
  else if (s.state === 'running' && s.etaSec != null) times.push(`осталось ~${fmtDur(s.etaSec)}`);
  out.push(times.join(' · '));
  if (s.percent != null) out.push(`${bar(s.percent / 100, width)} ${s.percent}%  ${c('2', '(по оценке времени)')}`);
  if (s.session) out.push(`Сессия: ${s.session}`);
  if (s.state === 'hold') {
    out.push(
      c('36', `Стенд удержан для ручной сессии до ${clock(s.holdUntil)} (ещё ${fmtDur(s.holdLeftSec)})`)
    );
  }
  if (s.state === 'running' || s.state === 'hold') {
    for (const st of s.stages) {
      out.push(`Этап: ${st.target}${st.core !== '-' ? ` · ${st.core}` : ''} · ${st.name} ${c('2', `(${fmtDur(st.sinceSec)})`)}`);
    }
    if (s.block) {
      const b = s.block;
      const frac = b.total ? b.done / b.total : 0;
      const cnt = Object.entries(b.counts)
        .filter(([, v]) => v)
        .map(([k, v]) => (k === 'FAIL' ? c('31', `${k} ${v}`) : `${k} ${v}`))
        .join(', ');
      const beta = b.etaSec != null ? c('2', `  ~${fmtDur(b.etaSec)}`) : '';
      out.push(`${b.core} ${b.name}: ${bar(frac, width)} ${b.done}/${b.total}${beta}${cnt ? `  ${cnt}` : ''}`);
      if (b.current) {
        out.push(`  сейчас: ${b.current}${b.currentSec != null ? c('2', ` (${fmtDur(b.currentSec)})`) : ''}`);
      }
    }
    if (s.state === 'running' && s.idleSec > IDLE_WARN_S) out.push(c('33', `Нет новых событий ${fmtDur(s.idleSec)} — тест долгий или прогон завис`));
  }
  const k = s.counts;
  out.push(
    `${s.state === 'hold' ? 'Ручная сессия' : 'Записано'}: PASS ${k.PASS} · ${k.FAIL ? c('31', `FAIL ${k.FAIL}`) : 'FAIL 0'} · KNOWN ${k.KNOWN} · SKIP ${k.SKIP}`
  );
  if (s.fails.length) {
    out.push(c('31', `Упало (${s.fails.length}):`));
    for (const f of s.fails.slice(-FAILS_SHOWN)) {
      out.push(`  ✗ ${f.core !== '-' ? `[${f.core}] ` : ''}${f.name}${f.live ? c('2', ' (сейчас)') : ''}`);
    }
    if (s.fails.length > FAILS_SHOWN) out.push(c('2', `  … и ещё ${s.fails.length - FAILS_SHOWN} — ${s.name}/*/results.tsv`));
  }
  // в терминале строка не длиннее его ширины: перенос сбивал бы перерисовку --watch
  const cols = process.stdout.isTTY ? process.stdout.columns || 0 : 0;
  return out.map((l) => (cols ? fit(l, cols) : l)).join('\n') + '\n';
}

/** Обрезает строку до cols видимых символов, не разрывая escape-последовательности цвета. */
function fit(line, cols) {
  if (line.replace(/\x1b\[[0-9;]*m/g, '').length <= cols) return line;
  let shown = 0;
  let res = '';
  for (let i = 0; i < line.length && shown < cols - 1; i++) {
    const m = /^\x1b\[[0-9;]*m/.exec(line.slice(i));
    if (m) {
      res += m[0];
      i += m[0].length - 1;
      continue;
    }
    res += line[i];
    shown++;
  }
  return `${res}…\x1b[0m`;
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const root = mainRoot();
  const report = resolveReport(root, args.report);
  if (args.mode === 'json') {
    process.stdout.write(JSON.stringify(collect(report)) + '\n');
    return;
  }
  if (args.mode === 'line') {
    process.stdout.write(collect(report).line + '\n');
    return;
  }
  const color = process.stdout.isTTY && !process.env.NO_COLOR;
  if (args.mode === 'text') {
    process.stdout.write(render(collect(report), color));
    return;
  }
  // --watch: перерисовка до конца прогона; без явного отчёта следит за build/router/last
  // (новый прогон, начатый во время наблюдения, подхватывается)
  const tick = () => {
    const rep = args.report ? report : resolveReport(root, '');
    const s = collect(rep);
    process.stdout.write(`${color ? '\x1b[H\x1b[2J' : ''}${render(s, color)}${color ? '\x1b[2m(Ctrl+C — выход)\x1b[0m\n' : ''}`);
    return s;
  };
  tick();
  const timer = setInterval(() => {
    const s = tick();
    if (args.report && s.state !== 'running') clearInterval(timer);
  }, WATCH_MS);
}

main();
