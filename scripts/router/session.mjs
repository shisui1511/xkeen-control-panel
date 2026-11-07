#!/usr/bin/env node
// scripts/router/session.mjs — сессия на стенде по спеке (make router-test SUITE=session).
//
// Запуск — только через run.sh: `make router-test SUITE=session SPEC=<спека.json>`. run.sh
// проверяет спеку и чистоту дерева, проверяет метки известных падений, берёт общую
// блокировку, создаёт каталог отчёта (RT_REPORT) и после сессии держит блокировку
// RT_HOLD_MINUTES. Этот файл проводит проходы спеки и сверяет результаты с ожиданиями.
// Этапы — существующие скрипты каталога: deploy.sh и smoke.sh ставят сборку,
// pw-suite.sh --switch переключает ядро через интерфейс панели, go-suite.sh (затем
// settle.sh) и pw-suite.sh гоняют тесты. При любом исходе стенд возвращается: рабочее
// дерево — к HEAD, на цели — сборка HEAD, активное ядро — исходное.
//
// Спека (JSON):
//   {
//     "target": "arm64",                    id цели из локального конфига
//     "cores": ["xray", "mihomo"],          ядра проходов (по умолчанию оба)
//     "keep_going": false,                  необязательно (true/false, по умолчанию false):
//                                           true — все проходы до конца (полный список
//                                           падений), false — остановка на первом
//                                           несовпадении (см. ниже)
//     "passes": [
//       {
//         "name": "before-fix",             [a-z0-9-]
//         "product": {                      необязательно: файлы продукта из другого коммита
//           "ref": "0aae0a7c",              (воспроизведение до исправления); тесты — из HEAD
//           "files": ["frontend/src/Editor.svelte"]
//         },
//         "version": "v0.33.0",             необязательно: сборка HEAD
//                                           под номером вышедшего релиза (тег vX.Y.Z[-<суффикс>.N]
//                                           из локального git или latest — новейший
//                                           опубликованный); сеть и данные GitHub
//                                           настоящие, подменён только номер установленной версии;
//                                           не сочетается с product и с проходом-прогоном; номер
//                                           живёт только в деплое прохода: XCP_VERSION снимается
//                                           со всех этапов, возврат стенда собирает HEAD без номера
//         "go": { "pkgs": "./internal/x", "run": "^TestRouterX$" },    необязательно
//         "specs": ["tests/router/editor-mobile.spec.ts"],              необязательно, от frontend/
//         "grep": "value-missing|backups-switch",                       необязательно, --grep
//         "expect": {
//           "count": 3,                     необязательно: число тестов go:/pw: на ядро
//           "default": "PASS",              статус тестов вне tests; без него они справочные
//           "tests": {
//             "pw:editor-json-tail value-missing vp:1280": { "status": "FAIL", "detail": "no parser" }
//           }
//         }
//       }
//     ]
//   }
// Проход-прогон набора (вместо go/specs): прогон набора router-test целиком как этап сессии.
//   {
//     "name": "narrow-go",                  [a-z0-9-]
//     "suite": "changed",                   full, changed или smoke (с release, см. ниже)
//     "files": ["internal/nodes/router_nodes_test.go"],    только у changed: выбор по списку
//                                           файлов (RT_FILES) вместо git; пути без «..»
//     "expect": {
//       "exit": 0,                          код выхода run.sh, по умолчанию 0
//       "summary": ["- Окно смоука: 30 с"], подстроки summary.md вложенного отчёта
//       "same_failures_as": "full-1",       множество строк FAIL, KNOWN и XPASS (статус, ядро,
//                                           проверка) совпадает с более ранним проходом-прогоном
//       "max_minutes": 30                   у любого прохода-прогона, целое 1…600: вердикт
//     }                                     «время прохода: N мин из max_minutes» по замеру
//   }                                       вложенного прогона
// Вложенная сессия — проход-прогон набора session (проверка самой сессии: ожидаемая остановка,
// предел времени):
//   {
//     "name": "nested",
//     "suite": "session",
//     "spec": "build/spec-check/inner.json", обязателен: путь от корня рабочей копии без «..»,
//                                           файл есть и не совпадает с внешней спекой; цель
//                                           та же; своих проходов session у вложенной спеки
//                                           нет (глубина 1)
//     "expect": { "exit": 1, "max_minutes": 30, "summary": ["Остановлено на первом несовпадении"] }
//   }
// Вложенный `sh run.sh session` идёт с SPEC=<spec>, под блокировкой сессии, отчёт — как у
// прогонов наборов; exit и summary сверяются по её коду и summary.md. files, release,
// interrupt и same_failures_as у session не допускаются. Стенд после неё — по её session.json
// (сборка HEAD, её исходное ядро); прерывание внешней сессии уходит только run.sh вложенной,
// он передаёт сигнал её session.mjs один раз — она возвращает стенд сама.
// Смоук установленного релиза — проход-прогон набора smoke с полем release:
//   {
//     "name": "release-smoke",
//     "suite": "smoke",                     набор smoke: деплой артефакта релиза и смоук
//     "release": "latest",                  тег vX.Y.Z[-<суффикс>.N] или latest — новейший
//                                           опубликованный (не draft) релиз; обязателен у smoke,
//                                           у full и changed запрещён
//     "expect": { "exit": 0, "summary": ["- Статус на GitHub: stand/arm64-smoke success"] }
//   }
// Вложенный `run.sh smoke` идёт с RELEASE=<тег>; статус stand/<arch>-smoke на коммите тега
// публикует сам run.sh. files и interrupt у smoke не допускаются. После прогона на стенде
// остаётся сборка релиза, поэтому следующий проход и конец сессии снова ставят HEAD.
// Прерывание прогона посреди этапа (необязательно, проверка исправления обрыва, G-145.1-5):
//     "interrupt": {
//       "stage": "go",                      обязателен; поддерживается только go
//       "signal": "TERM",                   TERM (kill группе make) или INT (Ctrl+C), по умолчанию TERM
//       "after": 20,                        1..600 с после появления go-<пакет>.raw
//       "deadline": 300                     30..3600 с на выход run.sh, по умолчанию 600
//     }
// Триггер — файл go-<пакет>.raw в <отчёт прохода>/<цель>/<ядро>/: go-suite.sh держит его, пока
// бинарник тестов работает на цели. Вложенный run.sh с interrupt запускается отдельной группой
// процессов (detached), как make в терминале; через after секунд после появления файла сигнал
// уходит всей группе (только при целом pid > 1), не вышел за deadline — SIGKILL группе. Этапы
// обвязки идут под setsid и сигнал не получают: их гасит сам run.sh (план 145.1-16). Вердикты
// прохода с interrupt, кроме exit и summary: «interrupt: сигнал» (отправлен), «interrupt: выход
// run.sh» (до deadline без SIGKILL), «interrupt: процессы на ПК» (нет: группа вложенного
// прогона пуста и нет go-suite/pw-suite/smoke — они держали бы блокировку), «interrupt:
// тестовые процессы на цели» (нет; в отчёте только число), restore-verify, xcp-after-restore и
// core-restored (PASS) и по вердикту на каждую лишнюю строку FAIL и XPASS результатов
// вложенного прогона. Прерывание по спеке не прерывает сессию: следующие проходы идут дальше.
// После SIGKILL run.sh не успевает восстановить стенд и оставляет снимок на цели: стенд по нему
// (rt_restore со сверкой) возвращает сессия, вердикт «interrupt: возврат стенда сессией»; не
// вернулся — сессия останавливается. Снимок, оставшийся к концу сессии, возвращает cleanup.
// С go, specs, grep и product проход-прогон не сочетается. Вложенный `sh run.sh [changed]`
// идёт под блокировкой сессии (RT_LOCK_HELD=1) без RT_HOLD_MINUTES, RT_CORES, RT_BASE и
// SPEC (SPEC — только у session), сам ставит сборку HEAD, снимает снимок и восстанавливает стенд. Его отчёт — в
// build/router/<каталог сессии>-<NN>-<проход> (RT_REPORT_DIR; ссылка last остаётся на
// сессии). В summary.md сессии — раздел «Прогоны наборов»: проход, набор, код, итог,
// оболочка, путь отчёта; те же данные — в session.json (suites).
//
// Статус ожидания — PASS, FAIL, KNOWN, XPASS или SKIP; несколько — через «|» (PASS|KNOWN).
// detail — подстрока детали в results.tsv. Без default тест вне tests справочный, но FAIL
// и XPASS у него — расхождение: неожиданное падение не теряется.
//
// Остановка на первом несовпадении (по умолчанию; "keep_going": true её выключает): красная
// сессия не дожидается конца, когда итог уже ясен. Первое несовпадение с ожиданиями:
//   - Go-этап — по мере вывода: сессия раз в секунду дочитывает файлы go-*.raw этапа в
//     <этап>/<цель>/<ядро>/ (вывод -test.v=test2json, пока бинарник работает на цели) и готовые
//     go-*.parsed пакетов; строка `--- FAIL: <тест>`, чей статус (XPASS при маркере
//     `XPASS slug=`, QFAIL при маркере `QUARANTINE slug=` у теста или предка, иначе FAIL)
//     ожидание теста не допускает, останавливает этап: TERM группе процессов этапа (как при
//     прерывании), ожидание выхода и опустения группы, затем добивание тестовых бинарников на
//     цели. Родитель, упавший из-за подтеста, отдельно не сверяется (как в go-parse.awk);
//     деталь (detail) по ходу вывода не сверяется — её сверяет разбор после этапа;
//   - Playwright, деплой со смоуком и прогоны наборов — после этапа (сверка results.tsv);
//     несовпадение в Go-части прохода со спеками не пускает этап Playwright.
// Остановленный проход сверяется по уже записанным результатам: тест, на котором остановились, —
// несовпадение, невыполненные тесты не сверяются. Следующие ядра и проходы не запускаются,
// стенд возвращается как при любом исходе (сборка HEAD, исходное ядро). В summary.md — раздел
// «## Остановлено на первом несовпадении»: проход, ядро, тест, ожидаемый и полученный статус и
// «Не выполнено:» (ядра прохода и проходы); код выхода 1. Несовпадение в последнем этапе
// остановкой не считается: невыполненного нет.
//
// Порядок: проходы по очереди; внутри прохода ядра змейкой, первым — активное. На N проходов
// приходится не больше N переключений, и ещё одно в конце, если стенд не на исходном ядре.
// Сборка ставится в начале и при смене продукта.
//
// Отчёт — в RT_REPORT: <NN>-<этап>/ — отчёты этапов в формате router-test (meta,
// <цель>/results.tsv), spec.json, session.log (вывод всех этапов), summary.md и session.json
// (итог по ожиданиям).
//
// Режимы без стенда (из корня репозитория):
//   node scripts/router/session.mjs --dry-run <спека>   план этапов
//   node scripts/router/session.mjs --target <спека>    проверить спеку и напечатать цель
//
// Код выхода сессии: 0 — все ожидания совпали и стенд вернулся в исходное состояние;
// 1 — расхождение, или этап сломался, или стенд не вернулся; 2 — неверная спека или запуск
// не через run.sh; 3 — сбой связи ПК↔стенд (не красный прогон, разбор по AGENTS.md; в том
// числе FAIL reachable во вложенном прогоне набора); 130 — прерывание.
// Прерывание во время вложенного прогона: сигнал уходит всей группе процессов текущего этапа
// (каждый этап и вложенный run.sh идут отдельной группой), сессия ждёт выхода run.sh (стенд по
// снимку он восстанавливает сам) и опустения группы; повторный сигнал — выход без возврата.
// Прерывание по спеке (interrupt) сессию не прерывает: после выхода вложенного прогона
// сессия сверяет вердикты и продолжает следующие проходы.

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import {
  appendFileSync,
  closeSync,
  existsSync,
  fstatSync,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  readSync,
  realpathSync,
  rmSync,
  writeFileSync
} from 'node:fs';
import { constants as osConstants } from 'node:os';
import path from 'node:path';
import { StringDecoder } from 'node:string_decoder';
import { fileURLToPath } from 'node:url';

