// Помощники представления списка источников вкладки «Узлы»: какие узлы видны для ядра и
// чем приглушены остальные (D-16, D-23), вариант и подпись статуса источника по ядру
// (D-25), уровень индикатора трафика, итог ручного «Обновить» (D-08). Только данные и
// ключи словарей: тексты собирают компоненты, поведение проверяют роутерные сценарии.

import { readable, writable } from 'svelte/store';
import type {
  KernelName,
  KernelStateView,
  NodeView,
  RefreshOutcome,
  SourceView,
  VerdictView
} from './configSources';
import { kernelLabel } from './kernelState';
import { sourceErrorText, type TFn } from './sourceErrors';

// Состояние вкладки живёт на уровне модуля, а не компонента: вкладки раздела
// «Конфигурация» размонтируются при смене, а раскрытие карточек и выбранное ядро
// должны её пережить (UI-SPEC, E1).

/** Раскрытые карточки источников по идентификатору. */
export const expandedSources = writable<Record<string, boolean>>({});

/** Явный выбор ядра в сегменте «Xray │ Mihomo»; null — берётся ядро по умолчанию. */
export const chosenKernel = writable<KernelName | null>(null);

export function toggleExpanded(id: string): void {
  expandedSources.update((m) => ({ ...m, [id]: !m[id] }));
}

export type StatusVariant = 'running' | 'stopped' | 'warning' | 'idle' | 'info';

export interface KernelStatusView {
  variant: StatusVariant;
  key: string;
  params: Record<string, string>;
}

/** Пометка узла в просматриваемом наборе; null — узел обычный. */
export type NodeMark = 'only_xray' | 'only_mihomo' | 'unsupported' | 'excluded';

export interface NodeItem {
  node: NodeView;
  dimmed: boolean;
  mark: NodeMark | null;
  /** Вердикт ядра, объясняющий пометку (причина в title чипа). */
  reason: VerdictView | null;
}

function otherKernel(kernel: KernelName): KernelName {
  return kernel === 'xray' ? 'mihomo' : 'xray';
}

/**
 * Узлы источника для просматриваемого ядра. Узел виден без приглушения, если ядро
 * получит его (в наборе и поддержан) и он не исключён по имени. Остальные остаются в
 * списке приглушёнными с пометкой: «только <другое ядро>», если там узел работает, и
 * «не поддерживается», если не работает нигде.
 */
export function nodesForKernel(s: SourceView, kernel: KernelName): NodeItem[] {
  const other = otherKernel(kernel);
  return s.nodes.map((node) => {
    if (node.excluded) {
      return { node, dimmed: true, mark: 'excluded' as const, reason: null };
    }
    const here = node.in_set[kernel] && node.support[kernel].ok;
    if (here) return { node, dimmed: false, mark: null, reason: null };
    const there = node.in_set[other] && node.support[other].ok;
    if (there) {
      return {
        node,
        dimmed: true,
        mark: other === 'xray' ? ('only_xray' as const) : ('only_mihomo' as const),
        reason: node.support[kernel].ok ? null : node.support[kernel]
      };
    }
    const why = !node.support[kernel].ok ? node.support[kernel] : node.support[other];
    return { node, dimmed: true, mark: 'unsupported' as const, reason: why.ok ? null : why };
  });
}

// --- Время ---

/** «Сейчас» с шагом в минуту для подписей «2 ч назад»; таймер один на всех подписчиков. */
export const minuteClock = readable(new Date(), (set) => {
  set(new Date());
  const timer = setInterval(() => set(new Date()), 60_000);
  return () => clearInterval(timer);
});

export interface AgoParts {
  unit: 'now' | 'minutes' | 'hours' | 'days';
  n: number;
}

/** Сколько прошло от then до now: минуты, часы, дни; меньше минуты — «только что». */
export function agoParts(then: Date, now: Date): AgoParts {
  const minutes = Math.floor((now.getTime() - then.getTime()) / 60000);
  if (!Number.isFinite(minutes) || minutes < 1) return { unit: 'now', n: 0 };
  if (minutes < 60) return { unit: 'minutes', n: minutes };
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return { unit: 'hours', n: hours };
  return { unit: 'days', n: Math.floor(hours / 24) };
}

/** Дата для интерфейса: ДД.ММ.ГГГГ по местному времени; не дата — пустая строка. */
export function formatDate(value: string | number | Date | undefined): string {
  if (value === undefined || value === '') return '';
  const d = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  const dd = String(d.getDate()).padStart(2, '0');
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  return `${dd}.${mm}.${d.getFullYear()}`;
}

