#!/usr/bin/env node
/*
 * check-router-areas.js — области роутерных тестов router-area (RTR-03, D-09…D-11).
 *
 * Выбор changed берёт тесты по областям: правка файла продукта выбирает тесты его области
 * (и канарейки общих областей). Неверная карта молча сужала бы гейт, поэтому страж проверяет
 * карту scripts/router/areas.json и разметку исходников (без запуска, по git ls-files) через
 * общий сканер scripts/router/core-marks.cjs.
 *
 * Правила:
 *   1. У каждого роутерного Go-файла (первая строка `//go:build router`) и каждого спека
 *      frontend/tests/router/*.spec.ts ровно одна строка `// router-area: <область>` в первых
 *      пяти строках; другая форма, повтор или строка ниже пятой — нарушение.
 *   2. Область строки есть в карте; схема карты верна (имена [a-z][a-z0-9-]*, глобы, канарейки,
 *      необязательный блок harness).
 *   3. Каждый глоб карты находит хотя бы один отслеживаемый файл.
 *   4. Каждый файл продукта принадлежит хотя бы одной области: файлы internal/ и cmd/, кроме
 *      *_test.go и internal/routertest/ (testdata входит), embed.go, scripts/setup.sh,
 *      frontend/src/lib/sourcesContract.json и routingContract.json.
 *   5. Канарейка области (D-11): Go-тест есть в файле этой области (у contract-* — в её поле
 *      tests), не в карантине, без
 *      CoreSpecific и метки known с ядром, не в ярусе full; спек — в своей области, ни один его
 *      тест не несёт тег ядра, карантин или ярус full. null допустим только с непустым reason.
 *      Длительность — медиана по build/router/history.tsv основной копии за 60 дней на каждом
 *      ядре короче 30 с (спек — сумма медиан его тестов); истории нет (CI) — проверка
 *      пропускается с пометкой.
 *   6. Поле tests у областей contract-*: каждая запись существует (тест или спек), и каждый
 *      тест или спек, читающий контракт или testdata (contractReaders), в списке есть.
 *   7. Подобласти (areas.<родитель>.subareas, RTR-14): схема — в validateAreas core-marks.cjs
 *      (безопасные глобы, записи tests, повторы); каждый глоб подобласти находит отслеживаемый
 *      файл и не выходит за пути родителя; каждая запись tests существует.
 *   8. Полнота подобластей: у области с subareas каждый её файл продукта найден глобом хотя бы
 *      одной подобласти, и каждый тест с её строкой router-area (Go-тест или спек) назван в
 *      tests хотя бы одной подобласти — этой или другой области, если сценарий проверяет
 *      её файлы. Канарейки остаются у родителя и проверяются правилом 5.
 * Самопроверка разбора на образцах идёт при каждом запуске: расхождение — код 1.
 *
 * Флаг --map-only: только карта, покрытие продукта, поле tests и подобласти (правила 2-4, 6-8) — без разметки
 * файлов тестов и канареек.
 *
 * Usage: node scripts/check-router-areas.js [--map-only]
 */
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const {
  AREAS_FILE,
  loadAreas,
  validateAreas,
  subareasOf,
  matchGlob,
  areaOfText,
  goTestFacts,
  specTestFacts,
  routerGoFiles,
  testFacts,
  contractReaders,
  titleMatcher,
  loadHistory: loadHistoryOf,
  median
} = require('./router/core-marks.cjs');

const root = path.resolve(__dirname, '..');
const args = process.argv.slice(2);
const MAP_ONLY = args.includes('--map-only');
for (const a of args) {
  if (a !== '--map-only') {
    console.error(`check-router-areas: неизвестный аргумент ${JSON.stringify(a)}; допустим только --map-only`);
    process.exit(2);
  }
}

const CANARY_MAX_SECONDS = 30;
const HISTORY_DAYS = 60;
const problems = [];
const violations = [];
const notes = [];
const loadHistory = () => loadHistoryOf(root, HISTORY_DAYS);

