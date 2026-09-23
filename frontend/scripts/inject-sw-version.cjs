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

// package.json is not bumped on release, so the cache name is derived from
// the nearest stable git tag plus the commit; otherwise it never changed
// between releases and caches of old hashed assets were never evicted.
function resolveVersion() {
  try {
    const { execSync } = require('child_process');
    const described = execSync(
      "git describe --tags --long --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*'",
      { cwd: __dirname, stdio: ['ignore', 'pipe', 'ignore'] }
    )
      .toString()
      .trim();
    const m = described.match(/^v(\d+\.\d+\.\d+)-(\d+)-g([0-9a-f]+)$/);
    if (m) {
      return m[2] === '0' ? m[1] : `${m[1]}+${m[2]}.g${m[3]}`;
    }
  } catch {
    // No git or no tags (e.g. source tarball): fall back below.
  }
  return require('../package.json').version;
}

function main() {
  try {
    const version = resolveVersion();
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

module.exports = { injectVersion, resolveVersion };
