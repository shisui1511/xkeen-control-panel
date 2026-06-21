/**
 * mihomoYaml.test.ts — Unit-тесты для генерации и парсинга YAML конфигураций Mihomo.
 */

import { test, describe, expect } from 'vitest';
import { slugifyProviderName, generateYAML, populateMihomoFromYAML } from '../src/lib/mihomoYaml';

describe('slugifyProviderName', () => {
  test('транслитерация кириллических имен подписок', () => {
    expect(slugifyProviderName('Моя Подписка', '', 'p0')).toBe('moya-podpiska');
    expect(slugifyProviderName('Сервис #1!', '', 'p0')).toBe('servis-1');
    expect(slugifyProviderName('---', '', 'sub-0')).toBe('sub-0');
    expect(slugifyProviderName('', '', 'provider-0')).toBe('provider-0');
    expect(slugifyProviderName('Google-dns_123', '', 'p0')).toBe('google-dns-123');
  });

  test('URL fallback when name is empty', () => {
    expect(slugifyProviderName('', 'https://example.com/subscriptions/my-sub.yaml', 'fallback')).toBe('my-sub-yaml');
    expect(slugifyProviderName('', 'https://example.com/api/v1/sub', 'fallback')).toBe('sub');
    expect(slugifyProviderName('', 'invalid-url', 'fallback')).toBe('fallback');
    expect(slugifyProviderName('', '', 'fallback')).toBe('fallback');
  });
});

describe('Mihomo YAML generation with proxy-providers and groups', () => {
  test('генерация YAML включает только Mihomo-подписки и новые поля групп', () => {
    const mockState: any = {
      existingTproxyPort: 12345,
      existingRedirPort: 12346,
      subscriptions: [
        { id: 'xray-sub', name: 'Xray Sub', enabled: true, type: 'xray', url: 'https://xray.com' }
      ],
      mihomoProviders: [
        {
          id: 'mihomo-sub',
          name: 'Моя Подписка',
          enabled: true,
          type: 'mihomo',
          url: 'https://mihomo.com/sub',
          interval: 2,
          hwid_token: 'test-hwid'
        }
      ],
      capabilities: {
        kernels: {
          mihomo: { version: '1.18.12' }
        }
      },
      proxies: [],
      groups: [
        {
          id: 'group-1',
          name: 'LoadBalanceGroup',
          type: 'load-balance',
          proxies: ['DIRECT'],
          useProviders: ['moya-podpiska'],
          strategy: 'consistent-hashing'
        }
      ],
      rules: [],
      dns: {},
      tun: {},
      sniffer: {}
    };

    const yaml = generateYAML(mockState);

    // Должно содержать proxy-providers с транслитерированным именем
    expect(yaml).toContain('proxy-providers:');
    expect(yaml).toContain('  moya-podpiska:');
    expect(yaml).toContain('    type: http');
    expect(yaml).toContain('    path: ./providers/moya-podpiska.yaml');
    expect(yaml).toContain('    url: "https://mihomo.com/sub"');
    expect(yaml).toContain('    interval: 7200'); // 2 * 3600
    expect(yaml).toContain('      User-Agent:\n        - "mihomo/1.18.12"');
    expect(yaml).toContain('      x-hwid:\n        - "test-hwid"');

    // Не должно содержать xray-sub в proxy-providers
    expect(yaml).not.toContain('xray-sub:');
    expect(yaml).not.toContain('xray-sub.yaml');

    // Группа должна содержать use и strategy
    expect(yaml).toContain('  - name: "LoadBalanceGroup"');
    expect(yaml).toContain('    type: load-balance');
    expect(yaml).toContain('    use:\n      - "moya-podpiska"');
    expect(yaml).toContain('    strategy: consistent-hashing');
    expect(yaml).toContain('    proxies:\n      - "DIRECT"');
  });
});

describe('Mihomo YAML parsing (populateMihomoFromYAML)', () => {
  test('парсинг inline use и strategy', () => {
    const yaml = `
proxy-groups:
  - name: GroupA
    type: load-balance
    use: [moya-podpiska, other-provider]
    strategy: round-robin
    proxies:
      - DIRECT
`;
    const result = populateMihomoFromYAML(yaml);
    expect(result.groups).toHaveLength(1);
    const g = result.groups[0];
    expect(g.name).toBe('GroupA');
    expect(g.type).toBe('load-balance');
    expect(g.useProviders).toEqual(['moya-podpiska', 'other-provider']);
    expect(g.strategy).toBe('round-robin');
  });

  test('парсинг multiline use block и strategy', () => {
    const yaml = `
proxy-groups:
  - name: GroupB
    type: load-balance
    use:
      - moya-podpiska
      - second-provider
    strategy: sticky-sessions
    proxies:
      - DIRECT
`;
    const result = populateMihomoFromYAML(yaml);
    expect(result.groups).toHaveLength(1);
    const g = result.groups[0];
    expect(g.name).toBe('GroupB');
    expect(g.type).toBe('load-balance');
    expect(g.useProviders).toEqual(['moya-podpiska', 'second-provider']);
    expect(g.strategy).toBe('sticky-sessions');
  });
});

