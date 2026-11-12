#!/usr/bin/env node
/*
 * check-core-marks.js — разметка «тест зависит от ядра» в роутерных тестах (D-05а).
 *
 * В changed роутерные тесты без метки ядра идут в одну ветвь матрицы, размеченные —
 * в обе. Тест, который читает ядро, но метки не имеет, терял бы проверку на второй
 * ветви. Скрипт читает исходники (git ls-files, без запуска) через общий сканер
 * scripts/router/core-marks.cjs.
 *
 * Правила (Go):
 *   1. Тело TestRouter*, читающее ядро (.Core, XCP_RT_CORE) или переключающее его
 *      (switch_kernel), обязано вызывать routertest.CoreSpecific(t).
 *   2. Помощник (не Test*) сам не берёт ядро из routertest.Current, routertest.CoreSpecific
 *      или XCP_RT_CORE и не читает .Core у параметра routertest.Target: цель из Current(t)
 *      не размечает тест, поэтому ядро передаётся строкой из теста с CoreSpecific.
 * Правила (TS, спеки frontend/tests/router/*.spec.ts, кроме core-switch.spec.ts):
 *   3. Спек, читающий ядро (RT.core, RT.wantCore, XCP_CORE, XCP_WANT_CORE, selectorApplies),
 *      объявляет такие тесты с тегом ядра (CORE_TAG, '@core' или coreTagsOf): тег нужен на
 *      каждом test(...), читающем ядро напрямую или через помощника спека, а не только в файле.
 *   4. Метка knownFailure с ядром в селекторе («арх/ядро», «* / ядро») идёт через coreTagsOf:
 *      тег @core-label выводится автоматически.
 *   5. Помощник frontend/tests/router/lib/*.ts (кроме env.ts и known.ts) не читает RT.core.
 * Правила метки «одно ядро» (D-01…D-04): тест идёт на одном ядре (ядро из SHA HEAD), его
 * зависимость от ядра на обоих ядрах проверяет названный тест-покрытие. Метка выполняет
 * правило 1, но метку ядра не даёт. Числового потолка на метки нет.
 *   6. Go: routertest.OneCore(t, "<причина>", "TestRouter<Покрытие>") — прямо в теле
 *      TestRouter* (не в помощнике и не в объявлении уровня файла), один раз, причина и
 *      покрытие — непустые строковые литералы, покрытие — имя другого TestRouter*;
 *      вместе с routertest.CoreSpecific метка не ставится.
 *      Спек: тег ONE_CORE_TAG ('@one-core') — только в деталях test(…), с аннотацией
 *      { type: ONE_CORE_ANNOTATION, description: '<причина>; covered-by: <название теста>' },
 *      причина и покрытие непустые, покрытие — другой тест; вместе с CORE_TAG тег не ставится;
 *      аннотация one-core без тега — нарушение. Тег выполняет правило 3.
 *   7. Покрытие существует и того же семейства (Go — тот же пакет и router-area, спек — та же
 *      router-area), несёт метку ядра (CoreSpecific, CORE_TAG), не в карантине и само не
 *      «одно ядро».
 * Самопроверка сканера на образцах идёт при каждом запуске: расхождение — код 1.
 *
 * Usage: node scripts/check-core-marks.js
 */
const path = require('path');
const {
  analyzeGo,
  goMarks,
  goTestFacts,
  goViolations,
  oneCoreIssues,
  specIssues,
  specTestFacts,
  specViolations,
  testFacts
} = require('./router/core-marks.cjs');

const root = path.resolve(__dirname, '..');

const HEAD = '//go:build router\n\npackage x\n\n';
const problems = [];

