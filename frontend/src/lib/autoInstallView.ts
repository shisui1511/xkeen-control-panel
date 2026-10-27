/**
 * autoInstallView.ts — чистая логика подписи состояния автоустановки панели.
 * Бэкенд отдаёт в GET /api/update/state код `auto_state` и дату `auto_install_on` (YYYY-MM-DD в
 * поясе роутера); срок интерфейс не считает. Модуль возвращает ключ i18n и параметры date,
 * window и version, перевод делает вызывающий код через $t(): карточка, блок «Доступна версия»
 * и баннер берут текст только отсюда, поэтому формулировки не расходятся. Кандидат
 * автоустановки (`auto_version`) на Основном канале может быть старше новейшей версии
 * (`latest_version`): тогда выбирается подпись с номером кандидата.
 */
import type { I18nRef } from './kernelView';
import { formatRouterDate } from './format';

/** Коды, для которых есть подпись. none, failed_version и неизвестные коды не рисуются. */
export const AUTO_STATES = ['delayed', 'scheduled', 'urgent', 'held', 'dev_build'] as const;

export type AutoState = (typeof AUTO_STATES)[number];

/** Коды, у которых есть срок (date и window в тексте). */
const DATED: readonly AutoState[] = ['delayed', 'scheduled', 'urgent'];

/** Подмножество UpdateCheckState, нужное помощнику. */
export interface AutoInstallInput {
  auto_state?: string;
  auto_install_on?: string;
  install_window?: string;
  auto_install?: boolean;
  auto_version?: string;
  latest_version?: string;
  error?: string;
}

/** Код состояния из закрытого перечня; null — нет поля, none, failed_version или неизвестный код. */
export function autoStateOf(
  st: Pick<AutoInstallInput, 'auto_state'> | null | undefined
): AutoState | null {
  const code = st?.auto_state ?? '';
  return (AUTO_STATES as readonly string[]).includes(code) ? (code as AutoState) : null;
}

/**
 * Кандидат автоустановки — не новейшая версия: подпись должна назвать его номер. Пусто, когда
 * кандидат совпадает с новейшей версией или панель его не отдала.
 */
export function otherAutoVersion(
  st: Pick<AutoInstallInput, 'auto_version' | 'latest_version'> | null | undefined
): string {
  const v = st?.auto_version ?? '';
  return v && v !== st?.latest_version ? v : '';
}

/** Ключ i18n по коду и поверхности. */
function keyFor(code: AutoState, surface: 'card' | 'banner'): string {
  if (surface === 'card') return `settings.update_auto_state_${code}`;
  switch (code) {
    case 'delayed':
    case 'scheduled':
      return 'dash.update_banner_auto';
    case 'urgent':
      return 'dash.update_banner_urgent';
    case 'held':
      return 'dash.update_banner_held';
    case 'dev_build':
      return 'dash.update_banner_dev_build';
  }
}

/**
 * Подпись состояния автоустановки; null, когда показывать нечего.
 * dev_build показывается при любой настройке автоустановки (объясняет, почему она не сработает),
 * остальные коды — только при включённой. Сроки не рисуются при ошибке проверки (данные могут
 * быть устаревшими) и без корректной даты.
 */
export function autoInstallView(
  st: AutoInstallInput | null | undefined,
  lang: string,
  surface: 'card' | 'banner'
): I18nRef | null {
  if (!st) return null;
  const code = autoStateOf(st);
  if (!code) return null;
  if (code !== 'dev_build' && st.auto_install !== true) return null;
  const key = keyFor(code, surface);
  if (!DATED.includes(code)) return { key };
  if (st.error) return null;
  const date = formatRouterDate(st.auto_install_on, lang);
  if (!date) return null;
  const params = { date, window: st.install_window ?? '' };
  const version = otherAutoVersion(st);
  if (version) return { key: `${key}_version`, params: { ...params, version } };
  return { key, params };
}
