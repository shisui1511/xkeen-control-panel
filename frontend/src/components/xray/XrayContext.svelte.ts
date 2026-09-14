import { getContext, setContext } from 'svelte';

export interface XrayRoutingRule {
  id: string;
  type: 'field';
  outboundTag: string;
  domain?: string[];
  ip?: string[];
  port?: string;
  network?: string;
  protocol?: string[];
  inboundTag?: string[];
  enabled?: boolean;
}

export interface DNSServer {
  address: string;
  port?: number;
  tag?: string;
  domains?: string[];
  skipFallback?: boolean;
  inboundPort?: number;
}

export interface XrayInbound {
  tag: string;
  port: number;
  listen?: string;
  protocol: string;
  settings?: Record<string, any>;
  sniffing?: Record<string, any>;
  streamSettings?: Record<string, any>;
}

export interface OutboundDetail {
  tag: string;
  protocol: string;
  server?: string;
}

export type XraySectionName = 'log' | 'dns' | 'inbounds' | 'outbounds' | 'routing' | 'policy';

/**
 * Реактивный класс состояния конструктора Xray (Svelte 5 runes).
 */
export class XrayContext {
  activeSection = $state<XraySectionName>('routing');
  logConfig = $state({ loglevel: 'warning', dnsLog: false });
  dnsConfig = $state<{
    tag: string;
    servers: (string | DNSServer)[];
    queryStrategy: string;
    hosts: Record<string, string>;
  }>({
    tag: 'dns-in',
    servers: [],
    queryStrategy: 'UseIP',
    hosts: {}
  });
  dnsOverVless = $state(false);
  routingConfig = $state<{ domainStrategy: string }>({
    domainStrategy: 'IPIfNonMatch'
  });
  routingRules = $state<XrayRoutingRule[]>([]);
  balancers = $state<any[]>([]);
  inbounds = $state<XrayInbound[]>([]);
  customOutbounds = $state<any[]>([]);
  subscriptionOutbounds = $state<any[]>([]);
  proxyTag = $state<string>('');
  policyConfig = $state<{ levels: Record<string, any>; system: Record<string, any> }>({
    levels: { '0': { handshake: 4, connIdle: 300, uplinkOnly: 2, downlinkOnly: 5 } },
    system: {}
  });
  isDirty = $state<boolean>(false);

  // Вычисляемый список тегов outbound
  get outboundTags(): string[] {
    return [
      'direct',
      'block',
      'dns-out',
      ...this.customOutbounds.map((o) => o.tag).filter(Boolean),
      ...this.subscriptionOutbounds.map((o) => o.tag).filter(Boolean)
    ];
  }

  // Вычисляемые детальные сведения об outbound узлах
  get outboundDetails(): OutboundDetail[] {
    const list: OutboundDetail[] = [
      { tag: 'direct', protocol: 'freedom' },
      { tag: 'block', protocol: 'blackhole' },
      { tag: 'dns-out', protocol: 'dns' }
    ];
    const seen = new Set<string>(['direct', 'block', 'dns-out']);

    for (const o of this.customOutbounds) {
      if (o.tag && !seen.has(o.tag)) {
        seen.add(o.tag);
        let server = '';
        if (o.settings?.vnext?.[0]?.address) {
          server = o.settings.vnext[0].address;
        } else if (o.settings?.servers?.[0]?.address) {
          server = o.settings.servers[0].address;
        } else if (o.settings?.peers?.[0]?.endpoint) {
          server = o.settings.peers[0].endpoint;
        }
        list.push({
          tag: o.tag,
          protocol: o.protocol || 'unknown',
          server: server || undefined
        });
      }
    }

    for (const o of this.subscriptionOutbounds) {
      if (o.tag && !seen.has(o.tag)) {
        seen.add(o.tag);
        let server = '';
        if (o.settings?.vnext?.[0]?.address) {
          server = o.settings.vnext[0].address;
        } else if (o.settings?.servers?.[0]?.address) {
          server = o.settings.servers[0].address;
        } else if (o.settings?.peers?.[0]?.endpoint) {
          server = o.settings.peers[0].endpoint;
        }
        list.push({
          tag: o.tag,
          protocol: o.protocol || 'unknown',
          server: server || undefined
        });
      }
    }
    return list;
  }

