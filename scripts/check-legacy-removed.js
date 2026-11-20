#!/usr/bin/env node
const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const contract = require('./legacy-removal-contract.json');
const { removedRoutes, removedMatch, removedRegistrations } = require('./lib/removed-routes');

const root = path.resolve(__dirname, '..');
const retired = new Set(contract.retired_files);
const neverInspect = new Set(['scripts/check-legacy-removed.js', 'scripts/legacy-removal-contract.json']);
const archiveFiles = [
  'internal/services/legacyarchive/archive.go',
  'internal/services/legacyarchive/marker.go',
  'internal/services/legacyarchive/router_archive_test.go',
  'internal/services/legacyarchive/scan.go',
  'internal/services/legacyarchive/clean.go',
  'internal/services/legacyarchive/access.go',
  'internal/handlers/router_legacy_test.go'
];
const violations = [];
const inspected = new Map();
let checked = 0;
let remaining = 0;
let skipped = 0;
// --ci: путей, игнорируемых git (локальная справка .claude/), в checkout CI нет по построению;
// их проверка идёт только локально. Отслеживаемый или отсутствующий без игнора путь не пропускается.
let ciMode = false;

function ignoredByGit(file) {
  try {
    execFileSync('git', ['check-ignore', '-q', '--', file], { cwd: root, stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
}

function skipInCI(file) {
  if (!ciMode || fs.existsSync(path.join(root, file)) || !ignoredByGit(file)) return false;
  skipped++;
  return true;
}

// Заменяем комментарии пробелами, сохраняя строки и границы токенов.
// Строки нужны предикатам путей, отдельно от проверки структуры кода.
const tokens = /<!--[^]*?-->|\/\*[^]*?\*\/|\/\/[^\r\n]*|"(?:\\[^]|[^"\\\r\n])*"|'(?:\\[^]|[^'\\\r\n])*'|`(?:\\[^]|[^`\\])*`/g;
const blank = (text) => text.replace(/[^\r\n]/g, ' ');
const isComment = (token) => token.startsWith('/') || token.startsWith('<!--');

function inspect(file) {
  if (inspected.has(file)) return inspected.get(file);
  try {
    if (neverInspect.has(file) || file.startsWith('.planning/')) {
      throw new Error('запрещённый предмет проверки');
    }
    const absolute = path.join(root, file);
    // Один дескриптор: тип файла и содержимое берутся без промежутка между проверкой и чтением.
    const fd = fs.openSync(absolute, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0));
    let raw;
    try {
      if (!fs.fstatSync(fd).isFile()) throw new Error('ожидался обычный файл');
      raw = fs.readFileSync(fd, 'utf8');
    } finally {
      fs.closeSync(fd);
    }
    const code = raw.replace(tokens, (token) => (isComment(token) ? blank(token) : token));
    const unquoted = raw.replace(tokens, blank);
    const subject = { raw, code, unquoted };
    inspected.set(file, subject);
    return subject;
  } catch (err) {
    violations.push(`${file}: файл недоступен для проверки (${err.code || err.message})`);
    inspected.set(file, null);
    return null;
  }
}

function checkPattern(file, text, pattern, required) {
  if (new RegExp(pattern, 'm').test(text) !== required) {
    violations.push(`${file}: ${required ? 'нет обязательного' : 'найден запрещённый'} шаблон ${pattern}`);
  }
}

function requireHeader(file, raw, prefix) {
  const lines = raw.split(/\r?\n/, 5);
  const found = lines.some((line) =>
    prefix.endsWith(':') ? line.startsWith(prefix + ' ') && line.length > prefix.length + 1 : line === prefix
  );
  if (!found) violations.push(`${file}: нет обязательной строки шапки ${prefix}`);
}

