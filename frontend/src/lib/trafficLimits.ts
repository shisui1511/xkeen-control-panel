// Клиент лимитов трафика (вкладка «Трафик → Лимиты», D-17): поля совпадают с json-тегами
// services.TrafficLimit, LimitView, LimitsView и LimitAlert. Маршруты нейтральны к ядру
// (GateData): лимиты работают и под Xray, и под Mihomo. Модуль при импорте запросов не делает.
import { writable } from 'svelte/store';
import { apiFetchJSON } from './api';

/** Действие при исчерпании (D-15): прежние варианты ограничения скорости и журнала удалены. */
export type LimitAction = 'notify' | 'block' | 'direct';
export type LimitPeriod = 'daily' | 'weekly' | 'monthly';
export type LimitTargetType = 'group' | 'all';
export type LimitState = 'ok' | 'threshold' | 'exhausted' | 'disabled' | 'group_deleted';
/** Состояние учёта: нет сокета Mihomo, нет счётчиков Xray, ядро остановлено. */
export type LimitData = 'ok' | 'no_socket' | 'no_counters' | 'stopped';
export type LimitGroupMode = 'manual' | 'auto' | 'fallback' | '';

/** Лимит группы панели (по стабильному ID) или всего трафика. */
export interface TrafficLimit {
  /** Пусто — новый лимит (ID выдаст сервер). */
  id: string;
  name: string;
  target_type: LimitTargetType;
  /** ID группы панели; пусто для «Весь трафик». */
  target_id: string;
  limit_bytes: number;
  period: LimitPeriod;
  /** Порог уведомления, 1–99 %. */
  threshold: number;
  action: LimitAction;
  enabled: boolean;
  used_bytes: number;
  /** Последний сброс счётчика (Unix, секунды). */
  last_reset: number;
  alerted_threshold: boolean;
  alerted_exceeded: boolean;
}

/** Лимит в списке с вычисленными сервером полями. */
export interface LimitView extends TrafficLimit {
  /** Начало следующего периода по часам роутера (Unix, секунды). */
  reset_at: number;
  state: LimitState;
  /** Имя и режим группы-цели; пусто для общего лимита и удалённой группы. */
  target_name: string;
  target_mode: LimitGroupMode;
  /** Расход приблизительный (Xray: узел входит в несколько групп). */
  approx: boolean;
  /** ID групп, которые лимит сейчас держит. */
  holding: string[];
}

/** Группа панели, доступная как цель лимита. */
export interface LimitGroup {
  id: string;
  name: string;
  mode: 'manual' | 'auto' | 'fallback';
}

export interface LimitsView {
  limits: LimitView[];
  /** Группы применённого состояния маршрутизации по имени; пусто, пока цели учёта неизвестны. */
  groups: LimitGroup[];
  data: LimitData;
  /** Активное ядро: xray | mihomo | '' (ни одного или конфликт). */
  kernel: string;
  now: number;
}

/** Оповещение о достижении порога или исчерпании лимита. */
export interface LimitAlert {
  limit_id: string;
  limit_name: string;
  kind: 'threshold' | 'exceeded';
  used_bytes: number;
  limit_bytes: number;
  percent: number;
  timestamp: number;
}

/** Поля, которые клиент отправляет при сохранении; счётчик и флаги ведёт сервер. */
export type LimitInput = Pick<
  TrafficLimit,
  | 'id'
  | 'name'
  | 'target_type'
  | 'target_id'
  | 'limit_bytes'
  | 'period'
  | 'threshold'
  | 'action'
  | 'enabled'
>;

/** Последний загруженный список лимитов; null — ещё не загружен. */
export const trafficLimits = writable<LimitsView | null>(null);

// Go отдаёт пустой срез как null: интерфейс получает массивы всегда
function normalizeView(v: LimitsView): LimitsView {
  return {
    limits: (v?.limits ?? []).map((l) => ({ ...l, holding: l.holding ?? [] })),
    groups: v?.groups ?? [],
    data: v?.data ?? 'ok',
    kernel: v?.kernel ?? '',
    now: v?.now ?? 0
  };
}

let listSeq = 0;

/** Читает список лимитов и кладёт его в стор; устаревший ответ отбрасывается. */
export async function loadLimits(signal?: AbortSignal): Promise<LimitsView> {
  const seq = ++listSeq;
  const data = normalizeView(await apiFetchJSON<LimitsView>('/api/traffic/limits', { signal }));
  if (seq === listSeq) trafficLimits.set(data);
  return data;
}

function postJSON<T>(path: string, body: unknown): Promise<T> {
  return apiFetchJSON<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  });
}

/** Создание (пустой id) или правка лимита; ответ — сохранённый лимит с ID сервера. */
export function saveLimit(l: LimitInput): Promise<TrafficLimit> {
  return postJSON<TrafficLimit>('/api/traffic/limits/save', l);
}

export async function deleteLimit(id: string): Promise<void> {
  await postJSON<unknown>('/api/traffic/limits/delete', { id });
}

export async function setLimitEnabled(id: string, enabled: boolean): Promise<void> {
  await postJSON<unknown>('/api/traffic/limits/enabled', { id, enabled });
}

