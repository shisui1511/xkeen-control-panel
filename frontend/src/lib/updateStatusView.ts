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
export const STEP_CODES = [
  'checking',
  'up_to_date',
  'downloading',
  'backup_creating',
  'installing',
  'restarting',
  'complete',
  'rollback_restoring'
] as const;

/** Коды сбоев (status=failed), для которых есть переведённая причина. */
export const FAILURE_CODES = [
  'panic',
  'check_failed',
  'download_failed',
  'checksum_failed',
  'chmod_failed',
  'backup_failed',
  'install_failed',
  'restart_failed',
  'health_check_failed',
  'auto_rollback_done',
  'auto_rollback_failed',
  'auto_rollback_start_failed',
  'rollback_failed'
] as const;

/** Коды шагов, в подписи которых есть версия; без неё берётся вариант `_plain`. */
const VERSIONED_CODES: readonly string[] = [
  'downloading',
  'backup_creating',
  'installing',
  'restarting',
  'complete',
  'rollback_restoring'
];

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

/**
 * Причина сбоя по коду плюс технический текст ошибки (params.detail, может быть
 * пустым). null, когда статус не failed или код неизвестен — вызывающий
 * показывает прежний message.
 */
export function failureView(s: UpdateStatusPayload): { reason: I18nRef; detail: string } | null {
  if (s.status !== 'failed') return null;
  const code = s.message_code;
  if (!code || !(FAILURE_CODES as readonly string[]).includes(code)) return null;
  return { reason: { key: `settings.update_fail_${code}` }, detail: s.params?.detail ?? '' };
}
