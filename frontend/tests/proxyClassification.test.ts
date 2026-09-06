import { describe, it, expect } from 'vitest';
import {
  classifyLatency,
  isSystemProxy,
  isProxyGroupType,
  isReferencedByOtherGroup,
  classifyGroupRole,
  splitGroupsByRole,
  type ClassifiableGroup
} from '../src/lib/proxyClassification';

describe('classifyLatency', () => {
  it('returns unchecked when delay is undefined', () => {
    expect(classifyLatency(undefined, true)).toBe('unchecked');
  });

  it('classifies fast/mid/bad buckets correctly', () => {
    expect(classifyLatency(120, true)).toBe('fast');
    expect(classifyLatency(450, true)).toBe('mid');
    expect(classifyLatency(900, true)).toBe('bad');
    expect(classifyLatency(0, true)).toBe('bad');
    expect(classifyLatency(120, false)).toBe('bad');
  });

  it('respects exact threshold boundaries', () => {
    expect(classifyLatency(299, true)).toBe('fast');
    expect(classifyLatency(300, true)).toBe('mid');
    expect(classifyLatency(799, true)).toBe('mid');
    expect(classifyLatency(800, true)).toBe('bad');
  });
});

describe('isSystemProxy', () => {
  it('recognizes system proxy names case-insensitively', () => {
    expect(isSystemProxy('DIRECT')).toBe(true);
    expect(isSystemProxy('REJECT')).toBe(true);
    expect(isSystemProxy('PASS')).toBe(true);
    expect(isSystemProxy('COMPATIBLE')).toBe(true);
    expect(isSystemProxy('REJECT-DROP')).toBe(true);
  });

  it('falls back to type when name does not match', () => {
    expect(isSystemProxy('sp-01', 'Shadowsocks')).toBe(false);
    expect(isSystemProxy('sp-01', 'Direct')).toBe(true);
  });
});

describe('isProxyGroupType', () => {
  it('recognizes all known group types case-insensitively', () => {
    expect(isProxyGroupType('Selector')).toBe(true);
    expect(isProxyGroupType('URLTest')).toBe(true);
    expect(isProxyGroupType('Fallback')).toBe(true);
    expect(isProxyGroupType('LoadBalance')).toBe(true);
    expect(isProxyGroupType('Relay')).toBe(true);
  });

  it('rejects unknown types', () => {
    expect(isProxyGroupType('Shadowsocks')).toBe(false);
    expect(isProxyGroupType(undefined)).toBe(false);
  });
});

const baseGroups: ClassifiableGroup[] = [
  { name: 'GLOBAL', type: 'Selector', now: 'YouTube', all: ['YouTube', 'QUIC', 'Заблок. сервисы'] },
  {
    name: 'Заблок. сервисы',
    type: 'Selector',
    now: 'sp',
    all: ['sp', 'sp2']
  },
  { name: 'YouTube', type: 'Selector', now: 'Заблок. сервисы', all: ['Заблок. сервисы', 'sp'] },
  { name: 'QUIC', type: 'Selector', now: 'REJECT', all: ['REJECT'] }
];

describe('isReferencedByOtherGroup', () => {
  it('is true when a non-GLOBAL group references the name', () => {
    expect(isReferencedByOtherGroup('Заблок. сервисы', baseGroups)).toBe(true);
  });

  it('excludes GLOBAL from the referencing check', () => {
    expect(isReferencedByOtherGroup('YouTube', baseGroups)).toBe(false);
  });
});

describe('classifyGroupRole', () => {
  const proxyTypes: Record<string, string | undefined> = {
    sp: 'Shadowsocks',
    sp2: 'Shadowsocks',
    REJECT: 'Reject'
  };

  it('classifies GLOBAL as core via keyword pattern', () => {
    expect(classifyGroupRole(baseGroups[0], baseGroups, new Set(), proxyTypes)).toBe('core');
  });

  it('classifies "Заблок. сервисы" as core via keyword pattern', () => {
    expect(classifyGroupRole(baseGroups[1], baseGroups, new Set(), proxyTypes)).toBe('core');
  });

  it('classifies QUIC (all-system all[]) as system', () => {
    expect(classifyGroupRole(baseGroups[3], baseGroups, new Set(), proxyTypes)).toBe('system');
  });

  it('classifies YouTube (unreferenced, non-system) as service', () => {
    expect(classifyGroupRole(baseGroups[2], baseGroups, new Set(), proxyTypes)).toBe('service');
  });

  it('pinning overrides all other classification', () => {
    expect(classifyGroupRole(baseGroups[2], baseGroups, new Set(['YouTube']), proxyTypes)).toBe(
      'core'
    );
  });
});

describe('splitGroupsByRole', () => {
  it('buckets groups and preserves order and total count', () => {
    const proxyTypes: Record<string, string | undefined> = {
      sp: 'Shadowsocks',
      sp2: 'Shadowsocks',
      REJECT: 'Reject'
    };
    const result = splitGroupsByRole(baseGroups, new Set(), proxyTypes);
    expect(result.core.map((g) => g.name)).toEqual(['GLOBAL', 'Заблок. сервисы']);
    expect(result.service.map((g) => g.name)).toEqual(['YouTube']);
    expect(result.system.map((g) => g.name)).toEqual(['QUIC']);
    expect(result.core.length + result.service.length + result.system.length).toBe(
      baseGroups.length
    );
  });
});