const SCRIPT = fileURLToPath(import.meta.url);
const SCRIPT_DIR = path.dirname(SCRIPT);
const CORES = ['xray', 'mihomo'];
const STATUSES = ['PASS', 'FAIL', 'KNOWN', 'XPASS', 'SKIP'];
const SAFE_PATH = /^[A-Za-z0-9_.][A-Za-z0-9_./-]*$/;
const SAFE_NAME = /^[a-z0-9][a-z0-9-]*$/;
const SAFE_REF = /^[A-Za-z0-9_.][A-Za-z0-9_./~^-]*$/;
// Тег релиза: стабильный или пре-релиз с любым буквенным суффиксом (rc, beta).
const RELEASE_TAG = /^v\d+\.\d+\.\d+(-[a-z]+\.\d+)?$/;
// Снимок незавершённого прогона на цели (RT_SNAP_DIR из lib.sh): с ним preflight отказывает.
const SNAP_BASE = '/opt/tmp/xcp-rt-snap/baseline';
// Служебные строки этапов: всегда расхождение, если их FAIL не ожидался явно.
const SERVICE = new Set(['pw:run', 'pw:setup']);

// Префикс ошибок разбора вложенной спеки (проход-прогон session).
let dieCtx = '';

function die(msg, code = 2) {
  process.stderr.write(`session: ${dieCtx}${msg}\n`);
  process.exit(code);
}

function git(...args) {
  return execFileSync('git', args, { encoding: 'utf8' }).trim();
}

// --- аргументы и спека --------------------------------------------------------

function parseArgs(argv) {
  const out = { spec: '', mode: 'run' };
  for (const a of argv) {
    if (a === '--dry-run') out.mode = 'dry-run';
    else if (a === '--target') out.mode = 'target';
    else if (a === '-h' || a === '--help') {
      const lines = readFileSync(SCRIPT, 'utf8').split('\n').slice(1);
      const head = lines.slice(
        0,
        lines.findIndex((l) => !l.startsWith('//'))
      );
      process.stdout.write(head.map((l) => l.replace(/^\/\/ ?/, '')).join('\n') + '\n');
      process.exit(0);
    } else if (!out.spec && !a.startsWith('--')) out.spec = a;
    else die(`неизвестный аргумент: ${a}`);
  }
  if (!out.spec) die('usage: session.mjs [--dry-run | --target] <спека.json>');
  return out;
}

function parseStatus(s, where) {
  if (typeof s !== 'string' || !s) die(`${where}: статус должен быть строкой`);
  const parts = s.split('|');
  for (const p of parts) {
    if (!STATUSES.includes(p)) die(`${where}: неизвестный статус «${p}» (${STATUSES.join(', ')})`);
  }
  return parts;
}

// Статусы, которые ожидание прохода допускает для теста: явное ожидание из tests, иначе
// default; без них тест справочный — допустимо всё, кроме FAIL и XPASS.
function expectedOf(expect, name) {
  const want = expect.tests.get(name);
  if (want) return want.status;
  if (SERVICE.has(name)) return ['PASS'];
  if (expect.def) return expect.def;
  return ['справочно'];
}

function allowedStatus(expect, name, status) {
  const want = expectedOf(expect, name);
  if (want[0] === 'справочно') return status !== 'FAIL' && status !== 'XPASS';
  return want.includes(status);
}

// Поле interrupt прохода-прогона: прерывание вложенного run.sh посреди этапа go.
function loadInterrupt(i, where) {
  if (!i || typeof i !== 'object' || Array.isArray(i)) die(`${where}.interrupt: объект`);
  for (const k of Object.keys(i)) {
    if (!['stage', 'signal', 'after', 'deadline'].includes(k)) {
      die(`${where}.interrupt.${k}: допустимы stage, signal, after, deadline`);
    }
  }
  if (i.stage !== 'go') die(`${where}.interrupt.stage: поддерживается только go`);
  const signal = i.signal ?? 'TERM';
  if (signal !== 'TERM' && signal !== 'INT') die(`${where}.interrupt.signal: TERM или INT`);
  if (!Number.isInteger(i.after) || i.after < 1 || i.after > 600) {
    die(`${where}.interrupt.after: целое от 1 до 600 (с)`);
  }
  const deadline = i.deadline ?? 600;
  if (!Number.isInteger(deadline) || deadline < 30 || deadline > 3600) {
    die(`${where}.interrupt.deadline: целое от 30 до 3600 (с)`);
  }
  return { stage: 'go', signal, after: i.after, deadline };
}

// Название прохода-прогона для плана и журнала: «smoke (релиз <тег>)», «changed по файлам …».
function suiteTitle(p, release) {
  if (p.suite === 'smoke') return `smoke (релиз ${release})`;
  if (p.suite === 'session') return `session по ${p.nested.path}`;
  return `${p.suite}${p.files.length ? ` по файлам ${p.files.join(' ')}` : ''}`;
}

// Проход-прогон набора: разбор и проверка полей. earlier — имена прогонов набора выше по спеке,
// ctx — { root, specFile, nested } внешней спеки (nested: спека сама вложенная).
function loadSuitePass(p, where, earlier, ctx) {
  if (!['full', 'changed', 'smoke', 'session'].includes(p.suite)) {
    die(`${where}.suite: full, changed, smoke или session`);
  }
  if (p.go || p.specs !== undefined || p.grep !== undefined || p.product) {
    die(`${where}: проход-прогон не сочетается с go/specs/product`);
  }
  if (p.version !== undefined) die(`${where}.version: только у прохода go/specs`);
  let nested = null;
  if (p.suite === 'session') {
    if (ctx.nested) die(`${where}: вложенная сессия не может содержать сессию`);
    if (p.interrupt !== undefined) die(`${where}.interrupt: у session не поддерживается`);
    if (typeof p.spec !== 'string' || !SAFE_PATH.test(p.spec) || p.spec.includes('..')) {
      die(`${where}.spec: путь спеки от корня рабочей копии без «..»`);
    }
    const file = path.join(ctx.root, p.spec);
    if (!existsSync(file)) die(`${where}.spec: нет файла ${p.spec}`);
    if (realpathSync(file) === realpathSync(ctx.specFile)) {
      die(`${where}.spec: вложенная спека совпадает с внешней`);
    }
    dieCtx = `${where}.spec ${p.spec}: `;
    const inner = loadSpec(file, ctx.root, true);
    dieCtx = '';
    nested = { path: p.spec, passes: inner.passes.length, target: inner.target };
    if (inner.target !== ctx.target) {
      die(`${where}.spec: цель вложенной спеки ${inner.target}, у внешней ${ctx.target}`);
    }
  } else if (p.spec !== undefined) {
    die(`${where}.spec: только у session`);
  }
  let release = null;
  if (p.suite === 'smoke') {
    if (typeof p.release !== 'string' || (p.release !== 'latest' && !RELEASE_TAG.test(p.release))) {
      die(`${where}.release: у smoke обязателен — latest или тег vX.Y.Z[-<суффикс>.N]`);
    }
    release = p.release;
    if (p.interrupt !== undefined) die(`${where}.interrupt: у smoke не поддерживается`);
  } else if (p.release !== undefined) {
    die(`${where}.release: только у smoke`);
  }
  let files = [];
  if (p.files !== undefined) {
    if (p.suite !== 'changed') die(`${where}.files: только у changed`);
    if (!Array.isArray(p.files) || !p.files.length) die(`${where}.files: непустой список путей`);
    for (const f of p.files) {
      if (typeof f !== 'string' || !SAFE_PATH.test(f) || f.includes('..')) {
        die(`${where}.files: недопустимый путь ${f}`);
      }
    }
    files = [...new Set(p.files)];
  }
  const e = p.expect ?? {};
  for (const k of Object.keys(e)) {
    if (!['exit', 'summary', 'same_failures_as', 'max_minutes'].includes(k)) {
      die(
        `${where}.expect.${k}: у прохода-прогона только exit, summary, same_failures_as, max_minutes`
      );
    }
  }
  if (e.exit !== undefined && (!Number.isInteger(e.exit) || e.exit < 0)) {
    die(`${where}.expect.exit: целое от 0`);
  }
  if (
    e.max_minutes !== undefined &&
    (!Number.isInteger(e.max_minutes) || e.max_minutes < 1 || e.max_minutes > 600)
  ) {
    die(`${where}.expect.max_minutes: целое от 1 до 600`);
  }
  if (p.suite === 'session' && e.same_failures_as !== undefined) {
    die(`${where}.expect.same_failures_as: у session не поддерживается`);
  }
  const summary = e.summary ?? [];
  if (!Array.isArray(summary) || summary.some((x) => typeof x !== 'string' || !x)) {
    die(`${where}.expect.summary: список непустых строк`);
  }
  let same = null;
  if (e.same_failures_as !== undefined) {
    if (typeof e.same_failures_as !== 'string' || !earlier.includes(e.same_failures_as)) {
      die(`${where}.expect.same_failures_as: нужно имя более раннего прохода-прогона`);
    }
    same = e.same_failures_as;
  }
  return {
    name: p.name,
    suite: p.suite,
    release,
    files,
    nested,
    interrupt: p.interrupt === undefined ? null : loadInterrupt(p.interrupt, where),
    version: null,
    product: null,
    go: null,
    specs: [],
    grep: '',
    expect: { exit: e.exit ?? 0, summary, same, maxMinutes: e.max_minutes ?? null }
  };
}

