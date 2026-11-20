#!/usr/bin/env node
/*
 * check-api-routes.js — every /api/... path the frontend calls must be a
 * route registered by the backend. Unknown API paths used to be answered
 * with the SPA page and 200, so calls to missing endpoints looked successful.
 *
 * Снятые маршруты старого слоя (removed_routes в scripts/legacy-removal-contract.json)
 * не регистрируются ни литералом, ни конкатенацией «…/"+имя», и фронтенд их не вызывает:
 * такой путь отвечает обычным 404 общего обработчика неизвестных /api/….
 *
 * Usage: node scripts/check-api-routes.js
 */
const fs = require('fs');
const path = require('path');

const { removedRoutes, removedMatch, removedRegistrations } = require('./lib/removed-routes');

const root = path.resolve(__dirname, '..');
const removed = removedRoutes();
const revived = [];

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
  revived.push(...removedRegistrations(path.relative(root, file), src, removed));
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
  const rel = path.relative(root, file);
  for (const m of src.matchAll(/['`"](\/api\/[A-Za-z0-9_/.-]+)/g)) {
    if (!known(m[1])) missing.push(`${rel}: ${m[1]}`);
  }
  // Любой путь панели, не только /api/… (снятые /mihomo/…/provider.yaml тоже).
  for (const m of src.matchAll(/['`"](\/[A-Za-z0-9_/.-]+)/g)) {
    const hit = removedMatch(m[1], removed);
    if (hit) revived.push(`${rel}: ${m[1]} — снятый маршрут ${hit}`);
  }
  // Обрезанный на ${…} префикс («/api/mihomo/profiles/${action}») сам по себе живой; снятым
  // он становится, если в том же файле есть литерал имени снятого действия ('adopt').
  for (const m of src.matchAll(/`(\/[A-Za-z0-9_/.-]+\/)\$\{/g)) {
    for (const entry of removed) {
      const rest = entry.startsWith(m[1]) ? entry.slice(m[1].length) : '';
      if (!rest || rest.includes('/')) continue;
      if (new RegExp(`['"\`]${rest.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}['"\`]`).test(src)) {
        revived.push(`${rel}: ${m[1]}\${…} с действием '${rest}' — снятый маршрут ${entry}`);
      }
    }
  }
}

let failed = false;
if (missing.length > 0) {
  console.error('❌ Фронтенд вызывает API, которого нет в бэкенде:');
  for (const m of [...new Set(missing)]) console.error('  ' + m);
  failed = true;
}
if (revived.length > 0) {
  console.error('❌ Снятый маршрут старого слоя вернулся (removed_routes, scripts/legacy-removal-contract.json):');
  for (const m of [...new Set(revived)]) console.error('  ' + m);
  failed = true;
}
if (failed) process.exit(1);
console.log(
  `✅ Все вызовы API фронтенда соответствуют маршрутам бэкенда (${routes.size} маршрутов), снятых маршрутов нет (${removed.length})`
);
