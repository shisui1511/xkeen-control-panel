// Клиент источников узлов (вкладка «Узлы» раздела «Конфигурация»): типы ответов API
// /api/configlayer/sources*, стор представления и загрузка. Поля — snake_case, как у
// сервера. Ключи объектов preview и refresh совпадают с frontend/src/lib/sourcesContract.json:
// сверку выполняет scripts/check-config-ui.js (необязательные поля — `?`).

import { writable, get } from 'svelte/store';
import { apiFetchJSON } from './api';
import { onLayerEvent, post, currentRevision, applyDraftAck, type DraftAck } from './configLayer';

// --- Типы контракта ---

export type KernelName = 'xray' | 'mihomo';

/** Ошибка ввода или ответа провайдера: код и параметры, текст собирает фронтенд (D-31). */
export interface SourceErrorPayload {
  code: string;
  params?: Record<string, string>;
  http_status?: number;
  line?: number;
  fragment?: string;
  detail?: string;
}

/** Вердикт матрицы возможностей: выражается ли узел в ядре. */
export interface VerdictView {
  ok: boolean;
  code?: string;
  params?: Record<string, string>;
  notes?: string[];
}

export interface SupportPair {
  xray: VerdictView;
  mihomo: VerdictView;
}

export interface KernelCounts {
  xray: number;
  mihomo: number;
}

export interface KernelFlags {
  xray: boolean;
  mihomo: boolean;
}

export interface PreviewNode {
  key: string;
  name: string;
  protocol: string;
  address: string;
  cred_mask: string;
  support: SupportPair;
  marks: string[];
  dup_sub_name?: string;
}

export interface PreviewMyNodes {
  nodes: PreviewNode[];
  count: number;
  only: KernelCounts;
  skipped: number;
}

export interface PreviewLineError {
  line: number;
  file?: string;
  fragment: string;
  error: SourceErrorPayload;
}

export interface PreviewKernel {
  installed: boolean;
  count: number;
  stubs: number;
  skipped: number;
  error?: SourceErrorPayload;
}

export interface PreviewSubscription {
  line: number;
  name: string;
  url_masked: string;
  kernels: Record<string, PreviewKernel>;
  warnings: SourceErrorPayload[];
  error?: SourceErrorPayload;
}

export interface PreviewView {
  token: string;
  expires_at: string;
  subscriptions: PreviewSubscription[];
  my_nodes: PreviewMyNodes | null;
  errors: PreviewLineError[];
  addable: number;
  nodes_only: boolean;
  collapsed: number;
}

export interface KernelOutcome {
  status: string;
  count: number;
  error_code?: string;
}

export type RefreshOutcomeKind =
  'unchanged' | 'applied' | 'rejected' | 'network' | 'deferred' | 'failed' | 'pending_updated';

export interface RefreshOutcome {
  outcome: RefreshOutcomeKind;
  added: number;
  removed: number;
  kernels: Record<string, KernelOutcome>;
  error?: SourceErrorPayload;
}

export interface StatusView {
  code: string;
  error_code?: string;
  http?: number;
  since: string;
  retry_at?: string;
  attempts?: number;
  params?: Record<string, string>;
}

export interface KernelStateView {
  count: number;
  status: StatusView;
  snapshot_at?: string;
  delta?: { added: number; removed: number };
  updated_at?: string;
  pending: boolean;
  stopped: boolean;
}

export interface NodeView {
  key: string;
  ref: string;
  entry_id?: string;
  name: string;
  protocol: string;
  address: string;
  cred_mask: string;
  support: SupportPair;
  in_set: KernelFlags;
  excluded: boolean;
  dup_in?: string[];
}

export interface MetaView {
  title?: string;
  upload?: number;
  download?: number;
  total?: number;
  expire?: number;
  interval_h?: number;
  support_url?: string;
  web_url?: string;
  announce?: string;
  hwid_locked?: boolean;
  checked_at?: string;
}

export interface SourceInterval {
  mode: string;
  hours?: number;
}

export interface SourceSettingsView {
  exclude: string[];
  interval: SourceInterval;
  hwid_custom: boolean;
}

export type DraftState = 'applied' | 'added' | 'changed';

export interface SourceView {
  id: string;
  kind: 'subscription' | 'my';
  name: string;
  enabled: boolean;
  draft_state: DraftState;
  url_masked?: string;
  host?: string;
  meta?: MetaView;
  settings?: SourceSettingsView;
  kernels: Partial<Record<KernelName, KernelStateView>>;
  nodes: NodeView[];
  stubs: number;
  skipped: number;
  unsupported: number;
}

export interface SourcesView {
  draft_revision: number;
  installed: KernelFlags;
  running: KernelFlags;
  default_kernel: string;
  sources: SourceView[];
}

export interface DiagView {
  kernel: string;
  empty: boolean;
  fetched_at?: string;
  http_status?: number;
  headers: { name: string; value: string }[];
  body: string;
  truncated: boolean;
  report: {
    parsed: number;
    stubs: number;
    skipped: unknown[];
  };
}

export interface NodeRaw {
  entry_id: string;
  name: string;
  format: string;
  raw: string;
}

/** Правка источника: передаются только изменяемые поля. */
export interface SourcePatch {
  name?: string;
  enabled?: boolean;
  exclude?: string[];
  interval?: SourceInterval;
  hwid?: string;
  url?: string;
}