// ── Самопроверка разбора: если он сломан, страж молчал бы ──
const GO_HEAD = '//go:build router\n\n';
const areaSamples = [
  // [текст, ожидаемая область, ожидаемое число нарушений]
  [GO_HEAD + '// router-area: dns\n\npackage x\n', 'dns', 0],
  [GO_HEAD + 'package x\n', null, 0],
  [GO_HEAD + '// router-area: dns\n// router-area: kernel\n\npackage x\n', null, 1],
  [GO_HEAD + '// router-area: dns, kernel\n\npackage x\n', null, 1],
  ['// e2e-pages: logs\n// e2e-kind: read\n// router-area: access-log\nimport x from "y";\n', 'access-log', 0],
  ['// e2e-pages: *\nimport a from "a";\nimport b from "b";\nimport c from "c";\nimport d from "d";\n// router-area: ui-shell\n', null, 1],
  ['//router-area: dns\n', null, 1]
];
for (const [text, wantArea, wantIssues] of areaSamples) {
  const got = areaOfText(text);
  if (got.area !== wantArea || got.issues.length !== wantIssues) {
    problems.push(
      `самопроверка строки области: образец ${JSON.stringify(text)} дал ${got.area} (${got.issues.length} нарушений); ожидалось ${wantArea} (${wantIssues})`
    );
  }
}
const globSamples = [
  ['internal/services/kernel*.go', 'internal/services/kernel_apply.go', true],
  ['internal/services/kernel*.go', 'internal/services/x/kernel.go', false],
  ['internal/**/testdata/**', 'internal/nodes/testdata/links/a.txt', true],
  ['internal/**/testdata/**', 'internal/nodes/x.go', false],
  ['cmd/xcp/**', 'cmd/xcp/main.go', true],
  ['internal/handlers/*.go', 'internal/handlers/x/y.go', false],
  ['embed.go', 'embed.go', true],
  ['embed.go', 'xembed.go', false]
];
for (const [glob, file, want] of globSamples) {
  if (matchGlob(glob, file) !== want) problems.push(`самопроверка глоба: ${glob} против ${file} дал ${!want}; ожидалось ${want}`);
}
const goFactSamples = [
  // [тело, ярус, причина яруса, Slow, карантин, метка ядра]
  ['func TestRouterA(t *testing.T) {\n\troutertest.Tier(t, routertest.Full, "долгий сценарий")\n}\n', 'full', 'долгий сценарий', null, null, false],
  ['func TestRouterB(t *testing.T) {\n\troutertest.Slow(t, "ждёт таймер")\n\troutertest.Quarantine(t, "some-slug")\n}\n', 'gate', null, 'ждёт таймер', 'some-slug', false],
  ['func TestRouterC(t *testing.T) {\n\ttg := routertest.CoreSpecific(t)\n\t_ = tg\n\t// routertest.Tier(t, routertest.Full, "в комментарии")\n}\n', 'gate', null, null, null, true],
  ['func TestRouterD(t *testing.T) {\n\ts := "routertest.Quarantine(t, \\"x\\")"\n\t_ = s\n}\n', 'gate', null, null, null, false]
];
for (const [body, tier, tierReason, slow, quar, core] of goFactSamples) {
  const got = goTestFacts(GO_HEAD + 'package x\n\n' + body)[0] || {};
  if (got.tier !== tier || got.tierReason !== tierReason || got.slowReason !== slow || got.quarantine !== quar || got.coreMarked !== core) {
    problems.push(`самопроверка фактов Go: образец ${JSON.stringify(body)} дал ${JSON.stringify(got)}`);
  }
}
const specFactSamples = [
  // [текст, название, ярус, причина яруса, slow, карантин, метка ядра]
  [
    "test('a', { tag: FULL_TAG, annotation: { type: 'tier-full', description: 'долго' } }, async () => {});\n",
    'a',
    'full',
    'долго',
    null,
    null,
    false
  ],
  [
    "test('b', { tag: CORE_TAG, annotation: [{ type: 'slow', description: 'ждёт' }] }, async () => {\n  quarantine('q-slug');\n});\n",
    'b',
    'gate',
    null,
    'ждёт',
    'q-slug',
    true
  ],
  ['test(`axe: тема ${theme}`, async () => {});\n', null, 'gate', null, null, null, false]
];
for (const [text, title, tier, tierReason, slow, quar, core] of specFactSamples) {
  const got = specTestFacts(text)[0] || {};
  if (
    got.test !== title ||
    got.tier !== tier ||
    got.tierReason !== tierReason ||
    got.slowReason !== slow ||
    got.quarantine !== quar ||
    got.coreMarked !== core
  ) {
    problems.push(`самопроверка фактов спека: образец ${JSON.stringify(text)} дал ${JSON.stringify(got)}`);
  }
}
if (validateAreas({ version: 1, areas: { Bad: {} }, extra: 1 }, root).length < 3) {
  problems.push('самопроверка схемы карты: неверная карта прошла проверку');
}
{
  // Подобласти: небезопасный глоб, неверная запись tests, повтор и лишний ключ отклоняются
  const area = (subareas) => ({
    version: 1,
    areas: { x: { title: 't', shared: true, paths: ['internal/x/**'], canary: { go: null, spec: null, reason: 'r' }, subareas } }
  });
  const good = area({ s: { title: 't', paths: ['internal/x/a.go'], tests: ['go:TestRouterA', 'tests/router/a.spec.ts'] } });
  const bad = [
    area({ s: { title: 't', paths: ['internal/../x/a.go'], tests: ['go:TestRouterA'] } }),
    area({ s: { title: 't', paths: ['internal/x/a.go'], tests: ['go:TestA; rm'] } }),
    area({ s: { title: 't', paths: ['internal/x/a.go'], tests: ['go:TestRouterA', 'go:TestRouterA'] } }),
    area({ s: { title: 't', paths: ['internal/x/a.go'], tests: ['go:TestRouterA'], extra: 1 } }),
    area({ S: { title: 't', paths: ['internal/x/a.go'], tests: ['go:TestRouterA'] } }),
    area({})
  ];
  if (validateAreas(good, root).length !== 0 || bad.some((d) => validateAreas(d, root).length === 0)) {
    problems.push('самопроверка схемы подобластей: верная карта отклонена или неверная прошла');
  }
}

