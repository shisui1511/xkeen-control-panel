// Граф импортов фронтенда для выбора роутерных спеков (используется select-changed.mjs).
//
// Граф строится заново при каждом запуске, поэтому карту «файл → страница → спеки»
// не нужно поддерживать вручную:
//   1. «Оболочка» — всё, что статически достижимо из src/main.ts (App,
//      Dashboard, Sidebar, i18n, стили…). Она рендерится на каждой странице,
//      её изменение запускает все спеки.
//   2. Страницы — ленивые import() в ветках `currentTab === '<маршрут>'`
//      в Dashboard.svelte. Для маршрута dashboard страницей считаются
//      импорты Dashboard.svelte, которые используются только в его ветке.
//   3. Спек привязан к маршрутам, которые он открывает (`#/<маршрут>`,
//      строковые литералы с именем маршрута, переход на '/'), и к
//      маршрутам из комментария `// e2e-pages: a, b`.
//
// Всё, что не удалось однозначно разобрать, трактуется в пользу запуска:
// неизвестный файл во frontend/ → все спеки, спек без маршрутов → всегда.

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const FRONTEND_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..', 'frontend');

// Синонимы маршрутов — повторяют tabFromHash() в frontend/src/lib/tabFromHash.ts
const ROUTE_ALIASES = {
  constructor: 'editor',
  'mihomo-gen': 'editor',
  subscriptions: 'proxies'
};

const RESOLVE_EXTENSIONS = ['', '.ts', '.js', '.svelte', '.svelte.ts', '.svelte.js'];

function listFiles(dir, filter) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...listFiles(full, filter));
    else if (filter(full)) out.push(full);
  }
  return out;
}

