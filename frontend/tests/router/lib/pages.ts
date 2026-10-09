import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Маршруты страниц панели для обхода (RT-05). Список берётся из
// frontend/src/Dashboard.svelte во время прогона: ветви `currentTab === '<маршрут>'`
// (тот же разбор, что parsePages в scripts/router/import-graph.mjs), поэтому новая
// страница попадает в обход без правки спека.

const DASHBOARD = fileURLToPath(new URL('../../../src/Dashboard.svelte', import.meta.url));

/** Страниц в панели сейчас 15; меньше 10 — значит, разбор файла сломан. */
const MIN_ROUTES = 10;

/** Маршруты страниц: отсортированные, без дублей. */
export function routes(): string[] {
  const text = readFileSync(DASHBOARD, 'utf8');
  const found = new Set<string>();
  for (const m of text.matchAll(/currentTab === '([a-z][\w-]*)'\}/g)) {
    found.add(m[1]);
  }
  const list = [...found].sort();
  if (list.length < MIN_ROUTES) {
    throw new Error(
      `разбор Dashboard.svelte сломан: найдено маршрутов ${list.length} (${list.join(', ')}), ожидалось не меньше ${MIN_ROUTES}`
    );
  }
  return list;
}
