import { describe, it, expect } from 'vitest';
import {
  rulesFromConfig,
  rulesToConfig,
  cleanRule,
  observatoryFor,
  cleanBalancer,
  lineDiff,
  hasJsonComments
} from './xrayRouting';

describe('xray routing model', () => {
  it('keeps every rule field and drops UI-only keys', () => {
    const rules = rulesFromConfig({
      rules: [
        {
          balancerTag: 'best',
          domain: ['geosite:youtube'],
          user: ['a@b'],
          attrs: { ':method': 'GET' },
          ruleTag: 'yt'
        },
        { outboundTag: 'direct', source: ['172.16.0.5'], sourcePort: '1000-2000' }
      ]
    });
    const cfg = rulesToConfig(rules);
    expect(cfg.rules).toEqual([
      {
        balancerTag: 'best',
        domain: ['geosite:youtube'],
        user: ['a@b'],
        attrs: { ':method': 'GET' },
        ruleTag: 'yt'
      },
      { outboundTag: 'direct', source: ['172.16.0.5'], sourcePort: '1000-2000' }
    ]);
    expect(cfg.xcpDisabledRules).toBeUndefined();
  });

  it('stores disabled rules with their position and restores them', () => {
    const rules = rulesFromConfig({
      rules: [{ outboundTag: 'a' }, { outboundTag: 'b' }, { outboundTag: 'c' }]
    });
    rules[1].enabled = false;
    const cfg = rulesToConfig(rules);
    expect(cfg.rules.map((r) => r.outboundTag)).toEqual(['a', 'c']);
    expect(cfg.xcpDisabledRules).toEqual([{ outboundTag: 'b', xcpPosition: 1 }]);

    const back = rulesFromConfig(cfg);
    expect(back.map((r) => [r.outboundTag, r.enabled])).toEqual([
      ['a', true],
      ['b', false],
      ['c', true]
    ]);
  });

  it('prefers the balancer over an outbound and drops empty values', () => {
    expect(
      cleanRule({
        id: 'x',
        enabled: true,
        outboundTag: 'proxy',
        balancerTag: 'lb',
        domain: [],
        port: '',
        domainRaw: 'x'
      })
    ).toEqual({ balancerTag: 'lb' });
  });

  it('builds observatory only for probing strategies', () => {
    expect(
      observatoryFor([{ tag: 'r', selector: ['p'], strategy: { type: 'random' } }])
    ).toBeUndefined();
    expect(
      observatoryFor([
        { tag: 'a', selector: ['vless-', 'trojan-'], strategy: { type: 'leastPing' } },
        { tag: 'b', selector: ['vless-', 'ss-'], strategy: { type: 'leastLoad' } }
      ])
    ).toEqual({
      subjectSelector: ['ss-', 'trojan-', 'vless-'],
      probeUrl: 'https://www.google.com/generate_204',
      probeInterval: '1m',
      enableConcurrency: true
    });
  });

  it('cleans balancers', () => {
    expect(
      cleanBalancer({
        tag: ' lb ',
        selector: ['a', ''],
        strategy: { type: 'random' },
        fallbackTag: ''
      })
    ).toEqual({
      tag: 'lb',
      selector: ['a']
    });
  });

  it('diffs lines', () => {
    expect(lineDiff('a\nb\nc', 'a\nx\nc')).toEqual([
      { kind: 'same', text: 'a' },
      { kind: 'del', text: 'b' },
      { kind: 'add', text: 'x' },
      { kind: 'same', text: 'c' }
    ]);
  });

  it('detects comments outside strings', () => {
    expect(hasJsonComments('{"a": "http://x"}')).toBe(false);
    expect(hasJsonComments('{\n // c\n "a": 1}')).toBe(true);
    expect(hasJsonComments('{"a": 1 /* c */}')).toBe(true);
  });
});

describe('jsonc helpers', () => {
  it('parses JSON with comments and keeps URLs in strings', async () => {
    const { parseJsonc } = await import('./xrayRouting');
    expect(parseJsonc('{\n // c\n "u": "https://x/y", /* b */ "n": 1\n}')).toEqual({
      u: 'https://x/y',
      n: 1
    });
    expect(parseJsonc('')).toBeUndefined();
    expect(parseJsonc('{bad')).toBeUndefined();
    expect(parseJsonc('{"s": "a\\"//b"}')).toEqual({ s: 'a"//b' });
  });
});

describe('dns over proxy rules', () => {
  it('builds managed rules and takes them back out of the rule list', async () => {
    const { dnsOverProxyRules, takeDnsOverProxyRules, rulesFromConfig, DNS_OVER_PROXY_TAG } =
      await import('./xrayRouting');
    const managed = dnsOverProxyRules('dns-in', 'vless-reality');
    expect(managed.map((r) => r.outboundTag)).toEqual(['direct', 'vless-reality']);
    expect(managed.every((r) => r.ruleTag === DNS_OVER_PROXY_TAG)).toBe(true);

    const ui = rulesFromConfig({ rules: [...managed, { outboundTag: 'direct', port: '53' }] });
    const { enabled, rest } = takeDnsOverProxyRules(ui);
    expect(enabled).toBe(true);
    expect(rest.map((r) => r.port)).toEqual(['53']);
    expect(takeDnsOverProxyRules(rest).enabled).toBe(false);
  });
});
