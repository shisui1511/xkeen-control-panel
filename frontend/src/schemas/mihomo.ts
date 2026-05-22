export const mihomoSchema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  type: 'object',
  title: 'Mihomo Configuration',
  description: 'Mihomo (Clash.Meta) configuration file',
  properties: {
    port: {
      type: 'integer',
      description: 'HTTP proxy port',
      default: 7890
    },
    'socks-port': {
      type: 'integer',
      description: 'SOCKS5 proxy port',
      default: 7891
    },
    'mixed-port': {
      type: 'integer',
      description: 'Mixed HTTP+SOCKS port',
      default: 7892
    },
    'redir-port': {
      type: 'integer',
      description: 'Transparent proxy port (Linux)'
    },
    'tproxy-port': {
      type: 'integer',
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
      description: 'REST API bind address (e.g. 127.0.0.1:9090)'
    },
    'external-ui': {
      type: 'string',
      description: 'Path to external dashboard files'
    },
    secret: {
      type: 'string',
      description: 'API secret token'
    },
    'interface-name': {
      type: 'string',
      description: 'Bind to specific network interface'
    },
    'routing-mark': {
      type: 'integer',
      description: 'SO_MARK value for Linux'
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
      description: 'Geo update interval in hours'
    },
    sniffer: {
      type: 'object',
      description: 'Traffic sniffing configuration',
      properties: {
        enable: { type: 'boolean' },
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
        'fake-ip-range': { type: 'string', description: 'Fake-IP address pool CIDR' },
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
        'proxy-server-nameserver': { type: 'array', items: { type: 'string' } }
      }
    },
    hosts: {
      type: 'object',
      description: 'Static host mappings',
      additionalProperties: {
        oneOf: [{ type: 'string' }, { type: 'array', items: { type: 'string' } }]
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
          port: { type: 'integer', description: 'Server port' },
          password: { type: 'string' },
          uuid: { type: 'string', description: 'VMess/VLESS UUID' },
          alterId: { type: 'integer' },
          cipher: { type: 'string', description: 'Encryption method' },
          udp: { type: 'boolean', description: 'Enable UDP relay' },
          tfo: { type: 'boolean', description: 'Enable TCP Fast Open' },
          'skip-cert-verify': { type: 'boolean' },
          tls: { type: 'boolean' },
          network: { type: 'string', enum: ['tcp', 'udp', 'ws', 'grpc', 'h2'] }
        },
        required: ['name', 'type', 'server', 'port']
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
            enum: ['select', 'url-test', 'fallback', 'load-balance', 'relay'],
            description: 'Group type'
          },
          proxies: {
            type: 'array',
            items: { type: 'string' },
            description: 'Proxy names in this group'
          },
          url: { type: 'string', description: 'Test URL for url-test/fallback' },
          interval: { type: 'integer', description: 'Test interval in seconds' },
          tolerance: { type: 'integer', description: 'Latency tolerance in ms' },
          lazy: { type: 'boolean', description: 'Lazy test (only on select)' },
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
    rules: {
      type: 'array',
      description: 'Traffic routing rules',
      items: {
        type: 'string',
        description: 'Rule in format: TYPE,ARG,POLICY or MATCH,POLICY'
      }
    },
    script: {
      type: 'object',
      description: 'Script-based configuration'
    }
  }
};
