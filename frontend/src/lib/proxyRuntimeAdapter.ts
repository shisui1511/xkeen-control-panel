// Адаптер вида работающих групп к карточке группы «Прокси» (режим «панель» у
// ProxyGroupCard). Чистые функции без запросов и без состояния: данные приходят
// из сторов configProxies.ts, карточка только показывает результат.
import type {
  GroupNow,
  NowDelay,
  PingResult,
  ProxyKernel,
  RuntimeGroup,
  RuntimeLink,
  RuntimeMember,
  RuntimeView
} from './configProxies';

/** Активное ядро карточки: '' — ядро не выбрано (оба остановлены). */
export type ActiveKernel = ProxyKernel | '';

/** Данные и действия карточки группы в режиме «панель». */
export interface PanelCard {
  group: RuntimeGroup;
  view: RuntimeView;
  kernel: ActiveKernel;
  /** Результаты пинга по ссылкам узлов. */
  results: Record<string, PingResult>;
  /** «Сейчас» этой группы из опроса ядра; null — нет данных. */
  now: GroupNow | null;
  /** История задержек ядра Mihomo по ссылкам узлов (авто- и резервные группы, D-05). */
  history: Record<string, NowDelay>;
  /** Участник, который сейчас выбирается в этой группе; '' — выбора нет. */
  selecting: string;
  /** Идёт выбор в связанной группе: выбор здесь заблокирован (D-17). */
  locked: boolean;
  /** Идущая пачка пинга охватывает эту группу. */
  pinging: boolean;
  /** Начало этой пачки (started_at): узлы без замера новее показывают спиннер. */
  pingSince?: string;
  /** На роутере идёт пачка пинга (любая): кнопка «Проверить» недоступна. */
  batchRunning: boolean;
  /** Узлы отсортированы по задержке. */
  sorted: boolean;
  onSelect(member: string): void;
  onPing(): void;
  onRetry(): void;
  onToggleSort(): void;
  onFocusGroup(id: string): void;
  onDiagnostics(member: RuntimeMember): void;
  onHistory(e: MouseEvent, ref: string): void;
}

/** Подпись ядра в текстах: Xray / Mihomo. */
export function kernelLabel(kernel: string): string {
  if (kernel === 'xray') return 'Xray';
  if (kernel === 'mihomo') return 'Mihomo';
  return '';
}

/** Другое ядро для строки несовместимых узлов (D-06). */
export function otherKernel(kernel: ActiveKernel): ActiveKernel {
  if (kernel === 'xray') return 'mihomo';
  if (kernel === 'mihomo') return 'xray';
  return '';
}

/** Узел, который активное ядро не умеет; группы, DIRECT и REJECT умеют оба ядра. */
function unsupported(m: RuntimeMember, kernel: ActiveKernel): boolean {
  if (m.kind !== 'node') return false;
  if (kernel === 'xray') return !m.xray;
  if (kernel === 'mihomo') return !m.mihomo;
  return false;
}

/**
 * Разделяет участников группы на показываемых в сетке и несовместимых с активным
 * ядром (свёрнутая строка D-06). Порядок — порядок состава группы. Без активного
 * ядра видны все.
 */
export function splitMembers(
  group: RuntimeGroup,
  kernel: ActiveKernel
): { visible: RuntimeMember[]; hidden: RuntimeMember[] } {
  const visible: RuntimeMember[] = [];
  const hidden: RuntimeMember[] = [];
  for (const m of group.members) {
    if (unsupported(m, kernel)) hidden.push(m);
    else visible.push(m);
  }
  return { visible, hidden };
}

/** Строка «Сейчас» карточки. */
export type NowLine =
  /** Цепочка звеньев: ручная группа (выбор) или авто/резерв (узел из «Сейчас» ядра). */
  | { kind: 'chain'; links: RuntimeLink[] }
  /** Xray без API: имя узла получить нельзя (D-09). */
  | { kind: 'fastest' }
  /** Ядро не сообщает узел (D-26): последний известный выбор не подставляется. */
  | { kind: 'unknown' }
  /** Строки нет (узлы источника без выбора). */
  | { kind: 'none' };

/**
 * «Сейчас» группы: для ручной — звенья выбора с сервера; для авто и резерва — узел из
 * «Сейчас» ядра с подписью из участников, «самый быстрый из живых» при Xray без API,
 * иначе — «неизвестен».
 */
export function nowLine(group: RuntimeGroup, now: GroupNow | null): NowLine {
  if (group.mode === 'source') return { kind: 'none' };
  if (group.mode === 'manual') {
    if (group.chain.length > 0) return { kind: 'chain', links: group.chain };
    return { kind: 'unknown' };
  }
  if (now?.ref) {
    const ref = now.ref;
    const m = group.members.find((x) => x.ref === ref);
    return { kind: 'chain', links: [{ kind: 'node', ref, name: m?.name || ref }] };
  }
  if (now?.fastest) return { kind: 'fastest' };
  return { kind: 'unknown' };
}

