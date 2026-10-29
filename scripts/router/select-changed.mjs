#!/usr/bin/env node
// Выбор проверок на роутерах по diff (make router-test SUITE=changed, D-25).
//
// Что выбирается:
//   Go         роутерные пакеты (каталоги с `_test.go`, у которого первая строка
//              `//go:build router`), затронутые изменением: сам пакет изменён, либо в его
//              зависимостях (`go list -tags router -test -deps`) есть изменённый пакет.
//   Playwright роутерные спеки frontend/tests/router/*.spec.ts, затронутые страницы
//              считаются по графу импортов frontend/src (логика scripts/router/import-graph.mjs):
//              спек выбирается, если открывает затронутый маршрут (`#/<маршрут>` в тексте
//              или `// e2e-pages: …`). `// e2e-pages: *` — спек обходит все страницы: он
//              выбирается при любом затронутом маршруте и сужается до них через --grep.
//              core-switch.spec.ts не выбирается: его запускает матрица ядер.
//
// При сомнении выбор только расширяется до полного (D-25): неизвестный файл, небезопасное
// имя, ошибка разбора, нет базы сравнения — RT_SELECT_ALL=1. Данные роутерных тестов
// (`testdata/`, контракт источников frontend/src/lib/sourcesContract.json) — тоже полный выбор.
//
// Вывод (stdout) — строки для eval в sh, значения в одинарных кавычках и только из
// безопасных символов (T-144.1-23):
//   RT_SELECT_ALL=0|1
//   RT_GO_PKGS='./internal/services ./internal/utils/xtables'
//   RT_SPECS='tests/router/pages.spec.ts'
//   RT_PW_GREP='^(?!.*page:(?!(?:logs|dashboard) ))'
//   RT_GO_CORE_PKGS='./internal/handlers ./internal/services'
//   RT_GO_CORE_RUN='^(TestRouterA|TestRouterB)$'
//   RT_SPECS_CORE='tests/router/pages.spec.ts'
//   RT_KERNEL_TOUCHED=0|1
// Строки 5–7 — тесты выбора, зависящие от ядра (D-01, D-03, D-04); по ним run.sh решает,
// нужна ли вторая ветвь (второе ядро):
//   RT_GO_CORE_PKGS  выбранные пакеты, где core-marks.cjs нашёл размеченные тесты (core,
//                    known-core); при полном выборе — все такие пакеты
//   RT_GO_CORE_RUN   выражение -test.run по именам этих тестов; пусто, если размеченных нет
//   RT_SPECS_CORE    выбранные спеки, где `playwright test --list` с отбором @core (вместе
//                    с сужением RT_PW_GREP) находит тест; список не получен — все выбранные
//   RT_KERNEL_TOUCHED  1, если изменён код запуска или перезапуска ядра либо watchdog
//                    (KERNEL_PATHS, D-07) или выбор полный по любой причине; иначе 0
// Сбой расчёта ядра не роняет выбор: списки расширяются (все пакеты, все спеки), не сужаются.
// Причины выбора — в stderr при --explain.
//
// Использование (из корня репозитория):
//   node scripts/router/select-changed.mjs [--base <ref>] [--explain]
//   node scripts/router/select-changed.mjs --files a,b,c [--explain]   # без git
//
// RT_PW_GREP: для названий вида `page:<маршрут> theme:… vp:…` оставляет только затронутые
// маршруты. Форма с отрицательным просмотром вперёд, а не `page:(a|b) `, чтобы тесты без
// префикса `page:` (другие сценарии в выбранных спеках) не отфильтровывались.

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import coreMarks from './core-marks.cjs';

const { goMarks, analyzeGo } = coreMarks;

// Изменённые файлы текущего выбора: goMarks читает только индекс, новые тесты он не видит
let changedFiles = [];

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const SPEC_DIR = 'frontend/tests/router';

// Только безопасные имена путей: значения уходят в eval и в командную строку.
const SAFE_PATH = /^[A-Za-z0-9_.][A-Za-z0-9_./-]*$/;
const SAFE_ROUTE = /^[a-z][a-z0-9_-]*$/;

