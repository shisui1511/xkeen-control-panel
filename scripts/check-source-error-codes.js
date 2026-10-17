#!/usr/bin/env node
/*
 * check-source-error-codes.js — список кодов ошибок SourceError в Go
 * (internal/nodes/errors.go) равен общему списку фронтенда
 * (frontend/src/lib/sourceErrorCodes.json). Единого источника у них нет:
 * код без текста на фронтенде показывается пользователю сырой строкой.
 *
 * Проверяется: каждая константа Code* перечислена в AllCodes(), значения
 * констант уникальны, JSON — отсортированный массив без дублей, множества равны.
 *
 * Usage: node scripts/check-source-error-codes.js [--root <каталог>]
 */
const fs = require('fs');
const path = require('path');

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf('--root');
const root = rootIdx >= 0 ? path.resolve(argv[rootIdx + 1]) : path.resolve(__dirname, '..');

const BACKEND = 'internal/nodes/errors.go';
const FRONTEND = 'frontend/src/lib/sourceErrorCodes.json';

function read(rel) {
  try {
    return fs.readFileSync(path.join(root, rel), 'utf8');
  } catch (e) {
    console.error('check-source-error-codes: не удалось прочитать файл: ' + e.message);
    process.exit(1);
  }
}

const errors = [];
const goSrc = read(BACKEND);

// Константы вида `CodeTimeout = "timeout"`.
const consts = new Map();
for (const m of goSrc.matchAll(/^\s*(Code[A-Za-z0-9]+)\s*=\s*"([a-z0-9_]+)"/gm)) {
  consts.set(m[1], m[2]);
}
if (consts.size === 0) errors.push(`${BACKEND}: не найдены константы Code*`);

const values = [...consts.values()];
const dupValues = values.filter((v, i) => values.indexOf(v) !== i);
for (const v of new Set(dupValues)) errors.push(`${BACKEND}: значение "${v}" у нескольких констант`);

// Тело AllCodes(): каждая константа должна быть перечислена.
const allBody = (goSrc.match(/func AllCodes\(\) \[\]string \{([\s\S]*?)\n\}/) || [])[1] ?? null;
if (allBody === null) {
  errors.push(`${BACKEND}: не найдена функция AllCodes`);
} else {
  for (const name of consts.keys()) {
    if (!new RegExp(`\\b${name}\\b`).test(allBody)) errors.push(`${BACKEND}: ${name} не перечислена в AllCodes()`);
  }
}

let front = null;
try {
  front = JSON.parse(read(FRONTEND));
} catch (e) {
  errors.push(`${FRONTEND}: не разбирается как JSON: ${e.message}`);
}
if (front !== null) {
  if (!Array.isArray(front) || front.some((c) => typeof c !== 'string')) {
    errors.push(`${FRONTEND}: ожидается массив строк`);
  } else {
    const sorted = [...front].sort();
    if (front.some((c, i) => c !== sorted[i])) errors.push(`${FRONTEND}: список не отсортирован по алфавиту`);
    for (const c of new Set(front.filter((v, i) => front.indexOf(v) !== i))) {
      errors.push(`${FRONTEND}: дубликат "${c}"`);
    }
    const goSet = new Set(values);
    const frSet = new Set(front);
    for (const c of goSet) if (!frSet.has(c)) errors.push(`код "${c}" есть в Go, но нет в ${FRONTEND}`);
    for (const c of frSet) if (!goSet.has(c)) errors.push(`код "${c}" есть в ${FRONTEND}, но нет в Go`);
  }
}

if (errors.length > 0) {
  for (const e of errors) console.error('❌ ' + e);
  process.exit(1);
}
console.log(`✅ Коды ошибок источников совпадают: ${consts.size}`);
