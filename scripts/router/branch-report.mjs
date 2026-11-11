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
//   - «## 10 самых долгих тестов» — по Go (go-*.jsonl) и Playwright (pw*/report*.json), секунды
//     суммируются по нормализованному имени (cleanName); колонка «перезапуски» — смены PID ядра
//     на стенде в окне теста (D-06);
//   - «## Тесты Парето (80% времени Go)» — тесты Go верхнего уровня всех ветвей по убыванию
//     секунд до накопленных 80% суммарного времени Go: строка «- Всего Go: N тестов, M с;
//     Парето: K тестов» и таблица Тест, Ядро, с, доля %, перезапуски;
//   - «## Превышен бюджет gate» (D-15) — тесты верхнего уровня этого прогона дольше бюджета (Go 60 с,
//     Playwright 30 с), кроме ярусов full и тестов с меткой Slow (Go — core-marks.cjs testFacts,
//     Playwright — теги и аннотации JSON-отчёта): таблица Тест, Ядро, с, бюджет с либо «- нет».
//     Только предупреждение: итог прогона не меняется; решение (full, Slow с причиной или todo) —
//     на ship:pre;
//   - «## Нужна метка ядра» (только --suite full) — страховка D-05б: проверка go:/pw: без метки
//     ядра, чей исход в ветвях xray и mihomo различается как класс (PASS / SKIP / KNOWN /
//     FAIL и XPASS), либо «- нет»; проверки с QPASS или QFAIL (карантин) в раздел не входят;
//   - «## Классы падений» (RTR-05, D-19…D-21) — класс каждого падения: new (находка по продукту),
//     flaky (упал в основном проходе и прошёл при подтверждающем перепрогоне, <цель>/confirm.tsv),
//     known (метка известного падения), xpass, stand (потеря связи со стендом), quarantine
//     (карантин). Таблица Класс, Проверка, Ядро, Причина класса, В истории (падений/прогонов за 30
//     дней по history.mjs failFreq; ошибка истории — «-»), затем строка итога по классам:
//       - Итог: зелёный
//       - Итог: красный (новых N, нестабильных N, xpass N, обрывов N)
//       - Итог: красный: нестабильный (нестабильных N)       (одни flaky)
//       - Итог: красный: смена версии стенда (новых N, нестабильных N, xpass N, обрывов N)
//                                                         (строка stop в version-stop.tsv, D-13)
//     Падения без confirm.tsv (старый отчёт, потолок перепрогона, проверка вне go:/pw:) — new.
//     Потеря связи (проверка reachable или признаки в детали: «нет связи», ERR_NETWORK_CHANGED и
//     т. п.) — stand; догнанная проверка (строка в <цель>/catchup.tsv, run.sh) получает класс по
//     результату догона: прошла — stand (итог не краснит), упала не из-за связи — new, снова из-за
//     связи — открытый stand. reachable считается догнанным, если catchup.tsv есть, а notrun.txt
//     пуст. Непустой notrun.txt (связь не вернулась, часть набора не выполнена) без открытых строк
//     stand добавляет строку notrun:связь, итог остаётся красным.
//   - «- Обрывы связи: N, догнано: M» (при N > 0, по <цель>/link-loss.tsv) и «## Не выполнено из-за
//     обрыва связи» (по <цель>/notrun.txt, D-26).
//     Причина класса — короткая фраза без текста ошибки; причина открытого stand (связь не
//     вернулась, догона не было) начинается со слов «не догнан» (run.sh считает их красными).
//   - Смена версии стенда (D-13, RTR-15) по <цель>/version-stop.tsv (stop<TAB><ядро><TAB><граница>
//     <TAB><ожидалась><TAB><на стенде><TAB><эпоха ПК> — первая остановка, suspect<TAB><ядро><TAB>
//     <проверка>, notrun<TAB><ядро><TAB><что не выполнено>) и <цель>/version-guard.tsv (ok, transition, unverified,
//     skip по границам; пишут rt_version_guard из lib.sh, go-suite.sh, pw-suite.sh и run.sh). Класс
//     stand, открытый (run.sh считает его красным), не new и без подтверждающего перепрогона: FAIL
//     version-guard — «смена версии стенда» (сбой самой сверки — отдельная проверка version-guard-error,
//     обычное падение класса new, в подтверждающем перепрогоне — «без сверки версии стенда»); падение из строк suspect того же ядра и xcp-after-restore
//     отчёта со строкой stop — «падение на сменённой версии стенда». В начале «Классы падений» —
//     строка «- Сверка версии стенда: N границ, смен нет» (с принятыми переходами — «N границ,
//     принятых переходов M, смен нет»; при остановке — «остановлено на границе <B>»; без ожидания —
//     «выключена — ожидаемая версия неизвестна»; нет version-guard.tsv и строки stop — строки нет).
//     При строке stop итог — «- Итог: красный: смена версии стенда (новых N, нестабильных N, xpass N,
//     обрывов N)» (обрывы — только потеря связи), за ним раздел «## Остановлено: смена версии
//     стенда»: цель, ядро и граница, ожидалась и на стенде, «- Падения на сменённой версии (класс
//     stand): N», «- Не выполнено:» и пункты из строк notrun. Отчёт без этих файлов — сводка прежняя.
//   - «## Тренд набора» (только --suite full, D-16) — число проверок go:/pw: и время тестов этого
//     отчёта против прошлого полного прогона истории (history.mjs, lastFull строго до начала
//     отчёта из meta) и до 5 самых долгих новых тестов; нет истории — «- Тренд: история
//     недоступна», сбой чтения не роняет сводку.
// Имена тестов: в ячейках таблиц — mdName (cleanName плюс экранирование `\` и `|`), в пунктах
// списков — cleanName без экранирования (текст совпадает с именем проверки в results.tsv).
// cleanName убирает ANSI, сводит пробельные символы к одному пробелу и режет до 300 знаков.
// Перезапуски: <цель>/<ядро>/kernel-pids.tsv («<эпоха устройства>\t<PID через запятую или ->» при
// каждой смене; пишет сэмплер run.sh) и clock.tsv («<эпоха ПК>\t<эпоха устройства>»). Перезапуск —
// строка со значением, отличным от предыдущей, и не «-», попавшая в окно теста на часах ПК. Окно
// Go-теста — начало этапа go-run-<имя> (или go-run-<имя>-<k>) из timings.tsv плюс сумма Elapsed
// предыдущих тестов верхнего уровня файла go-<имя>[-<k>].jsonl; окно Playwright — startTime и
// duration результатов изменяющих частей (pw, pw-2). Нет файлов ветви или окна — «-».
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
// Он же пишет <цель>/classes.tsv: «класс<TAB>ядро<TAB>проверка<TAB>причина» на каждую строку
// FAIL, XPASS, KNOWN, QFAIL и QPASS (читают run.sh, history.mjs, unattended.sh, rerun-plan.mjs);
// повторный запуск перезаписывает файл.
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
const BRANCH = [
  "sync",
  "switch",
  "smoke",
  "observe",
  "pw-read",
  "go",
  "settle",
  "pw",
  "catchup-go",
  "catchup-pw",
  "confirm-switch",
  "confirm-go",
  "confirm-pw",
];
const TEST_STAGES = new Set(["go", "pw-read", "pw", "catchup-go", "catchup-pw", "confirm-go", "confirm-pw"]);
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

