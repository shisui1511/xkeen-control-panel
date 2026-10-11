#!/usr/bin/env node
/*
 * check-config-ui.js — статическая согласованность клиента источников и каркаса
 * раздела «Конфигурация» (фаза 145). Не тесты поведения: поведение панели
 * проверяют роутерные сценарии (frontend/tests/router/config-nodes.spec.ts).
 *
 * 1. Типы ответов: интерфейсы frontend/src/lib/configSources.ts совпадают по
 *    ключам с frontend/src/lib/sourcesContract.json (обязательные поля — без «?»,
 *    необязательные — с «?»). SourceError ↔ SourceErrorPayload, Verdict ↔ VerdictView.
 * 2. Событие sources: configLayer.ts слушает его в одном SSE-потоке и отдаёт
 *    onLayerEvent; configSources.ts подписывается; configLayer.ts не импортирует
 *    configSources.ts (нет цикла); configSources.ts не импортируется вне
 *    config-раздела (при выключенном флаге запросов источников нет).
 * 3. Уведомления: бейдж меню считается от badgeTotal (изменения + видимые
 *    уведомления); ConfigNotices не использует {@html}, ссылку кабинета проверяет
 *    через new URL по схемам http/https и ставит target=_blank с
 *    rel="noopener noreferrer"; ApplyProgress не открывается для source_refresh.
 * 4. Каркас: Config.svelte со вкладками и маршрутами; NodesTab; ключи i18n в обоих
 *    словарях.
 *
 * Usage: node scripts/check-config-ui.js [--root <каталог>]
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
    console.error("check-config-ui: не удалось прочитать файл: " + e.message);
    process.exit(1);
  }
}

function expect(cond, message) {
  if (!cond) errors.push(message);
}

const SRC = "frontend/src";
const SOURCES_TS = `${SRC}/lib/configSources.ts`;
const LAYER_TS = `${SRC}/lib/configLayer.ts`;
const CONTRACT = `${SRC}/lib/sourcesContract.json`;

// --- 1. Типы ответов против контракта ---------------------------------------

// Контрактное имя -> имя интерфейса во фронтенде (отличаются только два)
const TS_NAME = { SourceError: "SourceErrorPayload", Verdict: "VerdictView" };

function interfaceKeys(src, name) {
  const m = new RegExp(`export interface ${name}\\b[^{]*\\{`).exec(src);
  if (!m) return null;
  let depth = 1;
  let i = m.index + m[0].length;
  const start = i;
  while (i < src.length && depth > 0) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") depth--;
    i++;
  }
  const body = src.slice(start, i - 1);
  // Ключи верхнего уровня: на глубине 0 строки вида `name?: тип;`
  const keys = new Map();
  let d = 0;
  let line = "";
  for (const ch of body) {
    if (ch === "{") d++;
    if (ch === "}") d--;
    if (d === 0 && ch === ";") {
      const km = /^\s*(?:\/\*.*?\*\/\s*)?([a-z_][a-z0-9_]*)(\?)?\s*:/.exec(
        line.replace(/\/\/.*$/gm, ""),
      );
      if (km) keys.set(km[1], km[2] === "?");
      line = "";
    } else if (d === 0 && ch === "}") {
      line = "";
    } else {
      line += ch;
    }
  }
  return keys;
}

const sourcesTs = read(SOURCES_TS);
let contract = {};
try {
  contract = JSON.parse(read(CONTRACT));
} catch (e) {
  errors.push(`${CONTRACT}: не разбирается как JSON: ${e.message}`);
}

for (const [type, spec] of Object.entries(contract)) {
  const tsName = TS_NAME[type] || type;
  const keys = interfaceKeys(sourcesTs, tsName);
  if (!keys) {
    errors.push(`${SOURCES_TS}: нет интерфейса ${tsName} (контракт ${type})`);
    continue;
  }
  for (const k of spec.required) {
    if (!keys.has(k)) errors.push(`${tsName}: нет обязательного ключа "${k}"`);
    else if (keys.get(k))
      errors.push(
        `${tsName}: ключ "${k}" обязателен в контракте, но объявлен как необязательный`,
      );
  }
  for (const k of spec.optional) {
    if (!keys.has(k))
      errors.push(`${tsName}: нет необязательного ключа "${k}"`);
    else if (!keys.get(k))
      errors.push(
        `${tsName}: ключ "${k}" необязателен в контракте, но объявлен как обязательный`,
      );
  }
  const allowed = new Set([...spec.required, ...spec.optional]);
  for (const k of keys.keys()) {
    if (!allowed.has(k))
      errors.push(`${tsName}: лишний ключ "${k}" вне контракта`);
  }
}

// --- 2. Событие sources, цикл импортов, граница флага ------------------------

const layerTs = read(LAYER_TS);
expect(
  /EVENT_NAMES\s*=\s*\[[^\]]*'sources'/.test(layerTs),
  `${LAYER_TS}: 'sources' нет в EVENT_NAMES`,
);
expect(
  /export function onLayerEvent\(/.test(layerTs),
  `${LAYER_TS}: нет onLayerEvent`,
);
expect(
  !/from '\.\/configSources'/.test(layerTs),
  `${LAYER_TS}: импортирует configSources (цикл импортов)`,
);
expect(
  /onLayerEvent\('sources'/.test(sourcesTs),
  `${SOURCES_TS}: нет подписки onLayerEvent('sources', …)`,
);
expect(
  /SOURCES_DEBOUNCE_MS\s*=\s*300/.test(sourcesTs),
  `${SOURCES_TS}: дебаунс перечитывания не 300 мс`,
);
expect(
  /'source_refresh'/.test(layerTs),
  `${LAYER_TS}: нет trigger source_refresh`,
);
expect(
  /step\.trigger/.test(layerTs),
  `${LAYER_TS}: apply_step не читает trigger шага`,
);
expect(
  (layerTs.match(/new EventSource\(/g) || []).length === 1,
  `${LAYER_TS}: потоков событий должно быть ровно один`,
);
expect(
  !/EventSource/.test(sourcesTs),
  `${SOURCES_TS}: открывает собственный поток событий`,
);

// Клиент источников подключают только компоненты раздела «Конфигурация»: иначе при
// выключенном флаге слоя пошли бы запросы /api/configlayer/sources
function walk(dir, out) {
  for (const ent of fs.readdirSync(path.join(root, dir), {
    withFileTypes: true,
  })) {
    const rel = `${dir}/${ent.name}`;
    if (ent.isDirectory()) walk(rel, out);
    else if (/\.(svelte|ts)$/.test(ent.name)) out.push(rel);
  }
  return out;
}
for (const file of walk(SRC, [])) {
  if (file === SOURCES_TS) continue;
  const text = read(file);
  if (
    /from '[^']*\/configSources'/.test(text) &&
    !file.startsWith(`${SRC}/components/config/`)
  ) {
    errors.push(
      `${file}: импортирует configSources вне components/config/ (запросы источников при выключенном флаге)`,
    );
  }
}

// --- 3. Бейдж, уведомления, ход применения ----------------------------------

expect(
  /export const badgeTotal\b/.test(layerTs),
  `${LAYER_TS}: нет badgeTotal`,
);
expect(
  /export const visibleNotices\b/.test(layerTs),
  `${LAYER_TS}: нет visibleNotices`,
);
expect(
  /export const navBadgeCount[^=]*=\s*derived\(badgeTotal/.test(layerTs),
  `${LAYER_TS}: navBadgeCount считается не от badgeTotal`,
);
for (const prefix of [
  "source_rejected:",
  "source_deferred:",
  "source_kernel_refused:",
  "source_network:",
]) {
  expect(
    layerTs.includes(`'${prefix}'`),
    `${LAYER_TS}: уведомления ${prefix} не входят в известные`,
  );
}

const sidebar = read(`${SRC}/components/Sidebar.svelte`);
expect(
  /\$badgeTotal/.test(sidebar),
  `Sidebar.svelte: подпись бейджа не от badgeTotal`,
);

const notices = read(`${SRC}/components/config/ConfigNotices.svelte`);
expect(
  !/\{@html/.test(notices),
  "ConfigNotices.svelte: {@html} запрещён (XSS)",
);
expect(
  /new URL\(/.test(notices),
  "ConfigNotices.svelte: ссылка кабинета не разбирается через new URL",
);
expect(
  /'http:'/.test(notices) && /'https:'/.test(notices),
  "ConfigNotices.svelte: не проверяются схемы http: и https:",
);
expect(
  /target="_blank"/.test(notices),
  'ConfigNotices.svelte: у ссылки нет target="_blank"',
);
expect(
  /rel="noopener noreferrer"/.test(notices),
  'ConfigNotices.svelte: у ссылки нет rel="noopener noreferrer"',
);
expect(
  /web_url/.test(notices),
  "ConfigNotices.svelte: не читается params.web_url",
);
expect(
  /\$visibleNotices/.test(notices),
  "ConfigNotices.svelte: список не из visibleNotices",
);
for (const id of [
  "source_rejected:",
  "source_deferred:",
  "source_kernel_refused:",
  "source_network:",
]) {
  expect(notices.includes(id), `ConfigNotices.svelte: нет разбора ${id}`);
}

const progress = read(`${SRC}/components/config/ApplyProgress.svelte`);
expect(
  /source_refresh/.test(progress),
  "ApplyProgress.svelte: фоновое обновление не отделено от хода применения",
);

// --- 4. Каркас вкладок и ключи i18n -----------------------------------------

const config = read(`${SRC}/Config.svelte`);
expect(
  /data-testid="config-tabs"/.test(config),
  'Config.svelte: нет data-testid="config-tabs"',
);
expect(
  /configSubTab\(/.test(config),
  "Config.svelte: вкладка не берётся из configSubTab(хэш)",
);
for (const id of ["config-tab-nodes", "config-tab-files"]) {
  expect(config.includes(id), `Config.svelte: нет testId ${id}`);
}
const tabs = read(`${SRC}/lib/configSubTab.ts`);
for (const name of ["configSubTab", "configSubTabHash"]) {
  expect(
    new RegExp(`export function ${name}\\(`).test(tabs),
    `configSubTab.ts: нет ${name}`,
  );
}
expect(
  read(`${SRC}/components/config/nodes/NodesTab.svelte`).includes(
    'data-testid="nodes-tab"',
  ),
  'NodesTab.svelte: нет data-testid="nodes-tab"',
);

const KEYS = [
  "cfg.tab.nodes",
  "cfg.tab.files",
  "cfg.tabs_aria",
  "cfg.nodes.empty_title",
  "cfg.nodes.empty_body",
  "cfg.nodes.empty_cta",
  "cfg.nodes.load_error",
  "cfg.nodes.retry",
  "cfg.notice.source_rejected",
  "cfg.notice.source_provider_error",
  "cfg.notice.source_deferred",
  "cfg.notice.source_network",
  "cfg.notice.source_kernel_refused",
  "cfg.notice.cabinet_link",
  "nav.config_badge_aria",
];
for (const lang of ["ru", "en"]) {
  const dict = JSON.parse(read(`${SRC}/locales/${lang}.json`));
  for (const key of KEYS)
    expect(
      typeof dict[key] === "string" && dict[key] !== "",
      `${lang}.json: нет ключа ${key}`,
    );
  // подписи вкладок — не длиннее двух слов, без многоточия
  for (const key of ["cfg.tab.nodes", "cfg.tab.files"]) {
    const v = dict[key] || "";
    expect(
      v.trim().split(/\s+/).length <= 2 && !/[…]|\.\.\./.test(v),
      `${lang}.json: подпись ${key} длиннее двух слов или с многоточием`,
    );
  }
}

if (errors.length > 0) {
  console.error("check-config-ui: найдено нарушений: " + errors.length);
  for (const e of errors) console.error("  - " + e);
  process.exit(1);
}
console.log("check-config-ui: OK");
