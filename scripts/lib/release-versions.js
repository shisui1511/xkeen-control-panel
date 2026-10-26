/*
 * release-versions.js — общая библиотека номеров версий панели (D-20, D-22).
 *
 * Без зависимостей и без побочных эффектов при загрузке. Её используют стражи CI
 * (check-release-versions.js, check-commits.js) и локальные хуки.
 *
 * Определения:
 *   - Линия панели — номера `v<major>.N.N`, где major берётся от наибольшего ядра среди тегов `v*`
 *     (без тегов — 0).
 *   - Невышедший номер — номер линии панели, ядро которого строго больше наибольшего ядра среди
 *     всех тегов `v*` (stable и пре-релизы, включая `-rc.N`). Осознанное упрощение D-20: пропуски
 *     ниже наибольшего ядра (несуществующий v0.N.7 при вышедшем v0.N+1.0) не ловятся. Сверка с
 *     множеством ядер тегов дала бы ложные срабатывания: prune_dev_releases.sh удаляет теги
 *     вышедших пре-релизов, и множество тегов не хранит всех вышедших номеров.
 *   - Чужая версия — номер, рядом с которым (в пределах 24 символов на той же строке) стоит слово
 *     xray, mihomo, xkeen, ядр, ядер или kernel: это версия ядра или другой программы, а не панели.
 *     Имя самой панели (xkeen-control-panel, «XKeen Control Panel») чужим словом не считается.
 *   - Исключение — строка `version-ok: <версия> <причина>` (причина не короче 3 символов; внутри
 *     HTML-комментария тоже засчитывается): снимает только названную версию (по ядру).
 */
const { execFileSync } = require('child_process');

const TAG_REGEX = /^v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/;
const FOREIGN_WORDS = /(xray|mihomo|xkeen|ядр|ядер|kernel)/i;
// Имя самой панели (xkeen-control-panel, XKeen Control Panel) чужим словом не считается
const PANEL_NAME = /xkeen[\s_-]*control[\s_-]*panel/gi;
const FOREIGN_WINDOW = 24;
const EXEMPT_REGEX = /version-ok:[ \t]*v?(\d+)\.(\d+)\.(\d+)(?:-[0-9A-Za-z.-]+)?[ \t]+([^\n]*)/g;

// Разбирает тег вида vX.Y.Z[-pre]; иначе null.
function parseTag(tag) {
  const m = TAG_REGEX.exec(String(tag).trim());
  if (!m) return null;
  return { major: Number(m[1]), minor: Number(m[2]), patch: Number(m[3]), pre: m[4] || null };
}

// Сравнивает ядра major.minor.patch: -1, 0 или 1.
function coreCmp(a, b) {
  for (const k of ['major', 'minor', 'patch']) {
    if (a[k] < b[k]) return -1;
    if (a[k] > b[k]) return 1;
  }
  return 0;
}

// Теги `v*` репозитория. execFileSync без shell.
function localTags(root) {
  const out = execFileSync('git', ['tag', '-l', 'v*'], { cwd: root, encoding: 'utf8' });
  return out.split('\n').map(s => s.trim()).filter(Boolean);
}

// Наибольшее ядро среди тегов v* (пре-релизы считаются по ядру) или null.
function maxCore(tags) {
  let best = null;
  for (const t of tags) {
    const p = parseTag(t);
    if (!p) continue;
    const core = { major: p.major, minor: p.minor, patch: p.patch };
    if (!best || coreCmp(core, best) > 0) best = core;
  }
  return best;
}

function formatCore(core) {
  return `v${core.major}.${core.minor}.${core.patch}`;
}

// Регулярное выражение номеров линии major. Якоря против четырёхчастных адресов (0.0.0.0, 127.0.0.1).
function panelVersionRegex(major) {
  return new RegExp(`(?<![\\w.])v?${Number(major)}\\.\\d+\\.\\d+(?![\\w]|\\.\\d)`, 'g');
}

function coreKey(c) {
  return `${c.major}.${c.minor}.${c.patch}`;
}

// Ядра из строк `version-ok: <версия> <причина>` (ключ — 'X.Y.Z').
function exemptions(text) {
  const set = new Set();
  const re = new RegExp(EXEMPT_REGEX.source, 'g');
  let m;
  while ((m = re.exec(text)) !== null) {
    const reason = m[4].replace(/-->.*$/, '').trim();
    if (reason.length >= 3) set.add(`${Number(m[1])}.${Number(m[2])}.${Number(m[3])}`);
  }
  return set;
}

// Номера линии major в тексте, кроме чужих версий и исключённых отметкой.
function findPanelVersions(text, major) {
  const exempt = exemptions(text);
  const re = panelVersionRegex(major);
  const found = [];
  let m;
  while ((m = re.exec(text)) !== null) {
    const start = m.index;
    const end = start + m[0].length;
    const lineStart = text.lastIndexOf('\n', start - 1) + 1;
    let lineEnd = text.indexOf('\n', end);
    if (lineEnd < 0) lineEnd = text.length;
    // имя панели вырезается из всей строки до окна: обрезанное окном имя тоже не «чужое»
    const lineText = text.slice(lineStart, lineEnd);
    const masked = lineText.replace(PANEL_NAME, s => ' '.repeat(s.length));
    const left = masked.slice(Math.max(0, start - lineStart - FOREIGN_WINDOW), start - lineStart);
    const right = masked.slice(end - lineStart, Math.min(lineEnd, end + FOREIGN_WINDOW) - lineStart);
    if (FOREIGN_WORDS.test(left) || FOREIGN_WORDS.test(right)) continue;
    const parts = /(\d+)\.(\d+)\.(\d+)/.exec(m[0]);
    const core = { major: Number(parts[1]), minor: Number(parts[2]), patch: Number(parts[3]) };
    if (exempt.has(coreKey(core))) continue;
    found.push({
      version: m[0],
      core,
      line: text.slice(0, start).split('\n').length,
      index: start
    });
  }
  return found;
}

// Невышедшие номера: ядро строго больше наибольшего среди тегов. Без тегов — пусто.
function findUnreleased(text, tags) {
  const max = maxCore(tags);
  if (!max) return [];
  return findPanelVersions(text, max.major).filter(f => coreCmp(f.core, max) > 0);
}

module.exports = {
  parseTag,
  coreCmp,
  localTags,
  maxCore,
  formatCore,
  panelVersionRegex,
  exemptions,
  findPanelVersions,
  findUnreleased
};
