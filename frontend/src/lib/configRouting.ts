// Клиент маршрутизации (вкладки «Группы» и «Правила» раздела «Конфигурация»): типы
// ответов API /api/configlayer/routing*, стор представления и функции маршрутов.
// Поля — snake_case, как у сервера. Ключи интерфейсов совпадают с
// frontend/src/lib/routingContract.json: сверку выполняет scripts/check-config-ui.js
// (необязательные поля — `?`). Интерфейс ничего не считает сам: состав группы,
// ссылки и ошибки приходят от сервера готовыми.

import { writable, get } from 'svelte/store';
import { apiFetchJSON } from './api';
import {
  onLayerEvent,
  post,
  currentRevision,
  applyDraftAck,
  type DraftAck,
  type RoutingSnapshot,
  type RoutingKernelView
} from './configLayer';
import routingErrorCodes from './routingErrorCodes.json';

export type { RoutingSnapshot, RoutingKernelView };

// --- Типы контракта ---

/** Проблема или предупреждение проверки: код и параметры, текст собирает фронтенд. */
export interface IssueView {
  code: string;
  params: Record<string, string>;
  /** Ключ места: group:<id>, rule:<id>, final, section:<имя>. */
  key?: string;
}

export interface Member {
  /** group, direct или reject. */
  kind: string;
  id?: string;
}

export interface Target {
  /** group, direct или reject. */
  kind: string;
  id?: string;
}

export interface ListRef {
  file: string;
  tag: string;
  kind: string;
}

export interface GroupSources {
  all: boolean;
  ids: string[];
}

export type GroupMode = 'manual' | 'auto' | 'fallback';

export interface Group {
  id: string;
  name: string;
  mode: GroupMode;
  sources: GroupSources;
  include: string[];
  exclude: string[];
  regex: boolean;
  picks: string[];
  members: Member[];
  reserve: string;
}

/** Число узлов группы по ядрам; null — ядро не установлено. */
export interface KernelCounts {
  xray: number | null;
  mihomo: number | null;
}

export interface MemberOption {
  /** group:<id>, direct, reject или node:<ref>. */
  member: string;
  label: string;
}

export interface GroupView extends Group {
  counts: KernelCounts;
  issues: IssueView[];
  warnings: IssueView[];
  member_options: MemberOption[];
  current: string;
  current_label: string;
}

export interface Cond {
  type: string;
  values: string[];
  lists: ListRef[];
}

export interface RawRule {
  kernel: string;
  text: string;
}

export interface Rule {
  id: string;
  enabled: boolean;
  /** Пусто (правило пользователя) или preset. */
  origin: string;
  conds: Cond[];
  target: Target;
  raw: RawRule | null;
}

export interface RuleView extends Rule {
  /** Номер в списке, с 1. */
  index: number;
  /** Пусто (оба ядра), xray или mihomo. */
  only: string;
  issues: IssueView[];
  warnings: IssueView[];
}

export interface ProbeConfig {
  v: number;
  url: string;
  interval_s: number;
  timeout_s: number;
}

export interface FeatureView {
  /** available, unavailable или undetermined. */
  status: string;
  min: string;
}

export interface RoutingView {
  revision: number;
  installed: Record<string, boolean>;
  desired: boolean;
  has_sources: boolean;
  defaults_done: boolean;
  groups: GroupView[];
  rules: RuleView[];
  final: Target;
  probe: ProbeConfig;
  probe_defaults: ProbeConfig;
  issues: IssueView[];
  features: Record<string, FeatureView>;
}

export interface KernelPreview {
  count: number;
  names: string[];
  members: Member[];
}

/** Состав группы по ядрам; null — ядро не установлено. */
export interface GroupPreview {
  xray: KernelPreview | null;
  mihomo: KernelPreview | null;
}

export interface RuleRef {
  id: string;
  index: number;
  summary: string;
  target: Target;
}

export interface GroupRef {
  id: string;
  name: string;
  /** member, composition или reserve. */
  role: string;
}

export interface BalancerLoss {
  group: string;
  lost: number;
}

export interface MemberLoss {
  group: string;
  member: string;
}

export interface KernelImpact {
  xray: BalancerLoss[];
  mihomo: MemberLoss[];
}