// --- 10 самых долгих тестов, перезапуски ядра, тесты Парето ----------------------------

// теги спеков Playwright по имени проверки (как в pw-parse.js, без «pw:»): `core`, `core-label`
const pwTags = new Map();
// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;]*m/g;
// Нормализованное имя теста: без ANSI, пробельные символы (в том числе переводы строки и
// табуляции) — один пробел, не длиннее 300 знаков. Это представление — в пунктах списков.
function cleanName(text) {
  const flat = String(text || "")
    .replace(ANSI, "")
    .replace(/\s+/g, " ")
    .trim();
  return flat.length > 300 ? flat.slice(0, 300) + "…" : flat;
}
// Имя теста для ячейки таблицы Markdown: cleanName плюс экранирование `\` и `|`. Все таблицы
// сводки берут имя только через эту функцию.
function mdName(text) {
  return cleanName(text).replace(/\\/g, "\\\\").replace(/\|/g, "\\|");
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

// Перезапуски ядра ветви (D-06): смены PID из <ядро>/kernel-pids.tsv («<эпоха устройства>\t<PID
// через запятую или ->») в часах ПК — через смещение из clock.tsv («<эпоха ПК>\t<эпоха
// устройства>»). Перезапуск — строка, чьё значение отличается от предыдущей и не равно «-»
// (первая строка — исходное состояние). Нет файлов ветви — null: в таблицах «-».
function kernelRestarts(core) {
  let pids = "";
  let clock = "";
  try {
    pids = fs.readFileSync(path.join(coreDir, core, "kernel-pids.tsv"), "utf8");
    clock = fs.readFileSync(path.join(coreDir, core, "clock.tsv"), "utf8");
  } catch {
    return null;
  }
  const c = clock.split("\n")[0].split("\t").map(Number);
  if (c.length < 2 || !Number.isFinite(c[0]) || !Number.isFinite(c[1])) return null;
  const shift = c[1] - c[0];
  const events = [];
  let prev = null;
  for (const line of pids.split("\n")) {
    if (!line) continue;
    const [t, v] = line.split("\t");
    const dev = Number(t);
    if (!Number.isFinite(dev) || v === undefined) continue;
    if (prev !== null && v !== prev && v !== "-") events.push(dev - shift);
    prev = v;
  }
  return events;
}
// Число перезапусков в окне [начало, конец] (секунды эпохи ПК); нет данных ветви — null.
function restartsIn(events, start, end) {
  if (!events) return null;
  const a = Math.floor(start);
  const b = Math.ceil(end);
  return events.filter((t) => t >= a && t <= b).length;
}

// Начала этапов go-run-<имя> (и go-run-<имя>-<k> режима последовательности) цели по ядру:
// 5-я колонка timings.tsv, часы ПК.
const goRunStart = new Map();
for (const line of raw.split("\n")) {
  if (!line) continue;
  const f = line.split("\t");
  if (f[0] !== target || !f[1] || !f[1].startsWith("go-run-")) continue;
  const start = Number(f[4]);
  if (!Number.isFinite(start)) continue;
  const key = `${f[5] && f[5] !== "-" ? f[5] : ""}\t${f[1]}`;
  if (!goRunStart.has(key)) goRunStart.set(key, start);
}

// Тесты по ключу «ядро\tcleanName»: { name, core, sec, kind (go|pw), restarts (число или null) }.
// Секунды и перезапуски суммируются по нормализованному имени (WR-01).
const testMap = new Map();
function addTest(kind, core, name, sec, restarts, flags = {}) {
  const clean = cleanName(name);
  const key = `${kind}\t${core}\t${clean}`;
  if (!testMap.has(key)) testMap.set(key, { name: clean, core, sec: 0, kind, restarts: null, full: false, slow: false });
  const t = testMap.get(key);
  t.sec += sec;
  if (flags.full) t.full = true;
  if (flags.slow) t.slow = true;
  if (restarts !== null) t.restarts = (t.restarts || 0) + restarts;
}

for (const core of coreDirs) {
  const base = path.join(coreDir, core);
  const events = kernelRestarts(core);
  // Go: событие test2json pass/fail/skip верхнего теста (без «/») несёт Elapsed. Тесты верхнего
  // уровня идут последовательно: начало теста — начало этапа go-run-<имя> плюс сумма Elapsed
  // предыдущих тестов верхнего уровня этого файла.
  for (const f of list(base, /^go-.*\.jsonl$/)) {
    let text = "";
    try {
      text = fs.readFileSync(path.join(base, f), "utf8");
    } catch {
      continue;
    }
    const stage = `go-run-${f.slice(3, -".jsonl".length)}`;
    const stageStart = goRunStart.has(`${core}\t${stage}`) ? goRunStart.get(`${core}\t${stage}`) : goRunStart.get(`\t${stage}`);
    let offset = 0;
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
      let restarts = null;
      if (stageStart !== undefined) {
        const s = stageStart + offset;
        restarts = restartsIn(events, s, s + ev.Elapsed);
      }
      offset += ev.Elapsed;
      addTest("go", core, ev.Test, ev.Elapsed, restarts);
    }
  }
  // Playwright: сумма длительностей прогонов теста; имя — как в pw-parse.js. Окна перезапусков —
  // только у изменяющих частей (pw, pw-2): часть только чтения идёт до запуска сэмплера.
  for (const dir of list(base, /^pw/)) {
    const windows = /^pw(-\d+)?$/.test(dir);
    for (const f of list(path.join(base, dir), /^report.*\.json$/)) {
      let rep;
      try {
        rep = JSON.parse(fs.readFileSync(path.join(base, dir, f), "utf8"));
      } catch {
        continue;
      }
      const visit = (suiteNode, titles) => {
        for (const spec of suiteNode.specs || []) {
          const name = [...titles, spec.title].join(" > ");
          const tagKey = cleanName(name);
          if (!pwTags.has(tagKey)) pwTags.set(tagKey, new Set());
          for (const tag of spec.tags || []) pwTags.get(tagKey).add(String(tag).replace(/^@/, ""));
          for (const test of spec.tests || []) {
            let ms = 0;
            let restarts = null;
            // ярус full и Slow: тег @full / аннотации tier-full и slow (lib/known.ts)
            const notes = new Set((test.annotations || []).map((a) => String(a && a.type)));
            const flags = {
              full: (spec.tags || []).some((tag) => String(tag).replace(/^@/, "") === "full") || notes.has("tier-full"),
              slow: notes.has("slow"),
            };
            for (const r of test.results || []) {
              const dur = Number(r.duration) || 0;
              ms += dur;
              const st = Date.parse(r.startTime || "");
              if (windows && Number.isFinite(st)) {
                const n = restartsIn(events, st / 1000, (st + dur) / 1000);
                if (n !== null) restarts = (restarts || 0) + n;
              }
            }
            addTest("pw", core, name, ms / 1000, restarts, flags);
          }
        }
        for (const child of suiteNode.suites || []) visit(child, [...titles, child.title]);
      };
      for (const top of rep.suites || []) visit(top, top.file === top.title ? [] : [top.title]);
    }
  }
}

