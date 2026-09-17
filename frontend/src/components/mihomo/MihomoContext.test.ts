import { describe, it, expect } from 'vitest';
import { MihomoContext } from './MihomoContext.svelte';

describe('MihomoContext', () => {
  it('initializes with default state', () => {
    const ctx = new MihomoContext();
    expect(ctx.activeSection).toBe('proxies');
    expect(ctx.proxies).toHaveLength(0);
    expect(ctx.groups).toHaveLength(0);
    expect(ctx.rules).toHaveLength(0);
    expect(ctx.listeners).toHaveLength(0);
    expect(ctx.isDirty).toBe(false);
    expect(ctx.dns.enabled).toBe(false);
    expect(ctx.tun.enabled).toBe(false);
  });

  describe('proxies management', () => {
    it('adds, updates and removes proxies', () => {
      const ctx = new MihomoContext();
      ctx.addGroup({ id: 'g1', name: 'Auto', type: 'select', proxies: ['node-1'] });

      ctx.addProxy({ id: 'p1', name: 'node-1', type: 'vless', server: '1.2.3.4', port: 443 });
      expect(ctx.proxies).toHaveLength(1);
      expect(ctx.isDirty).toBe(true);

      ctx.updateProxy('p1', { server: '5.6.7.8' });
      expect(ctx.proxies[0].server).toBe('5.6.7.8');

      // Removing proxy also removes it from group lists
      ctx.removeProxy('p1');
      expect(ctx.proxies).toHaveLength(0);
      expect(ctx.groups[0].proxies).toHaveLength(0);
    });

    it('duplicates and toggles proxies', () => {
      const ctx = new MihomoContext();
      ctx.addProxy({
        id: 'p1',
        name: 'my-proxy',
        type: 'ss',
        server: 'example.com',
        port: 8388,
        enabled: true
      });

      const dup = ctx.duplicateProxy('p1');
      expect(dup).not.toBeNull();
      expect(ctx.proxies).toHaveLength(2);
      expect(ctx.proxies[1].name).toBe('my-proxy (copy)');
      expect(ctx.proxies[1].id).not.toBe('p1');

      ctx.toggleProxy('p1');
      expect(ctx.proxies[0].enabled).toBe(false);
      ctx.toggleProxy('p1');
      expect(ctx.proxies[0].enabled).toBe(true);
    });
  });

  describe('groups management', () => {
    it('adds, updates and removes groups', () => {
      const ctx = new MihomoContext();
      ctx.addGroup({ id: 'g1', name: 'ProxyGroup', type: 'select', proxies: [] });
      expect(ctx.groups).toHaveLength(1);
      expect(ctx.isDirty).toBe(true);

      ctx.updateGroup('g1', { type: 'url-test', interval: 600 });
      expect(ctx.groups[0].type).toBe('url-test');
      expect(ctx.groups[0].interval).toBe(600);

      ctx.removeGroup('g1');
      expect(ctx.groups).toHaveLength(0);
    });
  });

  describe('rules management', () => {
    it('adds, updates, removes and reorders rules', () => {
      const ctx = new MihomoContext();
      ctx.addRule({ id: 'r1', type: 'DOMAIN-SUFFIX', value: 'google.com', outbound: 'DIRECT' });
      ctx.addRule({ id: 'r2', type: 'GEOIP', value: 'telegram', outbound: 'Proxy' });
      ctx.addRule({ id: 'r3', type: 'MATCH', value: '', outbound: 'DIRECT' });

      expect(ctx.rules).toHaveLength(3);

      ctx.updateRule('r1', { outbound: 'Proxy' });
      expect(ctx.rules[0].outbound).toBe('Proxy');

      // Move rule 0 to index 2
      ctx.moveRule(0, 2);
      expect(ctx.rules.map((r) => r.id)).toEqual(['r2', 'r3', 'r1']);

      ctx.removeRule('r2');
      expect(ctx.rules.map((r) => r.id)).toEqual(['r3', 'r1']);
    });
  });

  describe('listeners and providers', () => {
    it('manages listeners', () => {
      const ctx = new MihomoContext();
      ctx.addListener({
        id: 'l1',
        name: 'mixed-in',
        type: 'mixed',
        port: '7890',
        listen: '0.0.0.0'
      });
      expect(ctx.listeners).toHaveLength(1);

      ctx.updateListener('l1', { port: '7891' });
      expect(ctx.listeners[0].port).toBe('7891');

      ctx.removeListener('l1');
      expect(ctx.listeners).toHaveLength(0);
    });

    it('manages rule providers and presets', () => {
      const ctx = new MihomoContext();
      ctx.applyPreset('zkeen');
      expect(ctx.activePreset).toBe('zkeen');
      expect(ctx.ruleProviders.length).toBeGreaterThan(0);
      expect(ctx.groups.length).toBeGreaterThan(0);

      const initialCount = ctx.ruleProviders.length;
      ctx.removeRuleProvider(ctx.ruleProviders[0].name);
      expect(ctx.ruleProviders.length).toBeLessThan(initialCount);

      ctx.applyPreset('none');
      expect(ctx.activePreset).toBe('');
      expect(ctx.ruleProviders).toHaveLength(0);
    });
  });

  describe('yaml generation', () => {
    it('generates valid yaml string', () => {
      const ctx = new MihomoContext();
      ctx.addProxy({
        id: 'p1',
        name: 'Proxy1',
        type: 'ss',
        server: '1.2.3.4',
        port: 8388,
        cipher: 'aes-128-gcm',
        password: 'pass'
      });
      ctx.addGroup({ id: 'g1', name: 'Default', type: 'select', proxies: ['Proxy1'] });
      ctx.addRule({ id: 'r1', type: 'MATCH', value: '', outbound: 'Default' });

      const yaml = ctx.generateYaml();
      expect(typeof yaml).toBe('string');
      expect(yaml).toContain('Proxy1');
      expect(yaml).toContain('Default');
      expect(yaml).toContain('MATCH');
    });
  });
});
