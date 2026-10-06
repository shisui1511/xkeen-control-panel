// Разбор хэша адреса в имя вкладки. Чистые функции: адрес приходит аргументом,
// ничего не читается и не пишется в браузерные глобальные объекты — поэтому
// начальную вкладку можно вычислить при создании компонента, до первого кадра.

function basePathOf(hash: string): string | null {
  if (!hash || !hash.startsWith('#/')) return null;
  const path = hash.slice(2);
  const queryIdx = path.indexOf('?');
  return queryIdx !== -1 ? path.slice(0, queryIdx) : path;
}

/** Имя вкладки для хэша; пустой или неизвестный по форме адрес — `dashboard`. */
export function tabFromHash(hash: string): string {
  const basePath = basePathOf(hash);
  if (basePath === null) return 'dashboard';
  if (basePath === 'subscriptions' || basePath.startsWith('subscriptions/')) {
    return 'proxies';
  }
  if (basePath === 'mihomo-gen' || basePath === 'constructor') {
    return 'editor';
  }
  // Резерв под подразделы раздела «Конфигурация» (#/config/<раздел>)
  if (basePath.startsWith('config/')) {
    return 'config';
  }
  return basePath || 'dashboard';
}

/** Новый хэш для устаревших адресов подписок; для остальных — null. */
export function legacyRedirectHash(hash: string): string | null {
  const basePath = basePathOf(hash);
  if (basePath === null) return null;
  if (basePath.startsWith('subscriptions/')) {
    const id = basePath.slice('subscriptions/'.length);
    return `#/proxies?tab=providers&expand=${id}`;
  }
  if (basePath === 'subscriptions') {
    return '#/proxies?tab=providers';
  }
  return null;
}