// Самопроверка сканера: если разбор сломан, страж молчал бы. [текст, нарушений, меток, причина первой метки]
const samples = [
  [HEAD + 'func TestRouterA(t *testing.T) {\n\ttg := routertest.Current(t)\n\t_ = tg.Core\n}\n', 1, 0, ''],
  [
    HEAD +
      'func TestRouterB(t *testing.T) {\n\tp := routertest.NewPanel(t)\n\tp.Call("POST", "/api/service/control?action=switch_kernel&kernel=xray", nil)\n}\n',
    1,
    0,
    ''
  ],
  [HEAD + 'func TestRouterC(t *testing.T) {\n\ttg := routertest.CoreSpecific(t)\n\t_ = tg.Core\n}\n', 0, 1, 'core'],
  [HEAD + 'func rtX(t *testing.T) string { return routertest.Current(t).Core }\n', 1, 0, ''],
  [HEAD + 'func rtY(t *testing.T) string { return os.Getenv("XCP_RT_CORE") }\n', 1, 0, ''],
  [HEAD + 'func rtZ(tg routertest.Target) string { return tg.Core }\n', 1, 0, ''],
  [HEAD + 'func rtW(tg routertest.Target) string { return tg.Arch }\n', 0, 0, ''],
  [
    HEAD +
      'var m = map[string]routertest.KnownMark{\n\t"a": routertest.Known("*/xray", "s"),\n}\n\nfunc TestRouterD(t *testing.T) {\n\troutertest.Current(t)\n}\n',
    0,
    1,
    'known-core'
  ],
  [HEAD + 'func TestRouterE(t *testing.T) {\n\tv := routertest.Known("arm64/mihomo", "s")\n\t_ = v\n}\n', 0, 1, 'known-core'],
  [HEAD + 'var m = routertest.Known("*", "s")\n\nfunc TestRouterF(t *testing.T) {\n\troutertest.Current(t)\n}\n', 0, 0, ''],
  [HEAD + 'func TestRouterG(t *testing.T) {\n\t// читает tg.Core и XCP_RT_CORE\n\t/* switch_kernel */\n\troutertest.Current(t)\n}\n', 0, 0, ''],
  [HEAD + 'func TestRouterH(t *testing.T) {\n\ts := "//" + routertest.Current(t).Core\n\t_ = s\n}\n', 1, 0, ''],
  [HEAD + 'func TestRouterI(t *testing.T) {\n\ts := `\nfunc TestRouterJ() {}\n.Core`\n\t_ = s\n}\n', 0, 0, ''],
  [HEAD + 'func TestRouter_Bad;rm(t *testing.T) {\n\troutertest.CoreSpecific(t)\n}\n', 0, 0, ''],
  // метка «одно ядро»: правило 1 выполнено, метки ядра нет
  [
    HEAD +
      'func TestRouterK(t *testing.T) {\n\ttg := routertest.OneCore(t, "перезапуск ядра", "TestRouterC")\n\t_ = tg.Core\n}\n',
    0,
    0,
    ''
  ],
  [HEAD + 'func TestRouterL(t *testing.T) {\n\ttg := routertest.OneCore(t, "", "TestRouterC")\n\t_ = tg.Core\n}\n', 1, 0, ''],
  [HEAD + 'func TestRouterM(t *testing.T) {\n\ttg := routertest.OneCore(t, "перезапуск ядра", "TestRouterM")\n\t_ = tg.Core\n}\n', 1, 0, ''],
  [HEAD + 'func rtV(t *testing.T) {\n\troutertest.OneCore(t, "перезапуск ядра", "TestRouterC")\n}\n', 1, 0, ''],
  [
    HEAD +
      'func TestRouterN(t *testing.T) {\n\troutertest.CoreSpecific(t)\n\ttg := routertest.OneCore(t, "перезапуск ядра", "TestRouterC")\n\t_ = tg.Core\n}\n',
    1,
    1,
    'core'
  ]
];
for (const [text, wantViolations, wantMarks, wantReason] of samples) {
  const got = analyzeGo(text);
  const reason = got.marks.length ? got.marks[0].reason : '';
  if (got.violations.length !== wantViolations || got.marks.length !== wantMarks || reason !== wantReason) {
    problems.push(
      `самопроверка сканера: образец ${JSON.stringify(text.slice(HEAD.length))} дал нарушений ${got.violations.length}, меток ${got.marks.length} (${reason || '-'}); ожидалось ${wantViolations}, ${wantMarks} (${wantReason || '-'})`
    );
  }
}

// Факт «одно ядро» у Go-теста: причина и покрытие есть, метки ядра нет (тест идёт в одну ветвь)
{
  const f = goTestFacts(samples.find(([t]) => t.includes('TestRouterK('))[0])[0];
  if (!f || !f.oneCore || f.oneCore.reason !== 'перезапуск ядра' || f.oneCore.covers !== 'TestRouterC' || f.coreMarked !== false) {
    problems.push(`самопроверка сканера: факт «одно ядро» у Go-теста разобран неверно: ${JSON.stringify(f)}`);
  }
}

