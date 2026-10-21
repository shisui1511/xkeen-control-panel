#!/usr/bin/env node
/*
 * core-marks.cjs — статическая разметка «тест зависит от ядра» (Go-часть).
 *
 * Роутерный Go-тест, чей результат зависит от активного ядра, обязан явно
 * сказать об этом: вызвать routertest.CoreSpecific(t) прямо в теле TestRouter*.
 * Сканер читает исходники (git ls-files, без запуска) и отдаёт:
 *   - goMarks(root, pkgs?)   — список размеченных тестов с причиной:
 *       core        — в теле теста есть routertest.CoreSpecific(;
 *       known-core  — есть метка routertest.Known с ядром в селекторе («арх/ядро»)
 *                     в теле теста или в объявлении уровня файла (тогда метку
 *                     получают все TestRouter* этого файла);
 *   - goViolations(root)     — нарушения: тест читает ядро (.Core, XCP_RT_CORE)
 *       или переключает его (switch_kernel), но не размечен; помощник сам берёт
 *       ядро из Current/CoreSpecific/XCP_RT_CORE.
 * TS-часть (specFacts, specIssues, specViolations): спеки Playwright, читающие ядро
 * (RT.core, XCP_CORE, selectorApplies, переключение), обязаны нести тег ядра (CORE_TAG),
 * а метки knownFailure с ядром в селекторе — идти через coreTagsOf. core-switch.spec.ts
 * исключён: он идёт в каждой ветви.
 * Потребители: scripts/check-core-marks.js (страж в CI), выбор набора в changed
 * и сводка прогона. Имя теста принимается только по шаблону ^TestRouter[A-Za-z0-9_]+$
 * (имя попадает в -test.run).
 *
 * CLI: node scripts/router/core-marks.cjs --go [пакет ...]
 *   печатает TSV «пакет<TAB>тест<TAB>причина», отсортированный.
 */
'use strict';

