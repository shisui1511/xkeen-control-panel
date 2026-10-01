/**
 * updateStatusView.ts — чистая логика подписи фонового обновления панели.
 * Бэкенд отдаёт машинный код шага (message_code) и плоские параметры (params);
 * строка message остаётся только для логов. Модуль возвращает ключи i18n и
 * параметры, перевод делает вызывающий код через $t().
 */
import type { I18nRef } from './kernelView';

/** Состояние обновления из GET /api/update/status и SSE /api/update/events. */
export interface UpdateStatusPayload {
  status: string;
  message: string;
  message_code?: string;
  params?: Record<string, string>;
  progress: number;
  downloaded?: number;
  total?: number;
}

/** Коды шагов, для которых есть подпись. */
export const STEP_CODES = ['checking', 'downloading'] as const;

/** Коды шагов, в подписи которых есть версия; без неё берётся вариант `_plain`. */
const VERSIONED_CODES: readonly string[] = ['downloading'];

/**
 * Подпись шага по коду; null, когда статус failed, кода нет или он неизвестен
 * (обновление со старой версии панели) — вызывающий показывает прежний message.
 */
export function stepLabel(s: UpdateStatusPayload): I18nRef | null {
  if (s.status === 'failed') return null;
  const code = s.message_code;
  if (!code || !(STEP_CODES as readonly string[]).includes(code)) return null;
  const key = `settings.update_step_${code}`;
  if (!VERSIONED_CODES.includes(code)) return { key };
  const version = s.params?.version;
  if (!version) return { key: `${key}_plain` };
  return { key, params: { version } };
}
