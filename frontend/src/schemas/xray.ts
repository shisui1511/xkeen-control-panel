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

const sockoptSchema = {
  type: 'object',
  description: 'Socket options for connection tuning and proxy chaining',
  properties: {
    mark: {
      type: 'integer',
      description:
        "SO_MARK stamped on this outbound's own sockets so XKeen's iptables/ip-rule setup can recognize and exclude Xray's own upstream traffic from transparent-proxy interception — without it, Xray's own connections can get redirected back into itself (a routing loop)."
    },
    tcpFastOpen: {
      oneOf: [{ type: 'boolean' }, { type: 'integer' }],
      description: 'TCP Fast Open (TFO)'
    },
    tcpMptcp: { type: 'boolean', description: 'Multipath TCP (MPTCP)' },
    tcpNoDelay: { type: 'boolean', description: 'Disable Nagle algorithm (TCP_NODELAY)' },
    tcpKeepAliveInterval: { type: 'integer', description: 'TCP keepalive interval in seconds' },
    dialerProxy: { type: 'string', description: 'Outbound tag for chained proxying' }
  }
} as const;

const tlsSettingsSchema = {
  type: 'object',
  description: 'TLS transport security settings (security: tls)',
  properties: {
    serverName: { type: 'string', description: 'SNI sent during TLS handshake' },
    alpn: { type: 'array', items: { type: 'string' }, description: 'TLS ALPN protocol list' },
    minVersion: { type: 'string', enum: ['1.0', '1.1', '1.2', '1.3'] },
    maxVersion: { type: 'string', enum: ['1.0', '1.1', '1.2', '1.3'] },
    fingerprint: {
      type: 'string',
      enum: ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random'],
      description: 'uTLS client hello fingerprint'
    },
    allowInsecure: {
      type: 'boolean',
      description:
        'Accept the server TLS certificate without validation. Only for self-signed certs on a server you control — enabling this generally defeats the point of TLS by allowing a trivial man-in-the-middle.'
    },
    certificates: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          certificateFile: { type: 'string' },
          keyFile: { type: 'string' }
        }
      }
    }
  }
} as const;

const realitySettingsSchema = {
  type: 'object',
  description:
    "REALITY: connects with the TLS certificate of a real, unrelated website (no cert/domain of your own needed) so passive DPI sees what looks like a normal HTTPS handshake to that site. Client-side publicKey/shortId must match the server's REALITY config exactly, or the handshake fails.",
  properties: {
    show: { type: 'boolean', description: 'Print debug info (server-side)' },
    dest: { type: 'string', description: 'Camouflage target address:port (server-side)' },
    xver: { type: 'integer', description: 'PROXY protocol version toward dest (server-side)' },
    serverNames: {
      type: 'array',
      items: { type: 'string' },
      description: 'Allowed SNI values (server-side)'
    },
    privateKey: { type: 'string', description: 'REALITY private key (server-side)' },
    shortIds: {
      type: 'array',
      items: { type: 'string' },
      description: 'Allowed short IDs (server-side)'
    },
    publicKey: { type: 'string', description: 'REALITY public key (client-side)' },
    shortId: { type: 'string', description: 'REALITY short ID (client-side)' },
    spiderX: { type: 'string', description: 'REALITY spiderX path (client-side)' },
    fingerprint: {
      type: 'string',
      enum: ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random'],
      description: 'uTLS client hello fingerprint (client-side)'
    }
  }
} as const;

const wsSettingsSchema = {
  type: 'object',
  description: 'WebSocket transport options (network: ws)',
  properties: {
    path: { type: 'string' },
    headers: { type: 'object', additionalProperties: { type: 'string' } }
  }
} as const;

const grpcSettingsSchema = {
  type: 'object',
  description: 'gRPC transport options (network: grpc)',
  properties: {
    serviceName: { type: 'string' },
    multiMode: { type: 'boolean' }
  }
} as const;

const httpupgradeSettingsSchema = {
  type: 'object',
  description: 'HTTP-Upgrade transport options (network: httpupgrade)',
  properties: {
    path: { type: 'string' },
    host: { type: 'string' }
  }
} as const;

