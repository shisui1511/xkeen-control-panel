// Клиент профилей Mihomo (карточка «Профили», плашка профиля, диалоги создания и
// заполнения секций): типы ответов API /api/mihomo/profiles*, переходов панель ↔ свой
// профиль и заполнения секций из шаблона или профиля, функции запросов и тексты исходов.
// Поля — snake_case, как у сервера. Единственный путь компонентов раздела «Конфигурация» к
// этим маршрутам: решения (владелец, цель возврата, блокировка черновиком) принимает
// сервер, интерфейс ничего не считает сам.

import { apiFetchJSON } from './api';
import { post, currentRevision, applyDraftAck, type DraftAck } from './configLayer';
import { notifyApplyOutcome, type ApplyOutcome } from './serviceApply';
import { showToast } from '../stores';
import { t } from '../i18n';
import { get } from 'svelte/store';
import type { TFn } from './configCores';

// --- Типы контракта ---

/** Кто ведёт профиль: панель (xcp-main), пользователь или XKeen (обычный config.yaml). */
export type ProfileOwner = 'panel' | 'user' | 'xkeen';

export interface ProfileEntry {
  name: string;
  size: number;
  mtime: number;
  active: boolean;
  owner: ProfileOwner;
}

export interface ProfilesState {
  /** config.yaml — ссылка на профиль (профили включены). */
  managed: boolean;
  active?: string;
  profiles: ProfileEntry[];
  /** config.yaml — обычный файл XKeen (владелец xkeen, активен): источник копии и строка списка. */
  plain_config?: ProfileEntry | null;
  /** Профиль, на который Mihomo вернётся при отпускании. */
  return_target?: string;
}

/** Итог активации и переходов панель ↔ свой профиль. */
export interface ActivationResult {
  active: string;
  restarted: boolean;
  rolled_back: boolean;
  error?: string;
  /** Код отказа конвейера слоя (validation_failed, drift_blocked…). */
  code?: string;
  /** Профиль записан, а ядро не перезапускалось (остановлено, неактивно, запущены оба). */
  outcome?: string;
  /** Имя копии, в которую переехал обычный config.yaml. */
  adopted?: string;
}

/** Что войдёт в новый профиль: шаблон (kind template) или копия (copy_panel, copy_user). */
export interface ProfilePreview {
  kind: 'template' | 'copy_panel' | 'copy_user';
  sources?: number;
  nodes?: number;
  no_sources?: boolean;
  added?: string[];
  redir?: number;
  tproxy?: number;
  unix_unsupported?: boolean;
}

/** Запрос создания: ровно один источник — шаблон (файл), копия (имя) или пустой файл. */
export interface CreateProfileRequest {
  name: string;
  template?: string;
  from?: string;
  empty?: boolean;
}

/** Запрос предпросмотра: файл шаблона или имя копируемого профиля. */
export interface PreviewQuery {
  template?: string;
  from?: string;
}

/** Профили, ссылающиеся на файл источника (source) или на любой файл панели (all). */
export interface DependentsQuery {
  source?: string;
  all?: boolean;
}

/** Запрос заполнения секций: ровно один источник — файл шаблона или имя профиля. */
export interface FillSectionsRequest {
  template?: string;
  profile?: string;
}

/** Действие перехода: активировать свой профиль, отпустить Mihomo, вернуть панели. */
export type MihomoOwnerAction = 'activate' | 'release' | 'return';

/**
 * Шаблоны, из которых панель создаёт профиль, в порядке показа. Файлы — те же, что у
 * серверного списка шаблонов профиля; идентификатор — имя файла без расширения, по нему
 * строятся ключи cfg.prof.tpl.<id>.name и .desc.
 */
export const PROFILE_TEMPLATES: readonly { id: string; file: string }[] = [
  'rule-based',
  'global-proxy',
  'zkeen',
  'smart-antifilter',
  'split-services',
  'low-memory',
  'gaming-low-latency',
  'full-tunnel'
].map((id) => ({ id, file: `${id}.yaml` }));

// --- Запросы ---

const BASE = '/api/mihomo/profiles';

/** Тело POST без пустых полей: незаданный источник на сервер не уходит. */
function body(fields: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(fields)) {
    if (v !== undefined && v !== '' && v !== false) out[k] = v;
  }
  return out;
}

export function fetchProfiles(): Promise<ProfilesState> {
  return apiFetchJSON<ProfilesState>(BASE);
}

/** Создаёт профиль; ответ — обновлённый список. Ошибки несут code (profile_exists и др.). */
export function createProfile(req: CreateProfileRequest): Promise<ProfilesState> {
  return post<ProfilesState>(`${BASE}/create`, body({ ...req }));
}

