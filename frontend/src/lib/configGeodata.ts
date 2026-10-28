// Клиент менеджера geodata XKeen (вкладка «Списки» раздела «Конфигурация»): типы ответов
// API /api/configlayer/geodata*, стор снимка менеджера и загрузка. Поля — snake_case, как
// у сервера. Ключи интерфейсов совпадают с frontend/src/lib/geodataContract.json: сверку
// выполняет scripts/check-config-ui.js (необязательные поля — `?`). Интерфейс ничего не
// считает сам: источники, чипы, итоги запуска и проверка приходят от сервера готовыми.
// Модуль импортируют только Config.svelte и components/config/: при выключенном флаге
// слоя запросов менеджера нет (D-04 из 144). configLayer.ts этот модуль не импортирует.

import { writable, get } from 'svelte/store';
import { apiFetchJSON } from './api';
import { t } from '../i18n';
import { onLayerEvent, post, currentRevision, applyDraftAck, type DraftAck } from './configLayer';
import geodataErrorCodes from './geodataErrorCodes.json';

// --- Типы контракта ---

export interface XKeenView {
  /** Установлен ли XKeen (есть исполняемый файл). */
  installed: boolean;
  /** XKeen умеет пользовательские базы geodata (версия 2.0.1 и новее). */
  user_geodata: boolean;
  version?: string;
}

export interface GeodataEntry {
  file: string;
  url: string;
}

export interface DownloadView {
  /** Адрес прокси для GitHub, пусто — напрямую. */
  gh_proxy: string;
  /** Установлен ли jq. */
  jq: boolean;
  /** Включено ли обновление баз в настройках XKeen (geofile_update). */
  geofile_update: boolean;
  /** Код ошибки чтения xkeen.json, если снимок настроек не получен. */
  reason?: string;
  /** Записи geodata, которых XKeen не обработает (некорректные). */
  invalid?: GeodataEntry[];
  /** Причина, по которой не удалось установить jq. */
  jq_error?: string;
}

export interface ScheduleView {
  enabled: boolean;
  /** -1 — ежедневно, 0..6 — день недели, 0 — воскресенье. */
  day: number;
  hour: number;
  minute: number;
  /** Расписание панели приостановлено: базы обновлял cron XKeen (D-36). */
  suspended: boolean;
  /** Время по часам роутера, RFC3339. */
  now: string;
  /** Следующий запуск, RFC3339. */
  next?: string;
  /** Время повтора после сбоя сети, RFC3339. */
  retry_at?: string;
}

export interface CronView {
  /** В cron XKeen есть задача обновления баз. */
  found: boolean;
  /** Её расписание стандартное и панель может его перенять. */
  adoptable: boolean;
  /** Две задачи: панель и cron XKeen обновляют базы по двум расписаниям. */
  two: boolean;
  /** Владелец оставил cron XKeen («Не сейчас»). */
  kept: boolean;
  schedule?: string;
}

export interface RunView {
  running: boolean;
  /** Запуск ждёт окончания «Применить». */
  waiting_apply: boolean;
  /** Растёт с каждым изменением состояния менеджера. */
  seq: number;
  /** Что выполняется: ug, jq или remove. */
  job?: string;
  started?: string;
}

export interface LastRunView {
  at: string;
  /** ok, partial, failed или unknown. */
  outcome: string;
  /** Сколько файлов осталось без обновления. */
  not_updated: number;
  /** Вывод xkeen -ug разобран по файлам. */
  parsed: boolean;
  finished?: string;
  reason?: string;
}

export interface FileResultView {
  /** updated, unchanged, kept или failed. */
  status: string;
  at: string;
  /** Причина: no_size, html, size, sha256, network. */
  reason?: string;
  /** XKeen поставил файл без сверки SHA-256 (D-35). */
  unverified?: boolean;
  /** Сверку не провёл лимит запросов GitHub. */
  limit?: boolean;
}