function declarations(unquoted) {
  return new Set(Array.from(unquoted.matchAll(/^\s*func\s+(TestRouter\w+)\s*\(/gm), (match) => match[1]));
}

function checkRemoved(file) {
  checked++;
  try {
    fs.lstatSync(path.join(root, file));
    remaining++;
    violations.push(`${file}: снятый путь ещё существует`);
  } catch (err) {
    if (err.code !== 'ENOENT') violations.push(`${file}: отсутствие пути не подтверждено (${err.code})`);
  }
}

function checkTests(file, unquoted, scope) {
  const names = declarations(unquoted);
  const removedTests = new Set(scope.retired_tests);
  for (const name of scope.keep_test_declarations[file] || []) {
    if (!removedTests.has(name) && !names.has(name)) {
      violations.push(`${file}: пропал сценарий ${name}`);
    }
  }
  if (scope.retired_tests_must_be_absent) {
    for (const name of removedTests) {
      if (names.has(name)) violations.push(`${file}: снятая декларация ${name} осталась`);
    }
  }
}

function functionBody(text, header) {
  const match = header.exec(text);
  if (!match) return null;
  const start = match.index + match[0].length;
  let depth = 1;
  for (let i = start; i < text.length; i++) {
    if (text[i] === '{') depth++;
    if (text[i] === '}' && --depth === 0) return text.slice(start, i);
  }
  return null;
}

function checkAreas(file, raw) {
  let data;
  try {
    data = JSON.parse(raw);
  } catch {
    violations.push(`${file}: некорректный JSON областей`);
    return;
  }
  if (!data?.areas || Array.isArray(data.areas) || typeof data.areas !== 'object') {
    violations.push(`${file}: отсутствует объект areas`);
    return;
  }
  // Снятые сценарии (retired_tests области go-tests) не остаются ни в tests, ни в канарейках.
  const retiredTests = new Set(contract.scopes['go-tests']?.retired_tests || []);
  const pending = [data];
  while (pending.length > 0) {
    const entry = pending.pop();
    if (typeof entry === 'string') {
      if (retired.has(entry) || retired.has('frontend/' + entry)) {
        violations.push(`${file}: карта содержит снятый путь ${entry}`);
      }
      const test = /^(?:go:)?(TestRouter\w+)$/.exec(entry);
      if (test && retiredTests.has(test[1])) {
        violations.push(`${file}: карта содержит снятый сценарий ${test[1]}`);
      }
    } else if (entry && typeof entry === 'object') {
      pending.push(...Object.values(entry));
    }
  }
}

function checkSpecial(file, subject, scope) {
  switch (scope.special) {
    case 'exactly-one-dashboard-legacyRedirectHash-call':
      if (file === 'frontend/src/Dashboard.svelte') {
        const code = subject.unquoted.replace(/\bfunction\s+legacyRedirectHash\s*\(/g, blank);
        if ([...code.matchAll(/\blegacyRedirectHash\s*\(/g)].length !== 1) {
          violations.push(`${file}: ожидается ровно один вызов legacyRedirectHash`);
        }
      }
      break;
    case 'Start-block-requires-startOnce-bootstrap-SourceApplied-without-Draft':
      if (file === 'internal/services/configlayer/layer.go') {
        const body = functionBody(subject.unquoted, /\bfunc\s*\(\s*l\s+\*Layer\s*\)\s*Start\s*\(\s*\)\s*\{/);
        if (!body) {
          violations.push(`${file}: не найдено тело Layer.Start`);
        } else {
          for (const pattern of ['\\bstartOnce\\s*\\.\\s*Do\\s*\\(', '\\bbootstrap\\s*\\(', '\\bSourceApplied\\b']) {
            checkPattern(file, body, pattern, true);
          }
          checkPattern(file, body, '\\b(?:Draft|SourceDraft)\\b', false);
        }
      }
      break;
    case 'keep-test-declarations-except-explicit-retired-tests':
      checkTests(file, subject.unquoted, scope);
      break;
    case 'areas-json-has-no-retired-files':
      if (file === 'scripts/router/areas.json') checkAreas(file, subject.raw);
      break;
    case 'protected-legacy-routes-and-removed-registry':
    case undefined:
      // Дополнительные пути читаются по inspection_rules после выборки.
      break;
    default:
      violations.push(`неизвестный special ${scope.special}`);
  }
}

function checkScope(scope, files) {
  for (const file of files) {
    if (!scope.files.includes(file)) {
      violations.push(`${file}: путь вне выбранной области`);
      continue;
    }
    if (retired.has(file)) {
      checkRemoved(file);
      continue;
    }
    if (skipInCI(file)) continue;
    checked++;
    if (neverInspect.has(file)) {
      // Собственный страж может входить в перечень поставляемых файлов,
      // его содержимое не служит доказательством удаления старого кода.
      if (!fs.existsSync(path.join(root, file))) violations.push(`${file}: отсутствует`);
      continue;
    }
    const subject = inspect(file);
    if (!subject) continue;
    if (!scope.exclude_forbid?.includes(file)) {
      for (const pattern of scope.forbid) checkPattern(file, subject.code, pattern, false);
    }
    for (const rule of scope.required) {
      if (rule.file === file) checkPattern(file, subject.code, rule.pattern, true);
    }
    for (const prefix of scope.raw_required) requireHeader(file, subject.raw, prefix);
    checkSpecial(file, subject, scope);
  }
  for (const rule of scope.inspection_rules || []) {
    const subject = inspect(rule.file);
    if (!subject) continue;
    if (rule.pattern) checkPattern(rule.file, subject.code, rule.pattern, true);
    if (rule.forbid) checkPattern(rule.file, subject.code, rule.forbid, false);
    if (scope.special === 'protected-legacy-routes-and-removed-registry' && rule.forbid) {
      checkPattern(rule.file, subject.code, '\\b(?:Handle|HandleFunc)\\s*\\(\\s*"/api/configlayer/legacy(?:/[^"]*)?"', false);
    }
  }
  if (scope.special === 'protected-legacy-routes-and-removed-registry') checkRemovedRoutes();
}

// Снятые маршруты (removed_routes) не регистрируются ни в main.go, ни в сервере и не держат
// записей гейта ядра: такой путь отвечает обычным 404 общего обработчика неизвестных /api/….
function checkRemovedRoutes() {
  let list;
  try {
    list = removedRoutes();
  } catch (err) {
    violations.push(err.message);
    return;
  }
  for (const file of ['cmd/xcp/main.go', 'internal/server/server.go']) {
    const subject = inspect(file);
    if (!subject) continue;
    checked++;
    violations.push(...removedRegistrations(file, subject.raw, list));
  }
  const gate = 'internal/handlers/kernel_gate.go';
  const subject = inspect(gate);
  if (!subject) return;
  checked++;
  for (const m of subject.code.matchAll(/"(\/api\/[^"]*)"/g)) {
    const hit = removedMatch(m[1].replace(/\/$/, ''), list);
    if (hit) violations.push(`${gate}: запись гейта ${m[1]} относится к снятому маршруту ${hit}`);
  }
}

function checkArchive(files) {
  for (const file of files) {
    if (!archiveFiles.includes(file)) {
      violations.push(`${file}: путь вне области archive`);
      continue;
    }
    checked++;
    const subject = inspect(file);
    if (!subject) continue;
    const { code, raw, unquoted } = subject;
    switch (archiveFiles.indexOf(file)) {
      case 0:
        for (const pattern of [
          '\\bfunc Run\\s*\\(',
          '\\bValidateUnder\\s*\\(',
          '\\bos\\.OpenRoot\\s*\\(',
          'sourceNames\\s*=\\s*\\[\\]string\\s*\\{\\s*"subscriptions"\\s*,\\s*"subscriptions\\.json"\\s*,\\s*"cache"\\s*,\\s*"user_rules\\.json"\\s*,?\\s*\\}'
        ]) checkPattern(file, code, pattern, true);
        checkPattern(file, code, '"(?:xcp_hwid\\.txt|config_layer_state\\.json)"', false);
        break;
      case 1:
        for (const field of ['state', 'archive', 'created_at', 'files', 'url_count', 'kernel_cleanup_done']) {
          checkPattern(file, code, `json:"${field}"`, true);
        }
        for (const state of ['prepared', 'complete']) checkPattern(file, code, '"' + state + '"', true);
        checkPattern(file, code, 'json:"urls?(?:,|")', false);
        checkPattern(file, unquoted, '\\bValidateUnder\\s*\\(', true);
        break;
      case 2:
        requireHeader(file, raw, '//go:build router');
        requireHeader(file, raw, '// router-area:');
        if (!declarations(unquoted).has('TestRouterLegacyArchive')) {
          violations.push(`${file}: отсутствует TestRouterLegacyArchive`);
        }
        break;
      case 3:
        // Сканер: только точные имена и признаки владения, закреплённый корень.
        for (const pattern of [
          '"04_outbounds\\.zz_xcp_default\\.json"',
          '"04_outbounds\\.zz_xcp_selected\\.tail\\.json"',
          '"# --- BEGIN USER RULES ---"',
          '"# --- END USER RULES ---"',
          '`"user_rule"`'
        ]) checkPattern(file, code, pattern, true);
        for (const pattern of ['\\bValidateUnder\\s*\\(', '\\bos\\.OpenRoot\\s*\\(', '\\.Readlink\\s*\\(']) {
          checkPattern(file, unquoted, pattern, true);
        }
        for (const pattern of ['\\bfilepath\\.Glob\\s*\\(', '\\bfilepath\\.Match\\s*\\(', '\\bos\\.(?:Remove|Rename|WriteFile)\\s*\\(']) {
          checkPattern(file, unquoted, pattern, false);
        }
        break;
      case 4:
        // Очистка: копия до правки, проверка обоими валидаторами, правки через os.Root.
        for (const pattern of [
          '\\bNewBackupSet\\s*\\(',
          '\\bValidateXrayTree\\s*\\(',
          '\\bValidateMihomoTree\\s*\\(',
          '\\bappendKernelArchive\\s*\\(',
          '\\.Rename\\s*\\(tmp,'
        ]) checkPattern(file, unquoted, pattern, true);
        for (const pattern of ['\\bos\\.(?:Remove|Rename|Chmod|Chown|Symlink)\\s*\\(', '\\bfilepath\\.Glob\\s*\\(', '\\blog\\.']) {
          checkPattern(file, unquoted, pattern, false);
        }
        break;
      case 5:
        // Доступ к списку адресов: без журнала и без записи вне закреплённого каталога.
        for (const pattern of ['\\bprivateFile\\s*\\(', '\\bremovePrivate\\s*\\(']) {
          checkPattern(file, unquoted, pattern, true);
        }
        for (const pattern of ['\\blog\\.', '\\bfmt\\.', '\\bos\\.(?:Remove|ReadFile|Open)\\s*\\(']) {
          checkPattern(file, unquoted, pattern, false);
        }
        break;
      case 6:
        // HTTP-контракт идёт через настоящую панель, без подмены API.
        requireHeader(file, raw, '//go:build router');
        requireHeader(file, raw, '// router-area:');
        if (!declarations(unquoted).has('TestRouterLegacyAPI')) {
          violations.push(`${file}: отсутствует TestRouterLegacyAPI`);
        }
        checkPattern(file, unquoted, '\\bhttptest\\.', false);
        checkPattern(file, unquoted, '\\broutertest\\.NewPanel\\s*\\(', true);
        break;
    }
  }
  const schema = 'internal/services/configlayer/types.go';
  const state = inspect(schema);
  if (state) checkPattern(schema, state.unquoted, '^\\s*const\\s+SchemaVersion(?:\\s+int64)?\\s*=\\s*1\\s*$', true);
}

function main(argv) {
  const args = argv.filter((arg) => arg !== '--ci');
  ciMode = args.length !== argv.length;
  if (args.length === 0) {
    checkArchive(archiveFiles);
    for (const scope of Object.values(contract.scopes)) checkScope(scope, scope.files);
    for (const file of retired) checkRemoved(file);
    return;
  }
  const [option, name, ...explicit] = args;
  if (option !== '--scope' || !name) {
    violations.push('ожидается --scope <область> <пути>');
    return;
  }
  const files = [...new Set(name === 'archive' && explicit.length === 0 ? archiveFiles : explicit)];
  if (files.length === 0) {
    violations.push(`${name}: требуется непустой список путей`);
    return;
  }
  if (name === 'archive') {
    checkArchive(files);
  } else if (name === 'files-removed' || name === 'retired_files') {
    for (const file of files) {
      if (retired.has(file)) checkRemoved(file);
      else violations.push(`${file}: путь отсутствует в реестре снятых файлов`);
    }
  } else if (Object.hasOwn(contract.scopes, name)) {
    checkScope(contract.scopes[name], files);
  } else {
    violations.push(`${name}: неизвестная область`);
  }
}

main(process.argv.slice(2));
if (checked === 0) violations.push('не проверено ни одного пути');
for (const violation of violations) console.error(`check-legacy-removed: ${violation}`);
console.log(
  `check-legacy-removed: checked=${checked} violations=${violations.length} remaining=${remaining}` +
    (ciMode ? ` skipped_ignored=${skipped}` : '')
);
if (violations.length > 0) process.exitCode = 1;
