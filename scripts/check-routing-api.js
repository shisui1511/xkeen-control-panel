#!/usr/bin/env node
/*
 * check-routing-api.js — статическая согласованность API маршрутизации.
 *
 * 1. Контракт ключей ответов: frontend/src/lib/routingContract.json совпадает с
 *    json-тегами Go-типов (internal/services/routing/*.go, routing/lists/catalog.go,
 *    internal/services/configlayer/takeover.go и view.go): обязательные = теги без
 *    omitempty, необязательные = с omitempty, вложенные типы (children) выводятся из
 *    типов полей, встроенные структуры (Group в GroupView, Rule в RuleView)
 *    разворачиваются. Тот же файл читают роутерные проверки ответов панели и типы
 *    фронтенда.
 * 2. Проводка: каждый маршрут /api/configlayer/routing* из списка зарегистрирован в
 *    cmd/xcp/main.go через HandleProtected, у него есть обработчик в
 *    internal/handlers/routing.go, маршруты не входят в kernelGatedPrefixes;
 *    генераторы и проверка сервиса маршрутизации регистрируются до
 *    configLayer.Start(), routingSvc.Start() идёт после него, шаг остановки — после
 *    шага остановки слоя (defer: сервис останавливается раньше слоя). Сервис
 *    маршрутизации создаётся только в main.go.
 * 3. Коды ошибок: каждый код из frontend/src/lib/routingErrorCodes.json встречается
 *    в internal/handlers/routing.go и имеет ключ configlayer.routing.<код> в обоих
 *    бэкенд-словарях.
 * 4. Поля Xray условий: таблица xrayCondFields (validate.go, бэкенд отвергает
 *    два условия одного поля, cond_conflict) совпадает с COND_XRAY_FIELD
 *    (frontend/src/lib/routingView.ts, редактор не даёт второй тип того же поля)
 *    и покрывает все типы условий модели (Cond* в model.go).
 *
 * Usage: node scripts/check-routing-api.js [--root <каталог>]
 */
const fs = require("fs");
const path = require("path");

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf("--root");
const root =
  rootIdx >= 0
    ? path.resolve(argv[rootIdx + 1])
    : path.resolve(__dirname, "..");

const errors = [];

function read(rel) {
  try {
    return fs.readFileSync(path.join(root, rel), "utf8");
  } catch (e) {
    console.error("check-routing-api: не удалось прочитать файл: " + e.message);
    process.exit(1);
  }
}

// --- 1. Контракт ключей ------------------------------------------------------

const CONTRACT = "frontend/src/lib/routingContract.json";
// Go-файлы типов ответов. Файлы, которых ещё нет (их добавят следующие планы),
// пропускаются; тип контракта, не найденный ни в одном файле, — ошибка.
const GO_FILES = [
  "internal/services/routing/model.go",
  "internal/services/routing/view.go",
  "internal/services/routing/graph.go",
  "internal/services/routing/resolve.go",
  "internal/services/routing/validate.go",
  "internal/services/routing/presets.go",
  "internal/services/routing/selection.go",
  "internal/services/routing/lists_api.go",
  "internal/services/routing/lists/catalog.go",
  "internal/services/routing/lists/geodata.go",
  "internal/services/configlayer/takeover.go",
  "internal/services/configlayer/view.go",
];

let contract = null;
try {
  contract = JSON.parse(read(CONTRACT));
} catch (e) {
  errors.push(`${CONTRACT}: не разбирается как JSON: ${e.message}`);
}