export interface SourceView {
  /** Идентификатор строки — имя файла. */
  id: string;
  file: string;
  /** standard, panel, own или file. */
  origin: string;
  /** geosite или geoip. */
  kind: string;
  /** Файл есть на диске. */
  present: boolean;
  /** Есть запись geodata в xkeen.json. */
  entry: boolean;
  set_id?: string;
  provider?: string;
  url?: string;
  size?: number;
  mtime?: string;
  tags?: number;
  entries?: number;
  /** Парные имена файла (симлинк zkeen). */
  aliases?: string[];
  /** Временная запись geodata ждёт скачивания (D-01). */
  pending?: boolean;
  /** Запись не убрана после скачивания: нужно «Довести включение». */
  include_stuck?: boolean;
  /** Запись дублирует стандартный набор. */
  dup?: boolean;
  /** Ссылка записи отличается от стандартной. */
  dup_url?: boolean;
  /** Источник в черновике: delete или off. */
  draft?: string;
  /** Причина незавершённого удаления. */
  delete_incomplete?: string;
  /** Время обновления файла вне панели, RFC3339. */
  outside?: string;
  result?: FileResultView;
  /** Запись geodata некорректна: XKeen её не обработает. */
  invalid?: boolean;
  /** Файл большой: Xray дольше стартует. */
  heavy?: boolean;
}

export interface DraftDropView {
  file: string;
  /** delete или off. */
  mode: string;
}

export interface MihomoFile {
  name: string;
  size: number;
  mtime: string;
}

export interface View {
  xkeen: XKeenView;
  download: DownloadView;
  schedule: ScheduleView;
  cron: CronView;
  run: RunView;
  sources: SourceView[];
  /** Число файлов баз и их общий размер в байтах. */
  files: number;
  total_size: number;
  draft: DraftDropView[];
  mihomo: MihomoFile[];
  last_run?: LastRunView;
  geoipset?: FileResultView;
}

export interface OutputChunk {
  /** ug или jq. */
  job: string;
  /** Фактическое смещение начала text после зажатия запрошенного. */
  from: number;
  /** Смещение для следующего запроса. */
  next: number;
  text: string;
  running: boolean;
  /** Начало вывода отброшено: буфер переполнился. */
  truncated?: boolean;
  /** Номер запуска: сменился — начался новый запуск, вывод читается с начала. */
  run_id: number;
}

export interface ProbeCheck {
  code: string;
  /** ok, warn или block. */
  level: string;
  params?: Record<string, string>;
}

export interface ProbeResult {
  checks: ProbeCheck[];
  blocked: boolean;
  /** direct, gh_proxy или mirror. */
  route: string;
  size?: number;
  /** geosite или geoip. */
  kind?: string;
}

export interface PreviewView {
  before: string;
  after: string;
  comments_kept: boolean;
  /** Отпечаток `before`: возвращается в writeOp, чтобы запись шла по показанному файлу. */
  base: string;
}

export interface RuleRef {
  id: string;
  /** Номер правила в списке, с 1. */
  n: number;
}

export interface RawRef {
  rule: string;
  n: number;
}

export interface ForeignRef {
  file: string;
  line: number;
  /** Путь файла для ссылки «Открыть в Редакторе». */
  editor?: string;
}

export interface ListRef {
  file: string;
  tag: string;
  kind: string;
}

export interface GeodataListImpact {
  tag: string;
  kind: string;
  rules: RuleRef[];
  /** Идентификаторы правил, у которых после удаления не останется условий. */
  dies: string[];
  suggest?: ListRef;
}

export interface ImpactView {
  file: string;
  lists: GeodataListImpact[];
  raw: RawRef[];
  foreign: ForeignRef[];
}

export interface TagInfo {
  tag: string;
  count: number;
  examples: string[];
}

export interface TagsView {
  file: string;
  kind: string;
  tags: TagInfo[];
}

export interface EntriesPage {
  tag: string;
  total: number;
  offset: number;
  entries: string[];
  more: boolean;
}

export interface LookupMatch {
  file: string;
  tag: string;
  kind: string;
}

export interface LookupView {
  query: string;
  matches: LookupMatch[];
  more: number;
}

// --- Сторы ---

export const BASE = '/api/configlayer/geodata';

export const geodataView = writable<View | null>(null);
export const geodataStatus = writable<'idle' | 'loading' | 'ready' | 'error'>('idle');

// Растёт с каждым запросом: ответ устаревшего запроса не затирает более свежий
let loadSeq = 0;

function arr<T>(v: unknown): T[] {
  return Array.isArray(v) ? (v as T[]) : [];
}

/** Приводит ответ к полной форме: пропущенные списки становятся пустыми. */
function normalizeView(data: View): View {
  return {
    ...data,
    sources: arr<SourceView>(data.sources).map((s) => ({
      ...s,
      aliases: arr<string>(s.aliases)
    })),
    draft: arr<DraftDropView>(data.draft),
    mihomo: arr<MihomoFile>(data.mihomo),
    download: { ...data.download, invalid: arr<GeodataEntry>(data.download?.invalid) }
  };
}