export interface Impact {
  /** group, rule, source или node. */
  kind: string;
  id: string;
  rules: RuleRef[];
  groups: GroupRef[];
  final: boolean;
  empties: GroupRef[];
  kernels: KernelImpact;
}

/** Действие при удалении группы со ссылками. */
export interface Cascade {
  /** Пусто (не выбрано), redirect или drop. */
  mode: '' | 'redirect' | 'drop';
  to?: Target;
}

export interface MihomoPorts {
  redir: number;
  tproxy: number;
  allow_lan?: boolean;
  bind_address?: string;
}

export interface TakeoverXray {
  move: string[];
  left: string[];
  warnings: string[];
}

export interface TakeoverMihomo {
  prev_profile: string;
  ports: MihomoPorts;
}

/** Предпросмотр взятия; null — ядро не установлено. */
export interface TakeoverPreview {
  xray: TakeoverXray | null;
  mihomo: TakeoverMihomo | null;
}

export interface SelectResult {
  member: string;
  kernels: Record<string, string>;
}

export interface ServiceView {
  id: string;
  name_ru: string;
  name_en: string;
  refs: ListRef[];
}

export interface ListEntry {
  file: string;
  tag: string;
  kind: string;
  count: number;
  examples: string[];
}

export interface CountryView {
  code: string;
  ip: ListRef | null;
  site: ListRef | null;
  blocked: ListRef[];
}

export interface ExtendedSet {
  id: string;
  name: string;
  file: string;
  url: string;
  kind: string;
  size_hint: string;
  heavy: boolean;
  installed: boolean;
  in_xkeen: boolean;
}

export interface GeodataEntry {
  file: string;
  url: string;
}

export interface GeodataStatus {
  jq: boolean;
  geofile_update: boolean;
  entries: GeodataEntry[];
  reason?: string;
}

export interface GeodataPreview {
  before: string;
  after: string;
  comments_kept: boolean;
}

export interface CatalogView {
  services: ServiceView[];
  lists: ListEntry[];
  countries: CountryView[];
  extended: ExtendedSet[];
  geodata: GeodataStatus;
}

export interface PresetInfo {
  id: string;
  needs_country: boolean;
  needs_services: boolean;
  countries: string[];
}

export interface PresetState {
  id: string;
  country: string;
  services: string[];
  flags: Record<string, boolean>;
}

export interface PresetsView {
  presets: PresetInfo[];
  services: ServiceView[];
  ads_available: boolean;
  default_country: string;
  current: PresetState | null;
}

/** Выбор пользователя в диалоге пресета. */
export interface PresetChoice {
  /** all_proxy, except_country, selective или empty. */
  id: string;
  country?: string;
  services?: string[];
  flags: {
    quic: boolean;
    torrent: boolean;
    ads: boolean;
    service_groups: boolean;
  };
}

export interface RuleSummary {
  conds: Cond[];
  target: Target;
  target_name?: string;
}

export interface PresetPlan {
  remove: RuleSummary[];
  add: RuleSummary[];
  groups: string[];
  final: Target;
  final_name?: string;
  kept: number;
}

/** Подтверждение правки, которую сервер вернул вместе с сохранённым объектом. */
export interface SaveGroupResult {
  event: DraftAck;
  group: Group;
}

export interface SaveRuleResult {
  event: DraftAck;
  rule: Rule;
}

// --- Сторы ---

const BASE = '/api/configlayer/routing';

export const routingView = writable<RoutingView | null>(null);
export const routingStatus = writable<'idle' | 'loading' | 'ready' | 'error'>('idle');

// Растёт с каждым запросом: ответ устаревшего запроса не затирает более свежий
let loadSeq = 0;

function arr<T>(v: unknown): T[] {
  return Array.isArray(v) ? (v as T[]) : [];
}