// Go-структуры: имя → { fields: [{ name, type, tag, omit }], embedded: [имя] }.
const structs = new Map();
for (const file of GO_FILES) {
  if (!fs.existsSync(path.join(root, file))) continue;
  const src = read(file);
  for (const m of src.matchAll(/^type (\w+) struct \{\n([\s\S]*?)\n\}/gm)) {
    const fields = [];
    const embedded = [];
    for (const line of m[2].split("\n")) {
      const f = line.match(
        /^\s*(\w+)\s+(\S+)\s+`[^`]*json:"([^",]+)(,[^"]*)?"[^`]*`/,
      );
      if (f) {
        if (f[3] !== "-")
          fields.push({
            name: f[1],
            type: f[2],
            tag: f[3],
            omit: (f[4] || "").includes("omitempty"),
          });
        continue;
      }
      const e = line.match(/^\s*\*?(\w+)\s*(\/\/.*)?$/);
      if (e) embedded.push(e[1]);
    }
    structs.set(m[1], { fields, embedded });
  }
}

// allFields разворачивает встроенные структуры (поля встроенного идут первыми).
function allFields(name, seen = new Set()) {
  const s = structs.get(name);
  if (!s || seen.has(name)) return null;
  seen.add(name);
  const out = [];
  for (const e of s.embedded) {
    const inner = allFields(e, seen);
    if (inner) out.push(...inner);
  }
  out.push(...s.fields);
  return out;
}

// Тип поля → «Тип», «Тип[]», «Тип{}» (карта по ключам) или null для скаляров.
function childType(goType, known) {
  let t = goType;
  let suffix = "";
  if (t.startsWith("[]")) {
    suffix = "[]";
    t = t.slice(2);
  } else if (/^map\[string\]/.test(t)) {
    suffix = "{}";
    t = t.replace(/^map\[string\]/, "");
  }
  t = t.replace(/^\*/, "").replace(/^\w+\./, "");
  return known.has(t) ? t + suffix : null;
}

if (contract !== null) {
  const known = new Set(Object.keys(contract));
  for (const [name, spec] of Object.entries(contract)) {
    const fields = allFields(name);
    if (!fields) {
      errors.push(
        `${CONTRACT}: тип ${name} не найден в Go (${GO_FILES.join(", ")})`,
      );
      continue;
    }
    const required = fields.filter((f) => !f.omit).map((f) => f.tag);
    const optional = fields.filter((f) => f.omit).map((f) => f.tag);
    const same = (a, b) =>
      JSON.stringify([...a].sort()) === JSON.stringify([...b].sort());
    if (!same(required, spec.required || [])) {
      errors.push(
        `${CONTRACT}: ${name}.required = [${spec.required}], в Go [${required}]`,
      );
    }
    if (!same(optional, spec.optional || [])) {
      errors.push(
        `${CONTRACT}: ${name}.optional = [${spec.optional}], в Go [${optional}]`,
      );
    }
    const children = {};
    for (const f of fields) {
      const c = childType(f.type, known);
      if (c) children[f.tag] = c;
    }
    const declared = spec.children || {};
    if (!same(Object.keys(children), Object.keys(declared))) {
      errors.push(
        `${CONTRACT}: ${name}.children — ключи [${Object.keys(declared)}], в Go [${Object.keys(children)}]`,
      );
    } else {
      for (const [k, v] of Object.entries(children)) {
        if (declared[k] !== v)
          errors.push(
            `${CONTRACT}: ${name}.children.${k} = ${declared[k]}, в Go ${v}`,
          );
      }
    }
  }
}

// --- 2. Проводка -------------------------------------------------------------

