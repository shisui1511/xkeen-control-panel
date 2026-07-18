/* eslint-disable */
const fs = require('fs');
const path = require('path');

const CSS_PATH = path.join(__dirname, '../src/styles/global.css');
const PAIRS_PATH = path.join(__dirname, 'contrast-pairs.json');
const TOAST_PATH = path.join(__dirname, '../src/components/Toast.svelte');
const LOGIN_PATH = path.join(__dirname, '../src/Login.svelte');

function extractBlock(css, selectorRegex) {
  const blocks = [];
  const re = new RegExp(selectorRegex.source, 'g');
  let match;
  while ((match = re.exec(css)) !== null) {
    const startIndex = match.index + match[0].length;
    let braceCount = 1;
    let endIndex = startIndex;
    while (braceCount > 0 && endIndex < css.length) {
      const char = css[endIndex];
      if (char === '{') braceCount++;
      else if (char === '}') braceCount--;
      endIndex++;
    }
    if (braceCount === 0) {
      blocks.push(css.slice(startIndex, endIndex - 1));
    }
  }
  return blocks.join('\n');
}

function parseTokens(blockText) {
  const tokens = {};
  const re = /--([\w-]+)\s*:\s*([^;\r\n]+);/g;
  let match;
  while ((match = re.exec(blockText)) !== null) {
    tokens[`--${match[1]}`] = match[2].trim();
  }
  return tokens;
}

function resolve(value, tokens, depth = 0) {
  if (typeof value !== 'string') return value;
  const m = /^var\((--[\w-]+)\)$/.exec(value.trim());
  if (!m || depth > 5) return value;
  return resolve(tokens[m[1]] ?? value, tokens, depth + 1);
}

function hexToRgb(hex) {
  if (!hex) return null;
  let clean = hex.trim().replace(/^#/, '');
  if (clean.length === 3) {
    clean = clean.split('').map(c => c + c).join('');
  }
  if (clean.length === 6) {
    const r = parseInt(clean.slice(0, 2), 16);
    const g = parseInt(clean.slice(2, 4), 16);
    const b = parseInt(clean.slice(4, 6), 16);
    return [r, g, b];
  }
  // Поддержка rgb/rgba в resolved значении
  const rgbMatch = /^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)(?:\s*,\s*([\d.]+))?\s*\)$/i.exec(clean);
  if (rgbMatch) {
    return [parseInt(rgbMatch[1], 10), parseInt(rgbMatch[2], 10), parseInt(rgbMatch[3], 10)];
  }
  return null;
}

function compositeOver(fgRgb, alpha, bgRgb) {
  return fgRgb.map((c, i) => Math.round(alpha * c + (1 - alpha) * bgRgb[i]));
}

