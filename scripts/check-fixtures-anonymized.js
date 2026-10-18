#!/usr/bin/env node
/*
 * check-fixtures-anonymized.js — входные данные разборщиков узлов обезличены:
 * internal/nodes/testdata/inputs и internal/nodes/testdata/links не содержат
 * ни реальных доменов, ни реальных адресов провайдера (D-36).
 *
 * Правила (по тексту файла; base64-вставки внутри ссылок и файлы .b64
 * декодируются и проверяются так же):
 *   1. каждый домен (имя с точкой и буквенной зоной) — в зоне .example;
 *   2. каждый IPv4 — из TEST-NET (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24)
 *      либо частный/служебный (10/8, 127/8, 0.0.0.0): туннельные адреса
 *      конфигураций wg и loopback;
 *   3. URL со схемой http(s) ведёт только на хосты зоны .example.
 * Имена файлов вроде geoip.dat после «/» доменами не считаются.
 *
 * Usage: node scripts/check-fixtures-anonymized.js [--root <каталог>]
 */
const fs = require('fs');
const path = require('path');

const argv = process.argv.slice(2);
const rootIdx = argv.indexOf('--root');
const root = rootIdx >= 0 ? path.resolve(argv[rootIdx + 1]) : path.resolve(__dirname, '..');
const DIRS = ['internal/nodes/testdata/inputs', 'internal/nodes/testdata/links'];

const HOST_RE = /(?<![\w./@%-])[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}(?![\w-])/g;
const IPV4_RE = /(?<![\d.])(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?![\d])/g;
const URL_RE = /\bhttps?:\/\/(?:[^\s/@"'<>]*@)?([^\s/"'<>:?#@]+)/gi;
const B64_RE = /[A-Za-z0-9+/_-]{16,}={0,2}/g;

function ipAllowed([a, b, c]) {
  if (a === 10 || a === 127 || a === 0) return true;
  return (a === 192 && b === 0 && c === 2) || (a === 198 && b === 51 && c === 100) || (a === 203 && b === 0 && c === 113);
}

function decodeBase64(s) {
  for (const enc of ['base64', 'base64url']) {
    const buf = Buffer.from(s, enc);
    if (buf.length < 8) continue;
    const text = buf.toString('utf8');
    // Только осмысленный текст: без управляющих символов и символов замены.
    if (!/[\u0000-\u0008\u000e-\u001f�]/.test(text)) return text;
  }
  return null;
}

// Тексты для проверки: сам файл и вложенные декодированные base64-вставки.
function texts(content, depth = 0) {
  const out = [content];
  if (depth >= 2) return out;
  for (const m of content.matchAll(B64_RE)) {
    const dec = decodeBase64(m[0]);
    if (dec !== null) out.push(...texts(dec, depth + 1));
  }
  return out;
}

const problems = [];
let files = 0;

function check(rel) {
  const full = path.join(root, rel);
  let content = fs.readFileSync(full, 'utf8');
  if (rel.endsWith('.b64')) {
    const dec = decodeBase64(content.replace(/\s+/g, ''));
    if (dec === null) {
      problems.push(`${rel}: base64 не декодируется`);
      return;
    }
    content = dec;
  }
  files++;
  const seen = new Set();
  const note = (msg) => {
    if (!seen.has(msg)) {
      seen.add(msg);
      problems.push(`${rel}: ${msg}`);
    }
  };
  for (const text of texts(content)) {
    for (const m of text.matchAll(HOST_RE)) {
      if (!m[0].toLowerCase().endsWith('.example')) note(`домен вне зоны .example (длина ${m[0].length})`);
    }
    for (const m of text.matchAll(IPV4_RE)) {
      const o = m.slice(1, 5).map(Number);
      if (o.some((n) => n > 255)) continue;
      if (!ipAllowed(o)) note(`IPv4 вне TEST-NET (${o[0]}.x.x.x)`);
    }
    for (const m of text.matchAll(URL_RE)) {
      if (!m[1].toLowerCase().endsWith('.example')) note(`URL ведёт не на зону .example (длина хоста ${m[1].length})`);
    }
  }
}

for (const dir of DIRS) {
  let names = [];
  try {
    names = fs.readdirSync(path.join(root, dir)).sort();
  } catch (e) {
    problems.push(`${dir}: каталог не читается: ${e.message}`);
    continue;
  }
  for (const n of names) {
    if (fs.statSync(path.join(root, dir, n)).isFile()) check(`${dir}/${n}`);
  }
}

if (problems.length > 0) {
  // Значения не печатаются: сообщение называет файл и вид нарушения.
  for (const p of problems) console.error('❌ ' + p);
  process.exit(1);
}
console.log(`✅ Входные данные обезличены: файлов ${files}`);
