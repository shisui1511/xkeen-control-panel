#!/usr/bin/env node
/*
 * release-guard.js — сверка выпуска для хуков Claude Code и гейта ship:pre (D-05, D-18…D-21).
 *
 * Вся логика сверки лежит здесь, в отслеживаемом коде; локальные хуки (scripts/local/hook-release-*.sh)
 * только передают событие со stdin. Файл — CommonJS без зависимостей. git и gh вызываются только
 * через execFileSync/execFile с массивом аргументов (без shell) и с тайм-аутом; текст из событий
 * никогда не попадает в командную строку.
 *
 * Подкоманды (код выхода):
 *   scan [--file <путь>]       текст со stdin или из файла; печатает каждый невышедший номер версии
 *                              панели: 1 — найдено, 0 — нет, 2 — ошибка аргументов
 *   planning-check [--planning <д>]
 *                              невышедшие номера в milestone/milestone_name STATE.md и в строке
 *                              ROADMAP.md с 🚧: 1 — найдено, 0 — нет
 *   pre-hook [--planning <д>]  событие PreToolUse со stdin: deny на `gh pr create|edit` с невышедшим
 *                              номером, на метки вех вида номера версии и на закрытие вехи без
 *                              пройденной сверки; штатно всегда код 0 (с deny и без вывода)
 *   close-gate                 гейт закрытия вехи: новейший тег в origin/main покрывает main и имеет
 *                              статус stand/arm64-full = success, ни один последний статус stand/*
 *                              не failure и не error: 0 — можно, 1 — нельзя, 2 — сверка
 *                              не выполнена (сеть, срок RELEASE_GUARD_DEADLINE_MS, по умолчанию 20000)
 *   range --label <метка> [--planning <д>]
 *                              печатает строку `**Версии:** vA–vB` вехи
 *   post-hook [--planning <д>] событие PostToolUse: после закрытия вехи вписывает диапазон версий в
 *                              MILESTONES.md и запускает выпуск хвоста (HOOK_DRY_RUN=1 — без запуска);
 *                              после gh pr create|edit перепроверяет заголовок и тело PR, а после
 *                              Skill gsd-progress/gsd-next добавляет сводку выпуска
 *   status --event <SessionStart|UserPromptSubmit|PostToolUse> [--plain] [--planning <д>]
 *                              сводка выпуска только по делу (пре-релиз ждёт смоука, красный Auto
 *                              Release, стоп выпуска, невышедший номер в STATE/ROADMAP); нет сети —
 *                              одна строка «сверка выпуска не выполнена»; общий срок 7 с; UserPromptSubmit
 *                              отвечает только на /gsd-progress и /gsd-next; --plain — строки без JSON
 *
 * Невышедший номер — ядро строго больше наибольшего среди всех тегов `v*` (см. lib/release-versions.js);
 * при находке по локальным тегам номер перепроверяется по `git ls-remote --tags origin`, чтобы
 * свежий релиз не считался невышедшим.
 *
 * Usage: node scripts/release-guard.js <подкоманда> [аргументы]
 */
'use strict';

const { execFileSync, execFile } = require('child_process');
const fs = require('fs');
const path = require('path');
const rv = require('./lib/release-versions.js');

const T0 = Date.now();
const MAX_BODY_BYTES = 1024 * 1024;
const LS_REMOTE_TIMEOUT = 5000;

// ---------------------------------------------------------------------------------------------
// Вызовы git/gh
// ---------------------------------------------------------------------------------------------

let rootCache = null;
function root() {
  if (rootCache) return rootCache;
  try {
    rootCache = execFileSync('git', ['rev-parse', '--show-toplevel'], {
      cwd: process.cwd(),
      encoding: 'utf8',
      timeout: 3000,
      stdio: ['ignore', 'pipe', 'ignore']
    }).trim();
  } catch {
    rootCache = '';
  }
  if (!rootCache) rootCache = path.resolve(__dirname, '..');
  return rootCache;
}

function failText(err) {
  if (err && err.code === 'ETIMEDOUT') return 'тайм-аут';
  const stderr = err && err.stderr ? String(err.stderr).trim().split('\n')[0] : '';
  return stderr || (err && err.message) || String(err);
}

function execOpts(timeout) {
  return {
    cwd: root(),
    encoding: 'utf8',
    timeout,
    stdio: ['ignore', 'pipe', 'pipe'],
    maxBuffer: 16 * 1024 * 1024,
    env: Object.assign({}, process.env, { GIT_TERMINAL_PROMPT: '0' })
  };
}

function wrapError(name, args, err) {
  const e = new Error(`${name} ${args[0]}: ${failText(err)}`);
  e.status = err && err.status;
  e.timedOut = !!(err && err.code === 'ETIMEDOUT');
  return e;
}

// Общий срок сверки закрытия (D-05): RELEASE_GUARD_DEADLINE_MS, по умолчанию 20 с — меньше
// тайм-аута обёртки хука (25 с) и Claude Code (30 с). Пока срок не задан, ограничен только сам вызов.
let deadlineAt = null;
function setDeadline() {
  const ms = Number(process.env.RELEASE_GUARD_DEADLINE_MS);
  deadlineAt = T0 + (Number.isFinite(ms) && ms > 0 ? ms : 20000);
}