const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const TEST_NAME = /^TestRouter[A-Za-z0-9_]+$/;
const CORE_READ = /\.Core\b/;
const ENV_CORE = /XCP_RT_CORE/;
const SWITCH_KERNEL = /switch_kernel/;
const CORE_SPECIFIC = /\broutertest\.CoreSpecific\(/;
const CURRENT_CALL = /\broutertest\.Current\(/;
const KNOWN_WITH_SLASH = /\broutertest\.Known\(\s*"[^"\n]*\/[^"\n]*"/;
const DECL_START = /^(func|var|const|type)\s/;

/**
 * Разбирает исходник Go: возвращает code (комментарии вырезаны, строки целы) и
 * masked (дополнительно содержимое строк, бэктиков и рун заменено пробелами).
 * Переводы строк сохраняются, длины совпадают с исходником.
 */
function lex(text) {
  let code = '';
  let masked = '';
  const n = text.length;
  let i = 0;
  const blank = (s) => s.replace(/[^\n]/g, ' ');
  while (i < n) {
    const c = text[i];
    const next = text[i + 1];
    if (c === '/' && next === '/') {
      let j = i;
      while (j < n && text[j] !== '\n') j++;
      const seg = text.slice(i, j);
      code += blank(seg);
      masked += blank(seg);
      i = j;
    } else if (c === '/' && next === '*') {
      const end = text.indexOf('*/', i + 2);
      const j = end < 0 ? n : end + 2;
      const seg = text.slice(i, j);
      code += blank(seg);
      masked += blank(seg);
      i = j;
    } else if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < n && text[j] !== c && text[j] !== '\n') {
        j += text[j] === '\\' ? 2 : 1;
      }
      j = Math.min(j + 1, n);
      const seg = text.slice(i, j);
      code += seg;
      masked += c + blank(seg.slice(1, -1)) + (seg.length > 1 ? seg.slice(-1) : '');
      i = j;
    } else if (c === '`') {
      const end = text.indexOf('`', i + 1);
      const j = end < 0 ? n : end + 1;
      const seg = text.slice(i, j);
      code += seg;
      masked += '`' + blank(seg.slice(1, -1)) + (seg.length > 1 ? '`' : '');
      i = j;
    } else {
      code += c;
      masked += c;
      i++;
    }
  }
  return { code, masked };
}

/** Делит файл на объявления верхнего уровня: начало с func/var/const/type в колонке 0. */
function declarations(text) {
  const { code, masked } = lex(text);
  const starts = [];
  let offset = 0;
  for (const line of masked.split('\n')) {
    if (DECL_START.test(line)) starts.push(offset);
    offset += line.length + 1;
  }
  const decls = [];
  starts.forEach((start, k) => {
    const end = k + 1 < starts.length ? starts[k + 1] : code.length;
    const head = masked.slice(start, end).split('\n', 1)[0];
    const fn = /^func\s+(\([^)]*\)\s*)?([A-Za-z_]\w*)\s*[(\[]/.exec(head);
    decls.push({
      kind: fn ? 'func' : 'other',
      receiver: fn ? Boolean(fn[1]) : false,
      name: fn ? fn[2] : '',
      code: code.slice(start, end),
      masked: masked.slice(start, end)
    });
  });
  return decls;
}

/**
 * Анализ текста одного Go-файла.
 * Возвращает { marks: [{test, reason}], violations: [{name, reason}] }.
 */
function analyzeGo(text) {
  const decls = declarations(text);
  const fileKnown = decls.some((d) => d.kind !== 'func' && KNOWN_WITH_SLASH.test(d.code));
  const marks = [];
  const violations = [];
  for (const d of decls) {
    if (d.kind !== 'func' || d.receiver) continue;
    const isTest = /^Test/.test(d.name);
    if (isTest) {
      if (!TEST_NAME.test(d.name)) continue;
      const marked = CORE_SPECIFIC.test(d.masked);
      const known = KNOWN_WITH_SLASH.test(d.code) || fileKnown;
      if (marked) marks.push({ test: d.name, reason: 'core' });
      else if (known) marks.push({ test: d.name, reason: 'known-core' });
      const reads = CORE_READ.test(d.masked) || ENV_CORE.test(d.code) || SWITCH_KERNEL.test(d.code);
      if (reads && !marked) {
        violations.push({
          name: d.name,
          reason: 'тест читает или переключает ядро, но не вызывает routertest.CoreSpecific(t)'
        });
      }
    } else if (
      ENV_CORE.test(d.code) ||
      (CORE_READ.test(d.masked) && (CURRENT_CALL.test(d.masked) || CORE_SPECIFIC.test(d.masked)))
    ) {
      violations.push({
        name: d.name,
        reason: 'помощник сам берёт ядро: передайте ядро параметром из теста с CoreSpecific'
      });
    }
  }
  return { marks, violations };
}

/** Роутерные тестовые файлы Go из индекса git: [{file, pkg, text}]. */
function routerGoFiles(root) {
  const listed = execFileSync('git', ['ls-files', '--', 'internal', 'cmd'], {
    cwd: root,
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024
  })
    .split('\n')
    .filter((f) => /^(internal|cmd)\/.*_test\.go$/.test(f) && !f.startsWith('internal/routertest/'));
  const out = [];
  for (const file of listed) {
    const abs = path.join(root, file);
    if (!fs.existsSync(abs)) continue;
    const text = fs.readFileSync(abs, 'utf8');
    if (text.split('\n', 1)[0].trim() !== '//go:build router') continue;
    out.push({ file, pkg: path.posix.dirname(file), text });
  }
  return out;
}

function normPkg(p) {
  return p.replace(/^\.\//, '').replace(/\/\.\.\.$/, '').replace(/\/$/, '');
}

/** Размеченные тесты: [{pkg, test, reason, file}], отсортированы. */
function goMarks(root, pkgs) {
  const want = pkgs && pkgs.length ? new Set(pkgs.map(normPkg)) : null;
  const rows = [];
  for (const f of routerGoFiles(root)) {
    if (want && !want.has(f.pkg)) continue;
    for (const m of analyzeGo(f.text).marks) rows.push({ pkg: f.pkg, test: m.test, reason: m.reason, file: f.file });
  }
  rows.sort((a, b) => (a.pkg + '\t' + a.test).localeCompare(b.pkg + '\t' + b.test));
  return rows;
}

/** Нарушения разметки: [{file, name, reason}]. */
function goViolations(root) {
  const rows = [];
  for (const f of routerGoFiles(root)) {
    for (const v of analyzeGo(f.text).violations) rows.push({ file: f.file, name: v.name, reason: v.reason });
  }
  rows.sort((a, b) => (a.file + a.name).localeCompare(b.file + b.name));
  return rows;
}

/** TS-часть (Playwright): спеки frontend/tests/router/*.spec.ts. */
const SPEC_READS_CORE = /\b(?:RT\.core|RT\.wantCore|XCP_CORE|XCP_WANT_CORE|switch_kernel)\b|\bselectorApplies\(/;
const SPEC_CORE_TAGGED = /\bCORE_TAG\b|\bCORE_LABEL_TAG\b|['"`]@core['"`]|\bcoreTagsOf\(/;
const SPEC_USES_COREOF = /\bcoreTagsOf\(/;
const KNOWN_CALL = /\bknownFailure\(\s*(['"`])([^'"`\n]*)\1/g;
const LIB_READS_CORE = /\bRT\.core\b|\bXCP_CORE\b/;
const SPEC_EXEMPT = new Set(['core-switch.spec.ts']);
const LIB_EXEMPT = new Set(['env.ts', 'known.ts']);

/** Вырезает комментарии TS/JS и импорты; строки и шаблоны оставляет как есть. */
function stripTs(text) {
  let out = '';
  const n = text.length;
  let i = 0;
  while (i < n) {
    const c = text[i];
    const next = text[i + 1];
    if (c === '/' && next === '/') {
      while (i < n && text[i] !== '\n') i++;
    } else if (c === '/' && next === '*') {
      const end = text.indexOf('*/', i + 2);
      i = end < 0 ? n : end + 2;
    } else if (c === '"' || c === "'" || c === '`') {
      let j = i + 1;
      while (j < n && text[j] !== c && (c === '`' || text[j] !== '\n')) {
        j += text[j] === '\\' ? 2 : 1;
      }
      j = Math.min(j + 1, n);
      out += text.slice(i, j);
      i = j;
    } else {
      out += c;
      i++;
    }
  }
  return out.replace(/^import[\s\S]*?\bfrom\s*['"][^'"\n]+['"];?/gm, '');
}

/**
 * Факты об одном TS-спеке (после вырезания комментариев и импортов):
 * { readsCore, coreTagged, coreLabelSelectors, usesCoreTagsOf }.
 */
function specFacts(text) {
  const code = stripTs(text);
  const coreLabelSelectors = [];
  for (const m of code.matchAll(KNOWN_CALL)) {
    if (m[2].includes('/')) coreLabelSelectors.push(m[2]);
  }
  return {
    readsCore: SPEC_READS_CORE.test(code),
    coreTagged: SPEC_CORE_TAGGED.test(code),
    coreLabelSelectors,
    usesCoreTagsOf: SPEC_USES_COREOF.test(code)
  };
}

/** Причины нарушений одного спека (список строк). */
function specIssues(text) {
  const f = specFacts(text);
  const issues = [];
  if (f.readsCore && !f.coreTagged) {
    issues.push('спек читает ядро без тега ядра: объявите такие тесты с { tag: CORE_TAG }');
  }
  if (f.coreLabelSelectors.length > 0 && !f.usesCoreTagsOf) {
    issues.push(
      `метка knownFailure с ядром в селекторе (${f.coreLabelSelectors.join(', ')}) без coreTagsOf: тег @core-label выводится через test(title, { tag: coreTagsOf(() => …) }, fn)`
    );
  }
  return issues;
}

function trackedFiles(root, glob) {
  return execFileSync('git', ['ls-files', '--', glob], { cwd: root, encoding: 'utf8' })
    .split('\n')
    .filter(Boolean)
    .filter((f) => fs.existsSync(path.join(root, f)));
}

/** Нарушения разметки спеков и помощников Playwright: [{file, name, reason}]. */
function specViolations(root) {
  const rows = [];
  for (const file of trackedFiles(root, 'frontend/tests/router/*.spec.ts')) {
    if (SPEC_EXEMPT.has(path.posix.basename(file))) continue;
    for (const reason of specIssues(fs.readFileSync(path.join(root, file), 'utf8'))) {
      rows.push({ file, name: '(спек)', reason });
    }
  }
  for (const file of trackedFiles(root, 'frontend/tests/router/lib/*.ts')) {
    if (LIB_EXEMPT.has(path.posix.basename(file))) continue;
    if (LIB_READS_CORE.test(stripTs(fs.readFileSync(path.join(root, file), 'utf8')))) {
      rows.push({
        file,
        name: '(помощник)',
        reason: 'помощник читает ядро: решение принимается в спеке с тегом ядра'
      });
    }
  }
  rows.sort((a, b) => (a.file + a.name).localeCompare(b.file + b.name));
  return rows;
}

module.exports = { analyzeGo, goMarks, goViolations, specFacts, specIssues, specViolations };

if (require.main === module) {
  const argv = process.argv.slice(2);
  if (argv[0] !== '--go') {
    console.error('Usage: node scripts/router/core-marks.cjs --go [пакет ...]');
    process.exit(2);
  }
  const root = path.resolve(__dirname, '..', '..');
  for (const m of goMarks(root, argv.slice(1))) console.log(`${m.pkg}\t${m.test}\t${m.reason}`);
}
