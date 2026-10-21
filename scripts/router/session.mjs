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
//     "passes": [
//       {
//         "name": "before-fix",             [a-z0-9-]
//         "product": {                      необязательно: файлы продукта из другого коммита
//           "ref": "0aae0a7c",              (воспроизведение до исправления); тесты — из HEAD
//           "files": ["frontend/src/Editor.svelte"]
//         },
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
//     "suite": "changed",                   full или changed
//     "files": ["internal/nodes/router_nodes_test.go"],    только у changed: выбор по списку
//                                           файлов (RT_FILES) вместо git; пути без «..»
//     "expect": {
//       "exit": 0,                          код выхода run.sh, по умолчанию 0
//       "summary": ["- Окно смоука: 30 с"], подстроки summary.md вложенного отчёта
//       "same_failures_as": "full-1"        множество строк FAIL, KNOWN и XPASS (статус, ядро,
//     }                                     проверка) совпадает с более ранним проходом-прогоном
//   }
// С go, specs, grep и product проход-прогон не сочетается. Вложенный `sh run.sh [changed]`
// идёт под блокировкой сессии (RT_LOCK_HELD=1) без RT_HOLD_MINUTES, RT_CORES, RT_BASE и
// SPEC, сам ставит сборку HEAD, снимает снимок и восстанавливает стенд. Его отчёт — в
// build/router/<каталог сессии>-<NN>-<проход> (RT_REPORT_DIR; ссылка last остаётся на
// сессии). В summary.md сессии — раздел «Прогоны наборов»: проход, набор, код, итог,
// оболочка, путь отчёта; те же данные — в session.json (suites).
//
// Статус ожидания — PASS, FAIL, KNOWN, XPASS или SKIP; несколько — через «|» (PASS|KNOWN).
// detail — подстрока детали в results.tsv. Без default тест вне tests справочный, но FAIL
// и XPASS у него — расхождение: неожиданное падение не теряется.
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
// Прерывание во время вложенного прогона: сигнал уходит вложенному run.sh, сессия ждёт его
// выхода (стенд по снимку он восстанавливает сам); повторный сигнал — выход без возврата.

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import {
  appendFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync
} from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const SCRIPT = fileURLToPath(import.meta.url);
const SCRIPT_DIR = path.dirname(SCRIPT);
const CORES = ['xray', 'mihomo'];
const STATUSES = ['PASS', 'FAIL', 'KNOWN', 'XPASS', 'SKIP'];
const SAFE_PATH = /^[A-Za-z0-9_.][A-Za-z0-9_./-]*$/;
const SAFE_NAME = /^[a-z0-9][a-z0-9-]*$/;
const SAFE_REF = /^[A-Za-z0-9_.][A-Za-z0-9_./~^-]*$/;
// Служебные строки этапов: всегда расхождение, если их FAIL не ожидался явно.
const SERVICE = new Set(['pw:run', 'pw:setup']);

