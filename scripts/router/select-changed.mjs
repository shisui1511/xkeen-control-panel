#!/usr/bin/env node
// Выбор проверок на роутерах по diff (make router-test SUITE=changed, D-25; области — 148.1-09).
//
// Что выбирается:
//   Области     файл продукта (Go, setup.sh, контракты, testdata) относят к областям по карте
//               scripts/router/areas.json (глобы paths). Правка файла области выбирает по
//               ИМЕНАМ все Go-тесты области (строка `// router-area:` в файле теста) и все спеки
//               области (D-12). Область с флагом shared (kernel, configlayer, server) добавляет
//               канарейки всех областей, Go и спеки (D-10). Области contract-* выбирают проверки
//               из поля tests. Изменённый роутерный тест выбирает тесты своего файла; файл без
//               строки области, файл без тестов (помощники) и продукт без области — пакет целиком
//               (откат, как раньше: пакет и пакеты, от которых он зависит, `go list -deps`).
//   Go          записи RT_GO_SEQ `<каталог пакета>:<TestRouterИмя>`; пакеты — каталоги записей.
//               Роутерный пакет — каталог с `_test.go`, у которого первая строка `//go:build router`.
//   Playwright  роутерные спеки frontend/tests/router/*.spec.ts. Правка frontend/src — по графу
//               импортов (scripts/router/import-graph.mjs) обеих ревизий (база и head: перенос
//               или удаление импорта не теряет потребителей старой стороны): спек выбирается,
//               если открывает затронутый маршрут (`#/<маршрут>`, вкладка `#/config/<вкладка>`
//               или `// e2e-pages: …`). Обычный общий компонент добавляет pages с маршрутами
//               потребителей и читающую канарейку оболочки (RTR-13, D-09). Область карты у
//               файла frontend/src — только отметка RT_AREAS и объяснения: тесты области
//               целиком не выбираются.
//               `// e2e-pages: *` — спек обходит все страницы: при выборе только по маршрутам он
//               сужается до них через --grep; выбранный по иной причине (область, зависимости,
//               правка самого спека) идёт без сужения. core-switch.spec.ts не выбирается: его
//               запускает матрица ядер.
//
// Триггеры (RTR-02). Полный выбор — только правка самого выбора (SELECTOR_SELF: этот файл,
// areas.json, core-marks.cjs, import-graph.mjs). Обвязка стенда (HARNESS: прочие
// scripts/router/**, frontend/tests/router/lib/**, playwright.router.config.ts, global-setup/
// teardown, internal/routertest, цели стенда и VERSION в Makefile) — RT_HARNESS=1 без полного
// выбора. Зависимости (DEPS: package.json, package-lock.json, go.mod, go.sum) — патч/минор:
// pages.spec.ts без сужения и канарейки всех областей; мажор (D-13) — канарейки плюс все спеки
// (npm) или все Go-тесты (go.mod). Неизвестный файл — только смоук и предупреждение в причинах.
//
// Ярус full (D-14). Тест яруса full (метка Tier full у Go, @full у Playwright; core-marks.cjs,
// testFacts → tier) идёт в гейте, только если его область затронута напрямую: изменён файл
// продукта области по areas.json (область без флага shared) или сам файл теста. Через канарейку,
// зависимости, граф импортов фронтенда и общий код (shared) тесты full не выбираются. Playwright:
// RT_PW_INVERT='@full', пока ни один выбранный спек не выбран напрямую по области и не изменён
// сам (иначе тесты full выбранных спеков идут все, расширение). Откат на
// пакет целиком и полный выбор идут со всеми ярусами. Отсев Go — исключение из RT_GO_SEQ и из
// RT_GO_CORE_RUN; число отсеянных тестов — в причинах (--explain).
//
// Порядок (RTR-03): сначала тесты затронутых областей, затем остальные выбранные; внутри группы —
// по убыванию частоты падений из истории (history.mjs failFreq, окно 30 дней). Спеки затронутых
// областей — RT_SPECS_FIRST. Ошибка истории — без сортировки (причина в stderr).
//
// При сомнении выбор только расширяется (D-25, T-148.1-24): небезопасное имя, ошибка карты,
// фактов тестов или разбора, нет базы сравнения у зависимостей и Makefile — полный выбор или
// расширение (пакет целиком, мажор, обвязка).
//
// Вывод (stdout) — 13 строк для eval в sh в фиксированном порядке, значения в одинарных
// кавычках и только из безопасных символов (T-144.1-23, T-148.1-23); run.sh сверяет число и
// форму каждой строки:
//   RT_SELECT_ALL=0|1
//   RT_GO_PKGS='./internal/services ./internal/utils/xtables'
//   RT_SPECS='tests/router/pages.spec.ts'
//   RT_PW_GREP='^(?!.*page:(?!(?:logs|dashboard) ))'
//   RT_GO_CORE_PKGS='./internal/handlers ./internal/services'
//   RT_GO_CORE_RUN='^(TestRouterA|TestRouterB)$'
//   RT_SPECS_CORE='tests/router/pages.spec.ts'
//   RT_KERNEL_TOUCHED=0|1
//   RT_GO_SEQ='internal/handlers:TestRouterA internal/services:TestRouterB'
//   RT_SPECS_FIRST='tests/router/access-log.spec.ts'
//   RT_PW_INVERT=''
//   RT_HARNESS=0|1
//   RT_AREAS='access-log configlayer'
// Строки 5–7 — тесты выбора, зависящие от ядра (D-01, D-03, D-04); по ним run.sh решает,
// нужна ли вторая ветвь (второе ядро); при выборе по именам они берутся только из выбранных:
//   RT_GO_CORE_PKGS  выбранные пакеты, где core-marks.cjs нашёл размеченные тесты (core,
//                    known-core); при полном выборе — все такие пакеты
//   RT_GO_CORE_RUN   выражение -test.run по именам этих тестов; пусто, если размеченных нет
//   RT_SPECS_CORE    выбранные спеки, где `playwright test --list` с отбором @core (вместе
//                    с сужением RT_PW_GREP) находит тест; список не получен — все выбранные
//   RT_KERNEL_TOUCHED  1, если изменён код запуска или перезапуска ядра либо watchdog
//                    (KERNEL_PATHS, D-07) или выбор полный по любой причине; иначе 0
// Строки 9–13: RT_GO_SEQ — порядок и состав Go первой ветви по именам (пусто — пакеты целиком,
// откат); RT_SPECS_FIRST — спеки затронутых областей; RT_PW_INVERT — значение --grep-invert:
// '@full' (отсев яруса full), если ни один выбранный спек не выбран напрямую по области и не
// изменён сам, иначе пусто (расширение: тесты full выбранных спеков идут все), при полном выборе
// пусто; RT_HARNESS — изменена обвязка стенда; RT_AREAS — затронутые области.
// Сбой расчёта ядра не роняет выбор: списки расширяются (все пакеты, все спеки), не сужаются.
// Причины выбора — в stderr при --explain; последняя строка «вид выбора: …» (полный | обвязка |
// зависимости | области | смоук) читают run.sh и select-replay.mjs.
//
// Использование (из корня репозитория):
//   node scripts/router/select-changed.mjs [--base <ref>] [--head <ref>] [--explain]
//   node scripts/router/select-changed.mjs --files a,b,c [--explain]   # без git
// --head <ref> — сравнивать <база>..<head> без рабочего дерева (прогон по слитым PR,
// select-replay.mjs); карта областей, факты тестов и граф импортов при этом берутся из
// рабочей копии. Без --base база — merge-base origin/main и head (или HEAD).
//
// RT_PW_GREP: для названий вида `page:<маршрут> theme:… vp:…` оставляет только затронутые
// маршруты. Форма с отрицательным просмотром вперёд, а не `page:(a|b) `, чтобы тесты без
// префикса `page:` (другие сценарии в выбранных спеках) не отфильтровывались.

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import coreMarks from './core-marks.cjs';

