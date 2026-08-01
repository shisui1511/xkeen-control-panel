/* eslint-disable @typescript-eslint/no-require-imports */
const fs = require('fs');
const path = require('path');

// Determine frontend root and src directory
const frontendDir = path.resolve(__dirname, '..');
const i18nPath = path.join(frontendDir, 'src/i18n.ts');
const ruJsonPath = path.join(frontendDir, 'src/locales/ru.json');
const enJsonPath = path.join(frontendDir, 'src/locales/en.json');
const srcDir = path.join(frontendDir, 'src');

const content = fs.readFileSync(i18nPath, 'utf8');

function extractKeys(lang) {
  const startRegex = new RegExp(`${lang}:\\s*\\{`);
  const match = content.match(startRegex);
  if (!match) {
    throw new Error(`Block not found for language: ${lang}`);
  }

  const startIndex = match.index + match[0].length;
  let braceCount = 1;
  let endIndex = startIndex;

  while (braceCount > 0 && endIndex < content.length) {
    const char = content[endIndex];
    if (char === '{') braceCount++;
    else if (char === '}') braceCount--;
    endIndex++;
  }

  if (braceCount > 0) {
    throw new Error(`Closing brace not found for language block: ${lang}`);
  }

  const blockText = content.slice(startIndex, endIndex - 1);
  const keys = [];
  const keyRegex = /(?:'|")([^'"]+)(?:'|")\s*:/g;
  let keyMatch;
  while ((keyMatch = keyRegex.exec(blockText)) !== null) {
    keys.push(keyMatch[1]);
  }
  return keys;
}

function stripHtmlComments(input) {
  let previous;
  do {
    previous = input;
    input = input.replace(/<!--[\s\S]*?-->/g, '');
  } while (input !== previous);
  return input;
}

