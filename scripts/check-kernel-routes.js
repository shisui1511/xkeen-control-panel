#!/usr/bin/env node
/*
 * check-kernel-routes.js — таблица политик гейта ядра согласована с
 * регистрацией маршрутов. Без этого маршрут под префиксом ядра не стартует
 * (или остаётся без политики), а запись таблицы без маршрута — мусор.
 *
 * Правила:
 *   1. Каждый HandleProtected("/api/…") из cmd/xcp/main.go под префиксом из
 *      kernelGatedPrefixes имеет ключ в kernelRoutePolicies; для маршрута-
 *      конкатенации ("/api/…/"+действие) достаточно одного ключа с этим префиксом.
 *   2. Каждый ключ kernelRoutePolicies соответствует литералу маршрута или
 *      префиксу конкатенации.
 *   3. srv.SetProtectedWrapper(api.KernelRouteWrapper) стоит раньше первого
 *      HandleProtected с префиксом гейта, иначе гейт маршрут не увидит.
 *
 * Usage: node scripts/check-kernel-routes.js [--root <каталог>]
 */
const fs = require('fs');
const path = require('path');

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf('--root');
const root = rootIdx >= 0 ? path.resolve(argv[rootIdx + 1]) : path.resolve(__dirname, '..');

const MAIN_GO = 'cmd/xcp/main.go';
const GATE_GO = 'internal/handlers/kernel_gate.go';
const POLICY_HINT = 'добавьте запись в kernelRoutePolicies (internal/handlers/kernel_gate.go) с режимом и причиной';

function read(rel) {
  return fs.readFileSync(path.join(root, rel), 'utf8');
}

// Комментарии `//` убираются целиком, чтобы закомментированная регистрация не считалась.
function stripGoLineComments(src) {
  return src.replace(/^[ \t]*\/\/.*$/gm, '');
}

// Тело блока от строки с заголовком до первой строки, состоящей из `}`.
function block(src, header) {
  const start = src.indexOf(header);
  if (start < 0) return null;
  const from = start + header.length;
  const end = src.indexOf('\n}', from);
  return end < 0 ? null : src.slice(from, end);
}

const problems = [];
let mainSrc;
let gateSrc;
try {
  mainSrc = stripGoLineComments(read(MAIN_GO));
  gateSrc = stripGoLineComments(read(GATE_GO));
} catch (e) {
  console.error('check-kernel-routes: не удалось прочитать файл: ' + e.message);
  process.exit(1);
}

const prefixBlock = block(gateSrc, 'var kernelGatedPrefixes = []string{');
const policyBlock = block(gateSrc, 'var kernelRoutePolicies = map[string]KernelRoutePolicy{');
if (prefixBlock === null) problems.push(`${GATE_GO}: не найден блок kernelGatedPrefixes — сканер сломан`);
if (policyBlock === null) problems.push(`${GATE_GO}: не найден блок kernelRoutePolicies — сканер сломан`);

const prefixes = prefixBlock === null ? [] : [...prefixBlock.matchAll(/"([^"]+)"/g)].map((m) => m[1]);
const policyKeys = policyBlock === null ? [] : [...policyBlock.matchAll(/^\s*"(\/api\/[^"]+)"\s*:\s*\{/gm)].map((m) => m[1]);
if (prefixBlock !== null && prefixes.length === 0) problems.push(`${GATE_GO}: kernelGatedPrefixes пуст — сканер сломан`);
if (policyBlock !== null && policyKeys.length === 0) problems.push(`${GATE_GO}: kernelRoutePolicies пуст — сканер сломан`);

const routes = [...mainSrc.matchAll(/HandleProtected\("(\/api\/[^"]+)"(\+)?/g)].map((m) => ({
  pattern: m[1],
  concat: Boolean(m[2]),
  offset: m.index
}));
if (routes.length === 0) problems.push(`${MAIN_GO}: не найдено ни одного HandleProtected с литералом /api/…`);

const isGated = (pattern) => prefixes.some((p) => pattern.startsWith(p));
const keySet = new Set(policyKeys);
const gated = routes.filter((r) => isGated(r.pattern));
if (prefixes.length > 0 && routes.length > 0 && gated.length === 0) {
  problems.push(`${MAIN_GO}: не найдено маршрутов под префиксами ядер — сканер сломан`);
}

// 1. У каждого маршрута гейта есть политика.
for (const r of gated) {
  if (!r.concat) {
    if (!keySet.has(r.pattern)) {
      problems.push(`маршрут ${r.pattern} зарегистрирован под префиксом ядра без политики: ${POLICY_HINT}`);
    }
  } else if (!policyKeys.some((k) => k.startsWith(r.pattern))) {
    problems.push(`для префикса ${r.pattern}+действие нет ни одной записи в таблице: ${POLICY_HINT}`);
  }
}

// 2. Нет устаревших записей.
const literals = new Set(routes.filter((r) => !r.concat).map((r) => r.pattern));
const concatPrefixes = routes.filter((r) => r.concat).map((r) => r.pattern);
for (const key of policyKeys) {
  if (literals.has(key)) continue;
  if (concatPrefixes.some((p) => key.startsWith(p))) continue;
  problems.push(
    `запись таблицы ${key} не соответствует ни одной регистрации в main.go: удалите устаревшую запись из kernelRoutePolicies (internal/handlers/kernel_gate.go)`
  );
}

// 3. Обёртка гейта стоит раньше защищённых маршрутов гейта.
const wrapAt = mainSrc.indexOf('SetProtectedWrapper(api.KernelRouteWrapper)');
if (wrapAt < 0) {
  problems.push(`${MAIN_GO}: нет srv.SetProtectedWrapper(api.KernelRouteWrapper)`);
} else {
  for (const r of gated) {
    if (r.offset < wrapAt) {
      problems.push(`маршрут ${r.pattern} зарегистрирован раньше SetProtectedWrapper: гейт его не увидит`);
    }
  }
}

if (problems.length > 0) {
  console.error('❌ Таблица политик гейта ядра расходится с маршрутами:');
  for (const p of problems) console.error('  ' + p);
  process.exit(1);
}
console.log(`✅ Гейт ядра: маршрутов под префиксами: ${gated.length}, записей политик: ${policyKeys.length}, обёртка зарегистрирована раньше`);
