// Помощники окон расписания ручной группы (D-08…D-11). Определение отрезков и
// пересечений повторяет internal/services/routing/arbiter_schedule.go: клиент
// подсказывает конфликт до сохранения, источник истины — ответ сервера
// (409 schedule_overlap).
import type { ScheduleWindow } from './configProxies';

/** Функция перевода из стора $t. */
type TFn = (key: string, params?: Record<string, string | number>) => string;

export const MINUTES_PER_DAY = 24 * 60;
export const MINUTES_PER_WEEK = 7 * MINUTES_PER_DAY;

/** Окно в объёме, нужном для расчёта отрезков. */
type WindowTimes = Pick<ScheduleWindow, 'days' | 'from' | 'to'>;

/** HH:MM → минуты суток 0…1439; неверный формат — null. */
export function parseClock(hhmm: string): number | null {
  const m = /^(\d{2}):(\d{2})$/.exec(hhmm);
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  return h * 60 + min;
}

/**
 * Полуоткрытые отрезки окна на недельной оси минут [0, 10080), отсчёт с полуночи
 * понедельника. Окно через полночь заканчивается на следующий день; часть за концом
 * недели (окно воскресенья) переносится в начало недели вторым отрезком.
 * Испорченное окно отрезков не даёт.
 */
export function windowSegments(w: WindowTimes): Array<[number, number]> {
  const from = parseClock(w.from);
  const to = parseClock(w.to);
  if (from === null || to === null || from === to) return [];
  const out: Array<[number, number]> = [];
  for (const d of w.days) {
    if (d < 0 || d > 6) continue;
    const start = d * MINUTES_PER_DAY + from;
    let end = d * MINUTES_PER_DAY + to;
    if (from > to) end += MINUTES_PER_DAY;
    if (end > MINUTES_PER_WEEK) {
      out.push([start, MINUTES_PER_WEEK], [0, end - MINUTES_PER_WEEK]);
      continue;
    }
    out.push([start, end]);
  }
  return out;
}

/** Окна пересекаются хотя бы одной минутой недели (включено окно или нет — не важно). */
export function windowsOverlap(a: WindowTimes, b: WindowTimes): boolean {
  const sb = windowSegments(b);
  for (const x of windowSegments(a)) {
    for (const y of sb) {
      if (x[0] < y[1] && y[0] < x[1]) return true;
    }
  }
  return false;
}

/**
 * Первое окно группы, с которым пересекается w: сравниваются все окна, кроме окна с
 * тем же ID, включая выключенные (сервер проверяет так же, D-11).
 */
export function findOverlap<T extends ScheduleWindow>(
  w: Pick<ScheduleWindow, 'id' | 'days' | 'from' | 'to'>,
  others: readonly T[]
): T | null {
  for (const o of others) {
    if (w.id !== '' && o.id === w.id) continue;
    if (windowsOverlap(w, o)) return o;
  }
  return null;
}

/** Окно через полночь: «По» не позже «С». */
export function isOvernight(w: Pick<ScheduleWindow, 'from' | 'to'>): boolean {
  const from = parseClock(w.from);
  const to = parseClock(w.to);
  return from !== null && to !== null && to <= from;
}

/**
 * Сводка дней с понедельника: все семь — «Ежедн.», подряд идущие (от двух) —
 * диапазон «Пн–Пт», прочее — перечисление «Пн, Ср, Пт».
 */
export function daysSummary(days: readonly number[], t: TFn): string {
  const set = [...new Set(days.filter((d) => d >= 0 && d <= 6))].sort((a, b) => a - b);
  if (set.length === 0) return '';
  if (set.length === 7) return t('proxies.rt.sched.days_daily');
  const day = (d: number) => t(`proxies.rt.sched.day_${d}`);
  const consecutive = set.every((d, i) => i === 0 || d === set[i - 1] + 1);
  if (consecutive && set.length >= 2) return `${day(set[0])}–${day(set[set.length - 1])}`;
  return set.map(day).join(', ');
}

/** Сводка окна «Пн–Пт · 09:00–18:00» — строка списка, aria-подписи, конфликт. */
export function windowSummary(w: WindowTimes, t: TFn): string {
  return `${daysSummary(w.days, t)} · ${w.from}–${w.to}`;
}

/** HH:MM момента unix (секунды) по смещению пояса роутера (секунды от UTC). */
export function formatRouterTime(unix: number, offsetSec: number): string {
  const d = new Date((unix + offsetSec) * 1000);
  const hh = String(d.getUTCHours()).padStart(2, '0');
  const mm = String(d.getUTCMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

/** Смещение пояса «UTC+3», «UTC−5:30», «UTC» при нулевом. */
export function formatOffset(offsetSec: number): string {
  if (offsetSec === 0) return 'UTC';
  const sign = offsetSec > 0 ? '+' : '−';
  const abs = Math.abs(offsetSec);
  const h = Math.floor(abs / 3600);
  const m = Math.floor((abs % 3600) / 60);
  return m === 0 ? `UTC${sign}${h}` : `UTC${sign}${h}:${String(m).padStart(2, '0')}`;
}