/**
 * Читает снимок менеджера. Первая загрузка — статус loading; последующие (по событию SSE и
 * после правок) идут молча, без мигания скелетона. Сбой при уже загруженном снимке
 * состояние не портит.
 */
export async function loadGeodata(): Promise<void> {
  const seq = ++loadSeq;
  if (get(geodataView) === null) geodataStatus.set('loading');
  try {
    const data = await apiFetchJSON<View>(BASE);
    if (seq !== loadSeq) return;
    geodataView.set(normalizeView(data));
    geodataStatus.set('ready');
  } catch {
    if (seq !== loadSeq) return;
    if (get(geodataView) === null) geodataStatus.set('error');
  }
}

// События SSE geodata (счётчик и держатель замка, не данные) и draft (пометки черновика
// на удаление и выключение источников): снимок перечитывается с дебаунсом. Пока снимок ни
// разу не загружался, перечитывать нечего. configLayer этот модуль не импортирует — цикла нет.
const GEODATA_DEBOUNCE_MS = 300;
let geodataTimer: ReturnType<typeof setTimeout> | null = null;

function reloadSoon(): void {
  if (get(geodataView) === null) return;
  if (geodataTimer !== null) clearTimeout(geodataTimer);
  geodataTimer = setTimeout(() => {
    geodataTimer = null;
    void loadGeodata();
  }, GEODATA_DEBOUNCE_MS);
}

onLayerEvent('geodata', reloadSoon);
onLayerEvent('draft', reloadSoon);

// --- Клиент маршрутов ---

/** Операция записи в xkeen.json: набор полей зависит от вида (kind). */
export interface WriteOp {
  /** enable_set, add, set_url, gh_proxy, enable_update, dedup, finish_include или finish_delete. */
  kind: string;
  /** Идентификатор набора каталога (enable_set). */
  set?: string;
  file?: string;
  url?: string;
  /** Значение настройки (gh_proxy). */
  value?: string;
}

/** Действие над списком при удалении или выключении источника. */
export interface DropAction {
  tag: string;
  /** replace, remove или drop_rules. */
  action: 'replace' | 'remove' | 'drop_rules';
  /** Список-замена (action = replace). */
  with?: ListRef;
}

export interface ScheduleInput {
  enabled: boolean;
  day: number;
  hour: number;
  minute: number;
}

/** Ответ маршрута уже содержит свежий снимок: он заменяет стор, устаревшие чтения отбрасываются. */
function acceptView(view: View): View {
  loadSeq++;
  geodataView.set(normalizeView(view));
  geodataStatus.set('ready');
  return view;
}

/** Подтверждение правки черновика: ревизия и число изменений — в снимок слоя, снимок менеджера перечитывается. */
function acceptAck(ack: DraftAck): DraftAck {
  applyDraftAck(ack);
  void loadGeodata();
  return ack;
}

/** Вывод xkeen -ug (job = ug) или opkg (job = jq) начиная со смещения from. */
export function fetchOutput(job: 'ug' | 'jq', from: number): Promise<OutputChunk> {
  const q = new URLSearchParams({ job, from: String(from) });
  return apiFetchJSON<OutputChunk>(`${BASE}/output?${q.toString()}`);
}

/** «Обновить сейчас»: xkeen -ug под замком слоя; вывод читается опросом fetchOutput. */
export function runUpdate(): Promise<RunView> {
  return post<RunView>(`${BASE}/run`, {}).finally(() => void loadGeodata());
}

/** «Установить jq»: opkg install jq; вывод читается опросом fetchOutput('jq', …). */
export function installJQ(): Promise<RunView> {
  return post<RunView>(`${BASE}/jq`, {}).finally(() => void loadGeodata());
}

export function saveSchedule(input: ScheduleInput): Promise<View> {
  return post<View>(`${BASE}/schedule`, input).then(acceptView);
}

/** Решение по задаче cron XKeen: перенять, оставить, убрать или отдать XKeen. */
export function cronAction(action: 'adopt' | 'keep' | 'remove' | 'give'): Promise<View> {
  return post<View>(`${BASE}/cron`, { action }).then(acceptView);
}

