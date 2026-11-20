// Контракт и клиент страницы «Прокси»: модуль подключается только страницей «Прокси»
// и components/proxies/*. Поля совпадают с routingContract.json и DTO Go.
// При импорте модуль не делает запросов и не подписывается на события: подписку
// включает attachProxiesLive() страницы, запросы — её функции загрузки.
import { writable, get } from 'svelte/store';
import { apiFetchJSON } from './api';
import { onLayerEvent, post } from './configLayer';
import routingErrorCodes from './routingErrorCodes.json';
// Только типы: клиент маршрутизации вне раздела «Конфигурация» не подключается
import type { KernelCounts, ProbeConfig, SelectResult } from './configRouting';

export const PROXIES_BASE = '/api/configlayer/proxies';

export type ProxyKernel = 'xray' | 'mihomo';
export type RuntimeKernel = ProxyKernel | 'none' | '';
export type RuntimeMode = 'panel' | 'user_managed' | 'user_profile' | 'not_installed';
export type PingMethod = 'xray' | 'mihomo' | 'tls';
export type PingStatus = 'ok' | 'timeout' | 'error';

export interface RuntimeView {
  kernel: RuntimeKernel;
  running: ProxyKernel[];
  state: 'panel' | 'stopped' | 'user_profile' | 'user_managed' | 'conflict';
  no_socket: boolean;
  modes: RuntimeModes;
  probe: ProbeConfig;
  groups: RuntimeGroup[];
}

export interface RuntimeModes {
  xray: RuntimeMode;
  mihomo: RuntimeMode;
}

export interface RuntimeGroup {
  id: string;
  name: string;
  mode: 'manual' | 'auto' | 'fallback' | 'source';
  current: string;
  current_label: string;
  chain: RuntimeLink[];
  fallback?: string;
  selectable: boolean;
  unconfirmed: boolean;
  /** Идёт выбор или «Повторить» в этой группе или связанной (на сервере, из любой вкладки, D-17). */
  switching: boolean;
  locked_with: string[];
  counts: KernelCounts;
  members: RuntimeMember[];
}

export interface RuntimeLink {
  kind: 'group' | 'node' | 'direct' | 'reject';
  id?: string;
  ref?: string;
  name: string;
}

export interface RuntimeMember {
  member: string;
  kind: 'group' | 'node' | 'direct' | 'reject';
  ref?: string;
  group?: string;
  name: string;
  proto?: string;
  source?: string;
  /** Имя источника узла: заголовок окна диагностики источника. */
  source_name?: string;
  xray: boolean;
  mihomo: boolean;
}

export interface RuntimeNow {
  kernel: RuntimeKernel;
  groups: Record<string, GroupNow>;
  history: Record<string, NowDelay>;
}

export interface GroupNow {
  ref?: string;
  known: boolean;
  fastest?: boolean;
}

export interface NowDelay {
  ms: number;
  at: string;
}

export interface PingSnapshot {
  batch?: PingBatch;
  results: Record<string, PingResult>;
}

export interface PingBatch {
  id: string;
  state: 'running' | 'done' | 'cancelled' | 'kernel_changed' | 'failed';
  scope: 'all' | 'group' | 'repick';
  group?: string;
  kernel: RuntimeKernel;
  method: PingMethod;
  total: number;
  done: number;
  excluded: number;
  fallback_reason?: 'no_xray_binary' | 'low_memory' | 'xray_rejected';
  reason_code?: string;
  started_at: string;
  finished_at?: string;
}

export interface PingResult {
  ms: number;
  status: PingStatus;
  reason?: string;
  at: string;
  kernel: RuntimeKernel;
  method: PingMethod;
  history: PingPoint[];
}

export interface PingPoint {
  ms: number;
  status: PingStatus;
  at: string;
}

export interface PingStart {
  batch_id: string;
  joined: boolean;
  batch: PingBatch;
}

export interface ConfirmResult {
  group: string;
  confirmed: boolean;
}

// --- Сторы страницы ---

