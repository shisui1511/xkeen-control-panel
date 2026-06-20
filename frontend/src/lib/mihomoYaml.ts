export interface Proxy {
  id: string;
  name: string;
  type: string;
  server: string;
  port: number;
  uuid?: string;
  flow?: string;
  publicKey?: string;
  shortId?: string;
  servername?: string;
  password?: string;
  sni?: string;
  skipCertVerify?: boolean;
  obfsType?: 'none' | 'simple';
  obfsPassword?: string;
  congestion?: string;
  cipher?: string;
  network?: string;
  wsPath?: string;
  tls?: boolean;
  fingerprint?: string;
  alterID?: number;
}

export interface ProxyGroup {
  id: string;
  name: string;
  type: string;
  proxies: string[];
  includeAll?: boolean;
  url?: string;
  interval?: number;
  excludeFilter?: string;
  icon?: string;
  enabled?: boolean;
  hidden?: boolean;
  tolerance?: number;
  maxFailedTimes?: number;
}

export interface Rule {
  id: string;
  type: string;
  value: string;
  outbound: string;
}

export interface DNSConfig {
  enabled: boolean;
  nameservers: string[];
  fallback: string[];
  enhancedMode: 'fake-ip' | 'redir-host';
  fakeIPRange: string;
}

export interface TUNConfig {
  enabled: boolean;
  stack: 'system' | 'gvisor' | 'mixed';
  autoRoute: boolean;
  autoDetectInterface: boolean;
  dnsHijack: string[];
}

export interface SnifferConfig {
  enabled: boolean;
  sniffHttp: boolean;
  sniffTls: boolean;
  sniffQuic: boolean;
}

export interface RuleProvider {
  name: string;
  url: string;
  behavior: string;
  outbound: string;
  format: string;
  payload?: string[];
}

export const ZKEEN_RULE_PROVIDERS: RuleProvider[] = [
  {
    name: 'adlist@domain',
    url: 'https://github.com/zxc-rv/ad-filter/releases/latest/download/adlist.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'REJECT'
  },
  {
    name: 'category-ai@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/category-ai-!cn.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'AI'
  },
  {
    name: 'steam@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/steam.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Steam'
  },
  {
    name: 'spotify@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/spotify.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Spotify'
  },
  {
    name: 'speedtest@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/speedtest.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Speedtest'
  },
  {
    name: 'reddit@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/reddit.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Reddit'
  },
  {
    name: 'twitch@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/twitch.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Twitch'
  },
  {
    name: 'twitter@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/twitter.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Twitter'
  },
  {
    name: 'meta@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/meta.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Meta'
  },
  {
    name: 'discord@classical',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/classical/discord.txt',
    behavior: 'classical',
    format: 'text',
    outbound: 'Discord'
  },
  {
    name: 'refilter@domain',
    url: 'https://raw.githubusercontent.com/1andrevich/Re-filter-lists/release/refilter_domains.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Заблок. сервисы'
  },
  {
    name: 'telegram@ipcidr',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geoip/telegram.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'Telegram'
  },
  {
    name: 'github@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/github.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'GitHub'
  },
  {
    name: 'private@ip',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geoip/private.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'DIRECT'
  },
  {
    name: 'quic@inline',
    url: '',
    behavior: 'classical',
    format: 'inline',
    outbound: 'QUIC',
    payload: ['AND,((DST-PORT,443),(NETWORK,UDP))']
  },
  {
    name: 'netbios@inline',
    url: '',
    behavior: 'classical',
    format: 'inline',
    outbound: 'REJECT',
    payload: [
      'AND,((DST-PORT,135),(NETWORK,UDP))',
      'AND,((DST-PORT,137),(NETWORK,UDP))',
      'AND,((DST-PORT,138),(NETWORK,UDP))',
      'AND,((DST-PORT,139),(NETWORK,UDP))'
    ]
  }
];

export const RULE_PROVIDERS: Record<string, RuleProvider[]> = {
  zkeen: ZKEEN_RULE_PROVIDERS
};

export function yamlSafeString(v: string | number | boolean): string {
  if (typeof v !== 'string') return String(v);
  const escaped = v
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\n/g, '\\n')
    .replace(/\t/g, '\\t');
  return `"${escaped}"`;
}

export function sanitizeUrl(url: string): string {
  if (!url) return '';
  const trimmed = url.trim();
  if (!trimmed.startsWith('http://') && !trimmed.startsWith('https://')) {
    return '';
  }
  try {
    new URL(trimmed);
    return trimmed;
  } catch {
    return '';
  }
}

export function unquote(str: string): string {
  str = str.trim();
  if (str.startsWith('"')) {
    const closingIdx = str.indexOf('"', 1);
    if (closingIdx !== -1) {
      return str.slice(1, closingIdx);
    }
  } else if (str.startsWith("'")) {
    const closingIdx = str.indexOf("'", 1);
    if (closingIdx !== -1) {
      return str.slice(1, closingIdx);
    }
  }
  return str;
}

export function findTopLevelSection(lines: string[], sectionName: string) {
  const header = sectionName + ':';
  let start = -1;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trimEnd();
    if (trimmed === header || trimmed.startsWith(header + ' ') || trimmed.startsWith(header + '\t')) {
      if (line.length === line.trimStart().length) {
        start = i;
        break;
      }
    }
  }
  if (start === -1) {
    return { start: -1, end: -1 };
  }

  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();
    if (trimmed === '' || trimmed.startsWith('#')) {
      continue;
    }
    const raw = line.trimStart();
    if (line.length === raw.length && !raw.startsWith('- ')) {
      end = i;
      break;
    }
  }
  return { start, end };
}