// --- Сторы ---

export const sourcesView = writable<SourcesView | null>(null);
export const sourcesStatus = writable<'idle' | 'loading' | 'ready' | 'error'>('idle');

// Растёт с каждым запросом: ответ устаревшего запроса не затирает более свежий
let loadSeq = 0;

/**
 * Читает представление источников. Первая загрузка — статус loading; последующие
 * (по событию SSE) идут молча, без мигания скелетона. Сбой при уже загруженном
 * представлении состояние не портит.
 */
export async function loadSources(): Promise<void> {
  const seq = ++loadSeq;
  if (get(sourcesView) === null) sourcesStatus.set('loading');
  try {
    const data = await apiFetchJSON<SourcesView>('/api/configlayer/sources');
    if (seq !== loadSeq) return;
    sourcesView.set({ ...data, sources: Array.isArray(data.sources) ? data.sources : [] });
    sourcesStatus.set('ready');
  } catch {
    if (seq !== loadSeq) return;
    if (get(sourcesView) === null) sourcesStatus.set('error');
  }
}

// Событие SSE sources — представление изменилось (фоновое обновление, отложенная запись):
// перечитываем с дебаунсом, без второго потока событий. Пока представление ни разу не
// загружалось, перечитывать нечего. configLayer этот модуль не импортирует — цикла нет.
const SOURCES_DEBOUNCE_MS = 300;
let sourcesTimer: ReturnType<typeof setTimeout> | null = null;

onLayerEvent('sources', () => {
  if (get(sourcesView) === null) return;
  if (sourcesTimer !== null) clearTimeout(sourcesTimer);
  sourcesTimer = setTimeout(() => {
    sourcesTimer = null;
    void loadSources();
  }, SOURCES_DEBOUNCE_MS);
});

// --- Клиент маршрутов ---

export interface PreviewFile {
  name: string;
  content: string;
}

/** Итог добавления: событие черновика и число добавленного. */
export interface AddResult extends DraftAck {
  subscriptions: number;
  nodes: number;
}

const BASE = '/api/configlayer/sources';

function postJSON<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  return apiFetchJSON<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal
  });
}

/**
 * Правка черновика через маршрут источников: ревизия берётся из снимка слоя, по ответу
 * обновляются ревизия и счётчик изменений, представление источников перечитывается.
 * Ошибки (в т. ч. draft_conflict с code) пробрасываются: разбирает handleLayerError.
 */
async function editSources<T extends DraftAck>(
  path: string,
  body: Record<string, unknown>
): Promise<T> {
  const ack = await post<T>(`${BASE}/${path}`, { revision: currentRevision(), ...body });
  applyDraftAck(ack);
  void loadSources();
  return ack;
}

/** Предпросмотр ввода поля «Добавить»; повторный ввод отменяет прошлый запрос через signal. */
export function previewInput(
  text: string,
  files: PreviewFile[],
  signal?: AbortSignal
): Promise<PreviewView> {
  return postJSON<PreviewView>(`${BASE}/preview`, { text, files }, signal);
}

/** «Добавить»: кладёт результат предпросмотра в черновик. */
export function addPreview(token: string): Promise<AddResult> {
  return editSources<AddResult>('add', { token });
}

export function updateSource(id: string, patch: SourcePatch): Promise<DraftAck> {
  return editSources<DraftAck>('update', { id, patch });
}

export function deleteSource(id: string): Promise<DraftAck> {
  return editSources<DraftAck>('delete', { id });
}

/** Ручное обновление подписки; ответ приходит после запроса к провайдеру. */
export function refreshSource(id: string): Promise<RefreshOutcome> {
  return post<RefreshOutcome>(`${BASE}/refresh`, { id });
}

/** Полный адрес подписки по явному действию «показать». */
export async function revealUrl(id: string): Promise<string> {
  const res = await post<{ url: string }>(`${BASE}/reveal`, { id });
  return res.url;
}

export function fetchDiagnostics(id: string, kernel: KernelName): Promise<DiagView> {
  return apiFetchJSON<DiagView>(
    `${BASE}/diagnostics?id=${encodeURIComponent(id)}&kernel=${encodeURIComponent(kernel)}`
  );
}

/** Предпросмотр правки одного узла «Моих узлов». */
export function previewNode(
  text: string,
  entryId?: string,
  signal?: AbortSignal
): Promise<PreviewView> {
  const body = entryId ? { text, entry_id: entryId } : { text };
  return postJSON<PreviewView>(`${BASE}/nodes/preview`, body, signal);
}

/** Исходный текст узла целиком (секреты открыты — только по явному действию). */
export function nodeRaw(entryId: string): Promise<NodeRaw> {
  return post<NodeRaw>(`${BASE}/nodes/raw`, { entry_id: entryId });
}

/** Сохраняет узел «Моих узлов»; без entryId — новый. */
export function saveNode(
  entryId: string | undefined,
  name: string,
  text: string
): Promise<DraftAck> {
  return editSources<DraftAck>(
    'nodes/save',
    entryId ? { entry_id: entryId, name, text } : { name, text }
  );
}

export function deleteNode(entryId: string): Promise<DraftAck> {
  return editSources<DraftAck>('nodes/delete', { entry_id: entryId });
}