/** Вид работающих групп; null — ещё не загружен. */
export const runtimeView = writable<RuntimeView | null>(null);
/** Код ошибки последней загрузки вида ('' — ошибки нет). */
export const runtimeLoadError = writable<string>('');
/** Последняя пачка пинга и результаты по ссылкам узлов; null — ещё не загружены. */
export const pingSnapshot = writable<PingSnapshot | null>(null);
/** Текущие цели авто- и резервных групп и история Mihomo; null — ещё не загружены. */
export const runtimeNow = writable<RuntimeNow | null>(null);
/** Ядро перезапускается (опрос «Сейчас» получил 409 kernel_restarting): не ошибка. */
export const runtimeRestarting = writable<boolean>(false);

// --- Загрузка ---

/** Код ошибки запроса: code из конверта, иначе текст ошибки. */
function errorCode(e: unknown): string {
  const err = e as { code?: unknown; message?: unknown } | null;
  if (err && typeof err.code === 'string' && err.code !== '') return err.code;
  if (err && typeof err.message === 'string' && err.message !== '') return err.message;
  return 'unknown';
}

// Go отдаёт пустой срез как null: интерфейс получает массивы всегда
function normalizeView(v: RuntimeView): RuntimeView {
  return {
    ...v,
    running: v.running ?? [],
    no_socket: v.no_socket ?? false,
    groups: (v.groups ?? []).map((g) => ({
      ...g,
      chain: g.chain ?? [],
      locked_with: g.locked_with ?? [],
      members: g.members ?? [],
      switching: g.switching ?? false
    }))
  };
}

let runtimeSeq = 0;

/** Читает вид работающих групп; устаревший ответ (после более позднего запроса) отбрасывается. */
export async function loadRuntime(): Promise<void> {
  const seq = ++runtimeSeq;
  try {
    const data = await apiFetchJSON<RuntimeView>(PROXIES_BASE);
    if (seq !== runtimeSeq) return;
    runtimeView.set(normalizeView(data));
    runtimeLoadError.set('');
  } catch (e) {
    if (seq !== runtimeSeq) return;
    runtimeLoadError.set(errorCode(e));
  }
}

let resultsSeq = 0;

/** Читает последнюю пачку и результаты пинга; при ошибке прежние результаты остаются. */
export async function loadResults(): Promise<void> {
  const seq = ++resultsSeq;
  try {
    const data = await apiFetchJSON<PingSnapshot>(`${PROXIES_BASE}/results`);
    if (seq !== resultsSeq) return;
    pingSnapshot.set({ ...data, results: data.results ?? {} });
  } catch {
    // Результаты прежних замеров не стираются: следующее событие перечитает их снова
  }
}

/**
 * Читает «Сейчас» авто- и резервных групп (опрос страницы). 409 kernel_restarting —
 * признак runtimeRestarting без ошибки; прочие ошибки пробрасываются опросу (пауза).
 */
let nowSeq = 0;

export async function loadNow(signal?: AbortSignal): Promise<void> {
  const seq = ++nowSeq;
  try {
    const data = await apiFetchJSON<RuntimeNow>(`${PROXIES_BASE}/now`, { signal });
    // Поздний ответ не перекрывает более новый и сброс после смены ядра
    if (seq !== nowSeq) return;
    runtimeNow.set({ ...data, groups: data.groups ?? {}, history: data.history ?? {} });
    runtimeRestarting.set(false);
  } catch (e) {
    if (seq !== nowSeq) return;
    if ((e as { code?: unknown } | null)?.code === 'kernel_restarting') {
      runtimeRestarting.set(true);
      return;
    }
    throw e;
  }
}

/**
 * Сбрасывает «Сейчас» и историю ядра: после остановки или смены ядра и при входе на
 * страницу последний известный выбор не показывается (D-26). Идущий ответ отбрасывается.
 */
export function resetNow(): void {
  nowSeq++;
  runtimeNow.set(null);
}

// --- Действия ---

/**
 * Выбор участника ручной группы — тот же единый маршрут выбора, что и раньше (D-12):
 * ответ приходит после исхода на ядрах.
 */
export async function selectNode(group: string, member: string): Promise<SelectResult> {
  return post<SelectResult>('/api/configlayer/routing/select', { group, member });
}

/** Запуск пачки пинга: всех групп или одной; повтор во время пачки присоединяется к ней. */
export function startPing(scope: 'all' | 'group', group?: string): Promise<PingStart> {
  return post<PingStart>(
    `${PROXIES_BASE}/ping`,
    scope === 'group' ? { scope, group: group ?? '' } : { scope }
  );
}

