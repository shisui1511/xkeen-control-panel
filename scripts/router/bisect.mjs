#!/usr/bin/env node
// scripts/router/bisect.mjs — поиск виновного коммита двоичным поиском на стенде (RTR-11, D-22, todo 11).
//
// Поиск:
//   node scripts/router/bisect.mjs --target <id> --core <ядро> --from <ref> --to <ref>
//     [--mode regress|fix] (--go <каталог пакета>:<TestRouterИмя> | --spec <tests/router/спек.spec.ts> [--grep <re>])
//     --report <каталог под build/router> [--label <имя теста для итога>] [--assume-to]
//     [--restore-release <тег>] [--restore-core <ядро>] [--max-steps N]
//   Подбор тестов для автоматического поиска в конце полного прогона (стенд не трогается):
//   node scripts/router/bisect.mjs pick --report <каталог> --target <id> [--max N]
//
// Кандидаты. Если FROM лежит на first-parent цепочке TO — `git rev-list --first-parent --reverse TO ^FROM`
// (коммиты основной ветки после FROM, слияния целиком); иначе — `git rev-list --ancestry-path --reverse
// FROM..TO`. FROM должен быть предком TO. Каждый шаг — scripts/router/tree-step.sh: продукт и тест из
// дерева коммита (отдельный worktree), обвязка из HEAD; исход PASS, FAIL или SKIP (теста нет в дереве,
// сборка не удалась).
//
// Режимы. regress (по умолчанию) ищет первый коммит, где тест падает: FROM обязан проходить, TO — падать.
// fix ищет первый коммит, где тест проходит: FROM обязан падать, TO — проходить. Концы проверяются
// первыми (--assume-to пропускает проверку TO: так зовёт автоматический поиск, где падение только что
// видели); нарушение — вердикт «не воспроизводится», код 1. SKIP на FROM принимается как «до дефекта»
// (теста тогда ещё не было). Шаг SKIP в середине — берётся ближайший сосед в границах интервала; не
// осталось проверяемых — вердикт «между <a> и <b>». Потолок шагов — ceil(log2(N)) + 3 (или --max-steps).
//
// Возврат стенда (в finally при любом исходе): tree-step.sh cleanup снимает worktree поиска; сборка,
// стоявшая до поиска (--restore-release <тег> — ассет релиза, иначе сборка HEAD), ставится обратно
// deploy.sh; исходное ядро (--restore-core, иначе активное на начало поиска) возвращается переключением.
// Служебные результаты возврата идут в отдельный каталог <отчёт>/bisect-<n>/restore, а не в deploy.rc
// и version отчёта прогона.
//
// Вывод. В <отчёт>/summary.md дописывается раздел «## Поиск виновного коммита» (заголовок один на
// отчёт) с подразделом поиска: строки «<n>. <короткий sha> <тема до 60 символов>: PASS|FAIL|SKIP
// (<причина>)», строка `- Итог bisect <тест>: …` и «- Стенд вернулся: да|нет». Копия подраздела —
// <отчёт>/bisect-<n>.md, журнал — <отчёт>/bisect-<n>.log. Темы коммитов — только из git log, имена
// веток не выводятся. Строку итога подхватывает latest.md прогона без присмотра (unattended.sh).
//
// pick. Читает <отчёт>/<цель>/classes.tsv, берёт тесты класса new (go:, pw:), отсеивает тесты с классом
// flaky в истории за 30 дней (поиск по нестабильному тесту даёт ложного виновника, D-22), сопоставляет
// проверку с записью теста через rerun-plan.mjs и печатает TSV: `from <sha> <откуда>` либо
// `skip <причина>`; `test <ядро> go|pw <пакет:Тест|спек> <grep или -> <имя>` на тест (не больше --max,
// по умолчанию 3); `skipped <проверка> <причина>`. FROM — прошлый зелёный полный прогон из истории
// (lastFull green), иначе новейший stable-тег из `git tag --merged HEAD`.
//
// Коды выхода: 0 — вердикт есть (коммит или «между») и стенд вернулся, 1 — нет вердикта, не
// воспроизводится или стенд не вернулся, 2 — неверные аргументы (до обращения к стенду), 3 — обрыв
// связи, 130 — прерван сигналом (стенд возвращён). Команда pick: 0 и TSV, 2 — неверные аргументы.
//
// Только встроенные модули Node и history.mjs.

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { appendFileSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { lastFull, readHistory } from './history.mjs';

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));

