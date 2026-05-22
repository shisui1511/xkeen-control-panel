const fs = require('fs');
const path = require('path');

const i18nPath = path.join(__dirname, '../frontend/src/i18n.ts');
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

  // Извлекаем все ключи в кавычках (одинарных или двойных или обратных) перед двоеточием
  const keys = [];
  const keyRegex = /(?:'|")([^'"]+)(?:'|")\s*:/g;
  let keyMatch;
  while ((keyMatch = keyRegex.exec(blockText)) !== null) {
    keys.push(keyMatch[1]);
  }
  return keys;
}

try {
  const ruKeys = extractKeys('ru');
  const enKeys = extractKeys('en');

  const ruSet = new Set(ruKeys);
  const enSet = new Set(enKeys);

  let hasError = false;

  console.log(`Сканирование переводов...\nВсего ключей в RU: ${ruKeys.length}\nВсего ключей в EN: ${enKeys.length}`);

  // Проверка отсутствующих ключей в EN
  const missingInEn = ruKeys.filter(key => !enSet.has(key));
  if (missingInEn.length > 0) {
    console.error('\n❌ Ошибка: Следующие ключи есть в RU, но отсутствуют в EN:');
    missingInEn.forEach(key => console.error(`  - ${key}`));
    hasError = true;
  }

  // Проверка отсутствующих ключей в RU
  const missingInRu = enKeys.filter(key => !ruSet.has(key));
  if (missingInRu.length > 0) {
    console.error('\n❌ Ошибка: Следующие ключи есть в EN, но отсутствуют в RU:');
    missingInRu.forEach(key => console.error(`  - ${key}`));
    hasError = true;
  }

  if (hasError) {
    console.error('\n❌ Синхронизация i18n не пройдена!');
    process.exit(1);
  } else {
    console.log('\n✅ Переводы успешно синхронизированы!');
    process.exit(0);
  }
} catch (err) {
  console.error('❌ Ошибка при проверке переводов:', err.message);
  process.exit(1);
}
