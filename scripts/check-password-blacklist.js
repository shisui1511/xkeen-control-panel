#!/usr/bin/env node
/*
 * check-password-blacklist.js — чёрные списки паролей бэкенда
 * (internal/auth/password_blacklist.txt) и фронтенда
 * (frontend/src/lib/passwordBlacklist.json) совпадают без учёта регистра и
 * порядка. Расхождение пропускает на форме слабый пароль, который бэкенд
 * потом отклонит (или наоборот).
 *
 * Usage: node scripts/check-password-blacklist.js [--root <каталог>]
 */
const fs = require('fs');
const path = require('path');

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf('--root');
const root = rootIdx >= 0 ? path.resolve(argv[rootIdx + 1]) : path.resolve(__dirname, '..');

const BACKEND = 'internal/auth/password_blacklist.txt';
const FRONTEND = 'frontend/src/lib/passwordBlacklist.json';
const MIN_ENTRIES = 47;

function read(rel) {
  try {
    return fs.readFileSync(path.join(root, rel), 'utf8');
  } catch (e) {
    console.error('check-password-blacklist: не удалось прочитать файл: ' + e.message);
    process.exit(1);
  }
}

// Разбор как в parsePasswordBlacklist (internal/auth/password_policy.go).
const backend = new Set(
  read(BACKEND)
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l !== '')
    .map((l) => l.toLowerCase())
);

let list;
try {
  list = JSON.parse(read(FRONTEND));
} catch (e) {
  console.error(`❌ ${FRONTEND}: не разбирается как JSON: ${e.message}`);
  process.exit(1);
}
if (!Array.isArray(list) || list.some((w) => typeof w !== 'string')) {
  console.error(`❌ ${FRONTEND}: ожидается массив строк`);
  process.exit(1);
}
const frontend = new Set(list.map((w) => w.toLowerCase()));

const problems = [];
if (frontend.size < MIN_ENTRIES) {
  problems.push(`во фронтенд-списке ${frontend.size} записей, ожидается не меньше ${MIN_ENTRIES}`);
}
if (backend.size !== frontend.size) {
  problems.push(`размеры списков не равны: бэкенд ${backend.size}, фронтенд ${frontend.size}`);
}
for (const w of [...frontend].sort()) {
  if (!backend.has(w)) problems.push(`слово "${w}" есть во фронтенд-списке, но нет в ${BACKEND}`);
}
for (const w of [...backend].sort()) {
  if (!frontend.has(w)) problems.push(`слово "${w}" есть в ${BACKEND}, но нет во фронтенд-списке`);
}

if (problems.length > 0) {
  console.error('❌ Чёрные списки паролей бэкенда и фронтенда расходятся:');
  for (const p of problems) console.error('  ' + p);
  process.exit(1);
}
console.log(`✅ Чёрные списки паролей совпадают (${backend.size} записей)`);
