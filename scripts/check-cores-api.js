#!/usr/bin/env node
/*
 * check-cores-api.js — статическая согласованность API секций ядер (фаза 147).
 *
 * 1. Контракт ключей ответов: frontend/src/lib/coresContract.json совпадает с
 *    json-тегами Go-типов (internal/services/cores/service.go, advanced.go, raw.go,
 *    internal/services/configlayer/sections.go и view.go): обязательные = теги без
 *    omitempty, необязательные = с omitempty, вложенные типы (children) выводятся из
 *    типов полей, встроенные структуры разворачиваются. Тип контракта, не найденный
 *    ни в одном файле, — ошибка. Тот же файл читают типы фронтенда и роутерные
 *    проверки ответов панели.
 * 2. Проводка: каждый из десяти маршрутов /api/configlayer/cores* зарегистрирован в
 *    cmd/xcp/main.go через HandleProtected, у него есть обработчик в
 *    internal/handlers/cores.go, маршруты не входят в kernelGatedPrefixes
 *    (internal/handlers/kernel_gate.go); cores.New вызывается только в
 *    cmd/xcp/main.go; генераторы и проверка сервиса секций регистрируются до
 *    configLayer.Start(), coresSvc.Start() идёт после него, шаг остановки сервиса
 *    стоит после шага остановки слоя (defer: сервис останавливается раньше слоя).
 * 3. Коды ошибок: каждый код codes из frontend/src/lib/coresErrorCodes.json
 *    встречается в internal/handlers/cores.go и имеет ключ configlayer.cores.<код>
 *    в обоих бэкенд-словарях; каждый код issues совпадает со значением константы
 *    Issue* или DriftSectionForeign в internal/services/configlayer/sections.go;
 *    обратно — каждый код, который сервис секций может вернуть (литерал в поле Code
 *    или в errCode, строковые константы пакета cores, константы configlayer.Issue*
 *    и DriftSectionForeign), есть в codes, а значит, и в таблице статусов
 *    обработчиков и в бэкенд-словарях.
 * 4. Поля форм: FORM_KEYS (frontend/src/lib/configCores.ts) по каждой секции
 *    совпадает с XrayFormKeys (internal/services/cores/xray_model.go) и
 *    MihomoFormKeys (internal/services/cores/mihomo_model.go).
 * 5. Наличие форм: у каждой секции есть форма в
 *    frontend/src/components/config/sections/forms/ — Xray log, dns, inbounds,
 *    policy и Mihomo main, dns, inbounds, sniffer (<Ядро><Секция>Form.svelte), для
 *    «Прочих ключей» обоих ядер — OtherKeysForm.svelte. Пропажа формы — ошибка CI,
 *    а не карточка без формы.
 *
 * Usage: node scripts/check-cores-api.js [--root <каталог>] [--only <правила>]
 *   --root  корень проверяемого дерева (по умолчанию — корень репозитория);
 *   --only  правила через запятую из contract, wiring, codes, keys, forms
 *           (по умолчанию — все).
 */
const fs = require("fs");
const path = require("path");

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf("--root");
const root =
  rootIdx >= 0
    ? path.resolve(argv[rootIdx + 1])
    : path.resolve(__dirname, "..");
const onlyIdx = argv.indexOf("--only");
const only =
  onlyIdx >= 0
    ? new Set(
        String(argv[onlyIdx + 1] || "")
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
      )
    : null;

const errors = [];
const stats = {};

function read(rel) {
  try {
    return fs.readFileSync(path.join(root, rel), "utf8");
  } catch (e) {
    console.error("check-cores-api: не удалось прочитать файл: " + e.message);
    process.exit(1);
  }
}

function exists(rel) {
  return fs.existsSync(path.join(root, rel));
}

function parseJSON(rel) {
  try {
    return JSON.parse(read(rel));
  } catch (e) {
    errors.push(`${rel}: не разбирается как JSON: ${e.message}`);
    return null;
  }
}

