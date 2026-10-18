// Вкладки раздела «Конфигурация» и их маршруты (#/config/<вкладка>). Чистые функции:
// адрес приходит аргументом, браузерные глобальные объекты не читаются. Фазы 146–148
// добавляют вкладки в тот же список.

export type ConfigSubTab = 'nodes' | 'files';

/** Вкладка по умолчанию: открывается для `#/config` без суффикса и для незнакомого суффикса. */
export const DEFAULT_CONFIG_SUBTAB: ConfigSubTab = 'nodes';

/** Вкладка по хэшу адреса. Запрос после `?` отбрасывается; неизвестный путь — «Узлы». */
export function configSubTab(hash: string): ConfigSubTab {
  if (!hash || !hash.startsWith('#/')) return DEFAULT_CONFIG_SUBTAB;
  const path = hash.slice(2);
  const queryIdx = path.indexOf('?');
  const base = queryIdx !== -1 ? path.slice(0, queryIdx) : path;
  return base === 'config/files' ? 'files' : DEFAULT_CONFIG_SUBTAB;
}

/** Хэш адреса вкладки. */
export function configSubTabHash(tab: ConfigSubTab): string {
  return tab === 'files' ? '#/config/files' : '#/config/nodes';
}