/** Приводит ответ к полной форме: пропущенные списки становятся пустыми. */
function normalizeView(data: RoutingView): RoutingView {
  return {
    ...data,
    groups: arr<GroupView>(data.groups).map((g) => ({
      ...g,
      issues: arr<IssueView>(g.issues),
      warnings: arr<IssueView>(g.warnings),
      member_options: arr<MemberOption>(g.member_options),
      members: arr<Member>(g.members)
    })),
    rules: arr<RuleView>(data.rules).map((r) => ({
      ...r,
      conds: arr<Cond>(r.conds),
      issues: arr<IssueView>(r.issues),
      warnings: arr<IssueView>(r.warnings)
    })),
    issues: arr<IssueView>(data.issues),
    installed: data.installed ?? {},
    features: data.features ?? {}
  };
}

/**
 * Читает представление маршрутизации. Первая загрузка — статус loading; последующие
 * (по событию SSE и после правок) идут молча, без мигания скелетона. Сбой при уже
 * загруженном представлении состояние не портит.
 */
export async function loadRouting(): Promise<void> {
  const seq = ++loadSeq;
  if (get(routingView) === null) routingStatus.set('loading');
  try {
    const data = await apiFetchJSON<RoutingView>(BASE);
    if (seq !== loadSeq) return;
    routingView.set(normalizeView(data));
    routingStatus.set('ready');
  } catch {
    if (seq !== loadSeq) return;
    if (get(routingView) === null) routingStatus.set('error');
  }
}

// События SSE routing, sources и draft — представление могло измениться (взятие,
// дрейф, источник с узлами, правка из другой вкладки): перечитываем с дебаунсом.
// Пока представление ни разу не загружалось, перечитывать нечего. configLayer этот
// модуль не импортирует — цикла нет.
const ROUTING_DEBOUNCE_MS = 300;
let routingTimer: ReturnType<typeof setTimeout> | null = null;

function reloadSoon(): void {
  if (get(routingView) === null) return;
  if (routingTimer !== null) clearTimeout(routingTimer);
  routingTimer = setTimeout(() => {
    routingTimer = null;
    void loadRouting();
  }, ROUTING_DEBOUNCE_MS);
}

onLayerEvent('routing', reloadSoon);
onLayerEvent('sources', reloadSoon);
onLayerEvent('draft', reloadSoon);

// --- Клиент маршрутов ---

/** Ответ правки: подтверждение либо объект, у которого оно лежит в поле event. */
function ackOf(res: DraftAck | { event: DraftAck }): DraftAck {
  return 'event' in res ? res.event : res;
}

/**
 * Правка черновика через маршрут маршрутизации: ревизия берётся из снимка слоя, по ответу
 * обновляются ревизия и счётчик изменений, представление перечитывается. Ошибки
 * (в т. ч. draft_conflict с code) пробрасываются: разбирает handleLayerError.
 */
async function editRouting<T extends DraftAck | { event: DraftAck }>(
  path: string,
  body: Record<string, unknown>
): Promise<T> {
  const res = await post<T>(`${BASE}/${path}`, { revision: currentRevision(), ...body });
  applyDraftAck(ackOf(res));
  void loadRouting();
  return res;
}

/** Сохраняет группу; пустой id — новая. */
export function saveGroup(
  group: Partial<Group> & Pick<Group, 'name' | 'mode'>
): Promise<SaveGroupResult> {
  return editRouting<SaveGroupResult>('groups/save', { group });
}

/** Состав группы по ядрам для диалога; группа не сохраняется. */
export function previewGroup(group: Partial<Group>, signal?: AbortSignal): Promise<GroupPreview> {
  return apiFetchJSON<GroupPreview>(`${BASE}/groups/preview`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ group }),
    signal
  });
}

/** Удаляет группу; есть ссылки и mode пуст — ошибка group_referenced с impact в data. */
export function deleteGroup(id: string, cascade: Cascade = { mode: '' }): Promise<DraftAck> {
  return editRouting<DraftAck>('groups/delete', { id, cascade });
}

/** Что затронет удаление группы, правила, источника или узла. */
export function fetchImpact(
  kind: 'group' | 'rule' | 'source' | 'node',
  id: string
): Promise<Impact> {
  return post<Impact>(`${BASE}/impact`, { kind, id });
}

/** Сохраняет правило; пустой id — новое. */
export function saveRule(
  rule: Partial<Rule> & Pick<Rule, 'conds' | 'target'>
): Promise<SaveRuleResult> {
  return editRouting<SaveRuleResult>('rules/save', { rule });
}

