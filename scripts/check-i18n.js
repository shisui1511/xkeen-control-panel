const fs = require('fs');
const path = require('path');

// Пути к файлам переводов
const i18nPath = path.join(__dirname, '../frontend/src/i18n.ts');
const ruJsonPath = path.join(__dirname, '../frontend/src/locales/ru.json');
const enJsonPath = path.join(__dirname, '../frontend/src/locales/en.json');
const srcDir = path.join(__dirname, '../frontend/src');

const content = fs.readFileSync(i18nPath, 'utf8');

function extractKeys(lang) {
  // Находим начало словаря ru или en
  const startRegex = new RegExp(`${lang}:\\s*\\{`);
  const match = content.match(startRegex);
  if (!match) {
    throw new Error(`Не найден блок для языка: ${lang}`);
  }

  const startIndex = match.index + match[0].length;
  let braceCount = 1;
  let endIndex = startIndex;

  // Ищем парную закрывающую скобку для блока словаря
  while (braceCount > 0 && endIndex < content.length) {
    const char = content[endIndex];
    if (char === '{') braceCount++;
    else if (char === '}') braceCount--;
    endIndex++;
  }

  if (braceCount > 0) {
    throw new Error(`Не найдена закрывающая скобка для блока языка: ${lang}`);
  }

  const blockText = content.slice(startIndex, endIndex - 1);

  // Извлекаем все ключи в кавычках перед двоеточием
  const keys = [];
  const keyRegex = /(?:'|")([^'"]+)(?:'|")\s*:/g;
  let keyMatch;
  while ((keyMatch = keyRegex.exec(blockText)) !== null) {
    keys.push(keyMatch[1]);
  }
  return keys;
}

try {
  let hasError = false;

  // 1. Извлекаем базовые ключи из i18n.ts
  const ruBaseKeys = extractKeys('ru');
  const enBaseKeys = extractKeys('en');

  const ruBaseSet = new Set(ruBaseKeys);
  const enBaseSet = new Set(enBaseKeys);

  console.log(`Базовые ключи (i18n.ts): RU = ${ruBaseKeys.length}, EN = ${enBaseKeys.length}`);

  // Проверка симметричности базовых переводов
  const missingBaseInEn = ruBaseKeys.filter(key => !enBaseSet.has(key));
  if (missingBaseInEn.length > 0) {
    console.error('❌ Ошибка: Следующие базовые ключи в i18n.ts есть в RU, но отсутствуют в EN:');
    missingBaseInEn.forEach(key => console.error(`  - ${key}`));
    hasError = true;
  }

  const missingBaseInRu = enBaseKeys.filter(key => !ruBaseSet.has(key));
  if (missingBaseInRu.length > 0) {
    console.error('❌ Ошибка: Следующие базовые ключи в i18n.ts есть в EN, но отсутствуют в RU:');
    missingBaseInRu.forEach(key => console.error(`  - ${key}`));
    hasError = true;
  }

  // 2. Читаем переводы из JSON-файлов
  const ruJson = JSON.parse(fs.readFileSync(ruJsonPath, 'utf8'));
  const enJson = JSON.parse(fs.readFileSync(enJsonPath, 'utf8'));

  const ruJsonKeys = Object.keys(ruJson);
  const enJsonKeys = Object.keys(enJson);

  const ruJsonSet = new Set(ruJsonKeys);
  const enJsonSet = new Set(enJsonKeys);

  console.log(`Основные ключи (locales/*.json): RU = ${ruJsonKeys.length}, EN = ${enJsonKeys.length}`);

  // Проверка симметричности JSON переводов
  const missingJsonInEn = ruJsonKeys.filter(key => !enJsonSet.has(key));
  if (missingJsonInEn.length > 0) {
    console.error('❌ Ошибка: Следующие ключи в ru.json отсутствуют в en.json:');
    missingJsonInEn.forEach(key => console.error(`  - ${key}`));
    hasError = true;
  }

  const missingJsonInRu = enJsonKeys.filter(key => !ruJsonSet.has(key));
  if (missingJsonInRu.length > 0) {
    console.error('❌ Ошибка: Следующие ключи в en.json отсутствуют в ru.json:');
    missingJsonInRu.forEach(key => console.error(`  - ${key}`));
    hasError = true;
  }

  // 3. Создаем объединенный словарь (i18n.ts + *.json) для каждого языка
  const ruTotalSet = new Set([...ruBaseKeys, ...ruJsonKeys]);
  const enTotalSet = new Set([...enBaseKeys, ...enJsonKeys]);

  // 4. Сканируем все .svelte файлы в frontend/src/
  function walk(dir, acc = []) {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p, acc);
      else if (e.name.endsWith('.svelte')) acc.push(p);
    }
    return acc;
  }

  const used = new Set();
  const keyRe = /(?<![\w$])\$?t\(\s*['"]([a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9_]+)+)['"]/g;

  const svelteFiles = walk(srcDir);
  console.log(`Найдено .svelte файлов для сканирования: ${svelteFiles.length}`);

  for (const f of svelteFiles) {
    const txt = fs.readFileSync(f, 'utf8');
    let m;
    while ((m = keyRe.exec(txt)) !== null) {
      used.add(m[1]);
    }
  }

  console.log(`Всего уникальных i18n-ключей найдено в .svelte файлах: ${used.size}`);

  // Проверяем наличие используемых ключей в объединенных словарях
  const missingInRuTotal = [];
  const missingInEnTotal = [];

  for (const k of used) {
    if (!ruTotalSet.has(k)) {
      missingInRuTotal.push(k);
    }
    if (!enTotalSet.has(k)) {
      missingInEnTotal.push(k);
    }
  }

  if (missingInRuTotal.length > 0) {
    console.error(`\n❌ Ошибка: Следующие используемые в .svelte файлах ключи отсутствуют в словаре RU (i18n.ts или ru.json):`);
    missingInRuTotal.sort().forEach(key => console.error(`  - ${key}`));
    hasError = true;
  }

  if (missingInEnTotal.length > 0) {
    console.error(`\n❌ Ошибка: Следующие используемые в .svelte файлах ключи отсутствуют в словаре EN (i18n.ts или en.json):`);
    missingInEnTotal.sort().forEach(key => console.error(`  - ${key}`));
    hasError = true;
  }

  if (hasError) {
    console.error('\n❌ Синхронизация и проверка i18n не пройдены!');
    process.exit(1);
  } else {
    console.log('\n✅ Все переводы успешно проверены и синхронизированы!');
    process.exit(0);
  }
} catch (err) {
  console.error('❌ Системная ошибка при проверке переводов:', err.stack || err.message);
  process.exit(1);
}