// goFilesIn — Go-файлы каталога без тестов (пустой список, если каталога нет).
function goFilesIn(dir) {
  const abs = path.join(root, dir);
  if (!fs.existsSync(abs)) return [];
  return fs
    .readdirSync(abs)
    .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
    .sort()
    .map((f) => `${dir}/${f}`);
}

const same = (a, b) =>
  JSON.stringify([...a].sort()) === JSON.stringify([...b].sort());

// --- 1. Контракт ключей ------------------------------------------------------

const CONTRACT = "frontend/src/lib/coresContract.json";
// Go-файлы типов ответов. Отсутствующие в дереве пропускаются; тип контракта, не
// найденный ни в одном файле, — ошибка.
const GO_FILES = [
  "internal/services/cores/service.go",
  "internal/services/cores/advanced.go",
  "internal/services/cores/raw.go",
  "internal/services/configlayer/sections.go",
  "internal/services/configlayer/view.go",
];

function checkContract() {
  const contract = parseJSON(CONTRACT);
  if (contract === null) return;

  // Go-структуры: имя → { fields: [{ name, type, tag, omit }], embedded: [имя] }.
  const structs = new Map();
  for (const file of GO_FILES) {
    if (!exists(file)) continue;
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
  stats.contract = `типов контракта ${known.size}`;
}

// --- 5. Наличие форм ---------------------------------------------------------

const FORMS_DIR = "frontend/src/components/config/sections/forms";
// Секции с формой: <Ядро><Секция>Form.svelte; «Прочие ключи» — общая форма.
const FORM_SECTIONS = {
  Xray: ["Log", "Dns", "Inbounds", "Policy"],
  Mihomo: ["Main", "Dns", "Inbounds", "Sniffer"],
};

function checkForms() {
  let n = 0;
  const need = [];
  for (const [kernel, sections] of Object.entries(FORM_SECTIONS)) {
    for (const s of sections) {
      need.push([`${kernel.toLowerCase()}.${s.toLowerCase()}`, `${kernel}${s}Form.svelte`]);
    }
    need.push([`${kernel.toLowerCase()}.other`, "OtherKeysForm.svelte"]);
  }
  for (const [section, file] of need) {
    n++;
    if (!exists(`${FORMS_DIR}/${file}`))
      errors.push(
        `${FORMS_DIR}/${file}: нет формы секции ${section} (секция без формы показала бы пустую карточку)`,
      );
  }
  stats.forms = `форм ${n}`;
}

// --- 2. Проводка -------------------------------------------------------------

const ROUTES = {
  "/api/configlayer/cores": "ConfigCores",
  "/api/configlayer/cores/takeover": "ConfigCoresTakeover",
  "/api/configlayer/cores/take": "ConfigCoresTake",
  "/api/configlayer/cores/save": "ConfigCoresSave",
  "/api/configlayer/cores/preset": "ConfigCoresPreset",
  "/api/configlayer/cores/release": "ConfigCoresRelease",
  "/api/configlayer/cores/force-proxy": "ConfigCoresForceProxy",
  "/api/configlayer/cores/raw/parse": "ConfigCoresRawParse",
  "/api/configlayer/cores/reclaim": "ConfigCoresReclaim",
  "/api/configlayer/cores/access-log": "ConfigCoresAccessLog",
};

const MAIN = "cmd/xcp/main.go";
const HANDLERS = "internal/handlers/cores.go";
const GATE = "internal/handlers/kernel_gate.go";

// goFilesUnder — Go-файлы каталога и подкаталогов без тестов.
function goFilesUnder(dir) {
  const abs = path.join(root, dir);
  if (!fs.existsSync(abs)) return [];
  const out = [];
  for (const ent of fs.readdirSync(abs, { withFileTypes: true })) {
    const rel = `${dir}/${ent.name}`;
    if (ent.isDirectory()) out.push(...goFilesUnder(rel));
    else if (ent.name.endsWith(".go") && !ent.name.endsWith("_test.go"))
      out.push(rel);
  }
  return out.sort();
}

function checkWiring() {
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
  // Любой маршрут /api/configlayer/cores* обязан идти через HandleProtected: без
  // сессии и CSRF-токена к секциям ядер не должно быть доступа (T-147-64).
  for (const m of main.matchAll(
    /srv\.(Handle|HandlePublic)\("(\/api\/configlayer\/cores[^"]*)"/g,
  )) {
    errors.push(
      `${MAIN}: ${m[2]} зарегистрирован через ${m[1]}, нужен HandleProtected`,
    );
  }
  // Маршруты секций не входят в kernelGatedPrefixes: они работают при любом ядре.
  const gate = read(GATE);
  const gated = gate.match(
    /var kernelGatedPrefixes = \[\]string\{([\s\S]*?)\n\}/,
  );
  if (!gated) {
    errors.push(`${GATE}: не найден список kernelGatedPrefixes`);
  } else {
    const prefixes = [...gated[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
    for (const route of Object.keys(ROUTES)) {
      for (const p of prefixes) {
        if (route.startsWith(p))
          errors.push(
            `${GATE}: маршрут ${route} попадает под префикс ${p} из kernelGatedPrefixes — маршруты секций ядер не привязаны к ядру`,
          );
      }
    }
  }

  // lineOf — номер первой строки, содержащей подстроку, начиная после строки after
  // (0 — не найдена).
  const lineOf = (sub, after = 0) => {
    for (let i = after; i < mainLines.length; i++) {
      if (mainLines[i].includes(sub)) return i + 1;
    }
    return 0;
  };
  const created = lineOf("cores.New(");
  const need = {
    new: created,
    generators: lineOf("coresSvc.Generators()", created),
    checker: lineOf("coresSvc.Checker()", created),
    layerStart: lineOf("configLayer.Start()"),
    svcStart: lineOf("coresSvc.Start()"),
    layerStop: lineOf('defer stops.run("config-layer", configLayer.Stop)'),
    svcStop: lineOf('defer stops.run("cores", coresSvc.Stop)'),
    setSvc: lineOf("api.SetCoresService(coresSvc)"),
  };
  for (const [k, v] of Object.entries(need)) {
    if (v === 0) errors.push(`${MAIN}: не найдено «${k}»`);
  }
  if (Object.values(need).every((v) => v > 0)) {
    const order = (a, b, why) => {
      if (!(need[a] < need[b])) errors.push(`${MAIN}: ${why}`);
    };
    order(
      "new",
      "generators",
      "cores.New( должен идти до регистрации генераторов сервиса секций",
    );
    order(
      "generators",
      "layerStart",
      "регистрация генераторов сервиса секций (coresSvc.Generators()) должна идти до configLayer.Start()",
    );
    order(
      "checker",
      "layerStart",
      "проверка сервиса секций (Hooks.Checker) должна регистрироваться до configLayer.Start()",
    );
    order(
      "layerStart",
      "svcStart",
      "coresSvc.Start() должен идти после configLayer.Start()",
    );
    order(
      "svcStart",
      "setSvc",
      "api.SetCoresService( должен идти после coresSvc.Start()",
    );
    order(
      "layerStop",
      "svcStop",
      "шаг остановки cores должен идти после шага config-layer (defer: сервис останавливается раньше слоя)",
    );
  }
  // Сервис создаётся только в main.go: ни обработчики, ни другие пакеты его не
  // создают (без lazy-init).
  for (const file of [...goFilesUnder("internal"), ...goFilesUnder("cmd")]) {
    if (file === MAIN) continue;
    if (/\bcores\.New\(/.test(read(file))) {
      errors.push(
        `${file}: сервис секций ядер создаётся только в ${MAIN} (без lazy-init в обработчиках)`,
      );
    }
  }
  stats.wiring = `маршрутов ${Object.keys(ROUTES).length}`;
}

// --- 3. Коды ошибок ----------------------------------------------------------

const CODES = "frontend/src/lib/coresErrorCodes.json";
const SECTIONS_GO = "internal/services/configlayer/sections.go";
const CORES_DIR = "internal/services/cores";

function checkCodes() {
  const parsed = parseJSON(CODES);
  if (parsed === null) return;
  const codes = parsed.codes;
  const issues = parsed.issues;
  if (!Array.isArray(codes) || !Array.isArray(issues)) {
    errors.push(`${CODES}: ожидались массивы codes и issues`);
    return;
  }
  const handlers = read(HANDLERS);
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
    const key = `configlayer.cores.${code}`;
    if (!ru[key])
      errors.push(`internal/i18n/locales/ru.json: нет ключа ${key}`);
    if (!en[key])
      errors.push(`internal/i18n/locales/en.json: нет ключа ${key}`);
  }

  // Значения констант Issue* и DriftSectionForeign слоя.
  const layerConsts = new Map();
  for (const m of read(SECTIONS_GO).matchAll(
    /^\s*(Issue\w+|DriftSectionForeign)\s*=\s*"([^"]+)"/gm,
  )) {
    layerConsts.set(m[1], m[2]);
  }
  if (layerConsts.size === 0)
    errors.push(
      `${SECTIONS_GO}: не найдены константы Issue*/DriftSectionForeign (разбор стража не сработал)`,
    );
  const layerValues = new Set(layerConsts.values());
  for (const code of issues) {
    if (!layerValues.has(code))
      errors.push(
        `${CODES}: код проблемы ${code} не совпадает со значением ни одной константы Issue* или DriftSectionForeign в ${SECTIONS_GO}`,
      );
  }

  // Обратная сверка: коды, которые сервис секций может вернуть.
  const used = new Map(); // код → файл, где найден
  const note = (code, file) => {
    if (!used.has(code)) used.set(code, file);
  };
  for (const file of goFilesIn(CORES_DIR)) {
    const src = read(file);
    for (const m of src.matchAll(/\bCode:\s*"([^"]+)"/g)) note(m[1], file);
    for (const m of src.matchAll(/\berrCode\(\s*"([^"]+)"/g)) note(m[1], file);
    // Строковые константы пакета: codeИмя, issueИмя.
    for (const m of src.matchAll(
      /^\s*(?:const\s+)?(?:code|issue)[A-Z]\w*\s*=\s*"([^"]+)"/gm,
    )) {
      note(m[1], file);
    }
    for (const m of src.matchAll(
      /\bconfiglayer\.(Issue\w+|DriftSectionForeign)\b/g,
    )) {
      const v = layerConsts.get(m[1]);
      if (v === undefined)
        errors.push(
          `${file}: константа configlayer.${m[1]} не найдена в ${SECTIONS_GO}`,
        );
      else note(v, file);
    }
  }
  for (const [code, file] of used) {
    if (!seen.has(code))
      errors.push(
        `${file}: код ${code} из internal/services/cores не описан в coresErrorCodes.json codes (нет статуса ответа и бэкенд-текстов)`,
      );
  }
  stats.codes = `кодов ${codes.length}, проблем ${issues.length}`;
}

