#!/usr/bin/env node
/*
 * check-no-simulations.js — гейт «тесты только на роутере».
 *
 * Проект проверяется на живых роутерах (раздел «Тестирование» в AGENTS.md):
 * тесты вне роутера — юнит-тесты Go и vitest, Playwright на подменённых
 * ответах, shell-тесты, пороги покрытия, эталонные файлы — запрещены. Гейт
 * читает индекс git (git ls-files, git show :путь), а не рабочее дерево, чтобы
 * staged-нарушение не проходило pre-commit. Исключений и списка разрешённых
 * путей нет.
 *
 * Правила:
 *   1. *_test.go обязан иметь `//go:build router` до строки `package`.
 *   2. vitest: файлы *.test.{ts,js,mjs,cjs,tsx,jsx}, vitest в frontend/package.json,
 *      блок `test:` в frontend/vite.config.ts.
 *   3. Playwright: *.spec.{ts,js} только в frontend/tests/router/; в них нельзя
 *      подменять сеть (.route(, .routeFromHAR(, .fulfill(, .unroute().
 *   4. Shell-тесты *_test.sh и *_test.bash.
 *   5. Порог покрытия: scripts/check-coverage.sh и -coverprofile в COVERAGE_FILES.
 *   6. Эталонные файлы: testdata/golden/ и *.golden.
 *
 * Usage: node scripts/check-no-simulations.js
 */
const { execFileSync } = require('child_process');
const path = require('path');

const root = path.resolve(__dirname, '..');

// Файлы, где запрещён -coverprofile. Список расширяется правкой этой константы.
const COVERAGE_FILES = ['Makefile'];

const VITEST_SUFFIX = /\.test\.(ts|js|mjs|cjs|tsx|jsx)$/;
const SPEC_SUFFIX = /\.spec\.(ts|js)$/;
const ROUTER_SPEC_DIR = 'frontend/tests/router/';
const NETWORK_MOCK_CALL = /\.(route|routeFromHAR|routeWebSocket|fulfill|unroute)\(/;

function git(args) {
  return execFileSync('git', args, { cwd: root, encoding: 'utf8', maxBuffer: 256 * 1024 * 1024 });
}

// Содержимое файла из индекса (версия, которая попадёт в коммит).
function indexed(file) {
  return git(['show', ':' + file]);
}

const files = git(['ls-files', '-z']).split('\0').filter(Boolean);
const fileSet = new Set(files);
const violations = [];

function violate(file, rule) {
  violations.push(`${file}: ${rule}`);
}

// Выражение `//go:build` допустимо, только если требует тег router.
function requiresRouterTag(expr) {
  const e = expr.trim();
  return e === 'router' || /^router\s+&&\s+[^|]*$/.test(e);
}

function goTestHasRouterTag(src) {
  for (const line of src.split('\n')) {
    const t = line.trim();
    if (/^package\s/.test(t)) return false;
    const m = t.match(/^\/\/go:build\s+(.+)$/);
    if (m && requiresRouterTag(m[1])) return true;
  }
  return false;
}

for (const file of files) {
  // 1. Go-тесты без тега router.
  if (file.endsWith('_test.go') && !goTestHasRouterTag(indexed(file))) {
    violate(file, 'Go-тест без `//go:build router` до строки package');
  }

  // 2. vitest-файлы.
  if (VITEST_SUFFIX.test(file)) {
    violate(file, 'файл vitest (*.test.*)');
  }

  // 3. Playwright вне роутерного каталога и подмена сети внутри него.
  if (SPEC_SUFFIX.test(file) && !file.startsWith(ROUTER_SPEC_DIR)) {
    violate(file, 'Playwright-спек вне frontend/tests/router/');
  }
  if (file.startsWith(ROUTER_SPEC_DIR) && /\.(ts|js|mjs|cjs)$/.test(file)) {
    const lines = indexed(file).split('\n');
    lines.forEach((line, i) => {
      if (NETWORK_MOCK_CALL.test(line)) {
        violate(`${file}:${i + 1}`, `подмена сети в роутерном спеке (${line.trim()})`);
      }
    });
  }

  // 4. Shell-тесты.
  if (/_test\.(sh|bash)$/.test(file)) {
    violate(file, 'shell-тест (*_test.sh|*_test.bash)');
  }

  // 6. Эталонные файлы.
  if (file.includes('/testdata/golden/') || file.startsWith('testdata/golden/') || file.endsWith('.golden')) {
    violate(file, 'эталонный файл (golden)');
  }
}

// 2. Зависимости и скрипты vitest, блок test: в конфиге vite.
if (fileSet.has('frontend/package.json')) {
  let pkg = null;
  try {
    pkg = JSON.parse(indexed('frontend/package.json'));
  } catch (e) {
    violate('frontend/package.json', 'не разбирается как JSON: ' + e.message);
  }
  if (pkg) {
    for (const section of ['dependencies', 'devDependencies', 'peerDependencies', 'optionalDependencies']) {
      for (const name of Object.keys(pkg[section] || {})) {
        if (name === 'vitest' || name.startsWith('@vitest/')) {
          violate('frontend/package.json', `зависимость ${name} (${section})`);
        }
      }
    }
    for (const [name, cmd] of Object.entries(pkg.scripts || {})) {
      if (/vitest/.test(cmd)) violate('frontend/package.json', `скрипт ${name} запускает vitest`);
    }
  }
}
if (fileSet.has('frontend/vite.config.ts') && /^ {2}test\s*:/m.test(indexed('frontend/vite.config.ts'))) {
  violate('frontend/vite.config.ts', 'блок `test:` (конфиг vitest)');
}

// 5. Порог покрытия.
if (fileSet.has('scripts/check-coverage.sh')) {
  violate('scripts/check-coverage.sh', 'порог покрытия');
}
for (const f of COVERAGE_FILES) {
  if (fileSet.has(f) && /-coverprofile/.test(indexed(f))) {
    violate(f, 'сбор покрытия (-coverprofile)');
  }
}

if (violations.length > 0) {
  console.error('тесты вне роутера запрещены (D-07). Проект проверяется на роутерах, см. «Тестирование» в AGENTS.md:');
  for (const v of violations) console.error('  ' + v);
  process.exit(1);
}
console.log('нет симуляций');
