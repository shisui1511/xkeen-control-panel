// Граф импортов фронтенда для выбора роутерных спеков (используется select-changed.mjs).
//
// Граф строится заново при каждом запуске, поэтому карту «файл → страница → спеки»
// не нужно поддерживать вручную:
//   1. «Оболочка» — всё, что статически достижимо из src/main.ts (App,
//      Dashboard, Sidebar, i18n, стили…). Она рендерится на каждой странице.
//   2. Страницы — ленивые import() в ветках `{:else if currentTab === '<маршрут>' …}`
//      в Dashboard.svelte (ветка может нести дополнительное условие, как у editor и dat).
//      Для маршрута dashboard страницей считаются импорты Dashboard.svelte, которые
//      используются только в его ветке.
//   3. Спек привязан к маршрутам, которые он открывает (`#/<маршрут>`,
//      строковые литералы с именем маршрута, переход на '/'), и к
//      маршрутам из комментария `// e2e-pages: a, b`.
//
// Ревизии (148.1.1, RTR-13). Модель строится из рабочего дерева (fsReader) или из снимка git
// (gitReader: ls-tree и cat-file --batch по ref, содержимое не исполняется). Выбор сравнивает
// модели базы и head, чтобы перенос или удаление импорта не теряли потребителей старой стороны.
// Импорты разрешаются относительно файла и по алиасам compilerOptions.paths из
// frontend/tsconfig.json (с baseUrl); index-файлы каталогов (barrel), реэкспорт `export … from`
// и динамический import() — рёбра графа, циклы обход отсекает. Модель хранит прямые рёбра
// (graph) и обратные (importers).
//
// Всё, что не удалось однозначно разобрать, трактуется в пользу запуска:
// неизвестный файл во frontend/ → все спеки, спек без маршрутов → всегда.

import { execFileSync } from 'node:child_process';
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
const INDEX_FILES = ['index.ts', 'index.js', 'index.svelte.ts', 'index.svelte.js'];
const SOURCE_RE = /\.(svelte|ts|js|css|json)$/;
const GRAPH_RE = /\.(svelte|ts|js|css)$/;

/** Чтение рабочего дерева: абсолютные пути внутри frontendDir. */
export function fsReader(frontendDir = FRONTEND_DIR) {
  const cache = new Map();
  const listFiles = (dir) => {
    const out = [];
    if (!existsSync(dir)) return out;
    for (const entry of readdirSync(dir)) {
      const full = path.join(dir, entry);
      if (statSync(full).isDirectory()) out.push(...listFiles(full));
      else out.push(full);
    }
    return out;
  };
  return {
    name: 'рабочее дерево',
    frontendDir,
    files: (dir) => listFiles(dir),
    isFile: (abs) => existsSync(abs) && statSync(abs).isFile(),
    read(abs) {
      if (!cache.has(abs)) cache.set(abs, existsSync(abs) && statSync(abs).isFile() ? readFileSync(abs, 'utf8') : null);
      return cache.get(abs);
    }
  };
}

/**
 * Чтение снимка git: frontend/src и frontend/tsconfig*.json в ref. Пути — те же абсолютные, что у
 * fsReader (repoRoot/frontend/…), поэтому модели базы и head сопоставимы по ключам. Ошибка git
 * (нет ref, сбой чтения) — исключение: вызывающий расширяет выбор.
 */
export function gitReader(repoRoot, ref) {
  const frontendDir = path.join(repoRoot, 'frontend');
  const run = (args, input) =>
    execFileSync('git', args, {
      cwd: repoRoot,
      input,
      stdio: ['pipe', 'pipe', 'pipe'],
      maxBuffer: 512 * 1024 * 1024
    });
  const names = run(['ls-tree', '-r', '-z', '--name-only', ref, '--', 'frontend/src', 'frontend/tsconfig.json'])
    .toString('utf8')
    .split('\0')
    .filter(Boolean);
  const all = new Set(names.map((n) => path.join(repoRoot, n)));
  const text = new Map();
  const wanted = names.filter((n) => SOURCE_RE.test(n));
  if (wanted.length > 0) {
    const out = run(['cat-file', '--batch'], wanted.map((n) => `${ref}:${n}`).join('\n') + '\n');
    let pos = 0;
    for (const n of wanted) {
      const nl = out.indexOf(0x0a, pos);
      if (nl === -1) throw new Error(`git cat-file: оборван ответ на ${n}`);
      const header = out.subarray(pos, nl).toString('utf8');
      const m = /^[0-9a-f]+ (\w+) (\d+)$/.exec(header);
      if (!m) throw new Error(`git cat-file: ${n} не прочитан (${header})`);
      const size = Number(m[2]);
      text.set(path.join(repoRoot, n), out.subarray(nl + 1, nl + 1 + size).toString('utf8'));
      pos = nl + 1 + size + 1;
    }
  }
  return {
    name: `ревизия ${ref}`,
    frontendDir,
    files: (dir) => [...all].filter((f) => f.startsWith(dir + path.sep)),
    isFile: (abs) => all.has(abs),
    read: (abs) => (text.has(abs) ? text.get(abs) : null)
  };
}

