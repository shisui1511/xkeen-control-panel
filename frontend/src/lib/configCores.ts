// Клиент секций ядер (вкладка «Xray │ Mihomo» раздела «Конфигурация»): типы ответов API
// /api/configlayer/cores*, таблица полей форм и функции запросов. Поля — snake_case, как
// у сервера. Ключи интерфейсов совпадают с frontend/src/lib/coresContract.json: сверку
// выполняет scripts/check-config-ui.js (необязательные поля — `?`). Интерфейс ничего не
// считает сам: владелец секции, закреплённые значения и проблемы приходят готовыми.

import { get } from 'svelte/store';
import { apiFetchJSON } from './api';
import {
  onLayerEvent,
  post,
  currentRevision,
  applyDraftAck,
  layerSnapshot,
  type DraftAck
} from './configLayer';
import coresErrorCodes from './coresErrorCodes.json';

// --- Типы контракта ---

export type CoreKernel = 'xray' | 'mihomo';
export type XraySectionId = 'log' | 'dns' | 'inbounds' | 'policy' | 'other';
export type MihomoSectionId = 'main' | 'dns' | 'inbounds' | 'sniffer' | 'other';
/**
 * Кто ведёт секцию: панель, ждёт «Применить» (взятие), отпускание ждёт «Применить» (секция ещё у
 * панели), XKeen, пользователь, файла нет, дрейф.
 */
export type SectionOwner = 'panel' | 'pending' | 'releasing' | 'xkeen' | 'user' | 'none' | 'drift';

/** Значение, которое панель дописывает в секцию сама и которое менять нельзя. */
export interface PinnedValue {
  key: string;
  value: unknown;
}

/** Проблема ключа сырого блока: ключ и код (текст — cfg.sec.issue.<код> или cfg.sec.err.<код>). */
export interface KeyIssue {
  key: string;
  code: string;
}

export interface SectionView {
  id: XraySectionId | MihomoSectionId;
  owner: SectionOwner;
  files: string[];
  /** Значение секции целиком (действующее у чужой секции). */
  value: unknown;
  /** Только поля формы секции (FORM_KEYS). */
  form: unknown;
  /** Остаток вне формы: JSON у Xray, YAML у Mihomo. */
  advanced: string;
  pinned: PinnedValue[];
  dirty: boolean;
  panel_file?: string;
  /** Код блокировки взятия (inbounds_mixed, section_shadowed, xkeen_too_old). */
  blocked?: string;
  blocked_file?: string;
  comments?: string[];
  left?: string[];
  drift?: string[];
}

/** Группа, в которую панель направляет входящие force-proxy XKeen. */
export interface ForceProxyView {
  tags: string[];
  group: string;
  editable: boolean;
}

/** Активный профиль Mihomo: кто им управляет и как он называется. */
export interface ProfileView {
  owner: string;
  name: string;
}

export interface KernelView {
  kernel: CoreKernel;
  installed: boolean;
  running: boolean;
  sections: SectionView[];
  take_all: boolean;
  profile?: ProfileView;
  force_proxy?: ForceProxyView;
}

/** Что произойдёт с секцией при взятии (диалог взятия). */
export interface SectionPreview {
  id: string;
  /** Файлы, которые уйдут в резервные копии. */
  move: string[];
  /** Файлы, которые остаются на месте. */
  left: string[];
  /** Файлы с комментариями, которые будут потеряны. */
  comments: string[];
  /** Секция заполнится значениями пресета: файла нет. */
  preset: boolean;
  blocked?: string;
  blocked_file?: string;
}

export interface TakePreview {
  kernel: CoreKernel;
  sections: SectionPreview[];
  force_proxy_tags?: string[];
}

/** Синтаксическая ошибка сырого блока. */
export interface RawError {
  line: number;
  detail: string;
}

/** Разбор сырого блока: значение и поля формы либо ошибка. В черновик ничего не пишется. */
export interface RawResult {
  rejected: KeyIssue[];
  value?: unknown;
  form?: unknown;
  error?: RawError;
}

/** Часть снимка слоя (поле cores): состояние секции Xray. */
export interface CoreSectionSnap {
  desired: boolean;
  taken: boolean;
  file: string;
  moved: string[];
  left: string[];
  drift: string[];
}