/** «Сбросить счётчик»: расход обнуляется, удержание группы снимается. */
export async function resetLimit(id: string): Promise<void> {
  await postJSON<unknown>('/api/traffic/limits/reset', { id });
}

export async function loadLimitAlerts(signal?: AbortSignal): Promise<LimitAlert[]> {
  const data = await apiFetchJSON<LimitAlert[] | null>('/api/traffic/limits/alerts', { signal });
  return data ?? [];
}

export async function clearLimitAlerts(): Promise<void> {
  await postJSON<unknown>('/api/traffic/limits/alerts/clear', {});
}

// --- Единицы ---

/** Единицы формы лимита (двоичные, как у счётчиков панели). */
export const LIMIT_UNITS = [
  { value: 'MB', bytes: 1024 ** 2 },
  { value: 'GB', bytes: 1024 ** 3 },
  { value: 'TB', bytes: 1024 ** 4 }
] as const;

export type LimitUnit = (typeof LIMIT_UNITS)[number]['value'];

/** Байты в виде «12,4 GB» с разделителем дробной части языка интерфейса. */
export function formatLimitBytes(bytes: number, lang: string): string {
  const b = Math.max(0, bytes || 0);
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = b;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const num = new Intl.NumberFormat(lang, { maximumFractionDigits: i === 0 ? 0 : 1 }).format(v);
  return `${num} ${units[i]}`;
}

// --- Расход и даты лимита (вкладка «Лимиты» и блок «Лимит» карточки группы) ---

/** Расход в процентах лимита (может быть больше 100). */
export function limitPercent(l: LimitView): number {
  return l.limit_bytes > 0 ? (l.used_bytes / l.limit_bytes) * 100 : 0;
}

/** Заполнение полосы расхода: не выходит за 0…100 %. */
export function limitFill(l: LimitView): number {
  return Math.min(100, Math.max(0, limitPercent(l)));
}

/** Вид заливки полосы по состоянию лимита. */
export function limitFillKind(l: LimitView): 'ok' | 'warn' | 'danger' {
  if (l.state === 'exhausted') return 'danger';
  if (l.state === 'threshold') return 'warn';
  return 'ok';
}

/**
 * «DD.MM, HH:MM» момента unix-секунд: со смещением пояса — по часам роутера, без
 * него — в поясе браузера (у LimitsView смещения нет).
 */
export function formatLimitDate(unix: number, offsetSec?: number): string {
  if (!unix) return '—';
  const pad = (n: number) => String(n).padStart(2, '0');
  if (offsetSec !== undefined) {
    const d = new Date((unix + offsetSec) * 1000);
    return `${pad(d.getUTCDate())}.${pad(d.getUTCMonth() + 1)}, ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
  }
  const d = new Date(unix * 1000);
  return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}, ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Лимит в байтах → значение и единица формы (наибольшая единица с целым значением ≥ 1). */
export function splitLimitBytes(bytes: number): { value: number; unit: LimitUnit } {
  for (let i = LIMIT_UNITS.length - 1; i >= 0; i--) {
    const u = LIMIT_UNITS[i];
    if (bytes >= u.bytes)
      return { value: Math.round((bytes / u.bytes) * 1000) / 1000, unit: u.value };
  }
  return { value: Math.round((bytes / LIMIT_UNITS[0].bytes) * 1000) / 1000, unit: 'MB' };
}

// --- Тексты ошибок ---

export type LimitsTFn = (key: string, params?: Record<string, string | number>) => string;

// Поле отказа валидации сервера → ключ ошибки поля формы
const FIELD_ERROR_KEYS: Record<string, string> = {
  name: 'traffic.limits.err_name',
  limit_bytes: 'traffic.limits.err_limit',
  target_type: 'traffic.limits.err_target',
  target_id: 'traffic.limits.err_target'
};

/**
 * Текст ошибки сохранения лимита по коду ответа: отказ действия у групп «авто»/«резерв» и
 * отказы известных полей — их тексты формы; прочее — traffic.limits.save_failed с причиной
 * (переведённое сервером сообщение, иначе код).
 */
export function limitsErrorText(
  t: LimitsTFn,
  code: string | undefined,
  params?: Record<string, string>,
  message?: string
): string {
  if (code === 'action_invalid') return t('traffic.limits.hint_auto_notify');
  if (code === 'invalid_input') {
    const key = FIELD_ERROR_KEYS[params?.field ?? ''];
    if (key && params?.reason !== 'too_long' && params?.reason !== 'too_many') return t(key);
  }
  const reason = message && message !== '' ? message : code && code !== '' ? code : '?';
  return t('traffic.limits.save_failed', { reason });
}

/** Код, параметры и текст ошибки запроса лимитов. */
export function limitsErrorMeta(e: unknown): {
  code?: string;
  params?: Record<string, string>;
  message: string;
} {
  const err = e as { code?: unknown; params?: unknown; message?: unknown } | null;
  return {
    code: typeof err?.code === 'string' ? err.code : undefined,
    params:
      err?.params && typeof err.params === 'object'
        ? (err.params as Record<string, string>)
        : undefined,
    message: typeof err?.message === 'string' ? err.message : ''
  };
}
