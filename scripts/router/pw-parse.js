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
// Без зависимостей: только встроенные модули Node.

"use strict";

const fs = require("fs");

const file = process.argv[2];
if (!file) {
  console.error("usage: pw-parse.js <report.json>");
  process.exit(2);
}

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

function out(status, check, detail) {
  console.log([status, clean(check, 300), clean(detail, 400)].join("\t"));
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

function visit(suite, titles) {
  for (const spec of suite.specs || []) {
    const name = [...titles, spec.title].join(" > ");
    for (const test of spec.tests || []) {
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
    visit(child, [...titles, child.title]);
}

for (const top of report.suites || []) {
  visit(top, top.file === top.title ? [] : [top.title]);
}

for (const e of report.errors || []) {
  out("FAIL", "global-setup", e.message || e.value || "ошибка вне тестов");
}