/** Объявление о способе перезапуска: ядро и причина (port, tun). */
export interface RestartNote {
  kernel: string;
  reason: 'port' | 'tun';
}

/** Поле cores снимка слоя. */
export interface CoresSnapshot {
  xray: Record<string, CoreSectionSnap>;
  restart: RestartNote[];
}

// --- Поля форм секций ---

/**
 * Пути полей формы по секциям («[]» — элемент списка). Всё остальное значение секции
 * относится к «Дополнительно». Бэкенд держит такую же таблицу: совпадение проверяет
 * scripts/check-cores-api.js.
 */
export const FORM_KEYS: Record<string, readonly string[]> = {
  'xray.log': ['loglevel', 'access', 'error', 'dnsLog'],
  'xray.dns': ['servers', 'queryStrategy', 'disableCache'],
  'xray.inbounds': ['[].tag', '[].protocol', '[].listen', '[].port'],
  'xray.policy': [
    'levels.0.handshake',
    'levels.0.connIdle',
    'levels.0.uplinkOnly',
    'levels.0.downlinkOnly'
  ],
  'xray.other': [],
  'mihomo.main': [
    'mixed-port',
    'port',
    'socks-port',
    'redir-port',
    'tproxy-port',
    'allow-lan',
    'bind-address',
    'mode',
    'log-level',
    'ipv6'
  ],
  'mihomo.dns': ['enable', 'enhanced-mode', 'ipv6', 'nameserver', 'default-nameserver'],
  'mihomo.inbounds': [
    'listeners[].name',
    'listeners[].type',
    'listeners[].listen',
    'listeners[].port',
    'tun.enable',
    'tun.stack',
    'tun.auto-route'
  ],
  'mihomo.sniffer': ['enable', 'force-dns-mapping', 'parse-pure-ip', 'override-destination'],
  'mihomo.other': []
};

// --- Запросы ---

const BASE = '/api/configlayer/cores';

/** Вид секций ядра. */
export async function fetchCores(kernel: CoreKernel): Promise<KernelView> {
  return apiFetchJSON<KernelView>(`${BASE}?kernel=${encodeURIComponent(kernel)}`);
}

/** Предпросмотр взятия секций для диалога; диск и черновик не меняются. */
export async function fetchTakePreview(
  kernel: CoreKernel,
  sections: string[]
): Promise<TakePreview> {
  const qs = `kernel=${encodeURIComponent(kernel)}&sections=${encodeURIComponent(sections.join(','))}`;
  return apiFetchJSON<TakePreview>(`${BASE}/takeover?${qs}`);
}

/** Ответ правки: подтверждение либо объект, у которого оно лежит в поле event. */
type EditResult = DraftAck | { event: DraftAck };

function ackOf(res: EditResult): DraftAck {
  return 'event' in res ? res.event : res;
}

/**
 * Правка черновика через маршрут секций: ревизия берётся из снимка слоя, по ответу
 * обновляются ревизия и счётчик изменений. Ошибки (в т. ч. draft_conflict с code)
 * пробрасываются: разбирает handleLayerError.
 */
async function editCores(path: string, body: Record<string, unknown>): Promise<DraftAck> {
  const res = await post<EditResult>(`${BASE}/${path}`, { revision: currentRevision(), ...body });
  const ack = ackOf(res);
  applyDraftAck(ack);
  return ack;
}

/** Желание «секции ведёт панель»; файлы меняются при «Применить». */
export function takeSections(
  kernel: CoreKernel,
  sections: string[],
  ackComments: boolean
): Promise<DraftAck> {
  return editCores('take', { kernel, sections, ack_comments: ackComments });
}

/** Сохраняет значения формы и «Дополнительно» взятой секции в черновик. */
export function saveSection(
  kernel: CoreKernel,
  section: string,
  form: unknown,
  advanced: string
): Promise<DraftAck> {
  return editCores('save', { kernel, section, form, advanced });
}

/** Заменяет значения секции в черновике значениями пресета. */
export function presetSection(kernel: CoreKernel, section: string): Promise<DraftAck> {
  return editCores('preset', { kernel, section });
}

