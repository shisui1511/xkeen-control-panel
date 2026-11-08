#!/usr/bin/env node
/*
 * check-router-tiers.js — ярус full и метка Slow роутерных тестов (RTR-04, D-14, D-15).
 *
 * Ярус full получает тест по виду, не по длительности: реальное время (cron, слот расписания),
 * внесение сбоев, нагрузка на диск, реальные загрузки из интернета. Такой тест идёт в полном
 * прогоне, а в гейте PR — при прямом затрагивании его области. Долгий тест gate другого вида
 * (серии «Применить», активации профилей, смена ядра) остаётся в gate с меткой Slow и причиной —
 * он кандидат на ускорение. Метки читаются статически (без запуска) через сканер
 * scripts/router/core-marks.cjs; неверная метка молча меняла бы состав гейта.
 *
 * Разметка:
 *   Go  — routertest.Tier(t, routertest.Full, "<причина>") и routertest.Slow(t, "<причина>")
 *         прямо в теле TestRouter*;
 *   PW  — test('…', { tag: FULL_TAG, annotation: { type: 'tier-full', description: '<причина>' } }, …)
 *         и annotation: { type: 'slow', description: '<причина>' }.
 *
 * Правила (нарушение — код 1):
 *   1. Метка вне тела TestRouter* (в помощнике, на уровне файла, в другом Test*) — нарушение.
 *   2. Причина — непустой строковый литерал; уровень Tier — routertest.Full или routertest.Gate.
 *   3. @full без аннотации tier-full и аннотация tier-full без @full — нарушение.
 *   4. Тест одновременно full и Slow — нарушение.
 *   5. Причина яруса full начинается с вида: «реальное время:», «внесение сбоя:»,
 *      «нагрузка на диск:», «реальная загрузка:» или «визуальная матрица:» (решение владельца D-20 фазы 149).
 * Предупреждение (код не меняет): тест gate без Slow, чья медиана по build/router/history.tsv
 * основной копии за 60 дней больше бюджета (Go — 60 с, PW — 30 с) хотя бы на одном ядре, —
 * строка «бюджет gate превышен: <тест> <медиана> с». Истории нет (CI) — проверка пропускается.
 * Самопроверка разбора на образцах идёт при каждом запуске: расхождение — код 1.
 *
 * Usage: node scripts/check-router-tiers.js
 */
const fs = require('fs');
const path = require('path');
const {
  routerGoFiles,
  trackedFiles,
  testFacts,
  goTestFacts,
  specTestFacts,
  goTierIssues,
  specTierIssues,
  titleMatcher,
  loadHistory,
  median
} = require('./router/core-marks.cjs');

const root = path.resolve(__dirname, '..');
const HISTORY_DAYS = 60;
const BUDGET_GO = 60;
const BUDGET_PW = 30;
const REASON_KIND = /^(?:реальное время|внесение сбоя|нагрузка на диск|реальная загрузка|визуальная матрица): \S/;

const problems = [];
const violations = [];
const warnings = [];

