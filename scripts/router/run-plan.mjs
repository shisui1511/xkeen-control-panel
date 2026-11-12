#!/usr/bin/env node
// scripts/router/run-plan.mjs — состав прогона run.sh одним планом (D-12, RTR-14).
//
// Единственный источник решений о составе этапов наборов changed, harness, full и delta: блок
// harness (его тесты, отдельные этапы в changed, самопроверка сессии), вторая ветвь гибрида
// changed, окно смоука по умолчанию. Его исполняет run.sh (вывод --sh) и читает повтор выбора
// select-replay.mjs (runPlan, --json), чтобы оценка считала те же этапы, что гейт.
//
// Правила (перенесены из run.sh без изменения смысла):
//   changed  hExtra=1 — к выбору добавлены этапы набора harness (go-harness, pw-harness-read,
//            pw-harness-state в каждой ветви): RT_HARNESS=1, блок harness прочитан, RT_SELECT_ALL=0.
//            Вторая ветвь b2 — другое ядро, чем b1 (--b1: ядро из SHA HEAD, считает rt_sha_core в
//            run.sh), если выбор что-то затронул; b2Run=1 при непустых RT_GO_CORE_PKGS или
//            RT_SPECS_CORE либо при hExtra. Окно смоука по умолчанию 120 с при RT_KERNEL_TOUCHED=1,
//            иначе 30 с. Самопроверка сессии — при RT_HARNESS=1 (в том числе при полном выборе).
//   harness  обе ветви, все тесты блока основными этапами (go, pw-read, pw), окно 30 с,
//            самопроверка сессии.
//   full     полная матрица: обе ветви, все тесты, окно 120 с, самопроверки нет. Исключение —
//            тесты с меткой «одно ядро» (факт oneCore core-marks.cjs, D-01, D-03): они идут только
//            на ядре из SHA HEAD (--b1, обязателен набору full), как неразмеченные в гейте; ветвь
//            другого ядра исключает их Go-имена (go.skip → RT_GO_SKIP → -test.skip) и спеки тегом
//            (pw.invert '@one-core' → --grep-invert). Самопропуск t.Skip не используется: он дал бы
//            строки SKIP и ложное «нужна метка ядра». Поле плана oneCore: { core, go, pw } — ядро,
//            Go-имена по алфавиту и число спек-тестов с меткой.
//   delta    hExtra=0: тесты блока входят в состав добора через delta-plan.mjs --extra-* (отдельные
//            этапы harness прогнали бы блок дважды); b2 пусто и b2Run=0 — ветви добора DELTA_B1 и
//            DELTA_B2 по delta.sel.d; окно 120 с; самопроверка и строки блока harness — только при
//            RT_HARNESS=1 выбора по диффу (--select), иначе самопроверки нет и строки блока пусты.
//            Ветвей в плане нет (null): состав добора составляет delta-plan.mjs.
// Наборы rerun, smoke, reboot, bisect и session модулю не передаются (код 2): их состав — rerun-plan.mjs,
// bisect.mjs и session.mjs. Явный RT_SMOKE_WINDOW, отладка RT_CORES и сбой выбора (RT_SEL_BAD) —
// решения run.sh.
//
// API: harnessBlock(root) — блок harness areas.json (loadAreas core-marks.cjs) с проверкой, как
// прежде в run.sh: непустые go, specs и session, значения по шаблонам безопасных символов; ошибка —
// исключение. runPlan({ suite, vars, b1, root }) — план: { suite, hExtra, b2, b2Run, smokeDefault,
// selfcheck, harness, branches, harnessStages, selfcheckStage, oneCore }; oneCore — только у full,
// у остальных наборов null (в changed «одно ядро» и так идёт в одну ветвь как неразмеченный, D-03;
// harness и delta читают coreMarked без изменений); vars — 13 значений select.env
// (ключи RT_*), обязательны для changed и delta. branches — ветви с этапами по порядку run.sh
// (switch, smoke, observe, pw-read, pw-harness-read, go, settle, pw, go-harness, settle-harness,
// pw-harness-state): Go по именам (seq), пакетами (pkgs, run) или все ({ all, skip? }), спеки read и
// state (вид — строка `// e2e-kind: read` в первых пяти строках спека), отбор @core второй ветви
// (coreOnly), отсев invert; у delta — null.
//
// CLI (из корня рабочей копии):
//   node scripts/router/run-plan.mjs --sh --suite <changed|harness|full|delta> [--select <select.env>]
//     [--b1 <xray|mihomo>]
//   node scripts/router/run-plan.mjs --json --suite … — план целиком.
// --sh печатает тринадцать строк для eval в фиксированном порядке, значения в одинарных кавычках и
// только из безопасных символов (run.sh сверяет число строк, порядок имён и форму каждой, T-148.1.1-12,
// T-148.1.2-16):
//   RT_H_EXTRA='0|1'  RT_B2='xray|mihomo|'  RT_B2_RUN='0|1'  RT_SMOKE_DEFAULT='30|120'
//   RT_SELFCHECK='0|1'  RT_H_GO_SEQ  RT_H_GO_PKGS  RT_H_SPECS  RT_H_GREP  RT_H_SESSION
//   RT_OC_CORE='xray|mihomo|'  RT_OC_GO_SKIP='^(A|B)$|'  RT_OC_PW_INVERT='@one-core|'
// Строки RT_OC_* непусты только у full: ядро «одно ядро» (ядро из SHA HEAD), регэксп -test.skip
// Go-имён и отсев спеков для ветви другого ядра.
// --select — файл из 13 строк вывода select-changed.mjs (select.env отчёта); проверяется так же, как
// в run.sh. --b1 обязателен наборам changed и full. Код 0 — план напечатан; 1 — блок harness не прочитан или
// не прошёл проверку (причина в stderr); 2 — неверный вызов или ввод (причина в stderr).
// Без зависимостей: только встроенные модули Node и core-marks.cjs.

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import coreMarks from './core-marks.cjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const SUITES = ['changed', 'harness', 'full', 'delta'];
const CORES = ['xray', 'mihomo'];

