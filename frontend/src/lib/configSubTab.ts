// Вкладки раздела «Конфигурация» и их маршруты (#/config/<вкладка>). Чистые функции:
// адрес приходит аргументом, браузерные глобальные объекты не читаются. Фазы 147–148
// добавляют вкладки в тот же список.

export type ConfigSubTab = 'nodes' | 'groups' | 'rules' | 'files';

/** Вкладка по умолчанию: открывается для `#/config` без суффикса и для незнакомого суффикса. */
export const DEFAULT_CONFIG_SUBTAB: ConfigSubTab = 'nodes';

/** Вкладки в порядке показа. */
export const CONFIG_SUBTABS: readonly ConfigSubTab[] = ['nodes', 'groups', 'rules', 'files'];

/** Вкладка по хэшу адреса. Запрос после `?` отбрасывается; неизвестный путь — «Узлы». */
export function configSubTab(hash: string): ConfigSubTab {
  if (!hash || !hash.startsWith('#/')) return DEFAULT_CONFIG_SUBTAB;
  const path = hash.slice(2);
  const queryIdx = path.indexOf('?');
  const base = queryIdx !== -1 ? path.slice(0, queryIdx) : path;
  for (const tab of CONFIG_SUBTABS) {
    if (base === `config/${tab}`) return tab;
  }
  return DEFAULT_CONFIG_SUBTAB;
}

/** Хэш адреса вкладки. */
export function configSubTabHash(tab: ConfigSubTab): string {
  return `#/config/${tab}`;
}

/** Вкладка по значению Tabs; незнакомое значение — «Узлы». */
export function asConfigSubTab(value: string): ConfigSubTab {
  return (CONFIG_SUBTABS as readonly string[]).includes(value)
    ? (value as ConfigSubTab)
    : DEFAULT_CONFIG_SUBTAB;
}