// Обвязка и сборка: изменение требует полного набора.
const HARNESS_ALL = [
  /^scripts\/router\//,
  /^Makefile$/,
  /^frontend\/playwright\.router\.config\.ts$/,
  /^frontend\/tests\/router\/lib\//,
  /^frontend\/tests\/router\/global-(setup|teardown)\.ts$/,
  /^frontend\/package\.json$/,
  /^frontend\/package-lock\.json$/,
  /^go\.mod$/,
  /^go\.sum$/
];

// Код запуска или перезапуска ядра и watchdog (D-07): изменение даёт RT_KERNEL_TOUCHED=1 —
// run.sh берёт полное окно смоука. `_test.go` не считаются. Файл при этом разбирается как
// обычно, признак лишь добавляется к выбору.
const KERNEL_PATHS = [
  /^internal\/services\/kernel[\w]*\.go$/,
  /^internal\/services\/watchdog\.go$/,
  /^internal\/services\/xkeen[\w]*\.go$/,
  /^internal\/handlers\/kernel[\w]*\.go$/,
  /^internal\/handlers\/watchdog\.go$/,
  /^internal\/handlers\/service\.go$/,
  // перезапуск запущенного ядра после «Применить», его подтверждение и флаг перезапуска
  /^internal\/services\/configlayer\/(pipeline|restart|flag)\.go$/,
  // перезапуск Mihomo из обработчика (kernel_restarting)
  /^internal\/handlers\/mihomo\.go$/,
  // запуск служб и watchdog
  /^cmd\/xcp\/[\w]+\.go$/
];

// Данные роутерных тестов: их получают Go-тесты через окружение go-suite.sh (XCP_RT_LINKS,
// XCP_RT_INPUTS, XCP_RT_CONTRACT, XCP_RT_ROUTING_CONTRACT), а спеки — через lib/nodes-api.ts. Граф импортов и
// `go list -deps` этих связей не видят, поэтому правка данных даёт полный выбор (D-25).
// Новый путь данных для тестов добавлять сюда (см. комментарий в go-suite.sh).
const TEST_DATA_ALL = [
  /(^|\/)testdata\//,
  /^frontend\/src\/lib\/sourcesContract\.json$/,
  /^frontend\/src\/lib\/routingContract\.json$/
];

// Ничего не добавляют (смоук идёт всё равно): документация, CI, хуки, служебное.
const IGNORED = [
  /\.md$/,
  /^\.github\//,
  /^\.githooks\//,
  /^\.claude\//,
  /^\.planning\//,
  /^docs\//,
  /^LICENSE/,
  /^\.gitignore$/,
  /^\.golangci\.ya?ml$/,
  // уборка ПК и статическая проверка CI на роутер не влияют (D-25)
  /^scripts\/(tidy|tidy-lib)\.sh$/,
  /^scripts\/check-report-paths\.js$/,
  // Playwright-спеки с подменой сети и юнит-тесты фронтенда на роутерах не запускаются
  /^frontend\/tests\/(?!router\/)/,
  /^frontend\/src\/.*\.test\.ts$/,
  /^frontend\/scripts\//,
  /^frontend\/\.prettier/,
  /^frontend\/eslint\.config\./,
  /^frontend\/\.gitignore$/
];

// Файлы frontend/ вне src/, меняющие сборку панели: все роутерные спеки (не Go).
const FRONTEND_ALL = [
  /^frontend\/vite\.config\.ts$/,
  /^frontend\/svelte\.config\.js$/,
  /^frontend\/tsconfig[\w.-]*\.json$/,
  /^frontend\/index\.html$/,
  /^frontend\/public\//
];

function git(args) {
  return execFileSync('git', args, { cwd: ROOT, encoding: 'utf8' });
}

function modulePath() {
  const m = readFileSync(path.join(ROOT, 'go.mod'), 'utf8').match(/^module\s+(\S+)/m);
  if (!m) throw new Error('go.mod: не найдена строка module');
  return m[1];
}

