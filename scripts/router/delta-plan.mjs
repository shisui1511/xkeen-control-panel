#!/usr/bin/env node
// scripts/router/delta-plan.mjs — база и состав добора stand/<arch>-full на новый тег
// (make router-test SUITE=delta, RTR-11, D-24, D-25).
//
//   delta-plan.mjs base --release <тег> [--base-report <имя|путь>]
//   delta-plan.mjs merge --release <тег> --base-report <путь> --target <цель> --out-dir <каталог>
//        --select-env <файл> --b1 <ядро> --b2 <ядро> [--extra-go-seq <…>] [--extra-specs <…>]
//        [--extra-grep <…>]
//
// base. Находит отчёт полного прогона, от которого считается добор. Условия (todo 11, D-24):
//   - отчёт — suite=full с релизом A (не этап сессии, не прерванный) в истории build/router либо
//     заданный --base-report (имя каталога или путь под build/router; тот же набор условий);
//   - тег A отличается от нового тега B, коммит A — предок коммита B (та же ветка выпусков);
//   - A новее новейшего stable-тега (vX.Y.Z), достижимого из B: stable-тег — предок A и не равен
//     ему (A и B — одна веха);
//   - база полная: dirty=false и narrow=none в meta (состав не сужен RT_CORES, RT_SEL_DIR, RT_GO_*,
//     RT_SPECS*, RT_PW_GREP*), тесты обоих ядер у каждой цели, пустой notrun.txt, отправленный статус
//     stand/<arch>-full по каждой цели;
//   - роутерные тесты (*_test.go, frontend/tests/router/, internal/routertest/) не менялись между
//     коммитом прогона базы и HEAD вне диффа A..B: иначе такой тест не шёл бы ни в базе, ни в доборе.
// Без --base-report берётся самый свежий подходящий отчёт. Вывод: «<абсолютный путь отчёта>
// <TAB><тег A><TAB><sha коммита A>». Код 0 — база найдена, 1 — подходящей базы нет (причины по
// каждому кандидату — в stderr), 2 — неверные аргументы.
//
// merge. Составляет файлы <каталог>/<ядро>.sel (формат — rerun-plan.mjs; ключ CORE_ONLY=1 — вторая
// ветвь гоняет только тесты с меткой ядра) для одной цели из двух частей:
//   (а) упавшие в базе — rerun-plan.mjs <база> <цель> --only new,flaky,xpass,stand (known и
//       карантин в состав не входят, как и в условии «упавшие, кроме known»);
//   (б) выбор select-changed.mjs по диффу между тегами (13 строк КЛЮЧ='значение' в --select-env;
//       оболочка проверила их по шаблонам до записи файла): первая ветвь (--b1) — RT_GO_SEQ (или
//       все тесты пакетов RT_GO_PKGS при откате), RT_SPECS; вторая (--b2) — только тесты с меткой
//       ядра: RT_GO_CORE_RUN (или все тесты RT_GO_CORE_PKGS), RT_SPECS_CORE с CORE_ONLY=1.
//       Тесты обвязки (--extra-*, правка scripts/router между тегами) входят в обе ветви.
// Объединение только расширяет состав: спек в обоих источниках гоняется целиком, если у одного
// из них нет сужения --grep. Ветвь первого ядра пишется всегда (в ней идёт смоук нового тега даже
// при пустом составе); вторая — только при наличии в ней тестов.
// Вывод (TSV): base-failed<TAB>N, diff-go<TAB>N, diff-specs<TAB>N, skipped<TAB>«…» (упавший в базе
// тест, который перегнать нельзя; core-switch не считается: его гоняет этап переключения ядра),
// pw-invert<TAB>значение --grep-invert для этапа Playwright (пусто — отсев @full яруса снят: упавший в
// базе тест или тест обвязки мог быть яруса full),
// cores<TAB>ядра с файлами. Коды: 0 — составлено, 1 — ошибка, 2 — неверные аргументы.
// Без зависимостей: встроенные модули Node, history.mjs, core-marks.cjs, rerun-plan.mjs.

import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import coreMarks from "./core-marks.cjs";
import { readHistory, historyPaths } from "./history.mjs";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(SCRIPT_DIR, "..", "..");
const CORES = ["xray", "mihomo"];
const TAG_RE = /^v\d+\.\d+\.\d+(-[a-z]+\.\d+)?$/;
const STABLE_RE = /^v\d+\.\d+\.\d+$/;

