#!/usr/bin/env node
/*
 * check-release-source.js — источник выпусков панели отделён от репозитория кода.
 *
 * Несовместимые с прежней панелью выпуски публикуются в отдельном публичном репозитории GitHub,
 * имя которого записано одной строкой в internal/releasefeed/repository (контракт встраивается в
 * бинарник через go:embed). Прежние установки читают адрес, зашитый в их сборку (репозиторий кода),
 * и видят только совместимые выпуски; новая сборка, установщик, конвейер выпусков и обвязка стенда
 * работают только с контрактом. Страж сверяет контракт с потребителями и краснеет, если:
 *
 *   ядро (всегда) — контракт пуст, не одна строка owner/name или совпадает с репозиторием кода
 *     (модуль go.mod); internal/releasefeed/source.go не встраивает контракт, не объявляет
 *     Repository/APIReleasesURL/DownloadURL/Check или SourceRepository не совпадает с go.mod;
 *     Go-код вне пакета releasefeed пишет адрес релизов строкой (api.github.com/repos/<репо>/releases,
 *     github.com/<репо>/releases/…) репозитория кода или источника выпусков;
 *   --scope client — панель и ручной установщик: internal/handlers/update.go читает список, теги,
 *     ассеты только через releasefeed (APIReleasesURL, DownloadURL, Check) и не держит своих
 *     констант адреса; scripts/setup.sh задаёт REPO строкой контракта и все адреса релизов строит от
 *     ${REPO}; роутерный тест обновления читает релизы через releasefeed.APIReleasesURL;
 *   --scope publish — конвейер выпуска (.github/scripts/auto_release.sh, .github/workflows/build.yml,
 *     .github/workflows/auto-release.yml): контракт читается из файла и проверяется на пустоту;
 *     команды gh release получают --repo из контракта и отдельный токен. Только начальный
 *     расчёт номера читает список выпусков репозитория кода; глобальный GH_REPO не задаётся;
 *     перед create проверяется и переносится исходный тег, используется --verify-tag;
 *   --scope notes — заметки и ротация (.github/scripts/gen_notes.py, .github/workflows/release-notes.yml,
 *     .github/scripts/prune_dev_releases.sh): те же правила gh release; адреса ассетов в тексте
 *     заметок строятся от контракта, а установщик ссылается на исходный тег;
 *   --scope stand-download — обвязка стенда (scripts/router/deploy.sh, run.sh, release-status.sh):
 *     gh release (view, download, list) с явным --repo, контракт читается; статусы stand/* пишутся
 *     в репозиторий кода, не в переменную контракта; проверка sha256 ассета в deploy.sh сохранена;
 *   --scope stand — scripts/router/session.mjs: единственный вызов gh release list — в помощнике
 *     latestReleaseTag, с явным --repo из контракта и отказом при пустом или неверном источнике;
 *     оба потребителя — Session.resolveVersion (version: latest) и ветка smoke|delta (release:
 *     latest) — вызывают этот помощник ровно раз, без собственного gh release list и без запасного
 *     источника в обработке ошибки.
 *
 * Без --scope выполняются все проверки без исключений. Литерал имени источника выпусков вне
 * контракта допускается только там, где файл прочитать нельзя: scripts/setup.sh (скачивается одним
 * файлом) и README.md (адреса загрузки для пользователя).
 *
 * Usage: node scripts/check-release-source.js [--scope client|publish|notes|stand-download|stand]
 */
'use strict';

const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const CONTRACT = 'internal/releasefeed/repository';
const SOURCE_GO = 'internal/releasefeed/source.go';
const UPDATE_GO = 'internal/handlers/update.go';
const UPDATE_TEST = 'internal/handlers/router_update_test.go';
const SETUP_SH = 'scripts/setup.sh';
const SESSION = 'scripts/router/session.mjs';
const DEPLOY = 'scripts/router/deploy.sh';
const RUN = 'scripts/router/run.sh';
const RELEASE_STATUS = 'scripts/router/release-status.sh';