// ── Самопроверка разбора: если он сломан, страж молчал бы ──
const GO_HEAD = '//go:build router\n\npackage x\n\n';
const goSamples = [
  // [тело файла, ожидаемое число нарушений]
  ['func TestRouterA(t *testing.T) {\n\troutertest.Tier(t, routertest.Full, "реальное время: ждёт cron")\n}\n', 0],
  ['func TestRouterA(t *testing.T) {\n\troutertest.Slow(t, "серия «Применить»")\n}\n', 0],
  ['func rtHelper(t *testing.T) {\n\troutertest.Tier(t, routertest.Full, "реальное время: x")\n}\n', 1],
  ['func rtHelper(t *testing.T) {\n\troutertest.Slow(t, "x")\n}\n', 1],
  ['var x = func(t *testing.T) { routertest.Slow(t, "x") }\n', 1],
  ['func TestRouterA(t *testing.T) {\n\troutertest.Tier(t, routertest.Full, "")\n}\n', 1],
  ['func TestRouterA(t *testing.T) {\n\troutertest.Tier(t, routertest.Full, why)\n}\n', 1],
  ['func TestRouterA(t *testing.T) {\n\troutertest.Slow(t, "")\n}\n', 1],
  ['func TestRouterA(t *testing.T) {\n\troutertest.Tier(t, lvl, "реальное время: x")\n}\n', 1],
  [
    'func TestRouterA(t *testing.T) {\n\troutertest.Tier(t, routertest.Full, "реальное время: x")\n\troutertest.Slow(t, "y")\n}\n',
    1
  ],
  ['func TestRouterA(t *testing.T) {\n\t// routertest.Slow(t, "")\n\ts := "routertest.Tier(t, lvl, 1)"\n\t_ = s\n}\n', 0],
  ['func TestPlain(t *testing.T) {\n\troutertest.Slow(t, "x")\n}\n', 1]
];
for (const [body, want] of goSamples) {
  const got = goTierIssues(GO_HEAD + body).length;
  if (got !== want) problems.push(`самопроверка Go: образец ${JSON.stringify(body)} дал нарушений ${got}; ожидалось ${want}`);
}
const specSamples = [
  ["test('a', { tag: FULL_TAG, annotation: { type: 'tier-full', description: 'реальное время: x' } }, async () => {});\n", 0],
  ["test('a', { tag: [CORE_TAG, FULL_TAG], annotation: [{ type: 'tier-full', description: 'x' }] }, async () => {});\n", 0],
  ["test('a', { annotation: { type: 'slow', description: 'x' } }, async () => {});\n", 0],
  ["test('a', { tag: FULL_TAG, annotation: { type: TIER_FULL_ANNOTATION, description: 'x' } }, async () => {});\n", 0],
  ["test('a', { annotation: { type: SLOW_ANNOTATION, description: 'x' } }, async () => {});\n", 0],
  ["test('a', { annotation: { type: SLOW_ANNOTATION, description: '' } }, async () => {});\n", 1],
  ["test('a', { tag: FULL_TAG }, async () => {});\n", 1],
  ["test('a', { annotation: { type: 'tier-full', description: 'x' } }, async () => {});\n", 1],
  ["test('a', { tag: FULL_TAG, annotation: { type: 'tier-full', description: '' } }, async () => {});\n", 1],
  ["test('a', { tag: FULL_TAG, annotation: { type: 'tier-full', description: WHY } }, async () => {});\n", 1],
  ["test('a', { annotation: { type: 'slow', description: '' } }, async () => {});\n", 1],
  [
    "test('a', { tag: FULL_TAG, annotation: [{ type: 'tier-full', description: 'x' }, { type: 'slow', description: 'y' }] }, async () => {});\n",
    1
  ],
  ["// test('a', { tag: FULL_TAG }, async () => {});\ntest('b', async () => {});\n", 0]
];
for (const [text, want] of specSamples) {
  const got = specTierIssues(text).length;
  if (got !== want) problems.push(`самопроверка PW: образец ${JSON.stringify(text)} дал нарушений ${got}; ожидалось ${want}`);
}
const factSamples = [
  [goTestFacts(GO_HEAD + 'func TestRouterA(t *testing.T) {\n\troutertest.Tier(t, routertest.Full, "реальное время: x")\n}\n')[0], 'full', 'реальное время: x', null],
  [goTestFacts(GO_HEAD + 'func TestRouterA(t *testing.T) {\n\troutertest.Slow(t, "y")\n}\n')[0], 'gate', null, 'y'],
  [specTestFacts("test('a', { tag: FULL_TAG, annotation: { type: 'tier-full', description: 'x' } }, async () => {});\n")[0], 'full', 'x', null],
  [specTestFacts("test('a', { annotation: { type: 'slow', description: 'y' } }, async () => {});\n")[0], 'gate', null, 'y'],
  [
    specTestFacts("test('a', { tag: FULL_TAG, annotation: { type: TIER_FULL_ANNOTATION, description: 'z' } }, async () => {});\n")[0],
    'full',
    'z',
    null
  ],
  [specTestFacts("test('a', { annotation: { type: SLOW_ANNOTATION, description: 'w' } }, async () => {});\n")[0], 'gate', null, 'w']
];
for (const [got, tier, tierReason, slowReason] of factSamples) {
  if (!got || got.tier !== tier || got.tierReason !== tierReason || got.slowReason !== slowReason) {
    problems.push(`самопроверка фактов: получено ${JSON.stringify(got)}; ожидалось ${tier}/${tierReason}/${slowReason}`);
  }
}
if (
  !REASON_KIND.test('реальное время: ждёт cron') ||
  !REASON_KIND.test('визуальная матрица: ширины и темы') ||
  REASON_KIND.test('долго') ||
  REASON_KIND.test('реальное время:')
) {
  problems.push('самопроверка вида причины: шаблон REASON_KIND сломан');
}

