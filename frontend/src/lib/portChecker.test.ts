import { describe, it, expect } from 'vitest';
import {
  parseMihomoPorts,
  parseMihomoListenerPorts,
  findPortCollisions,
  type PortAllocation
} from './portChecker';
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

  it('parseMihomoListenerPorts returns allocations for each listener in listeners section', () => {
    const yaml = `
mixed-port: 7890
listeners:
  - name: mixed-in
    type: mixed
    port: 1080
    listen: 0.0.0.0
    udp: true
  - name: "socks-in"
    type: socks
    port: 1081
  - name: ss-in
    type: shadowsocks
    port: 8388
rules:
  - MATCH,DIRECT
`;
    const allocations = parseMihomoListenerPorts(yaml);
    expect(allocations).toHaveLength(3);
    expect(allocations).toEqual([
      { port: 1080, engine: 'mihomo', purpose: 'listener:mixed-in' },
      { port: 1081, engine: 'mihomo', purpose: 'listener:socks-in' },
      { port: 8388, engine: 'mihomo', purpose: 'listener:ss-in' }
    ]);
  });

  it('parseMihomoListenerPorts returns empty array when no listeners section', () => {
    const yaml = `
mixed-port: 7890
proxies: []
rules: []
`;
    expect(parseMihomoListenerPorts(yaml)).toEqual([]);
  });

  it('parseMihomoListenerPorts does not confuse top-level port with listener port', () => {
    const yaml = `
port: 7890
socks-port: 7891
listeners:
  - name: redirect-in
    type: redirect
    port: 12345
`;
    const allocations = parseMihomoListenerPorts(yaml);
    expect(allocations).toHaveLength(1);
    expect(allocations[0]).toEqual({
      port: 12345,
      engine: 'mihomo',
      purpose: 'listener:redirect-in'
    });
  });

  it('parseMihomoListenerPorts extracts ports specified with single and double quotes (WR-01)', () => {
    const yaml = `
listeners:
  - name: double-quote
    type: mixed
    port: "7890"
  - name: 'single-quote'
    type: socks
    port: '1080'
`;
    const allocations = parseMihomoListenerPorts(yaml);
    expect(allocations).toHaveLength(2);
    expect(allocations).toEqual([
      { port: 7890, engine: 'mihomo', purpose: 'listener:double-quote' },
      { port: 1080, engine: 'mihomo', purpose: 'listener:single-quote' }
    ]);
  });

  it('parseMihomoListenerPorts handles users block before port without false flush (WR-02)', () => {
    const yaml = `
listeners:
  - name: listener-with-users
    type: mixed
    users:
      - username: alice
        password: 123
      - username: bob
        password: 456
    port: 7892
`;
    const allocations = parseMihomoListenerPorts(yaml);
    expect(allocations).toHaveLength(1);
    expect(allocations[0]).toEqual({
      port: 7892,
      engine: 'mihomo',
      purpose: 'listener:listener-with-users'
    });
  });

  it('detects collision between listener and top-level port or another listener', () => {
    const yaml = `
mixed-port: 7890
listeners:
  - name: listener-dup-1
    type: mixed
    port: 7890
  - name: listener-dup-2
    type: socks
    port: 7890
`;
    const topLevel = parseMihomoPorts(yaml);
    const listeners = parseMihomoListenerPorts(yaml);
    const collisions = findPortCollisions([...topLevel, ...listeners]);

    expect(collisions).toHaveLength(1);
    expect(collisions[0]).toHaveLength(3);
    expect(collisions[0].map((p) => p.purpose)).toEqual([
      'mixed-port',
      'listener:listener-dup-1',
      'listener:listener-dup-2'
    ]);
  });

  it('detects collision between listener and reserved ports 5000, 5001, 1053', () => {
    const reserved: PortAllocation[] = [
      { port: 5000, engine: 'mihomo', purpose: 'redir-port' },
      { port: 5001, engine: 'mihomo', purpose: 'tproxy-port' },
      { port: 1053, engine: 'mihomo', purpose: 'dns' }
    ];
    const listenerPorts: PortAllocation[] = [
      { port: 5000, engine: 'mihomo', purpose: 'listener:redir-conflict' },
      { port: 1, engine: 'mihomo', purpose: 'listener:edge-1' },
      { port: 65535, engine: 'mihomo', purpose: 'listener:edge-65535' }
    ];
    const collisions = findPortCollisions([...reserved, ...listenerPorts]);

    expect(collisions).toHaveLength(1);
    expect(collisions[0][0].port).toBe(5000);
    // Port 1 and 65535 should not collide with anything
    const collidedPorts = collisions.map((c) => c[0].port);
    expect(collidedPorts).not.toContain(1);
    expect(collidedPorts).not.toContain(65535);
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
