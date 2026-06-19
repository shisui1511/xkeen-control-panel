<script lang="ts">
  import { onMount } from 'svelte';
  import { currentLang, t } from './i18n';
  import { capabilities, showToast, fetchCapabilities } from './stores';
  import { parseValidationError } from './lib/errorParser';

  export let onSwitchTab: (tab: string) => void = () => {};
  export let selectedFile: string = '';
  export let onInsertIntoEditor: (content: string) => void = () => {};
  export let embedded: boolean = false;
  export let initialPreset: string = '';
  export let invalidateCache: boolean = false;

  type ProxyType = 'vless' | 'hysteria2' | 'tuic' | 'ss' | 'vmess';
  type GroupType = 'select' | 'url-test' | 'fallback' | 'load-balance';
  type RuleType =
    | 'DOMAIN-SUFFIX'
    | 'DOMAIN-KEYWORD'
    | 'DOMAIN'
    | 'GEOIP'
    | 'GEOSITE'
    | 'IP-CIDR'
    | 'PROCESS-NAME'
    | 'RULE-SET'
    | 'MATCH';

  interface Proxy {
    id: string;
    name: string;
    type: ProxyType;
    server: string;
    port: number;
    // vless/vmess
    uuid?: string;
    flow?: string;
    // reality
    publicKey?: string;
    shortId?: string;
    servername?: string;
    // hy2
    password?: string;
    sni?: string;
    skipCertVerify?: boolean;
    obfsType?: 'none' | 'simple';
    obfsPassword?: string;
    // tuic
    congestion?: string;
    // ss
    cipher?: string;
    // vmess ws
    network?: string;
    wsPath?: string;
    tls?: boolean;
    fingerprint?: string;
    alterID?: number;
  }

  interface ProxyGroup {
    id: string;
    name: string;
    type: GroupType;
    proxies: string[];
    includeAll?: boolean;
    url?: string;
    interval?: number;
    excludeFilter?: string;
    icon?: string;
    enabled?: boolean;
    hidden?: boolean;         // NEW (D-02): hides group from Mihomo selector UI
    tolerance?: number;       // NEW (D-02): latency tolerance ms for url-test
    maxFailedTimes?: number;  // NEW (D-02): maps to YAML key max-failed-times
  }

  interface Rule {
    id: string;
    type: RuleType;
    value: string;
    outbound: string;
  }

  interface DNSConfig {
    enabled: boolean;
    nameservers: string[];
    fallback: string[];
    enhancedMode: 'fake-ip' | 'redir-host';
    fakeIPRange: string;
  }

  interface TUNConfig {
    enabled: boolean;
    stack: 'system' | 'gvisor' | 'mixed';
    autoRoute: boolean;
    autoDetectInterface: boolean;
    dnsHijack: string[];
  }

  // State
  let activeSection: 'proxies' | 'groups' | 'rules' | 'dns' | 'tun' | 'rulesets' = 'proxies';
  let proxies: Proxy[] = [];
  let groups: ProxyGroup[] = [];
  let rules: Rule[] = [];
  let activePreset: string = '';
  let activeRuleProvider: 'none' | 'zkeen' | 'metacubex' = 'none';
  let subscriptions: any[] = [];
  $: hasXraySubscriptions = subscriptions.some((s) => s.type !== 'mihomo');
  let hasZkeenGeodata = false;
  let existingTproxyPort: number | null = null;
  let existingRedirPort: number | null = null;
  let dns: DNSConfig = {
    enabled: false,
    nameservers: ['https://doh.pub/dns-query', '223.5.5.5'],
    fallback: ['https://8.8.8.8/dns-query', '1.1.1.1'],
    enhancedMode: 'fake-ip',
    fakeIPRange: '198.18.0.1/16'
  };
  let tun: TUNConfig = {
    enabled: false,
    stack: 'mixed',
    autoRoute: true,
    autoDetectInterface: true,
    dnsHijack: ['any:53']
  };
  let preservedKeys: string[] = [];
  let sniffer = {
    enabled: false,
    sniffHttp: true,
    sniffTls: true,
    sniffQuic: true
  };
  let canUndo = false;
  let isDirty = false;
  function checkUndo() {
    canUndo = !!localStorage.getItem('xcp_prev_mihomo_yaml');
  }


  // Import Node states
  let showImportModal = false;
  let importLink = '';
  let importTag = '';
  let importStep = 1; // 1: Input link, 2: Preview & Confirm tag
  let importLoading = false;
  let importNodes: { link: string; outbound: any; tag: string; rowError?: string | null }[] = [];
  let importErrorMsg = '';

  // Form visibility
  let showProxyForm = false;
  let showGroupForm = false;
  let showRuleForm = false;

  // New proxy form
  let np: Omit<Proxy, 'id'> = newProxyDefaults('vless');
  function newProxyDefaults(type: ProxyType): Omit<Proxy, 'id'> {
    return {
      name: '',
      type,
      server: '',
      port: 443,
      uuid: crypto.randomUUID(),
      flow: 'xtls-rprx-vision',
      publicKey: '',
      shortId: '',
      servername: 'www.apple.com',
      password: '',
      sni: '',
      skipCertVerify: false,
      obfsType: 'none',
      obfsPassword: '',
      congestion: 'bbr',
      cipher: 'aes-256-gcm',
      network: 'ws',
      wsPath: '/',
      tls: true,
      fingerprint: 'chrome'
    };
  }
  $: if (np.type)
    np = { ...newProxyDefaults(np.type), name: np.name, server: np.server, port: np.port };

  // New group form
  let ng: Omit<ProxyGroup, 'id'> = {
    name: '',
    type: 'select',
    proxies: [],
    includeAll: false,
    url: 'https://www.gstatic.com/generate_204',
    interval: 300
  };
  let ngProxyInput = '';

  // New rule form
  let nr: Omit<Rule, 'id'> = { type: 'DOMAIN-SUFFIX', value: '', outbound: 'DIRECT' };

  // ── Rule-provider URL constants ─────────────────────────────────────────
  const META_BASE_URL = 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo';
  const METACUBEX_BASE = 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo';

  interface RuleProvider {
    name: string;
    url: string;
    behavior: string;
    outbound: string;
    format: string;
    payload?: string[];
  }

  const ZKEEN_RULE_PROVIDERS: RuleProvider[] = [
    {
      name: 'adlist@domain',
      url: 'https://github.com/zxc-rv/ad-filter/releases/latest/download/adlist.mrs',
      behavior: 'domain',
      format: 'mrs',
      outbound: 'REJECT'
    },
    {
      name: 'category-ai@domain',
      url: `${METACUBEX_BASE}/geosite/category-ai-!cn.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'AI'
    },
    {
      name: 'steam@domain',
      url: `${METACUBEX_BASE}/geosite/steam.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'Steam'
    },
    {
      name: 'spotify@domain',
      url: `${METACUBEX_BASE}/geosite/spotify.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'Spotify'
    },
    {
      name: 'speedtest@domain',
      url: `${METACUBEX_BASE}/geosite/speedtest.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'Speedtest'
    },
    {
      name: 'reddit@domain',
      url: `${METACUBEX_BASE}/geosite/reddit.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'Reddit'
    },
    {
      name: 'twitch@domain',
      url: `${METACUBEX_BASE}/geosite/twitch.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'Twitch'
    },
    {
      name: 'twitter@domain',
      url: `${METACUBEX_BASE}/geosite/twitter.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'Twitter'
    },
    {
      name: 'meta@domain',
      url: `${METACUBEX_BASE}/geosite/meta.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'Meta'
    },
    {
      name: 'discord@classical',
      url: `${METACUBEX_BASE}/classical/discord.txt`,
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
      url: `${METACUBEX_BASE}/geoip/telegram.mrs`,
      behavior: 'ipcidr',
      format: 'mrs',
      outbound: 'Telegram'
    },
    {
      name: 'github@domain',
      url: `${METACUBEX_BASE}/geosite/github.mrs`,
      behavior: 'domain',
      format: 'mrs',
      outbound: 'GitHub'
    },
    {
      name: 'private@ip',
      url: `${METACUBEX_BASE}/geoip/private.mrs`,
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

  const RULE_PROVIDERS: Record<
    string,
    Array<{
      name: string;
      url: string;
      behavior: string;
      outbound: string;
      format?: string;
      payload?: string[];
    }>
  > = {
    zkeen: ZKEEN_RULE_PROVIDERS
  };

  const ZKEEN_16_GROUPS: Omit<ProxyGroup, 'id'>[] = [
    {
      name: 'Заблок. сервисы',
      type: 'select',
      includeAll: true,
      proxies: ['Fallback', 'Fastest'] as string[],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Reject.png'
    },
    {
      name: 'Fallback',
      type: 'fallback',
      includeAll: true,
      proxies: [] as string[],
      hidden: true,
      url: 'https://www.gstatic.com/generate_204',
      interval: 300,
      maxFailedTimes: 3,
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Auto.png'
    },
    {
      name: 'Fastest',
      type: 'url-test',
      includeAll: true,
      proxies: [] as string[],
      hidden: true,
      url: 'https://www.gstatic.com/generate_204',
      interval: 300,
      maxFailedTimes: 3,
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Available.png'
    },
    {
      name: 'YouTube',
      type: 'select',
      includeAll: true,
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/YouTube.png'
    },
    {
      name: 'Discord',
      type: 'select',
      includeAll: true,
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Discord.png'
    },
    {
      name: 'Twitch',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Заблок. сервисы', 'Fallback', 'Fastest'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Twitch.png'
    },
    {
      name: 'Reddit',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Заблок. сервисы', 'Fallback', 'Fastest'],
      icon: 'https://www.redditstatic.com/shreddit/assets/favicon/192x192.png'
    },
    {
      name: 'Meta',
      type: 'select',
      includeAll: true,
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://github.com/zxc-rv/assets/raw/main/group-icons/meta.png'
    },
    {
      name: 'Spotify',
      type: 'select',
      includeAll: true,
      excludeFilter: '🇷🇺',
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Spotify.png'
    },
    {
      name: 'Speedtest',
      type: 'select',
      includeAll: true,
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Speedtest.png'
    },
    {
      name: 'Telegram',
      type: 'select',
      includeAll: true,
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Telegram.png'
    },
    {
      name: 'Steam',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Заблок. сервисы', 'Fallback', 'Fastest'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Steam.png'
    },
    {
      name: 'CDN',
      type: 'select',
      includeAll: true,
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://www.svgrepo.com/show/396567/globe-with-meridians.svg'
    },
    {
      name: 'Google',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Заблок. сервисы', 'Fallback', 'Fastest'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Google_Search.png'
    },
    {
      name: 'GitHub',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Заблок. сервисы', 'Fallback', 'Fastest'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/GitHub.png'
    },
    {
      name: 'AI',
      type: 'select',
      includeAll: true,
      excludeFilter: '🇷🇺',
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Bot.png'
    },
    {
      name: 'Twitter',
      type: 'select',
      includeAll: true,
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Twitter.png'
    },
    {
      name: 'QUIC',
      type: 'select',
      includeAll: false,
      proxies: ['REJECT', 'PASS'],
      icon: 'https://github.com/zxc-rv/assets/raw/main/group-icons/quic.png'
    }
  ];

  const META_RULE_SETS_BY_CATEGORY: Record<
    string,
    Array<{ id: string; label: string; type: 'geosite' | 'geoip'; defaultOutbound: string }>
  > = {
    'Социальные сети': [
      { id: 'youtube', label: 'YouTube', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'telegram', label: 'Telegram', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'discord', label: 'Discord', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'twitter', label: 'Twitter/X', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'instagram', label: 'Instagram', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'reddit', label: 'Reddit', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'vk', label: 'VK', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'tiktok', label: 'TikTok', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'twitch', label: 'Twitch', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'facebook', label: 'Facebook', type: 'geosite', defaultOutbound: 'Proxy' }
    ],
    Сервисы: [
      { id: 'spotify', label: 'Spotify', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'steam', label: 'Steam', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'github', label: 'GitHub', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'openai', label: 'OpenAI', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'netflix', label: 'Netflix', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'google', label: 'Google', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'amazon', label: 'Amazon', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'speedtest', label: 'Speedtest', type: 'geosite', defaultOutbound: 'Proxy' }
    ],
    'Сети/CDN': [
      { id: 'cloudflare', label: 'Cloudflare', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'akamai', label: 'Akamai', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'fastly', label: 'Fastly', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'digitalocean', label: 'DigitalOcean', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'private', label: 'Private Network', type: 'geoip', defaultOutbound: 'DIRECT' },
      { id: 'telegram', label: 'Telegram IP', type: 'geoip', defaultOutbound: 'Proxy' }
    ],
    Блокировки: [
      {
        id: 'category-ads-all',
        label: 'Ads & Trackers',
        type: 'geosite',
        defaultOutbound: 'REJECT'
      },
      {
        id: 'category-ai-!cn',
        label: 'AI Services (non-CN)',
        type: 'geosite',
        defaultOutbound: 'Proxy'
      },
      {
        id: 'category-anticensorship',
        label: 'Anti-Censorship',
        type: 'geosite',
        defaultOutbound: 'Proxy'
      }
    ]
  };

  let selectedMetaRuleSets: Map<string, string> = new Map();

  function buildMetaRuleSetUrl(id: string, type: 'geosite' | 'geoip'): string {
    return `${META_BASE_URL}/${type}/${id}.mrs`;
  }

  // ── Presets ──────────────────────────────────────────────────────────────
  function applyPreset(id: string, silent = false) {
    activePreset = id;
    validationError = '';

    if (schema && schema.mihomo && schema.mihomo.presets) {
      const p = schema.mihomo.presets.find((x: any) => x.id === id);
      if (p) {
        activeRuleProvider = p.active_rule_provider || 'none';
        groups = (p.groups || []).map((g: any) => ({
          id: crypto.randomUUID(),
          name: g.name,
          type: g.type || 'select',
          proxies: g.name === 'Selective' || g.name === 'Proxy'
            ? ['DIRECT', ...proxies.map((pr) => pr.name)]
            : [...(g.proxies || [])],
          includeAll: g.include_all ?? false,
          excludeFilter: g.exclude_filter || '',
          url: g.url || 'https://www.gstatic.com/generate_204',
          interval: g.interval || 300,
          icon: g.icon || '',
          enabled: true,
          hidden: g.hidden ?? false,
          tolerance: g.tolerance ?? undefined,
          maxFailedTimes: g.max_failed_times ?? undefined
        }));
        rules = (p.rules || []).map((r: any) => ({
          id: crypto.randomUUID(),
          type: r.type,
          value: r.value,
          outbound: r.outbound
        }));
        selectedMetaRuleSets = new Map();
        if (p.selected_meta_rule_sets) {
          for (const [k, v] of Object.entries(p.selected_meta_rule_sets)) {
            selectedMetaRuleSets.set(k, v as string);
          }
        }
        if (!silent) {
          showToast('success', $t('editor.preset_applied'));
        }
        return;
      }
    }

    if (id === 'rule-based') {
      groups = [
        {
          id: crypto.randomUUID(),
          name: 'Selective',
          type: 'select',
          proxies: ['DIRECT', ...proxies.map((p) => p.name)],
          includeAll: true,
          url: 'https://www.gstatic.com/generate_204',
          interval: 300
        }
      ];
      rules = [];
      activeRuleProvider = 'metacubex';
      selectedMetaRuleSets = new Map([
        ['category-ads-all|geosite', 'REJECT'],
        ['telegram|geoip', 'Selective'],
        ['private|geoip', 'DIRECT']
      ]);
    } else if (id === 'global-proxy') {
      groups = [
        {
          id: crypto.randomUUID(),
          name: 'Proxy',
          type: 'select',
          proxies: ['DIRECT', ...proxies.map((p) => p.name)],
          includeAll: true,
          url: 'https://www.gstatic.com/generate_204',
          interval: 300
        }
      ];
      rules = [
        { id: crypto.randomUUID(), type: 'GEOIP', value: 'private', outbound: 'DIRECT' },
        { id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'Proxy' }
      ];
      activeRuleProvider = 'none';
      selectedMetaRuleSets = new Map();
    } else if (id === 'zkeen-selective') {
      groups = ZKEEN_16_GROUPS.map((g) => ({
        ...g,
        id: crypto.randomUUID(),
        enabled: true
      }));
      rules = [];
      activeRuleProvider = 'zkeen';
      selectedMetaRuleSets = new Map();
    } else if (id === 'only-blocked') {
      groups = [
        {
          id: crypto.randomUUID(),
          name: 'Selective',
          type: 'select',
          proxies: ['DIRECT', ...proxies.map((p) => p.name)],
          includeAll: true,
          url: 'https://www.gstatic.com/generate_204',
          interval: 300,
          icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Proxy.png'
        }
      ];
      rules = [
        { id: crypto.randomUUID(), type: 'RULE-SET', value: 'refilter@domain', outbound: 'Selective' },
        { id: crypto.randomUUID(), type: 'RULE-SET', value: 'private@ip', outbound: 'DIRECT' },
        { id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'DIRECT' }
      ];
      activeRuleProvider = 'zkeen';
      selectedMetaRuleSets = new Map();
    }
    if (!silent) {
      showToast('success', $t('editor.preset_applied'));
    }
  }

  // ── Import proxies from subscriptions ───────────────────────────────────
  async function loadSubscriptions() {
    try {
      const res = await fetch('/api/subscriptions');
      if (!res.ok) return;
      const subs = await res.json();
      if (Array.isArray(subs)) {
        subscriptions = subs.filter((s) => s.enabled);
      } else {
        subscriptions = [];
      }
    } catch (e: any) {
      console.error(e);
    }
  }

  // ── Import proxies from subscriptions ───────────────────────────────────
  async function loadSubscriptionProxies() {
    try {
      const res = await fetch('/api/subscriptions');
      if (!res.ok) return;
      const subs = await res.json();
      if (Array.isArray(subs)) {
        subscriptions = subs.filter((s) => s.enabled);
      } else {
        subscriptions = [];
      }
      if (!Array.isArray(subs) || subs.length === 0) {
        showToast('info', $t('editor.import_proxies_empty'));
        return;
      }
      let imported = 0;
      for (const sub of subs) {
        if (!sub.enabled) continue;
        if (sub.type === 'mihomo') continue; // Игнорируем Mihomo-подписки
        const nr = await fetch(`/api/subscriptions/nodes?id=${sub.id}`);
        if (!nr.ok) continue;
        const nodes: any[] = await nr.json();
        if (!nodes || nodes.length === 0) continue;
        const mapped = nodes.map((n: any) => {
          const serverRaw: string = n.server || '';
          const lastColon = serverRaw.lastIndexOf(':');
          const server = lastColon > 0 ? serverRaw.substring(0, lastColon) : serverRaw;
          const portStr = lastColon > 0 ? serverRaw.substring(lastColon + 1) : '443';
          const port = parseInt(portStr) || 443;
          return {
            id: crypto.randomUUID(),
            name: n.name || n.tag || `proxy-${imported}`,
            type: (n.protocol || 'vless') as ProxyType,
            server,
            port,
            uuid: n.uuid || '',
            password: n.password || '',
            flow: n.flow || '',
            publicKey: n.public_key || '',
            shortId: n.short_id || '',
            servername: n.servername || '',
            fingerprint: n.fingerprint || '',
            wsPath: n.ws_path || '',
            cipher: n.cipher || '',
            sni: n.sni || '',
            congestion: n.congestion || '',
            alterID: n.alter_id || 0,
            tls: n.security === 'tls' || n.security === 'reality',
            skipCertVerify: n.insecure || false,
            obfsType: (n.obfs_type || 'none') as any,
            obfsPassword: n.obfs_password || ''
          };
        });
        const existingNames = new Set(proxies.map((p) => p.name));
        const uniqueMapped = mapped.filter((n) => !existingNames.has(n.name));
        proxies = [...proxies, ...uniqueMapped];
        imported += uniqueMapped.length;
      }
      if (imported > 0) {
        showToast('success', $t('editor.import_proxies_done'));
      } else {
        showToast('info', $t('editor.import_proxies_empty'));
      }
    } catch (e: any) {
      showToast('error', $t('editor.import_proxies_error'));
    }
  }

  function generateUniqueProxyName(baseName: string, existing: string[]): string {
    let name = baseName.trim() || 'proxy';
    if (!existing.includes(name)) {
      return name;
    }
    let counter = 1;
    while (existing.includes(`${name}-${counter}`)) {
      counter++;
    }
    return `${name}-${counter}`;
  }

  function openImportModal() {
    showImportModal = true;
    importLink = '';
    importTag = '';
    importStep = 1;
    importLoading = false;
    importNodes = [];
    importErrorMsg = '';
  }

  function closeImportModal() {
    showImportModal = false;
  }

  function getNodeServer(node: any): string {
    if (!node || !node.settings) return '';
    if (node.settings.vnext && node.settings.vnext[0]) {
      return node.settings.vnext[0].address || '';
    }
    if (node.settings.servers && node.settings.servers[0]) {
      return node.settings.servers[0].address || '';
    }
    return '';
  }

  function getNodePort(node: any): string {
    if (!node || !node.settings) return '';
    if (node.settings.vnext && node.settings.vnext[0]) {
      return String(node.settings.vnext[0].port || '');
    }
    if (node.settings.servers && node.settings.servers[0]) {
      return String(node.settings.servers[0].port || '');
    }
    return '';
  }

  async function parseImportLink() {
    const trimmed = importLink.trim();
    if (!trimmed) {
      importErrorMsg = $t('subscr.import_error_empty');
      return;
    }

    const lines = trimmed
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l.length > 0);

    importErrorMsg = '';
    importLoading = true;

    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/outbound/parse', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ links: lines })
      });

      const data = await res.json();
      if (!res.ok) {
        importErrorMsg = data.error || $t('subscr.import_error_invalid');
        return;
      }

      if (data.data && data.data.length > 0) {
        const newImportNodes = [];
        const existingNames = proxies.map((p) => p.name);

        for (let i = 0; i < data.data.length; i++) {
          const result = data.data[i];
          if (result.outbound) {
            const baseName = result.outbound.tag || 'proxy';
            const uniqueName = generateUniqueProxyName(baseName, existingNames);
            existingNames.push(uniqueName);
            newImportNodes.push({
              link: lines[i],
              outbound: result.outbound,
              tag: uniqueName,
              rowError: result.error || null
            });
          } else {
            newImportNodes.push({
              link: lines[i],
              outbound: null,
              tag: '',
              rowError: result.error || $t('subscr.import_error_invalid')
            });
          }
        }

        importNodes = newImportNodes;
        importStep = 2;
      } else {
        importErrorMsg = $t('subscr.import_error_invalid');
      }
    } catch (e: any) {
      importErrorMsg = e.message || $t('subscr.import_error_invalid');
    } finally {
      importLoading = false;
    }
  }

  function mapParsedOutboundToMihomoProxy(parsed: any, customTag?: string): Proxy {
    const proto = parsed.protocol || 'vless';
    const tag = customTag || parsed.tag || 'imported-node';
    const p: Proxy = {
      id: crypto.randomUUID(),
      name: tag,
      type: 'vless',
      server: '',
      port: 443
    };

    if (proto === 'vless' || proto === 'vmess') {
      p.type = proto;
      const vnext = parsed.settings?.vnext?.[0];
      if (vnext) {
        p.server = vnext.address || '';
        p.port = vnext.port || 443;
        p.uuid = vnext.users?.[0]?.id || '';
        p.flow = vnext.users?.[0]?.flow || '';
      }
      const ss = parsed.streamSettings;
      if (ss) {
        p.tls = ss.security === 'tls' || ss.security === 'reality';
        p.network = ss.network || 'tcp';
        if (ss.wsSettings?.path) {
          p.wsPath = ss.wsSettings.path;
        }
        if (ss.security === 'reality') {
          const rOpts = ss.realitySettings;
          if (rOpts) {
            p.publicKey = rOpts.publicKey || '';
            p.shortId = rOpts.shortId || '';
            p.servername = rOpts.serverName || '';
            p.fingerprint = rOpts.fingerprint || 'chrome';
          }
        } else if (ss.security === 'tls') {
          const tOpts = ss.tlsSettings;
          if (tOpts) {
            p.servername = tOpts.serverName || '';
          }
        }
      }
    } else if (proto === 'shadowsocks' || proto === 'ss') {
      p.type = 'ss';
      const server = parsed.settings?.servers?.[0];
      if (server) {
        p.server = server.address || '';
        p.port = server.port || 443;
        p.cipher = server.method || 'aes-256-gcm';
        p.password = server.password || '';
      }
    } else if (proto === 'hysteria2' || proto === 'hy2') {
      p.type = 'hysteria2';
      const server = parsed.settings?.servers?.[0];
      if (server) {
        p.server = server.address || '';
        p.port = server.port || 443;
        p.password = server.password || '';
        p.sni = parsed.streamSettings?.tlsSettings?.serverName || '';
        p.skipCertVerify = parsed.streamSettings?.tlsSettings?.allowInsecure || false;
        const hy2Settings = parsed.settings?.hysteria2Settings;
        if (hy2Settings?.obfs) {
          p.obfsType = hy2Settings.obfs.type || 'none';
          p.obfsPassword = hy2Settings.obfs.password || '';
        }
      }
    } else if (proto === 'tuic') {
      p.type = 'tuic';
      const server = parsed.settings?.servers?.[0];
      if (server) {
        p.server = server.address || '';
        p.port = server.port || 443;
        p.uuid = server.uuid || '';
        p.password = server.password || '';
        p.congestion = 'bbr';
        p.sni = parsed.streamSettings?.tlsSettings?.serverName || '';
      }
    }
    return p;
  }

  function confirmImportNode() {
    try {
      const validNodes = importNodes.filter((n) => !n.rowError);
      const mappedList: Proxy[] = [];
      let skippedCount = importNodes.length - validNodes.length;

      for (const item of validNodes) {
        const p = mapParsedOutboundToMihomoProxy(item.outbound, item.tag.trim());
        if (p && p.server) {
          mappedList.push(p);
        } else {
          skippedCount++;
        }
      }

      proxies = [...proxies, ...mappedList];

      if (skippedCount > 0) {
        showToast('warning', $t('subscr.partial_map_warning'));
      }
      if (mappedList.length > 0) {
        showToast('success', $t('subscr.import_success', { count: mappedList.length }));
      }

      showImportModal = false;
    } catch (e: any) {
      importErrorMsg = e.message || $t('subscr.import_error');
    }
  }

  function populateMihomoFromYAML(text: string) {
    if (!text || text.trim() === '') {
      applyPreset('zkeen-selective', true);
      return;
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

      const parsedGroups: ProxyGroup[] = [];
      const parsedProxies: Proxy[] = [];
      const parsedRules: Rule[] = [];
      selectedMetaRuleSets = new Map();
      activeRuleProvider = 'none';
      preservedKeys = [];
      dns = {
        enabled: false,
        nameservers: ['https://doh.pub/dns-query', '223.5.5.5'],
        fallback: ['https://8.8.8.8/dns-query', '1.1.1.1'],
        enhancedMode: 'fake-ip',
        fakeIPRange: '198.18.0.1/16'
      };
      tun = {
        enabled: false,
        stack: 'mixed',
        autoRoute: true,
        autoDetectInterface: true,
        dnsHijack: ['any:53']
      };
      sniffer = {
        enabled: false,
        sniffHttp: false,
        sniffTls: false,
        sniffQuic: false
      };


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
              sec !== 'sniffer'
            ) {
              if (!preservedKeys.includes(sec)) {
                preservedKeys = [...preservedKeys, sec];
              }
            }
          }

          continue;
        }

        if (inGroups) {
          if (line.startsWith('  -') || line.startsWith(' -') || trimmed.startsWith('-')) {
            if (currentGroup) {
              parsedGroups.push(currentGroup);
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

          const proxiesInlineMatch = trimmed.match(/^proxies:\s*\[(.*)\]$/);
          if (proxiesInlineMatch) {
            currentGroup.proxies = proxiesInlineMatch[1]
              .split(',')
              .map((p) => unquote(p.trim()))
              .filter((p) => p.length > 0);
            inProxiesList = false;
            continue;
          }

          if (trimmed.startsWith('proxies:')) {
            inProxiesList = true;
            continue;
          }

          if (inProxiesList) {
            const proxyItemMatch = trimmed.match(/^-\s*(.+)$/);
            if (proxyItemMatch) {
              currentGroup.proxies.push(unquote(proxyItemMatch[1]));
            } else if (trimmed !== '' && !trimmed.startsWith('#') && !line.startsWith('    ')) {
              inProxiesList = false;
            }
          }
        }

        if (inProxies) {
          if (line.startsWith('  -') || line.startsWith(' -') || trimmed.startsWith('-')) {
            if (currentProxy) {
              parsedProxies.push(currentProxy);
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
        }

        if (inDNS) {
          if (trimmed.startsWith('nameserver:')) {
            inNameservers = true;
            inFallback = false;
            dns.nameservers = [];
            continue;
          }
          if (trimmed.startsWith('fallback:')) {
            inFallback = true;
            inNameservers = false;
            dns.fallback = [];
            continue;
          }

          const enableMatch = trimmed.match(/^enable:\s*(.+)$/);
          if (enableMatch) {
            dns.enabled = unquote(enableMatch[1]) === 'true';
            inNameservers = false;
            inFallback = false;
            continue;
          }
          const enhancedModeMatch = trimmed.match(/^enhanced-mode:\s*(.+)$/);
          if (enhancedModeMatch) {
            dns.enhancedMode = unquote(enhancedModeMatch[1]) as any;
            inNameservers = false;
            inFallback = false;
            continue;
          }
          const fakeIpRangeMatch = trimmed.match(/^fake-ip-range:\s*(.+)$/);
          if (fakeIpRangeMatch) {
            dns.fakeIPRange = unquote(fakeIpRangeMatch[1]);
            inNameservers = false;
            inFallback = false;
            continue;
          }

          if (inNameservers) {
            const listMatch = trimmed.match(/^-\s*(.+)$/);
            if (listMatch) {
              dns.nameservers = [...dns.nameservers, unquote(listMatch[1])];
            } else if (trimmed !== '' && !trimmed.startsWith('#') && !line.startsWith('    ')) {
              inNameservers = false;
            }
          }
          if (inFallback) {
            const listMatch = trimmed.match(/^-\s*(.+)$/);
            if (listMatch) {
              dns.fallback = [...dns.fallback, unquote(listMatch[1])];
            } else if (trimmed !== '' && !trimmed.startsWith('#') && !line.startsWith('    ')) {
              inFallback = false;
            }
          }
        }

        if (inTUN) {
          if (trimmed.startsWith('dns-hijack:')) {
            inDnsHijack = true;
            tun.dnsHijack = [];
            continue;
          }

          const enableMatch = trimmed.match(/^enable:\s*(.+)$/);
          if (enableMatch) {
            tun.enabled = unquote(enableMatch[1]) === 'true';
            inDnsHijack = false;
            continue;
          }
          const stackMatch = trimmed.match(/^stack:\s*(.+)$/);
          if (stackMatch) {
            tun.stack = unquote(stackMatch[1]) as any;
            inDnsHijack = false;
            continue;
          }
          const autoRouteMatch = trimmed.match(/^auto-route:\s*(.+)$/);
          if (autoRouteMatch) {
            tun.autoRoute = unquote(autoRouteMatch[1]) === 'true';
            inDnsHijack = false;
            continue;
          }
          const autoDetectMatch = trimmed.match(/^auto-detect-interface:\s*(.+)$/);
          if (autoDetectMatch) {
            tun.autoDetectInterface = unquote(autoDetectMatch[1]) === 'true';
            inDnsHijack = false;
            continue;
          }

          if (inDnsHijack) {
            const listMatch = trimmed.match(/^-\s*(.+)$/);
            if (listMatch) {
              tun.dnsHijack = [...tun.dnsHijack, unquote(listMatch[1])];
            } else if (trimmed !== '' && !trimmed.startsWith('#') && !line.startsWith('    ')) {
              inDnsHijack = false;
            }
          }
        }

        if (inSniffer) {
          const enableMatch = trimmed.match(/^enable:\s*(.+)$/);
          if (enableMatch) {
            sniffer.enabled = unquote(enableMatch[1]) === 'true';
            continue;
          }
          if (trimmed.includes('HTTP:')) {
            sniffer.sniffHttp = true;
          }
          if (trimmed.includes('TLS:')) {
            sniffer.sniffTls = true;
          }
          if (trimmed.includes('QUIC:')) {
            sniffer.sniffQuic = true;
          }
        }


        if (inRules) {
          const ruleMatch = trimmed.match(
            /^-\s*([A-Z0-9-]+)\s*,\s*([^,]+)\s*,\s*([^,]+?)(?:\s*,\s*([^,]+))?$/
          );
          const matchRuleMatch = trimmed.match(/^-\s*MATCH\s*,\s*(.+)$/);
          const ruleSetMatch = trimmed.match(/^-\s*RULE-SET\s*,\s*([^,]+)\s*,\s*(.+)$/);

          if (ruleMatch) {
            const rType = ruleMatch[1] as RuleType;
            const rValue = ruleMatch[2];
            const rOutbound = ruleMatch[4] ? `${ruleMatch[3]},${ruleMatch[4]}` : ruleMatch[3];
            parsedRules.push({
              id: crypto.randomUUID(),
              type: rType,
              value: rValue,
              outbound: rOutbound
            });
          } else if (matchRuleMatch) {
            const matchOutbound = matchRuleMatch[1];
            parsedRules.push({
              id: crypto.randomUUID(),
              type: 'MATCH',
              value: '',
              outbound: matchOutbound
            });
          } else if (ruleSetMatch) {
            const rsName = ruleSetMatch[1];
            const rsOutbound = ruleSetMatch[2];
            if (rsName.startsWith('geosite-') || rsName.startsWith('geoip-')) {
              const type = rsName.startsWith('geosite-') ? 'geosite' : 'geoip';
              const id = rsName.replace(/^geosite-/, '').replace(/^geoip-/, '');
              selectedMetaRuleSets.set(`${id}|${type}`, rsOutbound);
              activeRuleProvider = 'metacubex';
            } else {
              parsedRules.push({
                id: crypto.randomUUID(),
                type: 'RULE-SET',
                value: rsName,
                outbound: rsOutbound
              });
            }
          }
        }
      }

      if (currentGroup) {
        parsedGroups.push(currentGroup);
      }
      if (currentProxy) {
        parsedProxies.push(currentProxy);
      }

      if (parsedGroups.length > 0) {
        groups = parsedGroups;
        const hasZkeenGroup = parsedGroups.some((g) => g.name === 'Заблок. сервисы');
        if (hasZkeenGroup) {
          activePreset = 'zkeen-selective';
          activeRuleProvider = 'zkeen';
          groups = groups.map((g) => {
            const zG = ZKEEN_16_GROUPS.find((zg) => zg.name === g.name);
            if (zG) {
              return {
                ...g,
                icon: zG.icon,
                excludeFilter: zG.excludeFilter,
                enabled: g.enabled !== false
              };
            }
            return g;
          });
        }
      }
      if (parsedProxies.length > 0) {
        proxies = parsedProxies;
      }
      if (parsedRules.length > 0) {
        rules = parsedRules;
      }
      if (parsedGroups.length === 0 && parsedProxies.length === 0) {
        applyPreset('zkeen-selective', true);
      }
    } catch (err: any) {
      showToast(
        'warning',
        $currentLang === 'ru'
          ? 'Не удалось прочитать существующий config.yaml. Начинаем с чистого листа.'
          : 'Could not parse existing config.yaml. Starting fresh.'
      );
      applyPreset('zkeen-selective', true);
    }
  }

  function unquote(str: string): string {
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
    } else {
      str = str.split('#')[0].trim();
    }
    return str;
  }

  let configLoadedForPath = '';

  async function loadConfig(path: string, force = false) {
    if (!path) return;
    if (configLoadedForPath === path && !force) return;
    configLoadedForPath = path;
    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`);
      if (!res.ok) {
        const errText = await res.text();
        throw new Error(errText || `HTTP ${res.status}`);
      }
      const text = await res.text();
      populateMihomoFromYAML(text);
    } catch (e: any) {
      showToast('error', `Ошибка загрузки конфига: ${e.message}`);
    }
    await loadSubscriptions();
  }

  async function checkZkeenGeodata() {
    try {
      const res = await fetch('/api/dat/tags?name=geosite.dat');
      if (res.ok) {
        const json = await res.json();
        const tags = json.tags || [];
        const tagNames = tags.map((t: any) => t.tag.toLowerCase());
        hasZkeenGeodata =
          tagNames.includes('domains') &&
          tagNames.includes('other') &&
          tagNames.includes('politic');
      }
    } catch (e) {
      console.error('Failed to load geosite.dat tags:', e);
      hasZkeenGeodata = false;
    }
  }

  onMount(async () => {
    await loadSchema();
    await loadConfig(selectedFile || '/opt/etc/mihomo/config.yaml', true);
    await checkZkeenGeodata();
    await loadSubscriptions();
    checkUndo();
  });

  $: {
    if (selectedFile) {
      loadConfig(selectedFile);
    }
  }

  let prevInvalidateCache = false;
  $: if (invalidateCache && !prevInvalidateCache) {
    prevInvalidateCache = true;
    configLoadedForPath = '';
    loadConfig(selectedFile || '/opt/etc/mihomo/config.yaml', true);
  } else if (!invalidateCache) {
    prevInvalidateCache = false;
  }

  function sanitizeProxyName(name: string): { name: string; sanitized: boolean } {
    const original = name;
    const cleaned = name.replace(/[\n\r\t]/g, ' ').replace(/\s+/g, ' ').trim();
    return { name: cleaned, sanitized: cleaned !== original };
  }

  function addProxy() {
    if (!np.name.trim() || !np.server.trim()) return;
    if (np.type === 'hysteria2' && np.obfsType === 'simple' && !np.obfsPassword?.trim()) {
      const isRu = $currentLang === 'ru';
      showToast('error', isRu ? 'Пароль обфускации обязателен при типе simple' : 'Obfuscation password is required when type is simple');
      return;
    }
    const { name: cleanName, sanitized } = sanitizeProxyName(np.name);
    if (sanitized) {
      showToast('info', $t('editor.proxy_name_sanitized') || 'Имя прокси очищено от спецсимволов');
    }
    proxies = [...proxies, { ...np, name: cleanName, id: crypto.randomUUID() }];
    showProxyForm = false;
    np = newProxyDefaults('vless');
  }

  function removeProxy(id: string) {
    proxies = proxies.filter((p) => p.id !== id);
  }

  function addGroup() {
    if (!ng.name.trim()) return;
    groups = [...groups, { ...ng, id: crypto.randomUUID(), proxies: [...ng.proxies] }];
    showGroupForm = false;
    ng = {
      name: '',
      type: 'select',
      proxies: [],
      url: 'https://www.gstatic.com/generate_204',
      interval: 300
    };
    ngProxyInput = '';
  }

  function removeGroup(id: string) {
    groups = groups.filter((g) => g.id !== id);
  }

  function addRule() {
    if (nr.type !== 'MATCH' && !nr.value.trim()) return;
    rules = [...rules, { ...nr, id: crypto.randomUUID() }];
    showRuleForm = false;
    nr = { type: 'DOMAIN-SUFFIX', value: '', outbound: 'DIRECT' };
  }

  function removeRule(id: string) {
    rules = rules.filter((r) => r.id !== id);
  }

  function moveRule(id: string, dir: -1 | 1) {
    const idx = rules.findIndex((r) => r.id === id);
    if (idx < 0) return;
    const next = idx + dir;
    if (next < 0 || next >= rules.length) return;
    const arr = [...rules];
    [arr[idx], arr[next]] = [arr[next], arr[idx]];
    rules = arr;
  }

  function addGroupProxy() {
    const v = ngProxyInput.trim();
    if (v && !ng.proxies.includes(v)) {
      ng = { ...ng, proxies: [...ng.proxies, v] };
    }
    ngProxyInput = '';
  }

  // ── YAML generation ─────────────────────────────────────────────────────

  function yamlSafeString(v: string | number | boolean): string {
    if (typeof v !== 'string') return String(v);
    const escaped = v
      .replace(/\\/g, '\\\\')
      .replace(/"/g, '\\"')
      .replace(/\n/g, '\\n')
      .replace(/\t/g, '\\t');
    return `"${escaped}"`;
  }

  function sanitizeUrl(url: string): string {
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

  function generateYAML(): string {
    const lines: string[] = [];

    // external-controller must be first field (required for Clash API on port 9090)
    lines.push('external-controller: 0.0.0.0:9090');
    lines.push('');

    // System ports from XKeen (preserve existing values, fall back to defaults)
    lines.push(`tproxy-port: ${existingTproxyPort ?? 1181}`);
    lines.push(`redir-port: ${existingRedirPort ?? 1182}`);
    lines.push('');

    // Proxy-providers (if we have subscriptions)
    if (subscriptions.length > 0) {
      lines.push('proxy-providers:');
      for (const [i, sub] of subscriptions.entries()) {
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
        const mihomoVersion = $capabilities?.kernels?.mihomo?.version || '1.18.10';
        const ua = `mihomo/${mihomoVersion}`;
        const subHwid = sub.hwid_token || $capabilities?.global_hwid || '';
        
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

    if (activeRuleProvider === 'metacubex' && selectedMetaRuleSets.size > 0) {
      for (const [key, outbound] of selectedMetaRuleSets) {
        const [id, type] = key.split('|') as [string, 'geosite' | 'geoip'];
        const behavior = type === 'geoip' ? 'ipcidr' : 'domain';
        lines.push(`  ${type}-${id.replace(/[^a-z0-9-]/g, '-')}:`);
        lines.push(`    type: http`);
        lines.push(`    format: mrs`);
        lines.push(`    behavior: ${behavior}`);
        lines.push(`    url: ${yamlSafeString(sanitizeUrl(buildMetaRuleSetUrl(id, type)))}`);
        lines.push(`    interval: 86400`);
      }
    } else if (activeRuleProvider !== 'none' && activeRuleProvider !== 'metacubex') {
      const providers = activeRuleProvider === 'zkeen' ? ruleProviders : RULE_PROVIDERS[activeRuleProvider];
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
    if (proxies.length > 0) {
      lines.push('proxies:');
      for (const p of proxies) {
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
      if (activeRuleProvider === 'zkeen') {
        const primaryOutbound = outbound.split(',')[0].trim();
        const g = groups.find((x) => x.name === primaryOutbound);
        if (g && g.enabled === false) return false;
      }
      return true;
    };

    // Proxy groups
    if (groups.length > 0) {
      lines.push('proxy-groups:');
      for (const g of groups) {
        if (activeRuleProvider === 'zkeen' && g.enabled === false) {
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
      rules.length > 0 ||
      activeRuleProvider === 'zkeen' ||
      (activeRuleProvider === 'metacubex' && selectedMetaRuleSets.size > 0);
    if (hasRules) {
      lines.push('rules:');
      if (activeRuleProvider === 'zkeen') {
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
          ...(hasZkeenGeodata
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
        for (const r of rules) {
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
        if (activeRuleProvider === 'metacubex') {
          for (const [key, outbound] of selectedMetaRuleSets) {
            const [id, type] = key.split('|') as [string, 'geosite' | 'geoip'];
            lines.push(`  - RULE-SET,${type}-${id.replace(/[^a-z0-9-]/g, '-')},${outbound}`);
          }
        } else if (activeRuleProvider !== 'none') {
          const providers = activeRuleProvider === 'zkeen' ? ruleProviders : RULE_PROVIDERS[activeRuleProvider];
          if (providers) {
            for (const rp of providers) {
              if (rp.name === 'quic@inline' || rp.name === 'netbios@inline') {
                continue;
              }
              lines.push(`  - RULE-SET,${rp.name},${rp.outbound}`);
            }
          }
        }
        for (const r of rules) {
          if (r.type === 'MATCH') {
            lines.push(`  - MATCH,${r.outbound}`);
          } else {
            lines.push(`  - ${r.type},${r.value},${r.outbound}`);
          }
        }
        // If only rule-providers active but no manual rules, add a default MATCH
        if (
          rules.length === 0 &&
          activeRuleProvider === 'metacubex' &&
          selectedMetaRuleSets.size > 0
        ) {
          lines.push(`  - MATCH,DIRECT`);
        }
      }
      lines.push('');
    }

    // Sniffer
    lines.push('sniffer:');
    lines.push(`  enable: ${sniffer.enabled}`);
    if (sniffer.enabled) {
      lines.push('  sniff:');
      if (sniffer.sniffHttp) lines.push('    HTTP: { ports: [80, 8080] }');
      if (sniffer.sniffTls) lines.push('    TLS: { ports: [443, 8443] }');
      if (sniffer.sniffQuic) lines.push('    QUIC: { ports: [443, 8443] }');
      lines.push('  skip-dst-address: [rule-set:telegram@ipcidr]');
    }
    lines.push('');

    // DNS
    lines.push('dns:');
    lines.push(`  enable: ${dns.enabled}`);
    if (dns.enabled) {
      lines.push(`  enhanced-mode: ${dns.enhancedMode}`);
      if (dns.enhancedMode === 'fake-ip') lines.push(`  fake-ip-range: ${dns.fakeIPRange}`);
      lines.push(`  nameserver:`);
      for (const ns of dns.nameservers) lines.push(`    - ${yamlSafeString(ns)}`);
      if (dns.fallback.length > 0) {
        lines.push(`  fallback:`);
        for (const fb of dns.fallback) lines.push(`    - ${yamlSafeString(fb)}`);
      }
    }
    lines.push('');

    // TUN
    lines.push('tun:');
    lines.push(`  enable: ${tun.enabled}`);
    if (tun.enabled) {
      lines.push(`  stack: ${tun.stack}`);
      lines.push(`  auto-route: ${tun.autoRoute}`);
      lines.push(`  auto-detect-interface: ${tun.autoDetectInterface}`);
      if (tun.dnsHijack.length > 0) {
        lines.push(`  dns-hijack:`);
        for (const d of tun.dnsHijack) lines.push(`    - ${yamlSafeString(d)}`);
      }
    }
    lines.push('');

    return lines.join('\n').trimEnd();
  }

  let yaml = '';
  $: {
    // Explicit deps so Svelte 5 legacy mode tracks them across the function call
    void proxies;
    void groups;
    void rules;
    void activeRuleProvider;
    void selectedMetaRuleSets;
    void subscriptions;
    void dns.enabled;
    void dns.nameservers;
    void dns.fallback;
    void tun.enabled;
    void tun.stack;
    void hasZkeenGeodata;
    void sniffer.enabled;
    void sniffer.sniffHttp;
    void sniffer.sniffTls;
    void sniffer.sniffQuic;
    yaml = generateYAML();
  }

  async function copyYAML() {
    await navigator.clipboard.writeText(yaml);
    showToast('success', $currentLang === 'ru' ? 'YAML скопирован' : 'YAML copied');
  }

  function openInEditor() {
    if (onInsertIntoEditor) {
      onInsertIntoEditor(yaml);
    } else {
      onSwitchTab('editor');
    }
  }

  const ru = $currentLang === 'ru';

  let dnsRedirectLoading = false;

  async function enableDNSRedirect() {
    dnsRedirectLoading = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/service/dns-redirect', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ enabled: true })
      });
      if (res.ok) {
        showToast('success', ru ? 'Перехват DNS успешно включен' : 'DNS Interception enabled successfully');
        await fetchCapabilities();
      } else {
        const text = await res.text();
        showToast('error', text || (ru ? 'Не удалось включить перехват DNS' : 'Failed to enable DNS Interception'));
      }
    } catch (err: any) {
      showToast('error', err.message || String(err));
    } finally {
      dnsRedirectLoading = false;
    }
  }

  const PROXY_TYPES: ProxyType[] = ['vless', 'hysteria2', 'tuic', 'ss', 'vmess'];
  const GROUP_TYPES: GroupType[] = ['select', 'url-test', 'fallback', 'load-balance'];
  const RULE_TYPES: RuleType[] = [
    'DOMAIN-SUFFIX',
    'DOMAIN-KEYWORD',
    'DOMAIN',
    'GEOIP',
    'GEOSITE',
    'IP-CIDR',
    'PROCESS-NAME',
    'RULE-SET',
    'MATCH'
  ];
  const CIPHERS = ['aes-256-gcm', 'aes-128-gcm', 'chacha20-poly1305', '2022-blake3-aes-256-gcm'];

  let allProxyNames: string[] = [];
  $: allProxyNames = [
    'DIRECT',
    'REJECT',
    ...proxies.map((p) => p.name),
    ...groups.map((g) => g.name)
  ];

  // Dynamic tabs calculation and auto-switch
  $: tabs = [
    ['proxies', ru ? 'Прокси' : 'Proxies'],
    ['groups', ru ? 'Группы' : 'Groups'],
    ...(activeRuleProvider === 'metacubex' ? [['rulesets', ru ? 'Наборы' : 'Rule Sets']] : []),
    ['rules', ru ? 'Правила' : 'Rules'],
    ['dns', 'DNS'],
    ['tun', 'TUN']
  ];

  $: if (
    activeRuleProvider === 'metacubex' &&
    activeSection !== 'rulesets' &&
    activeSection !== 'proxies' &&
    activeSection !== 'groups' &&
    activeSection !== 'rules' &&
    activeSection !== 'dns' &&
    activeSection !== 'tun'
  ) {
    activeSection = 'rulesets';
  }

  let showApplyConfirm = false;
  let applyLoading = false;

  function extractSection(yamlText: string, sectionName: string): string {
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

  let schema: any = null;
  let schemaLoading = true;
  let schemaError = '';
  let validationError = '';

  async function loadSchema() {
    schemaLoading = true;
    schemaError = '';
    try {
      const res = await fetch('/api/assets/definition');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      schema = await res.json();
    } catch (e: any) {
      schemaError = e.message || 'Unknown error';
    } finally {
      schemaLoading = false;
    }
  }

  $: ruleProviders = (schema && schema.mihomo && schema.mihomo.rule_providers)
    ? schema.mihomo.rule_providers
    : ZKEEN_RULE_PROVIDERS;

  function findTopLevelSection(lines: string[], sectionName: string) {
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

  function replaceMihomoTopLevelSection(content: string, sectionName: string, newContent: string): string {
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
    out.push(...lines.slice(0, start));
    out.push(sectionName + ':');
    if (newLines.length > 0) {
      out.push(...newLines);
    }
    if (end < lines.length) {
      out.push(...lines.slice(end));
    }
    return out.join('\n');
  }

  async function handleApplyMihomo() {
    if (proxies.length === 0) {
      if (!confirm($t('editor.empty_proxies_warning') || 'No proxy servers configured.')) {
        return;
      }
    }
    if (!showApplyConfirm) {
      showApplyConfirm = true;
      return;
    }
    showApplyConfirm = false;
    applyLoading = true;

    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const path = selectedFile || '/opt/etc/mihomo/config.yaml';

      // Save previous state to localStorage for Undo
      const readRes = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`);
      if (readRes.ok) {
        const currentYAML = await readRes.text();
        localStorage.setItem('xcp_prev_mihomo_yaml', currentYAML);
        checkUndo();
      }

      const yamlContent = generateYAML();
      const sections: Record<string, string> = {
        'rule-providers': extractSection(yamlContent, 'rule-providers'),
        'proxy-groups': extractSection(yamlContent, 'proxy-groups'),
        rules: extractSection(yamlContent, 'rules'),
        proxies: extractSection(yamlContent, 'proxies'),
        dns: extractSection(yamlContent, 'dns'),
        tun: extractSection(yamlContent, 'tun'),
        'proxy-providers': extractSection(yamlContent, 'proxy-providers')
      };

      const readCurrentRes = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`);
      if (!readCurrentRes.ok) {
        throw new Error(`Failed to read current config: HTTP ${readCurrentRes.status}`);
      }
      let currentYAML = await readCurrentRes.text();

      for (const [sectionName, newSecContent] of Object.entries(sections)) {
        currentYAML = replaceMihomoTopLevelSection(currentYAML, sectionName, newSecContent);
      }

      validationError = '';

      const mergeRes = await fetch('/api/config/mihomo-merge', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ path, sections })
      });

      if (!mergeRes.ok) {
        if (mergeRes.status === 422) {
          const resData = await mergeRes.json();
          validationError = resData.error || 'Unknown validation error';
          showToast('error', $t('editor.validation_failed'));
          applyLoading = false;
          return;
        }
        const errorText = await mergeRes.text();
        throw new Error(errorText || 'Failed to merge config');
      }

      const restartRes = await fetch('/api/service/control?action=restart', {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      });

      if (!restartRes.ok) {
        throw new Error('Failed to restart service');
      }

      showToast(
        'success',
        ru
          ? 'Конфигурация Mihomo обновлена и перезапущена'
          : 'Mihomo configuration updated and restarted'
      );
    } catch (err: any) {
      console.error(err);
      showToast('error', err.message || (ru ? 'Ошибка сохранения' : 'Save error'));
    } finally {
      applyLoading = false;
    }
  }

  async function handleUndo() {
    const prevYAML = localStorage.getItem('xcp_prev_mihomo_yaml');
    if (!prevYAML) return;
    try {
      applyLoading = true;
      const csrfToken = localStorage.getItem('csrf_token');
      const path = selectedFile || '/opt/etc/mihomo/config.yaml';

      // Save back to file
      const saveRes = await fetch(`/api/config/save?path=${encodeURIComponent(path)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: prevYAML
      });

      if (!saveRes.ok) {
        throw new Error('Failed to save rolled back config');
      }

      // Re-populate state
      populateMihomoFromYAML(prevYAML);
      isDirty = false;

      // Restart service
      const restartRes = await fetch('/api/service/control?action=restart', {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      });
      if (!restartRes.ok) {
        throw new Error('Failed to restart service');
      }

      showToast('success', $t('editor.undo_success') || 'Last change reverted successfully');
      checkUndo();
    } catch (e: any) {
      showToast('error', `Undo failed: ${e.message}`);
    } finally {
      applyLoading = false;
    }
  }
