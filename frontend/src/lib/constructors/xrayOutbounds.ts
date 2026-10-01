// Системные исходящие Xray (`direct`, `block`, `dns-out`) живут в файле 04_outbounds.json
// рядом с пользовательскими, но в конструкторе показываются отдельным блоком «только чтение».
// Модуль разделяет массив на две части и собирает обратно, не меняя порядок записей:
// первый исходящий в списке Xray использует как исходящий по умолчанию.

export const SYSTEM_OUTBOUND_TAGS = ['direct', 'block', 'dns-out'];

/** Системная запись и число пользовательских записей перед ней в исходном файле. */
export type PlacedOutbound = { outbound: any; at: number };

function tagOf(o: any): string | undefined {
  return o && typeof o === 'object' && typeof o.tag === 'string' && o.tag ? o.tag : undefined;
}

function isSystem(o: any): boolean {
  const tag = tagOf(o);
  return tag !== undefined && SYSTEM_OUTBOUND_TAGS.includes(tag);
}

/** Убирает повторы по тегу (побеждает первая запись); записи без тега остаются как есть. */
export function dedupeByTag(list: any[]): any[] {
  const seen = new Set<string>();
  const out: any[] = [];
  for (const o of list) {
    const tag = tagOf(o);
    if (tag === undefined) {
      out.push(o);
      continue;
    }
    if (seen.has(tag)) continue;
    seen.add(tag);
    out.push(o);
  }
  return out;
}

/** Убирает повторы строк, сохраняя порядок первого появления. */
export function uniqueTags(tags: string[]): string[] {
  return [...new Set(tags)];
}

/**
 * Делит содержимое 04_outbounds.json на системные записи (с позицией) и пользовательские.
 * При повторах тега побеждает первая запись.
 */
export function splitOutbounds(all: unknown[]): { system: PlacedOutbound[]; custom: any[] } {
  const system: PlacedOutbound[] = [];
  const custom: any[] = [];
  const seenSystem = new Set<string>();
  const seenCustom = new Set<string>();
  for (const o of all ?? []) {
    const tag = tagOf(o);
    if (tag !== undefined && SYSTEM_OUTBOUND_TAGS.includes(tag)) {
      if (seenSystem.has(tag)) continue;
      seenSystem.add(tag);
      system.push({ outbound: o, at: custom.length });
      continue;
    }
    if (tag !== undefined) {
      if (seenCustom.has(tag)) continue;
      seenCustom.add(tag);
    }
    custom.push(o);
  }
  return { system, custom };
}

/**
 * Собирает массив для записи: системные записи возвращаются на свои места
 * (позиция ограничена длиной пользовательского списка), повторы тегов убираются.
 * Записей по умолчанию не добавляет.
 */
export function mergeOutbounds(system: PlacedOutbound[], custom: any[]): any[] {
  // системные записи перед пользовательской с индексом i — те, у которых at <= i
  const sorted = [...system].sort((a, b) => a.at - b.at);
  const result: any[] = [];
  let next = 0;
  const flushUpTo = (i: number) => {
    while (next < sorted.length && Math.min(sorted[next].at, custom.length) <= i) {
      result.push(sorted[next].outbound);
      next++;
    }
  };
  for (let i = 0; i < custom.length; i++) {
    flushUpTo(i);
    if (isSystem(custom[i])) continue;
    result.push(custom[i]);
  }
  flushUpTo(Number.POSITIVE_INFINITY);
  return dedupeByTag(result);
}
