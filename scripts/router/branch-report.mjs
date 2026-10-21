#!/usr/bin/env node
// scripts/router/branch-report.mjs <отчёт> <цель> --suite <имя> [--write] [--plan <plan.txt>]
// — разделы сводки прогона.
//
// Читает отчёт и печатает в stdout Markdown, который run.sh кладёт в summary.md после строки цели:
//   - строки плана ветвей из --plan (ядро ветви, вторая ветвь, окно смоука) — в самом начале;
//   - «## Этапы по ветвям» — таблица «этап × ветвь» (секунды) из timings.tsv;
//   - «- Итого: N с; критический путь: …» — от первого начала до последнего конца этапов;
//   - «- Оболочка: N с (тесты M с)» — итог без объединения интервалов этапов тестов;
//   - «- Строк времени без начала или ядра: N» — строки timings.tsv короче шести колонок;
//   - «- Восстановление: N с, путь: …» — этап restore и разбивка расхождений из restore.log
//     (D-15: быстрый путь без перезапуска ядра или перезапуск);
//   - «## 10 самых долгих тестов» — по Go (go-*.jsonl) и Playwright (pw*/report*.json);
//   - «## Нужна метка ядра» (только --suite full) — страховка D-05б: проверка go:/pw: без метки
//     ядра, чей исход в ветвях xray и mihomo различается как класс (PASS / SKIP / KNOWN /
//     FAIL и XPASS), либо «- нет».
// Формат timings.tsv: <цель> <этап> <секунды> <код> <начало> <ядро ветви или ->. У старых
// отчётов колонок пять или четыре: ядро берётся из суффикса имени этапа, иначе — по
// интервалу ветви.
// Метка ядра: Go — core-marks.cjs (goMarks по текущему дереву; подтесты наследуют метку
// родителя), Playwright — теги @core и @core-label спека в JSON-отчёте (<цель>/<ядро>/pw*/).
//
// Без --write скрипт только читает отчёт. С --write он дописывает в <цель>/results.tsv строку
//   FAIL<TAB>-<TAB>core-mark:<проверка><TAB>нужна метка ядра: xray <статус>, mihomo <статус>
// на каждую такую проверку (повторный запуск не дублирует строки) и пишет <цель>/layout.tsv:
// «проверка<TAB>ядра через пробел<TAB>причина» (метка ядра | метка падения с ядром | ротация —
// ядро из SHA | полная матрица | отладка RT_CORES; причина берётся по plan.txt).
// В вывод попадают лишь имена этапов и тестов, ядра, статусы и секунды: деталь результатов,
// адреса и тексты ошибок не выводятся.
// Без зависимостей: только встроенные модули Node и core-marks.cjs.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import coreMarks from "./core-marks.cjs";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");

