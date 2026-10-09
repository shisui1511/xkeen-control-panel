#!/usr/bin/env node
// Выбор проверок на роутерах по diff (make router-test SUITE=changed, D-25).
//
// Что выбирается:
//   Go         роутерные пакеты (каталоги с `_test.go`, у которого первая строка
//              `//go:build router`), затронутые изменением: сам пакет изменён, либо в его
//              зависимостях (`go list -tags router -test -deps`) есть изменённый пакет.
//   Playwright роутерные спеки frontend/tests/router/*.spec.ts, затронутые страницы
//              считаются по графу импортов frontend/src (логика frontend/scripts/select-e2e.mjs):
//              спек выбирается, если открывает затронутый маршрут (`#/<маршрут>` в тексте
//              или `// e2e-pages: …`). `// e2e-pages: *` — спек обходит все страницы: он
//              выбирается при любом затронутом маршруте и сужается до них через --grep.
//              core-switch.spec.ts не выбирается: его запускает матрица ядер.
//
// При сомнении выбор только расширяется до полного (D-25): неизвестный файл, небезопасное
// имя, ошибка разбора, нет базы сравнения — RT_SELECT_ALL=1.
//
// Вывод (stdout) — строки для eval в sh, значения в одинарных кавычках и только из
// безопасных символов (T-144.1-23):
//   RT_SELECT_ALL=0|1
//   RT_GO_PKGS='./internal/services ./internal/utils/xtables'
//   RT_SPECS='tests/router/pages.spec.ts'
//   RT_PW_GREP='^(?!.*page:(?!(?:logs|dashboard) ))'
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
  /^frontend\/tests\/router\/global-setup\.ts$/,
  /^frontend\/package\.json$/,
  /^frontend\/package-lock\.json$/,
  /^go\.mod$/,
  /^go\.sum$/
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
    for (const dirent of readdirSync(path.join(ROOT, dir), { withFileTypes: true })) {
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
    if (HARNESS_ALL.some((re) => re.test(file))) {
      escalate(`${file}: обвязка проверки или сборка`);
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
      const changedImports = new Set(
        [...goChanged].map((d) => (d === '.' ? mod : `${mod}/${d}`))
      );
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
      const mod = await import(pathToFileURL(path.join(ROOT, 'frontend/scripts/select-e2e.mjs')));
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
  // Последняя проверка безопасности вывода: иначе — всё
  const okList = (l) => l.every((x) => SAFE_PATH.test(x.replace(/^\.\//, '')));
  if (!okList(go) || !okList(specs) || !/^[A-Za-z0-9_|()?!^*.: -]*$/.test(grep)) {
    all = true;
    go = routerPkgs.map((p) => `./${p}`);
    specs = specsAll;
    grep = '';
    res.reasons.push('всё: значение выбора не прошло проверку безопасных символов');
  }
  return [
    `RT_SELECT_ALL=${all ? 1 : 0}`,
    `RT_GO_PKGS=${quote(go.join(' '))}`,
    `RT_SPECS=${quote(specs.join(' '))}`,
    `RT_PW_GREP=${quote(grep)}`
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