const restartsCell = (t) => (t.restarts === null ? "-" : String(t.restarts));
const longest = [...testMap.values()].sort((a, b) => b.sec - a.sec);
if (longest.length > 0) {
  out.push("## 10 самых долгих тестов", "", "| Тест | Ядро | с | перезапуски |", "|---|---|---:|---:|");
  for (const t of longest.slice(0, 10)) {
    out.push(`| ${mdName(t.name)} | ${t.core} | ${Math.round(t.sec)} | ${restartsCell(t)} |`);
  }
  out.push("");
}

// Тесты Парето (todo 01): тесты Go верхнего уровня всех ветвей по убыванию секунд, пока
// накопленное время не достигнет 80% суммарного времени тестов Go.
const goTests = longest.filter((t) => t.kind === "go");
if (goTests.length > 0) {
  const goTotal = goTests.reduce((s, t) => s + t.sec, 0);
  const pareto = [];
  let acc = 0;
  for (const t of goTests) {
    if (goTotal > 0 && acc >= 0.8 * goTotal) break;
    pareto.push(t);
    acc += t.sec;
  }
  out.push("## Тесты Парето (80% времени Go)", "");
  out.push(`- Всего Go: ${goTests.length} тестов, ${Math.round(goTotal)} с; Парето: ${pareto.length} тестов`, "");
  out.push("| Тест | Ядро | с | доля % | перезапуски |", "|---|---|---:|---:|---:|");
  for (const t of pareto) {
    const share = goTotal > 0 ? ((t.sec / goTotal) * 100).toFixed(1) : "0.0";
    out.push(`| ${mdName(t.name)} | ${t.core} | ${Math.round(t.sec)} | ${share} | ${restartsCell(t)} |`);
  }
  out.push("");
}

