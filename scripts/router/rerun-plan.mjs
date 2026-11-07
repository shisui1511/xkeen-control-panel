#!/usr/bin/env node
// scripts/router/rerun-plan.mjs <отчёт> <цель> --out-dir <каталог> [--core xray|mihomo]
//   [--only <класс,…>] [--max N] [--results <имя.tsv>] — состав перепрогона упавших тестов.
//
// Что делает. Читает строки результатов отчёта, берёт проверки go: и pw: со статусом FAIL или
// XPASS (KNOWN, QFAIL, SKIP и не-тестовые проверки вроде smoke, stand-ready, go:setup,
// pw:run — не берёт) и для каждого ядра, где проверка упала, пишет файл `<каталог>/<ядро>.sel`
// с составом перепрогона. Файл читает run.sh набора rerun (и подтверждающий перепрогон) без eval:
// построчно, по ключу, с проверкой значения по шаблону.
//
// Раскладка отчёта (путь — только под build/router рабочей копии или основной копии):
//   отчёт прогона  <отчёт>/<цель>/<результаты>             (по умолчанию results.tsv);
//   отчёт сессии   <отчёт>/<NN>-<этап>/<цель>/<результаты>  (все этапы по порядку).
// Строка результатов: статус<TAB>ядро<TAB>проверка<TAB>деталь. Имена pw-тестов ищутся в JSON-
// отчётах Playwright ядра: <цель>/<ядро>/{pw*,rerun-N/pw*,harness/pw*}/report*.json (для сессии
// — то же внутри каждого этапа); имя проверки строится так же, как в pw-parse.js.
//
// Формат <ядро>.sel (одна строка — один ключ; значения без перевода строки):
//   GO_SEQ=<каталог пакета>:<TestRouterИмя> …   шаблон [A-Za-z0-9_./: -]*; подтест
//                                               `TestX/sub` сводится к родителю TestX, каталог
//                                               пакета — из core-marks.cjs testFacts;
//   SPECS=tests/router/<спек>.spec.ts …         шаблон [A-Za-z0-9_./ -]*;
//   GREP=<регэксп Playwright>                   (?:…) из экранированных названий (описания
//                                               describe и название теста через пробел),
//                                               без якорей; пусто, если Playwright-тестов нет;
//   COUNT=<число тестов>                        Go (родители) плюс Playwright.
//
// Параметры. --only — оставить только падения заданных классов из `<цель>/classes.tsv`
// (класс<TAB>ядро<TAB>проверка<TAB>деталь; нет файла или у проверки нет класса — фильтр на неё
// не действует); --max N — первые N тестов по порядку строк (потолок подтверждающего
// перепрогона); --core — одно ядро; --results — другой файл результатов (rerun-1.tsv).
// Проверка core-switch (pw:core-switch:<ядро>) в состав не входит: её запускает отдельный этап
// переключения ядра, а не обычный запуск спеков.
//
// Вывод: «ядро: Go N, спеков M» на каждое ядро с .sel и «пропущено: <проверка> (<причина>)».
// Коды: 0 — файлы записаны; 1 — упавших тестов нет (файлы не пишутся); 2 — неверные аргументы.
// Без зависимостей: только встроенные модули Node и core-marks.cjs.

import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import coreMarks from "./core-marks.cjs";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const CORES = ["xray", "mihomo"];
const CLASSES = ["new", "flaky", "known", "xpass", "stand", "quarantine"];

function usage(msg) {
  if (msg) console.error(`rerun-plan: ${msg}`);
  console.error(
    "usage: rerun-plan.mjs <отчёт> <цель> --out-dir <каталог> [--core xray|mihomo] [--only <класс,…>] [--max N] [--results <имя.tsv>]",
  );
  process.exit(2);
}

// --- аргументы ----------------------------------------------------------------------------

const args = process.argv.slice(2);
const pos = [];
const opt = { core: "", only: "", max: 0, results: "results.tsv", outDir: "" };
for (let i = 0; i < args.length; i++) {
  const a = args[i];
  if (a === "--out-dir" || a === "--core" || a === "--only" || a === "--max" || a === "--results") {
    if (i + 1 >= args.length) usage(`${a} без значения`);
    const v = args[++i];
    if (a === "--out-dir") opt.outDir = v;
    else if (a === "--core") opt.core = v;
    else if (a === "--only") opt.only = v;
    else if (a === "--max") opt.max = /^[1-9][0-9]*$/.test(v) ? Number(v) : usage("--max: нужно положительное число");
    else opt.results = v;
  } else if (a.startsWith("--")) {
    usage(`неизвестный параметр ${a}`);
  } else {
    pos.push(a);
  }
}
if (pos.length !== 2) usage("нужны отчёт и цель");
const [reportArg, target] = pos;
if (!/^[a-z0-9-]+$/.test(target)) usage("цель: допустимы a-z, 0-9 и -");
if (!opt.outDir) usage("нужен --out-dir");
if (opt.core && !CORES.includes(opt.core)) usage("--core: xray или mihomo");
if (!/^[a-z0-9-]+\.tsv$/.test(opt.results)) usage("--results: имя вида [a-z0-9-]+.tsv");
const only = opt.only ? new Set(opt.only.split(",")) : null;
if (only) for (const c of only) if (!CLASSES.includes(c)) usage(`--only: неизвестный класс «${c}»`);