function stripBlockComments(input) {
  let previous;
  do {
    previous = input;
    input = input.replace(/\/\*[\s\S]*?\*\//g, '');
  } while (input !== previous);
  return input;
}

try {
  let hasError = false;

  // 1. Base keys from i18n.ts
  const ruBaseKeys = extractKeys('ru');
  const enBaseKeys = extractKeys('en');
  const ruBaseSet = new Set(ruBaseKeys);
  const enBaseSet = new Set(enBaseKeys);

  const missingBaseInEn = ruBaseKeys.filter((key) => !enBaseSet.has(key));
  if (missingBaseInEn.length > 0) {
    console.error('❌ Error: Base keys in i18n.ts present in RU but missing in EN:');
    missingBaseInEn.forEach((key) => console.error(`  - ${key}`));
    hasError = true;
  }

  const missingBaseInRu = enBaseKeys.filter((key) => !ruBaseSet.has(key));
  if (missingBaseInRu.length > 0) {
    console.error('❌ Error: Base keys in i18n.ts present in EN but missing in RU:');
    missingBaseInRu.forEach((key) => console.error(`  - ${key}`));
    hasError = true;
  }

  // 2. Read JSON locale files
  const ruJson = JSON.parse(fs.readFileSync(ruJsonPath, 'utf8'));
  const enJson = JSON.parse(fs.readFileSync(enJsonPath, 'utf8'));

  const ruJsonKeys = Object.keys(ruJson);
  const enJsonKeys = Object.keys(enJson);
  const ruJsonSet = new Set(ruJsonKeys);
  const enJsonSet = new Set(enJsonKeys);

  console.log(`Checking i18n keys: RU = ${ruJsonKeys.length}, EN = ${enJsonKeys.length}`);

  const missingJsonInEn = ruJsonKeys.filter((key) => !enJsonSet.has(key));
  if (missingJsonInEn.length > 0) {
    console.error('❌ Error: Keys in ru.json missing in en.json:');
    missingJsonInEn.forEach((key) => console.error(`  - ${key}`));
    hasError = true;
  }

  const missingJsonInRu = enJsonKeys.filter((key) => !ruJsonSet.has(key));
  if (missingJsonInRu.length > 0) {
    console.error('❌ Error: Keys in en.json missing in ru.json:');
    missingJsonInRu.forEach((key) => console.error(`  - ${key}`));
    hasError = true;
  }

  const ruTotalSet = new Set([...ruBaseKeys, ...ruJsonKeys]);
  const enTotalSet = new Set([...enBaseKeys, ...enJsonKeys]);

  // 3. Scan .svelte files for keys, Cyrillic literals, and fallbacks
  function walk(dir, acc = []) {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p, acc);
      else if (e.name.endsWith('.svelte')) acc.push(p);
    }
    return acc;
  }

  const svelteFiles = walk(srcDir);
  const usedKeys = new Set();
  const keyRe = /(?<![\w$])\$?t\(\s*['"]([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)+)['"]/g;
  const fallbackRe = /\$?t\([^)]+\)\s*\|\|\s*['"][^'"]*[\u0400-\u04FF]/;
  const cyrillicCharRe = /[\u0400-\u04FF]/;

  const cyrillicErrors = [];
  const fallbackErrors = [];

  for (const f of svelteFiles) {
    const txt = fs.readFileSync(f, 'utf8');
    const relativePath = path.relative(frontendDir, f);

    // Extract used keys
    let m;
    while ((m = keyRe.exec(txt)) !== null) {
      usedKeys.add(m[1]);
    }

    // Clean multiline HTML and JS comments before splitting into lines
    const cleanedTxt = stripBlockComments(stripHtmlComments(txt));

    const lines = cleanedTxt.split('\n');
    const rawLines = txt.split('\n');

    lines.forEach((line, index) => {
      const lineNum = index + 1;
      const rawLine = rawLines[index] || line;

      // Check fallback pattern $t(...) || 'кириллица'
      if (fallbackRe.test(line)) {
        fallbackErrors.push({ file: relativePath, line: lineNum, text: rawLine.trim() });
      }

      // Clean single-line comments
      let cleaned = line.replace(/\/\/.*/, '');

      if (cyrillicCharRe.test(cleaned)) {
        cyrillicErrors.push({ file: relativePath, line: lineNum, text: rawLine.trim() });
      }
    });
  }

  // Verify key presence
  const missingInRu = [];
  const missingInEn = [];
  for (const k of usedKeys) {
    if (!ruTotalSet.has(k)) missingInRu.push(k);
    if (!enTotalSet.has(k)) missingInEn.push(k);
  }

  if (missingInRu.length > 0) {
    console.error('\n❌ Error: Keys used in .svelte components missing in RU dictionary:');
    missingInRu.sort().forEach((k) => console.error(`  - ${k}`));
    hasError = true;
  }

  if (missingInEn.length > 0) {
    console.error('\n❌ Error: Keys used in .svelte components missing in EN dictionary:');
    missingInEn.sort().forEach((k) => console.error(`  - ${k}`));
    hasError = true;
  }

  if (fallbackErrors.length > 0) {
    console.error('\n❌ Error: $t(...) || "кириллица" fallbacks detected in .svelte components:');
    fallbackErrors.forEach((e) => console.error(`  ${e.file}:${e.line} -> ${e.text}`));
    hasError = true;
  }

  if (cyrillicErrors.length > 0) {
    console.error(
      `\n❌ Error: ${cyrillicErrors.length} hardcoded Cyrillic string(s) detected in .svelte components:`
    );
    cyrillicErrors.slice(0, 30).forEach((e) => console.error(`  ${e.file}:${e.line} -> ${e.text}`));
    if (cyrillicErrors.length > 30) {
      console.error(`  ... and ${cyrillicErrors.length - 30} more`);
    }
    hasError = true;
  }

  if (hasError) {
    console.error('\n❌ i18n verification failed!');
    process.exit(1);
  } else {
    console.log('✅ All i18n checks passed cleanly!');
    process.exit(0);
  }
} catch (err) {
  console.error('❌ System error during i18n verification:', err.stack || err.message);
  process.exit(1);
}
