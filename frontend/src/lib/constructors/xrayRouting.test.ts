import { describe, it, expect } from 'vitest';
import {
  rulesFromConfig,
  rulesToConfig,
  cleanRule,
  observatoryFor,
  cleanBalancer,
  lineDiff,
  hasJsonComments,
  parseXrayFileText,
  blankJsonComments
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

describe('parseXrayFileText', () => {
  it('treats a comment-only file without braces as an empty stub', () => {
    expect(parseXrayFileText('// Создайте файл по ссылке\n')).toEqual({
      data: undefined,
      unparsed: false
    });
  });

  it('treats a braces-with-comment stub as an empty object', () => {
    expect(parseXrayFileText('{\n// comment\n}')).toEqual({ data: {}, unparsed: false });
  });

  it('treats empty and whitespace-only text as not unparsed', () => {
    expect(parseXrayFileText('').unparsed).toBe(false);
    expect(parseXrayFileText('   \n').unparsed).toBe(false);
  });

  it('keeps a real syntax error as unparsed', () => {
    expect(parseXrayFileText('{ "routing": ')).toEqual({ data: undefined, unparsed: true });
  });

  it('parses a file with leading block comment', () => {
    expect(parseXrayFileText('/* a */ { "x": 1 }')).toEqual({ data: { x: 1 }, unparsed: false });
  });
});

describe('blankJsonComments', () => {
  it('keeps length and line breaks while making comments parseable', () => {
    const text = '{ // c\n "a": 1 }';
    const out = blankJsonComments(text);
    expect(out.length).toBe(text.length);
    expect(out.indexOf('\n')).toBe(text.indexOf('\n'));
    expect(JSON.parse(out)).toEqual({ a: 1 });
  });

  it('leaves comment-like text inside strings untouched', () => {
    const text = '{"u":"http://x//y"}';
    expect(blankJsonComments(text)).toBe(text);
  });

  it('blanks block comments across lines, keeping the line breaks', () => {
    const text = '/* a\nb */{}';
    const out = blankJsonComments(text);
    expect(out.length).toBe(text.length);
    expect(out.indexOf('\n')).toBe(text.indexOf('\n'));
    expect(JSON.parse(out)).toEqual({});
  });

  it('returns text without comments unchanged', () => {
    const text = '{\n  "a": [1, 2]\n}';
    expect(blankJsonComments(text)).toBe(text);
  });

  it('handles an unterminated block comment without throwing', () => {
    const text = '{} /* open';
    expect(blankJsonComments(text).length).toBe(text.length);
  });
});