// Самопроверка покрытия «одно ядро» на синтетических фактах: [описание, правка покрытия, нарушений]
const oneCoreBase = { kind: 'go', file: 'a_test.go', pkg: 'internal/x', area: 'dns', tier: 'gate', quarantine: null };
const oneCoreSamples = [
  ['правильное покрытие', {}, 0],
  ['покрытие из другого пакета', { pkg: 'internal/y' }, 1],
  ['покрытие из другой router-area', { area: 'routing' }, 1],
  ['покрытие без метки ядра', { coreMarked: false }, 1],
  ['покрытие в карантине', { quarantine: 'flaky-x' }, 1],
  ['покрытие само «одно ядро»', { coreMarked: false, oneCore: { reason: 'r', covers: 'TestRouterK' } }, 1],
  ['покрытия нет', { test: 'TestRouterZ' }, 1],
  // спек: семейство — та же router-area, пакета нет
  ['спек: правильное покрытие', { kind: 'pw', pkg: null }, 0, { kind: 'pw', pkg: null }],
  ['спек: покрытие из другой router-area', { kind: 'pw', pkg: null, area: 'routing' }, 1, { kind: 'pw', pkg: null }],
  ['спек: покрытие — Go-тест', {}, 1, { kind: 'pw', pkg: null }]
];
for (const [what, patch, want, selfPatch = {}] of oneCoreSamples) {
  const facts = [
    { ...oneCoreBase, test: 'TestRouterK', coreMarked: false, oneCore: { reason: 'r', covers: 'TestRouterC' }, ...selfPatch },
    { ...oneCoreBase, test: 'TestRouterC', coreMarked: true, oneCore: null, ...patch }
  ];
  // у покрытия «одно ядро» нарушение даёт и оно само (его покрытие K без метки ядра): считаем только K
  const got = oneCoreIssues(facts).filter((i) => i.test === 'TestRouterK').length;
  if (got !== want) {
    problems.push(`самопроверка покрытия «одно ядро»: ${what} дало нарушений ${got}; ожидалось ${want}`);
  }
}

// Факт «одно ядро» у спека: причина и покрытие из аннотации, тега ядра нет
const ONE_CORE_SPEC =
  "import { ONE_CORE_TAG, ONE_CORE_ANNOTATION } from './lib/known';\ntest('a', { tag: ONE_CORE_TAG, annotation: { type: ONE_CORE_ANNOTATION, description: 'перезапуск; covered-by: x' } }, async () => {\n  if (RT.core === 'xray') return;\n});\n";
{
  const f = specTestFacts(ONE_CORE_SPEC)[0];
  if (!f || !f.oneCore || f.oneCore.reason !== 'перезапуск' || f.oneCore.covers !== 'x' || f.coreMarked !== false) {
    problems.push(`самопроверка сканера спеков: факт «одно ядро» разобран неверно: ${JSON.stringify(f)}`);
  }
}

