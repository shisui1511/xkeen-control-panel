/* eslint-disable */
// Страж бейджей: в панели одна система — .badge (Badge.svelte, StatusBadge.svelte, global.css).
//  1) Убранные классы запрещены везде: .chip и .chip-*, .badge-error / -primary / -type,
//     .status-badge, .status-pill. Замена: .badge (soft), .badge--tag, .badge--status,
//     тоны .badge-neutral / -emphasis / -info / -success / -warning / -danger.
//  2) Храповик своих бейджей: класс с badge / chip / pill в имени, объявленный в <style>
//     компонента, — это ещё одна система бейджей. Старые такие классы записаны по файлам в
//     scripts/badges-baseline.json; число может только уменьшаться, новый файл — ноль.
//     Переопределять системные классы (.badge, .badge-info …) под место — можно.
//     После чистки: node scripts/check-badges.cjs --update (снижает базу).
const fs = require('fs');
const path = require('path');

const SRC = path.join(__dirname, '../src');
const BASELINE = path.join(__dirname, 'badges-baseline.json');

const SYSTEM = new Set([
  'badge',
  'badge-neutral',
  'badge-emphasis',
  'badge-info',
  'badge-success',
  'badge-warning',
  'badge-danger',
  'badge-dot',
  'badge-dot--pulse',
  'badge--tag',
  'badge--status',
  'badge--dot',
  'badge--icon'
]);
const REMOVED =
  /^(chip|chip-(success|warning|danger|info|default)|chip--[a-z]+|badge-(error|primary|type)|status-badge|status-badge-dot|status-pill(-[a-z-]+)?)$/;
const BADGE_LIKE = /(^|-)(badge|badges|chip|chips|pill|pills)(-|$)/;

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const f = path.join(dir, e.name);
    if (e.isDirectory()) walk(f, out);
    else if (/\.(svelte|css|ts)$/.test(e.name)) out.push(f);
  }
  return out;
}

const lineOf = (text, idx) => text.slice(0, idx).split('\n').length;
const noComments = (s) => s.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));

const problems = [];
const counts = {};
const where = {};
for (const file of walk(SRC)) {
  const rel = path.relative(SRC, file).split(path.sep).join('/');
  const text = fs.readFileSync(file, 'utf8');
  const isGlobal = rel === 'styles/global.css';

  // Стили: .svelte — блоки <style>, *.css — весь файл (с сохранением номеров строк)
  let css = '';
  if (file.endsWith('.svelte')) {
    css = text.replace(/<style[^>]*>([\s\S]*?)<\/style>|[\s\S]/gi, (m, body) =>
      body !== undefined
        ? ' '.repeat(m.length - body.length - 8) + body + '        '
        : m === '\n'
          ? '\n'
          : ' '
    );
  } else if (file.endsWith('.css')) css = text;
  css = noComments(css);
  // Селекторы — текст до «{», без значений деклараций
  for (const sel of css.matchAll(/(^|[};])([^{};]*)\{/g)) {
    if (/^\s*@/.test(sel[2])) continue;
    for (const m of sel[2].matchAll(/\.([a-zA-Z][\w-]*)/g)) {
      const name = m[1];
      const at = sel.index + sel[1].length + m.index;
      if (REMOVED.test(name)) {
        problems.push(`  src/${rel}:${lineOf(css, at)}: убранный класс .${name} → система .badge`);
      } else if (
        !isGlobal &&
        file.endsWith('.svelte') &&
        BADGE_LIKE.test(name) &&
        !SYSTEM.has(name)
      ) {
        counts[rel] = (counts[rel] || 0) + 1;
        (where[rel] ||= []).push(`${lineOf(css, at)} .${name}`);
      }
    }
  }

  // Разметка и строки скрипта: имена классов в class="…", class:…, и в строковых литералах
  if (!file.endsWith('.css')) {
    const scan = file.endsWith('.svelte')
      ? text.replace(/<style\b[^>]*>[\s\S]*?<\/style[^>]*>/gi, (m) => m.replace(/[^\n]/g, ' '))
      : text;
    const re = /class="([^"]*)"|class:([\w-]+)|(['"`])((?:(?!\3)[^\\\n]|\\.)*)\3/g;
    for (const m of scan.matchAll(re)) {
      const value = m[1] ?? m[2] ?? m[4] ?? '';
      const tokens =
        m[4] !== undefined ? value.split(/\s+/) : value.replace(/\{[^}]*\}/g, ' ').split(/\s+/);
      for (const tok of tokens) {
        // голое «chip» в JS-строке — не обязательно класс; проверяем его только в class="…"
        if (tok === 'chip' && m[4] !== undefined) continue;
        if (REMOVED.test(tok))
          problems.push(
            `  src/${rel}:${lineOf(scan, m.index)}: убранный класс ${tok} → система .badge`
          );
      }
    }
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
const total = Object.values(counts).reduce((a, b) => a + b, 0);

if (process.argv.includes('--update')) {
  const next = {};
  for (const [f, n] of Object.entries(counts).sort()) next[f] = Math.min(n, baseline[f] ?? n);
  fs.writeFileSync(BASELINE, JSON.stringify(next, null, 2) + '\n');
  console.log(
    `check-badges: база обновлена, своих бейджей ${Object.values(next).reduce((a, b) => a + b, 0)}`
  );
  process.exit(problems.length ? 1 : 0);
}

for (const [f, n] of Object.entries(counts)) {
  const allowed = baseline[f] ?? 0;
  if (n > allowed)
    problems.push(
      `  src/${f}: своих классов-бейджей ${n} (было ${allowed}) — используйте Badge / .badge; ${where[f].join(', ')}`
    );
}
if (problems.length) {
  console.error('check-badges: бейджи в обход системы .badge:');
  for (const p of problems) console.error(p);
  process.exit(1);
}
const stale = Object.entries(baseline).filter(([f, n]) => (counts[f] ?? 0) < n).length;
console.log(
  `check-badges: OK (своих бейджей осталось ${total})` +
    (stale ? `; база выше факта в ${stale} файл(ах) — обновите --update` : '')
);