// --- бюджет gate (D-15) ------------------------------------------------------------------

// Бюджет теста gate: Go 60 с, Playwright 30 с. Тесты яруса full и с меткой Slow бюджетом не
// ограничены. Только предупреждение: итог прогона не меняется.
const BUDGET = { go: 60, pw: 30 };
if (testMap.size > 0) {
  // факты Go: имя теста -> { full, slow }; не прочитаны - null (разметка Go не учитывается)
  let goFacts = null;
  try {
    goFacts = new Map();
    for (const r of coreMarks.testFacts(ROOT)) {
      if (r.kind !== "go") continue;
      const prev = goFacts.get(r.test);
      goFacts.set(r.test, { full: r.tier === "full" || (prev && prev.full), slow: !!r.slowReason || (prev && prev.slow) });
    }
  } catch {
    goFacts = null;
  }
  const over = longest
    .filter((t) => {
      if (t.kind === "go" && goFacts) {
        const f = goFacts.get(t.name);
        if (f && (f.full || f.slow)) return false;
      }
      if (t.full || t.slow) return false;
      return t.sec > BUDGET[t.kind];
    })
    .sort((a, b) => b.sec - a.sec);
  out.push("## Превышен бюджет gate", "");
  out.push(
    "Предупреждение: итог прогона не меняется; на ship:pre — full (по виду), Slow с причиной или todo (D-15).",
    ""
  );
  if (!goFacts) out.push("- Разметка Go не прочитана: full и Slow Go-тестов не учтены.", "");
  if (over.length === 0) out.push("- нет");
  else {
    out.push("| Тест | Ядро | с | бюджет с |", "|---|---|---:|---:|");
    for (const t of over) out.push(`| ${mdName(t.name)} | ${t.core} | ${Math.round(t.sec)} | ${BUDGET[t.kind]} |`);
  }
  out.push("");
}

// --- метки ядра: страховка D-05б и раскладка тестов по ветвям (D-19) ----------------------

