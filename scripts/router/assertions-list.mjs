#!/usr/bin/env node
// Список утверждений роутерного теста на базе и на HEAD (148.1.2, D-37; D-05 фазы 148.1).
//
// Разовый помощник ревью переделки тестов: выписывает утверждения названных тестов из
// исходников (без запуска) на ссылке git `<ref>` (`git show <ref>:<файл>`) и в рабочем дереве
// (HEAD с незакоммиченными правками), чтобы план имел список «до», а ревью сверяло «до» и
// «после» по смыслу. Стражем CI не является и в scripts/check-*.js, Makefile и workflows не
// подключается: сравнение числа утверждений ничего не доказывает, сверка идёт глазами.
//
// Вызов: node scripts/router/assertions-list.mjs [--json] <ref> <проверка>…
//   проверка — go:TestRouter<Имя> или pw:<спек от frontend/>[::<подстрока названия>]
//
// Go: файл с `func TestRouter<Имя>(` среди отслеживаемых *_test.go с первой строкой
// `//go:build router`; объявления (core-marks.cjs: declarations) всех роутерных файлов того же
// каталога. Просматриваются тело теста и тела помощников пакета, которые тело вызывает по имени
// напрямую (первый уровень: функции — вызовом `имя(`, методы — `x.имя(`). В них ищутся вызовы
//   fatal   — .Fatal( .Fatalf(
//   error   — .Error( .Errorf( (без `fmt.Errorf` и `err.Error()` без аргументов)
//   verdict — routertest.Verdict(
//   helper  — функции и методы с Expect, Require, Verdict или Must в имени (кроме Must* стандартной
//             библиотеки: regexp.MustCompile, template.Must и т. п.)
// Текст — первый строковый литерал аргументов, иначе имя вызова; строки помощника помечены
// «в <помощник>:».
// Спеки: test(...) с названием, содержащим подстроку (без неё — все тесты спека); в теле теста —
// expect(, expect.poll(, expect.soft(, expect.fail(. Текст — сообщение (второй аргумент, если
// литерал; у expect.poll — поле message) и цепочка сопоставителя (`.not.toHaveCount`); без
// сообщения — выражение-субъект (до 80 знаков).
// Во всех текстах IP-адреса и URL заменяются на `<адрес>`.
//
// Вывод TSV: сторона (base|head), проверка, №, вид, файл:строка, текст (табуляции и переводы
// строк — пробелы); затем по проверке строка-итог `# <проверка>: до N, после M`. Тест не найден
// на стороне — строка `# <проверка>: <сторона> — нет`, а в итоге вместо числа «—» (удалённый тест
// — нормальный случай: «после» пусто). --json — тот же состав одним объектом.
//
// Коды: 0 — списки напечатаны; 1 — тест или файл не найден хотя бы на одной стороне;
// 2 — неверный вызов (нет аргументов, ссылка не разрешается в коммит, проверка не по шаблону).

import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const { declarations, lex } = require('./core-marks.cjs');

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const GO_CHECK = /^go:(TestRouter[A-Za-z0-9_]+)$/;
const PW_CHECK = /^pw:(tests\/router\/[A-Za-z0-9_.-]+\.spec\.ts)(?:::(.+))?$/s;
const ASSERT_NAME = /Expect|Require|Verdict|Must/;
// Пакеты стандартной библиотеки с Must*/Error*, которые не утверждения теста.
const STD_QUALIFIERS = new Set(['fmt', 'errors', 'regexp', 'template', 'netip', 'url', 'json', 'strconv', 'time']);

function usage(msg) {
  if (msg) console.error(`assertions-list: ${msg}`);
  console.error('Вызов: node scripts/router/assertions-list.mjs [--json] <ref> <go:TestRouter<Имя>|pw:<спек>[::<подстрока>]>…');
  process.exit(2);
}

function git(args, opts = {}) {
  return execFileSync('git', args, { cwd: ROOT, encoding: 'utf8', maxBuffer: 256 * 1024 * 1024, ...opts });
}

/** git с допустимым кодом 1 (git grep без совпадений): stdout или ''. */
function gitMaybe(args) {
  try {
    return git(args, { stdio: ['ignore', 'pipe', 'pipe'] });
  } catch (e) {
    if (e.status === 1) return '';
    throw e;
  }
}

