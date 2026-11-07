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
 *   2. В спеке вида read после вырезания комментариев запрещены:
 *      - метод POST|PUT|DELETE|PATCH в строковом или шаблонном литерале, в любом регистре;
 *      - изменяющий метод контекста запросов: request.post(, request?.post(, request['post'](,
 *        и то же через псевдоним (const r = page.request, ({ request: r }), import { request as r },
 *        const c = await request.newContext(), const { post } = request);
 *      - действия UI: getByText/getByRole/getByLabel/getByTitle/locator/filter с текстом
 *        Применить|Сохранить|Удалить|Перезапустить|Apply|Save|Delete|Restart (строка или
 *        регулярка), press('Control+S'|'Meta+S');
 *      - execFileSync/execFile/execSync/exec/spawn/spawnSync с первым аргументом 'ssh';
 *      - запись и удаление файлов: writeFile*, appendFile*, rm*, rmdir*, unlink*, rename*,
 *        mkdir*, mkdtemp*, copyFile*, cp*, truncate*, symlink*, createWriteStream;
 *      - XCP_WANT_CORE.
 *      Комментарии вырезаются посимвольно: `/` после `=>`, return, typeof, операторов и скобок —
 *      начало регулярки, и `//` или `/*` внутри неё комментарий не открывают.
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

// Слова, после которых `/` начинает регулярку, а не деление.
const REGEX_AFTER_WORDS = new Set([
  'return',
  'typeof',
  'case',
  'do',
  'else',
  'in',
  'of',
  'new',
  'delete',
  'void',
  'throw',
  'instanceof',
  'yield',
  'await'
]);

// `/` начинает регулярку: в начале текста, после оператора или открывающей скобки, после
// `=>`, return, typeof и других слов из REGEX_AFTER_WORDS; после имени, числа, `)` и `]` — деление.
function regexAllowed(prev, prevWord) {
  if (prev === '') return true;
  if (/[\w$]/.test(prev)) return REGEX_AFTER_WORDS.has(prevWord);
  return '(,=:[!&|?{};+-*%<>~^'.includes(prev);
}

// Вырезает комментарии, сохраняя строковые литералы и регулярные литералы как есть.
// Внутри регулярки (с учётом классов […] и экранирования) `//` и `/*` комментарий не открывают.
function stripComments(text) {
  let out = '';
  let i = 0;
  let prev = ''; // последний значимый символ вне комментариев (для отличия деления от регулярки)
  let prevWord = ''; // последнее слово, если prev — его последний символ
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
      prevWord = '';
      continue;
    }
    if (c === '/' && regexAllowed(prev, prevWord)) {
      let j = i + 1;
      let inClass = false;
      while (j < n && text[j] !== '\n') {
        if (text[j] === '\\') j++;
        else if (text[j] === '[') inClass = true;
        else if (text[j] === ']') inClass = false;
        else if (text[j] === '/' && !inClass) break;
        j++;
      }
      if (j < n && text[j] === '/') {
        out += text.slice(i, j + 1);
        i = j + 1;
        prev = '/';
        prevWord = '';
        continue;
      }
      // регулярка не закрылась до конца строки — это не регулярка, `/` идёт как обычный символ
    }
    const glued = out.length > 0 && /[\w$]/.test(out[out.length - 1]);
    out += c;
    if (/[\w$]/.test(c)) {
      prevWord = glued ? prevWord + c : c;
      prev = c;
    } else if (!/\s/.test(c)) {
      prev = c;
      prevWord = '';
    }
    i++;
  }
  return out;
}

// Тексты действий, которые меняют стенд из интерфейса.
const UI_WORDS = 'Применить|Сохранить|Удалить|Перезапустить|Apply|Save|Delete|Restart';

// Регулярки без вложенных квантификаторов: разбор линейный на любом тексте (T-148.1-10).
const MUTATIONS = [
  [/(['"`])(?:post|put|delete|patch)\1/i, 'метод POST|PUT|DELETE|PATCH в строковом литерале (любой регистр)'],
  [
    new RegExp(`\\b(?:getByText|getByRole|getByLabel|getByTitle|locator|filter)\\s*\\(\\s*[^)]*?(?:${UI_WORDS})`, 'i'),
    'действие «Применить»/«Сохранить»/«Удалить»/«Перезапустить» (getByText/getByRole/locator)'
  ],
  [/\.press\s*\(\s*(['"`])(?:Control|Meta|ControlOrMeta)\+s\1/i, 'сочетание Control+S/Meta+S (keyboard.press)'],
  [/\b(?:execFileSync|execFile|execSync|exec|spawnSync|spawn)\s*\(\s*(['"`])ssh\b/, 'запуск ssh (execFileSync/execSync/spawn)'],
  [
    /\b(?:writeFile|appendFile|rm|rmdir|unlink|rename|mkdir|mkdtemp|copyFile|cp|truncate|symlink)(?:Sync)?\s*\(|\bcreateWriteStream\s*\(/,
    'запись или удаление файлов (writeFile*, appendFile*, rm*, unlink*, rename*, mkdir*, copyFile*)'
  ],
  [/XCP_WANT_CORE/, 'XCP_WANT_CORE']
];

// Экранирование для RegExp: и обратный слэш, и спецсимволы.
const reEscape = (x) => x.replace(/[\\^$.*+?()[\]{}|]/g, '\\$&');

const HTTP_MUTATING = /^(?:post|put|delete|patch)$/i;
const IDENT = '[A-Za-z_$][\\w$]*';
// Цепочка «a.b?.c» без вложенных неоднозначных квантификаторов: сегменты разделены точкой.
const CHAIN = '(?:[\\w$]+\\s*\\??\\.\\s*)*[\\w$]+';

// Нарушения сетевых изменений через контекст запросов Playwright и его псевдонимы (WR-01):
// request.post(, request?.post(, request['post'](, const r = page.request; r.put(,
// ({ request: r }) => r.delete(, const { post } = request; post(, const c = await request.newContext(); c.patch(.
function requestIssues(code) {
  const issues = [];
  const masked = maskStrings(code);
  const aliases = new Set(['request']);
  const fns = new Set();
  for (const m of masked.matchAll(new RegExp(`\\brequest\\s*(?::|\\bas\\b)\\s*(${IDENT})`, 'g'))) aliases.add(m[1]);
  const assign = new RegExp(
    `\\b(?:const|let|var)\\s+(${IDENT})\\s*(?::\\s*[\\w$.]+\\s*)?=\\s*(?:await\\s+)?(${CHAIN})(\\s*\\()?`,
    'g'
  );
  const destructure = new RegExp(`\\b(?:const|let|var)\\s*\\{([^}]*)\\}\\s*=\\s*(?:await\\s+)?(${CHAIN})`, 'g');
  for (let grew = true; grew; ) {
    grew = false;
    const add = (name) => {
      if (!aliases.has(name)) {
        aliases.add(name);
        grew = true;
      }
    };
    for (const m of masked.matchAll(assign)) {
      const seg = m[2].split(/\s*\??\.\s*/);
      const last = seg[seg.length - 1];
      if (!m[3] && aliases.has(last)) add(m[1]);
      else if (m[3] && last === 'newContext' && seg.length > 1 && aliases.has(seg[seg.length - 2])) add(m[1]);
    }
    for (const m of masked.matchAll(destructure)) {
      const seg = m[2].split(/\s*\??\.\s*/);
      if (!aliases.has(seg[seg.length - 1])) continue;
      for (const part of m[1].split(',')) {
        const p = /^\s*([\w$]+)\s*(?::\s*([\w$]+))?\s*$/.exec(part);
        if (p && HTTP_MUTATING.test(p[1])) fns.add(p[2] || p[1]);
      }
    }
  }
  for (const a of aliases) {
    const name = reEscape(a);
    const call = new RegExp(`(?:^|[^\\w$])${name}\\s*(?:\\?\\.|\\.)\\s*(post|put|delete|patch)\\s*\\(`, 'i');
    const bracket = new RegExp(`(?:^|[^\\w$])${name}\\s*(?:\\?\\.)?\\s*\\[\\s*(['"\`])(post|put|delete|patch)\\1\\s*\\]`, 'i');
    const m = call.exec(code) || bracket.exec(code);
    if (m) issues.push(`спек вида read содержит изменяющий вызов: ${a === 'request' ? '' : `псевдоним ${a} контекста запросов, `}метод ${m[m.length - 1]}`);
  }
  for (const f of fns) {
    if (new RegExp(`(?:^|[^\\w$.])${reEscape(f)}\\s*\\(`).test(masked)) {
      issues.push(`спек вида read содержит изменяющий вызов: ${f}(…) — метод контекста запросов`);
    }
  }
  return issues;
}

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
    const re = new RegExp(`(^|[^\\w$.])${reEscape(local)}(?![\\w$])`, 'g');
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
    issues.push(...requestIssues(code));
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
  [READ + "import { api } from './lib/nodes-api';\nconst f = api;\n", 1],
  // WR-01: сетевые формы изменения
  [READ + "await fetch('/api/x', { method: 'post' });\n", 1],
  [READ + "await fetch('/api/x', { method: `delete` });\n", 1],
  [READ + "await request?.put('/api/x');\n", 1],
  [READ + "await request['post']('/api/x');\n", 2], // литерал 'post' и форма request['post']
  [READ + "const r = page.request;\nawait r.put('/api/x', {});\n", 1],
  [READ + "test('x', async ({ request: rq }) => {\n  await rq.delete('/api/x');\n});\n", 1],
  [READ + 'const { post: send } = request;\nawait send(url);\n', 1],
  [READ + 'const ctx = await request.newContext();\nawait ctx.patch(url);\n', 1],
  [READ + "const r = page.request;\nawait r.get('/api/x');\nconst post = { title: 'x' };\n", 0],
  [READ + "await fetch('/api/x', { method: 'GET' });\nconst postal = 'postal';\n", 0],
  [READ + 'const { get } = request;\nawait get(url);\n', 0],
  // WR-02: регулярка против деления; `//` и `/*` внутри регулярки не прячут код
  [READ + "const f = (s: string) => /a\\/*b/.test(s);\nawait api(page, 'POST', '/x');\n", 1],
  [READ + "function f(s) {\n  return /a\\/\\//.test(s) ? fetch('/x', { method: 'DELETE' }) : 0;\n}\n", 1],
  [READ + "const re = /[/*]+/;\nawait api(page, 'PATCH', '/x');\n", 1],
  [READ + "const half = total / 2; // 'POST' в комментарии\nconst q = a / b / c; /* 'PUT' */\n", 0],
  [READ + "const re = /https?:\\/\\//; // 'POST'\nconst x = (a) / 2; // 'PATCH'\n", 0],
  // WR-03: действия UI, сочетания клавиш, ssh и запись файлов
  [READ + "await page.getByText('Применить').click();\n", 1],
  [READ + "await page.getByRole('button', { name: /удалить/i }).click();\n", 1],
  [READ + "await page.locator('button', { hasText: 'Перезапустить' }).click();\n", 1],
  [READ + "await page.keyboard.press('Control+S');\n", 1],
  [READ + "execFileSync('ssh', ['stand', 'true']);\n", 1],
  [READ + 'await fs.promises.rm(dir, { recursive: true });\n', 1],
  [READ + "fs.appendFileSync(f, 'x');\n", 1],
  [READ + "await expect(page.getByText('Ядро')).toBeVisible();\nawait page.keyboard.press('Escape');\n", 0],
  [READ + "const data = fs.readFileSync(f, 'utf8');\nexecFileSync('node', ['-v']);\n", 0],
  [READ + "await page.getByRole('heading', { name: 'Настройки' }).isVisible();\nawait page.keyboard.press('Control+A');\n", 0]
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
