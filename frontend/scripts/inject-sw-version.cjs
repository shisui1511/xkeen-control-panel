/* eslint-disable */
const fs = require('fs');
const path = require('path');

const SW_PATH = path.join(__dirname, '../dist/sw.js');
const CACHE_NAME_RE = /const CACHE_NAME = '([^']*)';/;

function injectVersion(source, version) {
  if (!CACHE_NAME_RE.test(source)) {
    throw new Error("Не найдено объявление const CACHE_NAME = '...'; в исходном тексте sw.js");
  }
  return source.replace(CACHE_NAME_RE, `const CACHE_NAME = 'xcp-v${version}';`);
}

function main() {
  try {
    const version = require('../package.json').version;
    let source;
    try {
      source = fs.readFileSync(SW_PATH, 'utf8');
    } catch (err) {
      if (err && err.code === 'ENOENT') {
        console.error(`❌ Не найден ${SW_PATH} — сначала выполните npm run build`);
        process.exit(1);
        return;
      }
      throw err;
    }

    const result = injectVersion(source, version);
    fs.writeFileSync(SW_PATH, result);

    console.log(`✅ CACHE_NAME в dist/sw.js обновлён: xcp-v${version}`);
  } catch (err) {
    console.error('❌ Ошибка при инъекции версии в dist/sw.js:', err.stack || err.message);
    process.exit(1);
  }
}

if (require.main === module) {
  main();
}

module.exports = { injectVersion };