/** Источник файлов одной стороны: base — коммит, head — рабочее дерево (отслеживаемые файлы). */
function source(side, sha) {
  if (side === 'base') {
    return {
      grep: (pattern, pathspec) =>
        gitMaybe(['grep', '-l', '-E', '-e', pattern, sha, '--', pathspec])
          .split('\n')
          .filter(Boolean)
          .map((l) => l.slice(sha.length + 1)),
      list: (dir) =>
        gitMaybe(['ls-tree', '--name-only', sha, '--', dir + '/'])
          .split('\n')
          .filter(Boolean),
      read: (file) => {
        try {
          return git(['show', `${sha}:${file}`], { stdio: ['ignore', 'pipe', 'ignore'] });
        } catch {
          return null;
        }
      }
    };
  }
  return {
    grep: (pattern, pathspec) => gitMaybe(['grep', '-l', '-E', '-e', pattern, '--', pathspec]).split('\n').filter(Boolean),
    list: (dir) =>
      gitMaybe(['ls-files', '--', dir + '/'])
        .split('\n')
        .filter((f) => f && path.posix.dirname(f) === dir),
    read: (file) => {
      if (!gitMaybe(['ls-files', '--', file]).trim()) return null;
      const abs = path.join(ROOT, file);
      return fs.existsSync(abs) ? fs.readFileSync(abs, 'utf8') : null;
    }
  };
}

const isRouterGo = (text) => text !== null && text.split('\n', 1)[0].trim() === '//go:build router';

/** Номер строки (с 1) для смещения в тексте. */
function lineAt(text, offset) {
  let n = 1;
  for (let i = 0; i < offset && i < text.length; i++) if (text.charCodeAt(i) === 10) n++;
  return n;
}