// --- Результаты пинга ---

/** Результат замера для плитки. */
export interface ResultView {
  state: 'none' | 'ok' | 'timeout' | 'error';
  ms?: number;
  /** Замер запасным путём по TLS (D-07). */
  tls: boolean;
  /** Подпись ядра, через которое получен замер, если оно не активное (D-20). */
  otherKernel?: string;
  /** Подпись активного ядра — для подсказки к замеру другим ядром. */
  active?: string;
  /** Время замера, мс от эпохи. */
  at?: number;
  /** Код причины неудачного замера. */
  reason?: string;
}

const NONE: ResultView = { state: 'none', tls: false };

function timeOf(at: string | undefined): number | undefined {
  if (!at) return undefined;
  const ms = Date.parse(at);
  return Number.isNaN(ms) ? undefined : ms;
}

/**
 * Замер узла по ссылке. Замер другим ядром помечается его подписью и не выдаётся за
 * замер активного ядра (D-20); замер без ядра (оба остановлены) другим не считается.
 */
export function resultView(
  results: Record<string, PingResult> | undefined,
  ref: string | undefined,
  activeKernel: ActiveKernel
): ResultView {
  if (!ref) return NONE;
  const r = results?.[ref];
  if (!r) return NONE;
  const view: ResultView = {
    state: r.status,
    tls: r.method === 'tls',
    at: timeOf(r.at)
  };
  if (r.status === 'ok') view.ms = r.ms;
  else if (r.reason) view.reason = r.reason;
  const via = r.kernel === 'xray' || r.kernel === 'mihomo' ? r.kernel : '';
  if (activeKernel !== '' && via !== '' && via !== activeKernel) {
    view.otherKernel = kernelLabel(via);
    view.active = kernelLabel(activeKernel);
  }
  return view;
}

/** Задержка из истории ядра Mihomo (авто- и резервные группы, D-05): 0 — нет ответа. */
export function historyView(delay: NowDelay | undefined): ResultView {
  if (!delay) return NONE;
  const at = timeOf(delay.at);
  return delay.ms > 0
    ? { state: 'ok', ms: delay.ms, tls: false, at }
    : { state: 'timeout', tls: false, at };
}

/** Замер активного ядра: есть и получен не другим ядром. */
function measured(v: ResultView): boolean {
  return v.state !== 'none' && v.otherKernel === undefined;
}

/**
 * Узел в формах proxyStats (ProxyLike и NodeSnapshot): HealthBar и фильтры карточки
 * считают его так же, как узел Clash. Замер другим ядром — «не проверен» (D-20).
 */
export function proxyLike(
  name: string,
  view: ResultView
): { name: string; alive: boolean; delay?: number; history: { time: string; delay: number }[] } {
  if (!measured(view)) return { name, alive: false, history: [] };
  const delay = view.state === 'ok' ? (view.ms ?? 0) : 0;
  const time = view.at !== undefined ? new Date(view.at).toISOString() : '';
  return { name, alive: delay > 0, delay, history: [{ time, delay }] };
}

/** История замеров узла в форме LatencyHistoryPopover ({ time, delay }; 0 — нет ответа). */
export function pingHistory(r: PingResult | undefined): { time: string; delay: number }[] {
  return (r?.history ?? []).map((h) => ({ time: h.at, delay: h.status === 'ok' ? h.ms : 0 }));
}

/**
 * Сортировка по задержке (D-13, UI-SPEC §4): ответившие по возрастанию, затем
 * неудачные, затем не измеренные (и замеры другим ядром), затем участники без замера
 * (группы, DIRECT, REJECT) и несовместимые. Внутри ступени — порядок состава.
 */
export function sortByPing(
  members: RuntimeMember[],
  results: Record<string, PingResult> | undefined,
  kernel: ActiveKernel
): RuntimeMember[] {
  const rank = (m: RuntimeMember): [number, number] => {
    if (m.kind !== 'node' || unsupported(m, kernel)) return [3, 0];
    const v = resultView(results, m.ref, kernel);
    if (!measured(v)) return [2, 0];
    if (v.state === 'ok') return [0, v.ms ?? 0];
    return [1, 0];
  };
  return members
    .map((m, i) => ({ m, i, r: rank(m) }))
    .sort((a, b) => a.r[0] - b.r[0] || a.r[1] - b.r[1] || a.i - b.i)
    .map((x) => x.m);
}
