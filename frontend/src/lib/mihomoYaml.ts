import { isMihomoAwg31Supported } from './awgFields';

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
  dialerProxy?: string;
  ports?: string;
  // WireGuard & AmneziaWG (TMPL-08)
  wgPrivateKey?: string;
  wgPublicKey?: string;
  wgIp?: string;
  wgPresharedKey?: string;
  wgMtu?: number;
  awgEnabled?: boolean;
  awgJc?: number;
  awgJmin?: number;
  awgJmax?: number;
  awgS1?: number;
  awgS2?: number;
  awgS3?: number;
  awgS4?: number;
  awgH1?: number | string;
  awgH2?: number | string;
  awgH3?: number | string;
  awgH4?: number | string;
  awgVersion?: string;
  awgHeaderProtectionKey?: string;
  awgI1?: string;
  awgI2?: string;
  awgI3?: string;
  awgI4?: string;
  awgI5?: string;
  awgContentPaddingAddition?: number;
  awgRandomTrailers?: boolean;
  awgDisableCookies?: boolean;
  awgRekeyAfterTime?: number;
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
  lazy?: boolean;
  expectedStatus?: string;
  excludeType?: string;
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

export type ListenerType = 'mixed' | 'socks' | 'http' | 'shadowsocks' | 'tproxy' | 'redirect';

export interface ListenerUser {
  username: string;
  password: string;
}

export interface Listener {
  id: string;
  name: string;
  type: ListenerType;
  listen: string;
  port: string;
  proxy?: string;
  udp?: boolean;
  users?: ListenerUser[];
  cipher?: string;
  password?: string;
  routingMark?: number;
}

export const BUILDER_LISTENER_TYPES: readonly string[] = [
  'mixed',
  'socks',
  'http',
  'shadowsocks',
  'tproxy',
  'redir'
];

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
  externalControllerType?: 'unix' | 'tcp';
  externalControllerTarget?: string;
  subscriptions: any[];
  mihomoProviders?: any[];
  capabilities?: any;
  hasZkeenGeodata?: boolean;
  ruleProviders?: RuleProvider[];
  listeners?: Listener[];
  listenersRaw?: string | null;
  listenersReadOnly?: boolean;
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

// Служебные слова, отбрасываемые при выводе бренда провайдера из profile-title.
// Зеркалит providerGenericWords бэкенда (subscription_converter.go).
const PROVIDER_GENERIC_WORDS = new Set(['подписка', 'профиль', 'subscription', 'profile']);

// Выводит бренд из profile-title: убирает эмодзи и символы, отбрасывает служебные слова.
function extractProviderBrand(title: string): string {
  const cleaned = title
    .split('')
    .filter((c) => /[\p{L}\p{N} \-_.+&]/u.test(c))
    .join('');
  return cleaned
    .split(/\s+/)
    .filter((w) => w && !PROVIDER_GENERIC_WORDS.has(w.toLowerCase()))
    .join(' ');
}

// Зеркалит GetMihomoProviderName бэкенда: имя пользователя → бренд из
// profile-title → fallback (ID). Сегмент URL не используется — он содержит
// секретный токен подписки. Регистр сохраняется.
export function slugifyProviderName(
  profileTitle: string,
  name: string,
  _urlStr: string,
  fallback: string
): string {
  const source = name || extractProviderBrand(profileTitle || '') || fallback;
  const slug = source
    .split('')
    .map((c) => (/[а-яёА-ЯЁ]/.test(c) ? (CYRILLIC_MAP[c.toLowerCase()] ?? c) : c))
    .join('')
    .replace(/[^A-Za-z0-9-]+/g, '-')
    .replace(/-{2,}/g, '-')
    .replace(/^-+|-+$/g, '');
  return slug || fallback;
}

