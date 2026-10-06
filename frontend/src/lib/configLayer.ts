// Клиент слоя «Конфигурация»: типы контракта API, стор состояния с одним
// SSE-соединением на вкладку, редьюсер событий и клиент маршрутов /api/configlayer/*.
// Состояние (черновик, файлы, применение) живёт на сервере; здесь — только его
// отражение. Номер ревизии черновика пользователю не показывается (D-05).

import { writable, derived, get, type Readable } from 'svelte/store';
import { apiFetchJSON } from './api';
import { showToast } from '../stores';
import { t } from '../i18n';

// --- Типы контракта (144-09) ---

export type FileState =
  | 'ok'
  | 'pending'
  | 'drift_modified'
  | 'drift_missing'
  | 'drift_renamed'
  | 'drift_empty'
  | 'released';

export interface LayerFile {
  key: string;
  kernel: 'xray' | 'mihomo';
  path: string;
  owner: 'panel' | 'manual';
  state: FileState;
  obsolete_name?: string;
  /** Другие пути, разрешающиеся в этот файл (config.yaml — симлинк на профиль панели). */
  alias_paths?: string[];
}

export interface LayerKernel {
  name: 'xkeen' | 'xray' | 'mihomo';
  installed: boolean;
  version: string;
  status: 'ok' | 'below_min' | 'undetermined' | 'not_installed';
  min_version: string;
}

export type StepId = 'build' | 'validate_xray' | 'validate_mihomo' | 'write' | 'restart';
export type StepState = 'pending' | 'running' | 'done' | 'failed' | 'skipped' | 'deferred';

export interface LayerStep {
  id: StepId;
  state: StepState;
  note_code?: string;
  message?: string;
}

export interface LayerRestart {
  kernel: 'xray' | 'mihomo';
  outcome:
    | 'restarted'
    | 'hot_reloaded'
    | 'restarted_after_reload_failed'
    | 'deferred'
    | 'untouched_inactive'
    | 'untouched_conflict'
    | 'failed_rolled_back'
    | 'failed_rollback_failed'
    | 'failed_kernel_down';
  note_code?: string;
}

export interface LayerIssue {
  key: string;
  code: string;
  detail?: string;
}

export interface LayerResult {
  ok: boolean;
  code: string;
  kernel?: string;
  message?: string;
  hint_code?: string;
  written: number;
  orphans_removed?: string[];
  rolled_back?: boolean;
  issues?: LayerIssue[];
}

export interface LayerApply {
  running: boolean;
  trigger?: 'user' | 'rebuild' | 'kernel_installed' | 'flag_on' | 'flag_off';
  steps: LayerStep[];
  restart?: LayerRestart[];
  result?: LayerResult;
}

export interface LayerNotice {
  id: string;
  kind: 'warning' | 'error' | 'info';
  kernel?: string;
  reason?: string;
}

export interface LayerSnapshot {
  enabled: boolean;
  dev_mode: boolean;
  draft_revision: number;
  draft_changes: number;
  drift_count: number;
  files: LayerFile[];
  kernels: LayerKernel[];
  features: Record<string, 'available' | 'unavailable' | 'undetermined'>;
  apply: LayerApply;
  notices: LayerNotice[];
}

export interface LayerDiff {
  key: string;
  expected: string;
  actual: string;
  missing: boolean;
  truncated: boolean;
}

// --- Сторы ---

export const layerSnapshot = writable<LayerSnapshot | null>(null);
export const layerStatus = writable<'idle' | 'loading' | 'ready' | 'error'>('idle');
export const layerConnection = writable<'connecting' | 'open' | 'lost'>('connecting');
/** Применение запущено из этой вкладки (отличает «эта вкладка» от «другая», D-16). */
export const applyStartedHere = writable(false);

export const draftChanges: Readable<number> = derived(layerSnapshot, (s) => s?.draft_changes ?? 0);
export const driftCount: Readable<number> = derived(layerSnapshot, (s) => s?.drift_count ?? 0);
export const applyRunning: Readable<boolean> = derived(
  layerSnapshot,
  (s) => s?.apply.running ?? false
);

