import { describe, it, expect } from 'vitest';
import { XrayContext } from './XrayContext.svelte';

describe('XrayContext', () => {
  it('initializes with default empty state', () => {
    const ctx = new XrayContext();
    expect(ctx.activeSection).toBe('routing');
    expect(ctx.routingRules).toHaveLength(0);
    expect(ctx.inbounds).toHaveLength(0);
    expect(ctx.customOutbounds).toHaveLength(0);
    expect(ctx.isDirty).toBe(false);
    expect(ctx.outboundTags).toEqual(['direct', 'block', 'dns-out']);
  });

  describe('rules management', () => {
    it('adds and updates rules', () => {
      const ctx = new XrayContext();
      ctx.addRule({ id: 'r1', outboundTag: 'proxy', domain: ['google.com'] });

      expect(ctx.routingRules).toHaveLength(1);
      expect(ctx.routingRules[0].outboundTag).toBe('proxy');
      expect(ctx.isDirty).toBe(true);

      ctx.updateRule('r1', { outboundTag: 'direct' });
      expect(ctx.routingRules[0].outboundTag).toBe('direct');
    });

    it('duplicates, toggles and removes rules', () => {
      const ctx = new XrayContext();
      ctx.addRule({ id: 'r1', outboundTag: 'proxy', enabled: true });

      ctx.duplicateRule('r1');
      expect(ctx.routingRules).toHaveLength(2);
      expect(ctx.routingRules[1].outboundTag).toBe('proxy');
      expect(ctx.routingRules[1].id).not.toBe('r1');

      ctx.toggleRule('r1');
      expect(ctx.routingRules[0].enabled).toBe(false);

      ctx.removeRule('r1');
      expect(ctx.routingRules).toHaveLength(1);
    });

    it('reorders rules correctly', () => {
      const ctx = new XrayContext();
      ctx.addRule({ id: 'r1', outboundTag: 'tag1' });
      ctx.addRule({ id: 'r2', outboundTag: 'tag2' });
      ctx.addRule({ id: 'r3', outboundTag: 'tag3' });

      ctx.reorderRules(0, 2);
      expect(ctx.routingRules.map((r) => r.id)).toEqual(['r2', 'r3', 'r1']);
    });
  });

  describe('outbounds management', () => {
    it('adds custom outbounds and updates computed tags and details', () => {
      const ctx = new XrayContext();
      ctx.addCustomOutbound({
        tag: 'my-vless',
        protocol: 'vless',
        settings: { vnext: [{ address: 'vless.example.com' }] }
      });

      expect(ctx.customOutbounds).toHaveLength(1);
      expect(ctx.outboundTags).toContain('my-vless');
      expect(
        ctx.outboundDetails.some((d) => d.tag === 'my-vless' && d.server === 'vless.example.com')
      ).toBe(true);

      ctx.updateCustomOutbound(0, {
        tag: 'updated-tag',
        protocol: 'vless'
      });
      expect(ctx.outboundTags).toContain('updated-tag');
      expect(ctx.outboundTags).not.toContain('my-vless');

      ctx.removeCustomOutbound(0);
      expect(ctx.customOutbounds).toHaveLength(0);
      expect(ctx.outboundTags).not.toContain('updated-tag');
    });
  });

  describe('inbounds and dns management', () => {
    it('manages inbounds', () => {
      const ctx = new XrayContext();
      ctx.addInbound({ tag: 'in-mixed', port: 10808, protocol: 'mixed' });
      expect(ctx.inbounds).toHaveLength(1);

      ctx.updateInbound('in-mixed', { port: 10809 });
      expect(ctx.inbounds[0].port).toBe(10809);

      ctx.removeInbound('in-mixed');
      expect(ctx.inbounds).toHaveLength(0);
    });

    it('manages dns servers', () => {
      const ctx = new XrayContext();
      ctx.addDnsServer('8.8.8.8');
      expect(ctx.dnsConfig.servers).toContain('8.8.8.8');

      ctx.updateDnsServer(0, '1.1.1.1');
      expect(ctx.dnsConfig.servers[0]).toBe('1.1.1.1');

      ctx.removeDnsServer(0);
      expect(ctx.dnsConfig.servers).toHaveLength(0);
    });
  });
});