// Потолок автоматического поиска по тестам (D-22) задаёт run.sh (RT_BISECT_MAX); здесь — умолчание pick.
const PICK_MAX_DEFAULT = 3;
// Окно истории для отсева нестабильных тестов, дней (D-22).
const FLAKY_DAYS = 30;

const REF_RE = /^[A-Za-z0-9_.][A-Za-z0-9_./~^-]*$/;
const GO_RE = /^[A-Za-z0-9_./-]+:TestRouter[A-Za-z0-9_]+$/;
const SPEC_RE = /^tests\/router\/[A-Za-z0-9_.-]+\.spec\.ts$/;
const TAG_RE = /^v[0-9]+\.[0-9]+\.[0-9]+(-[a-z]+\.[0-9]+)?$/;
const STABLE_RE = /^v[0-9]+\.[0-9]+\.[0-9]+$/;
const SHA_RE = /^[0-9a-f]{40}$/;
const SERVICE = new Set(['bin', 'release', 'last', 'session-last', 'unattended', 'history.tsv', 'history.idx']);

function usage(msg) {
  if (msg) console.error(`bisect: ${msg}`);
  console.error(
    'usage: bisect.mjs --target <id> --core <ядро> --from <ref> --to <ref> [--mode regress|fix] ' +
      '(--go <пакет>:<Тест> | --spec <спек> [--grep <re>]) --report <каталог> [--label <имя>] [--assume-to] ' +
      '[--restore-release <тег>] [--restore-core <ядро>] [--max-steps N]\n' +
      '       bisect.mjs pick --report <каталог> --target <id> [--max N]'
  );
  process.exit(2);
}

function gitIn(cwd, args) {
  return execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim();
}

// Корень рабочей копии: текущий каталог (run.sh переходит в корень перед этапами).
let ROOT;
try {
  ROOT = gitIn(process.cwd(), ['rev-parse', '--show-toplevel']);
} catch {
  usage('запуск вне репозитория');
}
const git = (args) => gitIn(ROOT, args);

// Коммит по ссылке: полный sha или null.
function resolveCommit(ref) {
  if (!REF_RE.test(ref)) return null;
  try {
    const sha = git(['rev-parse', '--verify', '--quiet', `${ref}^{commit}`]);
    return SHA_RE.test(sha) ? sha : null;
  } catch {
    return null;
  }
}

function subjectOf(sha, max) {
  let s = '';
  try {
    s = git(['log', '-1', '--format=%s', sha]);
  } catch {
    s = '';
  }
  s = s.replace(/[\t\r\n]+/g, ' ').trim();
  return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}

// Отчёт строго под <корень>/build/router/<имя>: без «..», без служебных имён.
function resolveReport(arg) {
  const abs = path.isAbsolute(arg) ? path.resolve(arg) : path.resolve(ROOT, arg);
  const base = path.join(ROOT, 'build', 'router');
  const rel = path.relative(base, abs);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel)) return null;
  const parts = rel.split(path.sep);
  if (parts.some((p) => p === '' || p === '.' || p === '..') || SERVICE.has(parts[0])) return null;
  return abs;
}

// --- аргументы -----------------------------------------------------------------------------

function parseArgs(argv, valued, flags) {
  const opt = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (flags.includes(a)) {
      opt[a.slice(2)] = true;
    } else if (valued.includes(a)) {
      if (i + 1 >= argv.length) usage(`${a} без значения`);
      opt[a.slice(2)] = argv[++i];
    } else {
      usage(`неизвестный аргумент: ${a}`);
    }
  }
  return opt;
}

function checkTarget(t) {
  if (!t || !/^[a-z0-9]+$/.test(t)) usage('--target: допустимы a-z и 0-9');
}

// --- pick: тесты для автоматического поиска ---------------------------------------------------

function parentCheck(check) {
  return check.startsWith('go:') ? check.split('/')[0] : check;
}

function isFlakyInHistory(rows, check, now) {
  const parent = parentCheck(check);
  return rows.some((r) => {
    if (r.class !== 'flaky' || parentCheck(r.check) !== parent) return false;
    const t = Date.parse(r.finished_utc);
    return Number.isFinite(t) && now - t <= FLAKY_DAYS * 86400 * 1000;
  });
}