// nested — спека прохода-прогона session (глубина вложения 1: своих session у неё нет).
function loadSpec(file, root, nested = false) {
  let spec;
  try {
    spec = JSON.parse(readFileSync(file, 'utf8'));
  } catch (e) {
    die(`спека ${file} не читается: ${e.message}`);
  }
  if (typeof spec.target !== 'string' || !/^[a-z0-9]+$/.test(spec.target)) {
    die('target: id цели из a-z и 0-9');
  }
  const cores = spec.cores ?? CORES;
  if (!Array.isArray(cores) || !cores.length || cores.some((c) => !CORES.includes(c))) {
    die(`cores: непустой список из ${CORES.join(', ')}`);
  }
  if (new Set(cores).size !== cores.length) die('cores: ядро указано дважды');
  const keepGoing = spec.keep_going ?? false;
  if (typeof keepGoing !== 'boolean') die('keep_going: true или false');
  if (!Array.isArray(spec.passes) || !spec.passes.length) die('passes: непустой список');
  const names = new Set();
  const suites = [];
  const passes = spec.passes.map((p, i) => {
    const where = `passes[${i}]`;
    if (typeof p.name !== 'string' || !SAFE_NAME.test(p.name)) die(`${where}.name: [a-z0-9-]`);
    if (names.has(p.name)) die(`${where}.name: «${p.name}» повторяется`);
    names.add(p.name);
    if (p.suite === undefined && p.interrupt !== undefined) {
      die(`${where}.interrupt: только у прохода-прогона`);
    }
    if (p.suite !== undefined) {
      const sp = loadSuitePass(p, where, suites, {
        root,
        specFile: file,
        nested,
        target: spec.target
      });
      // same_failures_as сравнивает строки results.tsv прогонов наборов; у вложенной сессии их нет
      if (sp.suite !== 'session') suites.push(sp.name);
      return sp;
    }
    let version = null;
    if (p.version !== undefined) {
      if (p.product) die(`${where}: version и product не сочетаются`);
      if (typeof p.version !== 'string' || (p.version !== 'latest' && !RELEASE_TAG.test(p.version))) {
        die(`${where}.version: latest или тег vX.Y.Z[-<суффикс>.N] вышедшего релиза`);
      }
      if (p.version !== 'latest' && !localTagExists(p.version)) {
        die(
          `${where}.version: тега ${p.version} нет — нужен номер вышедшего релиза (git fetch --tags)`
        );
      }
      version = p.version;
    }
    let product = null;
    if (p.product) {
      const { ref, files } = p.product;
      if (typeof ref !== 'string' || !SAFE_REF.test(ref))
        die(`${where}.product.ref: недопустимая ссылка`);
      let sha;
      try {
        sha = git('rev-parse', '--verify', '--quiet', `${ref}^{commit}`);
      } catch {
        die(`${where}.product.ref: коммит ${ref} не найден`);
      }
      if (!Array.isArray(files) || !files.length) die(`${where}.product.files: непустой список`);
      for (const f of files) {
        if (typeof f !== 'string' || !SAFE_PATH.test(f) || f.includes('..')) {
          die(`${where}.product.files: недопустимый путь ${f}`);
        }
        if (spawnSync('git', ['cat-file', '-e', `${sha}:${f}`]).status !== 0) {
          die(`${where}.product.files: ${f} нет в ${ref}`);
        }
      }
      product = { ref, sha, files: [...new Set(files)] };
    }
    let go = null;
    if (p.go) {
      const pkgs = String(p.go.pkgs || '');
      if (!pkgs.split(' ').every((s) => SAFE_PATH.test(s)))
        die(`${where}.go.pkgs: пакеты через пробел`);
      const run = p.go.run === undefined ? '' : String(p.go.run);
      if (/[\n\r]/.test(run)) die(`${where}.go.run: одна строка`);
      go = { pkgs, run };
    }
    const specs = p.specs ?? [];
    if (!Array.isArray(specs)) die(`${where}.specs: список путей от frontend/`);
    for (const s of specs) {
      if (
        typeof s !== 'string' ||
        !SAFE_PATH.test(s) ||
        !existsSync(path.join(root, 'frontend', s))
      ) {
        die(`${where}.specs: нет спека frontend/${s}`);
      }
    }
    const grep = p.grep === undefined ? '' : String(p.grep);
    if (/[\n\r]/.test(grep)) die(`${where}.grep: одна строка`);
    if (grep && !specs.length) die(`${where}.grep без specs: укажите спеки явно`);
    if (!go && !specs.length) die(`${where}: нечего запускать (нет go и specs)`);
    const e = p.expect ?? {};
    const expect = { count: null, def: null, tests: new Map() };
    if (e.count !== undefined) {
      if (!Number.isInteger(e.count) || e.count < 1) die(`${where}.expect.count: целое больше 0`);
      expect.count = e.count;
    }
    if (e.default !== undefined) expect.def = parseStatus(e.default, `${where}.expect.default`);
    for (const [name, t] of Object.entries(e.tests ?? {})) {
      if (!/^(pw|go):./.test(name))
        die(`${where}.expect.tests: имя «${name}» должно начинаться с pw: или go:`);
      const tw = `${where}.expect.tests["${name}"]`;
      const status = parseStatus(typeof t === 'string' ? t : t.status, tw);
      const detail = typeof t === 'object' && t.detail !== undefined ? String(t.detail) : null;
      expect.tests.set(name, { status, detail });
    }
    if (!expect.def && !expect.tests.size && expect.count === null) {
      die(`${where}.expect: нужен хотя бы один из count, default, tests`);
    }
    return { name: p.name, version, product, go, specs, grep, expect };
  });
  return { target: spec.target, cores, keepGoing, passes };
}

// Ядра прохода змейкой: сначала активное, затем остальные в порядке спеки.
function coreOrder(cores, cur) {
  return cores.includes(cur) ? [cur, ...cores.filter((c) => c !== cur)] : [...cores];
}

// Тег есть в локальном git (коммит, на который он указывает, достижим).
function localTagExists(tag) {
  return (
    spawnSync('git', ['rev-parse', '--verify', '--quiet', `refs/tags/${tag}^{commit}`]).status === 0
  );
}

// Новейший опубликованный (не draft) релиз GitHub; при сбое gh бросает.
function latestReleaseTag() {
  return execFileSync(
    'gh',
    [
      'release',
      'list',
      '--exclude-drafts',
      '--limit',
      '1',
      '--json',
      'tagName',
      '--jq',
      '.[0].tagName'
    ],
    { encoding: 'utf8', timeout: 30000 }
  ).trim();
}

// Ключ сборки: HEAD@<номер> всегда отличается от HEAD, поэтому смена номера вызывает деплой,
// а завершающий возврат стенда (deployed !== 'HEAD') ставит HEAD со своей версией.
function productKey(p) {
  if (p.version) return `HEAD@${p.resolved ?? p.version}`;
  return p.product ? `${p.product.sha}:${p.product.files.join(',')}` : 'HEAD';
}

// План сессии без стенда. Исходное ядро неизвестно до смоука: план считает его первым в
// cores, на стенде с другим активным ядром порядок ядер зеркальный.
function dryRun(spec, specFile) {
  const c0 = spec.cores[0];
  const lines = [
    `Сессия по ${specFile}: цель ${spec.target}, ядра ${spec.cores.join(', ')} (исходное считается ${c0})`,
    spec.keepGoing
      ? 'keep_going: все проходы до конца, полный список падений'
      : 'остановка на первом несовпадении (keep_going: false): Go — по ходу вывода, остальное — после этапа'
  ];
  let n = 0;
  const step = (s) => lines.push(`${++n}. ${s}`);
  let deployed = null;
  let cur = c0;
  for (const p of spec.passes) {
    if (p.suite) {
      // вложенный прогон ставит сборку HEAD сам и оставляет активным ядро, найденное в начале
      const irq = p.interrupt
        ? `; прерывание SIG${p.interrupt.signal} через ${p.interrupt.after} с после старта go, выход до ${p.interrupt.deadline} с`
        : '';
      if (p.suite === 'session') {
        step(
          `вложенная сессия по ${p.nested.path}, проходов: ${p.nested.passes}; отчёт build/router/<сессия>-NN-${p.name}`
        );
      } else {
        step(
          `прогон набора ${suiteTitle(p, p.release)}: вложенный run.sh, отчёт build/router/<сессия>-NN-${p.name}${irq}`
        );
      }
      if (p.expect.maxMinutes !== null) {
        lines.push(
          `   max_minutes: прогон прохода ${p.name} — не дольше ${p.expect.maxMinutes} мин`
        );
      }
      // смоук релиза оставляет на стенде сборку релиза, а не HEAD; вложенная сессия
      // возвращает стенд сама (сборка HEAD и её исходное ядро)
      deployed = p.suite === 'smoke' ? null : 'HEAD';
      continue;
    }
    const key = productKey(p);
    if (key !== deployed) {
      const target = p.version
        ? `HEAD под номером ${p.version}`
        : p.product
          ? `${p.product.files.join(' ')} из ${p.product.ref}`
          : 'HEAD';
      step(`деплой и смоук: ${target}`);
      deployed = key;
    }
    const what = [
      p.go ? `go ${p.go.pkgs}${p.go.run ? ` -run ${p.go.run}` : ''}` : '',
      p.specs.length ? `pw ${p.specs.join(' ')}${p.grep ? ` --grep '${p.grep}'` : ''}` : ''
    ]
      .filter(Boolean)
      .join(' + ');
    for (const c of coreOrder(spec.cores, cur)) {
      if (c !== cur) step(`переключение на ${c}`);
      cur = c;
      step(`проход ${p.name} на ${c}: ${what}`);
    }
  }
  if (deployed !== 'HEAD') step('деплой и смоук: HEAD (возврат продукта)');
  if (cur !== c0) step(`переключение на ${c0} (исходное)`);
  step(
    'проверка стенда: дерево чистое, сборка HEAD, исходное ядро, xcp запущен, снимка незавершённого прогона нет'
  );
  process.stdout.write(lines.join('\n') + '\n');
}

// --- сессия -------------------------------------------------------------------

class Session {
  constructor(spec, specFile, root) {
    this.spec = spec;
    this.root = root;
    this.t = spec.target;
    this.head = git('rev-parse', 'HEAD');
    this.dir = process.env.RT_REPORT;
    mkdirSync(this.dir, { recursive: true });
    writeFileSync(path.join(this.dir, 'spec.json'), readFileSync(specFile));
    this.log = path.join(this.dir, 'session.log');
    this.steps = [];
    this.verdicts = [];
    this.suites = [];
    this.suiteFails = new Map();
    this.notes = [];
    this.n = 0;
    this.c0 = null;
    this.cur = null;
    this.deployed = null;
    this.overridden = null;
    this.child = null;
    this.aborted = false;
    this.cleaning = false;
    this.network = false;
    this.broken = false;
    // остановка на первом несовпадении: { pass, core, test, expected, actual, how, live, todo }
    this.stopped = null;
    // вердикты с этим индексом и дальше ещё не проверены на остановку (checkStop)
    this.checked = 0;
  }

  say(msg) {
    const line = `session: ${msg}\n`;
    process.stdout.write(line);
    appendFileSync(this.log, line);
  }