export function extractSection(yamlText: string, sectionName: string): string {
  const lines = yamlText.split('\n');
  let start = -1;
  const header = sectionName + ':';
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trimEnd();
    if (
      (trimmed === header || trimmed.startsWith(header + ' ') || trimmed.startsWith(header + '\t')) &&
      line.length === line.trimStart().length
    ) {
      start = i;
      break;
    }
  }
  if (start === -1) return '';
  const resultLines: string[] = [];
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i];
    if (
      line.trim() !== '' &&
      !line.startsWith(' ') &&
      !line.startsWith('\t') &&
      !line.startsWith('#')
    ) {
      break;
    }
    resultLines.push(line);
  }
  return resultLines.join('\n').trimEnd();
}

export function replaceMihomoTopLevelSection(content: string, sectionName: string, newContent: string): string {
  const lines = content.split('\n');
  const { start, end } = findTopLevelSection(lines, sectionName);
  const newLines = newContent.trim() !== '' ? newContent.trimEnd().split('\n') : [];

  if (start === -1) {
    if (newLines.length === 0) return content;
    let appended = `\n${sectionName}:\n` + newLines.join('\n') + '\n';
    if (content.endsWith('\n')) {
      return content + appended.substring(1);
    }
    return content + appended;
  }

  const out: string[] = [];
  for (let i = 0; i < start; i++) {
    out.push(lines[i]);
  }
  if (newLines.length > 0) {
    out.push(`${sectionName}:`);
    for (const nl of newLines) {
      out.push(nl);
    }
  }
  for (let i = end; i < lines.length; i++) {
    out.push(lines[i]);
  }
  return out.join('\n');
}

export interface MihomoConfigState {
  proxies: Proxy[];
  groups: ProxyGroup[];
  rules: Rule[];
  dns: DNSConfig;
  tun: TUNConfig;
  sniffer: SnifferConfig;
  activeRuleProvider: string;
  selectedMetaRuleSets: Map<string, string>;
  preservedKeys: string[];
  existingTproxyPort: number | null;
  existingRedirPort: number | null;
  subscriptions: any[];
  capabilities?: any;
  hasZkeenGeodata?: boolean;
  ruleProviders?: RuleProvider[];
}