const ROUTES = {
  "/api/configlayer/routing": "ConfigRouting",
  "/api/configlayer/routing/groups/save": "ConfigRoutingGroupsSave",
  "/api/configlayer/routing/groups/preview": "ConfigRoutingGroupsPreview",
  "/api/configlayer/routing/groups/delete": "ConfigRoutingGroupsDelete",
  "/api/configlayer/routing/impact": "ConfigRoutingImpact",
  "/api/configlayer/routing/rules/save": "ConfigRoutingRulesSave",
  "/api/configlayer/routing/rules/delete": "ConfigRoutingRulesDelete",
  "/api/configlayer/routing/rules/move": "ConfigRoutingRulesMove",
  "/api/configlayer/routing/rules/enable": "ConfigRoutingRulesEnable",
  "/api/configlayer/routing/final": "ConfigRoutingFinal",
  "/api/configlayer/routing/probe": "ConfigRoutingProbe",
  "/api/configlayer/routing/take": "ConfigRoutingTake",
  "/api/configlayer/routing/release": "ConfigRoutingRelease",
  "/api/configlayer/routing/takeover": "ConfigRoutingTakeover",
  "/api/configlayer/routing/reclaim": "ConfigRoutingReclaim",
  "/api/configlayer/routing/select": "ConfigRoutingSelect",
  "/api/configlayer/routing/lists": "ConfigRoutingLists",
  "/api/configlayer/routing/lists/geodata/preview":
    "ConfigRoutingGeodataPreview",
  "/api/configlayer/routing/lists/geodata/add": "ConfigRoutingGeodataAdd",
  "/api/configlayer/routing/presets": "ConfigRoutingPresets",
  "/api/configlayer/routing/presets/plan": "ConfigRoutingPresetsPlan",
  "/api/configlayer/routing/presets/apply": "ConfigRoutingPresetsApply",
};

const MAIN = "cmd/xcp/main.go";
const HANDLERS = "internal/handlers/routing.go";
const main = read(MAIN);
const mainLines = main.split("\n");
const handlers = read(HANDLERS);

for (const [route, fn] of Object.entries(ROUTES)) {
  const want = `srv.HandleProtected("${route}", api.${fn})`;
  if (!main.includes(want)) errors.push(`${MAIN}: нет ${want}`);
  if (
    !new RegExp(
      `func \\(a \\*API\\) ${fn}\\(w http\\.ResponseWriter, r \\*http\\.Request\\)`,
    ).test(handlers)
  ) {
    errors.push(`${HANDLERS}: нет обработчика ${fn}`);
  }
}
// Любой маршрут /api/configlayer/routing* обязан идти через HandleProtected; маршруты,
// добавленные следующими планами, этим же правилом охраняются.
for (const m of main.matchAll(
  /srv\.(Handle|HandlePublic)\("(\/api\/configlayer\/routing[^"]*)"/g,
)) {
  errors.push(
    `${MAIN}: ${m[2]} зарегистрирован через ${m[1]}, нужен HandleProtected`,
  );
}

const gate = read("internal/handlers/kernel_gate.go");
if (gate.includes("/api/configlayer/routing")) {
  errors.push(
    "internal/handlers/kernel_gate.go: маршруты маршрутизации не должны входить в kernelGatedPrefixes",
  );
}

