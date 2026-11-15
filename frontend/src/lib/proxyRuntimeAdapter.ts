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
