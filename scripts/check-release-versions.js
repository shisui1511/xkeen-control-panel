#!/usr/bin/env node
/*
 * check-release-versions.js — номера версий панели в пользовательских текстах (D-22, D-23).
 *
 * Тексты UI и установщика не называют версию панели: «будет в v0.N.N» устаревает, а невышедший
 * номер вводит в заблуждение. Страж краснеет на любом номере линии панели в:
 *   - строковых значениях frontend/src/locales/{en,ru}.json и internal/i18n/locales/{en,ru}.json
 *     (место — `файл:ключ`);
 *   - непрокомментированных строках frontend/src/i18n.ts (место — `файл:номер`);
 *   - строках scripts/setup.sh, первый непробельный символ которых не `#` (место — `файл:номер`).
 * Версии ядер и чужих программ (Xray 1.8.0, Mihomo 0.19.0 и т. п.) и IP-адреса не считаются.
 *
 * Исключения:
 *   - JSON-локали без комментариев: список KEY_EXCEPTIONS ниже (файл → ключ → причина).
 *   - scripts/setup.sh: `# version-ok: <версия> <причина>` в той же строке.
 *
 * Major линии берётся от наибольшего тега v* (без тегов — 0). Самопроверка разбора на образцах
 * идёт при каждом запуске: расхождение — код 1.
 *
 * Usage: node scripts/check-release-versions.js
 */
const fs = require('fs');
const path = require('path');
const { localTags, maxCore, findPanelVersions } = require('./lib/release-versions.js');

const root = path.resolve(__dirname, '..');

const JSON_FILES = [
  'frontend/src/locales/en.json',
  'frontend/src/locales/ru.json',
  'internal/i18n/locales/en.json',
  'internal/i18n/locales/ru.json'
];
const TS_FILE = 'frontend/src/i18n.ts';
const SH_FILE = 'scripts/setup.sh';

// Исключения для JSON-локалей: { '<файл>': { '<ключ>': '<причина>' } }. Причина обязательна.
const KEY_EXCEPTIONS = {};

// Рекурсивно обходит строковые значения JSON; ключ — путь через точку.
function walkStrings(value, keyPath, visit) {
  if (typeof value === 'string') {
    visit(keyPath, value);
  } else if (Array.isArray(value)) {
    value.forEach((v, i) => walkStrings(v, `${keyPath}[${i}]`, visit));
  } else if (value && typeof value === 'object') {
    for (const [k, v] of Object.entries(value)) walkStrings(v, keyPath ? `${keyPath}.${k}` : k, visit);
  }
}

function isCommentLine(line, markers) {
  const t = line.trimStart();
  return markers.some(m => t.startsWith(m));
}

// Нарушения в строках файла: строки-комментарии пропускаются; отметка version-ok действует в строке.
function lineIssues(text, commentMarkers, major) {
  const out = [];
  text.split('\n').forEach((line, idx) => {
    if (isCommentLine(line, commentMarkers)) return;
    for (const f of findPanelVersions(line, major)) out.push({ where: idx + 1, version: f.version });
  });
  return out;
}

// Самопроверка: если разбор сломан, страж молчал бы. [текст, ожидаемое число нарушений]
const samples = [
  ['будет в v0.15.0', 1],
  ['Mihomo 0.19.0 и выше', 0],
  ['адрес 0.0.0.0 и 127.0.0.1', 0],
  ['версия 0.99.0 # version-ok: v0.99.0 пример формата', 0]
];

const problems = [];
for (const [text, want] of samples) {
  const got = lineIssues(text, ['#'], 0).length;
  if (got !== want) {
    problems.push(`самопроверка разбора: образец ${JSON.stringify(text)} дал нарушений ${got}; ожидалось ${want}`);
  }
}

let major = 0;
try {
  const max = maxCore(localTags(root));
  if (max) major = max.major;
} catch (e) {
  problems.push(`теги не прочитаны: ${e.message}`);
}

const violations = [];

for (const file of JSON_FILES) {
  let data;
  try {
    data = JSON.parse(fs.readFileSync(path.join(root, file), 'utf8'));
  } catch (e) {
    problems.push(`${file}: не прочитан: ${e.message}`);
    continue;
  }
  const exceptions = KEY_EXCEPTIONS[file] || {};
  walkStrings(data, '', (key, value) => {
    const reason = exceptions[key];
    if (typeof reason === 'string' && reason.trim().length >= 3) return;
    for (const f of findPanelVersions(value, major)) {
      violations.push(`${file}:${key}: номер ${f.version} версии панели в тексте`);
    }
  });
}

for (const [file, markers] of [
  [TS_FILE, ['//', '*', '/*']],
  [SH_FILE, ['#']]
]) {
  try {
    const text = fs.readFileSync(path.join(root, file), 'utf8');
    for (const i of lineIssues(text, markers, major)) {
      violations.push(`${file}:${i.where}: номер ${i.version} версии панели в тексте`);
    }
  } catch (e) {
    problems.push(`${file}: не прочитан: ${e.message}`);
  }
}

if (violations.length > 0 || problems.length > 0) {
  console.error('❌ Номера версий панели в пользовательских текстах:');
  for (const v of violations) console.error('  ' + v);
  for (const p of problems) console.error('  ' + p);
  if (violations.length > 0) {
    console.error(
      'Уберите номер из текста. Исключение: ключ в KEY_EXCEPTIONS стража (JSON) с причиной или `# version-ok: <версия> <причина>` в строке setup.sh.'
    );
  }
  process.exit(1);
}
console.log('✅ Номеров версий панели в пользовательских текстах нет');