function usage(msg) {
  if (msg) console.error(`delta-plan: ${msg}`);
  console.error(
    "usage: delta-plan.mjs base --release <тег> [--base-report <отчёт>]\n" +
      "       delta-plan.mjs merge --release <тег> --base-report <путь> --target <цель> --out-dir <каталог> --select-env <файл> --b1 <ядро> --b2 <ядро> [--extra-go-seq <…>] [--extra-specs <…>] [--extra-grep <…>]",
  );
  process.exit(2);
}

function git(args) {
  return execFileSync("git", args, { cwd: ROOT, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
}
function gitOk(args) {
  try {
    execFileSync("git", args, { cwd: ROOT, stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}
function tagCommit(tag) {
  if (!TAG_RE.test(tag)) return null;
  try {
    return git(["rev-parse", "-q", "--verify", `refs/tags/${tag}^{commit}`]);
  } catch {
    return null;
  }
}
function semverKey(tag) {
  return tag.replace(/^v/, "").split("-")[0].split(".").map(Number);
}
function newestStableMerged(sha) {
  let out = "";
  try {
    out = git(["tag", "--merged", sha, "--list", "v*"]);
  } catch {
    return null;
  }
  // stable-тег на самом коммите B (B — сам stable) вехе не предшествует
  const tags = out.split("\n").filter((t) => STABLE_RE.test(t) && tagCommit(t) !== sha);
  tags.sort((a, b) => {
    const x = semverKey(a);
    const y = semverKey(b);
    for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] - y[i];
    return 0;
  });
  return tags.length ? tags[tags.length - 1] : null;
}

// --- аргументы ----------------------------------------------------------------------------

const argv = process.argv.slice(2);
const cmd = argv[0];
if (cmd !== "base" && cmd !== "merge") usage(cmd ? `неизвестная команда: ${cmd}` : "нет команды");
const opt = {};
const NAMES = [
  "release", "base-report", "target", "out-dir", "select-env", "b1", "b2",
  "extra-go-seq", "extra-specs", "extra-grep",
];
for (let i = 1; i < argv.length; i++) {
  const a = argv[i];
  if (!a.startsWith("--") || !NAMES.includes(a.slice(2))) usage(`неизвестный параметр ${a}`);
  if (i + 1 >= argv.length) usage(`${a} без значения`);
  opt[a.slice(2)] = argv[++i];
}
if (!opt.release || !TAG_RE.test(opt.release)) usage("--release: ожидается тег vX.Y.Z или vX.Y.Z-<суффикс>.<N>");

// --- пути отчётов -------------------------------------------------------------------------

function reportRoots() {
  const roots = [path.join(ROOT, "build", "router")];
  try {
    roots.push(historyPaths().dir);
  } catch {
    // истории нет — только рабочая копия
  }
  return [...new Set(roots)];
}

function resolveReport(arg) {
  const roots = reportRoots();
  const cands = [];
  if (!arg.includes("/")) for (const r of roots) cands.push(path.join(r, arg));
  else cands.push(path.isAbsolute(arg) ? arg : path.resolve(ROOT, arg));
  for (const abs of cands) {
    for (const base of roots) {
      const rel = path.relative(base, abs);
      if (!rel || rel.startsWith("..") || path.isAbsolute(rel)) continue;
      const parts = rel.split(path.sep);
      if (parts.some((p) => p === "." || p === ".." || p === "")) continue;
      if (["bin", "release", "unattended", "last", "session-last", "history.tsv", "history.idx"].includes(parts[0])) continue;
      if (fs.existsSync(abs) && fs.statSync(abs).isDirectory()) return abs;
    }
  }
  return null;
}

function readText(file) {
  try {
    return fs.readFileSync(file, "utf8");
  } catch {
    return null;
  }
}

/** Набор, релиз и признак прерывания из summary.md (иначе report.md). */
function reportInfo(dir) {
  const text = readText(path.join(dir, "summary.md")) ?? readText(path.join(dir, "report.md"));
  if (text === null) return null;
  const pick = (re) => (text.match(re) || [])[1] || "";
  return {
    suite: pick(/^- Набор: (\S+)/m),
    release: pick(/^- Релиз: (\S+)/m),
    interrupted: /^- Прерван: да/m.test(text),
  };
}

// --- base ---------------------------------------------------------------------------------

function gitLines(args) {
  try {
    return git(args).split("\n").filter(Boolean);
  } catch {
    return null;
  }
}

/** Роутерные тесты и их помощники: Go-тесты, спеки и lib/ Playwright, пакет routertest. */
const isTestFile = (f) => /_test\.go$/.test(f) || f.startsWith("frontend/tests/router/") || f.startsWith("internal/routertest/");

/**
 * Полнота базы: в добор идут только упавшие в базе и выбор по диффу, поэтому тест, не шедший в базе,
 * не проверяется ни там, ни здесь. База годится, только если сама опубликовала бы stand/<arch>-full:
 * чистое дерево, состав без сужения окружением (narrow=none в meta пишет run.sh), обе ветви матрицы
 * ядер, пустой notrun.txt и отправленный статус полного прогона (его publish_release_status пишет
 * лишь после своих проверок: обрыв без догона, сбой деплоя). Пустая строка — годится.
 */
function completenessProblem(dir) {
  const meta = readText(path.join(dir, "meta")) || "";
  const md = readText(path.join(dir, "report.md")) || "";
  if (!/^dirty=false\s*$/m.test(meta) || /\(незакоммиченные правки: true\)/.test(md)) {
    return "база с незакоммиченными правками (или в meta нет dirty=false)";
  }
  if (!/^narrow=none\s*$/m.test(meta)) {
    return "состав базы мог быть сужен окружением (RT_CORES, RT_SEL_DIR, RT_GO_*, RT_SPECS*, RT_PW_GREP*): в meta нет narrow=none";
  }
  if (/отладка RT_CORES/.test(readText(path.join(dir, "plan.txt")) || "")) return "база шла не полной матрицей ядер (RT_CORES)";
  const targets = fs
    .readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isDirectory() && fs.existsSync(path.join(dir, e.name, "results.tsv")))
    .map((e) => e.name);
  if (!targets.length) return "в базе нет результатов целей";
  const sum = readText(path.join(dir, "summary.md")) || "";
  if (/^- Статус на GitHub: не отправлен/m.test(sum)) return "база не публиковала статус полного прогона (обрыв связи, notrun, сбой деплоя)";
  const sent = (sum.match(/^- Статус на GitHub: stand\/\S+-full (?:success|failure)\s*$/gm) || []).length;
  if (sent < targets.length) return "база не публиковала статус полного прогона по каждой цели";
  for (const t of targets) {
    const nr = path.join(dir, t, "notrun.txt");
    if (fs.existsSync(nr) && fs.statSync(nr).size > 0) return `в базе не выполнена часть набора цели ${t} (notrun.txt)`;
    const cores = new Set();
    for (const line of (readText(path.join(dir, t, "results.tsv")) || "").split("\n")) {
      const f = line.split("\t");
      if (f.length >= 3 && CORES.includes(f[1]) && /^(go|pw):/.test(f[2])) cores.add(f[1]);
    }
    for (const c of CORES) if (!cores.has(c)) return `в базе цели ${t} нет тестов ядра ${c}: матрица неполная`;
  }
  return "";
}

/**
 * Тесты берутся из HEAD, а выбор по диффу считается по A..B: тест, изменённый (добавленный) между
 * коммитом прогона базы (sha= в meta) и HEAD вне диффа A..B, не шёл ни в базе, ни в доборе. Такой
 * добор не составляется: нужен полный прогон. Пустая строка — таких тестов нет.
 */
function lateTestsProblem(dir, aTag, aSha, bTag, bSha) {
  const meta = readText(path.join(dir, "meta")) || "";
  const baseSha = (meta.match(/^sha=([0-9a-f]{40})\s*$/m) || [])[1];
  if (!baseSha) return "в meta базы нет sha";
  if (!gitOk(["cat-file", "-e", `${baseSha}^{commit}`])) return `коммита прогона базы ${baseSha.slice(0, 8)} нет в локальном git`;
  const ab = gitLines(["diff", "--no-renames", "--name-only", aSha, bSha]);
  const late = gitLines(["diff", "--no-renames", "--name-only", "--diff-filter=d", baseSha, "HEAD"]);
  if (!ab || !late) return "дифф тестов после базы не посчитан (git diff)";
  const abSet = new Set(ab);
  const out = late.filter((f) => isTestFile(f) && !abSet.has(f));
  if (!out.length) return "";
  return `тесты HEAD изменены после прогона базы вне диффа ${aTag}..${bTag}: ${out.slice(0, 3).join(", ")}${out.length > 3 ? ` и ещё ${out.length - 3}` : ""}`;
}

/** Пустая строка — отчёт годится в базу добора тега B, иначе причина отказа. */
function baseProblem(bTag, dir, aTagArg) {
  const info = reportInfo(dir);
  if (!info) return "в отчёте нет summary.md и report.md";
  if (info.suite !== "full") return `набор отчёта «${info.suite || "-"}», нужен full`;
  if (info.interrupted) return "отчёт прерван";
  const why = completenessProblem(dir);
  if (why) return why;
  const aTag = aTagArg || info.release;
  if (!aTag || !TAG_RE.test(aTag)) return "у отчёта нет тега релиза";
  if (aTag === bTag) return "тег базы совпадает с новым тегом";
  const aSha = tagCommit(aTag);
  const bSha = tagCommit(bTag);
  if (!aSha) return `тега ${aTag} нет в локальном git`;
  if (!bSha) return `тега ${bTag} нет в локальном git`;
  if (aSha === bSha) return "тег базы и новый тег указывают на один коммит";
  if (!gitOk(["merge-base", "--is-ancestor", aSha, bSha])) return `коммит ${aTag} не предок ${bTag}`;
  const stable = newestStableMerged(bSha);
  if (stable) {
    const sSha = tagCommit(stable);
    if (sSha === aSha) return `база — stable-тег ${stable}: нужен пре-релиз после него`;
    if (!gitOk(["merge-base", "--is-ancestor", sSha, aSha])) return `${aTag} старше последнего stable ${stable}: другая веха`;
  }
  return lateTestsProblem(dir, aTag, aSha, bTag, bSha);
}

if (cmd === "base") {
  const bTag = opt.release;
  if (!tagCommit(bTag)) {
    console.error(`delta-plan: тега ${bTag} нет в локальном git`);
    process.exit(1);
  }
  const reasons = [];
  if (opt["base-report"]) {
    const dir = resolveReport(opt["base-report"]);
    if (!dir) {
      console.error("delta-plan: BASE_REPORT: отчёт не найден под build/router");
      process.exit(1);
    }
    const why = baseProblem(bTag, dir, "");
    if (why) {
      console.error(`delta-plan: BASE_REPORT не годится в базу добора: ${why}`);
      process.exit(1);
    }
    const aTag = reportInfo(dir).release;
    console.log([dir, aTag, tagCommit(aTag)].join("\t"));
    process.exit(0);
  }
  const byReport = new Map();
  for (const r of readHistory()) {
    if (r.suite !== "full" || r.report.includes("/") || r.release === "-") continue;
    const v = byReport.get(r.report) || { report: r.report, release: r.release, finished: r.finished_utc };
    if (r.finished_utc > v.finished) v.finished = r.finished_utc;
    byReport.set(r.report, v);
  }
  const cands = [...byReport.values()].sort((a, b) => (a.finished < b.finished ? 1 : a.finished > b.finished ? -1 : a.report < b.report ? 1 : -1));
  for (const c of cands) {
    const dir = resolveReport(c.report);
    if (!dir) {
      reasons.push(`${c.report} (${c.release}): каталога отчёта нет (ротация)`);
      continue;
    }
    const why = baseProblem(bTag, dir, c.release);
    if (!why) {
      console.log([dir, c.release, tagCommit(c.release)].join("\t"));
      process.exit(0);
    }
    reasons.push(`${c.report} (${c.release}): ${why}`);
  }
  console.error(`delta-plan: подходящего полного прогона по более старому тегу этой вехи нет (просмотрено ${cands.length})`);
  for (const r of reasons.slice(0, 8)) console.error(`  - ${r}`);
  process.exit(1);
}

// --- merge --------------------------------------------------------------------------------

for (const k of ["base-report", "target", "out-dir", "select-env", "b1", "b2"]) if (!opt[k]) usage(`merge: нужен --${k}`);
if (!/^[a-z0-9-]+$/.test(opt.target)) usage("--target: допустимы a-z, 0-9 и -");
if (!CORES.includes(opt.b1) || !CORES.includes(opt.b2) || opt.b1 === opt.b2) usage("--b1 и --b2: разные ядра xray и mihomo");

const GO_SEQ_RE = /^[A-Za-z0-9_./-]+:TestRouter[A-Za-z0-9_]+$/;
const SPEC_RE = /^tests\/router\/[A-Za-z0-9_.-]+\.spec\.ts$/;

const baseDir = resolveReport(opt["base-report"]);
if (!baseDir) {
  console.error("delta-plan: база не найдена под build/router");
  process.exit(1);
}

/** Выбор select-changed.mjs: строки КЛЮЧ='значение' (одинарные кавычки) или КЛЮЧ=0|1. */
const sel = {};
for (const line of (readText(opt["select-env"]) || "").split("\n")) {
  const m = line.match(/^(RT_[A-Z_]+)=(?:'([^']*)'|([01]))$/);
  if (m) sel[m[1]] = m[2] ?? m[3];
}
for (const k of ["RT_GO_PKGS", "RT_SPECS", "RT_PW_GREP", "RT_GO_CORE_PKGS", "RT_GO_CORE_RUN", "RT_SPECS_CORE", "RT_GO_SEQ", "RT_PW_INVERT"]) {
  if (sel[k] === undefined) {
    console.error(`delta-plan: в ${opt["select-env"]} нет ${k}`);
    process.exit(1);
  }
}
// те же алфавиты, что проверяет run.sh у набора changed (T-148.1-23): значения попадают в файл состава
// и в окружение этапов
const SAFE = {
  RT_GO_PKGS: /^[A-Za-z0-9_./ -]*$/,
  RT_SPECS: /^[A-Za-z0-9_./ -]*$/,
  RT_PW_GREP: /^[A-Za-z0-9_|()?!^*.: -]*$/,
  RT_GO_CORE_PKGS: /^[A-Za-z0-9_./ -]*$/,
  RT_GO_CORE_RUN: /^[A-Za-z0-9_|^$()]*$/,
  RT_SPECS_CORE: /^[A-Za-z0-9_./ -]*$/,
  RT_GO_SEQ: /^[A-Za-z0-9_./: -]*$/,
  RT_PW_INVERT: /^[A-Za-z0-9_@|()?!^*.: -]*$/,
};
for (const [k, re] of Object.entries(SAFE)) {
  if (!re.test(sel[k])) {
    console.error(`delta-plan: ${k}: недопустимые символы`);
    process.exit(1);
  }
}
const words = (s) => (s || "").split(/\s+/).filter(Boolean);

// (а) упавшие в базе
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "delta-plan-"));
const baseSel = {};
const skipped = [];
let baseFailed = 0;
{
  let out = "";
  let rc = 0;
  try {
    out = execFileSync(
      process.execPath,
      [path.join(SCRIPT_DIR, "rerun-plan.mjs"), baseDir, opt.target, "--out-dir", tmp, "--only", "new,flaky,xpass,stand"],
      { cwd: ROOT, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
    );
  } catch (e) {
    rc = e.status ?? 2;
    out = String(e.stdout || "");
    if (rc !== 1) {
      console.error(`delta-plan: rerun-plan.mjs не разобрал базу (код ${rc}): ${String(e.stderr || "").trim().split("\n").pop()}`);
      fs.rmSync(tmp, { recursive: true, force: true });
      process.exit(1);
    }
  }
  for (const line of out.split("\n")) {
    const m = line.match(/^пропущено: (.*)$/);
    if (!m) continue;
    const mc = m[1].match(/^pw:core-switch:(xray|mihomo)\b/);
    if (mc) baseSel[`switch-${mc[1]}`] = true;
    else skipped.push(m[1].replace(/\s+/g, " ").slice(0, 200));
  }
  for (const core of CORES) {
    const text = readText(path.join(tmp, `${core}.sel`));
    if (text === null) continue;
    const val = (k) => (text.match(new RegExp(`^${k}=(.*)$`, "m")) || [])[1] ?? "";
    baseSel[core] = { goSeq: words(val("GO_SEQ")), specs: words(val("SPECS")), grep: val("GREP"), count: Number(val("COUNT")) || 0 };
    baseFailed += baseSel[core].count;
  }
  fs.rmSync(tmp, { recursive: true, force: true });
}

// (б) выбор по диффу: имена Go-тестов в записи «каталог:Имя»
const facts = coreMarks.testFacts(ROOT).filter((t) => t.kind === "go" && t.test && t.pkg);
const pkgDir = (p) => p.replace(/^\.\//, "");
function goByPkgs(pkgs) {
  const set = new Set(words(pkgs).map(pkgDir));
  return facts.filter((t) => set.has(t.pkg)).map((t) => `${t.pkg}:${t.test}`);
}
function goByNames(runExpr, pkgs) {
  const names = new Set(runExpr.match(/TestRouter[A-Za-z0-9_]+/g) || []);
  const set = pkgs ? new Set(words(pkgs).map(pkgDir)) : null;
  return facts.filter((t) => names.has(t.test) && (!set || set.has(t.pkg))).map((t) => `${t.pkg}:${t.test}`);
}
const selB1Go = sel.RT_GO_SEQ ? words(sel.RT_GO_SEQ) : goByPkgs(sel.RT_GO_PKGS);
const selB2Go = sel.RT_GO_CORE_RUN ? goByNames(sel.RT_GO_CORE_RUN, sel.RT_GO_CORE_PKGS) : goByPkgs(sel.RT_GO_CORE_PKGS);
const selSpec = { [opt.b1]: words(sel.RT_SPECS), [opt.b2]: words(sel.RT_SPECS_CORE) };
const selGo = { [opt.b1]: selB1Go, [opt.b2]: selB2Go };
const extraGo = words(opt["extra-go-seq"]);
const extraSpecs = words(opt["extra-specs"]);
const extraGrep = opt["extra-grep"] || "";
if (!SAFE.RT_PW_GREP.test(extraGrep)) {
  console.error("delta-plan: --extra-grep: недопустимые символы");
  process.exit(1);
}

for (const t of [...Object.values(selGo).flat(), ...extraGo]) {
  if (!GO_SEQ_RE.test(t)) {
    console.error(`delta-plan: недопустимая запись Go-теста «${t}»`);
    process.exit(1);
  }
}
for (const s of [...Object.values(selSpec).flat(), ...extraSpecs]) {
  if (!SPEC_RE.test(s)) {
    console.error(`delta-plan: недопустимый путь спека «${s}»`);
    process.exit(1);
  }
}

const uniq = (a) => [...new Set(a)];
fs.mkdirSync(opt["out-dir"], { recursive: true });
const written = [];
let diffGo = 0;
let diffSpecs = 0;
let invertDrop = false;
for (const core of [opt.b1, opt.b2]) {
  const b = baseSel[core] || { goSeq: [], specs: [], grep: "", count: 0 };
  const go = uniq([...b.goSeq, ...selGo[core], ...extraGo]);
  const specs = uniq([...b.specs, ...selSpec[core], ...extraSpecs]);
  // сужение --grep: каждая часть (упавшие базы, выбор, обвязка) сужает свои спеки; нет сужения хотя
  // бы у одной части — спеки идут целиком (только расширение)
  const parts = [];
  let narrow = true;
  if (b.specs.length) {
    if (b.grep) parts.push(b.grep);
    else narrow = false;
  }
  if (selSpec[core].length) {
    if (sel.RT_PW_GREP) parts.push(sel.RT_PW_GREP);
    else narrow = false;
  }
  if (extraSpecs.length) {
    if (extraGrep) parts.push(extraGrep);
    else narrow = false;
  }
  const uparts = uniq(parts);
  const grep = specs.length && narrow && uparts.length ? (uparts.length === 1 ? uparts[0] : uparts.map((p) => `(?:${p})`).join("|")) : "";
  // вторая ветвь без упавших и без обвязки гоняет только тесты с меткой ядра
  const coreOnly = core === opt.b2 && specs.length > 0 && !b.specs.length && !extraSpecs.length;
  if (b.specs.length || extraSpecs.length) invertDrop = true;
  if (core === opt.b2 && !go.length && !specs.length && !baseSel[`switch-${core}`]) continue;
  const body = [
    `GO_SEQ=${go.join(" ")}`,
    `SPECS=${specs.join(" ")}`,
    `GREP=${grep}`,
    `COUNT=${go.length + specs.length}`,
    ...(coreOnly ? ["CORE_ONLY=1"] : []),
    "",
  ].join("\n");
  fs.writeFileSync(path.join(opt["out-dir"], `${core}.sel`), body);
  written.push(core);
  diffGo += selGo[core].length;
  diffSpecs += selSpec[core].length;
}
if (baseSel["switch-" + opt.b1] || baseSel["switch-" + opt.b2]) {
  // упавшее переключение ядра проверяет этап ветви: у ядра должна быть ветвь
  for (const core of CORES) {
    if (baseSel[`switch-${core}`] && !written.includes(core)) {
      fs.writeFileSync(path.join(opt["out-dir"], `${core}.sel`), "GO_SEQ=\nSPECS=\nGREP=\nCOUNT=0\n");
      written.push(core);
    }
  }
}

console.log(`base-failed\t${baseFailed}`);
console.log(`diff-go\t${diffGo}`);
console.log(`diff-specs\t${diffSpecs}`);
for (const s of skipped) console.log(`skipped\t${s}`);
console.log(`pw-invert\t${invertDrop ? "" : sel.RT_PW_INVERT}`);
console.log(`cores\t${written.join(" ")}`);
