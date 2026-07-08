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
  username?: string;
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
  useProviders?: string[];
  strategy?: 'round-robin' | 'consistent-hashing' | 'sticky-sessions';
}

export interface Rule {
  id: string;
  type: string;
  value: string;
  outbound: string;
  noResolve?: boolean;
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
    name: 'reddit@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/reddit.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Reddit'
  },
  {
    name: 'youtube@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/youtube.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'YouTube'
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
    name: 'tiktok@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/tiktok.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'TikTok'
  },
  {
    name: 'discord@classical',
    url: 'https://github.com/zxc-rv/assets/raw/main/rules/discord.list',
    behavior: 'classical',
    format: 'text',
    outbound: 'Discord'
  },
  {
    name: 'speedtest@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/speedtest.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Speedtest'
  },
  {
    name: 'meta@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/meta.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Meta'
  },
  {
    name: 'meta@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/meta@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'Meta'
  },
  {
    name: 'telegram@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/telegram.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Telegram'
  },
  {
    name: 'telegram@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/telegram@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'Telegram'
  },
  {
    name: 'refilter@domain',
    url: 'https://github.com/legiz-ru/mihomo-rule-sets/raw/main/re-filter/domain-rule.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Заблок. сервисы'
  },
  {
    name: 'roblox@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/roblox.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Заблок. сервисы'
  },
  {
    name: 'github@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/github.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'GitHub'
  },
  {
    name: 'google@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/google.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'Google'
  },
  {
    name: 'google@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/google@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'Google'
  },
  {
    name: 'amazon@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/amazon.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'amazon@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/amazon@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'akamai@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/akamai.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'akamai@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/akamai@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'cloudflare@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/cloudflare.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'cloudflare@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/cloudflare@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'digitalocean@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/digitalocean.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'digitalocean@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/digitalocean@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'fastly@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/fastly.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'fastly@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/fastly@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'oracle@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/oracle.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'oracle@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/oracle@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'hetzner@domain',
    url: 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/hetzner.mrs',
    behavior: 'domain',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'hetzner@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/hetzner@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'scaleway@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/scaleway@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'ovh@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/ovh@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'vultr@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/vultr@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'vodafone@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/vodafone@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'gcore@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/gcore@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
  },
  {
    name: 'cdn77@ipcidr',
    url: 'https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/cdn77@ipcidr.mrs',
    behavior: 'ipcidr',
    format: 'mrs',
    outbound: 'CDN'
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
    outbound: 'REJECT',
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

export const ZKEEN_STANDARD_RULES = [
  { type: 'RULE-SET', value: 'adlist@domain', outbound: 'REJECT' },
  { type: 'RULE-SET', value: 'quic@inline', outbound: 'REJECT' },
  { type: 'RULE-SET', value: 'netbios@inline', outbound: 'REJECT' },
  {
    type: 'OR',
    value: '((DOMAIN-SUFFIX,gql.twitch.tv),(DOMAIN-SUFFIX,usher.ttvnw.net))',
    outbound: 'Заблок. сервисы'
  },
  { type: 'RULE-SET', value: 'category-ai@domain', outbound: 'AI' },
  { type: 'RULE-SET', value: 'steam@domain', outbound: 'Steam' },
  { type: 'RULE-SET', value: 'spotify@domain', outbound: 'Spotify' },
  { type: 'RULE-SET', value: 'reddit@domain', outbound: 'Reddit' },
  { type: 'RULE-SET', value: 'youtube@domain', outbound: 'YouTube' },
  { type: 'RULE-SET', value: 'twitch@domain', outbound: 'Twitch' },
  { type: 'RULE-SET', value: 'twitter@domain', outbound: 'Twitter' },
  { type: 'RULE-SET', value: 'tiktok@domain', outbound: 'TikTok' },
  { type: 'RULE-SET', value: 'discord@classical', outbound: 'Discord' },
  { type: 'RULE-SET', value: 'speedtest@domain', outbound: 'Speedtest' },
  {
    type: 'OR',
    value: '((RULE-SET,meta@domain),(RULE-SET,meta@ipcidr,no-resolve))',
    outbound: 'Meta'
  },
  {
    type: 'OR',
    value: '((RULE-SET,telegram@domain),(RULE-SET,telegram@ipcidr,no-resolve))',
    outbound: 'Telegram'
  },
  { type: 'RULE-SET', value: 'refilter@domain', outbound: 'Заблок. сервисы' },
  { type: 'OR', value: '((RULE-SET,roblox@domain))', outbound: 'Заблок. сервисы' },
  { type: 'RULE-SET', value: 'github@domain', outbound: 'GitHub' },
  { type: 'OR', value: '((RULE-SET,google@domain),(RULE-SET,google@ipcidr))', outbound: 'Google' },
  { type: 'OR', value: '((RULE-SET,amazon@domain),(RULE-SET,amazon@ipcidr))', outbound: 'CDN' },
  { type: 'OR', value: '((RULE-SET,akamai@domain),(RULE-SET,akamai@ipcidr))', outbound: 'CDN' },
  {
    type: 'OR',
    value: '((RULE-SET,cloudflare@domain),(RULE-SET,cloudflare@ipcidr))',
    outbound: 'CDN'
  },
  {
    type: 'OR',
    value: '((RULE-SET,digitalocean@domain),(RULE-SET,digitalocean@ipcidr))',
    outbound: 'CDN'
  },
  { type: 'OR', value: '((RULE-SET,fastly@domain),(RULE-SET,fastly@ipcidr))', outbound: 'CDN' },
  { type: 'OR', value: '((RULE-SET,oracle@domain),(RULE-SET,oracle@ipcidr))', outbound: 'CDN' },
  { type: 'OR', value: '((RULE-SET,hetzner@domain),(RULE-SET,hetzner@ipcidr))', outbound: 'CDN' },
  { type: 'RULE-SET', value: 'scaleway@ipcidr', outbound: 'CDN' },
  { type: 'RULE-SET', value: 'ovh@ipcidr', outbound: 'CDN' },
  { type: 'RULE-SET', value: 'vultr@ipcidr', outbound: 'CDN' },
  { type: 'RULE-SET', value: 'vodafone@ipcidr', outbound: 'CDN' },
  { type: 'RULE-SET', value: 'gcore@ipcidr', outbound: 'CDN' },
  { type: 'RULE-SET', value: 'cdn77@ipcidr', outbound: 'CDN' },
  { type: 'RULE-SET', value: 'private@ip', outbound: 'DIRECT' }
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
    if (
      trimmed === header ||
      trimmed.startsWith(header + ' ') ||
      trimmed.startsWith(header + '\t')
    ) {
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
      (trimmed === header ||
        trimmed.startsWith(header + ' ') ||
        trimmed.startsWith(header + '\t')) &&
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

export function replaceMihomoTopLevelSection(
  content: string,
  sectionName: string,
  newContent: string
): string {
  const lines = content.split('\n');
  const { start, end } = findTopLevelSection(lines, sectionName);
  const newLines = newContent.trim() !== '' ? newContent.trimEnd().split('\n') : [];

  if (start === -1) {
    if (newLines.length === 0) return content;
    const appended = `\n${sectionName}:\n` + newLines.join('\n') + '\n';
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
  mihomoProviders?: any[];
  capabilities?: any;
  hasZkeenGeodata?: boolean;
  ruleProviders?: RuleProvider[];
}

const CYRILLIC_MAP: Record<string, string> = {
  а: 'a',
  б: 'b',
  в: 'v',
  г: 'g',
  д: 'd',
  е: 'e',
  ё: 'yo',
  ж: 'zh',
  з: 'z',
  и: 'i',
  й: 'j',
  к: 'k',
  л: 'l',
  м: 'm',
  н: 'n',
  о: 'o',
  п: 'p',
  р: 'r',
  с: 's',
  т: 't',
  у: 'u',
  ф: 'f',
  х: 'kh',
  ц: 'ts',
  ч: 'ch',
  ш: 'sh',
  щ: 'shch',
  ы: 'y',
  э: 'e',
  ю: 'yu',
  я: 'ya',
  ь: '',
  ъ: ''
};

export function slugifyProviderName(
  profileTitle: string,
  name: string,
  urlStr: string,
  fallback: string
): string {
  let source = profileTitle || name;
  if (!source) {
    try {
      const parsed = new URL(urlStr);
      const base = parsed.pathname.split('/').filter(Boolean).pop() || '';
      if (base) source = base;
    } catch {
      // ignore invalid URL
    }
  }
  if (!source) source = fallback;
  const slug = source
    .toLowerCase()
    .split('')
    .map((c) => CYRILLIC_MAP[c] ?? c)
    .join('')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return slug || fallback;
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

  // Proxy-providers (только Mihomo-подписки)
  const providers = state.mihomoProviders ?? [];
  if (providers.length > 0) {
    lines.push('proxy-providers:');
    for (const [i, sub] of providers.entries()) {
      const providerName = sub.isVirtual
        ? sub.id
        : slugifyProviderName(
            sub.profile_title || '',
            sub.name || '',
            sub.url || '',
            sub.id || `provider-${i}`
          );
      lines.push(`  ${providerName}:`);
      if (sub.rawLines && sub.rawLines.length > 0) {
        let currentParent = '';
        let parentIndent = 0;
        for (const rawLine of sub.rawLines) {
          let processedLine = rawLine;
          const trimmed = rawLine.trim();
          const lineIndent = rawLine.length - rawLine.trimStart().length;

          if (currentParent && lineIndent <= parentIndent) {
            currentParent = '';
            parentIndent = 0;
          }

          if (trimmed.endsWith(':') && !trimmed.startsWith('-')) {
            currentParent = trimmed.slice(0, -1).trim();
            parentIndent = lineIndent;
          }

          if (trimmed.startsWith('url:')) {
            processedLine =
              rawLine.substring(0, rawLine.indexOf('url:') + 4) + ` ${yamlSafeString(sub.url)}`;
          } else if (trimmed.startsWith('interval:')) {
            const intervalSec = sub.interval > 720 ? sub.interval : sub.interval * 3600 || 86400;
            processedLine =
              rawLine.substring(0, rawLine.indexOf('interval:') + 9) + ` ${intervalSec}`;
          } else if (currentParent === 'x-hwid' && trimmed.startsWith('-')) {
            if (sub.hwid_token) {
              processedLine =
                rawLine.substring(0, rawLine.indexOf('-') + 1) +
                ` ${yamlSafeString(sub.hwid_token)}`;
            }
          }
          lines.push(processedLine);
        }
      } else {
        lines.push(`    type: http`);
        lines.push(`    path: ./proxy_providers/${providerName}.yaml`);
        const currentPort =
          typeof window !== 'undefined' && window.location.port && window.location.port !== '5173'
            ? window.location.port
            : '8090';
        lines.push(
          `    url: "http://127.0.0.1:${currentPort}/mihomo/provider.yaml?url=${encodeURIComponent(sub.url || '')}"`
        );
        const intervalSec = sub.interval > 720 ? sub.interval : sub.interval * 3600 || 86400;
        lines.push(`    interval: ${intervalSec}`);
        lines.push(`    health-check:`);
        lines.push(`      enable: true`);
        lines.push(`      url: http://www.gstatic.com/generate_204`);
        lines.push(`      interval: 300`);
      }
    }
    lines.push('');
  }

  // Rule-providers (if selected)
  if (state.activeRuleProvider !== 'none') {
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
    const buildMetaRuleSetUrl = (id: string, type: 'geosite' | 'geoip') =>
      `${metaBaseUrl}/${type}/${id}.mrs`;

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
      const providers =
        state.activeRuleProvider === 'zkeen'
          ? state.ruleProviders || ZKEEN_RULE_PROVIDERS
          : RULE_PROVIDERS[state.activeRuleProvider];
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
  }

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
      } else if (p.type === 'trojan') {
        lines.push(`    password: ${yamlSafeString(p.password || '')}`);
        if (p.sni) lines.push(`    sni: ${yamlSafeString(p.sni)}`);
        if (p.skipCertVerify) lines.push(`    skip-cert-verify: true`);
        if (p.network) {
          lines.push(`    network: ${p.network}`);
          if (p.network === 'ws') {
            lines.push(`    ws-opts:`);
            lines.push(`      path: ${yamlSafeString(p.wsPath || '/')}`);
          }
        }
      } else if (p.type === 'socks') {
        if (p.username) lines.push(`    username: ${yamlSafeString(p.username)}`);
        if (p.password) lines.push(`    password: ${yamlSafeString(p.password)}`);
      } else if (p.type === 'http') {
        if (p.username) lines.push(`    username: ${yamlSafeString(p.username)}`);
        if (p.password) lines.push(`    password: ${yamlSafeString(p.password)}`);
        if (p.tls) lines.push(`    tls: true`);
        if (p.skipCertVerify) lines.push(`    skip-cert-verify: true`);
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
      if (g.useProviders && g.useProviders.length > 0) {
        lines.push(`    use:`);
        for (const p of g.useProviders) lines.push(`      - ${yamlSafeString(p)}`);
      }
      if (g.type === 'load-balance' && g.strategy) {
        lines.push(`    strategy: ${g.strategy}`);
      }
      if (g.proxies && g.proxies.length > 0) {
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
      const activeRules = [...ZKEEN_STANDARD_RULES];
      if (state.hasZkeenGeodata) {
        const refilterIdx = activeRules.findIndex((r) => r.value === 'refilter@domain');
        if (refilterIdx !== -1) {
          activeRules.splice(
            refilterIdx + 1,
            0,
            { type: 'GEOSITE', value: 'DOMAINS', outbound: 'Заблок. сервисы' },
            { type: 'GEOSITE', value: 'OTHER', outbound: 'Заблок. сервисы' },
            { type: 'GEOSITE', value: 'POLITIC', outbound: 'Заблок. сервисы' }
          );
        }
      }

      for (const r of activeRules) {
        if (isOutboundEnabled(r.outbound)) {
          if (r.type === 'OR') {
            lines.push(`  - OR,${r.value},${r.outbound}`);
          } else {
            lines.push(`  - ${r.type},${r.value},${r.outbound}`);
          }
        }
      }

      // Custom user rules (except MATCH which goes last)
      for (const r of state.rules) {
        if (isOutboundEnabled(r.outbound)) {
          if (r.type === 'MATCH') continue;
          if (r.type === 'OR') {
            lines.push(`  - OR,${r.value},${r.outbound}`);
          } else {
            const suffix = r.noResolve ? ',no-resolve' : '';
            lines.push(`  - ${r.type},${r.value},${r.outbound}${suffix}`);
          }
        }
      }

      lines.push('  - MATCH,DIRECT');
    } else {
      if (state.activeRuleProvider !== 'none') {
        lines.push('  - RULE-SET,quic@inline,REJECT');
        lines.push('  - RULE-SET,netbios@inline,REJECT');
      }

      // Rule-set entries from rule-providers (before user rules, before MATCH)
      if (state.activeRuleProvider === 'metacubex') {
        for (const [key, outbound] of state.selectedMetaRuleSets) {
          const [id, type] = key.split('|') as [string, 'geosite' | 'geoip'];
          lines.push(`  - RULE-SET,${type}-${id.replace(/[^a-z0-9-]/g, '-')},${outbound}`);
        }
      } else if (state.activeRuleProvider !== 'none') {
        const providers =
          state.activeRuleProvider === 'zkeen'
            ? state.ruleProviders || ZKEEN_RULE_PROVIDERS
            : RULE_PROVIDERS[state.activeRuleProvider];
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
  }
  lines.push('');

  // DNS
  lines.push('dns:');
  lines.push(`  enable: ${state.dns.enabled}`);
  if (state.dns.enabled) {
    lines.push(`  enhanced-mode: ${state.dns.enhancedMode}`);
    if (state.dns.enhancedMode === 'fake-ip')
      lines.push(`  fake-ip-range: ${state.dns.fakeIPRange}`);
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

export interface ParsedMihomoConfig {
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
  mihomoProviders: any[];
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
    existingRedirPort: null,
    mihomoProviders: []
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
    let inUselist = false;
    let inProxyProviders = false;

    let currentGroup: any = null;
    let currentProxy: any = null;
    let currentProvider: any = null;
    let inProxiesList = false;
    let currentParentKey = '';
    let parentKeyIndent = 0;
    let currentProviderParentKey = '';
    let providerParentIndent = 0;

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
          inProxyProviders = sec === 'proxy-providers';

          if (
            sec !== 'proxy-groups' &&
            sec !== 'proxies' &&
            sec !== 'dns' &&
            sec !== 'tun' &&
            sec !== 'rules' &&
            sec !== 'rule-providers' &&
            sec !== 'sniffer' &&
            sec !== 'tproxy-port' &&
            sec !== 'redir-port' &&
            sec !== 'external-controller' &&
            sec !== 'proxy-providers'
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
        const indentMatch = line.match(/^(\s*)-/);
        const indentLength = indentMatch ? indentMatch[1].length : 0;
        const isNewGroup =
          line.startsWith('  -') ||
          line.startsWith(' -') ||
          (trimmed.startsWith('-') && indentLength < 4);

        if (isNewGroup) {
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
          inUselist = false;

          const nameMatch = trimmed.match(/^-\s+name:\s*(.+)$/);
          if (nameMatch) {
            currentGroup.name = unquote(nameMatch[1]);
          }
          continue;
        }

        // Reset list states when encountering other key-value pairs
        const isKeyValuePair = /^[a-zA-Z0-9_-]+:/.test(trimmed);
        if (isKeyValuePair && !trimmed.startsWith('proxies:') && !trimmed.startsWith('use:')) {
          inProxiesList = false;
          inUselist = false;
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

        const useMatch = trimmed.match(/^use:\s*\[(.+)\]$/);
        if (useMatch) {
          currentGroup.useProviders = useMatch[1]
            .split(',')
            .map((s) => unquote(s.trim()))
            .filter(Boolean);
          continue;
        }
        if (trimmed === 'use:') {
          inUselist = true;
          continue;
        }
        if (inUselist) {
          if (trimmed.startsWith('-')) {
            const item = trimmed.replace(/^-\s*/, '');
            currentGroup.useProviders = [...(currentGroup.useProviders || []), unquote(item)];
            continue;
          } else {
            inUselist = false;
          }
        }
        const strategyMatch = trimmed.match(/^strategy:\s*(.+)$/);
        if (strategyMatch) {
          currentGroup.strategy = unquote(strategyMatch[1]) as any;
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

        if (
          inProxiesList &&
          (trimmed.startsWith('-') || trimmed.startsWith('  -') || line.startsWith('    -'))
        ) {
          const proxyItemMatch = trimmed.match(/^-\s*(.+)$/);
          if (proxyItemMatch) {
            currentGroup.proxies.push(unquote(proxyItemMatch[1]));
          }
        }
      }

      if (inProxies) {
        const lineIndent = line.length - line.trimStart().length;
        if (currentParentKey && lineIndent <= parentKeyIndent) {
          currentParentKey = '';
          parentKeyIndent = 0;
        }

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
          currentParentKey = '';
          parentKeyIndent = 0;

          const nameMatch = trimmed.match(/^-\s+name:\s*(.+)$/);
          if (nameMatch) {
            currentProxy.name = unquote(nameMatch[1]);
          }
          continue;
        }

        if (!currentProxy) continue;

        if (trimmed.endsWith(':') && !trimmed.startsWith('-')) {
          currentParentKey = trimmed.slice(0, -1).trim();
          parentKeyIndent = lineIndent;
          continue;
        }

        const nameMatch = trimmed.match(/^name:\s*(.+)$/);
        if (nameMatch && !currentParentKey) {
          currentProxy.name = unquote(nameMatch[1]);
          continue;
        }
        const typeMatch = trimmed.match(/^type:\s*(.+)$/);
        if (typeMatch) {
          if (currentParentKey === 'obfs') {
            currentProxy.obfsType = unquote(typeMatch[1]) as any;
          } else if (!currentParentKey) {
            currentProxy.type = unquote(typeMatch[1]);
          }
          continue;
        }
        const serverMatch = trimmed.match(/^server:\s*(.+)$/);
        if (serverMatch && !currentParentKey) {
          currentProxy.server = unquote(serverMatch[1]);
          continue;
        }
        const portMatch = trimmed.match(/^port:\s*(.+)$/);
        if (portMatch && !currentParentKey) {
          currentProxy.port = parseInt(unquote(portMatch[1])) || 443;
          continue;
        }
        const uuidMatch = trimmed.match(/^uuid:\s*(.+)$/);
        if (uuidMatch && !currentParentKey) {
          currentProxy.uuid = unquote(uuidMatch[1]);
          continue;
        }
        const passwordMatch = trimmed.match(/^password:\s*(.+)$/);
        if (passwordMatch) {
          if (currentParentKey === 'obfs') {
            currentProxy.obfsPassword = unquote(passwordMatch[1]);
          } else if (!currentParentKey) {
            currentProxy.password = unquote(passwordMatch[1]);
          }
          continue;
        }
        const flowMatch = trimmed.match(/^flow:\s*(.+)$/);
        if (flowMatch && !currentParentKey) {
          currentProxy.flow = unquote(flowMatch[1]);
          continue;
        }
        const publicKeyMatch = trimmed.match(/^public-key:\s*(.+)$/);
        if (publicKeyMatch && currentParentKey === 'reality-opts') {
          currentProxy.publicKey = unquote(publicKeyMatch[1]);
          continue;
        }
        const shortIdMatch = trimmed.match(/^short-id:\s*(.+)$/);
        if (shortIdMatch && currentParentKey === 'reality-opts') {
          currentProxy.shortId = unquote(shortIdMatch[1]);
          continue;
        }
        const servernameMatch = trimmed.match(/^servername:\s*(.+)$/);
        if (servernameMatch && !currentParentKey) {
          currentProxy.servername = unquote(servernameMatch[1]);
          continue;
        }
        const sniMatch = trimmed.match(/^sni:\s*(.+)$/);
        if (sniMatch && !currentParentKey) {
          currentProxy.sni = unquote(sniMatch[1]);
          continue;
        }
        const congestionMatch = trimmed.match(/^congestion-controller:\s*(.+)$/);
        if (congestionMatch && !currentParentKey) {
          currentProxy.congestion = unquote(congestionMatch[1]);
          continue;
        }
        const cipherMatch = trimmed.match(/^cipher:\s*(.+)$/);
        if (cipherMatch && !currentParentKey) {
          currentProxy.cipher = unquote(cipherMatch[1]);
          continue;
        }
        const networkMatch = trimmed.match(/^network:\s*(.+)$/);
        if (networkMatch && !currentParentKey) {
          currentProxy.network = unquote(networkMatch[1]);
          continue;
        }
        const wsPathMatch = trimmed.match(/^path:\s*(.+)$/);
        if (wsPathMatch && currentParentKey === 'ws-opts') {
          currentProxy.wsPath = unquote(wsPathMatch[1]);
          continue;
        }
        const tlsMatch = trimmed.match(/^tls:\s*(.+)$/);
        if (tlsMatch && !currentParentKey) {
          currentProxy.tls = unquote(tlsMatch[1]) === 'true';
          continue;
        }
        const fingerprintMatch = trimmed.match(/^client-fingerprint:\s*(.+)$/);
        if (fingerprintMatch && !currentParentKey) {
          currentProxy.fingerprint = unquote(fingerprintMatch[1]);
          continue;
        }
        const skipCertVerifyMatch = trimmed.match(/^skip-cert-verify:\s*(.+)$/);
        if (skipCertVerifyMatch && !currentParentKey) {
          currentProxy.skipCertVerify = unquote(skipCertVerifyMatch[1]) === 'true';
          continue;
        }
        const usernameMatch = trimmed.match(/^username:\s*(.+)$/);
        if (usernameMatch && !currentParentKey) {
          currentProxy.username = unquote(usernameMatch[1]);
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
        if (
          inNameservers &&
          (trimmed.startsWith('-') || trimmed.startsWith('  -') || line.startsWith('    -'))
        ) {
          const listMatch = trimmed.match(/^-\s*(.+)$/);
          if (listMatch) {
            parsed.dns.nameservers = [...parsed.dns.nameservers, unquote(listMatch[1])];
          }
        }
        if (
          inFallback &&
          (trimmed.startsWith('-') || trimmed.startsWith('  -') || line.startsWith('    -'))
        ) {
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
        if (
          inDnsHijack &&
          (trimmed.startsWith('-') || trimmed.startsWith('  -') || line.startsWith('    -'))
        ) {
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
          let ruleType = '';
          let ruleValue = '';
          let ruleOutbound = '';
          let noResolve = false;

          if (trimmed.startsWith('- OR,')) {
            ruleType = 'OR';
            const valStr = trimmed.substring(5).trim();
            const lastCommaIdx = valStr.lastIndexOf(',');
            if (lastCommaIdx !== -1) {
              ruleValue = valStr.substring(0, lastCommaIdx).trim();
              ruleOutbound = valStr.substring(lastCommaIdx + 1).trim();
            } else {
              ruleValue = valStr;
              ruleOutbound = 'DIRECT';
            }
          } else {
            const rawParts = trimmed
              .substring(1)
              .split(',')
              .map((s) => s.trim());
            if (rawParts.length > 0) {
              ruleType = rawParts[0];
              const remaining = rawParts.slice(1);
              if (remaining.length > 0 && remaining[remaining.length - 1] === 'no-resolve') {
                noResolve = true;
                remaining.pop();
              }
              if (remaining.length > 1) {
                ruleOutbound = remaining[remaining.length - 1];
                ruleValue = remaining.slice(0, remaining.length - 1).join(',');
              } else if (remaining.length === 1) {
                ruleOutbound = remaining[0];
                ruleValue = '';
              }
            }
          }

          if (ruleType) {
            // Check if it is a standard Zkeen rule
            const isZkeenRule = ZKEEN_STANDARD_RULES.some(
              (zr) => zr.type === ruleType && zr.value === ruleValue && zr.outbound === ruleOutbound
            );
            const isZkeenGeodataRule =
              ruleType === 'GEOSITE' &&
              ['DOMAINS', 'OTHER', 'POLITIC'].includes(ruleValue) &&
              ruleOutbound === 'Заблок. сервисы';

            if (isZkeenRule || isZkeenGeodataRule) {
              parsed.activeRuleProvider = 'zkeen';
            } else if (
              ruleType === 'RULE-SET' &&
              (ruleValue.startsWith('geosite-') || ruleValue.startsWith('geoip-'))
            ) {
              parsed.activeRuleProvider = 'metacubex';
              const parts2 = ruleValue.split('-');
              const type = parts2[0] as 'geosite' | 'geoip';
              const originalId = parts2.slice(1).join('-');
              parsed.selectedMetaRuleSets.set(`${originalId}|${type}`, ruleOutbound);
            } else {
              // Custom rule
              if (ruleType === 'MATCH' && ruleOutbound === 'DIRECT') {
                // skip standard MATCH,DIRECT
              } else {
                parsed.rules.push({
                  id: crypto.randomUUID(),
                  type: ruleType,
                  value: ruleValue,
                  outbound: ruleOutbound,
                  noResolve
                });
              }
            }
          }
        }
      }

      if (inProxyProviders) {
        const lineIndent = line.length - line.trimStart().length;
        if (currentProviderParentKey && lineIndent <= providerParentIndent) {
          currentProviderParentKey = '';
          providerParentIndent = 0;
        }

        // Detect a new provider block: "  provider-name:"
        const providerNameMatch = line.match(/^ {2}([a-zA-Z0-9_\-@.]+):\s*$/);
        if (providerNameMatch) {
          if (currentProvider) {
            parsed.mihomoProviders.push(currentProvider);
          }
          currentProvider = {
            id: providerNameMatch[1],
            name: providerNameMatch[1],
            type: 'mihomo',
            url: '',
            interval: 24,
            hwid_token: '',
            rawLines: []
          };
          currentProviderParentKey = '';
          providerParentIndent = 0;
          continue;
        }

        if (currentProvider) {
          if (lineIndent > 2) {
            currentProvider.rawLines.push(line);
          }

          if (trimmed.endsWith(':') && !trimmed.startsWith('-')) {
            currentProviderParentKey = trimmed.slice(0, -1).trim();
            providerParentIndent = lineIndent;
            continue;
          }

          const urlMatch = trimmed.match(/^url:\s*(.+)$/);
          if (urlMatch && !currentProviderParentKey) {
            currentProvider.url = unquote(urlMatch[1]);
            continue;
          }

          const intervalMatch = trimmed.match(/^interval:\s*(.+)$/);
          if (intervalMatch && !currentProviderParentKey) {
            const val = parseInt(unquote(intervalMatch[1])) || 3600;
            currentProvider.interval = val > 720 ? Math.round(val / 3600) : val;
            continue;
          }

          if (currentProviderParentKey === 'x-hwid' && trimmed.startsWith('-')) {
            const hwidVal = trimmed.replace(/^-\s*/, '');
            currentProvider.hwid_token = unquote(hwidVal);
            continue;
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
    if (currentProvider) {
      parsed.mihomoProviders.push(currentProvider);
    }
  } catch (e) {
    console.error('Failed to parse Mihomo config:', e);
  }

  return parsed;
}
