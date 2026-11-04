// Клиент перехвата DNS роутера (фаза 148, DNSF-02): типы ответа GET /api/configlayer/dns,
// стор состояния и функции маршрутов /api/configlayer/dns/*. Ключи интерфейсов совпадают с
// frontend/src/lib/dnsContract.json (сверку выполняет scripts/check-config-ui.js,
// необязательные поля — `?`). Состояние перехвата, блокировки, апстримы и исходы считает
// бэкенд: интерфейс только отражает пришедшее. Событие `dns` слоя несёт лишь счётчик, по
// нему вид перечитывается. Запросы идут только при включённом флаге слоя «Конфигурация».

import { writable, derived, get, type Readable } from 'svelte/store';
import { apiFetchJSON } from './api';
import { onLayerEvent, post, applyDraftAck, layerSnapshot, type DraftAck } from './configLayer';
import { configLayerEnabled, activeKernelState } from '../stores';
import { t } from '../i18n';
import dnsErrorCodes from './dnsErrorCodes.json';

// --- Типы контракта ---

/** Состояние перехвата: выключен, идёт транзакция, окно подтверждения, включён, ждёт ядро, недоступен. */
export type DnsState = 'off' | 'applying' | 'window' | 'on' | 'waiting' | 'unavailable';

/** Причина блокировки действий карточки (порядок приоритета задаёт сервер). */
export type DnsBlock = 'conflict' | 'busy' | 'window' | 'draft' | 'stopped' | 'section' | 'group';

/** Кто держит порт 53: ядро, прошивка, другая служба или никто. */
export interface DnsOwner {
  kind: 'kernel' | 'firmware' | 'other' | 'none';
  kernel?: string;
  process?: string;
  addr?: string;
}

/** Группа-цель DNS-запросов: missing — группы с таким id нет. */
export interface DnsGroupRef {
  id: string;
  name: string;
  missing: boolean;
}

/** Окно подтверждения: вид, срок окончания по часам сервера и полная длина в секундах. */
export interface DnsWindow {
  kind: 'enable' | 'apply' | 'kernel_switch';
  deadline_ms: number;
  total_s: number;
}

/** Шаг транзакции включения или выключения. */
export interface DnsStep {
  id: string;
  status: 'pending' | 'running' | 'done' | 'failed';
}

/** Сбой транзакции: код текста, выполнен ли откат, параметры и вывод ядра. */
export interface DnsFailure {
  code: string;
  rolled_back: boolean;
  params?: Record<string, string>;
  detail?: string;
}

/** Исход без пользователя (откат по таймеру, возврат сторожем, смена ядра, ожидание ядра). */
export interface DnsOutcome {
  kind: string;
  at_ms: number;
  toast_pending: boolean;
  from?: string;
  to?: string;
  kernel?: string;
  reason?: string;
}

/** Старое перенаправление DNS XKeen (proxy_dns). */
export interface DnsLegacy {
  proxy_dns: boolean;
}

/** Параметры перехвата в черновике: id группы-цели и свои локальные зоны. */
export interface DnsParams {
  group: string;
  zones: string[];
}

/** Одна проба резолва: подпись (внешнее, локальное, ручное имя), путь и ответ. */
export interface DnsCheckGroup {
  label: 'external' | 'local' | 'manual';
  name: string;
  ok: boolean;
  latency_ms: number;
  path: 'group' | 'firmware' | 'zone' | 'unknown';
  answers: string[];
  zone?: string;
  group?: string;
  reason?: string;
}

/** Итог проверки резолва: при включении (auto) или вручную (manual). */
export interface DnsCheckResult {
  ok: boolean;
  source: 'auto' | 'manual';
  at_ms: number;
  groups: DnsCheckGroup[];
}

/** Состояние перехвата для карточки и баннера (GET /api/configlayer/dns). */
export interface DnsView {
  state: DnsState;
  desired: boolean;
  /** Ядро-перехватчик (xray или mihomo), пусто — перехват не желателен. */
  kernel: string;
  /** Активное ядро: xray, mihomo, both (конфликт) или none. */
  active_kernel: string;
  kernel_running: boolean;
  owner: DnsOwner;
  upstreams: string[];
  zones_builtin: string[];
  zones: string[];
  ipv6: boolean;
  default_name: string;
  blocks: DnsBlock[];
  steps: DnsStep[];
  legacy: DnsLegacy;
  /** Время сервера при ответе: по нему считается смещение часов. */
  server_now_ms: number;
  group?: DnsGroupRef;
  last_check?: DnsCheckResult;
  window?: DnsWindow;
  failure?: DnsFailure;
  outcome?: DnsOutcome;
  unavailable_reason?: string;
  waiting_reason?: string;
  /** Владелец DNS-секции ядра-перехватчика: panel, xkeen, user, none. */
  section_owner?: string;
  draft_params?: DnsParams;
  params_changed?: boolean;
}

/** Ответ действий, которые запускают транзакцию в фоне (enable, disable, rollback). */
export interface DnsStarted {
  started: boolean;
}

// --- Сторы ---

