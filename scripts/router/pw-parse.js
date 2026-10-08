#!/usr/bin/env node
// scripts/router/pw-parse.js <json-отчёт Playwright> — разбор отчёта в строки результатов.
//
// Вывод: «статус<TAB>проверка<TAB>деталь», по строке на тест, где статус — одно из
// PASS, FAIL, KNOWN, XPASS, SKIP; проверка — название теста (pw-suite.sh добавляет «pw:»).
//   - тест с аннотацией known-failure, упавший как ожидалось -> KNOWN, деталь — slug todo;
//   - тест с аннотацией known-failure, который прошёл (test.fail не сработал) -> XPASS;
//   - остальные: expected -> PASS, unexpected/flaky -> FAIL, skipped -> SKIP.
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
      const st = test.status;
      if (st === "skipped") {
        out("SKIP", name, "пропущен");
      } else if (
        slug &&
        st === "expected" &&
        test.expectedStatus === "failed"
      ) {
        out("KNOWN", name, slug);
      } else if (slug && st === "unexpected") {
        out(
          "XPASS",
          name,
          `${slug}: тест прошёл, метка известного падения больше не нужна`,
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