export function generateYAML(state: MihomoConfigState): string {
  const lines: string[] = [];

  // external-controller must be first field (required for Clash API on port 9090)
  lines.push('external-controller: 0.0.0.0:9090');
  lines.push('');

  // System ports from XKeen (preserve existing values, fall back to defaults)
  lines.push(`tproxy-port: ${state.existingTproxyPort ?? 5001}`);
  lines.push(`redir-port: ${state.existingRedirPort ?? 5000}`);
  lines.push('');

  // Proxy-providers (if we have subscriptions)
  if (state.subscriptions.length > 0) {
    lines.push('proxy-providers:');
    for (const [i, sub] of state.subscriptions.entries()) {
      let path = '';
      try {
        if (sub.url) {
          const parsed = new URL(sub.url);
          path = parsed.pathname || '';
        }
      } catch (e) {}
      path = path.replace(/\/+$/, '');
      let urlBase = path ? path.split('/').pop() : '';
      let providerName = (sub.name || urlBase || `provider-${i}`)
        .replace(/[^a-zA-Z0-9-]/g, '-')
        .replace(/-+/g, '-')
        .replace(/^-|-$/g, '')
        .toLowerCase();
      if (!providerName) {
        providerName = sub.id || `provider-${i}`;
      }
      lines.push(`  ${providerName}:`);
      lines.push(`    type: http`);
      lines.push(`    path: ./providers/${providerName}.yaml`);
      lines.push(`    url: ${yamlSafeString(sub.url)}`);
      lines.push(`    interval: ${sub.interval * 3600 || 86400}`);
      
      // Custom headers for User-Agent and x-hwid
      const mihomoVersion = state.capabilities?.kernels?.mihomo?.version || '1.18.10';
      const ua = `mihomo/${mihomoVersion}`;
      const subHwid = sub.hwid_token || state.capabilities?.global_hwid || '';
      
      lines.push(`    header:`);
      lines.push(`      User-Agent:`);
      lines.push(`        - ${yamlSafeString(ua)}`);
      if (subHwid) {
        lines.push(`      x-hwid:`);
        lines.push(`        - ${yamlSafeString(subHwid)}`);
      }
      
      lines.push(`    health-check:`);
      lines.push(`      enable: true`);
      lines.push(`      url: http://www.gstatic.com/generate_204`);
      lines.push(`      interval: 300`);
    }
    lines.push('');
  }

  // Rule-providers (if selected)
  lines.push('rule-providers:');
  // Always inject quic@inline and netbios@inline
  lines.push('  quic@inline:');
  lines.push('    type: inline');
  lines.push('    behavior: classical');
  lines.push('    payload:');
  lines.push('      - "AND,((DST-PORT,443),(NETWORK,UDP))"');
  lines.push('  netbios@inline:');
  lines.push('    type: inline');
  lines.push('    behavior: classical');
  lines.push('    payload:');
  lines.push('      - "AND,((DST-PORT,135),(NETWORK,UDP))"');
  lines.push('      - "AND,((DST-PORT,137),(NETWORK,UDP))"');
  lines.push('      - "AND,((DST-PORT,138),(NETWORK,UDP))"');
  lines.push('      - "AND,((DST-PORT,139),(NETWORK,UDP))"');

  const metaBaseUrl = 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo';
  const buildMetaRuleSetUrl = (id: string, type: 'geosite' | 'geoip') => `${metaBaseUrl}/${type}/${id}.mrs`;

  if (state.activeRuleProvider === 'metacubex' && state.selectedMetaRuleSets.size > 0) {
    for (const [key, outbound] of state.selectedMetaRuleSets) {
      const [id, type] = key.split('|') as [string, 'geosite' | 'geoip'];
      const behavior = type === 'geoip' ? 'ipcidr' : 'domain';
      lines.push(`  ${type}-${id.replace(/[^a-z0-9-]/g, '-')}:`);
      lines.push(`    type: http`);
      lines.push(`    format: mrs`);
      lines.push(`    behavior: ${behavior}`);
      lines.push(`    url: ${yamlSafeString(sanitizeUrl(buildMetaRuleSetUrl(id, type)))}`);
      lines.push(`    interval: 86400`);
    }
  } else if (state.activeRuleProvider !== 'none' && state.activeRuleProvider !== 'metacubex') {
    const providers = state.activeRuleProvider === 'zkeen' ? (state.ruleProviders || ZKEEN_RULE_PROVIDERS) : RULE_PROVIDERS[state.activeRuleProvider];
    if (providers && providers.length > 0) {
      for (const rp of providers) {
        if (rp.name === 'quic@inline' || rp.name === 'netbios@inline') {
          continue;
        }
        lines.push(`  ${rp.name}:`);
        if (rp.format === 'inline') {
          lines.push(`    type: inline`);
          lines.push(`    behavior: ${rp.behavior}`);
          lines.push(`    payload:`);
          if (rp.payload) {
            for (const item of rp.payload) {
              lines.push(`      - ${yamlSafeString(item)}`);
            }
          }
        } else {
          lines.push(`    type: http`);
          if (rp.format) {
            lines.push(`    format: ${rp.format}`);
          }
          lines.push(`    behavior: ${rp.behavior}`);
          lines.push(`    url: ${yamlSafeString(sanitizeUrl(rp.url))}`);
          lines.push(`    interval: 86400`);
        }
      }
    }
  }
  lines.push('');

  // Proxies
  if (state.proxies.length > 0) {
    lines.push('proxies:');
    for (const p of state.proxies) {
      lines.push(`  - name: ${yamlSafeString(p.name)}`);
      lines.push(`    type: ${p.type}`);
      lines.push(`    server: ${yamlSafeString(p.server)}`);
      lines.push(`    port: ${p.port}`);

      if (p.type === 'vless') {
        lines.push(`    uuid: ${p.uuid ?? ''}`);
        if (p.flow) lines.push(`    flow: ${p.flow}`);
        lines.push(`    tls: ${p.tls ?? true}`);
        if (p.publicKey) {
          lines.push(`    reality-opts:`);
          lines.push(`      public-key: ${yamlSafeString(p.publicKey)}`);
          lines.push(`      short-id: ${yamlSafeString(p.shortId || '')}`);
        }
        lines.push(`    client-fingerprint: ${p.fingerprint || 'chrome'}`);
        if (p.servername) lines.push(`    servername: ${yamlSafeString(p.servername)}`);
      } else if (p.type === 'hysteria2') {
        lines.push(`    password: ${yamlSafeString(p.password || '')}`);
        if (p.sni) lines.push(`    sni: ${yamlSafeString(p.sni)}`);
        if (p.skipCertVerify) lines.push(`    skip-cert-verify: true`);
        if (p.obfsType && p.obfsType !== 'none') {
          lines.push(`    obfs:`);
          lines.push(`      type: ${p.obfsType}`);
          if (p.obfsPassword) lines.push(`      password: ${yamlSafeString(p.obfsPassword)}`);
        }
      } else if (p.type === 'tuic') {
        lines.push(`    uuid: ${p.uuid ?? ''}`);
        lines.push(`    password: ${yamlSafeString(p.password || '')}`);
        lines.push(`    congestion-controller: ${p.congestion || 'bbr'}`);
        if (p.sni) lines.push(`    sni: ${yamlSafeString(p.sni)}`);
      } else if (p.type === 'ss') {
        lines.push(`    cipher: ${p.cipher || 'aes-256-gcm'}`);
        lines.push(`    password: ${yamlSafeString(p.password || '')}`);
      } else if (p.type === 'vmess') {
        lines.push(`    uuid: ${p.uuid ?? ''}`);
        lines.push(`    alterId: ${p.alterID ?? 0}`);
        lines.push(`    cipher: ${p.cipher || 'auto'}`);
        lines.push(`    tls: ${p.tls}`);
        lines.push(`    network: ${p.network || 'ws'}`);
        if (p.network === 'ws') {
          lines.push(`    ws-opts:`);
          lines.push(`      path: ${yamlSafeString(p.wsPath || '/')}`);
        }
        if (p.tls && p.sni) lines.push(`    servername: ${yamlSafeString(p.sni)}`);
      }
    }
    lines.push('');
  }

  // Helper to check if a target outbound group is enabled
  const isOutboundEnabled = (outbound: string) => {
    if (state.activeRuleProvider === 'zkeen') {
      const primaryOutbound = outbound.split(',')[0].trim();
      const g = state.groups.find((x) => x.name === primaryOutbound);
      if (g && g.enabled === false) return false;
    }
    return true;
  };

  // Proxy groups
  if (state.groups.length > 0) {
    lines.push('proxy-groups:');
    for (const g of state.groups) {
      if (state.activeRuleProvider === 'zkeen' && g.enabled === false) {
        continue;
      }
      lines.push(`  - name: ${yamlSafeString(g.name)}`);
      lines.push(`    type: ${g.type}`);
      if (g.icon) {
        lines.push(`    icon: ${yamlSafeString(g.icon)}`);
      }
      if (g.excludeFilter) {
        lines.push(`    exclude-filter: ${yamlSafeString(g.excludeFilter)}`);
      }
      if (g.includeAll === true) {
        lines.push(`    include-all: true`);
      }
      if (g.proxies.length > 0) {
        lines.push(`    proxies:`);
        for (const p of g.proxies) lines.push(`      - ${yamlSafeString(p)}`);
      }
      if (g.type !== 'select') {
        lines.push(`    url: ${g.url || 'https://www.gstatic.com/generate_204'}`);
        lines.push(`    interval: ${g.interval || 300}`);
        if (g.hidden === true) {
          lines.push(`    hidden: true`);
        }
        if (g.tolerance !== undefined && g.tolerance > 0) {
          lines.push(`    tolerance: ${g.tolerance}`);
        }
        if (g.maxFailedTimes !== undefined) {
          lines.push(`    max-failed-times: ${g.maxFailedTimes}`);
        }
      }
    }
    lines.push('');
  }

  // Rules
  const hasRules =
    state.rules.length > 0 ||
    state.activeRuleProvider === 'zkeen' ||
    (state.activeRuleProvider === 'metacubex' && state.selectedMetaRuleSets.size > 0);
  if (hasRules) {
    lines.push('rules:');
    if (state.activeRuleProvider === 'zkeen') {
      const zkeenRules = [
        { type: 'RULE-SET', val: 'adlist@domain', outbound: 'REJECT' },
        { type: 'RULE-SET', val: 'quic@inline', outbound: 'REJECT' },
        { type: 'RULE-SET', val: 'netbios@inline', outbound: 'REJECT' },
        {
          type: 'OR',
          val: '((DOMAIN-SUFFIX,gql.twitch.tv),(DOMAIN-SUFFIX,usher.ttvnw.net)),Заблок. сервисы',
          outbound: 'Заблок. сервисы'
        },
        { type: 'RULE-SET', val: 'category-ai@domain', outbound: 'AI' },
        { type: 'RULE-SET', val: 'steam@domain', outbound: 'Steam' },
        { type: 'RULE-SET', val: 'spotify@domain', outbound: 'Spotify' },
        { type: 'RULE-SET', val: 'reddit@domain', outbound: 'Reddit' },
        { type: 'RULE-SET', val: 'twitch@domain', outbound: 'Twitch' },
        { type: 'RULE-SET', val: 'twitter@domain', outbound: 'Twitter' },
        { type: 'RULE-SET', val: 'discord@classical', outbound: 'Discord' },
        { type: 'RULE-SET', val: 'speedtest@domain', outbound: 'Speedtest' },
        { type: 'GEOSITE', val: 'YOUTUBE', outbound: 'YouTube' },
        { type: 'GEOIP', val: 'YOUTUBE', outbound: 'YouTube' },
        { type: 'RULE-SET', val: 'meta@domain', outbound: 'Meta' },
        { type: 'GEOIP', val: 'META', outbound: 'Meta' },
        { type: 'GEOIP', val: 'AKAMAI', outbound: 'CDN' },
        { type: 'GEOIP', val: 'AMAZON', outbound: 'CDN' },
        { type: 'GEOIP', val: 'CDN77', outbound: 'CDN' },
        { type: 'GEOIP', val: 'CLOUDFLARE', outbound: 'CDN' },
        { type: 'GEOIP', val: 'COLOCROSSING', outbound: 'CDN' },
        { type: 'GEOIP', val: 'CONTABO', outbound: 'CDN' },
        { type: 'GEOIP', val: 'DIGITALOCEAN', outbound: 'CDN' },
        { type: 'GEOIP', val: 'FASTLY', outbound: 'CDN' },
        { type: 'GEOIP', val: 'GCORE', outbound: 'CDN' },
        { type: 'GEOIP', val: 'GOOGLE', outbound: 'Google' },
        { type: 'GEOIP', val: 'HETZNER', outbound: 'CDN' },
        { type: 'GEOIP', val: 'LINODE', outbound: 'CDN' },
        { type: 'GEOIP', val: 'MEGA', outbound: 'CDN' },
        { type: 'GEOIP', val: 'ORACLE', outbound: 'CDN' },
        { type: 'GEOIP', val: 'OVH', outbound: 'CDN' },
        { type: 'GEOIP', val: 'SCALEWAY', outbound: 'CDN' },
        { type: 'GEOIP', val: 'TELEGRAM', outbound: 'Telegram' },
        { type: 'GEOIP', val: 'VODAFONE', outbound: 'CDN' },
        { type: 'GEOIP', val: 'VULTR', outbound: 'CDN' },
        { type: 'RULE-SET', val: 'refilter@domain', outbound: 'Заблок. сервисы' },
        ...(state.hasZkeenGeodata
          ? [
              { type: 'GEOSITE', val: 'DOMAINS', outbound: 'Заблок. сервисы' },
              { type: 'GEOSITE', val: 'OTHER', outbound: 'Заблок. сервисы' },
              { type: 'GEOSITE', val: 'POLITIC', outbound: 'Заблок. сервисы' }
            ]
          : []),
        { type: 'RULE-SET', val: 'github@domain', outbound: 'GitHub' }
      ];

      for (const r of zkeenRules) {
        if (isOutboundEnabled(r.outbound)) {
          if (r.type === 'OR') {
            lines.push(`  - OR,${r.val}`);
          } else {
            lines.push(`  - ${r.type},${r.val},${r.outbound}`);
          }
        }
      }

      // Custom user rules (except MATCH which goes last)
      for (const r of state.rules) {
        if (isOutboundEnabled(r.outbound)) {
          if (r.type === 'MATCH') continue;
          lines.push(`  - ${r.type},${r.value},${r.outbound}`);
        }
      }

      if (isOutboundEnabled('DIRECT')) {
        lines.push('  - RULE-SET,private@ip,DIRECT');
      }
      lines.push('  - MATCH,DIRECT');
    } else {
      lines.push('  - RULE-SET,quic@inline,REJECT');
      lines.push('  - RULE-SET,netbios@inline,REJECT');

      // Rule-set entries from rule-providers (before user rules, before MATCH)
      if (state.activeRuleProvider === 'metacubex') {
        for (const [key, outbound] of state.selectedMetaRuleSets) {
          const [id, type] = key.split('|') as [string, 'geosite' | 'geoip'];
          lines.push(`  - RULE-SET,${type}-${id.replace(/[^a-z0-9-]/g, '-')},${outbound}`);
        }
      } else if (state.activeRuleProvider !== 'none') {
        const providers = state.activeRuleProvider === 'zkeen' ? (state.ruleProviders || ZKEEN_RULE_PROVIDERS) : RULE_PROVIDERS[state.activeRuleProvider];
        if (providers) {
          for (const rp of providers) {
            if (rp.name === 'quic@inline' || rp.name === 'netbios@inline') {
              continue;
            }
            lines.push(`  - RULE-SET,${rp.name},${rp.outbound}`);
          }
        }
      }
      for (const r of state.rules) {
        if (r.type === 'MATCH') {
          lines.push(`  - MATCH,${r.outbound}`);
        } else {
          lines.push(`  - ${r.type},${r.value},${r.outbound}`);
        }
      }
      // If only rule-providers active but no manual rules, add a default MATCH
      if (
        state.rules.length === 0 &&
        state.activeRuleProvider === 'metacubex' &&
        state.selectedMetaRuleSets.size > 0
      ) {
        lines.push(`  - MATCH,DIRECT`);
      }
    }
    lines.push('');
  }

  // Sniffer
  lines.push('sniffer:');
  lines.push(`  enable: ${state.sniffer.enabled}`);
  if (state.sniffer.enabled) {
    lines.push('  sniff:');
    if (state.sniffer.sniffHttp) lines.push('    HTTP: { ports: [80, 8080] }');
    if (state.sniffer.sniffTls) lines.push('    TLS: { ports: [443, 8443] }');
    if (state.sniffer.sniffQuic) lines.push('    QUIC: { ports: [443, 8443] }');
    lines.push('  skip-dst-address: [rule-set:telegram@ipcidr]');
  }
  lines.push('');

  // DNS
  lines.push('dns:');
  lines.push(`  enable: ${state.dns.enabled}`);
  if (state.dns.enabled) {
    lines.push(`  enhanced-mode: ${state.dns.enhancedMode}`);
    if (state.dns.enhancedMode === 'fake-ip') lines.push(`  fake-ip-range: ${state.dns.fakeIPRange}`);
    lines.push(`  nameserver:`);
    for (const ns of state.dns.nameservers) lines.push(`    - ${yamlSafeString(ns)}`);
    if (state.dns.fallback.length > 0) {
      lines.push(`  fallback:`);
      for (const fb of state.dns.fallback) lines.push(`    - ${yamlSafeString(fb)}`);
    }
  }
  lines.push('');

  // TUN
  lines.push('tun:');
  lines.push(`  enable: ${state.tun.enabled}`);
  if (state.tun.enabled) {
    lines.push(`  stack: ${state.tun.stack}`);
    lines.push(`  auto-route: ${state.tun.autoRoute}`);
    lines.push(`  auto-detect-interface: ${state.tun.autoDetectInterface}`);
    if (state.tun.dnsHijack.length > 0) {
      lines.push(`  dns-hijack:`);
      for (const d of state.tun.dnsHijack) lines.push(`    - ${yamlSafeString(d)}`);
    }
  }
  lines.push('');

  return lines.join('\n').trimEnd();
}

