/* eslint-disable */
const fs = require('fs');
const path = require('path');
const zlib = require('zlib');

const DIST_DIR = path.join(__dirname, '../dist');
const MANIFEST_PATH = path.join(__dirname, '../dist/.vite/manifest.json');
const SRC_INDEX_HTML_PATH = path.join(__dirname, '../index.html');
const DIST_INDEX_HTML_PATH = path.join(__dirname, '../dist/index.html');
const FONTS_CSS_PATH = path.join(__dirname, '../src/styles/fonts.css');
const PUBLIC_SW_PATH = path.join(__dirname, '../public/sw.js');
const DIST_SW_PATH = path.join(__dirname, '../dist/sw.js');
const BUDGET_BYTES = 200 * 1024; // 200 KB gzip

function findEntryChunk(manifest) {
  const entry = Object.values(manifest).find((e) => e && e.isEntry === true);
  return entry || null;
}

function gzipSize(buffer) {
  return zlib.gzipSync(buffer, { level: 9 }).length;
}

function formatKb(bytes) {
  return (bytes / 1024).toFixed(2);
}

// Проверка D-06: в index.html не должно быть внешних абсолютных ссылок
// (http://, https:// или схемо-относительных //) в href/src тегов <link>/<script>.
// data: URI и относительные пути нарушением не считаются.
function checkNoExternalResources(html) {
  const violations = [];
  const tagRegex = /<(link|script)\b[^>]*>/gi;
  let tagMatch;
  while ((tagMatch = tagRegex.exec(html)) !== null) {
    const tag = tagMatch[0];
    const attrRegex = /\b(href|src)\s*=\s*(?:"([^"]*)"|'([^']*)')/gi;
    let attrMatch;
    while ((attrMatch = attrRegex.exec(tag)) !== null) {
      const value = attrMatch[2] ?? attrMatch[3];
      if (/^(https?:)?\/\//i.test(value)) {
        violations.push(value);
      }
    }
  }
  return violations;
}

// Проверка D-05: в @font-face блоках src должен ссылаться только на .woff2.
function checkWoff2Only(css) {
  const violations = [];
  const fontFaceRegex = /@font-face\s*\{([^}]*)\}/gi;
  let faceMatch;
  while ((faceMatch = fontFaceRegex.exec(css)) !== null) {
    const block = faceMatch[1];
    const srcRegex = /src\s*:\s*([^;]+);/gi;
    let srcMatch;
    while ((srcMatch = srcRegex.exec(block)) !== null) {
      const urlRegex = /url\(\s*['"]?([^'")]+)['"]?\s*\)/gi;
      let urlMatch;
      while ((urlMatch = urlRegex.exec(srcMatch[1])) !== null) {
        const value = urlMatch[1];
        if (!value.toLowerCase().endsWith('.woff2')) {
          violations.push(value);
        }
      }
    }
  }
  return violations;
}

// Проверка REQ-13: CACHE_NAME в собранном dist/sw.js версионирован —
// плейсхолдер подставлен, значение совпадает с версией из package.json.
function checkSwCacheVersioned(distSwSource, version) {
  const violations = [];
  if (distSwSource.includes('__BUILD_VERSION__')) {
    violations.push('В dist/sw.js остался неподставленный build-плейсхолдер __BUILD_VERSION__');
  }
  const match = /const CACHE_NAME = '([^']*)';/.exec(distSwSource);
  const expected = `xcp-v${version}`;
  const actual = match ? match[1] : null;
  if (actual !== expected) {
    violations.push(
      `CACHE_NAME в dist/sw.js равен '${actual}', ожидалось '${expected}' (версия package.json не подставлена)`
    );
  }
  return violations;
}

