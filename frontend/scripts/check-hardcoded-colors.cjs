/* eslint-disable */
// Sibling script to check-contrast.cjs — детектор литеральных hex-цветов
// в исходниках frontend/src/**/*.svelte (DS2-06). Скан ограничен блоками
// <style> и inline-атрибутами style="" — подключён к npm run check:contrast
// (frontend/package.json) и запускается в CI на каждой сборке.
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
const HEX_VALUE_RE = new RegExp(HEX_VALUE, 'g');
// Находкой считается hex-запись, встретившаяся где угодно внутри значения
// CSS-декларации вида "property: value" (до ';' или '}') — а не только
// сразу после ':', ',' или '(' как раньше. Это ловит формы вида
// "border: 1px solid #ff0000", "box-shadow: 0 0 5px #00ff00" и т.п.
// Декларация ищется по шаблону "имя-свойства: значение", что естественно
// исключает CSS ID-селекторы (#sp-group { ... }) — перед их '#' нет
// пары "слово:", а также большинство псевдоклассов вида ".foo:hover {" —
// после "hover" сразу идёт '{', а не значение.
const DECLARATION_RE = /[a-zA-Z-]+\s*:\s*([^;{}]+)(?=[;}]|$)/g;

// Заменяет содержимое CSS-комментариев /* ... */ пробелами (сохраняя длину
// строки и переводы строк), чтобы DECLARATION_RE больше не видел "имя: hex"
// внутри закомментированного кода (например, "/* border: 1px solid #ccc;
// old */") как настоящую декларацию. Замена пробелами, а не удаление,
// намеренно сохраняет индексы символов — lineAt() ниже продолжает считать
// номера строк корректно для находок после комментария.
function stripCssComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, (match) => match.replace(/[^\n]/g, ' '));
}

// Возвращает находки { value, index } для всех hex-цветов, встретившихся
// внутри значений CSS-деклараций в `text` (текст одного блока <style> или
// содержимое одного inline-атрибута style="").
function findHexInDeclarations(text) {
  const results = [];
  const stripped = stripCssComments(text);
  let declMatch;
  DECLARATION_RE.lastIndex = 0;
  while ((declMatch = DECLARATION_RE.exec(stripped)) !== null) {
    const value = declMatch[1];
    const valueStart = declMatch.index + declMatch[0].length - value.length;
    let hexMatch;
    HEX_VALUE_RE.lastIndex = 0;
    while ((hexMatch = HEX_VALUE_RE.exec(value)) !== null) {
      results.push({ value: hexMatch[0], index: valueStart + hexMatch.index });
    }
  }
  return results;
}

const STYLE_BLOCK_RE = /<style\b[^>]*>([\s\S]*?)<\/style>/g;
const INLINE_STYLE_RE = /\bstyle\s*=\s*(?:"([^"]*)"|'([^']*)')/g;

// Whitelist — явные записи с обязательным обоснованием на каждую. Молчаливое
// исключение по маске каталога запрещено. Записи ограничены *.svelte-файлами,
// так как именно их сканирует walkSvelteFiles (global.css вне охвата — см.
// комментарий над walkSvelteFiles).
const WHITELIST = [
  {
    file: 'components/Button.svelte',
    value: '#fff',
    reason:
      'Контрастный текст на заливке .btn-danger (--danger): белый текст читаем на обоих значениях --danger (тёмная и светлая тема) без переключения — проектного токена "текст на danger-заливке" не существует, а --btn-primary-text семантически принадлежит primary/warning-варианту (120-03)'
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
    for (const hit of findHexInDeclarations(blockContent)) {
      if (isWhitelisted(relFile, hit.value)) continue;
      const absoluteIndex = blockStart + hit.index;
      findings.push({ file: relFile, line: lineAt(content, absoluteIndex), value: hit.value });
    }
  }

  // (б) значения inline-атрибутов style=""
  let inlineMatch;
  INLINE_STYLE_RE.lastIndex = 0;
  while ((inlineMatch = INLINE_STYLE_RE.exec(content)) !== null) {
    const attrValue = inlineMatch[1] ?? inlineMatch[2] ?? '';
    const attrStart = inlineMatch.index + inlineMatch[0].indexOf(attrValue);
    for (const hit of findHexInDeclarations(attrValue)) {
      if (isWhitelisted(relFile, hit.value)) continue;
      const absoluteIndex = attrStart + hit.index;
      findings.push({ file: relFile, line: lineAt(content, absoluteIndex), value: hit.value });
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

// Сканер намеренно охватывает только *.svelte (см. walkSvelteFiles ниже) и
// никогда не сканирует frontend/src/styles/global.css: там литеральные
// hex-значения — источник истины самих дизайн-токенов (:root /
// [data-theme='light']), а не хардкод их использования, и это разные вещи.
// Записей WHITELIST с file: 'styles/global.css' здесь нет специально — они
// физически не могут сработать при штатном запуске без --path и раньше
// вводили в заблуждение, будто global.css охвачен сканом.
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