// FROM автоматического поиска: прошлый зелёный полный прогон, иначе новейший stable-тег.
function pickFrom(rows) {
  const lf = lastFull(rows, { green: true });
  if (lf && SHA_RE.test(lf.sha)) {
    try {
      git(['cat-file', '-e', `${lf.sha}^{commit}`]);
      git(['merge-base', '--is-ancestor', lf.sha, 'HEAD']);
      return { sha: lf.sha, why: `прошлый зелёный полный прогон ${lf.report}` };
    } catch {
      // нет коммита или он не предок HEAD — пробуем stable-тег
    }
  }
  let tags = [];
  try {
    tags = git(['tag', '--merged', 'HEAD', '--sort=-v:refname']).split('\n').filter((t) => STABLE_RE.test(t));
  } catch {
    tags = [];
  }
  if (tags.length > 0) {
    const sha = resolveCommit(tags[0]);
    if (sha) return { sha, why: `новейший stable-тег ${tags[0]}` };
  }
  return null;
}

// Запись теста по проверке: через rerun-plan.mjs на одной строке результатов (--results).
function mapCheck(report, target, core, check, k) {
  const tdir = path.join(report, target);
  const name = `bisect-pick-${k}`;
  const resFile = path.join(tdir, `${name}.tsv`);
  const outDir = path.join(tdir, `${name}.d`);
  mkdirSync(tdir, { recursive: true });
  writeFileSync(resFile, `FAIL\t${core}\t${check}\t-\n`);
  const r = spawnSync(
    process.execPath,
    [path.join(SCRIPT_DIR, 'rerun-plan.mjs'), report, target, '--out-dir', outDir, '--results', `${name}.tsv`, '--core', core],
    { cwd: ROOT, encoding: 'utf8' }
  );
  let sel = null;
  if (r.status === 0) {
    try {
      const body = readFileSync(path.join(outDir, `${core}.sel`), 'utf8');
      sel = {};
      for (const line of body.split('\n')) {
        const m = /^([A-Z_]+)=(.*)$/.exec(line);
        if (m) sel[m[1]] = m[2];
      }
    } catch {
      sel = null;
    }
  }
  rmSync(resFile, { force: true });
  rmSync(outDir, { recursive: true, force: true });
  return sel;
}