  markDirty(): void {
    this.isDirty = true;
  }

  resetDirty(): void {
    this.isDirty = false;
  }

  addRule(rule: Partial<XrayRoutingRule>): void {
    const newRule: XrayRoutingRule = {
      id: rule.id || (typeof crypto !== 'undefined' ? crypto.randomUUID() : 'r-' + Date.now()),
      type: 'field',
      outboundTag: rule.outboundTag || 'direct',
      domain: rule.domain || [],
      ip: rule.ip || [],
      port: rule.port || '',
      network: rule.network || '',
      protocol: rule.protocol || [],
      inboundTag: rule.inboundTag || [],
      enabled: rule.enabled ?? true
    };
    this.routingRules.push(newRule);
    this.markDirty();
  }

  updateRule(id: string, updates: Partial<XrayRoutingRule>): void {
    const idx = this.routingRules.findIndex((r) => r.id === id);
    if (idx !== -1) {
      this.routingRules[idx] = { ...this.routingRules[idx], ...updates };
      this.markDirty();
    }
  }

  removeRule(id: string): void {
    this.routingRules = this.routingRules.filter((r) => r.id !== id);
    this.markDirty();
  }

  duplicateRule(id: string): void {
    const r = this.routingRules.find((x) => x.id === id);
    if (r) {
      const copy: XrayRoutingRule = {
        ...JSON.parse(JSON.stringify(r)),
        id: typeof crypto !== 'undefined' ? crypto.randomUUID() : 'r-' + Date.now()
      };
      const idx = this.routingRules.findIndex((x) => x.id === id);
      this.routingRules.splice(idx + 1, 0, copy);
      this.markDirty();
    }
  }

  toggleRule(id: string): void {
    const r = this.routingRules.find((x) => x.id === id);
    if (r) {
      r.enabled = !(r.enabled ?? true);
      this.markDirty();
    }
  }

  reorderRules(fromIndex: number, toIndex: number): void {
    if (
      fromIndex < 0 ||
      fromIndex >= this.routingRules.length ||
      toIndex < 0 ||
      toIndex >= this.routingRules.length ||
      fromIndex === toIndex
    ) {
      return;
    }
    const [moved] = this.routingRules.splice(fromIndex, 1);
    this.routingRules.splice(toIndex, 0, moved);
    this.markDirty();
  }

  addInbound(inbound: XrayInbound): void {
    this.inbounds.push(inbound);
    this.markDirty();
  }

  updateInbound(tag: string, updates: Partial<XrayInbound>): void {
    const idx = this.inbounds.findIndex((i) => i.tag === tag);
    if (idx !== -1) {
      this.inbounds[idx] = { ...this.inbounds[idx], ...updates };
      this.markDirty();
    }
  }

  removeInbound(tag: string): void {
    this.inbounds = this.inbounds.filter((i) => i.tag !== tag);
    this.markDirty();
  }

  addCustomOutbound(outbound: any): void {
    this.customOutbounds.push(outbound);
    this.markDirty();
  }

  updateCustomOutbound(index: number, outbound: any): void {
    if (index >= 0 && index < this.customOutbounds.length) {
      this.customOutbounds[index] = outbound;
      this.markDirty();
    }
  }

  removeCustomOutbound(index: number): void {
    if (index >= 0 && index < this.customOutbounds.length) {
      this.customOutbounds.splice(index, 1);
      this.markDirty();
    }
  }

  addDnsServer(server: string | DNSServer): void {
    this.dnsConfig.servers.push(server);
    this.markDirty();
  }

  updateDnsServer(index: number, server: string | DNSServer): void {
    if (index >= 0 && index < this.dnsConfig.servers.length) {
      this.dnsConfig.servers[index] = server;
      this.markDirty();
    }
  }

  removeDnsServer(index: number): void {
    if (index >= 0 && index < this.dnsConfig.servers.length) {
      this.dnsConfig.servers.splice(index, 1);
      this.markDirty();
    }
  }
}

export const XRAY_CONTEXT_KEY = Symbol('xray-context');

export function setXrayContext(ctx: XrayContext): void {
  setContext(XRAY_CONTEXT_KEY, ctx);
}

export function getXrayContext(): XrayContext {
  return getContext<XrayContext>(XRAY_CONTEXT_KEY);
}
