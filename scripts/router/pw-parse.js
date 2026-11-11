#!/usr/bin/env node
// scripts/router/pw-parse.js <json-отчёт Playwright> — разбор отчёта в строки результатов.
//
// Вывод: «статус<TAB>проверка<TAB>деталь», по строке на тест, где статус — одно из
// PASS, FAIL, KNOWN, XPASS, QPASS, QFAIL, SKIP; проверка — название теста (pw-suite.sh
// добавляет «pw:»).
//   - тест с аннотацией known-failure, упавший как ожидалось -> KNOWN, деталь — slug todo;
//   - тест с аннотацией known-failure, который прошёл (test.fail не сработал: последний результат
//     passed) -> XPASS;
//   - тест с аннотацией known-failure, не дошедший до ожидаемого падения (тайм-аут, прерывание:
//     последний результат не passed) -> FAIL со статусом и текстом ошибки, а не XPASS — это не
//     повод снимать метку;
//   - тест с аннотацией quarantine (карантин, D-17; без test.fail): expected -> QPASS, иначе
//     (unexpected, flaky, тайм-аут) -> QFAIL; деталь — «карантин: <slug>» и текст падения.
//     Описание аннотации — slug вида [a-z0-9][a-z0-9-]*; иначе карантин не признаётся и
//     падение остаётся FAIL с деталью «карантин без slug».
//     QPASS и QFAIL на итог не влияют: pw-suite.sh считает в итог только FAIL и XPASS;
//   - остальные: expected -> PASS, unexpected/flaky -> FAIL, skipped -> SKIP (деталь — описание
//     аннотации 'skip', то есть причина test.skip(условие, причина); без неё — «пропущен»).
// Ошибки вне тестов (глобальная настройка: панель не отвечает, вход не удался) -> FAIL global-setup.
//
// Необязательные ключи (pw-suite.sh, сверка версии стенда D-13); без них вывод прежний:
//   --files-out <файл>        для каждой выведенной строки теста — строка
//                             «<проверка><TAB>tests/router/<файл спека>» в том же порядке
//                             (проверка — как в выводе, без «pw:»; файл — из ближайшего набора
//                             с полем file). Строки global-setup идут последними и спека не имеют:
//                             в файл не пишутся.
//   --interrupted-out <файл>  прогон остановлен (мягкая остановка по смене версии): тест, чей
//                             последний результат interrupted, тест без результатов и тест,
//                             пропущенный без ожидаемого пропуска (Playwright «did not run»),
//                             в вывод не идут, а пишутся строкой «tests/router/<спек><TAB><название>».
// Неизвестный ключ, ключ без значения или лишний аргумент — код 2.
// Без зависимостей: только встроенные модули Node.

"use strict";

const fs = require("fs");

function usage() {
  console.error(
    "usage: pw-parse.js <report.json> [--files-out <файл>] [--interrupted-out <файл>]",
  );
  process.exit(2);
}

let file = null;
let filesOut = null;
let interruptedOut = null;
{
  const args = process.argv.slice(2);
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (a === "--files-out" || a === "--interrupted-out") {
      const v = args[i + 1];
      if (v === undefined || v === "") usage();
      if (a === "--files-out") filesOut = v;
      else interruptedOut = v;
      i++;
    } else if (a.startsWith("-") || file !== null) {
      usage();
    } else {
      file = a;
    }
  }
}
if (!file) usage();

let report;
try {
  report = JSON.parse(fs.readFileSync(file, "utf8"));
} catch (e) {
  console.error(`pw-parse: отчёт не читается: ${e.message}`);
  process.exit(3);
}

// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;]*m/g;

function clean(text, limit) {
  const flat = String(text || "")
    .replace(ANSI, "")
    .replace(/\s+/g, " ")
    .trim();
  return flat.length > limit ? flat.slice(0, limit) + "…" : flat;
}

// строки --files-out и --interrupted-out
const fileLines = [];
const interruptedLines = [];

// spec — файл спека строки теста (для --files-out; строка пишется всегда, даже с пустым
// спеком, чтобы порядок совпадал с выводом); у global-setup спека нет — строки нет
function outLine(status, check, detail, spec) {
  const name = clean(check, 300);
  console.log([status, name, clean(detail, 400)].join("\t"));
  if (spec !== undefined) fileLines.push(`${name}\t${spec}`);
}

