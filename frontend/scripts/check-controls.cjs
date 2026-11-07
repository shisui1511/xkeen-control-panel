/* eslint-disable */
// Страж элементов управления: кнопки, поля, флажки и переключатели идут через компоненты
// Button, Input, Checkbox, Toggle (src/components), вид — классы .btn / .input / .checkbox /
// .toggle-switch из global.css.
//  Запрещено сразу:
//   - класс .form-input (слит с .input; плотное поле — .input.input-sm);
//   - переключатель .toggle-switch без role="switch" у флажка.
//  Храповик (число по файлам в scripts/controls-baseline.json может только уменьшаться):
//   - button    — <button class="btn …"> в разметке вместо <Button>;
//   - input     — текстовый <input> (text, number, search, url, email, password, tel) вместо <Input>;
//   - checkbox  — <input type="checkbox"> вместо <Checkbox> или <Toggle>;
//   - override  — селектор в <style> компонента, который перекрашивает системные .btn* / .input;
//   - svg       — статичный встроенный <svg> (без {выражений}) вместо <Icon name=…>
//                 из lib/components/Icon.svelte. Графики и индикаторы с данными не считаются.
//  После чистки: node scripts/check-controls.cjs --update (снижает базу).
const fs = require('fs');
const path = require('path');

const SRC = path.join(__dirname, '../src');
const BASELINE = path.join(__dirname, 'controls-baseline.json');
const KINDS = ['button', 'input', 'checkbox', 'override', 'svg'];
const SVG_OWN = /^(lib\/components\/icons\/|components\/BrandMark\.svelte$)/;
const OWN = new Set([
  'components/Button.svelte',
  'components/Input.svelte',
  'components/Checkbox.svelte',
  'components/Toggle.svelte'
]);
const SYSTEM_CLASS =
  /\.(btn|btn-(primary|secondary|ghost|danger|danger-soft|warning|sm|xs|icon-only)|input|input-sm)(?![\w-])/;
const TEXT_TYPES = new Set(['text', 'number', 'search', 'url', 'email', 'password', 'tel']);

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const f = path.join(dir, e.name);
    if (e.isDirectory()) walk(f, out);
    else if (e.name.endsWith('.svelte')) out.push(f);
  }
  return out;
}

// Открывающие теги <button> и <input> с атрибутами: скобки {…} и кавычки могут быть вложенными
function* tags(markup) {
  const re = /<(button|input)\b/g;
  let m;
  while ((m = re.exec(markup))) {
    let depth = 0;
    let quote = '';
    let i = re.lastIndex;
    for (; i < markup.length; i++) {
      const c = markup[i];
      if (quote) {
        if (c === quote) quote = '';
      } else if (depth > 0) {
        if (c === '{') depth++;
        else if (c === '}') depth--;
        else if (c === '"' || c === "'" || c === '`') quote = c;
      } else if (c === '{') depth++;
      else if (c === '"' || c === "'") quote = c;
      else if (c === '>') break;
    }
    yield { tag: m[1], attrs: markup.slice(re.lastIndex, i), index: m.index };
    re.lastIndex = i;
  }
}

const blank = (m) => m.replace(/[^\n]/g, ' ');
const lineOf = (text, idx) => text.slice(0, idx).split('\n').length;

const counts = Object.fromEntries(KINDS.map((k) => [k, {}]));
const where = Object.fromEntries(KINDS.map((k) => [k, {}]));
const problems = [];
const add = (k, rel, line) => {
  counts[k][rel] = (counts[k][rel] || 0) + 1;
  (where[k][rel] ||= []).push(line);
};

for (const file of walk(SRC)) {
  const rel = path.relative(SRC, file).split(path.sep).join('/');
  if (OWN.has(rel)) continue;
  const text = fs.readFileSync(file, 'utf8');
  const markup = text
    .replace(/<script[\s\S]*?<\/script>/g, blank)
    .replace(/<style[\s\S]*?<\/style>/g, blank)
    .replace(/<!--[\s\S]*?-->/g, blank);

  for (const m of tags(markup)) {
    const { tag, attrs } = m;
    const line = lineOf(markup, m.index);
    const cls = (attrs.match(/\sclass="([^"]*)"/) || [, ''])[1].replace(/\{[^}]*\}/g, ' ');
    if (/(?<![\w-])form-input(?![\w-])/.test(cls))
      problems.push(`  src/${rel}:${line}: класс form-input → input (input-sm)`);
    if (tag === 'button') {
      if (cls.split(/\s+/).includes('btn')) add('button', rel, line);
      continue;
    }
    const type = (attrs.match(/\stype="([a-z]+)"/) || [, 'text'])[1];
    if (type === 'checkbox') {
      add('checkbox', rel, line);
      const before = markup.slice(Math.max(0, m.index - 120), m.index);
      if (/class="toggle-switch[^"]*"[^<]*$/.test(before) && !/\srole="switch"/.test(attrs))
        problems.push(`  src/${rel}:${line}: флажок в .toggle-switch без role="switch" → <Toggle>`);
    } else if (TEXT_TYPES.has(type) && !/\stype=\{/.test(attrs)) add('input', rel, line);
  }

  if (!SVG_OWN.test(rel)) {
    for (const m of markup.matchAll(/<svg\b[\s\S]*?<\/svg>/g)) {
      if (!m[0].includes('{')) add('svg', rel, lineOf(markup, m.index));
    }
  }

  // Перекраска системных классов в стиле компонента
  const style = text.replace(/<style[^>]*>([\s\S]*?)<\/style>|[\s\S]/g, (m, body) =>
    body !== undefined
      ? ' '.repeat(m.length - body.length - 8) + body + '        '
      : m === '\n'
        ? '\n'
        : ' '
  );
  const css = style.replace(/\/\*[\s\S]*?\*\//g, blank);
  for (const sel of css.matchAll(/(^|[};])([^{};@]*)\{/g)) {
    if (SYSTEM_CLASS.test(sel[2].replace(/:global\(([^)]*)\)/g, '$1')))
      add('override', rel, lineOf(css, sel.index + sel[1].length));
  }
}

const baseline = fs.existsSync(BASELINE) ? JSON.parse(fs.readFileSync(BASELINE, 'utf8')) : {};
const total = (k) => Object.values(counts[k]).reduce((a, b) => a + b, 0);
const summary = () => KINDS.map((k) => `${k} ${total(k)}`).join(', ');

if (process.argv.includes('--update')) {
  const next = {};
  for (const k of KINDS) {
    next[k] = {};
    for (const [f, n] of Object.entries(counts[k]).sort())
      next[k][f] = Math.min(n, baseline[k]?.[f] ?? n);
  }
  fs.writeFileSync(BASELINE, JSON.stringify(next, null, 2) + '\n');
  console.log(`check-controls: база обновлена — ${summary()}`);
  process.exit(problems.length ? 1 : 0);
}

const HINT = {
  button: '<Button>',
  input: '<Input>',
  checkbox: '<Checkbox> или <Toggle>',
  override: 'вариант или размер Button/Input вместо перекраски .btn/.input',
  svg: '<Icon name=…> (новая иконка — файл в lib/components/icons)'
};
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
if (problems.length) {
  console.error('check-controls: элементы управления в обход компонентов:');
  for (const p of problems) console.error(p);
  process.exit(1);
}
console.log(
  `check-controls: OK (осталось ${summary()})` +
    (stale ? `; база выше факта в ${stale} месте(ах) — обновите --update` : '')
);
