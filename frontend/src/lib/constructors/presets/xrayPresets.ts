export interface XrayProtocolOption {
  value: string;
  label: string;
  description?: string;
}

export interface XrayPresetRule {
  type: 'field';
  outboundTag: string;
  ip?: string[];
  domain?: string[];
  port?: string;
  network?: string;
  protocol?: string[];
  inboundTag?: string[];
}

export interface XrayDnsServerObject {
  address: string;
  port?: number;
  tag?: string;
  domains?: string[];
  expectIPs?: string[];
  skipFallback?: boolean;
}

export type XrayDnsServer = string | XrayDnsServerObject;

export interface XrayRoutingPreset {
  id: string;
  nameKey: string;
  descKey: string;
  dnsServers: XrayDnsServer[];
  dnsOverVless: boolean;
  rules: XrayPresetRule[];
}

/**
 * Поддерживаемые outbound-протоколы Xray
 */
export const XRAY_OUTBOUND_PROTOCOLS: XrayProtocolOption[] = [
  { value: 'vless', label: 'VLESS', description: 'Современный легкий протокол без шифрования' },
  { value: 'vmess', label: 'VMess', description: 'Классический протокол V2Ray с шифрованием' },
  { value: 'trojan', label: 'Trojan', description: 'Имитация HTTPS-трафика' },
  {
    value: 'shadowsocks',
    label: 'Shadowsocks',
    description: 'Быстрый зашифрованный SOCKS5 прокси'
  },
  {
    value: 'wireguard',
    label: 'WireGuard / AWG',
    description: 'UDP VPN туннель с поддержкой AmneziaWG'
  },
  {
    value: 'freedom',
    label: 'Freedom (Direct)',
    description: 'Прямое подключение к целевому серверу'
  },
  { value: 'blackhole', label: 'Blackhole (Block)', description: 'Сброс и блокировка трафика' }
];

/**
 * Стандартные шаблоны правил и конфигураций маршрутизации Xray
 */
export const XRAY_DEFAULT_PRESETS: XrayRoutingPreset[] = [
  {
    id: 'selective-routing',
    nameKey: 'xray.preset_selective',
    descKey: 'xray.preset_selective_desc',
    dnsOverVless: true,
    dnsServers: [
      '1.1.1.1',
      {
        address: '8.8.8.8',
        port: 53,
        tag: 'dns-in-ytb',
        domains: ['geosite:youtube', 'geosite:google'],
        skipFallback: true
      },
      {
        address: '77.88.8.8',
        port: 53,
        tag: 'dns-in-direct',
        domains: ['geosite:tld-ru'],
        skipFallback: false
      }
    ],
    rules: [
      {
        type: 'field',
        outboundTag: 'direct',
        ip: ['geoip:private']
      },
      {
        type: 'field',
        outboundTag: 'block',
        domain: ['geosite:category-ads-all']
      },
      {
        type: 'field',
        outboundTag: 'PROXY_TAG',
        network: 'tcp,udp'
      }
    ]
  },
  {
    id: 'all-proxy-routing',
    nameKey: 'xray.preset_all_proxy',
    descKey: 'xray.preset_all_proxy_desc',
    dnsOverVless: true,
    dnsServers: ['1.1.1.1', '8.8.8.8'],
    rules: [
      {
        type: 'field',
        outboundTag: 'direct',
        ip: ['geoip:private']
      },
      {
        type: 'field',
        outboundTag: 'PROXY_TAG',
        network: 'tcp,udp'
      }
    ]
  },
  {
    id: 'selective-no-quic',
    nameKey: 'xray.preset_selective_no_quic',
    descKey: 'xray.preset_selective_no_quic_desc',
    dnsOverVless: true,
    dnsServers: [
      '1.1.1.1',
      {
        address: '8.8.8.8',
        port: 53,
        tag: 'dns-in-ytb',
        domains: ['geosite:youtube', 'geosite:google'],
        skipFallback: true
      }
    ],
    rules: [
      {
        type: 'field',
        outboundTag: 'block',
        network: 'udp',
        port: '443'
      },
      {
        type: 'field',
        outboundTag: 'direct',
        ip: ['geoip:private']
      },
      {
        type: 'field',
        outboundTag: 'block',
        domain: ['geosite:category-ads-all']
      },
      {
        type: 'field',
        outboundTag: 'PROXY_TAG',
        network: 'tcp,udp'
      }
    ]
  },
  {
    id: 'only-blocked-routing',
    nameKey: 'xray.preset_blocked_only',
    descKey: 'xray.preset_blocked_only_desc',
    dnsOverVless: false,
    dnsServers: [
      '1.1.1.1',
      {
        address: '8.8.8.8',
        port: 53,
        tag: 'dns-in-ytb',
        domains: ['geosite:category-anticensorship', 'geosite:refilter'],
        skipFallback: true
      }
    ],
    rules: [
      {
        type: 'field',
        outboundTag: 'direct',
        ip: ['geoip:private']
      },
      {
        type: 'field',
        outboundTag: 'PROXY_TAG',
        domain: ['geosite:category-anticensorship', 'geosite:refilter']
      },
      {
        type: 'field',
        outboundTag: 'direct',
        port: '0-65535'
      }
    ]
  },
  {
    id: 'minimal-routing',
    nameKey: 'xray.preset_minimal',
    descKey: 'xray.preset_minimal_desc',
    dnsOverVless: false,
    dnsServers: ['1.1.1.1', '8.8.8.8'],
    rules: [
      {
        type: 'field',
        outboundTag: 'direct',
        ip: ['geoip:private']
      },
      {
        type: 'field',
        outboundTag: 'direct',
        port: '0-65535'
      }
    ]
  }
];

export const XRAY_PRESETS = {
  protocols: XRAY_OUTBOUND_PROTOCOLS,
  presets: XRAY_DEFAULT_PRESETS
};