/** Метка времени срока подписки (секунды Unix) → Date; 0 и меньше — срока нет. */
export function expireDate(expire: number | undefined): Date | null {
  if (!expire || expire <= 0) return null;
  return new Date(expire * 1000);
}

// --- Статус по ядру ---

const P = 'cfg.nodes.status.';

function view(
  variant: StatusVariant,
  key: string,
  params: Record<string, string> = {}
): KernelStatusView {
  return { variant, key: P + key, params };
}

/**
 * Вариант StatusBadge и ключ подписи статуса источника по ядру. Приоритет: ядро не
 * установлено или источник выключен → отказ ядра → отложено → отказ провайдера →
 * сеть → изменено в черновике → ядро остановлено → всё в порядке.
 */
export function kernelStatusView(
  k: KernelStateView | undefined,
  s: SourceView,
  installed: boolean,
  now: Date
): KernelStatusView {
  if (!installed) return view('idle', 'not_installed');
  if (!s.enabled) return view('idle', 'disabled');
  if (!k) return view('idle', 'none');

  const st = k.status;
  const snapshot = formatDate(k.snapshot_at);
  switch (st.code) {
    case 'refused':
      return view('stopped', 'refused');
    case 'deferred':
      return view('warning', 'deferred');
    case 'rejected':
      return view('warning', 'rejected', { date: snapshot, code: st.error_code ?? '' });
    case 'provider_error':
      return view('warning', 'provider_error', { date: snapshot, code: st.error_code ?? '' });
    case 'network': {
      if (st.retry_off) return view('warning', 'network_no_retry');
      let minutes = 0;
      if (st.retry_at) {
        const ms = new Date(st.retry_at).getTime() - now.getTime();
        if (Number.isFinite(ms) && ms > 0) minutes = Math.ceil(ms / 60000);
      }
      return view('warning', 'network', { in: String(minutes) });
    }
  }
  if (k.pending && s.draft_state !== 'applied') return view('info', 'draft');
  if (k.stopped) return view('idle', 'stopped');
  if (st.code === 'ok') {
    const at = k.updated_at ?? k.snapshot_at;
    if (!at) return view('running', 'ready');
    const ago = agoParts(new Date(at), now);
    const params: Record<string, string> = { ago_unit: ago.unit, ago_n: String(ago.n) };
    if (k.delta && (k.delta.added !== 0 || k.delta.removed !== 0)) {
      params.a = String(k.delta.added);
      params.b = String(k.delta.removed);
      return view('running', 'ok_delta', params);
    }
    return view('running', 'ok', params);
  }
  return view('idle', 'none');
}

/** Готовая подпись статуса: подставляет «когда», «через сколько», дату снимка и причину. */
export function statusLabel(v: KernelStatusView, t: TFn): string {
  const p: Record<string, string> = { ...v.params };
  if (p.ago_unit) {
    p.ago = t(`cfg.nodes.ago_${p.ago_unit}`, { n: p.ago_n ?? '0' });
  }
  if ('in' in p) {
    const minutes = Number(p.in);
    if (!Number.isFinite(minutes) || minutes <= 0) p.in = t('cfg.notice.in_soon');
    else if (minutes >= 60) p.in = t('cfg.notice.in_hours', { n: Math.round(minutes / 60) });
    else p.in = t('cfg.notice.in_minutes', { n: minutes });
  }
  if (v.key === P + 'rejected' || v.key === P + 'provider_error') {
    p.snapshot = p.date ? t(P + 'snapshot_from', { date: p.date }) : '';
    if (v.key === P + 'provider_error') {
      p.reason = p.code
        ? sourceErrorText({ code: p.code }, t).title
        : t(P + 'provider_error_plain');
    }
  }
  return t(v.key, p);
}

// --- Трафик ---

export type TrafficLevel = 'normal' | 'warning' | 'full';

/** Уровень индикатора: меньше 90% — норма, от 90% — предупреждение, 100% — исчерпан. */
export function trafficLevel(used: number, total: number): TrafficLevel {
  if (!(total > 0) || !(used > 0)) return 'normal';
  const ratio = used / total;
  if (ratio >= 1) return 'full';
  if (ratio >= 0.9) return 'warning';
  return 'normal';
}

/** Доля использованного трафика в процентах для ширины индикатора (0–100). */
export function trafficPercent(used: number, total: number): number {
  if (!(total > 0) || !(used > 0)) return 0;
  return Math.min(100, Math.round((used / total) * 100));
}

// --- Итог ручного «Обновить» ---