</script>

<div class="container">
  {#if schemaLoading}
    <div class="loading-state-block" style="padding: 48px; text-align: center; color: var(--fg-secondary);">
      <div class="spinner" style="width: 24px; height: 24px; border: 2px solid var(--accent); border-top-color: transparent; border-radius: 50%; animation: spin 1s linear infinite; margin: 0 auto 12px;"></div>
      <p>{$t('editor.loading_definition')}</p>
    </div>
  {:else if schemaError}
    <div class="error-state-block" style="padding: 48px; text-align: center;">
      <div class="error-icon" style="color: var(--danger); font-size: 24px; margin-bottom: 12px;">⚠</div>
      <p style="color: var(--danger); margin-bottom: 16px;">{$t('editor.definition_load_error', { error: schemaError })}</p>
      <button class="btn btn-secondary" on:click={loadSchema}>{ru ? 'Повторить попытку' : 'Retry'}</button>
    </div>
  {:else}
    {#if !embedded}
    <div class="page-head">
      <div>
        <div class="crumbs">
          {ru ? 'Сервисы' : 'Services'} <span class="crumb-sep">/</span>
          {ru ? 'Генератор Mihomo' : 'Mihomo Generator'}
        </div>
        <h1>{ru ? 'Визуальный генератор Mihomo' : 'Mihomo Visual Generator'}</h1>
        <p class="sub">
          {ru
            ? 'Сборка proxy, proxy-group, rules, DNS и TUN без ручного редактирования YAML.'
            : 'Build proxy, proxy-group, rules, DNS and TUN without hand-editing YAML.'}
        </p>
      </div>
      <div class="ph-actions">
        <button class="btn btn-secondary" on:click={openInEditor}>
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right:5px"
            ><path d="M12 20h9" /><path
              d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"
            /></svg
          >
          {#if selectedFile}
            {ru ? 'Вставить в редактор' : 'Insert into Editor'}
          {:else}
            {ru ? 'Открыть в редакторе' : 'Open in Editor'}
          {/if}
        </button>
        <button class="btn btn-primary" on:click={copyYAML} disabled={!yaml}>
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right:5px"
            ><rect x="9" y="9" width="13" height="13" rx="2" /><path
              d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
            /></svg
          >
          {ru ? 'Копировать YAML' : 'Copy YAML'}
        </button>
      </div>
    </div>
  {/if}

  {#if preservedKeys.length > 0}
    <div class="alert alert-warning" style="margin: 0 0 16px 0;" role="status">
      <span aria-hidden="true">⚠️</span>
      <div>
        <strong>{$t('editor.constructor_merge_warning_title')}</strong>
        <div style="margin-top: 2px;">
          {$t('editor.constructor_merge_warning_body', { keys: preservedKeys.join(', ') })}
        </div>
      </div>
    </div>
  {/if}

  <div class="gen-layout">
    <!-- Left: sections -->
    <div class="gen-left">
      <!-- Scenario selection -->
      <div
        class="constructor-scenario-bar"
        style="display: flex; align-items: center; gap: 10px; margin-bottom: 16px;"
      >
        <span class="scenario-label">{$t('editor.constructor_scenario')}:</span>
        <select
          id="preset-select"
          class="form-select preset-select"
          style="max-width: 250px;"
          data-testid="preset-select"
          value={activePreset}
          on:change={(e) => {
            const val = e.currentTarget.value;
            applyPreset(val);
            if (val === 'rule-based') {
              activeSection = 'rulesets';
            } else if (val === 'zkeen-selective') {
              activeSection = 'groups';
            }
          }}
        >
          <option value="">-- {$t('editor.constructor_scenario')} --</option>
          {#if schema && schema.mihomo && schema.mihomo.presets}
            {#each schema.mihomo.presets as p}
              <option value={p.id}>{$t(p.name)}</option>
            {/each}
          {:else}
            <option value="rule-based">{$t('editor.scenario_rule_based')}</option>
            <option value="global-proxy">{$t('editor.scenario_global_proxy')}</option>
            <option value="zkeen-selective">{$t('editor.scenario_zkeen_selective')}</option>
            <option value="only-blocked">{$t('preset.only-blocked')}</option>
          {/if}
        </select>
      </div>

      {#if activePreset === 'zkeen-selective' && !hasZkeenGeodata}
        <div class="alert alert-warning" style="margin-bottom: 16px; padding: 8px 12px; font-size: 13px; display: flex; align-items: center; gap: 8px; border-radius: var(--radius-sm);">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="flex-shrink: 0;"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
          <span>{$t('editor.requires_zkeen_geodata')}</span>
        </div>
      {/if}

      <!-- Rule providers -->
      <div class="rule-providers-row">
        <label class="form-label" for="rp-select">{$t('editor.constructor_rule_providers')}:</label>
        <select
          id="rp-select"
          class="form-select rp-select"
          bind:value={activeRuleProvider}
          on:change={(e) => {
            if (e.currentTarget.value === 'metacubex') {
              activeSection = 'rulesets';
            }
          }}
        >
          <option value="none">{$t('editor.rp_none')}</option>
          <option value="zkeen">{$t('editor.rp_zkeen')}</option>
          <option value="metacubex">{$t('editor.rp_metacubex')}</option>
        </select>
      </div>

      <!-- Section tabs -->
      <div class="sec-tabs">
        {#each tabs as [id, label]}
          <button
            class="sec-tab"
            class:active={activeSection === id}
            on:click={() => {
              activeSection = id as typeof activeSection;
              showProxyForm = false;
              showGroupForm = false;
              showRuleForm = false;
            }}
          >
            {label}
            {#if id === 'proxies' && proxies.length > 0}<span class="sec-count"
                >{proxies.length}</span
              >{/if}
            {#if id === 'groups' && groups.length > 0}<span class="sec-count">{groups.length}</span
              >{/if}
            {#if id === 'rulesets' && selectedMetaRuleSets.size > 0}<span class="sec-count"
                >{selectedMetaRuleSets.size}</span
              >{/if}
            {#if id === 'rules' && rules.length > 0}<span class="sec-count">{rules.length}</span
              >{/if}
            {#if id === 'dns' && dns.enabled}<span class="sec-dot"></span>{/if}
            {#if id === 'tun' && tun.enabled}<span class="sec-dot"></span>{/if}
          </button>
        {/each}
      </div>

      <!-- PROXIES -->
      {#if activeSection === 'proxies'}
        <div class="sec-body">
          {#each proxies as p (p.id)}
            <div class="item-row">
              <span class="item-badge type-{p.type}">{p.type}</span>
              <span class="item-name">{p.name}</span>
              <span class="item-meta">{p.server}:{p.port}</span>
              <button
                class="item-del"
                on:click={() => removeProxy(p.id)}
                title={ru ? 'Удалить' : 'Remove'}>✕</button
              >
            </div>
          {/each}

          {#if showProxyForm}
            <div class="form-card">
              <div class="form-row">
                <label class="form-label">{ru ? 'Тип' : 'Type'}</label>
                <select class="form-select" bind:value={np.type}>
                  {#each PROXY_TYPES as t}<option value={t}>{t}</option>{/each}
                </select>
              </div>
              <div class="form-row">
                <label class="form-label">{ru ? 'Имя' : 'Name'}</label>
                <input class="form-input" bind:value={np.name} placeholder="my-proxy" />
              </div>
              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Сервер' : 'Server'}</label>
                  <input class="form-input" bind:value={np.server} placeholder="example.com" />
                </div>
                <div class="form-col form-col-sm">
                  <label class="form-label">{ru ? 'Порт' : 'Port'}</label>
                  <input
                    class="form-input"
                    type="number"
                    bind:value={np.port}
                    min="1"
                    max="65535"
                  />
                </div>
              </div>

              {#if np.type === 'vless'}
                <div class="form-row">
                  <label class="form-label">UUID</label>
                  <div class="input-with-btn">
                    <input class="form-input" bind:value={np.uuid} placeholder="uuid" />
                    <button
                      class="btn-gen"
                      on:click={() => (np.uuid = crypto.randomUUID())}
                      title="Generate">⟳</button
                    >
                  </div>
                </div>
                <div class="form-row">
                  <label class="form-label">Reality Public Key</label>
                  <input class="form-input" bind:value={np.publicKey} placeholder="public-key" />
                </div>
                <div class="form-row2">
                  <div class="form-col">
                    <label class="form-label">Short ID</label>
                    <input class="form-input" bind:value={np.shortId} placeholder="short-id" />
                  </div>
                  <div class="form-col">
                    <label class="form-label">SNI</label>
                    <input
                      class="form-input"
                      bind:value={np.servername}
                      placeholder="www.apple.com"
                    />
                  </div>
                </div>
              {:else if np.type === 'hysteria2'}
                <div class="form-row">
                  <label class="form-label">{ru ? 'Пароль' : 'Password'}</label>
                  <input class="form-input" bind:value={np.password} placeholder="password" />
                </div>
                <div class="form-row">
                  <label class="form-label">SNI</label>
                  <input class="form-input" bind:value={np.sni} placeholder="example.com" />
                </div>
                <div class="form-row">
                  <label class="form-label">{$t('editor.obfsType')}</label>
                  <select class="form-select" bind:value={np.obfsType}>
                    <option value="none">{$t('editor.none')}</option>
                    <option value="simple">{$t('editor.simple')}</option>
                  </select>
                </div>
                {#if np.obfsType === 'simple'}
                  <div class="form-row">
                    <label class="form-label">{$t('editor.obfsPassword')}</label>
                    <input class="form-input" bind:value={np.obfsPassword} placeholder="obfs password" />
                  </div>
                {/if}
                <div class="form-row">
                  <label
                    class="toggle-label"
                    style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
                  >
                    <input type="checkbox" bind:checked={np.skipCertVerify} />
                    <span>{$t('editor.skipCertVerify')}</span>
                  </label>
                </div>
              {:else if np.type === 'tuic'}
                <div class="form-row">
                  <label class="form-label">UUID</label>
                  <div class="input-with-btn">
                    <input class="form-input" bind:value={np.uuid} placeholder="uuid" />
                    <button
                      class="btn-gen"
                      on:click={() => (np.uuid = crypto.randomUUID())}
                      title="Generate">⟳</button
                    >
                  </div>
                </div>
                <div class="form-row">
                  <label class="form-label">{ru ? 'Пароль' : 'Password'}</label>
                  <input class="form-input" bind:value={np.password} placeholder="password" />
                </div>
                <div class="form-row">
                  <label class="form-label">SNI</label>
                  <input class="form-input" bind:value={np.sni} placeholder="example.com" />
                </div>
              {:else if np.type === 'ss'}
                <div class="form-row">
                  <label class="form-label">Cipher</label>
                  <select class="form-select" bind:value={np.cipher}>
                    {#each CIPHERS as c}<option value={c}>{c}</option>{/each}
                  </select>
                </div>
                <div class="form-row">
                  <label class="form-label">{ru ? 'Пароль' : 'Password'}</label>
                  <input class="form-input" bind:value={np.password} placeholder="password" />
                </div>
              {:else if np.type === 'vmess'}
                <div class="form-row">
                  <label class="form-label">UUID</label>
                  <div class="input-with-btn">
                    <input class="form-input" bind:value={np.uuid} placeholder="uuid" />
                    <button
                      class="btn-gen"
                      on:click={() => (np.uuid = crypto.randomUUID())}
                      title="Generate">⟳</button
                    >
                  </div>
                </div>
                <div class="form-row2">
                  <div class="form-col">
                    <label class="form-label">Network</label>
                    <select class="form-select" bind:value={np.network}>
                      <option value="ws">WebSocket</option>
                      <option value="tcp">TCP</option>
                      <option value="grpc">gRPC</option>
                    </select>
                  </div>
                  <div class="form-col">
                    <label class="form-label">TLS</label>
                    <input type="checkbox" bind:checked={np.tls} style="margin-top:8px" />
                  </div>
                </div>
                {#if np.network === 'ws'}
                  <div class="form-row">
                    <label class="form-label">WS Path</label>
                    <input class="form-input" bind:value={np.wsPath} placeholder="/" />
                  </div>
                {/if}
                {#if np.tls}
                  <div class="form-row">
                    <label class="form-label">SNI</label>
                    <input class="form-input" bind:value={np.sni} placeholder="example.com" />
                  </div>
                {/if}
              {/if}

              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => (showProxyForm = false)}
                  >{ru ? 'Отмена' : 'Cancel'}</button
                >
                <button class="btn btn-primary" on:click={addProxy}
                  >{ru ? 'Добавить' : 'Add'}</button
                >
              </div>
            </div>
          {:else}
            <div class="constructor-proxy-list" style="display: flex; gap: 8px;">
              <button class="add-btn" style="flex: 1;" on:click={() => (showProxyForm = true)}>
                + {ru ? 'Добавить прокси' : 'Add proxy'}
              </button>
              <button
                class="add-btn import-btn"
                style="flex: 1;"
                on:click={loadSubscriptionProxies}
                disabled={!hasXraySubscriptions}
                title={hasXraySubscriptions
                  ? ($currentLang === 'ru'
                    ? 'Импортировать прокси-серверы из существующих активных Xray-подписок. Mihomo использует нативные proxy-providers для собственных подписок.'
                    : 'Import proxy servers from existing active Xray subscriptions. Mihomo uses native proxy-providers for its own subscriptions.')
                  : ($currentLang === 'ru'
                    ? 'Нет доступных активных Xray-подписок для импорта. Создайте или включите Xray-подписку в разделе «Подписки».'
                    : 'No active Xray subscriptions available for import. Create or enable an Xray subscription in the "Subscriptions" section.')}
              >
                ↓ {$t('editor.constructor_import_proxies')}
              </button>
              <button class="add-btn import-btn" style="flex: 1;" on:click={openImportModal}>
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  style="margin-right: 4px; display: inline-block; vertical-align: middle;"
                >
                  <path
                    d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242M12 12V22M12 12L15 15M12 12L9 15"
                  />
                </svg>
                {$t('subscr.import_node')}
              </button>
            </div>
          {/if}
        </div>
      {/if}

      <!-- GROUPS -->
      {#if activeSection === 'groups'}
        <div class="sec-body">
          {#if activeRuleProvider === 'zkeen'}
            <!-- Premium zkeen 16 groups UI -->
            <div class="zkeen-groups-grid">
              {#each groups as g (g.id)}
                <div class="zkeen-group-card" class:disabled={g.enabled === false}>
                  <div class="zkeen-group-header">
                    <div class="zkeen-group-icon-wrap">
                      <img
                        src={g.icon}
                        alt={g.name}
                        class="zkeen-group-icon"
                        on:error={() => {
                          const fallback =
                            'https://raw.githubusercontent.com/Koolson/Qure/master/IconSet/Color/Global.png';
                          if (g.icon !== fallback) {
                            g.icon = fallback;
                          } else {
                            g.icon =
                              'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
                          }
                          groups = [...groups];
                        }}
                      />
                    </div>
                    <div class="zkeen-group-title">
                      <span class="zkeen-group-name">{g.name}</span>
                      <div style="display: flex; gap: 4px; flex-wrap: wrap;">
                        {#if g.excludeFilter}
                          <span class="zkeen-exclude-badge">exclude: {g.excludeFilter}</span>
                        {/if}
                        {#if g.includeAll}
                          <span class="zkeen-include-badge">include-all</span>
                        {/if}
                      </div>
                    </div>
                    <label class="switch">
                      <input
                        type="checkbox"
                        checked={g.enabled !== false}
                        on:change={(e) => {
                          g.enabled = e.currentTarget.checked;
                          if (g.enabled === false) {
                            showToast('warning', $t('editor.group_disable_warning', { group: g.name }));
                          }
                          groups = [...groups];
                        }}
                      />
                      <span class="slider round"></span>
                    </label>
                  </div>

                  {#if g.enabled !== false}
                    <div class="zkeen-group-body">
                      <label class="form-label" style="font-size: 11px; margin-bottom: 2px;"
                        >{ru ? 'Исходящий канал по умолчанию' : 'Default outbound'}</label
                      >
                      <select
                        class="form-select"
                        value={g.proxies[0] || 'DIRECT'}
                        on:change={(e) => {
                          const val = e.currentTarget.value;
                          g.proxies = [val, ...g.proxies.slice(1).filter((p) => p !== val)];
                          groups = [...groups];
                        }}
                      >
                        <option value="DIRECT">DIRECT</option>
                        <option value="REJECT">REJECT</option>
                        <option value="PASS">PASS</option>
                        {#each allProxyNames.filter((n) => n !== 'DIRECT' && n !== 'REJECT' && n !== 'PASS' && n !== g.name) as n}
                          <option value={n}>{n}</option>
                        {/each}
                      </select>
                    </div>
                  {/if}
                </div>
              {/each}
            </div>
          {:else}
            {#each groups as g (g.id)}
              <div class="item-row">
                <span class="item-badge type-group">{g.type}</span>
                <span class="item-name">{g.name}</span>
                {#if g.includeAll}
                  <span
                    class="item-badge"
                    style="background: rgba(139, 92, 246, 0.2); color: #a78bfa; font-size: 10px; text-transform: none;"
                    >include-all</span
                  >
                {/if}
                <span class="item-meta">{g.proxies.length} {ru ? 'прокси' : 'proxies'}</span>
                <button class="item-del" on:click={() => removeGroup(g.id)}>✕</button>
              </div>
            {/each}

            {#if showGroupForm}
              <div class="form-card">
                <div class="form-row">
                  <label class="form-label">{ru ? 'Тип' : 'Type'}</label>
                  <select class="form-select" bind:value={ng.type}>
                    {#each GROUP_TYPES as t}<option value={t}>{t}</option>{/each}
                  </select>
                </div>
                <div class="form-row">
                  <label class="form-label">{ru ? 'Имя группы' : 'Group name'}</label>
                  <input class="form-input" bind:value={ng.name} placeholder="Выбор прокси" />
                </div>
                <div class="form-row">
                  <label
                    class="toggle-label"
                    style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
                  >
                    <input type="checkbox" bind:checked={ng.includeAll} />
                    <span
                      >{ru
                        ? 'Включить все провайдеры (include-all)'
                        : 'Include all providers'}</span
                    >
                  </label>
                </div>
                <div class="form-row">
                  <label class="form-label">{ru ? 'Прокси' : 'Proxies'}</label>
                  <div class="tag-input-wrap">
                    {#each ng.proxies as p}
                      <span class="tag-pill">
                        {p}
                        <button
                          class="tag-rm"
                          on:click={() =>
                            (ng = { ...ng, proxies: ng.proxies.filter((x) => x !== p) })}>✕</button
                        >
                      </span>
                    {/each}
                    <select
                      class="form-select-inline"
                      bind:value={ngProxyInput}
                      on:change={addGroupProxy}
                    >
                      <option value="">+ {ru ? 'добавить' : 'add'}...</option>
                      {#each allProxyNames as n}<option value={n}>{n}</option>{/each}
                    </select>
                  </div>
                </div>
                {#if ng.type !== 'select'}
                  <div class="form-row2">
                    <div class="form-col">
                      <label class="form-label">URL</label>
                      <input class="form-input" bind:value={ng.url} />
                    </div>
                    <div class="form-col form-col-sm">
                      <label class="form-label">{ru ? 'Интервал (с)' : 'Interval (s)'}</label>
                      <input class="form-input" type="number" bind:value={ng.interval} />
                    </div>
                  </div>
                {/if}
                <div class="form-actions">
                  <button class="btn btn-secondary" on:click={() => (showGroupForm = false)}
                    >{ru ? 'Отмена' : 'Cancel'}</button
                  >
                  <button class="btn btn-primary" on:click={addGroup}
                    >{ru ? 'Добавить' : 'Add'}</button
                  >
                </div>
              </div>
            {:else}
              <div class="constructor-proxy-list" style="display: flex; gap: 8px;">
                <button class="add-btn" style="flex: 1;" on:click={() => (showGroupForm = true)}>
                  + {ru ? 'Добавить группу' : 'Add group'}
                </button>
              </div>
            {/if}
          {/if}
        </div>
      {/if}

      <!-- RULESETS -->
      {#if activeSection === 'rulesets'}
        <div class="sec-body" data-testid="rulesets-picker">
          <div class="card rulesets-card" style="padding:16px;">
            <div class="rulesets-header">
              <h3 style="margin-top:0; margin-bottom:4px; font-size:16px;">
                {$t('editor.rulesets_picker')}
              </h3>
              <p
                class="sub"
                style="margin-top:0; margin-bottom:16px; font-size:12px; color:var(--fg-dim);"
              >
                {ru
                  ? 'Выберите наборы правил и укажите группу для каждого.'
                  : 'Select rule sets and assign a group for each.'}
              </p>
            </div>

            {#each Object.entries(META_RULE_SETS_BY_CATEGORY) as [category, items]}
              <div class="rulesets-category-group" style="margin-top:16px;">
                <h4
                  class="category-title"
                  style="font-size:13px; font-weight:600; color:var(--fg-secondary); margin-bottom:8px; padding-bottom:4px; border-bottom:1px solid rgba(255,255,255,0.05);"
                >
                  {category}
                </h4>
                <div
                  class="rulesets-grid"
                  style="display:grid; grid-template-columns:repeat(auto-fill, minmax(260px, 1fr)); gap:8px;"
                >
                  {#each items as item}
                    {@const key = `${item.id}|${item.type}`}
                    {@const isChecked = selectedMetaRuleSets.has(key)}
                    <div
                      class="ruleset-item-row"
                      class:selected={isChecked}
                      style="display:flex; align-items:center; justify-content:space-between; padding:8px 12px; background:rgba(255,255,255,0.02); border:1px solid var(--border); border-radius:var(--radius); transition:background var(--transition-fast), border-color var(--transition-fast);"
                    >
                      <label
                        class="ruleset-label"
                        for="ruleset-{item.type}-{item.id}"
                        style="display:flex; align-items:center; gap:8px; cursor:pointer; flex:1; user-select:none;"
                      >
                        <input
                          type="checkbox"
                          id="ruleset-{item.type}-{item.id}"
                          value={key}
                          checked={isChecked}
                          on:change={(e) => {
                            if (e.currentTarget.checked) {
                              let outbound = item.defaultOutbound;
                              if (
                                outbound === 'Proxy' &&
                                groups.some((g) => g.name === 'Selective')
                              ) {
                                outbound = 'Selective';
                              } else if (
                                outbound === 'Proxy' &&
                                groups.some((g) => g.name === 'Proxy')
                              ) {
                                outbound = 'Proxy';
                              } else if (!allProxyNames.includes(outbound)) {
                                outbound = allProxyNames[0] || 'DIRECT';
                              }
                              selectedMetaRuleSets.set(key, outbound);
                            } else {
                              selectedMetaRuleSets.delete(key);
                            }
                            selectedMetaRuleSets = new Map(selectedMetaRuleSets);
                          }}
                        />
                        <span
                          class="ruleset-name"
                          style="font-size:13px; font-weight:500; color:var(--fg-primary);"
                          >{item.label}</span
                        >
                        <span
                          class="ruleset-type-badge"
                          style="font-size:9px; font-weight:700; text-transform:uppercase; color:var(--fg-dim); background:rgba(255,255,255,0.05); padding:1px 4px; border-radius:4px;"
                          >{item.type}</span
                        >
                      </label>

                      {#if isChecked}
                        <select
                          class="ruleset-outbound-select"
                          style="font-size:12px; background:var(--bg-surface); border:1px solid var(--border); color:var(--fg-primary); border-radius:var(--radius-sm); padding:2px 6px; max-width:120px; outline:none;"
                          value={selectedMetaRuleSets.get(key)}
                          on:change={(e) => {
                            selectedMetaRuleSets.set(key, e.currentTarget.value);
                            selectedMetaRuleSets = selectedMetaRuleSets;
                          }}
                        >
                          {#each allProxyNames as n}
                            <option value={n}>{n}</option>
                          {/each}
                        </select>
                      {/if}
                    </div>
                  {/each}
                </div>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      <!-- RULES -->
      {#if activeSection === 'rules'}
        <div class="sec-body">
          {#each rules as r, i (r.id)}
            <div class="item-row item-row-rule">
              <div class="rule-order">
                <button class="order-btn" on:click={() => moveRule(r.id, -1)} disabled={i === 0}
                  >▲</button
                >
                <button
                  class="order-btn"
                  on:click={() => moveRule(r.id, 1)}
                  disabled={i === rules.length - 1}>▼</button
                >
              </div>
              <span class="item-badge type-rule">{r.type}</span>
              {#if r.type !== 'MATCH'}
                <span class="item-name rule-value">{r.value}</span>
              {/if}
              <span class="item-meta">→ {r.outbound}</span>
              <button class="item-del" on:click={() => removeRule(r.id)}>✕</button>
            </div>
          {/each}

          {#if showRuleForm}
            <div class="form-card">
              <div class="form-row2">
                <div class="form-col">
                  <label class="form-label">{ru ? 'Тип правила' : 'Rule type'}</label>
                  <select class="form-select" bind:value={nr.type}>
                    {#each RULE_TYPES as t}<option value={t}>{t}</option>{/each}
                  </select>
                </div>
                <div class="form-col">
                  <label class="form-label">{ru ? 'Исходящий' : 'Outbound'}</label>
                  <select class="form-select" bind:value={nr.outbound}>
                    {#each allProxyNames as n}<option value={n}>{n}</option>{/each}
                  </select>
                </div>
              </div>
              {#if nr.type !== 'MATCH'}
                <div class="form-row">
                  <label class="form-label">{ru ? 'Значение' : 'Value'}</label>
                  <input
                    class="form-input"
                    bind:value={nr.value}
                    placeholder={nr.type === 'GEOIP'
                      ? 'CN'
                      : nr.type === 'GEOSITE'
                        ? 'google'
                        : nr.type === 'IP-CIDR'
                          ? '192.168.0.0/16'
                          : 'example.com'}
                  />
                </div>
              {/if}
              <div class="form-actions">
                <button class="btn btn-secondary" on:click={() => (showRuleForm = false)}
                  >{ru ? 'Отмена' : 'Cancel'}</button
                >
                <button class="btn btn-primary" on:click={addRule}>{ru ? 'Добавить' : 'Add'}</button
                >
              </div>
            </div>
          {:else}
            <button class="add-btn" on:click={() => (showRuleForm = true)}>
              + {ru ? 'Добавить правило' : 'Add rule'}
            </button>
          {/if}
        </div>
      {/if}

      <!-- DNS -->
      {#if activeSection === 'dns'}
        <div class="sec-body">
          <div class="toggle-row">
            <label class="toggle-label">
              <input type="checkbox" bind:checked={dns.enabled} />
              <span>{ru ? 'Включить DNS' : 'Enable DNS'}</span>
            </label>
          </div>
          {#if dns.enabled}
            {#if $capabilities?.xkeen_dns === false}
              <div class="alert alert-warning" style="margin: 0 0 16px 0; display: flex; flex-direction: column; gap: 8px; align-items: flex-start;" role="status">
                <div style="display: flex; gap: 8px; align-items: center;">
                  <span aria-hidden="true">⚠️</span>
                  <span>{$t('editor.dns_intercept_warning')}</span>
                </div>
                <button class="btn btn-secondary btn-sm" style="font-size: 12px; padding: 4px 8px; display: flex; align-items: center; gap: 4px;" on:click={enableDNSRedirect} disabled={dnsRedirectLoading}>
                  {#if dnsRedirectLoading}
                    <span class="spinner" style="display: inline-block; width: 12px; height: 12px; border: 2px solid currentColor; border-top-color: transparent; border-radius: 50%; animation: spin 1s linear infinite;"></span>
                  {/if}
                  {$t('editor.dns_intercept_enable')}
                </button>
              </div>
            {/if}
            <div class="form-row">
              <label class="form-label">{ru ? 'Режим' : 'Enhanced mode'}</label>
              <select class="form-select" bind:value={dns.enhancedMode}>
                <option value="fake-ip">fake-ip</option>
                <option value="redir-host">redir-host</option>
              </select>
            </div>
            {#if dns.enhancedMode === 'fake-ip'}
              <div class="form-row">
                <label class="form-label">Fake-IP Range</label>
                <input class="form-input" bind:value={dns.fakeIPRange} />
              </div>
            {/if}
            <div class="form-row">
              <label class="form-label">Nameservers</label>
              <textarea
                class="form-textarea"
                value={dns.nameservers.join('\n')}
                rows="3"
                on:change={(e) =>
                  (dns.nameservers = e.currentTarget.value.split('\n').filter(Boolean))}
              ></textarea>
            </div>
            <div class="form-row">
              <label class="form-label">Fallback</label>
              <textarea
                class="form-textarea"
                value={dns.fallback.join('\n')}
                rows="2"
                on:change={(e) =>
                  (dns.fallback = e.currentTarget.value.split('\n').filter(Boolean))}
              ></textarea>
            </div>
          {/if}
        </div>
      {/if}

      <!-- TUN -->
      {#if activeSection === 'tun'}
        <div class="sec-body">
          <div class="toggle-row">
            <label class="toggle-label">
              <input type="checkbox" bind:checked={tun.enabled} />
              <span>{ru ? 'Включить TUN' : 'Enable TUN'}</span>
            </label>
          </div>
          {#if tun.enabled}
            <div class="form-row">
              <label class="form-label">Stack</label>
              <select class="form-select" bind:value={tun.stack}>
                <option value="mixed">mixed</option>
                <option value="system">system</option>
                <option value="gvisor">gvisor</option>
              </select>
            </div>
            <div class="toggle-row">
              <label class="toggle-label">
                <input type="checkbox" bind:checked={tun.autoRoute} />
                <span>auto-route</span>
              </label>
            </div>
            <div class="toggle-row">
              <label class="toggle-label">
                <input type="checkbox" bind:checked={tun.autoDetectInterface} />
                <span>auto-detect-interface</span>
              </label>
            </div>
            <div class="form-row">
              <label class="form-label">DNS hijack</label>
              <input
                class="form-input"
                value={tun.dnsHijack.join(', ')}
                on:change={(e) =>
                  (tun.dnsHijack = e.currentTarget.value
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean))}
              />
            </div>
          {/if}
          <div class="toggle-row" style="margin-top: 16px; border-top: 1px solid var(--border); padding-top: 16px;">
            <label class="toggle-label">
              <input type="checkbox" bind:checked={sniffer.enabled} />
              <span>{$t('editor.sniffer_enable')}</span>
            </label>
          </div>
          {#if sniffer.enabled}
            <div style="margin-left: 20px; display: flex; flex-direction: column; gap: 8px; margin-top: 8px;">
              <label class="checkbox-container" style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;">
                <input type="checkbox" bind:checked={sniffer.sniffHttp} style="width: auto; margin: 0;" />
                <span>Sniff HTTP (ports 80, 8080)</span>
              </label>
              <label class="checkbox-container" style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;">
                <input type="checkbox" bind:checked={sniffer.sniffTls} style="width: auto; margin: 0;" />
                <span>Sniff TLS (ports 443, 8443)</span>
              </label>
              <label class="checkbox-container" style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;">
                <input type="checkbox" bind:checked={sniffer.sniffQuic} style="width: auto; margin: 0;" />
                <span>Sniff QUIC (ports 443, 8443)</span>
              </label>
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Right: YAML preview -->
    <div class="gen-right">
      <div class="preview-header">
        <span class="preview-title">YAML {ru ? 'превью' : 'preview'}</span>
        {#if yaml}
          <button class="btn btn-secondary btn-sm" on:click={copyYAML}>
            <svg
              width="12"
              height="12"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><rect x="9" y="9" width="13" height="13" rx="2" /><path
                d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
              /></svg
            >
          </button>
        {/if}
      </div>
      <pre class="yaml-preview">{yaml ||
          (ru
            ? '# Добавьте элементы слева\n# чтобы сгенерировать YAML'
            : '# Add elements on the left\n# to generate YAML')}</pre>

      {#if validationError}
        <div class="validation-error-block" style="margin-top: 12px; padding: 12px; background: rgba(239, 91, 107, 0.1); border: 1px solid var(--danger); border-radius: var(--radius-md); color: var(--danger); font-size: 13px;">
          <div style="font-weight: bold; margin-bottom: 6px;">{$t('editor.validation_failed')}</div>
          <div style="white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 13px; margin-bottom: 8px;">
            {parseValidationError(validationError, $currentLang)}
          </div>
          <details>
            <summary style="cursor: pointer; font-size: 12px; opacity: 0.8; user-select: none;">{$t('editor.validation_details')}</summary>
            <pre style="margin: 6px 0 0 0; white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 12px; opacity: 0.9; max-height: 200px; overflow-y: auto;">{validationError}</pre>
          </details>
        </div>
      {/if}

      {#if embedded}
        <div class="gen-embedded-actions" style="margin-top: 12px; display: flex; flex-direction: column; gap: 8px;">
          <div style="display: flex; gap: 8px; width: 100%;">
             <button class="btn btn-secondary" style="flex: 1;" on:click={openInEditor}>
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                style="margin-right:5px"
                ><path d="M12 20h9" /><path
                  d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"
                /></svg
              >
              {#if selectedFile}
                {ru ? 'Вставить в редактор' : 'Insert into Editor'}
              {:else}
                {ru ? 'Открыть в редакторе' : 'Open in Editor'}
              {/if}
            </button>
            <button
              class="btn btn-primary"
              data-testid="apply-changes-btn"
              on:click={handleApplyMihomo}
              disabled={applyLoading || !yaml}
              style="flex: 1;"
            >
              {applyLoading
                ? ru
                  ? 'Сохранение...'
                  : 'Saving...'
                : ru
                  ? 'Применить изменения'
                  : 'Apply Changes'}
            </button>
          </div>
          {#if canUndo}
            <button
              class="btn btn-secondary"
              on:click={handleUndo}
              disabled={applyLoading}
              style="width: 100%;"
            >
              {$t('editor.undo')}
            </button>
          {/if}
        </div>
      {/if}
    </div>
  </div>
    {/if}
</div>

{#if showApplyConfirm}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    data-testid="apply-confirm-dialog"
    on:click={() => (showApplyConfirm = false)}
    on:keydown={(e) => e.key === 'Escape' && (showApplyConfirm = false)}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{$t('editor.apply_confirm_title')}</h2>
        <button class="modal-close-btn" on:click={() => (showApplyConfirm = false)}>&times;</button>
      </div>
      <div class="modal-card-body">
        <p>{$t('editor.apply_confirm_body')}</p>
        <div class="changed-files-list" style="margin-top: 12px;">
          <strong
            >{ru ? 'Будут обновлены секции в файле:' : 'Sections to be updated in file:'}</strong
          >
          <div style="margin: 8px 0; font-family: monospace; font-size: 13px;">
            <code>{selectedFile || '/opt/etc/mihomo/config.yaml'}</code>
          </div>
          <ul style="margin: 8px 0 0 0; padding-left: 20px;">
            <li><code>proxy-groups</code></li>
            <li><code>rule-providers</code></li>
            <li><code>rules</code></li>
          </ul>
          <p style="margin-top: 12px; font-size: 0.8125rem; color: var(--fg-secondary);">
            {ru
              ? '* Автоматически будет создана резервная копия (хранится до 5 последних бэкапов)'
              : '* A backup will be created automatically (up to 5 copies stored)'}
          </p>
        </div>
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={() => (showApplyConfirm = false)}>
          {$t('app.cancel')}
        </button>
        <button class="btn btn-primary" on:click={handleApplyMihomo} disabled={applyLoading}>
          {applyLoading ? $t('editor.saving') : $t('editor.apply_and_restart')}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if showImportModal}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    on:click={closeImportModal}
    on:keydown={(e) => e.key === 'Escape' && closeImportModal()}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{$t('subscr.import_modal_title')}</h2>
        <button class="modal-close-btn" on:click={closeImportModal}>&times;</button>
      </div>
      <div class="modal-card-body">
        {#if importErrorMsg}
          <div
            class="error-msg"
            style="color: var(--danger); margin-bottom: 12px; font-size: 13px;"
          >
            {importErrorMsg}
          </div>
        {/if}

        {#if importStep === 1}
          <div class="form-group">
            <label for="import-link" class="form-label">{$t('subscr.import_link_label')}</label>
            <textarea
              id="import-link"
              class="input textarea-link"
              bind:value={importLink}
              placeholder={$t('subscr.import_link_placeholder')}
              rows="4"
              style="resize: none; font-family: var(--font-family-mono, monospace); font-size: 12px; width: 100%; box-sizing: border-box; background: var(--bg-surface-hover); border: 1px solid var(--border); border-radius: var(--radius-sm, 4px); padding: 8px; color: var(--fg);"
            ></textarea>
          </div>
        {:else if importStep === 2 && importNodes.length > 0}
          <div class="preview-section">
            <h3 class="preview-title" style="margin: 0 0 12px 0; font-size: 14px;">
              {$t('subscr.import_preview_title')}
            </h3>
            <div
              class="preview-list"
              style="max-height: 260px; overflow-y: auto; display: flex; flex-direction: column; gap: 10px; padding-right: 4px; scrollbar-width: thin;"
            >
              {#each importNodes as item, idx}
                {#if item.rowError}
                  <div
                    class="preview-item-card"
                    style="background: var(--bg-card); border: 1px solid var(--danger); border-radius: var(--radius-sm, 4px); padding: 10px; display: flex; flex-direction: column; gap: 8px; position: relative;"
                  >
                    <button
                      type="button"
                      on:click={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
                      style="position: absolute; right: 10px; top: 10px; background: none; border: 0; color: var(--fg-secondary); cursor: pointer; font-size: 12px;"
                      aria-label="Remove">✕</button
                    >
                    <div style="font-size: 12px; color: var(--danger); padding-right: 20px;">
                      <strong>{$t('app.error')}:</strong>
                      {item.rowError}
                    </div>
                    <div
                      style="font-size: 11px; color: var(--fg-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; padding-right: 20px;"
                      title={item.link}
                    >
                      {item.link}
                    </div>
                  </div>
                {:else}
                  <div
                    class="preview-item-card"
                    style="background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius-sm, 4px); padding: 10px; display: flex; flex-direction: column; gap: 8px; position: relative;"
                  >
                    <button
                      type="button"
                      on:click={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
                      style="position: absolute; right: 10px; top: 10px; background: none; border: 0; color: var(--fg-secondary); cursor: pointer; font-size: 12px;"
                      aria-label="Remove">✕</button
                    >
                    <div
                      style="display: flex; justify-content: space-between; font-size: 12px; color: var(--fg-secondary); padding-right: 20px;"
                    >
                      <span
                        ><strong style="color: var(--fg);">{item.outbound?.protocol}</strong> · {getNodeServer(
                          item.outbound
                        )}:{getNodePort(item.outbound)}</span
                      >
                    </div>
                    <div style="display: flex; align-items: center; gap: 8px;">
                      <label
                        class="form-label"
                        style="margin: 0; font-size: 12px; flex-shrink: 0;"
                        for="import-tag-{idx}">{$t('subscr.import_tag_custom')}:</label
                      >
                      <input
                        id="import-tag-{idx}"
                        type="text"
                        class="input"
                        bind:value={item.tag}
                        style="flex-grow: 1; font-size: 12px; box-sizing: border-box; background: var(--bg-surface-hover); border: 1px solid var(--border); border-radius: var(--radius-sm, 4px); padding: 4px 8px; color: var(--fg); width: auto;"
                      />
                    </div>
                  </div>
                {/if}
              {/each}
            </div>
          </div>
        {/if}
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={closeImportModal} disabled={importLoading}>
          {$t('app.cancel')}
        </button>
        {#if importStep === 1}
          <button
            class="btn btn-primary"
            on:click={parseImportLink}
            disabled={!importLink.trim() || importLoading}
          >
            {#if importLoading}
              <span class="spinner-xs" style="margin-right: 6px;"></span>
            {/if}
            {$t('subscr.import_btn_parse')}
          </button>
        {:else}
          <button
            class="btn btn-primary"
            on:click={confirmImportNode}
            disabled={importLoading ||
              importNodes.length === 0 ||
              importNodes.some((n) => n.rowError)}
          >
            {#if importLoading}
              <span class="spinner-xs" style="margin-right: 6px;"></span>
            {/if}
            {ru ? `Импортировать (${importNodes.length})` : `Import (${importNodes.length})`}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .crumb-sep {
    color: var(--fg-faint);
    margin: 0 6px;
  }

  .gen-layout {
    display: grid;
    grid-template-columns: 1fr 380px;
    gap: 20px;
    align-items: start;
  }

  /* Sections */
  .sec-tabs {
    display: flex;
    gap: 2px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 4px;
    margin-bottom: 16px;
  }

  .sec-tab {
    flex: 1;
    background: none;
    border: none;
    color: var(--fg-secondary);
    font-size: 12px;
    font-weight: 500;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 5px;
    transition:
      background var(--transition-fast),
      color var(--transition-fast);
  }

  .sec-tab.active {
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-primary);
  }

  .sec-count {
    background: var(--primary);
    color: #0c2237;
    font-size: 9px;
    font-weight: 700;
    border-radius: 8px;
    padding: 1px 5px;
    line-height: 1.4;
  }

  .sec-dot {
    width: 6px;
    height: 6px;
    background: var(--success);
    border-radius: 50%;
  }

  .sec-body {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  /* Item rows */
  .item-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .item-row-rule {
    gap: 8px;
  }

  .item-badge {
    font-size: 10px;
    font-weight: 700;
    padding: 2px 7px;
    border-radius: 10px;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  .type-vless {
    background: rgba(41, 194, 240, 0.15);
    color: var(--primary);
  }
  .type-hysteria2 {
    background: rgba(70, 209, 138, 0.15);
    color: var(--success);
  }
  .type-tuic {
    background: rgba(240, 180, 80, 0.15);
    color: var(--warning);
  }
  .type-ss {
    background: rgba(239, 91, 107, 0.15);
    color: var(--danger);
  }
  .type-vmess {
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-secondary);
  }
  .type-group {
    background: rgba(139, 92, 246, 0.15);
    color: #a78bfa;
  }
  .type-rule {
    background: rgba(255, 255, 255, 0.05);
    color: var(--fg-dim);
    font-size: 9px;
  }

  .item-name {
    flex: 1;
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .rule-value {
    font-family: 'JetBrains Mono', monospace;
    font-size: 12px;
  }

  .item-meta {
    font-size: 11px;
    color: var(--fg-dim);
    flex-shrink: 0;
  }

  .item-del {
    background: none;
    border: none;
    color: var(--fg-faint);
    cursor: pointer;
    font-size: 11px;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
    transition: color var(--transition-fast);
    flex-shrink: 0;
    line-height: 1;
  }

  .item-del:hover {
    color: var(--danger);
  }

  .rule-order {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex-shrink: 0;
  }
  .order-btn {
    background: none;
    border: none;
    color: var(--fg-faint);
    font-size: 9px;
    cursor: pointer;
    padding: 1px 3px;
    line-height: 1;
    transition: color var(--transition-fast);
  }
  .order-btn:hover:not(:disabled) {
    color: var(--fg-primary);
  }
  .order-btn:disabled {
    opacity: 0.3;
    cursor: default;
  }

  /* Form */
  .form-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .form-row2 {
    display: flex;
    gap: 10px;
  }
  .form-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
  }
  .form-col-sm {
    flex: 0 0 100px;
  }

  .form-label {
    font-size: 11px;
    color: var(--fg-dim);
    font-weight: 500;
  }

  .form-input,
  .form-select {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 13px;
    padding: 6px 10px;
    outline: none;
    width: 100%;
    transition: border-color var(--transition-fast);
  }

  .form-input:focus,
  .form-select:focus {
    border-color: var(--primary);
  }

  .form-textarea {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 12px;
    font-family: 'JetBrains Mono', monospace;
    padding: 6px 10px;
    outline: none;
    width: 100%;
    resize: vertical;
    transition: border-color var(--transition-fast);
  }

  .form-textarea:focus {
    border-color: var(--primary);
  }

  .form-select-inline {
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-secondary);
    font-size: 12px;
    padding: 2px 4px;
    outline: none;
    cursor: pointer;
  }

  .input-with-btn {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .input-with-btn .form-input {
    flex: 1;
  }

  .btn-gen {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    cursor: pointer;
    font-size: 14px;
    transition: background var(--transition-fast);
    flex-shrink: 0;
  }

  .btn-gen:hover {
    background: rgba(255, 255, 255, 0.1);
    color: var(--fg-primary);
  }

  .tag-input-wrap {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 6px 8px;
    align-items: center;
  }

  .tag-pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: rgba(41, 194, 240, 0.12);
    border: 1px solid rgba(41, 194, 240, 0.25);
    color: var(--primary);
    font-size: 11px;
    border-radius: 10px;
    padding: 2px 8px;
  }

  .tag-rm {
    background: none;
    border: none;
    color: inherit;
    cursor: pointer;
    font-size: 10px;
    padding: 0;
    line-height: 1;
  }

  .form-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 4px;
  }

  .add-btn {
    width: 100%;
    background: rgba(255, 255, 255, 0.02);
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius);
    color: var(--fg-dim);
    font-size: 13px;
    padding: 12px;
    cursor: pointer;
    transition:
      background var(--transition-fast),
      color var(--transition-fast),
      border-color var(--transition-fast);
    text-align: center;
  }

  .add-btn:hover {
    background: rgba(41, 194, 240, 0.05);
    border-color: rgba(41, 194, 240, 0.3);
    color: var(--primary);
  }

  /* Toggle */
  .toggle-row {
    display: flex;
    align-items: center;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 13px;
    color: var(--fg-primary);
  }

  /* YAML preview */
  .gen-right {
    position: sticky;
    top: 20px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 140px);
  }

  .preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 14px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .preview-title {
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .btn-sm {
    padding: 4px 8px;
    font-size: 12px;
  }

  .yaml-preview {
    flex: 1;
    overflow-y: auto;
    margin: 0;
    padding: 14px 16px;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11.5px;
    line-height: 1.6;
    color: var(--fg-secondary);
    white-space: pre;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }

  @media (max-width: 900px) {
    .gen-layout {
      grid-template-columns: 1fr;
    }
    .gen-right {
      position: static;
      max-height: 300px;
    }
  }

  /* Scenario bar */
  .constructor-scenario-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    margin-bottom: 10px;
  }

  .scenario-label {
    font-size: 11px;
    color: var(--fg-dim);
    font-weight: 500;
    flex-shrink: 0;
  }

  .scenario-chip {
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--border);
    border-radius: 12px;
    color: var(--fg-secondary);
    font-size: 12px;
    font-weight: 500;
    padding: 4px 12px;
    cursor: pointer;
    transition:
      background var(--transition-fast),
      border-color var(--transition-fast),
      color var(--transition-fast);
    line-height: 1.4;
  }

  .scenario-chip:hover {
    background: rgba(41, 194, 240, 0.08);
    border-color: rgba(41, 194, 240, 0.35);
    color: var(--primary);
  }

  .scenario-chip.active {
    background: rgba(41, 194, 240, 0.15);
    border-color: rgba(41, 194, 240, 0.5);
    color: var(--primary);
  }

  /* Rule providers row */
  .rule-providers-row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
  }

  .rule-providers-row .form-label {
    flex-shrink: 0;
  }

  .rp-select {
    width: auto;
    min-width: 160px;
    font-size: 12px;
    padding: 5px 8px;
  }

  /* Proxy list action group */
  .constructor-proxy-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .import-btn {
    border-style: dashed;
    color: var(--fg-dim);
  }

  .import-btn:hover {
    background: rgba(70, 209, 138, 0.05);
    border-color: rgba(70, 209, 138, 0.3);
    color: var(--success);
  }

  /* Premium zkeen 16 groups grid */
  .zkeen-groups-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }

  .zkeen-group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    transition:
      opacity var(--transition-fast),
      border-color var(--transition-fast);
  }

  .zkeen-group-card.disabled {
    opacity: 0.6;
    border-color: var(--border);
  }

  .zkeen-group-header {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .zkeen-group-icon-wrap {
    width: 24px;
    height: 24px;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: rgba(255, 255, 255, 0.05);
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .zkeen-group-icon {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  .zkeen-group-title {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .zkeen-group-name {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .zkeen-exclude-badge {
    background: rgba(240, 180, 80, 0.1);
    color: var(--warning);
    font-size: 10px;
    padding: 1px 4px;
    border-radius: 4px;
    width: fit-content;
  }

  .zkeen-include-badge {
    background: rgba(139, 92, 246, 0.1);
    color: #a78bfa;
    font-size: 10px;
    padding: 1px 4px;
    border-radius: 4px;
    width: fit-content;
  }

  .zkeen-group-body {
    margin-top: 4px;
    border-top: 1px solid rgba(255, 255, 255, 0.03);
    padding-top: 8px;
  }

  /* Toggle Switch */
  .switch {
    position: relative;
    display: inline-block;
    width: 32px;
    height: 18px;
    flex-shrink: 0;
  }

  .switch input {
    opacity: 0;
    width: 0;
    height: 0;
  }

  .slider {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(255, 255, 255, 0.1);
    transition: 0.4s;
    border: 1px solid var(--border);
  }

  .slider:before {
    position: absolute;
    content: '';
    height: 12px;
    width: 12px;
    left: 2px;
    bottom: 2px;
    background-color: var(--fg-secondary);
    transition: 0.4s;
  }

  input:checked + .slider {
    background-color: var(--success);
    border-color: var(--success);
  }

  input:checked + .slider:before {
    transform: translateX(14px);
    background-color: #0c2237;
  }

  .slider.round {
    border-radius: 18px;
  }

  .slider.round:before {
    border-radius: 50%;
  }

  /* Modal styles */
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal-card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg, 8px);
    width: 500px;
    max-width: 90%;
    display: flex;
    flex-direction: column;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.25);
  }

  .modal-card-header {
    padding: 16px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-card-header h2 {
    margin: 0;
    font-size: 1.125rem;
    font-weight: 600;
    color: var(--fg);
  }

  .modal-close-btn {
    background: transparent;
    border: none;
    font-size: 1.5rem;
    color: var(--fg-secondary);
    cursor: pointer;
  }

  .modal-card-body {
    padding: 16px;
    font-size: var(--font-size-sm, 0.8125rem);
    color: var(--fg);
    max-height: 400px;
    overflow-y: auto;
  }

  .modal-card-footer {
    padding: 16px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
