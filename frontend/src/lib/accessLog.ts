// Исход включения и выключения access-лога Xray через секцию log (PART-01, D-13, D-17):
// разбор ответа POST /api/configlayer/cores/access-log и тосты по нему. Решения принимает
// сервер: здесь только перевод результата применения (result, restart) в сообщение. Общий
// для блока на «Логах» и диалога взятия секции.

import { get } from 'svelte/store';
import { currentLang, t } from '../i18n';
import { showToast } from '../stores';
import { parseValidationError } from './errorParser';
import type { LayerApply } from './configLayer';

/** Отказ применения: код результата и причина (вывод ядра, как пришёл). */
export interface AccessLogFailure {
  /** validation_failed и родственные коды проверки — «Xray не принял»; прочее — отказ записи/перезапуска. */
  kind: 'test' | 'apply';
  code: string;
  reason: string;
}

const TOAST_REASON_MAX = 300;

/** Результат применения: null, если применение прошло; иначе отказ с причиной. */
export function accessLogFailure(view: LayerApply): AccessLogFailure | null {
  const result = view.result;
  const restart = view.restart?.find((r) => r.kernel === 'xray');
  const outcome = restart?.outcome ?? '';
  if (result && !result.ok) {
    const raw = (result.message ?? '').trim();
    const reason = parseValidationError(raw, get(currentLang)) || raw || result.code;
    return {
      kind: result.code.startsWith('validation_') ? 'test' : 'apply',
      code: result.code,
      reason
    };
  }
  if (outcome.startsWith('failed_') || outcome === 'interrupted') {
    return { kind: 'apply', code: outcome, reason: get(t)('apply.restart_failed_unknown') };
  }
  return null;
}

function openLogs(): void {
  window.location.hash = '#/logs';
}

/** Короткая причина для тоста: вывод ядра целиком живёт только в диалоге. */
function shortReason(reason: string): string {
  return reason.length > TOAST_REASON_MAX ? `${reason.slice(0, TOAST_REASON_MAX)}…` : reason;
}

/** Тост отказа: «Xray не принял конфигурацию» либо «Сохранено, но перезапуск не удался». */
export function notifyAccessLogFailure(failure: AccessLogFailure): void {
  const tr = get(t);
  if (failure.kind === 'test') {
    showToast('error', tr('xlog.test_failed', { reason: shortReason(failure.reason) }), 10000);
    return;
  }
  showToast('error', tr('apply.restart_failed', { reason: shortReason(failure.reason) }), 10000, {
    label: tr('apply.open_logs'),
    onClick: openLogs
  });
}

/**
 * Тост успешного применения: ядро перезапущено — обычный текст; не запущено или не
 * тронуто — «вступит в силу при запуске»; конфликт ядер — отдельный текст.
 */
export function notifyAccessLogApplied(view: LayerApply, enabled: boolean): void {
  const tr = get(t);
  const outcome = view.restart?.find((r) => r.kernel === 'xray')?.outcome ?? '';
  if (outcome === 'untouched_conflict') {
    showToast('warning', tr('kernel.conflict_blocked_toast'), 8000);
    return;
  }
  const stopped =
    outcome === 'deferred' || outcome === 'untouched_inactive' || outcome === 'no_runtime_effect';
  if (stopped) {
    showToast(
      'success',
      tr(enabled ? 'xlog.enabled_stopped_toast' : 'xlog.disabled_stopped_toast')
    );
    return;
  }
  showToast('success', tr(enabled ? 'xlog.enabled_toast' : 'xlog.disabled_toast'));
}

/** Тост по ответу целиком: отказ либо успех; true — применение прошло. */
export function notifyAccessLog(view: LayerApply, enabled: boolean): boolean {
  const failure = accessLogFailure(view);
  if (failure) {
    notifyAccessLogFailure(failure);
    return false;
  }
  notifyAccessLogApplied(view, enabled);
  return true;
}
