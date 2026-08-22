<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { t, tp, currentLang } from './i18n';
  import { usePoller } from './lib/poller';
  import { capabilities, fetchCapabilities, showToast, devMode, showConfirm } from './stores';
  import { apiFetch, apiFetchJSON } from './lib/api';
  import { parseValidationError } from './lib/errorParser';
  import { getCountryFlag } from './lib/countryFlags';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import PlayIcon from './lib/components/icons/Play.svelte';
  import WarningIcon from './lib/components/icons/Warning.svelte';
  import ChevronDown from './lib/components/icons/ChevronDown.svelte';
  import FloatingProgress from './components/FloatingProgress.svelte';
  import LatencyHistoryPopover from './components/LatencyHistoryPopover.svelte';
  import PingTargetQuickMenu from './components/PingTargetQuickMenu.svelte';
  import {
    BatchLatencyTester,
    type BatchProgressState,
    formatTimeAgo
  } from './lib/batchLatencyTester';
  import { getTargetUrl, getCurrentPingConfig } from './lib/pingTargetStore';
  import type { PollerControls } from './lib/poller';
  import {
    splitGroupsByRole,
    classifyGroupRole,
    classifyLatency,
    isSystemProxy,
    isProxyGroupType,
    type GroupRole
  } from './lib/proxyClassification';
  import {
    readPinnedCoreGroups,
    togglePinnedCoreGroup,
    readProxiesViewMode,
    writeProxiesViewMode,
    type ProxiesViewMode
  } from './lib/proxyViewPrefs';
  import {
    getLastDelay,
    isProxyAlive,
    computeObservatoryStats,
    computeGroupHealthStats,
    groupMatchesLatencyFilter,
    type ObservatoryFilter,
    type NodeSnapshot
  } from './lib/proxyStats';
  import Pin from './lib/components/icons/Pin.svelte';
  import ViewGrid from './lib/components/icons/ViewGrid.svelte';
  import ViewList from './lib/components/icons/ViewList.svelte';
  import ObservatoryPanel from './components/proxies/ObservatoryPanel.svelte';
  import HealthBar from './components/proxies/HealthBar.svelte';
  import QuickSelectPopover, {
    type QuickSelectNode
  } from './components/proxies/QuickSelectPopover.svelte';

  // Subcomponents for providers (subscriptions)
  import SubscriptionList from './components/subscriptions/SubscriptionList.svelte';
  import SubscriptionFormModal from './components/subscriptions/SubscriptionFormModal.svelte';
  import NodeImporter from './components/subscriptions/NodeImporter.svelte';

  interface Proxy {
    name: string;
    type: string;
    alive?: boolean;
    delay?: number;
    now?: string;
    all?: string[];
    provider?: string;
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
    icon?: string;
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
    mihomo_provider?: {
      name: string;
      vehicle_type: string;
      updated_at: string;
      node_count: number;
    } | null;
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
    tested?: boolean;
  }

  // Active tab state: 'groups' | 'providers'
  let activeTab = $state<'groups' | 'providers'>(
    typeof window !== 'undefined' &&
      (window.location.hash.includes('tab=providers') ||
        window.location.search.includes('tab=providers'))
      ? 'providers'
      : 'groups'
  );

  // Groups and Proxies states
  let groups = $state<ProxyGroup[]>([]);
  let proxies = $state<Record<string, Proxy>>({});
  let loading = $state(false);
  let error = $state('');
  let loadTimedOut = $state(false);
  let testingGroupName = $state<string | null>(null);
  let testingProxy = $state('');
  let loadTimeoutId: ReturnType<typeof setTimeout> | null = null;
  let collapsedGroups = $state(new Set<string>());
  let filterQuery = $state('');
  let seenGroups = $state(new Set<string>());
  const pendingTimeouts: ReturnType<typeof setTimeout>[] = [];

  // Core-routing segmentation state (D-01, D-04)
  let pinnedCoreGroups = $state<string[]>(readPinnedCoreGroups());
  let groupCardEls = $state<Record<string, HTMLElement | null>>({});

  // Interactive observatory filter (D-08)
  let observatoryFilter = $state<ObservatoryFilter>(null);

  // Quick-Select popover state (D-13)
  let quickSelect = $state<{ groupName: string; anchor: HTMLElement } | null>(null);

  // Batch testing & latency history state
  let poller = $state<PollerControls | null>(null);
  const batchTester = new BatchLatencyTester();
  let batchProgress = $state<BatchProgressState | null>(null);
  let activePopover = $state<{
    name: string;
    history: { time: string; delay: number }[];
    el: HTMLElement;
  } | null>(null);
  let popoverHoverTimeout: ReturnType<typeof setTimeout> | null = null;

  // Subscription state variables
  let subscriptions = $state<Subscription[]>([]);
  let expandedSubs = $state<Record<string, boolean>>({});
  let subNodes = $state<Record<string, Node[]>>({});
  let subNodesLoading = $state<Record<string, boolean>>({});
  let subHealth = $state<Record<string, Record<string, NodeHealth>>>({});
  let checkingNodes = $state<Record<string, Record<string, boolean>>>({});
  let refreshLoading = $state<Record<string, boolean>>({});
  let subNodesError = $state<Record<string, boolean>>({});
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

  // View mode state (D-17)
  let viewMode = $state<ProxiesViewMode>(readProxiesViewMode());
  function setViewMode(mode: ProxiesViewMode) {
    viewMode = mode;
    writeProxiesViewMode(mode);
  }

  // Chunked rendering state (D-20)
  const NODE_RENDER_CHUNK = 50;
  let nodeRenderLimit = $state<Record<string, number>>({});

  function getRenderLimit(groupName: string): number {
    return nodeRenderLimit[groupName] ?? NODE_RENDER_CHUNK;
  }

  function increaseRenderLimit(groupName: string, total: number) {
    const current = getRenderLimit(groupName);
    const next = Math.min(total, current + NODE_RENDER_CHUNK);
    nodeRenderLimit[groupName] = next;
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
    nodeRenderLimit = {};
  }

  function expandAll() {
    // Сброс лимитов до NODE_RENDER_CHUNK по умолчанию для каждой группы
    nodeRenderLimit = {};
    const groupNames = groups.map((g) => g.name);
    const BATCH_SIZE = 4;
    const nextCollapsed = new Set(collapsedGroups);

    for (let i = 0; i < groupNames.length; i += BATCH_SIZE) {
      const batch = groupNames.slice(i, i + BATCH_SIZE);
      const delay = Math.floor(i / BATCH_SIZE) * 16;
      safeTimeout(() => {
        batch.forEach((name) => nextCollapsed.delete(name));
        collapsedGroups = new Set(nextCollapsed);
      }, delay);
    }
  }

  function getFilteredNodes(group: ProxyGroup, query: string): string[] {
    if (query.trim() === '') return group.all;
    const groupNameMatch = group.name.toLowerCase().includes(query.trim().toLowerCase());
    if (groupNameMatch) return group.all;
    return group.all.filter((node) => node.toLowerCase().includes(query.trim().toLowerCase()));
  }

  let filteredGroups = $derived(
    groups.filter((g) => {
      const matchesFilter = groupMatchesLatencyFilter(
        g.all,
        observatoryFilter,
        resolveNodeSnapshot
      );
      if (!matchesFilter) return false;
      if (searchDebouncedQuery.trim() === '') return true;
      const groupMatch = g.name.toLowerCase().includes(searchDebouncedQuery.trim().toLowerCase());
      const nodesMatch = g.all.some((node) =>
        node.toLowerCase().includes(searchDebouncedQuery.trim().toLowerCase())
      );
      return groupMatch || nodesMatch;
    })
  );

  // Core-routing segmentation (D-01, D-04) — depends on filteredGroups above.
  let proxyTypeMap = $derived(
    Object.fromEntries(Object.entries(proxies).map(([k, v]) => [k, v?.type]))
  );
  let groupSections = $derived(
    splitGroupsByRole(filteredGroups, new Set(pinnedCoreGroups), proxyTypeMap)
  );
  let coreNodesCount = $derived(groupSections.core.reduce((s, g) => s + g.all.length, 0));
  let serviceNodesCount = $derived(groupSections.service.reduce((s, g) => s + g.all.length, 0));
  let systemNodesCount = $derived(groupSections.system.reduce((s, g) => s + g.all.length, 0));

  function getEffectiveProxy(proxyName: string): Proxy | undefined {
    let currentName = proxyName;
    const visited = new Set<string>();
    while (currentName && !visited.has(currentName)) {
      visited.add(currentName);
      const p = proxies[currentName];
      if (!p) break;
      if (isProxyGroupType(p.type) && p.now) {
        if (p.now === currentName) break;
        currentName = p.now;
        continue;
      }
      return p;
    }
    return proxies[currentName];
  }

  function updateCollapsed() {
    const current = new Set(groups.map((g) => g.name));
    const next = new Set(collapsedGroups);
    for (const name of [...next]) {
      if (!current.has(name)) next.delete(name);
    }
    const pinnedNames = new Set(pinnedCoreGroups);
    for (const g of groups) {
      if (!seenGroups.has(g.name)) {
        next.add(g.name);
      }
      seenGroups.add(g.name);
      // Служебные мини-карточки (D-03): свернутое состояние по умолчанию, не
      // только при первом появлении — разворачивать в них нечего.
      if (classifyGroupRole(g, groups, pinnedNames, proxyTypeMap) === 'system') {
        next.add(g.name);
      }
    }
    collapsedGroups = next;
  }

  function toggleCollapse(groupName: string) {
    const next = new Set(collapsedGroups);
    if (next.has(groupName)) {
      next.delete(groupName);
    } else {
      next.add(groupName);
      delete nodeRenderLimit[groupName];
    }
    collapsedGroups = next;
  }

  // .gc-head is a non-button div[role="button"] (Pitfall 1 / D-02, D-13): nested
  // interactive elements (breadcrumb chip, pin button) mark themselves with
  // data-stop-head-click so the collapse toggle below ignores clicks on them.
  function handleHeadClick(e: MouseEvent, groupName: string) {
    const target = e.target as HTMLElement;
    if (target.closest('[data-stop-head-click]')) return;
    toggleCollapse(groupName);
  }

  function handleHeadKeydown(e: KeyboardEvent, groupName: string) {
    if (e.target !== e.currentTarget) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      toggleCollapse(groupName);
    }
  }

  // D-02: scrolls to and briefly highlights the parent group's card in the
  // Core section when a breadcrumb chip in a service card is clicked.
  function focusGroupCard(name: string) {
    const el = groupCardEls[name];
    if (!el) return;
    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    el.classList.add('flash-highlight');
    safeTimeout(() => {
      el.classList.remove('flash-highlight');
    }, 1400);
  }

  let groupFilters = $state<Record<string, 'all' | 'working' | 'timeouts' | 'latency'>>({});

  function getFilteredGroupNodes(groupName: string, allNodes: string[]): string[] {
    const filter = groupFilters[groupName] || 'all';
    let list = [...allNodes];
    if (filter === 'working') {
      list = list.filter((name) => {
        const delay = getProxyDelay(name);
        return delay !== undefined && delay > 0 && delay <= 800;
      });
    } else if (filter === 'timeouts') {
      list = list.filter((name) => {
        const delay = getProxyDelay(name);
        return delay === 0 || delay === undefined || delay > 800;
      });
    } else if (filter === 'latency') {
      list.sort((a, b) => {
        const delayA = getProxyDelay(a) ?? 99999;
        const delayB = getProxyDelay(b) ?? 99999;
        return delayA - delayB;
      });
    }
    return list;
  }

  let observatoryStats = $derived(computeObservatoryStats(proxies, subNodes, subHealth));

  async function fetchProxies(signal?: AbortSignal) {
    const reqSignal = signal instanceof AbortSignal ? signal : undefined;
    if (Object.keys(proxies).length === 0) {
      loading = true;
    }
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
      const [proxiesRes, providersRes] = await Promise.allSettled([
        apiFetchJSON<{ proxies: Record<string, any> }>('/api/mihomo/proxy/proxies', {
          signal: reqSignal
        }),
        apiFetchJSON<{ providers: Record<string, any> }>('/api/mihomo/proxy/providers/proxies', {
          signal: reqSignal
        })
      ]);

      const rootProxies = proxiesRes.status === 'fulfilled' ? proxiesRes.value?.proxies || {} : {};
      const providersMap =
        providersRes.status === 'fulfilled' ? providersRes.value?.providers || {} : {};

      const mergedProxies: Record<string, any> = { ...rootProxies };

      for (const [provName, provData] of Object.entries(providersMap)) {
        if (provData && Array.isArray((provData as any).proxies)) {
          for (const node of (provData as any).proxies) {
            if (!node || !node.name) continue;
            if (!mergedProxies[node.name]) {
              mergedProxies[node.name] = { ...node, provider: provName };
            } else {
              mergedProxies[node.name] = {
                ...mergedProxies[node.name],
                ...node,
                provider: provName
              };
            }
          }
        }
      }

      proxies = mergedProxies;

      const mappedGroups = Object.values(rootProxies)
        .filter((p: Proxy) => isProxyGroupType(p.type))
        .map((p: any) => ({
          name: p.name,
          type: p.type,
          now: p.now || '',
          all: p.all || [],
          alive: p.alive,
          delay: p.history?.[p.history.length - 1]?.delay,
          history: p.history || [],
          icon: String(p.icon || '').trim()
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
      updateCollapsed();
    } catch (e: any) {
      if (e?.name !== 'AbortError' && e?.status !== 401) {
        error = e.message;
      }
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
      const res = await apiFetch(`/api/mihomo/proxy/proxies/${encodeURIComponent(groupName)}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ name: proxyName })
      });
      if (!res.ok) throw new Error($t('proxies.select_error'));
      showToast(
        'success',
        $t('proxies.quick_select_success', { group: groupName, node: proxyName })
      );
      await fetchProxies();
    } catch (e: any) {
      groups[groupIndex] = {
        ...groups[groupIndex],
        now: oldProxyName
      };
      if (e?.status === 401) return;
      showToast('error', $t('proxies.select_error'));
    }
  }

  function openQuickSelect(anchor: HTMLElement, groupName: string) {
    if (quickSelect?.groupName === groupName) {
      quickSelect = null;
    } else {
      quickSelect = { groupName, anchor };
    }
  }

  async function handleQuickSelect(nodeName: string) {
    if (!quickSelect) return;
    const groupName = quickSelect.groupName;
    const grp = groups.find((g) => g.name === groupName);
    quickSelect = null;
    if (!grp || grp.type.toLowerCase() !== 'selector') return;
    await selectProxy(groupName, nodeName);
  }

  function buildQuickSelectNodes(group: ProxyGroup): QuickSelectNode[] {
    return group.all.map((name) => {
      const delay = getProxyDelay(name);
      const eff = getEffectiveProxy(name);
      const alive = eff ? isProxyAlive(eff) : proxies[name] ? isProxyAlive(proxies[name]) : true;
      const bucket = classifyLatency(delay, alive);
      return {
        name,
        isGroup: !!groups.find((g) => g.name === name),
        bucket,
        delay,
        latencyText: getLatencyText(name),
        latencyClass: getLatencyClass(name)
      };
    });
  }

  function isLatencyStale(proxyName: string): boolean {
    const hist = getProxyHistory(proxyName);
    if (!hist || hist.length === 0) return false;
    const lastItem = hist[hist.length - 1];
    if (!lastItem?.time) return false;
    const timestamp = new Date(lastItem.time).getTime();
    if (isNaN(timestamp)) return false;
    return Date.now() - timestamp > 300000; // 5 minutes TTL
  }

  function getProxyLastTime(proxyName: string): number | null {
    const hist = getProxyHistory(proxyName);
    if (!hist || hist.length === 0) return null;
    const lastItem = hist[hist.length - 1];
    if (!lastItem?.time) return null;
    const t = new Date(lastItem.time).getTime();
    return isNaN(t) ? null : t;
  }

  function getLatencyTitle(proxyName: string): string {
    const eff = getEffectiveProxy(proxyName);
    const proxy = eff || proxies[proxyName];
    const delay = getProxyDelay(proxyName);
    const parts: string[] = [];

    if (delay !== undefined) {
      parts.push(delay === 0 ? $t('proxies.timeout') : `${delay} ms`);
    } else {
      parts.push($t('proxies.not_tested'));
    }

    const lastTime = getProxyLastTime(proxyName);
    if (lastTime) {
      parts.push(formatTimeAgo(lastTime, $t));
    }

    if (proxy && (proxy.all?.length ?? 0) > 0) {
      parts.push(`${proxy.all?.length} ${$tp('proxies.nodes', proxy.all?.length ?? 0)}`);
    }

    return parts.join(' · ');
  }

  function handleBadgeMouseEnter(e: MouseEvent, proxyName: string) {
    if (popoverHoverTimeout) clearTimeout(popoverHoverTimeout);
    const target = e.currentTarget as HTMLElement;
    popoverHoverTimeout = setTimeout(() => {
      const history = getProxyHistory(proxyName);
      if (history && history.length > 0) {
        activePopover = {
          name: proxyName,
          history,
          el: target
        };
      }
    }, 400);
  }

  function handleBadgeMouseLeave() {
    if (popoverHoverTimeout) {
      clearTimeout(popoverHoverTimeout);
      popoverHoverTimeout = null;
    }
  }

  function handleBadgeClick(e: MouseEvent | KeyboardEvent, proxyName: string) {
    e.stopPropagation();
    if (popoverHoverTimeout) {
      clearTimeout(popoverHoverTimeout);
      popoverHoverTimeout = null;
    }
    const target = e.currentTarget as HTMLElement;
    const history = getProxyHistory(proxyName);
    if (activePopover?.name === proxyName) {
      activePopover = null;
    } else if (history && history.length > 0) {
      activePopover = {
        name: proxyName,
        history,
        el: target
      };
    }
  }

  async function testGroupLatency(group: ProxyGroup) {
    if (batchTester.isActive() || testingGroupName) return;
    testingGroupName = group.name;
    poller?.pause();
    const pingConfig = getCurrentPingConfig();
    const targetUrl = getTargetUrl(pingConfig);
    const timeoutMs = pingConfig.timeoutMs;

    try {
      const res = await apiFetch(
        `/api/mihomo/proxy/group/${encodeURIComponent(group.name)}/delay?url=${encodeURIComponent(targetUrl)}&timeout=${timeoutMs}`,
        { method: 'GET' }
      );
      if (res.ok) {
        const data = await res.json();
        if (data && typeof data === 'object') {
          for (const [nodeName, nodeDelay] of Object.entries(data)) {
            const delayNum = typeof nodeDelay === 'number' ? nodeDelay : 0;
            if (proxies[nodeName]) {
              const hist = proxies[nodeName].history ? [...proxies[nodeName].history!] : [];
              hist.push({ time: new Date().toISOString(), delay: delayNum });
              proxies[nodeName] = {
                ...proxies[nodeName],
                delay: delayNum,
                alive: delayNum > 0,
                history: hist
              };
            }
          }
          showToast('success', $t('proxies.test_group_success'));
          return;
        }
      }

      // Fallback to batch tester
      const nodeSet = new Set<string>();
      for (const node of group.all) {
        const p = proxies[node];
        if (
          node &&
          !['DIRECT', 'REJECT'].includes(node.toUpperCase()) &&
          p?.type !== 'Direct' &&
          p?.type !== 'Reject'
        ) {
          nodeSet.add(node);
        }
      }
      const nodes = Array.from(nodeSet);
      if (nodes.length === 0) return;

      await batchTester.run({
        nodes,
        targetUrl,
        timeoutMs,
        onProgressChange: (state) => {
          batchProgress = state;
          testingGroupName = state.running ? group.name : null;
        },
        onNodeComplete: (node, delay, rawHistoryItem) => {
          if (proxies[node]) {
            const currentHist = proxies[node].history ? [...proxies[node].history!] : [];
            if (rawHistoryItem) {
              currentHist.push(rawHistoryItem);
            }
            proxies[node] = {
              ...proxies[node],
              delay: delay ?? 0,
              alive: (delay ?? 0) > 0,
              history: currentHist
            };
          }
        }
      });
    } catch (err: any) {
      if (err?.name !== 'AbortError') {
        showToast('error', err?.message || 'Error testing group');
      }
    } finally {
      testingGroupName = null;
      batchProgress = null;
      poller?.resume();
    }
  }

  async function testProxyLatency(proxyName: string) {
    testingProxy = proxyName;
    const pingConfig = getCurrentPingConfig();
    const targetUrl = getTargetUrl(pingConfig);
    const timeoutMs = pingConfig.timeoutMs;

    try {
      const isGroup =
        groups.some((g) => g.name === proxyName) || isProxyGroupType(proxies[proxyName]?.type);

      if (isGroup) {
        const res = await apiFetch(
          `/api/mihomo/proxy/group/${encodeURIComponent(proxyName)}/delay?url=${encodeURIComponent(targetUrl)}&timeout=${timeoutMs}`,
          { method: 'GET' }
        );
        if (!res.ok) throw new Error($t('proxies.load_error'));
        const data = await res.json();
        if (data && typeof data === 'object') {
          for (const [nodeName, nodeDelay] of Object.entries(data)) {
            const delayNum = typeof nodeDelay === 'number' ? nodeDelay : 0;
            if (proxies[nodeName]) {
              const hist = proxies[nodeName].history ? [...proxies[nodeName].history!] : [];
              hist.push({ time: new Date().toISOString(), delay: delayNum });
              proxies[nodeName] = {
                ...proxies[nodeName],
                delay: delayNum,
                alive: delayNum > 0,
                history: hist
              };
            }
          }
        }
      } else {
        const res = await apiFetch(
          `/api/mihomo/proxy/proxies/${encodeURIComponent(proxyName)}/delay?url=${encodeURIComponent(targetUrl)}&timeout=${timeoutMs}`,
          { method: 'GET' }
        );
        if (!res.ok) {
          const prov = proxies[proxyName]?.provider;
          if (prov) {
            await apiFetch(
              `/api/mihomo/proxy/providers/proxies/${encodeURIComponent(prov)}/healthcheck?url=${encodeURIComponent(targetUrl)}&timeout=${timeoutMs}`,
              { method: 'GET' }
            );
            await fetchProxies();
            return;
          }
          throw new Error($t('proxies.load_error'));
        }
        const data = await res.json();
        const delay = typeof data?.delay === 'number' ? data.delay : 0;
        if (proxies[proxyName]) {
          const currentHist = proxies[proxyName].history ? [...proxies[proxyName].history!] : [];
          currentHist.push({
            time: new Date().toISOString(),
            delay
          });
          proxies[proxyName] = {
            ...proxies[proxyName],
            delay,
            alive: delay > 0,
            history: currentHist
          };
        }
      }
    } catch (e: any) {
      showToast('error', e.message);
    } finally {
      testingProxy = '';
    }
  }

  function getGroupTypeLabel(type: string): string {
    const key = (type || '').toLowerCase();
    const labelKeys: Record<string, string> = {
      selector: 'proxies.group_type_selector',
      urltest: 'proxies.group_type_urltest',
      fallback: 'proxies.group_type_fallback',
      loadbalance: 'proxies.group_type_loadbalance',
      relay: 'proxies.group_type_relay'
    };
    const translationKey = labelKeys[key];
    return translationKey ? $t(translationKey) : type;
  }

  function getProxyDelay(proxyName: string): number | undefined {
    const eff = getEffectiveProxy(proxyName);
    if (eff) {
      const d = getLastDelay(eff);
      if (d !== undefined) return d;
    }
    const proxy = proxies[proxyName];
    if (!proxy) return undefined;
    return getLastDelay(proxy);
  }

  function resolveNodeSnapshot(name: string): NodeSnapshot | undefined {
    const p = getEffectiveProxy(name);
    if (!p) return undefined;
    return {
      name: p.name,
      type: p.type,
      delay: getProxyDelay(p.name),
      alive: isProxyAlive(p)
    };
  }

  function getProxyHistory(proxyName: string): any[] {
    const eff = getEffectiveProxy(proxyName);
    if (eff && eff.history && eff.history.length > 0) {
      return eff.history;
    }
    return proxies[proxyName]?.history || [];
  }

  function getLatencyClass(proxyName: string): string {
    const eff = getEffectiveProxy(proxyName);
    const proxy = eff || proxies[proxyName];
    if (!proxy) return 'lat dim';
    if (isSystemProxy(proxy.name || proxyName, proxy.type)) return 'lat dim';
    const delay = getProxyDelay(proxyName);
    const alive = isProxyAlive(proxy);
    const bucket = classifyLatency(delay, alive);
    if (bucket === 'unchecked') return 'lat dim';
    let baseClass = 'lat';
    if (bucket === 'fast') {
      baseClass += ' ok';
    } else if (bucket === 'mid') {
      baseClass += ' mid';
    } else {
      baseClass += ' bad';
    }
    if (isLatencyStale(proxyName)) {
      baseClass += ' latency-stale';
    }
    return baseClass;
  }

  function getLatencyText(proxyName: string): string {
    const eff = getEffectiveProxy(proxyName);
    const proxy = eff || proxies[proxyName];
    if (!proxy) return '—';
    if (isSystemProxy(proxy.name || proxyName, proxy.type)) return '—';
    const delay = getProxyDelay(proxyName);
    const alive = isProxyAlive(proxy);
    const bucket = classifyLatency(delay, alive);
    if (bucket === 'unchecked') return '—';
    if (bucket === 'bad') return 'timeout';
    const prefix = isLatencyStale(proxyName) ? '~' : '';
    return `${prefix}${delay} ${$t('app.ms')}`;
  }

  let mihomoLaunching = $state(false);

  async function launchMihomo() {
    mihomoLaunching = true;
    try {
      const res = await apiFetch('/api/mihomo/control', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
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
      if (e?.status === 401) return;
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
      const totalNodes = subscriptions.reduce((sum, s) => {
        const count =
          getNodeSource(s) === 'mihomo'
            ? (s.mihomo_provider?.node_count ?? s.proxy_count ?? 0)
            : s.proxy_count || 0;
        return sum + count;
      }, 0);
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
        nextStr = `${diffHours}${$t('conn.hrs')} ${diffMins}${$t('conn.min')}`;
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
      const resReport = await apiFetch(`/api/subscriptions/parse-report?id=${sub.id}`);
      if (resReport.ok) {
        parseReportData = await resReport.json();
      }
      const resRaw = await apiFetch(`/api/subscriptions/raw?id=${sub.id}`);
      if (resRaw.ok) {
        rawResponseData = await resRaw.json();
      }
    } catch (e: any) {
      if (e?.status === 401) return;
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
      const res = await apiFetchJSON<{ groups: string[] }>('/api/mihomo/groups');
      availableMihomoGroups = res?.groups || [];
    } catch (e) {
      availableMihomoGroups = [];
    }
  }

  async function loadSubscriptions(signal?: AbortSignal) {
    const reqSignal = signal instanceof AbortSignal ? signal : undefined;
    if (subscriptions.length === 0 && Object.keys(proxies).length === 0) {
      loading = true;
    }
    try {
      loadAvailableMihomoGroups();
      subscriptions = await apiFetchJSON<Subscription[]>('/api/proxy-providers', {
        signal: reqSignal
      });
    } catch (e: any) {
      if (e?.name !== 'AbortError' && e?.status !== 401) {
        showToast('error', $t('subscr.load_error'));
      }
    } finally {
      loading = false;
    }
  }

  async function refreshSubscription(id: string) {
    const sub = subscriptions.find((s) => s.id === id);
    if (!sub) return;

    refreshLoading[id] = true;
    try {
      const tasks: Promise<
        | { kernel: 'xray' | 'mihomo'; status: 'fulfilled'; value?: any }
        | { kernel: 'xray' | 'mihomo'; status: 'rejected'; reason: any }
      >[] = [];

      if (sub.enable_xray) {
        tasks.push(
          (async () => {
            const res = await apiFetch(`/api/subscriptions/refresh?id=${id}`, {
              method: 'POST'
            });
            if (res.ok) {
              return { kernel: 'xray' as const, status: 'fulfilled' as const };
            } else {
              const text = await res.text();
              const parsedErr = parseValidationError(text, $currentLang === 'ru' ? 'ru' : 'en');
              throw { kernel: 'xray', reason: parsedErr || $t('app.error') };
            }
          })()
        );
      }

      const providerName = sub.mihomo_provider?.name;
      if (sub.enable_mihomo && providerName) {
        tasks.push(
          (async () => {
            const res = await apiFetch(`/api/proxy-providers/${providerName}/refresh`, {
              method: 'PUT'
            });
            if (res.ok) {
              return { kernel: 'mihomo' as const, status: 'fulfilled' as const };
            } else {
              const text = await res.text();
              const parsedErr = parseValidationError(text, $currentLang === 'ru' ? 'ru' : 'en');
              throw { kernel: 'mihomo', reason: parsedErr || $t('app.error') };
            }
          })()
        );
      }

      if (tasks.length === 0) {
        if (sub.enable_mihomo && !sub.mihomo_provider?.name) {
          showToast(
            'error',
            $t('subscr.refresh.mihomo_failed').replace('{message}', $t('app.unavailable'))
          );
        }
        refreshLoading[id] = false;
        return;
      }

      const results = await Promise.allSettled(tasks);

      for (const res of results) {
        if (res.status === 'fulfilled') {
          const val = res.value;
          if (val.kernel === 'xray') {
            showToast('success', $t('subscr.refresh.xray_started'));
          } else {
            showToast('success', $t('subscr.refresh.mihomo_started'));
          }
        } else {
          const err = res.reason;
          if (err && err.kernel === 'xray') {
            showToast('error', $t('subscr.refresh.xray_failed').replace('{message}', err.reason));
          } else if (err && err.kernel === 'mihomo') {
            showToast('error', $t('subscr.refresh.mihomo_failed').replace('{message}', err.reason));
          } else {
            showToast('error', $t('app.error'));
          }
        }
      }

      await loadSubscriptions();
      if (expandedSubs[id]) {
        await loadNodesBySource(id);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('app.error'));
    } finally {
      refreshLoading[id] = false;
    }
  }

  async function refreshAll() {
    loading = true;
    try {
      const res = await apiFetch('/api/subscriptions/refresh-all', {
        method: 'POST'
      });
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadSubscriptions();
        for (const id of Object.keys(expandedSubs)) {
          if (expandedSubs[id]) {
            await loadNodesBySource(id);
          }
        }
      } else {
        showToast('error', $t('app.error'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('app.error'));
    } finally {
      loading = false;
    }
  }

  async function saveSubscription() {
    if (!formURL.trim()) {
      showToast('error', $t('subscr.fill_url'));
      return;
    }

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
      const url = editingSub
        ? `/api/subscriptions/update?id=${encodeURIComponent(editingSub.id)}`
        : '/api/subscriptions/add';
      const res = await apiFetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
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
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('app.error'));
    }
  }

  async function deleteSubscription(id: string) {
    const sub = subscriptions.find((s) => s.id === id);
    if (!sub) return;
    const subName = sub.profile_title || sub.name || id;

    if (
      !(await showConfirm({
        title: $t('subscr.delete_title'),
        objectName: subName,
        consequence: $t('subscr.delete_consequence'),
        variant: 'danger',
        confirmLabel: $t('app.delete')
      }))
    )
      return;

    try {
      const res = await apiFetch(`/api/subscriptions/delete?id=${id}`, {
        method: 'POST'
      });
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadSubscriptions();
      } else {
        showToast('error', $t('app.error'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
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

  function handleClickOutside(e: MouseEvent) {
    if (activeDropdownId) {
      const target = e.target as HTMLElement;
      if (!target.closest('.dropdown-container')) {
        activeDropdownId = null;
      }
    }
  }

  function getNodeSource(sub: Subscription): 'mihomo' | 'xray' {
    if (sub.enable_mihomo && !sub.enable_xray) {
      return 'mihomo';
    }
    if (sub.enable_xray && !sub.enable_mihomo) {
      return 'xray';
    }
    if (sub.enable_mihomo && sub.enable_xray) {
      const active = $capabilities?.active_kernel;
      return active === 'mihomo' ? 'mihomo' : 'xray';
    }
    return 'xray';
  }

  async function loadNodes(subId: string) {
    subNodesLoading[subId] = true;
    try {
      const res = await apiFetch(`/api/subscriptions/nodes?id=${subId}`);
      if (res.ok) {
        subNodes[subId] = await res.json();
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      // Ignore
    } finally {
      subNodesLoading[subId] = false;
    }
  }

  async function loadMihomoNodes(subId: string) {
    const sub = subscriptions.find((s) => s.id === subId);
    if (!sub || !sub.mihomo_provider?.name) {
      subNodesError[subId] = true;
      return;
    }

    subNodesLoading[subId] = true;
    subNodesError[subId] = false;
    try {
      const res = await apiFetch(`/api/proxy-providers/${sub.mihomo_provider.name}/nodes`);
      if (res.ok) {
        const data: {
          tag: string;
          name: string;
          alive: boolean;
          tested: boolean;
          delay_ms: number;
        }[] = await res.json();
        subNodes[subId] = data.map((n) => ({
          tag: n.tag,
          name: n.name,
          active: false,
          is_new: false
        }));
        if (!subHealth[subId]) subHealth[subId] = {};
        data.forEach((n) => {
          subHealth[subId][n.tag] = {
            alive: n.alive,
            delay: n.tested ? n.delay_ms : undefined,
            tested: n.tested
          };
        });
      } else {
        subNodesError[subId] = true;
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      subNodesError[subId] = true;
    } finally {
      subNodesLoading[subId] = false;
    }
  }

  // Загружает список нод из источника, соответствующего активному ядру
  // подписки (Clash API для mihomo, распарсенная подписка для xray).
  async function loadNodesBySource(subId: string) {
    const sub = subscriptions.find((s) => s.id === subId);
    if (!sub) return;
    if (getNodeSource(sub) === 'mihomo') {
      await loadMihomoNodes(subId);
    } else {
      await loadNodes(subId);
    }
  }

  async function toggleExpand(subId: string) {
    expandedSubs[subId] = !expandedSubs[subId];
    if (expandedSubs[subId]) {
      await loadNodesBySource(subId);
    }
  }

  async function checkMihomoNodeHealth(subId: string, providerName: string, nodeTag: string) {
    if (!checkingNodes[subId]) checkingNodes[subId] = {};
    checkingNodes[subId][nodeTag] = true;

    function setNodeHealthFailed() {
      if (!subHealth[subId]) subHealth[subId] = {};
      subHealth[subId][nodeTag] = {
        alive: false,
        delay: 0,
        tested: true
      };
    }

    try {
      const targetURL = `/api/mihomo/proxy/proxies/${encodeURIComponent(nodeTag)}/delay?url=http://www.gstatic.com/generate_204&timeout=5000`;
      const res = await apiFetch(targetURL, {
        method: 'GET'
      });
      if (res.ok) {
        const health = await res.json();
        if (!subHealth[subId]) subHealth[subId] = {};
        subHealth[subId][nodeTag] = {
          alive: health.delay > 0,
          delay: health.delay,
          tested: true
        };
      } else {
        if (res.status === 404) {
          // Fallback: если прокси не подключен к группе, запускаем проверку здоровья всего провайдера
          const hcRes = await apiFetch(
            `/api/mihomo/proxy/providers/proxies/${encodeURIComponent(providerName)}/healthcheck`,
            {
              method: 'GET'
            }
          );
          if (hcRes.ok || hcRes.status === 204) {
            // Даем Mihomo время на выполнение пинга
            await new Promise((resolve) => setTimeout(resolve, 800));
            // Загружаем ноды заново, чтобы получить обновленные задержки
            const nodesRes = await apiFetch(
              `/api/proxy-providers/${encodeURIComponent(providerName)}/nodes`
            );
            if (nodesRes.ok) {
              const nodesData = await nodesRes.json();
              if (Array.isArray(nodesData)) {
                if (!subHealth[subId]) subHealth[subId] = {};
                nodesData.forEach((n: any) => {
                  subHealth[subId][n.tag] = {
                    alive: n.alive,
                    delay: n.tested ? n.delay_ms : undefined,
                    tested: n.tested
                  };
                });
              } else {
                setNodeHealthFailed();
              }
            } else {
              setNodeHealthFailed();
            }
          } else {
            setNodeHealthFailed();
          }
        } else {
          setNodeHealthFailed();
        }
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      setNodeHealthFailed();
    } finally {
      checkingNodes[subId][nodeTag] = false;
    }
  }

  async function checkNodeHealth(subId: string, nodeTag: string) {
    const sub = subscriptions.find((s) => s.id === subId);
    if (!sub) return;

    const source = getNodeSource(sub);
    if (source === 'mihomo' && sub.mihomo_provider?.name) {
      await checkMihomoNodeHealth(subId, sub.mihomo_provider.name, nodeTag);
      return;
    }

    if (!checkingNodes[subId]) checkingNodes[subId] = {};
    checkingNodes[subId][nodeTag] = true;
    try {
      const res = await apiFetch(
        `/api/subscriptions/health?id=${subId}&tag=${encodeURIComponent(nodeTag)}`
      );
      if (res.ok) {
        const health = await res.json();
        if (!subHealth[subId]) subHealth[subId] = {};
        subHealth[subId][nodeTag] = health;
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      // Ignore
    } finally {
      checkingNodes[subId][nodeTag] = false;
    }
  }

  async function setActiveNode(subId: string, nodeTag: string) {
    try {
      const res = await apiFetch(
        `/api/subscriptions/active?id=${subId}&tag=${encodeURIComponent(nodeTag)}`,
        {
          method: 'POST'
        }
      );
      if (res.ok) {
        showToast('success', $t('app.success'));
        await loadNodesBySource(subId);
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
      loadNodesBySource(subId).then(() => {
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

  function getGroupProviderName(group: ProxyGroup): string | null {
    if (!group || !Array.isArray(group.all) || group.all.length === 0) return null;
    const counts = new Map<string, number>();
    for (const nodeName of group.all) {
      const prov = proxies[nodeName]?.provider;
      if (prov && typeof prov === 'string' && prov.trim() !== '') {
        counts.set(prov, (counts.get(prov) || 0) + 1);
      }
    }
    if (counts.size === 0) return null;
    let topProv: string | null = null;
    let topCount = 0;
    for (const [prov, count] of counts.entries()) {
      if (count > topCount) {
        topCount = count;
        topProv = prov;
      }
    }
    return topProv;
  }

  function getDisplayChain(groupName: string): {
    items: ChainItem[];
    truncated: boolean;
    fullText: string;
  } {
    const fullChain = getSelectionChain(groupName);
    const fullText = fullChain.map((item) => item.name).join(' › ');
    if (fullChain.length <= 2) {
      return {
        items: fullChain,
        truncated: false,
        fullText
      };
    }
    return {
      items: [fullChain[0], fullChain[fullChain.length - 1]],
      truncated: true,
      fullText
    };
  }

  onMount(() => {
    const hash = window.location.hash;
    if (hash.includes('tab=providers') || window.location.search.includes('tab=providers')) {
      activeTab = 'providers';
    }

    poller = usePoller(async (signal) => {
      await fetchProxies(signal);
      await loadSubscriptions(signal);
      checkAutoExpand();
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

    return () => {
      poller?.stop();
      batchTester.cancel();
      if (popoverHoverTimeout) clearTimeout(popoverHoverTimeout);
      if (loadTimeoutId) clearTimeout(loadTimeoutId);
      pendingTimeouts.forEach(clearTimeout);
      window.removeEventListener('hashchange', handleHashChange);
      window.removeEventListener('click', handleClickOutside);
    };
  });

  onDestroy(() => {
    poller?.stop();
    batchTester.cancel();
    if (popoverHoverTimeout) clearTimeout(popoverHoverTimeout);
    if (loadTimeoutId) clearTimeout(loadTimeoutId);
    pendingTimeouts.forEach(clearTimeout);
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy_subs')} <span class="crumb-sep">›</span>
        {$t('proxies.title')}
      </div>
      <h1>{$t('proxies.title')}</h1>
      <p class="sub">{$t('proxies.subtitle')}</p>
    </div>
    {#if activeTab === 'groups'}
      <div class="ph-actions">
        <input
          class="group-search"
          type="search"
          bind:value={filterQuery}
          oninput={handleSearchInput}
          placeholder={$t('proxies.filter_placeholder')}
          aria-label={$t('proxies.filter_placeholder')}
        />
        <div class="view-toggle" role="group" aria-label={$t('proxies.view_mode_label')}>
          <button
            type="button"
            class="view-toggle-btn"
            data-view="grid"
            aria-pressed={viewMode === 'grid'}
            onclick={() => setViewMode('grid')}
          >
            <ViewGrid size={14} />
            <span>{$t('proxies.view_mode_grid')}</span>
          </button>
          <button
            type="button"
            class="view-toggle-btn"
            data-view="list"
            aria-pressed={viewMode === 'list'}
            onclick={() => setViewMode('list')}
          >
            <ViewList size={14} />
            <span>{$t('proxies.view_mode_list')}</span>
          </button>
        </div>
        <button class="btn btn-secondary" onclick={expandAll} title={$t('proxies.expand_all')}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"
          >
            <polyline points="6 9 12 15 18 9" />
            <polyline points="6 4 12 10 18 4" />
          </svg>
          {$t('proxies.expand_all')}
        </button>
        <button class="btn btn-secondary" onclick={collapseAll} title={$t('proxies.collapse_all')}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"
          >
            <polyline points="18 15 12 9 6 15" />
            <polyline points="18 20 12 14 6 20" />
          </svg>
          {$t('proxies.collapse_all')}
        </button>
        <button class="btn btn-secondary" onclick={() => fetchProxies()} disabled={loading}>
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
        <PingTargetQuickMenu />
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
          {$t('subscr.refresh_all')}
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
          {$t('subscr.add')}
        </button>
      </div>
    {/if}
  </div>

  <!-- Вкладки (Tabs) -->
  <div class="tabs-container">
    <button
      class="tab-btn"
      class:active={activeTab === 'groups'}
      onclick={() => (activeTab = 'groups')}
    >
      {$t('proxies.tab_groups')}
    </button>
    <button
      class="tab-btn"
      class:active={activeTab === 'providers'}
      onclick={() => (activeTab = 'providers')}
    >
      {$t('proxies.tab_providers')}
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
        <ObservatoryPanel
          stats={observatoryStats}
          activeFilter={observatoryFilter}
          onFilterChange={(f) => (observatoryFilter = f)}
        />
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
          description={$t('proxies.no_proxies_desc')}
          icon={WarningIcon}
          ctaText={$t('app.refresh')}
          oncta={fetchProxies}
        />
      {:else if searchDebouncedQuery.trim() !== '' && groupSections.core.length + groupSections.service.length + groupSections.system.length === 0}
        <div class="search-empty-state">
          {$t('proxies.search_no_matches')}
        </div>
      {:else}
        {#snippet groupCard(group: ProxyGroup, role: GroupRole)}
          {@const isCollapsed = collapsedGroups.has(group.name)}
          {@const nodes = getFilteredNodes(group, searchDebouncedQuery)}
          {@const isMini = role === 'system'}
          {@const nowUpper = (group.now || '').toUpperCase()}
          {@const isPinned = pinnedCoreGroups.includes(group.name)}
          {@const isAutoCore = role === 'core' && !isPinned}
          {@const groupTypeKey = group.type.toLowerCase()}
          {@const providerName = getGroupProviderName(group)}
          {@const displayChain = getDisplayChain(group.name)}
          <div
            class="group-card"
            class:expanded={!isCollapsed}
            class:gc-mini={isMini}
            class:out-direct={isMini && nowUpper === 'DIRECT'}
            class:out-reject={isMini && (nowUpper === 'REJECT' || nowUpper === 'REJECT-DROP')}
            class:out-pass={isMini && nowUpper === 'PASS'}
            data-group={group.name}
            data-role={role}
            bind:this={groupCardEls[group.name]}
          >
            <div
              class="gc-head"
              class:collapsible={!isMini}
              role="button"
              tabindex={isMini ? -1 : 0}
              aria-expanded={isMini ? undefined : !isCollapsed}
              onclick={isMini ? undefined : (e) => handleHeadClick(e, group.name)}
              onkeydown={isMini ? undefined : (e) => handleHeadKeydown(e, group.name)}
            >
              <div class="gc-head-row1">
                {#if group.icon}
                  <span class="group-icon-wrap" aria-hidden="true">
                    <img
                      src={group.icon}
                      alt=""
                      loading="lazy"
                      referrerpolicy="no-referrer"
                      class="brand-icon"
                      onerror={(e) => {
                        const target = e.currentTarget as HTMLElement;
                        if (target) target.style.display = 'none';
                      }}
                    />
                  </span>
                {/if}
                <span class="name">{group.name}</span>
                <span class="type-badge">
                  <span class="type-badge-icon" aria-hidden="true">
                    {#if groupTypeKey === 'selector'}
                      <svg
                        width="11"
                        height="11"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"><polyline points="20 6 9 17 4 12" /></svg
                      >
                    {:else if groupTypeKey === 'urltest' || groupTypeKey === 'fallback'}
                      <svg
                        width="11"
                        height="11"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                        ><path d="M21 12a9 9 0 1 1-3-6.7" /><path d="M21 3v6h-6" /></svg
                      >
                    {:else if groupTypeKey === 'loadbalance'}
                      <svg
                        width="11"
                        height="11"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"><path d="M7 7h11l-3-3" /><path d="M17 17H6l3 3" /></svg
                      >
                    {:else if groupTypeKey === 'relay'}
                      <svg
                        width="11"
                        height="11"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                        ><circle cx="8" cy="12" r="3" /><circle cx="16" cy="12" r="3" /><path
                          d="M10.5 12h3"
                        /></svg
                      >
                    {/if}
                  </span>
                  {getGroupTypeLabel(group.type)}
                </span>

                <div class="gc-head-actions">
                  {#if role === 'core' || role === 'service'}
                    <button
                      type="button"
                      class="gc-pin-btn"
                      data-stop-head-click
                      aria-pressed={isPinned}
                      title={isAutoCore
                        ? $t('proxies.pin_auto_core')
                        : isPinned
                          ? $t('proxies.unpin_from_core')
                          : $t('proxies.pin_to_core')}
                      aria-label={isAutoCore
                        ? $t('proxies.pin_auto_core')
                        : isPinned
                          ? $t('proxies.unpin_from_core')
                          : $t('proxies.pin_to_core')}
                      disabled={isAutoCore}
                      onclick={(e) => {
                        e.stopPropagation();
                        pinnedCoreGroups = togglePinnedCoreGroup(group.name);
                      }}
                    >
                      <Pin size={13} />
                    </button>
                  {/if}

                  {#if isMini}
                    <span class="gc-static-out" title={$t('proxies.static_output')}
                      >{group.now}</span
                    >
                  {:else if group.now}
                    {@const latencyClass = getLatencyClass(group.now)}
                    {@const latencyText = getLatencyText(group.now)}
                    <button
                      type="button"
                      class="gc-lat-box {latencyClass}"
                      data-stop-head-click
                      title={getLatencyTitle(group.now)}
                      onmouseenter={(e) => handleBadgeMouseEnter(e, group.now)}
                      onmouseleave={handleBadgeMouseLeave}
                      onclick={(e) => {
                        e.stopPropagation();
                        handleBadgeClick(e, group.now);
                      }}
                      onkeydown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          e.stopPropagation();
                          handleBadgeClick(e, group.now);
                        }
                      }}
                    >
                      {latencyText}
                    </button>
                    <button
                      type="button"
                      class="gc-ping-btn"
                      data-stop-head-click
                      title={testingGroupName && testingGroupName !== group.name
                        ? $t('proxies.test_group_busy')
                        : $t('proxies.test_group')}
                      aria-label={$t('proxies.test_group')}
                      disabled={testingGroupName === group.name || batchProgress?.running}
                      onclick={(e) => {
                        e.stopPropagation();
                        testGroupLatency(group);
                      }}
                    >
                      {#if testingGroupName === group.name}
                        <span
                          class="spinner"
                          style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
                        ></span>
                      {:else}
                        <svg
                          width="12"
                          height="12"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <polygon
                            points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"
                            fill="currentColor"
                          />
                        </svg>
                      {/if}
                    </button>
                  {/if}

                  {#if viewMode === 'list' && !isMini}
                    <HealthBar
                      compact={true}
                      stats={computeGroupHealthStats(nodes, resolveNodeSnapshot)}
                    />
                  {/if}

                  {#if !isMini}
                    <button
                      type="button"
                      class="gc-chevron-btn"
                      data-stop-head-click
                      aria-expanded={!isCollapsed}
                      aria-label={$t('proxies.toggle_group_nodes')}
                      onclick={(e) => {
                        e.stopPropagation();
                        toggleCollapse(group.name);
                      }}
                    >
                      <span class="chevron-wrap" class:rotated={!isCollapsed} aria-hidden="true">
                        <ChevronDown
                          size={14}
                          color={isCollapsed ? 'var(--fg-dim)' : 'var(--accent)'}
                        />
                      </span>
                    </button>
                  {/if}
                </div>
              </div>

              {#if !isMini}
                <div
                  class="gc-head-row2"
                  title={displayChain.fullText ? displayChain.fullText : undefined}
                >
                  <span class="gc-count-text"
                    >{group.all.length}
                    {$tp('proxies.nodes', group.all.length)}</span
                  >
                  {#if providerName}
                    <span class="gc-provider-badge" title={$t('proxies.from_provider')}>
                      <svg
                        width="10"
                        height="10"
                        viewBox="0 0 24 24"
                        fill="currentColor"
                        aria-hidden="true"
                      >
                        <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
                      </svg>
                      {providerName}
                    </span>
                  {/if}
                  <span class="gc-separator">·</span>
                  <span class="gc-active-label">{$t('proxies.active')}:</span>

                  {#snippet chainPill(item: ChainItem)}
                    {@const itemFlag = !item.isGroup ? getCountryFlag(item.name) : null}
                    {@const itemLatencyText = getLatencyText(item.name)}
                    {@const itemLatencyClass = getLatencyClass(item.name)}
                    {#if item.isGroup}
                      <button
                        type="button"
                        class="gc-now-pill gc-now-pill-link"
                        class:lat-ok={itemLatencyClass === 'lat ok'}
                        class:lat-mid={itemLatencyClass === 'lat mid'}
                        class:lat-bad={itemLatencyClass === 'lat bad'}
                        title={$t('proxies.goto_parent_group')}
                        data-stop-head-click
                        onclick={(e) => {
                          e.stopPropagation();
                          focusGroupCard(item.name);
                        }}
                      >
                        <div
                          class="gc-now-dot"
                          class:lat-ok={itemLatencyClass === 'lat ok'}
                          class:lat-mid={itemLatencyClass === 'lat mid'}
                          class:lat-bad={itemLatencyClass === 'lat bad'}
                        ></div>
                        {#if itemFlag}{itemFlag}
                        {/if}{item.name}
                      </button>
                    {:else}
                      <button
                        type="button"
                        class="gc-now-pill is-leaf gc-now-pill-trigger"
                        class:lat-ok={itemLatencyClass === 'lat ok'}
                        class:lat-mid={itemLatencyClass === 'lat mid'}
                        class:lat-bad={itemLatencyClass === 'lat bad'}
                        data-stop-head-click
                        aria-haspopup="listbox"
                        aria-expanded={quickSelect?.groupName === group.name}
                        title={$t('proxies.quick_select_title')}
                        onclick={(e) => {
                          e.stopPropagation();
                          openQuickSelect(e.currentTarget as HTMLElement, group.name);
                        }}
                      >
                        <div
                          class="gc-now-dot is-leaf"
                          class:lat-ok={itemLatencyClass === 'lat ok'}
                          class:lat-mid={itemLatencyClass === 'lat mid'}
                          class:lat-bad={itemLatencyClass === 'lat bad'}
                        ></div>
                        {#if itemFlag}{itemFlag}
                        {/if}{item.name}
                      </button>
                    {/if}
                  {/snippet}

                  {#if displayChain.truncated}
                    {@render chainPill(displayChain.items[0])}
                    <span class="gc-arrow">›</span>
                    <span class="gc-chain-ellipsis" title={displayChain.fullText}>…</span>
                    <span class="gc-arrow">›</span>
                    {@render chainPill(displayChain.items[1])}
                  {:else if displayChain.items.length > 0}
                    {#each displayChain.items as item, index}
                      {#if index > 0}
                        <span class="gc-arrow">›</span>
                      {/if}
                      {@render chainPill(item)}
                    {/each}
                  {:else}
                    <span style="color:var(--fg-dim)">—</span>
                  {/if}
                </div>
              {/if}
            </div>

            {#if !isMini}
              {#if viewMode === 'grid' && isCollapsed}
                <HealthBar stats={computeGroupHealthStats(nodes, resolveNodeSnapshot)} />
              {:else if !isCollapsed}
                {@const filteredNodesList = getFilteredGroupNodes(group.name, nodes)}
                {@const renderLimit = getRenderLimit(group.name)}
                {@const renderedNodes = filteredNodesList.slice(0, renderLimit)}
                <div class="gc-body">
                  <div class="gc-body-inner">
                    <div class="group-filters">
                      <button
                        type="button"
                        class="filter-chip"
                        class:active={(groupFilters[group.name] || 'all') === 'all'}
                        onclick={() => (groupFilters[group.name] = 'all')}
                      >
                        {$t('proxies.filter_all')}
                        <span class="filter-count">{nodes.length}</span>
                      </button>
                      <button
                        type="button"
                        class="filter-chip"
                        class:active={groupFilters[group.name] === 'working'}
                        onclick={() => (groupFilters[group.name] = 'working')}
                      >
                        {$t('proxies.filter_working')}
                      </button>
                      <button
                        type="button"
                        class="filter-chip"
                        class:active={groupFilters[group.name] === 'timeouts'}
                        onclick={() => (groupFilters[group.name] = 'timeouts')}
                      >
                        {$t('proxies.filter_timeouts')}
                      </button>
                      <button
                        type="button"
                        class="filter-chip"
                        class:active={groupFilters[group.name] === 'latency'}
                        onclick={() => (groupFilters[group.name] = 'latency')}
                      >
                        {$t('proxies.filter_by_latency')}
                      </button>

                      <div class="group-actions-spacer"></div>

                      <button
                        type="button"
                        class="filter-chip group-test-btn"
                        onclick={() => testGroupLatency(group)}
                        disabled={testingGroupName === group.name || batchProgress?.running}
                        title={$t('proxies.test_group')}
                      >
                        <svg
                          width="11"
                          height="11"
                          viewBox="0 0 24 24"
                          fill="currentColor"
                          style="margin-right: 4px;"
                        >
                          <polygon points="5 3 19 12 5 21 5 3"></polygon>
                        </svg>
                        {$t('proxies.test_group')}
                      </button>
                    </div>

                    <div class="proxy-grid">
                      {#each renderedNodes as proxyName}
                        {@const proxy = proxies[proxyName]}
                        {@const isAlive = isProxyAlive(proxy)}
                        {@const isDirectOrReject = ['DIRECT', 'REJECT'].includes(
                          proxyName.toUpperCase()
                        )}
                        {@const isActive = group.now === proxyName}
                        {@const flag = getCountryFlag(proxyName)}
                        {@const healthClass = getLatencyClass(proxyName)}
                        {@const healthText = getLatencyText(proxyName)}
                        <div class="proxy-card" class:now={isActive}>
                          <div
                            class="proxy-select-btn"
                            role="button"
                            tabindex={group.type === 'Selector' ? 0 : -1}
                            aria-disabled={group.type !== 'Selector'}
                            title={group.type !== 'Selector'
                              ? $t('proxies.managed_automatically')
                              : undefined}
                            onclick={() =>
                              group.type === 'Selector' && selectProxy(group.name, proxyName)}
                            onkeydown={(e) => {
                              if (
                                group.type === 'Selector' &&
                                (e.key === 'Enter' || e.key === ' ')
                              ) {
                                e.preventDefault();
                                selectProxy(group.name, proxyName);
                              }
                            }}
                          >
                            <div class="p-header">
                              <span class="p-name" title={proxyName}>
                                {#if flag}
                                  <span class="flag-icon" aria-hidden="true">{flag}</span>
                                {/if}
                                {proxyName}
                              </span>
                              <span class="p-type">{getProxyTypeLabel(proxy)}</span>
                            </div>
                          </div>

                          <div class="p-footer">
                            {#if (batchProgress?.running && batchProgress?.currentNode === proxyName) || testingProxy === proxyName}
                              <span class="lat dim">
                                <span class="lat-spinner"></span>
                              </span>
                            {:else}
                              <button
                                type="button"
                                class="lat {healthClass}"
                                title={getLatencyTitle(proxyName)}
                                onmouseenter={(e) => handleBadgeMouseEnter(e, proxyName)}
                                onmouseleave={handleBadgeMouseLeave}
                                onclick={(e) => handleBadgeClick(e, proxyName)}
                                onkeydown={(e) => {
                                  if (e.key === 'Enter' || e.key === ' ') {
                                    e.preventDefault();
                                    handleBadgeClick(e as any, proxyName);
                                  }
                                }}
                              >
                                {healthText}
                              </button>
                            {/if}

                            <div class="p-actions-wrap">
                              {#if !['DIRECT', 'REJECT'].includes(proxyName.toUpperCase()) && !['Direct', 'Reject', 'Compatible'].includes(proxy?.type || '')}
                                <button
                                  type="button"
                                  class="btn-latency-test"
                                  onclick={() => testProxyLatency(proxyName)}
                                  disabled={testingProxy === proxyName}
                                  title={$t('proxies.test_single')}
                                >
                                  {#if testingProxy === proxyName}
                                    <span
                                      class="spinner"
                                      style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
                                    ></span>
                                  {:else}
                                    <svg
                                      width="12"
                                      height="12"
                                      viewBox="0 0 24 24"
                                      fill="none"
                                      stroke="currentColor"
                                      stroke-width="2"
                                      style="opacity: 0.6;"
                                      ><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" /></svg
                                    >
                                  {/if}
                                </button>
                              {/if}

                              {#if group.type === 'Selector'}
                                <span class="selector-dot" class:active={isActive}
                                  >{isActive ? '●' : '○'}</span
                                >
                              {/if}
                            </div>
                          </div>
                        </div>
                      {/each}
                    </div>

                    {#if filteredNodesList.length > renderLimit}
                      {@const remaining = filteredNodesList.length - renderLimit}
                      <div class="proxy-grid-footer">
                        <button
                          type="button"
                          class="proxy-grid-more"
                          onclick={() => increaseRenderLimit(group.name, filteredNodesList.length)}
                        >
                          {$t('proxies.show_more_nodes')}
                          {remaining}
                          {$tp('proxies.nodes', remaining)}
                        </button>
                        <span class="rendered-nodes-hint">
                          {$t('proxies.rendered_nodes_hint', {
                            shown: renderLimit,
                            total: filteredNodesList.length
                          })}
                        </span>
                      </div>
                    {/if}
                  </div>
                </div>
              {/if}
            {/if}
          </div>
        {/snippet}

        <section class="proxy-section proxy-section-core">
          <h2 class="proxy-section-title">
            {$t('proxies.section_core')}
            <span class="proxy-section-count">
              {groupSections.core.length}
              {$tp('proxies.groups', groupSections.core.length)} ({coreNodesCount}
              {$tp('proxies.nodes', coreNodesCount)})
            </span>
          </h2>
          <div class="group-grid core-grid" class:group-list={viewMode === 'list'}>
            {#each groupSections.core as group (group.name)}
              {@render groupCard(group, 'core')}
            {/each}
          </div>
        </section>

        {#if groupSections.service.length > 0}
          <section class="proxy-section proxy-section-service">
            <h2 class="proxy-section-title">
              {$t('proxies.section_service')}
              <span class="proxy-section-count">
                {groupSections.service.length}
                {$tp('proxies.groups', groupSections.service.length)} ({serviceNodesCount}
                {$tp('proxies.nodes', serviceNodesCount)})
              </span>
            </h2>
            <div class="group-grid" class:group-list={viewMode === 'list'}>
              {#each groupSections.service as group (group.name)}
                {@render groupCard(group, 'service')}
              {/each}
            </div>
          </section>
        {/if}

        {#if groupSections.system.length > 0}
          <section class="proxy-section proxy-section-system">
            <h2 class="proxy-section-title">
              {$t('proxies.section_system')}
              <span class="proxy-section-count">
                {groupSections.system.length}
                {$tp('proxies.groups', groupSections.system.length)} ({systemNodesCount}
                {$tp('proxies.nodes', systemNodesCount)})
              </span>
            </h2>
            <div class="group-grid" class:group-list={viewMode === 'list'}>
              {#each groupSections.system as group (group.name)}
                {@render groupCard(group, 'system')}
              {/each}
            </div>
          </section>
        {/if}
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
          <p style="color: var(--fg-secondary); margin: 0;">
            {$t('subscr.empty')}
          </p>
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
            {$t('subscr.add_first')}
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
          {subNodesError}
          {getNodeSource}
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
          onRetryNodes={loadMihomoNodes}
        />
      {/if}
    </div>
  {/if}
</div>

<SubscriptionFormModal
  isOpen={showAddModal}
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

<NodeImporter
  {diagnosticSub}
  {diagnosticTab}
  {diagnosticLoading}
  {parseReportData}
  {rawResponseData}
  onClose={closeDiagnosticModal}
  onTabChange={(tab) => (diagnosticTab = tab)}
/>

{#if batchProgress?.running}
  <FloatingProgress progress={batchProgress} onCancel={() => batchTester.cancel()} />
{/if}

{#if activePopover}
  <LatencyHistoryPopover
    proxyName={activePopover.name}
    history={activePopover.history}
    anchorEl={activePopover.el}
    onClose={() => (activePopover = null)}
  />
{/if}

{#if quickSelect}
  {@const selectedGrp = groups.find((g) => g.name === quickSelect?.groupName)}
  {#if selectedGrp}
    <QuickSelectPopover
      groupName={selectedGrp.name}
      groupType={selectedGrp.type}
      nodes={buildQuickSelectNodes(selectedGrp)}
      currentNode={selectedGrp.now}
      anchorEl={quickSelect.anchor}
      onSelect={handleQuickSelect}
      onClose={() => (quickSelect = null)}
    />
  {/if}
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
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 420px), 1fr));
    gap: var(--grid-gap, 16px);
    margin-bottom: 30px;
    align-items: start;
  }
  .proxy-section {
    margin-bottom: 8px;
  }
  .proxy-section-title {
    font-size: 13px;
    font-weight: 700;
    color: var(--fg-secondary);
    margin: 0 0 10px;
  }
  .proxy-section-count {
    margin-left: 8px;
    font-weight: 500;
    color: var(--fg-faint);
    font-size: 12px;
  }
  .gc-provider-badge {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 99px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg-dim);
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gc-chain-ellipsis {
    color: var(--fg-faint);
    cursor: help;
    letter-spacing: 1px;
  }
  .search-empty-state {
    padding: 32px 16px;
    text-align: center;
    color: var(--fg-dim);
    font-size: 14px;
    background: var(--bg-card);
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
    margin-top: 8px;
  }
  .core-grid .group-card {
    border-left: 3px solid var(--accent);
  }
  .core-grid .group-card .gc-head .name {
    font-size: 17px;
  }
  .proxy-section-system .group-grid {
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 240px), 1fr));
  }
  .group-card.gc-mini .gc-head {
    padding: 8px 14px;
    min-height: 40px;
    flex-direction: row;
    align-items: center;
  }
  .group-card.gc-mini.out-direct {
    border-left: 3px solid var(--success);
  }
  .group-card.gc-mini.out-reject {
    border-left: 3px solid var(--danger);
  }
  .group-card.gc-mini.out-pass {
    border-left: 3px solid var(--fg-dim);
  }
  .gc-static-out {
    font-family: var(--font-family-mono);
    font-size: 11px;
    color: var(--fg-dim);
  }
  .gc-pin-btn {
    background: none;
    border: none;
    padding: 2px 4px;
    cursor: pointer;
    color: var(--fg-faint);
    border-radius: var(--radius-sm);
  }
  .gc-pin-btn:hover:not(:disabled) {
    color: var(--accent);
    background: var(--hover);
  }
  .gc-pin-btn[aria-pressed='true'] {
    color: var(--accent);
  }
  .gc-pin-btn:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .group-card.flash-highlight {
    animation: gc-flash 1.4s ease-out;
  }
  @keyframes gc-flash {
    0%,
    40% {
      box-shadow: 0 0 0 2px var(--accent);
    }
    100% {
      box-shadow: none;
    }
  }
  .group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg, 10px);
    overflow: hidden;
    box-shadow: var(--shadow-sm);
    transition: all 0.2s ease;
  }
  .group-card.expanded {
    grid-column: 1 / -1;
  }
  .group-card:hover {
    box-shadow:
      0 4px 20px rgba(0, 0, 0, 0.35),
      0 0 0 1px rgba(41, 194, 240, 0.15);
  }
  .group-card.skeleton-card {
    border-style: dashed;
    background: transparent;
  }
  .group-card .gc-head {
    background: linear-gradient(
      135deg,
      var(--bg-group-head-from, rgba(20, 51, 79, 0.9)),
      var(--bg-group-head-to, rgba(16, 42, 68, 0.95))
    );
    padding: 14px 18px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-bottom: 1px solid var(--border-strong);
    position: relative;
    overflow: hidden;
    width: 100%;
    text-align: left;
  }
  .group-card .gc-head::before {
    content: '';
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
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
  .group-card .gc-head.collapsible:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .gc-head-row1 {
    display: flex;
    align-items: center;
    width: 100%;
    gap: 8px;
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
  .gc-head-actions {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .type-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    padding: 0;
    color: var(--fg-dim);
    font-family: var(--font-family-sans);
    font-weight: 600;
    letter-spacing: 0;
    text-transform: none;
  }
  .type-badge-icon {
    display: inline-flex;
    opacity: 0.7;
  }
  .gc-lat-box {
    padding: 3px 10px;
    border-radius: 99px;
    font-family: var(--font-family-mono);
    font-size: 11px;
    font-weight: 800;
    background: none;
    border: none;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
  }
  .gc-lat-box:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
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
  .gc-ping-btn {
    background: none;
    border: none;
    padding: 2px 4px;
    cursor: pointer;
    color: var(--fg-faint);
    border-radius: var(--radius-sm, 4px);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s ease;
  }
  .gc-ping-btn:hover:not(:disabled) {
    color: var(--accent);
    background: var(--hover, rgba(255, 255, 255, 0.08));
  }
  .gc-ping-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .gc-ping-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
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
    text-align: left;
    font-family: inherit;
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
  .gc-now-pill-link {
    cursor: pointer;
  }
  .gc-now-pill-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .gc-now-pill-trigger {
    cursor: pointer;
  }
  .gc-now-pill-trigger:hover {
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .gc-now-pill-trigger:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  @media (max-width: 767px) {
    .core-grid {
      grid-template-columns: 1fr;
    }
    .core-grid .gc-head {
      min-height: 44px;
    }
  }

  .proxy-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 8px;
    padding: 12px;
    content-visibility: auto;
    contain-intrinsic-size: 80px;
  }

  .proxy-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 0;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    position: relative;
    transition: all var(--transition-fast);
    min-height: 84px;
  }
  .proxy-select-btn {
    display: flex;
    flex-direction: column;
    width: 100%;
    background: none;
    border: 0;
    padding: 10px 12px 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    border-radius: var(--radius-md) var(--radius-md) 0 0;
    flex: 1;
  }
  .proxy-select-btn:disabled,
  .proxy-select-btn[aria-disabled='true'] {
    cursor: default;
  }
  .proxy-select-btn:focus-visible {
    outline: 2px solid var(--accent, #29c2f0);
    outline-offset: -2px;
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
    box-shadow:
      inset 0 0 0 1px rgba(41, 194, 240, 0.08),
      0 2px 8px rgba(41, 194, 240, 0.08);
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
    padding: 4px 12px 10px;
    margin-top: auto;
  }
  .p-actions-wrap {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .btn-latency-test {
    background: transparent;
    border: none;
    padding: 4px;
    width: 24px;
    height: 24px;
    color: var(--fg-dim);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s;
    border-radius: var(--radius-sm);
    flex-shrink: 0;
  }
  .btn-latency-test:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.08);
  }
  .btn-latency-test:focus-visible {
    outline: 2px solid var(--accent, #29c2f0);
    outline-offset: 1px;
  }

  .group-filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    padding: 8px 16px 10px;
    border-bottom: 1px solid var(--border);
  }
  .filter-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px;
    border-radius: var(--radius-full, 9999px);
    font-size: 11px;
    font-weight: 500;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .filter-chip:hover {
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-primary);
  }
  .filter-chip.active {
    background: rgba(41, 194, 240, 0.12);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 600;
  }
  .filter-chip .filter-count {
    font-size: 10px;
    opacity: 0.7;
  }

  .group-actions-spacer {
    flex: 1;
  }

  .group-test-btn {
    margin-left: auto;
    color: var(--accent);
    border-color: rgba(41, 194, 240, 0.3);
  }

  .group-test-btn:hover {
    background: rgba(41, 194, 240, 0.15);
    border-color: var(--accent);
    color: var(--accent);
  }

  .group-test-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .selector-dot {
    font-size: 14px;
    color: var(--fg-dim);
    font-weight: 700;
  }
  .selector-dot.active {
    color: var(--accent);
  }

  .group-icon-wrap {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: 0 0 26px;
    border-radius: 8px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    overflow: hidden;
  }

  .group-icon-wrap .brand-icon {
    width: 18px;
    height: 18px;
    object-fit: contain;
    display: block;
  }

  .lat {
    font-family: var(--font-family-mono);
    font-size: 12px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 4px;
    white-space: nowrap;
  }
  button.lat {
    border: none;
    cursor: pointer;
    font: inherit;
    font-family: var(--font-family-mono);
    font-size: 12px;
    font-weight: 600;
    line-height: 1.2;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: opacity 0.15s ease;
  }
  button.lat:hover {
    opacity: 0.85;
  }
  button.lat:focus-visible {
    outline: 2px solid var(--accent, #29c2f0);
    outline-offset: 1px;
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

  .gc-chevron-btn {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: inherit;
    font: inherit;
  }
  .gc-chevron-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  /* View toggle (D-17) */
  .view-toggle {
    display: inline-flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .view-toggle-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    height: var(--btn-h, 32px);
    background: none;
    border: none;
    color: var(--fg-dim);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .view-toggle-btn + .view-toggle-btn {
    border-left: 1px solid var(--border);
  }
  .view-toggle-btn:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }
  .view-toggle-btn[aria-pressed='true'] {
    background: var(--accent-soft, var(--hover));
    color: var(--accent);
    font-weight: 600;
  }
  .view-toggle-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  /* Group List View (D-18, D-19) */
  .group-grid.group-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .group-grid.group-list .group-card {
    border-radius: var(--radius-sm, 6px);
  }
  .group-grid.group-list .group-card.expanded {
    grid-column: auto;
  }
  .group-list .gc-head {
    flex-direction: row;
    align-items: center;
    height: 40px;
    padding: 0 12px;
    gap: 10px;
  }
  .group-list .gc-head-row1 {
    flex: 0 1 auto;
    width: auto;
    gap: 8px;
  }
  .group-list .gc-head-row2 {
    width: auto;
    margin-top: 0;
    flex: 1 1 auto;
    min-width: 0;
    justify-content: flex-end;
    gap: 6px;
  }
  .group-list .gc-count-text,
  .group-list .gc-active-label,
  .group-list .gc-provider-badge {
    display: none;
  }
  .group-list .gc-head .name {
    font-size: 13px;
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .group-list .health-bar {
    width: 64px;
    margin: 0;
    flex: 0 0 64px;
  }
  .group-list .proxy-grid {
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 200px), 1fr));
    gap: 6px;
    padding: 8px 12px;
  }

  .group-list .gc-body {
    display: grid;
    grid-template-rows: 0fr;
    transition: grid-template-rows 0.18s ease;
  }
  .group-list .group-card.expanded .gc-body {
    grid-template-rows: 1fr;
  }
  .gc-body-inner {
    overflow: hidden;
    min-height: 0;
  }

  /* Chunked loading (D-20) */
  .proxy-grid-footer {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    margin: 0 16px 12px;
  }
  .proxy-grid-more {
    width: calc(100% - 32px);
    height: 32px;
    background: var(--bg-surface, rgba(255, 255, 255, 0.03));
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm, 4px);
    color: var(--fg-dim);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .proxy-grid-more:hover {
    color: var(--accent);
    border-color: var(--accent);
  }
  .rendered-nodes-hint {
    font-size: 11px;
    color: var(--fg-muted, var(--fg-dim));
  }

  @media (max-width: 767px) {
    .group-list .gc-head {
      height: 44px;
    }
    .group-list .gc-chevron-btn,
    .group-list .gc-ping-btn,
    .group-list .gc-pin-btn {
      min-width: 44px;
      min-height: 44px;
      justify-content: center;
    }
  }

  @media (max-width: 480px) {
    .view-toggle-btn {
      min-width: 44px;
      justify-content: center;
      padding: 0 6px;
    }
    .view-toggle-btn span {
      display: none;
    }
  }

  /* Mobile: proxy cards stack, observatory stats handled globally at 768px */
  @media (max-width: 640px) {
    .ph-actions {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      width: 100%;
      margin-top: 10px;
    }

    .ph-actions .group-search {
      order: -1;
      flex: 1 1 100%;
      width: 100%;
      min-width: 100%;
      font-size: 13px;
      padding: 8px 12px;
    }

    .ph-actions .btn {
      flex: 1 1 calc(50% - 4px);
      justify-content: center;
      padding: 8px 10px;
      font-size: 12px;
      min-height: 40px;
      white-space: nowrap;
      text-overflow: ellipsis;
      overflow: hidden;
    }

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
      padding: 0;
      min-height: 70px;
    }
    .proxy-select-btn {
      padding: 8px 10px 0;
    }
    .proxy-card .p-footer {
      padding: 2px 10px 8px;
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