// Тайм-аут вызова: меньшее из собственного и остатка срока; остатка нет — исключение.
function callTimeout(own) {
  if (deadlineAt === null) return own;
  const left = deadlineAt - Date.now();
  if (left <= 0) throw new Error('истёк срок сверки');
  return Math.min(own, left);
}

function git(args, ownTimeout) {
  const timeout = callTimeout(ownTimeout);
  try {
    return execFileSync('git', args, execOpts(timeout));
  } catch (err) {
    throw wrapError('git', args, err);
  }
}

function gh(args, ownTimeout) {
  const timeout = callTimeout(ownTimeout);
  try {
    return execFileSync('gh', args, execOpts(timeout));
  } catch (err) {
    throw wrapError('gh', args, err);
  }
}

// ---------------------------------------------------------------------------------------------
// Теги
// ---------------------------------------------------------------------------------------------

// Пары тег -> sha коммита (по `^{}`, иначе sha ссылки); читается один раз за вызов.
let remoteCache = null;
function remoteTagMap() {
  if (remoteCache) return remoteCache;
  const out = git(['ls-remote', '--tags', 'origin'], LS_REMOTE_TIMEOUT);
  const map = new Map();
  const peeled = new Set();
  for (const line of out.split('\n')) {
    const m = /^([0-9a-f]{40})\s+refs\/tags\/(.+?)(\^\{\})?$/.exec(line.trim());
    if (!m) continue;
    if (m[3]) {
      map.set(m[2], m[1]);
      peeled.add(m[2]);
    } else if (!peeled.has(m[2])) {
      map.set(m[2], m[1]);
    }
  }
  remoteCache = map;
  return map;
}

// Порядок версий: ядро, затем пре-релиз меньше stable того же ядра, пре-релизы — по имени и номеру.
function tagCmp(a, b) {
  const pa = rv.parseTag(a);
  const pb = rv.parseTag(b);
  const c = rv.coreCmp(pa, pb);
  if (c !== 0) return c;
  if (!pa.pre && !pb.pre) return 0;
  if (!pa.pre) return 1;
  if (!pb.pre) return -1;
  const ma = /^(.*?)(?:\.(\d+))?$/.exec(pa.pre);
  const mb = /^(.*?)(?:\.(\d+))?$/.exec(pb.pre);
  if (ma[1] !== mb[1]) return ma[1] < mb[1] ? -1 : 1;
  return Number(ma[2] || 0) - Number(mb[2] || 0);
}

// Наибольший тег по порядку версий или ''.
function latestTag(tags) {
  const list = tags.filter(t => rv.parseTag(t));
  if (!list.length) return '';
  return list.slice().sort(tagCmp)[list.length - 1];
}

// Невышедшие номера в тексте. Находка по локальным тегам перепроверяется по удалённым:
// без strict сбой ls-remote оставляет локальный результат, со strict — пробрасывается.
function unreleasedIn(text, strict) {
  const local = rv.localTags(root());
  let found = rv.findUnreleased(text, local);
  let tags = local;
  if (found.length) {
    try {
      tags = Array.from(new Set(local.concat(Array.from(remoteTagMap().keys()))));
      found = rv.findUnreleased(text, tags);
    } catch (err) {
      if (strict) throw err;
    }
  }
  const versions = Array.from(new Set(found.map(f => rv.formatCore(f.core))));
  return { versions, tags };
}

// ---------------------------------------------------------------------------------------------
// Ввод-вывод
// ---------------------------------------------------------------------------------------------

function readStdin() {
  try {
    return fs.readFileSync(0, 'utf8');
  } catch {
    return '';
  }
}

function flag(argv, name) {
  const i = argv.indexOf(name);
  if (i < 0 || i + 1 >= argv.length) return '';
  return argv[i + 1];
}

function emitDeny(reason) {
  process.stdout.write(
    JSON.stringify({
      hookSpecificOutput: {
        hookEventName: 'PreToolUse',
        permissionDecision: 'deny',
        permissionDecisionReason: reason
      }
    })
  );
}

// ---------------------------------------------------------------------------------------------
// scan
// ---------------------------------------------------------------------------------------------

function cmdScan(argv) {
  const hasFile = argv.includes('--file');
  const file = flag(argv, '--file');
  if (hasFile && !file) {
    process.stderr.write('scan: у --file нужен путь\n');
    return 2;
  }
  let text;
  try {
    text = file ? fs.readFileSync(file, 'utf8') : readStdin();
  } catch (err) {
    process.stderr.write(`scan: не прочитан ${file}: ${err.message}\n`);
    return 2;
  }
  const { versions } = unreleasedIn(text, false);
  for (const v of versions) process.stdout.write(`${v}\n`);
  return versions.length ? 1 : 0;
}

// ---------------------------------------------------------------------------------------------
// pre-hook: gh pr create|edit
// ---------------------------------------------------------------------------------------------

const PR_CMD_RE = /\bgh\s+pr\s+(create|edit)\b/;