// QFAIL и QPASS — исход теста в карантине (D-17): отдельный класс, на итог не влияет
const RESULT_ORDER = ["FAIL", "XPASS", "QFAIL", "KNOWN", "QPASS", "SKIP", "PASS"];
const classOf = (st) => (st === "FAIL" || st === "XPASS" ? "FAIL" : st === "QFAIL" || st === "QPASS" ? "quarantine" : st);
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
    // проверка в карантине хоть в одной ветви метки ядра не требует (D-17)
    if (classOf(perCore.get("xray")) === "quarantine" || classOf(perCore.get("mihomo")) === "quarantine") continue;
    if (classOf(perCore.get("xray")) === classOf(perCore.get("mihomo"))) continue;
    if (markOf(check) !== "") continue; // размечен или Go-разметка не прочитана
    needMark.push({ check, xray: perCore.get("xray"), mihomo: perCore.get("mihomo") });
  }
}

if (suite === "full") {
  out.push("## Нужна метка ядра", "");
  if (needMark.length === 0) out.push("- нет");
  // пункт списка: cleanName без экранирования — текст совпадает с именем проверки в results.tsv
  for (const n of needMark) {
    out.push(`- core-mark:${cleanName(n.check)}: нужна метка ядра: xray ${n.xray}, mihomo ${n.mihomo}`);
  }
  out.push("");
}

// --- тренд набора против прошлого полного прогона (D-16) ------------------------------------

async function trendSection() {
  try {
    const started = (fs.readFileSync(path.join(report, "meta"), "utf8").match(/^started=(\S+)\s*$/m) || [])[1];
    if (!started) return ["- Тренд: время начала отчёта неизвестно"];
    const hist = await import("./history.mjs");
    const rows = hist.readHistory();
    const prevRef = hist.lastFull(rows, { before: started });
    if (!prevRef) return ["- Тренд: прошлого полного прогона в истории нет"];
    // текущие проверки: верхние go: и pw: этого отчёта с ядром, секунды по всем ядрам
    const cur = new Set();
    const curSecs = new Map();
    for (const t of testMap.values()) {
      const check = `${t.kind}:${t.name}`;
      cur.add(`${check}\t${t.core}`);
      curSecs.set(check, (curSecs.get(check) || 0) + t.sec);
    }
    const old = new Set();
    const oldChecks = new Set();
    let oldTotal = 0;
    let oldTimed = false;
    for (const r of rows) {
      if (r.report !== prevRef.report || r.target !== target || r.core === "-") continue;
      const top = (r.check.startsWith("go:") && !r.check.includes("/")) || r.check.startsWith("pw:");
      if (!top) continue;
      old.add(`${r.check}\t${r.core}`);
      oldChecks.add(r.check);
      if (r.seconds !== null) {
        oldTotal += r.seconds;
        oldTimed = true;
      }
    }
    if (old.size === 0) return ["- Тренд: в прошлом полном прогоне нет данных этой цели"];
    let curTotal = 0;
    for (const v of curSecs.values()) curTotal += v;
    const signed = (n, places = 0) => {
      const v = places ? Number(n.toFixed(places)) : Math.round(n);
      const str = places ? v.toFixed(places) : String(v);
      return v >= 0 ? `+${str}` : str;
    };
    const mins = (sec) => `${(sec / 60).toFixed(1)} мин`;
    const was = oldTimed ? `${mins(oldTotal)}, ${signed((curTotal - oldTotal) / 60, 1)}` : "н/д";
    const lines = [
      `- Тренд: тестов ${cur.size} (было ${old.size}, ${signed(cur.size - old.size)}), время тестов ${mins(curTotal)} (было ${was}); прошлый полный — ${prevRef.report}`
    ];
    const fresh = [...curSecs]
      .filter(([check]) => !oldChecks.has(check))
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5)
      .map(([check, sec]) => `${cleanName(check)} (${Math.round(sec)} с)`);
    lines.push(`- Новые тесты: ${fresh.length > 0 ? fresh.join(", ") : "нет"}`);
    return lines;
  } catch {
    return ["- Тренд: история недоступна"];
  }
}

if (suite === "full") {
  out.push("## Тренд набора", "");
  out.push(...(await trendSection()));
  out.push("");
}

// --- классы падений (RTR-05, D-19…D-21) -----------------------------------------------------

// Признаки потери связи в детали строки результата (D-21); тот же список — в run.sh (RT_STAND_RE).
const STAND_RE =
  /нет связи|связь потеряна|ERR_NETWORK_CHANGED|net::ERR_INTERNET_DISCONNECTED|net::ERR_NAME_NOT_RESOLVED|ECONNRESET|ETIMEDOUT/;
// от худшего статуса к лучшему; прочие значения в разбор не берутся
const WORST = ["FAIL", "XPASS", "QFAIL", "KNOWN", "QPASS", "SKIP", "PASS"];
const PASSED = new Set(["PASS", "QPASS"]);