// ── Карта и отслеживаемые файлы ──
let map = null;
try {
  map = loadAreas(root);
} catch (e) {
  problems.push(`карта ${AREAS_FILE}: ${e.message}`);
}

const tracked = execFileSync('git', ['ls-files'], { cwd: root, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 })
  .split('\n')
  .filter(Boolean)
  .filter((f) => fs.existsSync(path.join(root, f)));
const EXTRA_PRODUCT = new Set([
  'embed.go',
  'scripts/setup.sh',
  'frontend/src/lib/sourcesContract.json',
  'frontend/src/lib/routingContract.json'
]);
const isProduct = (f) =>
  EXTRA_PRODUCT.has(f) ||
  (/^(internal|cmd)\//.test(f) && !f.endsWith('_test.go') && !f.startsWith('internal/routertest/'));
const product = tracked.filter(isProduct);

let testCount = 0;
if (map) {
  const areas = map.areas;
  const names = new Set(Object.keys(areas));

  // Правило 3: живые глобы. Правило 4: покрытие продукта.
  const globs = [];
  for (const [name, a] of Object.entries(areas)) {
    for (const g of a.paths) {
      if (!tracked.some((f) => matchGlob(g, f))) violations.push(`${AREAS_FILE}: область ${name}: глоб ${g} не находит ни одного отслеживаемого файла`);
      globs.push(g);
    }
  }
  for (const f of product) {
    if (!globs.some((g) => matchGlob(g, f))) violations.push(`${f}: файл продукта не принадлежит ни одной области (${AREAS_FILE})`);
  }
  for (const f of EXTRA_PRODUCT) {
    if (!product.includes(f)) problems.push(`файл продукта ${f} не найден в git ls-files — список стража устарел`);
  }

  // Факты тестов (нужны правилам 1, 5, 6, 7).
  let facts = [];
  try {
    facts = testFacts(root);
  } catch (e) {
    problems.push(`сканер фактов тестов не отработал: ${e.message}`);
  }
  if (facts.length === 0) problems.push('сканер не нашёл ни одного роутерного теста — разбор сломан');
  const specFiles = tracked.filter((f) => /^frontend\/tests\/router\/[^/]+\.spec\.ts$/.test(f));
  const testExists = (t) =>
    t.startsWith('go:') ? facts.some((x) => x.kind === 'go' && x.test === t.slice(3)) : specFiles.includes('frontend/' + t);

  // Правило 7: подобласти — пути внутри родителя, живые глобы, существующие тесты.
  for (const [name, a] of Object.entries(areas)) {
    if (!a.subareas) continue;
    const inParent = (f) => a.paths.some((g) => matchGlob(g, f));
    for (const [sub, s] of Object.entries(a.subareas)) {
      const at = `${AREAS_FILE}: подобласть ${name}/${sub}`;
      for (const g of s.paths) {
        const files = tracked.filter((f) => matchGlob(g, f));
        if (files.length === 0) violations.push(`${at}: глоб ${g} не находит ни одного отслеживаемого файла`);
        const outside = files.filter((f) => !inParent(f));
        if (outside.length > 0) violations.push(`${at}: глоб ${g} выходит за пути родителя ${name} (${outside.slice(0, 3).join(', ')})`);
      }
      for (const t of s.tests) if (!testExists(t)) violations.push(`${at}: tests — ${t} не найден`);
    }
  }

  // Правило 8: полнота подобластей. Каждый файл продукта родителя входит в подобласть (иначе
  // его правка выбирала бы область целиком), и каждый тест с меткой родителя назван в tests
  // какой-либо подобласти этого же родителя (иначе правка родителя его больше не выбирает).
  for (const [name, a] of Object.entries(areas)) {
    if (!a.subareas) continue;
    // подобласти именно этой области: тест, названный лишь в подобласти другой области, правкой
    // файлов родителя не выбирается
    const subListed = new Set(Object.values(a.subareas).flatMap((s) => s.tests));
    for (const f of product) {
      if (a.paths.some((g) => matchGlob(g, f)) && subareasOf(areas, name, f).length === 0) {
        violations.push(`${AREAS_FILE}: область ${name}: файл продукта ${f} не входит ни в одну подобласть`);
      }
    }
    const own = new Set();
    for (const x of facts) {
      if (x.area !== name) continue;
      own.add(x.kind === 'go' ? 'go:' + x.test : x.spec);
    }
    for (const t of [...own].sort()) {
      if (!subListed.has(t)) violations.push(`${AREAS_FILE}: область ${name}: ${t} с меткой области не назван ни в одной подобласти`);
    }
  }

  // Правило 6: поле tests у contract-*.
  let readers = {};
  try {
    readers = contractReaders(root);
  } catch (e) {
    problems.push(`разбор читателей контрактов не отработал: ${e.message}`);
  }
  for (const [name, a] of Object.entries(areas)) {
    const listed = new Set(a.tests || []);
    for (const t of listed) {
      if (!testExists(t)) violations.push(`${AREAS_FILE}: область ${name}: tests — ${t} не найден`);
    }
    if (readers[name]) {
      for (const t of readers[name]) {
        if (!listed.has(t)) violations.push(`${AREAS_FILE}: область ${name}: ${t} читает контракт или testdata, но не указан в tests`);
      }
    }
  }
  for (const name of Object.keys(readers)) {
    if (!names.has(name)) violations.push(`${AREAS_FILE}: нет области ${name} для читателей контракта`);
  }

  if (!MAP_ONLY) {
    // Правила 1-2: строка области в каждом роутерном файле, область известна.
    const files = routerGoFiles(root).map((f) => ({ file: f.file, text: f.text }));
    for (const file of specFiles) files.push({ file, text: fs.readFileSync(path.join(root, file), 'utf8') });
    testCount = files.length;
    for (const { file, text } of files) {
      const { area, issues } = areaOfText(text);
      for (const i of issues) violations.push(`${file}: ${i}`);
      if (issues.length > 0) continue;
      if (area === null) violations.push(`${file}: нет строки «// router-area: <область>» в первых пяти строках`);
      else if (!names.has(area)) violations.push(`${file}: неизвестная область ${area} (нет в ${AREAS_FILE})`);
    }

    // Правило 5: канарейки.
    const history = loadHistory();
    if (!history) notes.push('длительность канареек не проверена: нет истории (CI)');
    for (const [name, a] of Object.entries(areas)) {
      const c = a.canary;
      // У областей contract-* своих файлов тестов нет: канарейка берётся из их поля tests.
      const listed = new Set(a.tests || []);
      if (c.go) {
        const rows = facts.filter((x) => x.kind === 'go' && x.test === c.go);
        if (rows.length === 0) violations.push(`канарейка ${name}: Go-тест ${c.go} не найден`);
        for (const x of rows) {
          if (x.area !== name && !listed.has('go:' + c.go)) {
            violations.push(`канарейка ${name}: ${c.go} в файле области ${x.area} (${x.file})`);
          }
          if (x.quarantine !== null) violations.push(`канарейка ${name}: ${c.go} в карантине`);
          if (x.coreMarked) violations.push(`канарейка ${name}: ${c.go} с меткой ядра (CoreSpecific или known с ядром)`);
          if (x.tier !== 'gate') violations.push(`канарейка ${name}: ${c.go} в ярусе full`);
        }
        if (history) checkDuration(history, name, c.go, [(check) => check === 'go:' + c.go]);
      }
      if (c.spec) {
        if (!specFiles.includes('frontend/' + c.spec)) {
          violations.push(`канарейка ${name}: спек ${c.spec} не найден`);
          continue;
        }
        const rows = facts.filter((x) => x.kind === 'pw' && x.spec === c.spec);
        const { area } = areaOfText(fs.readFileSync(path.join(root, 'frontend', c.spec), 'utf8'));
        if (area !== name && !listed.has(c.spec)) violations.push(`канарейка ${name}: спек ${c.spec} в области ${area}`);
        for (const x of rows) {
          const t = x.test || x.titleTemplate || '(вычисляемое название)';
          if (x.quarantine !== null) violations.push(`канарейка ${name}: тест «${t}» спека ${c.spec} в карантине`);
          if (x.coreMarked) violations.push(`канарейка ${name}: тест «${t}» спека ${c.spec} с тегом ядра`);
          if (x.tier !== 'gate') violations.push(`канарейка ${name}: тест «${t}» спека ${c.spec} в ярусе full`);
        }
        if (history) checkDuration(history, name, c.spec, rows.map(titleMatcher).filter(Boolean));
      }
    }
  }
}

/** Длительность канарейки по ядрам: сумма медиан подходящих проверок, каждое ядро короче порога. */
function checkDuration(history, area, what, matchers) {
  const perCore = new Map();
  for (const [check, byCore] of history) {
    if (!matchers.some((m) => m(check))) continue;
    for (const [core, xs] of byCore) perCore.set(core, (perCore.get(core) || 0) + median(xs));
  }
  if (perCore.size === 0) {
    notes.push(`канарейка ${area}: ${what} — нет замеров в истории за ${HISTORY_DAYS} дней`);
    return;
  }
  for (const [core, sec] of perCore) {
    if (sec >= CANARY_MAX_SECONDS) {
      violations.push(`канарейка ${area}: ${what} на ядре ${core} — медиана ${sec.toFixed(1)} с, нужно меньше ${CANARY_MAX_SECONDS} с`);
    }
  }
}

if (violations.length > 0 || problems.length > 0) {
  console.error(`❌ Области роутерных тестов${MAP_ONLY ? ' (только карта)' : ''}:`);
  for (const v of violations) console.error('  ' + v);
  for (const p of problems) console.error('  ' + p);
  if (violations.length > 0) {
    console.error(
      `Карта — ${AREAS_FILE}: файл продукта добавляется в paths своей области; роутерный тест несёт строку «// router-area: <область>» в первых пяти строках (Go — после //go:build router и пустой строки).`
    );
  }
  process.exit(1);
}
for (const n of notes) console.log('  ' + n);
const areaCount = map ? Object.keys(map.areas).length : 0;
console.log(
  MAP_ONLY
    ? `✅ областей ${areaCount}, файлов продукта ${product.length} (только карта)`
    : `✅ областей ${areaCount}, файлов тестов ${testCount}, файлов продукта ${product.length}`
);
