#!/usr/bin/env node
/*
 * check-router-kinds.js — разметка вида роутерных Playwright-спеков (D-18, слой 3).
 *
 * pw-suite.sh --part read гонит спеки в несколько воркеров одновременно, поэтому в эту часть
 * должны попадать только спеки, которые не меняют стенд. Вид задаётся строкой
 * `// e2e-kind: read` в первых пяти строках спека; спек без строки — изменяющий (state, безопасное
 * умолчание). Скрипт читает исходники (без запуска).
 *
 * Правила:
 *   1. Строка `// e2e-kind: <вид>` допустима только в первых пяти строках, один раз, и только
 *      со значением read или state (точная форма: pw-suite.sh отбирает её через grep -x).
 *   2. В спеке вида read после вырезания комментариев запрещены: метод 'POST'|'PUT'|'DELETE'|
 *      'PATCH' в строковом литерале, request.post/put/delete/patch(, кнопки «Сохранить»/
 *      «Применить»/Save/Apply через getByRole('button', …), XCP_WANT_CORE, writeFile.
 *   3. Спек вида read импортирует из frontend/tests/router/lib только читающие помощники:
 *      модули console, env, known, pages, regex, motion целиком; из nodes-api — nodeLink,
 *      randomPort, installedKernels, listSources, withPanel, api и типы. Вызов api допустим
 *      только без метода или с литералом 'GET'; api не передаётся как значение. Импорт
 *      другого имени или модуля lib/, namespace- и default-импорт nodes-api, динамический
 *      импорт lib/ и импорт другого относительного модуля — нарушение: страж не видит,
 *      что делает помощник вне текста спека.
 * Самопроверка разбора на образцах идёт при каждом запуске: расхождение — код 1.
 *
 * Список спеков: без флага — git ls-files (так в CI); с --worktree — рабочее дерево через fs,
 * включая незакоммиченные и неотслеживаемые спеки (так перед прогоном на стенде).
 *
 * Usage: node scripts/check-router-kinds.js [--worktree]
 */
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const SPEC_DIR = 'frontend/tests/router';

const args = process.argv.slice(2);
const WORKTREE = args.includes('--worktree');
for (const a of args) {
  if (a !== '--worktree') {
    console.error(`check-router-kinds: неизвестный аргумент ${JSON.stringify(a)}; допустим только --worktree`);
    process.exit(2);
  }
}

const KIND_STRICT = /^\/\/ e2e-kind: (read|state)$/;

// Вырезает комментарии, сохраняя строковые литералы и регулярные литералы как есть.
function stripComments(text) {
  let out = '';
  let i = 0;
  let prev = ''; // последний значимый символ вне комментариев (для отличия деления от регулярки)
  const n = text.length;
  while (i < n) {
    const c = text[i];
    const d = text[i + 1];
    if (c === '/' && d === '/') {
      while (i < n && text[i] !== '\n') i++;
      continue;
    }
    if (c === '/' && d === '*') {
      const end = text.indexOf('*/', i + 2);
      i = end < 0 ? n : end + 2;
      out += ' ';
      continue;
    }
    if (c === "'" || c === '"' || c === '`') {
      let j = i + 1;
      while (j < n && text[j] !== c) {
        if (text[j] === '\\') j++;
        else if (c !== '`' && text[j] === '\n') break;
        j++;
      }
      out += text.slice(i, j + 1);
      i = j + 1;
      prev = c;
      continue;
    }
    if (c === '/' && (prev === '' || '(,=:[!&|?{};'.includes(prev))) {
      let j = i + 1;
      let inClass = false;
      while (j < n && text[j] !== '\n') {
        if (text[j] === '\\') j++;
        else if (text[j] === '[') inClass = true;
        else if (text[j] === ']') inClass = false;
        else if (text[j] === '/' && !inClass) break;
        j++;
      }
      out += text.slice(i, j + 1);
      i = j + 1;
      prev = '/';
      continue;
    }
    out += c;
    if (!/\s/.test(c)) prev = c;
    i++;
  }
  return out;
}