// --- путь отчёта: только под build/router рабочей копии или основной копии ------------------

function mainRoot() {
  try {
    const common = execFileSync("git", ["rev-parse", "--path-format=absolute", "--git-common-dir"], {
      cwd: ROOT,
      encoding: "utf8",
    }).trim();
    return path.dirname(common);
  } catch {
    return ROOT;
  }
}

function resolveReport(arg) {
  const abs = path.isAbsolute(arg) ? arg : path.resolve(ROOT, arg);
  const bases = [...new Set([ROOT, mainRoot()])].map((r) => path.join(r, "build", "router"));
  for (const base of bases) {
    const rel = path.relative(base, abs);
    if (!rel || rel.startsWith("..") || path.isAbsolute(rel)) continue;
    const parts = rel.split(path.sep);
    if (parts.some((p) => p === "." || p === ".." || p === "")) continue;
    // служебные имена верхнего уровня — не отчёты
    if (["bin", "release", "unattended", "last", "history.tsv", "history.idx"].includes(parts[0])) continue;
    return abs;
  }
  return null;
}

const report = resolveReport(reportArg);
if (!report) usage("отчёт вне build/router");
if (!fs.existsSync(report) || !fs.statSync(report).isDirectory()) usage("отчёт не найден");

// --- строки результатов: прогон или сессия ---------------------------------------------------

/** Каталоги цели: <отчёт>/<цель> либо <отчёт>/<NN>-<этап>/<цель> всех этапов сессии. */
function targetDirs() {
  const direct = path.join(report, target);
  if (fs.existsSync(path.join(direct, opt.results))) return [direct];
  const stages = fs
    .readdirSync(report, { withFileTypes: true })
    .filter((d) => d.isDirectory() && /^\d+-/.test(d.name))
    .map((d) => d.name)
    .sort();
  const dirs = [];
  for (const s of stages) {
    const dir = path.join(report, s, target);
    if (fs.existsSync(path.join(dir, opt.results))) dirs.push(dir);
  }
  return dirs;
}

const tdirs = targetDirs();
if (tdirs.length === 0) usage(`нет ${opt.results} цели ${target} в отчёте`);

const rows = [];
const classOf = new Map();
for (const dir of tdirs) {
  for (const line of fs.readFileSync(path.join(dir, opt.results), "utf8").split("\n")) {
    const col = line.split("\t");
    if (col.length < 3) continue;
    rows.push({ status: col[0], core: col[1], check: col[2] });
  }
  if (only) {
    try {
      for (const line of fs.readFileSync(path.join(dir, "classes.tsv"), "utf8").split("\n")) {
        const col = line.split("\t");
        if (col.length >= 3) classOf.set(`${col[1]}\t${col[2]}`, col[0]);
      }
    } catch {
      // нет classes.tsv — фильтр по классу не действует
    }
  }
}

// --- индекс Playwright: имя проверки -> файл спека и названия -------------------------------

// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;]*m/g;
// как в pw-parse.js (clean(check, 300)) и branch-report.mjs (cleanName)
function cleanName(text) {
  const flat = String(text || "").replace(ANSI, "").replace(/\s+/g, " ").trim();
  return flat.length > 300 ? flat.slice(0, 300) + "…" : flat;
}

function collectReports(dir, depth, inPw, out) {
  let entries;
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const e of entries) {
    const p = path.join(dir, e.name);
    if (e.isFile() && inPw && /^report.*\.json$/.test(e.name)) out.push(p);
    else if (e.isDirectory() && depth < 3) {
      if (/^pw[\w-]*$/.test(e.name)) collectReports(p, depth + 1, true, out);
      else if (!inPw && /^(rerun-\d+|harness)$/.test(e.name)) collectReports(p, depth + 1, false, out);
    }
  }
}