  // Каталог этапа в формате отчёта router-test (meta как у rt_report_init).
  stepDir(label) {
    const name = `${String(++this.n).padStart(2, '0')}-${label}`;
    const dir = path.join(this.dir, name);
    mkdirSync(path.join(dir, this.t), { recursive: true });
    const dirty = git('status', '--porcelain', '--untracked-files=no') !== '';
    writeFileSync(
      path.join(dir, 'meta'),
      `sha=${this.head}\ndirty=${dirty}\nstarted=${new Date().toISOString().slice(0, 19)}Z\n`
    );
    return dir;
  }

  // Запуск этапа: вывод — в консоль и session.log. Код выхода (сигнал — 128+n).
  // opts.detached — отдельная группа процессов; opts.onSpawn(child) вызывается сразу после
  // spawn и может вернуть функцию, которая будет вызвана при закрытии процесса; opts.drainIf()
  // — true, если после выхода лидера нужно дождаться опустения группы.
  run(cmd, args, env = {}, opts = {}) {
    if (this.aborted && !this.cleaning) return Promise.resolve(130);
    const base = { ...process.env };
    for (const k of [
      'RT_SPECS',
      'RT_PW_GREP',
      'RT_GO_PKGS',
      'RT_GO_RUN',
      'RT_REPORT',
      'RT_REPORT_DIR',
      'RT_CORE',
      'XCP_WANT_CORE',
      // номер сборки задаёт только проход с version (env его деплоя), не оператор и не прошлый этап
      'XCP_VERSION'
    ]) {
      delete base[k];
    }
    const full = { ...base, RT_LOCK_HELD: '1', ROUTERS: this.t, ...env };
    // undefined в env — снять переменную (вложенный прогон набора не наследует настройки сессии)
    for (const k of Object.keys(full)) if (full[k] === undefined) delete full[k];
    appendFileSync(this.log, `\n$ ${[cmd, ...args].join(' ')}\n`);
    return new Promise((resolve) => {
      const child = spawn(cmd, args, {
        cwd: this.root,
        env: full,
        stdio: ['ignore', 'pipe', 'pipe'],
        // каждый этап — своя группа процессов: сигнал прерывания уходит всей группе (этап и
        // его ssh, вложенный run.sh и его deploy.sh), а не одному pid
        detached: opts.detached !== false
      });
      this.child = child;
      this.childLeaderOnly = opts.leaderOnly === true;
      const onClose = opts.onSpawn ? opts.onSpawn(child) : null;
      const out = (d) => {
        process.stdout.write(d);
        appendFileSync(this.log, d);
      };
      child.stdout.on('data', out);
      child.stderr.on('data', out);
      child.on('close', (code, sig) => {
        this.child = null;
        if (onClose) onClose();
        const rc = code ?? 128 + (osConstants.signals[sig] ?? 15);
        // После прерывания потомки группы (deploy.sh вложенного прогона, ssh этапа) могли
        // пережить лидера: возврат стенда не стартует, пока группа не опустеет, иначе он шёл бы
        // одновременно с ними
        const pid = child.pid;
        // opts.drainIf — этап остановлен сессией (первое несовпадение): ждать группу и при коде < 128
        const interrupted = this.aborted || rc >= 128 || (opts.drainIf ? opts.drainIf() : false);
        if (opts.drain !== false && interrupted && Number.isInteger(pid) && pid > 1) {
          this.drainGroup(pid).then(() => resolve(rc));
        } else {
          resolve(rc);
        }
      });
    });
  }

  // Сигнал текущему этапу: всей его группе (лидер группы — сам этап, см. run); не вышло —
  // самому процессу.
  signalChild(sig) {
    const c = this.child;
    if (!c) return;
    const pid = c.pid;
    // вложенная сессия: сигнал только run.sh — он передаёт его session.mjs один раз; сигнал всей
    // группе дошёл бы до session.mjs дважды, а второй сигнал — выход без возврата стенда
    if (!this.childLeaderOnly && Number.isInteger(pid) && pid > 1) {
      try {
        process.kill(-pid, sig);
        return;
      } catch {
        // группы нет — сигнал самому процессу
      }
    }
    try {
      c.kill(sig);
    } catch {
      // процесс уже вышел
    }
  }

  // Ждёт, пока группа процессов опустеет: до 30 с, затем SIGKILL группе и ещё до 5 с.
  async drainGroup(pgid) {
    const alive = () => {
      try {
        process.kill(-pgid, 0);
        return true;
      } catch (e) {
        return e.code !== 'ESRCH';
      }
    };
    const pause = (ms) => new Promise((r) => setTimeout(r, ms));
    for (let i = 0; i < 60 && alive(); i++) await pause(500);
    if (!alive()) return;
    this.say(`потомки этапа живы после прерывания: SIGKILL группе ${pgid}`);
    try {
      process.kill(-pgid, 'SIGKILL');
    } catch {
      // группа уже вышла
    }
    for (let i = 0; i < 10 && alive(); i++) await pause(500);
  }

  // Команда на цели через rt_ssh из lib.sh; печатает stdout команды.
  remote(cmd) {
    const r = spawnSync(
      'sh',
      [
        '-c',
        '. "$1/lib.sh"; rt_load_targets >/dev/null && rt_ssh "$2" "$3"',
        'sh',
        SCRIPT_DIR,
        this.t,
        cmd
      ],
      {
        cwd: this.root,
        env: { ...process.env, ROUTERS: this.t },
        encoding: 'utf8'
      }
    );
    return { rc: r.status ?? 1, out: (r.stdout || '').trim() };
  }

  record(step) {
    this.steps.push(step);
    if (step.rc === 3) this.network = true;
  }

  // --- этапы ---

  applyProduct(p) {
    this.restoreProduct();
    if (!p.product) return;
    this.say(`файлы продукта из ${p.product.ref}: ${p.product.files.join(' ')}`);
    git('checkout', p.product.sha, '--', ...p.product.files);
    this.overridden = p.product.files;
  }

  restoreProduct() {
    if (!this.overridden) return;
    for (const f of this.overridden) {
      if (spawnSync('git', ['cat-file', '-e', `HEAD:${f}`]).status === 0)
        git('checkout', 'HEAD', '--', f);
      else rmSync(path.join(this.root, f), { force: true });
    }
    this.overridden = null;
  }

  // Деплой сборки рабочего дерева и смоук в один каталог этапа. Ядро, найденное смоуком,
  // становится текущим. Код: 0 — сборка стоит (смоук мог быть красным: это расхождение),
  // 3 — нет связи, иначе деплой не удался.
  // version — номер вышедшего релиза: только деплой этого прохода идёт с XCP_VERSION, смоук
  // сверяет xcp -v с этим номером (файл version этапа).
  async deployAndSmoke(key, label, version = null) {
    const dir = this.stepDir(`deploy-${label}`);
    this.say(`деплой и смоук: ${label} → ${path.relative(this.root, dir)}`);
    const drc = await this.run('sh', [path.join(SCRIPT_DIR, 'deploy.sh'), '--target', this.t], {
      RT_REPORT: dir,
      XCP_VERSION: version ?? undefined
    });
    this.record({ kind: 'deploy', label, dir, rc: drc, version });
    if (drc !== 0) {
      this.deployed = null;
      if (drc !== 3) this.broken = true;
      return drc;
    }
    this.deployed = key;
    const ver = readFileSync(path.join(dir, 'version'), 'utf8').trim();
    const src = await this.run('sh', [path.join(SCRIPT_DIR, 'smoke.sh'), this.t, ver], {
      RT_REPORT: dir
    });
    this.record({ kind: 'smoke', label, dir, rc: src, version });
    const coreFile = path.join(dir, this.t, 'core');
    const core = existsSync(coreFile) ? readFileSync(coreFile, 'utf8').trim() : '-';
    if (CORES.includes(core)) this.cur = core;
    if (src !== 0 && src !== 3) {
      this.verdicts.push({
        pass: '-',
        core: core,
        test: `smoke (${label})`,
        expected: ['PASS'],
        actual: 'FAIL',
        detail: 'смоук красный: см. results.tsv этапа',
        ok: false,
        report: path.relative(this.root, dir)
      });
    }
    return src === 3 ? 3 : 0;
  }

  // Номер прохода с version: latest разрешается в новейший опубликованный релиз, результат
  // должен быть тегом релиза и существовать в локальном git. Не вышло — проход не запускается:
  // вердикт с причиной, сессия сломана (стенд вернёт cleanup). Итог — в p.resolved.
  resolveVersion(p) {
    if (p.resolved) return true;
    let tag = p.version;
    let err = '';
    if (tag === 'latest') {
      try {
        tag = latestReleaseTag();
      } catch (e) {
        err = `gh release list не отработал: ${e.message.split('\n')[0]}`;
      }
    }
    if (!err && !RELEASE_TAG.test(tag)) {
      err = `новейший релиз не определён или тег «${tag}» не похож на vX.Y.Z`;
    }
    if (!err && !localTagExists(tag)) err = `тега ${tag} нет в локальном git (git fetch --tags)`;
    if (err) {
      this.say(`проход ${p.name} не начат: ${err}`);
      this.verdicts.push({
        pass: p.name,
        core: '-',
        test: 'version: тег',
        expected: [p.version],
        actual: 'не определён',
        detail: err,
        ok: false,
        report: '-'
      });
      this.broken = true;
      return false;
    }
    p.resolved = tag;
    this.say(`проход ${p.name}: сборка HEAD под номером ${tag}`);
    return true;
  }

  async switchTo(core) {
    const dir = this.stepDir(`switch-${core}`);
    this.say(`переключение ядра ${this.cur ?? '-'} → ${core}`);
    const rc = await this.run(
      'sh',
      [path.join(SCRIPT_DIR, 'pw-suite.sh'), this.t, core, '--switch', core],
      {
        RT_REPORT: dir
      }
    );
    this.record({ kind: 'switch', core, dir, rc });
    if (rc === 0) this.cur = core;
    else if (rc !== 3) this.broken = true;
    return rc;
  }

  // Возврат стенда по снимку, который оставил незавершённый вложенный прогон (SIGKILL по
  // deadline или повторный сигнал посреди восстановления): тем же rt_restore со сверкой и
  // удалением снимка. Без этого следующий прогон откажет на preflight, а стенд останется в
  // состоянии теста. Итог: { had: снимок был, ok: снимка нет или стенд сверен со снимком,
  // detail }.
  async restoreLeftover(label) {
    const r = this.remote(`[ -d ${SNAP_BASE} ] && echo snap=yes || echo snap=no`);
    if (r.rc === 3) {
      this.network = true;
      return { had: false, ok: false, detail: 'нет связи' };
    }
    if (r.rc !== 0 || !/^snap=(yes|no)$/m.test(r.out)) {
      return {
        had: false,
        ok: false,
        detail: `наличие снимка не проверено (код ${r.rc})`
      };
    }
    if (/^snap=no$/m.test(r.out)) return { had: false, ok: true, detail: 'снимка нет' };
    const dir = this.stepDir(`restore-${label}`);
    this.say(
      `остался снимок незавершённого прогона: возврат стенда → ${path.relative(this.root, dir)}`
    );
    const rc = await this.run(
      'sh',
      [
        '-c',
        '. "$1/lib.sh"; rt_load_targets >/dev/null || exit 2; rt_restore "$2"',
        'sh',
        SCRIPT_DIR,
        this.t
      ],
      { RT_REPORT: dir }
    );
    this.record({ kind: 'restore', label, dir, rc });
    const res = path.join(dir, this.t, 'results.tsv');
    let detail = `код ${rc}`;
    if (existsSync(res)) {
      const row = readFileSync(res, 'utf8')
        .split('\n')
        .map((l) => l.split('\t'))
        .find((f) => f[2] === 'restore-verify');
      if (row) {
        detail = row[3] ?? '';
        if (row[0] !== 'PASS' && detail.startsWith('нет связи,')) this.network = true;
      }
    }
    return { had: true, ok: rc === 0, detail };
  }

