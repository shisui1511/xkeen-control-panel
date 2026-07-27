/* eslint-disable */
const fs = require('fs');
const path = require('path');
const zlib = require('zlib');

const DIST_DIR = path.join(__dirname, '../dist');
const MANIFEST_PATH = path.join(__dirname, '../dist/.vite/manifest.json');
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

function main(argv) {
  try {
    const args = argv || [];
    const reportOnly = args.includes('--report');

    if (!fs.existsSync(MANIFEST_PATH)) {
      console.error('❌ Не найден dist/.vite/manifest.json — сначала выполните npm run build');
      process.exit(1);
      return;
    }

    const manifest = JSON.parse(fs.readFileSync(MANIFEST_PATH, 'utf8'));
    const entry = findEntryChunk(manifest);
    if (!entry) {
      console.error('❌ Не найден entry-чанк в dist/.vite/manifest.json');
      process.exit(1);
      return;
    }

    const jsPath = path.join(DIST_DIR, entry.file);
    const buffer = fs.readFileSync(jsPath);
    const gzipBytes = gzipSize(buffer);
    const kb = formatKb(gzipBytes);
    const budgetKb = formatKb(BUDGET_BYTES);

    console.log(`entry: ${entry.file}`);
    console.log(`gzip: ${kb} KB (бюджет ${budgetKb} KB)`);

    if (reportOnly) {
      process.exit(0);
      return;
    }

    if (gzipBytes > BUDGET_BYTES) {
      console.error(`❌ FAIL: ${entry.file} = ${kb} KB gzip > ${budgetKb} KB бюджет`);
      process.exit(1);
    } else {
      console.log(`✅ PASS: ${entry.file} = ${kb} KB gzip ≤ ${budgetKb} KB бюджет`);
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

module.exports = { BUDGET_BYTES, findEntryChunk, gzipSize, formatKb };