export function populateMihomoFromYAML(text: string): ParsedMihomoConfig {
  const parsed: ParsedMihomoConfig = {
    proxies: [],
    groups: [],
    rules: [],
    dns: {
      enabled: false,
      nameservers: ['https://doh.pub/dns-query', '223.5.5.5'],
      fallback: ['https://8.8.8.8/dns-query', '1.1.1.1'],
      enhancedMode: 'fake-ip',
      fakeIPRange: '198.18.0.1/16'
    },
    tun: {
      enabled: false,
      stack: 'mixed',
      autoRoute: true,
      autoDetectInterface: true,
      dnsHijack: ['any:53']
    },
    sniffer: {
      enabled: false,
      sniffHttp: false,
      sniffTls: false,
      sniffQuic: false
    },
    activeRuleProvider: 'none',
    selectedMetaRuleSets: new Map(),
    preservedKeys: [],
    existingTproxyPort: null,
    existingRedirPort: null
  };

  if (!text || text.trim() === '') {
    return parsed;
  }

  try {
    const lines = text.split('\n');
    let inGroups = false;
    let inProxies = false;
    let inDNS = false;
    let inTUN = false;
    let inRules = false;
    let inRuleProviders = false;
    let inNameservers = false;
    let inFallback = false;
    let inDnsHijack = false;
    let inSniffer = false;

    let currentGroup: any = null;
    let currentProxy: any = null;
    let inProxiesList = false;

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      const trimmed = line.trim();

      // Detect top-level sections
      if (/^[a-zA-Z0-9_-]+:/.test(line) && !line.startsWith(' ') && !line.startsWith('-')) {
        const match = line.match(/^([a-zA-Z0-9_-]+):/);
        if (match) {
          const sec = match[1];
          inGroups = sec === 'proxy-groups';
          inProxies = sec === 'proxies';
          inDNS = sec === 'dns';
          inTUN = sec === 'tun';
          inRules = sec === 'rules';
          inRuleProviders = sec === 'rule-providers';
          inSniffer = sec === 'sniffer';

          if (
            sec !== 'proxy-groups' &&
            sec !== 'proxies' &&
            sec !== 'dns' &&
            sec !== 'tun' &&
            sec !== 'rules' &&
            sec !== 'rule-providers' &&
            sec !== 'sniffer' &&
            sec !== 'tproxy-port' &&
            sec !== 'redir-port'
          ) {
            if (!parsed.preservedKeys.includes(sec)) {
              parsed.preservedKeys = [...parsed.preservedKeys, sec];
            }
          }

          if (sec === 'tproxy-port') {
            const valMatch = line.match(/tproxy-port:\s*["']?(\d+)["']?/);
            if (valMatch) parsed.existingTproxyPort = parseInt(valMatch[1], 10);
          } else if (sec === 'redir-port') {
            const valMatch = line.match(/redir-port:\s*["']?(\d+)["']?/);
            if (valMatch) parsed.existingRedirPort = parseInt(valMatch[1], 10);
          }
        }

        continue;
      }

      if (inGroups) {
        if (line.startsWith('  -') || line.startsWith(' -') || trimmed.startsWith('-')) {
          if (currentGroup) {
            parsed.groups.push(currentGroup);
          }
          currentGroup = {
            id: crypto.randomUUID(),
            name: '',
            type: 'select',
            proxies: [],
            includeAll: false
          };
          inProxiesList = false;

          const nameMatch = trimmed.match(/^-\s+name:\s*(.+)$/);
          if (nameMatch) {
            currentGroup.name = unquote(nameMatch[1]);
          }
          continue;
        }

        if (!currentGroup) continue;

        const nameMatch = trimmed.match(/^name:\s*(.+)$/);
        if (nameMatch) {
          currentGroup.name = unquote(nameMatch[1]);
          continue;
        }
        const typeMatch = trimmed.match(/^type:\s*(.+)$/);
        if (typeMatch) {
          currentGroup.type = unquote(typeMatch[1]);
          continue;
        }
        const includeAllMatch = trimmed.match(/^include-all:\s*(.+)$/);
        if (includeAllMatch) {
          currentGroup.includeAll = unquote(includeAllMatch[1]) === 'true';
          continue;
        }
        const urlMatch = trimmed.match(/^url:\s*(.+)$/);
        if (urlMatch) {
          currentGroup.url = unquote(urlMatch[1]);
          continue;
        }
        const intervalMatch = trimmed.match(/^interval:\s*(.+)$/);
        if (intervalMatch) {
          currentGroup.interval = parseInt(unquote(intervalMatch[1])) || 300;
          continue;
        }
        const excludeFilterMatch = trimmed.match(/^exclude-filter:\s*(.+)$/);
        if (excludeFilterMatch) {
          currentGroup.excludeFilter = unquote(excludeFilterMatch[1]);
          continue;
        }
        const iconMatch = trimmed.match(/^icon:\s*(.+)$/);
        if (iconMatch) {
          currentGroup.icon = unquote(iconMatch[1]);
          continue;
        }
        const hiddenMatch = trimmed.match(/^hidden:\s*(.+)$/);
        if (hiddenMatch) {
          currentGroup.hidden = unquote(hiddenMatch[1]) === 'true';
          continue;
        }
        const toleranceMatch = trimmed.match(/^tolerance:\s*(.+)$/);
        if (toleranceMatch) {
          currentGroup.tolerance = parseInt(unquote(toleranceMatch[1])) || undefined;
          continue;
        }
        const maxFailedTimesMatch = trimmed.match(/^max-failed-times:\s*(.+)$/);
        if (maxFailedTimesMatch) {
          currentGroup.maxFailedTimes = parseInt(unquote(maxFailedTimesMatch[1])) || undefined;
          continue;
        }

        if (trimmed.startsWith('proxies:')) {
          inProxiesList = true;
          const inlineMatch = trimmed.match(/^proxies:\s*\[(.*)\]$/);
          if (inlineMatch) {
            currentGroup.proxies = inlineMatch[1]
              .split(',')
              .map((p) => unquote(p.trim()))
              .filter(Boolean);
            inProxiesList = false;
          }
          continue;
        }

        if (inProxiesList && (trimmed.startsWith('-') || trimmed.startsWith('  -') || line.startsWith('    -'))) {
          const proxyItemMatch = trimmed.match(/^-\s*(.+)$/);
          if (proxyItemMatch) {
            currentGroup.proxies.push(unquote(proxyItemMatch[1]));
          }
        }
      }

      if (inProxies) {
        if (line.startsWith('  -') || line.startsWith(' -') || trimmed.startsWith('-')) {
          if (currentProxy) {
            parsed.proxies.push(currentProxy);
          }
          currentProxy = {
            id: crypto.randomUUID(),
            name: '',
            type: 'vless',
            server: '',
            port: 443
          };

          const nameMatch = trimmed.match(/^-\s+name:\s*(.+)$/);
          if (nameMatch) {
            currentProxy.name = unquote(nameMatch[1]);
          }
          continue;
        }

        if (!currentProxy) continue;

        const nameMatch = trimmed.match(/^name:\s*(.+)$/);
        if (nameMatch) {
          currentProxy.name = unquote(nameMatch[1]);
          continue;
        }
        const typeMatch = trimmed.match(/^type:\s*(.+)$/);
        if (typeMatch) {
          currentProxy.type = unquote(typeMatch[1]);
          continue;
        }
        const serverMatch = trimmed.match(/^server:\s*(.+)$/);
        if (serverMatch) {
          currentProxy.server = unquote(serverMatch[1]);
          continue;
        }
        const portMatch = trimmed.match(/^port:\s*(.+)$/);
        if (portMatch) {
          currentProxy.port = parseInt(unquote(portMatch[1])) || 443;
          continue;
        }
        const uuidMatch = trimmed.match(/^uuid:\s*(.+)$/);
        if (uuidMatch) {
          currentProxy.uuid = unquote(uuidMatch[1]);
          continue;
        }
        const passwordMatch = trimmed.match(/^password:\s*(.+)$/);
        if (passwordMatch) {
          currentProxy.password = unquote(passwordMatch[1]);
          continue;
        }
        const flowMatch = trimmed.match(/^flow:\s*(.+)$/);
        if (flowMatch) {
          currentProxy.flow = unquote(flowMatch[1]);
          continue;
        }
        const publicKeyMatch = trimmed.match(/^public-key:\s*(.+)$/);
        if (publicKeyMatch) {
          currentProxy.publicKey = unquote(publicKeyMatch[1]);
          continue;
        }
        const shortIdMatch = trimmed.match(/^short-id:\s*(.+)$/);
        if (shortIdMatch) {
          currentProxy.shortId = unquote(shortIdMatch[1]);
          continue;
        }
        const servernameMatch = trimmed.match(/^servername:\s*(.+)$/);
        if (servernameMatch) {
          currentProxy.servername = unquote(servernameMatch[1]);
          continue;
        }
        const sniMatch = trimmed.match(/^sni:\s*(.+)$/);
        if (sniMatch) {
          currentProxy.sni = unquote(sniMatch[1]);
          continue;
        }
        const congestionMatch = trimmed.match(/^congestion-controller:\s*(.+)$/);
        if (congestionMatch) {
          currentProxy.congestion = unquote(congestionMatch[1]);
          continue;
        }
        const cipherMatch = trimmed.match(/^cipher:\s*(.+)$/);
        if (cipherMatch) {
          currentProxy.cipher = unquote(cipherMatch[1]);
          continue;
        }
        const networkMatch = trimmed.match(/^network:\s*(.+)$/);
        if (networkMatch) {
          currentProxy.network = unquote(networkMatch[1]);
          continue;
        }
        const wsPathMatch = trimmed.match(/^path:\s*(.+)$/);
        if (wsPathMatch) {
          currentProxy.wsPath = unquote(wsPathMatch[1]);
          continue;
        }
        const tlsMatch = trimmed.match(/^tls:\s*(.+)$/);
        if (tlsMatch) {
          currentProxy.tls = unquote(tlsMatch[1]) === 'true';
          continue;
        }
        const fingerprintMatch = trimmed.match(/^client-fingerprint:\s*(.+)$/);
        if (fingerprintMatch) {
          currentProxy.fingerprint = unquote(fingerprintMatch[1]);
          continue;
        }
        const skipCertVerifyMatch = trimmed.match(/^skip-cert-verify:\s*(.+)$/);
        if (skipCertVerifyMatch) {
          currentProxy.skipCertVerify = unquote(skipCertVerifyMatch[1]) === 'true';
          continue;
        }
        const obfsTypeMatch = trimmed.match(/^type:\s*(.+)$/);
        if (obfsTypeMatch && line.includes('obfs:')) {
          currentProxy.obfsType = unquote(obfsTypeMatch[1]) as any;
          continue;
        }
        const obfsPasswordMatch = trimmed.match(/^password:\s*(.+)$/);
        if (obfsPasswordMatch && line.includes('obfs:')) {
          currentProxy.obfsPassword = unquote(obfsPasswordMatch[1]);
          continue;
        }
      }

      if (inDNS) {
        const enableMatch = trimmed.match(/^enable:\s*(.+)$/);
        if (enableMatch) {
          parsed.dns.enabled = unquote(enableMatch[1]) === 'true';
          continue;
        }
        const enhancedModeMatch = trimmed.match(/^enhanced-mode:\s*(.+)$/);
        if (enhancedModeMatch) {
          parsed.dns.enhancedMode = unquote(enhancedModeMatch[1]) as any;
          continue;
        }
        const fakeIpRangeMatch = trimmed.match(/^fake-ip-range:\s*(.+)$/);
        if (fakeIpRangeMatch) {
          parsed.dns.fakeIPRange = unquote(fakeIpRangeMatch[1]);
          continue;
        }
        if (trimmed.startsWith('nameserver:')) {
          inNameservers = true;
          inFallback = false;
          parsed.dns.nameservers = [];
          continue;
        }
        if (trimmed.startsWith('fallback:')) {
          inFallback = true;
          inNameservers = false;
          parsed.dns.fallback = [];
          continue;
        }
        if (inNameservers && (trimmed.startsWith('-') || trimmed.startsWith('  -') || line.startsWith('    -'))) {
          const listMatch = trimmed.match(/^-\s*(.+)$/);
          if (listMatch) {
            parsed.dns.nameservers = [...parsed.dns.nameservers, unquote(listMatch[1])];
          }
        }
        if (inFallback && (trimmed.startsWith('-') || trimmed.startsWith('  -') || line.startsWith('    -'))) {
          const listMatch = trimmed.match(/^-\s*(.+)$/);
          if (listMatch) {
            parsed.dns.fallback = [...parsed.dns.fallback, unquote(listMatch[1])];
          }
        }
      }

      if (inTUN) {
        const enableMatch = trimmed.match(/^enable:\s*(.+)$/);
        if (enableMatch) {
          parsed.tun.enabled = unquote(enableMatch[1]) === 'true';
          continue;
        }
        const stackMatch = trimmed.match(/^stack:\s*(.+)$/);
        if (stackMatch) {
          parsed.tun.stack = unquote(stackMatch[1]) as any;
          continue;
        }
        const autoRouteMatch = trimmed.match(/^auto-route:\s*(.+)$/);
        if (autoRouteMatch) {
          parsed.tun.autoRoute = unquote(autoRouteMatch[1]) === 'true';
          continue;
        }
        const autoDetectMatch = trimmed.match(/^auto-detect-interface:\s*(.+)$/);
        if (autoDetectMatch) {
          parsed.tun.autoDetectInterface = unquote(autoDetectMatch[1]) === 'true';
          continue;
        }
        if (trimmed.startsWith('dns-hijack:')) {
          inDnsHijack = true;
          parsed.tun.dnsHijack = [];
          continue;
        }
        if (inDnsHijack && (trimmed.startsWith('-') || trimmed.startsWith('  -') || line.startsWith('    -'))) {
          const listMatch = trimmed.match(/^-\s*(.+)$/);
          if (listMatch) {
            parsed.tun.dnsHijack = [...parsed.tun.dnsHijack, unquote(listMatch[1])];
          }
        }
      }

      if (inSniffer) {
        const enableMatch = trimmed.match(/^enable:\s*(.+)$/);
        if (enableMatch) {
          parsed.sniffer.enabled = unquote(enableMatch[1]) === 'true';
          continue;
        }
        if (trimmed.includes('HTTP:')) {
          parsed.sniffer.sniffHttp = true;
        }
        if (trimmed.includes('TLS:')) {
          parsed.sniffer.sniffTls = true;
        }
        if (trimmed.includes('QUIC:')) {
          parsed.sniffer.sniffQuic = true;
        }
      }

      if (inRules) {
        if (trimmed.startsWith('-')) {
          const parts = trimmed
            .substring(1)
            .split(',')
            .map((s) => s.trim());
          if (parts[0] === 'RULE-SET' || parts[0] === 'GEOSITE' || parts[0] === 'GEOIP') {
            const ruleName = parts[1];
            const ruleOutbound = parts[2];
            
            // Check if it is a zkeen geosite rule-provider
            const rp = ZKEEN_RULE_PROVIDERS.find((x) => x.name === ruleName || (x.behavior === 'domain' && `domain:${x.name.split('@')[0]}` === ruleName));
            if (rp) {
              parsed.activeRuleProvider = 'zkeen';
            } else if (ruleName.startsWith('geosite-') || ruleName.startsWith('geoip-')) {
              parsed.activeRuleProvider = 'metacubex';
              const parts2 = ruleName.split('-');
              const type = parts2[0] as 'geosite' | 'geoip';
              const originalId = parts2.slice(1).join('-');
              parsed.selectedMetaRuleSets.set(`${originalId}|${type}`, ruleOutbound);
            } else {
              // Custom manual rule
              parsed.rules.push({
                id: crypto.randomUUID(),
                type: parts[0],
                value: ruleName,
                outbound: ruleOutbound
              });
            }
          } else if (parts[0] === 'MATCH') {
            const ruleOutbound = parts[1];
            if (ruleOutbound !== 'DIRECT') {
              parsed.rules.push({
                id: crypto.randomUUID(),
                type: 'MATCH',
                value: '',
                outbound: ruleOutbound
              });
            }
          } else if (parts[0] === 'OR') {
            // Twitch OR rules
            parsed.activeRuleProvider = 'zkeen';
          } else {
            // General rule
            parsed.rules.push({
              id: crypto.randomUUID(),
              type: parts[0],
              value: parts[1] || '',
              outbound: parts[2] || 'DIRECT'
            });
          }
        }
      }

      if (inRuleProviders) {
        const nameMatch = trimmed.match(/^([a-zA-Z0-9_\-\@]+):$/);
        if (nameMatch) {
          const rpName = nameMatch[1];
          if (rpName !== 'quic@inline' && rpName !== 'netbios@inline') {
            const existsInZkeen = ZKEEN_RULE_PROVIDERS.some((rp) => rp.name === rpName);
            if (existsInZkeen) {
              parsed.activeRuleProvider = 'zkeen';
            }
          }
        }
      }
    }

    if (currentGroup) {
      parsed.groups.push(currentGroup);
    }
    if (currentProxy) {
      parsed.proxies.push(currentProxy);
    }
  } catch (e) {
    console.error('Failed to parse Mihomo config:', e);
  }

  return parsed;
}