/** Каталоги с роутерными тестами: `_test.go`, первая строка которого `//go:build router`. */
function routerPackages() {
  const found = new Set();
  const walk = (dir) => {
    for (const dirent of readdirSync(path.join(ROOT, dir), {
      withFileTypes: true
    })) {
      const entry = dirent.name;
      const rel = `${dir}/${entry}`;
      if (dirent.isDirectory()) {
        if (entry !== 'node_modules' && entry !== 'testdata') walk(rel);
      } else if (entry.endsWith('_test.go')) {
        const first = readFileSync(path.join(ROOT, rel), 'utf8').split('\n', 1)[0].trim();
        if (first === '//go:build router') found.add(dir);
      }
    }
  };
  for (const top of ['internal', 'cmd']) if (existsSync(path.join(ROOT, top))) walk(top);
  return [...found].sort();
}

/** Ближайший каталог с .go-файлами для изменённого файла (в том числе удалённого). */
function goDirOf(file) {
  let dir = path.posix.dirname(file);
  if (dir === '.') return '.';
  while (dir !== '.' && dir !== '') {
    const abs = path.join(ROOT, dir);
    if (existsSync(abs) && statSync(abs).isDirectory()) {
      if (readdirSync(abs).some((f) => f.endsWith('.go'))) return dir;
    }
    dir = path.posix.dirname(dir);
  }
  return null;
}

/** Все пакеты, от которых зависит роутерный пакет (импорт-пути), по двум архитектурам стендов. */
function depsOf(pkgDir) {
  const out = new Set();
  const variants = [
    { GOARCH: 'arm64', GOMIPS: '' },
    { GOARCH: 'mipsle', GOMIPS: 'softfloat' }
  ];
  for (const v of variants) {
    const text = execFileSync(
      'go',
      ['list', '-tags', 'router', '-test', '-deps', '-f', '{{.ImportPath}}', `./${pkgDir}`],
      {
        cwd: ROOT,
        encoding: 'utf8',
        env: { ...process.env, CGO_ENABLED: '0', GOOS: 'linux', ...v }
      }
    );
    for (const line of text.split('\n')) {
      const p = line.trim().replace(/ \[.*\]$/, '');
      if (p) out.add(p);
    }
  }
  return out;
}

function listRouterSpecs() {
  const dir = path.join(ROOT, SPEC_DIR);
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((f) => /^[\w.-]+\.spec\.ts$/.test(f) && f !== 'core-switch.spec.ts')
    .sort()
    .map((f) => `tests/router/${f}`);
}

function changedFromGit(base) {
  let from = base;
  if (!from) {
    from = git(['merge-base', 'origin/main', 'HEAD']).trim();
  }
  const files = git(['diff', '--name-only', '--no-renames', from]).split('\n').filter(Boolean);
  const status = git(['status', '--porcelain', '--untracked-files=all'])
    .split('\n')
    .filter(Boolean)
    .map((l) => l.slice(3));
  for (const s of status) {
    // переименование «старое -> новое»: значимы оба имени
    for (const part of s.split(' -> ')) files.push(part);
  }
  return [...new Set(files.map((f) => path.posix.normalize(f)))];
}

