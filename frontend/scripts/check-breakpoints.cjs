/* eslint-disable */
// Детектор брейкпоинтов вне общей шкалы в @media frontend/src/**/*.{svelte,css}.
// Шкала: 480 — телефон, 768 — планшет и мобильная навигация, 1024 — узкий
// десктоп; min-width 769 — пара к 768 (десктопный сайдбар), 2401 — широкие
// экраны. @container-запросы не проверяются: они считают ширину компонента.
const fs = require('fs');
const path = require('path');

const SRC_ROOT = path.join(__dirname, '../src');
const ALLOWED = { max: [480, 768, 1024], min: [769, 1024, 2401] };

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
  const text = fs.readFileSync(file, 'utf8');
  for (const media of text.matchAll(/@media[^{]*/g)) {
    for (const m of media[0].matchAll(/\((max|min)-width:\s*([0-9.]+)(px|rem|em)\)/g)) {
      if (m[3] === 'px' && ALLOWED[m[1]].includes(Number(m[2]))) continue;
      const line = text.slice(0, media.index).split('\n').length;
      findings.push(`${path.relative(SRC_ROOT, file)}:${line}  ${m[0]}`);
    }
  }
}

if (findings.length) {
  console.error('Брейкпоинт вне шкалы (max 480/768/1024, min 769/1024/2401):');
  for (const f of findings) console.error('  ' + f);
  process.exit(1);
}
console.log('check-breakpoints: OK');
