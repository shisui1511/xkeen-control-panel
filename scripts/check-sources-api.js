#!/usr/bin/env node
/*
 * check-sources-api.js — статическая согласованность API источников.
 *
 * 1. Контракт ключей ответов: frontend/src/lib/sourcesContract.json совпадает с
 *    json-тегами Go-типов (internal/services/sources/preview.go, refresh.go,
 *    internal/nodes/errors.go, capability.go): обязательные = теги без omitempty,
 *    необязательные = с omitempty, вложенные типы (children) выводятся из типов
 *    полей. Тот же файл читают роутерные проверки ответов панели и типы фронтенда.
 * 2. Проводка: двенадцать маршрутов /api/configlayer/sources* зарегистрированы в
 *    cmd/xcp/main.go через HandleProtected, у каждого есть обработчик, маршруты не
 *    входят в kernelGatedPrefixes; генераторы, хук фиксации и колбэк «дрейф решён»
 *    регистрируются до configLayer.Start(); Stop сервиса вызывается через defer
 *    раньше Stop слоя; установка ядра запускает и слой, и сервис источников.
 *
 * Usage: node scripts/check-sources-api.js [--root <каталог>]
 */
const fs = require('fs');
const path = require('path');

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf('--root');
const root = rootIdx >= 0 ? path.resolve(argv[rootIdx + 1]) : path.resolve(__dirname, '..');

const errors = [];

function read(rel) {
  try {
    return fs.readFileSync(path.join(root, rel), 'utf8');
  } catch (e) {
    console.error('check-sources-api: не удалось прочитать файл: ' + e.message);
    process.exit(1);
  }
}

// --- 1. Контракт ключей ------------------------------------------------------

const CONTRACT = 'frontend/src/lib/sourcesContract.json';
const GO_FILES = [
  'internal/services/sources/preview.go',
  'internal/services/sources/refresh.go',
  'internal/nodes/errors.go',
  'internal/nodes/capability.go'
];

let contract = null;
try {
  contract = JSON.parse(read(CONTRACT));
} catch (e) {
  errors.push(`${CONTRACT}: не разбирается как JSON: ${e.message}`);
}

// Go-структуры: имя → [{ name, type, tag, omit }].
const structs = new Map();
for (const file of GO_FILES) {
  const src = read(file);
  for (const m of src.matchAll(/^type (\w+) struct \{\n([\s\S]*?)\n\}/gm)) {
    const fields = [];
    for (const line of m[2].split('\n')) {
      const f = line.match(/^\s*(\w+)\s+(\S+)\s+`[^`]*json:"([^",]+)(,[^"]*)?"[^`]*`/);
      if (!f) continue;
      fields.push({ name: f[1], type: f[2], tag: f[3], omit: (f[4] || '').includes('omitempty') });
    }
    structs.set(m[1], fields);
  }
}

// Тип поля → «Тип», «Тип[]», «Тип{}» (карта по ключам) или null для скаляров.
function childType(goType, known) {
  let t = goType;
  let suffix = '';
  if (t.startsWith('[]')) {
    suffix = '[]';
    t = t.slice(2);
  } else if (/^map\[string\]/.test(t)) {
    suffix = '{}';
    t = t.replace(/^map\[string\]/, '');
  }
  t = t.replace(/^\*/, '').replace(/^\w+\./, '');
  return known.has(t) ? t + suffix : null;
}

if (contract !== null) {
  const known = new Set(Object.keys(contract));
  for (const [name, spec] of Object.entries(contract)) {
    const fields = structs.get(name);
    if (!fields) {
      errors.push(`${CONTRACT}: тип ${name} не найден в Go (${GO_FILES.join(', ')})`);
      continue;
    }
    const required = fields.filter((f) => !f.omit).map((f) => f.tag);
    const optional = fields.filter((f) => f.omit).map((f) => f.tag);
    const same = (a, b) => JSON.stringify([...a].sort()) === JSON.stringify([...b].sort());
    if (!same(required, spec.required || [])) {
      errors.push(`${CONTRACT}: ${name}.required = [${spec.required}], в Go [${required}]`);
    }
    if (!same(optional, spec.optional || [])) {
      errors.push(`${CONTRACT}: ${name}.optional = [${spec.optional}], в Go [${optional}]`);
    }
    const children = {};
    for (const f of fields) {
      const c = childType(f.type, known);
      if (c) children[f.tag] = c;
    }
    const declared = spec.children || {};
    if (!same(Object.keys(children), Object.keys(declared))) {
      errors.push(`${CONTRACT}: ${name}.children — ключи [${Object.keys(declared)}], в Go [${Object.keys(children)}]`);
    } else {
      for (const [k, v] of Object.entries(children)) {
        if (declared[k] !== v) errors.push(`${CONTRACT}: ${name}.children.${k} = ${declared[k]}, в Go ${v}`);
      }
    }
  }
}