export function generateYAML(state: MihomoConfigState): string {
  const lines: string[] = [];

  // AWG-05: гейт версии ядра для ключей AmneziaWG 3.1. Источник тот же, что у
  // ProxyForm.isAwg31Allowed (активное ядро + версия mihomo). Если 3.1 не
  // поддерживается (старое ядро / активен Xray / нет данных о ядре), 3.1-only
  // ключи не эмитятся — иначе ядро не распарсит конфиг и служба не стартует.
  // Classic/2.0-ключи эмитятся всегда, вывод для них байт-в-байт не меняется.
  const awgCaps = state.capabilities;
  const awgActiveKernel = awgCaps?.active_kernel || 'mihomo';
  const awg31Supported =
    awgActiveKernel !== 'xray' && isMihomoAwg31Supported(awgCaps?.kernels?.mihomo?.version);

  // external-controller or external-controller-unix (defaults to Unix Domain Socket)
  if (state.externalControllerType === 'tcp' && state.externalControllerTarget) {
    lines.push(`external-controller: ${state.externalControllerTarget}`);
  } else {
    lines.push('external-controller-unix: /opt/var/run/mihomo.sock');
  }
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
      for (const key of state.selectedMetaRuleSets.keys()) {
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
      } else if (p.type === 'socks' || p.type === 'socks5') {
        if (p.username) lines.push(`    username: ${yamlSafeString(p.username)}`);
        if (p.password) lines.push(`    password: ${yamlSafeString(p.password)}`);
      } else if (p.type === 'http') {
        if (p.username) lines.push(`    username: ${yamlSafeString(p.username)}`);
        if (p.password) lines.push(`    password: ${yamlSafeString(p.password)}`);
        if (p.tls) lines.push(`    tls: true`);
        if (p.skipCertVerify) lines.push(`    skip-cert-verify: true`);
      } else if (p.type === 'wireguard') {
        if (p.wgPrivateKey) lines.push(`    private-key: ${yamlSafeString(p.wgPrivateKey)}`);
        if (p.wgPublicKey) lines.push(`    public-key: ${yamlSafeString(p.wgPublicKey)}`);
        if (p.wgIp) lines.push(`    ip: ${yamlSafeString(p.wgIp)}`);
        if (p.wgPresharedKey) lines.push(`    pre-shared-key: ${yamlSafeString(p.wgPresharedKey)}`);
        if (p.wgMtu) lines.push(`    mtu: ${p.wgMtu}`);
        lines.push(`    udp: true`);

        if (p.awgEnabled) {
          lines.push(`    amnezia-wg-option:`);
          lines.push(`      jc: ${p.awgJc ?? 4}`);
          lines.push(`      jmin: ${p.awgJmin ?? 40}`);
          lines.push(`      jmax: ${p.awgJmax ?? 70}`);
          lines.push(`      s1: ${p.awgS1 ?? 15}`);
          lines.push(`      s2: ${p.awgS2 ?? 40}`);
          if (p.awgS3 !== undefined && p.awgS3 !== null) lines.push(`      s3: ${p.awgS3}`);
          if (p.awgS4 !== undefined && p.awgS4 !== null) lines.push(`      s4: ${p.awgS4}`);

          const formatH = (val: number | string | undefined, defVal: number) => {
            const v = val ?? defVal;
            if (typeof v === 'string') {
              return String(v).includes('-') ? `"${v}"` : v;
            }
            return v;
          };
          lines.push(`      h1: ${formatH(p.awgH1, 1000000001)}`);
          lines.push(`      h2: ${formatH(p.awgH2, 1000000002)}`);
          lines.push(`      h3: ${formatH(p.awgH3, 1000000003)}`);
          lines.push(`      h4: ${formatH(p.awgH4, 1000000004)}`);

          // Ключи AWG 3.1 — только если ядро их поддерживает (AWG-05 / WR-02).
          if (awg31Supported) {
            if (p.awgVersion) lines.push(`      version: ${yamlSafeString(p.awgVersion)}`);
            if (p.awgHeaderProtectionKey)
              lines.push(
                `      header-protection-key: ${yamlSafeString(p.awgHeaderProtectionKey)}`
              );
            // I1..I5 — шаблоны junk-пакетов с регистрозависимыми CPS-тегами
            // (<b 0x…>, <c>, <r N>, <t>): регистр не трогаем, только trim.
            if (p.awgI1) lines.push(`      i1: ${yamlSafeString(p.awgI1.trim())}`);
            if (p.awgI2) lines.push(`      i2: ${yamlSafeString(p.awgI2.trim())}`);
            if (p.awgI3) lines.push(`      i3: ${yamlSafeString(p.awgI3.trim())}`);
            if (p.awgI4) lines.push(`      i4: ${yamlSafeString(p.awgI4.trim())}`);
            if (p.awgI5) lines.push(`      i5: ${yamlSafeString(p.awgI5.trim())}`);
            if (p.awgContentPaddingAddition !== undefined && p.awgContentPaddingAddition !== null) {
              lines.push(`      content-padding-addition: ${p.awgContentPaddingAddition}`);
            }
            if (p.awgRandomTrailers === true) lines.push(`      random-trailers: true`);
            if (p.awgDisableCookies === true) lines.push(`      disable-cookies: true`);
            if (p.awgRekeyAfterTime !== undefined && p.awgRekeyAfterTime !== null) {
              lines.push(`      rekey-after-time: ${p.awgRekeyAfterTime}`);
            }
          }
        }
      }
      if (p.dialerProxy) {
        lines.push(`    dialer-proxy: ${yamlSafeString(p.dialerProxy)}`);
      }
      if (p.ports) {
        lines.push(`    ports: ${yamlSafeString(p.ports)}`);
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
      if (g.excludeType) {
        lines.push(`    exclude-type: ${yamlSafeString(g.excludeType)}`);
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
      // relay is just a proxy chain: name/type/proxies only. url/interval/lazy/
      // hidden/expected-status/tolerance/max-failed-times are meaningless for it
      // (the user already gets a deprecation warning).
      if (g.type !== 'select' && g.type !== 'relay') {
        lines.push(`    url: ${g.url || 'https://www.gstatic.com/generate_204'}`);
        lines.push(`    interval: ${g.interval || 300}`);
        if (g.hidden === true) {
          lines.push(`    hidden: true`);
        }
        if (g.lazy === true) {
          lines.push(`    lazy: true`);
        }
        if (g.expectedStatus) {
          lines.push(`    expected-status: ${g.expectedStatus}`);
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

  // Listeners
  if (state.listenersReadOnly && state.listenersRaw) {
    lines.push('listeners:');
    const rawLines = state.listenersRaw.split('\n');
    for (const rl of rawLines) {
      lines.push(rl);
    }
    lines.push('');
  } else if (state.listeners && state.listeners.length > 0) {
    lines.push('listeners:');
    for (const l of state.listeners) {
      lines.push(`  - name: ${yamlSafeString(l.name)}`);
      const yamlType = l.type === 'redirect' ? 'redir' : l.type;
      lines.push(`    type: ${yamlType}`);
      lines.push(`    listen: ${l.listen || '0.0.0.0'}`);
      lines.push(`    port: ${l.port}`);
      if (l.proxy) {
        lines.push(`    proxy: ${yamlSafeString(l.proxy)}`);
      }
      if (
        l.type === 'mixed' ||
        l.type === 'socks' ||
        l.type === 'tproxy' ||
        l.type === 'shadowsocks'
      ) {
        if (typeof l.udp === 'boolean') {
          lines.push(`    udp: ${l.udp}`);
        }
      }
      if (l.type === 'shadowsocks') {
        lines.push(`    cipher: ${l.cipher || 'aes-256-gcm'}`);
        lines.push(`    password: ${yamlSafeString(l.password || '')}`);
      }
      if (
        (l.type === 'mixed' || l.type === 'socks' || l.type === 'http') &&
        l.users &&
        l.users.length > 0
      ) {
        lines.push('    users:');
        for (const u of l.users) {
          lines.push(`      - username: ${yamlSafeString(u.username)}`);
          lines.push(`        password: ${yamlSafeString(u.password)}`);
        }
      }
      if (typeof l.routingMark === 'number' && l.routingMark > 0) {
        lines.push(`    routing-mark: ${l.routingMark}`);
      }
    }
    lines.push('');
  }

  return lines.join('\n').trimEnd();
}

/**
 * Предупреждение парсера конфига. Строка — готовый текст (легаси), объект —
 * код локализации + параметры интерполяции, резолвится через $t у потребителя.
 */
export type MihomoWarning = string | { code: string; params?: Record<string, string | number> };

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
  externalControllerType?: 'unix' | 'tcp';
  externalControllerTarget?: string;
  mihomoProviders: any[];
  listeners: Listener[];
  listenersRaw: string | null;
  listenersReadOnly: boolean;
  warnings: MihomoWarning[];
}

export function parseListenersSection(rawBlock: string): {
  listeners: Listener[];
  unrecognized: boolean;
  rawText: string;
} {
  if (!rawBlock || rawBlock.trim() === '') {
    return { listeners: [], unrecognized: false, rawText: '' };
  }

  const lines = rawBlock.split('\n');

  // Find the first element and determine base indent
  let firstElementIdx = -1;
  let baseIndent = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();
    if (trimmed === '' || trimmed.startsWith('#')) continue;
    const match = line.match(/^(\s*)-\s+/);
    if (match) {
      firstElementIdx = i;
      baseIndent = match[1].length;
      break;
    } else {
      // Non-comment, non-empty line before any element start
      return { listeners: [], unrecognized: true, rawText: rawBlock };
    }
  }

  if (firstElementIdx === -1) {
    return { listeners: [], unrecognized: true, rawText: rawBlock };
  }

  const chunks: string[][] = [];
  let currentChunk: string[] | null = null;

  for (let i = firstElementIdx; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();
    if (trimmed === '' || trimmed.startsWith('#')) {
      if (currentChunk) currentChunk.push(line);
      continue;
    }

    // Check if line starts an element at baseIndent
    const isNewElement =
      line.length >= baseIndent + 2 &&
      line.slice(0, baseIndent).trim() === '' &&
      line.slice(baseIndent).startsWith('- ');

    if (isNewElement) {
      if (currentChunk) {
        chunks.push(currentChunk);
      }
      currentChunk = [line];
    } else {
      if (currentChunk) {
        currentChunk.push(line);
      }
    }
  }
  if (currentChunk) {
    chunks.push(currentChunk);
  }

  const listeners: Listener[] = [];

  for (const chunk of chunks) {
    let name = '';
    let type = '';
    let listen = '0.0.0.0';
    let port = '';
    let proxy: string | undefined;
    let udp: boolean | undefined;
    let cipher: string | undefined;
    let password: string | undefined;
    let routingMark: number | undefined;
    const users: ListenerUser[] = [];

    let inUsers = false;
    let currentUser: ListenerUser | null = null;

    for (const rawLine of chunk) {
      const trimmed = rawLine.trim();
      if (trimmed === '' || trimmed.startsWith('#')) continue;

      const indent = rawLine.search(/\S/);

      if (inUsers) {
        // Match user item start: e.g. "- username: alice" or "- password: ..." or "- "
        const userItemMatch = rawLine.match(/^\s*-\s*(.*)$/);
        if (userItemMatch) {
          if (currentUser && (currentUser.username || currentUser.password)) {
            users.push(currentUser);
          }
          currentUser = { username: '', password: '' };
          const rest = userItemMatch[1].trim();
          if (rest) {
            const m = rest.match(/^([a-zA-Z0-9_-]+):\s*(.*)$/);
            if (m) {
              if (m[1] === 'username') currentUser.username = unquote(m[2]);
              else if (m[1] === 'password') currentUser.password = unquote(m[2]);
            }
          }
          continue;
        }

        // Left the users list if we reached another property at listener indent
        const isListenerPropertyIndent = indent > 0 && indent <= baseIndent + 2;
        if (isListenerPropertyIndent) {
          inUsers = false;
          if (currentUser && (currentUser.username || currentUser.password)) {
            users.push(currentUser);
            currentUser = null;
          }
        } else {
          // Inside a user item: e.g. "password: secret"
          const userPropMatch = trimmed.match(/^([a-zA-Z0-9_-]+):\s*(.*)$/);
          if (userPropMatch && currentUser) {
            const k = userPropMatch[1];
            const v = unquote(userPropMatch[2]);
            if (k === 'username') currentUser.username = v;
            else if (k === 'password') currentUser.password = v;
            continue;
          }
        }
      }

      if (trimmed.startsWith('users:')) {
        inUsers = true;
        continue;
      }

      const lineWithoutDash = rawLine.replace(/^\s*-\s+/, '  ');
      const match = lineWithoutDash.match(/^\s*([a-zA-Z0-9_-]+):\s*(.*)$/);
      if (match) {
        const key = match[1];
        const val = unquote(match[2].trim());
        if (key === 'name') {
          name = val;
        } else if (key === 'type') {
          type = val;
        } else if (key === 'listen') {
          listen = val;
        } else if (key === 'port') {
          port = val;
        } else if (key === 'proxy') {
          proxy = val;
        } else if (key === 'udp') {
          const lower = val.toLowerCase();
          udp = lower === 'true' || lower === 'yes';
        } else if (key === 'cipher') {
          cipher = val;
        } else if (key === 'password') {
          password = val;
        } else if (key === 'routing-mark' || key === 'routingMark') {
          const m = parseInt(val, 10);
          if (!isNaN(m) && m > 0) routingMark = m;
        }
      }
    }

    if (currentUser && (currentUser.username || currentUser.password)) {
      users.push(currentUser);
      currentUser = null;
    }

    if (!type) {
      return { listeners: [], unrecognized: true, rawText: rawBlock };
    }

    const isSupported =
      type === 'redir' || type === 'redirect' || BUILDER_LISTENER_TYPES.includes(type);

    if (!isSupported) {
      return { listeners: [], unrecognized: true, rawText: rawBlock };
    }

    const normalizedType: ListenerType =
      type === 'redir' || type === 'redirect' ? 'redirect' : (type as ListenerType);

    listeners.push({
      id: crypto.randomUUID(),
      name,
      type: normalizedType,
      listen: listen || '0.0.0.0',
      port,
      ...(proxy ? { proxy } : {}),
      ...(typeof udp === 'boolean' ? { udp } : {}),
      ...(cipher ? { cipher } : {}),
      ...(password ? { password } : {}),
      ...(typeof routingMark === 'number' ? { routingMark } : {}),
      ...(users.length > 0 ? { users } : {})
    });
  }

  return { listeners, unrecognized: false, rawText: '' };
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
    externalControllerType: 'unix',
    externalControllerTarget: '/opt/var/run/mihomo.sock',
    mihomoProviders: [],
    listeners: [],
    listenersRaw: null,
    listenersReadOnly: false,
    warnings: []
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
            sec !== 'external-controller-unix' &&
            sec !== 'proxy-providers' &&
            sec !== 'listeners'
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
          } else if (sec === 'external-controller-unix') {
            const valMatch = line.match(/external-controller-unix:\s*["']?([^#"']+)["']?/);
            if (valMatch) {
              parsed.externalControllerType = 'unix';
              parsed.externalControllerTarget = valMatch[1].trim();
            }
          } else if (sec === 'external-controller') {
            const valMatch = line.match(/external-controller:\s*["']?([^#"']+)["']?/);
            if (valMatch) {
              parsed.externalControllerType = 'tcp';
              parsed.externalControllerTarget = valMatch[1].trim();
            }
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
          const parsedType = unquote(typeMatch[1]);
          currentGroup.type = parsedType;
          if (parsedType === 'relay') {
            parsed.warnings = parsed.warnings || [];
            parsed.warnings.push({
              code: 'mihomo.warnings.relay_deprecated',
              params: { name: currentGroup.name || 'unnamed' }
            });
          }
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

        if (currentParentKey && lineIndent <= parentKeyIndent) {
          currentParentKey = '';
          parentKeyIndent = 0;
        }

        if (trimmed.endsWith(':') && !trimmed.startsWith('-')) {
          currentParentKey = trimmed.slice(0, -1).trim();
          parentKeyIndent = lineIndent;
          continue;
        }

        if (currentParentKey === 'amnezia-wg-option') {
          const jcMatch = trimmed.match(/^jc:\s*(.+)$/);
          if (jcMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgJc = parseInt(unquote(jcMatch[1]), 10);
            continue;
          }
          const jminMatch = trimmed.match(/^jmin:\s*(.+)$/);
          if (jminMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgJmin = parseInt(unquote(jminMatch[1]), 10);
            continue;
          }
          const jmaxMatch = trimmed.match(/^jmax:\s*(.+)$/);
          if (jmaxMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgJmax = parseInt(unquote(jmaxMatch[1]), 10);
            continue;
          }
          const s1Match = trimmed.match(/^s1:\s*(.+)$/);
          if (s1Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgS1 = parseInt(unquote(s1Match[1]), 10);
            continue;
          }
          const s2Match = trimmed.match(/^s2:\s*(.+)$/);
          if (s2Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgS2 = parseInt(unquote(s2Match[1]), 10);
            continue;
          }
          const s3Match = trimmed.match(/^s3:\s*(.+)$/);
          if (s3Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgS3 = parseInt(unquote(s3Match[1]), 10);
            continue;
          }
          const s4Match = trimmed.match(/^s4:\s*(.+)$/);
          if (s4Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgS4 = parseInt(unquote(s4Match[1]), 10);
            continue;
          }

          const parseH = (rawVal: string): number | string => {
            const v = unquote(rawVal).trim();
            if (v.includes('-')) return v;
            const n = parseInt(v, 10);
            return isNaN(n) ? v : n;
          };

          const h1Match = trimmed.match(/^h1:\s*(.+)$/);
          if (h1Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgH1 = parseH(h1Match[1]);
            continue;
          }
          const h2Match = trimmed.match(/^h2:\s*(.+)$/);
          if (h2Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgH2 = parseH(h2Match[1]);
            continue;
          }
          const h3Match = trimmed.match(/^h3:\s*(.+)$/);
          if (h3Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgH3 = parseH(h3Match[1]);
            continue;
          }
          const h4Match = trimmed.match(/^h4:\s*(.+)$/);
          if (h4Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgH4 = parseH(h4Match[1]);
            continue;
          }

          const verMatch = trimmed.match(/^version:\s*(.+)$/);
          if (verMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgVersion = unquote(verMatch[1]);
            continue;
          }
          const hpkMatch = trimmed.match(/^header-protection-key:\s*(.+)$/);
          if (hpkMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgHeaderProtectionKey = unquote(hpkMatch[1]);
            continue;
          }
          const i1Match = trimmed.match(/^i1:\s*(.+)$/);
          if (i1Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgI1 = unquote(i1Match[1]).trim();
            continue;
          }
          const i2Match = trimmed.match(/^i2:\s*(.+)$/);
          if (i2Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgI2 = unquote(i2Match[1]).trim();
            continue;
          }
          const i3Match = trimmed.match(/^i3:\s*(.+)$/);
          if (i3Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgI3 = unquote(i3Match[1]).trim();
            continue;
          }
          const i4Match = trimmed.match(/^i4:\s*(.+)$/);
          if (i4Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgI4 = unquote(i4Match[1]).trim();
            continue;
          }
          const i5Match = trimmed.match(/^i5:\s*(.+)$/);
          if (i5Match) {
            currentProxy.awgEnabled = true;
            currentProxy.awgI5 = unquote(i5Match[1]).trim();
            continue;
          }
          const cpaMatch = trimmed.match(/^content-padding-addition:\s*(.+)$/);
          if (cpaMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgContentPaddingAddition = parseInt(unquote(cpaMatch[1]), 10);
            continue;
          }
          const rtMatch = trimmed.match(/^random-trailers:\s*(.+)$/);
          if (rtMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgRandomTrailers = unquote(rtMatch[1]).trim() === 'true';
            continue;
          }
          const dcMatch = trimmed.match(/^disable-cookies:\s*(.+)$/);
          if (dcMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgDisableCookies = unquote(dcMatch[1]).trim() === 'true';
            continue;
          }
          const ratMatch = trimmed.match(/^rekey-after-time:\s*(.+)$/);
          if (ratMatch) {
            currentProxy.awgEnabled = true;
            currentProxy.awgRekeyAfterTime = parseInt(unquote(ratMatch[1]), 10);
            continue;
          }
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
        const privateKeyMatch = trimmed.match(/^private-key:\s*(.+)$/);
        if (privateKeyMatch && !currentParentKey) {
          currentProxy.wgPrivateKey = unquote(privateKeyMatch[1]);
          continue;
        }
        const publicKeyMatch = trimmed.match(/^public-key:\s*(.+)$/);
        if (publicKeyMatch) {
          if (currentParentKey === 'reality-opts') {
            currentProxy.publicKey = unquote(publicKeyMatch[1]);
          } else if (!currentParentKey) {
            currentProxy.wgPublicKey = unquote(publicKeyMatch[1]);
          }
          continue;
        }
        const ipMatch = trimmed.match(/^ip:\s*(.+)$/);
        if (ipMatch && !currentParentKey) {
          currentProxy.wgIp = unquote(ipMatch[1]);
          continue;
        }
        const presharedKeyMatch = trimmed.match(/^pre-shared-key:\s*(.+)$/);
        if (presharedKeyMatch && !currentParentKey) {
          currentProxy.wgPresharedKey = unquote(presharedKeyMatch[1]);
          continue;
        }
        const mtuMatch = trimmed.match(/^mtu:\s*(.+)$/);
        if (mtuMatch && !currentParentKey) {
          currentProxy.wgMtu = parseInt(unquote(mtuMatch[1]), 10);
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

    const listenersSec = findTopLevelSection(lines, 'listeners');
    if (listenersSec.start !== -1) {
      const rawBlock = lines.slice(listenersSec.start + 1, listenersSec.end).join('\n');
      const parsedListeners = parseListenersSection(rawBlock);
      parsed.listeners = parsedListeners.listeners;
      parsed.listenersReadOnly = parsedListeners.unrecognized;
      parsed.listenersRaw = parsedListeners.unrecognized ? parsedListeners.rawText : null;
    }
  } catch (e) {
    console.error('Failed to parse Mihomo config:', e);
  }

  return parsed;
}
