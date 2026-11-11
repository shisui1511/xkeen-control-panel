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
 * (RT.core, XCP_CORE, selectorApplies, переключение, помощник спека, читающий ядро),
 * обязаны нести тег ядра (CORE_TAG) на каждом таком test(...),
 * а метки knownFailure с ядром в селекторе — идти через coreTagsOf. core-switch.spec.ts
 * исключён: он идёт в каждой ветви.
 * Области router-area и факты тестов (loadAreas, matchGlob, areaOfText, testFacts — ярус,
 * Slow, карантин, метка ядра, область файла) — в конце файла; их читают
 * scripts/check-router-areas.js, выбор changed, страж ярусов и бюджет.
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
const TARGET_PARAM = /\broutertest\.Target\b/;
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
    } else if (CORE_READ.test(d.masked) && TARGET_PARAM.test(d.masked.split('\n', 1)[0])) {
      // Цель, полученная тестом из routertest.Current(t), не размечает тест: в теле его нет
      // ни .Core, ни CoreSpecific, а помощник читает ядро. Ядро передаётся строкой.
      violations.push({
        name: d.name,
        reason:
          'помощник принимает routertest.Target и читает ядро: передайте ядро строкой из теста с CoreSpecific'
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

/** Вырезает комментарии TS/JS и импорты; строки, шаблоны и литералы регулярных выражений оставляет как есть. */
function stripTs(text) {
  let out = '';
  const n = text.length;
  let i = 0;
  while (i < n) {
    const c = text[i];
    const next = text[i + 1];
    if (c === '/' && isRegexStart(text, i)) {
      // литерал регулярного выражения целиком: `\/\/` внутри него не комментарий
      const j = Math.min(regexEnd(text, i) + 1, n);
      out += text.slice(i, j);
      i = j;
    } else if (c === '/' && next === '/') {
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

/** Конец вызова, открытого скобкой в позиции open: индекс закрывающей скобки или -1. */
function matchParen(code, open) {
  let depth = 0;
  const n = code.length;
  for (let i = open; i < n; i++) {
    const c = code[i];
    if (c === '"' || c === "'" || c === '`') {
      let j = i + 1;
      while (j < n && code[j] !== c && (c === '`' || code[j] !== '\n')) {
        j += code[j] === '\\' ? 2 : 1;
      }
      i = j;
    } else if (c === '/' && isRegexStart(code, i)) {
      // литерал регулярного выражения: скобки и кавычки внутри не считаются
      i = regexEnd(code, i);
    } else if (c === '(') {
      depth++;
    } else if (c === ')') {
      depth--;
      if (depth === 0) return i;
    }
  }
  return -1;
}

/** Индекс закрывающего слеша литерала регулярного выражения, начатого в позиции i (или конец строки). */
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

/** Слеш в позиции i начинает литерал регулярного выражения, а не деление или комментарий. */
function isRegexStart(code, i) {
  if (code[i + 1] === '/' || code[i + 1] === '*') return false;
  let k = i - 1;
  while (k >= 0 && /\s/.test(code[k])) k--;
  if (k < 0 || '(,=:[!&|?{};<>+-*%^~'.includes(code[k])) return true;
  // слово-оператор перед слешем: `return /x/`, `await /x/.test(…)` и т. п.
  const word = /([A-Za-z_$][\w$]*)$/.exec(code.slice(Math.max(0, k - 12), k + 1));
  return word !== null && /^(?:return|typeof|case|in|of|else|do|void|throw|delete|new|await|yield)$/.test(word[1]);
}

/**
 * Помощники верхнего уровня спека, читающие ядро (прямо или через другой такой помощник):
 * Set имён. Объявление — строка в колонке 0 с function/const; оно тянется до следующей строки
 * в колонке 0, начинающейся со слова (закрывающие скобки в колонке 0 его не обрывают).
 */
function coreReadingHelpers(code) {
  const chunks = new Map();
  const lines = code.split('\n');
  let name = null;
  let buf = [];
  const flush = () => {
    if (name) chunks.set(name, buf.join('\n'));
  };
  for (const line of lines) {
    if (/^[A-Za-z_$]/.test(line)) {
      flush();
      const m = /^(?:export\s+)?(?:async\s+)?function\s+(\w+)|^(?:export\s+)?const\s+(\w+)\s*=/.exec(line);
      name = m ? m[1] || m[2] : null;
      buf = [line];
    } else {
      buf.push(line);
    }
  }
  flush();
  const tainted = new Set();
  for (const [n, body] of chunks) if (SPEC_READS_CORE.test(body)) tainted.add(n);
  for (let grew = true; grew; ) {
    grew = false;
    for (const [n, body] of chunks) {
      if (tainted.has(n)) continue;
      for (const t of tainted) {
        if (new RegExp('\\b' + t + '\\(').test(body)) {
          tainted.add(n);
          grew = true;
          break;
        }
      }
    }
  }
  return tainted;
}

/**
 * Вызовы test(...) спека, читающие ядро (RT.core и т. п. или помощник, читающий ядро), но
 * без тега ядра: список заголовков. Тег — CORE_TAG, '@core' или coreTagsOf в самом вызове либо
 * переменная tag, объявленная с ними. Вызов с несошедшимися скобками не пропускается, а
 * попадает в список с unparsed: true (разбор не удался — нарушение, а не молчаливый пропуск).
 */
function untaggedCoreTests(text) {
  const code = stripTs(text);
  const tainted = coreReadingHelpers(code);
  const tagVarOk = /\b(?:const|let)\s+tag\s*=[^;]*?(?:CORE_TAG|CORE_LABEL_TAG|coreTagsOf)/s.test(code);
  const bad = [];
  for (const m of code.matchAll(/(?<![\w.$])test\(/g)) {
    const open = m.index + m[0].length - 1;
    const end = matchParen(code, open);
    if (end < 0) {
      // страж не должен проходить на том, что не смог разобрать
      bad.push({ unparsed: true, title: code.slice(open, open + 60).replace(/\s+/g, ' ') });
      continue;
    }
    const seg = code.slice(open, end + 1);
    const reads =
      SPEC_READS_CORE.test(seg) || [...tainted].some((n) => new RegExp('\\b' + n + '\\(').test(seg));
    if (!reads) continue;
    const tagged =
      SPEC_CORE_TAGGED.test(seg) || (tagVarOk && /\{\s*tag\s*[,}]|\btag:\s*tag\b/.test(seg));
    if (!tagged) bad.push({ unparsed: false, title: seg.slice(1, 70).replace(/\s+/g, ' ') });
  }
  return bad;
}

/** Причины нарушений одного спека (список строк). */
function specIssues(text) {
  const f = specFacts(text);
  const issues = [];
  if (f.readsCore && !f.coreTagged) {
    issues.push('спек читает ядро без тега ядра: объявите такие тесты с { tag: CORE_TAG }');
  } else if (f.readsCore) {
    // тег есть у части тестов файла: остальные тесты, читающие ядро, тоже должны его нести
    for (const { unparsed, title } of untaggedCoreTests(text)) {
      issues.push(
        unparsed
          ? `не удалось разобрать скобки вызова test(…), проверка тега ядра невозможна: ${title}`
          : `тест читает ядро без тега ядра: объявите его с { tag: CORE_TAG } (${title})`
      );
    }
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

/*
 * Области router-area (RTR-03, D-09…D-11): карта scripts/router/areas.json и строка
 * `// router-area: <область>` в первых пяти строках каждого роутерного Go-файла и спека.
 * Потребители: scripts/check-router-areas.js (страж), выбор changed (148.1-09), страж
 * ярусов и бюджет — через testFacts(root).
 */
const AREAS_FILE = 'scripts/router/areas.json';
const AREA_NAME = /^[a-z][a-z0-9-]*$/;
const AREA_LINE = /^\/\/ router-area: ([a-z][a-z0-9-]*)$/;
const AREA_LINE_ANY = /^\s*\/\/\s*router-area\b/;
const SAFE_GLOB = /^[A-Za-z0-9_.*][A-Za-z0-9_.*/-]*$/;
const SPEC_PATH = /^tests\/router\/[A-Za-z0-9_.-]+\.spec\.ts$/;
const HARNESS_GO = /^(?:internal|cmd)(?:\/[A-Za-z0-9_.-]+)*:TestRouter[A-Za-z0-9_]+$/;
const SAFE_REPO_PATH = /^[A-Za-z0-9_.][A-Za-z0-9_./-]*$/;

/** Глоб карты в регулярное выражение: `*` — любой сегмент без «/», `**` — любые сегменты. */
function globRegExp(glob) {
  let re = '';
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i];
    if (c === '*' && glob[i + 1] === '*') {
      if (glob[i + 2] === '/') {
        re += '(?:[^/]+/)*';
        i += 2;
      } else {
        re += '.*';
        i += 1;
      }
    } else if (c === '*') {
      re += '[^/]*';
    } else {
      re += c.replace(/[.+?^${}()|[\]\\]/g, '\\$&');
    }
  }
  return new RegExp('^' + re + '$');
}

/** Путь от корня репозитория подходит под глоб карты областей. */
function matchGlob(glob, file) {
  return globRegExp(glob).test(file);
}

/**
 * Строка области в тексте файла: { area, issues }. Строка допустима только в первых пяти
 * строках, ровно в форме `// router-area: <область>` и один раз; любое другое упоминание
 * в виде строки-комментария — нарушение. Нет строки — { area: null, issues: [] }.
 */
function areaOfText(text) {
  const issues = [];
  const found = [];
  text.split('\n').forEach((line, idx) => {
    if (!AREA_LINE_ANY.test(line)) return;
    if (idx >= 5) {
      issues.push(`строка области на строке ${idx + 1}: допустима только в первых пяти строках`);
      return;
    }
    const m = AREA_LINE.exec(line);
    if (!m) {
      issues.push(`строка области ${JSON.stringify(line.trim())}: нужна форма «// router-area: <область>», одна область`);
      return;
    }
    found.push(m[1]);
  });
  if (found.length > 1) issues.push(`строка области повторяется (${found.join(', ')}): одна область на файл`);
  return { area: issues.length === 0 && found.length === 1 ? found[0] : null, issues };
}

/** Запись поля tests (области или подобласти): go:TestRouter* или tests/router/<спек>.spec.ts. */
function isTestEntry(t) {
  return typeof t === 'string' && ((t.startsWith('go:') && TEST_NAME.test(t.slice(3))) || SPEC_PATH.test(t));
}

/** Глоб карты безопасен: символы пути, без «..» и «***». */
function isSafeGlob(g) {
  return typeof g === 'string' && SAFE_GLOB.test(g) && !g.split('/').includes('..') && !g.includes('***');
}

/**
 * Подобласти общей области (RTR-14, D-12 фазы 148.1.1): areas.<родитель>.subareas —
 * { '<имя>': { title, paths, tests } }. Правка файла подобласти выбирает её тесты по именам из
 * tests (прямое затрагивание: ярус full разрешён) и, если родитель shared, канарейки всех
 * областей; тесты родителя целиком не выбираются. Строка router-area у файлов тестов остаётся
 * родительской. Совпадение путей с путями родителя, существование тестов и полноту покрытия
 * проверяет scripts/check-router-areas.js (ему нужен git ls-files и факты тестов).
 */
function validateSubareas(at, a, issues) {
  const s = a.subareas;
  if (!s || typeof s !== 'object' || Array.isArray(s) || Object.keys(s).length === 0) {
    issues.push(`${at}: subareas — непустой объект «имя → подобласть»`);
    return;
  }
  if ('tests' in a) issues.push(`${at}: subareas и tests вместе недопустимы — тесты перечисляют подобласти`);
  for (const [sub, v] of Object.entries(s)) {
    const st = `${at}: подобласть ${sub}`;
    if (!AREA_NAME.test(sub)) issues.push(`${st}: имя не по шаблону [a-z][a-z0-9-]*`);
    if (!v || typeof v !== 'object' || Array.isArray(v)) {
      issues.push(`${st}: не объект`);
      continue;
    }
    for (const k of Object.keys(v)) {
      if (!['title', 'paths', 'tests'].includes(k)) issues.push(`${st}: неизвестный ключ ${JSON.stringify(k)}`);
    }
    if (typeof v.title !== 'string' || !v.title.trim()) issues.push(`${st}: нет title`);
    if (!Array.isArray(v.paths) || v.paths.length === 0) issues.push(`${st}: paths — непустой список глобов`);
    else {
      for (const g of v.paths) if (!isSafeGlob(g)) issues.push(`${st}: недопустимый глоб ${JSON.stringify(g)}`);
      const dup = v.paths.filter((g, i) => v.paths.indexOf(g) !== i);
      if (dup.length > 0) issues.push(`${st}: paths — повтор ${[...new Set(dup)].join(', ')}`);
    }
    if (!Array.isArray(v.tests) || v.tests.length === 0) issues.push(`${st}: tests — непустой список`);
    else {
      for (const t of v.tests) {
        if (!isTestEntry(t)) issues.push(`${st}: tests — запись ${JSON.stringify(t)} не go:TestRouter* и не tests/router/<спек>.spec.ts`);
      }
      const dup = v.tests.filter((t, i) => v.tests.indexOf(t) !== i);
      if (dup.length > 0) issues.push(`${st}: tests — повтор ${[...new Set(dup)].join(', ')}`);
    }
  }
}

/** Подобласти области name, чьи глобы находят файл: [имя] (пусто — нет подобластей или совпадений). */
function subareasOf(areas, name, file) {
  const s = areas[name] && areas[name].subareas;
  if (!s) return [];
  return Object.keys(s).filter((sub) => s[sub].paths.some((g) => matchGlob(g, file)));
}

/** Проверка схемы карты областей: список нарушений (пустой — карта годна). */
function validateAreas(data, root) {
  const issues = [];
  if (!data || typeof data !== 'object' || Array.isArray(data)) return ['карта не объект JSON'];
  for (const k of Object.keys(data)) {
    if (!['version', 'areas', 'harness'].includes(k)) issues.push(`неизвестный ключ верхнего уровня ${JSON.stringify(k)}`);
  }
  if (data.version !== 1) issues.push('version должен быть 1');
  const areas = data.areas;
  if (!areas || typeof areas !== 'object' || Array.isArray(areas) || Object.keys(areas).length === 0) {
    issues.push('areas — непустой объект «имя → область»');
    return issues;
  }
  for (const [name, a] of Object.entries(areas)) {
    const at = `область ${name}`;
    if (!AREA_NAME.test(name)) issues.push(`${at}: имя не по шаблону [a-z][a-z0-9-]*`);
    if (!a || typeof a !== 'object' || Array.isArray(a)) {
      issues.push(`${at}: не объект`);
      continue;
    }
    for (const k of Object.keys(a)) {
      if (!['title', 'shared', 'paths', 'canary', 'tests', 'subareas'].includes(k)) {
        issues.push(`${at}: неизвестный ключ ${JSON.stringify(k)}`);
      }
    }
    if (typeof a.title !== 'string' || !a.title.trim()) issues.push(`${at}: нет title`);
    if (typeof a.shared !== 'boolean') issues.push(`${at}: shared — true или false`);
    if (!Array.isArray(a.paths) || a.paths.length === 0) issues.push(`${at}: paths — непустой список глобов`);
    else {
      for (const g of a.paths) if (!isSafeGlob(g)) issues.push(`${at}: недопустимый глоб ${JSON.stringify(g)}`);
    }
    if ('subareas' in a) validateSubareas(at, a, issues);
    const c = a.canary;
    if (!c || typeof c !== 'object' || Array.isArray(c)) issues.push(`${at}: нет canary { go, spec, reason? }`);
    else {
      for (const k of Object.keys(c)) {
        if (!['go', 'spec', 'reason'].includes(k)) issues.push(`${at}: canary — неизвестный ключ ${JSON.stringify(k)}`);
      }
      if (!('go' in c) || (c.go !== null && (typeof c.go !== 'string' || !TEST_NAME.test(c.go)))) {
        issues.push(`${at}: canary.go — имя TestRouter* или null`);
      }
      if (!('spec' in c) || (c.spec !== null && (typeof c.spec !== 'string' || !SPEC_PATH.test(c.spec)))) {
        issues.push(`${at}: canary.spec — путь tests/router/<спек>.spec.ts или null`);
      }
      if ('reason' in c && typeof c.reason !== 'string') issues.push(`${at}: canary.reason — строка`);
      if ((c.go === null || c.spec === null) && !(typeof c.reason === 'string' && c.reason.trim())) {
        issues.push(`${at}: канарейка null без reason`);
      }
    }
    if ('tests' in a) {
      if (!Array.isArray(a.tests) || a.tests.length === 0) issues.push(`${at}: tests — непустой список`);
      else {
        for (const t of a.tests) {
          if (!isTestEntry(t)) issues.push(`${at}: tests — запись ${JSON.stringify(t)} не go:TestRouter* и не tests/router/<спек>.spec.ts`);
        }
      }
    }
  }
  if ('harness' in data) {
    const h = data.harness;
    if (!h || typeof h !== 'object' || Array.isArray(h)) issues.push('harness — объект');
    else {
      for (const k of Object.keys(h)) {
        if (!['go', 'specs', 'grep', 'session'].includes(k)) issues.push(`harness: неизвестный ключ ${JSON.stringify(k)}`);
      }
      if ('go' in h && (!Array.isArray(h.go) || h.go.some((x) => typeof x !== 'string' || !HARNESS_GO.test(x)))) {
        issues.push('harness.go — список «<каталог пакета>:<TestRouterИмя>»');
      }
      if ('specs' in h && (!Array.isArray(h.specs) || h.specs.some((x) => typeof x !== 'string' || !SPEC_PATH.test(x)))) {
        issues.push('harness.specs — список tests/router/<спек>.spec.ts');
      }
      if ('grep' in h && (typeof h.grep !== 'string' || /[\r\n]/.test(h.grep))) {
        issues.push('harness.grep — строка без перевода строки');
      }
      if ('session' in h) {
        const s = h.session;
        if (
          typeof s !== 'string' ||
          !SAFE_REPO_PATH.test(s) ||
          !s.startsWith('scripts/router/') ||
          s.split('/').includes('..') ||
          (root && !fs.existsSync(path.join(root, s)))
        ) {
          issues.push('harness.session — путь к существующему файлу под scripts/router/');
        }
      }
    }
  }
  return issues;
}

/** Карта областей: { version, areas, harness }. Ошибка чтения или схемы — исключение. */
function loadAreas(root) {
  const file = path.join(root, AREAS_FILE);
  const data = JSON.parse(fs.readFileSync(file, 'utf8'));
  const issues = validateAreas(data, root);
  if (issues.length > 0) throw new Error(`${AREAS_FILE}: ${issues.join('; ')}`);
  return { version: data.version, areas: data.areas, harness: data.harness || null };
}

/**
 * Карта областей из текста (версия из git, RTR-14, D-21 фазы 148.1.1): разобранный объект карты
 * после того же validateAreas, что у стража и loadAreas. root — корень для проверки путей
 * harness.session; null — без проверки файлов (ревизия базы или head не в рабочем дереве).
 * Ошибка JSON или схемы — исключение с перечнем нарушений.
 */
function areasFromText(text, root) {
  let data;
  try {
    data = JSON.parse(text);
  } catch (e) {
    throw new Error(`не JSON (${String(e.message ?? e).split('\n', 1)[0]})`);
  }
  const issues = validateAreas(data, root);
  if (issues.length > 0) throw new Error(issues.join('; '));
  return data;
}

/** Значение в каноническом виде: ключи объектов по алфавиту, порядок массивов значим. */
function canonJson(v) {
  if (Array.isArray(v)) return `[${v.map(canonJson).join(',')}]`;
  if (v && typeof v === 'object') {
    return `{${Object.keys(v)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${canonJson(v[k])}`)
      .join(',')}}`;
  }
  return JSON.stringify(v === undefined ? null : v);
}

const listMinus = (a, b) => [...new Set(a.filter((x) => !b.includes(x)))];
const keysOf = (objs) => [...new Set(objs.flatMap((o) => (o && typeof o === 'object' ? Object.keys(o) : [])))].sort();

/**
 * Смысловое сравнение двух проверенных карт областей (база и head; RTR-14, D-21…D-23 фазы
 * 148.1.1). Сравнение по идентификаторам областей и подобластей, а не по тексту:
 *   format    изменения формата: version, набор ключей верхнего уровня, набор ключей,
 *             используемых областями, подобластями или канарейками (строки «что изменилось»)
 *   shared    [{ area, from, to }] — признак shared области, которая есть в обеих картах
 *   paths     [{ pattern, from: [области], to: [области] }] — шаблон убран из областей from и
 *             добавлен в области to (обе карты); перенос — оба списка непусты
 *   tests     [{ area, added, removed }] — записи поля tests области (обе карты)
 *   subareas  [{ area, sub, kind: added|removed|changed, paths: { added, removed },
 *             tests: { added, removed }, baseTests, headTests }]
 *   canary    [{ area, kind: go|spec, from, to }] — тест канарейки (null — нет); у добавленной
 *             области from — null, у удалённой to — null
 *   added, removed  области только в head или только в базе; переименование идентификатора —
 *             удаление плюс добавление (однозначного соответствия нет)
 *   cosmetic  только отображаемое: title, пояснение канарейки (reason) при том же тесте, порядок
 *             ключей и записей — выбор не расширяет
 *   harness   изменён блок harness (обвязка стенда)
 */
function diffAreaMaps(base, head) {
  const out = {
    format: [],
    shared: [],
    paths: [],
    tests: [],
    subareas: [],
    canary: [],
    added: [],
    removed: [],
    cosmetic: [],
    harness: false
  };
  const ba = base.areas;
  const ha = head.areas;
  if (base.version !== head.version) out.format.push(`version ${base.version} → ${head.version}`);
  const setChange = (what, b, h) => {
    if (b.join() === h.join()) return;
    const plus = listMinus(h, b).map((k) => `+${k}`);
    const minus = listMinus(b, h).map((k) => `−${k}`);
    out.format.push(`${what}: ${[...plus, ...minus].join(' ')}`);
  };
  setChange('ключи верхнего уровня', Object.keys(base).sort(), Object.keys(head).sort());
  setChange('ключи областей', keysOf(Object.values(ba)), keysOf(Object.values(ha)));
  const subObjs = (areas) => Object.values(areas).flatMap((a) => Object.values(a.subareas || {}));
  const bSub = subObjs(ba);
  const hSub = subObjs(ha);
  if (bSub.length > 0 && hSub.length > 0) setChange('ключи подобластей', keysOf(bSub), keysOf(hSub));
  setChange('ключи канареек', keysOf(Object.values(ba).map((a) => a.canary)), keysOf(Object.values(ha).map((a) => a.canary)));

  const bNames = Object.keys(ba);
  const hNames = Object.keys(ha);
  const both = bNames.filter((n) => n in ha).sort();
  out.added = hNames.filter((n) => !(n in ba)).sort();
  out.removed = bNames.filter((n) => !(n in ha)).sort();
  if (out.added.length === 0 && out.removed.length === 0 && bNames.join() !== hNames.join()) {
    out.cosmetic.push('порядок областей');
  }

  const moves = new Map();
  const move = (pattern) => {
    if (!moves.has(pattern)) moves.set(pattern, { pattern, from: [], to: [] });
    return moves.get(pattern);
  };
  const canaryOf = (a) => ({ go: (a && a.canary && a.canary.go) || null, spec: (a && a.canary && a.canary.spec) || null });
  for (const name of both) {
    const b = ba[name];
    const h = ha[name];
    for (const p of listMinus(b.paths, h.paths)) move(p).from.push(name);
    for (const p of listMinus(h.paths, b.paths)) move(p).to.push(name);
    if (listMinus(b.paths, h.paths).length === 0 && listMinus(h.paths, b.paths).length === 0 && b.paths.join() !== h.paths.join()) {
      out.cosmetic.push(`область ${name}: порядок paths`);
    }
    const bt = b.tests || [];
    const ht = h.tests || [];
    const tAdd = listMinus(ht, bt);
    const tRem = listMinus(bt, ht);
    if (tAdd.length > 0 || tRem.length > 0) out.tests.push({ area: name, added: tAdd, removed: tRem });
    else if (bt.join() !== ht.join()) out.cosmetic.push(`область ${name}: порядок tests`);
    if (b.shared !== h.shared) out.shared.push({ area: name, from: b.shared, to: h.shared });
    if (b.title !== h.title) out.cosmetic.push(`область ${name}: title`);
    const bc = canaryOf(b);
    const hc = canaryOf(h);
    for (const kind of ['go', 'spec']) {
      if (bc[kind] !== hc[kind]) out.canary.push({ area: name, kind, from: bc[kind], to: hc[kind] });
    }
    if (bc.go === hc.go && bc.spec === hc.spec && (b.canary.reason || '') !== (h.canary.reason || '')) {
      out.cosmetic.push(`область ${name}: пояснение канарейки`);
    }
    if (canonJson(b) === canonJson(h) && JSON.stringify(b) !== JSON.stringify(h)) {
      out.cosmetic.push(`область ${name}: порядок ключей`);
    }
    const bs = b.subareas || {};
    const hs = h.subareas || {};
    for (const sub of [...new Set([...Object.keys(bs), ...Object.keys(hs)])].sort()) {
      const sb = bs[sub];
      const sh = hs[sub];
      if (!sh) {
        out.subareas.push({ area: name, sub, kind: 'removed', paths: { added: [], removed: [...sb.paths] }, tests: { added: [], removed: [...sb.tests] }, baseTests: sb.tests, headTests: [] });
        continue;
      }
      if (!sb) {
        out.subareas.push({ area: name, sub, kind: 'added', paths: { added: [...sh.paths], removed: [] }, tests: { added: [...sh.tests], removed: [] }, baseTests: [], headTests: sh.tests });
        continue;
      }
      const paths = { added: listMinus(sh.paths, sb.paths), removed: listMinus(sb.paths, sh.paths) };
      const tests = { added: listMinus(sh.tests, sb.tests), removed: listMinus(sb.tests, sh.tests) };
      if (paths.added.length + paths.removed.length + tests.added.length + tests.removed.length > 0) {
        out.subareas.push({ area: name, sub, kind: 'changed', paths, tests, baseTests: sb.tests, headTests: sh.tests });
      } else if (canonJson(sb.paths) !== canonJson(sh.paths) || canonJson(sb.tests) !== canonJson(sh.tests)) {
        out.cosmetic.push(`подобласть ${name}/${sub}: порядок записей`);
      }
      if (sb.title !== sh.title) out.cosmetic.push(`подобласть ${name}/${sub}: title`);
    }
    if (Object.keys(bs).length > 0 && Object.keys(hs).length > 0 && Object.keys(bs).join() !== Object.keys(hs).join()) {
      if (listMinus(Object.keys(bs), Object.keys(hs)).length === 0 && listMinus(Object.keys(hs), Object.keys(bs)).length === 0) {
        out.cosmetic.push(`область ${name}: порядок подобластей`);
      }
    }
  }
  for (const name of out.added) {
    const c = canaryOf(ha[name]);
    for (const kind of ['go', 'spec']) if (c[kind]) out.canary.push({ area: name, kind, from: null, to: c[kind] });
  }
  for (const name of out.removed) {
    const c = canaryOf(ba[name]);
    for (const kind of ['go', 'spec']) if (c[kind]) out.canary.push({ area: name, kind, from: c[kind], to: null });
  }
  out.paths = [...moves.values()].sort((x, y) => (x.pattern < y.pattern ? -1 : x.pattern > y.pattern ? 1 : 0));
  out.harness = canonJson(base.harness ?? null) !== canonJson(head.harness ?? null);
  return out;
}

/** Аргументы вызова от открывающей скобки (в тексте без комментариев): [{text, start}] или null. */
function splitArgs(code, open) {
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
    } else if (c === '/' && isRegexStart(code, i)) {
      i = regexEnd(code, i);
    } else if (c === '(' || c === '[' || c === '{') {
      depth++;
    } else if (c === ')' || c === ']' || c === '}') {
      depth--;
      if (depth === 0) {
        const last = code.slice(start, i);
        if (last.trim() || list.length > 0) list.push({ text: last.trim(), start });
        return list;
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

/** n-й (с нуля) аргумент вызова Go в позиции open: строковое значение литерала или null. */
function goStringArg(code, open, n) {
  const args = splitArgs(code, open);
  if (!args || args.length <= n) return null;
  return literalValue(args[n].text);
}

/** Факты тестов одного роутерного Go-файла (без области): [{test, tier, tierReason, slowReason, quarantine, coreMarked}]. */
function goTestFacts(text) {
  const decls = declarations(text);
  const fileKnown = decls.some((d) => d.kind !== 'func' && KNOWN_WITH_SLASH.test(d.code));
  const rows = [];
  for (const d of decls) {
    if (d.kind !== 'func' || d.receiver || !TEST_NAME.test(d.name)) continue;
    const call = (re) => {
      const m = re.exec(d.masked);
      return m ? m.index + m[0].length - 1 : -1;
    };
    const tierAt = call(/\broutertest\.Tier\(/);
    let tier = 'gate';
    let tierReason = null;
    if (tierAt >= 0) {
      const args = splitArgs(d.code, tierAt);
      if (args && args.length >= 2 && /^routertest\.Full$/.test(args[1].text)) {
        tier = 'full';
        tierReason = goStringArg(d.code, tierAt, 2) || null;
      }
    }
    const slowAt = call(/\broutertest\.Slow\(/);
    const quarAt = call(/\broutertest\.Quarantine\(/);
    rows.push({
      test: d.name,
      tier,
      tierReason,
      slowReason: slowAt >= 0 ? goStringArg(d.code, slowAt, 1) || null : null,
      quarantine: quarAt >= 0 ? goStringArg(d.code, quarAt, 1) || '' : null,
      coreMarked: CORE_SPECIFIC.test(d.masked) || KNOWN_WITH_SLASH.test(d.code) || fileKnown
    });
  }
  return rows;
}

const SPEC_FULL_TAG = /\bFULL_TAG\b|['"`]@full['"`]/;

const ANNOTATION_CONST = { 'tier-full': 'TIER_FULL_ANNOTATION', slow: 'SLOW_ANNOTATION' };

/**
 * Описание аннотации заданного типа в тексте деталей test(…): строка, '' (без описания, с
 * нелитеральным описанием) или null. Тип — строковый литерал ('tier-full', 'slow') или
 * константа из lib/known.ts (TIER_FULL_ANNOTATION, SLOW_ANNOTATION).
 */
function annotationOf(seg, type) {
  const typeRe = `(?:(['"\`])${type}\\1|\\b${ANNOTATION_CONST[type]}\\b)`;
  const re = new RegExp(
    `\\{\\s*type:\\s*${typeRe}\\s*,\\s*description:\\s*(['"\`])((?:[^\\\\\\n]|\\\\.)*?)\\2\\s*,?\\s*\\}`
  );
  const m = re.exec(seg);
  if (m) return m[3];
  return new RegExp(`\\btype:\\s*${typeRe}`).test(seg) ? '' : null;
}

/** Факты тестов одного спека (без области): [{test, tier, tierReason, slowReason, quarantine, coreMarked}]. */
function specTestFacts(text) {
  const code = stripTs(text);
  const tagVarCore = /\b(?:const|let)\s+tag\s*=[^;]*?(?:CORE_TAG|CORE_LABEL_TAG|coreTagsOf)/s.test(code);
  const rows = [];
  for (const m of code.matchAll(/(?<![\w.$])test\(/g)) {
    const open = m.index + m[0].length - 1;
    const args = splitArgs(code, open);
    if (!args || args.length === 0) continue;
    const details = args.length >= 3 ? args[1].text : '';
    const body = args[args.length - 1].text;
    const tierReason = annotationOf(details, 'tier-full');
    const slow = annotationOf(details, 'slow');
    const q = /\bquarantine\(\s*(['"`])([^'"`\n]*)\1/.exec(body);
    const tpl = /^`((?:[^`\\]|\\.)*)`$/s.exec(args[0].text);
    rows.push({
      test: literalValue(args[0].text),
      // название-шаблон с подстановками (`axe: тема ${theme}`): для сверки с историей
      titleTemplate: tpl && tpl[1].includes('${') ? tpl[1] : null,
      tier: SPEC_FULL_TAG.test(details) ? 'full' : 'gate',
      tierReason: SPEC_FULL_TAG.test(details) ? tierReason || null : null,
      slowReason: slow === null ? null : slow || null,
      quarantine: q ? q[2] : null,
      coreMarked: SPEC_CORE_TAGGED.test(details) || (tagVarCore && /\{\s*tag\s*[,}]|\btag:\s*tag\b/.test(details))
    });
  }
  return rows;
}

const TIER_CALL = /\broutertest\.(Tier|Slow)\(/g;

/**
 * Нарушения разметки яруса и Slow в тексте одного роутерного Go-файла: [{name, reason}].
 * Метка — вызов routertest.Tier/Slow прямо в теле TestRouter*: в помощнике, объявлении уровня
 * файла или ином Test* её не читают выбор и сводка. Уровень — routertest.Full/Gate литералом,
 * причина — непустой строковый литерал; Full и Slow вместе недопустимы (ярус full — по виду,
 * Slow — долгий тест gate).
 */
function goTierIssues(text) {
  const issues = [];
  for (const d of declarations(text)) {
    const calls = [...d.masked.matchAll(TIER_CALL)];
    if (calls.length === 0) continue;
    const name = d.name || '(объявление уровня файла)';
    if (d.kind !== 'func' || d.receiver || !TEST_NAME.test(d.name)) {
      issues.push({ name, reason: 'routertest.Tier/Slow вне тела TestRouter*: метку читают только из тела теста' });
      continue;
    }
    let full = false;
    let slow = false;
    for (const m of calls) {
      const open = m.index + m[0].length - 1;
      const args = splitArgs(d.code, open);
      if (m[1] === 'Slow') {
        slow = true;
        if (!args || args.length !== 2 || !goStringArg(d.code, open, 1)) {
          issues.push({ name, reason: 'routertest.Slow: причина — непустой строковый литерал' });
        }
        continue;
      }
      if (!args || args.length !== 3 || !/^routertest\.(Full|Gate)$/.test(args[1].text)) {
        issues.push({ name, reason: 'routertest.Tier: уровень — routertest.Full или routertest.Gate литералом' });
        continue;
      }
      if (!goStringArg(d.code, open, 2)) issues.push({ name, reason: 'routertest.Tier: причина — непустой строковый литерал' });
      if (args[1].text === 'routertest.Full') full = true;
    }
    if (full && slow) issues.push({ name, reason: 'ярус full и Slow вместе: full — по виду теста, Slow — долгий тест gate' });
    if (calls.filter((m) => m[1] === 'Tier').length > 1) issues.push({ name, reason: 'routertest.Tier вызван больше одного раза' });
  }
  return issues;
}

/** Нарушения разметки яруса и Slow в тексте одного спека: [{test, reason}]. */
function specTierIssues(text) {
  const code = stripTs(text);
  const issues = [];
  for (const m of code.matchAll(/(?<![\w.$])test\(/g)) {
    const args = splitArgs(code, m.index + m[0].length - 1);
    if (!args || args.length === 0) continue;
    const details = args.length >= 3 ? args[1].text : '';
    const test = literalValue(args[0].text) || args[0].text.slice(0, 60);
    const full = SPEC_FULL_TAG.test(details);
    const fullAnn = annotationOf(details, 'tier-full');
    const slowAnn = annotationOf(details, 'slow');
    if (full && fullAnn === null) issues.push({ test, reason: '@full без аннотации tier-full с причиной' });
    if (!full && fullAnn !== null) issues.push({ test, reason: 'аннотация tier-full без тега @full (FULL_TAG)' });
    if (fullAnn === '') issues.push({ test, reason: 'tier-full: причина — непустой строковый литерал' });
    if (slowAnn === '') issues.push({ test, reason: 'slow: причина — непустой строковый литерал' });
    if (full && slowAnn !== null) issues.push({ test, reason: 'ярус full и slow вместе: full — по виду теста, slow — долгий тест gate' });
  }
  return issues;
}

/**
 * Факты всех роутерных тестов репозитория (по индексу git): массив
 * { kind: 'go'|'pw', file, pkg|null, spec|null, test (имя или null у PW с вычисляемым
 *   названием), area (null — нет строки области), tier: 'gate'|'full', tierReason,
 *   slowReason, quarantine (slug или null), coreMarked }.
 */
function testFacts(root) {
  const rows = [];
  for (const f of routerGoFiles(root)) {
    const { area } = areaOfText(f.text);
    for (const t of goTestFacts(f.text)) rows.push({ kind: 'go', file: f.file, pkg: f.pkg, spec: null, area, ...t });
  }
  for (const file of trackedFiles(root, 'frontend/tests/router/*.spec.ts')) {
    const text = fs.readFileSync(path.join(root, file), 'utf8');
    const { area } = areaOfText(text);
    const spec = file.replace(/^frontend\//, '');
    for (const t of specTestFacts(text)) rows.push({ kind: 'pw', file, pkg: null, spec, area, ...t });
  }
  return rows;
}

/*
 * Читатели данных контрактов: тесты, которые читают файл контракта или testdata прямо или
 * через помощника своего пакета (транзитивно), и спеки, читающие testdata (в том числе через
 * nodeLink из lib/nodes-api.ts). Граф импортов и `go list -deps` этих связей не видят, поэтому
 * области contract-* перечисляют их в поле tests, а страж сверяет список с этим разбором.
 */
const CONTRACT_READS = {
  'contract-sources': {
    go: /\bContractFile\(|\bLinksDir\(|\bInputsDir\(|"testdata"|testdata\//,
    spec: /testdata\/|\bnodeLink\b|sourcesContract/
  },
  'contract-routing': { go: /\bRoutingContractFile\(/, spec: /routingContract/ }
};

/** { '<область contract-*>': Set('go:<Имя>' | 'tests/router/<спек>') } по индексу git. */
function contractReaders(root) {
  const byPkg = new Map();
  for (const f of routerGoFiles(root)) {
    if (!byPkg.has(f.pkg)) byPkg.set(f.pkg, []);
    for (const d of declarations(f.text)) if (d.kind === 'func') byPkg.get(f.pkg).push(d);
  }
  const specs = trackedFiles(root, 'frontend/tests/router/*.spec.ts').map((file) => ({
    spec: file.replace(/^frontend\//, ''),
    code: stripTs(fs.readFileSync(path.join(root, file), 'utf8'))
  }));
  const out = {};
  for (const [area, re] of Object.entries(CONTRACT_READS)) {
    const set = new Set();
    for (const decls of byPkg.values()) {
      const tainted = new Set(decls.filter((d) => re.go.test(d.code)).map((d) => d.name));
      for (let grew = true; grew; ) {
        grew = false;
        for (const d of decls) {
          if (tainted.has(d.name)) continue;
          for (const n of tainted) {
            if (new RegExp('\\b' + n + '\\(').test(d.masked)) {
              tainted.add(d.name);
              grew = true;
              break;
            }
          }
        }
      }
      for (const d of decls) if (!d.receiver && TEST_NAME.test(d.name) && tainted.has(d.name)) set.add('go:' + d.name);
    }
    for (const s of specs) if (re.spec.test(s.code)) set.add(s.spec);
    out[area] = set;
  }
  return out;
}

/** Сравнение имени проверки pw:… истории с названием теста спека (точное, после « > », шаблон). */
function titleMatcher(x) {
  if (x.test !== null) {
    const t = x.test;
    return (check) => check === 'pw:' + t || check.endsWith(' > ' + t);
  }
  if (x.titleTemplate) {
    const re = new RegExp(
      '(?:^pw:|\\s>\\s)' +
        x.titleTemplate
          .split(/\$\{[^}]*\}/)
          .map((p) => p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
          .join('.+') +
        '$'
    );
    return (check) => re.test(check);
  }
  return null;
}

/** История основной копии: Map check → Map core → [секунды] за days дней или null. */
function loadHistory(root, days) {
  let file;
  try {
    const common = execFileSync('git', ['rev-parse', '--path-format=absolute', '--git-common-dir'], {
      cwd: root,
      encoding: 'utf8'
    }).trim();
    file = path.join(path.dirname(common), 'build', 'router', 'history.tsv');
  } catch {
    return null;
  }
  if (!fs.existsSync(file)) return null;
  const since = Date.now() - days * 86400 * 1000;
  const map = new Map();
  for (const line of fs.readFileSync(file, 'utf8').split('\n')) {
    const col = line.split('\t');
    if (col.length < 11) continue;
    const [finished, , , , , , core, check, status, , seconds] = col;
    if (!/^(?:go|pw):/.test(check) || status === 'SKIP' || !/^\d+(?:\.\d+)?$/.test(seconds)) continue;
    if (Date.parse(finished) < since) continue;
    if (!map.has(check)) map.set(check, new Map());
    const byCore = map.get(check);
    if (!byCore.has(core)) byCore.set(core, []);
    byCore.get(core).push(Number(seconds));
  }
  return map;
}

function median(xs) {
  const s = [...xs].sort((a, b) => a - b);
  const m = s.length >> 1;
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

module.exports = {
  analyzeGo,
  contractReaders,
  declarations,
  lex,
  goMarks,
  goViolations,
  specFacts,
  specIssues,
  specViolations,
  routerGoFiles,
  trackedFiles,
  AREAS_FILE,
  loadAreas,
  areasFromText,
  diffAreaMaps,
  validateAreas,
  subareasOf,
  matchGlob,
  areaOfText,
  goTestFacts,
  specTestFacts,
  goTierIssues,
  titleMatcher,
  loadHistory,
  median,
  specTierIssues,
  testFacts
};

if (require.main === module) {
  const argv = process.argv.slice(2);
  if (argv[0] !== '--go') {
    console.error('Usage: node scripts/router/core-marks.cjs --go [пакет ...]');
    process.exit(2);
  }
  const root = path.resolve(__dirname, '..', '..');
  for (const m of goMarks(root, argv.slice(1))) console.log(`${m.pkg}\t${m.test}\t${m.reason}`);
}
