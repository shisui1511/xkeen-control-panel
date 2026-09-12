/* eslint-disable */
// Sibling script to check-contrast.cjs — детектор литеральных hex-цветов
// в исходниках frontend/src/**/*.svelte (DS2-06). Скан ограничен блоками
// <style> и inline-атрибутами style="" — не подключён к check:contrast
// в этой волне, подключение выполняет план 120-14 после схождения счётчика.
//
// Охват намеренно ограничен шестнадцатеричной записью цвета (# + 3/4/6/8
// знаков), включая второй аргумент функции var(). Функциональные
// полупрозрачные записи (rgba(...)) и презентационные атрибуты SVG
// (fill=, stroke=) вне охвата этой фазы.
const fs = require('fs');
const path = require('path');

// Помощник extractBlock переиспользуется из check-contrast.cjs, но его
// brace-counting рассчитан на CSS-правила вида ".foo { ... }" — на границе
// тега <style> он не возвращает содержимое (не считаем это багом нашего
// скрипта: извлечение тега здесь делает собственный regex ниже).
require('./check-contrast.cjs');

const SRC_ROOT = path.join(__dirname, '../src');

// Хешевая запись цвета: # + 3/4/6/8 hex-знаков, на границе слова.
const HEX_VALUE = '#[0-9a-fA-F]{8}\\b|#[0-9a-fA-F]{6}\\b|#[0-9a-fA-F]{4}\\b|#[0-9a-fA-F]{3}\\b';
// Находкой считается hex-запись, которой предшествует ':', ',' или '(' —
// то есть она стоит в значении CSS-декларации (после property: ...), в
// списке значений (после запятой) или во втором аргументе var(...). Такой
// префикс естественно исключает CSS ID-селекторы (#sp-group { ... }), у
// которых перед '#' нет ни одного из этих символов.
const VALUE_HEX_RE = new RegExp(`[:,(]\\s*(${HEX_VALUE})`, 'g');

const STYLE_BLOCK_RE = /<style\b[^>]*>([\s\S]*?)<\/style>/g;
const INLINE_STYLE_RE = /\bstyle\s*=\s*(?:"([^"]*)"|'([^']*)')/g;

// Whitelist — явные записи с обязательным обоснованием на каждую. Молчаливое
// исключение по маске каталога запрещено. Пара ниже — тема-инвариантная
// терминальная поверхность (--bg-terminal/--fg-terminal, global.css), задана
// намеренно фиксированной в обеих темах (см. global.css рядом с токенами).
const WHITELIST = [
  {
    file: 'styles/global.css',
    value: '#050d16',
    reason:
      'Терминальная поверхность --bg-terminal намеренно тема-инвариантна: консоль читается как тёмная поверхность в обеих темах (global.css)'
  },
  {
    file: 'styles/global.css',
    value: '#d9e7f4',
    reason:
      'Терминальная поверхность --fg-terminal намеренно тема-инвариантна: парный к --bg-terminal литерал переднего плана (global.css)'
  }
];

function isWhitelisted(file, value) {
  const lowerValue = value.toLowerCase();
  return WHITELIST.some((w) => w.file === file && w.value.toLowerCase() === lowerValue);
}

function lineAt(content, index) {
  return content.slice(0, index).split('\n').length;
}

/**
 * Сканирует содержимое одного файла (строка целиком, как прочитанная с диска)
 * и возвращает находки { file, line, value }. `relFile` — путь файла
 * относительно frontend/src, используется для сверки с WHITELIST и для
 * человекочитаемого вывода.
 */
function scanContent(content, relFile) {
  const findings = [];

  // (а) содержимое блоков <style>
  let styleMatch;
  STYLE_BLOCK_RE.lastIndex = 0;
  while ((styleMatch = STYLE_BLOCK_RE.exec(content)) !== null) {
    const blockContent = styleMatch[1];
    const blockStart = styleMatch.index + styleMatch[0].indexOf(blockContent);
    let hexMatch;
    VALUE_HEX_RE.lastIndex = 0;
    while ((hexMatch = VALUE_HEX_RE.exec(blockContent)) !== null) {
      const value = hexMatch[1];
      if (isWhitelisted(relFile, value)) continue;
      const absoluteIndex = blockStart + hexMatch.index + hexMatch[0].indexOf(value);
      findings.push({ file: relFile, line: lineAt(content, absoluteIndex), value });
    }
  }

  // (б) значения inline-атрибутов style=""
  let inlineMatch;
  INLINE_STYLE_RE.lastIndex = 0;
  while ((inlineMatch = INLINE_STYLE_RE.exec(content)) !== null) {
    const attrValue = inlineMatch[1] ?? inlineMatch[2] ?? '';
    const attrStart = inlineMatch.index + inlineMatch[0].indexOf(attrValue);
    let hexMatch;
    VALUE_HEX_RE.lastIndex = 0;
    while ((hexMatch = VALUE_HEX_RE.exec(attrValue)) !== null) {
      const value = hexMatch[1];
      if (isWhitelisted(relFile, value)) continue;
      const absoluteIndex = attrStart + hexMatch.index + hexMatch[0].indexOf(value);
      findings.push({ file: relFile, line: lineAt(content, absoluteIndex), value });
    }
  }

  return findings;
}

function toSrcRelative(filePath) {
  const abs = path.resolve(filePath);
  return path.relative(SRC_ROOT, abs).split(path.sep).join('/');
}

function scanFile(filePath) {
  const relFile = toSrcRelative(filePath);
  const content = fs.readFileSync(filePath, 'utf8');
  return scanContent(content, relFile);
}

function walkSvelteFiles(dir) {
  const results = [];
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  for (const entry of entries) {
    if (entry.isSymbolicLink()) continue; // не следовать символьным ссылкам
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      results.push(...walkSvelteFiles(full));
    } else if (entry.isFile() && entry.name.endsWith('.svelte')) {
      results.push(full);
    }
  }
  return results;
}

function scanDir(dir) {
  const findings = [];
  for (const file of walkSvelteFiles(dir)) {
    findings.push(...scanFile(file));
  }
  return findings;
}

function main() {
  const args = process.argv.slice(2);
  const pathIndex = args.indexOf('--path');
  const findings =
    pathIndex !== -1 && args[pathIndex + 1] ? scanFile(args[pathIndex + 1]) : scanDir(SRC_ROOT);

  for (const f of findings) {
    console.log(`${f.file}:${f.line} ${f.value}`);
  }
  console.log(`\nВсего находок: ${findings.length}`);

  process.exit(findings.length > 0 ? 1 : 0);
}

module.exports = { scanContent, scanFile, scanDir, WHITELIST, SRC_ROOT };

if (require.main === module) {
  main();
}