/** Подпись бейджа меню: '' (скрыт) | '1'…'99' | '99+'. */
export function formatBadgeCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '';
  return n > 99 ? '99+' : String(Math.floor(n));
}

export const navBadgeCount: Readable<string> = derived(draftChanges, (n) => formatBadgeCount(n));

export const filesByPath: Readable<Map<string, LayerFile>> = derived(layerSnapshot, (s) => {
  const map = new Map<string, LayerFile>();
  for (const f of s?.files ?? []) {
    map.set(f.path, f);
    // Симлинк на файл панели открывается в Редакторе под своим путём, но защищён так же
    for (const alias of f.alias_paths ?? []) map.set(alias, f);
  }
  return map;
});

// --- Редьюсер событий ---

const STEP_ORDER: StepId[] = ['build', 'validate_xray', 'validate_mihomo', 'write', 'restart'];

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function asArray<T>(v: unknown): T[] {
  return Array.isArray(v) ? (v as T[]) : [];
}

function normalizeApply(raw: unknown): LayerApply {
  const a = isObject(raw) ? raw : {};
  const apply: LayerApply = {
    running: a.running === true,
    steps: asArray<LayerStep>(a.steps)
  };
  if (typeof a.trigger === 'string') apply.trigger = a.trigger as LayerApply['trigger'];
  if (Array.isArray(a.restart)) apply.restart = a.restart as LayerRestart[];
  if (isObject(a.result)) apply.result = a.result as unknown as LayerResult;
  return apply;
}

/** Приводит данные snapshot к полной форме: null и пропущенные поля становятся пустыми. */
function normalizeSnapshot(raw: Record<string, unknown>): LayerSnapshot {
  return {
    enabled: raw.enabled === true,
    dev_mode: raw.dev_mode === true,
    draft_revision: typeof raw.draft_revision === 'number' ? raw.draft_revision : 0,
    draft_changes: typeof raw.draft_changes === 'number' ? raw.draft_changes : 0,
    drift_count: typeof raw.drift_count === 'number' ? raw.drift_count : 0,
    files: asArray<LayerFile>(raw.files),
    kernels: asArray<LayerKernel>(raw.kernels),
    features: isObject(raw.features) ? (raw.features as LayerSnapshot['features']) : {},
    apply: normalizeApply(raw.apply),
    notices: asArray<LayerNotice>(raw.notices)
  };
}

function freshSteps(): LayerStep[] {
  return STEP_ORDER.map((id) => ({ id, state: 'pending' as StepState }));
}

/**
 * Чистый редьюсер события SSE. snapshot заменяет всё; прочие события правят
 * свою часть; неизвестное событие, битые данные или отсутствие состояния
 * (кроме snapshot) оставляют его без изменений.
 */
export function reduceLayerEvent(
  state: LayerSnapshot | null,
  name: string,
  data: unknown
): LayerSnapshot | null {
  if (name === 'snapshot') {
    return isObject(data) ? normalizeSnapshot(data) : state;
  }
  if (!state || !isObject(data)) return state;

  switch (name) {
    case 'draft': {
      if (typeof data.draft_revision !== 'number' || typeof data.draft_changes !== 'number') {
        return state;
      }
      return { ...state, draft_revision: data.draft_revision, draft_changes: data.draft_changes };
    }
    case 'files': {
      return {
        ...state,
        files: asArray<LayerFile>(data.files),
        drift_count: typeof data.drift_count === 'number' ? data.drift_count : state.drift_count
      };
    }
    case 'apply_step': {
      if (typeof data.id !== 'string' || typeof data.state !== 'string') return state;
      const step = data as unknown as LayerStep;
      // Шаг после завершённого (или не начатого) запуска — это начало нового
      const base: LayerApply = state.apply.running
        ? state.apply
        : { running: true, trigger: undefined, steps: freshSteps() };
      const steps = base.steps.some((s) => s.id === step.id)
        ? base.steps.map((s) => (s.id === step.id ? { ...s, ...step } : s))
        : [...base.steps, step];
      return { ...state, apply: { ...base, running: true, steps } };
    }
    case 'apply_done': {
      return { ...state, apply: normalizeApply(data) };
    }
    case 'notices': {
      return { ...state, notices: asArray<LayerNotice>(data.notices) };
    }
    default:
      return state;
  }
}

