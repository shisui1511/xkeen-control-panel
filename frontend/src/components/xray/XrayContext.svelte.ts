import type { UIRoutingRule } from '../../lib/constructors/xrayRouting';

/** A routing rule as edited in the constructor (all Xray fields kept). */
export type XrayRoutingRule = UIRoutingRule & { type?: 'field' };

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
