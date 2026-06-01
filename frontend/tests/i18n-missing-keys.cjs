#!/usr/bin/env node
// Аудит отсутствующих i18n-ключей (Phase 12, AUDIT-01, D-10).
// Собирает все t('a.b.c') / $t('a.b.c') из *.svelte и проверяет, что каждый
// присутствует в frontend/src/i18n.ts. Exit 1 + список, если есть missing.
//
// Все реальные ключи проекта namespaced (содержат точку): 'app.name', 'logs.title'.
// Требование точки в ключе отсекает ложные срабатывания на не-i18n вызовах t(...).
const fs = require('fs');
const path = require('path');

const srcDir = path.join(__dirname, '..', 'src');
const i18nPath = path.join(srcDir, 'i18n.ts');
const i18n = fs.readFileSync(i18nPath, 'utf8');

function walk(dir, acc) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, acc);
    else if (e.name.endsWith('.svelte')) acc.push(p);
  }
  return acc;
}

const used = new Set();
// $t('namespace.key') или t('namespace.key') — ключ обязан содержать точку.
const keyRe = /(?<![\w$])\$?t\(\s*['"]([a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9_]+)+)['"]/g;
for (const f of walk(srcDir, [])) {
  const txt = fs.readFileSync(f, 'utf8');
  let m;
  while ((m = keyRe.exec(txt)) !== null) used.add(m[1]);
}

const missing = [];
for (const k of used) {
  if (!i18n.includes(`'${k}'`) && !i18n.includes(`"${k}"`)) missing.push(k);
}

if (missing.length) {
  console.error(`MISSING i18n keys (${missing.length}):`);
  for (const k of missing.sort()) console.error('  ' + k);
  process.exit(1);
}
console.log(`OK: all ${used.size} used i18n keys present in i18n.ts`);
