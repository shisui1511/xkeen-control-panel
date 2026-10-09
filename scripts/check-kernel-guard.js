#!/usr/bin/env node
/*
 * check-kernel-guard.js — единый источник состояния ядра во фронтенде (ISOL-04).
 *
 * Состояние «какое ядро активно» вычисляется в одном месте (lib/kernelState.ts)
 * и раздаётся сторами (stores.ts). Если компонент читает поле активного ядра
 * из ответа capabilities напрямую, в конфликте ядер он выберет ядро сам и
 * обойдёт гейт. Скрипт не даёт вернуть такую проверку: ищет в frontend/src
 * обращения `.active_kernel` и `.activeKernel` (через точку) вне ALLOWLIST,
 * комментарии не учитываются.
 *
 * Usage: node scripts/check-kernel-guard.js [--root <каталог>]
 */
const fs = require('fs');
const path = require('path');

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf('--root');
const root = rootIdx >= 0 ? path.resolve(argv[rootIdx + 1]) : path.resolve(__dirname, '..');
const srcDir = path.join(root, 'frontend', 'src');

/** Обращение к полю через точку: `x.active_kernel`, `x.activeKernel`. */
const FORBIDDEN_PATTERNS = [/\.active_kernel\b/, /\.activeKernel\b/];

/** Пути относительно frontend/src, где обращение разрешено, и причины. */
const ALLOWLIST = {
  // стор capabilities: единственное место, где поле читается из ответа сервера
  'stores.ts': 'стор capabilities и производные isXray/isMihomo/isConflict',
  // чистая модель: превращает ответ capabilities в состояние ядра
  'lib/kernelState.ts': 'чистая модель состояния ядра без Svelte',
  // кэш меню: нормализация сохранённого ответа capabilities для навигации
  'lib/navCaps.ts': 'кэш capabilities для меню',
  // исход применения с сервера apply, а не capabilities
  'lib/serviceApply.ts': 'ApplyResult с сервера apply',
  // исход применения конструктора Mihomo, а не capabilities
  'lib/constructors/mihomoApply.ts': 'result.active_kernel исхода применения'
};

const HINT =
  'Используйте isXray/isMihomo/isConflict/activeKernelName из stores.ts ' +
  'или kernelStateOf из lib/kernelState.ts вместо прямого чтения поля активного ядра.';

/** Убирает комментарии, сохраняя переводы строк (номера строк остаются верными). */
function stripComments(text) {
  const keepNewlines = (m) => m.replace(/[^\n]/g, '');
  return (
    text
      .replace(/\/\*[\s\S]*?\*\//g, keepNewlines)
      .replace(/<!--[\s\S]*?-->/g, keepNewlines)
      // однострочный комментарий; `://` в адресах не считается началом комментария
      .replace(/(^|[^:\\])\/\/.*$/gm, '$1')
  );
}

/** Ищет запрещённые обращения в тексте файла (комментарии не учитываются). */
function findForbidden(content) {
  const findings = [];
  const original = content.split('\n');
  const lines = stripComments(content).split('\n');
  lines.forEach((line, i) => {
    if (FORBIDDEN_PATTERNS.some((re) => re.test(line))) {
      findings.push({ line: i + 1, text: (original[i] ?? line).trim() });
    }
  });
  return findings;
}

function collectSources(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === 'node_modules') continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      collectSources(full, out);
    } else if (/\.(ts|svelte)$/.test(entry.name) && !/\.(test|spec)\.ts$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

const problems = [];

// Самопроверка сканера: если регулярки или очистка комментариев сломаны, страж молчал бы.
const samples = [
  ["const k = $capabilities.active_kernel === 'xray';", 1],
  ['{#if $capabilities?.activeKernel}x{/if}', 1],
  ['const a = caps.active_kernel;\nconst b = 1;\nlet c = r.activeKernel;', 2],
  ["const p = { active_kernel: '' };", 0],
  ['let { activeKernel }: Props = $props();', 0],
  ['<Card activeKernel={kernel} />', 0],
  ['// прежде читали $capabilities.active_kernel', 0],
  ['/* $capabilities.active_kernel */\nconst x = 1;', 0],
  ['<!-- $capabilities.activeKernel -->', 0]
];
for (const [text, want] of samples) {
  const got = findForbidden(text).length;
  if (got !== want) problems.push(`самопроверка сканера: образец ${JSON.stringify(text)} дал ${got}, ожидалось ${want}`);
}
const lineCheck = findForbidden('const a = caps.active_kernel;\nconst b = 1;\nlet c = r.activeKernel;').map((f) => f.line);
if (lineCheck.join(',') !== '1,3') problems.push(`самопроверка сканера: номера строк ${lineCheck.join(',')}, ожидалось 1,3`);

if (!fs.existsSync(srcDir)) {
  console.error(`❌ нет каталога ${path.relative(root, srcDir) || srcDir}`);
  process.exit(1);
}

// Allowlist не устарел: каждый файл существует.
for (const rel of Object.keys(ALLOWLIST)) {
  if (!fs.existsSync(path.join(srcDir, rel))) problems.push(`в allowlist нет файла frontend/src/${rel}`);
}

const files = collectSources(srcDir);
if (files.length <= 50) problems.push(`найдено только ${files.length} исходников в frontend/src — сканер сломан`);

const violations = [];
for (const file of files) {
  const rel = path.relative(srcDir, file).split(path.sep).join('/');
  if (rel in ALLOWLIST) continue;
  for (const f of findForbidden(fs.readFileSync(file, 'utf8'))) {
    violations.push(`${rel}:${f.line}: ${f.text}`);
  }
}

if (violations.length > 0 || problems.length > 0) {
  console.error('❌ Прямое чтение поля активного ядра вне allowlist (ISOL-04):');
  for (const v of violations) console.error('  ' + v);
  for (const p of problems) console.error('  ' + p);
  if (violations.length > 0) console.error(HINT);
  process.exit(1);
}
console.log(`✅ Поле активного ядра читается только в allowlist (${files.length} исходников проверено)`);