const streamSettingsSchema = {
  type: 'object',
  description: 'Transport settings (TLS/Reality security + transport-specific options)',
  properties: {
    network: {
      type: 'string',
      enum: ['tcp', 'kcp', 'ws', 'http', 'domainsocket', 'quic', 'grpc', 'httpupgrade', 'xhttp'],
      description:
        'How the connection is wrapped for delivery over the wire. "tcp"/raw is fastest but least disguised; "ws"/"grpc"/"httpupgrade"/"xhttp" ride over an HTTP(S)-like layer so the traffic can hide behind a CDN or plain reverse proxy — pick whichever the server side is configured for, it must match exactly.'
    },
    security: {
      type: 'string',
      enum: ['none', 'tls', 'reality'],
      description:
        '"none": no encryption at this layer (fine if the inner protocol already encrypts, e.g. Shadowsocks). "tls": standard TLS to your own domain/certificate. "reality": borrows a real site\'s certificate identity instead of needing your own — see realitySettings.'
    },
    tlsSettings: tlsSettingsSchema,
    realitySettings: realitySettingsSchema,
    wsSettings: wsSettingsSchema,
    grpcSettings: grpcSettingsSchema,
    httpupgradeSettings: httpupgradeSettingsSchema,
    sockopt: sockoptSchema
  }
} as const;

const vmessVlessUserSchema = {
  type: 'object',
  properties: {
    id: {
      type: 'string',
      pattern: '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
      description: 'Client UUID'
    },
    alterId: { type: 'integer', description: 'VMess legacy AlterID (0 for AEAD)' },
    security: {
      type: 'string',
      enum: ['auto', 'aes-128-gcm', 'chacha20-poly1305', 'none', 'zero'],
      description: 'VMess encryption method'
    },
    encryption: { type: 'string', description: 'VLESS encryption (server: "none")' },
    flow: { type: 'string', enum: ['xtls-rprx-vision', ''], description: 'VLESS flow control' },
    level: { type: 'integer' },
    email: { type: 'string' }
  },
  required: ['id']
} as const;

const trojanClientSchema = {
  type: 'object',
  description: 'Authorized client (trojan inbound)',
  properties: {
    password: { type: 'string' },
    email: { type: 'string' },
    level: { type: 'integer' }
  },
  required: ['password']
} as const;

const inboundClientsSchema = {
  type: 'array',
  description: 'Authorized client list (id-based for vmess/vless, password-based for trojan)',
  items: { oneOf: [vmessVlessUserSchema, trojanClientSchema] }
} as const;

const vmessVlessOutboundSettingsSchema = {
  type: 'object',
  description: 'VMess/VLESS outbound server settings',
  properties: {
    vnext: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          address: { type: 'string', description: 'Server address' },
          port: { type: 'integer', minimum: 1, maximum: 65535, description: 'Server port' },
          users: { type: 'array', items: vmessVlessUserSchema }
        },
        required: ['address', 'port', 'users']
      }
    }
  }
} as const;

const trojanServerEntrySchema = {
  type: 'object',
  properties: {
    address: { type: 'string', description: 'Server address' },
    port: { type: 'integer', minimum: 1, maximum: 65535, description: 'Server port' },
    password: { type: 'string' },
    email: { type: 'string' },
    level: { type: 'integer' }
  },
  required: ['address', 'port', 'password']
} as const;

const inboundFallbackSchema = {
  type: 'object',
  description: 'VLESS fallback for unrecognized/non-proxy traffic',
  properties: {
    name: { type: 'string' },
    alpn: { type: 'string' },
    path: { type: 'string' },
    dest: {
      oneOf: [{ type: 'string' }, { type: 'integer' }],
      description: 'Fallback target address:port'
    },
    xver: { type: 'integer' }
  }
} as const;

const socksHttpAccountSchema = {
  type: 'object',
  properties: {
    user: { type: 'string' },
    pass: { type: 'string' }
  },
  required: ['user', 'pass']
} as const;

