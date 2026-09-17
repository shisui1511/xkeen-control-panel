export const mihomoSchema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  type: 'object',
  title: 'Mihomo Configuration',
  description: 'Mihomo (Clash.Meta) configuration file',
  properties: {
    port: {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: 'HTTP proxy port',
      default: 7890
    },
    'socks-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: 'SOCKS5 proxy port',
      default: 7891
    },
    'mixed-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: 'Mixed HTTP+SOCKS port',
      default: 7892
    },
    'redir-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: 'Transparent proxy port (Linux)'
    },
    'tproxy-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description: 'TPROXY port (Linux)'
    },
    'allow-lan': {
      type: 'boolean',
      description: 'Allow LAN connections',
      default: false
    },
    'bind-address': {
      type: 'string',
      description: 'Bind address',
      default: '*'
    },
    mode: {
      type: 'string',
      enum: ['rule', 'global', 'direct'],
      description: 'Proxy mode',
      default: 'rule'
    },
    'log-level': {
      type: 'string',
      enum: ['info', 'warning', 'error', 'debug', 'silent'],
      description: 'Log level',
      default: 'info'
    },
    ipv6: {
      type: 'boolean',
      description: 'Enable IPv6',
      default: false
    },
    'external-controller': {
      type: 'string',
      pattern: '^[^:\\s]*:\\d{1,5}$',
      description: 'REST API bind address (e.g. 127.0.0.1:9090)'
    },
    'external-controller-unix': {
      type: 'string',
      description: 'Unix Domain Socket path for REST API (e.g. /opt/var/run/mihomo.sock)'
    },
    'external-controller-tls': {
      type: 'string',
      description: 'HTTPS bind address for REST API (requires tls-cert/tls-key)'
    },
    'external-ui': {
      type: 'string',
      description: 'Path to external dashboard files'
    },
    'external-ui-name': {
      type: 'string',
      description: 'Name of the bundled external dashboard to serve'
    },
    'external-ui-url': {
      type: 'string',
      description: 'Download URL used to auto-fetch the external dashboard on startup'
    },
    secret: {
      type: 'string',
      description: 'API secret token'
    },
    authentication: {
      type: 'array',
      items: { type: 'string' },
      description: 'Basic-auth credentials for HTTP/SOCKS/Mixed inbound in "user:pass" form'
    },
    'skip-auth-prefixes': {
      type: 'array',
      items: { type: 'string' },
      description: 'Source CIDR prefixes exempt from inbound authentication'
    },
    'lan-allowed-ips': {
      type: 'array',
      items: { type: 'string' },
      description: 'CIDR list allowed to use inbound listeners when allow-lan is enabled'
    },
    'lan-disallowed-ips': {
      type: 'array',
      items: { type: 'string' },
      description: 'CIDR list denied from using inbound listeners when allow-lan is enabled'
    },
    'interface-name': {
      type: 'string',
      description: 'Bind to specific network interface'
    },
    'routing-mark': {
      type: 'integer',
      minimum: 0,
      description: 'SO_MARK value for Linux'
    },
    'global-ua': {
      type: 'string',
      description: 'Custom User-Agent for outbound HTTP requests made by the core itself'
    },
    'keep-alive-idle': {
      type: 'integer',
      minimum: 0,
      description: 'TCP keep-alive idle time in seconds before probing starts'
    },
    'keep-alive-interval': {
      type: 'integer',
      minimum: 0,
      description: 'TCP keep-alive probe interval in seconds'
    },
    'tcp-concurrent': {
      type: 'boolean',
      description: 'Dial all resolved IPs concurrently and use the fastest handshake'
    },
    'unified-delay': {
      type: 'boolean',
      description: 'Measure proxy latency including handshake time for consistent comparisons'
    },
    'find-process-mode': {
      type: 'string',
      enum: ['always', 'strict', 'off'],
      description: 'Process name resolution mode'
    },
    'global-client-fingerprint': {
      type: 'string',
      enum: [
        'chrome',
        'firefox',
        'safari',
        'ios',
        'android',
        'edge',
        '360',
        'qq',
        'random',
        'none'
      ],
      description: 'Default TLS fingerprint'
    },
    profile: {
      type: 'object',
      description: 'Profile settings',
      properties: {
        'store-selected': {
          type: 'boolean',
          description: 'Remember selected proxy for groups'
        },
        'store-fake-ip': {
          type: 'boolean',
          description: 'Cache fake-ip mappings'
        }
      }
    },
    'geodata-mode': {
      type: 'boolean',
      description: 'Use geodata format instead of GeoSite/GeoIP'
    },
    'geodata-loader': {
      type: 'string',
      enum: ['standard', 'memconservative'],
      description: 'GeoData loader strategy (memconservative trades speed for lower RAM usage)'
    },
    'geox-url': {
      type: 'object',
      description: 'Custom GeoIP/GeoSite download URLs',
      properties: {
        geoip: { type: 'string' },
        geosite: { type: 'string' },
        mmdb: { type: 'string' }
      }
    },
    'geo-auto-update': {
      type: 'boolean',
      description: 'Auto-update GeoIP/GeoSite'
    },
    'geo-update-interval': {
      type: 'integer',
      minimum: 1,
      description: 'Geo update interval in hours'
    },
    ntp: {
      type: 'object',
      description: 'NTP time sync configuration',
      properties: {
        enable: { type: 'boolean' },
        'write-to-system': {
          type: 'boolean',
          description: 'Write synced time to the system clock'
        },
        server: { type: 'string', description: 'NTP server address' },
        port: { type: 'integer', minimum: 1, maximum: 65535 },
        interval: { type: 'integer', description: 'Sync interval in seconds' }
      }
    },
    experimental: {
      type: 'object',
      description: 'Experimental / unstable core features, subject to change between releases',
      properties: {
        'ignore-resolve-fail': {
          type: 'boolean',
          description: 'Do not fail dial on DNS resolve error'
        },
        'dialer-ip-version': {
          type: 'string',
          enum: ['dual', '4', '6', 'ipv4', 'ipv6', 'ipv4-prefer', 'ipv6-prefer'],
          description: 'Preferred IP version when dialing'
        }
      }
    },
    sniffer: {
      type: 'object',
      description: 'Traffic sniffing configuration',
      properties: {
        enable: { type: 'boolean' },
        'force-dns-mapping': { type: 'boolean' },
        'parse-pure-ip': { type: 'boolean' },
        'override-destination': { type: 'boolean' },
        sniff: {
          type: 'object',
          properties: {
            TLS: { type: 'boolean' },
            HTTP: { type: 'boolean' },
            QUIC: { type: 'boolean' }
          }
        },
        'force-domain': { type: 'array', items: { type: 'string' } },
        'skip-domain': { type: 'array', items: { type: 'string' } },
        'port-whitelist': { type: 'array', items: { type: 'integer' } }
      }
    },
    tun: {
      type: 'object',
      description: 'TUN device configuration',
      properties: {
        enable: { type: 'boolean' },
        device: { type: 'string', description: 'TUN device name' },
        stack: {
          type: 'string',
          enum: ['system', 'gvisor', 'mixed'],
          description: 'TUN stack implementation'
        },
        'dns-hijack': { type: 'array', items: { type: 'string' } },
        'auto-route': { type: 'boolean' },
        'auto-detect-interface': { type: 'boolean' },
        'strict-route': { type: 'boolean' },
        mtu: { type: 'integer' }
      }
    },
    dns: {
      type: 'object',
      description: 'DNS configuration',
      properties: {
        enable: { type: 'boolean' },
        listen: { type: 'string', description: 'DNS server bind address' },
        'default-nameserver': {
          type: 'array',
          items: { type: 'string' },
          description: 'Default DNS resolvers'
        },
        'enhanced-mode': {
          type: 'string',
          enum: ['fake-ip', 'redir-host', 'normal'],
          description: 'DNS enhanced mode'
        },
        'fake-ip-range': {
          type: 'string',
          pattern: '^([0-9]{1,3}\\.){3}[0-9]{1,3}/\\d{1,2}$',
          description: 'Fake-IP address pool CIDR'
        },
        'fake-ip-filter': { type: 'array', items: { type: 'string' } },
        nameserver: {
          type: 'array',
          items: { type: 'string' },
          description: 'Primary DNS servers'
        },
        fallback: { type: 'array', items: { type: 'string' }, description: 'Fallback DNS servers' },
        'fallback-filter': {
          type: 'object',
          properties: {
            geoip: { type: 'boolean' },
            'geoip-code': { type: 'string' },
            ipcidr: { type: 'array', items: { type: 'string' } },
            domain: { type: 'array', items: { type: 'string' } }
          }
        },
        'nameserver-policy': {
          type: 'object',
          additionalProperties: { type: 'string' },
          description: 'Per-domain DNS policy'
        },
        'proxy-server-nameserver': { type: 'array', items: { type: 'string' } },
        'direct-nameserver': { type: 'array', items: { type: 'string' } }
      }
    },
    hosts: {
      type: 'object',
      description: 'Static host mappings',
      additionalProperties: {
        oneOf: [{ type: 'string' }, { type: 'array', items: { type: 'string' } }]
      }
    },
    'proxy-providers': {
      type: 'object',
      description: 'Named remote/local proxy subscription providers, referenced by proxy-groups',
      additionalProperties: {
        type: 'object',
        properties: {
          type: {
            type: 'string',
            enum: ['http', 'file', 'inline'],
            description: 'Provider source type'
          },
          url: { type: 'string', description: 'Subscription URL (type: http)' },
          path: { type: 'string', description: 'Local cache/config path' },
          interval: { type: 'integer', description: 'Auto-update interval in seconds' },
          'health-check': {
            type: 'object',
            properties: {
              enable: { type: 'boolean' },
              url: { type: 'string' },
              interval: { type: 'integer' },
              lazy: { type: 'boolean' }
            }
          },
          filter: { type: 'string', description: 'Regex filter applied to proxy names' },
          'exclude-filter': {
            type: 'string',
            description: 'Regex exclusion filter applied to proxy names'
          },
          'exclude-type': { type: 'string', description: 'Regex filter excluding proxy types' },
          override: {
            type: 'object',
            description: 'Per-field overrides applied to every proxy from this provider'
          }
        },
        required: ['type']
      }
    },
    proxies: {
      type: 'array',
      description: 'Proxy server definitions',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string', description: 'Proxy name' },
          type: {
            type: 'string',
            enum: [
              'ss',
              'ssr',
              'vmess',
              'vless',
              'trojan',
              'hysteria',
              'hysteria2',
              'tuic',
              'wireguard',
              'socks5',
              'http',
              'snell'
            ],
            description: 'Proxy protocol type'
          },
          server: { type: 'string', description: 'Server address' },
          port: { type: 'integer', minimum: 1, maximum: 65535, description: 'Server port' },
          username: { type: 'string', description: 'Username (socks5/http auth)' },
          password: { type: 'string' },
          uuid: {
            type: 'string',
            pattern:
              '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
            description: 'VMess/VLESS UUID'
          },
          alterId: { type: 'integer' },
          cipher: { type: 'string', description: 'Encryption method' },
          udp: { type: 'boolean', description: 'Enable UDP relay' },
          tfo: { type: 'boolean', description: 'Enable TCP Fast Open' },
          'skip-cert-verify': { type: 'boolean' },
          tls: { type: 'boolean' },
          sni: { type: 'string', description: 'TLS Server Name Indication (overrides server)' },
          servername: { type: 'string', description: 'Alias of sni used by some proxy types' },
          alpn: { type: 'array', items: { type: 'string' }, description: 'TLS ALPN protocol list' },
          'client-fingerprint': {
            type: 'string',
            enum: ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random'],
            description: 'uTLS client hello fingerprint'
          },
          flow: {
            type: 'string',
            enum: ['xtls-rprx-vision', ''],
            description: 'VLESS flow control (XTLS)'
          },
          network: {
            type: 'string',
            enum: ['tcp', 'udp', 'ws', 'grpc', 'h2', 'http'],
            description: 'Transport protocol'
          },
          'ws-opts': {
            type: 'object',
            description: 'WebSocket transport options (network: ws)',
            properties: {
              path: { type: 'string' },
              headers: { type: 'object', additionalProperties: { type: 'string' } },
              'max-early-data': { type: 'integer' },
              'early-data-header-name': { type: 'string' }
            }
          },
          'grpc-opts': {
            type: 'object',
            description: 'gRPC transport options (network: grpc)',
            properties: {
              'grpc-service-name': { type: 'string' }
            }
          },
          'h2-opts': {
            type: 'object',
            description: 'HTTP/2 transport options (network: h2)',
            properties: {
              host: { type: 'array', items: { type: 'string' } },
              path: { type: 'string' }
            }
          },
          'reality-opts': {
            type: 'object',
            description: 'REALITY TLS camouflage options (requires tls: true)',
            properties: {
              'public-key': { type: 'string', description: 'REALITY server public key' },
              'short-id': { type: 'string', description: 'REALITY short ID' }
            },
            required: ['public-key']
          },
          plugin: {
            type: 'string',
            enum: ['obfs', 'v2ray-plugin', 'shadow-tls', 'restls'],
            description: 'Shadowsocks plugin'
          },
          'plugin-opts': {
            type: 'object',
            description: 'Shadowsocks plugin-specific options',
            properties: {
              mode: { type: 'string' },
              host: { type: 'string' },
              tls: { type: 'boolean' },
              'skip-cert-verify': { type: 'boolean' },
              password: { type: 'string' },
              version: { type: 'string' }
            }
          },
          obfs: { type: 'string', description: 'Hysteria obfuscation mode' },
          'obfs-password': { type: 'string', description: 'Hysteria obfuscation password' },
          'auth-str': { type: 'string', description: 'Hysteria auth string (v1)' },
          up: { type: 'string', description: 'Hysteria upload bandwidth (e.g. "100 Mbps")' },
          down: { type: 'string', description: 'Hysteria download bandwidth (e.g. "100 Mbps")' },
          'congestion-controller': {
            type: 'string',
            enum: ['cubic', 'new_reno', 'bbr'],
            description: 'TUIC congestion control algorithm'
          },
          'reduce-rtt': { type: 'boolean', description: 'TUIC 0-RTT handshake' },
          'dialer-proxy': { type: 'string', description: 'Chain dialer proxy' },
          ports: { type: 'string', description: 'Port hopping range' },
          smux: { type: 'object', description: 'Multiplexing settings' },
          // WireGuard & AmneziaWG (TMPL-08)
          'private-key': { type: 'string', description: 'WireGuard private key' },
          'public-key': { type: 'string', description: 'WireGuard or Reality public key' },
          'pre-shared-key': { type: 'string', description: 'WireGuard pre-shared key (optional)' },
          ip: { type: 'string', description: 'WireGuard interface local IP' },
          mtu: { type: 'integer', description: 'WireGuard interface MTU' },
          'amnezia-wg-option': {
            type: 'object',
            description: 'AmneziaWG obfuscation options',
            properties: {
              jc: { type: 'integer', description: 'Junk packet count' },
              jmin: { type: 'integer', description: 'Minimum junk packet size' },
              jmax: { type: 'integer', description: 'Maximum junk packet size' },
              s1: { type: 'integer', description: 'Handshake response padding size' },
              s2: { type: 'integer', description: 'Initiation response padding size' },
              s3: { type: 'integer', description: 'Extended padding size 3' },
              s4: { type: 'integer', description: 'Extended padding size 4' },
              h1: {
                type: ['integer', 'string'],
                description: 'Initiation packet magic header (number or range)'
              },
              h2: {
                type: ['integer', 'string'],
                description: 'Response packet magic header (number or range)'
              },
              h3: {
                type: ['integer', 'string'],
                description: 'Underload packet magic header (number or range)'
              },
              h4: {
                type: ['integer', 'string'],
                description: 'Transport packet magic header (number or range)'
              },
              version: { type: 'string', description: 'AmneziaWG protocol version' },
              'header-protection-key': {
                type: 'string',
                description: 'Header protection secret key'
              },
              i1: { type: 'string', description: 'CPS initiation token 1' },
              i2: { type: 'string', description: 'CPS initiation token 2' },
              i3: { type: 'string', description: 'CPS initiation token 3' },
              i4: { type: 'string', description: 'CPS initiation token 4' },
              i5: { type: 'string', description: 'CPS initiation token 5' },
              'content-padding-addition': {
                type: 'integer',
                description: 'Content padding addition size'
              },
              'random-trailers': { type: 'boolean', description: 'Random trailers enabled' },
              'disable-cookies': { type: 'boolean', description: 'Disable cookies flag' },
              'rekey-after-time': { type: 'integer', description: 'Rekey after time (seconds)' }
            }
          }
        },
        required: ['name', 'type', 'server', 'port'],
        allOf: [
          {
            if: { properties: { type: { enum: ['vmess', 'vless'] } }, required: ['type'] },
            then: { required: ['uuid'] }
          },
          {
            if: {
              properties: { type: { enum: ['ss', 'ssr', 'trojan', 'snell', 'hysteria2'] } },
              required: ['type']
            },
            then: { required: ['password'] }
          },
          {
            if: { properties: { type: { const: 'tuic' } }, required: ['type'] },
            then: { required: ['uuid', 'password'] }
          },
          {
            if: { properties: { type: { const: 'wireguard' } }, required: ['type'] },
            then: { required: ['private-key', 'ip'] }
          }
        ]
      }
    },
    'proxy-groups': {
      type: 'array',
      description: 'Proxy group definitions',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string', description: 'Group name' },
          type: {
            type: 'string',
            enum: ['select', 'url-test', 'fallback', 'load-balance'],
            description: 'Group type'
          },
          proxies: {
            type: 'array',
            items: { type: 'string' },
            description: 'Proxy names in this group'
          },
          url: { type: 'string', description: 'Test URL for url-test/fallback' },
          interval: { type: 'integer', minimum: 1, description: 'Test interval in seconds' },
          tolerance: { type: 'integer', minimum: 0, description: 'Latency tolerance in ms' },
          lazy: { type: 'boolean', description: 'Lazy test (only on select)' },
          'expected-status': { type: 'string', description: 'Expected HTTP status code' },
          'exclude-type': { type: 'string', description: 'Exclude proxy types regex' },
          'include-all': { type: 'boolean', description: 'Include all proxies' },
          'include-all-providers': { type: 'boolean', description: 'Include all providers' },
          'disable-udp': { type: 'boolean' },
          strategy: {
            type: 'string',
            enum: ['consistent-hashing', 'round-robin'],
            description: 'Load balance strategy'
          }
        },
        required: ['name', 'type']
      }
    },
    listeners: {
      type: 'array',
      description: 'Inbound listener definitions',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string', description: 'Listener name (matchable with IN-NAME)' },
          type: {
            type: 'string',
            enum: [
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
            ],
            description: 'Inbound listener protocol type'
          },
          listen: { type: 'string', description: 'Binding IP address (defaults to 0.0.0.0)' },
          port: {
            oneOf: [
              { type: 'integer', minimum: 1, maximum: 65535 },
              { type: 'string', description: 'Port range, e.g. "20000-20100"' }
            ],
            description: 'Listening port or port range'
          },
          proxy: {
            type: 'string',
            description: 'Forward traffic directly to proxy/group bypassing rules'
          },
          rule: {
            type: 'string',
            description: 'Name of sub-rules section to match traffic against'
          },
          'routing-mark': {
            type: 'integer',
            minimum: 0,
            description: 'Linux socket SO_MARK value'
          },
          udp: { type: 'boolean', description: 'Enable UDP support' },
          users: {
            type: 'array',
            description: 'Inbound authentication credentials',
            items: {
              type: 'object',
              properties: {
                username: { type: 'string', description: 'Username' },
                password: { type: 'string', description: 'Password' }
              }
            }
          },
          cipher: { type: 'string', description: 'Shadowsocks cipher' },
          password: { type: 'string', description: 'Shadowsocks password' }
        },
        required: ['name', 'type']
      }
    },
    'rule-providers': {
      type: 'object',
      description: 'Named remote/local rule-set providers, referenced from rules via RULE-SET',
      additionalProperties: {
        type: 'object',
        properties: {
          type: {
            type: 'string',
            enum: ['http', 'file', 'inline'],
            description: 'Provider source type'
          },
          behavior: {
            type: 'string',
            enum: ['domain', 'ipcidr', 'classical'],
            description: 'Rule-set content format'
          },
          url: { type: 'string', description: 'Rule-set URL (type: http)' },
          path: { type: 'string', description: 'Local cache/config path' },
          format: {
            type: 'string',
            enum: ['yaml', 'text', 'mrs'],
            description: 'Rule-set file format'
          },
          interval: { type: 'integer', description: 'Auto-update interval in seconds' }
        },
        required: ['type', 'behavior']
      }
    },
    'sub-rules': {
      type: 'object',
      description:
        'Named rule subsets matchable from listeners[].rule or rules via SUB-RULE, for split routing scopes',
      additionalProperties: {
        type: 'array',
        items: { type: 'string' }
      }
    },
    rules: {
      type: 'array',
      description: 'Traffic routing rules',
      items: {
        type: 'string',
        pattern:
          '^(DOMAIN|DOMAIN-SUFFIX|DOMAIN-KEYWORD|DOMAIN-REGEX|DOMAIN-WILDCARD|GEOSITE|GEOIP|SRC-GEOIP|IP-ASN|SRC-IP-ASN|IP-CIDR|IP-CIDR6|SRC-IP-CIDR|IP-SUFFIX|SRC-IP-SUFFIX|SRC-PORT|DST-PORT|IN-PORT|IN-TYPE|IN-USER|IN-NAME|PROCESS-NAME|PROCESS-PATH|PROCESS-NAME-REGEX|PROCESS-PATH-REGEX|NETWORK|UID|SUB-RULE|RULE-SET|AND|OR|NOT|MATCH),.+$',
        description: 'Rule in format: TYPE,ARG[,ARG2],POLICY[,no-resolve] or MATCH,POLICY'
      }
    },
    script: {
      type: 'object',
      description: 'Script-based configuration'
    }
  }
};