/** Проверка источника тем же путём, что загрузка XKeen; файл и ссылка не сохраняются. */
export function probeSource(file: string, url: string, mode: 'add' | 'edit'): Promise<ProbeResult> {
  return post<ProbeResult>(`${BASE}/probe`, { file, url, mode });
}

/** Предпросмотр записи в xkeen.json: до и после, потеря комментариев, отпечаток base. */
export function previewOp(op: WriteOp): Promise<PreviewView> {
  return post<PreviewView>(`${BASE}/preview`, { op });
}

/**
 * Запись в xkeen.json по показанному файлу (base). При updateNow сразу идёт xkeen -ug.
 * Ответ 502 update_failed значит «запись сделана»: ошибка пробрасывается с data.written,
 * а снимок перечитывается.
 */
export function writeOp(
  op: WriteOp,
  base: string,
  acceptCommentLoss: boolean,
  updateNow: boolean
): Promise<View> {
  return post<View>(`${BASE}/write`, {
    op,
    base,
    accept_comment_loss: acceptCommentLoss,
    update_now: updateNow
  }).then(acceptView, (err: unknown) => {
    void loadGeodata();
    throw err;
  });
}

/** Что затронет удаление или выключение файла: списки с правилами, сырые правила, чужие ссылки. */
export function fetchImpact(file: string): Promise<ImpactView> {
  const q = new URLSearchParams({ file });
  return apiFetchJSON<ImpactView>(`${BASE}/impact?${q.toString()}`);
}

/**
 * Удаление или выключение источника в черновике. Ревизия берётся из снимка слоя, по ответу
 * обновляются ревизия и число изменений. Ошибки (draft_conflict, foreign_ref, raw_ref)
 * пробрасываются: разбирает handleLayerError или geodataErrorText.
 */
export function dropSource(
  file: string,
  mode: 'delete' | 'off',
  actions: DropAction[]
): Promise<DraftAck> {
  return post<DraftAck>(`${BASE}/drop`, { rev: currentRevision(), file, mode, actions }).then(
    acceptAck
  );
}

/** «Вернуть источник»: убирает пометку удаления или выключения из черновика. */
export function restoreSource(file: string): Promise<DraftAck> {
  return post<DraftAck>(`${BASE}/restore`, { rev: currentRevision(), file }).then(acceptAck);
}

export function fetchTags(file: string): Promise<TagsView> {
  const q = new URLSearchParams({ file });
  return apiFetchJSON<TagsView>(`${BASE}/tags?${q.toString()}`);
}

export function fetchEntries(
  file: string,
  tag: string,
  q: string,
  offset: number,
  limit: number
): Promise<EntriesPage> {
  const params = new URLSearchParams({
    file,
    tag,
    q,
    offset: String(offset),
    limit: String(limit)
  });
  return apiFetchJSON<EntriesPage>(`${BASE}/entries?${params.toString()}`);
}

/** Поиск «куда относится»: в каких списках есть домен или IP. */
export function lookupList(q: string): Promise<LookupView> {
  const params = new URLSearchParams({ q });
  return apiFetchJSON<LookupView>(`${BASE}/lookup?${params.toString()}`);
}

// --- Тексты ошибок ---

/** Все коды, которые знает сервер. */
export const GEODATA_ERROR_CODES: string[] = geodataErrorCodes;

const KNOWN_CODES = new Set(GEODATA_ERROR_CODES);

// Параметры, которые подставляются в тексты ошибок; не пришедший параметр заменяется
// на «?», а не остаётся голым «{file}» в интерфейсе.
const ERROR_PARAMS = ['field', 'reason', 'file', 'tag'];

/**
 * Текст ошибки менеджера: код ответа (err.code) → cfg.geo.err.<код>, неизвестный код или
 * ошибка без кода — cfg.geo.err.unknown. Параметры берутся из err.params.
 */
export function geodataErrorText(err: unknown): string {
  const e = typeof err === 'object' && err !== null ? (err as Record<string, unknown>) : {};
  const code = typeof e.code === 'string' ? e.code : undefined;
  const key =
    code !== undefined && KNOWN_CODES.has(code) ? `cfg.geo.err.${code}` : 'cfg.geo.err.unknown';
  const p: Record<string, string> = {};
  for (const name of ERROR_PARAMS) p[name] = '?';
  if (typeof e.params === 'object' && e.params !== null) {
    for (const [k, v] of Object.entries(e.params as Record<string, unknown>)) {
      if (typeof v === 'string' && v !== '') p[k] = v;
    }
  }
  return get(t)(key, p);
}