const argv = process.argv.slice(2);
const pos = [];
let suite = "";
let write = false;
let planFile = "";
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === "--suite") suite = argv[++i] || "";
  else if (argv[i] === "--write") write = true;
  else if (argv[i] === "--plan") planFile = argv[++i] || "";
  else pos.push(argv[i]);
}
if (pos.length !== 2) {
  console.error("usage: branch-report.mjs <отчёт> <цель> --suite <имя> [--write] [--plan <plan.txt>]");
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

// Строки плана ветвей (plan.txt run.sh): печатаются первыми.
let planLines = [];
if (planFile) {
  try {
    planLines = fs
      .readFileSync(planFile, "utf8")
      .split("\n")
      .filter((l) => l.trim() !== "");
  } catch {
    planLines = [];
  }
}

let raw = "";
try {
  raw = fs.readFileSync(path.join(report, "timings.tsv"), "utf8");
} catch {
  raw = "";
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

const haveStages = stages.length > 0;

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

// --- восстановление стенда (D-15) --------------------------------------------------
// Секунды этапа restore из timings.tsv и разбивка расхождений из restore.log
// (snapshot.sh: «restore: расхождений N (панель P, ядро C, изменчивые ядра V) …» и строка
// пути: «ядро не перезапускаю / не трогаю / не перезапускалось» — быстрый путь).

function recoveryLines() {
  if (suite !== "full" && suite !== "changed") return [];
  let log = "";
  try {
    log = fs.readFileSync(path.join(report, target, "restore.log"), "utf8");
  } catch {
    return ["- Восстановление: не выполнялось"];
  }
  let secs = "-";
  for (const line of raw.split("\n")) {
    const f = line.split("\t");
    if (f[0] === target && f[1] === "restore" && Number.isFinite(Number(f[2]))) secs = f[2];
  }
  const fast = /ядро не (?:перезапуска|трогаю)/.test(log);
  const route = fast ? "быстрый (ядро не перезапускалось)" : "с перезапуском ядра";
  let diffs = "расхождений нет данных";
  const full = log.match(/restore: расхождений (\d+) \(панель (\d+), ядро (\d+), изменчивые ядра (\d+)\)/);
  const old = log.match(/restore: расхождений(?: файлов)? (\d+)/);
  if (full) diffs = `расхождений ${full[1]} (панель ${full[2]}, ядро ${full[3]}, изменчивые ядра ${full[4]})`;
  else if (old) diffs = `расхождений ${old[1]}`;
  return [`- Восстановление: ${secs} с, путь: ${route}; ${diffs}`];
}

const out = [...planLines];

if (haveStages) {
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
  out.push(...recoveryLines());
  out.push("");
} else if (recoveryLines().length > 0) {
  out.push("", ...recoveryLines(), "");
}

// --- 10 самых долгих тестов --------------------------------------------------------

const longest = [];
// теги спеков Playwright по имени проверки (как в pw-parse.js, без «pw:»): `core`, `core-label`
const pwTags = new Map();
// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;]*m/g;
function cleanName(text) {
  const flat = String(text || "")
    .replace(ANSI, "")
    .replace(/\s+/g, " ")
    .trim();
  return flat.length > 300 ? flat.slice(0, 300) + "…" : flat;
}
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
          const tagKey = cleanName(name);
          if (!pwTags.has(tagKey)) pwTags.set(tagKey, new Set());
          for (const tag of spec.tags || []) pwTags.get(tagKey).add(String(tag).replace(/^@/, ""));
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

// --- метки ядра: страховка D-05б и раскладка тестов по ветвям (D-19) ----------------------

const RESULT_ORDER = ["FAIL", "XPASS", "KNOWN", "SKIP", "PASS"];
const classOf = (st) => (st === "FAIL" || st === "XPASS" ? "FAIL" : st);
const resultsFile = path.join(report, target, "results.tsv");
let resultsRaw = "";
try {
  resultsRaw = fs.readFileSync(resultsFile, "utf8");
} catch {
  resultsRaw = "";
}

// проверка -> ядро -> худший статус
const byCheck = new Map();
const existingMarks = new Set();
for (const line of resultsRaw.split("\n")) {
  if (!line) continue;
  const f = line.split("\t");
  const [st, core, check] = f;
  if (check && check.startsWith("core-mark:")) existingMarks.add(check.slice("core-mark:".length));
  if (!check || !(check.startsWith("go:") || check.startsWith("pw:"))) continue;
  if (core !== "xray" && core !== "mihomo") continue;
  if (!byCheck.has(check)) byCheck.set(check, new Map());
  const perCore = byCheck.get(check);
  const prev = perCore.get(core);
  if (!prev || RESULT_ORDER.indexOf(st) < RESULT_ORDER.indexOf(prev)) perCore.set(core, st);
}

// метка проверки: "" (нет), "core" (метка ядра) или "known-core" (метка падения с ядром)
let goMarkMap = null;
try {
  goMarkMap = new Map();
  for (const m of coreMarks.goMarks(ROOT)) {
    if (!goMarkMap.has(m.test) || m.reason === "core") goMarkMap.set(m.test, m.reason);
  }
} catch {
  goMarkMap = null; // разметка Go не прочитана: Go-проверки в D-05б не участвуют
}
function markOf(check) {
  if (check.startsWith("go:")) {
    if (!goMarkMap) return null;
    return goMarkMap.get(check.slice(3).split("/")[0]) || "";
  }
  const tags = pwTags.get(check.slice(3));
  if (!tags) return "";
  if (tags.has("core")) return "core";
  if (tags.has("core-label")) return "known-core";
  return "";
}

const needMark = [];
const bothBranches = [...byCheck.values()].some((m) => m.has("xray")) && [...byCheck.values()].some((m) => m.has("mihomo"));
if (suite === "full" && bothBranches) {
  for (const [check, perCore] of byCheck) {
    if (!perCore.has("xray") || !perCore.has("mihomo")) continue;
    if (classOf(perCore.get("xray")) === classOf(perCore.get("mihomo"))) continue;
    if (markOf(check) !== "") continue; // размечен или Go-разметка не прочитана
    needMark.push({ check, xray: perCore.get("xray"), mihomo: perCore.get("mihomo") });
  }
}

if (suite === "full") {
  out.push("## Нужна метка ядра", "");
  if (needMark.length === 0) out.push("- нет");
  for (const n of needMark) {
    const name = n.check.replace(/\|/g, "\\|");
    out.push(`- core-mark:${name}: нужна метка ядра: xray ${n.xray}, mihomo ${n.mihomo}`);
  }
  out.push("");
}

if (write) {
  // строки FAIL core-mark:<проверка> в results.tsv (без дублей при повторном запуске)
  const rows = needMark.filter((n) => !existingMarks.has(n.check));
  if (rows.length > 0) {
    const text = rows
      .map((n) => `FAIL\t-\tcore-mark:${n.check}\tнужна метка ядра: xray ${n.xray}, mihomo ${n.mihomo}\n`)
      .join("");
    try {
      fs.appendFileSync(resultsFile, text);
    } catch {
      // отчёта цели нет — писать некуда
    }
  }
  // layout.tsv: проверка, ядра, причина раскладки
  const planText = planLines.join("\n");
  const mode = /отладка RT_CORES/.test(planText) ? "debug" : /^- Ядро ветви:/m.test(planText) ? "hybrid" : "matrix";
  const lay = [];
  for (const [check, perCore] of byCheck) {
    const mark = markOf(check);
    let why = "полная матрица";
    if (mode === "debug") why = "отладка RT_CORES";
    else if (mode === "hybrid") {
      why = mark === "core" ? "метка ядра" : mark === "known-core" ? "метка падения с ядром" : "ротация — ядро из SHA";
    }
    lay.push(`${check}\t${["xray", "mihomo"].filter((c) => perCore.has(c)).join(" ")}\t${why}`);
  }
  if (lay.length > 0) {
    try {
      fs.writeFileSync(path.join(report, target, "layout.tsv"), lay.join("\n") + "\n");
    } catch {
      // каталог цели недоступен
    }
  }
}

process.stdout.write(out.join("\n") + "\n");