// Проверка REQ-13/F-21: ветка /api/ обработчика fetch в public/sw.js не
// обращается к Cache Storage API. Регион-ограниченная проверка: вырезает
// только тело if-блока /api/ (та же техника парности скобок, что и
// extractBlock в check-contrast.cjs), т.к. идентификатор caches легитимно
// присутствует в install/activate/app-shell/cache-first блоках того же файла.
function checkApiBranchUncached(swSource) {
  const violations = [];
  const conditionRegex = /if\s*\(\s*url\.pathname\.startsWith\(\s*['"]\/api\/['"]\s*\)\s*\)\s*\{/;
  const match = conditionRegex.exec(swSource);
  if (!match) {
    violations.push('Не найдена ветка обработки /api/ в обработчике fetch');
    return violations;
  }
  const startIndex = match.index + match[0].length;
  let braceCount = 1;
  let endIndex = startIndex;
  while (braceCount > 0 && endIndex < swSource.length) {
    const char = swSource[endIndex];
    if (char === '{') braceCount++;
    else if (char === '}') braceCount--;
    endIndex++;
  }
  const region = swSource.slice(startIndex, endIndex - 1);
  if (/\bcaches\b/.test(region)) {
    violations.push(
      'Ветка /api/ обращается к Cache Storage (caches.*) — кэширование должно быть полностью отключено'
    );
  }
  return violations;
}

function main(argv) {
  try {
    const args = argv || [];
    const reportOnly = args.includes('--report');
    let failed = false;

    // Группа 1: бюджет размера главного JS-чанка первого экрана
    console.log('🔄 Проверка бюджета размера бандла...');
    if (!fs.existsSync(MANIFEST_PATH)) {
      console.error('❌ Не найден dist/.vite/manifest.json — сначала выполните npm run build');
      failed = true;
    } else {
      const manifest = JSON.parse(fs.readFileSync(MANIFEST_PATH, 'utf8'));
      const entry = findEntryChunk(manifest);
      if (!entry) {
        console.error('❌ Не найден entry-чанк в dist/.vite/manifest.json');
        failed = true;
      } else {
        const jsPath = path.join(DIST_DIR, entry.file);
        const buffer = fs.readFileSync(jsPath);
        const gzipBytes = gzipSize(buffer);
        const kb = formatKb(gzipBytes);
        const budgetKb = formatKb(BUDGET_BYTES);

        console.log(`entry: ${entry.file}`);
        console.log(`gzip: ${kb} KB (бюджет ${budgetKb} KB)`);

        if (gzipBytes > BUDGET_BYTES) {
          console.error(`❌ FAIL: ${entry.file} = ${kb} KB gzip > ${budgetKb} KB бюджет`);
          failed = true;
        } else {
          console.log(`✅ PASS: ${entry.file} = ${kb} KB gzip ≤ ${budgetKb} KB бюджет`);
        }
      }
    }

    // Группа 2 (D-06): index.html и собранный dist/index.html не должны содержать
    // ссылок на внешние хосты.
    console.log('🔄 Проверка отсутствия внешних CDN-ресурсов (D-06)...');
    let externalOk = true;
    if (fs.existsSync(SRC_INDEX_HTML_PATH)) {
      const html = fs.readFileSync(SRC_INDEX_HTML_PATH, 'utf8');
      for (const violation of checkNoExternalResources(html)) {
        console.error(`❌ Внешний ресурс в index.html: ${violation}`);
        externalOk = false;
      }
    }
    if (fs.existsSync(DIST_INDEX_HTML_PATH)) {
      const html = fs.readFileSync(DIST_INDEX_HTML_PATH, 'utf8');
      for (const violation of checkNoExternalResources(html)) {
        console.error(`❌ Внешний ресурс в dist/index.html: ${violation}`);
        externalOk = false;
      }
    }
    if (externalOk) {
      console.log('✅ PASS: внешних CDN-ресурсов в index.html не найдено');
    } else {
      failed = true;
    }

    // Группа 3 (D-05): все @font-face в fonts.css ссылаются только на .woff2.
    console.log('🔄 Проверка woff2-only политики шрифтов (D-05)...');
    let fontsOk = true;
    if (fs.existsSync(FONTS_CSS_PATH)) {
      const css = fs.readFileSync(FONTS_CSS_PATH, 'utf8');
      for (const violation of checkWoff2Only(css)) {
        console.error(`❌ Шрифт не в формате woff2 в fonts.css: ${violation}`);
        fontsOk = false;
      }
    }
    if (fontsOk) {
      console.log('✅ PASS: все @font-face ссылаются только на .woff2');
    } else {
      failed = true;
    }

    // Группа 4 (REQ-13): ветка /api/ в public/sw.js не обращается к Cache Storage.
    console.log('🔄 Проверка отсутствия кэширования /api/** в Service Worker...');
    let apiBranchOk = true;
    if (fs.existsSync(PUBLIC_SW_PATH)) {
      const swSource = fs.readFileSync(PUBLIC_SW_PATH, 'utf8');
      for (const violation of checkApiBranchUncached(swSource)) {
        console.error(`❌ ${violation}`);
        apiBranchOk = false;
      }
    } else {
      console.error(`❌ Не найден ${PUBLIC_SW_PATH}`);
      apiBranchOk = false;
    }
    if (apiBranchOk) {
      console.log('✅ PASS: ветка /api/ в public/sw.js не обращается к Cache Storage');
    } else {
      failed = true;
    }

    // Группа 5 (REQ-13): CACHE_NAME в dist/sw.js версионирован по package.json.
    console.log('🔄 Проверка версии CACHE_NAME в dist/sw.js...');
    let swVersionOk = true;
    if (fs.existsSync(DIST_SW_PATH)) {
      const distSwSource = fs.readFileSync(DIST_SW_PATH, 'utf8');
      const version = require('../package.json').version;
      for (const violation of checkSwCacheVersioned(distSwSource, version)) {
        console.error(`❌ ${violation}`);
        swVersionOk = false;
      }
    } else {
      console.error(`❌ Не найден dist/sw.js — сначала выполните npm run build`);
      swVersionOk = false;
    }
    if (swVersionOk) {
      console.log('✅ PASS: версия CACHE_NAME в dist/sw.js подставлена корректно');
    } else {
      failed = true;
    }

    if (reportOnly) {
      process.exit(0);
      return;
    }

    if (failed) {
      console.error('\n❌ Проверка бюджета бандла и политики ассетов не пройдена!');
      process.exit(1);
    } else {
      console.log('\n✅ Все проверки бюджета бандла и политики ассетов успешно пройдены!');
      process.exit(0);
    }
  } catch (err) {
    console.error('❌ Системная ошибка при проверке размера бандла:', err.stack || err.message);
    process.exit(1);
  }
}

if (require.main === module) {
  main(process.argv.slice(2));
}

module.exports = {
  BUDGET_BYTES,
  findEntryChunk,
  gzipSize,
  formatKb,
  checkNoExternalResources,
  checkWoff2Only,
  checkSwCacheVersioned,
  checkApiBranchUncached
};