function readText(name) {
  try {
    return fs.readFileSync(path.join(report, target, name), "utf8");
  } catch {
    return null;
  }
}
// строки файла результатов цели: статус, ядро, проверка, деталь (деталь в вывод не идёт)
function readRows(name) {
  const text = readText(name);
  if (!text) return [];
  const rows = [];
  for (const line of text.split("\n")) {
    if (!line) continue;
    const f = line.split("\t");
    if (f.length < 3 || !WORST.includes(f[0])) continue;
    rows.push({ st: f[0], core: f[1], check: f[2], detail: f[3] || "" });
  }
  return rows;
}
// семья проверки: подтест Go сводится к родителю (перепрогон гонит родительский TestRouter*)
const famOf = (core, check) => `${core}\t${check.startsWith("go:") ? "go:" + check.slice(3).split("/")[0] : check}`;
const exactOf = (core, check) => `${core}\t${check}`;
// ключ -> { st: худший статус, detail: деталь первой худшей строки }
function worstBy(rows, keyOf) {
  const m = new Map();
  for (const r of rows) {
    const k = keyOf(r.core, r.check);
    const prev = m.get(k);
    if (!prev || WORST.indexOf(r.st) < WORST.indexOf(prev.st)) m.set(k, { st: r.st, detail: r.detail });
  }
  return m;
}

const mainRows = readRows("results.tsv");
// строки core-mark:… попадают в results.tsv только с --write: в сводке без него они учитываются здесь
for (const n of needMark) {
  if (!existingMarks.has(n.check)) mainRows.push({ st: "FAIL", core: "-", check: `core-mark:${n.check}`, detail: "" });
}
const mainWorst = worstBy(mainRows, exactOf);
const confirmRows = readRows("confirm.tsv");
const confirmWorst = worstBy(
  confirmRows.filter((r) => /^(go|pw):/.test(r.check)),
  famOf
);
// сверка версии стенда в подтверждающем перепрогоне не выполнилась: прошедший перепрогон без сверки
// не подтверждает нестабильность (границу никто не проверил)
const confirmGuardBroken = confirmRows.some((r) => r.st === "FAIL" && r.check === "version-guard-error");
// «без перепрогона: …» — перепрогон пропущен из-за потолка (run.sh пишет причину в confirm-skip)
const catchupExists = readText("catchup.tsv") !== null;
const catchupRows = readRows("catchup.tsv");
const catchupFam = worstBy(
  catchupRows.filter((r) => /^(go|pw):/.test(r.check)),
  famOf
);
const notrunLines = (readText("notrun.txt") || "")
  .split("\n")
  .map((l) => l.trim())
  .filter(Boolean);
const confirmSkipText = readText("confirm-skip");
const confirmSkipWhy = confirmSkipText === null ? null : confirmSkipText.split("\n")[0].trim() || "без перепрогона";

// Сверка версии стенда (D-13, контракт сверки версии): <цель>/version-stop.tsv — первая строка stop
// (ядро, граница, ожидалась, на стенде, эпоха ПК), строки suspect (ядро, проверка — падение на
// сменённой версии) и notrun (ядро, что не выполнено); <цель>/version-guard.tsv — строки ok,
// transition, skip и unverified (связь пропала на сверке) по границам. Нет файлов (старый отчёт) — сводка прежняя.
const vStopText = readText("version-stop.tsv");
const vGuardText = readText("version-guard.tsv");
const vStopRows = (vStopText || "")
  .split("\n")
  .filter(Boolean)
  .map((l) => l.split("\t"));
const vStop = vStopRows.find((f) => f[0] === "stop") || null;
const vSuspect = new Set(vStopRows.filter((f) => f[0] === "suspect" && f.length >= 3).map((f) => famOf(f[1], f[2])));
const vNotrun = vStopRows
  .filter((f) => f[0] === "notrun" && f.length >= 3)
  .map((f) => (f[1] && f[1] !== "-" ? `${f[1]}: ${f.slice(2).join(" ")}` : f.slice(2).join(" ")));
const vGuardKinds = (vGuardText || "")
  .split("\n")
  .filter(Boolean)
  .map((l) => l.split("\t")[0]);
// Класс stand смены версии (открытый, не находка): строка version-guard, падение из строк suspect и
// xcp-after-restore отчёта со строкой stop. В подтверждающий перепрогон такие строки не идут: цикл
// ветвей при смене версии останавливается раньше него (run.sh).
function versionClass(core, check) {
  if (check === "version-guard") return { cls: "stand", why: "смена версии стенда", open: true, version: true };
  if (vSuspect.has(famOf(core, check)) || (vStop && check === "xcp-after-restore")) {
    return { cls: "stand", why: "падение на сменённой версии стенда", open: true, version: true };
  }
  return null;
}