// lineOf — номер первой строки, содержащей подстроку, начиная после строки after
// (0 — не найдена).
function lineOf(sub, after = 0) {
  for (let i = after; i < mainLines.length; i++) {
    if (mainLines[i].includes(sub)) return i + 1;
  }
  return 0;
}
const created = lineOf("routing.New(");
const need = {
  new: created,
  generators: lineOf("routingSvc.Generators()", created),
  register: lineOf("configLayer.Register(configlayer.Hooks{", created),
  checker: lineOf("routingSvc.Checker()", created),
  layerStart: lineOf("configLayer.Start()"),
  svcStart: lineOf("routingSvc.Start()"),
  layerStop: lineOf('defer stops.run("config-layer", configLayer.Stop)'),
  svcStop: lineOf('defer stops.run("routing", routingSvc.Stop)'),
  setSvc: lineOf("api.SetRoutingService(routingSvc)"),
  srcCommit: lineOf("sourcesSvc.CommitLive"),
  liveCommit: lineOf("routingSvc.CommitLive", created),
  afterApply: lineOf("routingSvc.AfterApply", created),
  watcher: lineOf("routing.NewSelectionWatcher(", created),
  watcherStart: lineOf("selWatch.Start()", created),
  watcherStop: lineOf('defer stops.run("routing-selection"', created),
};
for (const [k, v] of Object.entries(need)) {
  if (v === 0) errors.push(`${MAIN}: не найдено «${k}»`);
}
if (Object.values(need).every((v) => v > 0)) {
  const order = (a, b, why) => {
    if (!(need[a] < need[b])) errors.push(`${MAIN}: ${why}`);
  };
  order("new", "register", "routing.New( должен идти до configLayer.Register(");
  order(
    "register",
    "layerStart",
    "configLayer.Register( сервиса маршрутизации должен идти до configLayer.Start()",
  );
  order(
    "checker",
    "layerStart",
    "проверка сервиса маршрутизации (Hooks.Checker) должна регистрироваться до configLayer.Start()",
  );
  order(
    "layerStart",
    "svcStart",
    "routingSvc.Start() должен идти после configLayer.Start()",
  );
  order(
    "svcStart",
    "setSvc",
    "api.SetRoutingService( должен идти после routingSvc.Start()",
  );
  order(
    "srcCommit",
    "liveCommit",
    "фиксатор routingSvc.CommitLive должен идти после фиксатора источников (каталог узлов считается по уже зафиксированным источникам)",
  );
  order(
    "liveCommit",
    "layerStart",
    "фиксатор routingSvc.CommitLive должен регистрироваться до configLayer.Start()",
  );
  order(
    "afterApply",
    "layerStart",
    "хук routingSvc.AfterApply должен регистрироваться до configLayer.Start()",
  );
  order(
    "layerStart",
    "watcher",
    "routing.NewSelectionWatcher( должен идти после configLayer.Start()",
  );
  order(
    "svcStart",
    "watcher",
    "routing.NewSelectionWatcher( должен идти после routingSvc.Start()",
  );
  order(
    "watcher",
    "watcherStart",
    "selWatch.Start() должен идти после routing.NewSelectionWatcher(",
  );
  order(
    "svcStop",
    "watcherStop",
    "шаг остановки routing-selection должен идти после шага routing (defer: наблюдатель останавливается раньше сервиса и слоя)",
  );
  order(
    "layerStop",
    "svcStop",
    "шаг остановки routing должен идти после шага config-layer (defer: сервис останавливается раньше слоя)",
  );
}
// Сервис создаётся только в main.go: ни обработчики, ни другие пакеты его не создают.
for (const dir of ["internal/handlers"]) {
  for (const file of fs.readdirSync(path.join(root, dir))) {
    if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
    const text = read(`${dir}/${file}`);
    if (/\brouting\.New\(/.test(text)) {
      errors.push(
        `${dir}/${file}: сервис маршрутизации создаётся только в cmd/xcp/main.go (без lazy-init в обработчиках)`,
      );
    }
    if (/\brouting\.NewSelectionWatcher\(/.test(text)) {
      errors.push(
        `${dir}/${file}: наблюдатель выбора создаётся только в cmd/xcp/main.go`,
      );
    }
  }
}

// --- 3. Коды ошибок ----------------------------------------------------------

const CODES = "frontend/src/lib/routingErrorCodes.json";
let codes = [];
try {
  const parsed = JSON.parse(read(CODES));
  codes = Array.isArray(parsed) ? parsed : parsed.codes;
  if (!Array.isArray(codes)) throw new Error("ожидался массив кодов");
} catch (e) {
  errors.push(`${CODES}: ${e.message}`);
}
const ru = JSON.parse(read("internal/i18n/locales/ru.json"));
const en = JSON.parse(read("internal/i18n/locales/en.json"));
const seen = new Set();
for (const code of codes) {
  if (seen.has(code)) errors.push(`${CODES}: код ${code} повторяется`);
  seen.add(code);
  if (!handlers.includes(`"${code}"`))
    errors.push(
      `${HANDLERS}: код ${code} из ${CODES} не встречается в обработчиках`,
    );
  const key = `configlayer.routing.${code}`;
  if (!(key in ru))
    errors.push(`internal/i18n/locales/ru.json: нет ключа ${key}`);
  if (!(key in en))
    errors.push(`internal/i18n/locales/en.json: нет ключа ${key}`);
}

// --- 4. Поля Xray условий ----------------------------------------------------

const MODEL_GO = "internal/services/routing/model.go";
const VALIDATE_GO = "internal/services/routing/validate.go";
const VIEW_TS = "frontend/src/lib/routingView.ts";

// Типы условий модели: имя константы → строковое значение.
const condConsts = new Map();
{
  const block = read(MODEL_GO).match(
    /\/\/ Типы условий правила[^\n]*\nconst \(\n([\s\S]*?)\n\)/,
  );
  if (!block) {
    errors.push(`${MODEL_GO}: не найден const-блок «Типы условий правила»`);
  } else {
    for (const m of block[1].matchAll(/^\s*(Cond\w+)\s*=\s*"([^"]+)"/gm)) {
      condConsts.set(m[1], m[2]);
    }
    if (condConsts.size === 0)
      errors.push(
        `${MODEL_GO}: в блоке «Типы условий правила» нет констант Cond*`,
      );
  }
}

// Тип условия → поле Xray из таблицы бэкенда.
const backFields = new Map();
{
  const body = read(VALIDATE_GO).match(
    /var xrayCondFields = map\[string\]string\{\n([\s\S]*?)\n\}/,
  );
  if (!body) {
    errors.push(`${VALIDATE_GO}: не найдена таблица xrayCondFields`);
  } else {
    for (const m of body[1].matchAll(/^\s*(\w+):\s*"([^"]+)",/gm)) {
      const type = condConsts.get(m[1]);
      if (type === undefined) {
        errors.push(
          `${VALIDATE_GO}: ключ ${m[1]} таблицы xrayCondFields — не тип условия модели`,
        );
        continue;
      }
      backFields.set(type, m[2]);
    }
  }
}