export function deleteRule(id: string): Promise<DraftAck> {
  return editRouting<DraftAck>('rules/delete', { id });
}

/** Переставляет правило на позицию `to` (с нуля). */
export function moveRule(id: string, to: number): Promise<DraftAck> {
  return editRouting<DraftAck>('rules/move', { id, to });
}

export function setRuleEnabled(id: string, enabled: boolean): Promise<DraftAck> {
  return editRouting<DraftAck>('rules/enable', { id, enabled });
}

/** Цель «Всё остальное». */
export function setFinal(target: Target): Promise<DraftAck> {
  return editRouting<DraftAck>('final', { target });
}

export function saveProbe(probe: ProbeConfig): Promise<DraftAck> {
  return editRouting<DraftAck>('probe', { probe });
}

/** Желание «маршрутизацией управляет панель»; файлы меняются при «Применить». */
export function takeRouting(): Promise<DraftAck> {
  return editRouting<DraftAck>('take', {});
}

/** Желание вернуть маршрутизацию XKeen; файлы возвращаются при «Применить». */
export function releaseRouting(): Promise<DraftAck> {
  return editRouting<DraftAck>('release', {});
}

/** Предпросмотр взятия для диалога; диск не меняется. */
export function fetchTakeover(): Promise<TakeoverPreview> {
  return apiFetchJSON<TakeoverPreview>(`${BASE}/takeover`);
}

/** Решает дрейф маршрутизации ядра в пользу панели; ответ приходит после перезапуска ядра. */
export async function reclaimRouting(kernel: 'xray' | 'mihomo'): Promise<void> {
  await post<{ reclaimed: boolean }>(`${BASE}/reclaim`, { kernel });
  void loadRouting();
}

export function fetchLists(): Promise<CatalogView> {
  return apiFetchJSON<CatalogView>(`${BASE}/lists`);
}

export function previewGeodata(set: string): Promise<GeodataPreview> {
  return post<GeodataPreview>(`${BASE}/lists/geodata/preview`, { set });
}

/** «Добавить в XKeen»: запись геоданных в xkeen.json, при updateNow — обновление баз. */
export function addGeodata(
  set: string,
  acceptCommentLoss: boolean,
  updateNow: boolean
): Promise<GeodataStatus> {
  return post<GeodataStatus>(`${BASE}/lists/geodata/add`, {
    set,
    accept_comment_loss: acceptCommentLoss,
    update_now: updateNow
  });
}

export function fetchPresets(): Promise<PresetsView> {
  return apiFetchJSON<PresetsView>(`${BASE}/presets`);
}

/** План пресета (что уйдёт и что придёт); черновик не меняется. */
export function planPreset(choice: PresetChoice): Promise<PresetPlan> {
  return post<PresetPlan>(`${BASE}/presets/plan`, { choice });
}

/** Применяет пресет одной правкой черновика; take — вместе со взятием маршрутизации. */
export function applyPreset(choice: PresetChoice, take: boolean): Promise<DraftAck> {
  return editRouting<DraftAck>('presets/apply', { choice, take });
}

/** Выбор участника ручной группы: Mihomo получает его сразу, Xray — перезапуском ядра. */
export async function selectMember(group: string, member: string): Promise<SelectResult> {
  const res = await post<SelectResult>(`${BASE}/select`, { group, member });
  void loadRouting();
  return res;
}

// --- Тексты ошибок ---

/** Функция перевода из стора $t. */
export type TFn = (key: string, params?: Record<string, string | number>) => string;

/** Все коды, которые знает сервер. */
export const ROUTING_ERROR_CODES: string[] = routingErrorCodes;

const KNOWN_CODES = new Set(ROUTING_ERROR_CODES);

// Параметры, которые подставляются в тексты ошибок; не пришедший параметр заменяется
// на «?», а не остаётся голым «{field}» в интерфейсе.
const ERROR_PARAMS = ['field', 'reason', 'group', 'file', 'kernel', 'name', 'tag'];

/** Текст ошибки по коду: cfg.routing.err.<код>, неизвестный код — cfg.routing.err.unknown. */
export function routingErrorText(
  t: TFn,
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