function srgbChannel(c8bit) {
  const c = c8bit / 255;
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

function relativeLuminance([r, g, b]) {
  return 0.2126 * srgbChannel(r) + 0.7152 * srgbChannel(g) + 0.0722 * srgbChannel(b);
}

function contrastRatio(rgbA, rgbB) {
  if (!rgbA || !rgbB) return 1;
  const lA = relativeLuminance(rgbA);
  const lB = relativeLuminance(rgbB);
  const [lighter, darker] = lA > lB ? [lA, lB] : [lB, lA];
  return (lighter + 0.05) / (darker + 0.05);
}

function checkRawColors(blockText, contextName) {
  let hasRaw = false;
  const propRegex = /^\s*(color|background|background-color|border-color|scrollbar-color)\s*:\s*([^;]+);/gmi;
  let match;
  while ((match = propRegex.exec(blockText)) !== null) {
    const prop = match[1].toLowerCase();
    const val = match[2].trim();
    if (/(#[0-9a-fA-F]{3,8}\b|rgba?\(\s*\d)/.test(val)) {
      console.error(`❌ Ошибка [D-06]: Селектор ${contextName} содержит сырой цвет в свойстве ${prop}: ${val}`);
      hasRaw = true;
    }
  }
  return hasRaw;
}

function main() {
  try {
    let failed = false;
    const css = fs.readFileSync(CSS_PATH, 'utf8');
    const rootText = extractBlock(css, /:root\s*\{/);
    const lightText = extractBlock(css, /\[data-theme=['"]light['"]\]\s*\{/);
    const darkTokens = parseTokens(rootText);
    const lightTokens = { ...darkTokens, ...parseTokens(lightText) };

    const pairs = JSON.parse(fs.readFileSync(PAIRS_PATH, 'utf8'));

    console.log('🔄 Запуск проверки контраста WCAG AA...');

    for (const pair of pairs) {
      const tokens = pair.theme === 'light' ? lightTokens : darkTokens;
      const fgVal = tokens[pair.fg];
      if (!fgVal) {
        console.error(`❌ Ошибка: Токен ${pair.fg} не найден для темы ${pair.theme}`);
        failed = true;
        continue;
      }
      const fgHex = resolve(fgVal, tokens);
      const fgRgb = hexToRgb(fgHex);

      let bgRgb;
      let bgDesc = '';
      if (typeof pair.bg === 'string') {
        const bgVal = tokens[pair.bg];
        if (!bgVal) {
          console.error(`❌ Ошибка: Токен ${pair.bg} не найден для темы ${pair.theme}`);
          failed = true;
          continue;
        }
        const bgHex = resolve(bgVal, tokens);
        bgRgb = hexToRgb(bgHex);
        bgDesc = pair.bg;
      } else if (pair.bg && typeof pair.bg === 'object') {
        const overVal = tokens[pair.bg.over];
        const mixVal = tokens[pair.bg.mixToken];
        if (!overVal || !mixVal) {
          console.error(`❌ Ошибка: Токены ${pair.bg.over} или ${pair.bg.mixToken} не найдены для темы ${pair.theme}`);
          failed = true;
          continue;
        }
        const overHex = resolve(overVal, tokens);
        const mixHex = resolve(mixVal, tokens);
        const overRgb = hexToRgb(overHex);
        const mixRgb = hexToRgb(mixHex);
        const alpha = (pair.bg.mixPercent ?? 0) / 100;
        bgRgb = compositeOver(mixRgb, alpha, overRgb);
        bgDesc = `${pair.bg.mixPercent}% var(${pair.bg.mixToken}) over var(${pair.bg.over})`;
      }

      if (!fgRgb || !bgRgb) {
        console.error(`❌ Ошибка: Не удалось распарсить цвета для пары ${pair.id} (${pair.fg} vs ${bgDesc})`);
        failed = true;
        continue;
      }

      const ratio = contrastRatio(fgRgb, bgRgb);
      if (ratio < pair.minRatio) {
        failed = true;
        console.error(`❌ FAIL [${pair.theme}] ${pair.id} (${pair.fg} vs ${bgDesc}): ${ratio.toFixed(2)}:1 < ${pair.minRatio}:1`);
      } else {
        console.log(`✅ PASS [${pair.theme}] ${pair.id}: ${ratio.toFixed(2)}:1`);
      }
    }

    // Проверка D-06
    console.log('🔄 Запуск проверки отсутствия сырых hex/rgba в D-05 селекторах...');
    const scopedSelectors = [
      '.alert-success', '.alert-error', '.alert-warning', '.alert-close-btn',
      '.chip-success', '.chip-warning', '.chip-danger', '.chip-info', '.chip-default',
      '.badge', '.badge-success', '.badge-warning', '.badge-error', '.badge-danger',
      '.badge-info', '.badge-primary', '.badge-type',
      '.status-badge', '.status-dot',
      '.status-badge-value', '.version-badge', '.api-offline'
    ];

    for (const sel of scopedSelectors) {
      // Ищем селектор, избегая совпадений с суффиксами
      const escapedSelector = sel.replace(/[-\/\\^$*+?.()|[\]{}]/g, '\\$&');
      const regex = new RegExp(escapedSelector + '(?![a-zA-Z0-9_-])[^{]*\\{');
      const block = extractBlock(css, regex);
      if (block && checkRawColors(block, sel)) {
        failed = true;
      }
    }

    // Проверка Toast.svelte и Login.svelte
    if (fs.existsSync(TOAST_PATH)) {
      const toastContent = fs.readFileSync(TOAST_PATH, 'utf8');
      const toastStyle = extractBlock(toastContent, /<style\b[^>]*>/);
      const toastBlock = extractBlock(toastStyle, /\.toast(?![a-zA-Z0-9_-])[^{]*\{/);
      if (toastBlock && checkRawColors(toastBlock, 'Toast.svelte (.toast)')) {
        failed = true;
      }
    }

    if (fs.existsSync(LOGIN_PATH)) {
      const loginContent = fs.readFileSync(LOGIN_PATH, 'utf8');
      const loginStyle = extractBlock(loginContent, /<style\b[^>]*>/);
      const loginBlock = extractBlock(loginStyle, /\.login-card(?![a-zA-Z0-9_-])[^{]*\{/);
      if (loginBlock && checkRawColors(loginBlock, 'Login.svelte (.login-card)')) {
        failed = true;
      }
    }

    if (failed) {
      console.error('\n❌ Проверка контраста не пройдена!');
      process.exit(1);
    } else {
      console.log('\n✅ Все проверки контраста успешно пройдены!');
      process.exit(0);
    }
  } catch (err) {
    console.error('❌ Системная ошибка при проверке контраста:', err.stack || err.message);
    process.exit(1);
  }
}

if (require.main === module) {
  main();
}

module.exports = {
  hexToRgb,
  compositeOver,
  srgbChannel,
  relativeLuminance,
  contrastRatio,
  resolve,
  parseTokens,
  extractBlock,
  checkRawColors
};
