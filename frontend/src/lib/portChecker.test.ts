import { describe, it, expect } from 'vitest';
import { parseMihomoPorts, findPortCollisions, type PortAllocation } from './portChecker';
import { generateYAML, type MihomoConfigState } from './mihomoYaml';

describe('portChecker', () => {
  it('should ignore external-controller-unix without creating port allocations', () => {
    const yaml = `
port: 7890
socks-port: 7891
external-controller-unix: /opt/var/run/mihomo.sock
tproxy-port: 5001
redir-port: 5000
`;
    const allocations = parseMihomoPorts(yaml);
    const ports = allocations.map((a) => a.port);
    const purposes = allocations.map((a) => a.purpose);

    expect(ports).toEqual([7890, 7891, 5001, 5000]);
    expect(purposes).not.toContain('external-controller');
    expect(purposes).not.toContain('external-controller-unix');
  });

  it('should extract port allocation for external-controller TCP', () => {
    const yaml = `
port: 7890
external-controller: 0.0.0.0:9090
tproxy-port: 5001
`;
    const allocations = parseMihomoPorts(yaml);
    const ctrlAlloc = allocations.find((a) => a.purpose === 'external-controller');

    expect(ctrlAlloc).toBeDefined();
    expect(ctrlAlloc?.port).toBe(9090);
    expect(ctrlAlloc?.engine).toBe('mihomo');
  });

  it('detects collisions between ports correctly', () => {
    const allocations: PortAllocation[] = [
      { port: 9090, engine: 'mihomo', purpose: 'external-controller' },
      { port: 9090, engine: 'xray', purpose: 'vless' },
      { port: 5000, engine: 'mihomo', purpose: 'redir-port' }
    ];
    const collisions = findPortCollisions(allocations);
    expect(collisions).toHaveLength(1);
    expect(collisions[0]).toHaveLength(2);
    expect(collisions[0][0].port).toBe(9090);
  });
});

describe('generateYAML controller settings', () => {
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

  it('generates external-controller-unix by default', () => {
    const output = generateYAML(baseState);
    expect(output).toContain('external-controller-unix: /opt/var/run/mihomo.sock');
    expect(output).not.toContain('external-controller:');
  });

  it('generates external-controller TCP when explicitly selected', () => {
    const state: MihomoConfigState = {
      ...baseState,
      externalControllerType: 'tcp',
      externalControllerTarget: '127.0.0.1:9095'
    };
    const output = generateYAML(state);
    expect(output).toContain('external-controller: 127.0.0.1:9095');
    expect(output).not.toContain('external-controller-unix:');
  });
});