// ── Файлы репозитория ──
let facts = [];
try {
  facts = testFacts(root);
  for (const f of routerGoFiles(root)) {
    for (const i of goTierIssues(f.text)) violations.push(`${f.file}: ${i.name} — ${i.reason}`);
  }
  for (const file of trackedFiles(root, 'frontend/tests/router/*.spec.ts')) {
    for (const i of specTierIssues(fs.readFileSync(path.join(root, file), 'utf8'))) {
      violations.push(`${file}: «${i.test}» — ${i.reason}`);
    }
  }
} catch (e) {
  problems.push(`сканер не отработал: ${e.message}`);
}
if (problems.length === 0 && facts.length === 0) problems.push('сканер не нашёл ни одного роутерного теста — разбор сломан');

const label = (x) => (x.kind === 'go' ? x.test : `${x.spec}: «${x.test || x.titleTemplate || '(вычисляемое название)'}»`);
let fullCount = 0;
let slowCount = 0;
for (const x of facts) {
  if (x.tier === 'full') {
    fullCount++;
    if (!x.tierReason) violations.push(`${x.file}: ${label(x)} — ярус full без причины`);
    else if (!REASON_KIND.test(x.tierReason)) {
      violations.push(
        `${x.file}: ${label(x)} — причина яруса full «${x.tierReason}» не начинается с вида («реальное время:», «внесение сбоя:», «нагрузка на диск:», «реальная загрузка:», «визуальная матрица:»)`
      );
    }
  }
  if (x.slowReason) slowCount++;
}

// ── Бюджет gate по истории (только предупреждения) ──
const history = problems.length === 0 ? loadHistory(root, HISTORY_DAYS) : null;
if (problems.length === 0 && !history) warnings.push('бюджет gate не проверен: нет истории (CI)');
if (history) {
  for (const x of facts) {
    if (x.tier !== 'gate' || x.slowReason) continue;
    const budget = x.kind === 'go' ? BUDGET_GO : BUDGET_PW;
    const match = x.kind === 'go' ? (check) => check === 'go:' + x.test : titleMatcher(x);
    if (!match) continue;
    let worst = null;
    for (const [check, byCore] of history) {
      if (!match(check)) continue;
      for (const [core, xs] of byCore) {
        const m = median(xs);
        if (worst === null || m > worst.sec) worst = { sec: m, core };
      }
    }
    if (worst && worst.sec > budget) {
      warnings.push(`бюджет gate превышен: ${label(x)} ${worst.sec.toFixed(0)} с (ядро ${worst.core}, бюджет ${budget} с)`);
    }
  }
}

if (violations.length > 0 || problems.length > 0) {
  console.error('❌ Ярусы и метка Slow роутерных тестов:');
  for (const v of violations) console.error('  ' + v);
  for (const p of problems) console.error('  ' + p);
  if (violations.length > 0) {
    console.error(
      'Go: routertest.Tier(t, routertest.Full, "<вид>: <причина>") и routertest.Slow(t, "<причина>") — литералами прямо в теле TestRouter*. Playwright: { tag: FULL_TAG, annotation: { type: TIER_FULL_ANNOTATION, description: \'<вид>: <причина>\' } } или annotation: { type: SLOW_ANNOTATION, description: \'<причина>\' }. Ярус full — по виду теста (реальное время, внесение сбоя, нагрузка на диск, реальная загрузка), долгий тест другого вида — Slow.'
    );
  }
  process.exit(1);
}
for (const w of warnings) console.log('  ' + w);
console.log(`✅ ярус full ${fullCount}, Slow ${slowCount}`);
