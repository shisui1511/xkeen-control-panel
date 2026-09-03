export const shadowsocksCiphers = [
  '2022-blake3-aes-128-gcm',
  '2022-blake3-aes-256-gcm',
  '2022-blake3-chacha20-poly1305',
  'aes-256-gcm',
  'aes-128-gcm',
  'chacha20-poly1305',
  'xchacha20-poly1305',
  'none'
] as const;

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
          settings: {
            type: 'object',
            description: 'Protocol-specific settings',
            properties: {
              method: {
                type: 'string',
                enum: [...shadowsocksCiphers],
                description: 'Shadowsocks encryption method'
              }
            }
          },
          streamSettings: {
            type: 'object',
            description: 'Transport settings (TLS, WebSocket, etc.)',
            properties: {
              sockopt: {
                type: 'object',
                description: 'Socket options for connection tuning and proxy chaining',
                properties: {
                  mark: { type: 'integer', description: 'SO_MARK value for routing' },
                  tcpFastOpen: {
                    oneOf: [{ type: 'boolean' }, { type: 'integer' }],
                    description: 'TCP Fast Open (TFO)'
                  },
                  tcpMptcp: { type: 'boolean', description: 'Multipath TCP (MPTCP)' },
                  dialerProxy: { type: 'string', description: 'Outbound tag for chained proxying' }
                }
              }
            }
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
              'loopback',
              'wireguard'
            ],
            description: 'Outbound protocol'
          },
          settings: {
            type: 'object',
            description: 'Protocol-specific settings',
            properties: {
              secretKey: { type: 'string', description: 'WireGuard private key' },
              address: {
                type: 'array',
                items: { type: 'string' },
                description: 'Local tunnel IP addresses with CIDR mask'
              },
              peers: {
                type: 'array',
                description: 'Peer list',
                items: {
                  type: 'object',
                  properties: {
                    endpoint: { type: 'string', description: 'Remote server address:port' },
                    publicKey: { type: 'string', description: 'Remote server public key' },
                    preSharedKey: { type: 'string', description: 'Pre-shared key (PSK)' },
                    keepAlive: { type: 'integer', description: 'Keepalive interval in seconds' },
                    allowedIPs: {
                      type: 'array',
                      items: { type: 'string' },
                      description: 'Allowed IPs routing CIDR'
                    }
                  }
                }
              },
              mtu: { type: 'integer', description: 'Interface MTU' },
              reserved: {
                type: 'array',
                items: { type: 'integer' },
                description: 'Reserved bytes for handshake padding'
              },
              method: {
                type: 'string',
                enum: [...shadowsocksCiphers],
                description: 'Shadowsocks encryption method'
              },
              servers: {
                type: 'array',
                items: {
                  type: 'object',
                  properties: {
                    method: {
                      type: 'string',
                      enum: [...shadowsocksCiphers],
                      description: 'Shadowsocks encryption method'
                    }
                  }
                }
              }
            }
          },
          streamSettings: {
            type: 'object',
            description: 'Transport settings',
            properties: {
              network: { type: 'string' },
              security: { type: 'string' },
              sockopt: {
                type: 'object',
                description: 'Socket options for connection tuning and proxy chaining',
                properties: {
                  mark: { type: 'integer', description: 'SO_MARK value for routing' },
                  tcpFastOpen: {
                    oneOf: [{ type: 'boolean' }, { type: 'integer' }],
                    description: 'TCP Fast Open (TFO)'
                  },
                  tcpMptcp: { type: 'boolean', description: 'Multipath TCP (MPTCP)' },
                  dialerProxy: { type: 'string', description: 'Outbound tag for chained proxying' }
                }
              }
            }
          },
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
    fakedns: {
      type: 'object',
      description: 'FakeDNS pool configuration',
      properties: {
        ipPool: { type: 'string', description: 'Fake-IP address pool CIDR (e.g. 198.18.0.0/15)' },
        poolSize: { type: 'integer', description: 'Fake-IP pool size' }
      }
    },
    burstObservatory: {
      type: 'object',
      description: 'Burst health monitoring',
      properties: {
        subjectSelector: { type: 'array', items: { type: 'string' } },
        probeURL: { type: 'string', description: 'URL for burst health probes' },
        probeInterval: { type: 'string', description: 'Probe interval' }
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
