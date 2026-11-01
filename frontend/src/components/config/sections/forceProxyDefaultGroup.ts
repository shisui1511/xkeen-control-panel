// Имя группы маршрутизации, которую панель создаёт по умолчанию. Пока для входящих
// force-proxy группа не выбрана, сервер направляет их в эту группу (routing: defaultProxyGroupName),
// поэтому селектор группы показывает её выбранной. Значение совпадает с серверным.
export const FORCE_PROXY_DEFAULT_GROUP = 'Прокси';

// Сравнение имени группы с именем по умолчанию без учёта регистра, как на сервере
// (routing: findGroupByName, strings.EqualFold).
export function isForceProxyDefaultGroup(name: string): boolean {
  return name.toLowerCase() === FORCE_PROXY_DEFAULT_GROUP.toLowerCase();
}
