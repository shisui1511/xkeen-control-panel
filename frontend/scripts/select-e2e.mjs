#!/usr/bin/env node
// Выбор e2e-спеков по изменённым файлам.
//
// Граф импортов фронтенда строится заново при каждом запуске, поэтому
// карту «файл → страница → спеки» не нужно поддерживать вручную:
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
//
// Использование:
//   node scripts/select-e2e.mjs --base origin/main            # список спеков
//   node scripts/select-e2e.mjs --base origin/main --explain  # с объяснением
//   node scripts/select-e2e.mjs --files src/Logs.svelte       # явный список
//   node scripts/select-e2e.mjs --base origin/main --run      # запустить playwright
//   node scripts/select-e2e.mjs --base <sha> --head <sha> --github-output
//   node scripts/select-e2e.mjs --all --github-output         # все спеки

import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync, statSync, appendFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const FRONTEND_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const REPO_DIR = path.resolve(FRONTEND_DIR, '..');

// Файлы вне src/, изменение которых меняет поведение всех e2e
const ALL_PATTERNS = [
  /^package(-lock)?\.json$/,
  /^playwright\.config\.ts$/,
  /^vite\.config\.ts$/,
  /^svelte\.config\.js$/,
  /^tsconfig[\w.-]*\.json$/,
  /^index\.html$/,
  /^public\//,
  /^tests\/helpers\//,
  /^scripts\/select-e2e\.mjs$/
];

// Файлы frontend/, которые не влияют на e2e
const IGNORE_PATTERNS = [
  /\.md$/,
  /^src\/.*\.test\.ts$/,
  /^tests\/.*\.test\.ts$/,
  /^scripts\//,
  /^\.prettier/,
  /^eslint\.config\./,
  /^\.gitignore$/
];

// Файлы вне frontend/, изменение которых меняет e2e
const REPO_ALL_PATTERNS = [/^\.github\/workflows\/ci\.yml$/];

// Синонимы маршрутов — повторяют getTabFromHash() в Dashboard.svelte
const ROUTE_ALIASES = {
  constructor: 'editor',
  'mihomo-gen': 'editor',
  subscriptions: 'proxies'
};

const RESOLVE_EXTENSIONS = ['', '.ts', '.js', '.svelte', '.svelte.ts', '.svelte.js'];

function toPosix(p) {
  return p.split(path.sep).join('/');
}

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

/**
 * Решение по списку изменённых файлов (пути от корня репозитория).
 * Возвращает { mode: 'none' | 'some' | 'all', specs, reasons }.
 */
export function selectSpecs(changedFiles, frontendDir = FRONTEND_DIR) {
  const reasons = [];
  const all = (why) => ({ mode: 'all', specs: allSpecs(frontendDir), reasons: [...reasons, why] });

  const frontendRel = toPosix(path.relative(REPO_DIR, frontendDir));
  const touchedSpecs = new Set();
  const touchedSrc = [];

  for (const file of changedFiles) {
    if (REPO_ALL_PATTERNS.some((re) => re.test(file))) return all(`${file}: конфигурация CI`);
    if (!file.startsWith(frontendRel + '/')) continue;
    const rel = file.slice(frontendRel.length + 1);
    if (/^tests\/[\w.-]+\.spec\.ts$/.test(rel)) {
      if (existsSync(path.join(frontendDir, rel))) touchedSpecs.add(rel);
      continue;
    }
    if (IGNORE_PATTERNS.some((re) => re.test(rel))) continue;
    if (ALL_PATTERNS.some((re) => re.test(rel))) return all(`${rel}: влияет на все e2e`);
    if (rel.startsWith('src/')) {
      touchedSrc.push(rel);
      continue;
    }
    return all(`${rel}: неизвестный файл фронтенда`);
  }

  const model = buildModel(frontendDir);
  const affectedRoutes = new Set();
  for (const rel of touchedSrc) {
    const abs = path.join(frontendDir, rel);
    if (!existsSync(abs)) {
      // Удалённый файл: кто его импортировал, тот тоже изменён в этом диффе
      reasons.push(`${rel}: удалён`);
      continue;
    }
    if (model.shell.has(abs))
      return all(`${rel}: оболочка приложения (рендерится на всех страницах)`);
    const routes = [...model.pageFiles].filter(([, files]) => files.has(abs)).map(([r]) => r);
    if (routes.length === 0) {
      reasons.push(`${rel}: не импортируется приложением`);
      continue;
    }
    routes.forEach((r) => affectedRoutes.add(r));
    reasons.push(`${rel}: страницы ${routes.join(', ')}`);
  }

  const selected = new Set(touchedSpecs);
  for (const s of touchedSpecs) reasons.push(`${s}: изменён сам спек`);
  for (const spec of allSpecs(frontendDir)) {
    const routes = specRoutes(readFileSync(path.join(frontendDir, spec), 'utf8'), model.routes);
    if (routes === null) {
      if (affectedRoutes.size || touchedSpecs.size) {
        selected.add(spec);
        reasons.push(`${spec}: маршруты не определены — запускается всегда`);
      }
      continue;
    }
    if ([...routes].some((r) => affectedRoutes.has(r))) selected.add(spec);
  }

  const specs = [...selected].sort();
  return { mode: specs.length ? 'some' : 'none', specs, reasons };
}