// Тип условия → поле Xray из таблицы фронтенда.
const frontFields = new Map();
{
  const body = read(VIEW_TS).match(
    /export const COND_XRAY_FIELD[^=]*=\s*\{([\s\S]*?)\}/,
  );
  if (!body) {
    errors.push(`${VIEW_TS}: не найдена таблица COND_XRAY_FIELD`);
  } else {
    for (const m of body[1].matchAll(/(\w+)\s*:\s*'([^']+)'/g)) {
      frontFields.set(m[1], m[2]);
    }
  }
}

const condTypes = [...condConsts.values()];
for (const type of condTypes) {
  if (backFields.size > 0 && !backFields.has(type))
    errors.push(`${VALIDATE_GO}: в xrayCondFields нет типа условия ${type}`);
  if (frontFields.size > 0 && !frontFields.has(type))
    errors.push(`${VIEW_TS}: в COND_XRAY_FIELD нет типа условия ${type}`);
  if (
    backFields.has(type) &&
    frontFields.has(type) &&
    backFields.get(type) !== frontFields.get(type)
  )
    errors.push(
      `поле Xray типа ${type} различается: xrayCondFields (${VALIDATE_GO}) — ${backFields.get(type)}, COND_XRAY_FIELD (${VIEW_TS}) — ${frontFields.get(type)}`,
    );
}
for (const type of frontFields.keys()) {
  if (condConsts.size > 0 && !condTypes.includes(type))
    errors.push(
      `${VIEW_TS}: ключ ${type} таблицы COND_XRAY_FIELD — не тип условия модели`,
    );
}

if (errors.length > 0) {
  for (const e of errors) console.error("❌ " + e);
  process.exit(1);
}
console.log(
  `✅ API маршрутизации согласован: типов контракта ${contract ? Object.keys(contract).length : 0}, маршрутов ${Object.keys(ROUTES).length}, кодов ${codes.length}, типов условий ${condTypes.length}`,
);
