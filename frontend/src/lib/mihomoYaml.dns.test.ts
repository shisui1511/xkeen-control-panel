import { describe, it, expect } from 'vitest';
import {
  generateYAML,
  populateMihomoFromYAML,
  applyDnsProxyGroup,
  detectDnsProxyGroup,
  type MihomoConfigState
} from './mihomoYaml';

const baseState: MihomoConfigState = {
  proxies: [],
  groups: [],
  rules: [],
  dns: {
    enabled: true,
    nameservers: ['https://dns.google/dns-query', '1.1.1.1', 'tls://1.1.1.1#Old'],
    fallback: [],
    enhancedMode: 'redir-host',
    fakeIPRange: '198.18.0.1/16'
  },
  tun: {
    enabled: false,
    stack: 'mixed',
    autoRoute: true,
    autoDetectInterface: true,
    dnsHijack: []
  },
  sniffer: { enabled: false, sniffHttp: false, sniffTls: false, sniffQuic: false },
  activeRuleProvider: 'none',
  selectedMetaRuleSets: new Map(),
  preservedKeys: [],
  existingTproxyPort: 5001,
  existingRedirPort: 5000,
  subscriptions: []
};

describe('Mihomo DNS through a proxy group', () => {
  it('suffixes only encrypted resolvers and can strip the suffix', () => {
    expect(applyDnsProxyGroup(['https://a/dns-query', '1.1.1.1', 'tls://b#X'], 'VPN')).toEqual([
      'https://a/dns-query#VPN',
      '1.1.1.1',
      'tls://b#VPN'
    ]);
    expect(applyDnsProxyGroup(['https://a/dns-query#VPN'], '')).toEqual(['https://a/dns-query']);
    expect(detectDnsProxyGroup(['https://a#VPN', 'tls://b#VPN', '1.1.1.1'])).toBe('VPN');
    expect(detectDnsProxyGroup(['https://a#A', 'tls://b#B'])).toBe('');
  });

  it('generates proxy-server-nameserver and suffixed resolvers', () => {
    const yaml = generateYAML({
      ...baseState,
      dns: { ...baseState.dns, proxyGroup: 'Заблок. сервисы' }
    });
    expect(yaml).toMatch(/proxy-server-nameserver:\n\s+- "?77\.88\.8\.8"?\n\s+- "?1\.1\.1\.1"?/);
    expect(yaml).toContain('https://dns.google/dns-query#Заблок. сервисы');
    expect(yaml).toContain('tls://1.1.1.1#Заблок. сервисы');
    expect(yaml).not.toContain('#Old');
  });

  it('parses the group back and keeps other DNS lists separate', () => {
    const parsed = populateMihomoFromYAML(
      [
        'dns:',
        '  enable: true',
        '  default-nameserver:',
        '    - 9.9.9.9',
        '  proxy-server-nameserver:',
        '    - 77.88.8.8',
        '  nameserver:',
        '    - "https://dns.google/dns-query#VPN"',
        '    - "https://cloudflare-dns.com/dns-query#VPN"',
        'rules:',
        '  - MATCH,DIRECT',
        ''
      ].join('\n')
    );
    expect(parsed.dns.proxyGroup).toBe('VPN');
    expect(parsed.dns.nameservers).toEqual([
      'https://dns.google/dns-query',
      'https://cloudflare-dns.com/dns-query'
    ]);
    expect(parsed.dns.proxyServerNameservers).toEqual(['77.88.8.8']);
  });
});
