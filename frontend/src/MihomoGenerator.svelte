<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Modal from './components/Modal.svelte';
  import DraftRestoreBanner from './components/DraftRestoreBanner.svelte';
  import { registerDirtySource, getDraft, clearDraft, type DraftRecord } from './lib/dirtyRegistry';
  import { activateRestartGrace } from './lib/serviceGrace';
  import { currentLang, t, tp } from './i18n';
  import { capabilities, showToast, fetchCapabilities, showConfirm } from './stores';
  import { apiFetch, apiFetchJSON } from './lib/api';
  import { parseValidationError } from './lib/errorParser';
  import {
    findPortCollisions,
    parseXrayPorts,
    parseMihomoPorts,
    parseMihomoListenerPorts,
    type PortAllocation
  } from './lib/portChecker';
  import {
    slugifyProviderName,
    yamlSafeString,
    sanitizeUrl,
    unquote,
    generateYAML as generateMihomoYAML,
    populateMihomoFromYAML as populateMihomoFromYAML_raw,
    ZKEEN_RULE_PROVIDERS,
    type RuleProvider,
    type Listener
  } from './lib/mihomoYaml';
  import ProxyForm from './components/mihomo/ProxyForm.svelte';
  import GroupForm from './components/mihomo/GroupForm.svelte';
  import RuleForm from './components/mihomo/RuleForm.svelte';
  import PreflightWarnings, {
    type PreflightWarning
  } from './components/editor/PreflightWarnings.svelte';

  let {
    onSwitchTab = () => {},
    selectedFile = '',
    onInsertIntoEditor = () => {},
    embedded = false,
    initialPreset = '',
    invalidateCache = false
  }: {
    onSwitchTab?: (tab: string) => void;
    selectedFile?: string;
    onInsertIntoEditor?: (content: string) => void;
    embedded?: boolean;
    initialPreset?: string;
    invalidateCache?: boolean;
  } = $props();

  type ProxyType =
    'vless' | 'hysteria2' | 'tuic' | 'ss' | 'vmess' | 'trojan' | 'wireguard' | 'socks5' | 'http';
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
    // vless/vmess/trojan
    uuid?: string;
    flow?: string;
    // reality
    publicKey?: string;
    shortId?: string;
    servername?: string;
    // hy2/trojan/ss
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
    enabled?: boolean;
    // Advanced options (Phase 102)
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
    awgH1?: number;
    awgH2?: number;
    awgH3?: number;
    awgH4?: number;
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
    hidden?: boolean; // NEW (D-02): hides group from Mihomo selector UI
    tolerance?: number; // NEW (D-02): latency tolerance ms for url-test
    maxFailedTimes?: number; // NEW (D-02): maps to YAML key max-failed-times
    useProviders?: string[];
    strategy?: 'round-robin' | 'consistent-hashing' | 'sticky-sessions';
    lazy?: boolean;
    expectedStatus?: string;
    excludeType?: string;
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
  let activeSection = $state<
    'proxies' | 'groups' | 'rules' | 'dns' | 'tun' | 'rulesets' | 'listeners'
  >('proxies');
  let proxies: Proxy[] = $state([]);
  let groups: ProxyGroup[] = $state([]);
  let rules: Rule[] = $state([]);
  let listeners = $state<Listener[]>([]);
  let listenersRaw = $state<string | null>(null);
  let listenersReadOnly = $state(false);

  let showListenerForm = $state(false);
  let editingListenerId = $state<string | null>(null);

  function newListenerDefaults(): Listener {
    return {
      id: crypto.randomUUID(),
      name: '',
      type: 'mixed',
      listen: '0.0.0.0',
      port: '',
      udp: true,
      users: []
    };
  }

  let newListener = $state<Listener>(newListenerDefaults());

  let listenerNameValid = $derived(newListener.name.trim().length > 0);
  let listenerPortValid = $derived.by(() => {
    if (!newListener.port || !newListener.port.trim()) return false;
    const n = Number(newListener.port);
    return Number.isInteger(n) && n >= 1 && n <= 65535;
  });
  let listenerSSPasswordValid = $derived(
    newListener.type !== 'shadowsocks' || (newListener.password?.trim().length ?? 0) > 0
  );

  function openListenerForm(l?: Listener) {
    if (l) {
      editingListenerId = l.id;
      newListener = {
        ...l,
        users: l.users ? l.users.map((u) => ({ ...u })) : []
      };
    } else {
      editingListenerId = null;
      newListener = newListenerDefaults();
    }
    showListenerForm = true;
  }

  function cancelListenerForm() {
    showListenerForm = false;
    editingListenerId = null;
  }

  function saveListener() {
    const toSave: Listener = {
      ...newListener,
      name: newListener.name.trim(),
      listen: newListener.listen.trim() || '0.0.0.0',
      port: String(newListener.port).trim(),
      proxy: newListener.proxy?.trim() || undefined,
      users:
        newListener.type === 'mixed' || newListener.type === 'socks' || newListener.type === 'http'
          ? (newListener.users || []).filter((u) => u.username.trim() || u.password.trim())
          : undefined,
      cipher: newListener.type === 'shadowsocks' ? newListener.cipher || 'aes-256-gcm' : undefined,
      password: newListener.type === 'shadowsocks' ? newListener.password || '' : undefined,
      udp:
        newListener.type !== 'http' && newListener.type !== 'redirect'
          ? !!newListener.udp
          : undefined
    };

    if (editingListenerId) {
      listeners = listeners.map((item) => (item.id === editingListenerId ? toSave : item));
    } else {
      listeners = [...listeners, toSave];
    }
    showListenerForm = false;
    editingListenerId = null;
  }

  function addListenerUser() {
    newListener.users = [...(newListener.users || []), { username: '', password: '' }];
  }

  function removeListenerUser(index: number) {
    newListener.users = (newListener.users || []).filter((_, i) => i !== index);
  }
  let activePreset: string = $state('');
  let activeRuleProvider = $state<'none' | 'zkeen' | 'metacubex'>('none');
  let externalControllerType = $state<'unix' | 'tcp'>('unix');
  let externalControllerTarget = $state<string>('127.0.0.1:9090');
  let subscriptions: any[] = $state([]);
  let mihomoProviders: any[] = $state([]);
  let lastParsedProviders: any[] = $state([]);
  let saveWarnings = $state<PreflightWarning[]>([]);

  function mergeMihomoProviders(dbSubs: any[], parsedProviders: any[]) {
    const dbMapByUrl = new Map<string, any>();
    const dbMapByName = new Map<string, any>();

    function cleanUrl(urlStr: string): string {
      if (!urlStr) return '';
      try {
        const match = urlStr.match(/[?&]url=([^&]+)/);
        if (match) {
          return decodeURIComponent(match[1]).trim().toLowerCase();
        }
        return urlStr.trim().toLowerCase();
      } catch {
        return urlStr.trim().toLowerCase();
      }
    }

    for (const sub of dbSubs) {
      const originalUrl = cleanUrl(sub.url || '');
      if (originalUrl) {
        dbMapByUrl.set(originalUrl, sub);
      }
      const providerName = slugifyProviderName(
        sub.profile_title || '',
        sub.name || '',
        sub.url || '',
        sub.id
      );
      dbMapByName.set(providerName, sub);
    }

    const merged = [...dbSubs];

    for (const p of parsedProviders) {
      const originalUrl = cleanUrl(p.url || '');
      const hasMatch = (originalUrl && dbMapByUrl.has(originalUrl)) || dbMapByName.has(p.name);

      if (!hasMatch) {
        const rawUrl = originalUrl || p.url;
        merged.push({
          id: p.id,
          name: p.name,
          url: rawUrl,
          interval: p.interval,
          enabled: true,
          enable_mihomo: true,
          isVirtual: true,
          rawLines: p.rawLines
        });
      }
    }

    return merged;
  }

  let hasXraySubscriptions = $derived(subscriptions.some((s) => s.enable_xray));
  let hasMihomoProviders = $derived(mihomoProviders.length > 0);
  let hasZkeenGeodata = $state(false);
  let existingTproxyPort: number | null = $state(null);
  let existingRedirPort: number | null = $state(null);
  let dns: DNSConfig = $state({
    enabled: false,
    nameservers: ['https://doh.pub/dns-query', '223.5.5.5'],
    fallback: ['https://8.8.8.8/dns-query', '1.1.1.1'],
    enhancedMode: 'fake-ip',
    fakeIPRange: '198.18.0.1/16'
  });
  let tun: TUNConfig = $state({
    enabled: false,
    stack: 'mixed',
    autoRoute: true,
    autoDetectInterface: true,
    dnsHijack: ['any:53']
  });
  let preservedKeys: string[] = $state([]);
  let dismissMergeWarning = $state(false);
  let lastPreservedKeysStr = '';
  $effect(() => {
    if (preservedKeys.join(',') !== lastPreservedKeysStr) {
      lastPreservedKeysStr = preservedKeys.join(',');
      const dismissed = localStorage.getItem('xcp:dismissed_warning:preserved_keys');
      dismissMergeWarning = dismissed === lastPreservedKeysStr;
    }
  });

  let dismissZkeenGeodataWarning = $state(false);
  let lastActivePreset = '';
  $effect(() => {
    if (activePreset !== lastActivePreset) {
      if (lastActivePreset && activePreset !== lastActivePreset) {
        localStorage.removeItem('xcp:dismissed_warning:zkeen_geodata');
      }
      lastActivePreset = activePreset;
      const dismissed = localStorage.getItem('xcp:dismissed_warning:zkeen_geodata');
      dismissZkeenGeodataWarning = dismissed === activePreset;
    }
  });

  // Safe Merge State
  let safeMergeEnabled = $state(true);
  let safeMergeExpanded = $state(false);

  // Resizable Splitter State
  const PREVIEW_STORAGE_KEY = 'mihomo_builder_preview_width';
  let previewWidth = $state(440);
  let showPreviewPane = $state(true);
  let isResizingPreview = $state(false);
  let copyFeedback = $state(false);

  // Preset Tracking
  let lastAppliedPreset = $state('');
  let presetBaseline = $state('');

  let sniffer = $state({
    enabled: false,
    sniffHttp: true,
    sniffTls: true,
    sniffQuic: true
  });
  let canUndo = $state(false);
  let isDirty = $state(false);
  function checkUndo() {
    canUndo = !!localStorage.getItem('xcp_prev_mihomo_yaml');
  }

  // Import Node states
  let showImportModal = $state(false);
  let importLink = $state('');
  let importTag = $state('');
  let importStep = $state(1); // 1: Input link, 2: Preview & Confirm tag
  let importLoading = $state(false);
  let importNodes: { link: string; outbound: any; tag: string; rowError?: string | null }[] =
    $state([]);
  let importErrorMsg = $state('');

  // Form visibility
  let showProxyForm = $state(false);
  let showGroupForm = $state(false);
  let showRuleForm = $state(false);
  let editingProxyId: string | null = $state(null);
  let editingGroupId: string | null = $state(null);

  // New proxy form
  let np: Omit<Proxy, 'id'> = $state(newProxyDefaults('vless'));
  function newProxyDefaults(type: ProxyType): Omit<Proxy, 'id'> {
    return {
      name: '',
      type,
      server: '',
      port: type === 'wireguard' ? 51820 : 443,
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
      fingerprint: 'chrome',
      // WireGuard / AmneziaWG defaults (TMPL-08)
      wgPrivateKey: '',
      wgPublicKey: '',
      wgIp: '',
      wgPresharedKey: '',
      wgMtu: 1420,
      awgEnabled: false,
      awgJc: 4,
      awgJmin: 40,
      awgJmax: 70,
      awgS1: 15,
      awgS2: 40,
      awgH1: 1000000001,
      awgH2: 1000000002,
      awgH3: 1000000003,
      awgH4: 1000000004
    };
  }
  let lastType = 'vless';
  $effect(() => {
    if (np.type && np.type !== lastType) {
      const port = np.type === 'wireguard' ? 51820 : lastType === 'wireguard' ? 443 : np.port;
      lastType = np.type;
      np = { ...newProxyDefaults(np.type), name: np.name, server: np.server, port };
    }
  });

  // New group form
  let ng: Omit<ProxyGroup, 'id'> = $state({
    name: '',
    type: 'select',
    proxies: [],
    includeAll: false,
    url: 'https://www.gstatic.com/generate_204',
    interval: 300,
    useProviders: [],
    strategy: undefined
  });

  // New rule form
  let nr: Omit<Rule, 'id'> = $state({ type: 'DOMAIN-SUFFIX', value: '', outbound: 'DIRECT' });

  // Moved state variables to prevent duplicate declarations and temporal dead zone (TDZ) issues
  let validationError = $state('');
  let schema: any = $state(null);
  let schemaLoading = $state(true);
  let schemaError = $state('');
  let showApplyConfirm = $state(false);
  let applyLoading = $state(false);
  let dnsRedirectLoading = $state(false);

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
      name: 'Blocked Services',
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
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/YouTube.png'
    },
    {
      name: 'Discord',
      type: 'select',
      includeAll: true,
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Discord.png'
    },
    {
      name: 'Twitch',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Twitch.png'
    },
    {
      name: 'Reddit',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
      icon: 'https://www.redditstatic.com/shreddit/assets/favicon/192x192.png'
    },
    {
      name: 'Meta',
      type: 'select',
      includeAll: true,
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://github.com/zxc-rv/assets/raw/main/group-icons/meta.png'
    },
    {
      name: 'Spotify',
      type: 'select',
      includeAll: true,
      excludeFilter: '🇷🇺',
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Spotify.png'
    },
    {
      name: 'Speedtest',
      type: 'select',
      includeAll: true,
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Speedtest.png'
    },
    {
      name: 'Telegram',
      type: 'select',
      includeAll: true,
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Telegram.png'
    },
    {
      name: 'Steam',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Steam.png'
    },
    {
      name: 'CDN',
      type: 'select',
      includeAll: true,
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://www.svgrepo.com/show/396567/globe-with-meridians.svg'
    },
    {
      name: 'Google',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Google_Search.png'
    },
    {
      name: 'GitHub',
      type: 'select',
      includeAll: true,
      proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/GitHub.png'
    },
    {
      name: 'AI',
      type: 'select',
      includeAll: true,
      excludeFilter: '🇷🇺',
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Bot.png'
    },
    {
      name: 'Twitter',
      type: 'select',
      includeAll: true,
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Twitter.png'
    },
    {
      name: 'TikTok',
      type: 'select',
      includeAll: true,
      proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
      icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/TikTok.png'
    }
  ];

  const META_RULE_SETS_BY_CATEGORY: Record<
    string,
    Array<{ id: string; label: string; type: 'geosite' | 'geoip'; defaultOutbound: string }>
  > = {
    'Social Networks': [
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
    Services: [
      { id: 'spotify', label: 'Spotify', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'steam', label: 'Steam', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'github', label: 'GitHub', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'openai', label: 'OpenAI', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'netflix', label: 'Netflix', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'google', label: 'Google', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'amazon', label: 'Amazon', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'speedtest', label: 'Speedtest', type: 'geosite', defaultOutbound: 'Proxy' }
    ],
    'Networks/CDN': [
      { id: 'cloudflare', label: 'Cloudflare', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'akamai', label: 'Akamai', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'fastly', label: 'Fastly', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'digitalocean', label: 'DigitalOcean', type: 'geosite', defaultOutbound: 'DIRECT' },
      { id: 'private', label: 'Private Network', type: 'geoip', defaultOutbound: 'DIRECT' },
      { id: 'telegram', label: 'Telegram IP', type: 'geoip', defaultOutbound: 'Proxy' }
    ],
    Blocked: [
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

  let selectedMetaRuleSets: Map<string, string> = $state(new Map());

  const META_BASE_URL = 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo';

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
          proxies:
            g.name === 'Selective' || g.name === 'Proxy'
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
        lastAppliedPreset = id;
        presetBaseline = JSON.stringify({
          activeRuleProvider,
          groups: groups.map((g) => ({ name: g.name, type: g.type, enabled: g.enabled })),
          rules: rules.map((r) => ({ type: r.type, value: r.value, outbound: r.outbound }))
        });
        if (!silent) {
          isDirty = true;
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
          name: 'GLOBAL',
          type: 'select',
          proxies: proxies.map((p) => p.name),
          includeAll: true,
          excludeFilter: '',
          url: 'https://www.gstatic.com/generate_204',
          interval: 300
        }
      ];
      rules = [{ id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'GLOBAL' }];
      activeRuleProvider = 'none';
      selectedMetaRuleSets = new Map();
    } else if (id === 'zkeen-selective') {
      groups = (
        schema?.mihomo?.presets?.find((x: any) => x.id === 'zkeen-selective')?.groups ||
        ZKEEN_16_GROUPS
      ).map((g: any) => ({
        id: crypto.randomUUID(),
        name: g.name,
        type: g.type || 'select',
        proxies:
          g.name === 'GLOBAL'
            ? ['DIRECT', ...proxies.map((pr) => pr.name)]
            : [...(g.proxies || [])],
        includeAll: g.include_all ?? g.includeAll ?? false,
        excludeFilter: g.exclude_filter || g.excludeFilter || '',
        url: g.url || 'https://www.gstatic.com/generate_204',
        interval: g.interval || 300,
        icon: g.icon || '',
        enabled: true,
        hidden: g.hidden ?? false,
        tolerance: g.tolerance ?? undefined,
        maxFailedTimes: g.max_failed_times ?? g.maxFailedTimes ?? undefined
      }));
      rules = [
        { id: crypto.randomUUID(), type: 'GEOIP', value: 'ru', outbound: 'DIRECT' },
        { id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'DIRECT' }
      ];
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
        {
          id: crypto.randomUUID(),
          type: 'RULE-SET',
          value: 'refilter@domain',
          outbound: 'Selective'
        },
        { id: crypto.randomUUID(), type: 'RULE-SET', value: 'private@ip', outbound: 'DIRECT' },
        { id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'DIRECT' }
      ];
      activeRuleProvider = 'zkeen';
      selectedMetaRuleSets = new Map();
    }
    lastAppliedPreset = id;
    presetBaseline = JSON.stringify({
      activeRuleProvider,
      groups: groups.map((g) => ({ name: g.name, type: g.type, enabled: g.enabled })),
      rules: rules.map((r) => ({ type: r.type, value: r.value, outbound: r.outbound }))
    });
    if (!silent) {
      isDirty = true;
      showToast('success', $t('editor.preset_applied'));
    }
  }

  const isPresetModified = $derived.by(() => {
    if (!lastAppliedPreset || !presetBaseline) return false;
    const current = JSON.stringify({
      activeRuleProvider,
      groups: groups.map((g) => ({ name: g.name, type: g.type, enabled: g.enabled })),
      rules: rules.map((r) => ({ type: r.type, value: r.value, outbound: r.outbound }))
    });
    return current !== presetBaseline;
  });

  // ── Import proxies from subscriptions ───────────────────────────────────
  async function loadSubscriptions() {
    try {
      const res = await apiFetch('/api/subscriptions');
      if (!res.ok) return;
      const subs = await res.json();
      if (Array.isArray(subs)) {
        subscriptions = subs.filter((s) => s.enabled);
        const dbMihomo = subs.filter((s) => s.enabled && s.enable_mihomo);
        mihomoProviders = mergeMihomoProviders(dbMihomo, lastParsedProviders);
      } else {
        subscriptions = [];
        mihomoProviders = mergeMihomoProviders([], lastParsedProviders);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      console.error(e);
    }
  }

  // ── Import proxies from subscriptions ───────────────────────────────────
  async function loadSubscriptionProxies() {
    try {
      const res = await apiFetch('/api/subscriptions');
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
        const nr = await apiFetch(`/api/subscriptions/nodes?id=${sub.id}`);
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
      const data = await apiFetchJSON<any>('/api/outbound/parse', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ links: lines })
      });

      const parsedItems = (Array.isArray(data) ? data : data?.data) || [];

      if (parsedItems.length > 0) {
        const newImportNodes = [];
        const existingNames = proxies.map((p) => p.name);

        for (let i = 0; i < parsedItems.length; i++) {
          const result = parsedItems[i];
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

      if (mappedList.length > 0) {
        proxies = [...proxies, ...mappedList];
        isDirty = true;
        showToast('success', $t('subscr.import_success', { count: mappedList.length }));
      } else {
        proxies = [...proxies, ...mappedList];
      }

      if (skippedCount > 0) {
        showToast('warning', $t('subscr.partial_map_warning'));
      }

      showImportModal = false;
    } catch (e: any) {
      importErrorMsg = e.message || $t('subscr.import_error');
    }
  }

  function populateMihomoFromYAML(text: string) {
    if (!text || text.trim() === '') {
      applyPreset('zkeen-selective', true);
      lastParsedProviders = [];
      mihomoProviders = mergeMihomoProviders(
        subscriptions.filter((s) => s.enable_mihomo),
        []
      );
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
      externalControllerType = res.externalControllerType || 'unix';
      externalControllerTarget = res.externalControllerTarget || '127.0.0.1:9090';
      listeners = res.listeners || [];
      listenersRaw = res.listenersRaw || null;
      listenersReadOnly = res.listenersReadOnly || false;

      lastParsedProviders = res.mihomoProviders || [];
      mihomoProviders = mergeMihomoProviders(
        subscriptions.filter((s) => s.enable_mihomo),
        lastParsedProviders
      );

      if (res.groups.length === 0 && res.proxies.length === 0) {
        applyPreset('zkeen-selective', true);
      }
    } catch (err: any) {
      showToast('warning', $t('mihomo.read_config_fallback'));
      applyPreset('zkeen-selective', true);
    }
  }

  let configLoadedForPath = '';

  async function loadConfig(path: string, force = false) {
    if (!path) return;
    if (configLoadedForPath === path && !force) return;
    configLoadedForPath = path;
    try {
      const res = await apiFetch(`/api/config/read?path=${encodeURIComponent(path)}`);
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
      if (e?.status === 401) return;
      showToast('error', $t('mihomo.config_load_error', { err: e.message }));
    }
    await loadSubscriptions();
  }

  async function checkZkeenGeodata() {
    try {
      const res = await apiFetch('/api/dat/tags?name=geosite.dat');
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

  let detectedDraft = $state<DraftRecord | null>(null);
  let unregisterDirty: (() => void) | null = null;

  function handleRestoreDraft() {
    if (detectedDraft?.data?.yaml) {
      populateMihomoFromYAML(detectedDraft.data.yaml);
      isDirty = true;
      clearDraft('mihomo_generator');
      detectedDraft = null;
      showToast('success', $t('draft.restored_toast'));
    }
  }

  function handleDiscardDraft() {
    clearDraft('mihomo_generator');
    detectedDraft = null;
    showToast('info', $t('draft.discarded_toast'));
  }

  onMount(async () => {
    try {
      const savedWidth = localStorage.getItem(PREVIEW_STORAGE_KEY);
      if (savedWidth) {
        const parsed = parseInt(savedWidth, 10);
        if (!isNaN(parsed) && parsed >= 280 && parsed <= 800) {
          previewWidth = parsed;
        }
      }
    } catch {
      // ignore
    }

    await loadSchema();
    await loadConfig(selectedFile || '/opt/etc/mihomo/config.yaml', true);
    await checkZkeenGeodata();
    checkUndo();

    const draft = getDraft('mihomo_generator');
    if (draft) {
      detectedDraft = draft;
    }

    unregisterDirty = registerDirtySource('mihomo_generator', {
      name: $t('editor.tab_constructor') || 'Mihomo Generator',
      isDirty: () => isDirty,
      onSave: async () => {
        await handleApplyMihomo(true);
        return !isDirty;
      },
      getDraft: () => ({
        yaml: generateYAML()
      }),
      restoreDraft: (draftRecord) => {
        if (draftRecord?.data?.yaml) {
          detectedDraft = draftRecord;
          handleRestoreDraft();
        }
      }
    });
  });

  onDestroy(() => {
    if (unregisterDirty) {
      unregisterDirty();
      unregisterDirty = null;
    }
  });

  $effect(() => {
    if (selectedFile) {
      loadConfig(selectedFile);
    }
  });

  let prevInvalidateCache = false;
  $effect(() => {
    if (invalidateCache && !prevInvalidateCache) {
      prevInvalidateCache = true;
      configLoadedForPath = '';
      loadConfig(selectedFile || '/opt/etc/mihomo/config.yaml', true);
    } else if (!invalidateCache) {
      prevInvalidateCache = false;
    }
  });

  function sanitizeProxyName(name: string): { name: string; sanitized: boolean } {
    const original = name;
    const cleaned = name
      .replace(/[\n\r\t]/g, ' ')
      .replace(/\s+/g, ' ')
      .trim();
    return { name: cleaned, sanitized: cleaned !== original };
  }

  function addProxy() {
    if (!np.name.trim() || !np.server.trim()) return;
    if (np.type === 'hysteria2' && np.obfsType === 'simple' && !np.obfsPassword?.trim()) {
      showToast('error', $t('mihomo.simple_obfs_pass_required'));
      return;
    }
    const { name: cleanName, sanitized } = sanitizeProxyName(np.name);
    if (sanitized) {
      showToast('info', $t('editor.proxy_name_sanitized'));
    }
    if (editingProxyId) {
      proxies = proxies.map((p) =>
        p.id === editingProxyId ? { ...np, name: cleanName, id: editingProxyId } : p
      );
      editingProxyId = null;
    } else {
      proxies = [...proxies, { ...np, name: cleanName, id: crypto.randomUUID() }];
    }
    isDirty = true;
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
    isDirty = true;
  }

  function duplicateProxy(p: Proxy) {
    const baseCopyName = `${p.name}_copy`;
    const uniqueName = generateUniqueProxyName(
      baseCopyName,
      proxies.map((pr) => pr.name)
    );
    const { name: cleanName } = sanitizeProxyName(uniqueName);
    const newP: Proxy = {
      ...p,
      id: crypto.randomUUID(),
      name: cleanName
    };
    proxies = [...proxies, newP];
    isDirty = true;
    showToast('info', $t('app.duplicate'));
  }

  function toggleProxy(id: string) {
    proxies = proxies.map((p) =>
      p.id === id ? { ...p, enabled: p.enabled === false ? true : false } : p
    );
    isDirty = true;
  }

  function addGroup() {
    if (!ng.name.trim()) return;
    if (editingGroupId) {
      groups = groups.map((g) =>
        g.id === editingGroupId
          ? {
              ...ng,
              id: editingGroupId,
              proxies: [...ng.proxies],
              useProviders: ng.useProviders ? [...ng.useProviders] : []
            }
          : g
      );
      editingGroupId = null;
    } else {
      groups = [
        ...groups,
        {
          ...ng,
          id: crypto.randomUUID(),
          proxies: [...ng.proxies],
          useProviders: ng.useProviders ? [...ng.useProviders] : []
        }
      ];
    }
    isDirty = true;
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
    isDirty = true;
  }

  function addRule() {
    rules = [
      ...rules,
      {
        id: crypto.randomUUID(),
        type: nr.type || 'DOMAIN-SUFFIX',
        value: nr.value || '',
        outbound: nr.outbound || 'DIRECT'
      }
    ];
    showRuleForm = false;
    nr = { type: 'DOMAIN-SUFFIX', value: '', outbound: 'DIRECT' };
    isDirty = true;
  }

  function removeRule(id: string) {
    rules = rules.filter((r) => r.id !== id);
    isDirty = true;
  }

  function moveRule(id: string, dir: -1 | 1) {
    const idx = rules.findIndex((r) => r.id === id);
    if (idx < 0) return;
    const next = idx + dir;
    if (next < 0 || next >= rules.length) return;
    const arr = [...rules];
    [arr[idx], arr[next]] = [arr[next], arr[idx]];
    rules = arr;
    isDirty = true;
  }

  // ── YAML generation ─────────────────────────────────────────────────────

  function generateYAML(): string {
    const activeProxies = proxies.filter((p) => p.enabled !== false);
    return generateMihomoYAML({
      proxies: activeProxies,
      groups,
      rules,
      dns,
      tun,
      sniffer,
      activeRuleProvider,
      selectedMetaRuleSets,
      preservedKeys: safeMergeEnabled ? preservedKeys : [],
      existingTproxyPort,
      existingRedirPort,
      externalControllerType,
      externalControllerTarget,
      subscriptions,
      mihomoProviders,
      capabilities: $capabilities,
      hasZkeenGeodata,
      ruleProviders,
      listeners,
      listenersRaw,
      listenersReadOnly
    });
  }

  let yaml = $derived.by(() => {
    // Explicit deps so Svelte 5 tracks them across the function call
    void proxies;
    void groups;
    void rules;
    void listeners;
    void listenersRaw;
    void listenersReadOnly;
    void activeRuleProvider;
    void selectedMetaRuleSets;
    void subscriptions;
    void mihomoProviders;
    void externalControllerType;
    void externalControllerTarget;
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
    void safeMergeEnabled;
    return generateYAML();
  });

  async function copyYAML() {
    if (!yaml) return;
    try {
      await navigator.clipboard.writeText(yaml);
      copyFeedback = true;
      showToast('success', $t('mihomo.yaml_copied'));
      setTimeout(() => {
        copyFeedback = false;
      }, 2000);
    } catch (e) {
      console.error(e);
    }
  }

  function downloadYaml() {
    if (!yaml) return;
    const blob = new Blob([yaml], { type: 'text/yaml;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'config.yaml';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  const yamlFileSize = $derived.by(() => {
    if (!yaml) return '0 B';
    const bytes = new Blob([yaml]).size;
    if (bytes < 1024) return `${bytes} B`;
    return `${(bytes / 1024).toFixed(1)} KB`;
  });

  function startResizePreview(e: MouseEvent | PointerEvent) {
    e.preventDefault();
    isResizingPreview = true;
    const startX = e.clientX;
    const startWidth = previewWidth;

    function onPointerMove(moveEvent: MouseEvent | PointerEvent) {
      const delta = startX - moveEvent.clientX;
      const newWidth = Math.min(800, Math.max(280, startWidth + delta));
      previewWidth = newWidth;
    }

    function onPointerUp() {
      isResizingPreview = false;
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', onPointerUp);
      window.removeEventListener('mousemove', onPointerMove);
      window.removeEventListener('mouseup', onPointerUp);
      try {
        localStorage.setItem(PREVIEW_STORAGE_KEY, String(previewWidth));
      } catch {
        // ignore
      }
    }

    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('mousemove', onPointerMove);
    window.addEventListener('mouseup', onPointerUp);
  }

  function togglePreviewPane() {
    showPreviewPane = !showPreviewPane;
  }

  function openInEditor() {
    if (onInsertIntoEditor) {
      onInsertIntoEditor(yaml);
    } else {
      onSwitchTab('editor');
    }
  }

  let ru = $derived($currentLang === 'ru');

  async function enableDNSRedirect() {
    dnsRedirectLoading = true;
    try {
      const res = await apiFetch('/api/service/dns-redirect', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ enabled: true })
      });
      if (res.ok) {
        showToast('success', $t('mihomo.dns_intercept_enabled'));
        await fetchCapabilities();
      } else {
        const text = await res.text();
        showToast('error', text || $t('mihomo.dns_intercept_error'));
      }
    } catch (err: any) {
      if (err?.status === 401) return;
      showToast('error', err.message || String(err));
    } finally {
      dnsRedirectLoading = false;
    }
  }

  const PROXY_TYPES: ProxyType[] = [
    'vless',
    'hysteria2',
    'tuic',
    'ss',
    'vmess',
    'trojan',
    'wireguard',
    'socks5',
    'http'
  ];
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

  let allProxyNames: string[] = $derived([
    'DIRECT',
    'REJECT',
    ...proxies.map((p) => p.name),
    ...groups.map((g) => g.name)
  ]);

  // Dynamic tabs calculation and auto-switch
  let tabs = $derived([
    ['proxies', $t('mihomo.tab_proxies')],
    ['groups', $t('mihomo.tab_groups')],
    ...(activeRuleProvider === 'metacubex' ? [['rulesets', $t('mihomo.tab_rulesets')]] : []),
    ['rules', $t('mihomo.tab_rules')],
    ['dns', 'DNS'],
    ['tun', 'TUN'],
    ['listeners', $t('mihomo.tab_listeners')]
  ]);

  $effect(() => {
    if (
      activeRuleProvider === 'metacubex' &&
      activeSection !== 'rulesets' &&
      activeSection !== 'proxies' &&
      activeSection !== 'groups' &&
      activeSection !== 'rules' &&
      activeSection !== 'dns' &&
      activeSection !== 'tun' &&
      activeSection !== 'listeners'
    ) {
      activeSection = 'rulesets';
    }
  });

  // extractSection is imported from './lib/mihomoYaml'

  async function loadSchema() {
    schemaLoading = true;
    schemaError = '';
    try {
      const res = await apiFetch('/api/assets/definition');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      schema = await res.json();
    } catch (e: any) {
      if (e?.status === 401) return;
      schemaError = e.message || 'Unknown error';
    } finally {
      schemaLoading = false;
    }
  }

  let ruleProviders = $derived(
    schema && schema.mihomo && schema.mihomo.rule_providers
      ? schema.mihomo.rule_providers
      : ZKEEN_RULE_PROVIDERS
  );

  // findTopLevelSection and replaceMihomoTopLevelSection are imported from './lib/mihomoYaml'

  function collectListenerPortWarnings(): PreflightWarning[] {
    const yaml = generateYAML();
    const topPorts = parseMihomoPorts(yaml);
    const listenerPorts = parseMihomoListenerPorts(yaml);
    const reserved: PortAllocation[] = [
      { port: 5000, engine: 'mihomo', purpose: 'redir-port' },
      { port: 5001, engine: 'mihomo', purpose: 'tproxy-port' },
      { port: 1053, engine: 'mihomo', purpose: 'dns' }
    ];

    const existingKeys = new Set(topPorts.map((p) => `${p.port}:${p.purpose}`));
    const uniqueReserved = reserved.filter((r) => !existingKeys.has(`${r.port}:${r.purpose}`));
    const allAllocations = [...topPorts, ...uniqueReserved, ...listenerPorts];

    const collisions = findPortCollisions(allAllocations);
    const warnings: PreflightWarning[] = [];
    const seenPorts = new Set<number>();

    for (const group of collisions) {
      const hasListener = group.some((p) => p.purpose.startsWith('listener:'));
      if (!hasListener) continue;

      const portNum = group[0].port;
      if (seenPorts.has(portNum)) continue;
      seenPorts.add(portNum);

      const firstListenerIdx = group.findIndex((p) => p.purpose.startsWith('listener:'));
      const other = group.find((p, idx) => idx !== firstListenerIdx) || group[0];
      const conflictName = other.purpose.startsWith('listener:')
        ? other.purpose.slice(9)
        : other.purpose;

      warnings.push({
        code: 'listener_port_collision',
        message: $t('mihomo.listener_port_collision', {
          port: String(portNum),
          conflict: conflictName
        })
      });
    }

    return warnings;
  }

  async function handleApplyMihomo(skipConfirm: boolean | unknown = false) {
    const shouldSkipConfirm = skipConfirm === true;
    if (!shouldSkipConfirm && !showApplyConfirm && proxies.length === 0) {
      if (
        !(await showConfirm({
          title: $t('editor.empty_proxies_title'),
          consequence: $t('editor.empty_proxies_warning'),
          variant: 'warning',
          confirmLabel: $t('app.continue')
        }))
      ) {
        return;
      }
    }
    if (!shouldSkipConfirm && !showApplyConfirm) {
      showApplyConfirm = true;
      return;
    }
    showApplyConfirm = false;
    applyLoading = true;

    // Check port collisions
    let xrayPorts: PortAllocation[] = [];
    try {
      const res = await apiFetch(
        '/api/config/read?path=' + encodeURIComponent('/opt/etc/xray/configs/00_main.json')
      );
      if (res.ok) {
        const text = await res.text();
        xrayPorts = parseXrayPorts(text);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
    }

    let mihomoPorts: PortAllocation[] = [
      { port: existingTproxyPort ?? 5001, engine: 'mihomo', purpose: 'tproxy-port' },
      { port: existingRedirPort ?? 5000, engine: 'mihomo', purpose: 'redir-port' },
      { port: 7890, engine: 'mihomo', purpose: 'mixed-port' }
    ];
    try {
      const resM = await apiFetch(
        '/api/config/read?path=' + encodeURIComponent('/opt/etc/mihomo/config.yaml')
      );
      if (resM.ok) {
        const textM = await resM.text();
        const parsedM = parseMihomoPorts(textM);
        if (parsedM.length > 0) {
          mihomoPorts = parsedM;
        }
      }
    } catch (e: any) {
      if (e?.status === 401) return;
    }

    const allPorts = [...mihomoPorts, ...xrayPorts];
    const collisions = findPortCollisions(allPorts);
    if (collisions.length > 0) {
      const details = collisions
        .map((group) => {
          const portNum = group[0].port;
          const descriptions = group.map((p) => `${p.engine} (${p.purpose})`).join(' vs ');
          return `Port ${portNum}: ${descriptions}`;
        })
        .join('\n');

      if (
        !(await showConfirm({
          title: $t('editor.port_collision_title'),
          message: details,
          consequence: $t('editor.port_collision_warning'),
          variant: 'danger',
          confirmLabel: $t('app.continue')
        }))
      ) {
        applyLoading = false;
        return;
      }
    }

    try {
      const path = selectedFile || '/opt/etc/mihomo/config.yaml';

      // Save previous state to localStorage for Undo
      let currentYAML = '';
      const readRes = await apiFetch(`/api/config/read?path=${encodeURIComponent(path)}`);
      if (readRes.ok) {
        currentYAML = await readRes.text();
        localStorage.setItem('xcp_prev_mihomo_yaml', currentYAML);
        checkUndo();
      }

      const yamlContent = generateYAML();
      validationError = '';
      saveWarnings = [];
      const listenerWarnings = collectListenerPortWarnings();

      let mergeRes: { content: string; stats?: any; warnings?: PreflightWarning[] };
      try {
        mergeRes = await apiFetchJSON<{
          content: string;
          stats?: any;
          warnings?: PreflightWarning[];
        }>('/api/config/smart-merge', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({
            type: 'mihomo',
            existing_content: currentYAML,
            template_content: yamlContent,
            target_file: path,
            template_owns_nodes: true
          })
        });
      } catch (mergeErr: any) {
        if (mergeErr?.status === 401) return;
        console.error('Smart merge failed:', mergeErr);
        showToast('error', $t('editor.smart_merge_failed'));
        applyLoading = false;
        return;
      }

      if (!mergeRes || !mergeRes.content) {
        showToast('error', $t('editor.smart_merge_failed'));
        applyLoading = false;
        return;
      }

      const saveRes = await apiFetch(`/api/config/save?path=${encodeURIComponent(path)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: mergeRes.content
      });

      if (!saveRes.ok) {
        if (saveRes.status === 422) {
          const resData = await saveRes.json();
          validationError = resData.error || 'Unknown validation error';
          showToast('error', $t('editor.validation_failed'));
          applyLoading = false;
          return;
        }
        const errorText = await saveRes.text();
        throw new Error(errorText || 'Failed to save config');
      }

      const saveJson = await saveRes.json().catch(() => null);
      const saveData = saveJson?.data ?? saveJson;
      const saveResWarnings = Array.isArray(saveData?.warnings) ? saveData.warnings : [];
      const mergeWarnings = Array.isArray((mergeRes as any)?.warnings)
        ? (mergeRes as any).warnings
        : Array.isArray((mergeRes as any)?.data?.warnings)
          ? (mergeRes as any).data.warnings
          : [];
      const backendWarnings = saveResWarnings.length > 0 ? saveResWarnings : mergeWarnings;
      saveWarnings = [...listenerWarnings, ...backendWarnings];

      let restartUrl = '/api/service/control?action=restart';
      const activeKernel = $capabilities?.active_kernel;
      if (activeKernel && activeKernel !== 'mihomo') {
        if (
          await showConfirm({
            title: $t('editor.switch_kernel_title'),
            consequence: $t('editor.switch_kernel_confirm', { kernel: activeKernel }),
            variant: 'primary',
            confirmLabel: $t('editor.switch')
          })
        ) {
          restartUrl = '/api/service/control?action=switch_kernel&kernel=mihomo';
        }
      }

      activateRestartGrace(6000);
      const restartRes = await apiFetch(restartUrl, {
        method: 'POST'
      });

      if (!restartRes.ok) {
        throw new Error('Failed to restart service');
      }

      await fetchCapabilities();

      isDirty = false;
      const stats = mergeRes.stats || {};
      showToast(
        'success',
        $t('editor.smart_merge_applied', {
          nodes: $tp('editor.smart_merge_applied_nodes', stats.proxies ?? 0),
          providers: $tp('editor.smart_merge_applied_providers', stats.proxy_providers ?? 0),
          rules: $tp('editor.smart_merge_applied_rules', stats.rules ?? 0),
          userRules: $tp('editor.smart_merge_applied_user_rules', stats.user_rules ?? 0)
        })
      );
    } catch (err: any) {
      if (err?.status === 401) return;
      console.error(err);
      showToast('error', err.message || $t('mihomo.save_error'));
    } finally {
      applyLoading = false;
    }
  }

  async function handleUndo() {
    const prevYAML = localStorage.getItem('xcp_prev_mihomo_yaml');
    if (!prevYAML) return;
    try {
      applyLoading = true;
      const path = selectedFile || '/opt/etc/mihomo/config.yaml';

      // Save back to file
      const saveRes = await apiFetch(`/api/config/save?path=${encodeURIComponent(path)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
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
      let restartUrl = '/api/service/control?action=restart';
      const activeKernel = $capabilities?.active_kernel;
      if (activeKernel && activeKernel !== 'mihomo') {
        if (
          await showConfirm({
            title: $t('editor.switch_kernel_title'),
            consequence: $t('editor.switch_kernel_confirm', { kernel: activeKernel }),
            variant: 'primary',
            confirmLabel: $t('editor.switch')
          })
        ) {
          restartUrl = '/api/service/control?action=switch_kernel&kernel=mihomo';
        }
      }

      const restartRes = await apiFetch(restartUrl, {
        method: 'POST'
      });
      if (!restartRes.ok) {
        throw new Error('Failed to restart service');
      }

      await fetchCapabilities();

      showToast('success', $t('editor.undo_success'));
      checkUndo();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', `Undo failed: ${e.message}`);
    } finally {
      applyLoading = false;
    }
  }
</script>

<div class="container">
  {#if detectedDraft}
    <DraftRestoreBanner
      timestamp={detectedDraft.timestamp}
      onRestore={handleRestoreDraft}
      onDiscard={handleDiscardDraft}
    />
  {/if}

  {#if schemaLoading}
    <div
      class="loading-state-block"
      style="padding: 48px; text-align: center; color: var(--fg-secondary);"
    >
      <div class="spinner" style="--spinner-size: 24px; margin: 0 auto 12px;"></div>
      <p>{$t('editor.loading_definition')}</p>
    </div>
  {:else if schemaError}
    <div class="error-state-block" style="padding: 48px; text-align: center;">
      <div class="error-icon" style="color: var(--danger); font-size: 24px; margin-bottom: 12px;">
        ⚠
      </div>
      <p style="color: var(--danger); margin-bottom: 16px;">
        {$t('editor.definition_load_error', { error: schemaError })}
      </p>
      <button class="btn btn-secondary" onclick={loadSchema}>{$t('app.retry')}</button>
    </div>
  {:else}
    {#if !embedded}
      <div class="page-head">
        <div>
          <div class="crumbs">
            {$t('nav.group_system')} <span class="crumb-sep">›</span>
            {$t('editor.title')} <span class="crumb-sep">›</span>
            {$t('mihomo.breadcrumb_generator')}
          </div>
          <h1>{$t('mihomo.h1')}</h1>
          <p class="sub">
            {$t('mihomo.h1_sub')}
          </p>
        </div>
        <div class="ph-actions">
          <button
            type="button"
            class="btn btn-secondary"
            onclick={togglePreviewPane}
            title={showPreviewPane ? $t('mihomo.hide_preview') : $t('mihomo.show_preview')}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
              <line x1="15" y1="3" x2="15" y2="21" />
            </svg>
            <span>{showPreviewPane ? $t('mihomo.hide_preview') : $t('mihomo.show_preview')}</span>
          </button>
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
              {$t('mihomo.insert_editor')}
            {:else}
              {$t('mihomo.open_editor')}
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
            {$t('mihomo.copy_yaml')}
          </button>
        </div>
      </div>
    {:else}
      <div class="embedded-head-toolbar">
        <div class="embedded-title-tag">
          <span style="color: var(--fg-secondary);">{$t('editor.title')} › </span>
          <strong>{$t('mihomo.breadcrumb_generator')}</strong>
        </div>
        <div class="ph-actions">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            onclick={togglePreviewPane}
            title={showPreviewPane ? $t('mihomo.hide_preview') : $t('mihomo.show_preview')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
              <line x1="15" y1="3" x2="15" y2="21" />
            </svg>
            <span>{showPreviewPane ? $t('mihomo.hide_preview') : $t('mihomo.show_preview')}</span>
          </button>
        </div>
      </div>
    {/if}

    {#if preservedKeys.length > 0}
      <div class="card safe-merge-card alert-warning" data-testid="safe-merge-card">
        <div class="safe-merge-head">
          <div class="safe-merge-title-group">
            <div class="safe-merge-icon-wrap">
              <svg
                width="15"
                height="15"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
              </svg>
            </div>
            <div>
              <div class="safe-merge-title">{$t('mihomo.safe_merge_title')}</div>
              <div class="safe-merge-desc">
                {$t('mihomo.safe_merge_desc')}
                <span class="sr-only">({preservedKeys.join(', ')})</span>
              </div>
            </div>
          </div>
          <label class="switch safe-merge-switch" title={$t('mihomo.safe_merge_toggle')}>
            <input type="checkbox" bind:checked={safeMergeEnabled} />
            <span class="slider round"></span>
          </label>
        </div>

        {#if safeMergeEnabled}
          <div class="safe-merge-tags">
            {#each safeMergeExpanded ? preservedKeys : preservedKeys.slice(0, 6) as key}
              <span class="directive-tag"><code>{key}</code></span>
            {/each}
            {#if preservedKeys.length > 6}
              <button
                type="button"
                class="btn-tag-expand"
                onclick={() => (safeMergeExpanded = !safeMergeExpanded)}
              >
                {safeMergeExpanded
                  ? $t('mihomo.safe_merge_tags_less')
                  : $t('mihomo.safe_merge_tags_more', { count: preservedKeys.length - 6 })}
              </button>
            {/if}
          </div>
        {/if}
      </div>
    {/if}

    <div class="gen-layout" class:resizing={isResizingPreview}>
      <!-- Left: sections -->
      <div class="gen-left">
        <!-- Scenario selection -->
        <div class="constructor-scenario-bar">
          <div class="scenario-select-wrap">
            <label for="preset-select" class="form-label"
              >{$t('editor.constructor_scenario')}:</label
            >
            <select
              id="preset-select"
              class="form-select preset-select"
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
          {#if isPresetModified}
            <span class="preset-modified-chip">{$t('xray.preset_modified')}</span>
          {/if}
        </div>

        <PreflightWarnings
          warnings={saveWarnings}
          onDismiss={() => {
            saveWarnings = [];
          }}
        />

        {#if activePreset === 'zkeen-selective' && !hasZkeenGeodata && !dismissZkeenGeodataWarning}
          <div
            class="alert alert-warning alert-dismissible"
            style="margin-bottom: 16px; padding: 8px 12px; font-size: 13px; display: flex; align-items: center; gap: 8px; border-radius: var(--radius-sm);"
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              style="flex-shrink: 0;"
              ><path
                d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
              /><line x1="12" y1="9" x2="12" y2="13" /><line
                x1="12"
                y1="17"
                x2="12.01"
                y2="17"
              /></svg
            >
            <span>{$t('editor.requires_zkeen_geodata')}</span>
            <button
              type="button"
              class="alert-close-btn"
              style="top: 50%; transform: translateY(-50%);"
              onclick={() => {
                dismissZkeenGeodataWarning = true;
                localStorage.setItem('xcp:dismissed_warning:zkeen_geodata', activePreset);
              }}
              aria-label={$t('app.close')}>&times;</button
            >
          </div>
        {/if}

        <!-- Rule providers -->
        <div class="rule-providers-row">
          <label class="form-label" for="rp-select">{$t('mihomo.rule_provider_label')}</label>
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
                showListenerForm = false;
              }}
            >
              {label}
              {#if id === 'proxies' && proxies.length > 0}
                <span class="sec-count">{proxies.length}</span>
              {/if}
              {#if id === 'groups' && groups.length > 0}
                <span class="sec-count">{groups.length}</span>
              {/if}
              {#if id === 'rulesets' && selectedMetaRuleSets.size > 0}
                <span class="sec-count">{selectedMetaRuleSets.size}</span>
              {/if}
              {#if id === 'rules' && rules.length > 0}
                <span class="sec-count">{rules.length}</span>
              {/if}
              {#if id === 'dns'}
                <span
                  class="tab-status-badge"
                  class:status-on={dns.enabled}
                  class:status-off={!dns.enabled}
                >
                  {dns.enabled ? $t('mihomo.tab_status_on') : $t('mihomo.tab_status_off')}
                </span>
              {/if}
              {#if id === 'tun'}
                <span
                  class="tab-status-badge"
                  class:status-on={tun.enabled}
                  class:status-off={!tun.enabled}
                >
                  {tun.enabled ? $t('mihomo.tab_status_on') : $t('mihomo.tab_status_off')}
                </span>
              {/if}
              {#if id === 'listeners' && listeners.length > 0}
                <span class="sec-count">{listeners.length}</span>
              {/if}
            </button>
          {/each}
        </div>

        <!-- PROXIES -->
        {#if activeSection === 'proxies'}
          <div class="sec-body">
            {#each proxies as p (p.id)}
              <div class="item-row" class:item-disabled={p.enabled === false}>
                <span class="item-badge type-{p.type}">{p.type}</span>
                <span class="item-name">{p.name}</span>
                <span class="item-meta">{p.server}:{p.port}</span>

                <label
                  class="switch item-switch"
                  title={p.enabled === false
                    ? $t('mihomo.proxy_disabled')
                    : $t('mihomo.proxy_enabled')}
                >
                  <input
                    type="checkbox"
                    checked={p.enabled !== false}
                    onchange={() => toggleProxy(p.id)}
                  />
                  <span class="slider round"></span>
                </label>

                <div class="item-actions">
                  <button
                    type="button"
                    class="item-btn"
                    onclick={() => editProxy(p)}
                    title={$t('app.edit')}
                  >
                    ✎
                  </button>
                  <button
                    type="button"
                    class="item-btn"
                    onclick={() => duplicateProxy(p)}
                    title={$t('app.duplicate')}
                  >
                    ⎘
                  </button>
                  <button
                    type="button"
                    class="item-btn item-btn-danger"
                    onclick={() => removeProxy(p.id)}
                    title={$t('app.delete')}
                  >
                    ✕
                  </button>
                </div>
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
                <div
                  class="sec-subtitle"
                  style="margin-top: 16px; margin-bottom: 8px; border-top: 1px solid var(--border); padding-top: 12px; font-weight: 600; font-size: 13px; color: var(--fg-secondary);"
                >
                  {$t('mihomo.proxy_providers_hint')}
                </div>
                {#each mihomoProviders as sub}
                  <div class="item-row" style="border-left: 3px solid var(--success);">
                    <span
                      class="item-badge type-mihomo"
                      style="background: rgba(16, 185, 129, 0.15); color: var(--success); border-color: rgba(16, 185, 129, 0.3);"
                      >mihomo</span
                    >
                    <span class="item-name">{sub.name}</span>
                    <span
                      class="item-meta"
                      title={sub.url}
                      style="max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;"
                    >
                      {sub.url}
                    </span>
                    <button
                      type="button"
                      class="item-btn"
                      onclick={loadSubscriptions}
                      title={$t('mihomo.refresh_provider')}
                    >
                      ⟳
                    </button>
                  </div>
                {/each}
              {/if}

              <div class="constructor-proxy-list">
                <button
                  type="button"
                  class="add-btn btn-action-primary"
                  onclick={() => (showProxyForm = true)}
                >
                  + {$t('mihomo.add_proxy')}
                </button>
                <button
                  type="button"
                  class="add-btn import-btn"
                  onclick={loadSubscriptionProxies}
                  disabled={!hasXraySubscriptions}
                  title={hasXraySubscriptions
                    ? $t('mihomo.import_xray_desc')
                    : $t('mihomo.no_xray_subs')}
                >
                  ↓ {$t('editor.constructor_import_proxies')}
                </button>
                <button type="button" class="add-btn import-btn" onclick={openImportModal}>
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
                              showToast(
                                'warning',
                                $t('editor.group_disable_warning', { group: g.name })
                              );
                            }
                            groups = [...groups];
                          }}
                        />
                        <span class="slider round"></span>
                      </label>
                    </div>

                    {#if g.enabled !== false}
                      <div class="zkeen-group-body">
                        <label
                          for="mihomo-group-default-outbound-{g.name}"
                          class="form-label"
                          style="font-size: 11px; margin-bottom: 2px;"
                          >{$t('mihomo.default_outbound')}</label
                        >
                        <select
                          id="mihomo-group-default-outbound-{g.name}"
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
                      style="background: color-mix(in srgb, var(--success) 20%, transparent); color: var(--success); font-size: 10px; text-transform: none;"
                      title={g.useProviders.join(', ')}>use: {g.useProviders.length}</span
                    >
                  {/if}
                  {#if g.type === 'load-balance' && g.strategy}
                    <span
                      class="item-badge"
                      style="background: color-mix(in srgb, var(--warning) 20%, transparent); color: var(--warning); font-size: 10px; text-transform: none;"
                      >{g.strategy}</span
                    >
                  {/if}
                  <span class="item-meta">{g.proxies.length} {$t('mihomo.proxies_count')}</span>
                  <button class="item-edit" onclick={() => editGroup(g)} title={$t('app.edit')}
                    >✎</button
                  >
                  <button
                    class="item-del"
                    onclick={() => removeGroup(g.id)}
                    title={$t('app.delete')}>✕</button
                  >
                </div>
              {/each}

              {#if showGroupForm}
                <GroupForm
                  bind:ng
                  isEdit={!!editingGroupId}
                  {allProxyNames}
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
                      strategy: undefined
                    };
                  }}
                />
              {:else}
                <button class="add-btn" onclick={() => (showGroupForm = true)}>
                  + {$t('mihomo.add_group')}
                </button>
              {/if}
            {/if}
          </div>
        {/if}

        <!-- RULE SETS (MetaCubeX) -->
        {#if activeSection === 'rulesets'}
          <div class="sec-body">
            <div
              class="rulesets-hint"
              style="font-size:12px; color:var(--fg-dim); margin-bottom:12px;"
            >
              {$t('mihomo.rule_sets_hint')}
            </div>

            <div
              class="rulesets-container rulesets-picker"
              data-testid="rulesets-picker"
              style="display:flex; flex-direction:column; gap:16px;"
            >
              {#each Object.entries(META_RULE_SETS_BY_CATEGORY) as [catName, items]}
                <div
                  class="ruleset-cat-card"
                  style="background:var(--bg-elevated); border:1px solid var(--border); border-radius:var(--radius); padding:12px;"
                >
                  <div
                    class="ruleset-cat-title"
                    style="font-size:13px; font-weight:600; color:var(--fg-primary); margin-bottom:8px; display:flex; align-items:center; gap:6px;"
                  >
                    <span>{catName}</span>
                    <span style="font-size:11px; font-weight:normal; color:var(--fg-dim);"
                      >({items.length})</span
                    >
                  </div>
                  <div
                    class="ruleset-items-grid"
                    style="display:grid; grid-template-columns:repeat(auto-fill, minmax(280px, 1fr)); gap:8px;"
                  >
                    {#each items as item}
                      {@const key = `${item.id}|${item.type}`}
                      {@const isChecked = selectedMetaRuleSets.has(key)}
                      <div
                        class="ruleset-item-row"
                        style="display:flex; align-items:center; justify-content:space-between; padding:6px 10px; background:var(--bg-card); border:1px solid var(--border-subtle); border-radius:var(--radius-sm);"
                      >
                        <label
                          class="checkbox-label"
                          style="display:flex; align-items:center; gap:8px; cursor:pointer; user-select:none; margin:0;"
                        >
                          <input
                            type="checkbox"
                            value={key}
                            id="ruleset-{item.type}-{item.id}"
                            checked={isChecked}
                            onchange={(e) => {
                              if (e.currentTarget.checked) {
                                selectedMetaRuleSets.set(key, item.defaultOutbound || 'DIRECT');
                              } else {
                                selectedMetaRuleSets.delete(key);
                              }
                              selectedMetaRuleSets = new Map(selectedMetaRuleSets);
                            }}
                          />
                          <span
                            class="ruleset-item-label"
                            style="font-size:13px; font-weight:500; color:var(--fg-primary);"
                            >{item.label}</span
                          >
                          <span
                            class="ruleset-type-badge"
                            style="font-size:9px; background:var(--bg-surface); padding:2px 4px; border-radius:4px; opacity:0.7;"
                            >{item.type}</span
                          >
                        </label>
                        {#if isChecked}
                          <select
                            class="form-select"
                            style="font-size:11px; padding:2px 4px; height:24px; width:80px;"
                            value={selectedMetaRuleSets.get(key)}
                            onchange={(e) => {
                              selectedMetaRuleSets.set(key, e.currentTarget.value);
                              selectedMetaRuleSets = new Map(selectedMetaRuleSets);
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
                <button class="item-del" onclick={() => removeRule(r.id)} title={$t('app.delete')}
                  >✕</button
                >
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
                + {$t('mihomo.add_rule')}
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
                <span>{$t('mihomo.enable_dns')}</span>
              </label>
            </div>
            {#if dns.enabled}
              {#if $capabilities?.xkeen_dns === false}
                <div
                  class="alert alert-warning"
                  style="margin: 0 0 16px 0; display: flex; flex-direction: column; gap: 8px; align-items: flex-start;"
                  role="status"
                >
                  <div style="display: flex; gap: 8px; align-items: center;">
                    <span aria-hidden="true">⚠️</span>
                    <span>{$t('editor.dns_intercept_warning')}</span>
                  </div>
                  <button
                    class="btn btn-secondary btn-sm"
                    style="font-size: 12px; padding: 4px 8px; display: flex; align-items: center; gap: 4px;"
                    onclick={enableDNSRedirect}
                    disabled={dnsRedirectLoading}
                  >
                    {#if dnsRedirectLoading}
                      <span
                        class="spinner"
                        style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
                      ></span>
                    {/if}
                    {$t('editor.dns_intercept_enable')}
                  </button>
                </div>
              {/if}
              <div class="form-row">
                <label class="form-label" for="mihomo-dns-enhanced-mode"
                  >{$t('mihomo.enhanced_mode')}</label
                >
                <select
                  id="mihomo-dns-enhanced-mode"
                  class="form-select"
                  bind:value={dns.enhancedMode}
                >
                  <option value="fake-ip">fake-ip</option>
                  <option value="redir-host">redir-host</option>
                </select>
              </div>
              {#if dns.enhancedMode === 'fake-ip'}
                <div class="form-row">
                  <label class="form-label" for="mihomo-dns-fakeip-range">Fake-IP Range</label>
                  <input
                    id="mihomo-dns-fakeip-range"
                    class="form-input"
                    bind:value={dns.fakeIPRange}
                  />
                </div>
              {/if}
              <div class="form-row">
                <label class="form-label" for="mihomo-dns-nameservers">Nameservers</label>
                <textarea
                  id="mihomo-dns-nameservers"
                  class="form-textarea"
                  value={dns.nameservers.join('\n')}
                  rows="3"
                  onchange={(e) =>
                    (dns.nameservers = e.currentTarget.value.split('\n').filter(Boolean))}
                ></textarea>
              </div>
              <div class="form-row">
                <label class="form-label" for="mihomo-dns-fallback">Fallback</label>
                <textarea
                  id="mihomo-dns-fallback"
                  class="form-textarea"
                  value={dns.fallback.join('\n')}
                  rows="3"
                  onchange={(e) =>
                    (dns.fallback = e.currentTarget.value.split('\n').filter(Boolean))}></textarea>
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
                <span>{$t('mihomo.enable_tun')}</span>
              </label>
            </div>
            {#if tun.enabled}
              <div class="form-row">
                <label class="form-label" for="mihomo-tun-stack">Stack</label>
                <select id="mihomo-tun-stack" class="form-select" bind:value={tun.stack}>
                  <option value="system">system</option>
                  <option value="gvisor">gvisor</option>
                  <option value="mixed">mixed</option>
                </select>
              </div>
              <div class="toggle-row">
                <label class="toggle-label">
                  <input type="checkbox" bind:checked={tun.autoRoute} />
                  <span>Auto route</span>
                </label>
              </div>
              <div class="toggle-row">
                <label class="toggle-label">
                  <input type="checkbox" bind:checked={tun.autoDetectInterface} />
                  <span>Auto detect interface</span>
                </label>
              </div>
              <div class="form-row">
                <label class="form-label" for="mihomo-tun-dns-hijack">DNS Hijack</label>
                <input
                  id="mihomo-tun-dns-hijack"
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
            <div
              class="toggle-row"
              style="margin-top: 16px; border-top: 1px solid var(--border); padding-top: 16px;"
            >
              <label class="toggle-label">
                <input type="checkbox" bind:checked={sniffer.enabled} />
                <span>{$t('editor.sniffer_enable')}</span>
              </label>
            </div>
            {#if sniffer.enabled}
              <div
                style="margin-left: 20px; display: flex; flex-direction: column; gap: 8px; margin-top: 8px;"
              >
                <label
                  class="checkbox-container"
                  style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;"
                >
                  <input
                    type="checkbox"
                    bind:checked={sniffer.sniffHttp}
                    style="width: auto; margin: 0;"
                  />
                  <span>Sniff HTTP (ports 80, 8080)</span>
                </label>
                <label
                  class="checkbox-container"
                  style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;"
                >
                  <input
                    type="checkbox"
                    bind:checked={sniffer.sniffTls}
                    style="width: auto; margin: 0;"
                  />
                  <span>Sniff TLS (ports 443, 8443)</span>
                </label>
                <label
                  class="checkbox-container"
                  style="display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; user-select: none;"
                >
                  <input
                    type="checkbox"
                    bind:checked={sniffer.sniffQuic}
                    style="width: auto; margin: 0;"
                  />
                  <span>Sniff QUIC (ports 443, 8443)</span>
                </label>
              </div>
            {/if}

            <div
              class="form-row"
              style="margin-top: 16px; border-top: 1px solid var(--border); padding-top: 16px;"
            >
              <label class="form-label" for="mihomo-ctrl-type">{$t('mihomo.controller_type')}</label
              >
              <select id="mihomo-ctrl-type" class="form-select" bind:value={externalControllerType}>
                <option value="unix">{$t('mihomo.controller_unix_label')}</option>
                <option value="tcp">{$t('mihomo.controller_tcp_label')}</option>
              </select>
            </div>
            {#if externalControllerType === 'tcp'}
              <div class="form-row">
                <label class="form-label" for="mihomo-ctrl-target"
                  >{$t('mihomo.controller_tcp_address')}</label
                >
                <input
                  id="mihomo-ctrl-target"
                  class="form-input"
                  bind:value={externalControllerTarget}
                  placeholder="127.0.0.1:9090"
                />
              </div>
              {#if externalControllerTarget.startsWith('0.0.0.0:') || externalControllerTarget.startsWith(':') || externalControllerTarget === '0.0.0.0'}
                <div
                  class="inline-warning"
                  style="margin-top: 6px; font-size: 12px; color: var(--color-warning, #f59e0b); display: flex; align-items: center; gap: 6px;"
                >
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
                    />
                    <line x1="12" y1="9" x2="12" y2="13" />
                    <line x1="12" y1="17" x2="12.01" y2="17" />
                  </svg>
                  <span>{$t('mihomo.insecure_lan_warning')}</span>
                </div>
              {/if}
            {/if}
          </div>
        {/if}

        <!-- LISTENERS -->
        {#if activeSection === 'listeners'}
          <div class="sec-body">
            {#if listenersReadOnly}
              <div class="alert alert-warning" role="status">
                <div style="font-weight: 600; margin-bottom: 4px;">
                  {$t('mihomo.listener_readonly_title')}
                </div>
                <div style="font-size: 13px; margin-bottom: 8px;">
                  {$t('mihomo.listener_readonly_body')}
                </div>
                <div class="safe-merge-tags">
                  <span class="directive-tag"><code>listeners</code></span>
                </div>
              </div>
            {:else if showListenerForm}
              <div class="form-card">
                <div class="form-row">
                  <label class="form-label" for="listener-name">{$t('mihomo.listener_name')}</label>
                  <input
                    id="listener-name"
                    class="form-input"
                    bind:value={newListener.name}
                    placeholder="my-listener"
                  />
                  {#if !listenerNameValid}
                    <span
                      class="form-validation-msg"
                      style="font-size: 11px; color: var(--warning); margin-top: 2px;"
                    >
                      {$t('mihomo.listener_name_required')}
                    </span>
                  {/if}
                </div>

                <div class="form-row2">
                  <div class="form-col">
                    <label class="form-label" for="listener-type"
                      >{$t('mihomo.listener_type')}</label
                    >
                    <select
                      id="listener-type"
                      class="form-select"
                      bind:value={newListener.type}
                      onchange={() => {
                        if (newListener.type === 'shadowsocks' && !newListener.cipher) {
                          newListener.cipher = 'aes-256-gcm';
                        }
                      }}
                    >
                      <option value="mixed">mixed</option>
                      <option value="socks">socks</option>
                      <option value="http">http</option>
                      <option value="shadowsocks">shadowsocks</option>
                      <option value="tproxy">tproxy</option>
                      <option value="redirect">redirect</option>
                    </select>
                  </div>

                  <div class="form-col">
                    <label class="form-label" for="listener-listen"
                      >{$t('mihomo.listener_listen')}</label
                    >
                    <input
                      id="listener-listen"
                      class="form-input"
                      bind:value={newListener.listen}
                      placeholder="0.0.0.0"
                    />
                  </div>

                  <div class="form-col form-col-sm">
                    <label class="form-label" for="listener-port"
                      >{$t('mihomo.listener_port')}</label
                    >
                    <input
                      id="listener-port"
                      type="number"
                      min="1"
                      max="65535"
                      class="form-input"
                      bind:value={newListener.port}
                      placeholder="7890"
                    />
                    {#if !listenerPortValid}
                      <span
                        class="form-validation-msg"
                        style="font-size: 11px; color: var(--warning); margin-top: 2px;"
                      >
                        {$t('mihomo.listener_port_required')}
                      </span>
                    {/if}
                  </div>
                </div>

                <div class="form-row">
                  <label class="form-label" for="listener-destination"
                    >{$t('mihomo.listener_destination')}</label
                  >
                  <select
                    id="listener-destination"
                    class="form-select"
                    value={newListener.proxy &&
                    (groups.some((g) => g.name === newListener.proxy) ||
                      proxies.some((p) => p.name === newListener.proxy))
                      ? newListener.proxy
                      : ''}
                    onchange={(e) => {
                      newListener.proxy = e.currentTarget.value || undefined;
                    }}
                  >
                    <option value="">{$t('mihomo.listener_dest_rules')}</option>
                    {#if groups.length > 0}
                      <optgroup label={$t('mihomo.listener_dest_groups')}>
                        {#each groups as g}
                          <option value={g.name}>{g.name}</option>
                        {/each}
                      </optgroup>
                    {/if}
                    {#if proxies.length > 0}
                      <optgroup label={$t('mihomo.listener_dest_nodes')}>
                        {#each proxies as p}
                          <option value={p.name}>{p.name}</option>
                        {/each}
                      </optgroup>
                    {/if}
                  </select>
                  <div
                    class="form-hint"
                    style="font-size: 12px; color: var(--fg-dim); margin-top: 4px;"
                  >
                    {$t('mihomo.listener_destination_hint')}
                  </div>
                </div>

                {#if newListener.type !== 'http' && newListener.type !== 'redirect'}
                  <div class="toggle-row" style="margin-top: 4px;">
                    <label class="toggle-label">
                      <input type="checkbox" bind:checked={newListener.udp} />
                      <span>{$t('mihomo.listener_udp')}</span>
                    </label>
                  </div>
                {/if}

                {#if newListener.type === 'shadowsocks'}
                  <div class="form-row">
                    <label class="form-label" for="listener-cipher"
                      >{$t('mihomo.listener_cipher')}</label
                    >
                    <select
                      id="listener-cipher"
                      class="form-select"
                      bind:value={newListener.cipher}
                    >
                      {#each CIPHERS as c}
                        <option value={c}>{c}</option>
                      {/each}
                    </select>
                  </div>
                  <div class="form-row">
                    <label class="form-label" for="listener-password"
                      >{$t('mihomo.listener_password')}</label
                    >
                    <input
                      id="listener-password"
                      type="password"
                      class="form-input"
                      bind:value={newListener.password}
                    />
                    {#if !listenerSSPasswordValid}
                      <span
                        class="form-validation-msg"
                        style="font-size: 11px; color: var(--warning); margin-top: 2px;"
                      >
                        {$t('mihomo.listener_ss_password_required')}
                      </span>
                    {/if}
                  </div>
                {/if}

                {#if newListener.type === 'mixed' || newListener.type === 'socks' || newListener.type === 'http'}
                  <div class="form-row" style="margin-top: 6px;">
                    <div
                      style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 4px;"
                    >
                      <span class="form-label" style="margin-bottom: 0;"
                        >{$t('mihomo.listener_users')}</span
                      >
                      <button
                        type="button"
                        class="btn btn-secondary btn-sm"
                        style="font-size: 11px; padding: 2px 8px;"
                        onclick={addListenerUser}
                      >
                        + {$t('mihomo.listener_add_user')}
                      </button>
                    </div>
                    {#if newListener.users && newListener.users.length > 0}
                      <div
                        style="display: flex; flex-direction: column; gap: 8px; margin-top: 4px;"
                      >
                        {#each newListener.users as user, uIdx}
                          <div style="display: flex; gap: 8px; align-items: center;">
                            <input
                              type="text"
                              class="form-input"
                              placeholder={$t('mihomo.listener_username')}
                              bind:value={user.username}
                            />
                            <input
                              type="password"
                              class="form-input"
                              placeholder={$t('mihomo.listener_password')}
                              bind:value={user.password}
                            />
                            <button
                              type="button"
                              class="item-del"
                              aria-label={$t('app.delete')}
                              title={$t('app.delete')}
                              onclick={() => removeListenerUser(uIdx)}
                            >
                              ✕
                            </button>
                          </div>
                        {/each}
                      </div>
                    {/if}
                    <div
                      class="form-hint"
                      style="font-size: 12px; color: var(--fg-dim); margin-top: 6px;"
                    >
                      {$t('mihomo.listener_open_proxy_hint')}
                    </div>
                  </div>
                {/if}

                <div
                  class="form-actions"
                  style="display: flex; gap: 8px; justify-content: flex-end; margin-top: 12px;"
                >
                  <button type="button" class="btn btn-secondary" onclick={cancelListenerForm}>
                    {$t('app.cancel')}
                  </button>
                  <button type="button" class="btn btn-primary" onclick={saveListener}>
                    {editingListenerId ? $t('app.save') : $t('app.add')}
                  </button>
                </div>
              </div>
            {:else if listeners.length === 0}
              <div
                class="rulesets-hint"
                style="font-size: 12px; color: var(--fg-dim); margin-bottom: 12px;"
              >
                {$t('mihomo.listeners_hint')}
              </div>
              <button type="button" class="add-btn" onclick={() => openListenerForm()}>
                + {$t('mihomo.add_listener')}
              </button>
            {:else}
              {#each listeners as l (l.id)}
                <div class="item-row">
                  <span class="item-badge type-{l.type}">{l.type}</span>
                  <span class="item-name">{l.name}</span>
                  <span class="item-meta">{l.listen}:{l.port}</span>
                  <span class="item-meta">
                    {l.proxy ? `→ ${l.proxy}` : $t('mihomo.listener_dest_rules')}
                  </span>
                  <button
                    type="button"
                    class="item-edit"
                    aria-label={$t('app.edit')}
                    title={$t('app.edit')}
                    onclick={() => openListenerForm(l)}
                  >
                    ✎
                  </button>
                  <button
                    type="button"
                    class="item-del"
                    aria-label={$t('app.delete')}
                    title={$t('app.delete')}
                    onclick={() => {
                      listeners = listeners.filter((item) => item.id !== l.id);
                    }}
                  >
                    ✕
                  </button>
                </div>
              {/each}
              <button type="button" class="add-btn" onclick={() => openListenerForm()}>
                + {$t('mihomo.add_listener')}
              </button>
            {/if}
          </div>
        {/if}
      </div>

      <!-- Splitter -->
      {#if showPreviewPane}
        <button
          type="button"
          class="mihomo-splitter"
          class:active={isResizingPreview}
          aria-label={$t('xray.resize_preview')}
          tabindex="-1"
          onpointerdown={startResizePreview}
          onmousedown={startResizePreview}
        >
          <div class="splitter-handle"></div>
        </button>

        <!-- Right: YAML preview -->
        <div class="gen-right card" style="width: {previewWidth}px; flex: 0 0 {previewWidth}px;">
          <div class="preview-header">
            <div class="preview-title-wrap">
              <span class="preview-title">YAML {$t('mihomo.preview')}</span>
              <span class="preview-size-badge">{yamlFileSize}</span>
            </div>
            <div class="preview-header-actions">
              {#if yaml}
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  onclick={copyYAML}
                  title={$t('mihomo.copy_yaml')}
                >
                  {#if copyFeedback}
                    <svg
                      width="12"
                      height="12"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="var(--color-success, #22c55e)"
                      stroke-width="2.5"
                    >
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  {:else}
                    <svg
                      width="12"
                      height="12"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <rect x="9" y="9" width="13" height="13" rx="2" /><path
                        d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
                      />
                    </svg>
                  {/if}
                  <span style="margin-left: 4px;"
                    >{copyFeedback ? $t('mihomo.yaml_copied') : $t('mihomo.copy_yaml')}</span
                  >
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  onclick={downloadYaml}
                  title={$t('mihomo.download_yaml_title')}
                >
                  <svg
                    width="12"
                    height="12"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                    <polyline points="7 10 12 15 17 10" />
                    <line x1="12" y1="15" x2="12" y2="3" />
                  </svg>
                  <span style="margin-left: 4px;">{$t('mihomo.download_yaml')}</span>
                </button>
              {/if}
            </div>
          </div>

          <pre class="yaml-preview" data-testid="mihomo-yaml-preview">{yaml ||
              $t('mihomo.empty_yaml_hint')}</pre>

          {#if validationError}
            <div
              class="validation-error-block"
              style="margin: 12px; padding: 12px; background: rgba(239, 91, 107, 0.1); border: 1px solid var(--danger); border-radius: var(--radius-md); color: var(--danger); font-size: 13px;"
            >
              <div style="font-weight: bold; margin-bottom: 6px;">
                {$t('editor.validation_failed')}
              </div>
              <div
                style="white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 13px; margin-bottom: 8px;"
              >
                {parseValidationError(validationError, $currentLang)}
              </div>
              <details>
                <summary style="cursor: pointer; font-size: 12px; opacity: 0.8; user-select: none;"
                  >{$t('editor.validation_details')}</summary
                >
                <pre
                  style="margin: 6px 0 0 0; white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 12px; opacity: 0.9; max-height: 200px; overflow-y: auto;">{validationError}</pre>
              </details>
            </div>
          {/if}

          <!-- Bottom Actions Toolbar -->
          <div class="gen-preview-footer">
            <button type="button" class="btn btn-secondary" onclick={openInEditor}>
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
                {$t('mihomo.insert_editor')}
              {:else}
                {$t('mihomo.open_editor')}
              {/if}
            </button>

            {#if canUndo}
              <button
                type="button"
                class="btn btn-secondary"
                onclick={handleUndo}
                disabled={applyLoading}
              >
                {$t('editor.undo')}
              </button>
            {/if}

            <button
              type="button"
              class="btn btn-primary"
              data-testid="apply-changes-btn"
              onclick={handleApplyMihomo}
              disabled={applyLoading || !yaml}
            >
              {applyLoading ? $t('mihomo.saving') : $t('mihomo.apply_and_restart')}
            </button>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>

<Modal
  isOpen={showApplyConfirm}
  title={$t('editor.apply_confirm_title')}
  dataTestid="apply-confirm-dialog"
  onclose={() => (showApplyConfirm = false)}
>
  <p>{$t('editor.apply_confirm_body')}</p>
  <div class="changed-files-list" style="margin-top: 12px;">
    <strong>{$t('mihomo.sections_to_update')}</strong>
    <div style="margin: 8px 0; font-family: monospace; font-size: 13px;">
      <code>{selectedFile || '/opt/etc/mihomo/config.yaml'}</code>
    </div>
    <ul style="margin: 8px 0 0 0; padding-left: 20px;">
      <li><code>proxy-groups</code></li>
      <li><code>rule-providers</code></li>
      <li><code>rules</code></li>
    </ul>
    <p style="margin-top: 12px; font-size: 0.8125rem; color: var(--fg-secondary);">
      {$t('mihomo.backup_notice')}
    </p>
  </div>
  <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
    <button class="btn btn-secondary" onclick={() => (showApplyConfirm = false)}>
      {$t('app.cancel')}
    </button>
    <button class="btn btn-primary" onclick={handleApplyMihomo} disabled={applyLoading}>
      {applyLoading ? $t('editor.saving') : $t('editor.apply_and_restart')}
    </button>
  </div>
</Modal>

<Modal isOpen={showImportModal} title={$t('subscr.import_modal_title')} onclose={closeImportModal}>
  <div style="display: flex; flex-direction: column; gap: 16px;">
    {#if importErrorMsg}
      <div class="error-msg" style="color: var(--danger); margin-bottom: 12px; font-size: 13px;">
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
                  aria-label={$t('app.remove')}>✕</button
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
                  aria-label={$t('app.remove')}>✕</button
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

    <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
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
          {$t('mihomo.import_count', { count: importNodes.length })}
        </button>
      {/if}
    </div>
  </div>
</Modal>

<style>
  .crumb-sep {
    color: var(--fg-faint);
    margin: 0 6px;
  }

  .embedded-head-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
    padding-bottom: 12px;
    border-bottom: 1px solid var(--border);
  }

  .embedded-title-tag {
    font-size: 14px;
  }

  .safe-merge-card {
    margin-bottom: 16px;
    padding: 12px 16px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .safe-merge-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .safe-merge-title-group {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .safe-merge-icon-wrap {
    width: 28px;
    height: 28px;
    border-radius: var(--radius-sm);
    background: rgba(41, 194, 240, 0.12);
    color: var(--primary);
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .safe-merge-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .safe-merge-desc {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 1px;
  }

  .safe-merge-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    align-items: center;
  }

  .directive-tag {
    font-size: 11px;
    font-family: var(--font-family-mono, monospace);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 2px 6px;
    color: var(--fg-secondary);
  }

  .btn-tag-expand {
    background: none;
    border: none;
    font-size: 11px;
    color: var(--primary);
    cursor: pointer;
    padding: 2px 6px;
    border-radius: 4px;
  }

  .btn-tag-expand:hover {
    text-decoration: underline;
  }

  .gen-layout {
    display: flex;
    gap: 0;
    align-items: stretch;
    position: relative;
    min-height: 520px;
  }

  .gen-left {
    flex: 1;
    min-width: 320px;
    overflow-y: auto;
    padding-right: 12px;
  }

  .mihomo-splitter {
    width: 12px;
    margin: 0 4px;
    cursor: col-resize;
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    user-select: none;
    z-index: 10;
    background: transparent;
    border: none;
    padding: 0;
    outline: none;
  }

  .mihomo-splitter:hover .splitter-handle,
  .mihomo-splitter.active .splitter-handle {
    background: var(--color-primary, #0284c7);
    box-shadow: 0 0 8px rgba(2, 132, 199, 0.4);
  }

  .splitter-handle {
    width: 4px;
    height: 36px;
    border-radius: 2px;
    background: var(--color-border, #334155);
    transition: all 0.15s ease;
  }

  .gen-layout.resizing {
    user-select: none;
    cursor: col-resize;
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

  .tab-status-badge {
    font-size: 9px;
    font-weight: 700;
    border-radius: 6px;
    padding: 1px 5px;
    line-height: 1.2;
  }

  .tab-status-badge.status-on {
    background: rgba(70, 209, 138, 0.2);
    color: var(--success);
  }

  .tab-status-badge.status-off {
    background: rgba(255, 255, 255, 0.05);
    color: var(--fg-dim);
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
    transition: opacity var(--transition-fast);
  }

  .item-row.item-disabled {
    opacity: 0.55;
    filter: grayscale(0.4);
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

  .item-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-left: auto;
  }

  .item-btn {
    background: none;
    border: none;
    color: var(--fg-faint);
    cursor: pointer;
    font-size: 12px;
    padding: 4px 6px;
    border-radius: var(--radius-sm);
    transition:
      color var(--transition-fast),
      background var(--transition-fast);
    line-height: 1;
  }

  .item-btn:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.05);
  }

  .item-btn-danger:hover {
    color: var(--danger);
    background: rgba(239, 91, 107, 0.1);
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

  .item-switch {
    margin-left: 8px;
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
    background: var(--bg-surface, #1e293b);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--fg-secondary);
    font-size: 13px;
    padding: 10px 14px;
    cursor: pointer;
    transition:
      background var(--transition-fast),
      color var(--transition-fast),
      border-color var(--transition-fast);
    text-align: center;
  }

  .add-btn:hover {
    background: var(--bg-card-hover, #334155);
    border-color: var(--border-focus, var(--primary));
    color: var(--fg-primary);
  }

  .btn-action-primary {
    border-color: var(--border);
    color: var(--fg-primary);
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
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-width: 280px;
    max-width: 800px;
    height: calc(100vh - 160px);
    position: sticky;
    top: 16px;
  }

  .preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .preview-title-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .preview-title {
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .preview-size-badge {
    font-size: 10px;
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-secondary);
    padding: 1px 6px;
    border-radius: 10px;
  }

  .preview-header-actions {
    display: flex;
    align-items: center;
    gap: 6px;
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

  .gen-preview-footer {
    display: flex;
    gap: 8px;
    padding: 10px 12px;
    background: var(--bg-surface);
    border-top: 1px solid var(--border);
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .gen-preview-footer .btn-primary {
    flex: 1;
    min-width: 140px;
  }

  @media (max-width: 900px) {
    .gen-layout {
      flex-direction: column;
    }
    .gen-right {
      position: static;
      max-height: 350px;
      width: 100% !important;
      flex: 1 1 auto !important;
    }
    .mihomo-splitter {
      display: none;
    }
  }

  /* Scenario bar */
  .constructor-scenario-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }

  .scenario-select-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .preset-modified-chip {
    font-size: 11px;
    font-weight: 600;
    color: var(--warning);
    background: rgba(245, 158, 11, 0.12);
    border: 1px solid rgba(245, 158, 11, 0.3);
    padding: 2px 8px;
    border-radius: 12px;
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
    gap: 8px;
    flex-wrap: wrap;
    align-items: center;
  }

  .constructor-proxy-list .add-btn {
    width: auto;
    flex: 1 1 auto;
    min-width: 140px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 8px 12px;
    font-size: 12px;
    font-weight: 500;
  }

  .constructor-proxy-list .btn-action-primary {
    background: var(--bg-surface, #1e293b);
    border: 1px solid var(--border);
    color: var(--fg-primary);
  }

  .constructor-proxy-list .btn-action-primary:hover {
    background: var(--bg-card-hover, #334155);
    border-color: var(--primary);
  }

  .constructor-proxy-list .import-btn {
    background: var(--bg-surface, #1e293b);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
  }

  .constructor-proxy-list .import-btn:hover:not(:disabled) {
    background: var(--bg-card-hover, #334155);
    border-color: var(--primary);
    color: var(--fg-primary);
  }

  .constructor-proxy-list .import-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
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
</style>
