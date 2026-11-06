/* eslint-disable */
// Страж шкалы движения: длительности переходов и разовых анимаций берутся из токенов
// --duration-* / --transition-* (src/styles/global.css), а не пишутся числом. Разнобой
// 0.1s / 0.15s / 0.18s / 0.2s делает отклик панели неровным, и его не видно на ревью.
// Проверяются объявления transition, transition-duration и animation в .svelte и .css.
// Разрешены: 0s/0ms (выключение движения), бесконечные циклы (спиннеры, пульс — у них
// свой ритм), отрицательные и любые animation-delay, объявления токенов в :root global.css.
const fs = require('fs');
const path = require('path');

const SRC = path.join(__dirname, '../src');
const GLOBAL = path.join(SRC, 'styles/global.css');
const DUR = /(?<![\w-])(\d*\.?\d+)(ms|s)\b/g;

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const f = path.join(dir, e.name);
    if (e.isDirectory()) walk(f, out);
    else if (/\.(svelte|css)$/.test(e.name)) out.push(f);
  }
  return out;
}

const DECL = /(?<![\w-])(transition|transition-duration|animation)\s*:([^;{}]*);/g;
const problems = [];
for (const file of walk(SRC)) {
  const text = fs.readFileSync(file, 'utf8');
  let m;
  DECL.lastIndex = 0;
  while ((m = DECL.exec(text))) {
    const [, prop, value] = m;
    if (prop === 'animation' && /\binfinite\b/.test(value)) continue;
    const raw = [...value.matchAll(DUR)].filter((d) => Number(d[1]) !== 0);
    if (raw.length === 0) continue;
    if (file === GLOBAL && /^\s*--/.test(text.slice(text.lastIndexOf('\n', m.index) + 1, m.index)))
      continue;
    const line = text.slice(0, m.index).split('\n').length;
    problems.push(
      `${path.relative(path.join(__dirname, '..'), file)}:${line}  ${prop}: ${value.trim().replace(/\s+/g, ' ')}`
    );
  }
}

if (problems.length > 0) {
  console.error('check-motion: длительность числом вместо токена шкалы движения');
  console.error('  (--duration-fast/exit/enter/move/slow, --transition-fast/normal/move):');
  for (const p of problems) console.error('  ' + p);
  process.exit(1);
}
console.log('check-motion: OK');