/** Решение по списку изменённых файлов. */
async function select(changed) {
  const reasons = [];
  const routerPkgs = routerPackages();
  const specsAll = listRouterSpecs();
  const res = {
    all: false,
    goPkgs: new Set(),
    specs: new Set(),
    grepRoutes: null, // null — без сужения
    kernelTouched: false, // затронут код запуска ядра или watchdog (KERNEL_PATHS)
    reasons
  };
  const escalate = (why) => {
    res.all = true;
    reasons.push(`всё: ${why}`);
  };

  const touchedSrc = [];
  const touchedSpecs = new Set();
  const goChanged = new Set(); // каталоги изменённых Go-пакетов
  const goTestDirs = new Set(); // каталоги с изменёнными _test.go
  let pwAll = false;
  let goAll = false;

  for (const raw of changed) {
    const file = raw;
    if (!SAFE_PATH.test(file)) {
      escalate(`небезопасное имя файла ${JSON.stringify(file)}`);
      continue;
    }
    // Признак не отменяет разбор файла: он идёт дальше как обычно (выбор пакетов)
    if (!file.endsWith('_test.go') && KERNEL_PATHS.some((re) => re.test(file))) {
      res.kernelTouched = true;
      reasons.push(`${file}: код запуска или перезапуска ядра либо watchdog — полное окно смоука`);
    }
    if (HARNESS_ALL.some((re) => re.test(file))) {
      escalate(`${file}: обвязка проверки или сборка`);
      continue;
    }
    if (TEST_DATA_ALL.some((re) => re.test(file))) {
      escalate(
        `${file}: данные роутерных тестов (Go-тесты получают их через окружение go-suite.sh, спеки — через lib/nodes-api.ts)`
      );
      continue;
    }
    if (IGNORED.some((re) => re.test(file))) {
      reasons.push(`${file}: на роутерные проверки не влияет`);
      continue;
    }
    if (/^frontend\/tests\/router\/[\w.-]+\.spec\.ts$/.test(file)) {
      if (file.endsWith('/core-switch.spec.ts')) {
        reasons.push(`${file}: спек матрицы ядер, запускается в каждой ветви`);
      } else if (existsSync(path.join(ROOT, file))) {
        touchedSpecs.add(`tests/router/${path.posix.basename(file)}`);
        reasons.push(`${file}: изменён сам спек`);
      } else {
        reasons.push(`${file}: спек удалён`);
      }
      continue;
    }
    if (FRONTEND_ALL.some((re) => re.test(file))) {
      pwAll = true;
      reasons.push(`${file}: влияет на сборку панели — все роутерные спеки`);
      continue;
    }
    if (file.startsWith('frontend/src/')) {
      touchedSrc.push(file.slice('frontend/'.length));
      continue;
    }
    if (file.startsWith('frontend/')) {
      escalate(`${file}: неизвестный файл фронтенда`);
      continue;
    }
    if (/^(internal|cmd)\//.test(file) || /^[\w.-]+\.go$/.test(file)) {
      const dir = goDirOf(file);
      if (dir === null) {
        // Данные без пакета рядом (например, наборы для тестов другого пакета): владельца
        // не определить — все роутерные пакеты
        goAll = true;
        reasons.push(`${file}: нет пакета Go рядом — все роутерные пакеты`);
      } else if (file.endsWith('_test.go')) {
        goTestDirs.add(dir);
        reasons.push(`${file}: тест пакета ${dir}`);
      } else {
        goChanged.add(dir);
        reasons.push(`${file}: код пакета ${dir}`);
      }
      continue;
    }
    escalate(`${file}: неизвестный файл`);
  }

  // --- Go ---------------------------------------------------------------------
  if (!res.all && goAll) {
    routerPkgs.forEach((p) => res.goPkgs.add(p));
  }
  if (!res.all) {
    for (const dir of goTestDirs) {
      if (routerPkgs.includes(dir)) {
        res.goPkgs.add(dir);
        reasons.push(`${dir}: изменён тест роутерного пакета — пакет выбран`);
      } else {
        reasons.push(`${dir}: изменён обычный тест, роутерных тестов в пакете нет`);
      }
    }
    if (goChanged.size > 0) {
      const mod = modulePath();
      const changedImports = new Set([...goChanged].map((d) => (d === '.' ? mod : `${mod}/${d}`)));
      for (const pkg of routerPkgs) {
        if (res.goPkgs.has(pkg)) continue;
        const own = `${mod}/${pkg}`;
        const deps = depsOf(pkg);
        const hit = [...changedImports].find((p) => p === own || deps.has(p));
        if (hit) {
          res.goPkgs.add(pkg);
          reasons.push(`${pkg}: зависит от изменённого пакета ${hit.slice(mod.length + 1) || '.'}`);
        }
      }
      if (![...routerPkgs].some((p) => res.goPkgs.has(p))) {
        reasons.push('Go: ни один роутерный пакет не затронут');
      }
    }
  }

  // --- Playwright -------------------------------------------------------------
  if (!res.all) {
    const affected = new Set();
    let model = null;
    let specRoutes = null;
    if (touchedSrc.length > 0) {
      const mod = await import(pathToFileURL(path.join(ROOT, 'scripts/router/import-graph.mjs')));
      specRoutes = mod.specRoutes;
      model = mod.buildModel(path.join(ROOT, 'frontend'));
      for (const rel of touchedSrc) {
        const abs = path.join(ROOT, 'frontend', rel);
        if (!existsSync(abs)) {
          reasons.push(`frontend/${rel}: удалён`);
          continue;
        }
        if (model.shell.has(abs)) {
          pwAll = true;
          reasons.push(`frontend/${rel}: оболочка приложения — все роутерные спеки`);
          continue;
        }
        const routes = [...model.pageFiles].filter(([, files]) => files.has(abs)).map(([r]) => r);
        if (routes.length === 0) {
          reasons.push(`frontend/${rel}: не импортируется приложением`);
          continue;
        }
        routes.forEach((r) => affected.add(r));
        reasons.push(`frontend/${rel}: страницы ${routes.join(', ')}`);
      }
    }

    if (pwAll) {
      specsAll.forEach((s) => res.specs.add(s));
    } else if (affected.size > 0 || touchedSpecs.size > 0) {
      const star = new Set();
      for (const spec of specsAll) {
        const text = readFileSync(path.join(ROOT, 'frontend', spec), 'utf8');
        const ann = [...text.matchAll(/\/\/\s*e2e-pages:\s*([^\n]+)/g)].flatMap((m) =>
          m[1].split(',').map((r) => r.trim())
        );
        const isStar = ann.includes('*');
        if (isStar) star.add(spec);
        if (touchedSpecs.has(spec)) {
          res.specs.add(spec);
          continue;
        }
        if (affected.size === 0) continue;
        if (isStar) {
          res.specs.add(spec);
          reasons.push(`${spec}: обходит все страницы — сужается до затронутых`);
          continue;
        }
        const routes = specRoutes(text, model.routes);
        if (routes === null) {
          res.specs.add(spec);
          reasons.push(`${spec}: маршруты не определены — запускается всегда`);
        } else if ([...routes].some((r) => affected.has(r))) {
          res.specs.add(spec);
          reasons.push(`${spec}: открывает затронутые страницы`);
        }
      }
      // Сужение по маршрутам, только если ни один спек обхода не изменён сам
      const walkTouched = [...touchedSpecs].some((s) => star.has(s));
      const walkSelected = [...res.specs].some((s) => star.has(s));
      if (walkSelected && !walkTouched && affected.size > 0) {
        const routes = [...affected].filter((r) => SAFE_ROUTE.test(r)).sort();
        if (routes.length !== affected.size) {
          reasons.push('маршрут с небезопасным именем — обход идёт целиком');
        } else {
          res.grepRoutes = routes;
        }
      }
    }
  }

  return { res, routerPkgs, specsAll };
}