const SCOPES = {
  client: [UPDATE_GO, SETUP_SH, UPDATE_TEST],
  publish: ['.github/scripts/auto_release.sh', '.github/workflows/build.yml', '.github/workflows/auto-release.yml'],
  notes: ['.github/scripts/gen_notes.py', '.github/workflows/release-notes.yml', '.github/scripts/prune_dev_releases.sh'],
  'stand-download': [DEPLOY, RUN, RELEASE_STATUS],
  stand: [SESSION]
};
// Файлы, где литерал источника допустим: прочитать контракт они не могут.
const LITERAL_ALLOWED = new Set([SETUP_SH, 'README.md']);

const REPO_RE = /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\/[A-Za-z0-9._-]+$/;
const PUBLISHER = '.github/scripts/auto_release.sh';
const BUILD_WORKFLOW = '.github/workflows/build.yml';
const NOTES_GENERATOR = '.github/scripts/gen_notes.py';

const problems = [];
const fail = (where, msg) => problems.push(`${where}: ${msg}`);

function rel(p) {
  return path.join(root, p);
}
function read(p) {
  try {
    return fs.readFileSync(rel(p), 'utf8');
  } catch {
    return null;
  }
}
function need(p) {
  const text = read(p);
  if (text === null) fail(p, 'файл не найден');
  return text;
}
function escapeRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
function lineOf(text, index) {
  return text.slice(0, index).split('\n').length;
}

// --- аргументы ---------------------------------------------------------------
const args = process.argv.slice(2);
let scope = null;
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--scope') scope = args[++i];
  else if (args[i].startsWith('--scope=')) scope = args[i].slice('--scope='.length);
  else {
    console.error(`check-release-source: неизвестный аргумент ${args[i]}`);
    process.exit(2);
  }
}
if (scope !== null && !Object.hasOwn(SCOPES, scope)) {
  console.error(`check-release-source: неизвестный scope «${scope}» (есть: ${Object.keys(SCOPES).join(', ')})`);
  process.exit(2);
}

// --- ядро: контракт и пакет --------------------------------------------------
function sourceRepository() {
  const gomod = need('go.mod');
  const m = gomod && gomod.match(/^module\s+github\.com\/([^\s/]+\/[^\s/]+)\s*$/m);
  if (!m) {
    fail('go.mod', 'модуль не вида github.com/<owner>/<name> — репозиторий кода не определён');
    return null;
  }
  return m[1];
}

function readContract(source) {
  const raw = need(CONTRACT);
  if (raw === null) return null;
  const body = raw.replace(/\n$/, '');
  if (body.trim() === '') {
    fail(CONTRACT, 'контракт пуст');
    return null;
  }
  if (body.includes('\n') || body !== body.trim()) {
    fail(CONTRACT, 'контракт — ровно одна строка owner/name без пробелов');
    return null;
  }
  if (!REPO_RE.test(body)) {
    fail(CONTRACT, `«${body}» — не имя репозитория owner/name`);
    return null;
  }
  if (source && body.toLowerCase() === source.toLowerCase()) {
    fail(CONTRACT, `источник выпусков совпадает с репозиторием кода ${source}`);
    return null;
  }
  return body;
}

function checkPackage(source) {
  const text = need(SOURCE_GO);
  if (text === null) return;
  if (!/^\/\/go:embed repository\s*$/m.test(text)) fail(SOURCE_GO, 'нет //go:embed repository');
  for (const fn of ['Repository', 'APIReleasesURL', 'DownloadURL', 'Check']) {
    if (!new RegExp(`^func ${fn}\\(`, 'm').test(text)) fail(SOURCE_GO, `не объявлена func ${fn}`);
  }
  const m = text.match(/^const SourceRepository = "([^"]+)"$/m);
  if (!m) fail(SOURCE_GO, 'нет const SourceRepository');
  else if (source && m[1] !== source) fail(SOURCE_GO, `SourceRepository «${m[1]}» не совпадает с модулем go.mod ${source}`);
  if (/api\.github\.com\/repos\/"\s*\+\s*SourceRepository|github\.com\/"\s*\+\s*SourceRepository/.test(text)) {
    fail(SOURCE_GO, 'адрес релизов строится от SourceRepository');
  }
}