  async runPass(p, core) {
    const dir = this.stepDir(`${p.name}-${core}`);
    this.say(`проход ${p.name} на ${core} → ${path.relative(this.root, dir)}`);
    let rc = 0;
    if (p.go) {
      const env = { RT_REPORT: dir, RT_GO_PKGS: p.go.pkgs };
      if (p.go.run) env.RT_GO_RUN = p.go.run;
      const watch = this.spec.keepGoing ? {} : this.goWatch(p, core, dir);
      const g = await this.run(
        'sh',
        [path.join(SCRIPT_DIR, 'go-suite.sh'), this.t, core],
        env,
        watch
      );
      this.record({ kind: 'go', pass: p.name, core, dir, rc: g, version: p.resolved ?? null });
      if (g === 3 && !this.stopped?.live) return 3;
      if (!this.stopped && p.specs.length && !this.spec.keepGoing && !this.aborted) {
        // несовпадение в Go-части, которое видно только после этапа (например, PASS вместо
        // ожидаемого FAIL), не пускает этап Playwright
        const m = this.firstMismatch(p, dir);
        if (m) this.setStopped({ pass: p.name, core, ...m, how: 'после этапа go', live: false });
      }
      if (this.stopped) {
        // этап остановлен посреди пакета: тестовый бинарник на цели мог пережить обрыв ssh
        if (this.stopped.live) this.killRemoteTests();
        if (p.specs.length) this.stopped.todo.push(`проход ${p.name} на ${core}: этап Playwright`);
        this.judge(p, core, dir);
        return 0;
      }
      if (p.specs.length) {
        // после разрушающих Go-тестов спеки начинают с рабочего стенда (как в run.sh)
        const s = spawnSync(
          'sh',
          [
            '-c',
            '. "$1/lib.sh"; rt_load_targets >/dev/null && RT_SSH_STDIN=1 rt_ssh "$2" "sh -s" <"$1/settle.sh"',
            'sh',
            SCRIPT_DIR,
            this.t
          ],
          {
            cwd: this.root,
            env: { ...process.env, ROUTERS: this.t },
            encoding: 'utf8'
          }
        );
        const st = s.status === 0 ? 'PASS' : 'FAIL';
        appendFileSync(
          path.join(dir, this.t, 'results.tsv'),
          `${st}\t${core}\tstand-ready\tпосле Go-тестов стенд ${st === 'PASS' ? 'в рабочем состоянии' : 'не пришёл в рабочее состояние'}\n`
        );
      }
    }
    if (p.specs.length) {
      const env = { RT_REPORT: dir, RT_SPECS: p.specs.join(' ') };
      if (p.grep) env.RT_PW_GREP = p.grep;
      rc = await this.run('sh', [path.join(SCRIPT_DIR, 'pw-suite.sh'), this.t, core], env);
      this.record({ kind: 'pw', pass: p.name, core, dir, rc, version: p.resolved ?? null });
      if (rc === 3) return 3;
    }
    this.judge(p, core, dir);
    return 0;
  }

  // --- остановка на первом несовпадении ---

  setStopped(s) {
    this.stopped = { todo: [], ...s };
    this.say(
      `остановка на первом несовпадении (${s.how}): проход ${s.pass}, ядро ${s.core}: ${s.test} — ожидалось ${s.expected.join('|')}, получено ${s.actual}`
    );
  }

  // Первое несовпадение среди вердиктов, добавленных после прошлой проверки (сверка после
  // этапа). Возвращает true, если сессия остановлена.
  checkStop() {
    if (this.stopped) return true;
    const fresh = this.verdicts.slice(this.checked);
    this.checked = this.verdicts.length;
    if (this.spec.keepGoing || this.aborted) return false;
    const v = fresh.find((x) => !x.ok);
    if (!v) return false;
    this.setStopped({
      pass: v.pass,
      core: v.core,
      test: v.test,
      expected: v.expected,
      actual: v.actual,
      how: 'после этапа',
      live: false
    });
    return true;
  }

  // Первая строка results.tsv этапа, которую ожидание прохода не допускает.
  firstMismatch(p, dir) {
    const file = path.join(dir, this.t, 'results.tsv');
    if (!existsSync(file)) return null;
    for (const line of readFileSync(file, 'utf8').split('\n')) {
      if (!line) continue;
      const [status, , check = ''] = line.split('\t');
      if (/^(pw|go):/.test(check)) {
        if (!allowedStatus(p.expect, check, status)) {
          return { test: check, expected: expectedOf(p.expect, check), actual: status };
        }
      } else if (status === 'FAIL' || status === 'XPASS') {
        return { test: check, expected: ['PASS'], actual: status };
      }
    }
    return null;
  }

  // Сторож Go-этапа: раз в секунду дочитывает go-*.raw в <этап>/<цель>/<ядро>/ (и на уровень
  // ниже) с запомненной позиции и разбирает маркеры теста; готовые go-*.parsed (их .raw уже
  // удалён) сверяет целиком. Несовпадение — this.stopped и TERM группе этапа; если бинарник
  // в этот момент не работает (между пакетами), сигнал уходит, как только появится следующий
  // .raw. Возвращает opts для run.
  goWatch(p, core, dir) {
    const art = path.join(dir, this.t, core);
    const raws = new Map();
    const parsedDone = new Set();
    let found = false;
    let sent = false;
    let child = null;
    const MARKS = [
      ['xpass', /XPASS slug=[a-z0-9-]+/],
      ['quarantine', /QUARANTINE slug=[a-z0-9-]+/]
    ];
    const signal = () => {
      if (sent || !child) return;
      sent = true;
      this.stopped.live = true;
      this.say('этап go останавливается: TERM группе этапа');
      this.signalChild('SIGTERM');
    };
    const check = (name, status, hasRaw) => {
      if (found || this.aborted) return;
      const test = `go:${name}`;
      if (allowedStatus(p.expect, test, status)) return;
      found = true;
      this.setStopped({
        pass: p.name,
        core,
        test,
        expected: expectedOf(p.expect, test),
        actual: status,
        how: 'по ходу этапа go',
        live: false
      });
      if (hasRaw) signal();
    };
    const list = () => {
      const out = [];
      const walk = (d, depth) => {
        let ents;
        try {
          ents = readdirSync(d, { withFileTypes: true });
        } catch {
          return;
        }
        for (const e of ents) {
          const f = path.join(d, e.name);
          if (e.isDirectory()) {
            if (depth === 0) walk(f, 1);
          } else if (/^go-.+\.(raw|parsed)$/.test(e.name)) {
            out.push(f);
          }
        }
      };
      walk(art, 0);
      return out;
    };
    const onFail = (st, name) => {
      // родитель, упавший из-за подтеста, отдельно не сверяется (badchild в go-parse.awk)
      const byChild = [...st.failed].some((f) => f.startsWith(`${name}/`));
      st.failed.add(name);
      if (byChild) return;
      const has = (t, k) => st.marks.get(t)?.has(k) ?? false;
      const parts = name.split('/');
      const ancestors = parts.map((_, i) => parts.slice(0, i + 1).join('/'));
      let status = 'FAIL';
      if (has(name, 'xpass')) status = 'XPASS';
      else if (ancestors.some((a) => has(a, 'quarantine'))) status = 'QFAIL';
      check(name, status, true);
    };
    const rawLine = (st, line) => {
      // вывод -test.v=test2json: служебные строки начинаются с \x16, у подтестов — с отступом
      const l = line.replace(/^[\s\x16]+/, '');
      let m = l.match(/^=== (RUN|CONT|NAME)\s+(\S+)/);
      if (m) {
        st.cur = m[2];
        return;
      }
      m = l.match(/^--- (FAIL|PASS|SKIP): (\S+)/);
      if (m) {
        if (m[1] === 'FAIL') onFail(st, m[2]);
        return;
      }
      if (!st.cur) return;
      for (const [kind, re] of MARKS) {
        if (!re.test(l)) continue;
        if (!st.marks.has(st.cur)) st.marks.set(st.cur, new Set());
        st.marks.get(st.cur).add(kind);
      }
    };
    const readRaw = (f) => {
      let st = raws.get(f);
      if (!st) {
        st = {
          pos: 0,
          dec: new StringDecoder('utf8'),
          rest: '',
          cur: '',
          marks: new Map(),
          failed: new Set()
        };
        raws.set(f, st);
      }
      let fd;
      try {
        fd = openSync(f, 'r');
      } catch {
        return;
      }
      try {
        const size = fstatSync(fd).size;
        while (st.pos < size) {
          const buf = Buffer.alloc(Math.min(size - st.pos, 1 << 20));
          const n = readSync(fd, buf, 0, buf.length, st.pos);
          if (n <= 0) break;
          st.pos += n;
          st.rest += st.dec.write(buf.subarray(0, n));
        }
      } finally {
        closeSync(fd);
      }
      const lines = st.rest.split('\n');
      st.rest = lines.pop();
      for (const line of lines) rawLine(st, line);
    };
    const readParsed = (f) => {
      parsedDone.add(f);
      let text = '';
      try {
        text = readFileSync(f, 'utf8');
      } catch {
        return;
      }
      for (const line of text.split('\n')) {
        const [status, name = ''] = line.split('\t');
        if (name.startsWith('go:')) check(name.slice(3), status, false);
      }
    };
    const poll = () => {
      if (sent || this.aborted) return;
      const files = list();
      const live = files.filter((f) => f.endsWith('.raw'));
      if (found) {
        // несовпадение найдено между пакетами: остановить, как только пойдёт следующий бинарник
        if (live.length) signal();
        return;
      }
      for (const f of live) readRaw(f);
      for (const f of files) {
        if (found) break;
        if (!f.endsWith('.parsed') || parsedDone.has(f)) continue;
        if (existsSync(f.replace(/\.parsed$/, '.raw'))) continue;
        readParsed(f);
      }
      if (found && !sent && live.some((f) => existsSync(f))) signal();
    };
    return {
      drainIf: () => sent,
      onSpawn: (c) => {
        child = c;
        const timer = setInterval(poll, 1000);
        return () => {
          clearInterval(timer);
          child = null;
        };
      }
    };
  }

