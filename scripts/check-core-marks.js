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
 * Самопроверка сканера на образцах идёт при каждом запуске: расхождение — код 1.
 *
 * Usage: node scripts/check-core-marks.js
 */
const path = require('path');
const { analyzeGo, goMarks, goViolations, specIssues, specViolations } = require('./router/core-marks.cjs');

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
  [HEAD + 'func TestRouter_Bad;rm(t *testing.T) {\n\troutertest.CoreSpecific(t)\n}\n', 0, 0, '']
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
  violations = goViolations(root).concat(specViolations(root));
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
      'Go: тест, читающий ядро, получает цель через routertest.CoreSpecific(t) прямо в своём теле. Playwright: тест, читающий ядро, объявляется с { tag: CORE_TAG }.'
    );
  }
  process.exit(1);
}
console.log(`✅ Тесты, зависящие от ядра, размечены (${marks.length} размеченных Go-тестов)`);