/** JSON с комментариями и висячими запятыми (tsconfig); null — не разобран. */
function parseJsonc(text) {
  if (text === null) return null;
  try {
    return JSON.parse(text);
  } catch {
    // строки не трогаем: комментарий вне строки начинается с // или /*
    const stripped = text
      .replace(/("(?:[^"\\\n]|\\.)*")|\/\/[^\n]*|\/\*[\s\S]*?\*\//g, (m, str) => str ?? '')
      .replace(/,(\s*[}\]])/g, '$1');
    try {
      return JSON.parse(stripped);
    } catch {
      return null;
    }
  }
}

/**
 * Алиасы проекта из frontend/tsconfig.json: compilerOptions.paths относительно baseUrl.
 * Возвращает [{ prefix, exact, targets: [абсолютный префикс] }]. Наследование через extends
 * пакетов (@tsconfig/svelte) путей проекта не задаёт и не читается.
 */
function loadAliases(reader) {
  const cfg = parseJsonc(reader.read(path.join(reader.frontendDir, 'tsconfig.json')));
  const opts = (cfg && cfg.compilerOptions) || {};
  const paths = opts.paths || {};
  const baseDir = path.resolve(reader.frontendDir, opts.baseUrl || '.');
  const out = [];
  for (const [key, targets] of Object.entries(paths)) {
    if (!Array.isArray(targets)) continue;
    const exact = !key.endsWith('*');
    const prefix = exact ? key : key.slice(0, -1);
    out.push({
      prefix,
      exact,
      targets: targets.map((t) => path.resolve(baseDir, exact ? t : t.replace(/\*$/, '')))
    });
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

function resolveBase(reader, base) {
  for (const ext of RESOLVE_EXTENSIONS) {
    const candidate = base + ext;
    if (reader.isFile(candidate)) return candidate;
  }
  for (const idx of INDEX_FILES) {
    const candidate = path.join(base, idx);
    if (reader.isFile(candidate)) return candidate;
  }
  return null;
}

function resolveImport(reader, aliases, fromFile, spec) {
  const clean = spec.replace(/\?.*$/, '');
  if (clean.startsWith('.')) return resolveBase(reader, path.resolve(path.dirname(fromFile), clean));
  for (const a of aliases) {
    if (a.exact ? clean !== a.prefix : !clean.startsWith(a.prefix)) continue;
    for (const t of a.targets) {
      const hit = resolveBase(reader, a.exact ? t : t + clean.slice(a.prefix.length));
      if (hit) return hit;
    }
  }
  return null;
}

/** Граф импортов src/: файл → { static: Set, dynamic: Set } */
export function buildGraph(frontendDir = FRONTEND_DIR, reader = fsReader(frontendDir), aliases = loadAliases(reader)) {
  const srcDir = path.join(frontendDir, 'src');
  const files = reader.files(srcDir).filter((f) => GRAPH_RE.test(f) && !/\.test\.ts$/.test(f));
  const graph = new Map();
  for (const file of files) {
    const { staticImports, dynamicImports } = parseImports(reader.read(file) ?? '');
    const resolve = (list) =>
      new Set(
        list
          .map((s) => resolveImport(reader, aliases, file, s))
          .filter((f) => f && f.startsWith(srcDir + path.sep))
      );
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
export function parsePages(frontendDir = FRONTEND_DIR, reader = fsReader(frontendDir), aliases = loadAliases(reader)) {
  const dashFile = path.join(frontendDir, 'src', 'Dashboard.svelte');
  const text = reader.read(dashFile);
  if (text === null) throw new Error(`${reader.name}: нет src/Dashboard.svelte — маршруты не определить`);
  // Ветка маршрута: `{:else if currentTab === 'x'}` или с дополнительным условием
  // (`currentTab === 'editor' && !(…)}`)
  const branchRe = /\{(?:#if|:else if)\s+currentTab === '([a-z][\w-]*)'/g;
  const branches = [...text.matchAll(branchRe)].map((m) => ({ route: m[1], start: m.index }));
  const pages = new Map();
  let dashboardRange = null;

  branches.forEach((b, i) => {
    const end = i + 1 < branches.length ? branches[i + 1].start : text.indexOf('</main>', b.start);
    const body = text.slice(b.start, end === -1 ? undefined : end);
    const entries = pages.get(b.route) ?? new Set();
    for (const m of body.matchAll(/import\(\s*['"]([^'"]+)['"]\s*\)/g)) {
      const resolved = resolveImport(reader, aliases, dashFile, m[1]);
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
        const resolved = resolveImport(reader, aliases, dashFile, spec);
        if (resolved) dashboardOnly.add(resolved);
      }
    }
    pages.set('dashboard', dashboardOnly);
  }
  return { pages, dashFile, dashboardOnly };
}

/**
 * Вкладки страницы: Map вкладка → Set входных файлов её компонентов; null — вкладок нет.
 * Компонент относится к вкладке, если его тег стоит в ветке `{#if|:else if subTab === 'x'}`
 * (до закрытия `{/if}`) или сам тег содержит `subTab === 'x'` (FilesTab active={…}). Компоненты
 * вне веток — общие для всех вкладок и в Map не входят.
 */
function parseSubTabs(reader, aliases, pageFile) {
  const text = reader.read(pageFile);
  if (text === null) return null;
  const openers = [...text.matchAll(/\{(?:#if|:else if)\s+subTab === '([a-z][\w-]*)'\s*\}/g)].map((m) => ({
    tab: m[1],
    at: m.index
  }));
  if (openers.length === 0) return null;
  const tabs = new Map();
  for (const m of text.matchAll(/import\s+([A-Z]\w*)\s+from\s+['"](\.[^'"]+)['"]/g)) {
    const [, name, spec] = m;
    const file = resolveImport(reader, aliases, pageFile, spec);
    if (!file) continue;
    for (const u of text.matchAll(new RegExp(`<${name}\\b[^>]*>`, 'g'))) {
      let tab = /subTab === '([a-z][\w-]*)'/.exec(u[0])?.[1] ?? null;
      if (!tab) {
        const prev = openers.filter((o) => o.at < u.index).pop();
        if (prev && !text.slice(prev.at, u.index).includes('{/if}')) tab = prev.tab;
      }
      if (!tab) continue;
      if (!tabs.has(tab)) tabs.set(tab, new Set());
      tabs.get(tab).add(file);
    }
  }
  return tabs;
}

/**
 * Оболочка и файлы каждой страницы.
 * opts.reader — источник файлов (по умолчанию рабочее дерево frontendDir).
 */
export function buildModel(frontendDir = FRONTEND_DIR, opts = {}) {
  const reader = opts.reader ?? fsReader(frontendDir);
  const aliases = loadAliases(reader);
  const graph = buildGraph(frontendDir, reader, aliases);
  const { pages, dashboardOnly } = parsePages(frontendDir, reader, aliases);
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
    // Страница с вкладками (`{:else if subTab === 'x'}`, Config.svelte) делится на маршруты
    // `<маршрут>/<вкладка>`: компоненты чужих вкладок в обход вкладки не входят
    const tabs = [...entries].map((e) => parseSubTabs(reader, aliases, e)).find((t) => t && t.size > 0);
    if (!tabs) {
      pageFiles.set(route, reach(graph, [...entries], { followDynamic: true }));
      continue;
    }
    const all = new Set([...tabs.values()].flatMap((s) => [...s]));
    for (const [tab, own] of tabs) {
      const skip = new Set([...all].filter((f) => !own.has(f)));
      pageFiles.set(`${route}/${tab}`, reach(graph, [...entries], { followDynamic: true, skip }));
    }
  }

  // Обратные рёбра: файл → прямые импортёры (статические и динамические)
  const importers = new Map();
  for (const [f, node] of graph) {
    for (const d of [...node.static, ...node.dynamic]) {
      if (!importers.has(d)) importers.set(d, new Set());
      importers.get(d).add(f);
    }
  }

  /** Маршруты, на которых файл достижим. */
  const routesOf = (abs) => [...pageFiles].filter(([, files]) => files.has(abs)).map(([r]) => r);

  // routes — маршруты обхода (вкладки как `config/<вкладка>`), topRoutes — страницы Dashboard
  return {
    graph,
    importers,
    shell,
    pageFiles,
    routes: new Set(pageFiles.keys()),
    topRoutes: new Set(pages.keys()),
    shared,
    reader,
    routesOf
  };
}

/** Страница маршрута: `config/files` → `config`. */
export function topRoute(route) {
  return route.split('/', 1)[0];
}

/**
 * Маршруты, которые открывает спек; null — определить не удалось.
 * Вкладка (`#/config/cores`, `e2e-pages: config/cores`) — свой маршрут; страница без вкладки
 * (`#/config`, литерал 'config', `#/config/${…}`) — все её вкладки: спек может переключать их.
 */
export function specRoutes(specText, knownRoutes) {
  const routes = new Set();
  const norm = (r) => ROUTE_ALIASES[r] ?? r;
  // `#/logs` и `#/config/cores` в URL, `#\/logs` и `#\/config\/cores` в регулярках toHaveURL
  for (const m of specText.matchAll(/#\\?\/([a-z][\w-]*)(?:\\?\/([a-z][\w-]*))?/g)) {
    routes.add(m[2] ? `${norm(m[1])}/${m[2]}` : norm(m[1]));
  }
  // Литералы с именем маршрута: списки страниц в sweep-спеках и т.п.
  for (const m of specText.matchAll(/['"`]([a-z][\w-]*)['"`]/g)) {
    const r = norm(m[1]);
    if (knownRoutes.has(r) || [...knownRoutes].some((k) => k.startsWith(`${r}/`))) routes.add(r);
  }
  // Переход на корень без хэша — дашборд (оболочка входит всегда)
  if (/(?:goto|visitPage)\([^)]*?['"`]\/(?:\?[^'"`#]*)?['"`]/.test(specText)) {
    routes.add('dashboard');
  }
  // Аннотация: маршруты через запятую или пробел
  for (const m of specText.matchAll(/\/\/\s*e2e-pages:\s*([^\n]+)/g)) {
    for (const r of m[1].split(/[,\s]+/).filter(Boolean)) {
      const [top, tab] = r.split('/');
      routes.add(tab ? `${norm(top)}/${tab}` : norm(top));
    }
  }
  const valid = new Set();
  for (const r of routes) {
    if (knownRoutes.has(r)) {
      valid.add(r);
      continue;
    }
    // Страница без вкладки или незнакомая вкладка — все вкладки страницы (или сама страница)
    const top = topRoute(r);
    if (knownRoutes.has(top)) valid.add(top);
    else for (const k of knownRoutes) if (k.startsWith(`${top}/`)) valid.add(k);
  }
  return valid.size ? valid : null;
}

/**
 * Файлы frontend/src, на которые спек ссылается напрямую: относительные импорты и
 * `new URL('…', import.meta.url)` самого спека и его помощников из tests/router (транзитивно
 * по помощникам, не по src). Шаблон `${…}` в пути — любой сегмент. srcFiles — абсолютные пути
 * файлов src (для шаблонов). Возвращает Set абсолютных путей.
 */
export function specSources(specAbs, srcFiles, frontendDir = FRONTEND_DIR) {
  const reader = fsReader(frontendDir);
  const srcDir = path.join(frontendDir, 'src') + path.sep;
  const testsDir = path.join(frontendDir, 'tests', 'router') + path.sep;
  const out = new Set();
  const seen = new Set();
  const stack = [specAbs];
  while (stack.length) {
    const file = stack.pop();
    if (seen.has(file)) continue;
    seen.add(file);
    const text = reader.read(file);
    if (text === null) continue;
    const refs = [];
    for (const m of text.matchAll(/(?:^|[;\s])(?:import|export)\s+(?:[^'";]*?\sfrom\s*)?['"]([^'"\n]+)['"]/g)) refs.push(m[1]);
    for (const m of text.matchAll(/import\(\s*['"]([^'"\n]+)['"]\s*\)/g)) refs.push(m[1]);
    for (const m of text.matchAll(/new URL\(\s*(['"`])([^'"`\n]+)\1\s*,\s*import\.meta\.url/g)) refs.push(m[2]);
    for (const ref of refs) {
      if (!ref.startsWith('.')) continue;
      if (ref.includes('${')) {
        const abs = path.resolve(path.dirname(file), ref.replace(/\$\{[^}]*\}/g, '\u0000'));
        const re = new RegExp(
          '^' + abs.split('\u0000').map((p) => p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('[^/]+') + '$'
        );
        for (const f of srcFiles) if (re.test(f)) out.add(f);
        continue;
      }
      const hit = resolveBase(reader, path.resolve(path.dirname(file), ref));
      if (!hit) continue;
      if (hit.startsWith(srcDir)) out.add(hit);
      else if (hit.startsWith(testsDir)) stack.push(hit);
    }
  }
  return out;
}