// Класс одной проверки. stand — потеря связи; open — связь не вернулась или догона не было
// (причина начинается со слов «не догнан»: run.sh краснит такие строки).
function classify(core, check, st, detail) {
  if (st === "XPASS") return { cls: "xpass", why: "метка известного падения больше не нужна" };
  if (st === "KNOWN") return { cls: "known", why: "метка известного падения" };
  if (st === "QFAIL") return { cls: "quarantine", why: "карантин: упал" };
  if (st === "QPASS") return { cls: "quarantine", why: "карантин: прошёл", quiet: true };
  if (st !== "FAIL") return null;
  const v = versionClass(core, check);
  if (v) return v;
  if (check === "reachable" || STAND_RE.test(detail)) {
    if (check === "reachable") {
      if (catchupExists && notrunLines.length === 0) return { cls: "stand", why: "потеря связи, догон выполнен" };
    } else {
      const c = catchupFam.get(famOf(core, check));
      if (c && (PASSED.has(c.st) || c.st === "KNOWN")) return { cls: "stand", why: "потеря связи, догнан: прошёл" };
      if (c && c.st === "FAIL" && !STAND_RE.test(c.detail)) return { cls: "new", why: "упал при догоне после потери связи" };
    }
    return { cls: "stand", why: "не догнан: потеря связи со стендом", open: true };
  }
  if (!/^(go|pw):/.test(check)) return { cls: "new", why: "падение проверки" };
  if (confirmSkipWhy !== null) return { cls: "new", why: confirmSkipWhy };
  if (confirmGuardBroken) return { cls: "new", why: "подтверждающий перепрогон без сверки версии стенда" };
  const c = confirmWorst.get(famOf(core, check));
  if (c && PASSED.has(c.st)) return { cls: "flaky", why: "прошёл при подтверждающем перепрогоне" };
  if (c && (c.st === "FAIL" || c.st === "XPASS")) return { cls: "new", why: "упал снова при подтверждающем перепрогоне" };
  return { cls: "new", why: "без подтверждающего перепрогона" };
}

const classRows = [];
for (const [key, v] of mainWorst) {
  const [core, check] = key.split("\t");
  const c = classify(core, check, v.st, v.detail);
  if (c) classRows.push({ core, check, ...c });
}
// счёт по тестам: подтест Go и его родитель — один тест
// строки догона, не бывшие падением основного прохода (набор не дошёл до них из-за обрыва)
for (const [key, v] of worstBy(catchupRows, exactOf)) {
  const [core, check] = key.split("\t");
  const m = mainWorst.get(key);
  if (m && (m.st === "FAIL" || m.st === "XPASS")) continue;
  if (v.st === "XPASS") classRows.push({ core, check, cls: "xpass", why: "метка известного падения больше не нужна" });
  else if (v.st === "FAIL" && versionClass(core, check)) classRows.push({ core, check, ...versionClass(core, check) });
  else if (v.st === "FAIL") {
    const standLike = STAND_RE.test(v.detail);
    classRows.push({
      core,
      check,
      cls: standLike ? "stand" : "new",
      why: standLike ? "не догнан: потеря связи при догоне" : "упал при догоне после потери связи",
      open: standLike,
    });
  }
}
// смена версии стенда, а строки version-guard среди результатов нет (её записал этап подтверждающего
// перепрогона в confirm.tsv): строка класса всё равно есть — итог красный, run.sh видит класс
if (vStop && !classRows.some((r) => r.check === "version-guard")) {
  classRows.push({ core: vStop[1] || "-", check: "version-guard", cls: "stand", why: "смена версии стенда", open: true, version: true });
}
// часть набора не выполнена, а строк открытого stand из-за связи нет: итог всё равно красный
if (notrunLines.length > 0 && !classRows.some((r) => r.open && !r.version)) {
  classRows.push({ core: "-", check: "notrun:связь", cls: "stand", why: "не догнан: часть набора не выполнена (notrun.txt)", open: true });
}
const nOf = (pred) => new Set(classRows.filter(pred).map((r) => `${r.cls}\t${famOf(r.core, r.check)}`)).size;
const nNew = nOf((r) => r.cls === "new");
const nFlaky = nOf((r) => r.cls === "flaky");
const nXpass = nOf((r) => r.cls === "xpass");
const nOpen = nOf((r) => r.open);
// обрывы связи без строк смены версии: их счёт в итоге «красный: смена версии стенда»
const nLink = nOf((r) => r.open && !r.version);
const nOnChanged = nOf((r) => r.version && r.why === "падение на сменённой версии стенда");
const haveResults = readText("results.tsv") !== null;

