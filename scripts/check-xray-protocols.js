#!/usr/bin/env node
/*
 * check-xray-protocols.js — набор протоколов, для которых Xray пишет outbound
 * во фрагмент подписки (allowedXrayProtocols в internal/services/subscription.go),
 * равен списку, по которому фронтенд разрешает выбрать узел
 * (XRAY_SELECTABLE_PROTOCOLS в NodeList.svelte). Единого источника у них нет:
 * при расхождении кнопка выбора валидного узла молча заблокирована.
 *
 * Usage: node scripts/check-xray-protocols.js [--root <каталог>]
 */
const fs = require('fs');
const path = require('path');

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf('--root');
const root = rootIdx >= 0 ? path.resolve(argv[rootIdx + 1]) : path.resolve(__dirname, '..');

const BACKEND = 'internal/services/subscription.go';
const FRONTEND = 'frontend/src/components/subscriptions/NodeList.svelte';

function read(rel) {
  try {
    return fs.readFileSync(path.join(root, rel), 'utf8');
  } catch (e) {
    console.error('check-xray-protocols: не удалось прочитать файл: ' + e.message);
    process.exit(1);
  }
}

const backendBlock = (read(BACKEND).match(/allowedXrayProtocols\s*=\s*map\[string\]bool\{([^}]*)\}/) || [])[1] ?? null;
const frontendBlock = (read(FRONTEND).match(/XRAY_SELECTABLE_PROTOCOLS\s*=\s*new Set\(\[([^\]]*)\]/) || [])[1] ?? null;

const errors = [];
if (backendBlock === null) errors.push(`${BACKEND}: не найден блок allowedXrayProtocols`);
if (frontendBlock === null) errors.push(`${FRONTEND}: не найден блок XRAY_SELECTABLE_PROTOCOLS`);
if (errors.length > 0) {
  for (const e of errors) console.error('❌ ' + e);
  process.exit(1);
}

const backend = new Set([...backendBlock.matchAll(/"([^"]+)"\s*:\s*true/g)].map((m) => m[1]));
const frontend = new Set([...frontendBlock.matchAll(/'([^']+)'/g)].map((m) => m[1]));
if (backend.size === 0 || frontend.size === 0) {
  console.error('❌ пустой набор протоколов (бэкенд: ' + backend.size + ', фронтенд: ' + frontend.size + ') — сканер сломан');
  process.exit(1);
}

const onlyBackend = [...backend].filter((p) => !frontend.has(p)).sort();
const onlyFrontend = [...frontend].filter((p) => !backend.has(p)).sort();
if (onlyBackend.length > 0 || onlyFrontend.length > 0) {
  console.error('❌ Протоколы Xray на бэкенде и во фронтенде расходятся:');
  for (const p of onlyBackend) console.error(`  ${p}: есть в allowedXrayProtocols (${BACKEND}), нет в XRAY_SELECTABLE_PROTOCOLS (${FRONTEND})`);
  for (const p of onlyFrontend) console.error(`  ${p}: есть в XRAY_SELECTABLE_PROTOCOLS (${FRONTEND}), нет в allowedXrayProtocols (${BACKEND})`);
  process.exit(1);
}
console.log(`✅ Протоколы Xray бэкенда и фронтенда совпадают (${backend.size})`);
