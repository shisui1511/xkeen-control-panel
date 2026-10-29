import type { SourceView } from '../../../lib/configGeodata';

/**
 * Название набора для заголовка «Выключить набор…» и строки черновика: стандартный набор —
 * поставщик и вид, как в его группе; набор панели и свой источник — имя файла.
 */
export function dropSetName(s: SourceView): string {
  if (s.origin === 'standard') return `${s.provider ?? s.set_id ?? s.file} ${s.kind}`;
  return s.file;
}
