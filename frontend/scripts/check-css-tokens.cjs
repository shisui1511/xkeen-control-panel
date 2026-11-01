/* eslint-disable */
// Страж токенов дизайн-системы: ловит var(--x) на несуществующие
// (молча ломают стили) и устаревшие имена-алиасы в frontend/src.
//  1) var(--x) без запасного значения, где --x нигде не объявлен — ошибка;
//  2) устаревшие алиасы из global.css (--primary, --fg-muted, ...) в новом
//     коде запрещены — используйте канонические имена токенов.
// Сами объявления алиасов в src/styles/global.css не проверяются.
const fs = require('fs');
const path = require('path');

const SRC = path.join(__dirname, '../src');
const GLOBAL = path.join(SRC, 'styles/global.css');

const DEPRECATED = {
  '--primary': '--accent',
  '--color-primary': '--accent',
  '--fg-muted': '--fg-dim',
  '--text-muted': '--fg-dim',
  '--text': '--fg-primary',
  '--text-secondary': '--fg-secondary',
  '--bg-surface': '--bg-card',
  '--card-bg': '--bg-card',
  '--bg-secondary': '--bg-elevated',
  '--border-color': '--border',
  '--error': '--danger'
};

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const f = path.join(dir, e.name);
    if (e.isDirectory()) walk(f, out);
    else if (/\.(svelte|css|ts)$/.test(e.name)) out.push(f);
  }
  return out;
}

const files = walk(SRC);
const defined = new Set();
const sources = new Map();
for (const f of files) {
  const s = fs.readFileSync(f, 'utf8');
  sources.set(f, s);
  for (const m of s.matchAll(/(?:^|[\s;{"'`])(--[a-zA-Z0-9-]+)\s*:/g)) defined.add(m[1]);
  for (const m of s.matchAll(/setProperty\(\s*['"](--[a-zA-Z0-9-]+)/g)) defined.add(m[1]);
  for (const m of s.matchAll(/['"`](--[a-zA-Z0-9-]+)['"`]\s*[:,\]]/g)) defined.add(m[1]);
}

const problems = [];
for (const [f, s] of sources) {
  const rel = path.relative(path.join(__dirname, '..'), f);
  const lines = s.split('\n');
  lines.forEach((line, i) => {
    for (const m of line.matchAll(/var\(\s*(--[a-zA-Z0-9-]+)\s*([,)])/g)) {
      const name = m[1];
      const hasFallback = m[2] === ',';
      if (DEPRECATED[name] && f !== GLOBAL) {
        problems.push(`${rel}:${i + 1}: устаревший токен ${name} → ${DEPRECATED[name]}`);
      } else if (!defined.has(name) && !hasFallback) {
        problems.push(
          `${rel}:${i + 1}: токен ${name} нигде не объявлен (добавьте в global.css или задайте запасное значение)`
        );
      }
    }
  });
}

// 3) rgba(...) с цветами, у которых есть токены дизайн-системы: акцент,
//    статусы (success/warning/danger/idle/purple) и белые наложения.
//    Вместо них — rgba(var(--accent-rgb), a), color-mix(... var(--success) ...)
//    или --surface-tint*/--border-light. Нейтральный чёрный (тени, подложки
//    модалок) разрешён. Исключения: строки с inset/gradient (блики на
//    тёмных поверхностях), файлы с заведомо тёмным фоном и marker
//    ds-allow-rgba в той же строке.
const RGBA_ALLOW_FILES = new Set([
  'src/components/status/SystemStatusCapsule.svelte',
  'src/components/Skeleton.svelte',
  'src/components/Terminal.svelte'
]);
const TOKEN_RGB = new Set([
  '255,255,255',
  '41,194,240',
  '70,209,138',
  '34,197,94',
  '244,112,127',
  '239,68,68',
  '235,94,85',
  '239,91,107',
  '240,180,80',
  '245,158,11',
  '245,166,35',
  '234,179,8',
  '201,162,58',
  '168,85,247',
  '220,38,38',
  '59,130,246',
  '25,118,210'
]);
for (const [f, s] of sources) {
  if (!f.endsWith('.svelte')) continue;
  const rel = path.relative(path.join(__dirname, '..'), f);
  if (RGBA_ALLOW_FILES.has(rel)) continue;
  s.split('\n').forEach((line, i) => {
    if (/inset|gradient|ds-allow-rgba/.test(line)) return;
    for (const m of line.matchAll(
      /rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*(?:,\s*[0-9.]+\s*)?\)/g
    )) {
      if (TOKEN_RGB.has(`${m[1]},${m[2]},${m[3]}`)) {
        problems.push(
          `${rel}:${i + 1}: rgba(${m[1]}, ${m[2]}, ${m[3]}, …) — используйте токены (accent-rgb, color-mix с --success/--danger/--warning, --surface-tint*, --border-light)`
        );
      }
    }
  });
}

if (problems.length) {
  console.error(`check-css-tokens: найдено проблем: ${problems.length}`);
  for (const p of problems) console.error('  ' + p);
  process.exit(1);
}
console.log('check-css-tokens: OK');