/** Убирает адреса и URL, табуляции и переводы строк. */
function clean(s) {
  return s
    .replace(/[a-z][a-z0-9+.-]*:\/\/[^\s'"`]+/gi, '<адрес>')
    .replace(/\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b/g, '<адрес>')
    .replace(/\b(?:[0-9a-f]{1,4}:){3,7}[0-9a-f]{1,4}\b/gi, '<адрес>')
    .replace(/[\t\r\n]+/g, ' ')
    .replace(/\s{2,}/g, ' ')
    .trim();
}

// ---- разбор аргументов вызова (как splitArgs/literalValue в core-marks.cjs; там не экспортированы) ----

/** Слеш в позиции i начинает литерал регулярного выражения (только для TS). */
function isRegexStart(code, i) {
  if (code[i + 1] === '/' || code[i + 1] === '*') return false;
  let k = i - 1;
  while (k >= 0 && /\s/.test(code[k])) k--;
  if (k < 0 || '(,=:[!&|?{};<>+-*%^~'.includes(code[k])) return true;
  const word = /([A-Za-z_$][\w$]*)$/.exec(code.slice(Math.max(0, k - 12), k + 1));
  return word !== null && /^(?:return|typeof|case|in|of|else|do|void|throw|delete|new|await|yield)$/.test(word[1]);
}

function regexEnd(code, i) {
  const n = code.length;
  let j = i + 1;
  let cls = false;
  while (j < n && code[j] !== '\n' && (cls || code[j] !== '/')) {
    if (code[j] === '\\') j++;
    else if (code[j] === '[') cls = true;
    else if (code[j] === ']') cls = false;
    j++;
  }
  return j;
}

/** Аргументы вызова от открывающей скобки open: { args: [{text, start}], end } или null. */
function splitArgs(code, open, ts) {
  const list = [];
  let depth = 0;
  let start = open + 1;
  const n = code.length;
  for (let i = open; i < n; i++) {
    const c = code[i];
    if (c === '"' || c === "'" || c === '`') {
      let j = i + 1;
      while (j < n && code[j] !== c && (c === '`' || code[j] !== '\n')) j += code[j] === '\\' ? 2 : 1;
      i = j;
    } else if (ts && c === '/' && isRegexStart(code, i)) {
      i = regexEnd(code, i);
    } else if (c === '(' || c === '[' || c === '{') {
      depth++;
    } else if (c === ')' || c === ']' || c === '}') {
      depth--;
      if (depth === 0) {
        const last = code.slice(start, i);
        if (last.trim() || list.length > 0) list.push({ text: last.trim(), start });
        return { args: list, end: i };
      }
    } else if (c === ',' && depth === 1) {
      list.push({ text: code.slice(start, i).trim(), start });
      start = i + 1;
    }
  }
  return null;
}

/** Строковый литерал без подстановок: значение или null. */
function literalValue(s) {
  const m = /^(['"`])((?:[^\\\n]|\\.)*)\1$/s.exec(s.trim());
  if (!m || (m[1] === '`' && m[2].includes('${'))) return null;
  return m[2].replace(/\\(.)/g, '$1');
}

/** Шаблонная строка с подстановками: сырое содержимое или null. */
function templateValue(s) {
  const m = /^`((?:[^`\\]|\\.)*)`$/s.exec(s.trim());
  return m ? m[1] : null;
}

// ---- Go ----

/** Объявления файла со смещениями начала в исходнике. */
function declsWithOffsets(text) {
  const decls = declarations(text);
  const total = lex(text).code.length;
  let end = total;
  for (let k = decls.length - 1; k >= 0; k--) {
    decls[k].start = end - decls[k].code.length;
    end = decls[k].start;
  }
  return decls;
}

/** Вызовы в теле объявления: [{name, qualifier, open, at}] (at — начало имени, смещение в теле). */
function goCalls(masked) {
  const out = [];
  // Заголовок объявления (имя, получатель, параметры) — не вызовы: тело начинается с «{» вне скобок.
  let body = 0;
  for (let depth = 0; body < masked.length; body++) {
    const ch = masked[body];
    if (ch === '(' || ch === '[') depth++;
    else if (ch === ')' || ch === ']') depth--;
    else if (ch === '{' && depth === 0) break;
  }
  const re = /(?:([A-Za-z_]\w*)\s*\.\s*|(?<!\w))([A-Za-z_]\w*)\s*\(/g;
  for (const m of masked.matchAll(re)) {
    if (m.index < body) continue;
    // «x.y.name(» — квалификатор ближайший; «).name(» и «].name(» — метод без имени получателя
    const qualifier = m[1] !== undefined ? m[1] : null;
    let method = qualifier !== null;
    if (!method && m.index > 0) {
      let k = m.index - 1;
      while (k >= 0 && /\s/.test(masked[k])) k--;
      if (masked[k] === '.') method = true;
    }
    if (['func', 'if', 'for', 'switch', 'return', 'go', 'defer', 'select', 'range'].includes(m[2])) continue;
    const open = m.index + m[0].length - 1;
    out.push({ name: m[2], qualifier, method, open, at: m.index + m[0].lastIndexOf(m[2]) });
  }
  return out;
}

/** Утверждения в одном теле: [{kind, offset, text}] (offset — от начала исходника). */
function goAssertions(decl, fileText) {
  const rows = [];
  for (const c of goCalls(decl.masked)) {
    let kind = null;
    if (c.method && (c.name === 'Fatal' || c.name === 'Fatalf') && c.qualifier !== 'log') kind = 'fatal';
    else if (c.method && (c.name === 'Error' || c.name === 'Errorf') && !STD_QUALIFIERS.has(c.qualifier)) kind = 'error';
    else if (c.qualifier === 'routertest' && c.name === 'Verdict') kind = 'verdict';
    else if (ASSERT_NAME.test(c.name) && !STD_QUALIFIERS.has(c.qualifier) && !/^Must(?:Compile|Parse)/.test(c.name))
      kind = 'helper';
    if (!kind) continue;
    const parsed = splitArgs(decl.code, c.open, false);
    if (kind === 'error' && (!parsed || parsed.args.length === 0)) continue; // err.Error()
    let text = null;
    for (const a of parsed ? parsed.args : []) {
      const v = literalValue(a.text);
      if (v !== null) {
        text = v;
        break;
      }
    }
    const callName = (c.qualifier ? c.qualifier + '.' : '') + c.name;
    rows.push({ kind, offset: decl.start + c.at, text: text ?? callName });
  }
  return rows;
}

/** Имена импортированных пакетов файла (псевдоним или последний элемент пути). */
function goImports(text) {
  const names = new Set();
  const block = /^import\s*(?:\(([\s\S]*?)^\)|(.+)$)/gm;
  for (const m of text.matchAll(block)) {
    for (const line of (m[1] ?? m[2]).split('\n')) {
      const im = /^\s*([A-Za-z_]\w*|\.)?\s*"([^"]+)"/.exec(line);
      if (im) names.add(im[1] && im[1] !== '.' ? im[1] : im[2].split('/').pop());
    }
  }
  return names;
}

/** Утверждения Go-теста на одной стороне: { found, file, rows } . */
function goSide(src, test) {
  const files = src.grep(`func ${test}\\(`, '*_test.go').filter((f) => isRouterGo(src.read(f)));
  if (files.length === 0) return { found: false, file: null, rows: [] };
  const file = files[0];
  const dir = path.posix.dirname(file);
  const pkg = [];
  for (const f of src.list(dir)) {
    if (!f.endsWith('_test.go')) continue;
    const text = src.read(f);
    if (!isRouterGo(text)) continue;
    for (const d of declsWithOffsets(text)) if (d.kind === 'func') pkg.push({ file: f, text, decl: d });
  }
  const own = pkg.find((p) => p.file === file && !p.decl.receiver && p.decl.name === test);
  if (!own) return { found: false, file, rows: [] };
  const rows = [];
  const push = (p, list, helper) => {
    for (const r of list) {
      rows.push({
        kind: r.kind,
        file: p.file,
        line: lineAt(p.text, r.offset),
        text: helper ? `в ${helper}: ${r.text}` : r.text
      });
    }
  };
  push(own, goAssertions(own.decl, own.text), null);
  const seen = new Set();
  const imports = goImports(own.text);
  for (const c of goCalls(own.decl.masked)) {
    if (c.qualifier && imports.has(c.qualifier)) continue; // вызов другого пакета: routertest.X, http.Get
    const targets = pkg.filter((p) => p.decl.name === c.name && p.decl.receiver === c.method && p.decl.name !== test);
    for (const p of targets) {
      const key = p.file + ':' + p.decl.start;
      if (seen.has(key)) continue;
      seen.add(key);
      push(p, goAssertions(p.decl, p.text), p.decl.name);
    }
  }
  return { found: true, file, rows };
}

// ---- Playwright ----

/** Комментарии TS заменены пробелами (длина и переводы строк сохраняются). */
function blankTs(text) {
  let out = '';
  const n = text.length;
  let i = 0;
  const blank = (s) => s.replace(/[^\n]/g, ' ');
  while (i < n) {
    const c = text[i];
    const next = text[i + 1];
    if (c === '/' && isRegexStart(text, i)) {
      const j = Math.min(regexEnd(text, i) + 1, n);
      out += text.slice(i, j);
      i = j;
    } else if (c === '/' && next === '/') {
      let j = i;
      while (j < n && text[j] !== '\n') j++;
      out += blank(text.slice(i, j));
      i = j;
    } else if (c === '/' && next === '*') {
      const end = text.indexOf('*/', i + 2);
      const j = end < 0 ? n : end + 2;
      out += blank(text.slice(i, j));
      i = j;
    } else if (c === '"' || c === "'" || c === '`') {
      let j = i + 1;
      while (j < n && text[j] !== c && (c === '`' || text[j] !== '\n')) j += text[j] === '\\' ? 2 : 1;
      j = Math.min(j + 1, n);
      out += text.slice(i, j);
      i = j;
    } else {
      out += c;
      i++;
    }
  }
  return out;
}

