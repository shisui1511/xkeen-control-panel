import type { Lang } from '../i18n';

/**
 * Schema `description` fields are authored as { ru, en } pairs instead of plain
 * strings. This walks a raw schema and resolves every description to the active
 * locale before it reaches codemirror-json-schema (which expects a plain string
 * per the JSON Schema spec).
 */
export function localizeSchema(node: unknown, lang: Lang): unknown {
  if (Array.isArray(node)) {
    return node.map((item) => localizeSchema(item, lang));
  }
  if (node && typeof node === 'object') {
    const out: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
      if (key === 'description' && value && typeof value === 'object' && !Array.isArray(value)) {
        const pair = value as { ru?: string; en?: string };
        out.description = pair[lang] ?? pair.en ?? pair.ru ?? '';
      } else {
        out[key] = localizeSchema(value, lang);
      }
    }
    return out;
  }
  return node;
}
