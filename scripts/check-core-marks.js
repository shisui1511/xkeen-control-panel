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
 *      или XCP_RT_CORE: ядро передаётся параметром из теста с CoreSpecific.
 * Самопроверка сканера на образцах идёт при каждом запуске: расхождение — код 1.
 *
 * Usage: node scripts/check-core-marks.js
 */
const path = require('path');
const { analyzeGo, goMarks, goViolations } = require('./router/core-marks.cjs');

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
  [HEAD + 'func rtZ(tg routertest.Target) string { return tg.Core }\n', 0, 0, ''],
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

let violations = [];
let marks = [];
try {
  violations = goViolations(root);
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
    console.error('Тест, читающий ядро, получает цель через routertest.CoreSpecific(t) прямо в своём теле.');
  }
  process.exit(1);
}
console.log(`✅ Тесты, зависящие от ядра, размечены (${marks.length} размеченных Go-тестов)`);