// Шаблоны блока harness — те же, что проверял run.sh перед eval.
const H_GO_ENTRY = /^[A-Za-z0-9_./-]+:TestRouter[A-Za-z0-9_]+$/;
const H_SPEC = /^[A-Za-z0-9_./-]+$/;
const H_GREP = /^[A-Za-z0-9_|()?!^*.: -]*$/;
const H_SESSION = /^scripts\/router\/[A-Za-z0-9_./-]+$/;

// select.env: 13 строк в этом порядке, шаблоны значений — как в run.sh (T-148.1-23).
const Q = "'";
const SELECT_KEYS = [
  ['RT_SELECT_ALL', /^[01]$/, false],
  ['RT_GO_PKGS', /^[A-Za-z0-9_./ -]*$/, true],
  ['RT_SPECS', /^[A-Za-z0-9_./ -]*$/, true],
  ['RT_PW_GREP', /^[A-Za-z0-9_|()?!^*.: -]*$/, true],
  ['RT_GO_CORE_PKGS', /^[A-Za-z0-9_./ -]*$/, true],
  ['RT_GO_CORE_RUN', /^[A-Za-z0-9_|^$()]*$/, true],
  ['RT_SPECS_CORE', /^[A-Za-z0-9_./ -]*$/, true],
  ['RT_KERNEL_TOUCHED', /^[01]$/, false],
  ['RT_GO_SEQ', /^[A-Za-z0-9_./: -]*$/, true],
  ['RT_SPECS_FIRST', /^[A-Za-z0-9_./ -]*$/, true],
  ['RT_PW_INVERT', /^[A-Za-z0-9_@|()?!^*.: -]*$/, true],
  ['RT_HARNESS', /^[01]$/, false],
  ['RT_AREAS', /^[a-z0-9 -]*$/, true],
];

// Строки --sh: имя и шаблон значения (без кавычек) — их же сверяет run.sh.
const SH_KEYS = [
  ['RT_H_EXTRA', /^[01]$/],
  ['RT_B2', /^(xray|mihomo|)$/],
  ['RT_B2_RUN', /^[01]$/],
  ['RT_SMOKE_DEFAULT', /^(30|120)$/],
  ['RT_SELFCHECK', /^[01]$/],
  ['RT_H_GO_SEQ', /^[A-Za-z0-9_./: -]*$/],
  ['RT_H_GO_PKGS', /^[A-Za-z0-9_./ -]*$/],
  ['RT_H_SPECS', /^[A-Za-z0-9_./ -]*$/],
  ['RT_H_GREP', /^[A-Za-z0-9_|()?!^*.: -]*$/],
  ['RT_H_SESSION', /^(scripts\/router\/[A-Za-z0-9_./-]+)?$/],
  ['RT_OC_CORE', /^(xray|mihomo|)$/],
  ['RT_OC_GO_SKIP', /^(\^\([A-Za-z0-9_|]+\)\$)?$/],
  ['RT_OC_PW_INVERT', /^(@one-core)?$/],
];

// Имя Go-теста «одно ядро» в регэкспе -test.skip: только [A-Za-z0-9_] (T-148.1.2-16).
const GO_TEST_NAME = /^TestRouter[A-Za-z0-9_]+$/;
const ONE_CORE_TAG = '@one-core';