export function renameProfile(name: string, newName: string): Promise<ProfilesState> {
  return post<ProfilesState>(`${BASE}/rename`, { name, new_name: newName });
}

export function deleteProfile(name: string): Promise<ProfilesState> {
  return post<ProfilesState>(`${BASE}/delete`, { name });
}

/** Активирует профиль; маршрут сам выбирает путь по владельцу Mihomo (черновик — draft_open). */
export function activateProfile(name: string): Promise<ActivationResult> {
  return post<ActivationResult>(`${BASE}/activate`, { name });
}

/** Что войдёт в профиль: подстановка шаблона либо копия; диск не меняется. */
export function previewProfile(q: PreviewQuery): Promise<ProfilePreview> {
  const qs = new URLSearchParams();
  if (q.template) qs.set('template', q.template);
  if (q.from) qs.set('from', q.from);
  return apiFetchJSON<ProfilePreview>(`${BASE}/preview?${qs.toString()}`);
}

/** Профили пользователя, чьи ссылки на файлы панели пропадут; только чтение для предупреждения. */
export async function fetchDependentProfiles(q: DependentsQuery): Promise<string[]> {
  const qs = new URLSearchParams();
  if (q.source) qs.set('source', q.source);
  if (q.all) qs.set('all', '1');
  const res = await apiFetchJSON<{ profiles?: string[] }>(`${BASE}/deps?${qs.toString()}`);
  return Array.isArray(res?.profiles) ? res.profiles : [];
}

/**
 * Отпускает Mihomo, возвращает его панели или активирует свой профиль одним синхронным
 * применением; затрагивает только Mihomo. Ответ приходит после перезапуска ядра.
 */
export function mihomoOwnerAction(
  action: MihomoOwnerAction,
  profile = ''
): Promise<ActivationResult> {
  return post<ActivationResult>('/api/configlayer/routing/mihomo', body({ action, profile }));
}

/** Заполняет секции профиля панели значениями шаблона или профиля; значения — в черновик. */
export async function fillSections(req: FillSectionsRequest): Promise<DraftAck> {
  const ack = await post<DraftAck>('/api/configlayer/cores/fill', {
    revision: currentRevision(),
    ...body({ ...req })
  });
  applyDraftAck(ack);
  return ack;
}

// --- Тексты исходов ---

/**
 * Тост по итогу активации или перехода: успех — «Активен профиль»; запись без перезапуска
 * (ядро остановлено, неактивно, конфликт) — по исходу применения; не запустился — с откатом
 * и действием «Логи»; не прошёл проверку ядром — «не прошёл проверку».
 */
export function switchOutcomeToast(res: ActivationResult, name: string): void {
  const tr = get(t);
  if (res.error && res.rolled_back) {
    showToast('error', tr('cfg.prof.rolled_back', { name, reason: res.error }), 10000, {
      label: tr('apply.open_logs'),
      onClick: () => {
        window.location.hash = '#/logs';
      }
    });
    return;
  }
  if (res.error) {
    // «Не прошёл проверку ядром» — только для отказа проверки; прочие отказы
    // конвейера (дрейф, занятый замок) называют свою причину.
    const key =
      !res.code || res.code === 'validation_failed' ? 'profiles.invalid' : 'cfg.prof.switch_failed';
    showToast('error', tr(key, { error: res.error }), 10000);
    return;
  }
  if (
    res.outcome === 'saved_kernel_stopped' ||
    res.outcome === 'saved_kernel_inactive' ||
    res.outcome === 'saved_kernel_conflict'
  ) {
    notifyApplyOutcome({
      outcome: res.outcome as ApplyOutcome,
      kernel: 'mihomo',
      active_kernel: '',
      active_running: false
    });
    return;
  }
  showToast('success', tr('profiles.activated', { name: res.active || name }));
}

/** Причина отказа действия с профилем: по коду ответа, иначе текст сервера. */
export function profileErrorText(tr: TFn, err: unknown, name = ''): string {
  const e = err as { code?: string; params?: Record<string, string>; message?: string } | null;
  switch (e?.code) {
    case 'profile_reserved':
      return tr('cfg.prof.err_reserved');
    case 'profile_exists':
      return tr('cfg.prof.err_exists', { name: e?.params?.name ?? name });
    case 'profile_return_target':
      return tr('cfg.prof.err_prev_target');
    case 'draft_open':
      return tr('cfg.prof.draft_block');
    case 'panel_absent':
      return tr('cfg.prof.copy_panel_off');
    default:
      return typeof e?.message === 'string' && e.message !== '' ? e.message : String(err);
  }
}