describe('Mihomo YAML parsing (populateMihomoFromYAML) OR and no-resolve', () => {
  test('parsing OR rules and no-resolve rules', () => {
    const yaml = `
rules:
  - OR,((RULE-SET,meta@domain),(RULE-SET,meta@ipcidr,no-resolve)),Meta
  - OR,((DOMAIN-SUFFIX,gql.twitch.tv),(DOMAIN-SUFFIX,usher.ttvnw.net)),Заблок. сервисы
  - IP-CIDR,192.168.1.0/24,DIRECT,no-resolve
  - DOMAIN,google.com,ProxyGroup
`;
    const result = populateMihomoFromYAML(yaml);
    
    expect(result.activeRuleProvider).toBe('zkeen');
    
    const customRules = result.rules;
    expect(customRules).toHaveLength(2);
    
    expect(customRules[0].type).toBe('IP-CIDR');
    expect(customRules[0].value).toBe('192.168.1.0/24');
    expect(customRules[0].outbound).toBe('DIRECT');
    expect(customRules[0].noResolve).toBe(true);

    expect(customRules[1].type).toBe('DOMAIN');
    expect(customRules[1].value).toBe('google.com');
    expect(customRules[1].outbound).toBe('ProxyGroup');
    expect(customRules[1].noResolve).toBeFalsy();
  });

  test('generating OR rules and no-resolve rules', () => {
    const mockState: any = {
      activeRuleProvider: 'zkeen',
      proxies: [],
      groups: [
        { name: 'Meta', enabled: true, proxies: [] },
        { name: 'Telegram', enabled: true, proxies: [] },
        { name: 'Google', enabled: true, proxies: [] },
        { name: 'DIRECT', enabled: true, proxies: [] },
        { name: 'Заблок. сервисы', enabled: true, proxies: [] },
        { name: 'AI', enabled: true, proxies: [] },
        { name: 'Steam', enabled: true, proxies: [] },
        { name: 'Spotify', enabled: true, proxies: [] },
        { name: 'Reddit', enabled: true, proxies: [] },
        { name: 'YouTube', enabled: true, proxies: [] },
        { name: 'Twitch', enabled: true, proxies: [] },
        { name: 'Twitter', enabled: true, proxies: [] },
        { name: 'Discord', enabled: true, proxies: [] },
        { name: 'Speedtest', enabled: true, proxies: [] },
        { name: 'GitHub', enabled: true, proxies: [] },
        { name: 'CDN', enabled: true, proxies: [] },
        { name: 'TikTok', enabled: true, proxies: [] }
      ],
      rules: [
        { id: '1', type: 'IP-CIDR', value: '192.168.1.0/24', outbound: 'DIRECT', noResolve: true },
        { id: '2', type: 'DOMAIN', value: 'google.com', outbound: 'DIRECT' },
        { id: '3', type: 'OR', value: '((DOMAIN,test.com),(DOMAIN,test2.com))', outbound: 'DIRECT' }
      ],
      dns: {},
      tun: {},
      sniffer: {}
    };

    const yaml = generateYAML(mockState);
    
    expect(yaml).toContain('- OR,((RULE-SET,meta@domain),(RULE-SET,meta@ipcidr,no-resolve)),Meta');
    expect(yaml).toContain('- OR,((RULE-SET,telegram@domain),(RULE-SET,telegram@ipcidr,no-resolve)),Telegram');
    expect(yaml).toContain('- OR,((DOMAIN-SUFFIX,gql.twitch.tv),(DOMAIN-SUFFIX,usher.ttvnw.net)),Заблок. сервисы');

    expect(yaml).toContain('- IP-CIDR,192.168.1.0/24,DIRECT,no-resolve');
    expect(yaml).toContain('- DOMAIN,google.com,DIRECT');
    expect(yaml).toContain('- OR,((DOMAIN,test.com),(DOMAIN,test2.com)),DIRECT');
  });

  test('parsing nested proxy options (reality-opts, ws-opts, obfs) and preservedKeys exclusions', () => {
    const yaml = `
external-controller: 127.0.0.1:9090
proxy-providers:
  test:
    type: http
proxies:
  - name: "vless-reality"
    type: vless
    server: server.com
    port: 443
    uuid: my-uuid
    reality-opts:
      public-key: my-pubkey
      short-id: my-shortid
  - name: "hysteria-obfs"
    type: hysteria2
    server: server.com
    port: 443
    obfs:
      type: simple
      password: my-obfs-pass
  - name: "vmess-ws"
    type: vmess
    server: server.com
    port: 443
    uuid: my-uuid
    network: ws
    ws-opts:
      path: /my-path
`;
    const result = populateMihomoFromYAML(yaml);
    
    expect(result.preservedKeys).not.toContain('external-controller');
    expect(result.preservedKeys).not.toContain('proxy-providers');
    
    expect(result.proxies).toHaveLength(3);
    
    const p1 = result.proxies[0];
    expect(p1.name).toBe('vless-reality');
    expect(p1.publicKey).toBe('my-pubkey');
    expect(p1.shortId).toBe('my-shortid');
    
    const p2 = result.proxies[1];
    expect(p2.name).toBe('hysteria-obfs');
    expect(p2.obfsType).toBe('simple');
    expect(p2.obfsPassword).toBe('my-obfs-pass');
    
    const p3 = result.proxies[2];
    expect(p3.name).toBe('vmess-ws');
    expect(p3.wsPath).toBe('/my-path');
  });
});