/** Желание вернуть секцию прежнему владельцу; файлы возвращаются при «Применить». */
export function releaseSection(kernel: CoreKernel, section: string): Promise<DraftAck> {
  return editCores('release', { kernel, section });
}

/** Группа, в которую панель направляет входящие force-proxy. */
export function setForceProxyGroup(kernel: CoreKernel, group: string): Promise<DraftAck> {
  return editCores('force-proxy', { kernel, group });
}

/** Разбор сырого блока «Дополнительно»; черновик не меняется, ревизия не нужна. */
export function parseRaw(kernel: CoreKernel, section: string, text: string): Promise<RawResult> {
  return post<RawResult>(`${BASE}/raw/parse`, { kernel, section, text });
}

/** Решает дрейф секции в пользу панели: файл панели возвращается на диск сразу. */
export async function reclaimSection(kernel: CoreKernel, section: string): Promise<void> {
  await post<{ reclaimed: boolean }>(`${BASE}/reclaim`, { kernel, section });
}

// --- Перечитывание вида ---

// Вид секций устаревает при правке черновика (draft), смене владельца маршрутизации
// (routing), изменении файлов и ходе «Применить». У слоя есть подписки на draft и routing;
// изменения files и apply приходят в снимок слоя, за ним и следим. Одна общая отложенная
// перезагрузка на все причины. configLayer этот модуль не импортирует — цикла нет.
const CORES_DEBOUNCE_MS = 300;

/** Подпись состояния файлов и применения в снимке слоя: меняется, когда вид мог устареть. */
function snapshotSignature(): string {
  const s = get(layerSnapshot);
  if (!s) return '';
  return `${s.files.map((f) => `${f.key}:${f.state}`).join('|')}#${s.apply.running ? 1 : 0}`;
}

/**
 * Вызывает cb (с задержкой 300 мс, одним вызовом на серию событий), когда вид секций мог
 * устареть. Возвращает функцию отписки.
 */
export function onCoresChanged(cb: () => void): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const soon = (): void => {
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      cb();
    }, CORES_DEBOUNCE_MS);
  };
  const offDraft = onLayerEvent('draft', soon);
  const offRouting = onLayerEvent('routing', soon);
  let last = snapshotSignature();
  const offSnapshot = layerSnapshot.subscribe(() => {
    const next = snapshotSignature();
    if (next === last) return;
    last = next;
    soon();
  });
  return () => {
    offDraft();
    offRouting();
    offSnapshot();
    if (timer !== null) clearTimeout(timer);
    timer = null;
  };
}

// --- Тексты ошибок ---

/** Функция перевода из стора $t. */
export type TFn = (key: string, params?: Record<string, string | number>) => string;

/** Коды ошибок API секций. */
export const CORES_ERROR_CODES: string[] = coresErrorCodes.codes;
/** Коды проблем проверки секций (текст — cfg.sec.issue.<код>). */
export const CORES_ISSUE_CODES: string[] = coresErrorCodes.issues;

const KNOWN_CODES = new Set(CORES_ERROR_CODES);
const KNOWN_ISSUES = new Set(CORES_ISSUE_CODES);

// Параметры, которые подставляются в тексты ошибок: не пришедший параметр заменяется
// на «?», а не остаётся голым «{file}» в интерфейсе.
const ERROR_PARAMS = ['file', 'files', 'kernel', 'name', 'key', 'section', 'group', 'min', 'value'];

/** Текст ошибки по коду: cfg.sec.err.<код>, неизвестный код — cfg.sec.err.unknown. */
export function coresErrorText(
  t: TFn,
  code: string | undefined,
  params?: Record<string, string>
): string {
  const key =
    code !== undefined && KNOWN_CODES.has(code) ? `cfg.sec.err.${code}` : 'cfg.sec.err.unknown';
  const p: Record<string, string> = {};
  for (const name of ERROR_PARAMS) p[name] = '?';
  for (const [k, v] of Object.entries(params ?? {})) {
    if (typeof v === 'string' && v !== '') p[k] = v;
  }
  return t(key, p);
}

/** Ключ текста проблемы проверки секции: cfg.sec.issue.<код>; код не из списка — null. */
export function coresIssueKey(code: string | undefined): string | null {
  return code !== undefined && KNOWN_ISSUES.has(code) ? `cfg.sec.issue.${code}` : null;
}
