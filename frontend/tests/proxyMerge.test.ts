import { describe, it, expect } from 'vitest';
import { preserveInFlightLatency, type MeasurementCarrier } from '../src/lib/proxyMerge';

interface FakeProxy extends MeasurementCarrier {
  name: string;
  type: string;
  provider?: string;
}

describe('preserveInFlightLatency', () => {
  it('returns the same object reference when the protected set is empty (fast path)', () => {
    const incoming: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 10 }
    };
    const current: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 999 }
    };
    const result = preserveInFlightLatency(incoming, current, new Set());
    expect(result).toBe(incoming);
  });

  it('keeps delay/alive/history from current for a protected node with a measurement, other fields from incoming', () => {
    const incoming: Record<string, FakeProxy> = {
      'Node-A': {
        name: 'Node-A',
        type: 'Vless',
        provider: 'sub1',
        delay: 0,
        alive: false,
        history: []
      }
    };
    const current: Record<string, FakeProxy> = {
      'Node-A': {
        name: 'Node-A',
        type: 'Vless',
        provider: 'sub1',
        delay: 42,
        alive: true,
        history: [{ time: '2026-08-22T00:00:00Z', delay: 42 }]
      }
    };
    const result = preserveInFlightLatency(incoming, current, new Set(['Node-A']));
    expect(result['Node-A']).toEqual({
      name: 'Node-A',
      type: 'Vless',
      provider: 'sub1',
      delay: 42,
      alive: true,
      history: [{ time: '2026-08-22T00:00:00Z', delay: 42 }]
    });
  });

  it('leaves an unprotected node exactly as in incoming, even if current looks fresher', () => {
    const incoming: Record<string, FakeProxy> = {
      'Node-B': { name: 'Node-B', type: 'Vless', delay: 0, alive: false, history: [] }
    };
    const current: Record<string, FakeProxy> = {
      'Node-B': {
        name: 'Node-B',
        type: 'Vless',
        delay: 999,
        alive: true,
        history: [{ time: 'x', delay: 999 }]
      }
    };
    const result = preserveInFlightLatency(incoming, current, new Set(['Node-A']));
    expect(result['Node-B']).toEqual(incoming['Node-B']);
  });

  it('does not resurrect a protected node that disappeared from incoming', () => {
    const incoming: Record<string, FakeProxy> = {
      'Node-C': { name: 'Node-C', type: 'Vless', delay: 5 }
    };
    const current: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 42, history: [{ time: 'x', delay: 42 }] }
    };
    const result = preserveInFlightLatency(incoming, current, new Set(['Node-A']));
    expect(result['Node-A']).toBeUndefined();
    expect(Object.keys(result)).toEqual(['Node-C']);
  });

  it('takes the record entirely from incoming when current has no measurement (empty history, non-numeric delay)', () => {
    const incoming: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 15, alive: true, history: [] }
    };
    const current: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', history: [] }
    };
    const result = preserveInFlightLatency(incoming, current, new Set(['Node-A']));
    expect(result['Node-A']).toEqual(incoming['Node-A']);
  });

  it('takes the record entirely from incoming when current is missing history and delay altogether', () => {
    const incoming: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 15, alive: true }
    };
    const current: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless' }
    };
    const result = preserveInFlightLatency(incoming, current, new Set(['Node-A']));
    expect(result['Node-A']).toEqual(incoming['Node-A']);
  });

  it('ignores a protected name absent from both incoming and current without throwing', () => {
    const incoming: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 1 }
    };
    const current: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 1 }
    };
    expect(() => preserveInFlightLatency(incoming, current, new Set(['Ghost-Node']))).not.toThrow();
    const result = preserveInFlightLatency(incoming, current, new Set(['Ghost-Node']));
    expect(result['Ghost-Node']).toBeUndefined();
  });

  it('does not mutate incoming, current, or their nested history arrays', () => {
    const incomingHistory = [{ time: 'a', delay: 1 }];
    const currentHistory = [{ time: 'b', delay: 2 }];
    const incoming: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 1, history: incomingHistory }
    };
    const current: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 2, history: currentHistory }
    };
    const incomingSnapshot = JSON.parse(JSON.stringify(incoming));
    const currentSnapshot = JSON.parse(JSON.stringify(current));

    preserveInFlightLatency(incoming, current, new Set(['Node-A']));

    expect(incoming).toEqual(incomingSnapshot);
    expect(current).toEqual(currentSnapshot);
    expect(incoming['Node-A'].history).toBe(incomingHistory);
    expect(current['Node-A'].history).toBe(currentHistory);
  });

  it('carries the history array with the same elements in the same order (deep equal)', () => {
    const history = [
      { time: 't1', delay: 10 },
      { time: 't2', delay: 20 },
      { time: 't3', delay: 30 }
    ];
    const incoming: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 0, history: [] }
    };
    const current: Record<string, FakeProxy> = {
      'Node-A': { name: 'Node-A', type: 'Vless', delay: 30, history }
    };
    const result = preserveInFlightLatency(incoming, current, new Set(['Node-A']));
    expect(result['Node-A'].history).toEqual(history);
  });
});
