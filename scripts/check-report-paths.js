#!/usr/bin/env node
// check-report-paths.js — отчёт стенда и пути вывода Playwright строятся только от корня рабочей копии.
//
// Случай, который ловит проверка: этап (pw-suite.sh) принимал чужой относительный RT_REPORT как есть,
// делал `cd frontend`, и Playwright разрешал outputDir/outputFile от frontend/ — половина отчёта
// оседала в frontend/build/router. Теперь все этапы принимают RT_REPORT через rt_report_use (lib.sh),
// которая приводит путь к абсолютному под <корень>/build/router. Скрипт читает исходники
// (git ls-files, без запуска).
//
// Правила:
//   R1. В scripts/router/*.sh, кроме lib.sh, вне комментариев нет конструкции «проверка непустоты
//       ${RT_REPORT:-}, затем || rt_report_init» (приём чужого RT_REPORT мимо rt_report_use).
//   R2. Каждый scripts/router/*.sh, кроме lib.sh, где вне комментариев есть RT_REPORT, содержит строку
//       rt_report_use или строку, состоящую только из rt_report_init (как в run.sh).
//   R3. lib.sh определяет rt_report_use() {, и в теле функции есть "$_root/$RT_REPORT",
//       "$_root"/build/router/?*) и */../*.
//   R4. pw-suite.sh содержит PW_DIR="$RT_REPORT/, XCP_PW_STATE=$STATE, XCP_PW_OUT=$OUT, XCP_PW_JSON=$_json,
//       утверждение "$ROOT"/build/router/*) и не меньше двух некомментарных строк с .playwright-artifacts-.
//   R5. В scripts/router/*.sh каждое некомментарное вхождение build/router/ непосредственно предварено
//       одним из: $_root/, "$_root"/, $ROOT/, "$ROOT"/, $(rt_repo_root)/.
//   R6. В session.mjs есть path.join(this.root, 'build', 'router', и нигде RT_REPORT или RT_REPORT_DIR
//       не получает строковый или шаблонный литерал.
// Самопроверка разбора на образцах идёт при каждом запуске: расхождение — код 1.
//
// Usage: node scripts/check-report-paths.js
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');

const isShComment = (line) => /^\s*#/.test(line);
const isJsComment = (line) => /^\s*\/\//.test(line);

// Некомментарные строки sh: [номер строки (с 1), текст]
function shCode(text) {
  return text
    .split('\n')
    .map((line, idx) => [idx + 1, line])
    .filter(([, line]) => !isShComment(line));
}

