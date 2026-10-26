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
 *   pre-hook [--planning <д>]  событие PreToolUse со stdin: deny на `gh pr create|edit` с невышедшим
 *                              номером; штатно всегда код 0 (с deny и без вывода)
 *
 * Невышедший номер — ядро строго больше наибольшего среди всех тегов `v*` (см. lib/release-versions.js);
 * при находке по локальным тегам номер перепроверяется по `git ls-remote --tags origin`, чтобы
 * свежий релиз не считался невышедшим.
 *
 * Usage: node scripts/release-guard.js <подкоманда> [аргументы]
 */
'use strict';

const { execFileSync } = require('child_process');
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

function git(args, ownTimeout) {
  try {
    return execFileSync('git', args, execOpts(ownTimeout));
  } catch (err) {
    throw wrapError('git', args, err);
  }
}

function gh(args, ownTimeout) {
  try {
    return execFileSync('gh', args, execOpts(ownTimeout));
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
      const st = fs.statSync(full);
      if (!st.isFile() || st.size > MAX_BODY_BYTES) continue;
      texts.push(fs.readFileSync(full, 'utf8'));
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
  const planning = flag(argv, '--planning') || path.join(root(), '.planning');
  void planning;

  try {
    const reason = prDenyReason(command, ev);
    if (reason) emitDeny(reason);
  } catch {
    // внутренняя ошибка вне закрытия вехи не должна мешать работе
  }
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
    case 'pre-hook':
      return preHook(argv);
    default:
      process.stderr.write(
        'Usage: node scripts/release-guard.js <scan|pre-hook> [аргументы]\n'
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