const shadowsocksServerEntrySchema = {
  type: 'object',
  properties: {
    address: { type: 'string', description: 'Server address' },
    port: { type: 'integer', minimum: 1, maximum: 65535, description: 'Server port' },
    method: {
      type: 'string',
      enum: [...shadowsocksCiphers],
      description: 'Shadowsocks encryption method'
    },
    password: { type: 'string' },
    uot: { type: 'boolean', description: 'UDP-over-TCP' },
    level: { type: 'integer' }
  },
  required: ['address', 'port', 'method', 'password']
} as const;

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
          description:
            'Whether routing rules match on the domain name, or resolve it to an IP first. "AsIs": match by domain, never resolve unless a rule needs an IP. "IPIfNonMatch": resolve if no domain-based rule matched, then try again by IP. "IPOnDemand": resolve as soon as any rule set needs an IP-based match.'
        },
        domainMatcher: {
          type: 'string',
          enum: ['hybrid', 'linear'],
          description:
            'Algorithm used to match domain rules against traffic. "hybrid" is the faster indexed matcher and the current default; "linear" checks rules in order and is mainly a debugging/compatibility fallback.'
        },
        rules: {
          type: 'array',
          description:
            'Traffic routing rules. Unlike Mihomo, order does not strictly decide priority — the most specific matching rule wins. outboundTag/balancerTag must name a tag that actually exists among outbounds/balancers; a typo does not error, it just silently falls through and never matches.',
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
          port: {
            oneOf: [
              { type: 'integer', minimum: 1, maximum: 65535 },
              { type: 'string', description: 'Port range, e.g. "1000-2000"' }
            ],
            description: 'Listening port or port range'
          },
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
            description:
              'Peeks at TLS SNI / HTTP Host to recover the real domain of a connection when routing only has a bare destination IP to go on (e.g. under transparent/dokodemo-door inbounds) — without it, domain-based routing rules cannot match traffic coming through this inbound.',
            properties: {
              enabled: { type: 'boolean' },
              destOverride: {
                type: 'array',
                items: { type: 'string' },
                description: 'Which protocols to sniff for, e.g. ["tls","http"].'
              },
              routeOnly: {
                type: 'boolean',
                description:
                  'Use the sniffed domain only for routing decisions, keep connecting to the original destination IP — set true when the IP is directly reachable and you just need domain-based rule matching, not a full rewrite.'
              }
            }
          },
          settings: {
            type: 'object',
            description: 'Protocol-specific settings',
            properties: {
              clients: inboundClientsSchema,
              decryption: {
                type: 'string',
                description: 'VLESS decryption ("none" on server side)'
              },
              fallbacks: { type: 'array', items: inboundFallbackSchema },
              method: {
                type: 'string',
                enum: [...shadowsocksCiphers],
                description: 'Shadowsocks encryption method'
              },
              password: { type: 'string', description: 'Shadowsocks/trojan shared password' },
              network: {
                type: 'string',
                enum: ['tcp', 'udp', 'tcp,udp'],
                description: 'Allowed L4 network (shadowsocks/dokodemo-door)'
              },
              auth: {
                type: 'string',
                enum: ['noauth', 'password'],
                description: 'Socks auth mode'
              },
              accounts: {
                type: 'array',
                items: socksHttpAccountSchema,
                description: 'Socks/HTTP credentials'
              },
              udp: { type: 'boolean', description: 'Enable UDP relay' },
              ip: { type: 'string', description: 'IP returned to UDP clients (socks)' },
              address: { type: 'string', description: 'Forward target address (dokodemo-door)' },
              followRedirect: {
                type: 'boolean',
                description: 'Use iptables-redirected destination'
              }
            }
          },
          streamSettings: streamSettingsSchema
        },
        required: ['protocol']
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
              // vmess/vless
              vnext: vmessVlessOutboundSettingsSchema.properties.vnext,
              // trojan
              servers: {
                oneOf: [
                  { type: 'array', items: trojanServerEntrySchema },
                  { type: 'array', items: shadowsocksServerEntrySchema }
                ],
                description: 'Server list (trojan or single-server shadowsocks form)'
              },
              // shadowsocks legacy single-server form (flattened, no servers[])
              address: {
                oneOf: [
                  { type: 'string', description: 'Shadowsocks server address' },
                  {
                    type: 'array',
                    items: { type: 'string' },
                    description: 'WireGuard local tunnel IP addresses with CIDR mask'
                  }
                ]
              },
              port: {
                type: 'integer',
                minimum: 1,
                maximum: 65535,
                description: 'Shadowsocks server port'
              },
              method: {
                type: 'string',
                enum: [...shadowsocksCiphers],
                description: 'Shadowsocks encryption method'
              },
              password: { type: 'string', description: 'Shadowsocks password' },
              // freedom
              domainStrategy: {
                type: 'string',
                enum: ['AsIs', 'UseIP', 'UseIPv4', 'UseIPv6'],
                description:
                  'How the freedom (direct-connect) outbound handles a domain destination. "AsIs" connects by domain (server does the DNS lookup); "UseIP*" resolves via Xray\'s own dns config first — useful to force a specific IP family for direct traffic.'
              },
              redirect: {
                type: 'string',
                description:
                  'Force every connection through this outbound to a fixed address:port instead of the original destination — handy for pointing "direct" traffic at a local service.'
              },
              userLevel: { type: 'integer' },
              // blackhole
              response: {
                type: 'object',
                description:
                  'What the blackhole outbound sends before dropping the connection. "none": close immediately with no data (best for silently killing blocked traffic). "http": send a canned HTTP response first — mimics a real server for protocols that expect one.',
                properties: {
                  type: { type: 'string', enum: ['none', 'http'] }
                }
              },
              // dns (forward outbound)
              network: {
                type: 'string',
                enum: ['tcp', 'udp'],
                description: 'DNS outbound forwarding network'
              },
              nonIPQuery: {
                type: 'string',
                enum: ['drop', 'skip'],
                description: 'DNS outbound behaviour for non-IP queries'
              },
              // loopback
              inboundTag: { type: 'string', description: 'Loopback outbound target inbound tag' },
              // wireguard
              secretKey: { type: 'string', description: 'WireGuard private key' },
              peers: {
                type: 'array',
                description: 'WireGuard peer list',
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
              mtu: { type: 'integer', description: 'WireGuard interface MTU' },
              reserved: {
                type: 'array',
                items: { type: 'integer' },
                description:
                  'Reserved handshake bytes used by some WireGuard-obfuscating servers (e.g. certain VPS providers) to tag/allow traffic. Leave [0,0,0] unless the server explicitly issued specific reserved values — a mismatch fails silently as a connection timeout.'
              }
            }
          },
          streamSettings: streamSettingsSchema,
          proxySettings: { type: 'object', description: 'Proxy forwarding settings' },
          mux: {
            type: 'object',
            description:
              'Multiplexes several logical connections over one underlying TCP/TLS connection, cutting down on repeated handshakes for many short-lived requests — mainly useful on high-latency or handshake-expensive links; can hurt throughput on a single large transfer if concurrency is set too high.',
            properties: {
              enabled: { type: 'boolean' },
              concurrency: { type: 'integer' },
              xudpConcurrency: { type: 'integer' },
              xudpProxyUDP: { type: 'boolean' }
            }
          }
        },
        required: ['protocol']
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
      description:
        "Xray's own fake-ip pool for domain-based routing, independent from Mihomo's dns.fake-ip-range — if both cores run on this router, use non-overlapping ranges so the two never hand out colliding placeholder addresses.",
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
      description:
        'Background health/latency prober for the outbounds listed in subjectSelector — results feed balancer strategies like leastPing so routing can automatically avoid a currently-down or slow upstream.',
      properties: {
        subjectSelector: { type: 'array', items: { type: 'string' } },
        probeURL: { type: 'string', description: 'URL for health probes' },
        probeInterval: { type: 'string', description: 'Probe interval (e.g. 10s)' },
        enableConcurrency: { type: 'boolean' }
      }
    }
  }
};