class InputError extends Error {}

const words = (s) => (s ? String(s).split(/\s+/).filter(Boolean) : []);
const other = (core) => (core === 'xray' ? 'mihomo' : 'xray');

/** Блок harness areas.json с проверкой значений; ошибка чтения или формы — исключение. */
export function harnessBlock(root = ROOT) {
  const h = coreMarks.loadAreas(root).harness;
  if (!h || !Array.isArray(h.go) || !h.go.length || !Array.isArray(h.specs) || !h.specs.length || !h.session) {
    throw new Error('в areas.json нет блока harness с go, specs и session');
  }
  for (const e of h.go) {
    if (typeof e !== 'string' || !H_GO_ENTRY.test(e)) throw new Error(`блок harness: недопустимая запись go: ${String(e).slice(0, 80)}`);
  }
  for (const s of h.specs) {
    if (typeof s !== 'string' || !H_SPEC.test(s)) throw new Error(`блок harness: недопустимый спек: ${String(s).slice(0, 80)}`);
  }
  const grep = h.grep === undefined || h.grep === null ? '' : h.grep;
  if (typeof grep !== 'string' || !H_GREP.test(grep)) throw new Error('блок harness: недопустимое значение grep');
  if (typeof h.session !== 'string' || !H_SESSION.test(h.session)) throw new Error('блок harness: недопустимый путь session');
  const pkgs = [...new Set(h.go.map((x) => './' + x.split(':')[0]))];
  return { go: [...h.go], pkgs, specs: [...h.specs], grep, session: h.session };
}

/** Значения select.env (13 строк вывода select-changed.mjs) с проверкой формы; ошибка — InputError. */
export function parseSelectEnv(text) {
  const lines = String(text).split('\n');
  if (lines.length > 0 && lines[lines.length - 1] === '') lines.pop();
  if (lines.length !== SELECT_KEYS.length) throw new InputError(`select.env: строк ${lines.length}, ожидалось ${SELECT_KEYS.length}`);
  const vars = {};
  SELECT_KEYS.forEach(([key, re, quoted], i) => {
    const line = lines[i];
    if (!line.startsWith(key + '=')) throw new InputError(`select.env: строка ${i + 1} — ожидалось ${key}=`);
    let v = line.slice(key.length + 1);
    if (quoted) {
      if (v.length < 2 || !v.startsWith(Q) || !v.endsWith(Q)) throw new InputError(`select.env: ${key} без одинарных кавычек`);
      v = v.slice(1, -1);
    }
    if (!re.test(v)) throw new InputError(`select.env: ${key} не прошло проверку`);
    vars[key] = v;
  });
  return vars;
}

// Вид спека: строка `// e2e-kind: read` в первых пяти строках — read, иначе state (изменяющий).
function specKind(root, spec) {
  try {
    const head = fs.readFileSync(path.join(root, 'frontend', spec), 'utf8').split('\n').slice(0, 5);
    return head.some((l) => /^\s*\/\/\s*e2e-kind:\s*read\s*$/.test(l)) ? 'read' : 'state';
  } catch {
    return 'state';
  }
}
function splitSpecs(root, specs) {
  const read = [];
  const state = [];
  for (const s of specs) (specKind(root, s) === 'read' ? read : state).push(s);
  return { read, state };
}

/**
 * Тесты с меткой «одно ядро» дерева (testFacts): Go-имена (уникальные, по алфавиту) и число
 * спек-тестов. Имя не по шаблону — исключение: в регэксп -test.skip идут только проверенные имена.
 */
function oneCoreFacts(root) {
  const go = new Set();
  let pw = 0;
  for (const f of coreMarks.testFacts(root)) {
    if (!f.oneCore) continue;
    if (f.kind === 'go') {
      if (typeof f.test !== 'string' || !GO_TEST_NAME.test(f.test)) throw new Error(`«одно ядро»: недопустимое имя Go-теста: ${String(f.test).slice(0, 80)}`);
      go.add(f.test);
    } else if (f.kind === 'pw') {
      pw++;
    }
  }
  return { go: [...go].sort(), pw };
}

