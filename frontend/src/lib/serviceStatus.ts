/**
 * serviceStatus.ts — форма ответа /api/service/status и правила показа
 * «последнего известного» значения: бэкенд отдаёт кэш с полями stale и
 * age_seconds, интерфейс молчит до порога и затем показывает тихий бейдж.
 */

/** После скольких секунд возраста кэша показывается бейдж «данные от HH:MM». */
export const STALE_BADGE_AFTER_SECONDS = 30;

export interface ServiceStatusData {
  is_running: boolean;
  active_kernel?: string;
  pid?: number;
  uptime?: string;
  binary_path?: string;
  raw?: string;
  /** Значение взято из кэша, а не из свежего опроса. */
  stale?: boolean;
  /** Возраст кэша; отсутствует, пока первый опрос не удался (холодный кэш). */
  age_seconds?: number;
  watchdog?: unknown;
  xkeen_installed?: boolean;
  xkeen_installer_available?: boolean;
  xkeen_setup_incomplete?: boolean;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/** Разбирает ответ с конвертом {success, data} или сырой объект; иначе null. */
export function parseServiceStatus(json: unknown): ServiceStatusData | null {
  if (!isRecord(json)) return null;
  if (json.success === false) return null;
  const body = isRecord(json.data) ? json.data : json;
  if (typeof body.is_running !== 'boolean') return null;
  return body as unknown as ServiceStatusData;
}

/** Бейдж устаревания: только когда возраст известен и строго больше порога. */
export function staleBadgeVisible(d: ServiceStatusData): boolean {
  return typeof d.age_seconds === 'number' && d.age_seconds > STALE_BADGE_AFTER_SECONDS;
}

/** Холодный кэш: статус собран без опроса xkeen, состояние ядра неизвестно. */
export function isColdUnknown(d: ServiceStatusData): boolean {
  return d.stale === true && d.age_seconds === undefined;
}

/** HH:MM момента снимка (now − age). timeZone по умолчанию локальный. */
export function snapshotTimeLabel(
  ageSeconds: number,
  nowMs: number,
  locale: string,
  timeZone?: string
): string {
  return new Intl.DateTimeFormat(locale, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    ...(timeZone ? { timeZone } : {})
  }).format(new Date(nowMs - ageSeconds * 1000));
}