/** Утверждения в отрезке [from, to) кода спека: [{kind, offset, text}]. */
function specAssertions(code, from, to) {
  const rows = [];
  const re = /(?<![\w.$])expect(?:\s*\.\s*(poll|soft|fail))?\s*\(/g;
  re.lastIndex = from;
  let m;
  while ((m = re.exec(code)) && m.index < to) {
    const variant = m[1] || '';
    const open = m.index + m[0].length - 1;
    const parsed = splitArgs(code, open, true);
    if (!parsed) continue;
    const args = parsed.args;
    let msg = null;
    if (variant === 'fail') msg = args[0] ? literalValue(args[0].text) : null;
    else if (variant === 'poll') {
      const opt = args[1] ? /\bmessage\s*:\s*(['"`](?:[^\\\n]|\\.)*?['"`])/s.exec(args[1].text) : null;
      msg = opt ? literalValue(opt[1]) : null;
    } else msg = args[1] ? literalValue(args[1].text) ?? templateValue(args[1].text) : null;
    const chain = [];
    const tail = /^\s*((?:\.\s*(?:not|resolves|rejects)\s*)*)\.\s*([A-Za-z_]\w*)\s*\(/.exec(code.slice(parsed.end + 1, parsed.end + 400));
    if (tail) {
      for (const mod of tail[1].matchAll(/(not|resolves|rejects)/g)) chain.push(mod[1]);
      chain.push(tail[2]);
    }
    const parts = [];
    if (variant) parts.push(`[${variant}]`);
    if (msg !== null) parts.push(`«${msg}»`);
    else if (variant !== 'fail' && args[0]) {
      const subj = args[0].text.replace(/\s+/g, ' ');
      parts.push(subj.length > 80 ? subj.slice(0, 77) + '…' : subj);
    }
    if (chain.length) parts.push('→ ' + chain.join('.'));
    rows.push({ kind: 'expect', offset: m.index, text: parts.join(' ') });
    re.lastIndex = open + 1;
  }
  return rows;
}

/** Утверждения тестов спека на одной стороне: { found, file, rows }. */
function specSide(src, spec, sub) {
  const file = 'frontend/' + spec;
  const text = src.read(file);
  if (text === null) return { found: false, file: null, rows: [] };
  const code = blankTs(text);
  const rows = [];
  let found = false;
  for (const m of code.matchAll(/(?<![\w.$])test\s*\(/g)) {
    const open = m.index + m[0].length - 1;
    const parsed = splitArgs(code, open, true);
    if (!parsed || parsed.args.length < 2) continue;
    const title = literalValue(parsed.args[0].text) ?? templateValue(parsed.args[0].text);
    if (title === null || (sub !== undefined && !title.includes(sub))) continue;
    found = true;
    const body = parsed.args[parsed.args.length - 1];
    for (const r of specAssertions(code, body.start, parsed.end)) {
      rows.push({
        kind: r.kind,
        file,
        line: lineAt(text, r.offset),
        text: sub !== undefined ? r.text : `${title}: ${r.text}`
      });
    }
  }
  return { found, file: found ? file : null, rows };
}

// ---- main ----

const argv = process.argv.slice(2);
const json = argv.includes('--json');
const rest = argv.filter((a) => a !== '--json');
if (rest.length < 2) usage(rest.length === 0 ? null : 'нужна хотя бы одна проверка');
const [ref, ...checks] = rest;
let sha;
try {
  sha = git(['rev-parse', '--verify', '--quiet', `${ref}^{commit}`], { stdio: ['ignore', 'pipe', 'ignore'] }).trim();
} catch {
  usage(`ссылка ${ref} не разрешается в коммит`);
}
const parsedChecks = checks.map((c) => {
  const g = GO_CHECK.exec(c);
  if (g) return { check: c, go: g[1] };
  const p = PW_CHECK.exec(c);
  if (p) return { check: c, spec: p[1], sub: p[2] };
  usage(`проверка «${c}» не по шаблону go:TestRouter<Имя> или pw:tests/router/<спек>.spec.ts[::<подстрока>]`);
  return null;
});

const sides = { base: source('base', sha), head: source('head', sha) };
const result = { ref, base: sha, checks: [] };
let missing = false;
for (const pc of parsedChecks) {
  const entry = { check: pc.check };
  for (const side of ['base', 'head']) {
    const s = pc.go ? goSide(sides[side], pc.go) : specSide(sides[side], pc.spec, pc.sub);
    s.rows = s.rows.map((r, i) => ({ n: i + 1, kind: r.kind, at: `${r.file}:${r.line}`, text: clean(r.text) }));
    if (!s.found) missing = true;
    entry[side] = s;
  }
  result.checks.push(entry);
}

if (json) {
  console.log(JSON.stringify(result, null, 2));
} else {
  const out = [];
  for (const e of result.checks) {
    for (const side of ['base', 'head']) {
      for (const r of e[side].rows) out.push([side, e.check, r.n, r.kind, r.at, r.text].join('\t'));
    }
    for (const side of ['base', 'head']) if (!e[side].found) out.push(`# ${e.check}: ${side} — нет`);
    const count = (side) => (e[side].found ? String(e[side].rows.length) : '—');
    out.push(`# ${e.check}: до ${count('base')}, после ${count('head')}`);
  }
  console.log(out.join('\n'));
}
process.exit(missing ? 1 : 0);