const pwIndex = new Map(); // ядро -> Map(имя -> { spec, titles })
function pwIndexOf(core) {
  if (pwIndex.has(core)) return pwIndex.get(core);
  const idx = new Map();
  pwIndex.set(core, idx);
  const files = [];
  for (const dir of tdirs) collectReports(path.join(dir, core), 0, false, files);
  for (const f of files) {
    let rep;
    try {
      rep = JSON.parse(fs.readFileSync(f, "utf8"));
    } catch {
      continue;
    }
    const visit = (node, titles, file) => {
      for (const spec of node.specs || []) {
        const all = [...titles, spec.title];
        const key = cleanName(all.join(" > "));
        if (!idx.has(key)) idx.set(key, { spec: path.basename(spec.file || file || ""), titles: all });
      }
      for (const child of node.suites || []) visit(child, [...titles, child.title], file);
    };
    for (const top of rep.suites || []) visit(top, top.file === top.title ? [] : [top.title], top.file);
  }
  return idx;
}

// --- Go: пакет теста по core-marks testFacts ------------------------------------------------

let goPkg = null;
function goPackageOf(name) {
  if (goPkg === null) {
    goPkg = new Map();
    try {
      for (const t of coreMarks.testFacts(ROOT)) {
        if (t.kind === "go" && t.test && t.pkg && !goPkg.has(t.test)) goPkg.set(t.test, t.pkg);
      }
    } catch {
      goPkg = new Map();
    }
  }
  return goPkg.get(name) || null;
}

// --- отбор упавших --------------------------------------------------------------------------

const skipped = [];
const seen = new Set();
const picked = []; // { core, kind, key, pkg, spec, titles }
for (const r of rows) {
  if (r.status !== "FAIL" && r.status !== "XPASS") continue;
  if (!/^(go|pw):/.test(r.check)) continue;
  if (!CORES.includes(r.core)) {
    skipped.push(`${r.check} (ядро «${r.core}»: перегонять некуда)`);
    continue;
  }
  if (opt.core && r.core !== opt.core) continue;
  if (only) {
    const cls = classOf.get(`${r.core}\t${r.check}`);
    if (cls && !only.has(cls)) continue;
  }
  if (r.check.startsWith("go:")) {
    const name = r.check.slice(3).split("/")[0];
    if (!/^TestRouter[A-Za-z0-9_]+$/.test(name)) {
      skipped.push(`${r.check} (не тест TestRouter*)`);
      continue;
    }
    const id = `${r.core}\tgo\t${name}`;
    if (seen.has(id)) continue;
    const pkg = goPackageOf(name);
    if (!pkg || !/^[A-Za-z0-9_./-]+$/.test(pkg)) {
      skipped.push(`${r.check} (пакет теста не найден в core-marks)`);
      continue;
    }
    seen.add(id);
    picked.push({ core: r.core, kind: "go", key: name, pkg });
  } else {
    const name = r.check.slice(3);
    if (/^core-switch:/.test(name)) {
      skipped.push(`${r.check} (переключение ядра: отдельный этап прогона)`);
      continue;
    }
    const id = `${r.core}\tpw\t${name}`;
    if (seen.has(id)) continue;
    const hit = pwIndexOf(r.core).get(name);
    if (!hit || !/^[A-Za-z0-9_.-]+\.spec\.ts$/.test(hit.spec)) {
      skipped.push(`${r.check} (теста нет в JSON-отчёте Playwright ядра)`);
      continue;
    }
    seen.add(id);
    picked.push({ core: r.core, kind: "pw", key: name, spec: hit.spec, titles: hit.titles });
  }
}
const chosen = opt.max > 0 ? picked.slice(0, opt.max) : picked;

// --- файлы .sel -----------------------------------------------------------------------------

function escRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replace(/[\r\n]+/g, "\\s+");
}

for (const s of skipped) console.log(`пропущено: ${s}`);
if (chosen.length === 0) {
  console.log("упавших тестов для перепрогона нет");
  process.exit(1);
}

fs.mkdirSync(opt.outDir, { recursive: true });
for (const core of CORES) {
  const mine = chosen.filter((t) => t.core === core);
  if (mine.length === 0) continue;
  const go = mine.filter((t) => t.kind === "go");
  const pw = mine.filter((t) => t.kind === "pw");
  const specs = [...new Set(pw.map((t) => `tests/router/${t.spec}`))];
  const grep = pw.length > 0 ? `(?:${pw.map((t) => escRe(t.titles.join(" "))).join("|")})` : "";
  const body = [
    `GO_SEQ=${go.map((t) => `${t.pkg}:${t.key}`).join(" ")}`,
    `SPECS=${specs.join(" ")}`,
    `GREP=${grep}`,
    `COUNT=${mine.length}`,
    "",
  ].join("\n");
  fs.writeFileSync(path.join(opt.outDir, `${core}.sel`), body);
  console.log(`${core}: Go ${go.length}, спеков ${specs.length}`);
}
