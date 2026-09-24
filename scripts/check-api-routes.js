#!/usr/bin/env node
/*
 * check-api-routes.js — every /api/... path the frontend calls must be a
 * route registered by the backend. Unknown API paths used to be answered
 * with the SPA page and 200, so calls to missing endpoints looked successful.
 *
 * Usage: node scripts/check-api-routes.js
 */
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');

function walk(dir, exts, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== 'node_modules') walk(full, exts, out);
    } else if (exts.some((e) => entry.name.endsWith(e)) && !entry.name.endsWith('.test.ts')) {
      out.push(full);
    }
  }
  return out;
}

// Routes: literal paths passed to Handle/HandleProtected/HandlePublic, plus
// prefixes built as "/api/.../"+name.
const routes = new Set();
for (const file of walk(path.join(root, 'cmd'), ['.go']).concat(walk(path.join(root, 'internal', 'server'), ['.go']))) {
  const src = fs.readFileSync(file, 'utf8');
  for (const m of src.matchAll(/Handle(?:Protected|Public)?\("(\/[^"]+)"(\+)?/g)) {
    routes.add(m[2] ? m[1] + '*' : m[1]);
  }
}

function known(p) {
  for (const r of routes) {
    if (r === p) return true;
    if (r.endsWith('*') && p.startsWith(r.slice(0, -1))) return true;
    if (r.endsWith('/') && p.startsWith(r)) return true;
    if (r.includes('{')) {
      const re = new RegExp('^' + r.replace(/\{[^}]+\}/g, '[^/]+') + '$');
      if (re.test(p)) return true;
    }
    // A template literal cut at ${…} ends with "/": any route below matches.
    if (p.endsWith('/') && r.startsWith(p)) return true;
  }
  return false;
}

const missing = [];
for (const file of walk(path.join(root, 'frontend', 'src'), ['.svelte', '.ts'])) {
  const src = fs.readFileSync(file, 'utf8');
  for (const m of src.matchAll(/['`"](\/api\/[A-Za-z0-9_/.-]+)/g)) {
    if (!known(m[1])) missing.push(`${path.relative(root, file)}: ${m[1]}`);
  }
}

if (missing.length > 0) {
  console.error('❌ Фронтенд вызывает API, которого нет в бэкенде:');
  for (const m of [...new Set(missing)]) console.error('  ' + m);
  process.exit(1);
}
console.log(`✅ Все вызовы API фронтенда соответствуют маршрутам бэкенда (${routes.size} маршрутов)`);