function parseImports(source) {
  const staticImports = [];
  const dynamicImports = [];
  // Между import и from не бывает кавычек и `;` — иначе side-effect
  // импорт (`import './x.css'`) склеился бы со следующим импортом
  const staticRe = /(?:^|[;\s])(?:import|export)\s+(?:[^'";]*?\sfrom\s*)?['"]([^'"\n]+)['"]/g;
  const dynamicRe = /import\(\s*['"]([^'"\n]+)['"]\s*\)/g;
  const cssRe = /@import\s+(?:url\()?['"]([^'"\n]+)['"]/g;
  for (const m of source.matchAll(staticRe)) staticImports.push(m[1]);
  for (const m of source.matchAll(cssRe)) staticImports.push(m[1]);
  for (const m of source.matchAll(dynamicRe)) dynamicImports.push(m[1]);
  return { staticImports, dynamicImports };
}

function resolveImport(fromFile, spec) {
  if (!spec.startsWith('.')) return null;
  const clean = spec.replace(/\?.*$/, '');
  const base = path.resolve(path.dirname(fromFile), clean);
  for (const ext of RESOLVE_EXTENSIONS) {
    const candidate = base + ext;
    if (existsSync(candidate) && statSync(candidate).isFile()) return candidate;
  }
  for (const idx of ['index.ts', 'index.js']) {
    const candidate = path.join(base, idx);
    if (existsSync(candidate)) return candidate;
  }
  return null;
}

/** Граф импортов src/: файл → { static: Set, dynamic: Set } */
export function buildGraph(frontendDir = FRONTEND_DIR) {
  const srcDir = path.join(frontendDir, 'src');
  const files = listFiles(srcDir, (f) => /\.(svelte|ts|js|css)$/.test(f) && !/\.test\.ts$/.test(f));
  const graph = new Map();
  for (const file of files) {
    const { staticImports, dynamicImports } = parseImports(readFileSync(file, 'utf8'));
    const resolve = (list) =>
      new Set(list.map((s) => resolveImport(file, s)).filter((f) => f && f.startsWith(srcDir)));
    graph.set(file, { static: resolve(staticImports), dynamic: resolve(dynamicImports) });
  }
  return graph;
}

function reach(graph, starts, { followDynamic, skip = new Set() }) {
  const seen = new Set();
  const stack = [...starts];
  while (stack.length) {
    const f = stack.pop();
    if (seen.has(f) || skip.has(f)) continue;
    seen.add(f);
    const node = graph.get(f);
    if (!node) continue;
    for (const d of node.static) stack.push(d);
    if (followDynamic) for (const d of node.dynamic) stack.push(d);
  }
  return seen;
}

/**
 * Страницы из Dashboard.svelte: маршрут → входные файлы.
 * Для dashboard — статические импорты, используемые только в его ветке.
 */
export function parsePages(frontendDir = FRONTEND_DIR) {
  const dashFile = path.join(frontendDir, 'src', 'Dashboard.svelte');
  const text = readFileSync(dashFile, 'utf8');
  const branchRe = /currentTab === '([a-z][\w-]*)'\}/g;
  const branches = [...text.matchAll(branchRe)].map((m) => ({ route: m[1], start: m.index }));
  const pages = new Map();
  let dashboardRange = null;

  branches.forEach((b, i) => {
    const end = i + 1 < branches.length ? branches[i + 1].start : text.indexOf('</main>', b.start);
    const body = text.slice(b.start, end === -1 ? undefined : end);
    const entries = new Set();
    for (const m of body.matchAll(/import\(\s*['"]([^'"]+)['"]\s*\)/g)) {
      const resolved = resolveImport(dashFile, m[1]);
      if (resolved) entries.add(resolved);
    }
    if (b.route === 'dashboard') dashboardRange = [b.start, end];
    pages.set(b.route, entries);
  });

  // Импорты Dashboard.svelte, чьи имена встречаются только в ветке dashboard
  const dashboardOnly = new Set();
  if (dashboardRange) {
    const importRe = /import\s+(\w+)\s+from\s+['"](\.[^'"]+)['"]/g;
    for (const m of text.matchAll(importRe)) {
      const [stmt, name, spec] = m;
      const usages = [...text.matchAll(new RegExp(`\\b${name}\\b`, 'g'))]
        .map((u) => u.index)
        .filter((idx) => idx < m.index || idx >= m.index + stmt.length);
      if (
        usages.length > 0 &&
        usages.every((idx) => idx >= dashboardRange[0] && idx < dashboardRange[1])
      ) {
        const resolved = resolveImport(dashFile, spec);
        if (resolved) dashboardOnly.add(resolved);
      }
    }
    pages.set('dashboard', dashboardOnly);
  }
  return { pages, dashFile, dashboardOnly };
}

/** Оболочка и файлы каждой страницы */
export function buildModel(frontendDir = FRONTEND_DIR) {
  const graph = buildGraph(frontendDir);
  const { pages, dashboardOnly } = parsePages(frontendDir);
  const mainFile = path.join(frontendDir, 'src', 'main.ts');

  // Оболочка: статически достижимое из main.ts, кроме dashboard-only веток.
  // Файлы, нужные и оболочке, и dashboard, попадут в оболочку через другие пути.
  const shellCore = reach(graph, [mainFile], { followDynamic: false, skip: dashboardOnly });
  const shared = new Set();
  for (const f of dashboardOnly) {
    for (const g of reach(graph, [f], { followDynamic: false })) {
      if (shellCore.has(g)) shared.add(g);
    }
  }

  const pageEntries = new Set([...pages.values()].flatMap((s) => [...s]));
  // Ленивые import() из оболочки, не являющиеся страницами, — тоже оболочка
  const extraShell = [];
  for (const f of shellCore) {
    for (const d of graph.get(f)?.dynamic ?? []) if (!pageEntries.has(d)) extraShell.push(d);
  }
  const shell = new Set([
    ...shellCore,
    ...reach(graph, extraShell, { followDynamic: true, skip: pageEntries })
  ]);

  const pageFiles = new Map();
  for (const [route, entries] of pages) {
    pageFiles.set(route, reach(graph, [...entries], { followDynamic: true }));
  }
  return { graph, shell, pageFiles, routes: new Set(pages.keys()), shared };
}

/** Маршруты, которые открывает спек; null — определить не удалось */
export function specRoutes(specText, knownRoutes) {
  const routes = new Set();
  const norm = (r) => ROUTE_ALIASES[r] ?? r;
  // `#/logs` в URL и `#\/logs` в регулярках toHaveURL(/#\/logs/)
  for (const m of specText.matchAll(/#\\?\/([a-z][\w-]*)/g)) routes.add(norm(m[1]));
  // Литералы с именем маршрута: списки страниц в sweep-спеках и т.п.
  for (const m of specText.matchAll(/['"`]([a-z][\w-]*)['"`]/g)) {
    const r = norm(m[1]);
    if (knownRoutes.has(r)) routes.add(r);
  }
  // Переход на корень без хэша — дашборд (оболочка входит всегда)
  if (/(?:goto|visitPage)\([^)]*?['"`]\/(?:\?[^'"`#]*)?['"`]/.test(specText)) {
    routes.add('dashboard');
  }
  for (const m of specText.matchAll(/\/\/\s*e2e-pages:\s*([^\n]+)/g)) {
    for (const r of m[1].split(',')) routes.add(norm(r.trim()));
  }
  const valid = [...routes].filter((r) => knownRoutes.has(r));
  return valid.length ? new Set(valid) : null;
}