/** Последнее состояние перехвата; null — ещё не пришло или флаг слоя выключен. */
export const dnsTakeover = writable<DnsView | null>(null);
/** Первая загрузка состояния не удалась (прежний вид при сбое перечитывания остаётся). */
export const dnsLoadError = writable(false);
/**
 * Смещение часов: время сервера минус локальное время при получении ответа. Отсчёт окна
 * подтверждения считается от него, а не от локальных часов ПК (148-14).
 */
export const dnsClockOffset = writable(0);

/** Идёт окно подтверждения: баннер оболочки показан. */
export const dnsWindowActive: Readable<boolean> = derived(
  dnsTakeover,
  (v) => v?.state === 'window' && v.window !== undefined
);

/** Идёт транзакция перехвата или другая операция: действия карточки неактивны. */
export const dnsBusy: Readable<boolean> = derived(
  dnsTakeover,
  (v) => v !== null && (v.state === 'applying' || v.blocks.includes('busy'))
);

// --- Запросы ---

const BASE = '/api/configlayer/dns';

function asArray<T>(v: unknown): T[] {
  return Array.isArray(v) ? (v as T[]) : [];
}

/** Приводит ответ к полной форме: пропущенные списки становятся пустыми. */
function normalizeView(raw: DnsView): DnsView {
  return {
    ...raw,
    owner: raw.owner ?? { kind: 'none' },
    upstreams: asArray<string>(raw.upstreams),
    zones_builtin: asArray<string>(raw.zones_builtin),
    zones: asArray<string>(raw.zones),
    blocks: asArray<DnsBlock>(raw.blocks),
    steps: asArray<DnsStep>(raw.steps),
    legacy: raw.legacy ?? { proxy_dns: false }
  };
}

// Номер чтения: поздний ответ устаревшего запроса не затирает свежий вид
let loadSeq = 0;

/** Кладёт вид в стор и пересчитывает смещение часов. */
function setView(raw: DnsView): DnsView {
  const view = normalizeView(raw);
  if (typeof view.server_now_ms === 'number' && view.server_now_ms > 0) {
    dnsClockOffset.set(view.server_now_ms - Date.now());
  }
  loadSeq++;
  dnsTakeover.set(view);
  dnsLoadError.set(false);
  return view;
}

/** Читает состояние перехвата. Без включённого флага слоя запроса нет. */
export async function loadDns(): Promise<void> {
  if (!get(configLayerEnabled)) return;
  const mine = ++loadSeq;
  try {
    const raw = await apiFetchJSON<DnsView>(BASE);
    if (mine !== loadSeq) return;
    setView(raw);
  } catch {
    if (mine !== loadSeq) return;
    // Прежний вид остаётся на экране; ошибка видна, только пока вида ещё нет
    if (get(dnsTakeover) === null) dnsLoadError.set(true);
  }
}

/** Проверка резолва через 127.0.0.1:53; пустое имя — имя по умолчанию сервера. */
export function checkDns(name: string): Promise<DnsCheckResult> {
  return post<DnsCheckResult>(`${BASE}/check`, { name });
}

/** Запуск транзакции; ход и итог приходят событием dns, вид перечитывается. */
async function startAction(action: string): Promise<DnsStarted> {
  const res = await post<DnsStarted>(`${BASE}/${action}`, {});
  void loadDns();
  return res;
}

/** Включает перехват: предпроверки синхронно (отказ — ошибка с кодом), транзакция в фоне. */
export function enableDns(): Promise<DnsStarted> {
  return startAction('enable');
}

/** Выключает перехват или снимает желаемое состояние «ждёт ядро». */
export function disableDns(): Promise<DnsStarted> {
  return startAction('disable');
}

/** «Откатить сейчас» в окне подтверждения. */
export function rollbackDns(): Promise<DnsStarted> {
  return startAction('rollback');
}

/** Действие, которое отвечает новым видом: стор обновляется сразу. */
async function viewAction(action: string): Promise<DnsView> {
  return setView(await post<DnsView>(`${BASE}/${action}`, {}));
}

/** «Оставить»: закрывает окно подтверждения (повтор из другой вкладки — тот же вид). */
export function keepDns(): Promise<DnsView> {
  return viewAction('keep');
}

/** Выключает старое перенаправление DNS, включённое мимо панели. */
export function legacyOffDns(): Promise<DnsView> {
  return viewAction('legacy-off');
}

/** Тост об исходе без пользователя показан: сервер снимает признак toast_pending. */
export function outcomeSeenDns(): Promise<DnsView> {
  return viewAction('outcome-seen');
}

/** Параметры перехвата (группа-цель, свои зоны) — секция черновика dns_takeover. */
export async function saveDnsParams(revision: number, value: DnsParams): Promise<DraftAck> {
  const ack = await post<DraftAck>('/api/configlayer/draft', {
    revision,
    section: 'dns_takeover',
    value
  });
  applyDraftAck(ack);
  return ack;
}

// --- Перечитывание вида ---