export interface RefreshToast {
  key: string;
  params: Record<string, string>;
  kind: 'success' | 'info' | 'warning' | 'error';
  /** Дополнение: ядра, запрос под которые не удался, пока остальные обновились. */
  extra?: { key: string; params: Record<string, string> };
}

/**
 * Ядра, отвергшие новый состав при проверке. outcome описывает остальные ядра:
 * applied или deferred при одном отвергнутом ядре значит, что оно осталось на
 * прежнем наборе.
 */
function refusedKernels(o: RefreshOutcome): string[] {
  return Object.entries(o.kernels ?? {})
    .filter(([, k]) => k.status === 'refused')
    .map(([name]) => kernelLabel(name))
    .sort();
}

/**
 * Ядра, запрос под которые не удался (сеть, отказ провайдера, ответ без подписки),
 * хотя общий итог не ошибка: другое ядро получило свежий набор или остался прежний.
 */
function failedKernels(o: RefreshOutcome): string[] {
  const failed = ['network', 'rejected', 'provider_error'];
  return Object.entries(o.kernels ?? {})
    .filter(([, k]) => failed.includes(k.status))
    .map(([name]) => kernelLabel(name))
    .sort();
}

/**
 * Тост инициатору ручного обновления (D-08, D-29): ключ, параметры и вид. Если один
 * запрос не удался, а общий итог не ошибка, тост предупреждает и называет ядра.
 */
export function refreshToast(o: RefreshOutcome): RefreshToast {
  const toast = baseRefreshToast(o);
  if (['unchanged', 'applied', 'deferred', 'pending_updated'].includes(o.outcome)) {
    const failed = failedKernels(o);
    if (failed.length > 0) {
      return {
        ...toast,
        kind: toast.kind === 'error' ? 'error' : 'warning',
        extra: { key: 'cfg.nodes.refresh_partial_failed', params: { kernels: failed.join(', ') } }
      };
    }
  }
  return toast;
}

function baseRefreshToast(o: RefreshOutcome): RefreshToast {
  const code = o.error?.code ?? '';
  const refused = refusedKernels(o);
  switch (o.outcome) {
    case 'unchanged':
      return { key: 'cfg.nodes.refresh_unchanged', params: {}, kind: 'info' };
    case 'applied':
      if (refused.length > 0) {
        return {
          key: 'cfg.nodes.refresh_partial',
          params: { a: String(o.added), b: String(o.removed), kernels: refused.join(', ') },
          kind: 'warning'
        };
      }
      return {
        key: 'cfg.nodes.refresh_applied',
        params: { a: String(o.added), b: String(o.removed) },
        kind: 'success'
      };
    case 'rejected':
      return { key: 'cfg.nodes.refresh_rejected', params: { code }, kind: 'warning' };
    case 'network':
      return { key: 'cfg.nodes.refresh_network', params: { code }, kind: 'warning' };
    case 'deferred':
      if (refused.length > 0) {
        return {
          key: 'cfg.nodes.refresh_partial_deferred',
          params: { kernels: refused.join(', ') },
          kind: 'warning'
        };
      }
      return { key: 'cfg.nodes.refresh_deferred', params: {}, kind: 'warning' };
    case 'pending_updated':
      return { key: 'cfg.nodes.refresh_pending', params: {}, kind: 'info' };
    default:
      return { key: 'cfg.nodes.refresh_failed', params: { code }, kind: 'error' };
  }
}

// --- Ссылки провайдера ---

/** Ссылка принимается только после разбора как адрес и только со схемой http или https. */
export function safeHttpUrl(raw: string | undefined): string | undefined {
  if (!raw) return undefined;
  try {
    const u = new URL(raw);
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.href : undefined;
  } catch {
    return undefined;
  }
}

/** Отказ провайдера, при котором слот устройства освобождают в личном кабинете. */
export function isDeviceRejected(k: KernelStateView | undefined): boolean {
  if (!k || k.status.code !== 'rejected') return false;
  const code = k.status.error_code ?? '';
  return code === 'all_stubs' || code === 'http_auth';
}

// --- Список узлов ---

/** Поле поиска появляется, когда в источнике больше узлов, чем этот порог. */
export const SEARCH_THRESHOLD = 12;
/** Сколько строк рисуется сразу и на сколько растёт «Показать ещё». */
export const PAGE_FIRST = 100;
export const PAGE_STEP = 50;

/** Фильтр по имени без учёта регистра; пустой запрос оставляет всё. */
export function filterByName(items: NodeItem[], query: string): NodeItem[] {
  const q = query.trim().toLowerCase();
  if (q === '') return items;
  return items.filter((i) => i.node.name.toLowerCase().includes(q));
}
