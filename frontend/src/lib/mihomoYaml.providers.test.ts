import { describe, it, expect } from 'vitest';
import { generateYAML, populateMihomoFromYAML } from './mihomoYaml';

const source = [
  'proxy-providers:',
  '  sub:',
  '    type: http',
  '    url: "https://sub.example/list"',
  '    interval: 43200',
  '    path: ./proxy_providers/sub.yaml',
  '    health-check:',
  '      enable: true',
  '      url: http://www.gstatic.com/generate_204',
  '      interval: 300',
  'rules:',
  '  - MATCH,DIRECT',
  ''
].join('\n');

describe('Mihomo proxy-provider round trip', () => {
  it('keeps nested health-check url and interval', () => {
    const parsed = populateMihomoFromYAML(source);
    const provider = parsed.mihomoProviders[0];
    expect(provider.url).toBe('https://sub.example/list');

    const yaml = generateYAML({
      proxies: [],
      groups: [],
      rules: [],
      dns: {
        enabled: false,
        nameservers: [],
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
      subscriptions: [],
      mihomoProviders: [{ ...provider, url: 'https://sub.example/new' }]
    });

    expect(yaml).toMatch(/^ {4}url: "?https:\/\/sub\.example\/new"?$/m);
    expect(yaml).toMatch(/^ {4}interval: 43200$/m);
    expect(yaml).toMatch(/^ {6}url: http:\/\/www\.gstatic\.com\/generate_204$/m);
    expect(yaml).toMatch(/^ {6}interval: 300$/m);
  });
});
