import { describe, it, expect } from 'vitest';
import { mihomoSchema } from './mihomo';

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