// Тест, не выполненный из-за остановки прогона: последний результат interrupted, результатов нет
// или пропуск без ожидаемого пропуска (так Playwright помечает тест, начатый после остановки).
function interrupted(test) {
  const last = (test.results || []).at(-1);
  if (!last) return true;
  if (last.status === "interrupted") return true;
  return last.status === "skipped" && test.expectedStatus !== "skipped";
}

function knownSlug(test) {
  const lists = [test.annotations || []];
  for (const r of test.results || []) lists.push(r.annotations || []);
  for (const list of lists) {
    for (const a of list) {
      if (a && a.type === "known-failure")
        return a.description || "known-failure";
    }
  }
  return null;
}

// slug карантина: null — аннотации нет, "" — аннотация без допустимого slug (карантин не
// признаётся: check-known.sh не видит такого вызова, todo и срока у него нет)
function quarantineSlug(test) {
  const lists = [test.annotations || []];
  for (const r of test.results || []) lists.push(r.annotations || []);
  for (const list of lists) {
    for (const a of list) {
      if (a && a.type === "quarantine")
        return typeof a.description === "string" &&
          /^[a-z0-9][a-z0-9-]*$/.test(a.description)
          ? a.description
          : "";
    }
  }
  return null;
}

function skipReason(test) {
  const lists = [test.annotations || []];
  for (const r of test.results || []) lists.push(r.annotations || []);
  for (const list of lists) {
    for (const a of list) {
      if (a && a.type === "skip" && a.description) return a.description;
    }
  }
  return null;
}

function errorText(test) {
  for (const r of test.results || []) {
    const e = r.error || (r.errors && r.errors[0]);
    if (e && (e.message || e.value)) return e.message || e.value;
  }
  return "";
}

function visit(suite, titles, parentFile) {
  const file = suite.file || parentFile;
  const specFile = file ? `tests/router/${file}` : "";
  for (const spec of suite.specs || []) {
    const name = [...titles, spec.title].join(" > ");
    for (const test of spec.tests || []) {
      if (interruptedOut !== null && interrupted(test)) {
        interruptedLines.push(`${specFile}\t${clean(name, 300)}`);
        continue;
      }
      // строка результата с файлом спека (для --files-out)
      const out = (status, check, detail) =>
        outLine(status, check, detail, specFile);
      const slug = knownSlug(test);
      const qslug = quarantineSlug(test);
      const st = test.status;
      // test.fail() даёт unexpected и при прошедшем тесте, и при тайм-ауте: различает статус
      // последнего результата
      const last = (test.results || []).at(-1);
      if (st === "skipped") {
        out("SKIP", name, skipReason(test) || "пропущен");
      } else if (
        slug &&
        st === "expected" &&
        test.expectedStatus === "failed"
      ) {
        out("KNOWN", name, slug);
      } else if (
        slug &&
        st === "unexpected" &&
        last &&
        last.status === "passed"
      ) {
        out(
          "XPASS",
          name,
          `${slug}: тест прошёл, метка известного падения больше не нужна`,
        );
      } else if (qslug === "" && st !== "expected") {
        // аннотация quarantine без slug: падение не глушится
        out(
          "FAIL",
          name,
          `карантин без slug: ${errorText(test) || last?.status || st}`,
        );
      } else if (qslug) {
        // карантин (D-17): исход виден, на итог не влияет
        if (st === "expected") out("QPASS", name, `карантин: ${qslug}`);
        else
          out(
            "QFAIL",
            name,
            `карантин: ${qslug}: ${errorText(test) || last?.status || st}`,
          );
      } else if (slug && st === "unexpected") {
        out(
          "FAIL",
          name,
          `${slug}: ${last?.status ?? "unexpected"}: ${errorText(test)}`,
        );
      } else if (st === "expected") {
        out("PASS", name, "");
      } else {
        out("FAIL", name, errorText(test) || st);
      }
    }
  }
  // вложенные наборы — это test.describe, их названия входят в имя проверки
  for (const child of suite.suites || [])
    visit(child, [...titles, child.title], file);
}

for (const top of report.suites || []) {
  visit(top, top.file === top.title ? [] : [top.title], "");
}

for (const e of report.errors || []) {
  outLine("FAIL", "global-setup", e.message || e.value || "ошибка вне тестов");
}

const joinLines = (lines) => lines.map((l) => l + "\n").join("");
if (filesOut !== null) fs.writeFileSync(filesOut, joinLines(fileLines));
if (interruptedOut !== null)
  fs.writeFileSync(interruptedOut, joinLines(interruptedLines));
