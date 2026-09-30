/**
 * kernelView.ts — чистая логика отображения карточки ядра: этапы установки,
 * итог операции, бейдж состояния и подпись отката. Без Svelte и сторов —
 * возвращает ключи i18n и параметры, перевод делает вызывающий код через $t().
 */

export type KernelStage = 'starting' | 'downloading' | 'extracting' | 'replacing';
export type KernelResultKind = 'installed' | 'updated' | 'reinstalled' | 'rolled_back' | 'uploaded';

/** Подмножество полей KernelInfo бэкенда, которое читает логика отображения. */
export interface KernelLike {
  current_version?: string;
  latest_version?: string;
  has_update?: boolean;
  ahead_of_latest?: boolean;
  has_backup?: boolean;
  backup_version?: string;
  channel?: string;
  status?: string;
  stage?: string;
  result_kind?: string;
  result_version?: string;
  /** Код причины status=failed от бэкенда; перевод собирает фронтенд. */
  error_kind?: string;
  message?: string;
}

export interface KernelBadge {
  variant: 'stopped' | 'warning' | 'idle';
  key?: string;
  label?: string;
  params?: Record<string, string>;
}

export interface I18nRef {
  key: string;
  params?: Record<string, string>;
}

/** Статусы, пока которые карточка опрашивается и действия недоступны. */
export const TRANSITIONAL_KERNEL_STATUSES = ['checking', 'downloading', 'installing'] as const;

/** Виды ошибки проверки релиза, которые бэкенд отдаёт в error_kind при status=failed. */
export const ERROR_KINDS = ['release_lookup_failed', 'no_release', 'unsupported_arch'] as const;

const STAGES: readonly string[] = ['starting', 'downloading', 'extracting', 'replacing'];
const RESULT_KINDS: readonly string[] = [
  'installed',
  'updated',
  'reinstalled',
  'rolled_back',
  'uploaded'
];

export function isTransitionalStatus(status: string | undefined): boolean {
  return (TRANSITIONAL_KERNEL_STATUSES as readonly string[]).includes(status ?? '');
}

/** Бэкенд сообщает отсутствующее ядро литералом «not installed». */
export function formatKernelVersion(v: string | undefined): string {
  if (!v || v === 'not installed') return '';
  const bare = v.replace(/^v/, '');
  // Плавающая сборка (alpha-<sha>) — без префикса v
  return /^\d/.test(bare) ? `v${bare}` : bare;
}

function bareVersion(v: string | undefined): string {
  return formatKernelVersion(v).replace(/^v/, '');
}

/** Ключ подписи этапа установки; null, когда установка не идёт. */
export function stageLabelKey(k: KernelLike): string | null {
  if (k.status !== 'downloading' && k.status !== 'installing') return null;
  if (k.stage && STAGES.includes(k.stage)) return `svc.kernel_stage_${k.stage}`;
  return k.status === 'downloading' ? 'svc.kernel_stage_downloading' : 'svc.kernel_stage_starting';
}

/** Итог завершённой операции; null для неизвестного вида или незавершённого статуса. */
export function resultMessage(k: KernelLike): I18nRef | null {
  if (k.status !== 'done') return null;
  if (!k.result_kind || !RESULT_KINDS.includes(k.result_kind)) return null;
  const version = formatKernelVersion(k.result_version || k.current_version);
  // Без версии («Установлено {version}» оставило бы висячий пробел) — фраза без параметра
  if (!version) return { key: `svc.kernel_result_${k.result_kind}_plain` };
  return { key: `svc.kernel_result_${k.result_kind}`, params: { version } };
}

/**
 * Перевод причины неудачи по error_kind; null, когда статус не failed или вид
 * неизвестен — вызывающий показывает прежний message. Деталь — часть message
 * после первого «: » (текст ошибки запроса или архитектура); у no_release её нет.
 */
export function failureMessage(k: KernelLike): I18nRef | null {
  if (k.status !== 'failed') return null;
  const kind = k.error_kind;
  if (!kind || !(ERROR_KINDS as readonly string[]).includes(kind)) return null;
  const key = `svc.kernel_error_${kind}`;
  if (kind === 'no_release') return { key };
  const message = k.message ?? '';
  const sep = message.indexOf(': ');
  return { key, params: { detail: sep >= 0 ? message.slice(sep + 2).trim() : '' } };
}

/** Значения current_version, когда версия не определилась (сбой запуска бинарника). */
const UNKNOWN_KERNEL_VERSIONS = ['error', 'unknown'];

export function kernelBadge(k: KernelLike): KernelBadge {
  if (!formatKernelVersion(k.current_version)) {
    return { variant: 'stopped', key: 'kernel.status.not_installed' };
  }
  if (k.status === 'checking') return { variant: 'idle', key: 'svc.channel_checking' };
  if (k.status === 'failed') return { variant: 'stopped', key: 'svc.kernel_error_badge' };
  if (UNKNOWN_KERNEL_VERSIONS.includes(k.current_version ?? '')) {
    return { variant: 'warning', key: 'svc.version_unknown_badge' };
  }
  if (k.has_update) {
    return { variant: 'warning', label: `→ ${formatKernelVersion(k.latest_version)}` };
  }
  if (k.ahead_of_latest && k.channel === 'stable') {
    return {
      variant: 'warning',
      key: 'svc.prerelease_ahead',
      params: { version: formatKernelVersion(k.latest_version) }
    };
  }
  return { variant: 'idle', key: 'svc.actual_badge' };
}

/** Кнопка отката скрыта без бэкапа и когда бэкап — та же версия, что установлена. */
export function rollbackVisible(k: KernelLike): boolean {
  if (!k.has_backup) return false;
  const backup = bareVersion(k.backup_version);
  return !(backup && backup === bareVersion(k.current_version));
}

export function rollbackLabel(k: KernelLike): I18nRef {
  const version = formatKernelVersion(k.backup_version);
  return version ? { key: 'svc.rollback_to', params: { version } } : { key: 'svc.rollback' };
}

/** «Поставить стабильную» — только на канале stable поверх более новой pre-release. */
export function showInstallStable(k: KernelLike): boolean {
  return !!k.ahead_of_latest && k.channel === 'stable' && !isTransitionalStatus(k.status);
}
