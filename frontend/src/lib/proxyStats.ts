// Чистые функции статистики виджета и полос здоровья поверх canonical classifyLatency.
// Модуль не имеет обращений к DOM и полностью покрывается unit-тестами.

import {
  classifyLatency,
  isSystemProxy,
  isProxyGroupType,
  LATENCY_DEGRADED_MS,
  type LatencyBucket
} from './proxyClassification';

export interface ProxyLike {
  name: string;
  type?: string;
  alive?: boolean;
  delay?: number;
  now?: string;
  all?: string[];
  history?: { time: string; delay: number }[];
}

export function getLastDelay(p: ProxyLike | undefined): number | undefined {
  if (!p) return undefined;
  if (Array.isArray(p.history) && p.history.length > 0) {
    const last = p.history[p.history.length - 1];
    if (typeof last?.delay === 'number') return last.delay;
  }
  return typeof p.delay === 'number' ? p.delay : undefined;
}

export function isProxyAlive(p: ProxyLike | undefined): boolean {
  if (!p) return false;
  if (Array.isArray(p.history) && p.history.length > 0) {
    const last = p.history[p.history.length - 1];
    return typeof last?.delay === 'number' ? last.delay > 0 : false;
  }
  return p.alive ?? false;
}

export interface ObservatoryStats {
  totalNodes: number;
  proxyNodes: number;
  systemNodes: number;
  healthy: number;
  degraded: number;
  down: number;
  unchecked: number;
  avgLatency: number;
}

export interface SubNodeLike {
  tag?: string;
  name?: string;
}

export interface NodeHealthLike {
  alive: boolean;
  delay?: number;
  tested?: boolean;
}

export function computeObservatoryStats(
  proxies: Record<string, ProxyLike>,
  subNodes?: Record<string, SubNodeLike[]>,
  subHealth?: Record<string, Record<string, NodeHealthLike>>
): ObservatoryStats {
  const nodeMap = new Map<string, { alive: boolean; delay?: number }>();
  const systemSet = new Set<string>();

  if (proxies) {
    for (const p of Object.values(proxies)) {
      if (!p || !p.name) continue;
      if (isProxyGroupType(p.type)) continue;
      if (isSystemProxy(p.name, p.type)) {
        systemSet.add(p.name);
        continue;
      }
      nodeMap.set(p.name, {
        alive: isProxyAlive(p),
        delay: getLastDelay(p)
      });
    }
  }

  if (subNodes) {
    for (const [subId, nodes] of Object.entries(subNodes)) {
      if (!Array.isArray(nodes)) continue;
      const hMap = subHealth?.[subId] || {};
      for (const n of nodes) {
        const key = n?.tag || n?.name;
        if (!key) continue;
        if (nodeMap.has(key) || systemSet.has(key)) continue;
        if (isSystemProxy(key)) {
          systemSet.add(key);
          continue;
        }
        const h = hMap[key];
        nodeMap.set(key, {
          alive: h ? h.alive : false,
          delay: h?.tested ? h.delay : undefined
        });
      }
    }
  }

  let healthy = 0;
  let degraded = 0;
  let down = 0;
  let unchecked = 0;
  let totalLatency = 0;
  let latencyCount = 0;

  for (const node of nodeMap.values()) {
    const bucket: LatencyBucket = classifyLatency(node.delay, node.alive);
    if (bucket === 'fast') healthy++;
    else if (bucket === 'mid') degraded++;
    else if (bucket === 'bad') down++;
    else unchecked++;

    if (typeof node.delay === 'number' && node.delay > 0 && node.delay < LATENCY_DEGRADED_MS) {
      totalLatency += node.delay;
      latencyCount++;
    }
  }

  const proxyNodes = nodeMap.size;
  const systemNodes = systemSet.size;
  const totalNodes = proxyNodes + systemNodes;
  const avgLatency = latencyCount > 0 ? Math.round(totalLatency / latencyCount) : 0;

  return {
    totalNodes,
    proxyNodes,
    systemNodes,
    healthy,
    degraded,
    down,
    unchecked,
    avgLatency
  };
}

