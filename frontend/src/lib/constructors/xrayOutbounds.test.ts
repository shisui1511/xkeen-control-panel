import { describe, it, expect } from 'vitest';
import {
  SYSTEM_OUTBOUND_TAGS,
  splitOutbounds,
  mergeOutbounds,
  dedupeByTag,
  uniqueTags
} from './xrayOutbounds';

const direct = { tag: 'direct', protocol: 'freedom', settings: { domainStrategy: 'UseIP' } };
const block = { tag: 'block', protocol: 'blackhole' };
const proxyA = { tag: 'proxyA', protocol: 'vless' };
const proxyB = { tag: 'proxyB', protocol: 'vmess' };

const roundTrip = (x: any[]) => {
  const { system, custom } = splitOutbounds(x);
  return mergeOutbounds(system, custom);
};

describe('xray outbounds split/merge', () => {
  it('lists the system tags', () => {
    expect(SYSTEM_OUTBOUND_TAGS).toEqual(['direct', 'block', 'dns-out']);
  });

  it('splits system entries from custom ones and restores the file', () => {
    const { system, custom } = splitOutbounds([direct, block]);
    expect(system).toEqual([
      { outbound: direct, at: 0 },
      { outbound: block, at: 0 }
    ]);
    expect(custom).toEqual([]);
    expect(mergeOutbounds(system, custom)).toEqual([direct, block]);
  });

  it('keeps the position of system entries', () => {
    const { system, custom } = splitOutbounds([proxyA, direct, block]);
    expect(system.map((s) => s.at)).toEqual([1, 1]);
    expect(custom).toEqual([proxyA]);
    expect(mergeOutbounds(system, custom)).toEqual([proxyA, direct, block]);
    expect(roundTrip([direct, proxyA, block])).toEqual([direct, proxyA, block]);
  });

  it('cleans duplicated tags and keeps the first entry settings', () => {
    const directDup = { tag: 'direct', protocol: 'freedom', settings: { other: true } };
    expect(roundTrip([direct, block, directDup, { ...block }, proxyA])).toEqual([
      direct,
      block,
      proxyA
    ]);
  });

  it('keeps the first of duplicated custom entries', () => {
    const { custom } = splitOutbounds([proxyA, { ...proxyA, protocol: 'other' }]);
    expect(custom).toEqual([proxyA]);
  });

  it('does not add default entries to a file without system ones', () => {
    expect(roundTrip([proxyA])).toEqual([proxyA]);
    expect(mergeOutbounds([], [proxyA])).toEqual([proxyA]);
  });

  it('puts system entries back within a shrunken custom list', () => {
    const { system } = splitOutbounds([proxyA, proxyB, direct, block]);
    expect(mergeOutbounds(system, [proxyA])).toEqual([proxyA, direct, block]);
    expect(mergeOutbounds(system, [])).toEqual([direct, block]);
  });

  it('does not duplicate a system tag imported as a custom entry', () => {
    const { system } = splitOutbounds([direct, block, proxyA]);
    expect(mergeOutbounds(system, [{ tag: 'direct', protocol: 'freedom' }, proxyA])).toEqual([
      direct,
      block,
      proxyA
    ]);
  });

  it('is an identity for files without duplicate tags', () => {
    const cases = [
      [],
      [direct],
      [direct, block, proxyA, proxyB],
      [proxyA, direct, proxyB, block],
      [proxyA, proxyB, direct, block],
      [direct, { tag: 'dns-out', protocol: 'dns' }, proxyA]
    ];
    for (const c of cases) expect(roundTrip(c)).toEqual(c);
  });

  it('keeps entries without a tag', () => {
    const noTag = { protocol: 'freedom' };
    expect(roundTrip([noTag, direct, noTag])).toEqual([noTag, direct, noTag]);
  });

  it('dedupeByTag drops repeated tags only', () => {
    expect(dedupeByTag([direct, proxyA, direct, proxyA])).toEqual([direct, proxyA]);
  });

  it('uniqueTags keeps the first occurrence order', () => {
    expect(uniqueTags(['direct', 'block', 'dns-out', 'direct', 'x', 'x'])).toEqual([
      'direct',
      'block',
      'dns-out',
      'x'
    ]);
  });
});