function die(msg, code = 2) {
  process.stderr.write(`session: ${msg}\n`);
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

// Проход-прогон набора: разбор и проверка полей. earlier — имена прогонов набора выше по спеке.
function loadSuitePass(p, where, earlier) {
  if (p.suite !== 'full' && p.suite !== 'changed') die(`${where}.suite: full или changed`);
  if (p.go || p.specs !== undefined || p.grep !== undefined || p.product) {
    die(`${where}: проход-прогон не сочетается с go/specs/product`);
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
    if (!['exit', 'summary', 'same_failures_as'].includes(k)) {
      die(`${where}.expect.${k}: у прохода-прогона только exit, summary, same_failures_as`);
    }
  }
  if (e.exit !== undefined && (!Number.isInteger(e.exit) || e.exit < 0)) {
    die(`${where}.expect.exit: целое от 0`);
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
    files,
    product: null,
    go: null,
    specs: [],
    grep: '',
    expect: { exit: e.exit ?? 0, summary, same }
  };
}

function loadSpec(file, root) {
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
  if (!Array.isArray(spec.passes) || !spec.passes.length) die('passes: непустой список');
  const names = new Set();
  const suites = [];
  const passes = spec.passes.map((p, i) => {
    const where = `passes[${i}]`;
    if (typeof p.name !== 'string' || !SAFE_NAME.test(p.name)) die(`${where}.name: [a-z0-9-]`);
    if (names.has(p.name)) die(`${where}.name: «${p.name}» повторяется`);
    names.add(p.name);
    if (p.suite !== undefined) {
      const sp = loadSuitePass(p, where, suites);
      suites.push(sp.name);
      return sp;
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
    return { name: p.name, product, go, specs, grep, expect };
  });
  return { target: spec.target, cores, passes };
}

// Ядра прохода змейкой: сначала активное, затем остальные в порядке спеки.
function coreOrder(cores, cur) {
  return cores.includes(cur) ? [cur, ...cores.filter((c) => c !== cur)] : [...cores];
}

function productKey(p) {
  return p.product ? `${p.product.sha}:${p.product.files.join(',')}` : 'HEAD';
}

// План сессии без стенда. Исходное ядро неизвестно до смоука: план считает его первым в
// cores, на стенде с другим активным ядром порядок ядер зеркальный.
function dryRun(spec, specFile) {
  const c0 = spec.cores[0];
  const lines = [
    `Сессия по ${specFile}: цель ${spec.target}, ядра ${spec.cores.join(', ')} (исходное считается ${c0})`
  ];
  let n = 0;
  const step = (s) => lines.push(`${++n}. ${s}`);
  let deployed = null;
  let cur = c0;
  for (const p of spec.passes) {
    if (p.suite) {
      // вложенный прогон ставит сборку HEAD сам и оставляет активным ядро, найденное в начале
      step(
        `прогон набора ${p.suite}${p.files.length ? ` по файлам ${p.files.join(' ')}` : ''}: вложенный run.sh, отчёт build/router/<сессия>-NN-${p.name}`
      );
      deployed = 'HEAD';
      continue;
    }
    const key = productKey(p);
    if (key !== deployed) {
      step(
        `деплой и смоук: ${p.product ? `${p.product.files.join(' ')} из ${p.product.ref}` : 'HEAD'}`
      );
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
  step('проверка стенда: дерево чистое, сборка HEAD, исходное ядро, xcp запущен');
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
  run(cmd, args, env = {}) {
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
      'XCP_WANT_CORE'
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
        stdio: ['ignore', 'pipe', 'pipe']
      });
      this.child = child;
      const out = (d) => {
        process.stdout.write(d);
        appendFileSync(this.log, d);
      };
      child.stdout.on('data', out);
      child.stderr.on('data', out);
      child.on('close', (code, sig) => {
        this.child = null;
        resolve(code ?? 128 + (sig === 'SIGINT' ? 2 : 15));
      });
    });
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
  async deployAndSmoke(key, label) {
    const dir = this.stepDir(`deploy-${label}`);
    this.say(`деплой и смоук: ${label} → ${path.relative(this.root, dir)}`);
    const drc = await this.run('sh', [path.join(SCRIPT_DIR, 'deploy.sh'), '--target', this.t], {
      RT_REPORT: dir
    });
    this.record({ kind: 'deploy', label, dir, rc: drc });
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
    this.record({ kind: 'smoke', label, dir, rc: src });
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

  async runPass(p, core) {
    const dir = this.stepDir(`${p.name}-${core}`);
    this.say(`проход ${p.name} на ${core} → ${path.relative(this.root, dir)}`);
    let rc = 0;
    if (p.go) {
      const env = { RT_REPORT: dir, RT_GO_PKGS: p.go.pkgs };
      if (p.go.run) env.RT_GO_RUN = p.go.run;
      const g = await this.run('sh', [path.join(SCRIPT_DIR, 'go-suite.sh'), this.t, core], env);
      this.record({ kind: 'go', pass: p.name, core, dir, rc: g });
      if (g === 3) return 3;
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
      this.record({ kind: 'pw', pass: p.name, core, dir, rc });
      if (rc === 3) return 3;
    }
    this.judge(p, core, dir);
    return 0;
  }

  // Проход-прогон набора: вложенный run.sh под блокировкой сессии, отчёт в build/router.
  // Вложенный прогон сам ставит сборку HEAD, снимает снимок и восстанавливает стенд.
  async runSuite(p) {
    const base = `${path.basename(this.dir)}-${String(++this.n).padStart(2, '0')}-${p.name}`;
    const dir = path.join(this.root, 'build', 'router', base);
    const rel = path.relative(this.root, dir);
    this.say(
      `прогон набора ${p.suite}${p.files.length ? ` по файлам ${p.files.join(' ')}` : ''} → ${rel}`
    );
    const env = {
      RT_REPORT_DIR: dir,
      RT_HOLD_MINUTES: undefined,
      RT_CORES: undefined,
      RT_BASE: undefined,
      RT_FILES: p.files.length ? p.files.join(',') : undefined,
      SPEC: undefined,
      SUITE: undefined,
      RELEASE: undefined
    };
    const args = [path.join(SCRIPT_DIR, 'run.sh')];
    if (p.suite === 'changed') args.push('changed');
    const rc = await this.run('sh', args, env);
    this.record({ kind: 'suite', pass: p.name, label: p.suite, dir, rc });
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
    this.deployed =
      existsSync(deployRc) && readFileSync(deployRc, 'utf8').trim() === '0' ? 'HEAD' : null;
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
      suite: p.suite,
      files: p.files,
      exit: rc,
      total: num(/^- Итого: (\d+) с/m),
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
    for (const [name, want] of expect.tests) {
      const r = got.get(name);
      if (!r) {
        push(name, want.status, 'нет', 'тест не запускался: имя или --grep', false);
        continue;
      }
      const okStatus = want.status.includes(r.status);
      const okDetail = want.detail === null || r.detail.includes(want.detail);
      const why = okStatus && !okDetail ? `в детали нет «${want.detail}»: ${r.detail}` : r.detail;
      push(name, want.status, r.status, why, okStatus && okDetail);
    }
    for (const [name, r] of got) {
      if (expect.tests.has(name)) continue;
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
    if (expect.count !== null) {
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
      'x=$(pidof xray | cut -d" " -f1); m=$(pidof mihomo | cut -d" " -f1); c=none; [ -n "$x" ] && c=xray; [ -n "$m" ] && c=mihomo; [ -n "$x" ] && [ -n "$m" ] && c=both; echo "$c"; pidof xcp >/dev/null && echo xcp-up || echo xcp-down'
    );
    const [core = '-', xcp = '-'] = r.out.split('\n');
    end.core = r.rc === 0 ? core : '-';
    end.core_initial = this.c0 ?? '-';
    end.core_ok = r.rc === 0 && core === this.c0;
    end.xcp_up = r.rc === 0 && xcp === 'xcp-up';
    end.ok = end.tree_clean && end.product_head && end.core_ok && end.xcp_up;
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
        `- ${s.kind}${s.pass ? ` ${s.pass}` : ''}${s.core ? ` ${s.core}` : ''}${s.label ? ` ${s.label}` : ''}: код ${s.rc} (${path.relative(this.root, s.dir)})`
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
          suites: this.suites,
          steps: this.steps.map((s) => ({
            ...s,
            dir: path.relative(this.root, s.dir)
          })),
          stand_end: end,
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
    if (++interrupts > 1) process.exit(130);
    s.say('прерывание — возвращаю стенд (повторный сигнал — выход без возврата)');
    s.aborted = true;
    if (s.child) s.child.kill('SIGTERM');
  };
  for (const sig of ['SIGINT', 'SIGTERM', 'SIGHUP']) process.on(sig, onSignal);

  try {
    for (const p of spec.passes) {
      if (s.aborted || s.network || s.broken) break;
      if (p.suite) {
        // вложенный прогон деплоит рабочее дерево: файлы продукта прежнего прохода — к HEAD
        s.restoreProduct();
        await s.runSuite(p);
        continue;
      }
      s.applyProduct(p);
      const key = productKey(p);
      if (s.deployed !== key) {
        const rc = await s.deployAndSmoke(
          key,
          p.product ? `${p.name}-${p.product.ref.slice(0, 8)}` : 'head'
        );
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
      }
      for (const core of coreOrder(spec.cores, s.cur)) {
        if (s.aborted || s.network || s.broken) break;
        if (s.cur !== core) {
          const rc = await s.switchTo(core);
          if (rc !== 0) break;
        }
        const rc = await s.runPass(p, core);
        if (rc === 3) break;
      }
    }
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
  } else if (s.broken || mismatches || !end?.ok) {
    code = 1;
    const why = [];
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