// --- Загрузка состояния и SSE ---

const EVENT_NAMES = ['snapshot', 'draft', 'files', 'apply_step', 'apply_done', 'notices'] as const;
const RETRY_MIN_MS = 3000;
const RETRY_MAX_MS = 30000;

let source: EventSource | null = null;
let started = false;
let retryTimer: ReturnType<typeof setTimeout> | null = null;
let retryDelay = RETRY_MIN_MS;
// Растёт с каждым применённым событием SSE: ответ GET, начатый до события, не затирает его
let eventSeq = 0;

/** Флаг «запущено здесь» не переживает снимок, в котором применение не идёт (потерянный apply_done). */
function syncStartedHere(): void {
  if (get(layerSnapshot)?.apply.running === false) applyStartedHere.set(false);
}

function applyEvent(name: string, data: unknown): void {
  eventSeq++;
  layerSnapshot.update((s) => reduceLayerEvent(s, name, data));
  if (name === 'snapshot') {
    layerStatus.set('ready');
    syncStartedHere();
  }
  if (name === 'apply_done') applyStartedHere.set(false);
}

function setSnapshot(raw: unknown): void {
  if (!isObject(raw)) return;
  layerSnapshot.set(normalizeSnapshot(raw));
  layerStatus.set('ready');
  syncStartedHere();
}

/** Читает снимок целиком. Сбой первой загрузки — статус error; позже состояние остаётся прежним. */
export async function refetchLayerState(): Promise<void> {
  if (get(layerSnapshot) === null && get(layerStatus) !== 'loading') layerStatus.set('loading');
  const seq = eventSeq;
  try {
    const data = await apiFetchJSON<Record<string, unknown>>('/api/configlayer/state');
    if (get(layerSnapshot) === null || seq === eventSeq) setSnapshot(data);
    else layerStatus.set('ready');
  } catch {
    if (get(layerSnapshot) === null) layerStatus.set('error');
  }
}

function scheduleReconnect(): void {
  if (!started || retryTimer !== null) return;
  retryTimer = setTimeout(() => {
    retryTimer = null;
    if (started) connect();
  }, retryDelay);
  retryDelay = Math.min(retryDelay * 2, RETRY_MAX_MS);
}

function connect(): void {
  if (!started || source) return;
  const es = new EventSource('/api/configlayer/events');
  source = es;
  for (const name of EVENT_NAMES) {
    es.addEventListener(name, (e: Event) => {
      if (source !== es) return;
      try {
        applyEvent(name, JSON.parse((e as MessageEvent).data));
      } catch {
        // битое событие пропускаем: следующий snapshot или GET восстановит состояние
      }
    });
  }
  es.onopen = () => {
    if (source !== es) return;
    const wasLost = get(layerConnection) === 'lost';
    retryDelay = RETRY_MIN_MS;
    layerConnection.set('open');
    // Пока связи не было, события могли пройти мимо — читаем состояние целиком (D-06)
    if (wasLost) void refetchLayerState();
  };
  es.onerror = () => {
    if (source !== es) return;
    layerConnection.set('lost');
    // Соединение закрыто окончательно (ошибка HTTP) — переподключаемся сами;
    // в состоянии CONNECTING браузер повторяет попытку без нашего участия
    if (es.readyState === 2) {
      es.close();
      source = null;
      scheduleReconnect();
    }
  };
}

/** Запускает загрузку состояния и одно SSE-соединение на вкладку. Повторный вызов ничего не делает. */
export function startConfigLayer(): void {
  if (started) return;
  started = true;
  retryDelay = RETRY_MIN_MS;
  layerStatus.set('loading');
  layerConnection.set('connecting');
  void refetchLayerState().then(() => {
    if (started) connect();
  });
}

/** Закрывает соединение и таймеры, сбрасывает состояние. */
export function stopConfigLayer(): void {
  started = false;
  if (retryTimer !== null) {
    clearTimeout(retryTimer);
    retryTimer = null;
  }
  if (source) {
    source.close();
    source = null;
  }
  retryDelay = RETRY_MIN_MS;
  layerSnapshot.set(null);
  layerStatus.set('idle');
  layerConnection.set('connecting');
  applyStartedHere.set(false);
}

