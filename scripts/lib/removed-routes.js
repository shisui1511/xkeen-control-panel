/*
 * removed-routes.js — общий список снятых HTTP-маршрутов старого слоя и разбор регистраций Go.
 *
 * Без зависимостей и побочных эффектов при загрузке. Список берётся из
 * scripts/legacy-removal-contract.json (removed_routes): его читают check-legacy-removed.js,
 * check-api-routes.js и check-kernel-routes.js, чтобы снятый маршрут не вернулся ни
 * регистрацией, ни записью гейта ядра, ни вызовом фронтенда. Снятый маршрут отвечает обычным
 * 404 общего обработчика неизвестных /api/… — отдельного обработчика-заглушки у него нет.
 *
 * Совпадение по сегментам: «/api/dat» снимает «/api/dat» и всё под «/api/dat/…», но не «/api/data».
 */
const contract = require('../legacy-removal-contract.json');

function removedRoutes() {
  const list = contract.removed_routes;
  if (!Array.isArray(list) || list.length === 0) {
    throw new Error('legacy-removal-contract.json: пустой или отсутствующий removed_routes');
  }
  for (const entry of list) {
    if (typeof entry !== 'string' || !entry.startsWith('/') || entry.endsWith('/')) {
      throw new Error(`legacy-removal-contract.json: некорректная запись removed_routes ${JSON.stringify(entry)}`);
    }
  }
  return list;
}

// Снятая запись, которой соответствует путь (или null).
function removedMatch(route, list = removedRoutes()) {
  const clean = route.replace(/[?#].*$/, '');
  for (const entry of list) {
    if (clean === entry || clean.startsWith(entry + '/')) return entry;
  }
  return null;
}

// Путь-префикс (маршрут-конкатенация «…/»+имя или template literal, обрезанный на ${…})
// может достать до снятого маршрута, если снятая запись лежит под ним.
function prefixReachesRemoved(prefix, list = removedRoutes()) {
  return list.find((entry) => entry.startsWith(prefix)) || null;
}

// Строковые литералы и комментарии Go: комментарии вырезаются, литералы остаются.
function stripGoComments(src) {
  return src.replace(/\/\*[^]*?\*\/|\/\/[^\n]*|"(?:\\.|[^"\\\n])*"|`[^`]*`/g, (token) =>
    token.startsWith('/') ? token.replace(/[^\n]/g, ' ') : token
  );
}

/*
 * Регистрации маршрутов Go: литералы Handle/HandleProtected/HandlePublic/HandleFunc и
 * конкатенации «"/api/…/"+имя». Конкатенация внутри «for _, имя := range []string{…}»
 * раскрывается в полный список путей; нераскрытая остаётся префиксом (expanded: false).
 */
function goRegistrations(src) {
  const code = stripGoComments(src);
  const loops = [];
  for (const m of code.matchAll(/for\s+_\s*,\s*(\w+)\s*:=\s*range\s+\[\]string\s*\{([^}]*)\}\s*\{/g)) {
    const names = [...m[2].matchAll(/"([^"]*)"/g)].map((x) => x[1]);
    loops.push({ variable: m[1], names, start: m.index + m[0].length });
  }
  const out = [];
  for (const m of code.matchAll(/\b(?:Handle|HandleProtected|HandlePublic|HandleFunc)\s*\(\s*"(\/[^"]*)"\s*(\+\s*(\w+))?/g)) {
    const line = code.slice(0, m.index).split('\n').length;
    if (!m[2]) {
      out.push({ path: m[1], line, expanded: true });
      continue;
    }
    const loop = loops.filter((l) => l.variable === m[3] && l.start <= m.index).pop();
    if (loop) {
      for (const name of loop.names) out.push({ path: m[1] + name, line, expanded: true });
    } else {
      out.push({ path: m[1], line, expanded: false });
    }
  }
  return out;
}

// Нарушения: регистрации, которые совпадают со снятым маршрутом или могут до него достать.
function removedRegistrations(file, src, list = removedRoutes()) {
  const problems = [];
  for (const reg of goRegistrations(src)) {
    const hit = reg.expanded ? removedMatch(reg.path, list) : prefixReachesRemoved(reg.path, list);
    if (hit) {
      problems.push(
        `${file}:${reg.line}: регистрация ${reg.path}${reg.expanded ? '' : '+…'} возвращает снятый маршрут ${hit}`
      );
    }
  }
  return problems;
}

module.exports = { removedRoutes, removedMatch, prefixReachesRemoved, stripGoComments, goRegistrations, removedRegistrations };