// Этапы ветви по порядку run.sh (full_target). go: { seq } | { pkgs, run } | { all: true, skip? } | null;
// pw: { specs, grep, invert, first, coreOnly } | { all: true, invert? } | null; harness — блок или null.
function branchStages(root, { go, pw, harness, window }) {
  const st = [{ stage: 'switch' }, { stage: 'smoke' }, { stage: 'observe', seconds: window }];
  const pwSplit = pw && !pw.all ? splitSpecs(root, pw.specs) : null;
  if (pw) st.push({ stage: 'pw-read', ...(pw.all ? { ...pw } : { ...pw, specs: pwSplit.read }) });
  const hSplit = harness ? splitSpecs(root, harness.specs) : null;
  if (harness) st.push({ stage: 'pw-harness-read', specs: hSplit.read, grep: harness.grep });
  if (go) st.push({ stage: 'go', ...go }, { stage: 'settle' });
  if (pw) st.push({ stage: 'pw', ...(pw.all ? { ...pw } : { ...pw, specs: pwSplit.state }) });
  if (harness) {
    st.push({ stage: 'go-harness', seq: harness.go }, { stage: 'settle-harness' });
    st.push({ stage: 'pw-harness-state', specs: hSplit.state, grep: harness.grep });
  }
  return st;
}

/** План состава прогона набора; неверный ввод — InputError, блок harness не прочитан — Error. */
export function runPlan({ suite, vars = null, b1 = '', root = ROOT } = {}) {
  if (!SUITES.includes(suite)) throw new InputError(`набор ${suite || '-'} модулю не передаётся (только ${SUITES.join(', ')})`);
  if ((suite === 'changed' || suite === 'delta') && !vars) throw new InputError(`набору ${suite} нужен --select <select.env>`);
  if (vars) {
    for (const [key, re] of SELECT_KEYS) {
      if (typeof vars[key] !== 'string' || !re.test(vars[key])) throw new InputError(`значение ${key} не прошло проверку`);
    }
  }
  if (b1 && !CORES.includes(b1)) throw new InputError('--b1: xray или mihomo');
  if (suite === 'changed' && !b1) throw new InputError('набору changed нужен --b1 <xray|mihomo> (ядро из SHA HEAD)');
  if (suite === 'full' && !b1) throw new InputError('набору full нужен --b1 <xray|mihomo> (ядро из SHA HEAD)');

  const plan = {
    suite,
    hExtra: 0,
    b2: '',
    b2Run: 0,
    smokeDefault: 120,
    selfcheck: 0,
    harness: null,
    branches: null,
    harnessStages: [],
    selfcheckStage: null,
    oneCore: null,
  };

  if (suite === 'harness') {
    const h = harnessBlock(root);
    plan.harness = h;
    plan.smokeDefault = 30;
    plan.selfcheck = 1;
    // все тесты блока — основными этапами в каждой ветви, гибрида нет
    plan.branches = CORES.map((core) => ({
      core,
      role: 'matrix',
      stages: branchStages(root, { go: { seq: h.go }, pw: { specs: h.specs, grep: h.grep, invert: '', first: '', coreOnly: false }, harness: null, window: 30 }),
    }));
  } else if (suite === 'full') {
    plan.smokeDefault = 120;
    // «одно ядро» (D-01, D-03): ветвь b1 — все тесты, ветвь другого ядра — без тестов с меткой
    const oc = oneCoreFacts(root);
    plan.oneCore = { core: b1, go: oc.go, pw: oc.pw };
    plan.branches = CORES.map((core) => {
      const own = core === b1;
      const go = own ? { all: true } : { all: true, skip: oc.go };
      const pw = own ? { all: true } : { all: true, invert: oc.pw > 0 ? ONE_CORE_TAG : '' };
      return { core, role: 'matrix', stages: branchStages(root, { go, pw, harness: null, window: 120 }) };
    });
  } else if (suite === 'delta') {
    plan.smokeDefault = 120;
    if (vars.RT_HARNESS === '1') {
      plan.harness = harnessBlock(root);
      plan.selfcheck = 1;
    }
  } else {
    // changed
    const all = vars.RT_SELECT_ALL === '1';
    if (vars.RT_HARNESS === '1') {
      plan.harness = harnessBlock(root);
      plan.selfcheck = 1;
      if (!all) plan.hExtra = 1;
    }
    plan.smokeDefault = vars.RT_KERNEL_TOUCHED === '1' ? 120 : 30;
    const any = all || words(vars.RT_GO_PKGS).length > 0 || words(vars.RT_SPECS).length > 0 || plan.hExtra === 1;
    if (any) {
      plan.b2 = other(b1);
      if (words(vars.RT_GO_CORE_PKGS).length > 0 || words(vars.RT_SPECS_CORE).length > 0 || plan.hExtra === 1) plan.b2Run = 1;
      const h = plan.hExtra === 1 ? plan.harness : null;
      const seq = words(vars.RT_GO_SEQ);
      let go1 = null;
      if (all) go1 = { all: true };
      else if (seq.length > 0) go1 = { seq };
      else if (words(vars.RT_GO_PKGS).length > 0) go1 = { pkgs: words(vars.RT_GO_PKGS), run: '' };
      const pw1 = all
        ? { all: true }
        : words(vars.RT_SPECS).length > 0
          ? { specs: words(vars.RT_SPECS), grep: vars.RT_PW_GREP, invert: vars.RT_PW_INVERT, first: vars.RT_SPECS_FIRST, coreOnly: false }
          : null;
      plan.branches = [{ core: b1, role: 'b1', stages: branchStages(root, { go: go1, pw: pw1, harness: h, window: plan.smokeDefault }) }];
      if (plan.b2Run === 1) {
        const go2 = words(vars.RT_GO_CORE_PKGS).length > 0 ? { pkgs: words(vars.RT_GO_CORE_PKGS), run: vars.RT_GO_CORE_RUN } : null;
        const pw2 =
          words(vars.RT_SPECS_CORE).length > 0
            ? { specs: words(vars.RT_SPECS_CORE), grep: all ? '' : vars.RT_PW_GREP, invert: all ? '' : vars.RT_PW_INVERT, first: '', coreOnly: true }
            : null;
        plan.branches.push({ core: plan.b2, role: 'b2', stages: branchStages(root, { go: go2, pw: pw2, harness: h, window: plan.smokeDefault }) });
      }
    } else {
      plan.branches = [];
    }
  }
  if (plan.branches) {
    plan.harnessStages = plan.branches.flatMap((b) => b.stages.filter((s) => /harness/.test(s.stage)).map((s) => ({ core: b.core, ...s })));
  }
  if (plan.selfcheck === 1) plan.selfcheckStage = { stage: 'harness-selfcheck', session: plan.harness.session };
  return plan;
}