const MUTATIONS = [
  [/['"](POST|PUT|DELETE|PATCH)['"]/, "метод 'POST'|'PUT'|'DELETE'|'PATCH'"],
  [/\brequest\s*\.\s*(post|put|delete|patch)\s*\(/, 'request.post/put/delete/patch'],
  [/getByRole\(\s*['"]button['"]\s*,\s*\{[^}]*(Сохранить|Применить|Save|Apply)/, 'кнопка «Сохранить»/«Применить»'],
  [/XCP_WANT_CORE/, 'XCP_WANT_CORE'],
  [/writeFile/, 'writeFile']
];

// Импорты спека read (CR-02). Модули lib/, которые целиком только читают стенд.
const READ_LIB_MODULES = new Set(['console', 'env', 'known', 'pages', 'regex', 'motion']);
// Читающие имена nodes-api: resetDraft, addToDraft, layerFlag меняют стенд и сюда не входят.
const READ_NODES_API = new Set([
  'nodeLink',
  'randomPort',
  'installedKernels',
  'listSources',
  'withPanel',
  'api',
  'ApiResult',
  'Installed',
  'SourceInfo'
]);
const LIB_PREFIX = `${SPEC_DIR}/lib/`;

// Модуль lib/ по относительному пути импорта; null — не относительный путь (пакет), '' — относительный вне lib/.
function libModule(spec) {
  if (!spec.startsWith('.')) return null;
  const full = path.posix.normalize(`${SPEC_DIR}/${spec}`);
  if (!full.startsWith(LIB_PREFIX)) return '';
  const rest = full.slice(LIB_PREFIX.length).replace(/\.(ts|js|mjs|cjs)$/, '');
  return rest.includes('/') ? '' : rest;
}

// Разбор клаузы импорта: default, namespace и именованные ({ a, b as c, type D }).
function parseClause(clause) {
  const res = { def: null, ns: null, named: [] };
  let rest = clause;
  const braces = /\{([^}]*)\}/.exec(rest);
  if (braces) {
    for (const part of braces[1].split(',')) {
      const p = part.trim();
      if (!p) continue;
      const m = /^(type\s+)?([\w$]+)(?:\s+as\s+([\w$]+))?$/.exec(p);
      if (!m) {
        res.named.push({ name: p, local: p, type: false, bad: true });
        continue;
      }
      res.named.push({ name: m[2], local: m[3] || m[2], type: Boolean(m[1]), bad: false });
    }
    rest = rest.replace(braces[0], ' ');
  }
  const ns = /\*\s*as\s+([\w$]+)/.exec(rest);
  if (ns) {
    res.ns = ns[1];
    rest = rest.replace(ns[0], ' ');
  }
  const def = rest.replace(/,/g, ' ').trim();
  if (def) res.def = def;
  return res;
}

// Аргументы вызова от открывающей скобки: массив строк или null, если скобки не сошлись.
function callArgs(code, open) {
  const list = [];
  let depth = 0;
  let start = open + 1;
  for (let i = open; i < code.length; i++) {
    const c = code[i];
    if (c === "'" || c === '"' || c === '`') {
      let j = i + 1;
      while (j < code.length && code[j] !== c) {
        if (code[j] === '\\') j++;
        j++;
      }
      i = j;
      continue;
    }
    if (c === '(' || c === '[' || c === '{') depth++;
    else if (c === ')' || c === ']' || c === '}') {
      depth--;
      if (depth === 0) {
        const last = code.slice(start, i).trim();
        if (last || list.length > 0) list.push(last);
        return list;
      }
    } else if (c === ',' && depth === 1) {
      list.push(code.slice(start, i).trim());
      start = i + 1;
    }
  }
  return null;
}

// Содержимое строковых литералов заменяется пробелами той же длины: поиск имён не видит текст строк.
function maskStrings(code) {
  let out = '';
  for (let i = 0; i < code.length; i++) {
    const c = code[i];
    if (c !== "'" && c !== '"' && c !== '`') {
      out += c;
      continue;
    }
    let j = i + 1;
    while (j < code.length && code[j] !== c) {
      if (code[j] === '\\') j++;
      j++;
    }
    out += c + code.slice(i + 1, j).replace(/[^\n]/g, ' ') + (j < code.length ? c : '');
    i = j;
  }
  return out.slice(0, code.length);
}

// Нарушения импортов спека read: изменяющие помощники lib/ и непроверяемые модули.
function importIssues(code) {
  const issues = [];
  const apiLocals = [];
  let body = code;
  const statics = [
    [/\bimport\s+(type\s+)?([^;'"`()]*?)\s*\bfrom\s*(['"])([^'"\n]+)\3/g, 'clause'],
    [/\bimport\s*(['"])([^'"\n]+)\1/g, 'side'],
    [/\bexport\s+(type\s+)?([^;'"`()]*?)\s*\bfrom\s*(['"])([^'"\n]+)\3/g, 'reexport']
  ];
  for (const [re, form] of statics) {
    for (const m of code.matchAll(re)) {
      const spec = form === 'side' ? m[2] : m[4];
      body = body.replace(m[0], ' '.repeat(m[0].length));
      const mod = libModule(spec);
      if (mod === null) continue;
      if (mod === '') {
        issues.push(`спек read импортирует непроверяемый модуль ${spec}: допустимы только ./lib/*`);
        continue;
      }
      if (READ_LIB_MODULES.has(mod)) continue;
      if (form !== 'clause') {
        issues.push(`спек read импортирует модуль ${mod} целиком (${form === 'side' ? 'импорт ради побочного эффекта' : 'реэкспорт'})`);
        continue;
      }
      const typeOnly = Boolean(m[1]);
      const cl = parseClause(m[2]);
      if (mod !== 'nodes-api') {
        if (typeOnly) continue;
        const names = cl.named.filter((x) => !x.type).map((x) => x.name);
        if (cl.def) names.push(cl.def);
        if (cl.ns) names.push(`* as ${cl.ns}`);
        for (const name of names) issues.push(`спек read импортирует изменяющий помощник ${name} из ${mod}`);
        continue;
      }
      if (typeOnly) continue;
      if (cl.ns) issues.push(`спек read импортирует nodes-api целиком (* as ${cl.ns}): допустимы только читающие имена`);
      if (cl.def) issues.push(`спек read импортирует default из nodes-api (${cl.def})`);
      for (const x of cl.named) {
        if (x.bad) issues.push(`спек read: не разобран импорт ${JSON.stringify(x.name)} из nodes-api`);
        else if (x.type) continue;
        else if (!READ_NODES_API.has(x.name)) issues.push(`спек read импортирует изменяющий помощник ${x.name} из nodes-api`);
        else if (x.name === 'api') apiLocals.push(x.local);
      }
    }
  }
  for (const m of code.matchAll(/\b(?:import|require)\s*\(\s*(['"`])([^'"`\n]+)\1/g)) {
    const mod = libModule(m[2]);
    if (mod === null || READ_LIB_MODULES.has(mod)) continue;
    issues.push(`спек read подключает ${mod === '' ? m[2] : mod} динамически: страж не видит, какие помощники вызываются`);
  }
  // api из nodes-api: только вызовы без метода или с литералом 'GET'.
  const masked = maskStrings(body);
  for (const local of apiLocals) {
    const re = new RegExp(`(^|[^\\w$.])${local.replace(/\$/g, '\\$')}(?![\\w$])`, 'g');
    for (const m of masked.matchAll(re)) {
      const at = m.index + m[1].length + local.length;
      const rest = body.slice(at);
      const ws = /^\s*/.exec(rest)[0].length;
      if (rest[ws] !== '(') {
        issues.push(`спек read передаёт ${local} из nodes-api как значение: вызовы не проверить`);
        continue;
      }
      const list = callArgs(body, at + ws);
      if (list === null) {
        issues.push(`спек read: не разобран вызов ${local}(…) — скобки не сошлись`);
        continue;
      }
      const method = list[1];
      if (method === undefined || /^(['"`])GET\1$/i.test(method)) continue;
      issues.push(`спек read вызывает ${local}(…) с методом ${method}: допустим только 'GET'`);
    }
  }
  return issues;
}

// Возвращает список причин нарушения для текста спека.
function kindIssues(text) {
  const issues = [];
  const lines = text.split('\n');
  const kinds = [];
  lines.forEach((line, idx) => {
    // строка-комментарий про вид: любая форма, кроме точной, — нарушение (упоминание в середине строки не разметка)
    if (!/^\s*\/\/\s*e2e-kind\b/.test(line)) return;
    if (idx >= 5) {
      issues.push(`строка вида на строке ${idx + 1}: допустима только в первых пяти строках`);
      return;
    }
    const m = KIND_STRICT.exec(line);
    if (!m) {
      issues.push(`строка вида ${JSON.stringify(line.trim())}: нужна форма «// e2e-kind: read» или «// e2e-kind: state»`);
      return;
    }
    kinds.push(m[1]);
  });
  if (kinds.length > 1) issues.push('строка вида повторяется');
  if (kinds[0] === 'read') {
    const code = stripComments(text);
    for (const [re, what] of MUTATIONS) {
      if (re.test(code)) issues.push(`спек вида read содержит изменяющий вызов: ${what}`);
    }
    issues.push(...importIssues(code));
  }
  return issues;
}

const problems = [];

const READ = '// e2e-pages: *\n// e2e-kind: read\n';
// Самопроверка: если разбор сломан, страж молчал бы. [текст, ожидаемое число нарушений]
const samples = [
  [READ + "await api(page, 'POST', '/api/x');\n", 1],
  [READ + "// 'POST' в комментарии не считается\nawait page.goto('/');\n", 0],
  ['// e2e-pages: *\n// e2e-kind: fast\nawait page.goto("/");\n', 1],
  // CR-02: импорты помощников lib/
  [READ + "import { resetDraft } from './lib/nodes-api';\n", 1],
  [READ + "import { listSources } from './lib/nodes-api';\n", 0],
  [READ + "import * as nodes from './lib/nodes-api';\n", 1],
  [READ + "import { layerFlag as lf, withPanel } from './lib/nodes-api.ts';\n", 1],
  [READ + "import { ensureDnsTarget } from './lib/dns-target';\n", 1],
  [READ + "import { attachConsoleGuard } from './lib/console';\nimport { RT, T } from './lib/env';\n", 0],
  [READ + "import { type SourceInfo, nodeLink } from './lib/nodes-api';\nimport type { Installed } from './lib/nodes-api';\n", 0],
  [READ + "import { helper } from './helpers';\n", 1],
  [READ + "const m = await import('./lib/nodes-api');\n", 1],
  [READ + "import { api } from './lib/nodes-api';\nawait api(page, 'GET', '/api/settings');\nawait api(page, \"get\", '/x');\n", 0],
  [READ + "import { api as call } from './lib/nodes-api';\nconst m = 'GE' + 'T';\nawait call(page, m, '/api/settings');\n", 1],
  [READ + "import { api } from './lib/nodes-api';\nconst f = api;\n", 1]
];
for (const [text, want] of samples) {
  const got = kindIssues(text).length;
  if (got !== want) {
    problems.push(`самопроверка разбора: образец ${JSON.stringify(text)} дал нарушений ${got}; ожидалось ${want}`);
  }
}

// Спеки рабочего дерева, включая неотслеживаемые (как их найдёт Playwright: **/*.spec.ts).
function worktreeSpecs() {
  const found = [];
  const walk = (rel) => {
    for (const e of fs.readdirSync(path.join(root, rel), { withFileTypes: true })) {
      const r = `${rel}/${e.name}`;
      if (e.isDirectory()) {
        if (e.name !== 'node_modules') walk(r);
      } else if (e.isFile() && e.name.endsWith('.spec.ts')) found.push(r);
    }
  };
  walk(SPEC_DIR);
  return found.sort();
}

const violations = [];
let readCount = 0;
let specCount = 0;
try {
  const files = WORKTREE
    ? worktreeSpecs()
    : execFileSync('git', ['ls-files', `${SPEC_DIR}/*.spec.ts`], { cwd: root, encoding: 'utf8' })
        .split('\n')
        .filter(Boolean);
  if (files.length === 0) {
    problems.push(`${WORKTREE ? 'рабочее дерево' : 'git ls-files'}: не найдено ни одного роутерного спека — разбор сломан`);
  }
  specCount = files.length;
  for (const f of files) {
    const text = fs.readFileSync(path.join(root, f), 'utf8');
    if (text.split('\n').slice(0, 5).includes('// e2e-kind: read')) readCount++;
    for (const reason of kindIssues(text)) violations.push(`${f}: ${reason}`);
  }
} catch (e) {
  problems.push(`сканер не отработал: ${e.message}`);
}

const MODE = WORKTREE ? 'рабочее дерево' : 'git ls-files';
if (violations.length > 0 || problems.length > 0) {
  console.error(`❌ Разметка вида роутерных Playwright-спеков (режим: ${MODE}):`);
  for (const v of violations) console.error('  ' + v);
  for (const p of problems) console.error('  ' + p);
  if (violations.length > 0) {
    console.error(
      'Спек, который меняет стенд (ядро, «Применить», службы, источники, файлы), остаётся без строки вида: в параллельную часть read попадают только спеки с `// e2e-kind: read`.'
    );
  }
  process.exit(1);
}
console.log(`✅ Вид роутерных спеков размечен (режим: ${MODE}; спеков: ${specCount}, только чтения: ${readCount})`);