  // Тестовые бинарники на цели после остановки Go-этапа: ssh этапа оборван, бинарник мог
  // остаться работать — добить, чтобы возврат стенда не шёл одновременно с тестом.
  killRemoteTests() {
    const r = this.remote(
      'n=0; for f in /proc/[0-9]*/cmdline; do p=${f#/proc/}; p=${p%/cmdline}; tr "\\000" " " <"$f" 2>/dev/null | grep -q "[.]test -test[.]" && kill "$p" 2>/dev/null && n=$((n+1)); done; echo "n=$n"'
    );
    const m = r.out.match(/^n=(\d+)$/m);
    if (r.rc === 3) this.network = true;
    if (!m) {
      this.notes.push(
        `после остановки этапа go тестовые процессы на цели не проверены (код ${r.rc})`
      );
    } else if (Number(m[1]) > 0) {
      this.say(`после остановки этапа go добито тестовых процессов на цели: ${m[1]}`);
    }
  }

  // Прерывание вложенного run.sh по спеке (interrupt). Вложенный прогон идёт отдельной группой
  // процессов (detached), как make в терминале. Опрос раз в секунду: появился go-<пакет>.raw в
  // <отчёт>/<цель>/<ядро>/ — go-этап начался; через after секунд сигнал уходит группе; не вышла
  // за deadline секунд — SIGKILL группе. Возвращает opts для run и состояние для вердиктов.
  interruptWatch(i, dir) {
    const state = {
      pkg: '',
      goAt: 0,
      sentAt: 0,
      closedAt: 0,
      killed: false,
      pgid: 0
    };
    const findRaw = () => {
      const tdir = path.join(dir, this.t);
      let cores;
      try {
        cores = readdirSync(tdir, { withFileTypes: true }).filter((d) => d.isDirectory());
      } catch {
        return '';
      }
      for (const c of cores) {
        let names;
        try {
          names = readdirSync(path.join(tdir, c.name));
        } catch {
          continue;
        }
        const raw = names.find((n) => /^go-.+\.raw$/.test(n));
        if (raw) return raw.slice(3, -4);
      }
      return '';
    };
    const opts = {
      detached: true,
      // потомков после выхода run.sh проверяет вердикт «процессы на ПК», группу не добивать
      drain: false,
      onSpawn: (child) => {
        // группа вложенного прогона — только по целому pid > 1; иначе сигнал не шлём вовсе
        const pid = child.pid;
        const okPid = Number.isInteger(pid) && pid > 1;
        if (okPid) state.pgid = pid;
        let kill = null;
        const poll = setInterval(() => {
          if (state.sentAt || !okPid) return;
          if (!state.goAt) {
            const pkg = findRaw();
            if (!pkg) return;
            state.goAt = Date.now();
            state.pkg = pkg;
            return;
          }
          if (this.aborted || Date.now() - state.goAt < i.after * 1000) return;
          state.sentAt = Date.now();
          this.say(`прерывание: SIG${i.signal} группе вложенного прогона`);
          try {
            process.kill(-pid, 'SIG' + i.signal);
          } catch {
            // группа уже вышла
          }
          kill = setTimeout(() => {
            state.killed = true;
            this.say(`вложенный прогон не вышел за ${i.deadline} с: SIGKILL группе`);
            try {
              process.kill(-pid, 'SIGKILL');
            } catch {
              // группа уже вышла
            }
          }, i.deadline * 1000);
        }, 1000);
        return () => {
          clearInterval(poll);
          if (kill) clearTimeout(kill);
        };
      }
    };
    return { opts, state };
  }

  // Проход-прогон набора: вложенный run.sh под блокировкой сессии, отчёт в build/router.
  // Вложенный прогон сам ставит сборку HEAD, снимает снимок и восстанавливает стенд.
  async runSuite(p) {
    const base = `${path.basename(this.dir)}-${String(++this.n).padStart(2, '0')}-${p.name}`;
    const dir = path.join(this.root, 'build', 'router', base);
    const rel = path.relative(this.root, dir);
    // смоук релиза: тег — из спеки или новейший опубликованный (не draft) релиз
    let tag = null;
    if (p.suite === 'smoke') {
      tag = p.release;
      let err = '';
      if (tag === 'latest') {
        try {
          tag = latestReleaseTag();
        } catch (e) {
          err = `gh release list не отработал: ${e.message.split('\n')[0]}`;
        }
      }
      if (!err && !RELEASE_TAG.test(tag)) {
        err = `новейший релиз не определён или тег «${tag}» не похож на vX.Y.Z`;
      }
      if (err) {
        this.say(`прогон набора smoke не начат: ${err}`);
        this.verdicts.push({
          pass: p.name,
          core: '-',
          test: 'release: тег',
          expected: [p.release],
          actual: 'не определён',
          detail: err,
          ok: false,
          report: '-'
        });
        return 0;
      }
    }
    this.say(`прогон набора ${suiteTitle(p, tag)} → ${rel}`);
    const env = {
      RT_REPORT_DIR: dir,
      RT_HOLD_MINUTES: undefined,
      RT_CORES: undefined,
      RT_BASE: undefined,
      // настройки окна смоука и выбора набора вложенный прогон выставляет сам
      RT_SMOKE_WINDOW: undefined,
      RT_SMOKE_IPT_WAIT: undefined,
      RT_PW_CORE_ONLY: undefined,
      RT_GO_CORE_PKGS: undefined,
      RT_GO_CORE_RUN: undefined,
      RT_SPECS_CORE: undefined,
      RT_KERNEL_TOUCHED: undefined,
      RT_SELECT_ALL: undefined,
      RT_FILES: p.files.length ? p.files.join(',') : undefined,
      // вложенная сессия: её спека; у остальных наборов SPEC не наследуется
      SPEC: p.suite === 'session' ? p.nested.path : undefined,
      SUITE: undefined,
      // установка артефакта релиза и публикация статуса — только у smoke с release
      RELEASE: tag ?? undefined
    };
    const args = [path.join(SCRIPT_DIR, 'run.sh')];
    if (p.suite !== 'full') args.push(p.suite);
    const irq = p.interrupt ? this.interruptWatch(p.interrupt, dir) : null;
    const t0 = Date.now();
    const rc = await this.run(
      'sh',
      args,
      env,
      irq ? irq.opts : { leaderOnly: p.suite === 'session' }
    );
    const took = Math.round((Date.now() - t0) / 1000);
    if (irq) irq.state.closedAt = Date.now();
    this.record({
      kind: 'suite',
      pass: p.name,
      label: tag ? `smoke ${tag}` : p.suite,
      dir,
      rc
    });
    if (this.aborted && !existsSync(dir)) return 0;

    const res = path.join(dir, this.t, 'results.tsv');
    const rows = [];
    if (existsSync(res)) {
      for (const line of readFileSync(res, 'utf8').split('\n')) {
        if (!line) continue;
        const [status, core = '-', check = '', detail = ''] = line.split('\t');
        rows.push({ status, core, check, detail });
      }
    }
    // вложенный прогон поставил сборку HEAD и вернул ядро снимка. Признак — только код деплоя 0
    // из deploy.rc: прогон мог не дойти до деплоя (check-known, нет целей, неверный отчёт), и тогда
    // results.tsv нет, а на стенде остаётся сборка прохода product.ref. Нет признака — сборку HEAD
    // вернёт cleanup
    const deployRc = path.join(dir, this.t, 'deploy.rc');
    // (после смоука релиза на стенде артефакт релиза, а не HEAD — признак тоже сбрасывается)
    this.deployed =
      p.suite !== 'smoke' && existsSync(deployRc) && readFileSync(deployRc, 'utf8').trim() === '0'
        ? 'HEAD'
        : null;
    if (p.suite === 'session') this.nestedEnd(p, dir, rc);
    // сбой связи — только проверка reachable и потеря связи при восстановлении; по подстроке
    // «нет связи» в любой строке нельзя: так пишет и смоук, когда не отвечает сама панель
    if (
      rows.some(
        (r) =>
          r.status === 'FAIL' &&
          (r.check === 'reachable' ||
            (r.check === 'restore-verify' && r.detail.startsWith('нет связи,')))
      )
    ) {
      this.network = true;
    }
    if (!this.c0) {
      const snap = path.join(dir, this.t, 'snapshot.log');
      const coreFile = path.join(dir, this.t, 'core');
      let core = '';
      if (existsSync(snap)) {
        const m = readFileSync(snap, 'utf8').match(/^core=(.*)$/m);
        if (m) core = m[1].trim();
      }
      if (!CORES.includes(core) && existsSync(coreFile))
        core = readFileSync(coreFile, 'utf8').trim();
      if (CORES.includes(core)) {
        // восстановление вложенного прогона вернуло ядро снимка; позже снимок берёт активное
        // ядро как есть, поэтому this.cur после первого прогона не трогаем
        this.c0 = core;
        this.cur = core;
        this.say(`исходное ядро стенда: ${core}`);
      } else if (!this.network && !this.aborted) {
        this.notes.push(
          `после прогона набора ${p.name} не найдено исходное ядро: сессия остановлена`
        );
        this.broken = true;
      }
    }

    let md = '';
    const mdFile = path.join(dir, 'summary.md');
    if (existsSync(mdFile)) md = readFileSync(mdFile, 'utf8');
    const num = (re) => {
      const m = md.match(re);
      return m ? Number(m[1]) : null;
    };
    this.suites.push({
      pass: p.name,
      suite: tag ? `smoke ${tag}` : p.suite,
      files: p.files,
      exit: rc,
      // у вложенной сессии строки «Итого» нет — время по замеру прохода
      total: num(/^- Итого: (\d+) с/m) ?? (p.suite === 'session' ? took : null),
      shell: num(/^- Оболочка: (\d+) с/m),
      report: rel
    });

    // вердикты прохода
    const push = (test, expected, actual, detail, ok) =>
      this.verdicts.push({
        pass: p.name,
        core: '-',
        test,
        expected,
        actual,
        detail,
        ok,
        report: rel
      });
    push('exit', [String(p.expect.exit)], String(rc), '', rc === p.expect.exit);
    if (p.expect.maxMinutes !== null) {
      const min = Math.round(took / 6) / 10;
      push(
        `время прохода: ${min} мин из ${p.expect.maxMinutes}`,
        [`до ${p.expect.maxMinutes} мин`],
        `${min} мин`,
        `${took} с`,
        took <= p.expect.maxMinutes * 60
      );
    }
    if (irq) {
      const st = irq.state;
      const sent = st.sentAt > 0;
      push(
        'interrupt: сигнал',
        ['отправлен'],
        sent ? 'отправлен' : 'не отправлен',
        sent
          ? `SIG${p.interrupt.signal} через ${Math.round((st.sentAt - st.goAt) / 1000)} с после старта go-${st.pkg}`
          : 'этап go не начался до выхода run.sh',
        sent
      );
      const secs = sent ? Math.round((st.closedAt - st.sentAt) / 1000) : 0;
      push(
        'interrupt: выход run.sh',
        [`до ${p.interrupt.deadline} с`],
        !sent ? 'не вышел' : st.killed ? 'не вышел' : `${secs} с`,
        !sent
          ? 'сигнал не отправлен'
          : st.killed
            ? `SIGKILL группе через ${p.interrupt.deadline} с`
            : '',
        sent && !st.killed
      );
    }
    if (irq) this.judgeInterrupt(irq.state, rows, push);
    if (irq && irq.state.killed && !this.network) {
      // run.sh убит до конца восстановления: стенд возвращает сессия, иначе следующие проходы
      // откажут на preflight. Не вернулся — сессия останавливается
      const lr = await this.restoreLeftover(p.name);
      push(
        'interrupt: возврат стенда сессией',
        ['PASS'],
        lr.ok ? 'PASS' : 'FAIL',
        lr.had ? `по снимку вложенного прогона: ${lr.detail}` : lr.detail,
        lr.ok
      );
      if (!lr.ok && !this.network) this.broken = true;
    }
    for (const sub of p.expect.summary) {
      const ok = md.includes(sub);
      push(`summary: ${sub}`, ['есть'], ok ? 'есть' : 'нет', md ? '' : 'summary.md не создан', ok);
    }
    const fails = new Set(
      rows
        .filter((r) => ['FAIL', 'KNOWN', 'XPASS'].includes(r.status))
        .map((r) => `${r.status}\t${r.core}\t${r.check}`)
    );
    this.suiteFails.set(p.name, fails);
    if (p.expect.same) {
      const other = this.suiteFails.get(p.expect.same);
      const test = `same_failures_as ${p.expect.same}`;
      if (!other) {
        push(test, ['совпадает'], 'нет', `прогон ${p.expect.same} не выполнялся`, false);
      } else {
        const fmt = (a, b) =>
          [...a]
            .filter((x) => !b.has(x))
            .slice(0, 10)
            .map((x) => x.replace(/\t/g, ' '))
            .join(', ') || '-';
        const onlyHere = fmt(fails, other);
        const onlyThere = fmt(other, fails);
        const same = onlyHere === '-' && onlyThere === '-';
        push(
          test,
          ['совпадает'],
          same ? 'совпадает' : 'расходится',
          same
            ? `${fails.size} строк`
            : `только в ${p.name}: ${onlyHere}; только в ${p.expect.same}: ${onlyThere}`,
          same
        );
      }
    }
    return 0;
  }