// «падений/прогонов за 30 дней» по истории; ошибка истории — «-»
let freq = null;
try {
  const hist = await import("./history.mjs");
  freq = hist.failFreq(hist.readHistory(), { days: 30 });
} catch {
  freq = null;
}
const histCell = (check) => {
  const v = freq && freq.get(check);
  return v ? `${v.fails}/${v.runs}` : "-";
};

out.push("## Классы падений", "");
// строка сверки версии стенда (D-13): нет version-guard.tsv и остановки (старый отчёт) — строки нет
if (vStop) out.push(`- Сверка версии стенда: остановлено на границе ${cleanName(vStop[2] || "-")}`);
else if (vGuardText !== null) {
  const nChecked = vGuardKinds.filter((k) => k === "ok" || k === "transition").length;
  const nTrans = vGuardKinds.filter((k) => k === "transition").length;
  const nUnver = vGuardKinds.filter((k) => k === "unverified").length;
  if (nChecked === 0 && vGuardKinds.includes("skip")) out.push("- Сверка версии стенда: выключена — ожидаемая версия неизвестна");
  else if (nTrans > 0) out.push(`- Сверка версии стенда: ${nChecked} границ, принятых переходов ${nTrans}, смен нет${nUnver > 0 ? `, не сверено из-за связи ${nUnver}` : ""}`);
  else out.push(`- Сверка версии стенда: ${nChecked} границ, смен нет${nUnver > 0 ? `, не сверено из-за связи ${nUnver}` : ""}`);
}
const lossRows = (readText("link-loss.tsv") || "").split("\n").filter(Boolean);
if (lossRows.length > 0) {
  const caught = lossRows.filter((l) => l.split("\t")[2] === "догон").length;
  out.push(`- Обрывы связи: ${lossRows.length}, догнано: ${caught}`);
}
const confirmCount = Number((readText("confirm-count") || "").trim());
if (confirmSkipWhy !== null) out.push(`- Подтверждающий перепрогон: пропущен (новых падений ${Number.isFinite(confirmCount) ? confirmCount : "-"}, ${confirmSkipWhy})`);
else if (readText("confirm.tsv") !== null) out.push(`- Подтверждающий перепрогон: перегнано тестов ${Number.isFinite(confirmCount) ? confirmCount : "-"}`);
if (out[out.length - 1] !== "") out.push("");
const shown = classRows.filter((r) => !r.quiet);
if (shown.length === 0) out.push("- падений нет");
else {
  out.push("| Класс | Проверка | Ядро | Причина класса | В истории |", "|---|---|---|---|---:|");
  for (const r of shown) out.push(`| ${r.cls} | ${mdName(r.check)} | ${r.core} | ${r.why} | ${histCell(r.check)} |`);
}
out.push("");
if (!haveResults) out.push("- Итог: красный (результатов нет)");
else if (vStop) out.push(`- Итог: красный: смена версии стенда (новых ${nNew}, нестабильных ${nFlaky}, xpass ${nXpass}, обрывов ${nLink})`);
else if (nNew + nFlaky + nXpass + nOpen === 0) out.push("- Итог: зелёный");
else if (nNew + nXpass + nOpen === 0) out.push(`- Итог: красный: нестабильный (нестабильных ${nFlaky})`);
else out.push(`- Итог: красный (новых ${nNew}, нестабильных ${nFlaky}, xpass ${nXpass}, обрывов ${nOpen})`);
out.push("");
if (vStop) {
  out.push("## Остановлено: смена версии стенда", "");
  out.push(`- Цель: ${target}, ядро: ${cleanName(vStop[1] || "-")}, граница: ${cleanName(vStop[2] || "-")}`);
  out.push(`- Ожидалась: ${cleanName(vStop[3] || "-")}; на стенде: ${cleanName(vStop[4] || "-")}`);
  out.push(`- Падения на сменённой версии (класс stand): ${nOnChanged}`);
  out.push("- Не выполнено:");
  if (vNotrun.length === 0) out.push("  - нет");
  for (const l of vNotrun) out.push(`  - ${cleanName(l)}`);
  out.push("");
}
if (notrunLines.length > 0) {
  out.push("## Не выполнено из-за обрыва связи", "");
  for (const l of notrunLines) out.push(`- ${cleanName(l)}`);
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
  // classes.tsv: класс, ядро, проверка, причина класса (без текста ошибки); файл пишется и пустым
  if (haveResults) {
    try {
      const body = classRows.map((r) => `${r.cls}\t${r.core}\t${r.check}\t${r.why}`).join("\n");
      fs.writeFileSync(path.join(report, target, "classes.tsv"), body ? body + "\n" : "");
    } catch {
      // каталог цели недоступен
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