// Самопроверка TS-правил: [текст спека, ожидаемое число нарушений]
const specSamples = [
  ["test('a', async () => {\n  if (RT.core === 'xray') return;\n});\n", 1],
  [
    "import { CORE_TAG } from './lib/known';\ntest('a', { tag: CORE_TAG }, async () => {\n  if (RT.core === 'xray') return;\n});\n",
    0
  ],
  ["// RT.core в комментарии\ntest('a', async () => {});\n", 0],
  [
    "import { CORE_TAG } from './lib/known';\ntest('a', async () => {\n  if (RT.core === 'xray') return;\n});\n",
    1
  ],
  ["function m() {\n  knownFailure('*/xray', 's');\n}\n", 1],
  ["const tag = coreTagsOf(() => {\n  knownFailure('*/xray', 's');\n});\n", 0],
  ["function m() {\n  knownFailure('*', 's');\n  knownFailure('arm64', 's');\n}\n", 0],
  // тег есть у одного теста файла, второй читает ядро без тега
  [
    "test('a', { tag: CORE_TAG }, async () => {\n  if (RT.core === 'xray') return;\n});\ntest('b', async () => {\n  if (RT.core === 'mihomo') return;\n});\n",
    1
  ],
  // помощник спека читает ядро, тест его вызывает без тега
  [
    "function tailDir() {\n  return RT.core;\n}\ntest('a', { tag: CORE_TAG }, async () => {\n  tailDir();\n});\ntest('b', async () => {\n  tailDir();\n});\n",
    1
  ],
  [
    "test('a', { tag: CORE_TAG }, async () => {\n  if (RT.core === 'xray') return;\n});\ntest('b', async () => {\n  await page.goto('/');\n});\n",
    0
  ],
  // регулярный литерал со скобкой и апострофом не сбивает разбор: тест b без тега найден
  [
    "test('a', { tag: CORE_TAG }, async () => {\n  if (RT.core === 'xray') return;\n});\ntest('b', async () => {\n  expect(x).toMatch(/\\(/);\n  expect(y).toMatch(/it's/);\n  if (RT.core === 'mihomo') return;\n});\n",
    1
  ],
  // скобки вызова не сошлись: страж сообщает о неразобранном вызове, а не молчит
  [
    "test('a', { tag: CORE_TAG }, async () => {\n  if (RT.core === 'xray') return;\n});\ntest('b', async () => {\n  const r = a ? /x/ : (1;\n",
    1
  ],
  // метка «одно ядро»: правило 3 выполнено, причина и покрытие — в аннотации
  [ONE_CORE_SPEC, 0],
  // тег без аннотации с причиной и покрытием
  ["test('a', { tag: ONE_CORE_TAG }, async () => {\n  if (RT.core === 'xray') return;\n});\n", 1],
  // аннотация без покрытия
  [
    "test('a', { tag: ONE_CORE_TAG, annotation: { type: ONE_CORE_ANNOTATION, description: 'перезапуск' } }, async () => {\n  if (RT.core === 'xray') return;\n});\n",
    1
  ],
  // тег «одно ядро» вместе с тегом ядра
  [
    "test('a', { tag: [CORE_TAG, ONE_CORE_TAG], annotation: { type: ONE_CORE_ANNOTATION, description: 'перезапуск; covered-by: x' } }, async () => {\n  if (RT.core === 'xray') return;\n});\n",
    1
  ],
  // аннотация one-core без тега: тест к тому же читает ядро без метки
  [
    "test('a', { annotation: { type: 'one-core', description: 'перезапуск; covered-by: x' } }, async () => {\n  if (RT.core === 'xray') return;\n});\n",
    2
  ],
  // тег вне деталей test(…)
  [
    "test.describe('d', { tag: ONE_CORE_TAG }, () => {\n  test('a', async () => {\n    await page.goto('/');\n  });\n});\n",
    1
  ]
];
for (const [text, want] of specSamples) {
  const got = specIssues(text).length;
  if (got !== want) {
    problems.push(
      `самопроверка сканера спеков: образец ${JSON.stringify(text)} дал нарушений ${got}; ожидалось ${want}`
    );
  }
}

let violations = [];
let marks = [];
try {
  violations = goViolations(root).concat(
    specViolations(root),
    oneCoreIssues(testFacts(root)).map((i) => ({ file: i.file, name: i.test, reason: i.reason }))
  );
  marks = goMarks(root);
} catch (e) {
  problems.push(`сканер не отработал: ${e.message}`);
}
if (problems.length === 0 && marks.length === 0 && violations.length === 0) {
  problems.push('сканер не нашёл ни одного размеченного теста — разбор сломан');
}

if (violations.length > 0 || problems.length > 0) {
  console.error('❌ Разметка зависимости роутерных тестов от ядра:');
  for (const v of violations) console.error(`  ${v.file}: ${v.name} — ${v.reason}`);
  for (const p of problems) console.error('  ' + p);
  if (violations.length > 0) {
    console.error(
      'Go: тест, читающий ядро, получает цель через routertest.CoreSpecific(t) (или routertest.OneCore(t, "<причина>", "<покрытие>")) прямо в своём теле. Playwright: тест, читающий ядро, объявляется с { tag: CORE_TAG }.'
    );
  }
  process.exit(1);
}
console.log(`✅ Тесты, зависящие от ядра, размечены (${marks.length} размеченных Go-тестов)`);