const R1_RE = /\[\s+-n\s+["']?\$\{RT_REPORT:-\}["']?\s+\]\s*\|\|\s*rt_report_init/;
const R5_RE = /build\/router\//g;
const R5_OK = ['$_root/', '"$_root"/', '$ROOT/', '"$ROOT"/', '$(rt_repo_root)/'];
const R6_RE = /RT_REPORT(_DIR)?\s*:\s*['"`]/;

// Правила R1, R2, R5 для текста одного sh-скрипта (не lib.sh). Возвращает список причин.
function shIssues(text) {
  const issues = [];
  const code = shCode(text);
  for (const [n, line] of code) {
    if (R1_RE.test(line)) issues.push(`${n}: приём RT_REPORT мимо rt_report_use (условный rt_report_init)`);
    R5_RE.lastIndex = 0;
    let m;
    while ((m = R5_RE.exec(line)) !== null) {
      const before = line.slice(0, m.index);
      if (!R5_OK.some((p) => before.endsWith(p))) {
        issues.push(`${n}: путь build/router/ строится не от корня рабочей копии`);
        break;
      }
    }
  }
  return issues;
}

// Правило R2: скрипт, использующий RT_REPORT вне комментариев, вызывает rt_report_use (или rt_report_init одной строкой).
function useIssues(text) {
  const code = shCode(text);
  if (!code.some(([, line]) => line.includes('RT_REPORT'))) return [];
  const ok = code.some(([, line]) => /^\s*rt_report_use\b/.test(line) || /^\s*rt_report_init\s*$/.test(line));
  return ok ? [] : [' использует RT_REPORT, но не вызывает rt_report_use'];
}

// Правило R6 для текста session.mjs.
function mjsIssues(text) {
  const issues = [];
  if (!text.includes("path.join(this.root, 'build', 'router'")) {
    issues.push("нет path.join(this.root, 'build', 'router', …): каталог отчёта сессии не от корня рабочей копии");
  }
  text.split('\n').forEach((line, idx) => {
    if (isJsComment(line)) return;
    if (R6_RE.test(line)) issues.push(`${idx + 1}: RT_REPORT/RT_REPORT_DIR получает строковый литерал`);
  });
  return issues;
}

const problems = [];

// Самопроверка: если разбор сломан, страж молчал бы. [функция, текст, ожидаемое число нарушений]
const samples = [
  [shIssues, '[ -n "${RT_REPORT:-}" ] || rt_report_init\n', 1],
  [shIssues, 'rt_report_use\n', 0],
  [useIssues, 'echo "$RT_REPORT"\n', 1],
  [useIssues, 'rt_report_use\necho "$RT_REPORT"\n', 0],
  [shIssues, '# [ -n "${RT_REPORT:-}" ] || rt_report_init\n', 0],
  [shIssues, 'mkdir -p "build/router/x"\n', 1],
  [shIssues, 'mkdir -p "$ROOT/build/router/x"\n', 0],
  [mjsIssues, "path.join(this.root, 'build', 'router', base)\nconst env = { RT_REPORT: 'build/router/x' };\n", 1]
];
for (const [fn, text, want] of samples) {
  const got = fn(text).length;
  if (got !== want) {
    problems.push(`самопроверка разбора: образец ${JSON.stringify(text)} дал нарушений ${got}; ожидалось ${want}`);
  }
}

const violations = [];
let scripts = 0;
try {
  const files = execFileSync('git', ['ls-files', 'scripts/router/*.sh', 'scripts/router/session.mjs'], {
    cwd: root,
    encoding: 'utf8'
  })
    .split('\n')
    .filter(Boolean);
  if (files.length === 0) problems.push('git ls-files не нашёл ни одного скрипта стенда — разбор сломан');
  const read = (f) => fs.readFileSync(path.join(root, f), 'utf8');
  for (const f of files) {
    scripts++;
    const text = read(f);
    if (f === 'scripts/router/lib.sh') {
      const lines = text.split('\n');
      const start = lines.findIndex((l) => l.startsWith('rt_report_use() {'));
      if (start < 0) {
        violations.push(`${f}: нет определения rt_report_use() {`);
        continue;
      }
      let end = lines.findIndex((l, i) => i > start && l === '}');
      if (end < 0) end = lines.length;
      const body = lines.slice(start, end).join('\n');
      for (const need of ['"$_root/$RT_REPORT"', '"$_root"/build/router/?*)', '*/../*']) {
        if (!body.includes(need)) violations.push(`${f}: в rt_report_use нет ${need}`);
      }
      continue;
    }
    if (f.endsWith('.mjs')) {
      for (const reason of mjsIssues(text)) violations.push(`${f}:${reason}`);
      continue;
    }
    for (const reason of shIssues(text)) violations.push(`${f}:${reason}`);
    for (const reason of useIssues(text)) violations.push(`${f}:${reason}`);
    if (f === 'scripts/router/pw-suite.sh') {
      for (const need of [
        'PW_DIR="$RT_REPORT/',
        'XCP_PW_STATE=$STATE',
        'XCP_PW_OUT=$OUT',
        'XCP_PW_JSON=$_json',
        '"$ROOT"/build/router/*)'
      ]) {
        if (!text.includes(need)) violations.push(`${f}: нет ${need}`);
      }
      const cleanups = shCode(text).filter(([, line]) => line.includes('.playwright-artifacts-')).length;
      if (cleanups < 2) violations.push(`${f}: уборка .playwright-artifacts-* нужна и после запуска, и в EXIT-trap (найдено ${cleanups})`);
    }
  }
} catch (e) {
  problems.push(`сканер не отработал: ${e.message}`);
}

if (violations.length > 0 || problems.length > 0) {
  console.error('❌ check-report-paths: пути отчёта стенда строятся не от корня рабочей копии:');
  for (const v of violations) console.error('  ' + v);
  for (const p of problems) console.error('  ' + p);
  if (violations.length > 0) {
    console.error(
      'Этапы принимают RT_REPORT только через rt_report_use (lib.sh); пути build/router/ — от корня рабочей копии.'
    );
  }
  process.exit(1);
}
console.log(`✅ check-report-paths: пути отчёта строятся от корня рабочей копии (скриптов: ${scripts})`);
