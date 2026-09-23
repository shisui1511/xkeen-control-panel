import { describe, it, expect } from 'vitest';
import { summarizeGroupDelay } from './groupDelay';

describe('summarizeGroupDelay', () => {
  const members = ['DIRECT', 'REJECT', 'node-a', 'Fastest', 'YouTube', 'dead'];

  it('counts alive members, best delay and current selection', () => {
    const res = summarizeGroupDelay(
      { DIRECT: 55, 'node-a': 120, Fastest: 26, YouTube: 81, dead: 0 },
      members,
      'YouTube'
    );
    expect(res).toEqual({ total: 4, alive: 3, best: 26, current: 81 });
  });

  it('ignores built-in outbounds and missing entries', () => {
    const res = summarizeGroupDelay({ DIRECT: 10 }, members, 'DIRECT');
    expect(res).toEqual({ total: 4, alive: 0, best: null, current: 10 });
  });

  it('handles an empty response', () => {
    expect(summarizeGroupDelay(null, members)).toEqual({
      total: 4,
      alive: 0,
      best: null,
      current: null
    });
  });
});