function pickMain(argv) {
  const opt = parseArgs(argv, ['--report', '--target', '--max'], []);
  checkTarget(opt.target);
  if (!opt.report) usage('pick: нужен --report');
  const report = resolveReport(opt.report);
  if (!report) usage('--report: каталог должен лежать под build/router');
  let max = PICK_MAX_DEFAULT;
  if (opt.max !== undefined) {
    if (!/^[0-9]+$/.test(opt.max)) usage('--max: нужно целое число');
    max = Number(opt.max);
  }
  const out = [];
  const classes = path.join(report, opt.target, 'classes.tsv');
  if (!existsSync(classes)) {
    out.push('skip\tнет classes.tsv цели');
    process.stdout.write(out.join('\n') + '\n');
    return;
  }
  const rows = readHistory();
  const from = pickFrom(rows);
  if (!from) {
    out.push('skip\tнет ни зелёного полного прогона в истории, ни stable-тега: начало поиска неизвестно');
    process.stdout.write(out.join('\n') + '\n');
    return;
  }
  out.push(`from\t${from.sha}\t${from.why}`);
  const now = Date.now();
  const seen = new Set();
  let picked = 0;
  let k = 0;
  for (const line of readFileSync(classes, 'utf8').split('\n')) {
    const col = line.split('\t');
    if (col.length < 3 || col[0] !== 'new') continue;
    const core = col[1];
    const check = col[2];
    if (!/^(go|pw):/.test(check) || !/^(xray|mihomo)$/.test(core) || /^pw:core-switch:/.test(check)) continue;
    const parent = parentCheck(check);
    if (seen.has(parent)) continue;
    seen.add(parent);
    if (isFlakyInHistory(rows, check, now)) {
      out.push(`skipped\t${parent}\tкласс flaky в истории за ${FLAKY_DAYS} дней`);
      continue;
    }
    if (picked >= max) {
      out.push(`skipped\t${parent}\tпотолок ${max} тестов`);
      continue;
    }
    const sel = mapCheck(report, opt.target, core, parent, ++k);
    if (!sel) {
      out.push(`skipped\t${parent}\tтест не сопоставлен с записью`);
      continue;
    }
    if (parent.startsWith('go:')) {
      const entry = (sel.GO_SEQ || '').split(' ')[0];
      if (!GO_RE.test(entry)) {
        out.push(`skipped\t${parent}\tтест не сопоставлен с записью`);
        continue;
      }
      out.push(`test\t${core}\tgo\t${entry}\t-\t${parent}`);
    } else {
      const spec = (sel.SPECS || '').split(' ')[0];
      const grep = sel.GREP || '';
      if (!SPEC_RE.test(spec) || /['\t\n]/.test(grep)) {
        out.push(`skipped\t${parent}\tтест не сопоставлен с записью`);
        continue;
      }
      out.push(`test\t${core}\tpw\t${spec}\t${grep || '-'}\t${parent}`);
    }
    picked++;
  }
  process.stdout.write(out.join('\n') + '\n');
}

// --- поиск ----------------------------------------------------------------------------------

const argv = process.argv.slice(2);
if (argv[0] === 'pick') {
  pickMain(argv.slice(1));
  process.exit(0);
}

const opt = parseArgs(
  argv,
  [
    '--target',
    '--core',
    '--from',
    '--to',
    '--mode',
    '--go',
    '--spec',
    '--grep',
    '--report',
    '--label',
    '--restore-release',
    '--restore-core',
    '--max-steps'
  ],
  ['--assume-to']
);

// Все проверки аргументов — до обращения к стенду (код 2).
checkTarget(opt.target);
if (opt.core !== 'xray' && opt.core !== 'mihomo') usage('--core: xray или mihomo');
const mode = opt.mode || 'regress';
if (mode !== 'regress' && mode !== 'fix') usage('--mode: regress или fix');
if (opt.go && opt.spec) usage('нужен один из --go и --spec');
if (!opt.go && !opt.spec) usage('нужен --go или --spec');
if (opt.go && !GO_RE.test(opt.go)) usage('--go: ожидается <каталог пакета>:TestRouter<Имя>');
if (opt.spec && !SPEC_RE.test(opt.spec)) usage('--spec: ожидается tests/router/<имя>.spec.ts');
if (opt.go && opt.grep !== undefined) usage('--grep относится только к --spec');
if (opt.grep !== undefined && /['\n]/.test(opt.grep)) usage('--grep: недопустимы одинарная кавычка и перевод строки');
if (opt['restore-release'] !== undefined && !TAG_RE.test(opt['restore-release'])) {
  usage('--restore-release: ожидается тег vX.Y.Z или vX.Y.Z-beta.N');
}
if (opt['restore-core'] !== undefined && opt['restore-core'] !== 'xray' && opt['restore-core'] !== 'mihomo') {
  usage('--restore-core: xray или mihomo');
}
if (opt['max-steps'] !== undefined && !/^[1-9][0-9]*$/.test(opt['max-steps'])) usage('--max-steps: положительное целое');
if (!opt.report) usage('нужен --report');
const REPORT = resolveReport(opt.report);
if (!REPORT) usage('--report: каталог должен лежать под build/router');
if (!opt.from || !opt.to) usage('нужны --from и --to');
const FROM = resolveCommit(opt.from);
if (!FROM) usage(`--from: '${opt.from}' не указывает на коммит`);
const TO = resolveCommit(opt.to);
if (!TO) usage(`--to: '${opt.to}' не указывает на коммит`);
if (FROM === TO) usage('--from и --to указывают на один коммит');
try {
  git(['merge-base', '--is-ancestor', FROM, TO]);
} catch {
  usage('--from не предок --to');
}

const TARGET = opt.target;
const CORE = opt.core;
const LABEL = opt.label || (opt.go ? `go:${opt.go.split(':')[1]}` : `pw:${opt.grep || path.basename(opt.spec)}`);
const short = (sha) => sha.slice(0, 8);

// Кандидаты: коммиты после FROM до TO включительно, от старых к новым.
function buildCandidates() {
  const onChain = git(['rev-list', '--first-parent', TO]).split('\n').includes(FROM);
  const list = onChain
    ? git(['rev-list', '--first-parent', '--reverse', TO, `^${FROM}`])
    : git(['rev-list', '--ancestry-path', '--reverse', `${FROM}..${TO}`]);
  return list.split('\n').filter((s) => SHA_RE.test(s));
}
const C = buildCandidates();
if (C.length === 0) usage('между --from и --to нет коммитов');
const CAP = opt['max-steps'] ? Number(opt['max-steps']) : Math.ceil(Math.log2(C.length)) + 3;

// Номер поиска в отчёте и пути возврата.
mkdirSync(REPORT, { recursive: true });
let N = 1;
while (existsSync(path.join(REPORT, `bisect-${N}.md`)) || existsSync(path.join(REPORT, `bisect-${N}.log`))) N++;
const BASE = path.join(REPORT, `bisect-${N}`);
const LOG = path.join(REPORT, `bisect-${N}.log`);
const RESTORE_DIR = path.join(BASE, 'restore');
writeFileSync(LOG, '');

function say(msg) {
  console.log(msg);
  appendFileSync(LOG, `${msg}\n`);
}

// --- запуск процессов ------------------------------------------------------------------------

let aborted = false;
let cleaning = false;
let child = null;

for (const sig of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
  process.on(sig, () => {
    if (cleaning) return;
    aborted = true;
    if (child) {
      try {
        // этап идёт отдельной группой процессов (run): сигнал уходит всей группе
        process.kill(-child.pid, 'SIGTERM');
      } catch {
        // группа уже вышла
      }
    }
  });
}

// Окружение этапа: блокировка уже держится родителем, состав набора и отчёта задаёт поиск.
function stageEnv(extra = {}) {
  const env = { ...process.env };
  for (const k of [
    'RT_SPECS',
    'RT_SPECS_FIRST',
    'RT_PW_GREP',
    'RT_PW_GREP_INVERT',
    'RT_PW_CORE_ONLY',
    'RT_GO_PKGS',
    'RT_GO_RUN',
    'RT_GO_RUN_SEQ',
    'RT_ART_SUB',
    'RT_RESULTS_NAME',
    'RT_REPORT',
    'RT_REPORT_DIR',
    'RT_CORE',
    'XCP_WANT_CORE'
  ]) {
    delete env[k];
  }
  return { ...env, RT_LOCK_HELD: '1', ROUTERS: TARGET, ...extra };
}

// Запуск этапа без оболочки (execFile-семантика): вывод — в консоль и журнал, stdout возвращается.
function run(cmd, args, extra = {}) {
  if (aborted && !cleaning) return Promise.resolve({ rc: 130, out: '' });
  appendFileSync(LOG, `\n$ ${[cmd, ...args].join(' ')}\n`);
  return new Promise((resolve) => {
    const c = spawn(cmd, args, {
      cwd: ROOT,
      env: stageEnv(extra),
      stdio: ['ignore', 'pipe', 'pipe'],
      detached: true
    });
    child = c;
    let out = '';
    c.stdout.on('data', (d) => {
      out += d;
      appendFileSync(LOG, d);
    });
    c.stderr.on('data', (d) => appendFileSync(LOG, d));
    c.on('error', (e) => {
      child = null;
      appendFileSync(LOG, `bisect: не запустился ${cmd}: ${e.message}\n`);
      resolve({ rc: 1, out });
    });
    c.on('close', (code, sig) => {
      child = null;
      const rc = code ?? (sig ? 128 + 15 : 1);
      // после прерывания потомки группы могли пережить лидера: возврат стенда ждёт их выхода
      if (aborted && !cleaning && Number.isInteger(c.pid)) drainGroup(c.pid).then(() => resolve({ rc, out }));
      else resolve({ rc, out });
    });
  });
}

// Ждёт, пока группа процессов опустеет: до 30 с, затем SIGKILL группе и ещё до 5 с.
async function drainGroup(pgid) {
  const alive = () => {
    try {
      process.kill(-pgid, 0);
      return true;
    } catch (e) {
      return e.code !== 'ESRCH';
    }
  };
  const pause = (ms) => new Promise((r) => setTimeout(r, ms));
  for (let i = 0; i < 60 && alive(); i++) await pause(500);
  if (!alive()) return;
  try {
    process.kill(-pgid, 'SIGKILL');
  } catch {
    // группа уже вышла
  }
  for (let i = 0; i < 10 && alive(); i++) await pause(500);
}

// Команда на цели через rt_ssh из lib.sh; печатает stdout команды.
function remote(cmd) {
  const r = spawnSync(
    'sh',
    ['-c', '. "$1/lib.sh"; rt_load_targets >/dev/null && rt_ssh "$2" "$3"', 'sh', SCRIPT_DIR, TARGET, cmd],
    { cwd: ROOT, env: { ...process.env, ROUTERS: TARGET }, encoding: 'utf8' }
  );
  return { rc: r.status ?? 1, out: (r.stdout || '').trim() };
}

const CORE_CMD =
  'x=$(pidof xray | cut -d" " -f1); m=$(pidof mihomo | cut -d" " -f1); ' +
  'if [ -n "$x" ] && [ -n "$m" ]; then echo both; elif [ -n "$x" ]; then echo xray; ' +
  'elif [ -n "$m" ]; then echo mihomo; else echo none; fi';

// --- шаг и предикаты --------------------------------------------------------------------------

const isBad = (result) => (mode === 'regress' ? result === 'FAIL' : result === 'PASS');

// Сбой связи внутри теста (таймаут клиента, отказ соединения, TLS): такой FAIL шага говорит о
// сети стенда, а не о коде коммита. Если падение на «падающем» конце диапазона (эталон) не
// такое, шаг с сетевым FAIL засчитывается как SKIP — иначе поиск сдвигает границу по сбою сети.
const NET_FAIL =
  /context deadline exceeded|Client\.Timeout|connection refused|connection reset|i\/o timeout|TLS handshake|net::ERR_|ECONNRESET|socket hang up/;
let refNet = null;

// Деталь первой упавшей строки шага (<каталог шага>/<цель>/test.tsv): статус FAIL или XPASS.
function failDetail(dir) {
  let text = '';
  try {
    text = readFileSync(path.join(dir, TARGET, 'test.tsv'), 'utf8');
  } catch {
    return '';
  }
  for (const line of text.split('\n')) {
    const c = line.split('\t');
    if (c[0] === 'FAIL' || c[0] === 'XPASS') return c.slice(3).join('\t');
  }
  return '';
}

const lines = [];
let stepNo = 0;
const cache = new Map();
let netLoss = false;
let stepError = '';

// Шаг на коммите: { result, reason } либо null (обрыв связи, ошибка шага или прерывание — см. netLoss,
// stepError, aborted). Результат кэшируется по sha.
async function probe(sha, searchStep) {
  if (cache.has(sha)) return cache.get(sha);
  const n = ++stepNo;
  const dir = path.join(BASE, `${String(n).padStart(2, '0')}-${short(sha)}`);
  const args = [path.join(SCRIPT_DIR, 'tree-step.sh'), sha, TARGET, CORE];
  if (opt.go) args.push('--go', opt.go);
  else {
    args.push('--spec', opt.spec);
    if (opt.grep) args.push('--grep', opt.grep);
  }
  args.push('--out', dir);
  say(`шаг ${n}${searchStep ? '' : ' (конец)'}: ${short(sha)} ${subjectOf(sha, 60)}`);
  const { rc, out } = await run('sh', args);
  if (aborted) return null;
  if (rc === 3) {
    netLoss = true;
    return null;
  }
  if (rc !== 0) {
    stepError = `ошибка шага на ${short(sha)} (код ${rc})`;
    return null;
  }
  const result = (/^RESULT=(PASS|FAIL|SKIP)$/m.exec(out) || [])[1];
  const reason = ((/^REASON=(.*)$/m.exec(out) || [])[1] || '').trim();
  if (!result) {
    stepError = `шаг на ${short(sha)} не вернул RESULT`;
    return null;
  }
  const r = { result, reason };
  if (result === 'FAIL') {
    const net = NET_FAIL.test(failDetail(dir));
    if (!searchStep) {
      // эталон — конец диапазона, где тест обязан падать (FROM в fix, TO в regress)
      if (refNet === null) refNet = net;
    } else if (net && refNet === false) {
      r.result = 'SKIP';
      r.reason = 'сбой связи в тесте (не то падение, что на конце диапазона)';
    }
  }
  cache.set(sha, r);
  lines.push(`${n}. ${short(sha)} ${subjectOf(sha, 60)}: ${r.result} (${r.reason || '-'})`);
  say(lines[lines.length - 1]);
  return r;
}

// Порядок проб внутри (lo, hi): середина, затем соседи по очереди.
function* probeOrder(mid, lo, hi) {
  for (let d = 0; ; d++) {
    const a = mid + d;
    const b = mid - d;
    let any = false;
    if (a > lo && a < hi) {
      yield a;
      any = true;
    }
    if (d > 0 && b > lo && b < hi) {
      yield b;
      any = true;
    }
    if (!any) return;
  }
}

// --- поиск ------------------------------------------------------------------------------------

async function search() {
  const toShort = short(TO);
  const fromShort = short(FROM);
  // конец TO: в режиме regress обязан падать, в fix — проходить
  if (!opt['assume-to']) {
    const r = await probe(TO, false);
    if (!r) return { kind: 'none' };
    if (r.result === 'SKIP') {
      return { kind: 'norepro', text: `не воспроизводится на ${toShort}: шаг пропущен (${r.reason || '-'})` };
    }
    if (!isBad(r.result)) {
      return {
        kind: 'norepro',
        text: `не воспроизводится на ${toShort}: тест ${r.result === 'PASS' ? 'прошёл' : 'не прошёл'}`
      };
    }
  }
  // конец FROM: в regress обязан проходить, в fix — падать; SKIP принимается (теста ещё не было)
  const f = await probe(FROM, false);
  if (!f) return { kind: 'none' };
  if (f.result !== 'SKIP' && isBad(f.result)) {
    return {
      kind: 'norepro',
      text: `не воспроизводится на ${fromShort}: тест ${f.result === 'PASS' ? 'проходил' : 'падал'} уже там`
    };
  }
  let lo = -1;
  let hi = C.length - 1;
  let steps = 0;
  let capped = false;
  while (hi - lo > 1) {
    const mid = lo + Math.floor((hi - lo) / 2);
    let decided = false;
    for (const idx of probeOrder(mid, lo, hi)) {
      if (cache.get(C[idx])?.result === 'SKIP') continue;
      if (steps >= CAP) {
        capped = true;
        break;
      }
      const r = await probe(C[idx], true);
      steps++;
      if (!r) return { kind: 'none' };
      if (r.result === 'SKIP') continue;
      if (isBad(r.result)) hi = idx;
      else lo = idx;
      decided = true;
      break;
    }
    if (!decided) break;
  }
  if (hi - lo === 1) {
    return { kind: 'commit', sha: C[hi] };
  }
  const a = lo < 0 ? FROM : C[lo];
  const b = C[hi];
  const why = capped ? 'потолок шагов' : 'остальные шаги пропущены (SKIP)';
  return { kind: 'between', a, b, why };
}

// --- возврат стенда ----------------------------------------------------------------------------

async function restoreStand(restoreCore) {
  cleaning = true;
  let ok = true;
  const notes = [];
  await run('sh', [path.join(SCRIPT_DIR, 'tree-step.sh'), 'cleanup']);
  mkdirSync(path.join(RESTORE_DIR, TARGET), { recursive: true });
  const dargs = [path.join(SCRIPT_DIR, 'deploy.sh')];
  if (opt['restore-release']) dargs.push('--release', opt['restore-release']);
  dargs.push('--target', TARGET);
  const d = await run('sh', dargs, { RT_REPORT: RESTORE_DIR });
  const rcFile = path.join(RESTORE_DIR, TARGET, 'deploy.rc');
  const drc = existsSync(rcFile) ? readFileSync(rcFile, 'utf8').trim() : '-';
  if (d.rc !== 0 || drc !== '0') {
    ok = false;
    notes.push(`сборка не возвращена (код ${d.rc}, deploy.rc ${drc})`);
  }
  if (restoreCore) {
    const now = remote(CORE_CMD);
    if (now.rc === 0 && now.out !== restoreCore) {
      const s = await run('sh', [path.join(SCRIPT_DIR, 'pw-suite.sh'), TARGET, restoreCore, '--switch', restoreCore], {
        RT_REPORT: RESTORE_DIR,
        RT_RESULTS_NAME: 'switch.tsv',
        RT_ART_SUB: 'restore-switch'
      });
      if (s.rc !== 0) {
        ok = false;
        notes.push(`ядро не возвращено (код ${s.rc})`);
      }
    } else if (now.rc !== 0) {
      ok = false;
      notes.push(`ядро не проверено (код ${now.rc})`);
    }
    const fin = remote(CORE_CMD);
    if (fin.rc !== 0 || fin.out !== restoreCore) {
      ok = false;
      notes.push(`ядро сейчас ${fin.rc === 0 ? fin.out : 'неизвестно'}, исходное ${restoreCore}`);
    }
  }
  return { ok, notes };
}

// --- вывод в summary.md ------------------------------------------------------------------------

function appendSummary(block) {
  const file = path.join(REPORT, 'summary.md');
  let cur = '';
  try {
    cur = readFileSync(file, 'utf8');
  } catch {
    cur = '';
  }
  let add = '';
  if (!/^## Поиск виновного коммита$/m.test(cur)) add += '\n## Поиск виновного коммита\n';
  add += `\n${block}\n`;
  appendFileSync(file, add);
  writeFileSync(path.join(REPORT, `bisect-${N}.md`), `${block}\n`);
}

async function main() {
  let restoreCore = opt['restore-core'];
  if (!restoreCore) {
    const now = remote(CORE_CMD);
    if (now.rc === 3) {
      console.error('bisect: нет связи со стендом');
      return 3;
    }
    if (now.rc === 0 && (now.out === 'xray' || now.out === 'mihomo')) restoreCore = now.out;
  }
  let flakyWarn = false;
  try {
    flakyWarn = isFlakyInHistory(readHistory(), LABEL, Date.now());
  } catch {
    flakyWarn = false;
  }
  say(
    `поиск ${N}: ${LABEL} на ${CORE}, режим ${mode}, ${short(FROM)}..${short(TO)}, кандидатов ${C.length}, потолок шагов ${CAP}`
  );
  let verdict = { kind: 'none' };
  try {
    verdict = await search();
  } catch (e) {
    stepError = `сбой поиска: ${e.message}`;
    verdict = { kind: 'none' };
  } finally {
    // возврат стенда — при любом исходе
    verdict.restore = await restoreStand(restoreCore);
  }
  const back = verdict.restore.ok;
  let itog;
  let code;
  const extra = [];
  if (flakyWarn) extra.push(`- Предупреждение: у теста класс flaky в истории за ${FLAKY_DAYS} дней, вердикт ненадёжен`);
  if (verdict.kind === 'commit') {
    itog = `- Итог bisect ${LABEL}: ${short(verdict.sha)} ${subjectOf(verdict.sha, 80)}`;
    extra.push(mode === 'regress' ? '- Первый коммит, где тест падает' : '- Первый коммит, где тест проходит');
    code = back ? 0 : 1;
  } else if (verdict.kind === 'between') {
    itog = `- Итог bisect ${LABEL}: между ${short(verdict.a)} и ${short(verdict.b)}`;
    extra.push(`- Интервал не сужен до коммита: ${verdict.why}`);
    code = back ? 0 : 1;
  } else if (verdict.kind === 'norepro') {
    itog = `- Итог bisect ${LABEL}: ${verdict.text}`;
    code = 1;
  } else {
    const why = netLoss ? 'обрыв связи' : aborted ? 'прерван сигналом' : stepError || 'шаг не выполнен';
    itog = `- Итог bisect ${LABEL}: нет вердикта (${why})`;
    code = netLoss ? 3 : aborted ? 130 : 1;
  }
  const block = [
    `### Поиск ${N}: ${LABEL} (${CORE}, режим ${mode}, ${short(FROM)}..${short(TO)})`,
    '',
    `- Кандидатов: ${C.length}, потолок шагов: ${CAP}`,
    ...lines,
    itog,
    ...extra,
    `- Стенд вернулся: ${back ? 'да' : `нет (${verdict.restore.notes.join('; ')})`}`
  ].join('\n');
  appendSummary(block);
  say(itog);
  say(`стенд вернулся: ${back ? 'да' : 'нет'}`);
  return code;
}

main().then(
  (code) => process.exit(code),
  (e) => {
    console.error(`bisect: ${e.message}`);
    process.exit(1);
  }
);
