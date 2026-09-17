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