// --- 2. Проводка -------------------------------------------------------------

const ROUTES = {
  '/api/configlayer/sources': 'ConfigSources',
  '/api/configlayer/sources/preview': 'ConfigSourcesPreview',
  '/api/configlayer/sources/add': 'ConfigSourcesAdd',
  '/api/configlayer/sources/update': 'ConfigSourcesUpdate',
  '/api/configlayer/sources/delete': 'ConfigSourcesDelete',
  '/api/configlayer/sources/refresh': 'ConfigSourcesRefresh',
  '/api/configlayer/sources/reveal': 'ConfigSourcesReveal',
  '/api/configlayer/sources/diagnostics': 'ConfigSourcesDiagnostics',
  '/api/configlayer/sources/nodes/preview': 'ConfigSourcesNodePreview',
  '/api/configlayer/sources/nodes/raw': 'ConfigSourcesNodeRaw',
  '/api/configlayer/sources/nodes/save': 'ConfigSourcesNodeSave',
  '/api/configlayer/sources/nodes/delete': 'ConfigSourcesNodeDelete'
};

const MAIN = 'cmd/xcp/main.go';
const main = read(MAIN);
const mainLines = main.split('\n');
const handlers = read('internal/handlers/sources.go');

for (const [route, fn] of Object.entries(ROUTES)) {
  const want = `srv.HandleProtected("${route}", api.${fn})`;
  if (!main.includes(want)) errors.push(`${MAIN}: нет ${want}`);
  if (!new RegExp(`func \\(a \\*API\\) ${fn}\\(w http\\.ResponseWriter, r \\*http\\.Request\\)`).test(handlers)) {
    errors.push(`internal/handlers/sources.go: нет обработчика ${fn}`);
  }
}
for (const m of main.matchAll(/srv\.(Handle|HandlePublic)\("(\/api\/configlayer\/sources[^"]*)"/g)) {
  errors.push(`${MAIN}: ${m[2]} зарегистрирован через ${m[1]}, нужен HandleProtected`);
}

const gate = read('internal/handlers/kernel_gate.go');
if (gate.includes('/api/configlayer/sources')) {
  errors.push('internal/handlers/kernel_gate.go: маршруты источников не должны входить в kernelGatedPrefixes');
}

// lineOf — номер первой строки, содержащей подстроку (0 — не найдена).
function lineOf(sub) {
  const i = mainLines.findIndex((l) => l.includes(sub));
  return i < 0 ? 0 : i + 1;
}
const need = {
  new: lineOf('sources.New('),
  register: lineOf('RegisterGenerator('),
  committer: lineOf('SetLiveCommitter('),
  drift: lineOf('OnDriftResolved('),
  layerStart: lineOf('configLayer.Start()'),
  svcStart: lineOf('sourcesSvc.Start()'),
  layerStop: lineOf('defer configLayer.Stop()'),
  svcStop: lineOf('defer sourcesSvc.Stop()')
};
for (const [k, v] of Object.entries(need)) {
  if (v === 0) errors.push(`${MAIN}: не найдено «${k}»`);
}
if (Object.values(need).every((v) => v > 0)) {
  const order = (a, b, why) => {
    if (!(need[a] < need[b])) errors.push(`${MAIN}: ${why}`);
  };
  order('new', 'register', 'sources.New( должен идти до RegisterGenerator(');
  order('register', 'layerStart', 'RegisterGenerator( должен идти до configLayer.Start()');
  order('committer', 'layerStart', 'SetLiveCommitter( должен идти до configLayer.Start()');
  order('drift', 'layerStart', 'OnDriftResolved( должен идти до configLayer.Start()');
  order('layerStart', 'svcStart', 'sourcesSvc.Start() должен идти после configLayer.Start()');
  order('layerStop', 'svcStop', 'defer sourcesSvc.Stop() должен идти после defer configLayer.Stop() (сервис останавливается раньше слоя)');
}

const kernel = read('internal/handlers/kernel.go');
const hookLayer = kernel.indexOf('a.configLayer.OnKernelInstalled(');
const hookSrc = kernel.indexOf('a.sources.OnKernelInstalled(');
if (hookLayer < 0 || hookSrc < 0 || hookSrc < hookLayer) {
  errors.push('internal/handlers/kernel.go: onKernelInstallDone должен вызывать a.configLayer.OnKernelInstalled( и затем a.sources.OnKernelInstalled(');
}

if (errors.length > 0) {
  for (const e of errors) console.error('❌ ' + e);
  process.exit(1);
}
console.log(`✅ API источников согласован: типов контракта ${contract ? Object.keys(contract).length : 0}, маршрутов ${Object.keys(ROUTES).length}`);
