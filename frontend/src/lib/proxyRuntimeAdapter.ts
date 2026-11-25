// Адаптер вида работающих групп к карточке группы «Прокси» (режим «панель» у
// ProxyGroupCard). Чистые функции без запросов и без состояния: данные приходят
// из сторов configProxies.ts, карточка только показывает результат.
import type {
  GroupNow,
  NowDelay,
  PingResult,
  ProxyKernel,
  RuntimeClock,
  RuntimeGroup,
  RuntimeHold,
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
  /** Идёт «Повторить» (подтверждение выбора) этой группы (D-16). */
  confirming: boolean;
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
  /** Группу держит лимит с действием «Блокировать»: стоит служебный REJECT (D-13). */
  | { kind: 'reject' }
  /** Группу держит лимит с действием «Напрямую»: стоит служебный DIRECT (D-13). */
  | { kind: 'direct' }
  /** Ядро не сообщает узел (D-26): последний известный выбор не подставляется. */
  | { kind: 'unknown' }
  /** Строки нет (узлы источника без выбора). */
  | { kind: 'none' };

/**
 * «Сейчас» группы: для ручной — звенья выбора с сервера; для авто и резерва — узел из
 * «Сейчас» ядра с подписью из участников, «самый быстрый из живых» при Xray без API,
 * иначе — «неизвестен». Неподтверждённый выбор (D-16) и «самый быстрый», когда ни один
 * узел не ответил (D-26), — тоже «неизвестен»: последний известный выбор не подставляется.
 * Узел, подтверждённый ядром, неудачные пробы не опровергают. Ручная группа, которую
 * лимит держит на служебном REJECT или DIRECT, — «Блокировка» или «Напрямую» (D-13).
 */
export function nowLine(group: RuntimeGroup, now: GroupNow | null, allFailed = false): NowLine {
  if (group.mode === 'source') return { kind: 'none' };
  if (group.unconfirmed) return { kind: 'unknown' };
  if (group.mode === 'manual') {
    // Служебные REJECT/DIRECT в сетке не показываются: позицию называет строка «Сейчас»
    if (group.hold?.kind === 'quota' && (group.current === 'reject' || group.current === 'direct'))
      return { kind: group.current };
    if (group.chain.length > 0) return { kind: 'chain', links: group.chain };
    return { kind: 'unknown' };
  }
  if (now?.ref) {
    const ref = now.ref;
    const m = group.members.find((x) => x.ref === ref);
    return { kind: 'chain', links: [{ kind: 'node', ref, name: m?.name || ref }] };
  }
  if (now?.fastest && !allFailed) return { kind: 'fastest' };
  return { kind: 'unknown' };
}

// --- Удержание группы арбитром (D-04, D-05) ---

/** DD.MM момента unix-секунд по часам роутера (смещение пояса из вида, D-08). */
export function routerDay(unix: number | undefined, clock: RuntimeClock | undefined): string {
  if (!unix) return '--.--';
  const d = new Date((unix + (clock?.offset ?? 0)) * 1000);
  const dd = String(d.getUTCDate()).padStart(2, '0');
  const mo = String(d.getUTCMonth() + 1).padStart(2, '0');
  return `${dd}.${mo}`;
}

/** Параметры текстов удержания лимитом: имя лимита и день сброса по часам роутера. */
export function quotaHoldParams(
  hold: RuntimeHold,
  clock: RuntimeClock | undefined
): { name: string; date: string } {
  return { name: hold.limit_name || hold.limit || '', date: routerDay(hold.reset_at, clock) };
}

/**
 * Удержание — только след неудачного возврата (Stalled без действующего удержания):
 * лимит уже снят или окно кончилось, а ядро не приняло возврат к исходному выбору.
 */
export function holdLeftover(hold: RuntimeHold): boolean {
  if (!hold.stalled) return false;
  if (hold.kind === 'quota') return !hold.limit_name;
  if (hold.kind === 'schedule' || hold.kind === 'override') return !hold.until;
  return false;
}

/** Группу держит действующий лимит: след неудачного возврата выбор не блокирует (сервер его принимает). */
export function quotaHeld(hold: RuntimeHold | undefined): boolean {
  return hold?.kind === 'quota' && !holdLeftover(hold);
}

/** Узел окна, которое держит группу (у перехвата hold.member — узел пользователя). */
export function holdWindowMember(group: RuntimeGroup): string {
  const hold = group.hold;
  if (!hold) return '';
  if (hold.kind === 'schedule') return hold.member ?? '';
  return group.schedule.find((w) => w.id === hold.window)?.member ?? '';
}

/** Участник — резервный узел группы «Резерв» (поле fallback — ссылка узла). */
export function isFallbackNode(group: RuntimeGroup, m: RuntimeMember): boolean {
  if (group.mode !== 'fallback' || !group.fallback) return false;
  return m.member === group.fallback || (!!m.ref && m.ref === group.fallback);
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

/**
 * Ни один узел не ответил (D-24): у группы есть совместимые узлы, все измерены активным
 * ядром и ни один замер не удачный. Нет совместимых узлов — другое состояние (D-23).
 */
export function noReply(
  group: RuntimeGroup,
  results: Record<string, PingResult> | undefined,
  kernel: ActiveKernel
): boolean {
  const nodes = splitMembers(group, kernel).visible.filter((m) => m.kind === 'node');
  if (nodes.length === 0) return false;
  return nodes.every((m) => {
    const v = resultView(results, m.ref, kernel);
    return measured(v) && v.state !== 'ok';
  });
}
