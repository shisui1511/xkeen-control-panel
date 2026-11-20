// Клиент окна чистого старта (фаза 151, D-06) и уборки следов старой панели (D-04, D-05).
// Признак «окно ждёт закрытия» приходит несекретным полем legacy снимка слоя; адреса прежних
// подписок отдаёт только GET /api/configlayer/legacy. Адреса — секрет пользователя: они живут
// только в памяти этого модуля, пока окно открыто, и не попадают в localStorage, консоль,
// тосты и атрибуты разметки. Закрытие окна хранит сервер (как закрытие мастера), событие
// legacy слоя несёт только счётчик: по нему окно перечитывается и в других вкладках.

import { writable, get } from 'svelte/store';
import { apiFetchJSON } from './api';
import {
  onLayerEvent,
  post,
  layerSnapshot,
  refetchLayerState,
  normalizeLegacySummary,
  type LegacySummary
} from './configLayer';
import { showToast } from '../stores';
import { t } from '../i18n';

export type { LegacySummary };

/** Ответ GET /api/configlayer/legacy: сводка и адреса прежних подписок как есть. */
export interface LegacyView extends LegacySummary {
  urls: string[];
}

const BASE = '/api/configlayer/legacy';

/** Окно со списком адресов; null — ответа ещё нет (скелетон) или окно не открыто. */
export const legacyView = writable<LegacyView | null>(null);
/** Чтение списка не удалось: окно показывает путь архива вместо списка. */
export const legacyLoadError = writable(false);
/**
 * Окно закрыто в этой вкладке. Ставится сразу (оптимистично) и держится до перезагрузки
 * страницы: при сбое записи окно вернётся только при следующей загрузке после входа.
 */
export const legacyClosedHere = writable(false);

// Номер чтения: поздний ответ устаревшего запроса не затирает свежий вид и не воскрешает
// адреса после закрытия окна
let loadSeq = 0;

/**
 * Приводит ответ к полной форме. Адреса не нормализуются и не перекодируются: строка идёт
 * в окно и в буфер обмена ровно такой, какой её сохранила старая панель.
 */
function normalizeView(raw: unknown): LegacyView {
  const urls =
    typeof raw === 'object' && raw !== null && Array.isArray((raw as { urls?: unknown }).urls)
      ? ((raw as { urls: unknown[] }).urls.filter((u) => typeof u === 'string') as string[])
      : [];
  return { ...normalizeLegacySummary(raw), urls };
}

/** Читает окно (сводку и адреса). Ошибка оставляет окно открытым с путём архива. */
export async function loadLegacy(): Promise<void> {
  const mine = ++loadSeq;
  try {
    const raw = await apiFetchJSON<unknown>(BASE, { cache: 'no-store' });
    if (mine !== loadSeq) return;
    legacyView.set(normalizeView(raw));
    legacyLoadError.set(false);
  } catch {
    if (mine !== loadSeq) return;
    // Текст ошибки не выводится: окно показывает только путь архива из снимка
    legacyLoadError.set(true);
  }
}

/** Забывает адреса: окно закрыто или снято с экрана. */
function forgetLegacy(): void {
  loadSeq++;
  legacyView.set(null);
  legacyLoadError.set(false);
}

/**
 * Слежение за открытым окном: первое чтение и перечитывание по событию legacy (закрытие в
 * другой вкладке). Возвращает функцию снятия, которая забывает адреса.
 */
export function watchLegacy(): () => void {
  void loadLegacy();
  const off = onLayerEvent('legacy', () => void loadLegacy());
  return () => {
    off();
    forgetLegacy();
  };
}

/** Убирает окно с экрана этой вкладки без записи на сервер. */
function hideLegacy(): void {
  legacyClosedHere.set(true);
  forgetLegacy();
}

/**
 * Причина отказа для тоста: конверт ошибки кладёт настоящую причину в `detail`, а `message`
 * при ответе 500 — общее «Внутренняя ошибка сервера».
 */
export function legacyErrorReason(err: unknown): string {
  const detail = (err as { detail?: unknown } | null)?.detail;
  if (typeof detail === 'string' && detail !== '') return detail;
  return err instanceof Error ? err.message : String(err);
}

function closeFailed(err: unknown): void {
  const reason = legacyErrorReason(err);
  showToast('warning', get(t)('cfg.reset.close_failed', { reason }), 6000);
}

/**
 * Записывает закрытие окна на сервер. Окно в этой вкладке убирается в любом случае; сбой —
 * тост, окно покажется снова при следующей загрузке. Возвращает, удалась ли запись.
 */
export async function closeLegacy(): Promise<boolean> {
  try {
    const summary = await post<unknown>(`${BASE}/close`, {});
    const legacy = normalizeLegacySummary(summary);
    layerSnapshot.update((s) => (s ? { ...s, legacy } : s));
    return true;
  } catch (err) {
    closeFailed(err);
    return false;
  } finally {
    hideLegacy();
  }
}

/** «Закрыть», Esc, крестик, подложка: окно уходит сразу, запись идёт следом. */
export function dismissLegacy(): void {
  hideLegacy();
  void closeLegacy();
}

/**
 * «Убрать» следы старой панели в файлах ядер (D-04): сервер делает резервную копию каждого
 * файла, вырезает вставки, проверяет конфигурацию ядром и откатывает набор при отказе.
 * Ошибка пробрасывается вызывающему; после успеха снимок перечитывается — уведомление
 * исчезает, когда следов не осталось.
 */
export async function cleanLegacy(): Promise<void> {
  await post<unknown>(`${BASE}/clean`, {});
  await refetchLayerState();
}
