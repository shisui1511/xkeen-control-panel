import { describe, it, expect } from 'vitest';
import {
  computeObservatoryStats,
  computeGroupHealthStats,
  groupMatchesLatencyFilter,
  getLastDelay,
  isProxyAlive,
  type ProxyLike,
  type NodeSnapshot
} from '../src/lib/proxyStats';

describe('proxyStats unit tests', () => {
  describe('getLastDelay & isProxyAlive', () => {
    it('returns delay from history if present', () => {
      const p: ProxyLike = {
        name: 'test',
        delay: 50,
        history: [
          { time: '1', delay: 100 },
          { time: '2', delay: 200 }
        ]
      };
      expect(getLastDelay(p)).toBe(200);
      expect(isProxyAlive(p)).toBe(true);
    });

    it('returns delay field when history is empty', () => {
      const p: ProxyLike = { name: 'test', delay: 75, alive: true };
      expect(getLastDelay(p)).toBe(75);
      expect(isProxyAlive(p)).toBe(true);
    });

    it('returns false for alive when history delay is 0', () => {
      const p: ProxyLike = {
        name: 'test',
        alive: true,
        history: [{ time: '1', delay: 0 }]
      };
      expect(isProxyAlive(p)).toBe(false);
    });
  });

  describe('computeObservatoryStats — invariants & scenarios', () => {
    it('satisfies Invariants A and B on empty input', () => {
      const res = computeObservatoryStats({});
      expect(res.healthy + res.degraded + res.down + res.unchecked).toBe(res.proxyNodes);
      expect(res.proxyNodes + res.systemNodes).toBe(res.totalNodes);
      expect(res.totalNodes).toBe(0);
      expect(res.avgLatency).toBe(0);
    });

    it('satisfies Invariants A and B on audit scenario (43 proxies + 2 system)', () => {
      const proxies: Record<string, ProxyLike> = {
        DIRECT: { name: 'DIRECT', type: 'Direct' },
        REJECT: { name: 'REJECT', type: 'Reject' },
        GLOBAL: { name: 'GLOBAL', type: 'Selector', all: [] }
      };

      // 13 fast (healthy)
      for (let i = 1; i <= 13; i++) {
        proxies[`fast-${i}`] = {
          name: `fast-${i}`,
          type: 'Vless',
          alive: true,
          history: [{ time: 't', delay: 100 }]
        };
      }

      // 1 mid (degraded)
      proxies['mid-1'] = {
        name: 'mid-1',
        type: 'Vless',
        alive: true,
        history: [{ time: 't', delay: 450 }]
      };

      // 29 bad (down)
      for (let i = 1; i <= 29; i++) {
        proxies[`down-${i}`] = {
          name: `down-${i}`,
          type: 'Vless',
          alive: false,
          history: [{ time: 't', delay: 0 }]
        };
      }

      const res = computeObservatoryStats(proxies);

      // Invariants
      expect(res.healthy + res.degraded + res.down + res.unchecked).toBe(res.proxyNodes);
      expect(res.proxyNodes + res.systemNodes).toBe(res.totalNodes);

      // Audit exact numbers
      expect(res.totalNodes).toBe(45);
      expect(res.proxyNodes).toBe(43);
      expect(res.systemNodes).toBe(2);
      expect(res.healthy).toBe(13);
      expect(res.degraded).toBe(1);
      expect(res.down).toBe(29);
      expect(res.unchecked).toBe(0);
      expect(res.avgLatency).toBe(125); // (13*100 + 450) / 14 = 1750/14 = 125
    });

    it('accounts for unchecked nodes (alive: true, delay: undefined)', () => {
      const proxies: Record<string, ProxyLike> = {
        'untested-1': { name: 'untested-1', type: 'Shadowsocks', alive: true },
        'untested-2': { name: 'untested-2', type: 'Shadowsocks', alive: true, delay: undefined }
      };
      const res = computeObservatoryStats(proxies);
      expect(res.proxyNodes).toBe(2);
      expect(res.unchecked).toBe(2);
      expect(res.healthy).toBe(0);
      expect(res.healthy + res.degraded + res.down + res.unchecked).toBe(res.proxyNodes);
    });

    it('merges subscription nodes without duplicating root proxies', () => {
      const proxies: Record<string, ProxyLike> = {
        'node-1': { name: 'node-1', type: 'Vless', alive: true, delay: 100 }
      };
      const subNodes = {
        sub1: [{ tag: 'node-1' }, { tag: 'node-2' }]
      };
      const subHealth = {
        sub1: {
          'node-1': { alive: true, tested: true, delay: 100 },
          'node-2': { alive: true, tested: true, delay: 200 }
        }
      };
      const res = computeObservatoryStats(proxies, subNodes, subHealth);
      expect(res.proxyNodes).toBe(2);
      expect(res.healthy).toBe(2);
      expect(res.healthy + res.degraded + res.down + res.unchecked).toBe(res.proxyNodes);
    });

    it('classifies thresholds correctly (120ms -> healthy, 450ms -> degraded, 900ms -> down, 0ms -> down, alive: false -> down)', () => {
      const proxies: Record<string, ProxyLike> = {
        p1: { name: 'p1', alive: true, delay: 120 },
        p2: { name: 'p2', alive: true, delay: 450 },
        p3: { name: 'p3', alive: true, delay: 900 },
        p4: { name: 'p4', alive: true, delay: 0 },
        p5: { name: 'p5', alive: false, delay: 120 }
      };
      const res = computeObservatoryStats(proxies);
      expect(res.healthy).toBe(1);
      expect(res.degraded).toBe(1);
      expect(res.down).toBe(3); // p3, p4, p5
      expect(res.unchecked).toBe(0);
    });
  });

  describe('computeGroupHealthStats — invariants & thresholds', () => {
    it('satisfies Invariant C on mixed group nodes', () => {
      const nodes = ['n1', 'n2', 'n3', 'n4', 'n5', 'DIRECT'];
      const snapshotMap: Record<string, NodeSnapshot> = {
        n1: { name: 'n1', alive: true, delay: 100 }, // fast
        n2: { name: 'n2', alive: true, delay: 350 }, // mid
        n3: { name: 'n3', alive: false, delay: 0 }, // bad
        n4: { name: 'n4', alive: true, delay: undefined }, // unchecked
        n5: { name: 'n5', alive: true, delay: 850 }, // bad
        DIRECT: { name: 'DIRECT', type: 'Direct', alive: true } // system
      };

      const res = computeGroupHealthStats(nodes, (name) => snapshotMap[name]);

      expect(res.fast + res.mid + res.bad + res.unchecked + res.system).toBe(res.total);
      expect(res.total).toBe(6);
      expect(res.fast).toBe(1);
      expect(res.mid).toBe(1);
      expect(res.bad).toBe(2);
      expect(res.unchecked).toBe(1);
      expect(res.system).toBe(1);
      expect(res.fastPct + res.midPct + res.badPct + res.uncheckedPct + res.systemPct).toBe(100);
    });

    it('handles empty input gracefully without division by zero', () => {
      const res = computeGroupHealthStats([], () => undefined);
      expect(res.total).toBe(0);
      expect(res.fast).toBe(0);
      expect(res.fastPct).toBe(0);
    });

    it('matches thresholds: 299ms fast, 300ms mid, 799ms mid, 800ms bad', () => {
      const nodes = ['a', 'b', 'c', 'd'];
      const snapshotMap: Record<string, NodeSnapshot> = {
        a: { name: 'a', alive: true, delay: 299 },
        b: { name: 'b', alive: true, delay: 300 },
        c: { name: 'c', alive: true, delay: 799 },
        d: { name: 'd', alive: true, delay: 800 }
      };
      const res = computeGroupHealthStats(nodes, (name) => snapshotMap[name]);
      expect(res.fast).toBe(1); // a
      expect(res.mid).toBe(2); // b, c
      expect(res.bad).toBe(1); // d
    });
  });

  describe('groupMatchesLatencyFilter', () => {
    const snapshotMap: Record<string, NodeSnapshot> = {
      fast1: { name: 'fast1', alive: true, delay: 100 },
      mid1: { name: 'mid1', alive: true, delay: 400 },
      bad1: { name: 'bad1', alive: false, delay: 0 },
      sys1: { name: 'sys1', type: 'Direct', alive: true }
    };
    const resolve = (name: string) => snapshotMap[name];

    it('returns true when filter is null', () => {
      expect(groupMatchesLatencyFilter(['bad1'], null, resolve)).toBe(true);
      expect(groupMatchesLatencyFilter([], null, resolve)).toBe(true);
    });

    it('matches healthy filter', () => {
      expect(groupMatchesLatencyFilter(['fast1', 'bad1'], 'healthy', resolve)).toBe(true);
      expect(groupMatchesLatencyFilter(['mid1', 'bad1'], 'healthy', resolve)).toBe(false);
    });

    it('matches degraded filter', () => {
      expect(groupMatchesLatencyFilter(['mid1', 'bad1'], 'degraded', resolve)).toBe(true);
      expect(groupMatchesLatencyFilter(['fast1', 'bad1'], 'degraded', resolve)).toBe(false);
    });

    it('matches down filter', () => {
      expect(groupMatchesLatencyFilter(['bad1'], 'down', resolve)).toBe(true);
      expect(groupMatchesLatencyFilter(['fast1', 'mid1'], 'down', resolve)).toBe(false);
    });

    it('returns false for system-only groups', () => {
      expect(groupMatchesLatencyFilter(['sys1'], 'healthy', resolve)).toBe(false);
      expect(groupMatchesLatencyFilter(['sys1'], 'degraded', resolve)).toBe(false);
      expect(groupMatchesLatencyFilter(['sys1'], 'down', resolve)).toBe(false);
    });
  });
});
