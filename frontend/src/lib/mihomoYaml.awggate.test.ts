import { describe, it, expect } from 'vitest';
import { generateYAML, type MihomoConfigState, type Proxy } from './mihomoYaml';

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
  sniffer: { enabled: false, sniffHttp: false, sniffTls: false, sniffQuic: false },
  activeRuleProvider: 'none',
  selectedMetaRuleSets: new Map(),
  preservedKeys: [],
  existingTproxyPort: 5001,
  existingRedirPort: 5000,
  subscriptions: []
};

function wg31Proxy(): Proxy {
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
    awgVersion: '3.1',
    awgHeaderProtectionKey: 'hpk',
    awgI1: '0A1B',
    awgContentPaddingAddition: 16,
    awgRandomTrailers: true,
    awgDisableCookies: true,
    awgRekeyAfterTime: 120
  } as Proxy;
}

function stateWith(caps: unknown): MihomoConfigState {
  return { ...baseState, proxies: [wg31Proxy()], capabilities: caps };
}

const KEYS_31 = [
  'version:',
  'header-protection-key:',
  'i1:',
  'content-padding-addition:',
  'random-trailers:',
  'disable-cookies:',
  'rekey-after-time:'
];

describe('generateYAML — AWG 3.1 emit gate on kernel capability (WR-02)', () => {
  it('skips all 3.1-only keys when mihomo is too old', () => {
    const yaml = generateYAML(
      stateWith({ active_kernel: 'mihomo', kernels: { mihomo: { version: '1.18.0' } } })
    );
    for (const k of KEYS_31) expect(yaml).not.toContain(k);
    // classic + 2.0 keys still present
    expect(yaml).toContain('jc: 4');
    expect(yaml).toContain('s1: 15');
    expect(yaml).toContain('jmax: 70');
  });

  it('skips 3.1-only keys when Xray is the active kernel', () => {
    const yaml = generateYAML(
      stateWith({ active_kernel: 'xray', kernels: { mihomo: { version: '1.19.30' } } })
    );
    for (const k of KEYS_31) expect(yaml).not.toContain(k);
  });

  it('skips 3.1-only keys when capabilities are unknown', () => {
    const yaml = generateYAML(stateWith(undefined));
    for (const k of KEYS_31) expect(yaml).not.toContain(k);
    expect(yaml).toContain('jc: 4');
  });

  it('emits 3.1-only keys when mihomo supports AWG 3.1', () => {
    const yaml = generateYAML(
      stateWith({ active_kernel: 'mihomo', kernels: { mihomo: { version: '1.19.30' } } })
    );
    for (const k of KEYS_31) expect(yaml).toContain(k);
  });
});
