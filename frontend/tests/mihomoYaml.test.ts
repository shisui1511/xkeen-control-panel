/**
 * mihomoYaml.test.ts — Unit-тесты для генерации и парсинга YAML конфигураций Mihomo.
 */

import { test, describe, expect } from 'vitest';
import { slugifyProviderName, generateYAML, populateMihomoFromYAML } from '../src/lib/mihomoYaml';

describe('slugifyProviderName', () => {
  test('транслитерация кириллических имен подписок', () => {
    expect(slugifyProviderName('', 'Моя Подписка', '', 'p0')).toBe('moya-podpiska');
    expect(slugifyProviderName('', 'Сервис #1!', '', 'p0')).toBe('servis-1');
    expect(slugifyProviderName('', '---', '', 'sub-0')).toBe('sub-0');
    expect(slugifyProviderName('', '', '', 'provider-0')).toBe('provider-0');
    expect(slugifyProviderName('', 'Google-dns_123', '', 'p0')).toBe('Google-dns-123');
  });

  test('fallback to ID when name/title are empty', () => {
    expect(slugifyProviderName('', '', '', 'sub_42')).toBe('sub-42');
    expect(slugifyProviderName('', '', '', 'fallback')).toBe('fallback');
  });

  test('приоритет name над profileTitle', () => {
    expect(slugifyProviderName('Acme 🌏 Подписка', 'Личный VPN', '', 'sub_1')).toBe('lichnyj-VPN');
    expect(slugifyProviderName('Профиль 1', 'Имя 1', '', 'p0')).toBe('imya-1');
  });

  test('извлечение бренда из profileTitle', () => {
    expect(slugifyProviderName('My VPN Subscription', '', '', 'sub_1')).toBe('My-VPN');
    expect(slugifyProviderName('Acme 🌏 Подписка', '', '', 'sub_1')).toBe('Acme');
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
    expect(yaml).toContain('    path: ./proxy_providers/moya-podpiska.yaml');
    expect(yaml).toContain(
      '    url: "http://127.0.0.1:8090/mihomo/provider.yaml?url=https%3A%2F%2Fmihomo.com%2Fsub"'
    );
    expect(yaml).toContain('    interval: 7200'); // 2 * 3600
    expect(yaml).not.toContain('User-Agent:');
    expect(yaml).not.toContain('x-hwid:');

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
    expect(yaml).toContain(
      '- OR,((RULE-SET,telegram@domain),(RULE-SET,telegram@ipcidr,no-resolve)),Telegram'
    );
    expect(yaml).toContain(
      '- OR,((DOMAIN-SUFFIX,gql.twitch.tv),(DOMAIN-SUFFIX,usher.ttvnw.net)),Заблок. сервисы'
    );

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

describe('Phase 43: Mihomo Configurator Deep Analysis fixes', () => {
  test('activeRuleProvider = none does not generate rule-providers section', () => {
    const mockState: any = {
      activeRuleProvider: 'none',
      selectedMetaRuleSets: new Map(),
      proxies: [],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: { enabled: false }
    };
    const yaml = generateYAML(mockState);
    expect(yaml).not.toContain('rule-providers:');
    expect(yaml).not.toContain('quic@inline');
    expect(yaml).not.toContain('netbios@inline');
  });

  test('activeRuleProvider = none with custom rules generates rules without quic/netbios inline rule-sets', () => {
    const mockState: any = {
      activeRuleProvider: 'none',
      selectedMetaRuleSets: new Map(),
      proxies: [],
      groups: [],
      rules: [{ id: '1', type: 'DOMAIN', value: 'google.com', outbound: 'DIRECT' }],
      dns: {},
      tun: {},
      sniffer: { enabled: false }
    };
    const yaml = generateYAML(mockState);
    expect(yaml).toContain('rules:');
    expect(yaml).toContain('- DOMAIN,google.com,DIRECT');
    expect(yaml).not.toContain('quic@inline');
    expect(yaml).not.toContain('netbios@inline');
  });

  test('sniffer enabled does not contain skip-dst-address', () => {
    const mockState: any = {
      activeRuleProvider: 'none',
      selectedMetaRuleSets: new Map(),
      proxies: [],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: {
        enabled: true,
        sniffHttp: true,
        sniffTls: true,
        sniffQuic: true
      }
    };
    const yaml = generateYAML(mockState);
    expect(yaml).toContain('sniffer:');
    expect(yaml).toContain('  enable: true');
    expect(yaml).toContain('  sniff:');
    expect(yaml).toContain('    HTTP: { ports: [80, 8080] }');
    expect(yaml).not.toContain('skip-dst-address');
  });

  test('activeRuleProvider = zkeen generates quic and netbios inline providers', () => {
    const mockState: any = {
      activeRuleProvider: 'zkeen',
      selectedMetaRuleSets: new Map(),
      proxies: [],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: { enabled: false }
    };
    const yaml = generateYAML(mockState);
    expect(yaml).toContain('rule-providers:');
    expect(yaml).toContain('  quic@inline:');
    expect(yaml).toContain('  netbios@inline:');
  });

  test('activeRuleProvider = metacubex with selectedMetaRuleSets generates quic, netbios and meta rule providers', () => {
    const mockState: any = {
      activeRuleProvider: 'metacubex',
      selectedMetaRuleSets: new Map([
        ['telegram|geosite', 'Telegram'],
        ['cn|geoip', 'DIRECT']
      ]),
      proxies: [],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: { enabled: false }
    };
    const yaml = generateYAML(mockState);
    expect(yaml).toContain('rule-providers:');
    expect(yaml).toContain('  quic@inline:');
    expect(yaml).toContain('  netbios@inline:');
    expect(yaml).toContain('  geosite-telegram:');
    expect(yaml).toContain('  geoip-cn:');
  });

  test('parsing proxy-providers from YAML', () => {
    const yaml = `
proxy-providers:
  provider1:
    type: http
    path: ./providers/provider1.yaml
    url: "https://example.com/sub"
    interval: 3600
    header:
      User-Agent:
        - "mihomo/1.18.10"
      x-hwid:
        - "my-hwid"
`;
    const result = populateMihomoFromYAML(yaml);
    expect(result.mihomoProviders).toHaveLength(1);
    const p = result.mihomoProviders![0];
    expect(p.id).toBe('provider1');
    expect(p.url).toBe('https://example.com/sub');
    expect(p.interval).toBe(1); // 3600 seconds = 1 hour
    expect(p.hwid_token).toBe('my-hwid');
  });

  test('generating YAML for trojan, socks and http proxies', () => {
    const mockState: any = {
      proxies: [
        {
          name: 'trojan-proxy',
          type: 'trojan',
          server: 'server.com',
          port: 443,
          password: 'pass',
          sni: 'sni.com',
          skipCertVerify: true,
          network: 'ws',
          wsPath: '/path'
        },
        {
          name: 'socks-proxy',
          type: 'socks',
          server: 'server.com',
          port: 1080,
          username: 'user',
          password: 'pass'
        },
        {
          name: 'socks5-proxy',
          type: 'socks5',
          server: 'server5.com',
          port: 1085,
          username: 'user5',
          password: 'pass5'
        },
        {
          name: 'http-proxy',
          type: 'http',
          server: 'server.com',
          port: 8080,
          username: 'user',
          password: 'pass',
          tls: true,
          skipCertVerify: true
        }
      ],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: { enabled: false }
    };
    const yaml = generateYAML(mockState);
    expect(yaml).toContain('  - name: "trojan-proxy"');
    expect(yaml).toContain('    type: trojan');
    expect(yaml).toContain('    password: "pass"');
    expect(yaml).toContain('    sni: "sni.com"');
    expect(yaml).toContain('    skip-cert-verify: true');
    expect(yaml).toContain('    network: ws');
    expect(yaml).toContain('      path: "/path"');

    expect(yaml).toContain('  - name: "socks-proxy"');
    expect(yaml).toContain('    type: socks');
    expect(yaml).toContain('    username: "user"');
    expect(yaml).toContain('    password: "pass"');

    expect(yaml).toContain('  - name: "socks5-proxy"');
    expect(yaml).toContain('    type: socks5');
    expect(yaml).toContain('    username: "user5"');
    expect(yaml).toContain('    password: "pass5"');

    expect(yaml).toContain('  - name: "http-proxy"');
    expect(yaml).toContain('    type: http');
    expect(yaml).toContain('    username: "user"');
    expect(yaml).toContain('    password: "pass"');
    expect(yaml).toContain('    tls: true');
    expect(yaml).toContain('    skip-cert-verify: true');
  });
});

describe('Virtual proxy-providers in generateYAML', () => {
  test('виртуальный провайдер сохраняет свой id без slugify', () => {
    const mockState: any = {
      existingTproxyPort: 12345,
      existingRedirPort: 12346,
      subscriptions: [],
      mihomoProviders: [
        {
          id: 'legacy-ui-provider',
          name: 'legacy-ui-provider',
          enabled: true,
          type: 'mihomo',
          url: 'https://mihomo.com/sub',
          interval: 2,
          isVirtual: true
        }
      ],
      proxies: [],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: {}
    };

    const yaml = generateYAML(mockState);
    expect(yaml).toContain('proxy-providers:');
    expect(yaml).toContain('  legacy-ui-provider:');
    expect(yaml).not.toContain('  legacy-ui-provider-');
  });
});

describe('WireGuard and AmneziaWG support (TMPL-08)', () => {
  test('узел wireguard с включенной обфускацией генерирует вложенный блок amnezia-wg-option и не содержит плоских полей', () => {
    const mockState: any = {
      existingTproxyPort: 12345,
      existingRedirPort: 12346,
      subscriptions: [],
      mihomoProviders: [],
      proxies: [
        {
          id: 'wg-1',
          name: 'awg-node',
          type: 'wireguard',
          server: '198.51.100.1',
          port: 51820,
          wgPrivateKey: 'aWdQcml2YXRlS2V5MQ==',
          wgPublicKey: 'aWdQdWJsaWNLZXkx==',
          wgIp: '10.2.0.2/32',
          wgPresharedKey: 'cHJlc2hhcmVkS2V5MQ==',
          wgMtu: 1420,
          awgEnabled: true,
          awgJc: 4,
          awgJmin: 40,
          awgJmax: 70,
          awgS1: 15,
          awgS2: 40,
          awgH1: 1000000001,
          awgH2: 1000000002,
          awgH3: 1000000003,
          awgH4: 1000000004
        }
      ],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: {}
    };

    const yaml = generateYAML(mockState);

    // 1. Позитивные проверки: поля узла на отступе 4 пробела
    expect(yaml).toContain('  - name: "awg-node"');
    expect(yaml).toContain('    type: wireguard');
    expect(yaml).toContain('    server: "198.51.100.1"');
    expect(yaml).toContain('    port: 51820');
    expect(yaml).toContain('    private-key: "aWdQcml2YXRlS2V5MQ=="');
    expect(yaml).toContain('    public-key: "aWdQdWJsaWNLZXkx=="');
    expect(yaml).toContain('    ip: "10.2.0.2/32"');
    expect(yaml).toContain('    pre-shared-key: "cHJlc2hhcmVkS2V5MQ=="');
    expect(yaml).toContain('    mtu: 1420');
    expect(yaml).toContain('    udp: true');

    // 2. Вложенный блок amnezia-wg-option на отступе 4 пробела, а параметры на отступе 6 пробелов
    expect(yaml).toContain('    amnezia-wg-option:');
    expect(yaml).toContain('      jc: 4');
    expect(yaml).toContain('      jmin: 40');
    expect(yaml).toContain('      jmax: 70');
    expect(yaml).toContain('      s1: 15');
    expect(yaml).toContain('      s2: 40');
    expect(yaml).toContain('      h1: 1000000001');
    expect(yaml).toContain('      h2: 1000000002');
    expect(yaml).toContain('      h3: 1000000003');
    expect(yaml).toContain('      h4: 1000000004');

    // 3. Негативные проверки: ни один из параметров обфускации НЕ встречается на 4 пробелах (уровне узла)
    const lines = yaml.split('\n');
    for (const key of ['jc:', 'jmin:', 'jmax:', 's1:', 's2:', 'h1:', 'h2:', 'h3:', 'h4:']) {
      const flatMatches = lines.filter((line) => line.startsWith(`    ${key}`));
      expect(flatMatches).toHaveLength(0);
    }
  });

  test('узел wireguard с выключенной обфускацией не генерирует блок amnezia-wg-option', () => {
    const mockState: any = {
      existingTproxyPort: 12345,
      existingRedirPort: 12346,
      subscriptions: [],
      mihomoProviders: [],
      proxies: [
        {
          id: 'wg-clean',
          name: 'clean-wg',
          type: 'wireguard',
          server: '198.51.100.2',
          port: 51820,
          wgPrivateKey: 'privKey==',
          wgPublicKey: 'pubKey==',
          wgIp: '10.2.0.3/32',
          wgMtu: 1420,
          awgEnabled: false
        }
      ],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: {}
    };

    const yaml = generateYAML(mockState);
    expect(yaml).toContain('  - name: "clean-wg"');
    expect(yaml).toContain('    type: wireguard');
    expect(yaml).toContain('    private-key: "privKey=="');
    expect(yaml).toContain('    public-key: "pubKey=="');
    expect(yaml).toContain('    ip: "10.2.0.3/32"');
    expect(yaml).toContain('    udp: true');

    // amnezia-wg-option и pre-shared-key отсутствуют
    expect(yaml).not.toContain('amnezia-wg-option');
    expect(yaml).not.toContain('pre-shared-key');
  });

  test('круговой проход генерация -> разбор -> генерация сохраняет все параметры без потерь', () => {
    const mockState: any = {
      existingTproxyPort: 12345,
      existingRedirPort: 12346,
      subscriptions: [],
      mihomoProviders: [],
      proxies: [
        {
          id: 'wg-roundtrip',
          name: 'roundtrip-node',
          type: 'wireguard',
          server: '198.51.100.5',
          port: 51820,
          wgPrivateKey: 'aWdQcml2YXRlS2V5MQ==',
          wgPublicKey: 'aWdQdWJsaWNLZXkx==',
          wgIp: '10.2.0.2/32',
          wgPresharedKey: 'cHJlc2hhcmVkS2V5MQ==',
          wgMtu: 1420,
          awgEnabled: true,
          awgJc: 4,
          awgJmin: 40,
          awgJmax: 70,
          awgS1: 15,
          awgS2: 40,
          awgH1: 1000000001,
          awgH2: 1000000002,
          awgH3: 1000000003,
          awgH4: 1000000004
        }
      ],
      groups: [],
      rules: [],
      dns: {},
      tun: {},
      sniffer: {}
    };

    const firstYaml = generateYAML(mockState);
    const parsedState = populateMihomoFromYAML(firstYaml);

    expect(parsedState.proxies).toHaveLength(1);
    const p = parsedState.proxies[0];
    expect(p.type).toBe('wireguard');
    expect(p.wgPrivateKey).toBe('aWdQcml2YXRlS2V5MQ==');
    expect(p.wgPublicKey).toBe('aWdQdWJsaWNLZXkx==');
    expect(p.wgIp).toBe('10.2.0.2/32');
    expect(p.wgPresharedKey).toBe('cHJlc2hhcmVkS2V5MQ==');
    expect(p.wgMtu).toBe(1420);
    expect(p.awgEnabled).toBe(true);
    expect(p.awgJc).toBe(4);
    expect(p.awgJmin).toBe(40);
    expect(p.awgJmax).toBe(70);
    expect(p.awgS1).toBe(15);
    expect(p.awgS2).toBe(40);
    expect(p.awgH1).toBe(1000000001);
    expect(p.awgH2).toBe(1000000002);
    expect(p.awgH3).toBe(1000000003);
    expect(p.awgH4).toBe(1000000004);

    const secondYaml = generateYAML({
      ...mockState,
      proxies: parsedState.proxies
    });

    // Строгое побайтовое равенство последовательных генераций
    expect(secondYaml).toBe(firstYaml);
  });

  test('разбор конфигурации с плоскими полями обфускации не включает awgEnabled', () => {
    const flatYaml = `
proxies:
  - name: "flat-awg"
    type: wireguard
    server: 198.51.100.10
    port: 51820
    private-key: "privKey"
    public-key: "pubKey"
    ip: "10.2.0.5/32"
    jc: 4
    jmin: 40
    jmax: 70
    s1: 15
    s2: 40
    h1: 1000000001
    h2: 1000000002
    h3: 1000000003
    h4: 1000000004
`;
    const parsedState = populateMihomoFromYAML(flatYaml);
    expect(parsedState.proxies).toHaveLength(1);
    const p = parsedState.proxies[0];
    expect(p.type).toBe('wireguard');
    expect(p.wgPrivateKey).toBe('privKey');
    // awgEnabled обязан быть ложным, так как параметры не были внутри amnezia-wg-option
    expect(p.awgEnabled).toBeFalsy();
  });
});