const { goMarks, analyzeGo, loadAreas, matchGlob, testFacts, goTestFacts, specTestFacts, areaOfText } = coreMarks;

// Изменённые файлы текущего выбора: goMarks читает только индекс, новые тесты он не видит
let changedFiles = [];
// Границы сравнения: from — база (null без git), to — head (null — рабочее дерево)
const ctx = { from: null, to: null };

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const SPEC_DIR = 'frontend/tests/router';

// Только безопасные имена путей: значения уходят в eval и в командную строку.
const SAFE_PATH = /^[A-Za-z0-9_.][A-Za-z0-9_./-]*$/;
const SAFE_ROUTE = /^[a-z][a-z0-9_-]*$/;
const SEQ_ENTRY = /^[A-Za-z0-9_./-]+:TestRouter[A-Za-z0-9_]+$/;

// Правка самого выбора: единственная причина полного выбора (RTR-02).
const SELECTOR_SELF = [/^scripts\/router\/(select-changed\.mjs|areas\.json|core-marks\.cjs|import-graph\.mjs)$/];

// Обвязка стенда: RT_HARNESS=1 без полного выбора (набор обвязки — 148.1-11). Цели стенда в
// Makefile разбирает makefileStand(), internal/routertest — ветка Go.
const HARNESS = [
  /^scripts\/router\//,
  /^scripts\/version\.sh$/,
  /^frontend\/playwright\.router\.config\.ts$/,
  /^frontend\/tests\/router\/lib\//,
  /^frontend\/tests\/router\/global-(setup|teardown)\.ts$/
];

// Зависимости: узкий набор (смоук, pages, канарейки), при мажоре — шире (D-13).
const DEPS = [/^frontend\/package\.json$/, /^frontend\/package-lock\.json$/, /^go\.mod$/, /^go\.sum$/];
// Смена версии этих пакетов npm считается мажорной (любая).
const WATCHED_NPM = new Set(['svelte', 'vite', '@playwright/test']);

// Цели Makefile, влияющие на стенд: сборка для роутеров и версия.
const STAND_TARGET = /^(router-[A-Za-z0-9_-]+|update-version|version)$/;

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
  // уборка ПК и статические проверки CI на роутер не влияют (D-25)
  /^scripts\/tidy\.sh$/,
  /^scripts\/check-[\w-]+\.js$/,
  /^scripts\/release-guard\.js$/,
  /^scripts\/lib\//,
  // Playwright-спеки с подменой сети и юнит-тесты фронтенда на роутерах не запускаются
  /^frontend\/tests\/(?!router\/)/,
  /^frontend\/src\/.*\.test\.ts$/,
  /^frontend\/scripts\//,
  /^frontend\/\.prettier/,
  /^frontend\/eslint\.config\./,
  /^frontend\/\.gitignore$/
];

// Спеки фронтенда с особой ролью (RTR-13). pages — read-обход всех маршрутов (`e2e-pages: *`).
// Читающая канарейка оболочки (D-08) — существующий read-спек: шапка `// e2e-kind: read`
// проверяется при каждом выборе. Не найден или не read — все спеки (расширение).
const PAGES_SPEC = 'tests/router/pages.spec.ts';
const SHELL_READ_CANARY = 'tests/router/config-visual.spec.ts';

// Файлы frontend/ вне src/, меняющие сборку панели: все роутерные спеки (не Go).
const FRONTEND_ALL = [
  /^frontend\/vite\.config\.ts$/,
  /^frontend\/svelte\.config\.js$/,
  /^frontend\/tsconfig[\w.-]*\.json$/,
  /^frontend\/index\.html$/,
  /^frontend\/public\//
];