/** Тринадцать строк для eval в run.sh. */
export function shLines(plan) {
  const h = plan.harness;
  const oc = plan.oneCore;
  const vals = {
    RT_H_EXTRA: String(plan.hExtra),
    RT_B2: plan.b2,
    RT_B2_RUN: String(plan.b2Run),
    RT_SMOKE_DEFAULT: String(plan.smokeDefault),
    RT_SELFCHECK: String(plan.selfcheck),
    RT_H_GO_SEQ: h ? h.go.join(' ') : '',
    RT_H_GO_PKGS: h ? h.pkgs.join(' ') : '',
    RT_H_SPECS: h ? h.specs.join(' ') : '',
    RT_H_GREP: h ? h.grep : '',
    RT_H_SESSION: h ? h.session : '',
    RT_OC_CORE: oc ? oc.core : '',
    RT_OC_GO_SKIP: oc && oc.go.length > 0 ? `^(${oc.go.join('|')})$` : '',
    RT_OC_PW_INVERT: oc && oc.pw > 0 ? ONE_CORE_TAG : '',
  };
  return SH_KEYS.map(([key, re]) => {
    if (!re.test(vals[key])) throw new Error(`значение ${key} не прошло проверку`);
    return `${key}=${Q}${vals[key]}${Q}`;
  });
}

function cli(argv) {
  let mode = '';
  let suite = '';
  let select = '';
  let b1 = '';
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--sh' || a === '--json') {
      if (mode) throw new InputError('--sh и --json не сочетаются');
      mode = a.slice(2);
    } else if (a === '--suite' || a === '--select' || a === '--b1') {
      const v = argv[++i];
      if (v === undefined) throw new InputError(`${a}: нет значения`);
      if (a === '--suite') suite = v;
      else if (a === '--select') select = v;
      else b1 = v;
    } else {
      throw new InputError(`неизвестный аргумент ${String(a).slice(0, 40)}`);
    }
  }
  if (!mode) throw new InputError('нужен --sh или --json');
  let vars = null;
  if (select) {
    let text;
    try {
      text = fs.readFileSync(select, 'utf8');
    } catch {
      throw new InputError(`--select: файл не прочитан: ${select}`);
    }
    vars = parseSelectEnv(text);
  }
  const plan = runPlan({ suite, vars, b1 });
  if (mode === 'json') process.stdout.write(JSON.stringify(plan, null, 2) + '\n');
  else process.stdout.write(shLines(plan).join('\n') + '\n');
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    cli(process.argv.slice(2));
  } catch (e) {
    process.stderr.write(`run-plan.mjs: ${e && e.message ? e.message : String(e)}\n`);
    process.stderr.write('usage: run-plan.mjs --sh|--json --suite <changed|harness|full|delta> [--select <select.env>] [--b1 <xray|mihomo>]\n');
    process.exit(e instanceof InputError ? 2 : 1);
  }
}
