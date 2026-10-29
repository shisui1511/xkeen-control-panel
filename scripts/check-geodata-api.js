#!/usr/bin/env node
/*
 * check-geodata-api.js — статическая согласованность API менеджера geodata.
 *
 * 1. Контракт ключей ответов: frontend/src/lib/geodataContract.json совпадает с
 *    json-тегами Go-типов (internal/services/geodata/*.go, routing/cascade_geodata.go,
 *    routing/lists/*.go, internal/handlers/geodata.go): обязательные = теги без
 *    omitempty, необязательные = с omitempty, вложенные типы (children) выводятся из
 *    типов полей, встроенные структуры (routing.GeodataImpact в ImpactView)
 *    разворачиваются. Имя типа во фронтенде и в Go может различаться (RuleRef —
 *    GeodataRuleRef), соответствие — таблица GO_NAME.
 * 2. Проводка: каждый из 15 маршрутов /api/configlayer/geodata* зарегистрирован в
 *    cmd/xcp/main.go через HandleProtected, у него есть обработчик в
 *    internal/handlers/geodata.go, префикс не входит в kernelGatedPrefixes. Сервис
 *    менеджера создаётся только в cmd/xcp/main.go; Register с AfterApply идёт до
 *    configLayer.Start(), geoSvc.Start() — после него, шаг остановки geodata
 *    регистрируется после шага routing (defer: менеджер останавливается раньше
 *    сервиса маршрутизации).
 * 3. Коды ошибок: каждый код из frontend/src/lib/geodataErrorCodes.json есть строкой в
 *    internal/handlers/geodata.go или internal/handlers/configlayer.go и имеет текст
 *    cfg.geo.err.<код> в обоих словарях фронтенда; каждый код таблицы
 *    geodataManagerStatus (handlers/geodata.go) — configlayer.geodata.<код> в обоих
 *    бэкенд-словарях; каждый код, который возвращает пакет geodata (литералы
 *    CodedError{Code: …} и константы removeReason*), есть в таблице или разобран в
 *    writeGeodataError отдельно (иначе ответ — 500 internal).
 *
 * Usage: node scripts/check-geodata-api.js [--root <каталог>]
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
    console.error("check-geodata-api: не удалось прочитать файл: " + e.message);
    process.exit(1);
  }
}

// --- 1. Контракт ключей ------------------------------------------------------

const CONTRACT = "frontend/src/lib/geodataContract.json";
// Go-файлы типов ответов. Тип контракта, не найденный ни в одном файле, — ошибка.
const GO_FILES = [
  "internal/services/geodata/view.go",
  "internal/services/geodata/ops.go",
  "internal/services/geodata/probe.go",
  "internal/services/geodata/foreign.go",
  "internal/services/routing/cascade_geodata.go",
  "internal/services/routing/lists/geodata.go",
  "internal/services/routing/lists/catalog.go",
  "internal/services/routing/lists/index.go",
  "internal/services/routing/lists/entries.go",
  "internal/services/routing/lists/lookup.go",
  "internal/services/routing/lists/mihomo_files.go",
  "internal/handlers/geodata.go",
];
// Имя типа в контракте (и во фронтенде) → имя типа в Go, если они различаются.
const GO_NAME = { RuleRef: "GeodataRuleRef" };
const CONTRACT_NAME = Object.fromEntries(
  Object.entries(GO_NAME).map(([c, g]) => [g, c]),
);

let contract = null;
try {
  contract = JSON.parse(read(CONTRACT));
} catch (e) {
  errors.push(`${CONTRACT}: не разбирается как JSON: ${e.message}`);
}

// Go-структуры: имя → { fields: [{ name, type, tag, omit }], embedded: [имя] }.
const structs = new Map();
for (const file of GO_FILES) {
  if (!fs.existsSync(path.join(root, file))) {
    errors.push(`${file}: файл типов ответов не найден`);
    continue;
  }
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
      const e = line.match(/^\s*\*?(?:\w+\.)?(\w+)\s*(\/\/.*)?$/);
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
// Имя типа переводится в имя контракта (GO_NAME).
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
  t = CONTRACT_NAME[t] || t;
  return known.has(t) ? t + suffix : null;
}

if (contract !== null) {
  const known = new Set(Object.keys(contract));
  for (const [name, spec] of Object.entries(contract)) {
    const goName = GO_NAME[name] || name;
    const fields = allFields(goName);
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
  "/api/configlayer/geodata": "ConfigGeodata",
  "/api/configlayer/geodata/output": "ConfigGeodataOutput",
  "/api/configlayer/geodata/run": "ConfigGeodataRun",
  "/api/configlayer/geodata/jq": "ConfigGeodataJQ",
  "/api/configlayer/geodata/schedule": "ConfigGeodataSchedule",
  "/api/configlayer/geodata/cron": "ConfigGeodataCron",
  "/api/configlayer/geodata/probe": "ConfigGeodataProbe",
  "/api/configlayer/geodata/preview": "ConfigGeodataPreview",
  "/api/configlayer/geodata/write": "ConfigGeodataWrite",
  "/api/configlayer/geodata/impact": "ConfigGeodataImpact",
  "/api/configlayer/geodata/drop": "ConfigGeodataDrop",
  "/api/configlayer/geodata/restore": "ConfigGeodataRestore",
  "/api/configlayer/geodata/tags": "ConfigGeodataTags",
  "/api/configlayer/geodata/entries": "ConfigGeodataEntries",
  "/api/configlayer/geodata/lookup": "ConfigGeodataLookup",
};

const MAIN = "cmd/xcp/main.go";
const HANDLERS = "internal/handlers/geodata.go";
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
// Любой маршрут /api/configlayer/geodata* обязан идти через HandleProtected и быть в
// списке выше: маршрут, добавленный без стража, не пройдёт.
for (const m of main.matchAll(
  /srv\.(Handle|HandlePublic|HandleProtected)\("(\/api\/configlayer\/geodata[^"]*)"/g,
)) {
  if (m[1] !== "HandleProtected")
    errors.push(
      `${MAIN}: ${m[2]} зарегистрирован через ${m[1]}, нужен HandleProtected`,
    );
  if (!(m[2] in ROUTES))
    errors.push(`${MAIN}: маршрут ${m[2]} не входит в список ROUTES стража`);
}

const gate = read("internal/handlers/kernel_gate.go");
if (gate.includes("/api/configlayer/geodata")) {
  errors.push(
    "internal/handlers/kernel_gate.go: маршруты менеджера geodata не должны входить в kernelGatedPrefixes",
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
const created = lineOf("geodata.New(");
const need = {
  new: created,
  register: lineOf("AfterApply: geoSvc.AfterApply", created),
  layerStart: lineOf("configLayer.Start()"),
  svcStart: lineOf("geoSvc.Start()"),
  setSvc: lineOf("api.SetGeodataService("),
  routingStop: lineOf('defer stops.run("routing", routingSvc.Stop)'),
  svcStop: lineOf('defer stops.run("geodata", geoSvc.Stop)'),
};
for (const [k, v] of Object.entries(need)) {
  if (v === 0) errors.push(`${MAIN}: не найдено «${k}»`);
}
if (Object.values(need).every((v) => v > 0)) {
  const order = (a, b, why) => {
    if (!(need[a] < need[b])) errors.push(`${MAIN}: ${why}`);
  };
  order("new", "register", "geodata.New( должен идти до configLayer.Register(");
  order(
    "register",
    "layerStart",
    "configLayer.Register( с AfterApply менеджера должен идти до configLayer.Start()",
  );
  order(
    "layerStart",
    "svcStart",
    "geoSvc.Start() должен идти после configLayer.Start()",
  );
  order(
    "svcStart",
    "setSvc",
    "api.SetGeodataService( должен идти после geoSvc.Start()",
  );
  order(
    "routingStop",
    "svcStop",
    "шаг остановки geodata должен идти после шага routing (defer: менеджер останавливается раньше сервиса маршрутизации)",
  );
}

// Сервис создаётся только в cmd/xcp/main.go: ни обработчики, ни другие пакеты его не
// создают (тесты не считаются).
function goFiles(dir) {
  const out = [];
  for (const ent of fs.readdirSync(path.join(root, dir), {
    withFileTypes: true,
  })) {
    const rel = `${dir}/${ent.name}`;
    if (ent.isDirectory()) out.push(...goFiles(rel));
    else if (ent.name.endsWith(".go") && !ent.name.endsWith("_test.go"))
      out.push(rel);
  }
  return out;
}
for (const dir of ["internal", "cmd"]) {
  for (const file of goFiles(dir)) {
    if (file === MAIN) continue;
    if (/\bgeodata\.New\(/.test(read(file))) {
      errors.push(
        `${file}: сервис менеджера geodata создаётся только в cmd/xcp/main.go (без lazy-init)`,
      );
    }
  }
}

// --- 3. Коды ошибок ----------------------------------------------------------

const CODES = "frontend/src/lib/geodataErrorCodes.json";
let codes = [];
try {
  const parsed = JSON.parse(read(CODES));
  codes = Array.isArray(parsed) ? parsed : parsed.codes;
  if (!Array.isArray(codes)) throw new Error("ожидался массив кодов");
} catch (e) {
  errors.push(`${CODES}: ${e.message}`);
}
const LAYER_HANDLERS = "internal/handlers/configlayer.go";
const layerHandlers = read(LAYER_HANDLERS);
const feRu = JSON.parse(read("frontend/src/locales/ru.json"));
const feEn = JSON.parse(read("frontend/src/locales/en.json"));
const seen = new Set();
for (const code of codes) {
  if (seen.has(code)) errors.push(`${CODES}: код ${code} повторяется`);
  seen.add(code);
  if (!handlers.includes(`"${code}"`) && !layerHandlers.includes(`"${code}"`))
    errors.push(
      `${HANDLERS}, ${LAYER_HANDLERS}: код ${code} из ${CODES} не встречается в обработчиках`,
    );
  const key = `cfg.geo.err.${code}`;
  if (!(key in feRu))
    errors.push(`frontend/src/locales/ru.json: нет ключа ${key}`);
  if (!(key in feEn))
    errors.push(`frontend/src/locales/en.json: нет ключа ${key}`);
}

// Таблица статусов менеджера: каждый её код имеет текст в бэкенд-словарях.
const beRu = JSON.parse(read("internal/i18n/locales/ru.json"));
const beEn = JSON.parse(read("internal/i18n/locales/en.json"));
const tableMatch = handlers.match(
  /var geodataManagerStatus = map\[string\]int\{\n([\s\S]*?)\n\}/,
);
let tableCodes = [];
if (!tableMatch) {
  errors.push(`${HANDLERS}: не найдена таблица geodataManagerStatus`);
} else {
  tableCodes = [...tableMatch[1].matchAll(/^\s*"(\w+)":\s*http\./gm)].map(
    (m) => m[1],
  );
  if (tableCodes.length === 0)
    errors.push(`${HANDLERS}: таблица geodataManagerStatus пуста`);
}
for (const code of tableCodes) {
  const key = `configlayer.geodata.${code}`;
  if (!(key in beRu))
    errors.push(`internal/i18n/locales/ru.json: нет ключа ${key}`);
  if (!(key in beEn))
    errors.push(`internal/i18n/locales/en.json: нет ключа ${key}`);
}

// Коды, которые возвращает сам сервис: литералы CodedError{Code: …} и причины
// удаления removeReason* (FinishDelete отдаёт их как CodedError).
const GEODATA_PKG = "internal/services/geodata";
const SWITCH_CODES = new Set(["geodata_busy", "apply_busy", "bad_op"]);
const serviceCodes = new Set();
for (const file of goFiles(GEODATA_PKG)) {
  const text = read(file);
  for (const m of text.matchAll(/CodedError\{Code: "([a-z0-9_]+)"\}/g))
    serviceCodes.add(m[1]);
  for (const m of text.matchAll(/^\s*removeReason\w*\s*=\s*"([a-z0-9_]+)"/gm))
    serviceCodes.add(m[1]);
}
if (serviceCodes.size === 0)
  errors.push(`${GEODATA_PKG}: не найдены коды ошибок сервиса`);
for (const code of serviceCodes) {
  if (!tableCodes.includes(code) && !SWITCH_CODES.has(code))
    errors.push(
      `${HANDLERS}: код ${code} пакета geodata не в geodataManagerStatus (ответ 500 internal)`,
    );
}

if (errors.length > 0) {
  for (const e of errors) console.error("❌ " + e);
  process.exit(1);
}
console.log(
  `✅ API менеджера geodata согласован: типов контракта ${contract ? Object.keys(contract).length : 0}, маршрутов ${Object.keys(ROUTES).length}, кодов ${codes.length}, кодов таблицы статусов ${tableCodes.length}`,
);