/** Отмена общей пачки (из любой вкладки); готовые результаты остаются. */
export function cancelPing(): Promise<PingBatch> {
  return post<PingBatch>(`${PROXIES_BASE}/ping/cancel`, {});
}

/** Повторное подтверждение активного узла группы с неподтверждённым выбором (D-16). */
export function confirmGroup(group: string): Promise<ConfirmResult> {
  return post<ConfirmResult>(`${PROXIES_BASE}/confirm`, { group });
}

// --- Тексты ошибок ---

/** Функция перевода из стора $t. */
export type ProxiesTFn = (key: string, params?: Record<string, string | number>) => string;

const KNOWN_CODES = new Set<string>(routingErrorCodes);

// Параметры текстов ошибок: не пришедший параметр — «?», а не голое «{field}»
const ERROR_PARAMS = ['field', 'reason', 'group', 'file', 'kernel', 'name', 'tag'];

/** Текст ошибки по коду: cfg.routing.err.<код>, неизвестный код — cfg.routing.err.unknown. */
export function proxiesErrorText(
  t: ProxiesTFn,
  code: string | undefined,
  params?: Record<string, string>
): string {
  const key =
    code !== undefined && KNOWN_CODES.has(code)
      ? `cfg.routing.err.${code}`
      : 'cfg.routing.err.unknown';
  const p: Record<string, string> = {};
  for (const name of ERROR_PARAMS) p[name] = '?';
  for (const [k, v] of Object.entries(params ?? {})) {
    if (typeof v === 'string' && v !== '') p[k] = v;
  }
  return t(key, p);
}

// --- Живое обновление (D-14) ---

/** Дебаунс перечитывания по событиям слоя. */
export const PROXIES_DEBOUNCE_MS = 300;

/** Вид события proxies: select, structure, ping или batch; без данных — ''. */
function eventKind(data: unknown): string {
  const kind = (data as { kind?: unknown } | null | undefined)?.kind;
  return typeof kind === 'string' ? kind : '';
}

/**
 * Подписка страницы на события слоя через общий поток SSE (второго потока нет).
 * proxies: выбор и состав — перечитать вид, пинг и пачка — результаты, без данных
 * (восстановление связи) — оба; routing и sources — вид. Перечитывание с дебаунсом
 * 300 мс; событие до первой загрузки вида откладывается до её конца (первое чтение
 * могло уйти раньше события). Возвращает отписку для ухода со страницы.
 */
export function attachProxiesLive(): () => void {
  let viewTimer: ReturnType<typeof setTimeout> | null = null;
  let resultsTimer: ReturnType<typeof setTimeout> | null = null;
  // Событие вида пришло, пока первая загрузка не закончилась: перечитать после неё
  let viewAfterLoad = false;

  const viewSoon = (): void => {
    if (get(runtimeView) === null) {
      viewAfterLoad = true;
      return;
    }
    if (viewTimer !== null) clearTimeout(viewTimer);
    viewTimer = setTimeout(() => {
      viewTimer = null;
      void loadRuntime();
    }, PROXIES_DEBOUNCE_MS);
  };
  const resultsSoon = (): void => {
    if (resultsTimer !== null) clearTimeout(resultsTimer);
    resultsTimer = setTimeout(() => {
      resultsTimer = null;
      void loadResults();
    }, PROXIES_DEBOUNCE_MS);
  };

  const offs = [
    onLayerEvent('proxies', (data) => {
      const kind = eventKind(data);
      if (kind === 'select' || kind === 'structure') viewSoon();
      else if (kind === 'ping' || kind === 'batch') resultsSoon();
      else {
        viewSoon();
        resultsSoon();
      }
    }),
    onLayerEvent('routing', viewSoon),
    onLayerEvent('sources', viewSoon),
    runtimeView.subscribe((view) => {
      if (view === null || !viewAfterLoad) return;
      viewAfterLoad = false;
      viewSoon();
    })
  ];

  return () => {
    for (const off of offs) off();
    if (viewTimer !== null) clearTimeout(viewTimer);
    if (resultsTimer !== null) clearTimeout(resultsTimer);
    viewTimer = null;
    resultsTimer = null;
  };
}
