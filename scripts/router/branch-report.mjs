#!/usr/bin/env node
// scripts/router/branch-report.mjs <отчёт> <цель> --suite <имя> — разделы сводки прогона.
//
// Читает timings.tsv отчёта и печатает в stdout Markdown, который run.sh дописывает в
// summary.md после строки цели:
//   - «## Этапы по ветвям» — таблица «этап × ветвь» (секунды);
//   - «- Итого: N с; критический путь: …» — от первого начала до последнего конца этапов;
//   - «- Оболочка: N с (тесты M с)» — итог без объединения интервалов этапов тестов;
//   - «- Строк времени без начала или ядра: N» — строки timings.tsv короче шести колонок;
//   - «## 10 самых долгих тестов» — по Go (go-*.jsonl) и Playwright (pw*/report*.json).
// Формат timings.tsv: <цель> <этап> <секунды> <код> <начало> <ядро ветви или ->. У старых
// отчётов колонок пять или четыре: ядро берётся из суффикса имени этапа, иначе — по
// интервалу ветви. Скрипт только читает отчёт. В вывод попадают лишь имена этапов и тестов,
// ядра и секунды: деталь результатов, адреса и тексты ошибок не выводятся.
// Без зависимостей: только встроенные модули Node.

import fs from "node:fs";
import path from "node:path";

const argv = process.argv.slice(2);
const pos = [];
let suite = "";
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === "--suite") suite = argv[++i] || "";
  else pos.push(argv[i]);
}
if (pos.length !== 2) {
  console.error("usage: branch-report.mjs <отчёт> <цель> --suite <имя>");
  process.exit(2);
}
const [report, target] = pos;

// Верхние общие этапы (без ветви) и ветвевые (с суффиксом ядра в имени).
const COMMON = new Set([
  "reachable",
  "frontend-ci",
  "frontend-build",
  "init-script",
  "upload",
  "install",
  "preflight",
  "snapshot",
  "restore",
  "verify",
]);
const BRANCH = ["sync", "switch", "smoke", "observe", "pw-read", "go", "settle", "pw"];
const TEST_STAGES = new Set(["go", "pw-read", "pw"]);
const CORE_SUFFIX = /-(xray|mihomo)$/;

function isCommon(name) {
  return COMMON.has(name) || name.startsWith("build-");
}

// Набор reboot не делится на ветви: сводка по ним не нужна.
if (suite === "reboot") process.exit(0);

let raw = "";
try {
  raw = fs.readFileSync(path.join(report, "timings.tsv"), "utf8");
} catch {
  process.exit(0);
}

let shortRows = 0;
const stages = [];
for (const line of raw.split("\n")) {
  if (!line) continue;
  const f = line.split("\t");
  if (f[0] !== target && f[0] !== "all") continue;
  if (f.length < 6) shortRows++;
  const start = Number(f[4]);
  const sec = Number(f[2]);
  if (f.length < 5 || !Number.isFinite(start) || !Number.isFinite(sec)) continue;
  const name = f[1];
  const m = name.match(CORE_SUFFIX);
  const base = m ? name.slice(0, -m[0].length) : name;
  const col6 = f[5] && f[5] !== "-" ? f[5] : "";
  let kind = "";
  if (isCommon(name)) kind = "common";
  else if (BRANCH.includes(base)) kind = "branch";
  else continue; // подэтапы (smoke-kernel, go-build-…, go-run-…) и чужие этапы в таблицу не входят
  stages.push({
    name,
    base,
    sec,
    code: f[3],
    start,
    end: start + sec,
    kind,
    core: kind === "branch" ? col6 || (m ? m[1] : "") : "",
  });
}

if (stages.length === 0) process.exit(0);

// Интервалы ветвей: от начала её sync-<ядро> или switch-<ядро> до начала следующей.
const branchStart = new Map();
for (const s of stages) {
  if (s.kind === "branch" && s.core && (s.base === "sync" || s.base === "switch")) {
    if (!branchStart.has(s.core) || s.start < branchStart.get(s.core)) branchStart.set(s.core, s.start);
  }
}
const branches = [...branchStart.entries()].sort((a, b) => a[1] - b[1]);
for (const s of stages) {
  if (s.kind !== "branch" || s.core) continue;
  let pick = "";
  for (const [core, st] of branches) if (st <= s.start) pick = core;
  s.core = pick;
}
// ядра, которые есть только в именах этапов, но без sync/switch, тоже получают колонку
const cores = [...branches.map((b) => b[0])];
for (const s of stages) if (s.core && !cores.includes(s.core)) cores.push(s.core);

// --- таблица этап × ветвь ----------------------------------------------------------

const cols = ["", ...cores]; // "" — общее
const cell = new Map();
function add(row, core, sec) {
  const k = `${row}\t${core}`;
  cell.set(k, (cell.get(k) || 0) + sec);
}
const commonRows = [];
for (const s of stages.filter((x) => x.kind === "common").sort((a, b) => a.start - b.start)) {
  if (!commonRows.includes(s.name)) commonRows.push(s.name);
  add(s.name, "", s.sec);
}
const branchRows = BRANCH.filter((b) => stages.some((s) => s.kind === "branch" && s.base === b));
for (const s of stages) if (s.kind === "branch") add(s.base, s.core, s.sec);

