/* eslint-disable */
// Детектор размеров шрифта в обход шкалы: font-size с литералом в px/rem
// в frontend/src/**/*.{svelte,css}. Размер задаётся токеном
// var(--font-size-2xs … 3xl) из styles/global.css; относительные единицы
// (em, %) допустимы — они масштабируют текст от родителя, а не задают его.
const fs = require('fs');
const path = require('path');

const SRC_ROOT = path.join(__dirname, '../src');
const LITERAL_RE = /font-size\s*:\s*([0-9.]+(?:px|rem))/g;

function walk(dir, out) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, out);
    else if (/\.(svelte|css)$/.test(entry.name)) out.push(full);
  }
  return out;
}

const findings = [];
for (const file of walk(SRC_ROOT, [])) {
  const text = fs
    .readFileSync(file, 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));
  let match;
  LITERAL_RE.lastIndex = 0;
  while ((match = LITERAL_RE.exec(text)) !== null) {
    const line = text.slice(0, match.index).split('\n').length;
    findings.push(`${path.relative(SRC_ROOT, file)}:${line}  font-size: ${match[1]}`);
  }
}

if (findings.length) {
  console.error('Размер шрифта задан литералом — используйте var(--font-size-*):');
  for (const f of findings) console.error('  ' + f);
  process.exit(1);
}
console.log('check-font-sizes: OK');
