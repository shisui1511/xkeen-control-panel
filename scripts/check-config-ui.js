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
 * 5. Поле «Добавить» и предпросмотр: у каждого кода из sourceErrorCodes.json есть
 *    непустые cfg.nodes.err.<код>.title и .hint в ru и en (и общий cfg.nodes.err.unknown),
 *    плейсхолдеры в них — только известные параметры, у счётчиков через $tp есть
 *    формы _one/_few/_many в обоих словарях; разметка компонентов (id поля, отмена
 *    запроса, aria-live, раскрытие, «Подробности», файлы).
 * 6. Список источников и узлов (карточка, строка узла, таблица статусов StatusBadge).
 * 7. Модали (план 145-15): правка узла идёт по исходному тексту через nodeRaw →
 *    previewNode → saveNode с отменой запроса; настройки подписки — Select из проекта,
 *    только изменённые поля, без sockopt и флажков ядер; диагностика — <pre> с
 *    переносом, «Скопировать» копирует показанный текст, «Показать ещё 50»; действия
 *    карточки и строки узла; удаление через showConfirm; полнота словарей.
 *
 * 8. Маршрутизация (план 146-17): вкладки «Группы» и «Правила» и их testId; интерфейсы
 *    configRouting.ts совпадают по ключам с routingContract.json; событие routing,
 *    подписки клиента (дебаунс 300 мс), нет обратного импорта в configLayer.ts и импорта
 *    клиента вне config-раздела; {@html} в компонентах config запрещён; у каждого кода
 *    из routingErrorCodes.json и каждой причины invalid_input есть текст в ru и en;
 *    ключи $tp вкладок имеют формы _one/_few/_many; тексты D-28.
 *
 * 9. Менеджер geodata (план 146.1-07): интерфейсы configGeodata.ts совпадают по ключам с
 *    geodataContract.json; событие geodata слушается в одном SSE-потоке, клиент подписан
 *    на geodata и draft с дебаунсом 300 мс; configLayer.ts клиент не импортирует (нет
 *    цикла), а клиент не импортируется вне Config.svelte и components/config/ (при
 *    выключенном флаге запросов менеджера нет); у каждого кода из geodataErrorCodes.json
 *    есть текст cfg.geo.err.<код> в ru и en, у ключей _one есть _few и _many; ключи D-35
 *    и D-36 на месте; у каждой причины сбоя (константы runReason*, removeReason*,
 *    jqReason*, Reason* пакета geodata и коды незавершённого удаления) есть текст
 *    cfg.geo.reason_<код> в ru и en.
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
// (import type стирается при сборке и запросов не делает)
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
    /^\s*import(?!\s+type\b)[^;]*?from '[^']*\/configSources'/m.test(text) &&
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
  "routing_auto_taken:",
  "routing_reconcile:",
  "routing_repick:",
  "routing_list_missing:",
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
  "routing_auto_taken:",
  "routing_reconcile:",
  "routing_repick:",
  "routing_list_missing:",
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
  "cfg.notice.source_network_no_retry",
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

// --- 5. Поле «Добавить», предпросмотр и тексты ошибок -----------------------

const ADD_FIELD = `${SRC}/components/config/nodes/AddSourceField.svelte`;
const ADD_PREVIEW = `${SRC}/components/config/nodes/AddPreview.svelte`;
const ERRORS_TS = `${SRC}/lib/sourceErrors.ts`;
const CODES = JSON.parse(read(`${SRC}/lib/sourceErrorCodes.json`));

