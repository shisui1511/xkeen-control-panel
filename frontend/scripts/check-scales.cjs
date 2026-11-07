/* eslint-disable */
// Храповик шкал дизайн-системы: отступы, радиусы, слои z-index и тени берутся из токенов
// src/styles/global.css, а не задаются литералами в <style> компонентов и в *.css.
//   spacing — padding*, margin*, gap, row-gap, column-gap: длина в px/rem вместо
//             var(--spacing-…). Разрешены 0 и ±1px (оптическая подгонка), em и % —
//             они считают от родителя.
//   radius  — border-radius и border-*-radius: длина в px/rem вместо var(--radius-…).
//             Круг — литерал 50%, проценты разрешены.
//   z-index — число вне -1…10 вместо var(--z-…). Малые значения — локальный порядок
//             внутри компонента, всё выше — общий слой страницы.
//   shadow  — box-shadow с цветом-литералом (rgba(0,0,0,…), #hex, black/white) вместо
//             var(--shadow-…) или цвета из токена (rgba(var(--accent-rgb), …)).
// Запасное значение внутри var(--x, …) не проверяется. Объявления самих токенов (--x: …)
// не проверяются. Старый код ещё содержит литералы: их число по файлам и видам записано
// в scripts/scales-baseline.json. Число может только уменьшаться; новый файл — ноль.
// После чистки: node scripts/check-scales.cjs --update (снижает базу).
const fs = require('fs');
const path = require('path');

const SRC = path.join(__dirname, '../src');
const BASELINE = path.join(__dirname, 'scales-baseline.json');
const KINDS = ['spacing', 'radius', 'z-index', 'shadow'];

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const f = path.join(dir, e.name);
    if (e.isDirectory()) walk(f, out);
    else if (/\.(svelte|css)$/.test(e.name)) out.push(f);
  }
  return out;
}

// Оставляет только CSS: в .svelte — содержимое <style>, остальное заменяет пустыми
// строками, чтобы номера строк совпадали с файлом. Комментарии гасятся так же.
function cssOf(file, text) {
  let css = text;
  if (file.endsWith('.svelte')) {
    css = text.replace(/<style[^>]*>([\s\S]*?)<\/style>|[\s\S]/gi, (m, body) =>
      body !== undefined
        ? ' '.repeat(m.length - body.length - 8) + body + '        '
        : m === '\n'
          ? '\n'
          : ' '
    );
  }
  return css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));
}

// var(--x, запасное) → var(--x): запасное значение никогда не срабатывает, если токен объявлен
function stripFallbacks(value) {
  let out = '';
  for (let i = 0; i < value.length; i++) {
    const m = value.slice(i).match(/^var\(\s*(--[\w-]+)\s*,/);
    if (!m) {
      out += value[i];
      continue;
    }
    let depth = 1;
    let j = i + m[0].length;
    for (; j < value.length && depth > 0; j++) {
      if (value[j] === '(') depth++;
      else if (value[j] === ')') depth--;
    }
    out += `var(${m[1]})`;
    i = j - 1;
  }
  return out;
}

const LENGTH = /(-?\d*\.?\d+)(px|rem)\b/g;
const SPACING_PROP =
  /^(padding|margin)(-(top|right|bottom|left|inline|block)(-(start|end))?)?$|^(row-|column-)?gap$/;
const RADIUS_PROP = /^border(-(top|bottom|start|end)-(left|right|start|end))?-radius$/;
const LITERAL_COLOR = /rgba?\(\s*[\d.]|hsla?\(\s*[\d.]|#[0-9a-f]{3,8}\b|\b(black|white)\b/i;

function violation(prop, raw) {
  const value = stripFallbacks(raw.replace(/!important/, '').trim());
  if (SPACING_PROP.test(prop)) {
    for (const [, n, unit] of value.matchAll(LENGTH)) {
      const v = Math.abs(parseFloat(n));
      if (v === 0 || (unit === 'px' && v === 1)) continue;
      return 'spacing';
    }
  } else if (RADIUS_PROP.test(prop)) {
    for (const [, n] of value.matchAll(LENGTH)) if (parseFloat(n) !== 0) return 'radius';
  } else if (prop === 'z-index') {
    if (/^-?\d+$/.test(value) && (+value < -1 || +value > 10)) return 'z-index';
  } else if (prop === 'box-shadow') {
    if (LITERAL_COLOR.test(value)) return 'shadow';
  }
  return null;
}

const DECL = /(?<![\w-])([a-z][a-z-]*)\s*:\s*([^;{}]+)/g;
const counts = Object.fromEntries(KINDS.map((k) => [k, {}]));
const where = Object.fromEntries(KINDS.map((k) => [k, {}]));
for (const file of walk(SRC)) {
  const rel = path.relative(SRC, file).split(path.sep).join('/');
  const css = cssOf(file, fs.readFileSync(file, 'utf8'));
  for (const m of css.matchAll(DECL)) {
    const kind = violation(m[1], m[2]);
    if (!kind) continue;
    counts[kind][rel] = (counts[kind][rel] || 0) + 1;
    (where[kind][rel] ||= []).push(css.slice(0, m.index).split('\n').length);
  }
}

// База читается одним вызовом: нет файла — пустая база (первый запуск --update)
function readBaseline() {
  try {
    return JSON.parse(fs.readFileSync(BASELINE, 'utf8'));
  } catch (e) {
    if (e.code === 'ENOENT') return {};
    throw e;
  }
}
const baseline = readBaseline();
const total = (k) => Object.values(counts[k]).reduce((a, b) => a + b, 0);

if (process.argv.includes('--update')) {
  const next = {};
  for (const k of KINDS) {
    next[k] = {};
    for (const [f, n] of Object.entries(counts[k]).sort()) {
      // База только снижается: --update не узаконивает новые литералы
      next[k][f] = Math.min(n, baseline[k]?.[f] ?? n);
    }
  }
  fs.writeFileSync(BASELINE, JSON.stringify(next, null, 2) + '\n');
  console.log(`check-scales: база обновлена — ${KINDS.map((k) => `${k} ${total(k)}`).join(', ')}`);
  process.exit(0);
}

const HINT = {
  spacing: 'var(--spacing-…)',
  radius: 'var(--radius-…) или 50% для круга',
  'z-index': 'var(--z-…) или литерал -1…10',
  shadow: 'var(--shadow-…) или цвет из токена'
};
const problems = [];
let stale = 0;
for (const k of KINDS) {
  for (const [f, n] of Object.entries(counts[k])) {
    const allowed = baseline[k]?.[f] ?? 0;
    if (n > allowed)
      problems.push(
        `  [${k}] src/${f}: ${n} (было ${allowed}) → ${HINT[k]}; строки ${where[k][f].join(', ')}`
      );
  }
  for (const [f, n] of Object.entries(baseline[k] || {})) if ((counts[k][f] ?? 0) < n) stale++;
}
if (problems.length > 0) {
  console.error('check-scales: новые литералы в обход шкал дизайн-системы:');
  for (const p of problems) console.error(p);
  process.exit(1);
}
console.log(
  `check-scales: OK (осталось ${KINDS.map((k) => `${k} ${total(k)}`).join(', ')})` +
    (stale ? `; база выше факта в ${stale} месте(ах) — обновите --update` : '')
);