/** Размеченные тесты Go в пакетах `./internal/…`: { pkgs, run, rows } (rows — для причин). */
function goCoreOf(goPkgs) {
  const want = goPkgs.map((p) => p.replace(/^\.\//, ''));
  // Пустой список goMarks понимает как «все пакеты»: пустой выбор отдаём сами
  const rows = want.length > 0 ? goMarks(ROOT, want) : [];
  // Новые (ещё не в индексе) и изменённые роутерные тесты читаем с диска сами
  const seen = new Set(rows.map((r) => `${r.pkg}\t${r.test}`));
  for (const file of changedFiles) {
    const pkg = path.posix.dirname(file);
    if (!want.includes(pkg) || !/_test\.go$/.test(file) || !SAFE_PATH.test(file)) continue;
    const abs = path.join(ROOT, file);
    if (!existsSync(abs)) continue;
    const text = readFileSync(abs, 'utf8');
    if (text.split('\n', 1)[0].trim() !== '//go:build router') continue;
    for (const m of analyzeGo(text).marks) {
      if (seen.has(`${pkg}\t${m.test}`)) continue;
      seen.add(`${pkg}\t${m.test}`);
      rows.push({ pkg, test: m.test, reason: m.reason, file });
    }
  }
  const names = [
    ...new Set(rows.map((r) => r.test).filter((t) => /^TestRouter[A-Za-z0-9_]+$/.test(t)))
  ].sort();
  const withMarks = new Set(rows.map((r) => r.pkg));
  return {
    pkgs: want.filter((p) => withMarks.has(p)).map((p) => `./${p}`),
    run: names.length > 0 ? `^(${names.join('|')})$` : '',
    rows
  };
}

/** Выражение отбора тегов ядра для Playwright; то же строит pw-suite.sh при RT_PW_CORE_ONLY=1. */
function coreGrep(grep) {
  return grep === '' ? '@core' : `(?=.*@core)${grep}`;
}

/** Выбранные спеки, в которых есть хотя бы один тест с тегом ядра: { specs, reasons }. */
function pwCoreOf(specs, grep) {
  const reasons = [];
  if (specs.length === 0) return { specs: [], reasons };
  const fallback = (why) => {
    reasons.push(
      `ядро: список Playwright не получен (${why}) — вторая ветвь берёт все выбранные спеки`
    );
    return { specs, reasons };
  };
  const frontend = path.join(ROOT, 'frontend');
  const bin = path.join(frontend, 'node_modules', '.bin', 'playwright');
  if (!existsSync(bin)) return fallback('нет frontend/node_modules/.bin/playwright');
  // Конфигу переменные стенда не нужны (--list), адрес и пароль панели в окружение не передаём
  const env = Object.fromEntries(
    Object.entries(process.env).filter(([k]) => !k.startsWith('XCP_'))
  );
  let json;
  try {
    let text;
    try {
      text = execFileSync(
        bin,
        [
          'test',
          '-c',
          'playwright.router.config.ts',
          '--list',
          '--reporter=json',
          '--grep',
          coreGrep(grep),
          ...specs
        ],
        {
          cwd: frontend,
          env,
          encoding: 'utf8',
          timeout: 60000,
          maxBuffer: 64 * 1024 * 1024
        }
      );
    } catch (e) {
      // «No tests found» — код 1, но это законный ответ «тестов с тегом нет»: JSON есть в stdout
      if (typeof e.stdout !== 'string' || e.stdout === '') throw e;
      text = e.stdout;
    }
    json = JSON.parse(text);
  } catch (e) {
    return fallback(
      String(e.message ?? e)
        .split('\n', 1)[0]
        .slice(0, 120)
    );
  }
  const errors = json && Array.isArray(json.errors) ? json.errors : [];
  const noTests = errors.every((er) => /No tests found/.test(String(er.message ?? '')));
  if (!json || !Array.isArray(json.suites) || !noTests) {
    return fallback('ответ с ошибками или неожиданной формы');
  }
  if (errors.length > 0 && json.suites.length > 0) {
    return fallback('ответ с ошибками или неожиданной формы');
  }
  const counts = new Map();
  const walk = (suite) => {
    for (const sp of suite.specs ?? []) {
      const key = `tests/router/${path.posix.basename(String(sp.file ?? ''))}`;
      counts.set(key, (counts.get(key) ?? 0) + (sp.tests ?? []).length);
    }
    for (const child of suite.suites ?? []) walk(child);
  };
  json.suites.forEach(walk);
  const found = specs.filter((s) => (counts.get(s) ?? 0) > 0);
  for (const s of found) reasons.push(`ядро: pw ${s} — тестов с тегом ядра ${counts.get(s)}`);
  return { specs: found, reasons };
}

/**
 * Тесты выбора, зависящие от ядра (D-01, D-03, D-04): Go — размеченные core/known-core,
 * Playwright — спеки с тегом @core. Ошибка расчёта не роняет выбор, а расширяет списки
 * до безопасных значений: все выбранные пакеты (без отбора по именам) и все выбранные спеки.
 */
function coreSelection(goPkgs, specs, grep) {
  const reasons = [];
  let go;
  try {
    go = goCoreOf(goPkgs);
    for (const r of go.rows) reasons.push(`ядро: go ./${r.pkg} ${r.test} (${r.reason})`);
  } catch (e) {
    reasons.push(`ядро: разметка Go не получена (${e.message}) — берутся все выбранные пакеты`);
    go = { pkgs: goPkgs, run: '', rows: [] };
  }
  let pw;
  try {
    pw = pwCoreOf(specs, grep);
  } catch (e) {
    reasons.push(`ядро: отбор Playwright не удался (${e.message}) — берутся все выбранные спеки`);
    pw = { specs, reasons: [] };
  }
  reasons.push(...pw.reasons);
  return { goPkgs: go.pkgs, goRun: go.run, specs: pw.specs, reasons };
}

/** Безопасные значения ядра для отката на полный выбор: все пакеты с размеченными тестами и все спеки. */
function coreSafeAll(routerPkgs, specsAll, reasons) {
  const all = routerPkgs.map((p) => `./${p}`);
  try {
    const go = goCoreOf(all);
    return { goPkgs: go.pkgs, goRun: go.run, specs: specsAll, reasons };
  } catch {
    return { goPkgs: all, goRun: '', specs: specsAll, reasons };
  }
}

function emit(res, routerPkgs, specsAll) {
  const quote = (s) => `'${s}'`;
  const goList = (set) => routerPkgs.filter((p) => set.has(p)).map((p) => `./${p}`);
  let all = res.all;
  let go;
  let specs;
  let grep = '';
  if (all) {
    go = routerPkgs.map((p) => `./${p}`);
    specs = specsAll;
  } else {
    go = goList(res.goPkgs);
    specs = specsAll.filter((s) => res.specs.has(s));
    if (res.grepRoutes) {
      grep = `^(?!.*page:(?!(?:${res.grepRoutes.join('|')}) ))`;
    }
  }
  const okList = (l) => l.every((x) => SAFE_PATH.test(x.replace(/^\.\//, '')));
  const okGrep = (g) => /^[A-Za-z0-9_|()?!^*.: -]*$/.test(g);
  const okRun = (r) => /^[A-Za-z0-9_|^$()]*$/.test(r);
  let core = coreSelection(go, specs, grep);
  // Последняя проверка безопасности вывода: иначе — всё
  if (
    !okList(go) ||
    !okList(specs) ||
    !okGrep(grep) ||
    !okList(core.goPkgs) ||
    !okList(core.specs) ||
    !okRun(core.goRun)
  ) {
    all = true;
    go = routerPkgs.map((p) => `./${p}`);
    specs = specsAll;
    grep = '';
    core = coreSafeAll(routerPkgs, specsAll, core.reasons);
    res.reasons.push('всё: значение выбора не прошло проверку безопасных символов');
  }
  res.reasons.push(...core.reasons);
  return [
    `RT_SELECT_ALL=${all ? 1 : 0}`,
    `RT_GO_PKGS=${quote(go.join(' '))}`,
    `RT_SPECS=${quote(specs.join(' '))}`,
    `RT_PW_GREP=${quote(grep)}`,
    `RT_GO_CORE_PKGS=${quote(core.goPkgs.join(' '))}`,
    `RT_GO_CORE_RUN=${quote(core.goRun)}`,
    `RT_SPECS_CORE=${quote(core.specs.join(' '))}`,
    `RT_KERNEL_TOUCHED=${all || res.kernelTouched ? 1 : 0}`
  ].join('\n');
}

async function main(argv) {
  const args = { base: null, files: null, explain: false };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--base') args.base = argv[++i];
    else if (a === '--files') args.files = (argv[++i] ?? '').split(',').filter(Boolean);
    else if (a === '--explain') args.explain = true;
    else {
      console.error(`select-changed: неизвестный аргумент ${a}`);
      process.exit(2);
    }
  }

  let out;
  let reasons;
  try {
    const changed = args.files
      ? args.files.map((f) => path.posix.normalize(f))
      : changedFromGit(args.base);
    changedFiles = changed;
    const { res, routerPkgs, specsAll } = await select(changed);
    out = emit(res, routerPkgs, specsAll);
    reasons = res.reasons;
  } catch (e) {
    // Не смогли посчитать — безопасный вариант: всё
    const orEmpty = (fn) => {
      try {
        return fn();
      } catch {
        return [];
      }
    };
    const res = { all: true, reasons: [`всё: выбор не удался (${e.message})`] };
    out = emit(res, orEmpty(routerPackages), orEmpty(listRouterSpecs));
    reasons = res.reasons;
  }

  if (args.explain) {
    const first = out.split('\n')[0];
    console.error(`выбор: ${first}`);
    for (const line of out.split('\n').slice(1)) console.error(`  ${line.replace(/^RT_/, '')}`);
    console.error('причины:');
    for (const r of reasons) console.error(`  - ${r}`);
  }
  console.log(out);
}

await main(process.argv.slice(2));