// Тексты файлов из --body-file/-F с буквальным путём (без $ и обратных кавычек), до 1 МБ.
function bodyFileTexts(command, cwd) {
  const texts = [];
  const re = /(?:^|\s)(?:--body-file|-F)(?:=|\s+)("[^"]*"|'[^']*'|\S+)/g;
  let m;
  while ((m = re.exec(command)) !== null) {
    let p = m[1];
    if ((p.startsWith('"') && p.endsWith('"')) || (p.startsWith("'") && p.endsWith("'"))) {
      p = p.slice(1, -1);
    }
    if (!p || p === '-' || /[$`]/.test(p)) continue;
    try {
      const full = path.resolve(cwd || process.cwd(), p);
      // fstat по открытому дескриптору: между проверкой и чтением файл не подменить
      const fd = fs.openSync(full, 'r');
      try {
        const st = fs.fstatSync(fd);
        if (!st.isFile() || st.size > MAX_BODY_BYTES) continue;
        texts.push(fs.readFileSync(fd, 'utf8'));
      } finally {
        fs.closeSync(fd);
      }
    } catch {
      // нет файла — тело проверит post-hook
    }
  }
  return texts;
}

// Причина отказа для gh pr create|edit с невышедшим номером или null.
function prDenyReason(command, ev) {
  if (!PR_CMD_RE.test(command)) return null;
  const text = [command].concat(bodyFileTexts(command, ev && ev.cwd)).join('\n');
  const { versions, tags } = unreleasedIn(text, false);
  if (!versions.length) return null;
  const last = latestTag(tags) || '?';
  return (
    `Номер ${versions[0]} ещё не вышел (последний тег — ${last}). ` +
    `Уберите номер из заголовка и тела PR или добавьте строку version-ok: ${versions[0]} <причина>.`
  );
}

// ---------------------------------------------------------------------------------------------
// Планирование: frontmatter STATE.md, невышедшие номера в STATE/ROADMAP
// ---------------------------------------------------------------------------------------------

function readFile(file) {
  try {
    return fs.readFileSync(file, 'utf8');
  } catch {
    return null;
  }
}

// Простые строки `ключ: значение` frontmatter (между первой парой `---`).
function frontmatter(text) {
  const out = {};
  if (!text) return out;
  const lines = text.split('\n');
  if (lines[0].trim() !== '---') return out;
  for (let i = 1; i < lines.length; i++) {
    if (lines[i].trim() === '---') break;
    const m = /^([A-Za-z_][\w-]*):[ \t]*(.*?)[ \t]*$/.exec(lines[i]);
    if (m) out[m[1]] = m[2].replace(/^(["'])(.*)\1$/, '$2');
  }
  return out;
}

function defaultPlanning() {
  return path.join(root(), '.planning');
}

// Значения milestone и milestone_name из STATE.md и первая строка ROADMAP.md с 🚧.
function planningText(planning) {
  const parts = [];
  const fm = frontmatter(readFile(path.join(planning, 'STATE.md')));
  if (fm.milestone) parts.push(fm.milestone);
  if (fm.milestone_name) parts.push(fm.milestone_name);
  const road = readFile(path.join(planning, 'ROADMAP.md'));
  if (road) {
    const first = road.split('\n').find(l => l.includes('🚧'));
    if (first) parts.push(first);
  }
  return parts.join('\n');
}

// Отсутствующий каталог или файл — без находок (в worktree .planning нет).
function planningFindings(planning, strict) {
  const text = planningText(planning);
  if (!text) return [];
  return unreleasedIn(text, strict).versions;
}

function cmdPlanningCheck(argv) {
  const found = planningFindings(flag(argv, '--planning') || defaultPlanning(), false);
  for (const v of found) process.stdout.write(`${v}\n`);
  return found.length ? 1 : 0;
}

// ---------------------------------------------------------------------------------------------
// Состояние main и гейт закрытия вехи (D-05, D-17)
// ---------------------------------------------------------------------------------------------

const TAG_NAME_RE = /^v\d+\.\d+\.\d+(-[a-z]+\.\d+)?$/;
// Те же «достойные» коммиты, что WORTHY в .github/scripts/auto_release.sh
const WORTHY_RE =
  /^(feat|fix|perf|refactor|revert)(\([^)]*\))?!?:|^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE/;

// Достижим ли коммит из origin/main: 0 — да, 1 — нет, иное (тайм-аут, сбой запуска) — исключение.
function reachableFromMain(sha) {
  try {
    git(['cat-file', '-e', `${sha}^{commit}`], 5000);
  } catch (err) {
    if (err.timedOut || typeof err.status !== 'number') throw err;
    return false; // коммита нет локально — его нет и в истории main
  }
  try {
    git(['merge-base', '--is-ancestor', sha, 'origin/main'], 5000);
    return true;
  } catch (err) {
    if (err.timedOut || err.status !== 1) throw err;
    return false;
  }
}

// Хвост: новейший тег (stable или пре-релиз), достижимый из origin/main; null — такого нет.
function findTail() {
  git(['fetch', '-q', 'origin', 'main'], 8000);
  const map = remoteTagMap();
  const names = Array.from(map.keys()).filter(n => TAG_NAME_RE.test(n)).sort(tagCmp).reverse();
  for (const name of names) {
    const sha = map.get(name);
    if (reachableFromMain(sha)) return { tag: name, sha, tags: names };
  }
  return null;
}

// Статусы стенда на коммите: { full, red } — full: последний stand/arm64-full — success;
// red: контексты stand/*, чей последний статус failure или error (такой хвост автовыпуск в
// stable не продвинет, как и verdict в auto_release.sh). Красный смоук снимает более поздний
// success полного прогона, обратное не действует (stand_red в auto_release.sh).
function standState(sha) {
  const out = gh(['api', `repos/{owner}/{repo}/commits/${sha}/statuses?per_page=100`], 5000);
  const list = JSON.parse(out).filter(s => s && typeof s.context === 'string' && s.context.startsWith('stand/'));
  list.sort((a, b) => String(b.created_at || '').localeCompare(String(a.created_at || '')));
  const last = new Map();
  const at = new Map();
  for (const s of list) {
    if (last.has(s.context)) continue;
    last.set(s.context, s.state);
    at.set(s.context, String(s.created_at || ''));
  }
  const isRed = st => st === 'failure' || st === 'error';
  const smoke = 'stand/arm64-smoke';
  const fullCtx = 'stand/arm64-full';
  const red = Array.from(last.entries())
    .filter(([ctx, st]) => isRed(st)
      && !(ctx === smoke && last.get(fullCtx) === 'success' && at.get(fullCtx) > at.get(smoke)))
    .map(([ctx]) => ctx)
    .sort();
  return { full: last.get('stand/arm64-full') === 'success', red };
}

// Результат: { code, lines, reason }; 0 — можно закрывать, 1 — нельзя, 2 — сверка не выполнена.
function closeGate() {
  if (deadlineAt === null) setDeadline();
  try {
    const tail = findTail();
    if (!tail) {
      return {
        code: 1,
        lines: [
          'close-gate: ни один тег не достижим из origin/main: дождаться пре-релиза, затем полный прогон на нём (make router-test SUITE=full RELEASE=<тег>)'
        ]
      };
    }
    const log = git(['log', '--format=%s%n%b', `${tail.sha}..origin/main`], 5000);
    const covers = !log.split('\n').some(l => WORTHY_RE.test(l));
    if (!covers) {
      return {
        code: 1,
        lines: [
          `close-gate: после ${tail.tag} в main есть изменения для выпуска: дождаться нового пре-релиза, затем полный прогон на нём`
        ]
      };
    }
    const stand = standState(tail.sha);
    if (!stand.full) {
      return {
        code: 1,
        lines: [
          `close-gate: нет зелёного полного прогона на ${tail.tag}: make router-test SUITE=full RELEASE=${tail.tag}`
        ]
      };
    }
    if (stand.red.length) {
      return {
        code: 1,
        lines: [
          `close-gate: на ${tail.tag} красный статус стенда (${stand.red.join(', ')}): автовыпуск хвост в stable не продвинет, сначала разобрать`
        ]
      };
    }
    return { code: 0, lines: [`close-gate: можно закрывать, хвост ${tail.tag}`] };
  } catch (err) {
    const reason = err && err.message ? err.message : String(err);
    return { code: 2, lines: [`close-gate: сверка не выполнена: ${reason}`], reason };
  }
}

function cmdCloseGate() {
  setDeadline();
  const r = closeGate();
  for (const l of r.lines) process.stdout.write(`${l}\n`);
  return r.code;
}

// ---------------------------------------------------------------------------------------------
// Вызовы команд GSD (одно определение для pre-hook, post-hook и обёртки плана 08)
// ---------------------------------------------------------------------------------------------

// Вызов начинается в начале строки, после ; & | ( { ` ! $( , после ключевого слова оболочки
// (if then do elif else while until) или после `bash|sh|zsh|dash … -c` с кавычкой; перед именем
// допускаются time/env/command/exec/nohup/sudo и присваивания VAR=…. Имя — gsd_run, gsd-sdk,
// …gsd-tools[.cjs] или node с любым скриптом. Кавычки и heredoc иначе не разбираются: ложный
// отказ гейта дешевле пропуска. Тот же шаблон — в scripts/local/hook-release.sh (is_closing).
const GSD_CALL_SRC =
  '(?:^|[;&|({`!]|\\$\\(|\\b(?:if|then|do|elif|else|while|until)\\s|\\b(?:bash|sh|zsh|dash)\\s+(?:-[A-Za-z]+\\s+)*-[A-Za-z]*c\\s+["\']?)' +
  '\\s*(?:(?:time|env|command|exec|nohup|sudo)\\s+|[A-Za-z_]\\w*=\\S*\\s+)*' +
  '(?:gsd_run|gsd-sdk|\\S*gsd-tools(?:\\.cjs)?|node\\s+\\S+)\\s+(?:query\\s+)?' +
  '(milestone[. ]complete|state[. ]milestone-switch|phases[. ]clear)(?=[\\s\'"`;)&|]|$)';

// Вызовы: { sub: 'milestone.complete'|'state.milestone-switch'|'phases.clear', rest, line };
// rest — строка от подкоманды. Упоминание слов в тексте (grep/printf/echo, JSON) вызовом не считается.
function gsdCalls(command) {
  const calls = [];
  for (const line of String(command).split('\n')) {
    const re = new RegExp(GSD_CALL_SRC, 'g');
    let m;
    while ((m = re.exec(line)) !== null) {
      const subStart = m.index + m[0].length - m[1].length;
      calls.push({ sub: m[1].replace(' ', '.'), rest: line.slice(subStart), line });
    }
  }
  return calls;
}

// Вызов закрытия вехи: milestone.complete, в строке есть --confirm и нет --dry-run.
function isClosing(call) {
  return (
    call.sub === 'milestone.complete' &&
    /--confirm(?![\w-])/.test(call.line) &&
    !/--dry-run(?![\w-])/.test(call.line)
  );
}

const LABEL_RES = {
  'milestone.complete': /milestone[. ]complete\s+["']?([^\s"']+)/,
  'state.milestone-switch': /state[. ]milestone-switch\b.*--milestone\s+["']?([^\s"']+)/,
  'phases.clear': /phases[. ]clear\b.*--archive-version\s+["']?([^\s"']+)/
};

// Метка вехи из вызова; если её не разобрать (нет аргумента, флаг, переменная, подстановка) —
// значение milestone из STATE.md; нет файла — пустая строка.
function callLabel(call, planning) {
  const m = LABEL_RES[call.sub].exec(call.rest);
  const label = m ? m[1] : '';
  if (label && !label.startsWith('-') && !/[$`]/.test(label)) return label;
  const fm = frontmatter(readFile(path.join(planning, 'STATE.md')));
  return fm.milestone || '';
}

function notVerified(reason) {
  return (
    `Сверка выпуска не выполнена, закрытие вехи отложено: ${String(reason).slice(0, 200)}; ` +
    'повторить, когда будет сеть, или проверить `node scripts/release-guard.js close-gate`.'
  );
}

// Ветка закрытия вехи: проверяет имя, STATE/ROADMAP и close-gate; null — всё прошло.
// Любая незавершённая сверка (срок, исключение, код 2) даёт отказ, а не пропуск.
function closeDeny(closing, planning) {
  try {
    setDeadline();
    const found = [];
    for (const c of closing) {
      const nm = /--name\s+(?:"([^"]*)"|'([^']*)'|(\S+))/.exec(c.line);
      const name = nm ? nm[1] || nm[2] || nm[3] || '' : '';
      if (name) found.push(...unreleasedIn(name, true).versions);
    }
    found.push(...planningFindings(planning, true));
    const uniq = Array.from(new Set(found));
    if (uniq.length) {
      return (
        `Закрытие вехи остановлено: в имени вехи или в STATE/ROADMAP невышедший номер версии ${uniq.join(', ')}. ` +
        'Уберите номер или добавьте строку version-ok: <версия> <причина>.'
      );
    }
    const gate = closeGate();
    if (gate.code === 1) return gate.lines.join(' ');
    if (gate.code === 2) return notVerified(gate.reason);
    return null;
  } catch (err) {
    return notVerified(err && err.message ? err.message : err);
  }
}

// Причина отказа для вызовов GSD (метки вехи, закрытие) или null.
function milestoneDeny(command, planning) {
  const calls = gsdCalls(command);
  if (!calls.length) return null;
  try {
    for (const c of calls) {
      const label = callLabel(c, planning);
      if (label && /^v?\d+\.\d+/.test(label)) {
        return 'Веха называется M<n> и именем по сути, без номера версии (D-02).';
      }
    }
  } catch {
    // проверка вида метки — не гейт закрытия: ошибка не блокирует
  }
  const closing = calls.filter(isClosing);
  if (!closing.length) return null;
  return closeDeny(closing, planning);
}

function preHook(argv) {
  let ev;
  try {
    ev = JSON.parse(readStdin());
  } catch {
    return 0;
  }
  if (!ev || ev.tool_name !== 'Bash') return 0;
  const command = String((ev.tool_input && ev.tool_input.command) || '');
  if (!command) return 0;
  const planning = flag(argv, '--planning') || defaultPlanning();

  try {
    const reason = milestoneDeny(command, planning);
    if (reason) {
      emitDeny(reason);
      return 0;
    }
  } catch {
    // closeDeny обрабатывает свои ошибки отказом; сюда попадают только прочие сбои
  }
  try {
    const reason = prDenyReason(command, ev);
    if (reason) emitDeny(reason);
  } catch {
    // внутренняя ошибка вне закрытия вехи не должна мешать работе
  }
  return 0;
}

// ---------------------------------------------------------------------------------------------
// Диапазон версий вехи (D-01) и выпуск хвоста (D-04)
// ---------------------------------------------------------------------------------------------

const RANGE_RE = /^\*\*Версии:\*\*\s*v?(\d+)\.(\d+)\.(\d+)\s*[–-]\s*v?(\d+)\.(\d+)\.(\d+)/;
const START_MARKER_RE = /\*\*Версии с:\*\*\s*v?(\d+)\.(\d+)\.(\d+)/;

function toCore(a, b, c) {
  return { major: Number(a), minor: Number(b), patch: Number(c) };
}

// Границы записи вехи в MILESTONES.md: [заголовок, начало следующей записи) или null.
function entryBounds(lines, label) {
  const h = lines.findIndex(l => l === `## ${label}` || l.startsWith(`## ${label} `));
  if (h < 0) return null;
  let end = lines.length;
  for (let i = h + 1; i < lines.length; i++) {
    if (/^## /.test(lines[i])) {
      end = i;
      break;
    }
  }
  return { h, end };
}

// Конец предыдущего диапазона (записи идут от новых к старым) или null.
function previousRangeEnd(planning, label) {
  const text = readFile(path.join(planning, 'MILESTONES.md'));
  if (!text) return null;
  const lines = text.split('\n');
  const b = entryBounds(lines, label);
  for (let i = b ? b.end : 0; i < lines.length; i++) {
    const m = RANGE_RE.exec(lines[i]);
    if (m) return toCore(m[4], m[5], m[6]);
  }
  return null;
}

function startMarker(planning, label) {
  for (const f of [path.join(planning, 'milestones', `${label}-ROADMAP.md`), path.join(planning, 'ROADMAP.md')]) {
    const m = START_MARKER_RE.exec(readFile(f) || '');
    if (m) return toCore(m[1], m[2], m[3]);
  }
  return null;
}

// Строка `**Версии:** vA–vB` (U+2013) и предупреждение (или '') для вехи; tail — результат findTail().
function rangeLine(label, planning, tail) {
  const end = rv.parseTag(tail.tag);
  const prevEnd = previousRangeEnd(planning, label);
  let start = null;
  if (prevEnd) {
    for (const name of tail.tags) {
      const p = rv.parseTag(name);
      if (rv.coreCmp(p, prevEnd) > 0 && (!start || rv.coreCmp(p, start) < 0)) start = toCore(p.major, p.minor, p.patch);
    }
  }
  if (!start) start = startMarker(planning, label);
  if (!start) {
    return {
      line: `**Версии:** ?–${rv.formatCore(end)}`,
      warn: `начало диапазона вехи ${label} не найдено (нет предыдущей строки «Версии:» и маркера «Версии с:»)`
    };
  }
  if (rv.coreCmp(start, end) > 0) return { line: '**Версии:** новых версий не вышло', warn: '' };
  return { line: `**Версии:** ${rv.formatCore(start)}–${rv.formatCore(end)}`, warn: '' };
}

function cmdRange(argv) {
  const label = flag(argv, '--label');
  if (!label) {
    process.stderr.write('range: нужен --label <метка>\n');
    return 2;
  }
  const planning = flag(argv, '--planning') || defaultPlanning();
  const tail = findTail();
  if (!tail) {
    process.stderr.write('range: в origin/main нет тегов\n');
    return 2;
  }
  const r = rangeLine(label, planning, tail);
  if (r.warn) process.stderr.write(`range: ${r.warn}\n`);
  process.stdout.write(`${r.line}\n`);
  return 0;
}

// Вписывает строку диапазона первым абзацем записи вехи (есть `**Версии:**` — заменяет).
// Возвращает '' при успехе или причину.
function writeRange(file, label, line) {
  const text = readFile(file);
  if (text === null) return 'нет MILESTONES.md';
  const lines = text.split('\n');
  const b = entryBounds(lines, label);
  if (!b) return `в MILESTONES.md нет записи вехи ${label}`;
  let at = -1;
  for (let i = b.h + 1; i < b.end; i++) {
    if (/^\*\*Версии:\*\*/.test(lines[i])) {
      at = i;
      break;
    }
  }
  if (at >= 0) {
    lines[at] = line;
  } else {
    lines.splice(b.h + 1, 0, '', line);
    if (lines[b.h + 3] !== undefined && lines[b.h + 3].trim() !== '') lines.splice(b.h + 3, 0, '');
  }
  const tmp = `${file}.tmp-${process.pid}`;
  fs.writeFileSync(tmp, lines.join('\n'));
  fs.renameSync(tmp, file);
  return '';
}

// Метка вехи по самому свежему архиву M<n>-ROADMAP.md не старше 10 мин.
function freshArchiveLabel(planning) {
  let best = null;
  let dir;
  try {
    dir = fs.readdirSync(path.join(planning, 'milestones'));
  } catch {
    return '';
  }
  for (const name of dir) {
    const m = /^(M\d+)-ROADMAP\.md$/.exec(name);
    if (!m) continue;
    const mtime = fs.statSync(path.join(planning, 'milestones', name)).mtimeMs;
    if (Date.now() - mtime > 10 * 60 * 1000) continue;
    if (!best || mtime > best.mtime) best = { label: m[1], mtime };
  }
  return best ? best.label : '';
}

// Что делать после успешного закрытия вехи: диапазон в MILESTONES.md и выпуск хвоста.
// Возвращает текст для additionalContext или '' (архива нет — закрытие не состоялось).
function afterClose(call, planning) {
  const m = /milestone[. ]complete\s+["']?(M\d+)/.exec(call.rest);
  const label = m ? m[1] : freshArchiveLabel(planning);
  if (!label) return 'Метку вехи не удалось определить, диапазон версий не записан.';
  if (!fs.existsSync(path.join(planning, 'milestones', `${label}-ROADMAP.md`))) return '';

  const parts = [];
  let tail;
  let range;
  try {
    tail = findTail();
    if (!tail) throw new Error('в origin/main нет тегов');
    range = rangeLine(label, planning, tail);
  } catch (err) {
    return `Веха ${label} закрыта, но диапазон версий не записан и хвост не выпущен: ${err.message}.`;
  }
  const wrote = writeRange(path.join(planning, 'MILESTONES.md'), label, range.line);
  parts.push(
    wrote
      ? `Веха ${label} закрыта, диапазон версий не записан: ${wrote}`
      : `Веха ${label} закрыта: ${range.line}`
  );
  if (range.warn) parts.push(range.warn);

  const parsed = rv.parseTag(tail.tag);
  if (!parsed.pre) {
    parts.push(`хвост уже stable ${tail.tag}`);
  } else {
    const base = rv.formatCore(parsed);
    if (process.env.HOOK_DRY_RUN === '1') {
      parts.push(`DRY_RUN: запустил бы auto-release.yml promote_base=${base}`);
    } else {
      try {
        gh(['workflow', 'run', 'auto-release.yml', '-f', `promote_base=${base}`, '-f', 'dry_run=false'], 20000);
        parts.push(`автовыпуск продвигает ${tail.tag} в stable без выдержки`);
      } catch (err) {
        parts.push(`запуск автовыпуска не удался (${err.message}); запустить вручную: promote_base=${base}`);
      }
    }
  }
  return parts.join('; ');
}

function emitContext(eventName, text) {
  process.stdout.write(
    JSON.stringify({ hookSpecificOutput: { hookEventName: eventName, additionalContext: text } })
  );
}

// PR после gh pr create|edit: невышедший номер в заголовке или теле.
function prContext(command) {
  if (!PR_CMD_RE.test(command)) return '';
  const nm = /\bgh\s+pr\s+edit\s+(\d+)(?=\s|$)/.exec(command);
  const args = ['pr', 'view'];
  if (nm) args.push(nm[1]);
  args.push('--json', 'number,title,body');
  const pr = JSON.parse(gh(args, 6000));
  const { versions } = unreleasedIn(`${pr.title || ''}\n${pr.body || ''}`, false);
  if (!versions.length) return '';
  return (
    `PR #${pr.number} содержит невышедший номер ${versions.join(', ')} — исправить: ` +
    `gh pr edit ${pr.number} --title <заголовок> --body-file <файл> (или добавить строку version-ok: <версия> <причина>)`
  );
}

// ---------------------------------------------------------------------------------------------
// Сводка выпуска: SessionStart, /gsd-progress, /gsd-next (D-18, D-19, D-35)
// ---------------------------------------------------------------------------------------------

const STATUS_TOTAL_MS = 7000;
const STATUS_CALL_MS = 6000;
const STATUS_EVENTS = ['SessionStart', 'UserPromptSubmit', 'PostToolUse'];
const PROGRESS_PROMPT_RE = /^\s*\/gsd[-:](progress|next)\b/;
const PROGRESS_SKILL_RE = /^gsd[-:](progress|next)$/;
const children = new Set();

// Асинхронный gh с тайм-аутом; ошибка — { ok: false }.
function ghAsync(args) {
  return new Promise(resolve => {
    const child = execFile(
      'gh',
      args,
      { cwd: root(), encoding: 'utf8', timeout: STATUS_CALL_MS, maxBuffer: 8 * 1024 * 1024, env: execOpts(0).env },
      (err, stdout) => {
        children.delete(child);
        resolve(err ? { ok: false, out: '' } : { ok: true, out: stdout });
      }
    );
    children.add(child);
    if (child.stdin) child.stdin.end();
  });
}

function killChildren() {
  for (const c of children) {
    try {
      c.kill();
    } catch {
      // уже завершён
    }
  }
  children.clear();
}

function parseJson(r) {
  try {
    return JSON.parse(r.out);
  } catch {
    return null;
  }
}

// Сетевые строки сводки или null, если GitHub недоступен или ответ не разобран.
async function collectNet() {
  const [rel, run, stop, issues] = await Promise.all([
    ghAsync(['release', 'list', '--exclude-drafts', '--limit', '30', '--json', 'tagName,isPrerelease,publishedAt']),
    // несколько завершённых запусков: большинство запусков от status и issues пропущены if
    ghAsync(['run', 'list', '--workflow', 'auto-release.yml', '--branch', 'main', '--status', 'completed',
      '--limit', '20', '--json', 'conclusion,url']),
    ghAsync(['variable', 'get', 'RELEASE_STOP']),
    ghAsync(['api', 'repos/{owner}/{repo}/issues?state=open&labels=release-stop&per_page=5'])
  ]);
  const releases = rel.ok ? parseJson(rel) : null;
  const runs = run.ok ? parseJson(run) : null;
  const issueList = issues.ok ? parseJson(issues) : null;
  if (!Array.isArray(releases) || !Array.isArray(runs) || !Array.isArray(issueList)) return null;

  const lines = [];
  const names = releases.map(r => r.tagName).filter(n => TAG_NAME_RE.test(n));
  const stableNames = releases.filter(r => !r.isPrerelease).map(r => r.tagName).filter(n => TAG_NAME_RE.test(n));
  const lastStable = latestTag(stableNames);
  const base = lastStable ? rv.parseTag(lastStable) : null;
  // Новейший пре-релиз каждой базы, которой ещё нет в stable.
  const newest = new Map();
  for (const n of names) {
    const p = rv.parseTag(n);
    if (!p.pre || (base && rv.coreCmp(p, base) <= 0)) continue;
    const key = rv.formatCore(p);
    if (!newest.has(key) || tagCmp(n, newest.get(key)) > 0) newest.set(key, n);
  }
  const tags = Array.from(newest.values()).sort(tagCmp).slice(-3);
  const statuses = await Promise.all(
    tags.map(t => ghAsync(['api', `repos/{owner}/{repo}/commits/${t}/statuses?per_page=100`]))
  );
  for (let i = 0; i < tags.length; i++) {
    const list = statuses[i].ok ? parseJson(statuses[i]) : null;
    if (!Array.isArray(list)) return null;
    const seen = list.some(s => s && (s.context === 'stand/arm64-smoke' || s.context === 'stand/arm64-full'));
    if (!seen) lines.push(`Пре-релиз ${tags[i]} ждёт смоука: make router-test SUITE=smoke RELEASE=${tags[i]}`);
  }

  // последний запуск со значимым исходом: пропущенные и отменённые красный не перекрывают
  const lastRun = runs.find(r => r && r.conclusion && r.conclusion !== 'skipped' && r.conclusion !== 'cancelled');
  if (lastRun && lastRun.conclusion === 'failure') {
    lines.push(`Auto Release красный: ${String(lastRun.url || '')}`);
  }
  const stopValue = stop.ok ? stop.out.trim() : '';
  if (stopValue && stopValue !== '0' && stopValue.toLowerCase() !== 'false') {
    lines.push('Стоп выпуска включён: переменная RELEASE_STOP');
  }
  for (const it of issueList) {
    if (it && Number.isInteger(it.number) && !it.pull_request) {
      // автор метки здесь не проверяется (автовыпуск засчитывает только метку от пользователя с
      // правом записи), поэтому строка нейтральная
      lines.push(
        `Открыт issue #${it.number} с меткой release-stop: стоп, если метку поставил пользователь с правом записи (проверить, кто её поставил)`
      );
      break;
    }
  }
  return lines;
}

// Строки сводки: только по делу. Общий срок STATUS_TOTAL_MS на все вызовы gh и git.
async function collectStatus(planning) {
  deadlineAt = T0 + STATUS_TOTAL_MS;
  const netP = collectNet().catch(() => null);
  const lines = [];
  // сверка STATE/ROADMAP идёт, пока gh отвечает, и работает без сети
  let planLine = '';
  try {
    const found = planningFindings(planning, false);
    if (found.length) planLine = `В STATE/ROADMAP невышедший номер: ${found.join(', ')}`;
  } catch {
    // нет git или тегов — строки не будет
  }
  let timer;
  const left = Math.max(0, deadlineAt - Date.now());
  const net = await Promise.race([
    netP,
    new Promise(resolve => {
      timer = setTimeout(() => resolve(null), left);
    })
  ]);
  clearTimeout(timer);
  killChildren();
  if (net === null) lines.push('Сверка выпуска не выполнена (нет сети или GitHub API)');
  else lines.push(...net);
  if (planLine) lines.push(planLine);
  return lines;
}

function statusText(lines, progress) {
  return `${progress ? 'Добавьте в итог строкой: ' : ''}Сверка выпуска: ${lines.join('\n')}`;
}

async function cmdStatus(argv) {
  const event = flag(argv, '--event');
  if (!STATUS_EVENTS.includes(event)) {
    process.stderr.write('status: нужен --event SessionStart|UserPromptSubmit|PostToolUse\n');
    return 2;
  }
  const plain = argv.includes('--plain');
  try {
    let progress = event !== 'SessionStart';
    if (!plain) {
      let ev = {};
      const raw = readStdin();
      if (raw.trim()) {
        try {
          ev = JSON.parse(raw) || {};
        } catch {
          ev = {};
        }
      }
      if (event === 'UserPromptSubmit' && !PROGRESS_PROMPT_RE.test(String(ev.prompt || ''))) return 0;
    }
    if (plain) progress = false;
    const lines = await collectStatus(flag(argv, '--planning') || defaultPlanning());
    if (!lines.length) return 0;
    if (plain) process.stdout.write(`${lines.join('\n')}\n`);
    else emitContext(event, statusText(lines, progress));
  } catch {
    // сводка — подсказка: сбой не мешает работе
  }
  return 0;
}

async function postHook(argv) {
  let ev;
  try {
    ev = JSON.parse(readStdin());
  } catch {
    return 0;
  }
  if (!ev) return 0;
  const planning = flag(argv, '--planning') || defaultPlanning();
  const contexts = [];
  try {
    if (ev.tool_name === 'Bash') {
      const command = String((ev.tool_input && ev.tool_input.command) || '');
      const failed = ev.tool_response && (ev.tool_response.interrupted === true || ev.tool_response.is_error === true);
      const closing = failed ? [] : gsdCalls(command).filter(isClosing);
      if (closing.length) {
        const text = afterClose(closing[0], planning);
        if (text) contexts.push(text);
      }
      try {
        const text = prContext(command);
        if (text) contexts.push(text);
      } catch {
        // PR не прочитан — без вывода
      }
    }
    if (ev.tool_name === 'Skill') {
      const skill = String((ev.tool_input && ev.tool_input.skill) || '');
      if (PROGRESS_SKILL_RE.test(skill)) {
        const lines = await collectStatus(planning);
        if (lines.length) contexts.push(statusText(lines, true));
      }
    }
  } catch {
    return 0;
  }
  if (contexts.length) emitContext('PostToolUse', contexts.join('\n'));
  return 0;
}

// ---------------------------------------------------------------------------------------------
// Точка входа
// ---------------------------------------------------------------------------------------------

async function main() {
  const [cmd, ...argv] = process.argv.slice(2);
  switch (cmd) {
    case 'scan':
      return cmdScan(argv);
    case 'planning-check':
      return cmdPlanningCheck(argv);
    case 'pre-hook':
      return preHook(argv);
    case 'close-gate':
      return cmdCloseGate();
    case 'range':
      return cmdRange(argv);
    case 'post-hook':
      return postHook(argv);
    case 'status':
      return cmdStatus(argv);
    default:
      process.stderr.write(
        'Usage: node scripts/release-guard.js <scan|planning-check|pre-hook|close-gate|range|post-hook|status> [аргументы]\n'
      );
      return 2;
  }
}

main().then(
  code => {
    process.exitCode = code;
  },
  err => {
    const hook = /-hook$/.test(process.argv[2] || '');
    if (!hook) process.stderr.write(`release-guard: ${err && err.message ? err.message : err}\n`);
    process.exitCode = hook ? 0 : 2;
  }
);