// Вид устаревает по событию dns службы, по правке черновика (блокировка draft), по смене
// владельца маршрутизации (группа-цель), по ходу «Применить» и занятости замка (busy) и по
// смене состояния ядер (stopped, conflict). Одна общая отложенная перезагрузка на все
// причины; configLayer этот модуль не импортирует — цикла нет.
const DNS_DEBOUNCE_MS = 300;

let users = 0;
let timer: ReturnType<typeof setTimeout> | null = null;
let unsubs: Array<() => void> = [];
let flagUnsub: (() => void) | null = null;

function loadSoon(): void {
  if (timer !== null) clearTimeout(timer);
  timer = setTimeout(() => {
    timer = null;
    void loadDns();
  }, DNS_DEBOUNCE_MS);
}

/** Подпись снимка слоя: меняется, когда могли измениться блокировки busy и draft. */
function snapshotSignature(): string {
  const s = get(layerSnapshot);
  if (!s) return '';
  return `${s.apply.running ? 1 : 0}#${s.draft_changes}#${s.busy_holder ?? ''}`;
}

function subscribeAll(): void {
  unsubs.push(onLayerEvent('dns', loadSoon));
  unsubs.push(onLayerEvent('draft', loadSoon));
  unsubs.push(onLayerEvent('routing', loadSoon));
  let lastSnap = snapshotSignature();
  unsubs.push(
    layerSnapshot.subscribe(() => {
      const next = snapshotSignature();
      if (next === lastSnap) return;
      lastSnap = next;
      loadSoon();
    })
  );
  let lastKernel = get(activeKernelState);
  unsubs.push(
    activeKernelState.subscribe((k) => {
      if (k === lastKernel) return;
      lastKernel = k;
      loadSoon();
    })
  );
}

function unsubscribeAll(): void {
  for (const off of unsubs) off();
  unsubs = [];
  if (timer !== null) clearTimeout(timer);
  timer = null;
}

function resetState(): void {
  loadSeq++;
  dnsTakeover.set(null);
  dnsLoadError.set(false);
}

/**
 * Начинает слежение за состоянием перехвата (карточка, баннер). Вызовы считаются: слежение
 * идёт, пока есть хотя бы один пользователь. При выключенном флаге слоя запросов нет, вид
 * сбрасывается; включение флага сразу читает состояние.
 */
export function startDns(): void {
  users++;
  if (users > 1) return;
  let active = false;
  flagUnsub = configLayerEnabled.subscribe((on) => {
    if (on && !active) {
      active = true;
      subscribeAll();
      void loadDns();
    } else if (!on && active) {
      active = false;
      unsubscribeAll();
      resetState();
    }
  });
}

/** Снимает слежение одного пользователя; с последним — подписки и вид сбрасываются. */
export function stopDns(): void {
  if (users === 0) return;
  users--;
  if (users > 0) return;
  flagUnsub?.();
  flagUnsub = null;
  unsubscribeAll();
  resetState();
}

// --- Тексты ошибок ---

/** Коды ошибок API перехвата (текст — cfg.dns.err.<код>). */
export const DNS_ERROR_CODES: string[] = dnsErrorCodes.codes;
/** Коды проблем проверки параметров перехвата (текст — cfg.dns.issue.<код>). */
export const DNS_ISSUE_CODES: string[] = dnsErrorCodes.issues;

const KNOWN_CODES = new Set(DNS_ERROR_CODES);

// Параметры текстов ошибок: не пришедший параметр заменяется на «?», а не остаётся голым
// «{detail}» в интерфейсе.
const ERROR_PARAMS = ['detail', 'reason', 'name', 'group', 'owner', 'zone', 'kernel'];

/**
 * Текст ошибки по коду: cfg.dns.err.<код> для кодов из dnsErrorCodes.json, иначе
 * cfg.dns.err.generic с причиной (reason) из ответа.
 */
export function dnsErrorText(code: string | undefined, params?: Record<string, string>): string {
  const key =
    code !== undefined && KNOWN_CODES.has(code) ? `cfg.dns.err.${code}` : 'cfg.dns.err.generic';
  const p: Record<string, string> = {};
  for (const name of ERROR_PARAMS) p[name] = '?';
  for (const [k, v] of Object.entries(params ?? {})) {
    if (typeof v === 'string' && v !== '') p[k] = v;
  }
  return get(t)(key, p);
}

/** Текст ошибки запроса: код и параметры ответа, деталь и сообщение сервера — в параметры. */
export function dnsRequestErrorText(err: unknown): string {
  const e = (typeof err === 'object' && err !== null ? err : {}) as {
    code?: unknown;
    params?: unknown;
    detail?: unknown;
    message?: unknown;
  };
  const params: Record<string, string> = {};
  if (typeof e.params === 'object' && e.params !== null) {
    for (const [k, v] of Object.entries(e.params as Record<string, unknown>)) {
      if (typeof v === 'string') params[k] = v;
    }
  }
  const message = typeof e.message === 'string' ? e.message : String(err);
  if (typeof e.detail === 'string' && e.detail !== '' && !params.detail) params.detail = e.detail;
  if (!params.reason) params.reason = params.detail || message;
  return dnsErrorText(typeof e.code === 'string' ? e.code : undefined, params);
}