// --- Клиент маршрутов ---

interface DraftAck {
  draft_revision: number;
  draft_changes: number;
}

function post<T>(path: string, body: unknown): Promise<T> {
  return apiFetchJSON<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  });
}

function currentRevision(): number {
  return get(layerSnapshot)?.draft_revision ?? 0;
}

function applyDraftAck(ack: DraftAck): void {
  layerSnapshot.update((s) => reduceLayerEvent(s, 'draft', ack));
}

/** Правка секции черновика. Ошибки (в т. ч. draft_conflict) пробрасываются: разбирает handleLayerError. */
export async function editDraft(section: string, value: unknown): Promise<void> {
  const ack = await post<DraftAck>('/api/configlayer/draft', {
    revision: currentRevision(),
    section,
    value
  });
  applyDraftAck(ack);
}

export async function resetDraft(): Promise<void> {
  const ack = await post<DraftAck>('/api/configlayer/draft/reset', {
    revision: currentRevision()
  });
  applyDraftAck(ack);
}

/**
 * Запуск применения. Флаг «запущено здесь» ставится до запроса: итог может
 * прийти по SSE раньше ответа, и тогда apply_done уже сбросил бы флаг.
 */
async function startRun(path: string, body: unknown): Promise<void> {
  applyStartedHere.set(true);
  try {
    await post<{ started: boolean }>(path, body);
  } catch (err) {
    applyStartedHere.set(false);
    throw err;
  }
}

export function applyNow(): Promise<void> {
  return startRun('/api/configlayer/apply', {});
}

export function rebuildFiles(keys: string[] | 'all'): Promise<void> {
  return startRun('/api/configlayer/files/rebuild', keys === 'all' ? { all: true } : { keys });
}

export async function releaseFile(key: string): Promise<void> {
  const res = await post<{ files?: LayerFile[]; drift_count?: number }>(
    '/api/configlayer/files/release',
    { key }
  );
  layerSnapshot.update((s) =>
    reduceLayerEvent(s, 'files', { files: res.files ?? [], drift_count: res.drift_count })
  );
}

export function fetchDiff(key: string): Promise<LayerDiff> {
  return apiFetchJSON<LayerDiff>(`/api/configlayer/files/diff?key=${encodeURIComponent(key)}`);
}

export async function dismissNotice(id: string): Promise<void> {
  const res = await post<{ notices?: LayerNotice[] }>('/api/configlayer/notices/dismiss', { id });
  layerSnapshot.update((s) => reduceLayerEvent(s, 'notices', { notices: res.notices ?? [] }));
}

export async function diagAction(
  action: 'add' | 'add_broken_xray' | 'add_broken_mihomo' | 'remove'
): Promise<void> {
  const ack = await post<DraftAck>('/api/configlayer/diag', {
    revision: currentRevision(),
    action
  });
  applyDraftAck(ack);
}

/**
 * Единый разбор ошибок слоя: draft_conflict — предупреждение и тихое
 * перечитывание (D-05); apply_busy — информационный тост (D-16); прочее —
 * тост с текстом сервера (только текст, без разметки).
 */
export function handleLayerError(err: unknown): void {
  const code = isObject(err) || err instanceof Error ? (err as { code?: unknown }).code : undefined;
  const tr = get(t);
  if (code === 'draft_conflict') {
    showToast('warning', tr('cfg.toast.draft_conflict'), 6000);
    void refetchLayerState();
    return;
  }
  if (code === 'apply_busy') {
    showToast('info', tr('cfg.toast.apply_busy'));
    return;
  }
  if (code === 'file_managed') {
    // Редактор мог показать файл как свободный: перечитываем состояние слоя, и баннер
    // «Отпустить управление» появится
    showToast('warning', err instanceof Error ? err.message : tr('editor.managed_readonly_hint'));
    void refetchLayerState();
    return;
  }
  showToast('error', err instanceof Error ? err.message : String(err));
}
