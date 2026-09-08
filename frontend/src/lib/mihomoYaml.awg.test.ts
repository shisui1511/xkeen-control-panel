import { describe, it, expect } from 'vitest';
import {
  generateYAML,
  populateMihomoFromYAML,
  type MihomoConfigState,
  type Proxy
} from './mihomoYaml';

const baseState: MihomoConfigState = {
  proxies: [],
  groups: [],
  rules: [],
  dns: {
    enabled: false,
    nameservers: [],
    fallback: [],
    enhancedMode: 'fake-ip',
    fakeIPRange: '198.18.0.1/16'
  },
  tun: {
    enabled: false,
    stack: 'mixed',
    autoRoute: true,
    autoDetectInterface: true,
    dnsHijack: []
  },
  sniffer: {
    enabled: false,
    sniffHttp: false,
    sniffTls: false,
    sniffQuic: false
  },
  activeRuleProvider: 'none',
  selectedMetaRuleSets: new Map(),
  preservedKeys: [],
  existingTproxyPort: 5001,
  existingRedirPort: 5000,
  subscriptions: []
};

// Ядро с поддержкой AWG 3.1 — чтобы 3.1-ключи вообще эмитились (см. WR-02).
const awg31Caps = { active_kernel: 'mihomo', kernels: { mihomo: { version: '1.19.30' } } };

function wgProxy(extra: Partial<Proxy>): Proxy {
  return {
    id: 'p1',
    name: 'wg1',
    type: 'wireguard',
    server: '1.2.3.4',
    port: 51820,
    wgPrivateKey: 'priv',
    wgPublicKey: 'pub',
    wgIp: '10.0.0.2',
    awgEnabled: true,
    awgJc: 4,
    awgJmin: 40,
    awgJmax: 70,
    awgS1: 15,
    awgS2: 40,
    ...extra
  } as Proxy;
}

describe('AmneziaWG I1..I5 case-sensitivity (CR-01)', () => {
  it('preserves CPS-tag templates and hex case verbatim on emit', () => {
    const state: MihomoConfigState = {
      ...baseState,
      proxies: [wgProxy({ awgI1: '0A<c>1B', awgI2: '<b 0xf1a0>' })],
      capabilities: awg31Caps
    };
    const yaml = generateYAML(state);
    expect(yaml).toContain('i1: "0A<c>1B"');
    expect(yaml).toContain('i2: "<b 0xf1a0>"');
    expect(yaml).not.toContain('0A<C>1B');
    expect(yaml).not.toContain('0XF1A0');
  });

  it('round-trips I1/I2 unchanged through generate -> parse', () => {
    const state: MihomoConfigState = {
      ...baseState,
      proxies: [wgProxy({ awgI1: '0A<c>1B', awgI2: '<b 0xf1a0>' })],
      capabilities: awg31Caps
    };
    const yaml = generateYAML(state);
    const parsed = populateMihomoFromYAML(yaml);
    const wg = parsed.proxies.find((p) => p.name === 'wg1');
    expect(wg?.awgI1).toBe('0A<c>1B');
    expect(wg?.awgI2).toBe('<b 0xf1a0>');
  });
});
