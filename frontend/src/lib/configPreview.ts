// Клиент предпросмотра «Применить» (D-08…D-12, CFG-02): типы ответа
// GET /api/configlayer/apply/preview, загрузка и единая точка открытия окна изменений.
// Секреты в ответе уже замаскированы сервером (D-11): клиент маску не снимает и сырых
// значений для окна не запрашивает.

import { writable, get } from 'svelte/store';
import { apiFetchJSON } from './api';
import { conflictVisible, showToast } from '../stores';
import { t } from '../i18n';

/** Вид изменения строки. */
export type ChangeKind = 'added' | 'changed' | 'removed';

/** Строка изменения черновика по сущности (контракт ChangeRow слоя). */
export interface ChangeRow {
  tab: string;
  kind: ChangeKind;
  entity: string;
  id: string;
  label: string;
  kernel?: string;
  section: string;
}

/** Файл ядра, который «Применить» запишет или удалит: «было на диске → станет». */
export interface PreviewFile {
  key: string;
  path: string;
  action: 'write' | 'delete';
  before: string;
  after: string;
  added: number;
  removed: number;
  missing: boolean;
  truncated: boolean;
  secrets_changed: number;
}

/** Файлы одного установленного ядра; deferred — правки вступят в силу при запуске ядра. */
export interface PreviewKernel {
  kernel: string;
  active: boolean;
  deferred: boolean;
  files: PreviewFile[];
}

/** Причина, по которой «Применить» сейчас откажет. */
export interface PreviewBlock {
  code: 'drift_blocked' | 'checks_failed' | 'apply_busy' | 'geodata_busy' | string;
  count: number;
}

/** Чужой файл ядра, который при взятии уходит в копию. */
export interface PreviewMoved {
  kernel: string;
  path: string;
}

/** Объявление перезапуска: ядро и причина (как в снимке слоя). */
export interface PreviewRestart {
  kernel: string;
  reason: string;
}

/** Ответ предпросмотра; count равно числу строк changes и draft_changes снимка (D-12). */
export interface ApplyPreview {
  draft_revision: number;
  count: number;
  changes: ChangeRow[];
  restart: PreviewRestart[];
  kernels: PreviewKernel[];
  moved: PreviewMoved[];
  blocked: PreviewBlock | null;
  secrets_changed: number;
}

/** Порядок групп окна — порядок вкладок раздела; owner — владение файлами. */
export const PREVIEW_TAB_ORDER = ['nodes', 'groups', 'rules', 'lists', 'cores', 'owner'] as const;

export type PreviewTab = (typeof PREVIEW_TAB_ORDER)[number];

/** Маска секрета, как её выводит сервер. */
export const SECRET_MASK = '••••••••';

/** Предпросмотр «Применить» по текущему черновику. Только чтение: черновик и диск не меняются. */
export function fetchApplyPreview(): Promise<ApplyPreview> {
  return apiFetchJSON<ApplyPreview>('/api/configlayer/apply/preview');
}

/**
 * Окно изменений одно на раздел и открывается только через openApplyPreview(): так
 * проверка конфликта ядер (D-18) стоит перед окном у всех путей «Применить».
 */
export const applyPreviewOpen = writable(false);

/**
 * Открывает окно изменений. При конфликте ядер (запущены оба) окно не открывается:
 * показывается причина и возвращается false (D-18).
 */
export function openApplyPreview(): boolean {
  if (get(conflictVisible)) {
    showToast('warning', get(t)('cfg.apply_blocked_conflict'));
    return false;
  }
  applyPreviewOpen.set(true);
  return true;
}

/** Имя ядра для текста строки. */
export function kernelTitle(kernel: string | undefined): string {
  if (kernel === 'xray') return 'Xray';
  if (kernel === 'mihomo') return 'Mihomo';
  return kernel ?? '';
}

type Translate = (key: string, params?: Record<string, string | number>) => string;

const KNOWN_ENTITIES = new Set([
  'source',
  'my_node',
  'group',
  'rule',
  'rules_final',
  'probe',
  'preset',
  'geodata',
  'section',
  'dns_takeover',
  'diag',
  'other'
]);

/**
 * Текст строки на языке панели. Владение секцией и маршрутизацией читается по виду
 * (взятие или возврат); неизвестная сущность — общий текст «Секция …». Имя секции ядра
 * берётся из словаря секций, если оно там есть, иначе — идентификатор как есть.
 */
export function rowText(row: ChangeRow, tr: Translate): string {
  const kernel = kernelTitle(row.kernel);
  let name = row.label;
  if ((row.entity === 'section' || row.entity === 'section_owner') && row.kernel) {
    const titleKey = `cfg.sec.title.${row.kernel}.${row.label}`;
    const title = tr(titleKey);
    if (title !== titleKey) name = title;
  }
  let key: string;
  if (row.entity === 'section_owner' || row.entity === 'routing_owner') {
    key = `cfg.preview.entity.${row.entity}_${row.kind === 'removed' ? 'removed' : 'added'}`;
  } else if (KNOWN_ENTITIES.has(row.entity)) {
    key = `cfg.preview.entity.${row.entity}`;
  } else {
    key = 'cfg.preview.entity.other';
  }
  return tr(key, { name, kernel });
}