export function allSpecs(frontendDir = FRONTEND_DIR) {
  return (
    readdirSync(path.join(frontendDir, 'tests'))
      // Только безопасные имена: список уходит в командную строку CI
      .filter((f) => /^[\w.-]+\.spec\.ts$/.test(f))
      .map((f) => `tests/${f}`)
      .sort()
  );
}

function changedFromGit(base, head) {
  const range = head ? `${base}...${head}` : base;
  const out = execFileSync('git', ['diff', '--name-only', range], {
    cwd: REPO_DIR,
    encoding: 'utf8'
  });
  const files = out.split('\n').filter(Boolean);
  if (!head) {
    // Локальный режим: плюс незакоммиченные и новые файлы
    const extra = execFileSync('git', ['status', '--porcelain'], {
      cwd: REPO_DIR,
      encoding: 'utf8'
    })
      .split('\n')
      .filter(Boolean)
      .map((l) => l.slice(3).replace(/^.* -> /, ''));
    files.push(...extra);
  }
  return [...new Set(files)];
}

function main(argv) {
  const args = {
    base: null,
    head: null,
    files: null,
    all: false,
    explain: false,
    run: false,
    gh: false
  };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--base') args.base = argv[++i];
    else if (a === '--head') args.head = argv[++i];
    else if (a === '--files') args.files = argv[++i].split(',').filter(Boolean);
    else if (a === '--all') args.all = true;
    else if (a === '--explain') args.explain = true;
    else if (a === '--run') args.run = true;
    else if (a === '--github-output') args.gh = true;
    else {
      console.error(`Неизвестный аргумент: ${a}`);
      process.exit(2);
    }
  }

  let changed;
  let result;
  try {
    if (args.all) throw new Error('запрошен полный прогон (--all)');
    changed = args.files
      ? args.files.map((f) => path.posix.normalize(f.startsWith('frontend/') ? f : `frontend/${f}`))
      : changedFromGit(args.base ?? 'origin/main', args.head);
    result = selectSpecs(changed);
  } catch (e) {
    // Не смогли посчитать — безопасный вариант: все спеки
    result = { mode: 'all', specs: allSpecs(), reasons: [e.message] };
  }

  const total = allSpecs().length;
  const summary =
    result.mode === 'none'
      ? 'e2e: изменения не затрагивают фронтенд — спеки не запускаются'
      : `e2e: ${result.specs.length} из ${total} спеков (${result.mode === 'all' ? 'все' : 'выборочно'})`;

  if (args.explain || args.gh) {
    console.error(summary);
    for (const r of result.reasons) console.error(`  - ${r}`);
  }

  if (args.gh) {
    // Шардов столько, чтобы на каждый приходилось ~8 спеков, но не больше 4
    const shards = result.mode === 'none' ? 0 : Math.min(4, Math.ceil(result.specs.length / 8));
    const out = process.env.GITHUB_OUTPUT;
    const lines = [
      `mode=${result.mode}`,
      `specs=${result.specs.join(' ')}`,
      `shards=${JSON.stringify(Array.from({ length: shards }, (_, i) => i + 1))}`,
      `shard_total=${shards}`
    ];
    if (out) appendFileSync(out, lines.join('\n') + '\n');
    else console.log(lines.join('\n'));
    const stepSummary = process.env.GITHUB_STEP_SUMMARY;
    if (stepSummary) {
      const body = [
        `### ${summary}`,
        '',
        ...result.reasons.map((r) => `- ${r}`),
        '',
        result.mode === 'some' ? result.specs.map((s) => `- \`${s}\``).join('\n') : ''
      ];
      appendFileSync(stepSummary, body.join('\n') + '\n');
    }
    return;
  }

  if (args.run) {
    if (result.mode === 'none') {
      console.error(summary);
      return;
    }
    const res = spawnSync('npx', ['playwright', 'test', ...result.specs], {
      cwd: FRONTEND_DIR,
      stdio: 'inherit'
    });
    process.exit(res.status ?? 1);
  }

  console.log(result.specs.join('\n'));
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2));
}
