// Разбор хэша адреса в имя вкладки. Чистые функции: адрес приходит аргументом,
// ничего не читается и не пишется в браузерные глобальные объекты — поэтому
// начальную вкладку можно вычислить при создании компонента, до первого кадра.

interface ParsedHash {
  base: string;
  params: URLSearchParams;
}

function parseHash(hash: string): ParsedHash | null {
  if (!hash || !hash.startsWith('#/')) return null;
  const path = hash.slice(2);
  const queryIdx = path.indexOf('?');
  if (queryIdx === -1) return { base: path, params: new URLSearchParams() };
  return {
    base: path.slice(0, queryIdx),
    params: new URLSearchParams(path.slice(queryIdx + 1))
  };
}

interface LegacyRoute {
  matches: (base: string, params: URLSearchParams) => boolean;
  target: string;
}

// Единая таблица прежних адресов настройки (D-10): экраны, писавшие конфиг ядра мимо слоя,
// удалены, их адреса ведут в подразделы «Конфигурации». Совпадение проверяется по пути и
// значению параметра, поэтому порядок параметров запроса на результат не влияет. Цели —
// адреса #/config/*, которых в таблице нет: повторный разбор даёт null, цикла нет.
// Таблица хранится до ревизии после 1.0.
const LEGACY_ROUTES: readonly LegacyRoute[] = [
  // Конструктор Xray, конструктор маршрутизации Xray и генератор Mihomo
  { matches: (base) => base === 'constructor' || base === 'mihomo-gen', target: '#/config/cores' },
  {
    matches: (base, params) => base === 'editor' && params.get('tab') === 'constructor',
    target: '#/config/cores'
  },
  // Редактор (с путём файла в запросе или в адресе): файл выбирает lib/editorRoute.ts
  { matches: (base) => base === 'editor' || base.startsWith('editor/'), target: '#/config/files' },
  // Подписки и вкладка «Провайдеры» на «Прокси»
  {
    matches: (base) => base === 'subscriptions' || base.startsWith('subscriptions/'),
    target: '#/config/nodes'
  },
  {
    matches: (base, params) => base === 'proxies' && params.get('tab') === 'providers',
    target: '#/config/nodes'
  },
  // Менеджер DAT
  { matches: (base) => base === 'dat', target: '#/config/lists' },
  // Маркерные «Свои правила» на «Правилах»
  {
    matches: (base, params) => base === 'rules' && params.get('tab') === 'exceptions',
    target: '#/config/rules'
  }
];

/** Новый хэш для прежнего адреса настройки; для остальных адресов — null. */
export function legacyRedirectHash(hash: string): string | null {
  const parsed = parseHash(hash);
  if (parsed === null) return null;
  const route = LEGACY_ROUTES.find((r) => r.matches(parsed.base, parsed.params));
  return route ? route.target : null;
}

/** Имя вкладки для хэша; пустой или неизвестный по форме адрес — `dashboard`. */
export function tabFromHash(hash: string): string {
  // Прежний адрес сразу даёт вкладку цели: первый кадр не рисует удалённую страницу
  const parsed = parseHash(legacyRedirectHash(hash) ?? hash);
  if (parsed === null) return 'dashboard';
  const { base } = parsed;
  // Подразделы раздела «Конфигурация» (#/config/<раздел>)
  if (base.startsWith('config/')) return 'config';
  return base || 'dashboard';
}
