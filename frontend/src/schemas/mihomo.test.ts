import { describe, it, expect } from 'vitest';
import { mihomoSchema } from './mihomo';
import { BUILDER_LISTENER_TYPES } from '../lib/mihomoYaml';

// Verified against MetaCubeX/mihomo listener/parse.go on release tag v1.19.30 (2026-09-06)
export const CORE_LISTENER_TYPES = [
  'socks',
  'http',
  'tproxy',
  'redir',
  'mixed',
  'tunnel',
  'tun',
  'shadowsocks',
  'snell',
  'vmess',
  'vless',
  'trojan',
  'hysteria2',
  'hysteria2-realm',
  'tuic',
  'shadowquic',
  'anytls',
  'mieru',
  'sudoku',
  'trusttunnel'
] as const;

describe('mihomoSchema listeners', () => {
  it('defines listeners as an array in properties', () => {
    const listeners = (mihomoSchema as any).properties.listeners;
    expect(listeners).toBeDefined();
    expect(listeners.type).toBe('array');
    expect(listeners.description).toBeDefined();
  });

  it('defines listeners items as object with required name and type', () => {
    const items = (mihomoSchema as any).properties.listeners.items;
    expect(items).toBeDefined();
    expect(items.type).toBe('object');
    expect(items.required).toEqual(['name', 'type']);
  });

  it('contains all expected listener properties', () => {
    const props = (mihomoSchema as any).properties.listeners.items.properties;
    expect(props).toBeDefined();
    expect(props.name.type).toBe('string');
    expect(props.type.type).toBe('string');
    expect(props.listen.type).toBe('string');
    expect(props.udp.type).toBe('boolean');
    expect(props.cipher.type).toBe('string');
    expect(props.password.type).toBe('string');
    expect(props['routing-mark'].type).toBe('integer');
  });

  it('allows port to be either integer or string', () => {
    const portProp = (mihomoSchema as any).properties.listeners.items.properties.port;
    expect(portProp).toBeDefined();
    const types = portProp.oneOf ? portProp.oneOf.map((s: any) => s.type) : [portProp.type];
    expect(types).toContain('integer');
    expect(types).toContain('string');
  });

  it('defines users as an array of objects with username and password', () => {
    const usersProp = (mihomoSchema as any).properties.listeners.items.properties.users;
    expect(usersProp.type).toBe('array');
    expect(usersProp.items.type).toBe('object');
    expect(usersProp.items.properties.username.type).toBe('string');
    expect(usersProp.items.properties.password.type).toBe('string');
  });

  it('defines proxy and rule with distinct non-empty descriptions', () => {
    const props = (mihomoSchema as any).properties.listeners.items.properties;
    expect(props.proxy.type).toBe('string');
    expect(props.proxy.description).toBeTruthy();
    expect(props.rule.type).toBe('string');
    expect(props.rule.description).toBeTruthy();
    expect(props.proxy.description).not.toEqual(props.rule.description);
  });

  it('does not define top-level required array', () => {
    expect((mihomoSchema as any).required).toBeUndefined();
  });
});

describe('mihomoSchema listeners type enum', () => {
  const typeProp = (mihomoSchema as any).properties.listeners.items.properties.type;
  const enumList: string[] = typeProp?.enum;

  it('strictly equals CORE_LISTENER_TYPES in order and content', () => {
    expect(enumList).toEqual(CORE_LISTENER_TYPES);
  });

  it('contains only unique types without duplicates', () => {
    expect(enumList).toBeDefined();
    const uniqueSet = new Set(enumList);
    expect(uniqueSet.size).toBe(enumList.length);
  });

  it('does not contain invalid redirect alias', () => {
    expect(enumList).not.toContain('redirect');
    expect(enumList).toContain('redir');
  });

  it('does not contain sub-options as top-level types', () => {
    const subOptions = ['restls', 'jls', 'kcptun', 'shadowtls', 'mkcp', 'mekya', 'tlsmirror'];
    for (const opt of subOptions) {
      expect(enumList).not.toContain(opt);
    }
  });

  it('contains every builder listener type', () => {
    for (const builderType of BUILDER_LISTENER_TYPES) {
      const expectedSchemaType = builderType === 'redirect' ? 'redir' : builderType;
      expect(enumList).toContain(expectedSchemaType);
    }
  });

  it('defines rule property strictly as string (not boolean)', () => {
    const ruleProp = (mihomoSchema as any).properties.listeners.items.properties.rule;
    expect(ruleProp.type).toBe('string');
    expect(ruleProp.type).not.toBe('boolean');
  });
});
