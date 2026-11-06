/* eslint-disable */
// Храповик встроенных стилей: оформление живёт в классах (стиль компонента или global.css),
// а не в атрибуте style="…" разметки. Статичный style="…" (без {выражений}) прячет значения
// от токенов и стражей дизайн-системы и не переопределяется темой.
// Разрешены: style с выражением ({…}) и директивы style:prop — это данные (ширина полосы,
// позиция), а не оформление.
// Старый код ещё содержит такие атрибуты: их число по файлам записано в
// scripts/inline-styles-baseline.json. Число в файле может только уменьшаться; новый файл —
// ноль. После чистки: node scripts/check-inline-styles.cjs --update (снижает базу).
const fs = require('fs');
const path = require('path');

const SRC = path.join(__dirname, '../src');
const BASELINE = path.join(__dirname, 'inline-styles-baseline.json');
const STATIC_STYLE = /\sstyle="([^"]*)"/g;

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const f = path.join(dir, e.name);
    if (e.isDirectory()) walk(f, out);
    else if (e.name.endsWith('.svelte')) out.push(f);
  }
  return out;
}

function markup(text) {
  // Скрипт и стиль компонента не проверяются: style="…" в строках шаблонов JS — не атрибут
  return text
    .replace(/<script[\s\S]*?<\/script>/g, (m) => '\n'.repeat(m.split('\n').length - 1))
    .replace(/<style[\s\S]*?<\/style>/g, (m) => '\n'.repeat(m.split('\n').length - 1));
}

const counts = {};
const where = {};
for (const file of walk(SRC)) {
  const rel = path.relative(SRC, file).split(path.sep).join('/');
  const text = markup(fs.readFileSync(file, 'utf8'));
  let m;
  STATIC_STYLE.lastIndex = 0;
  while ((m = STATIC_STYLE.exec(text))) {
    if (m[1].includes('{')) continue;
    counts[rel] = (counts[rel] || 0) + 1;
    (where[rel] ||= []).push(text.slice(0, m.index).split('\n').length);
  }
}

const baseline = fs.existsSync(BASELINE) ? JSON.parse(fs.readFileSync(BASELINE, 'utf8')) : {};

if (process.argv.includes('--update')) {
  const next = {};
  for (const [f, n] of Object.entries(counts).sort()) {
    // База только снижается: --update не узаконивает новые атрибуты
    next[f] = Math.min(n, baseline[f] ?? n);
  }
  fs.writeFileSync(BASELINE, JSON.stringify(next, null, 2) + '\n');
  const total = Object.values(next).reduce((a, b) => a + b, 0);
  console.log(`check-inline-styles: база обновлена, всего ${total}`);
  process.exit(0);
}

const problems = [];
for (const [f, n] of Object.entries(counts)) {
  const allowed = baseline[f] ?? 0;
  if (n > allowed)
    problems.push(`  src/${f}: ${n} (было ${allowed}), строки ${where[f].join(', ')}`);
}
if (problems.length > 0) {
  console.error('check-inline-styles: новый статичный style="…" в разметке — вынесите в класс:');
  for (const p of problems) console.error(p);
  process.exit(1);
}
const total = Object.values(counts).reduce((a, b) => a + b, 0);
const stale = Object.entries(baseline).filter(([f, n]) => (counts[f] ?? 0) < n);
console.log(
  `check-inline-styles: OK (осталось ${total})` +
    (stale.length ? `; база выше факта в ${stale.length} файл(ах) — обновите --update` : '')
);
