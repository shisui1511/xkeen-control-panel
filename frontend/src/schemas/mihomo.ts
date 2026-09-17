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
      description:
        'REDIRECT transparent-proxy port. Only needed if traffic is routed into Mihomo via iptables REDIRECT; on this panel XKeen usually owns the iptables rules, so this is set to match whatever XKeen was configured to redirect to.'
    },
    'tproxy-port': {
      type: 'integer',
      minimum: 1,
      maximum: 65535,
      description:
        'TPROXY transparent-proxy port (preserves original destination IP, needed for correct GEOIP/IP-CIDR rule matching). Same caveat as redir-port: XKeen manages the iptables TPROXY rules on this router, this value must match what XKeen points at.'
    },
    'allow-lan': {
      type: 'boolean',
      description:
        'Allow other LAN devices to reach port/socks-port/mixed-port on this router, not just localhost. Combine with authentication or lan-allowed-ips, otherwise anyone on the LAN gets an open proxy.',
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
      description:
        'REST API bind address (e.g. 127.0.0.1:9090). This panel talks to Mihomo over this API, normally via a local unix socket instead — only set this to a LAN-reachable address (0.0.0.0:9090) if you deliberately want an external dashboard like Zashboard, and always pair it with secret.'
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
      description:
        'Bearer token required to call external-controller. Mandatory in practice whenever external-controller is reachable from the LAN, otherwise anyone on the network can read traffic stats or change the active proxy/rules.'
    },
    authentication: {
      type: 'array',
      items: { type: 'string' },
      description:
        'Basic-auth credentials for HTTP/SOCKS/Mixed inbound in "user:pass" form. Use this (or lan-allowed-ips) whenever allow-lan is true, so the router does not become an open proxy for the whole LAN/guest Wi-Fi.'
    },
    'skip-auth-prefixes': {
      type: 'array',
      items: { type: 'string' },
      description:
        "Source CIDR prefixes exempt from the authentication above — e.g. the router's own LAN subnet, so trusted devices are not prompted while a guest network still is."
    },
    'lan-allowed-ips': {
      type: 'array',
      items: { type: 'string' },
      description:
        'CIDR allow-list for inbound listeners when allow-lan is enabled — the simplest way to expose the proxy to your own devices only, without per-device credentials.'
    },
    'lan-disallowed-ips': {
      type: 'array',
      items: { type: 'string' },
      description:
        'CIDR deny-list for inbound listeners when allow-lan is enabled — evaluated before lan-allowed-ips, useful for blocking a specific guest-Wi-Fi subnet while allowing everything else.'
    },
    'interface-name': {
      type: 'string',
      description: 'Bind to specific network interface'
    },
    'routing-mark': {
      type: 'integer',
      minimum: 0,
      description:
        "SO_MARK stamped on Mihomo's own outbound sockets so XKeen's ip rule/iptables setup can recognize and exclude them from transparent-proxy interception — without it, Mihomo's own upstream connections can get redirected back into itself (a routing loop)."
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
      description:
        'Dial all resolved IPs concurrently (Happy Eyeballs) and use whichever handshake completes first. Helps when a domain resolves to both IPv4 and IPv6 or multiple servers and one path is slow/blocked.'
    },
    'unified-delay': {
      type: 'boolean',
      description:
        'Include TLS/TCP handshake time in the latency shown for url-test/fallback groups, not just round-trip ping. Without it, a proxy with a fast ping but slow handshake can still look "fastest" and get auto-selected.'
    },
    'find-process-mode': {
      type: 'string',
      enum: ['always', 'strict', 'off'],
      description:
        'Controls PROCESS-NAME rule matching by resolving which local process owns a connection. Mihomo running on the router itself (not the client device) generally cannot see per-app process names for LAN clients — leave this off unless you know the router OS actually exposes that info, otherwise it just adds overhead for rules that will never match.'
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
      description:
        'Default uTLS client-hello fingerprint applied to outbound TLS connections when a proxy does not set its own client-fingerprint. Mimicking a real browser (chrome/firefox/...) helps traffic blend in and avoids TLS-fingerprint-based DPI blocking.'
    },
    profile: {
      type: 'object',
      description:
        'What Mihomo persists to disk across restarts (survives router reboots as long as the cache directory is on non-volatile storage).',
      properties: {
        'store-selected': {
          type: 'boolean',
          description:
            'Remember which proxy was manually selected in each select-type group, so a router reboot does not silently fall back to the group default.'
        },
        'store-fake-ip': {
          type: 'boolean',
          description:
            'Persist the fake-ip domain↔IP cache across restarts, so DNS-dependent rules keep matching the same way right after Mihomo restarts instead of needing fresh lookups.'
        }
      }
    },
    'geodata-mode': {
      type: 'boolean',
      description:
        'Use the compact .dat geodata format instead of separate GeoIP.mmdb/GeoSite.dat files — smaller on-disk footprint, relevant on router storage.'
    },
    'geodata-loader': {
      type: 'string',
      enum: ['standard', 'memconservative'],
      description:
        'How geo databases are loaded into memory. "standard" loads them fully for fastest lookups; "memconservative" streams from disk to save RAM — worth switching to on routers with limited RAM (typical Keenetic hardware) if Mihomo is getting OOM-killed.'
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
      description:
        "Built-in NTP client Mihomo can use to correct its own clock. Matters because most Keenetic routers have no battery-backed RTC — after a power loss the clock resets to a stale build date, which breaks TLS certificate validation (REALITY/TLS handshakes fail with 'certificate expired/not yet valid') until the system clock syncs some other way.",
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
      description:
        'Opt-in core behaviour that may still change or be removed between Mihomo releases — expect these keys to occasionally need re-checking against release notes after an update.',
      properties: {
        'ignore-resolve-fail': {
          type: 'boolean',
          description:
            "Keep dialing even when Mihomo's own DNS resolution fails for a domain, letting the proxy's own remote DNS resolve it instead — useful with domains that are only resolvable from the proxy server's network (e.g. behind GFW-style DNS poisoning)."
        },
        'dialer-ip-version': {
          type: 'string',
          enum: ['dual', '4', '6', 'ipv4', 'ipv6', 'ipv4-prefer', 'ipv6-prefer'],
          description:
            'Which IP family Mihomo prefers when a destination has both A and AAAA records. Force "ipv4"/"4" if the router\'s IPv6 uplink is flaky or unavailable, to stop connections stalling on a dead IPv6 route.'
        }
      }
    },
    sniffer: {
      type: 'object',
      description:
        'Peeks at TLS SNI / HTTP Host / QUIC handshakes to recover the real domain of a connection when only an IP address is available for rule matching. This is what makes DOMAIN-based rules work correctly under redir-port/tproxy-port or fake-ip mode, where Mihomo otherwise only sees a bare destination IP.',
      properties: {
        enable: { type: 'boolean' },
        'force-dns-mapping': { type: 'boolean' },
        'parse-pure-ip': { type: 'boolean' },
        'override-destination': {
          type: 'boolean',
          description:
            'Replace the connection target with the sniffed domain before routing, instead of only using it for rule matching. Needed when the destination IP itself is unreachable directly (e.g. it was a fake-ip placeholder).'
        },
        sniff: {
          type: 'object',
          description:
            'Per-protocol sniffing toggles — disable ones you do not need to save router CPU.',
          properties: {
            TLS: { type: 'boolean' },
            HTTP: { type: 'boolean' },
            QUIC: { type: 'boolean' }
          }
        },
        'force-domain': {
          type: 'array',
          items: { type: 'string' },
          description:
            'Always sniff these domains even if global sniffing conditions would otherwise skip them.'
        },
        'skip-domain': {
          type: 'array',
          items: { type: 'string' },
          description:
            'Never sniff these domains — e.g. ones known to break when their destination is rewritten.'
        },
        'port-whitelist': { type: 'array', items: { type: 'integer' } }
      }
    },
    tun: {
      type: 'object',
      description:
        "Mihomo's own virtual network interface for transparent proxying (an alternative to the redir-port/tproxy-port + iptables approach). On this panel XKeen already owns transparent interception via its own iptables/TPROXY rules on Keenetic — enabling tun here on top of that is redundant and the two can fight over the same traffic. Leave tun disabled unless you specifically switched XKeen's interception mode to rely on it.",
      properties: {
        enable: { type: 'boolean' },
        device: { type: 'string', description: 'TUN device name' },
        stack: {
          type: 'string',
          enum: ['system', 'gvisor', 'mixed'],
          description:
            'Userspace network stack backing the TUN device. "gvisor" is the safest portable default; "system" needs kernel TUN support and can be faster but is more sensitive to the router\'s kernel/network stack quirks.'
        },
        'dns-hijack': { type: 'array', items: { type: 'string' } },
        'auto-route': {
          type: 'boolean',
          description:
            "Let Mihomo add its own system routes to send traffic into the TUN device automatically. On Keenetic this conflicts with XKeen's own routing/iptables setup and typically does not work as expected — routes must be managed by XKeen's scripts instead."
        },
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
          description:
            '"fake-ip": Mihomo answers DNS queries with addresses from fake-ip-range and maps them back to the real domain internally — most reliable for DOMAIN-based rules and works well with redir-port/tproxy-port, but breaks apps that hardcode/compare IP addresses. "redir-host": rewrites DNS answers to Mihomo\'s own listen address — simpler but only carries the domain through HTTP Host headers. "normal": no rewriting, DOMAIN rules need sniffer to work at all under transparent proxying.'
        },
        'fake-ip-range': {
          type: 'string',
          pattern: '^([0-9]{1,3}\\.){3}[0-9]{1,3}/\\d{1,2}$',
          description:
            'CIDR pool of placeholder IPs handed out in fake-ip mode. Must not overlap any real subnet in use on the LAN/WAN (default 198.18.0.0/16 is an IANA-reserved benchmarking range, safe to keep unless something else on the network already uses it).'
        },
        'fake-ip-filter': {
          type: 'array',
          items: { type: 'string' },
          description:
            'Domains that always get their real IP instead of a fake one — needed for anything that breaks under fake-ip, e.g. LAN hostnames, NTP, or services that do reverse-IP checks.'
        },
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
      description:
        'Subscription-based alternative to hand-listing servers in proxies: each entry auto-downloads/refreshes a proxy list from a URL (or watches a local file) and can be referenced from proxy-groups via use, instead of a static proxies array.',
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
          'skip-cert-verify': {
            type: 'boolean',
            description:
              'Accept the server TLS certificate without validation. Only for self-signed certs on a server you control — leaving this on generally defeats the point of TLS by allowing a trivial man-in-the-middle.'
          },
          tls: { type: 'boolean' },
          sni: { type: 'string', description: 'TLS Server Name Indication (overrides server)' },
          servername: { type: 'string', description: 'Alias of sni used by some proxy types' },
          alpn: { type: 'array', items: { type: 'string' }, description: 'TLS ALPN protocol list' },
          'client-fingerprint': {
            type: 'string',
            enum: ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random'],
            description:
              "Makes the TLS ClientHello look like a real browser's (uTLS) instead of Go's default, which many DPI systems fingerprint and block specifically because it does not look like normal browser traffic."
          },
          flow: {
            type: 'string',
            enum: ['xtls-rprx-vision', ''],
            description:
              'VLESS-only XTLS flow control. "xtls-rprx-vision" avoids double-encrypting TLS-in-TLS traffic for a real throughput gain, but only applies with tls: true (typically paired with reality-opts) — leave empty for plain/ws/grpc transports where it does not apply.'
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
            description:
              "REALITY: connects with the TLS certificate of a real, unrelated website (no cert/domain of your own needed) so passive DPI sees what looks like a normal HTTPS handshake to that site. Server and public-key/short-id here must match the server's REALITY setup exactly, or the handshake fails outright.",
            properties: {
              'public-key': { type: 'string', description: 'REALITY server public key' },
              'short-id': { type: 'string', description: 'REALITY short ID' }
            },
            required: ['public-key']
          },
          plugin: {
            type: 'string',
            enum: ['obfs', 'v2ray-plugin', 'shadow-tls', 'restls'],
            description:
              'Wraps Shadowsocks traffic in another protocol to disguise it — plain Shadowsocks has a detectable traffic pattern that some DPI blocks outright. shadow-tls/restls make the connection look like real TLS to a cover domain, v2ray-plugin adds WebSocket/TLS, obfs is the simpler legacy option.'
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
          'dialer-proxy': {
            type: 'string',
            description:
              "Name of another proxy in this file to tunnel through first (proxy chaining) — this proxy's connection is made through that one instead of directly, e.g. WireGuard-over-a-proxy to reach a server blocked by IP."
          },
          ports: {
            type: 'string',
            description:
              'Hysteria2/TUIC port-hopping range (e.g. "20000-30000") — the client rotates source ports across this range, which helps evade simple UDP-flow-based blocking of a single fixed port.'
          },
          smux: {
            type: 'object',
            description:
              'Multiplexes several logical streams over one underlying connection, cutting down on repeated TLS/QUIC handshakes for many short-lived requests — mainly useful on high-latency or handshake-expensive links.'
          },
          // WireGuard & AmneziaWG (TMPL-08)
          'private-key': { type: 'string', description: 'WireGuard private key' },
          'public-key': { type: 'string', description: 'WireGuard or Reality public key' },
          'pre-shared-key': { type: 'string', description: 'WireGuard pre-shared key (optional)' },
          ip: { type: 'string', description: 'WireGuard interface local IP' },
          mtu: { type: 'integer', description: 'WireGuard interface MTU' },
          'amnezia-wg-option': {
            type: 'object',
            description:
              "AmneziaWG packet-obfuscation options layered on top of WireGuard. Plain WireGuard has a very recognizable handshake that DPI can fingerprint and block even without decrypting it; these junk-packet/header-magic parameters must match the server's AmneziaWG config exactly (a mismatch fails silently as a connection timeout, not a clear error).",
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
      description:
        'Extra inbound servers Mihomo itself exposes (running it as a socks/vmess/vless/... server), separate from and in addition to port/socks-port/mixed-port. Opposite direction from proxies (which are upstream servers Mihomo connects out to) — use this only if other devices/clients should connect to this router as a proxy server over a specific protocol.',
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
      description:
        'Reusable, auto-updating rule sets (e.g. a domain list for a specific service or region) referenced from rules via RULE-SET,<name>,<policy> instead of pasting hundreds of individual rule lines — keeps the rules list short and lets a set update itself without editing this config.',
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
      description:
        'Traffic routing rules, evaluated top to bottom — the first matching rule wins and later rules are never checked, so more specific rules (a single domain) must come before broader ones (a whole GEOSITE/GEOIP set) that would otherwise shadow them. Usually ends with a catch-all MATCH,<policy> rule.',
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
