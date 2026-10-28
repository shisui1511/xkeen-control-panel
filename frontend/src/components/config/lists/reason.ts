import { geodataErrorText } from '../../../lib/configGeodata';

/**
 * Причина сбоя для подстановки в «…: {reason}.»: текст ошибки менеджера без конечной точки
 * и с первой буквой в нижнем регистре, чтобы фраза не получила двойную точку.
 */
export function reasonOf(err: unknown): string {
  const text = geodataErrorText(err).trim().replace(/\.$/, '');
  return text === '' ? text : text.charAt(0).toLowerCase() + text.slice(1);
}

/** Вид файла из ответа менеджера в вид списка для условий правила: site или ip. */
export function listKind(kind: string): 'site' | 'ip' {
  return kind === 'ip' || kind === 'geoip' ? 'ip' : 'site';
}
