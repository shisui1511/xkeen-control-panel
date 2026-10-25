#!/usr/bin/env node
/*
 * check-router-kinds.js — разметка вида роутерных Playwright-спеков (D-18, слой 3).
 *
 * pw-suite.sh --part read гонит спеки в несколько воркеров одновременно, поэтому в эту часть
 * должны попадать только спеки, которые не меняют стенд. Вид задаётся строкой
 * `// e2e-kind: read` в первых пяти строках спека; спек без строки — изменяющий (state, безопасное
 * умолчание). Скрипт читает исходники (git ls-files, без запуска).
 *
 * Правила:
 *   1. Строка `// e2e-kind: <вид>` допустима только в первых пяти строках, один раз, и только
 *      со значением read или state (точная форма: pw-suite.sh отбирает её через grep -x).
 *   2. В спеке вида read после вырезания комментариев запрещены: метод 'POST'|'PUT'|'DELETE'|
 *      'PATCH' в строковом литерале, request.post/put/delete/patch(, кнопки «Сохранить»/
 *      «Применить»/Save/Apply через getByRole('button', …), XCP_WANT_CORE, writeFile.
 * Самопроверка разбора на образцах идёт при каждом запуске: расхождение — код 1.
 *
 * Usage: node scripts/check-router-kinds.js
 */
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');

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
  }
  return issues;
}

const problems = [];

// Самопроверка: если разбор сломан, страж молчал бы. [текст, ожидаемое число нарушений]
const samples = [
  ["// e2e-pages: *\n// e2e-kind: read\nawait api(page, 'POST', '/api/x');\n", 1],
  ["// e2e-pages: *\n// e2e-kind: read\n// 'POST' в комментарии не считается\nawait page.goto('/');\n", 0],
  ['// e2e-pages: *\n// e2e-kind: fast\nawait page.goto("/");\n', 1]
];
for (const [text, want] of samples) {
  const got = kindIssues(text).length;
  if (got !== want) {
    problems.push(`самопроверка разбора: образец ${JSON.stringify(text)} дал нарушений ${got}; ожидалось ${want}`);
  }
}

const violations = [];
let readCount = 0;
try {
  const files = execFileSync('git', ['ls-files', 'frontend/tests/router/*.spec.ts'], { cwd: root, encoding: 'utf8' })
    .split('\n')
    .filter(Boolean);
  if (files.length === 0) problems.push('git ls-files не нашёл ни одного роутерного спека — разбор сломан');
  for (const f of files) {
    const text = fs.readFileSync(path.join(root, f), 'utf8');
    if (text.split('\n').slice(0, 5).includes('// e2e-kind: read')) readCount++;
    for (const reason of kindIssues(text)) violations.push(`${f}: ${reason}`);
  }
} catch (e) {
  problems.push(`сканер не отработал: ${e.message}`);
}

if (violations.length > 0 || problems.length > 0) {
  console.error('❌ Разметка вида роутерных Playwright-спеков:');
  for (const v of violations) console.error('  ' + v);
  for (const p of problems) console.error('  ' + p);
  if (violations.length > 0) {
    console.error(
      'Спек, который меняет стенд (ядро, «Применить», службы, источники, файлы), остаётся без строки вида: в параллельную часть read попадают только спеки с `// e2e-kind: read`.'
    );
  }
  process.exit(1);
}
console.log(`✅ Вид роутерных спеков размечен (спеков только чтения: ${readCount})`);