function walk(dir, out) {
  for (const e of fs.readdirSync(rel(dir), { withFileTypes: true })) {
    const p = path.posix.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === 'node_modules' || e.name === 'testdata' || e.name.startsWith('.')) continue;
      walk(p, out);
    } else if (e.name.endsWith('.go')) out.push(p);
  }
}

// Адрес релизов строкой в Go вне releasefeed — обход контракта (и запасной путь к коду).
function checkGoLiterals(source, contract) {
  const files = [];
  for (const d of ['cmd', 'internal']) if (fs.existsSync(rel(d))) walk(d, files);
  const repos = [source, contract].filter(Boolean).map(escapeRe);
  if (!repos.length) return;
  const re = new RegExp(`(?:api\\.github\\.com/repos/|github\\.com/)(?:${repos.join('|')})/releases`, 'g');
  for (const f of files) {
    if (f.startsWith('internal/releasefeed/')) continue;
    const text = read(f);
    for (const m of text.matchAll(re)) fail(`${f}:${lineOf(text, m.index)}`, `адрес релизов строкой (${m[0]}) — только через releasefeed`);
  }
}

// --- client -----------------------------------------------------------------
function checkClient(source, contract) {
  const upd = need(UPDATE_GO);
  if (upd !== null) {
    if (!upd.includes('"github.com/' + (source || '') + '/internal/releasefeed"')) fail(UPDATE_GO, 'не импортирует internal/releasefeed');
    for (const old of ['githubAPIReleases', 'githubDownloadURL']) {
      if (upd.includes(old)) fail(UPDATE_GO, `осталась прежняя константа ${old}`);
    }
    if (/"https:\/\/(?:api\.)?github\.com\//.test(upd)) fail(UPDATE_GO, 'адрес GitHub строкой — только через releasefeed');
    const fnBody = name => {
      const m = upd.match(new RegExp(`^func ${name}\\([^]*?^}`, 'm'));
      return m ? m[0] : null;
    };
    const fr = fnBody('fetchReleases');
    if (!fr || !fr.includes('releasefeed.APIReleasesURL()') || !fr.includes('releasefeed.Check()')) {
      fail(UPDATE_GO, 'fetchReleases: список релизов не из releasefeed.APIReleasesURL() с проверкой releasefeed.Check()');
    }
    const fc = fnBody('fetchChangelog');
    if (!fc || !fc.includes('releasefeed.APIReleasesURL()') || !fc.includes('releasefeed.Check()')) {
      fail(UPDATE_GO, 'fetchChangelog: тело релиза не из releasefeed.APIReleasesURL() с проверкой releasefeed.Check()');
    }
    const pu = upd.match(/^func \(a \*API\) performUpdate\([^]*?^}/m);
    if (!pu || !pu[0].includes('releasefeed.DownloadURL(') || !pu[0].includes('releasefeed.Check()')) {
      fail(UPDATE_GO, 'performUpdate: ассет не из releasefeed.DownloadURL с проверкой releasefeed.Check()');
    }
    const uc = upd.match(/^func \(a \*API\) UpdateCheck\([^]*?^}/m);
    if (!uc || !uc[0].includes('releasefeed.DownloadURL(') || !uc[0].includes('releasefeed.Repository()')) {
      fail(UPDATE_GO, 'UpdateCheck: download_url не из releasefeed.DownloadURL или нет release_repository');
    }
  }

  // Ручной установщик скачивается одним файлом и контракт прочитать не может: REPO — литерал,
  // равный контракту; каждый адрес релизов (API, загрузка, .sha256) строится от ${REPO}.
  const sh = need(SETUP_SH);
  if (sh !== null) {
    const repoLines = [...sh.matchAll(/^REPO=(.*)$/gm)];
    const m = repoLines.length === 1 && repoLines[0][1].match(/^"([^"]*)"\s*$/);
    if (!m) fail(SETUP_SH, 'нужна ровно одна строка REPO="owner/name"');
    else if (source && m[1].toLowerCase() === source.toLowerCase()) fail(SETUP_SH, `REPO указывает на репозиторий кода ${source}`);
    else if (contract && m[1] !== contract) fail(SETUP_SH, `REPO="${m[1]}" не совпадает с контрактом ${contract}`);
    sh.split('\n').forEach((line, i) => {
      if (/^\s*#/.test(line)) return;
      for (const u of line.matchAll(/(?:api\.github\.com\/repos\/|github\.com\/)([^\s"']*?)\/releases/g)) {
        if (u[1] !== '${REPO}') fail(`${SETUP_SH}:${i + 1}`, `адрес релизов не от \${REPO}: ${u[0]}`);
      }
    });
    if (!/sha256sum/.test(sh) || !/gunzip/.test(sh)) fail(SETUP_SH, 'пропала проверка sha256 или распаковка .gz');
  }

  const t = need(UPDATE_TEST);
  if (t !== null && !t.includes('releasefeed.APIReleasesURL()')) fail(UPDATE_TEST, 'релизы читаются не через releasefeed.APIReleasesURL()');
}

// --- общие правила gh release для shell, YAML и Python ----------------------
// Логические строки: продолжения через обратную косую черту склеиваются.
function logicalLines(text) {
  const out = [];
  const lines = text.split('\n');
  for (let i = 0; i < lines.length; i++) {
    const start = i + 1;
    let line = lines[i];
    while (/\\\s*$/.test(line) && i + 1 < lines.length) {
      line = line.replace(/\\\s*$/, ' ') + lines[++i];
    }
    out.push({ n: start, line });
  }
  return out;
}

function isComment(line) {
  return /^\s*#/.test(line);
}

// Переменные, значение которых прочитано из контракта (shell VAR=$(…repository…), YAML env, Python).
function contractVars(texts) {
  const vars = new Set();
  const re = new RegExp(`([A-Za-z_][A-Za-z0-9_]*)\\s*[:=]+[^\\n]*${escapeRe(CONTRACT)}`, 'g');
  const literalPath = new RegExp(`^[A-Za-z_][A-Za-z0-9_]*\\s*[:=]+\\s*["']?${escapeRe(CONTRACT)}$`);
  for (const text of texts) {
    for (const m of text.matchAll(re)) {
      if (!literalPath.test(m[0])) vars.add(m[1]);
    }
  }
  return vars;
}

function repoValues(line) {
  return [...line.matchAll(/(?:^|\s)(?:--repo|-R)(?:\s+|=)("[^"]*"|'[^']*'|\$\{\{[^}]*\}\}|\S+)/g)]
    .map(m => m[1].replace(/^["']|["']$/g, ''));
}

function refersToVar(value, vars) {
  for (const v of vars) {
    if (value === `$${v}` || value === `\${${v}}` || value === `\${{ env.${v} }}`) return true;
  }
  return false;
}

function checkGhRelease(file, text, ctx) {
  const { vars, pipeline, standCommands } = ctx;
  const standSeen = new Set();
  const baseline = file === PUBLISHER && text.match(/^collect_baseline\(\) \{[^]*?^}/m);
  const baselineStart = baseline ? lineOf(text, baseline.index) : 0;
  const baselineEnd = baseline ? lineOf(text, baseline.index + baseline[0].length) : 0;
  let legacyReads = 0;
  let creates = 0;
  for (const { n, line } of logicalLines(text)) {
    if (isComment(line)) continue;
    const where = `${file}:${n}`;
    if (/(?:^|[\s;&|(])(?:export\s+)?GH_REPO\s*[:=]/.test(line) || /^\s*GH_REPO\s*:/.test(line)) {
      fail(where, 'глобальный GH_REPO — репозиторий задаётся явным --repo у каждой команды');
    }
    // Убираем строки сообщений, сохраняя позиции команд в исходной строке.
    const code = line.replace(/"(?:[^"\\]|\\.)*"|'[^']*'/g, q =>
      q.startsWith('"') && q.includes('$(') ? q : ' '.repeat(q.length));
    for (const m of code.matchAll(/\bgh\s+release\s+([a-z-]+)/g)) {
      const sub = m[1];
      const end = code.slice(m.index).search(/[;&|)]/);
      const command = line.slice(m.index, end === -1 ? undefined : m.index + end);
      const values = repoValues(command);
      if (values.length !== 1) {
        fail(where, `gh release ${sub} требует ровно один явный --repo`);
        continue;
      }
      const [value] = values;
      if (standCommands) {
        if (!Object.hasOwn(standCommands, sub) || !refersToVar(value, [standCommands[sub]]) || standSeen.has(sub)) {
          fail(where, `gh release ${sub}: неверная или повторная команда источника стенда (--repo ${value})`);
        }
        standSeen.add(sub);
      }
      if (!pipeline) continue;
      const usesReleaseToken = /\bGH_TOKEN="\$\{?RELEASE_TOKEN\}?"\s*$/.test(line.slice(0, m.index));
      if (sub === 'list' && n >= baselineStart && n <= baselineEnd &&
          refersToVar(value, ['SOURCE_REPO']) && !usesReleaseToken) {
        legacyReads++;
        continue;
      }
      if (!refersToVar(value, vars)) {
        fail(where, `gh release ${sub} --repo ${value}: не переменная, прочитанная из ${CONTRACT}`);
      }
      if (!usesReleaseToken) fail(where, `gh release ${sub}: требуется отдельный RELEASE_TOKEN`);
      if (sub === 'create' || sub === 'new') {
        creates++;
        if (file !== BUILD_WORKFLOW || !/\s--verify-tag(?:\s|$)/.test(command) || /\s--target[=\s]/.test(command)) {
          fail(where, 'создание релиза допустимо в build.yml только с --verify-tag');
        }
      }
    }
  }
  for (const sub of Object.keys(standCommands || {})) {
    if (!standSeen.has(sub)) fail(file, `не найдена команда gh release ${sub}`);
  }
  if (file === PUBLISHER && legacyReads !== 1) fail(file, 'нужно одно чтение совместимой истории в collect_baseline');
  if (file === BUILD_WORKFLOW) {
    if (creates !== 1) fail(file, 'нужна одна команда создания релиза');
    if (!/require_source_tag "\$VERSION" "\$sha"[^]*require_release_access[^]*collect_baseline[^]*mirror_release_tag "\$VERSION" "\$sha"[^]*gh release create/.test(text)) {
      fail(file, 'до создания релиза нужно проверить доступ и сверить скопированный исходный тег');
    }
  }

  // Python: subprocess-список ["gh", "release", "<sub>", …]
  for (const m of text.matchAll(/\[\s*["']gh["']\s*,\s*["']release["']\s*,\s*["']([a-z-]+)["']([^\]]*)\]/g)) {
    if (pipeline || !/["'](?:--repo|-R)["']/.test(m[2])) {
      fail(`${file}:${lineOf(text, m.index)}`, `gh release ${m[1]}: команды конвейера должны проходить проверку явной цели и токена`);
    }
  }
}

function checkContractRead(files, texts, vars, where) {
  if (!vars.size) {
    fail(where, `ни один файл (${files.join(', ')}) не читает ${CONTRACT}`);
    return;
  }
  const all = texts.join('\n');
  for (const v of vars) {
    const guard = new RegExp(`-z\\s+"?\\$\\{?${v}\\}?"?|\\$\\{${v}:\\?|not\\s+${v}\\b|!\\s*${v}\\b|${v}\\s*===?\\s*['"]{2}`);
    if (!guard.test(all)) fail(where, `${v}: нет отказа при пустом источнике выпусков`);
  }
}

function checkLiteral(files, contract) {
  if (!contract) return;
  for (const f of files) {
    if (LITERAL_ALLOWED.has(f)) continue;
    const text = read(f);
    if (text === null) continue;
    let i = text.indexOf(contract);
    while (i !== -1) {
      fail(`${f}:${lineOf(text, i)}`, `имя источника выпусков строкой — читать ${CONTRACT}`);
      i = text.indexOf(contract, i + 1);
    }
  }
}

function checkPipelineScope(name, contract) {
  const files = SCOPES[name];
  const texts = files.map(need).filter(t => t !== null);
  if (texts.length !== files.length) return;
  const inputs = name === 'notes' ? [...texts, need(PUBLISHER) || ''] : texts;
  const vars = contractVars(inputs);
  checkContractRead(files, inputs, vars, `scope ${name}`);
  const pipeline = name === 'publish' || name === 'notes';
  files.forEach((f, i) => checkGhRelease(f, texts[i], { vars, pipeline }));
  checkLiteral(files, contract);

  if (name === 'notes') {
    const py = NOTES_GENERATOR;
    const text = texts[files.indexOf(py)];
    text.split('\n').forEach((line, i) => {
      for (const url of line.matchAll(/(?:https:\/\/(?:api\.github\.com\/repos\/|github\.com\/))?([^\s"'()]+)\/releases\b/g)) {
        if (url[1] !== '{release_repo}' || !vars.has('release_repo')) {
          fail(`${py}:${i + 1}`, 'адрес релизов в заметке не из контракта');
        }
      }
      if (line.includes('raw.githubusercontent.com/') && !line.includes('raw.githubusercontent.com/{source_repo}/{version}/scripts/setup.sh')) {
        fail(`${py}:${i + 1}`, 'ссылка на установщик должна вести в исходный тег');
      }
    });
    for (const expected of ['{release_repo}/releases/tag/{version}', '{release_repo}/releases/download/{version}/', '/opt/etc/xcp/legacy/']) {
      if (!text.includes(expected)) fail(py, `в инструкции перехода отсутствует ${expected}`);
    }
  }

}

function checkStandDownloads(contract) {
  const files = SCOPES['stand-download'];
  const texts = files.map(need);
  if (texts.includes(null)) return;
  const [deploy, run, status] = texts.map(text => logicalLines(text)
    .filter(({ line }) => !isComment(line)).map(({ line }) => line).join('\n'));
  checkGhRelease(DEPLOY, deploy, { standCommands: { view: 'ASSET_REPO', download: 'ASSET_REPO' } });
  checkGhRelease(RUN, run, { standCommands: { list: 'RELEASE_REPO' } });
  checkGhRelease(RELEASE_STATUS, status, { standCommands: {} });
  checkLiteral(files, contract);

  for (const [i, text] of [deploy, run, status].entries()) {
    if (!/\bSOURCE_REPO=\$\(sed [^\n]+go\.mod"?\)/.test(text)) {
      fail(files[i], 'репозиторий кода должен определяться из go.mod');
    }
  }
  if (
    !deploy.includes(`RELEASE_CONTRACT=${CONTRACT}`) ||
    !deploy.includes('RELEASE_COMMIT=$(git rev-parse --verify "refs/tags/$RELEASE_TAG^{commit}")') ||
    !/RELEASE_ENTRY=\$\(git ls-tree "\$RELEASE_COMMIT" -- "\$RELEASE_CONTRACT"\) \|\| exit 1\s+if \[ -z "\$RELEASE_ENTRY" \]; then\s+ASSET_REPO=\$SOURCE_REPO\s+else\s+ASSET_REPO=\$\(git show "\$RELEASE_COMMIT:\$RELEASE_CONTRACT"/.test(deploy)
  ) {
    fail(DEPLOY, 'источник ассетов должен определяться по контракту коммита тега, репозиторий кода допустим при его отсутствии');
  }
  if (
    !/gh release download [^\n]+-p "\$asset" -p "\$asset\.sha256"/.test(deploy) ||
    !/if ! \(cd "\$REL_DIR" && sha256sum -c "\$asset\.sha256"\); then[^]*?\bexit 1\b[^]*?\bfi\b/.test(deploy)
  ) {
    fail(DEPLOY, 'ассет и .sha256 должны скачиваться вместе, ошибка sha256sum -c должна прерывать установку');
  }

  if (
    !run.includes('if [ "${RELEASE:-}" = latest ]; then') ||
    !run.includes(`' ${CONTRACT}) || rt_latest_die`) ||
    !/RELEASE_REPO=\$\(LC_ALL=C awk -v source="\$SOURCE_REPO"/.test(run) ||
    !/RELEASE=\$\(gh release list --repo "\$RELEASE_REPO" [^\n]+\) \|\|\s+rt_latest_die/.test(run) ||
    !run.includes(`TAG_REPO=$(git show "refs/tags/$RELEASE^{commit}:${CONTRACT}"`) ||
    !run.includes('[ "$TAG_REPO" = "$RELEASE_REPO" ] || rt_latest_die')
  ) {
    fail(RUN, 'RELEASE=latest должен читать отдельный источник и сверять контракт исходного тега');
  }

  const statusCalls = status.split('\n').filter(line => /\bgh\s+api\s/.test(line));
  if (
    statusCalls.length !== 2 ||
    !statusCalls[0].includes('SHA=$(gh api "repos/$SOURCE_REPO/commits/$TAG" --jq .sha') ||
    !statusCalls[1].includes('gh api -X POST "repos/$SOURCE_REPO/statuses/$SHA"') ||
    !/LOCAL_SHA=\$\(git -C "[^"]+" rev-parse --verify "refs\/tags\/\$TAG\^\{commit\}"/.test(status) ||
    !/if \[ "\$SHA" != "\$LOCAL_SHA" \]; then[^]*?\bexit 1\b[^]*?\bfi\b\s+if \[ "\$DRY_RUN"/.test(status)
  ) {
    fail(RELEASE_STATUS, 'нужны сверка исходного тега с локальным SHA и публикация статуса на этот SHA в репозитории кода');
  }
}

// --- stand: session.mjs ------------------------------------------------------
// Тело блока, открытого первой «{» после позиции from (учёт строк и комментариев грубый, но
// фигурные скобки в строках session.mjs сбалансированы).
function blockAt(text, from) {
  const open = text.indexOf('{', from);
  if (open === -1) return null;
  let depth = 0;
  for (let i = open; i < text.length; i++) {
    const c = text[i];
    if (c === '{') depth++;
    else if (c === '}' && --depth === 0) return { start: open, end: i + 1, body: text.slice(open, i + 1) };
  }
  return null;
}

// Участок вызова: блок после якоря anchorRe (глобальное выражение), для которого pick истинно;
// такой блок должен быть ровно один.
function checkCaller(text, label, anchorRe, pick) {
  const blocks = [];
  for (const m of text.matchAll(anchorRe)) {
    const b = blockAt(text, m.index + m[0].length - 1);
    if (b && pick(b.body)) blocks.push(b);
  }
  if (blocks.length !== 1) {
    fail(SESSION, `${label}: участков вызова ${blocks.length}, нужен ровно один`);
    return;
  }
  const block = blocks[0];
  const where = `${SESSION}:${lineOf(text, block.start)}`;
  const calls = [...block.body.matchAll(/\blatestReleaseTag\(\)/g)];
  if (calls.length !== 1) fail(where, `${label}: latestReleaseTag() вызван ${calls.length} раз, нужен ровно один`);
  if (/['"]release['"]\s*,\s*['"]list['"]|exec\w*\(\s*[`'"]gh\s+release/.test(block.body)) {
    fail(where, `${label}: собственный gh release list в обход latestReleaseTag`);
  }
  const tryAt = block.body.search(/\btry\s*\{\s*tag\s*=\s*latestReleaseTag\(\)/);
  if (tryAt === -1) {
    fail(where, `${label}: вызов latestReleaseTag() не в виде try { tag = latestReleaseTag() }`);
    return;
  }
  const tryBlock = blockAt(block.body, tryAt);
  const catchM = tryBlock && block.body.slice(tryBlock.end).match(/^\s*catch\s*(?:\([^)]*\))?\s*/);
  const catchBlock = catchM && blockAt(block.body, tryBlock.end + catchM[0].length - 1);
  if (!catchBlock) {
    fail(where, `${label}: нет обработки ошибки latestReleaseTag()`);
    return;
  }
  if (/\btag\s*=(?!=)/.test(catchBlock.body) || /\b(?:execFileSync|execSync|spawnSync)\b/.test(catchBlock.body)) {
    fail(where, `${label}: запасной источник в обработке ошибки latestReleaseTag()`);
  }
  if (!/\berr\s*=(?!=)/.test(catchBlock.body)) fail(where, `${label}: ошибка latestReleaseTag() не превращается в отказ (err = …)`);
}

function checkStand(contract) {
  const text = need(SESSION);
  if (text === null) return;
  const fnM = text.match(/^function latestReleaseTag\(\)\s*/m);
  if (!fnM) {
    fail(SESSION, 'нет помощника function latestReleaseTag()');
    return;
  }
  const fn = blockAt(text, fnM.index + fnM[0].length - 1);
  const where = `${SESSION}:${lineOf(text, fnM.index)}`;
  if (!/['"]release['"]\s*,\s*['"]list['"]/.test(fn.body)) fail(where, 'latestReleaseTag: не gh release list');
  if (!/['"]--repo['"]/.test(fn.body)) fail(where, 'latestReleaseTag: gh release list без явного --repo');
  if (!text.includes(CONTRACT)) fail(SESSION, `источник выпусков не читается из ${CONTRACT}`);
  if (!/\bthrow\b/.test(fn.body) && !/\bthrow\b/.test(text.slice(text.indexOf(CONTRACT) - 2000, text.indexOf(CONTRACT) + 2000))) {
    fail(where, 'latestReleaseTag: нет отказа при пустом или неверном источнике');
  }
  // gh release list только внутри помощника
  const listRe = /['"]release['"]\s*,\s*['"]list['"]|exec\w*\(\s*[`'"]gh\s+release\s+list/g;
  for (const m of text.matchAll(listRe)) {
    if (m.index < fn.start || m.index >= fn.end) fail(`${SESSION}:${lineOf(text, m.index)}`, 'gh release list вне latestReleaseTag');
  }
  checkCaller(text, 'Session.resolveVersion (version: latest)', /^\s*resolveVersion\(p\)\s*\{/gm, () => true);
  checkCaller(
    text,
    'ветка smoke|delta (release: latest)',
    /if \(p\.suite === 'smoke' \|\| p\.suite === 'delta'\)\s*\{/g,
    body => /\btag\s*=\s*p\.release\b/.test(body)
  );
  checkLiteral([SESSION], contract);
}

// --- запуск -----------------------------------------------------------------
const source = sourceRepository();
const contract = readContract(source);
checkPackage(source);
checkGoLiterals(source, contract);

const run = scope ? [scope] : Object.keys(SCOPES);
for (const s of run) {
  if (s === 'client') checkClient(source, contract);
  else if (s === 'stand') checkStand(contract);
  else if (s === 'stand-download') checkStandDownloads(contract);
  else checkPipelineScope(s, contract);
}

const label = scope ? `scope ${scope}` : 'полная проверка';
if (problems.length) {
  console.error(`check-release-source (${label}): нарушений: ${problems.length}`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(`check-release-source (${label}): источник выпусков ${contract} отделён от ${source}`);
