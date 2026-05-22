export const xraySchema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  type: 'object',
  title: 'Xray Configuration',
  description: 'Xray-core configuration file',
  properties: {
    log: {
      type: 'object',
      description: 'Log configuration',
      properties: {
        access: { type: 'string', description: 'Access log file path' },
        error: { type: 'string', description: 'Error log file path' },
        loglevel: {
          type: 'string',
          enum: ['debug', 'info', 'warning', 'error', 'none'],
          description: 'Log level'
        }
      }
    },
    api: {
      type: 'object',
      description: 'API configuration for stats and control',
      properties: {
        tag: { type: 'string', description: 'API inbound tag' },
        services: {
          type: 'array',
          items: {
            type: 'string',
            enum: ['HandlerService', 'LoggerService', 'StatsService', 'RoutingService']
          }
        }
      }
    },
    dns: {
      type: 'object',
      description: 'DNS configuration',
      properties: {
        servers: {
          type: 'array',
          items: {
            oneOf: [
              { type: 'string', description: 'DNS server address' },
              {
                type: 'object',
                properties: {
                  address: { type: 'string' },
                  port: { type: 'integer' },
                  domains: { type: 'array', items: { type: 'string' } }
                }
              }
            ]
          }
        }
      }
    },
    routing: {
      type: 'object',
      description: 'Traffic routing rules',
      properties: {
        domainStrategy: {
          type: 'string',
          enum: ['AsIs', 'IPIfNonMatch', 'IPOnDemand'],
          description: 'Domain resolution strategy'
        },
        domainMatcher: {
          type: 'string',
          enum: ['hybrid', 'linear']
        },
        rules: {
          type: 'array',
          description: 'Routing rules',
          items: {
            type: 'object',
            properties: {
              type: { type: 'string', enum: ['field'], description: 'Rule type' },
              domain: {
                type: 'array',
                items: { type: 'string' },
                description: 'Domain matching list'
              },
              ip: { type: 'array', items: { type: 'string' }, description: 'IP matching list' },
              port: { type: 'string', description: 'Port range' },
              network: { type: 'string', enum: ['tcp', 'udp'], description: 'Network protocol' },
              source: { type: 'array', items: { type: 'string' }, description: 'Source IP/CIDR' },
              user: { type: 'array', items: { type: 'string' } },
              inboundTag: { type: 'array', items: { type: 'string' } },
              protocol: { type: 'array', items: { type: 'string' } },
              outboundTag: { type: 'string', description: 'Target outbound tag' },
              balancerTag: { type: 'string', description: 'Target balancer tag' }
            }
          }
        },
        balancers: {
          type: 'array',
          description: 'Load balancing configurations',
          items: {
            type: 'object',
            properties: {
              tag: { type: 'string', description: 'Balancer tag name' },
              selector: {
                type: 'array',
                items: { type: 'string' },
                description: 'Selector patterns for outbound tags'
              },
              strategy: {
                type: 'object',
                properties: {
                  type: { type: 'string', enum: ['random', 'leastPing', 'roundRobin', 'leastLoad'] }
                }
              }
            }
          }
        }
      }
    },
    inbounds: {
      type: 'array',
      description: 'Inbound proxy configurations',
      items: {
        type: 'object',
        properties: {
          tag: { type: 'string', description: 'Inbound tag identifier' },
          port: { type: 'integer', description: 'Listening port' },
          protocol: {
            type: 'string',
            enum: [
              'vmess',
              'vless',
              'trojan',
              'shadowsocks',
              'socks',
              'http',
              'dokodemo-door',
              'mtproto'
            ],
            description: 'Inbound protocol'
          },
          listen: { type: 'string', description: 'Bind address' },
          sniffing: {
            type: 'object',
            properties: {
              enabled: { type: 'boolean' },
              destOverride: { type: 'array', items: { type: 'string' } }
            }
          },
          settings: { type: 'object', description: 'Protocol-specific settings' },
          streamSettings: {
            type: 'object',
            description: 'Transport settings (TLS, WebSocket, etc.)'
          }
        }
      }
    },
    outbounds: {
      type: 'array',
      description: 'Outbound proxy configurations',
      items: {
        type: 'object',
        properties: {
          tag: { type: 'string', description: 'Outbound tag identifier' },
          protocol: {
            type: 'string',
            enum: [
              'vmess',
              'vless',
              'trojan',
              'shadowsocks',
              'freedom',
              'blackhole',
              'dns',
              'loopback'
            ],
            description: 'Outbound protocol'
          },
          settings: { type: 'object', description: 'Protocol-specific settings' },
          streamSettings: { type: 'object', description: 'Transport settings' },
          proxySettings: { type: 'object', description: 'Proxy forwarding settings' },
          mux: {
            type: 'object',
            description: 'Multiplexing configuration',
            properties: {
              enabled: { type: 'boolean' },
              concurrency: { type: 'integer' },
              xudpConcurrency: { type: 'integer' },
              xudpProxyUDP: { type: 'boolean' }
            }
          }
        }
      }
    },
    policy: {
      type: 'object',
      description: 'Connection policy configuration',
      properties: {
        levels: {
          type: 'object',
          additionalProperties: {
            type: 'object',
            properties: {
              handshake: { type: 'integer' },
              connIdle: { type: 'integer' },
              uplinkOnly: { type: 'integer' },
              downlinkOnly: { type: 'integer' },
              statsUserUplink: { type: 'boolean' },
              statsUserDownlink: { type: 'boolean' },
              bufferSize: { type: 'integer' }
            }
          }
        },
        system: {
          type: 'object',
          properties: {
            statsInboundUplink: { type: 'boolean' },
            statsInboundDownlink: { type: 'boolean' },
            statsOutboundUplink: { type: 'boolean' },
            statsOutboundDownlink: { type: 'boolean' }
          }
        }
      }
    },
    stats: { type: 'object' },
    reverse: {
      type: 'object',
      description: 'Reverse proxy configuration',
      properties: {
        bridges: { type: 'array', items: { type: 'object' } },
        portals: { type: 'array', items: { type: 'object' } }
      }
    },
    observatory: {
      type: 'object',
      description: 'Outbound health monitoring',
      properties: {
        subjectSelector: { type: 'array', items: { type: 'string' } },
        probeURL: { type: 'string', description: 'URL for health probes' },
        probeInterval: { type: 'string', description: 'Probe interval (e.g. 10s)' },
        enableConcurrency: { type: 'boolean' }
      }
    }
  }
};