const field = read(ADD_FIELD);
for (const lit of [
  'id="nodes-add-input"',
  'data-testid="nodes-add"',
  "AbortController",
  "onpaste",
  'accept=".conf,.yaml,.yml,.json,.txt"',
  "multiple",
  "previewInput(",
  "addPreview(",
]) {
  expect(field.includes(lit), `AddSourceField.svelte: нет ${lit}`);
}
// предпросмотр не пишет в черновик: из полей ввода вызывается только addPreview
expect(
  !/\b(updateSource|deleteSource|saveNode|deleteNode)\(/.test(field),
  "AddSourceField.svelte: правит черновик не через кнопку «Добавить»",
);
expect(
  !/localStorage|sessionStorage/.test(field),
  "AddSourceField.svelte: вставленный текст не должен попадать в хранилище браузера",
);

const prev = read(ADD_PREVIEW);
for (const lit of [
  'aria-live="polite"',
  'data-testid="nodes-preview"',
  "aria-expanded",
  "<details",
]) {
  expect(prev.includes(lit), `AddPreview.svelte: нет ${lit}`);
}
expect(!/\{@html/.test(prev), "AddPreview.svelte: {@html} в предпросмотре");

const errTs = read(ERRORS_TS);
for (const name of ["sourceErrorText", "verdictReason"]) {
  expect(
    new RegExp(`export function ${name}\\(`).test(errTs),
    `sourceErrors.ts: нет ${name}`,
  );
}
expect(
  /export const SOURCE_ERROR_CODES/.test(errTs) &&
    /cfg\.nodes\.err\.unknown/.test(errTs),
  "sourceErrors.ts: нет SOURCE_ERROR_CODES или запасного текста cfg.nodes.err.unknown",
);
const paramList = /const PARAM_NAMES = \[([\s\S]*?)\];/.exec(errTs);
const PARAMS = new Set(
  paramList ? [...paramList[1].matchAll(/'([a-z_]+)'/g)].map((m) => m[1]) : [],
);
expect(PARAMS.size > 0, "sourceErrors.ts: не найден список PARAM_NAMES");
PARAMS.add("code");

// Параметры, без которых текст теряет смысл (сервер передаёт их всегда)
const NEEDS = {
  http_auth: ["status"],
  expired: ["date"],
  dns_failed: ["host"],
  unknown_scheme: ["scheme"],
  yaml_error: ["line"],
  json_error: ["line"],
  wg_missing_key: ["key", "section"],
  needs_mihomo: ["kernel_min"],
  needs_xray: ["kernel_min"],
};

for (const lang of ["ru", "en"]) {
  const dict = JSON.parse(read(`${SRC}/locales/${lang}.json`));
  let checked = 0;
  for (const code of [...CODES, "unknown"]) {
    const title = dict[`cfg.nodes.err.${code}.title`];
    const hint = dict[`cfg.nodes.err.${code}.hint`];
    expect(
      typeof title === "string" && title.trim() !== "",
      `${lang}.json: нет cfg.nodes.err.${code}.title`,
    );
    expect(
      typeof hint === "string" && hint.trim() !== "",
      `${lang}.json: нет cfg.nodes.err.${code}.hint`,
    );
    checked++;
    for (const text of [title, hint]) {
      for (const m of String(text ?? "").matchAll(/\{([a-z_]+)\}/g)) {
        expect(
          PARAMS.has(m[1]),
          `${lang}.json: cfg.nodes.err.${code} использует неизвестный параметр {${m[1]}}`,
        );
      }
    }
    for (const param of NEEDS[code] ?? []) {
      expect(
        `${title} ${hint}`.includes(`{${param}}`),
        `${lang}.json: cfg.nodes.err.${code} не подставляет {${param}}`,
      );
    }
  }
  expect(
    checked === CODES.length + 1,
    `${lang}.json: проверены не все коды (${checked} из ${CODES.length + 1})`,
  );
  // счётчики через $tp: все три формы в словаре
  for (const base of [
    "cfg.nodes.preview_my",
    "cfg.nodes.stubs",
    "cfg.nodes.added_subs",
    "cfg.nodes.added_nodes",
    "cfg.nodes.preview_skipped",
  ]) {
    for (const suffix of ["_one", "_few", "_many"]) {
      expect(
        typeof dict[base + suffix] === "string" && dict[base + suffix] !== "",
        `${lang}.json: нет формы ${base}${suffix}`,
      );
    }
  }
}

// --- 6. Список источников и узлов (план 145-14) -----------------------------

const CARD = `${SRC}/components/config/nodes/SourceCard.svelte`;
const ROW = `${SRC}/components/config/nodes/NodeRow.svelte`;
const TAB = `${SRC}/components/config/nodes/NodesTab.svelte`;
const VIEW_TS = `${SRC}/lib/nodesView.ts`;

const card = read(CARD);
const row = read(ROW);
const tab = read(TAB);
const view = read(VIEW_TS);

for (const lit of [
  'role="progressbar"',
  "aria-valuenow",
  "aria-expanded",
  'rel="noopener noreferrer"',
  'target="_blank"',
  'data-testid="source-status-cabinet"',
  "refreshSource(",
  "updateSource(",
  "revealUrl(",
  "isDeviceRejected(",
  "safeHttpUrl(",
]) {
  expect(card.includes(lit), `SourceCard.svelte: нет ${lit}`);
}
// ссылки провайдера: каждая с target=_blank несёт rel noopener noreferrer
expect(
  (card.match(/target="_blank"/g) ?? []).length ===
    (card.match(/rel="noopener noreferrer"/g) ?? []).length,
  "SourceCard.svelte: у ссылки с target=_blank нет rel=noopener noreferrer",
);
// ссылки кабинета и поддержки берутся только из safeHttpUrl, не из сырых полей meta
expect(
  !/href=\{(?:meta|source)[.?]/.test(card),
  "SourceCard.svelte: href берётся из сырого поля источника, а не из safeHttpUrl",
);
// полный URL подписки не хранится в сторе и не пишется в журнал
expect(
  !/console\.|localStorage|sessionStorage|\.set\(\s*revealedUrl/.test(card),
  "SourceCard.svelte: полный URL подписки не должен попадать в журнал или хранилище",
);
expect(
  !/revealedUrl\s*=\s*\$state<string \| null>\(\s*source/.test(card),
  "SourceCard.svelte: revealedUrl инициализируется из представления",
);

for (const [name, src] of [
  ["SourceCard.svelte", card],
  ["NodeRow.svelte", row],
  ["NodesTab.svelte", tab],
]) {
  expect(!/\{@html/.test(src), `${name}: {@html}`);
  expect(!/on:click|\$:\s/.test(src), `${name}: устаревший синтаксис Svelte 4`);
  expect(
    !/--bg-surface-hover|--color-primary/.test(src),
    `${name}: неопределённый токен (--bg-surface-hover / --color-primary)`,
  );
  expect(
    !/class="[^"]*\bbadge\b/.test(src),
    `${name}: пометки узлов — .chip, а не .badge`,
  );
}

// узел: приглушение цветом без opacity, без переключателей и флажков, без --danger
expect(row.includes("var(--fg-dim)"), "NodeRow.svelte: нет var(--fg-dim)");
expect(!/opacity/.test(row), "NodeRow.svelte: приглушение прозрачностью");
expect(
  !/<input|role="switch"|toggle-switch|type="checkbox"/.test(row),
  "NodeRow.svelte: у узла нет переключателей и флажков ядер (D-17)",
);
expect(!/--danger/.test(row), "NodeRow.svelte: --danger в пометках узла");
expect(
  /chip chip-info/.test(row) && /chip chip-warning/.test(row),
  "NodeRow.svelte: «только <ядро>» — chip-info, «не поддерживается» — chip-warning",
);
expect(
  !/\bcred|secret|password/i.test(row),
  "NodeRow.svelte: учётные данные узла не показываются (D-34)",
);

// вкладка: сегмент ядра показывается только при двух установленных ядрах
expect(
  tab.includes('data-testid="nodes-kernel-segment"') &&
    /\{#if both\}/.test(tab) &&
    /const both = \$derived\(installed\.xray && installed\.mihomo\)/.test(tab),
  "NodesTab.svelte: сегмент ядра не привязан к двум установленным ядрам",
);

// nodesView.ts: экспорт и таблица соответствия статусов
for (const name of [
  "kernelStatusView",
  "nodesForKernel",
  "trafficLevel",
  "refreshToast",
  "safeHttpUrl",
  "statusLabel",
]) {
  expect(
    new RegExp(`export function ${name}\\(`).test(view),
    `nodesView.ts: нет ${name}`,
  );
}
expect(
  /new URL\(raw\)/.test(view) && /'http:'/.test(view) && /'https:'/.test(view),
  "nodesView.ts: safeHttpUrl не проверяет схему через new URL",
);
const STATUS_VARIANT = {
  not_installed: "idle",
  disabled: "idle",
  refused: "stopped",
  deferred: "warning",
  rejected: "warning",
  provider_error: "warning",
  network: "warning",
  network_no_retry: "warning",
  network_after_apply: "warning",
  draft: "info",
  stopped: "idle",
  ready: "running",
  ok: "running",
  ok_delta: "running",
  none: "idle",
};
const usedStatus = new Map();
for (const m of view.matchAll(
  /view\(\s*'(running|stopped|warning|idle|info)',\s*'([a-z_]+)'/g,
)) {
  usedStatus.set(m[2], m[1]);
}
for (const [key, variant] of Object.entries(STATUS_VARIANT)) {
  expect(
    usedStatus.get(key) === variant,
    `nodesView.ts: статус ${key} должен быть ${variant}, а не ${usedStatus.get(key)}`,
  );
}
expect(
  [...usedStatus.keys()].every((k) => k in STATUS_VARIANT),
  "nodesView.ts: появился статус, которого нет в таблице check-config-ui.js",
);
expect(
  /ratio >= 1\)\s*return 'full'/.test(view) &&
    /ratio >= 0\.9\)\s*return 'warning'/.test(view),
  "nodesView.ts: пороги индикатора трафика — 90% и 100%",
);

// словари: статусы, итоги обновления, счётчики и плюрализация
const refreshKeys = [...view.matchAll(/'(cfg\.nodes\.refresh_[a-z_]+)'/g)].map(
  (m) => m[1],
);
expect(
  refreshKeys.length >= 7,
  "nodesView.ts: не все итоги ручного обновления",
);
for (const lang of ["ru", "en"]) {
  const dict = JSON.parse(read(`${SRC}/locales/${lang}.json`));
  const has = (k) => typeof dict[k] === "string" && dict[k].trim() !== "";
  for (const key of Object.keys(STATUS_VARIANT)) {
    expect(
      has(`cfg.nodes.status.${key}`),
      `${lang}.json: нет cfg.nodes.status.${key}`,
    );
  }
  for (const key of new Set(refreshKeys)) {
    expect(has(key), `${lang}.json: нет ${key}`);
  }
  for (const unit of ["now", "minutes", "hours", "days"]) {
    expect(
      has(`cfg.nodes.ago_${unit}`),
      `${lang}.json: нет cfg.nodes.ago_${unit}`,
    );
  }
  for (const key of [
    "cfg.nodes.search_empty",
    "cfg.nodes.search_placeholder",
    "cfg.nodes.my_empty",
    "cfg.nodes.no_nodes",
    "cfg.nodes.kernel_segment_aria",
    "cfg.nodes.cabinet",
    "cfg.nodes.support",
  ]) {
    expect(has(key), `${lang}.json: нет ${key}`);
  }
  for (const base of [
    "cfg.nodes.sources_count",
    "cfg.nodes.nodes_count",
    "cfg.nodes.stubs_unused",
  ]) {
    for (const suffix of ["_one", "_few", "_many"]) {
      expect(has(base + suffix), `${lang}.json: нет формы ${base}${suffix}`);
    }
  }
}

// --- 7. Модали: правка узла, настройки подписки, диагностика, удаление -------

const NODES_DIR = `${SRC}/components/config/nodes`;
const editModal = read(`${NODES_DIR}/NodeEditModal.svelte`);
const settingsModal = read(`${NODES_DIR}/SourceSettingsModal.svelte`);
const diagModal = read(`${NODES_DIR}/SourceDiagnosticsModal.svelte`);
const cardSrc = read(`${NODES_DIR}/SourceCard.svelte`);
const rowSrc = read(`${NODES_DIR}/NodeRow.svelte`);

// правка узла: исходный текст → тот же распознаватель → запись с ревизией; форм по протоколам нет
for (const call of [
  "nodeRaw(",
  "previewNode(",
  "saveNode(",
  "AbortController",
]) {
  expect(editModal.includes(call), `NodeEditModal.svelte: нет ${call}`);
}
expect(
  /<textarea[^>]*\bid="\{uid\}-text"/.test(editModal) &&
    /for="\{uid\}-text"/.test(editModal),
  "NodeEditModal.svelte: у поля «Исходный текст» нет связанной подписи (label for)",
);
expect(
  /mode="single"/.test(editModal),
  "NodeEditModal.svelte: предпросмотр не в режиме одного узла",
);
expect(
  /DEBOUNCE_MS\s*=\s*600/.test(editModal),
  "NodeEditModal.svelte: дебаунс предпросмотра не 600 мс",
);
expect(
  !/<select\b/.test(editModal) &&
    !/vless|vmess|trojan|hysteria/i.test(editModal),
  "NodeEditModal.svelte: формы под протоколы запрещены (D-13)",
);
expect(
  !/localStorage|sessionStorage|console\./.test(
    editModal + settingsModal + diagModal,
  ),
  "модали: исходный текст и адрес не кладутся в хранилище браузера и не логируются (T-145-40)",
);

// настройки подписки
expect(
  /import Select from/.test(settingsModal) && !/<select\b/.test(settingsModal),
  "SourceSettingsModal.svelte: только Select.svelte, нативного <select> нет",
);
expect(
  settingsModal.includes("updateSource(") &&
    settingsModal.includes("revealUrl("),
  "SourceSettingsModal.svelte: нет updateSource( или revealUrl(",
);
expect(
  !/sockopt|xray_enabled|mihomo_enabled|type="checkbox"/i.test(settingsModal),
  "SourceSettingsModal.svelte: sockopt и флажки ядер запрещены (U-03, D-09)",
);
expect(
  /<details[\s\S]*?cfg\.nodes\.advanced[\s\S]*?<\/details>/.test(
    settingsModal,
  ) && /cfg\.nodes\.hwid_label/.test(settingsModal),
  "SourceSettingsModal.svelte: HWID не в блоке «Дополнительно»",
);
expect(
  /\.filter\(\(line\) => line !== ''\)/.test(settingsModal),
  "SourceSettingsModal.svelte: пустые строки исключений не отбрасываются",
);
expect(
  /EXCLUDE_MAX_ROWS\s*=\s*8/.test(settingsModal),
  "SourceSettingsModal.svelte: textarea исключений растёт не до 8 строк",
);
const intervalKeys = ["provider", "1", "3", "6", "12", "24", "off"];
for (const k of intervalKeys) {
  expect(
    settingsModal.includes(`'cfg.nodes.interval_${k}'`),
    `SourceSettingsModal.svelte: нет варианта интервала ${k}`,
  );
}

// диагностика
expect(
  diagModal.includes("fetchDiagnostics(") && /<pre\b/.test(diagModal),
  "SourceDiagnosticsModal.svelte: нет fetchDiagnostics( или <pre",
);
expect(
  !diagModal.includes("{@html"),
  "SourceDiagnosticsModal.svelte: {@html} запрещён",
);
expect(
  /copy\(headersText\)/.test(diagModal) && /copy\(bodyText\)/.test(diagModal),
  "SourceDiagnosticsModal.svelte: «Скопировать» должна копировать показанный текст",
);
expect(
  /PAGE_FIRST/.test(diagModal) && /PAGE_STEP/.test(diagModal),
  "SourceDiagnosticsModal.svelte: нет «Показать ещё 50» после 100 строк",
);
expect(
  /overflow-wrap:\s*anywhere/.test(diagModal) &&
    /\.dg-pre[\s\S]*?font-size:\s*var\(--font-size-xs\)/.test(diagModal),
  "SourceDiagnosticsModal.svelte: <pre> без переноса или не mono 12px",
);
expect(
  /diag\.empty/.test(diagModal) && /cfg\.nodes\.diag_all_ok/.test(diagModal),
  "SourceDiagnosticsModal.svelte: нет пустого состояния или «Все строки разобраны»",
);
expect(
  /variant="pill"/.test(diagModal) && /SegmentedControl/.test(diagModal),
  "SourceDiagnosticsModal.svelte: сегмент ядер и вкладки pill",
);

// карточка и строка узла
expect(
  cardSrc.includes("showConfirm(") &&
    cardSrc.includes("deleteSource(") &&
    cardSrc.includes("deleteNode("),
  "SourceCard.svelte: удаление подписки и узла идёт не через showConfirm( + deleteSource( / deleteNode(",
);
expect(
  /\{:else\}[\s\S]*SourceSettingsModal[\s\S]*SourceDiagnosticsModal/.test(
    cardSrc,
  ) && /\{#if isMy\}[\s\S]*NodeEditModal/.test(cardSrc),
  "SourceCard.svelte: «Настройки» и «Диагностика» — только у подписок, «Изменить» — только у «Моих узлов»",
);
expect(
  /node\.entry_id && \(onedit \|\| ondelete\)/.test(rowSrc),
  "NodeRow.svelte: действия строки — только у узлов с entry_id (Мои узлы)",
);
expect(
  /aria-label=\{\$t\('cfg\.nodes\.edit_aria'/.test(rowSrc) &&
    /aria-label=\{\$t\('cfg\.nodes\.delete_aria'/.test(rowSrc),
  "NodeRow.svelte: у icon-кнопок нет aria-label",
);

// запрещённые токены и хардкодные цвета в новых файлах
for (const [name, src] of [
  ["NodeEditModal.svelte", editModal],
  ["SourceSettingsModal.svelte", settingsModal],
  ["SourceDiagnosticsModal.svelte", diagModal],
]) {
  expect(
    !/--bg-surface-hover|--color-primary/.test(src),
    `${name}: неопределённые токены --bg-surface-hover / --color-primary`,
  );
  expect(
    !/on:click|\$:\s/.test(src),
    `${name}: синтаксис Svelte 4 (on:click, $:)`,
  );
  expect(
    !/style="[^"]*#[0-9a-fA-F]{3,8}/.test(src),
    `${name}: цвет в inline-стиле`,
  );
}

// словари: ключи модалей в обоих языках, формы счётчика удаления подписки
const MODAL_KEYS = [
  "btn_edit",
  "btn_delete",
  "btn_settings",
  "btn_diag",
  "btn_delete_source",
  "edit_title",
  "edit_name",
  "edit_text",
  "edit_warning",
  "edit_aria",
  "delete_aria",
  "save_draft",
  "save_failed",
  "cancel",
  "settings_title",
  "settings_name",
  "settings_url",
  "settings_exclude",
  "settings_interval",
  "interval_provider",
  "interval_1",
  "interval_3",
  "interval_6",
  "interval_12",
  "interval_24",
  "interval_off",
  "advanced",
  "hwid_label",
  "hwid_placeholder",
  "diag_title",
  "diag_empty",
  "diag_all_ok",
  "diag_no_response",
  "diag_meta_none",
  "tab_report",
  "tab_headers",
  "tab_raw",
  "delete_sub_title",
  "delete_sub_btn",
  "delete_node_title",
  "delete_node_body",
  "delete_node_btn",
];
for (const lang of ["ru", "en"]) {
  const dict = JSON.parse(read(`${SRC}/locales/${lang}.json`));
  const has = (k) => typeof dict[k] === "string" && dict[k].trim() !== "";
  for (const key of MODAL_KEYS) {
    expect(has(`cfg.nodes.${key}`), `${lang}.json: нет cfg.nodes.${key}`);
  }
  for (const suffix of ["_one", "_few", "_many"]) {
    expect(
      has(`cfg.nodes.delete_sub_body${suffix}`),
      `${lang}.json: нет формы cfg.nodes.delete_sub_body${suffix}`,
    );
  }
}

// --- 8. Маршрутизация: вкладки «Группы» и «Правила», клиент, словари (план 146-17) --------

const ROUTING_TS = `${SRC}/lib/configRouting.ts`;
const ROUTING_CONTRACT = `${SRC}/lib/routingContract.json`;
const ROUTING_CODES = `${SRC}/lib/routingErrorCodes.json`;

// Каркас: вкладки, маршруты, компоненты
for (const id of ["config-tab-groups", "config-tab-rules"]) {
  expect(config.includes(id), `Config.svelte: нет testId ${id}`);
}
for (const tabName of ["groups", "rules"]) {
  expect(
    new RegExp(`'${tabName}'`).test(tabs),
    `configSubTab.ts: вкладка ${tabName} не известна`,
  );
}
for (const [file, id] of [
  [
    `${SRC}/components/config/groups/GroupsTab.svelte`,
    'data-testid="groups-tab"',
  ],
  [
    `${SRC}/components/config/rules/RulesTab.svelte`,
    'data-testid="rules-tab"',
  ],
  [
    `${SRC}/components/config/rules/RulesTab.svelte`,
    'data-testid="rules-fallback"',
  ],
]) {
  expect(read(file).includes(id), `${file}: нет ${id}`);
}

// Каждый код предупреждения проверки (warn* в validate.go и lint.go) разбирает
// фронтенд: иначе предупреждение показалось бы общим «Не удалось проверить
// черновик» (issueTextKey)
{
  const warnCodes = [];
  for (const file of [
    "internal/services/routing/validate.go",
    "internal/services/routing/lint.go",
  ]) {
    for (const m of read(file).matchAll(/^\s*warn[A-Za-z]+\s*=\s*"([a-z_]+)"/gm)) {
      warnCodes.push(m[1]);
    }
  }
  expect(warnCodes.length > 0, "validate.go/lint.go: не найдены коды предупреждений warn*");
  const uiText = walk(`${SRC}/components/config`, [])
    .map((f) => read(f))
    .join("\n");
  for (const code of warnCodes) {
    expect(
      uiText.includes(`'${code}'`),
      `код предупреждения ${code} (бэкенд) не разбирается в components/config`,
    );
  }
}

// Типы ответов против routingContract.json; интерфейс может наследовать другой
// (`extends`), RoutingSnapshot и RoutingKernelView объявлены в configLayer.ts
// (configLayer не импортирует configRouting)
const routingTs = read(ROUTING_TS);
const routingSources = [routingTs, layerTs];
function routingInterfaceKeys(name, seen = new Set()) {
  if (seen.has(name)) return null;
  seen.add(name);
  for (const src of routingSources) {
    const own = interfaceKeys(src, name);
    if (!own) continue;
    const head = new RegExp(
      `export interface ${name}\\s+extends\\s+([A-Za-z0-9_,\\s]+?)\\s*\\{`,
    ).exec(src);
    if (head) {
      for (const parent of head[1].split(",").map((x) => x.trim())) {
        const inherited = routingInterfaceKeys(parent, seen);
        if (inherited) for (const [k, v] of inherited) own.set(k, v);
      }
    }
    return own;
  }
  return null;
}
let routingContract = {};
try {
  routingContract = JSON.parse(read(ROUTING_CONTRACT));
} catch (e) {
  errors.push(`${ROUTING_CONTRACT}: не разбирается как JSON: ${e.message}`);
}
for (const [type, spec] of Object.entries(routingContract)) {
  const keys = routingInterfaceKeys(type);
  if (!keys) {
    errors.push(
      `${ROUTING_TS}: нет интерфейса ${type} (контракт маршрутизации)`,
    );
    continue;
  }
  for (const k of spec.required) {
    if (!keys.has(k)) errors.push(`${type}: нет обязательного ключа "${k}"`);
    else if (keys.get(k))
      errors.push(
        `${type}: ключ "${k}" обязателен в контракте, но объявлен как необязательный`,
      );
  }
  for (const k of spec.optional) {
    if (!keys.has(k)) errors.push(`${type}: нет необязательного ключа "${k}"`);
    else if (!keys.get(k))
      errors.push(
        `${type}: ключ "${k}" необязателен в контракте, но объявлен как обязательный`,
      );
  }
  const allowed = new Set([...spec.required, ...spec.optional]);
  for (const k of keys.keys()) {
    if (!allowed.has(k))
      errors.push(`${type}: лишний ключ "${k}" вне контракта`);
  }
}

// События SSE: routing слушается в одном потоке, клиент маршрутизации подписан на
// routing, sources и draft с дебаунсом 300 мс; обратного импорта нет
expect(
  /EVENT_NAMES\s*=\s*\[[^\]]*'routing'/.test(layerTs),
  `${LAYER_TS}: 'routing' нет в EVENT_NAMES`,
);
expect(
  !/from '\.\/configRouting'/.test(layerTs),
  `${LAYER_TS}: импортирует configRouting (цикл импортов)`,
);
for (const ev of ["routing", "sources", "draft"]) {
  expect(
    new RegExp(`onLayerEvent\\('${ev}'`).test(routingTs),
    `${ROUTING_TS}: нет подписки onLayerEvent('${ev}', …)`,
  );
}
expect(
  /ROUTING_DEBOUNCE_MS\s*=\s*300/.test(routingTs),
  `${ROUTING_TS}: дебаунс перечитывания не 300 мс`,
);
expect(
  !/EventSource/.test(routingTs),
  `${ROUTING_TS}: открывает собственный поток событий`,
);
expect(
  /export async function loadRouting\(/.test(routingTs) &&
    routingTs.includes("/api/configlayer/routing"),
  `${ROUTING_TS}: нет loadRouting или адреса /api/configlayer/routing`,
);

// Клиент маршрутизации подключают только Config.svelte и components/config/: иначе при
// выключенном флаге слоя пошли бы запросы /api/configlayer/routing (T-146-51)
for (const file of walk(SRC, [])) {
  if (file === ROUTING_TS) continue;
  const text = read(file);
  if (
    /^\s*import(?!\s+type\b)[^;]*?from '[^']*\/configRouting'/m.test(text) &&
    file !== `${SRC}/Config.svelte` &&
    !file.startsWith(`${SRC}/components/config/`)
  ) {
    errors.push(
      `${file}: импортирует configRouting вне Config.svelte и components/config/ (запросы маршрутизации при выключенном флаге)`,
    );
  }
}

// XSS (T-146-50): имена групп и значения условий выводятся только интерполяцией
for (const file of walk(`${SRC}/components/config`, [])) {
  if (file.endsWith(".svelte") && /\{@html/.test(read(file))) {
    errors.push(`${file}: {@html} запрещён в компонентах config (XSS)`);
  }
}

// Словари: коды ошибок API, причины invalid_input, подписи вкладок, тексты D-28
const routingCodes = JSON.parse(read(ROUTING_CODES));
const reasonsList = /ROUTING_REASONS\s*=\s*\[([\s\S]*?)\];/.exec(routingTs);
const routingReasons = reasonsList
  ? [...reasonsList[1].matchAll(/'([a-z_]+)'/g)].map((m) => m[1])
  : [];
expect(
  routingReasons.length > 0,
  `${ROUTING_TS}: не найден список ROUTING_REASONS`,
);

// Ключи $tp в компонентах вкладок маршрутизации: нужны все три формы
const tpKeys = new Set();
for (const dir of ["groups", "rules", "routing"]) {
  const rel = `${SRC}/components/config/${dir}`;
  if (!fs.existsSync(path.join(root, rel))) continue;
  for (const file of walk(rel, [])) {
    for (const m of read(file).matchAll(/\$tp\(\s*['"`]([^'"`]+)['"`]/g)) {
      tpKeys.add(m[1]);
    }
  }
}

for (const lang of ["ru", "en"]) {
  const dict = JSON.parse(read(`${SRC}/locales/${lang}.json`));
  const has = (k) => typeof dict[k] === "string" && dict[k].trim() !== "";
  for (const code of [...routingCodes, "unknown"]) {
    expect(
      has(`cfg.routing.err.${code}`),
      `${lang}.json: нет cfg.routing.err.${code}`,
    );
  }
  for (const reason of routingReasons) {
    expect(
      has(`cfg.routing.reason.${reason}`),
      `${lang}.json: нет cfg.routing.reason.${reason}`,
    );
  }
  for (const key of ["cfg.tab.groups", "cfg.tab.rules"]) {
    const v = dict[key] || "";
    expect(
      has(key) && v.trim().split(/\s+/).length <= 2 && !/[…]|\.\.\./.test(v),
      `${lang}.json: подпись ${key} пуста, длиннее двух слов или с многоточием`,
    );
  }
  for (const base of [
    "cfg.groups.count",
    "cfg.rules.count",
    "cfg.routing.apply_blocked",
    "cfg.groups.preview_nodes",
    "cfg.groups.del_rules",
    "cfg.groups.del_groups",
    "cfg.groups.del_effect_xray",
    "cfg.preset.plan_first_rules",
    "cfg.preset.plan_first_groups",
    ...tpKeys,
  ]) {
    for (const suffix of ["_one", "_few", "_many"]) {
      expect(has(base + suffix), `${lang}.json: нет формы ${base}${suffix}`);
    }
  }
  // тексты D-28 (в английском словаре — перевод без русской фразы)
  const d28 = lang === "ru" ? /взятия маршрутизации/ : /routing is taken over/;
  expect(
    d28.test(dict["cfg.restart.no_runtime_effect"] || "") &&
      d28.test(dict["cfg.step_note.no_runtime_effect"] || ""),
    `${lang}.json: тексты no_runtime_effect (D-28) не заменены`,
  );
  // каждая группа ключей фазы непуста
  for (const prefix of [
    "cfg.groups.",
    "cfg.rules.",
    "cfg.routing.",
    "cfg.list.",
    "cfg.geodata.",
    "cfg.preset.",
  ]) {
    expect(
      Object.keys(dict).some((k) => k.startsWith(prefix)),
      `${lang}.json: нет ключей ${prefix}*`,
    );
  }
}

// --- 9. Менеджер geodata: клиент, событие, словари (план 146.1-07) --------

const GEODATA_TS = `${SRC}/lib/configGeodata.ts`;
const GEODATA_CONTRACT = `${SRC}/lib/geodataContract.json`;
const GEODATA_CODES = `${SRC}/lib/geodataErrorCodes.json`;

// Типы ответов против geodataContract.json
const geodataTs = read(GEODATA_TS);
let geodataContract = {};
try {
  geodataContract = JSON.parse(read(GEODATA_CONTRACT));
} catch (e) {
  errors.push(`${GEODATA_CONTRACT}: не разбирается как JSON: ${e.message}`);
}
for (const [type, spec] of Object.entries(geodataContract)) {
  const keys = interfaceKeys(geodataTs, type);
  if (!keys) {
    errors.push(`${GEODATA_TS}: нет интерфейса ${type} (контракт geodata)`);
    continue;
  }
  for (const k of spec.required) {
    if (!keys.has(k)) errors.push(`${type}: нет обязательного ключа "${k}"`);
    else if (keys.get(k))
      errors.push(
        `${type}: ключ "${k}" обязателен в контракте, но объявлен как необязательный`,
      );
  }
  for (const k of spec.optional) {
    if (!keys.has(k)) errors.push(`${type}: нет необязательного ключа "${k}"`);
    else if (!keys.get(k))
      errors.push(
        `${type}: ключ "${k}" необязателен в контракте, но объявлен как обязательный`,
      );
  }
  const allowed = new Set([...spec.required, ...spec.optional]);
  for (const k of keys.keys()) {
    if (!allowed.has(k))
      errors.push(`${type}: лишний ключ "${k}" вне контракта`);
  }
  // Вложенные типы контракта (children) объявлены в контракте и в клиенте
  for (const child of Object.values(spec.children || {})) {
    const childType = child.replace(/\[\]$/, "");
    expect(
      Object.prototype.hasOwnProperty.call(geodataContract, childType),
      `${GEODATA_CONTRACT}: тип ${childType} (поле типа ${type}) не описан в контракте`,
    );
  }
}

// Событие geodata: один SSE-поток, подписки клиента с дебаунсом, нет обратного импорта
expect(
  /EVENT_NAMES\s*=\s*\[[^\]]*'geodata'/.test(layerTs),
  `${LAYER_TS}: 'geodata' нет в EVENT_NAMES`,
);
expect(
  !/from '\.\/configGeodata'/.test(layerTs),
  `${LAYER_TS}: импортирует configGeodata (цикл импортов)`,
);
expect(
  /export const geodataHolder\b/.test(layerTs) &&
    /busy_holder\?:\s*string/.test(layerTs),
  `${LAYER_TS}: нет стора geodataHolder или поля busy_holder снимка`,
);
for (const ev of ["geodata", "draft"]) {
  expect(
    new RegExp(`onLayerEvent\\('${ev}'`).test(geodataTs),
    `${GEODATA_TS}: нет подписки onLayerEvent('${ev}', …)`,
  );
}
expect(
  /GEODATA_DEBOUNCE_MS\s*=\s*300/.test(geodataTs),
  `${GEODATA_TS}: дебаунс перечитывания не 300 мс`,
);
expect(
  !/EventSource/.test(geodataTs),
  `${GEODATA_TS}: открывает собственный поток событий`,
);
expect(
  /export async function loadGeodata\(/.test(geodataTs) &&
    geodataTs.includes("/api/configlayer/geodata"),
  `${GEODATA_TS}: нет loadGeodata или адреса /api/configlayer/geodata`,
);
// Уведомления менеджера (D-23): без них ConfigNotices их отбросит
for (const id of [
  "'geodata_update:'",
  "'geodata_cleanup:'",
  "'geodata_include:'",
  "'geodata_delete:'",
  "'geodata_cron_two'",
]) {
  expect(layerTs.includes(id), `${LAYER_TS}: нет идентификатора уведомления ${id}`);
}

// Клиент менеджера подключают только Config.svelte и components/config/: иначе при
// выключенном флаге слоя пошли бы запросы /api/configlayer/geodata
for (const file of walk(SRC, [])) {
  if (file === GEODATA_TS) continue;
  const text = read(file);
  if (
    /^\s*import(?!\s+type\b)[^;]*?from '[^']*\/configGeodata'/m.test(text) &&
    file !== `${SRC}/Config.svelte` &&
    !file.startsWith(`${SRC}/components/config/`)
  ) {
    errors.push(
      `${file}: импортирует configGeodata вне Config.svelte и components/config/ (запросы менеджера при выключенном флаге)`,
    );
  }
}

// Словари менеджера: тексты кодов ошибок API, формы $tp, ключи D-35 и D-36
const geodataCodes = JSON.parse(read(GEODATA_CODES));
expect(
  Array.isArray(geodataCodes) && geodataCodes.length > 0,
  `${GEODATA_CODES}: пустой список кодов ошибок`,
);
// Причины сбоев, которые клиент показывает через cfg.geo.reason_<код>: константы причин
// пакета geodata (запуск, jq, удаление, разбор вывода) и коды правки xkeen.json, с
// которыми остаётся незавершённое удаление (PendingDelete.Reason)
const GEODATA_PKG = "internal/services/geodata";
const geodataReasons = new Set([
  "foreign_ref",
  "xkeen_json_invalid",
  "geodata_changed",
  "comments_lost",
]);
for (const name of fs.readdirSync(path.join(root, GEODATA_PKG))) {
  if (!name.endsWith(".go") || name.endsWith("_test.go")) continue;
  const text = read(`${GEODATA_PKG}/${name}`);
  for (const m of text.matchAll(
    /^\s*(?:runReason|removeReason|jqReason|Reason)\w*\s*=\s*"([a-z0-9_]+)"/gm,
  )) {
    geodataReasons.add(m[1]);
  }
}
expect(
  geodataReasons.size > 4,
  `${GEODATA_PKG}: не найдены константы причин (runReason*, removeReason*, jqReason*, Reason*)`,
);
for (const lang of ["ru", "en"]) {
  const dict = JSON.parse(read(`${SRC}/locales/${lang}.json`));
  const has = (k) => typeof dict[k] === "string" && dict[k].trim() !== "";
  for (const code of geodataReasons) {
    expect(
      has(`cfg.geo.reason_${code}`),
      `${lang}.json: нет текста причины cfg.geo.reason_${code}`,
    );
  }
  for (const code of [...geodataCodes, "unknown"]) {
    expect(
      has(`cfg.geo.err.${code}`),
      `${lang}.json: нет cfg.geo.err.${code}`,
    );
  }
  for (const key of Object.keys(dict)) {
    if (!key.startsWith("cfg.geo.") || !key.endsWith("_one")) continue;
    const base = key.slice(0, -"_one".length);
    for (const suffix of ["_few", "_many"]) {
      expect(has(base + suffix), `${lang}.json: нет формы ${base}${suffix}`);
    }
  }
  for (const key of [
    "cfg.tab.lists",
    "cfg.geo.res_unverified",
    "cfg.geo.res_unverified_limit",
    "cfg.geo.cron_suspended",
  ]) {
    expect(has(key), `${lang}.json: нет ключа ${key}`);
  }
}

if (errors.length > 0) {
  console.error("check-config-ui: найдено нарушений: " + errors.length);
  for (const e of errors) console.error("  - " + e);
  process.exit(1);
}
console.log("check-config-ui: OK");