export interface GroupHealthStats {
  fast: number;
  mid: number;
  bad: number;
  unchecked: number;
  system: number;
  total: number;
  fastPct: number;
  midPct: number;
  badPct: number;
  uncheckedPct: number;
  systemPct: number;
}

export interface NodeSnapshot {
  name: string;
  type?: string;
  delay?: number;
  alive: boolean;
}

export function computeGroupHealthStats(
  nodeNames: string[],
  resolve: (name: string) => NodeSnapshot | undefined
): GroupHealthStats {
  let fast = 0;
  let mid = 0;
  let bad = 0;
  let unchecked = 0;
  let system = 0;

  const total = Array.isArray(nodeNames) ? nodeNames.length : 0;

  if (total > 0) {
    for (const name of nodeNames) {
      const snapshot = resolve(name);
      if (!snapshot) {
        unchecked++;
        continue;
      }
      if (isSystemProxy(snapshot.name, snapshot.type)) {
        system++;
        continue;
      }
      const bucket = classifyLatency(snapshot.delay, snapshot.alive);
      if (bucket === 'fast') fast++;
      else if (bucket === 'mid') mid++;
      else if (bucket === 'bad') bad++;
      else unchecked++;
    }
  }

  let fastPct = 0;
  let midPct = 0;
  let badPct = 0;
  let uncheckedPct = 0;
  let systemPct = 0;

  if (total > 0) {
    const buckets = [
      { key: 'fast', count: fast },
      { key: 'mid', count: mid },
      { key: 'bad', count: bad },
      { key: 'unchecked', count: unchecked },
      { key: 'system', count: system }
    ];

    const raw = buckets.map((b) => {
      const exact = (b.count / total) * 100;
      const floor = Math.floor(exact);
      return { key: b.key, count: b.count, floor, rem: exact - floor, pct: floor };
    });

    const sumFloor = raw.reduce((s, r) => s + r.floor, 0);
    let diff = 100 - sumFloor;

    // Сортируем по величине остатка для распределения недостающих процентов
    const sorted = [...raw].filter((r) => r.count > 0).sort((a, b) => b.rem - a.rem);

    for (let i = 0; i < diff && i < sorted.length; i++) {
      sorted[i].pct += 1;
    }

    const pctMap = Object.fromEntries(raw.map((r) => [r.key, r.pct]));
    fastPct = pctMap.fast || 0;
    midPct = pctMap.mid || 0;
    badPct = pctMap.bad || 0;
    uncheckedPct = pctMap.unchecked || 0;
    systemPct = pctMap.system || 0;
  }

  return {
    fast,
    mid,
    bad,
    unchecked,
    system,
    total,
    fastPct,
    midPct,
    badPct,
    uncheckedPct,
    systemPct
  };
}

export type ObservatoryFilter = 'healthy' | 'degraded' | 'down' | 'unchecked' | null;

export function groupMatchesLatencyFilter(
  nodeNames: string[],
  filter: ObservatoryFilter,
  resolve: (name: string) => NodeSnapshot | undefined
): boolean {
  if (filter === null) return true;
  if (!Array.isArray(nodeNames) || nodeNames.length === 0) return false;

  for (const name of nodeNames) {
    const snapshot = resolve(name);
    if (!snapshot) {
      if (filter === 'unchecked') return true;
      continue;
    }
    if (isSystemProxy(snapshot.name, snapshot.type)) continue;
    const bucket = classifyLatency(snapshot.delay, snapshot.alive);
    if (filter === 'healthy' && bucket === 'fast') return true;
    if (filter === 'degraded' && bucket === 'mid') return true;
    if (filter === 'down' && bucket === 'bad') return true;
    if (filter === 'unchecked' && bucket === 'unchecked') return true;
  }
  return false;
}