  // Стенд после вложенной сессии — по её session.json: она сама возвращает сборку HEAD и своё
  // исходное ядро (оно же активное ядро внешней сессии на момент прохода). Код 3 — сбой связи.
  nestedEnd(p, dir, rc) {
    if (rc === 3) this.network = true;
    let j = null;
    try {
      j = JSON.parse(readFileSync(path.join(dir, 'session.json'), 'utf8'));
    } catch {
      this.notes.push(`вложенная сессия ${p.name} не оставила session.json: стенд вернёт cleanup`);
      return;
    }
    const end = j.stand_end ?? {};
    this.deployed = end.product_head === true ? 'HEAD' : null;
    if (CORES.includes(j.core_initial) && !this.c0) {
      this.c0 = j.core_initial;
      this.say(`исходное ядро стенда: ${this.c0}`);
    }
    this.cur = CORES.includes(end.core) ? end.core : null;
  }

  // Вердикты после прерванного вложенного прогона: этап погас, блокировка свободна, стенд
  // восстановлен, лишних падений нет. В отчёт идут только числа: командные строки процессов
  // (рабочий каталог, регэксп -test.run) не выводятся.
  judgeInterrupt(st, rows, push) {
    // процессы на ПК: группа вложенного прогона и этапы обвязки (держали бы дескриптор блокировки).
    // Проверка повторяется до трёх раз с паузой 1 с: процессы только что вышли, и зомби-сироты
    // ещё не подобраны init. Живой член группы — не зомби (ps -o stat); этап обвязки — процесс,
    // чья командная строка НАЧИНАЕТСЯ с `sh <путь>/scripts/router/<этап>.sh`, поэтому редактор,
    // tail или shellcheck с тем же путём в аргументах не считаются.
    let groupAlive = false;
    let stages = -1;
    for (let attempt = 0; attempt < 3; attempt++) {
      groupAlive = false;
      if (st.pgid > 1) {
        const ps = spawnSync('ps', ['-eo', 'pgid=,stat='], {
          encoding: 'utf8'
        });
        if (!ps.error && ps.status === 0) {
          groupAlive = ps.stdout.split('\n').some((l) => {
            const [g, stat = ''] = l.trim().split(/\s+/);
            return Number(g) === st.pgid && !stat.startsWith('Z');
          });
        } else {
          try {
            process.kill(-st.pgid, 0);
            groupAlive = true;
          } catch (e) {
            groupAlive = e.code !== 'ESRCH';
          }
        }
      }
      const pg = spawnSync(
        'pgrep',
        ['-f', '^(/[^ ]*/)?(ba|da)?sh +[^ ]*scripts/router/(go-suite|pw-suite|smoke)[.]sh'],
        { encoding: 'utf8' }
      );
      stages = -1;
      if (!pg.error && pg.status === 1) stages = 0;
      else if (!pg.error && pg.status === 0) stages = pg.stdout.split('\n').filter(Boolean).length;
      if (!groupAlive && stages === 0) break;
      if (attempt < 2) spawnSync('sleep', ['1']);
    }
    const pcOk = !groupAlive && stages === 0;
    push(
      'interrupt: процессы на ПК',
      ['нет'],
      pcOk ? 'нет' : 'есть',
      `группа: ${groupAlive ? 'жива' : 'пуста'}; этапов обвязки: ${stages < 0 ? 'не проверено' : stages}`,
      pcOk
    );

    // тестовые процессы на цели: ./<пакет>.test -test.v=test2json; шаблон со скобками не находит
    // сам себя в командной строке проверки
    const r = this.remote(
      'n=0; for f in /proc/[0-9]*/cmdline; do tr "\\000" " " <"$f" 2>/dev/null | grep -q "[.]test -test[.]" && n=$((n+1)); done; echo "n=$n"'
    );
    const m = r.out.match(/^n=(\d+)$/m);
    if (r.rc !== 0 || !m) {
      push('interrupt: тестовые процессы на цели', ['нет'], 'нет связи', `код ${r.rc}`, false);
    } else {
      const n = Number(m[1]);
      push(
        'interrupt: тестовые процессы на цели',
        ['нет'],
        n === 0 ? 'нет' : `есть: ${n}`,
        '',
        n === 0
      );
    }

    // восстановление стенда вложенным прогоном
    const restore = ['restore-verify', 'xcp-after-restore', 'core-restored'];
    for (const check of restore) {
      const own = rows.filter((x) => x.check === check);
      const row = own.find((x) => x.status !== 'PASS') ?? own[own.length - 1];
      push(
        check,
        ['PASS'],
        row ? row.status : 'нет',
        row ? row.detail : 'строки нет в results.tsv вложенного прогона',
        !!row && row.status === 'PASS'
      );
    }

    // остальные падения: при коде 130 код выхода о них не сообщает
    for (const row of rows) {
      if (restore.includes(row.check)) continue;
      if (row.status !== 'FAIL' && row.status !== 'XPASS') continue;
      push(row.check, ['PASS'], row.status, `${row.core}: ${row.detail}`, false);
    }
  }

  // Сверка results.tsv этапа с ожиданиями прохода.
  judge(p, core, dir) {
    const rel = path.relative(this.root, dir);
    const file = path.join(dir, this.t, 'results.tsv');
    const got = new Map();
    const extra = [];
    if (existsSync(file)) {
      for (const line of readFileSync(file, 'utf8').split('\n')) {
        if (!line) continue;
        const [status, , check, detail = ''] = line.split('\t');
        if (/^(pw|go):/.test(check)) got.set(check, { status, detail });
        else if (status === 'FAIL' || status === 'XPASS') extra.push({ check, status, detail });
      }
    }
    const push = (test, expected, actual, detail, ok) =>
      this.verdicts.push({
        pass: p.name,
        core,
        test,
        expected,
        actual,
        detail,
        ok,
        report: rel
      });
    const { expect } = p;
    // этап остановлен на первом несовпадении посреди вывода: тест остановки — несовпадение,
    // тесты, до которых этап не дошёл, не сверяются
    const st = this.stopped;
    const stop = st && st.live && st.pass === p.name && st.core === core ? st : null;
    if (stop) {
      push(
        stop.test,
        stop.expected,
        stop.actual,
        `${stop.how}: этап остановлен, следующие тесты не выполнялись`,
        false
      );
    }
    for (const [name, want] of expect.tests) {
      if (stop && name === stop.test) continue;
      const r = got.get(name);
      if (!r) {
        if (stop) continue;
        push(name, want.status, 'нет', 'тест не запускался: имя или --grep', false);
        continue;
      }
      const okStatus = want.status.includes(r.status);
      const okDetail = want.detail === null || r.detail.includes(want.detail);
      const why = okStatus && !okDetail ? `в детали нет «${want.detail}»: ${r.detail}` : r.detail;
      push(name, want.status, r.status, why, okStatus && okDetail);
    }
    for (const [name, r] of got) {
      if (expect.tests.has(name) || (stop && name === stop.test)) continue;
      if (SERVICE.has(name)) {
        push(name, ['PASS'], r.status, r.detail, r.status === 'PASS');
      } else if (expect.def) {
        push(name, expect.def, r.status, r.detail, expect.def.includes(r.status));
      } else if (r.status === 'FAIL' || r.status === 'XPASS') {
        push(name, ['справочно'], r.status, `неожиданное падение: ${r.detail}`, false);
      } else {
        push(name, ['справочно'], r.status, r.detail, true);
      }
    }
    for (const e of extra) push(e.check, ['PASS'], e.status, e.detail, false);
    if (expect.count !== null && !stop) {
      const n = [...got.keys()].filter((k) => !SERVICE.has(k)).length;
      push(`число тестов`, [String(expect.count)], String(n), '', n === expect.count);
    }
  }

  // --- возврат стенда ---

  async cleanup() {
    this.cleaning = true;
    try {
      this.restoreProduct();
    } catch (e) {
      this.notes.push(`файлы продукта не вернулись к HEAD: ${e.message}`);
    }
    if (this.network) {
      this.notes.push('сбой связи: стенд не возвращался автоматически — проверить вручную');
      return;
    }
    // снимок незавершённого прогона (сессию прервали посреди восстановления вложенного run.sh)
    const lr = await this.restoreLeftover('leftover');
    if (this.network) return;
    if (lr.had && !lr.ok)
      this.notes.push(`снимок незавершённого прогона не вернул стенд: ${lr.detail}`);
    // после прохода-прогона deployed === 'HEAD': вложенный run.sh поставил сборку HEAD и сам
    // вернул стенд по снимку, повторный деплой не нужен
    if (this.deployed !== 'HEAD') {
      const rc = await this.deployAndSmoke('HEAD', 'head-restore');
      if (rc === 3) return;
    }
    if (this.c0 && this.cur !== this.c0) await this.switchTo(this.c0);
  }

