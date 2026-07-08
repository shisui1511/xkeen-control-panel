<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { t, currentLang } from './i18n';
  import { capabilities, fetchCapabilities, showToast, devMode } from './stores';
  import { parseValidationError } from './lib/errorParser';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import PlayIcon from './lib/components/icons/Play.svelte';
  import WarningIcon from './lib/components/icons/Warning.svelte';
  import ChevronDown from './lib/components/icons/ChevronDown.svelte';

  // Subcomponents for providers (subscriptions)
  import SubscriptionList from './components/subscriptions/SubscriptionList.svelte';
  import SubscriptionFormModal from './components/subscriptions/SubscriptionFormModal.svelte';
  import NodeImporter from './components/subscriptions/NodeImporter.svelte';

  interface Proxy {
    name: string;
    type: string;
    alive?: boolean;
    delay?: number;
    history?: { time: string; delay: number }[];
  }

  function getProxyTypeLabel(proxy: Proxy | undefined): string {
    if (!proxy) return '';
    const type = proxy.type.toLowerCase();
    if (type === 'shadowsocks') return 'SS';
    if (type === 'shadowsocksr') return 'SSR';
    if (type === 'vmess') return 'VMess';
    if (type === 'vless') return 'VLess';
    if (type === 'trojan') return 'Trojan';
    if (type === 'hysteria') return 'Hysteria';
    if (type === 'hysteria2') return 'Hysteria 2';
    if (type === 'tuic') return 'TUIC';
    if (type === 'socks5') return 'Socks5';
    if (type === 'http') return 'HTTP';
    if (type === 'wireguard') return 'WG';
    return proxy.type;
  }

  interface ProxyGroup {
    name: string;
    type: string;
    now: string;
    all: string[];
    alive?: boolean;
    delay?: number;
    history?: { time: string; delay: number }[];
  }

  interface ObservatoryStats {
    totalProxies: number;
    healthyProxies: number;
    degradedProxies: number;
    downProxies: number;
    avgLatency: number;
  }

  interface Subscription {
    id: string;
    name: string;
    profile_title?: string;
    url: string;
    enabled: boolean;
    interval: number;
    use_provider_interval: boolean;
    enable_xray: boolean;
    enable_mihomo: boolean;
    mihomo_integrated: boolean;
    hwid_locked: boolean;
    last_update: string;
    last_error?: string;
    proxy_count?: number;
    upload?: number;
    download?: number;
    total?: number;
    expire?: number;
    support_url?: string;
    announcement?: string;
    profile_update_hours?: number;
    tag_prefix?: string;
    filter_name?: string;
    filter_type?: string;
    filter_transport?: string;
    mihomo_groups?: string[];
    routing_mode?: 'manual' | 'auto';
  }

  interface Node {
    tag: string;
    name?: string;
    country?: string;
    flag?: string;
    active: boolean;
    use_case?: string;
    speed?: string;
    protocol?: string;
    transport?: string;
    security?: string;
    is_new?: boolean;
  }

  interface NodeHealth {
    alive: boolean;
    delay?: number;
    http_code?: number;
  }

  // Active tab state: 'groups' | 'providers'
  let activeTab = $state<'groups' | 'providers'>(
    (typeof window !== 'undefined' && (window.location.hash.includes('tab=providers') || window.location.search.includes('tab=providers')))
      ? 'providers'
      : 'groups'
  );

  // Groups and Proxies states
  let groups = $state<ProxyGroup[]>([]);
  let proxies = $state<Record<string, Proxy>>({});
  let loading = $state(false);
  let error = $state('');
  let loadTimedOut = $state(false);
  let testingLatency = $state(false);
  let testingProxy = $state('');
  let loadTimeoutId: ReturnType<typeof setTimeout> | null = null;
  let collapsedGroups = $state(new Set<string>());
  let filterQuery = $state('');
  let seenGroups = $state(new Set<string>());
  const pendingTimeouts: ReturnType<typeof setTimeout>[] = [];

  // Subscription state variables
  let subscriptions = $state<Subscription[]>([]);
  let expandedSubs = $state<Record<string, boolean>>({});
  let subNodes = $state<Record<string, Node[]>>({});
  let subNodesLoading = $state<Record<string, boolean>>({});
  let subHealth = $state<Record<string, Record<string, NodeHealth>>>({});
  let checkingNodes = $state<Record<string, Record<string, boolean>>>({});
  let refreshLoading = $state<Record<string, boolean>>({});
  let activeDropdownId = $state<string | null>(null);

  // Form modal states for subscriptions
  let showAddModal = $state(false);
  let editingSub = $state<Subscription | null>(null);
  let formName = $state('');
  let formEnableXray = $state(false);
  let formEnableMihomo = $state(false);
  let formURL = $state('');
  let formInterval = $state(24);
  let formRoutingMode = $state<'manual' | 'auto'>('manual');
  let formTagPrefix = $state('');
  let formFilterName = $state('');
  let formFilterType = $state('');
  let formFilterTransport = $state('');
  let formMihomoGroups = $state<string[]>([]);
  let formEnabled = $state(true);
  let formUseProviderInterval = $state(false);
  let availableMihomoGroups = $state<string[]>([]);

  // Diagnostic states
  let showDiagnosticModal = $state(false);
  let diagnosticSub = $state<Subscription | null>(null);
  let diagnosticTab = $state<'report' | 'headers' | 'raw'>('report');
  let diagnosticLoading = $state(false);
  let parseReportData = $state<any>(null);
  let rawResponseData = $state<any>(null);

  // Auto-branding definitions
  const brandIcons: Record<string, { svg: string; color: string }> = {
    youtube: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M23.498 6.163a3.003 3.003 0 0 0-2.11-2.107C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.388.511a3.003 3.003 0 0 0-2.11 2.107C0 8.053 0 12 0 12s0 3.947.502 5.837a3.003 3.003 0 0 0 2.11 2.107C4.495 20.455 12 20.455 12 20.455s7.505 0 9.388-.511a3.003 3.003 0 0 0 2.11-2.107C24 15.947 24 12 24 12s0-3.947-.502-5.837zM9.545 15.568V8.432L15.818 12l-6.273 3.568z"/></svg>`,
      color: '#FF0000'
    },
    discord: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028c.462-.63.874-1.295 1.226-1.994.021-.041.001-.09-.041-.106a13.094 13.094 0 0 1-1.873-.894.077.077 0 0 1-.008-.128c.126-.093.252-.19.372-.287a.075.075 0 0 1 .077-.011c3.92 1.793 8.18 1.793 12.061 0a.073.073 0 0 1 .078.009c.12.099.246.195.373.289a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.894.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.078.078 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.156-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.156 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.156-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.156 2.418z"/></svg>`,
      color: '#5865F2'
    },
    telegram: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm5.894 8.221l-1.97 9.28c-.145.658-.537.818-1.084.508l-3-2.21-1.446 1.394c-.16.16-.295.295-.605.295l.213-3.053 5.56-5.017c.24-.213-.054-.334-.373-.12l-6.869 4.325-2.96-.924c-.643-.204-.657-.643.136-.953l11.57-4.458c.536-.196 1.006.128.832.978z"/></svg>`,
      color: '#26A5E4'
    },
    tg: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm5.894 8.221l-1.97 9.28c-.145.658-.537.818-1.084.508l-3-2.21-1.446 1.394c-.16.16-.295.295-.605.295l.213-3.053 5.56-5.017c.24-.213-.054-.334-.373-.12l-6.869 4.325-2.96-.924c-.643-.204-.657-.643.136-.953l11.57-4.458c.536-.196 1.006.128.832.978z"/></svg>`,
      color: '#26A5E4'
    },
    spotify: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.4 0 0 5.4 0 12s5.4 12 12 12 12-5.4 12-12S18.6 0 12 0zm5.5 17.3c-.2.3-.6.4-.9.2-2.3-1.4-5.3-1.8-8.8-1-.3.1-.7-.1-.8-.4-.1-.3.1-.7.4-.8 3.8-.9 7.1-.5 9.7 1.1.3.1.4.5.2.9zm1.5-3.3c-.3.4-.8.5-1.2.3-2.7-1.6-6.8-2.1-10-1.1-.4.1-.9-.1-1-.6-.1-.4.1-.9.6-1 3.7-1.1 8.2-.6 11.3 1.3.3.2.5.8.3 1.1zm.1-3.4C15.6 8.5 9.7 8.3 6.3 9.3c-.5.2-1.1-.1-1.2-.6-.2-.5.1-1.1.6-1.2 3.9-1.2 10.4-1 14.5 1.5.5.3.6 1 .3 1.5-.3.5-1 .6-1.4.3z"/></svg>`,
      color: '#1DB954'
    },
    steam: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 .002a11.996 11.996 0 0 0-11.968 10.74L6.16 14.9a3.298 3.298 0 0 1 3.27-2.903l2.802-4.004a3.3 3.3 0 1 1 3.3 3.3l-4.004 2.802a3.298 3.298 0 0 1-2.903 3.27l4.158 6.13A12 12 0 1 0 12 .002zm-2.57 15.6a1.65 1.65 0 1 0 0-3.3 1.65 1.65 0 0 0 0 3.3z"/></svg>`,
      color: 'var(--fg-primary)'
    },
    reddit: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M24 11.5c0-1.65-1.35-3-3-3-.96 0-1.86.48-2.42 1.24-1.64-1-3.85-1.64-6.23-1.72l1.32-4.17 4.31.91c0 1.1.9 2 2 2 1.1 0 2-.9 2-2s-.9-2-2-2c-.93 0-1.7.63-1.92 1.48l-4.82-1.02c-.18-.04-.38.07-.44.25l-1.5 4.74c-2.43.06-4.67.69-6.34 1.71-.56-.74-1.46-1.22-2.42-1.22-1.65 0-3 1.35-3 3 0 1.11.61 2.08 1.51 2.6-.08.4-.12.8-.12 1.2 0 4.14 4.83 7.5 10.78 7.5s10.78-3.36 10.78-7.5c0-.4-.04-.8-.12-1.2.9-.52 1.51-1.49 1.51-2.6z"/></svg>`,
      color: '#FF4500'
    },
    github: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.3 3.438 9.8 8.205 11.385.6.11.82-.26.82-.577v-2.234c-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22v3.293c0 .319.22.694.825.576C20.565 21.795 24 17.3 24 12c0-6.63-5.37-12-12-12z"/></svg>`,
      color: 'var(--fg-primary)'
    },
    gh: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.3 3.438 9.8 8.205 11.385.6.11.82-.26.82-.577v-2.234c-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22v3.293c0 .319.22.694.825.576C20.565 21.795 24 17.3 24 12c0-6.63-5.37-12-12-12z"/></svg>`,
      color: 'var(--fg-primary)'
    },
    google: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12.24 10.285V14.4h6.887c-.648 2.41-2.519 4.113-5.136 4.113-3.48 0-6.3-2.82-6.3-6.3 0-3.48 2.82-6.3 6.3-6.3 1.635 0 3.118.621 4.254 1.636l3.18-3.18C19.124 2.4 15.938 1.2 12.24 1.2 6.136 1.2 1.2 6.136 1.2 1.2 12.24s4.936 11.04 11.04 11.04c6.375 0 10.596-4.485 10.596-10.785 0-.727-.067-1.425-.195-2.1H12.24z"/></svg>`,
      color: 'var(--accent)'
    },
    netflix: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M15.986 0L8.014 11.562V0H4.5v24h3.514l7.972-11.562V24H19.5V0h-3.514z"/></svg>`,
      color: '#E50914'
    },
    twitch: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M11.571 4.714h1.715v5.143H11.57zm4.715 0H18v5.143h-1.714zM6 0L1.714 4.286v15.428h5.143V24l4.286-4.286h3.428L22.286 12V0zm14.571 11.143l-3.428 3.428h-3.429l-3 3v-3H6.857V1.714h13.714Z"/></svg>`,
      color: '#9146FF'
    },
    meta: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M15.282 5.093c.895 0 1.706.326 2.378.96 1.233 1.157 1.83 2.766 1.83 4.887 0 2.217-.655 3.916-1.892 4.981-.663.57-1.439.865-2.316.865-.632 0-1.242-.234-1.758-.636-.263-.207-.506-.44-.725-.7l-.804.896-.06.059c-.496.438-1.12.681-1.805.681-.877 0-1.653-.295-2.316-.865-1.237-1.065-1.892-2.764-1.892-4.98 0-2.122.597-3.73 1.83-4.888.672-.634 1.483-.96 2.378-.96.637 0 1.25.234 1.769.64.258.2.496.427.712.678l.805-.898.06-.057c.49-.43 1.11-.663 1.79-.663zm0-2.093c-1.3 0-2.455.518-3.282 1.353-.827-.835-1.982-1.353-3.282-1.353-2.11 0-3.957.905-5.228 2.505C1.196 7.157.4 9.423.4 12.016c0 2.64.757 4.9 2.052 6.55 1.272 1.62 3.12 2.527 5.266 2.527 1.3 0 2.455-.518 3.282-1.353.827.835 1.982 1.353 3.282 1.353 2.147 0 3.994-.906 5.266-2.527C20.843 16.917 21.6 14.657 21.6 12.016c0-2.593-.796-4.86-2.09-6.51-1.27-1.6-3.118-2.506-5.228-2.506z"/></svg>`,
      color: '#0668E1'
    },
    speedtest: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="m12 14 4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/></svg>`,
      color: '#00F0FF'
    },
    ai: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/><path d="M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z"/></svg>`,
      color: 'var(--accent)'
    },
    openai: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/><path d="M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z"/></svg>`,
      color: 'var(--accent)'
    },
    chatgpt: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/><path d="M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z"/></svg>`,
      color: 'var(--accent)'
    },
    cdn: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 17.58A5 5 0 0 0 18 8h-1.26A8 8 0 1 0 4 16.25"/><path d="M8 16h.01M8 20h.01M12 18h.01M12 22h.01M16 16h.01M16 20h.01"/></svg>`,
      color: '#A0A0A0'
    },
    tiktok: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12.53.07a8 8 0 0 1 .18 1.7 5.6 5.6 0 0 0 4.14 5.2 8 8 0 0 1-.22 1.6 7.1 7.1 0 0 1-3.52-1 8 8 0 0 1-.18-1.7 5.6 5.6 0 0 0-4.14-5.2v14a4.13 4.13 0 1 1-4.24-4.13h1.36v-1.6H4.15A5.73 5.73 0 1 0 9.88 20V0h2.65z"/></svg>`,
      color: '#FE2C55'
    },
    direct: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="5" y1="12" x2="19" y2="12"/><polyline points="12 5 19 12 12 19"/></svg>`,
      color: 'var(--success)'
    },
    reject: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg>`,
      color: 'var(--danger)'
    },
    block: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg>`,
      color: 'var(--danger)'
    },
    fallback: {
      svg: `<svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>`,
      color: 'var(--warning)'
    }
  };

  function getGroupIcon(groupName: string): { svg: string; color: string } | null {
    const lower = groupName.toLowerCase();
    for (const key of Object.keys(brandIcons)) {
      if (lower.includes(key)) {
        return brandIcons[key];
      }
    }
    return null;
  }

  // Country Flag Emoji definitions
  const flagMap: Record<string, string> = {
    RU: '🇷🇺', Russia: '🇷🇺', Россия: '🇷🇺',
    US: '🇺🇸', USA: '🇺🇸', США: '🇺🇸',
    GB: '🇬🇧', UK: '🇬🇧', 'United Kingdom': '🇬🇧', Англия: '🇬🇧',
    DE: '🇩🇪', Germany: '🇩🇪', Германия: '🇩🇪',
    NL: '🇳🇱', Netherlands: '🇳🇱', Нидерланды: '🇳🇱',
    FR: '🇫🇷', France: '🇫🇷', Франция: '🇫🇷',
    FI: '🇫🇮', Finland: '🇫🇮', Финляндия: '🇫🇮',
    TR: '🇹🇷', Turkey: '🇹🇷', Турция: '🇹🇷',
    SG: '🇸🇬', Singapore: '🇸🇬', Сингапур: '🇸🇬',
    JP: '🇯🇵', Japan: '🇯🇵', Япония: '🇯🇵',
    HK: '🇭🇰', 'Hong Kong': '🇭🇰', Гонконг: '🇭🇰',
    TW: '🇹🇼', Taiwan: '🇹🇼', Тайвань: '🇹🇼',
    KR: '🇰🇷', 'South Korea': '🇰🇷', Корея: '🇰🇷', Seoul: '🇰🇷',
    IN: '🇮🇳', India: '🇮🇳', Индия: '🇮🇳',
    BR: '🇧🇷', Brazil: '🇧🇷', Бразилия: '🇧🇷'
  };

  function getCountryFlag(nodeName: string): string {
    const lower = nodeName.toLowerCase();
    for (const [key, emoji] of Object.entries(flagMap)) {
      const escapedKey = key.replace(/[-/\\^$*+?.()|[\]{}]/g, '\\$&');
      const regex = new RegExp(`\\b${escapedKey}\\b|${escapedKey}`, 'i');
      if (regex.test(lower)) {
        return emoji;
      }
    }
    return '';
  }

  let searchDebouncedQuery = $state('');
  let searchTimeoutId: ReturnType<typeof setTimeout> | null = null;

  function handleSearchInput(e: Event) {
    const target = e.target as HTMLInputElement;
    if (searchTimeoutId) clearTimeout(searchTimeoutId);
    searchTimeoutId = setTimeout(() => {
      searchDebouncedQuery = target.value;
    }, 200);
  }

  function collapseAll() {
    const nextCollapsed = new Set<string>();
    groups.forEach((g) => nextCollapsed.add(g.name));
    collapsedGroups = nextCollapsed;
  }

  function expandAll() {
    collapsedGroups = new Set<string>();
  }

  function getFilteredNodes(group: ProxyGroup, query: string): string[] {
    if (query.trim() === '') return group.all;
    const groupNameMatch = group.name.toLowerCase().includes(query.trim().toLowerCase());
    if (groupNameMatch) return group.all;
    return group.all.filter((node) => node.toLowerCase().includes(query.trim().toLowerCase()));
  }

  let filteredGroups = $derived(
    searchDebouncedQuery.trim() === ''
      ? groups
      : groups.filter((g) => {
          const groupMatch = g.name.toLowerCase().includes(searchDebouncedQuery.trim().toLowerCase());
          const nodesMatch = g.all.some((node) => node.toLowerCase().includes(searchDebouncedQuery.trim().toLowerCase()));
          return groupMatch || nodesMatch;
        })
  );

  function getLastDelay(proxy: Proxy): number | undefined {
    if (proxy.history && proxy.history.length > 0) {
      return proxy.history[proxy.history.length - 1].delay;
    }
    return proxy.delay;
  }

  function isProxyAlive(proxy: Proxy): boolean {
    if (proxy.history && proxy.history.length > 0) {
      return proxy.history[proxy.history.length - 1].delay > 0;
    }
    return proxy.alive ?? false;
  }

  function updateCollapsed() {
    const current = new Set(groups.map((g) => g.name));
    const next = new Set(collapsedGroups);
    for (const name of [...next]) {
      if (!current.has(name)) next.delete(name);
    }
    for (const g of groups) {
      if (g.all.length > 8 && !seenGroups.has(g.name)) {
        next.add(g.name);
      }
      seenGroups.add(g.name);
    }
    collapsedGroups = next;
  }

  function toggleCollapse(groupName: string) {
    const next = new Set(collapsedGroups);
    if (next.has(groupName)) {
      next.delete(groupName);
    } else {
      next.add(groupName);
    }
    collapsedGroups = next;
  }

  function computeStats(): ObservatoryStats {
    const proxyList = Object.values(proxies).filter(
      (p) =>
        p.type !== 'Selector' &&
        p.type !== 'URLTest' &&
        p.type !== 'Fallback' &&
        p.type !== 'LoadBalance' &&
        p.type !== 'Direct' &&
        p.type !== 'Reject'
    );
    const total = proxyList.length;
    const healthy = proxyList.filter(
      (p) => isProxyAlive(p) && (getLastDelay(p) || 0) > 0 && (getLastDelay(p) || 0) < 300
    ).length;
    const degraded = proxyList.filter(
      (p) => isProxyAlive(p) && (getLastDelay(p) || 0) >= 300 && (getLastDelay(p) || 0) < 800
    ).length;
    const down = proxyList.filter(
      (p) => !isProxyAlive(p) || (getLastDelay(p) || 0) === 0 || (getLastDelay(p) || 0) >= 800
    ).length;

    const activeList = proxyList.filter((p) => isProxyAlive(p) && (getLastDelay(p) || 0) > 0);
    const avg =
      activeList.length > 0
        ? activeList.reduce((sum, p) => sum + (getLastDelay(p) || 0), 0) / activeList.length
        : 0;

    return {
      totalProxies: total,
      healthyProxies: healthy,
      degradedProxies: degraded,
      downProxies: down,
      avgLatency: Math.round(avg)
    };
  }

  async function fetchProxies() {
    loading = true;
    error = '';
    loadTimedOut = false;
    if (loadTimeoutId) clearTimeout(loadTimeoutId);
    loadTimeoutId = setTimeout(() => {
      if (loading) {
        loading = false;
        loadTimedOut = true;
        error = $t('ds.empty.load_timeout');
      }
    }, 10000);
    try {
      const res = await fetch('/api/mihomo/proxy/proxies');
      if (!res.ok) throw new Error($t('proxies.load_error'));
      const data = await res.json();
      proxies = data.proxies || {};
      const mappedGroups = Object.values(proxies)
        .filter((p: Proxy) => {
          return ['Selector', 'URLTest', 'Fallback', 'LoadBalance'].includes(p.type);
        })
        .map((p: any) => ({
          name: p.name,
          type: p.type,
          now: p.now || '',
          all: p.all || [],
          alive: p.alive,
          delay: p.history?.[p.history.length - 1]?.delay,
          history: p.history || []
        }));

      const groupNames = new Set(mappedGroups.map((g) => g.name));
      const isLeaf = (g: any) => {
        if (g.name === 'GLOBAL') return false;
        return !g.all.some((member: string) => member !== g.name && groupNames.has(member));
      };

      mappedGroups.sort((a, b) => {
        if (a.name === 'GLOBAL') return 1;
        if (b.name === 'GLOBAL') return -1;

        const aLeaf = isLeaf(a);
        const bLeaf = isLeaf(b);

        if (aLeaf && !bLeaf) return -1;
        if (!aLeaf && bLeaf) return 1;

        return a.name.localeCompare(b.name);
      });

      groups = mappedGroups;
      Object.keys(proxies).forEach((name) => {
        if (data.proxies[name]?.history) {
          proxies[name].history = data.proxies[name].history;
        }
      });
      updateCollapsed();
    } catch (e: any) {
      error = e.message;
    } finally {
      if (loadTimeoutId) {
        clearTimeout(loadTimeoutId);
        loadTimeoutId = null;
      }
      loading = false;
    }
  }

  async function selectProxy(groupName: string, proxyName: string) {
    const groupIndex = groups.findIndex((g) => g.name === groupName);
    if (groupIndex === -1) return;

    const oldProxyName = groups[groupIndex].now;
    groups[groupIndex] = {
      ...groups[groupIndex],
      now: proxyName
    };

    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/mihomo/proxy/proxies/${encodeURIComponent(groupName)}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ name: proxyName })
      });
      if (!res.ok) throw new Error($t('proxies.select_error'));
      await fetchProxies();
    } catch (e: any) {
      groups[groupIndex] = {
        ...groups[groupIndex],
        now: oldProxyName
      };
      showToast('error', $t('proxies.select_error'));
    }
  }

  async function testLatency() {
    testingLatency = true;
    error = '';
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const urlTestGroups = groups.filter((g) => g.type === 'URLTest');
      if (urlTestGroups.length > 0) {
        await Promise.all(
          urlTestGroups.map((g) =>
            fetch(
              `/api/mihomo/proxy/group/${encodeURIComponent(g.name)}/delay?url=http://www.gstatic.com/generate_204&timeout=5000`,
              {
                method: 'GET',
                headers: { 'X-CSRF-Token': csrfToken || '' }
              }
            )
          )
        );
      } else {
        const res = await fetch(
          '/api/mihomo/proxy/proxies/delay?url=http://www.gstatic.com/generate_204&timeout=5000',
          {
            method: 'GET',
            headers: { 'X-CSRF-Token': csrfToken || '' }
          }
        );
        if (!res.ok) throw new Error($t('proxies.load_error'));
      }
      safeTimeout(async () => {
        await fetchProxies();
        testingLatency = false;
      }, 2000);
    } catch (e: any) {
      showToast('error', e.message);
      testingLatency = false;
    }
  }

  async function testProxyLatency(proxyName: string) {
    testingProxy = proxyName;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(
        `/api/mihomo/proxy/proxies/${encodeURIComponent(proxyName)}/delay?url=http://www.gstatic.com/generate_204&timeout=5000`,
        {
          method: 'GET',
          headers: { 'X-CSRF-Token': csrfToken || '' }
        }
      );
      if (!res.ok) throw new Error($t('proxies.load_error'));
      safeTimeout(async () => {
        await fetchProxies();
        testingProxy = '';
      }, 1500);
    } catch (e: any) {
      showToast('error', e.message);
      testingProxy = '';
    }
  }

  function getGroupTypeLabel(type: string): string {
    const labels: Record<string, string> = {
      Selector: 'Selector',
      URLTest: 'URLTest',
      Fallback: 'Fallback',
      LoadBalance: 'LoadBalance'
    };
    return labels[type] || type;
  }

  function getProxyDelay(proxyName: string): number | undefined {
    const proxy = proxies[proxyName];
    if (!proxy) return undefined;
    return getLastDelay(proxy);
  }

  function getLatencyClass(proxyName: string): string {
    const proxy = proxies[proxyName];
    if (!proxy) return 'lat dim';
    if (
      ['DIRECT', 'REJECT'].includes(proxyName.toUpperCase()) ||
      ['Direct', 'Reject', 'Compatible'].includes(proxy.type)
    )
      return 'lat dim';
    const delay = getProxyDelay(proxyName);
    if (delay === undefined || delay === 0 || delay >= 800) return 'lat bad';
    if (delay < 300) return 'lat ok';
    return 'lat mid';
  }

  function getLatencyText(proxyName: string): string {
    const proxy = proxies[proxyName];
    if (!proxy) return '—';
    if (
      ['DIRECT', 'REJECT'].includes(proxyName.toUpperCase()) ||
      ['Direct', 'Reject', 'Compatible'].includes(proxy.type)
    )
      return '—';
    const delay = getProxyDelay(proxyName);
    if (delay === undefined || delay === 0 || delay >= 800) return 'timeout';
    return `${delay} ${$t('app.ms')}`;
  }

  let mihomoLaunching = $state(false);

  async function launchMihomo() {
    mihomoLaunching = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/mihomo/control', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ action: 'start' })
      });
      if (!res.ok) throw new Error('Failed to start Mihomo');
      safeTimeout(async () => {
        await fetchCapabilities();
        await fetchProxies();
        mihomoLaunching = false;
      }, 1500);
      safeTimeout(async () => {
        await fetchCapabilities();
        await fetchProxies();
      }, 4000);
    } catch (e: any) {
      showToast('error', e.message);
      mihomoLaunching = false;
    }
  }

  function safeTimeout(fn: () => void | Promise<void>, ms: number): ReturnType<typeof setTimeout> {
    const id = setTimeout(fn, ms);
    pendingTimeouts.push(id);
    return id;
  }

  // Stats derived for subscriptions
  let stats = $derived(
    (() => {
      const totalNodes = subscriptions.reduce((sum, s) => sum + (s.proxy_count || 0), 0);
      let minNext = Infinity;
      subscriptions.forEach((s) => {
        if (s.enabled && s.last_update && !s.last_update.startsWith('0001')) {
          const next = new Date(s.last_update).getTime() + s.interval * 3600 * 1000;
          const diff = next - Date.now();
          if (diff > 0 && diff < minNext) {
            minNext = diff;
          }
        }
      });
      let nextStr = '—';
      if (minNext !== Infinity) {
        const diffHours = Math.floor(minNext / (3600 * 1000));
        const diffMins = Math.floor((minNext % (3600 * 1000)) / (60 * 1000));
        nextStr = `${diffHours}ч ${diffMins}м`;
      }
      return {
        total: subscriptions.length,
        nodes: totalNodes,
        next: nextStr
      };
    })()
  );

  async function openDiagnosticModal(sub: Subscription) {
    diagnosticSub = sub;
    showDiagnosticModal = true;
    diagnosticTab = 'report';
    diagnosticLoading = true;
    parseReportData = null;
    rawResponseData = null;

    try {
      const resReport = await fetch(`/api/subscriptions/parse-report?id=${sub.id}`);
      if (resReport.ok) {
        parseReportData = await resReport.json();
      }
      const resRaw = await fetch(`/api/subscriptions/raw?id=${sub.id}`);
      if (resRaw.ok) {
        rawResponseData = await resRaw.json();
      }
    } catch (e) {
      // Ignored
    } finally {
      diagnosticLoading = false;
    }
  }

  function closeDiagnosticModal() {
    showDiagnosticModal = false;
    diagnosticSub = null;
  }

  async function loadAvailableMihomoGroups() {
    try {
      const res = await fetch('/api/config/read?path=%2Fopt%2Fetc%2Fmihomo%2Fconfig.yaml');
      if (!res.ok) return;
      const data = await res.json();
      const yamlContent = data.content || '';
      const groupNames: string[] = [];
      const lines = yamlContent.split('\n');
      let inProxyGroups = false;
      for (let line of lines) {
        const trimmed = line.trim();
        if (trimmed.startsWith('proxy-groups:')) {
          inProxyGroups = true;
          continue;
        }
        if (inProxyGroups) {
          if (line.startsWith('-') || line.startsWith(' ') || line.trim() === '') {
            if (trimmed.startsWith('- name:')) {
              const name = trimmed.replace('- name:', '').trim().replace(/^['"]|['"]$/g, '');
              if (name) groupNames.push(name);
            }
          } else {
            break;
          }
        }
      }
      availableMihomoGroups = groupNames;
    } catch (e) {
      availableMihomoGroups = [];
    }
  }

  async function loadSubscriptions() {
    loading = true;
    try {
      const res = await fetch('/api/subscriptions');
      if (res.ok) {
        subscriptions = await res.json();
      }
    } catch (e) {
      showToast('error', $t('subscr.load_error'));
    } finally {
      loading = false;
    }
  }

  async function refreshSubscription(id: string) {
    refreshLoading[id] = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/subscriptions/refresh?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadSubscriptions();
        if (expandedSubs[id]) {
          await loadNodes(id);
        }
      } else {
        const text = await res.text();
        const parsedErr = parseValidationError(text, $currentLang === 'ru' ? 'ru' : 'en');
        showToast('error', parsedErr || $t('app.error'));
        await loadSubscriptions();
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    } finally {
      refreshLoading[id] = false;
    }
  }

  async function refreshAll() {
    loading = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/subscriptions/refresh-all', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadSubscriptions();
        for (const id of Object.keys(expandedSubs)) {
          if (expandedSubs[id]) {
            await loadNodes(id);
          }
        }
      } else {
        showToast('error', $t('app.error'));
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    } finally {
      loading = false;
    }
  }

  async function saveSubscription() {
    if (!formURL.trim()) {
      showToast('error', $t('subscr.fill_url') || 'Please fill in the URL field');
      return;
    }

    const csrfToken = localStorage.getItem('csrf_token');
    const payload = {
      id: editingSub ? editingSub.id : '',
      name: formName,
      url: formURL,
      enabled: formEnabled,
      interval: formInterval,
      use_provider_interval: formUseProviderInterval,
      enable_xray: formEnableXray,
      enable_mihomo: formEnableMihomo,
      tag_prefix: formTagPrefix,
      filter_name: formFilterName,
      filter_type: formFilterType,
      filter_transport: formFilterTransport,
      mihomo_groups: formMihomoGroups,
      routing_mode: formRoutingMode
    };

    try {
      const url = editingSub ? '/api/subscriptions/update' : '/api/subscriptions/add';
      const res = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify(payload)
      });

      if (res.ok) {
        showToast('success', $t('app.success'));
        showAddModal = false;
        await loadSubscriptions();
      } else {
        const text = await res.text();
        const parsedErr = parseValidationError(text, $currentLang === 'ru' ? 'ru' : 'en');
        showToast('error', parsedErr || $t('app.error'));
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    }
  }

  async function deleteSubscription(id: string) {
    const sub = subscriptions.find((s) => s.id === id);
    if (!sub) return;
    const confirmMsg = $t('subscr.delete_confirm')
      ? $t('subscr.delete_confirm').replace('{name}', sub.profile_title || sub.name)
      : `Удалить подписку: Вы уверены, что хотите безвозвратно удалить подписку '${sub.profile_title || sub.name}'?`;

    if (!confirm(confirmMsg)) return;

    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(`/api/subscriptions/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadSubscriptions();
      } else {
        showToast('error', $t('app.error'));
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    }
  }

  function openAddModal() {
    editingSub = null;
    formName = '';
    formURL = '';
    formInterval = 24;
    formEnabled = true;
    formUseProviderInterval = false;
    formEnableXray = true;
    formEnableMihomo = false;
    formRoutingMode = 'manual';
    formTagPrefix = '';
    formFilterName = '';
    formFilterType = '';
    formFilterTransport = '';
    formMihomoGroups = [];
    showAddModal = true;
    loadAvailableMihomoGroups();
  }

  function openEditModal(sub: Subscription) {
    editingSub = sub;
    formName = sub.name;
    formURL = sub.url;
    formInterval = sub.interval;
    formEnabled = sub.enabled;
    formUseProviderInterval = sub.use_provider_interval ?? false;
    formEnableXray = sub.enable_xray ?? false;
    formEnableMihomo = sub.enable_mihomo ?? false;
    formRoutingMode = sub.routing_mode ?? 'manual';
    formTagPrefix = sub.tag_prefix ?? '';
    formFilterName = sub.filter_name ?? '';
    formFilterType = sub.filter_type ?? '';
    formFilterTransport = sub.filter_transport ?? '';
    formMihomoGroups = sub.mihomo_groups ?? [];
    showAddModal = true;
    loadAvailableMihomoGroups();
  }

  function closeModal() {
    showAddModal = false;
    editingSub = null;
  }

  function toggleDropdown(id: string) {
    if (activeDropdownId === id) {
      activeDropdownId = null;
    } else {
      activeDropdownId = id;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      closeModal();
      closeDiagnosticModal();
    }
  }

  function handleClickOutside(e: MouseEvent) {
    if (activeDropdownId) {
      const target = e.target as HTMLElement;
      if (!target.closest('.dropdown-container')) {
        activeDropdownId = null;
      }
    }
  }

  async function loadNodes(subId: string) {
    subNodesLoading[subId] = true;
    try {
      const res = await fetch(`/api/subscriptions/nodes?id=${subId}`);
      if (res.ok) {
        subNodes[subId] = await res.json();
      }
    } catch (e) {
      // Ignore
    } finally {
      subNodesLoading[subId] = false;
    }
  }

  async function toggleExpand(subId: string) {
    expandedSubs[subId] = !expandedSubs[subId];
    if (expandedSubs[subId]) {
      await loadNodes(subId);
    }
  }

  async function checkNodeHealth(subId: string, nodeTag: string) {
    if (!checkingNodes[subId]) checkingNodes[subId] = {};
    checkingNodes[subId][nodeTag] = true;
    try {
      const res = await fetch(
        `/api/subscriptions/health?id=${subId}&tag=${encodeURIComponent(nodeTag)}`
      );
      if (res.ok) {
        const health = await res.json();
        if (!subHealth[subId]) subHealth[subId] = {};
        subHealth[subId][nodeTag] = health;
      }
    } catch (e) {
      // Ignore
    } finally {
      checkingNodes[subId][nodeTag] = false;
    }
  }

  async function setActiveNode(subId: string, nodeTag: string) {
    const csrfToken = localStorage.getItem('csrf_token');
    try {
      const res = await fetch(
        `/api/subscriptions/active?id=${subId}&tag=${encodeURIComponent(nodeTag)}`,
        {
          method: 'POST',
          headers: { 'X-CSRF-Token': csrfToken || '' }
        }
      );
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadNodes(subId);
      } else {
        const text = await res.text();
        showToast('error', text || $t('app.error'));
      }
    } catch (e) {
      showToast('error', $t('app.error'));
    }
  }

  function checkAutoExpand() {
    const hash = window.location.hash;
    const regex = /#\/proxies\?expand=(.+)/;
    const match = hash.match(regex);
    if (match && match[1]) {
      const subId = match[1];
      expandedSubs[subId] = true;
      loadNodes(subId).then(() => {
        setTimeout(() => {
          const el = document.getElementById(`sub-card-${subId}`);
          if (el) {
            el.scrollIntoView({ behavior: 'smooth', block: 'start' });
          }
        }, 100);
      });
    }
  }

  interface ChainItem {
    name: string;
    isGroup: boolean;
  }

  function getSelectionChain(groupName: string): ChainItem[] {
    const chain: ChainItem[] = [];
    let current = groupName;
    const visited = new Set<string>();
    while (current && !visited.has(current)) {
      visited.add(current);
      const grp = groups.find((g) => g.name === current);
      if (!grp) {
        break;
      }
      const selected = grp.now;
      if (!selected) break;
      const isSelectedGroup = groups.some((g) => g.name === selected);
      chain.push({ name: selected, isGroup: isSelectedGroup });
      current = selected;
    }
    return chain;
  }

  onMount(() => {
    const hash = window.location.hash;
    if (hash.includes('tab=providers') || window.location.search.includes('tab=providers')) {
      activeTab = 'providers';
    }

    fetchProxies();
    loadSubscriptions().then(() => {
      checkAutoExpand();
    });

    const interval = setInterval(() => {
      fetchProxies();
      loadSubscriptions();
    }, 10000);

    const handleHashChange = () => {
      if (window.location.hash.includes('tab=providers')) {
        activeTab = 'providers';
      } else if (window.location.hash.includes('tab=groups')) {
        activeTab = 'groups';
      }
      checkAutoExpand();
    };

    window.addEventListener('hashchange', handleHashChange);
    window.addEventListener('click', handleClickOutside);
    window.addEventListener('keydown', handleKeydown);

    return () => {
      clearInterval(interval);
      if (loadTimeoutId) clearTimeout(loadTimeoutId);
      pendingTimeouts.forEach(clearTimeout);
      window.removeEventListener('hashchange', handleHashChange);
      window.removeEventListener('click', handleClickOutside);
      window.removeEventListener('keydown', handleKeydown);
    };
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('proxies.title')}
      </div>
      <h1>{$t('proxies.title')}</h1>
      <p class="sub">{$t('proxies.subtitle')}</p>
    </div>
    {#if activeTab === 'groups'}
      <div class="ph-actions">
        <button class="btn btn-secondary" onclick={collapseAll} title={$t('proxies.collapse_all')}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;">
            <polyline points="18 15 12 9 6 15" />
            <polyline points="18 20 12 14 6 20" />
          </svg>
          {$t('proxies.collapse_all') || 'Свернуть все'}
        </button>
        <button class="btn btn-secondary" onclick={expandAll} title={$t('proxies.expand_all')}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;">
            <polyline points="6 9 12 15 18 9" />
            <polyline points="6 4 12 10 18 4" />
          </svg>
          {$t('proxies.expand_all') || 'Развернуть все'}
        </button>

        <input
          class="group-search"
          type="search"
          bind:value={filterQuery}
          oninput={handleSearchInput}
          placeholder={$t('proxies.filter_placeholder')}
          aria-label={$t('proxies.filter_placeholder')}
        />
        <button class="btn btn-secondary" onclick={fetchProxies} disabled={loading}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
          >
          {loading ? $t('app.loading') : $t('app.refresh')}
        </button>
        <button class="btn btn-primary" onclick={testLatency} disabled={testingLatency}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="currentColor"
            style="margin-right: 6px;"><polygon points="5 3 19 12 5 21 5 3" /></svg
          >
          {testingLatency ? $t('proxies.testing') : $t('proxies.test_latency')}
        </button>
      </div>
    {:else}
      <div class="ph-actions">
        <button class="btn btn-secondary" onclick={refreshAll} disabled={loading}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
          >
          {$t('subscr.refresh_all') || 'Обновить всё'}
        </button>

        <button class="btn btn-primary" onclick={openAddModal}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
          >
          {$t('subscr.add') || 'Добавить подписку'}
        </button>
      </div>
    {/if}
  </div>

  <!-- Вкладки (Tabs) -->
  <div class="tabs-container">
    <button
      class="tab-btn"
      class:active={activeTab === 'groups'}
      onclick={() => activeTab = 'groups'}
    >
      {$t('proxies.tab_groups') || 'Группы'}
    </button>
    <button
      class="tab-btn"
      class:active={activeTab === 'providers'}
      onclick={() => activeTab = 'providers'}
    >
      {$t('proxies.tab_providers') || 'Провайдеры'}
    </button>
  </div>

  {#if activeTab === 'groups'}
    {#if $capabilities !== null && !$capabilities.mihomo.reachable}
      <EmptyState
        title={$t('ds.empty.mihomo_offline_title')}
        description={$capabilities?.active_kernel === 'mihomo'
          ? $t('ds.empty.mihomo_offline_desc_actionable')
          : $t('ds.empty.mihomo_offline_desc')}
        icon={PlayIcon}
        ctaText={mihomoLaunching
          ? $t('ds.empty.mihomo_offline_loading')
          : $t('ds.empty.mihomo_offline_cta')}
        ctaLoading={mihomoLaunching}
        oncta={launchMihomo}
      />
    {:else if error}
      <EmptyState
        title={$t('ds.empty.error_title')}
        description={error}
        icon={WarningIcon}
        ctaText={$t('app.refresh')}
        oncta={fetchProxies}
      />
    {:else}
      <!-- Observatory statistics -->
      {#if groups.length > 0 && $capabilities?.mihomo?.reachable}
        {@const stats = computeStats()}
        <div class="card" style="margin-bottom:18px;">
          <h2 class="card-title" style="margin-top: 0;">{$t('proxies.observatory_title')}</h2>
          <div class="stats-grid">
            <div class="stat-box">
              <div class="stat-label">{$t('proxies.obs_total')}</div>
              <div class="stat-value">{stats.totalProxies}</div>
              <div class="res-sub">{$t('proxies.obs_total_sub', { groupsCount: groups.length })}</div>
            </div>
            <div class="stat-box">
              <div class="stat-label">{$t('proxies.obs_healthy')}</div>
              <div class="stat-value" style="color:var(--success);">{stats.healthyProxies}</div>
              <div class="res-sub">{$t('proxies.obs_healthy_sub')}</div>
            </div>
            <div class="stat-box">
              <div class="stat-label">{$t('proxies.obs_degraded')}</div>
              <div class="stat-value" style="color:var(--warning);">{stats.degradedProxies}</div>
              <div class="res-sub">{$t('proxies.obs_degraded_sub')}</div>
            </div>
            <div class="stat-box">
              <div class="stat-label">{$t('proxies.obs_unreachable')}</div>
              <div class="stat-value" style="color:var(--danger);">{stats.downProxies}</div>
              <div class="res-sub">{$t('proxies.obs_unreachable_sub')}</div>
            </div>
          </div>
        </div>
      {/if}

      <!-- Groups Grid -->
      {#if loading && groups.length === 0}
        <div class="group-grid">
          {#each Array(4) as _}
            <div class="group-card skeleton-card">
              <div class="gc-head">
                <Skeleton width="120px" height="18px" />
                <Skeleton width="60px" height="14px" style="margin-left: auto;" />
              </div>
              <div class="proxy-grid">
                {#each Array(3) as _}
                  <div class="proxy-card">
                    <div class="p-header">
                      <Skeleton width="70px" height="14px" />
                      <Skeleton width="30px" height="10px" />
                    </div>
                    <div class="p-footer">
                      <Skeleton width="40px" height="10px" />
                    </div>
                  </div>
                {/each}
              </div>
            </div>
          {/each}
        </div>
      {:else if groups.length === 0}
        <EmptyState
          title={$t('proxies.no_proxies')}
          description={$t('proxies.no_proxies_desc') || ''}
          icon={WarningIcon}
          ctaText={$t('app.refresh')}
          oncta={fetchProxies}
        />
      {:else}
        <div class="group-grid">
          {#each filteredGroups as group}
            {@const isCollapsed = collapsedGroups.has(group.name)}
            {@const collapsible = group.all.length > 8}
            {@const nodes = getFilteredNodes(group, searchDebouncedQuery)}
            {@const icon = getGroupIcon(group.name)}
            <div class="group-card">
              <div
                class="gc-head"
                class:collapsible
                role={collapsible ? 'button' : undefined}
                tabindex={collapsible ? 0 : undefined}
                aria-expanded={collapsible ? !isCollapsed : undefined}
                onclick={() => collapsible && toggleCollapse(group.name)}
                onkeydown={(e) =>
                  (e.key === 'Enter' || e.key === ' ') && collapsible && toggleCollapse(group.name)}
              >
                <div class="gc-head-row1">
                  {#if icon}
                    <span class="group-icon-wrap" style="color: {icon.color}; display: inline-flex; align-items: center; justify-content: center; width: 16px; height: 16px; margin-right: 6px;">
                      <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                      {@html icon.svg}
                    </span>
                  {/if}
                  <span class="name">{group.name}</span>
                  <span class="type-badge">{group.type.toUpperCase()}</span>
                  
                  {#if group.now}
                    {@const latencyClass = getLatencyClass(group.now)}
                    {@const latencyText = getLatencyText(group.now)}
                    <div class="gc-lat-box {latencyClass}">{latencyText}</div>
                  {/if}
                  
                  {#if collapsible}
                    <span class="chevron-wrap" class:rotated={!isCollapsed} aria-hidden="true">
                      <ChevronDown size={14} color={isCollapsed ? 'var(--fg-dim)' : 'var(--accent)'} />
                    </span>
                  {/if}
                </div>
                
                <div class="gc-head-row2">
                  <span class="gc-count-text">{group.all.length} {$t('proxies.obs_unreachable_sub') ? 'узлов' : 'nodes'}</span>
                  <span class="gc-separator">·</span>
                  <span class="gc-active-label">{$t('proxies.active') || 'Активен'}:</span>
                  
                  {#each getSelectionChain(group.name) as item, index}
                    {@const itemFlag = !item.isGroup ? getCountryFlag(item.name) : null}
                    {@const itemLatencyText = getLatencyText(item.name)}
                    {@const itemLatencyClass = getLatencyClass(item.name)}
                    {#if index > 0}
                      <span class="gc-arrow">›</span>
                    {/if}
                    <div class="gc-now-pill" class:is-leaf={!item.isGroup} class:lat-ok={itemLatencyClass === 'lat ok'} class:lat-mid={itemLatencyClass === 'lat mid'} class:lat-bad={itemLatencyClass === 'lat bad'}>
                      <div class="gc-now-dot" class:is-leaf={!item.isGroup} class:lat-ok={itemLatencyClass === 'lat ok'} class:lat-mid={itemLatencyClass === 'lat mid'} class:lat-bad={itemLatencyClass === 'lat bad'}></div>
                      {#if itemFlag}{itemFlag} {/if}{item.name}
                    </div>
                  {:else}
                    <span style="color:var(--fg-dim)">—</span>
                  {/each}
                </div>
              </div>

              {#if isCollapsed}
                <div class="dot-container">
                  {#each nodes as proxyName}
                    {@const healthClass = getLatencyClass(proxyName)}
                    {@const healthText = getLatencyText(proxyName)}
                    {@const isActive = group.now === proxyName}
                    <button
                      class="dot-indicator {healthClass}"
                      class:now={isActive}
                      title={group.type === 'Selector' ? `${proxyName}: ${healthText}` : $t('proxies.managed_automatically')}
                      aria-label="{proxyName}: {healthText}"
                      style={group.type === 'Selector' ? 'cursor: pointer;' : 'cursor: default;'}
                      onclick={(e) => {
                        e.stopPropagation();
                        if (group.type === 'Selector') {
                          selectProxy(group.name, proxyName);
                          toggleCollapse(group.name);
                        }
                      }}
                    ></button>
                  {/each}
                </div>
              {:else}
                <div class="proxy-grid">
                  {#each nodes as proxyName}
                    {@const isActive = group.now === proxyName}
                    {@const healthClass = getLatencyClass(proxyName)}
                    {@const healthText = getLatencyText(proxyName)}
                    {@const proxy = proxies[proxyName]}
                    {@const flag = getCountryFlag(proxyName)}

                    <div
                      class="proxy-card"
                      class:now={isActive}
                      role="button"
                      tabindex={group.type === 'Selector' ? 0 : -1}
                      title={group.type !== 'Selector' ? $t('proxies.managed_automatically') : undefined}
                      onclick={() => group.type === 'Selector' && selectProxy(group.name, proxyName)}
                      onkeydown={(e) =>
                        e.key === 'Enter' &&
                        group.type === 'Selector' &&
                        selectProxy(group.name, proxyName)}
                      style={group.type === 'Selector' ? 'cursor: pointer;' : 'cursor: default;'}
                    >
                      <div class="p-header">
                        <span class="p-name">
                          {#if flag}{flag} {/if}{proxyName}
                        </span>
                        <span class="p-type">{getProxyTypeLabel(proxy)}</span>
                      </div>

                      <div class="p-footer">
                        <span class={healthClass}>{healthText}</span>

                        <div class="p-actions-wrap">
                          {#if !['DIRECT', 'REJECT'].includes(proxyName.toUpperCase()) && !['Direct', 'Reject', 'Compatible'].includes(proxy?.type || '')}
                            <button
                              class="btn-latency-test"
                              onclick={(e) => { e.stopPropagation(); testProxyLatency(proxyName); }}
                              disabled={testingProxy === proxyName}
                              title={$t('proxies.test_single')}
                            >
                              {#if testingProxy === proxyName}
                                <span class="spinner" style="font-size: 10px; font-family: monospace;">...</span>
                              {:else}
                                <svg
                                  width="12"
                                  height="12"
                                  viewBox="0 0 24 24"
                                  fill="none"
                                  stroke="currentColor"
                                  stroke-width="2"
                                  style="opacity: 0.6;"
                                ><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" /></svg>
                              {/if}
                            </button>
                          {/if}

                          {#if group.type === 'Selector'}
                            <span class="selector-dot" class:active={isActive}>{isActive ? '●' : '○'}</span>
                          {/if}
                        </div>
                      </div>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  {:else if activeTab === 'providers'}
    {#if $capabilities?.xray && !$capabilities.xray.conf_dir_exists && $capabilities.active_kernel === 'xray'}
      <div class="confdir-warning">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="flex-shrink:0"
          ><path
            d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
          /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg
        >
        <span>{$t('subscr.confdir_warning').replace('{dir}', $capabilities.xray.conf_dir)}</span>
      </div>
    {/if}

    <div class="providers-view">
      {#if subscriptions.length === 0}
        <div
          class="card text-center"
          style="padding: 3rem; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1rem;"
        >
          <p style="color: var(--fg-secondary); margin: 0;">{$t('subscr.empty') || 'Список подписок пуст'}</p>
          <button class="btn btn-primary" onclick={openAddModal}>
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              style="margin-right: 6px;"
            >
              <line x1="12" y1="5" x2="12" y2="19"></line>
              <line x1="5" y1="12" x2="19" y2="12"></line>
            </svg>
            {$t('subscr.add_first') || 'Добавить первую подписку'}
          </button>
        </div>
      {:else}
        <SubscriptionList
          {subscriptions}
          {expandedSubs}
          {refreshLoading}
          {activeDropdownId}
          {subNodesLoading}
          {subNodes}
          {subHealth}
          {checkingNodes}
          devMode={$devMode}
          {stats}
          onToggleExpand={toggleExpand}
          onRefreshSub={refreshSubscription}
          onEditSub={openEditModal}
          onDeleteSub={deleteSubscription}
          onOpenDiagnostic={openDiagnosticModal}
          onSetActiveNode={setActiveNode}
          onCheckNodeHealth={checkNodeHealth}
          onToggleDropdown={toggleDropdown}
        />
      {/if}
    </div>
  {/if}
</div>

{#if showAddModal}
  <SubscriptionFormModal
    {editingSub}
    bind:formName
    bind:formEnableXray
    bind:formEnableMihomo
    bind:formURL
    bind:formInterval
    bind:formRoutingMode
    bind:formTagPrefix
    bind:formFilterName
    bind:formFilterType
    bind:formFilterTransport
    bind:formMihomoGroups
    bind:formEnabled
    bind:formUseProviderInterval
    {availableMihomoGroups}
    onClose={closeModal}
    onSave={saveSubscription}
  />
{/if}

{#if showDiagnosticModal && diagnosticSub}
  <NodeImporter
    {diagnosticSub}
    diagnosticTab={diagnosticTab}
    diagnosticLoading={diagnosticLoading}
    parseReportData={parseReportData}
    rawResponseData={rawResponseData}
    onClose={closeDiagnosticModal}
    onTabChange={(tab) => (diagnosticTab = tab)}
  />
{/if}

<style>
  /* Tabs styles */
  .tabs-container {
    display: flex;
    gap: 8px;
    margin-bottom: 20px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 0;
  }
  .tab-btn {
    background: transparent;
    border: none;
    padding: 10px 16px;
    color: var(--fg-dim);
    font-weight: 500;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    transition: all 0.2s;
    font-size: 14px;
  }
  .tab-btn:hover {
    color: var(--fg-primary);
  }
  .tab-btn.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  /* Confdir warning styles */
  .confdir-warning {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 14px;
    margin-bottom: 16px;
    background: color-mix(in srgb, var(--color-warning, #f59e0b) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--color-warning, #f59e0b) 40%, transparent);
    border-radius: var(--radius-sm, 6px);
    color: var(--fg);
    font-size: 13px;
    line-height: 1.5;
  }
  .confdir-warning svg {
    color: var(--color-warning, #f59e0b);
    margin-top: 2px;
  }

  .group-grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 16px;
    margin-bottom: 30px;
  }
  .group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg, 10px);
    overflow: hidden;
    box-shadow: var(--shadow-sm);
    transition: all 0.2s ease;
  }
  .group-card:hover {
    box-shadow: 0 4px 20px rgba(0,0,0,0.35), 0 0 0 1px rgba(41, 194, 240, 0.15);
  }
  .group-card.skeleton-card {
    border-style: dashed;
    background: transparent;
  }
  .group-card .gc-head {
    background: linear-gradient(135deg, rgba(20, 51, 79, 0.9), rgba(16, 42, 68, 0.95));
    padding: 14px 18px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-bottom: 1px solid var(--border-strong);
    position: relative;
    overflow: hidden;
  }
  .group-card .gc-head::before {
    content: '';
    position: absolute;
    top: 0; left: 0; right: 0; bottom: 0;
    background: radial-gradient(ellipse at top left, rgba(41, 194, 240, 0.05), transparent 60%);
    pointer-events: none;
  }
  .group-card .gc-head.collapsible {
    cursor: pointer;
    user-select: none;
  }
  .group-card .gc-head.collapsible:hover {
    background: var(--hover);
  }
  .gc-head-row1 {
    display: flex;
    align-items: center;
    width: 100%;
    gap: 10px;
  }
  .gc-head-row2 {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    font-size: 12px;
    color: var(--fg-secondary);
    width: 100%;
    margin-top: 2px;
  }
  .group-card .gc-head .name {
    font-weight: 800;
    color: var(--fg-primary);
    font-size: 15px;
    letter-spacing: -0.01em;
  }
  .type-badge {
    margin-left: auto;
    font-size: 10px;
    padding: 2px 8px;
    border-radius: 99px;
    background: rgba(41, 194, 240, 0.1);
    border: 1px solid rgba(41, 194, 240, 0.2);
    color: var(--accent);
    font-family: var(--font-family-mono);
    font-weight: 700;
    text-transform: uppercase;
  }
  .gc-lat-box {
    padding: 3px 10px;
    border-radius: 99px;
    font-family: var(--font-family-mono);
    font-size: 11px;
    font-weight: 800;
  }
  .gc-lat-box.lat.ok {
    color: var(--success);
    background: rgba(70, 209, 138, 0.15);
    border: 1px solid rgba(70, 209, 138, 0.35);
  }
  .gc-lat-box.lat.mid {
    color: var(--warning);
    background: rgba(240, 180, 80, 0.15);
    border: 1px solid rgba(240, 180, 80, 0.35);
  }
  .gc-lat-box.lat.bad {
    color: var(--danger);
    background: rgba(239, 91, 107, 0.15);
    border: 1px solid rgba(239, 91, 107, 0.35);
  }
  .gc-lat-box.lat.dim {
    color: var(--fg-dim);
    background: rgba(92, 116, 145, 0.15);
    border: 1px solid rgba(92, 116, 145, 0.35);
  }
  .gc-count-text {
    color: var(--fg-dim);
  }
  .gc-separator {
    color: var(--fg-faint);
  }
  .gc-active-label {
    color: var(--fg-secondary);
    font-size: 11px;
  }
  .gc-arrow {
    color: var(--fg-faint);
    margin: 0 2px;
  }
  .gc-now-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 10px;
    border-radius: var(--radius-lg, 10px);
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--border);
    color: var(--fg-primary);
    font-size: 11px;
    font-weight: 600;
    transition: all 0.2s;
  }
  .gc-now-pill.is-leaf {
    background: rgba(41, 194, 240, 0.08);
    border-color: rgba(41, 194, 240, 0.2);
    color: var(--accent);
  }
  .gc-now-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--fg-dim);
  }
  .gc-now-dot.is-leaf {
    background: var(--accent);
  }
  .gc-now-dot.lat-ok {
    background: var(--success);
  }
  .gc-now-dot.lat-mid {
    background: var(--warning);
  }
  .gc-now-dot.lat-bad {
    background: var(--danger);
  }
  .gc-now-pill.lat-ok {
    background: rgba(70, 209, 138, 0.08);
    border-color: rgba(70, 209, 138, 0.2);
    color: var(--success);
  }
  .gc-now-pill.lat-mid {
    background: rgba(240, 180, 80, 0.08);
    border-color: rgba(240, 180, 80, 0.2);
    color: var(--warning);
  }
  .gc-now-pill.lat-bad {
    background: rgba(239, 91, 107, 0.08);
    border-color: rgba(239, 91, 107, 0.2);
    color: var(--danger);
  }

  .proxy-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 8px;
    padding: 12px;
  }

  .proxy-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    min-height: 84px;
    position: relative;
    transition: all var(--transition-fast);
  }
  .proxy-card::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: var(--radius-md);
    background: linear-gradient(135deg, rgba(41, 194, 240, 0.03), transparent);
    opacity: 0;
    transition: opacity var(--transition-fast);
    pointer-events: none;
  }
  .proxy-card:hover {
    border-color: var(--border-strong);
    transform: translateY(-1px);
    background: var(--hover);
  }
  .proxy-card:hover::after {
    opacity: 1;
  }
  .proxy-card.now {
    background: linear-gradient(135deg, rgba(41, 194, 240, 0.12), rgba(41, 194, 240, 0.04));
    border-color: rgba(41, 194, 240, 0.45);
    box-shadow: inset 0 0 0 1px rgba(41, 194, 240, 0.08), 0 2px 8px rgba(41, 194, 240, 0.08);
  }
  .proxy-card .p-header {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-bottom: 8px;
  }
  .proxy-card .p-name {
    font-weight: 600;
    color: var(--fg-primary);
    font-size: 13px;
    word-break: break-all;
  }
  .proxy-card .p-type {
    color: var(--fg-dim);
    font-size: 11px;
    font-family: var(--font-family-mono);
  }
  .proxy-card .p-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    margin-top: auto;
  }
  .p-actions-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .btn-latency-test {
    background: transparent;
    border: none;
    padding: 4px;
    color: var(--fg-dim);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s;
    border-radius: var(--radius-sm);
  }
  .btn-latency-test:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.05);
  }

  .dot-container {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 10px 18px 14px;
  }
  .dot-indicator {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    border: none;
    padding: 0;
    background: var(--fg-dim);
    transition: transform 0.2s, box-shadow 0.2s;
  }
  .dot-indicator:hover {
    transform: scale(1.4);
  }
  .dot-indicator.now {
    box-shadow: 0 0 0 2px var(--bg-card), 0 0 0 4px var(--accent);
  }
  .dot-indicator.ok {
    background: var(--success);
  }
  .dot-indicator.mid {
    background: var(--warning);
  }
  .dot-indicator.bad {
    background: var(--danger);
  }
  .dot-indicator.dim {
    background: var(--fg-dim);
  }

  .selector-dot {
    font-size: 14px;
    color: var(--fg-dim);
    font-weight: 700;
  }
  .selector-dot.active {
    color: var(--accent);
  }

  .brand-icon {
    width: 16px;
    height: 16px;
    vertical-align: middle;
  }

  .lat {
    font-family: var(--font-family-mono);
    font-size: 12px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 4px;
    white-space: nowrap;
  }
  .lat.ok {
    color: var(--success);
    background: color-mix(in srgb, var(--success) 10%, transparent);
  }
  .lat.mid {
    color: var(--warning);
    background: color-mix(in srgb, var(--warning) 10%, transparent);
  }
  .lat.bad {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }
  .lat.dim {
    color: var(--fg-dim);
    background: color-mix(in srgb, var(--fg-dim) 10%, transparent);
  }

  .group-search {
    padding: 6px 12px;
    border: 1px solid var(--border);
    background: var(--bg-input);
    color: var(--fg);
    border-radius: var(--radius-sm);
    font-size: 13px;
    width: 200px;
    transition: border-color 0.2s;
  }
  .group-search:focus {
    border-color: var(--accent);
    outline: none;
  }

  .chevron-wrap {
    display: inline-flex;
    align-items: center;
    transition: transform var(--transition-normal);
  }
  .chevron-wrap.rotated {
    transform: rotate(180deg);
  }

  /* Mobile: proxy cards stack, observatory stats handled globally at 768px */
  @media (max-width: 640px) {
    .group-grid {
      gap: 10px;
    }
    .group-card .gc-head {
      padding: 12px 14px;
      flex-wrap: wrap;
      gap: 6px;
    }
    .proxy-grid {
      grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
      gap: 6px;
      padding: 8px;
    }
    .proxy-card {
      padding: 8px 10px;
      min-height: 70px;
    }
    .proxy-card .p-name {
      font-size: 12px;
    }
    .lat {
      font-size: 11px;
      padding: 2px 5px;
    }
    .group-search {
      width: 100%;
    }
  }
</style>