const out = [];
out.push("", "## Этапы по ветвям", "", `Цель: ${target}`, "");
out.push(`| Этап | Общее | ${cores.join(" | ")} |`.replace(/ \| \|$/, " |"));
out.push(`|---|---:|${cores.map(() => "---:").join("|")}|`);
for (const row of [...commonRows, ...branchRows]) {
  const cells = cols.map((c) => {
    const v = cell.get(`${row}\t${c}`);
    return v === undefined ? "–" : String(v);
  });
  out.push(`| ${row} | ${cells.join(" | ")} |`);
}
out.push("");

// --- итог и критический путь -------------------------------------------------------

const t0 = Math.min(...stages.map((s) => s.start));
const t1 = Math.max(...stages.map((s) => s.end));
const total = t1 - t0;

const TOL = 1;
const used = new Set();
let cur = stages.reduce((a, b) => (b.end > a.end || (b.end === a.end && b.sec > a.sec) ? b : a));
const path_ = [cur];
used.add(cur);
for (;;) {
  let best = null;
  for (const s of stages) {
    if (used.has(s) || s.end > cur.start + TOL) continue;
    if (!best || s.end > best.end || (s.end === best.end && s.sec > best.sec)) best = s;
  }
  if (!best) break;
  used.add(best);
  path_.push(best);
  cur = best;
}
path_.reverse();
out.push(`- Итого: ${total} с; критический путь: ${path_.map((s) => `${s.name} (${s.sec})`).join(" → ")}`);

// Объединение интервалов этапов тестов.
const iv = stages
  .filter((s) => s.kind === "branch" && TEST_STAGES.has(s.base) && s.sec > 0)
  .map((s) => [s.start, s.end])
  .sort((a, b) => a[0] - b[0]);
let tests = 0;
let curS = null;
let curE = null;
for (const [a, b] of iv) {
  if (curE === null || a > curE) {
    if (curE !== null) tests += curE - curS;
    curS = a;
    curE = b;
  } else if (b > curE) {
    curE = b;
  }
}
if (curE !== null) tests += curE - curS;
out.push(`- Оболочка: ${total - tests} с (тесты ${tests} с)`);
out.push(`- Строк времени без начала или ядра: ${shortRows}`);
out.push("");

// --- 10 самых долгих тестов --------------------------------------------------------

const longest = [];
const coreDir = path.join(report, target);
let coreDirs = [];
try {
  coreDirs = fs
    .readdirSync(coreDir, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name);
} catch {
  coreDirs = [];
}

function list(dir, re) {
  try {
    return fs.readdirSync(dir).filter((n) => re.test(n));
  } catch {
    return [];
  }
}

for (const core of coreDirs) {
  const base = path.join(coreDir, core);
  // Go: событие test2json pass/fail/skip верхнего теста (без «/») несёт Elapsed
  for (const f of list(base, /^go-.*\.jsonl$/)) {
    let text = "";
    try {
      text = fs.readFileSync(path.join(base, f), "utf8");
    } catch {
      continue;
    }
    for (const line of text.split("\n")) {
      if (!line.includes('"Elapsed"')) continue;
      let ev;
      try {
        ev = JSON.parse(line);
      } catch {
        continue;
      }
      if (!["pass", "fail", "skip"].includes(ev.Action)) continue;
      if (!ev.Test || ev.Test.includes("/")) continue;
      if (typeof ev.Elapsed !== "number") continue;
      longest.push({ name: ev.Test, core, sec: ev.Elapsed });
    }
  }
  // Playwright: сумма длительностей прогонов теста; имя — как в pw-parse.js
  for (const dir of list(base, /^pw/)) {
    for (const f of list(path.join(base, dir), /^report.*\.json$/)) {
      let rep;
      try {
        rep = JSON.parse(fs.readFileSync(path.join(base, dir, f), "utf8"));
      } catch {
        continue;
      }
      const sums = new Map();
      const visit = (suiteNode, titles) => {
        for (const spec of suiteNode.specs || []) {
          const name = [...titles, spec.title].join(" > ");
          for (const test of spec.tests || []) {
            let ms = 0;
            for (const r of test.results || []) ms += Number(r.duration) || 0;
            sums.set(name, (sums.get(name) || 0) + ms);
          }
        }
        for (const child of suiteNode.suites || []) visit(child, [...titles, child.title]);
      };
      for (const top of rep.suites || []) visit(top, top.file === top.title ? [] : [top.title]);
      for (const [name, ms] of sums) longest.push({ name, core, sec: ms / 1000 });
    }
  }
}

if (longest.length > 0) {
  longest.sort((a, b) => b.sec - a.sec);
  out.push("## 10 самых долгих тестов", "", "| Тест | Ядро | с |", "|---|---|---:|");
  for (const t of longest.slice(0, 10)) {
    const name = t.name.replace(/\\/g, "\\\\").replace(/\|/g, "\\|");
    out.push(`| ${name} | ${t.core} | ${Math.round(t.sec)} |`);
  }
  out.push("");
}

process.stdout.write(out.join("\n") + "\n");