  standEnd() {
    const end = {
      tree_clean: git('status', '--porcelain', '--untracked-files=no') === ''
    };
    end.product_head = this.deployed === 'HEAD';
    const r = this.remote(
      `x=$(pidof xray | cut -d" " -f1); m=$(pidof mihomo | cut -d" " -f1); c=none; [ -n "$x" ] && c=xray; [ -n "$m" ] && c=mihomo; [ -n "$x" ] && [ -n "$m" ] && c=both; echo "$c"; pidof xcp >/dev/null && echo xcp-up || echo xcp-down; [ -d ${SNAP_BASE} ] && echo snap-left || echo snap-none`
    );
    const [core = '-', xcp = '-', snap = '-'] = r.out.split('\n');
    end.core = r.rc === 0 ? core : '-';
    end.core_initial = this.c0 ?? '-';
    end.core_ok = r.rc === 0 && core === this.c0;
    end.xcp_up = r.rc === 0 && xcp === 'xcp-up';
    end.no_snapshot = r.rc === 0 && snap === 'snap-none';
    end.ok = end.tree_clean && end.product_head && end.core_ok && end.xcp_up && end.no_snapshot;
    return end;
  }

  report(result, end) {
    // ячейка таблицы Markdown: сначала обратный слеш, иначе «\|» из детали закроет ячейку
    const esc = (s) =>
      String(s).replace(/\\/g, '\\\\').replace(/\|/g, '\\|').replace(/\s+/g, ' ').slice(0, 220);
    const L = [];
    L.push('# Сессия на стенде', '');
    L.push(`- Итог: ${result}`);
    L.push(`- HEAD: ${this.head}`);
    L.push(
      `- Цель: ${this.t}; исходное ядро: ${this.c0 ?? '-'}; ядра проходов: ${this.spec.cores.join(', ')}`
    );
    L.push(`- Отчёт: ${path.relative(this.root, this.dir)}`, '');
    const st = this.stopped;
    if (st) {
      L.push('## Остановлено на первом несовпадении', '');
      L.push(
        `- проход ${st.pass}, ядро ${st.core}: ${esc(st.test)} — ожидалось ${st.expected.join('|')}, получено ${st.actual} (${st.how})`
      );
      L.push(`- Не выполнено:${st.todo.length ? '' : ' —'}`);
      for (const t of st.todo) L.push(`  - ${t}`);
      L.push('');
    }
    L.push('## Ожидания', '');
    L.push('| Проход | Ядро | Тест | Ожидалось | Получено | Совпало | Деталь | Отчёт |');
    L.push('|---|---|---|---|---|---|---|---|');
    for (const v of this.verdicts) {
      L.push(
        `| ${v.pass} | ${v.core} | ${esc(v.test)} | ${v.expected.join('\\|')} | ${v.actual} | ${v.ok ? 'да' : '**нет**'} | ${esc(v.detail)} | ${v.report} |`
      );
    }
    if (this.suites.length) {
      L.push('', '## Прогоны наборов', '');
      L.push('| Проход | Набор | Код | Итог, с | Оболочка, с | Отчёт |');
      L.push('|---|---|---|---|---|---|');
      for (const r of this.suites) {
        const set = r.files.length ? `${r.suite} (${esc(r.files.join(', '))})` : r.suite;
        L.push(
          `| ${r.pass} | ${set} | ${r.exit} | ${r.total ?? '-'} | ${r.shell ?? '-'} | ${r.report} |`
        );
      }
    }
    L.push('', '## Этапы', '');
    for (const s of this.steps) {
      L.push(
        `- ${s.kind}${s.pass ? ` ${s.pass}` : ''}${s.core ? ` ${s.core}` : ''}${s.label ? ` ${s.label}` : ''}${s.version ? ` под номером ${s.version}` : ''}: код ${s.rc} (${path.relative(this.root, s.dir)})`
      );
    }
    L.push('', '## Стенд в конце', '');
    if (end) {
      L.push(`- рабочее дерево чистое: ${end.tree_clean ? 'да' : '**нет**'}`);
      L.push(`- на цели сборка HEAD: ${end.product_head ? 'да' : '**нет**'}`);
      L.push(
        `- активное ядро: ${end.core} (исходное ${end.core_initial}) — ${end.core_ok ? 'да' : '**нет**'}`
      );
      L.push(`- xcp запущен: ${end.xcp_up ? 'да' : '**нет**'}`);
      L.push(`- снимка незавершённого прогона нет: ${end.no_snapshot ? 'да' : '**нет**'}`);
    } else {
      L.push('- не проверялся');
    }
    for (const n of this.notes) L.push(`- ${n}`);
    const md = L.join('\n') + '\n';
    writeFileSync(path.join(this.dir, 'summary.md'), md);
    writeFileSync(
      path.join(this.dir, 'session.json'),
      JSON.stringify(
        {
          result,
          head: this.head,
          target: this.t,
          core_initial: this.c0,
          verdicts: this.verdicts,
          // номер сборки каждого прохода go/specs: разрешённый тег вышедшего релиза или null
          passes: this.spec.passes
            .filter((p) => !p.suite)
            .map((p) => ({ name: p.name, version: p.resolved ?? null })),
          suites: this.suites,
          steps: this.steps.map((s) => ({
            ...s,
            dir: path.relative(this.root, s.dir)
          })),
          stand_end: end,
          stopped: this.stopped,
          notes: this.notes
        },
        null,
        2
      ) + '\n'
    );
    process.stdout.write('\n' + md);
  }
}

async function main() {
  const argv = process.argv.slice(2);
  const args = parseArgs(argv);
  const specFile = path.resolve(args.spec);
  const root = git('rev-parse', '--show-toplevel');
  process.chdir(root);
  const spec = loadSpec(specFile, root);
  if (args.mode === 'dry-run') {
    dryRun(spec, path.relative(root, specFile));
    return;
  }
  if (args.mode === 'target') {
    process.stdout.write(spec.target + '\n');
    return;
  }
  if (process.env.RT_LOCK_HELD !== '1' || !process.env.RT_REPORT) {
    die('сессия запускается через make router-test SUITE=session SPEC=<спека>');
  }
  if (git('status', '--porcelain', '--untracked-files=no') !== '') {
    die('рабочее дерево не чистое: проходы меняют файлы продукта');
  }

  const s = new Session(spec, specFile, root);
  s.say(`отчёт ${path.relative(root, s.dir)}`);
  let interrupts = 0;
  const onSignal = () => {
    if (++interrupts > 1) {
      s.signalChild('SIGTERM');
      process.exit(130);
    }
    s.say('прерывание — возвращаю стенд (повторный сигнал — выход без возврата)');
    s.aborted = true;
    s.signalChild('SIGTERM');
  };
  for (const sig of ['SIGINT', 'SIGTERM', 'SIGHUP']) process.on(sig, onSignal);

  // Невыполненное при остановке: оставшиеся ядра текущего прохода и следующие проходы.
  const passTitle = (q) =>
    q.suite
      ? `проход ${q.name} (прогон набора ${q.suite})`
      : `проход ${q.name}: ядра ${spec.cores.join(', ')}`;
  const stopAt = (i, cores = []) => {
    if (cores.length) {
      s.stopped.todo.push(`проход ${spec.passes[i].name}: ядра ${cores.join(', ')}`);
    }
    for (const q of spec.passes.slice(i + 1)) s.stopped.todo.push(passTitle(q));
  };

  try {
    for (let i = 0; i < spec.passes.length; i++) {
      const p = spec.passes[i];
      if (s.aborted || s.network || s.broken || s.stopped) break;
      if (p.suite) {
        // вложенный прогон деплоит рабочее дерево: файлы продукта прежнего прохода — к HEAD
        s.restoreProduct();
        await s.runSuite(p);
        if (s.checkStop()) stopAt(i);
        continue;
      }
      if (p.version && !s.resolveVersion(p)) break;
      s.applyProduct(p);
      const key = productKey(p);
      if (s.deployed !== key) {
        const label = p.version
          ? `${p.name}-${p.resolved}`
          : p.product
            ? `${p.name}-${p.product.ref.slice(0, 8)}`
            : 'head';
        const rc = await s.deployAndSmoke(key, label, p.resolved ?? null);
        if (rc === 3 || s.broken) break;
        if (!s.c0) {
          if (!s.cur) {
            s.notes.push('после деплоя смоук не нашёл активное ядро: сессия остановлена');
            s.broken = true;
            break;
          }
          s.c0 = s.cur;
          s.say(`исходное ядро стенда: ${s.c0}`);
        }
        // красный смоук — несовпадение: проход не начинается
        if (s.checkStop()) {
          stopAt(i, coreOrder(spec.cores, s.cur));
          break;
        }
      }
      const order = coreOrder(spec.cores, s.cur);
      for (let j = 0; j < order.length; j++) {
        const core = order[j];
        if (s.aborted || s.network || s.broken) break;
        if (s.cur !== core) {
          const rc = await s.switchTo(core);
          if (rc !== 0) break;
        }
        const rc = await s.runPass(p, core);
        if (s.checkStop()) {
          stopAt(i, order.slice(j + 1));
          break;
        }
        if (rc === 3) break;
      }
    }
    // несовпадение в последнем этапе без остановки посреди вывода — не остановка: невыполненного нет
    if (s.stopped && !s.stopped.live && !s.stopped.todo.length) s.stopped = null;
  } catch (e) {
    s.notes.push(`сессия упала: ${e.message}`);
    s.broken = true;
  }

  try {
    await s.cleanup();
  } catch (e) {
    s.notes.push(`возврат стенда упал: ${e.message}`);
  }
  const end = s.network ? null : s.standEnd();
  const mismatches = s.verdicts.filter((v) => !v.ok).length;
  let code = 0;
  let result;
  if (s.aborted) {
    code = 130;
    result = 'прервана';
  } else if (s.network) {
    code = 3;
    result = 'сбой связи ПК↔стенд — не красный прогон, нужен todo с разбором (AGENTS.md)';
  } else if (s.broken || s.stopped || mismatches || !end?.ok) {
    code = 1;
    const why = [];
    if (s.stopped) {
      why.push(
        `остановлено на первом несовпадении: проход ${s.stopped.pass}, ядро ${s.stopped.core}, ${s.stopped.test}`
      );
    }
    if (s.broken) why.push('этап сломался');
    if (mismatches) why.push(`расхождений с ожиданиями: ${mismatches}`);
    if (!end?.ok) why.push('стенд не вернулся в исходное состояние');
    result = `красный (${why.join('; ')})`;
  } else {
    result = `зелёный (ожиданий: ${s.verdicts.length})`;
  }
  s.report(result, end);

  process.exit(code);
}

main().catch((e) => die(e.stack || e.message, 1));
