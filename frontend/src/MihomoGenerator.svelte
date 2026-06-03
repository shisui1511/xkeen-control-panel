<script lang="ts">
  import { currentLang, t } from './i18n';
  import { showToast } from './stores';

  export let onSwitchTab: (tab: string) => void = () => {};
  export let selectedFile: string = '';
  export let onInsertIntoEditor: (content: string) => void = () => {};
  export let embedded: boolean = false;
  export let initialPreset: string = '';

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
    // tuic
    congestion?: string;
    // ss
    cipher?: string;
    // vmess ws
    network?: string;
    wsPath?: string;
    tls?: boolean;
    fingerprint?: string;
  }

  interface ProxyGroup {
    id: string;
    name: string;
    type: GroupType;
    proxies: string[];
    includeAll?: boolean;
    url?: string;
    interval?: number;
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

  const RULE_PROVIDERS: Record<string, Array<{ name: string; url: string; behavior: string; outbound: string; format?: string }>> = {
    zkeen: [
      {
        name: 'zkeen-geosite-category-ads',
        url: `${META_BASE_URL}/geosite/category-ads-all.mrs`,
        behavior: 'domain',
        format: 'mrs',
        outbound: 'REJECT'
      },
      {
        name: 'zkeen-geosite-ru',
        url: `${META_BASE_URL}/geosite/ru.mrs`,
        behavior: 'domain',
        format: 'mrs',
        outbound: 'DIRECT'
      },
      {
        name: 'zkeen-geoip-ru',
        url: `${META_BASE_URL}/geoip/ru.mrs`,
        behavior: 'ipcidr',
        format: 'mrs',
        outbound: 'DIRECT'
      }
    ]
  };

  const META_RULE_SETS_BY_CATEGORY: Record<string, Array<{ id: string; label: string; type: 'geosite' | 'geoip'; defaultOutbound: string }>> = {
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
    'Сервисы': [
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
    'Блокировки': [
      { id: 'category-ads-all', label: 'Ads & Trackers', type: 'geosite', defaultOutbound: 'REJECT' },
      { id: 'category-ai-!cn', label: 'AI Services (non-CN)', type: 'geosite', defaultOutbound: 'Proxy' },
      { id: 'category-anticensorship', label: 'Anti-Censorship', type: 'geosite', defaultOutbound: 'Proxy' }
    ]
  };

  let selectedMetaRuleSets: Map<string, string> = new Map();

  function buildMetaRuleSetUrl(id: string, type: 'geosite' | 'geoip'): string {
    return `${META_BASE_URL}/${type}/${id}.mrs`;
  }

  // ── Presets ──────────────────────────────────────────────────────────────
  function applyPreset(id: 'rule-based' | 'global-proxy') {
    activePreset = id;
    if (id === 'rule-based') {
      groups = [
        { id: crypto.randomUUID(), name: 'Selective', type: 'select', proxies: ['DIRECT', ...proxies.map(p => p.name)], includeAll: true, url: 'https://www.gstatic.com/generate_204', interval: 300 }
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
        { id: crypto.randomUUID(), name: 'Proxy', type: 'select', proxies: ['DIRECT', ...proxies.map(p => p.name)], includeAll: true, url: 'https://www.gstatic.com/generate_204', interval: 300 }
      ];
      rules = [
        { id: crypto.randomUUID(), type: 'GEOIP', value: 'private', outbound: 'DIRECT' },
        { id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'Proxy' }
      ];
      activeRuleProvider = 'none';
      selectedMetaRuleSets = new Map();
    }
    showToast('success', $t('editor.preset_applied'));
  }

  // ── Import proxies from subscriptions ───────────────────────────────────
  async function loadSubscriptionProxies() {
    try {
      const res = await fetch('/api/subscriptions');
      if (!res.ok) return;
      const subs: any[] = await res.json();
      subscriptions = subs.filter(s => s.enabled);
      if (!subs || subs.length === 0) {
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
            type: ((n.protocol || 'vless') as ProxyType),
            server,
            port
          };
        });
        proxies = [...proxies, ...mapped];
        imported += mapped.length;
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

  function addProxy() {
    if (!np.name.trim() || !np.server.trim()) return;
    proxies = [...proxies, { ...np, id: crypto.randomUUID() }];
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

  function q(v: string | number | boolean) {
    if (typeof v !== 'string') return String(v);
    const escaped = v.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
    return v.includes(':') || v.includes('#') || v === '' ? `"${escaped}"` : escaped;
  }

  function generateYAML(): string {
    const lines: string[] = [];

    // Proxy-providers (if we have subscriptions)
    if (subscriptions.length > 0) {
      lines.push('proxy-providers:');
      for (const sub of subscriptions) {
        const providerName = sub.name.replace(/[^a-zA-Z0-9-]/g, '-').toLowerCase();
        lines.push(`  ${providerName}:`);
        lines.push(`    type: http`);
        lines.push(`    path: ./providers/${providerName}.yaml`);
        lines.push(`    url: ${q(sub.url)}`);
        lines.push(`    interval: ${sub.interval * 3600 || 86400}`);
        lines.push(`    health-check:`);
        lines.push(`      enable: true`);
        lines.push(`      url: http://www.gstatic.com/generate_204`);
        lines.push(`      interval: 300`);
      }
      lines.push('');
    }

    // Rule-providers (if selected)
    if (activeRuleProvider === 'metacubex' && selectedMetaRuleSets.size > 0) {
      lines.push('rule-providers:');
      for (const [key, outbound] of selectedMetaRuleSets) {
        const [id, type] = key.split('|') as [string, 'geosite' | 'geoip'];
        const behavior = type === 'geoip' ? 'ipcidr' : 'domain';
        lines.push(`  ${type}-${id.replace(/[^a-z0-9-]/g, '-')}:`);
        lines.push(`    type: http`);
        lines.push(`    format: mrs`);
        lines.push(`    behavior: ${behavior}`);
        lines.push(`    url: "${buildMetaRuleSetUrl(id, type)}"`);
        lines.push(`    interval: 86400`);
      }
      lines.push('');
    } else if (activeRuleProvider !== 'none' && activeRuleProvider !== 'metacubex') {
      const providers = RULE_PROVIDERS[activeRuleProvider];
      if (providers && providers.length > 0) {
        lines.push('rule-providers:');
        for (const rp of providers) {
          lines.push(`  ${rp.name}:`);
          lines.push(`    type: http`);
          if (rp.format) {
            lines.push(`    format: ${rp.format}`);
          }
          lines.push(`    behavior: ${rp.behavior}`);
          lines.push(`    url: "${rp.url}"`);
          lines.push(`    interval: 86400`);
        }
        lines.push('');
      }
    }

    // Proxies
    if (proxies.length > 0) {
      lines.push('proxies:');
      for (const p of proxies) {
        lines.push(`  - name: ${q(p.name)}`);
        lines.push(`    type: ${p.type}`);
        lines.push(`    server: ${q(p.server)}`);
        lines.push(`    port: ${p.port}`);

        if (p.type === 'vless') {
          lines.push(`    uuid: ${p.uuid}`);
          if (p.flow) lines.push(`    flow: ${p.flow}`);
          lines.push(`    tls: true`);
          lines.push(`    reality-opts:`);
          lines.push(`      public-key: ${q(p.publicKey || '')}`);
          lines.push(`      short-id: ${q(p.shortId || '')}`);
          lines.push(`    client-fingerprint: ${p.fingerprint || 'chrome'}`);
          if (p.servername) lines.push(`    servername: ${q(p.servername)}`);
        } else if (p.type === 'hysteria2') {
          lines.push(`    password: ${q(p.password || '')}`);
          if (p.sni) lines.push(`    sni: ${q(p.sni)}`);
        } else if (p.type === 'tuic') {
          lines.push(`    uuid: ${p.uuid}`);
          lines.push(`    password: ${q(p.password || '')}`);
          lines.push(`    congestion-controller: ${p.congestion || 'bbr'}`);
          if (p.sni) lines.push(`    sni: ${q(p.sni)}`);
        } else if (p.type === 'ss') {
          lines.push(`    cipher: ${p.cipher || 'aes-256-gcm'}`);
          lines.push(`    password: ${q(p.password || '')}`);
        } else if (p.type === 'vmess') {
          lines.push(`    uuid: ${p.uuid}`);
          lines.push(`    alterId: 0`);
          lines.push(`    cipher: auto`);
          lines.push(`    tls: ${p.tls}`);
          lines.push(`    network: ${p.network || 'ws'}`);
          if (p.network === 'ws') {
            lines.push(`    ws-opts:`);
            lines.push(`      path: ${q(p.wsPath || '/')}`);
          }
          if (p.tls && p.sni) lines.push(`    servername: ${q(p.sni)}`);
        }
      }
      lines.push('');
    }

    // Proxy groups
    if (groups.length > 0) {
      lines.push('proxy-groups:');
      for (const g of groups) {
        lines.push(`  - name: ${q(g.name)}`);
        lines.push(`    type: ${g.type}`);
        if (g.includeAll || subscriptions.length > 0) {
          lines.push(`    include-all: true`);
        }
        if (g.proxies.length > 0) {
          lines.push(`    proxies:`);
          for (const p of g.proxies) lines.push(`      - ${q(p)}`);
        }
        if (g.type !== 'select') {
          lines.push(`    url: ${g.url || 'https://www.gstatic.com/generate_204'}`);
          lines.push(`    interval: ${g.interval || 300}`);
        }
      }
      lines.push('');
    }

    // Rules
    if (rules.length > 0 || activeRuleProvider !== 'none' || (activeRuleProvider === 'metacubex' && selectedMetaRuleSets.size > 0)) {
      lines.push('rules:');
      // Rule-set entries from rule-providers (before user rules, before MATCH)
      if (activeRuleProvider === 'metacubex') {
        for (const [key, outbound] of selectedMetaRuleSets) {
          const [id, type] = key.split('|') as [string, 'geosite' | 'geoip'];
          lines.push(`  - RULE-SET,${type}-${id.replace(/[^a-z0-9-]/g, '-')},${outbound}`);
        }
      } else if (activeRuleProvider !== 'none') {
        const providers = RULE_PROVIDERS[activeRuleProvider];
        if (providers) {
          for (const rp of providers) {
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
      if (rules.length === 0 && (activeRuleProvider !== 'none' || (activeRuleProvider === 'metacubex' && selectedMetaRuleSets.size > 0))) {
        lines.push(`  - MATCH,DIRECT`);
      }
      lines.push('');
    }

    // DNS
    if (dns.enabled) {
      lines.push('dns:');
      lines.push(`  enable: true`);
      lines.push(`  enhanced-mode: ${dns.enhancedMode}`);
      if (dns.enhancedMode === 'fake-ip') lines.push(`  fake-ip-range: ${dns.fakeIPRange}`);
      lines.push(`  nameserver:`);
      for (const ns of dns.nameservers) lines.push(`    - ${q(ns)}`);
      if (dns.fallback.length > 0) {
        lines.push(`  fallback:`);
        for (const fb of dns.fallback) lines.push(`    - ${q(fb)}`);
      }
      lines.push('');
    }

    // TUN
    if (tun.enabled) {
      lines.push('tun:');
      lines.push(`  enable: true`);
      lines.push(`  stack: ${tun.stack}`);
      lines.push(`  auto-route: ${tun.autoRoute}`);
      lines.push(`  auto-detect-interface: ${tun.autoDetectInterface}`);
      if (tun.dnsHijack.length > 0) {
        lines.push(`  dns-hijack:`);
        for (const d of tun.dnsHijack) lines.push(`    - ${q(d)}`);
      }
      lines.push('');
    }

    return lines.join('\n').trimEnd();
  }

  let yaml = '';
  $: {
    // Explicit deps so Svelte 5 legacy mode tracks them across the function call
    void proxies; void groups; void rules; void activeRuleProvider; void selectedMetaRuleSets; void subscriptions;
    void dns.enabled; void dns.nameservers; void dns.fallback;
    void tun.enabled; void tun.stack;
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

  $: if (activeRuleProvider === 'metacubex' && activeSection !== 'rulesets' && activeSection !== 'proxies' && activeSection !== 'groups' && activeSection !== 'rules' && activeSection !== 'dns' && activeSection !== 'tun') {
    activeSection = 'rulesets';
  }
</script>

<div class="container">
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

  <div class="gen-layout">
    <!-- Left: sections -->
    <div class="gen-left">
      <!-- Scenario chips -->
      <div class="constructor-scenario-bar">
        <span class="scenario-label">{$t('editor.constructor_scenario')}:</span>
        {#each [
          ['rule-based', $t('editor.scenario_rule_based')],
          ['global-proxy', $t('editor.scenario_global_proxy')]
        ] as [id, label]}
          <button
            class="scenario-chip"
            aria-pressed={activePreset === id}
            class:active={activePreset === id}
            on:click={() => {
              applyPreset(id as 'rule-based' | 'global-proxy');
              if (id === 'rule-based') {
                activeSection = 'rulesets';
              }
            }}
          >{label}</button>
        {/each}
      </div>

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
            <div class="constructor-proxy-list">
              <button class="add-btn" on:click={() => (showProxyForm = true)}>
                + {ru ? 'Добавить прокси' : 'Add proxy'}
              </button>
              <button class="add-btn import-btn" on:click={loadSubscriptionProxies}>
                ↓ {$t('editor.constructor_import_proxies')}
              </button>
            </div>
          {/if}
        </div>
      {/if}

      <!-- GROUPS -->
      {#if activeSection === 'groups'}
        <div class="sec-body">
          {#each groups as g (g.id)}
            <div class="item-row">
              <span class="item-badge type-group">{g.type}</span>
              <span class="item-name">{g.name}</span>
              {#if g.includeAll}
                <span class="item-badge" style="background: rgba(139, 92, 246, 0.2); color: #a78bfa; font-size: 10px; text-transform: none;">include-all</span>
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
                <label class="toggle-label" style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;">
                  <input type="checkbox" bind:checked={ng.includeAll} />
                  <span>{ru ? 'Включить все провайдеры (include-all)' : 'Include all providers'}</span>
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
        </div>
      {/if}

      <!-- RULESETS -->
      {#if activeSection === 'rulesets'}
        <div class="sec-body" data-testid="rulesets-picker">
          <div class="card rulesets-card" style="padding:16px;">
            <div class="rulesets-header">
              <h3 style="margin-top:0; margin-bottom:4px; font-size:16px;">{$t('editor.rulesets_picker')}</h3>
              <p class="sub" style="margin-top:0; margin-bottom:16px; font-size:12px; color:var(--fg-dim);">
                {ru ? 'Выберите наборы правил и укажите группу для каждого.' : 'Select rule sets and assign a group for each.'}
              </p>
            </div>

            {#each Object.entries(META_RULE_SETS_BY_CATEGORY) as [category, items]}
              <div class="rulesets-category-group" style="margin-top:16px;">
                <h4 class="category-title" style="font-size:13px; font-weight:600; color:var(--fg-secondary); margin-bottom:8px; padding-bottom:4px; border-bottom:1px solid rgba(255,255,255,0.05);">{category}</h4>
                <div class="rulesets-grid" style="display:grid; grid-template-columns:repeat(auto-fill, minmax(260px, 1fr)); gap:8px;">
                  {#each items as item}
                    {@const key = `${item.id}|${item.type}`}
                    {@const isChecked = selectedMetaRuleSets.has(key)}
                    <div class="ruleset-item-row" class:selected={isChecked} style="display:flex; align-items:center; justify-content:space-between; padding:8px 12px; background:rgba(255,255,255,0.02); border:1px solid var(--border); border-radius:var(--radius); transition:background var(--transition-fast), border-color var(--transition-fast);">
                      <label class="ruleset-label" for="ruleset-{item.type}-{item.id}" style="display:flex; align-items:center; gap:8px; cursor:pointer; flex:1; user-select:none;">
                        <input
                          type="checkbox"
                          id="ruleset-{item.type}-{item.id}"
                          value={key}
                          checked={isChecked}
                          on:change={(e) => {
                            if (e.currentTarget.checked) {
                              let outbound = item.defaultOutbound;
                              if (outbound === 'Proxy' && groups.some(g => g.name === 'Selective')) {
                                outbound = 'Selective';
                              } else if (outbound === 'Proxy' && groups.some(g => g.name === 'Proxy')) {
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
                        <span class="ruleset-name" style="font-size:13px; font-weight:500; color:var(--fg-primary);">{item.label}</span>
                        <span class="ruleset-type-badge" style="font-size:9px; font-weight:700; text-transform:uppercase; color:var(--fg-dim); background:rgba(255,255,255,0.05); padding:1px 4px; border-radius:4px;">{item.type}</span>
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

      {#if embedded}
        <div class="gen-embedded-actions" style="margin-top: 12px; display: flex; gap: 8px;">
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
          <button class="btn btn-primary" on:click={copyYAML} disabled={!yaml} style="flex: 1;">
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
            {ru ? 'Копировать' : 'Copy'}
          </button>
        </div>
      {/if}
    </div>
  </div>
</div>

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
</style>
