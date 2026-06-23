<script lang="ts">
  import { onMount } from 'svelte';
  import { currentLang, t } from './i18n';
  import { capabilities, showToast, fetchCapabilities } from './stores';
  import { parseValidationError } from './lib/errorParser';
  import { findPortCollisions, type PortAllocation } from './lib/portChecker';
  import {
    yamlSafeString,
    sanitizeUrl,
    unquote,
    extractSection,
    replaceMihomoTopLevelSection,
    generateYAML as generateMihomoYAML,
    populateMihomoFromYAML as populateMihomoFromYAML_raw,
    ZKEEN_RULE_PROVIDERS,
    type RuleProvider
  } from './lib/mihomoYaml';
  import ProxyForm from './components/mihomo/ProxyForm.svelte';
  import GroupForm from './components/mihomo/GroupForm.svelte';
  import RuleForm from './components/mihomo/RuleForm.svelte';

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
    useProviders?: string[];
    strategy?: 'round-robin' | 'consistent-hashing' | 'sticky-sessions';
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
  let mihomoProviders: any[] = [];
  $: hasXraySubscriptions = subscriptions.some((s) => s.enable_xray);
  $: hasMihomoProviders = mihomoProviders.length > 0;
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
  let dismissMergeWarning = false;
  let lastPreservedKeysStr = '';
  $: if (preservedKeys.join(',') !== lastPreservedKeysStr) {
    lastPreservedKeysStr = preservedKeys.join(',');
    const dismissed = localStorage.getItem('xcp:dismissed_warning:preserved_keys');
    dismissMergeWarning = (dismissed === lastPreservedKeysStr);
  }

  let dismissZkeenGeodataWarning = false;
  let lastActivePreset = '';
  $: if (activePreset !== lastActivePreset) {
    if (lastActivePreset && activePreset !== lastActivePreset) {
      localStorage.removeItem('xcp:dismissed_warning:zkeen_geodata');
    }
    lastActivePreset = activePreset;
    const dismissed = localStorage.getItem('xcp:dismissed_warning:zkeen_geodata');
    dismissZkeenGeodataWarning = (dismissed === activePreset);
  }

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
  let editingProxyId: string | null = null;
  let editingGroupId: string | null = null;



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
  let lastType = 'vless';
  $: if (np.type && np.type !== lastType) {
    lastType = np.type;
    np = { ...newProxyDefaults(np.type), name: np.name, server: np.server, port: np.port };
  }

  // New group form
  let ng: Omit<ProxyGroup, 'id'> = {
    name: '',
    type: 'select',
    proxies: [],
    includeAll: false,
    url: 'https://www.gstatic.com/generate_204',
    interval: 300,
    useProviders: [],
    strategy: undefined
  };

  // New rule form
  let nr: Omit<Rule, 'id'> = { type: 'DOMAIN-SUFFIX', value: '', outbound: 'DIRECT' };

  // Moved state variables to prevent duplicate declarations and temporal dead zone (TDZ) issues
  let validationError = '';
  let schema: any = null;
  let schemaLoading = true;
  let schemaError = '';
  let showApplyConfirm = false;
  let applyLoading = false;
  let dnsRedirectLoading = false;

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
      name: 'TikTok',
      type: 'select',
      includeAll: true,
      proxies: ['Заблок. сервисы', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/TikTok.png'
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
        mihomoProviders = subs.filter((s) => s.enabled && s.enable_mihomo);
      } else {
        subscriptions = [];
        mihomoProviders = [];
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
      const res = populateMihomoFromYAML_raw(text) as any;
      proxies = res.proxies;
      groups = res.groups;
      rules = res.rules;
      dns = res.dns;
      tun = res.tun;
      sniffer = res.sniffer;
      activeRuleProvider = res.activeRuleProvider as any;
      selectedMetaRuleSets = res.selectedMetaRuleSets;
      preservedKeys = res.preservedKeys;
      existingTproxyPort = res.existingTproxyPort;
      existingRedirPort = res.existingRedirPort;
      
      if (res.groups.length === 0 && res.proxies.length === 0) {
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



  let configLoadedForPath = '';

  async function loadConfig(path: string, force = false) {
    if (!path) return;
    if (configLoadedForPath === path && !force) return;
    configLoadedForPath = path;
    try {
      const res = await fetch(`/api/config/read?path=${encodeURIComponent(path)}`);
      if (res.status === 404) {
        populateMihomoFromYAML('');
        return;
      }
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
    if (editingProxyId) {
      proxies = proxies.map(p => p.id === editingProxyId ? { ...np, name: cleanName, id: editingProxyId } : p);
      editingProxyId = null;
    } else {
      proxies = [...proxies, { ...np, name: cleanName, id: crypto.randomUUID() }];
    }
    showProxyForm = false;
    np = newProxyDefaults('vless');
  }

  function editProxy(p: Proxy) {
    np = { ...p };
    editingProxyId = p.id;
    showProxyForm = true;
  }

  function removeProxy(id: string) {
    proxies = proxies.filter((p) => p.id !== id);
  }

  function addGroup() {
    if (!ng.name.trim()) return;
    if (editingGroupId) {
      groups = groups.map(g => g.id === editingGroupId ? { ...ng, id: editingGroupId, proxies: [...ng.proxies], useProviders: ng.useProviders ? [...ng.useProviders] : [] } : g);
      editingGroupId = null;
    } else {
      groups = [...groups, { ...ng, id: crypto.randomUUID(), proxies: [...ng.proxies], useProviders: ng.useProviders ? [...ng.useProviders] : [] }];
    }
    showGroupForm = false;
    ng = {
      name: '',
      type: 'select',
      proxies: [],
      includeAll: false,
      url: 'https://www.gstatic.com/generate_204',
      interval: 300,
      useProviders: [],
      strategy: undefined
    };
  }

  function editGroup(g: ProxyGroup) {
    ng = {
      name: g.name,
      type: g.type,
      proxies: [...g.proxies],
      includeAll: g.includeAll || false,
      url: g.url || 'https://www.gstatic.com/generate_204',
      interval: g.interval || 300,
      useProviders: g.useProviders ? [...g.useProviders] : [],
      strategy: g.strategy
    };
    editingGroupId = g.id;
    showGroupForm = true;
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

  // ── YAML generation ─────────────────────────────────────────────────────

  function generateYAML(): string {
    return generateMihomoYAML({
      proxies,
      groups,
      rules,
      dns,
      tun,
      sniffer,
      activeRuleProvider,
      selectedMetaRuleSets,
      preservedKeys,
      existingTproxyPort,
      existingRedirPort,
      subscriptions,
      mihomoProviders,
      capabilities: $capabilities,
      hasZkeenGeodata,
      ruleProviders
    });
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
    void mihomoProviders;
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

  $: ru = $currentLang === 'ru';

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

  // extractSection is imported from './lib/mihomoYaml'

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

  // findTopLevelSection and replaceMihomoTopLevelSection are imported from './lib/mihomoYaml'

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

    // Check port collisions
    const mihomoPorts: PortAllocation[] = [
      { port: existingTproxyPort ?? 5001, engine: 'mihomo', purpose: 'tproxy-port' },
      { port: existingRedirPort ?? 5000, engine: 'mihomo', purpose: 'redir-port' },
      { port: 9090, engine: 'mihomo', purpose: 'external-controller' }
    ];

    let xrayPorts: PortAllocation[] = [];
    try {
      const res = await fetch('/api/config/read?path=' + encodeURIComponent('/opt/etc/xray/03_inbounds.json'));
      if (res.ok) {
        const data = await res.json();
        if (data && Array.isArray(data.inbounds)) {
          for (const ib of data.inbounds) {
            if (ib && ib.port) {
              xrayPorts.push({
                port: Number(ib.port),
                engine: 'xray',
                purpose: ib.tag || 'inbound'
              });
            }
          }
        }
      }
    } catch (e) {
      console.warn('Failed to load Xray inbounds for port checking:', e);
    }

    const allPorts = [...mihomoPorts, ...xrayPorts];
    const collisions = findPortCollisions(allPorts);
    if (collisions.length > 0) {
      const details = collisions.map(group => {
        const portNum = group[0].port;
        const descriptions = group.map(p => `${p.engine} (${p.purpose})`).join(' vs ');
        return `Port ${portNum}: ${descriptions}`;
      }).join('\n');
      
      const msg = ru 
        ? `Обнаружен конфликт портов:\n${details}\n\nПродолжить применение?`
        : `Port collisions detected:\n${details}\n\nDo you want to proceed?`;
      if (!confirm(msg)) {
        applyLoading = false;
        return;
      }
    }

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

      const activeKernel = $capabilities?.active_kernel;
      let restartUrl = '/api/service/control?action=restart';
      if (activeKernel && activeKernel !== 'mihomo') {
        const msg = ru
          ? `Активным ядром сейчас является '${activeKernel}'. Хотите переключить его на 'mihomo'?`
          : `Active kernel is currently '${activeKernel}'. Do you want to switch to 'mihomo'?`;
        if (confirm(msg)) {
          restartUrl = '/api/service/control?action=switch_kernel&kernel=mihomo';
        }
      }

      const restartRes = await fetch(restartUrl, {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      });

      if (!restartRes.ok) {
        throw new Error('Failed to restart service');
      }

      await fetchCapabilities();

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
      const activeKernel = $capabilities?.active_kernel;
      let restartUrl = '/api/service/control?action=restart';
      if (activeKernel && activeKernel !== 'mihomo') {
        const msg = ru
          ? `Активным ядром сейчас является '${activeKernel}'. Хотите переключить его на 'mihomo'?`
          : `Active kernel is currently '${activeKernel}'. Do you want to switch to 'mihomo'?`;
        if (confirm(msg)) {
          restartUrl = '/api/service/control?action=switch_kernel&kernel=mihomo';
        }
      }

      const restartRes = await fetch(restartUrl, {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      });
      if (!restartRes.ok) {
        throw new Error('Failed to restart service');
      }

      await fetchCapabilities();

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
      <button class="btn btn-secondary" onclick={loadSchema}>{ru ? 'Повторить попытку' : 'Retry'}</button>
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
        <button class="btn btn-secondary" onclick={openInEditor}>
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
        <button class="btn btn-primary" onclick={copyYAML} disabled={!yaml}>
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

  {#if preservedKeys.length > 0 && !dismissMergeWarning}
    <div class="alert alert-warning alert-dismissible" style="margin: 0 0 16px 0;" role="status">
      <span aria-hidden="true">⚠️</span>
      <div>
        <strong>{$t('editor.constructor_merge_warning_title')}</strong>
        <div style="margin-top: 2px;">
          {$t('editor.constructor_merge_warning_body', { keys: preservedKeys.join(', ') })}
        </div>
      </div>
      <button type="button" class="alert-close-btn" onclick={() => {
        dismissMergeWarning = true;
        localStorage.setItem('xcp:dismissed_warning:preserved_keys', preservedKeys.join(','));
      }} aria-label={$t('app.close') || 'Close'}>&times;</button>
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
          onchange={(e) => {
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

      {#if activePreset === 'zkeen-selective' && !hasZkeenGeodata && !dismissZkeenGeodataWarning}
        <div class="alert alert-warning alert-dismissible" style="margin-bottom: 16px; padding: 8px 12px; font-size: 13px; display: flex; align-items: center; gap: 8px; border-radius: var(--radius-sm);">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="flex-shrink: 0;"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
          <span>{$t('editor.requires_zkeen_geodata')}</span>
          <button type="button" class="alert-close-btn" style="top: 50%; transform: translateY(-50%);" onclick={() => {
            dismissZkeenGeodataWarning = true;
            localStorage.setItem('xcp:dismissed_warning:zkeen_geodata', activePreset);
          }} aria-label={$t('app.close') || 'Close'}>&times;</button>
        </div>
      {/if}

      <!-- Rule providers -->
      <div class="rule-providers-row">
        <label class="form-label" for="rp-select">{$t('editor.constructor_rule_providers')}:</label>
        <select
          id="rp-select"
          class="form-select rp-select"
          bind:value={activeRuleProvider}
          onchange={(e) => {
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
            onclick={() => {
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
                class="item-edit"
                onclick={() => editProxy(p)}
                title={ru ? 'Редактировать' : 'Edit'}>✎</button
              >
              <button
                class="item-del"
                onclick={() => removeProxy(p.id)}
                title={ru ? 'Удалить' : 'Remove'}>✕</button
              >
            </div>
          {/each}

          {#if showProxyForm}
            <ProxyForm
              bind:np
              isEdit={!!editingProxyId}
              onSave={addProxy}
              onCancel={() => {
                showProxyForm = false;
                editingProxyId = null;
                np = newProxyDefaults('vless');
              }}
            />
          {:else}
            {#if mihomoProviders && mihomoProviders.length > 0}
              <div class="sec-subtitle" style="margin-top: 16px; margin-bottom: 8px; border-top: 1px solid var(--border); padding-top: 12px; font-weight: 600; font-size: 13px; color: var(--fg-secondary);">
                {ru ? 'Провайдеры подписок (proxy-providers)' : 'Subscription providers (proxy-providers)'}
              </div>
              {#each mihomoProviders as sub}
                <div class="item-row" style="border-left: 3px solid var(--success);">
                  <span class="item-badge type-mihomo" style="background: rgba(16, 185, 129, 0.15); color: var(--success); border-color: rgba(16, 185, 129, 0.3);">mihomo</span>
                  <span class="item-name">{sub.name}</span>
                  <span class="item-meta" title={sub.url} style="max-width: 350px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                    {sub.url}
                  </span>
                </div>
              {/each}
            {/if}

            <div class="constructor-proxy-list" style="display: flex; gap: 8px; flex-wrap: wrap;">
              <button class="add-btn" style="flex: 1; min-width: 120px;" onclick={() => (showProxyForm = true)}>
                + {ru ? 'Добавить прокси' : 'Add proxy'}
              </button>
              <button
                class="add-btn import-btn"
                style="flex: 1; min-width: 120px;"
                onclick={loadSubscriptionProxies}
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
              <button class="add-btn import-btn" style="flex: 1; min-width: 120px;" onclick={openImportModal}>
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
                        onerror={() => {
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
                        onchange={(e) => {
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
                        onchange={(e) => {
                          const val = e.currentTarget.value;
                          g.proxies = [val, ...g.proxies.slice(1).filter((p) => p !== val)];
                          groups = [...groups];
                        }}
                      >
                        <option value="DIRECT">DIRECT</option>
                        <option value="REJECT">REJECT</option>
                        {#each allProxyNames.filter((n) => n !== 'DIRECT' && n !== 'REJECT' && n !== g.name) as n}
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
                {#if g.useProviders && g.useProviders.length > 0}
                  <span
                    class="item-badge"
                    style="background: rgba(16, 185, 129, 0.2); color: #34d399; font-size: 10px; text-transform: none;"
                    title={g.useProviders.join(', ')}
                    >use: {g.useProviders.length}</span
                  >
                {/if}
                {#if g.type === 'load-balance' && g.strategy}
                  <span
                    class="item-badge"
                    style="background: rgba(245, 158, 11, 0.2); color: #fbbf24; font-size: 10px; text-transform: none;"
                    >{g.strategy}</span
                  >
                {/if}
                <span class="item-meta">{g.proxies.length} {ru ? 'прокси' : 'proxies'}</span>
                <button
                  class="item-edit"
                  onclick={() => editGroup(g)}
                  title={ru ? 'Редактировать' : 'Edit'}>✎</button
                >
                <button class="item-del" onclick={() => removeGroup(g.id)}>✕</button>
              </div>
            {/each}

            {#if showGroupForm}
              <GroupForm
                bind:ng
                isEdit={!!editingGroupId}
                {allProxyNames}
                {mihomoProviders}
                onSave={addGroup}
                onCancel={() => {
                  showGroupForm = false;
                  editingGroupId = null;
                  ng = {
                    name: '',
                    type: 'select',
                    proxies: [],
                    includeAll: false,
                    url: 'https://www.gstatic.com/generate_204',
                    interval: 300,
                    useProviders: [],
                    strategy: 'consistent-hash'
                  };
                }}
              />
            {:else}
              <div class="constructor-proxy-list" style="display: flex; gap: 8px;">
                <button class="add-btn" style="flex: 1;" onclick={() => (showGroupForm = true)}>
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
                          onchange={(e) => {
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
                          onchange={(e) => {
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
                <button class="order-btn" onclick={() => moveRule(r.id, -1)} disabled={i === 0}
                  >▲</button
                >
                <button
                  class="order-btn"
                  onclick={() => moveRule(r.id, 1)}
                  disabled={i === rules.length - 1}>▼</button
                >
              </div>
              <span class="item-badge type-rule">{r.type}</span>
              {#if r.type !== 'MATCH'}
                <span class="item-name rule-value">{r.value}</span>
              {/if}
              <span class="item-meta">→ {r.outbound}</span>
              <button class="item-del" onclick={() => removeRule(r.id)}>✕</button>
            </div>
          {/each}

          {#if showRuleForm}
            <RuleForm
              bind:nr
              {allProxyNames}
              onSave={addRule}
              onCancel={() => (showRuleForm = false)}
            />
          {:else}
            <button class="add-btn" onclick={() => (showRuleForm = true)}>
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
                <button class="btn btn-secondary btn-sm" style="font-size: 12px; padding: 4px 8px; display: flex; align-items: center; gap: 4px;" onclick={enableDNSRedirect} disabled={dnsRedirectLoading}>
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
                onchange={(e) =>
                  (dns.nameservers = e.currentTarget.value.split('\n').filter(Boolean))}
              ></textarea>
            </div>
            <div class="form-row">
              <label class="form-label">Fallback</label>
              <textarea
                class="form-textarea"
                value={dns.fallback.join('\n')}
                rows="2"
                onchange={(e) =>
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
                onchange={(e) =>
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
          <button class="btn btn-secondary btn-sm" onclick={copyYAML}>
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
             <button class="btn btn-secondary" style="flex: 1;" onclick={openInEditor}>
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
              onclick={handleApplyMihomo}
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
              onclick={handleUndo}
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
    onclick={() => (showApplyConfirm = false)}
    onkeydown={(e) => e.key === 'Escape' && (showApplyConfirm = false)}
  >
    <div class="modal-card" role="presentation" onclick={(e) => e.stopPropagation()}>
      <div class="modal-card-header">
        <h2>{$t('editor.apply_confirm_title')}</h2>
        <button class="modal-close-btn" onclick={() => (showApplyConfirm = false)}>&times;</button>
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
        <button class="btn btn-secondary" onclick={() => (showApplyConfirm = false)}>
          {$t('app.cancel')}
        </button>
        <button class="btn btn-primary" onclick={handleApplyMihomo} disabled={applyLoading}>
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
    onclick={closeImportModal}
    onkeydown={(e) => e.key === 'Escape' && closeImportModal()}
  >
    <div class="modal-card" role="presentation" onclick={(e) => e.stopPropagation()}>
      <div class="modal-card-header">
        <h2>{$t('subscr.import_modal_title')}</h2>
        <button class="modal-close-btn" onclick={closeImportModal}>&times;</button>
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
                      onclick={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
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
                      onclick={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
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
        <button class="btn btn-secondary" onclick={closeImportModal} disabled={importLoading}>
          {$t('app.cancel')}
        </button>
        {#if importStep === 1}
          <button
            class="btn btn-primary"
            onclick={parseImportLink}
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
            onclick={confirmImportNode}
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

  .item-edit {
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

  .item-edit:hover {
    color: var(--primary);
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
    background: var(--bg-surface);
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
    background: #1e1e1e;
    color: #d4d4d4;
    font-family: var(--font-mono, monospace);
    font-size: var(--font-size-xs, 0.75rem);
    line-height: 1.5;
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