// --- 4. Поля форм ------------------------------------------------------------

const FORM_TS = "frontend/src/lib/configCores.ts";
const XRAY_MODEL = "internal/services/cores/xray_model.go";
const MIHOMO_MODEL = "internal/services/cores/mihomo_model.go";

// quoted — строки в кавычках из фрагмента.
const quoted = (text) =>
  [...text.matchAll(/'([^']*)'|"([^"]*)"/g)].map((m) =>
    m[1] !== undefined ? m[1] : m[2],
  );

function checkKeys() {
  // Фронтенд: FORM_KEYS = { 'ядро.секция': ['путь', …], … }.
  const front = new Map();
  const body = read(FORM_TS).match(
    /export const FORM_KEYS[^=]*=\s*\{([\s\S]*?)\n\};/,
  );
  if (!body) {
    errors.push(`${FORM_TS}: не найдена таблица FORM_KEYS`);
    return;
  }
  const str = `(?:'[^']*'|"[^"]*")`;
  for (const m of body[1].matchAll(
    new RegExp(`(${str})\\s*:\\s*\\[((?:\\s*${str}\\s*,?)*)\\s*\\]`, "g"),
  )) {
    front.set(quoted(m[1])[0], quoted(m[2]));
  }
  if (front.size === 0) {
    errors.push(`${FORM_TS}: в FORM_KEYS не разобрана ни одна секция`);
    return;
  }

  // Бэкенд: XrayFormKeys (ключи — строки) и MihomoFormKeys (ключи — константы).
  const back = new Map();
  const xray = read(XRAY_MODEL).match(
    /var XrayFormKeys = map\[string\]\[\]string\{\n([\s\S]*?)\n\}/,
  );
  if (!xray) errors.push(`${XRAY_MODEL}: не найдена таблица XrayFormKeys`);
  else
    for (const m of xray[1].matchAll(/"([^"]+)":\s*\{([^}]*)\}/g)) {
      back.set(`xray.${m[1]}`, quoted(m[2]));
    }
  const mihomoSrc = read(MIHOMO_MODEL);
  const idConst = new Map();
  for (const m of mihomoSrc.matchAll(/^\s*(mihomo\w+)\s*=\s*"([^"]+)"/gm)) {
    idConst.set(m[1], m[2]);
  }
  const mihomo = mihomoSrc.match(
    /var MihomoFormKeys = map\[string\]\[\]string\{\n([\s\S]*?)\n\}/,
  );
  if (!mihomo)
    errors.push(`${MIHOMO_MODEL}: не найдена таблица MihomoFormKeys`);
  else
    for (const m of mihomo[1].matchAll(/(\w+):\s*\{([^}]*)\}/g)) {
      const id = idConst.get(m[1]);
      if (id === undefined)
        errors.push(
          `${MIHOMO_MODEL}: ключ ${m[1]} таблицы MihomoFormKeys — не идентификатор секции`,
        );
      else back.set(`mihomo.${id}`, quoted(m[2]));
    }

  for (const [id, keys] of front) {
    if (!back.has(id)) {
      errors.push(`${FORM_TS}: секции ${id} нет в таблицах полей бэкенда`);
      continue;
    }
    if (!same(keys, back.get(id)))
      errors.push(
        `поля формы секции ${id} различаются: FORM_KEYS (${FORM_TS}) — [${keys}], бэкенд — [${back.get(id)}]`,
      );
  }
  for (const id of back.keys()) {
    if (!front.has(id))
      errors.push(`${FORM_TS}: в FORM_KEYS нет секции ${id} из таблиц бэкенда`);
  }
  stats.keys = `секций ${front.size}`;
}

// --- Запуск правил ----------------------------------------------------------

const RULES = {
  contract: checkContract,
  wiring: checkWiring,
  codes: checkCodes,
  keys: checkKeys,
  forms: checkForms,
};

const names = Object.keys(RULES);
if (only) {
  for (const n of only)
    if (!names.includes(n)) {
      console.error(`check-cores-api: неизвестное правило «${n}»`);
      process.exit(2);
    }
}
for (const [name, fn] of Object.entries(RULES)) {
  if (only && !only.has(name)) continue;
  fn();
}

if (errors.length > 0) {
  for (const e of errors) console.error("❌ " + e);
  process.exit(1);
}
console.log(
  "check-cores-api: OK" +
    (Object.keys(stats).length > 0
      ? " (" + Object.values(stats).join(", ") + ")"
      : ""),
);