function git(args) {
  return execFileSync('git', args, { cwd: ROOT, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
}

/** Текст файла в ref (null — нет файла или ref). */
function gitShow(ref, file) {
  try {
    return execFileSync('git', ['show', `${ref}:${file}`], {
      cwd: ROOT,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
      maxBuffer: 64 * 1024 * 1024
    });
  } catch {
    return null;
  }
}

/** Текст файла на стороне head: в ref, если задан --head, иначе в рабочем дереве. */
function headText(file) {
  if (ctx.to) return gitShow(ctx.to, file);
  const abs = path.join(ROOT, file);
  return existsSync(abs) ? readFileSync(abs, 'utf8') : null;
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

/** Изменённые файлы: { files, from, to }. С head — только diff <база>..<head>, без рабочего дерева. */
function changedFromGit(base, head) {
  let from = base;
  if (!from) {
    from = git(['merge-base', 'origin/main', head || 'HEAD']).trim();
  }
  const files = git(['diff', '--name-only', '--no-renames', from, ...(head ? [head] : [])])
    .split('\n')
    .filter(Boolean);
  if (!head) {
    const status = git(['status', '--porcelain', '--untracked-files=all'])
      .split('\n')
      .filter(Boolean)
      .map((l) => l.slice(3));
    for (const s of status) {
      // переименование «старое -> новое»: значимы оба имени
      for (const part of s.split(' -> ')) files.push(part);
    }
  }
  return { files: [...new Set(files.map((f) => path.posix.normalize(f)))], from, to: head || null };
}

// --- зависимости: мажорное обновление (D-13) ----------------------------------------------

/** Мажорная часть спецификации версии npm: `^5.1.0` → 5, `~0.4.2` → 0.4; нестандартная — вся строка. */
function majorOf(spec) {
  const m = /(\d+)(?:\.(\d+))?/.exec(String(spec));
  if (!m) return String(spec);
  return m[1] === '0' && m[2] !== undefined ? `0.${m[2]}` : m[1];
}

/** Мажор npm: причина (строка) или null. Нет базы или текста — мажор (расширение). */
function npmMajorReason(files) {
  if (!ctx.from) return 'нет базы сравнения — считается мажором';
  const baseTxt = gitShow(ctx.from, 'frontend/package.json');
  const headTxt = headText('frontend/package.json');
  if (baseTxt === null || headTxt === null) return 'package.json не прочитан — считается мажором';
  let a;
  let b;
  try {
    a = JSON.parse(baseTxt);
    b = JSON.parse(headTxt);
  } catch {
    return 'package.json не разобран — считается мажором';
  }
  const flat = (o) => ({
    ...(o.optionalDependencies || {}),
    ...(o.peerDependencies || {}),
    ...(o.devDependencies || {}),
    ...(o.dependencies || {})
  });
  const da = flat(a);
  const db = flat(b);
  for (const name of new Set([...Object.keys(da), ...Object.keys(db)])) {
    if (da[name] === db[name] || da[name] === undefined || db[name] === undefined) continue;
    if (WATCHED_NPM.has(name)) return `${name}: ${da[name]} → ${db[name]}`;
    if (majorOf(da[name]) !== majorOf(db[name])) return `${name}: ${da[name]} → ${db[name]}`;
  }
  if (files.has('frontend/package-lock.json')) {
    const baseLock = gitShow(ctx.from, 'frontend/package-lock.json');
    const headLock = headText('frontend/package-lock.json');
    if (baseLock === null || headLock === null) return 'package-lock.json не прочитан — считается мажором';
    try {
      const la = JSON.parse(baseLock).packages || {};
      const lb = JSON.parse(headLock).packages || {};
      for (const name of WATCHED_NPM) {
        const va = (la[`node_modules/${name}`] || {}).version;
        const vb = (lb[`node_modules/${name}`] || {}).version;
        if (va !== vb) return `${name} (lock): ${va ?? '-'} → ${vb ?? '-'}`;
      }
    } catch {
      return 'package-lock.json не разобран — считается мажором';
    }
  }
  return null;
}

/** Мажор Go: смена `go`, `toolchain` или модуля с суффиксом /vN. Нет базы — мажор. */
function goMajorReason() {
  if (!ctx.from) return 'нет базы сравнения — считается мажором';
  const baseTxt = gitShow(ctx.from, 'go.mod');
  const headTxt = headText('go.mod');
  if (baseTxt === null || headTxt === null) return 'go.mod не прочитан — считается мажором';
  const keys = (text) => {
    const set = new Set();
    for (const line of text.split('\n')) {
      const t = line.replace(/\/\/.*$/, '').trim();
      let m = /^(go|toolchain)\s+(\S+)$/.exec(t);
      if (m) set.add(`${m[1]} ${m[2]}`);
      m = /^(?:require\s+)?(\S+\/v\d+)\s+v\S+/.exec(t);
      if (m) set.add(`module ${m[1]}`);
      m = /^module\s+(\S+)$/.exec(t);
      if (m) set.add(`module ${m[1]}`);
    }
    return set;
  };
  const ka = keys(baseTxt);
  const kb = keys(headTxt);
  const diff = [...ka].filter((x) => !kb.has(x)).concat([...kb].filter((x) => !ka.has(x)));
  return diff.length > 0 ? diff.join(', ') : null;
}

// --- Makefile ---------------------------------------------------------------------------

/** Владельцы строк Makefile: 'comment' | 'blank' | имя цели | 'top' (переменные и условия). */
function makefileOwners(text) {
  const lines = text.split('\n');
  const owners = [];
  let cur = 'top';
  let cont = false;
  for (const line of lines) {
    if (cont) {
      owners.push(cur);
    } else if (/^\s*$/.test(line)) {
      owners.push('blank');
    } else if (/^\s*#/.test(line)) {
      owners.push('comment');
    } else if (/^\t/.test(line)) {
      owners.push(cur);
    } else if (/^\s*[A-Za-z_][A-Za-z0-9_]*\s*[:?+]?=/.test(line) || /^\s*(ifeq|ifneq|ifdef|ifndef|else|endif|include)\b/.test(line)) {
      cur = 'top';
      owners.push('top');
    } else {
      const m = /^([^\s:=][^:=]*?)\s*:(?!=)/.exec(line);
      if (m) cur = m[1].trim().split(/\s+/)[0];
      owners.push(m ? cur : 'top');
    }
    cont = /\\\s*$/.test(line);
  }
  return owners;
}

/** Правка Makefile затрагивает цели стенда или VERSION: { stand, why }. Нет базы — стенд (расширение). */
function makefileStand() {
  if (!ctx.from) return { stand: true, why: 'нет базы сравнения — считается правкой обвязки' };
  let diff;
  try {
    diff = git(['diff', '-U0', '--no-color', '--no-renames', ctx.from, ...(ctx.to ? [ctx.to] : []), '--', 'Makefile']);
  } catch {
    return { stand: true, why: 'diff Makefile не получен — считается правкой обвязки' };
  }
  const baseTxt = gitShow(ctx.from, 'Makefile');
  const headTxt = headText('Makefile');
  const baseLines = (baseTxt ?? '').split('\n');
  const headLines = (headTxt ?? '').split('\n');
  const baseOwn = makefileOwners(baseTxt ?? '');
  const headOwn = makefileOwners(headTxt ?? '');
  const standLine = (owner, text) => {
    if (owner === 'comment' || owner === 'blank') return false;
    if (STAND_TARGET.test(owner)) return true;
    return owner === 'top' && /\b(VERSION|BINARY_NAME)\b/.test(text);
  };
  for (const m of diff.matchAll(/^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/gm)) {
    const oldStart = Number(m[1]);
    const oldLen = m[2] === undefined ? 1 : Number(m[2]);
    const newStart = Number(m[3]);
    const newLen = m[4] === undefined ? 1 : Number(m[4]);
    for (let i = 0; i < oldLen; i++) {
      const idx = oldStart - 1 + i;
      if (standLine(baseOwn[idx] ?? 'top', baseLines[idx] ?? '')) return { stand: true, why: `строка ${oldStart + i} (цель ${baseOwn[idx]})` };
    }
    for (let i = 0; i < newLen; i++) {
      const idx = newStart - 1 + i;
      if (standLine(headOwn[idx] ?? 'top', headLines[idx] ?? '')) return { stand: true, why: `строка ${newStart + i} (цель ${headOwn[idx]})` };
    }
  }
  return { stand: false, why: '' };
}

// --- граф импортов фронтенда (RTR-13) ----------------------------------------------------

/**
 * Модели графа обеих ревизий: база (снимок ctx.from, если он есть) и head (снимок ctx.to или
 * рабочее дерево). Ошибка чтения или разбора — исключение: вызывающий выбирает все спеки.
 * specSources(абсолютный путь спека) — прямые ссылки спека на файлы src (кэш).
 */
async function frontGraph() {
  const mod = await import(pathToFileURL(path.join(ROOT, 'scripts/router/import-graph.mjs')));
  const fe = path.join(ROOT, 'frontend');
  const models = [];
  if (ctx.from) models.push({ side: 'база', model: mod.buildModel(fe, { reader: mod.gitReader(ROOT, ctx.from) }) });
  models.push({
    side: 'head',
    model: mod.buildModel(fe, { reader: ctx.to ? mod.gitReader(ROOT, ctx.to) : mod.fsReader(fe) })
  });
  const routes = new Set(models.flatMap((m) => [...m.model.routes]));
  const srcFiles = [...new Set(models.flatMap((m) => m.model.reader.files(path.join(fe, 'src'))))];
  const cache = new Map();
  const specSources = (specAbs) => {
    if (!cache.has(specAbs)) cache.set(specAbs, mod.specSources(specAbs, srcFiles, fe));
    return cache.get(specAbs);
  };
  return { mod, models, routes, srcFiles, specSources };
}

// --- факты тестов и порядок -------------------------------------------------------------

/** Факты роутерных тестов: индекс git плюс новые (ещё не в индексе) изменённые тесты с диска. */
function buildFacts(changed) {
  let rows = testFacts(ROOT);
  const isGoTest = (f) => /^(internal|cmd)\/.*_test\.go$/.test(f) && !f.startsWith('internal/routertest/');
  const isSpec = (f) => /^frontend\/tests\/router\/[\w.-]+\.spec\.ts$/.test(f);
  const own = new Set(changed.filter((f) => SAFE_PATH.test(f) && (isGoTest(f) || isSpec(f))));
  // С --head изменённые тесты берутся из коммита слияния, а не из рабочей копии
  if (ctx.to) rows = rows.filter((r) => !own.has(r.file));
  const have = new Set(rows.map((r) => r.file));
  for (const file of own) {
    if (have.has(file)) continue;
    const text = headText(file);
    if (text === null) continue;
    const { area } = areaOfText(text);
    if (isGoTest(file)) {
      if (text.split('\n', 1)[0].trim() !== '//go:build router') continue;
      for (const t of goTestFacts(text)) rows.push({ kind: 'go', file, pkg: path.posix.dirname(file), spec: null, area, ...t });
    } else {
      for (const t of specTestFacts(text)) rows.push({ kind: 'pw', file, pkg: null, spec: file.replace(/^frontend\//, ''), area, ...t });
    }
  }
  return rows;
}

/** Падения Go-тестов по истории за 30 дней: Map 'go:<Имя>' → число падений (с подтестами) или null. */
async function goFailScores(reasons) {
  try {
    const hist = await import(pathToFileURL(path.join(ROOT, 'scripts/router/history.mjs')));
    const rows = hist.readHistory();
    if (rows.length === 0) {
      reasons.push('порядок: история пуста — без сортировки по частоте падений');
      return null;
    }
    const scores = new Map();
    for (const [check, v] of hist.failFreq(rows, { days: 30 })) {
      if (!check.startsWith('go:')) continue;
      const base = check.split('/', 1)[0];
      scores.set(base, (scores.get(base) || 0) + v.fails);
    }
    reasons.push(`порядок: по частоте падений за 30 дней (строк истории ${rows.length})`);
    return scores;
  } catch (e) {
    reasons.push(`порядок: история не прочитана (${String(e.message ?? e).split('\n', 1)[0]}) — без сортировки по частоте падений`);
    return null;
  }
}

/** Решение по списку изменённых файлов. */
async function select(changed) {
  const reasons = [];
  const routerPkgs = routerPackages();
  const specsAll = listRouterSpecs();
  const res = {
    all: false,
    goSel: new Map(), // 'пакет:Тест' → true (затронутая область) | false (канарейка, зависимости)
    goFullOk: new Set(), // 'пакет:Тест', которым ярус full не мешает: область напрямую, свой файл, откат на пакет
    fullDropGo: [], // тесты яруса full, отсеянные из выбора (D-14)
    goAllPkgs: false, // все роутерные пакеты целиком (откат, мажор Go)
    specsDirect: new Set(), // спеки затронутых областей, правок и маршрутов — идут первыми
    specsOther: new Set(), // канарейки и зависимости
    specsForced: new Set(), // выбраны не по маршрутам: без сужения обхода
    specsFullOk: new Set(), // спеки, чья область затронута напрямую или изменённые сами: тесты full идут (D-14)
    pwFull: [], // факты Playwright-тестов яруса full: { spec } — для числа отсеянных
    grepRoutes: null, // null — без сужения
    areas: new Set(), // затронутые области
    harness: false,
    deps: false,
    kernelTouched: false, // затронут код запуска ядра или watchdog (KERNEL_PATHS)
    kind: 'смоук',
    reasons
  };
  const escalate = (why) => {
    res.all = true;
    reasons.push(`всё: ${why}`);
  };
  const warnUnknown = (file) =>
    reasons.push(`предупреждение: неизвестный файл ${file} — только смоук; добавьте путь в areas.json или в список не влияющих`);

  // Карта областей и факты тестов: без них имена не выбрать — расширение до полного выбора
  let areaMap;
  let facts;
  try {
    areaMap = loadAreas(ROOT).areas;
    facts = buildFacts(changed);
  } catch (e) {
    escalate(`карта областей или факты тестов не получены (${String(e.message ?? e).split('\n', 1)[0]})`);
    return { res, routerPkgs, specsAll };
  }
  const areasOf = (file) => Object.keys(areaMap).filter((n) => areaMap[n].paths.some((g) => matchGlob(g, file)));
  const goRows = facts.filter((r) => r.kind === 'go');
  const specOfRow = (r) => r.spec;
  // fullOk — основание выбрать и тест яруса full (D-14): область затронута напрямую, изменён сам
  // тест или откат на пакет целиком; канарейки, зависимости и общий код его не дают
  const addGo = (pkg, test, direct, fullOk = false) => {
    const key = `${pkg}:${test}`;
    res.goSel.set(key, direct || res.goSel.get(key) === true);
    if (fullOk) res.goFullOk.add(key);
  };
  const addWholePkg = (pkg, direct) => {
    for (const r of goRows) if (r.pkg === pkg) addGo(r.pkg, r.test, direct, true);
  };
  const specSelectable = (s) => specsAll.includes(s);

  /** Тесты области: Go и спеки (факты + поле tests). Нет теста из tests — расширение. */
  const addAreaTests = (name, direct) => {
    const a = areaMap[name];
    // общий код (shared) задевает все области: тесты full его области напрямую не выбираются
    const fullOk = direct && !a.shared;
    let go = 0;
    const specs = new Set();
    for (const r of goRows) if (r.area === name) { addGo(r.pkg, r.test, direct, fullOk); go++; }
    for (const r of facts) if (r.kind === 'pw' && r.area === name && specOfRow(r)) specs.add(specOfRow(r));
    for (const t of a.tests || []) {
      if (t.startsWith('go:')) {
        const hit = goRows.filter((r) => r.test === t.slice(3));
        if (hit.length === 0) escalate(`область ${name}: тест ${t} из поля tests не найден`);
        for (const r of hit) { addGo(r.pkg, r.test, direct, fullOk); go++; }
      } else {
        specs.add(t);
      }
    }
    const pick = [...specs].filter(specSelectable);
    for (const s of pick) {
      (direct ? res.specsDirect : res.specsOther).add(s);
      res.specsForced.add(s);
      if (fullOk) res.specsFullOk.add(s);
    }
    return { go, specs: pick.length };
  };
  /** Канарейки: по одному Go-тесту и спеку каждой области (D-11). */
  const addCanaries = (why) => {
    for (const [name, a] of Object.entries(areaMap)) {
      if (a.canary.go) {
        const hit = goRows.filter((r) => r.test === a.canary.go);
        if (hit.length === 0) escalate(`канарейка ${a.canary.go} области ${name} не найдена`);
        for (const r of hit) if (!res.goSel.has(`${r.pkg}:${r.test}`)) addGo(r.pkg, r.test, false);
      }
      if (a.canary.spec && specSelectable(a.canary.spec)) {
        res.specsForced.add(a.canary.spec);
        if (!res.specsDirect.has(a.canary.spec)) res.specsOther.add(a.canary.spec);
      }
    }
    reasons.push(`канарейки всех областей (${why})`);
  };

  const touchedSrc = [];
  // Области, отмеченные только файлами frontend/src: в RT_AREAS и объяснении, но тесты области
  // целиком не выбираются — фронтенд классифицирует граф импортов (D-09)
  const frontMarked = new Set();
  const touchedSpecs = new Set();
  const goChanged = new Set(); // каталоги изменённых Go-пакетов без области (откат на пакет)
  const goTestFiles = new Set(); // изменённые _test.go
  const depFiles = new Set();
  let makefile = false;
  let pwAll = false;
  let sharedHit = false;

  for (const file of changed) {
    if (!SAFE_PATH.test(file)) {
      escalate(`небезопасное имя файла ${JSON.stringify(file)}`);
      continue;
    }
    // Признак не отменяет разбор файла: он идёт дальше как обычно (выбор тестов)
    if (!file.endsWith('_test.go') && KERNEL_PATHS.some((re) => re.test(file))) {
      res.kernelTouched = true;
      reasons.push(`${file}: код запуска или перезапуска ядра либо watchdog — полное окно смоука`);
    }
    if (SELECTOR_SELF.some((re) => re.test(file))) {
      escalate(`${file}: правка самого выбора`);
      continue;
    }
    if (HARNESS.some((re) => re.test(file))) {
      res.harness = true;
      reasons.push(`${file}: обвязка стенда — набор обвязки, полный выбор не нужен`);
      continue;
    }
    if (DEPS.some((re) => re.test(file))) {
      depFiles.add(file);
      continue;
    }
    if (file === 'Makefile') {
      makefile = true;
      continue;
    }
    // Контракты и данные тестов — области contract-* (поле tests)
    const contract = areasOf(file).filter((n) => areaMap[n].tests);
    if (contract.length > 0) {
      for (const n of contract) {
        res.areas.add(n);
        reasons.push(`${file}: область ${n} — проверки из поля tests`);
      }
      continue;
    }
    if (/(^|\/)testdata\//.test(file)) {
      warnUnknown(file);
      continue;
    }
    if (IGNORED.some((re) => re.test(file))) {
      reasons.push(`${file}: на роутерные проверки не влияет`);
      continue;
    }
    if (/^frontend\/tests\/router\/[\w.-]+\.spec\.ts$/.test(file)) {
      if (file.endsWith('/core-switch.spec.ts')) {
        reasons.push(`${file}: спек матрицы ядер, запускается в каждой ветви`);
      } else if (headText(file) !== null) {
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
      for (const n of areasOf(file)) {
        frontMarked.add(n);
        reasons.push(`${file}: область ${n} (выбор — по графу импортов, не всеми тестами области)`);
      }
      continue;
    }
    if (file.startsWith('frontend/')) {
      warnUnknown(file);
      continue;
    }
    if (/^(internal|cmd)\//.test(file) || /^[\w.-]+\.go$/.test(file)) {
      if (file.endsWith('_test.go')) {
        goTestFiles.add(file);
        continue;
      }
      if (file.startsWith('internal/routertest/')) {
        res.harness = true;
        reasons.push(`${file}: помощники роутерных тестов — обвязка; тесты пакетов, зависящих от них, выбираются как раньше`);
      }
      const hit = areasOf(file);
      if (hit.length > 0) {
        for (const n of hit) {
          res.areas.add(n);
          if (areaMap[n].shared) sharedHit = true;
          reasons.push(`${file}: область ${n}${areaMap[n].shared ? ' (общий код)' : ''}`);
        }
        continue;
      }
      const dir = goDirOf(file);
      if (dir === null) {
        // Данные без пакета рядом: владельца не определить — все роутерные пакеты
        res.goAllPkgs = true;
        reasons.push(`${file}: нет пакета Go рядом — все роутерные пакеты`);
      } else {
        goChanged.add(dir);
        reasons.push(`${file}: нет области — пакет ${dir} целиком`);
      }
      continue;
    }
    // прочие файлы продукта, покрытые картой (scripts/setup.sh)
    const hit = areasOf(file);
    if (hit.length > 0) {
      for (const n of hit) {
        res.areas.add(n);
        if (areaMap[n].shared) sharedHit = true;
        reasons.push(`${file}: область ${n}`);
      }
      continue;
    }
    warnUnknown(file);
  }

  // --- Makefile ---------------------------------------------------------------
  if (makefile && !res.all) {
    const mk = makefileStand();
    if (mk.stand) {
      res.harness = true;
      reasons.push(`Makefile: ${mk.why || 'изменены цели стенда или VERSION'} — обвязка стенда`);
    } else {
      reasons.push('Makefile: изменены цели и переменные вне стенда — на роутер не влияет');
    }
  }

  // --- зависимости (D-13) -----------------------------------------------------
  if (depFiles.size > 0 && !res.all) {
    res.deps = true;
    const npmTouched = depFiles.has('frontend/package.json') || depFiles.has('frontend/package-lock.json');
    const goTouched = depFiles.has('go.mod') || depFiles.has('go.sum');
    const files = [...depFiles].join(', ');
    // патч и минор: pages.spec.ts без сужения и канарейки всех областей
    const pages = 'tests/router/pages.spec.ts';
    if (specSelectable(pages)) {
      res.specsOther.add(pages);
      res.specsForced.add(pages);
    }
    addCanaries(`зависимости: ${files}`);
    reasons.push(`${files}: зависимости — смоук, pages без сужения и канарейки`);
    if (npmTouched) {
      const why = npmMajorReason(depFiles);
      if (why) {
        specsAll.forEach((s) => {
          res.specsOther.add(s);
          res.specsForced.add(s);
        });
        reasons.push(`npm: мажорное обновление или смена Svelte/Vite/Playwright (${why}) — канарейки и все спеки`);
      }
    }
    if (depFiles.has('go.mod')) {
      const why = goMajorReason();
      if (why) {
        res.goAllPkgs = true;
        reasons.push(`go.mod: мажорное обновление или смена Go (${why}) — канарейки и все Go-тесты`);
      }
    } else if (goTouched) {
      reasons.push('go.sum: без go.mod — патч/минор');
    }
  }

  // --- области: тесты по именам -----------------------------------------------
  if (!res.all) {
    for (const name of [...res.areas].sort()) {
      const n = addAreaTests(name, true);
      reasons.push(`область ${name}: тестов Go ${n.go}, спеков ${n.specs}`);
    }
    if (sharedHit) addCanaries('правка общего кода');
    // Отметка фронтенда: только RT_AREAS и объяснение, тесты выбирает граф импортов
    for (const name of [...frontMarked].sort()) {
      if (res.areas.has(name)) continue;
      res.areas.add(name);
      reasons.push(`область ${name}: отмечена правкой frontend/src — тесты области целиком не выбираются`);
    }
  }

  // --- изменённые роутерные тесты ---------------------------------------------
  if (!res.all) {
    for (const file of [...goTestFiles].sort()) {
      const dir = path.posix.dirname(file);
      const fileRows = goRows.filter((r) => r.file === file);
      if (headText(file) === null) {
        // Удалён: роутерные тесты файла исчезли — запускать нечего; иначе (помощники, обычный
        // тест, база неизвестна) — пакет целиком, как для изменённого
        const baseText = ctx.from ? gitShow(ctx.from, file) : null;
        const tagged = baseText !== null && baseText.split('\n', 1)[0].trim() === '//go:build router';
        if (tagged && goTestFacts(baseText).length > 0) {
          reasons.push(`${file}: файл роутерных тестов удалён — запускать нечего`);
          continue;
        }
      }
      if (fileRows.length > 0 && fileRows[0].area) {
        for (const r of fileRows) addGo(r.pkg, r.test, true, true);
        reasons.push(`${file}: изменён роутерный тест — тесты файла (${fileRows.length})`);
      } else if (routerPkgs.includes(dir)) {
        addWholePkg(dir, true);
        reasons.push(
          fileRows.length > 0
            ? `${file}: нет строки router-area — пакет ${dir} целиком`
            : `${file}: тестов TestRouter в файле нет (помощники, обычный тест или удалён) — пакет ${dir} целиком`
        );
      } else {
        reasons.push(`${file}: изменён обычный тест, роутерных тестов в пакете ${dir} нет`);
      }
    }
  }

  // --- пакеты без области: зависимости go list --------------------------------
  if (!res.all) {
    if (res.goAllPkgs) {
      reasons.push('Go: все роутерные пакеты целиком');
    } else if (goChanged.size > 0) {
      const mod = modulePath();
      const changedImports = new Set([...goChanged].map((d) => (d === '.' ? mod : `${mod}/${d}`)));
      let any = false;
      for (const pkg of routerPkgs) {
        const own = `${mod}/${pkg}`;
        const deps = depsOf(pkg);
        const hit = [...changedImports].find((p) => p === own || deps.has(p));
        if (hit) {
          addWholePkg(pkg, true);
          any = true;
          reasons.push(`${pkg}: зависит от изменённого пакета ${hit.slice(mod.length + 1) || '.'} — пакет целиком`);
        }
      }
      if (!any) reasons.push('Go: ни один роутерный пакет не затронут');
    }
  }

  // --- ярус full: отсев Go-тестов без прямого затрагивания (D-14) ---------------
  // Откат на пакет целиком (goAllPkgs) отсева не знает: при сомнении выбор только расширяется
  if (!res.all && !res.goAllPkgs) {
    const tierOf = new Map(goRows.map((r) => [`${r.pkg}:${r.test}`, r.tier]));
    for (const key of [...res.goSel.keys()]) {
      if (tierOf.get(key) === 'full' && !res.goFullOk.has(key)) {
        res.goSel.delete(key);
        res.fullDropGo.push(key);
      }
    }
    if (res.fullDropGo.length > 0) {
      reasons.push(
        `ярус full: ${res.fullDropGo.length} тестов не выбраны — область затронута не напрямую (${res.fullDropGo.map((k) => k.slice(k.indexOf(':') + 1)).join(', ')})`
      );
    }
  }

  // --- Playwright -------------------------------------------------------------
  if (!res.all) {
    const affected = new Set();
    const touchedAbs = touchedSrc.map((rel) => path.join(ROOT, 'frontend', rel));
    let graph = null;
    const said = new Set();
    const sayOnce = (line) => {
      if (said.has(line)) return;
      said.add(line);
      reasons.push(line);
    };
    // Спек с особой ролью (pages без сужения, канарейки оболочки): выбран не по маршрутам
    const addForced = (spec, why) => {
      if (!specSelectable(spec)) return false;
      res.specsForced.add(spec);
      if (!res.specsDirect.has(spec)) res.specsOther.add(spec);
      sayOnce(`${spec}: ${why}`);
      return true;
    };
    // Канарейка оболочки: существующий спек, шапка или содержимое сверяются при каждом выборе
    const addShellCanary = (spec, fits, what, why) => {
      const abs = path.join(ROOT, 'frontend', spec);
      const text = existsSync(abs) ? readFileSync(abs, 'utf8') : null;
      if (text === null || !specSelectable(spec) || !fits(text)) {
        pwAll = true;
        sayOnce(`${spec}: ${what} не найдена или не подходит — все роутерные спеки`);
        return;
      }
      addForced(spec, `${what} (${why})`);
    };
    const isReadSpec = (text) => text.split('\n', 5).some((l) => /^\/\/\s*e2e-kind:\s*read\s*$/.test(l.trim()));

    if (touchedSrc.length > 0) {
      try {
        graph = await frontGraph();
        reasons.push(`граф импортов: ${graph.models.map((m) => `${m.side} — маршрутов ${m.model.routes.size}, оболочка ${m.model.shell.size} файлов`).join('; ')}`);
      } catch (e) {
        pwAll = true;
        reasons.push(`граф импортов не построен (${String(e.message ?? e).split('\n', 1)[0]}) — все роутерные спеки`);
      }
    }
    let componentHit = false;
    if (graph) {
      for (const rel of touchedSrc) {
        const abs = path.join(ROOT, 'frontend', rel);
        const present = graph.models.filter((m) => m.model.reader.isFile(abs));
        if (present.length === 0) {
          reasons.push(`frontend/${rel}: удалён (нет в ${graph.models.map((m) => m.side).join(' и ')})`);
          continue;
        }
        const inShell = present.filter((m) => m.model.shell.has(abs)).map((m) => m.side);
        if (inShell.length > 0) {
          pwAll = true;
          reasons.push(`frontend/${rel}: оболочка приложения (${inShell.join(', ')}) — все роутерные спеки`);
          continue;
        }
        // Маршруты обеих ревизий: перенос или удаление импорта не теряет потребителей старой стороны
        const per = present.map((m) => ({ side: m.side, routes: m.model.routesOf(abs) }));
        const routes = [...new Set(per.flatMap((p) => p.routes))];
        if (routes.length === 0) {
          reasons.push(`frontend/${rel}: не импортируется приложением (${per.map((p) => p.side).join(', ')})`);
          continue;
        }
        routes.forEach((r) => affected.add(r));
        componentHit = true;
        const sides =
          per.length > 1 && per.some((p) => p.routes.join() !== per[0].routes.join())
            ? ` (${per.map((p) => `${p.side}: ${p.routes.join(', ') || '-'}`).join('; ')})`
            : '';
        reasons.push(`frontend/${rel}: страницы ${routes.join(', ')}${sides}`);
      }
    }
    // Обычный общий компонент: спеки маршрутов-потребителей, pages с их маршрутами и читающая
    // канарейка оболочки (D-09)
    if (componentHit && !pwAll) {
      addShellCanary(SHELL_READ_CANARY, isReadSpec, 'читающая канарейка оболочки', 'правка общего компонента');
    }

    for (const s of touchedSpecs) {
      res.specsDirect.add(s);
      res.specsForced.add(s);
      res.specsFullOk.add(s);
    }

    // Обход всех страниц (`// e2e-pages: *`) сужается до маршрутов, только если ни одна
    // причина, кроме маршрутов, его не выбрала
    const star = new Set();
    for (const spec of specsAll) {
      const text = readFileSync(path.join(ROOT, 'frontend', spec), 'utf8');
      const ann = [...text.matchAll(/\/\/\s*e2e-pages:\s*([^\n]+)/g)].flatMap((m) =>
        m[1].split(',').map((r) => r.trim())
      );
      if (ann.includes('*')) star.add(spec);
    }

    if (pwAll) {
      specsAll.forEach((s) => res.specsDirect.add(s));
    } else if (affected.size > 0) {
      for (const spec of specsAll) {
        if (res.specsForced.has(spec)) continue;
        if (star.has(spec)) {
          res.specsDirect.add(spec);
          reasons.push(`${spec}: обходит все страницы — сужается до затронутых`);
          continue;
        }
        const specAbs = path.join(ROOT, 'frontend', spec);
        const text = readFileSync(specAbs, 'utf8');
        const routes = graph.mod.specRoutes(text, graph.routes);
        if (routes === null) {
          // Маршрут не известен: прямые ссылки спека на изменённые файлы называются, иначе
          // включение консервативное
          res.specsDirect.add(spec);
          const hit = touchedAbs.filter((a) => graph.specSources(specAbs).has(a));
          reasons.push(
            hit.length > 0
              ? `${spec}: маршруты не определены, ссылается напрямую на ${hit.map((a) => path.relative(ROOT, a)).join(', ')}`
              : `${spec}: маршруты не определены (нет #/…, литералов маршрутов и e2e-pages) — включается консервативно`
          );
        } else if ([...routes].some((r) => affected.has(r))) {
          res.specsDirect.add(spec);
          reasons.push(`${spec}: открывает затронутые страницы (${[...routes].filter((r) => affected.has(r)).sort().join(', ')})`);
        }
      }
      const all = new Set([...res.specsDirect, ...res.specsOther]);
      const walkSelected = [...all].some((s) => star.has(s));
      const walkForced = [...res.specsForced].some((s) => star.has(s));
      if (walkSelected && !walkForced) {
        // Обход pages называет страницы (`page:config`), не вкладки: вкладка сужает до страницы
        const tops = new Set([...affected].map((r) => graph.mod.topRoute(r)));
        const routes = [...tops].filter((r) => SAFE_ROUTE.test(r)).sort();
        if (routes.length !== tops.size) {
          reasons.push('маршрут с небезопасным именем — обход идёт целиком');
        } else {
          res.grepRoutes = routes;
        }
      }
    }
  }

  res.pwFull = facts.filter((r) => r.kind === 'pw' && r.tier === 'full').map((r) => ({ spec: r.spec }));
  return { res, routerPkgs, specsAll, routerGoRows: goRows };
}

/** Размеченные тесты Go в пакетах `./internal/…`: { pkgs, run, rows } (rows — для причин). */
function goCoreOf(goPkgs, only) {
  const want = goPkgs.map((p) => p.replace(/^\.\//, ''));
  // Пустой список goMarks понимает как «все пакеты»: пустой выбор отдаём сами
  let rows = want.length > 0 ? goMarks(ROOT, want) : [];
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
  // Выбор по именам: вторая ветвь берёт только выбранные размеченные тесты
  if (only) rows = rows.filter((r) => only.has(`${r.pkg}:${r.test}`));
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
 * only — множество 'пакет:Тест' выбора по именам (null — выбор пакетами).
 */
function coreSelection(goPkgs, specs, grep, only) {
  const reasons = [];
  let go;
  try {
    go = goCoreOf(goPkgs, only);
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
    const go = goCoreOf(all, null);
    return { goPkgs: go.pkgs, goRun: go.run, specs: specsAll, reasons };
  } catch {
    return { goPkgs: all, goRun: '', specs: specsAll, reasons };
  }
}

async function emit(res, routerPkgs, specsAll) {
  const quote = (s) => `'${s}'`;
  let all = res.all;
  let go;
  let seq = [];
  let specs;
  let first = [];
  let grep = '';
  let names = null; // выбор Go по именам: множество 'пакет:Тест'
  if (all) {
    go = routerPkgs.map((p) => `./${p}`);
    specs = specsAll;
  } else {
    if (res.goAllPkgs) {
      go = routerPkgs.map((p) => `./${p}`);
    } else {
      names = new Set(res.goSel.keys());
      const scores = names.size > 0 ? await goFailScores(res.reasons) : null;
      const score = (key) => (scores ? scores.get(`go:${key.slice(key.indexOf(':') + 1)}`) || 0 : 0);
      const order = [...res.goSel.entries()].sort((x, y) => {
        if (x[1] !== y[1]) return x[1] ? -1 : 1; // затронутые области — первыми
        return score(y[0]) - score(x[0]) || (x[0] < y[0] ? -1 : x[0] > y[0] ? 1 : 0);
      });
      seq = order.map(([k]) => k);
      const dirs = new Set(seq.map((k) => k.slice(0, k.indexOf(':'))));
      go = routerPkgs.filter((p) => dirs.has(p)).map((p) => `./${p}`);
    }
    const sel = new Set([...res.specsDirect, ...res.specsOther]);
    specs = specsAll.filter((s) => sel.has(s));
    first = specs.filter((s) => res.specsDirect.has(s));
    if (res.grepRoutes) {
      grep = `^(?!.*page:(?!(?:${res.grepRoutes.join('|')}) ))`;
    }
  }
  const okList = (l) => l.every((x) => SAFE_PATH.test(x.replace(/^\.\//, '')));
  const okGrep = (g) => /^[A-Za-z0-9_|()?!^*.: -]*$/.test(g);
  const okRun = (r) => /^[A-Za-z0-9_|^$()]*$/.test(r);
  const okSeq = (l) => l.every((x) => SEQ_ENTRY.test(x));
  const okAreas = (l) => l.every((x) => /^[a-z][a-z0-9-]*$/.test(x));
  const areas = [...res.areas].sort();
  let core = coreSelection(go, specs, grep, names);
  // Последняя проверка безопасности вывода: иначе — всё
  if (
    !okList(go) ||
    !okList(specs) ||
    !okList(first) ||
    !okSeq(seq) ||
    !okAreas(areas) ||
    !okGrep(grep) ||
    !okList(core.goPkgs) ||
    !okList(core.specs) ||
    !okRun(core.goRun)
  ) {
    all = true;
    go = routerPkgs.map((p) => `./${p}`);
    seq = [];
    specs = specsAll;
    first = [];
    grep = '';
    core = coreSafeAll(routerPkgs, specsAll, core.reasons);
    res.reasons.push('всё: значение выбора не прошло проверку безопасных символов');
  }
  // Ярус full (D-14): спеки выбраны не напрямую по области и не правкой самого спека — тесты
  // @full отсеиваются; иначе (расширение) идут все тесты выбранных спеков. Полный выбор — пусто.
  const specsFullOk = res.specsFullOk ?? new Set();
  const invert = all || specs.some((s) => specsFullOk.has(s)) ? '' : '@full';
  if (invert) {
    const dropped = (res.pwFull ?? []).filter((r) => specs.includes(r.spec)).length;
    res.reasons.push(
      `ярус full: Playwright — тестов @full в выбранных спеках ${dropped} не выбраны — область затронута не напрямую`
    );
  }
  res.reasons.push(...core.reasons);
  const nothing = !all && go.length === 0 && specs.length === 0;
  const kind = all
    ? 'полный'
    : res.harness
      ? 'обвязка'
      : res.deps
        ? 'зависимости'
        : nothing
          ? 'смоук'
          : 'области';
  res.reasons.push(`вид выбора: ${kind}`);
  return [
    `RT_SELECT_ALL=${all ? 1 : 0}`,
    `RT_GO_PKGS=${quote(go.join(' '))}`,
    `RT_SPECS=${quote(specs.join(' '))}`,
    `RT_PW_GREP=${quote(grep)}`,
    `RT_GO_CORE_PKGS=${quote(core.goPkgs.join(' '))}`,
    `RT_GO_CORE_RUN=${quote(core.goRun)}`,
    `RT_SPECS_CORE=${quote(core.specs.join(' '))}`,
    `RT_KERNEL_TOUCHED=${all || res.kernelTouched ? 1 : 0}`,
    `RT_GO_SEQ=${quote(all ? '' : seq.join(' '))}`,
    `RT_SPECS_FIRST=${quote(first.join(' '))}`,
    `RT_PW_INVERT=${quote(invert)}`,
    `RT_HARNESS=${res.harness ? 1 : 0}`,
    `RT_AREAS=${quote(areas.join(' '))}`
  ].join('\n');
}

async function main(argv) {
  const args = { base: null, head: null, files: null, explain: false };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--base') args.base = argv[++i];
    else if (a === '--head') args.head = argv[++i];
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
    let changed;
    if (args.files) {
      changed = args.files.map((f) => path.posix.normalize(f));
    } else {
      const g = changedFromGit(args.base, args.head);
      changed = g.files;
      ctx.from = g.from;
      ctx.to = g.to;
    }
    changedFiles = changed;
    const { res, routerPkgs, specsAll } = await select(changed);
    out = await emit(res, routerPkgs, specsAll);
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
    const res = {
      all: true,
      goSel: new Map(),
      specsDirect: new Set(),
      specsOther: new Set(),
      areas: new Set(),
      harness: false,
      deps: false,
      reasons: [`всё: выбор не удался (${e.message})`]
    };
    out = await emit(res, orEmpty(routerPackages), orEmpty(listRouterSpecs));
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
