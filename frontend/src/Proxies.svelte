<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { t, currentLang } from './i18n';
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
  let testingLatency = $state(false);
  let testingProxy = $state('');
  let loadTimeoutId: ReturnType<typeof setTimeout> | null = null;
  let collapsedGroups = $state(new Set<string>());
  let filterQuery = $state('');
  let seenGroups = $state(new Set<string>());
  const pendingTimeouts: ReturnType<typeof setTimeout>[] = [];

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
          const groupMatch = g.name
            .toLowerCase()
            .includes(searchDebouncedQuery.trim().toLowerCase());
          const nodesMatch = g.all.some((node) =>
            node.toLowerCase().includes(searchDebouncedQuery.trim().toLowerCase())
          );
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

  function getEffectiveProxy(proxyName: string): Proxy | undefined {
    let currentName = proxyName;
    const visited = new Set<string>();
    while (currentName && !visited.has(currentName)) {
      visited.add(currentName);
      const p = proxies[currentName];
      if (!p) break;
      if (
        ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay'].includes(p.type || '') &&
        p.now
      ) {
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
    for (const g of groups) {
      if (!seenGroups.has(g.name)) {
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

  let groupFilters = $state<Record<string, 'all' | 'working' | 'timeouts' | 'latency'>>({});

  interface GroupHealthStats {
    fast: number;
    mid: number;
    bad: number;
    unchecked: number;
    total: number;
    fastPct: number;
    midPct: number;
    badPct: number;
    uncheckedPct: number;
    tooltip: string;
  }

  function getGroupHealthStats(nodeNames: string[]): GroupHealthStats {
    let fast = 0;
    let mid = 0;
    let bad = 0;
    let unchecked = 0;

    for (const name of nodeNames) {
      const eff = getEffectiveProxy(name);
      const p = eff || proxies[name];
      if (!p) {
        unchecked++;
        continue;
      }
      if (
        ['DIRECT', 'REJECT'].includes((p.name || name).toUpperCase()) ||
        ['Direct', 'Reject', 'Compatible'].includes(p.type || '')
      ) {
        unchecked++;
        continue;
      }
      const delay = getProxyDelay(name);
      const alive = isProxyAlive(p);
      if (delay === undefined) {
        unchecked++;
      } else if (!alive || delay === 0 || delay > 400) {
        bad++;
      } else if (delay < 150) {
        fast++;
      } else {
        mid++;
      }
    }

    const total = nodeNames.length || 1;
    const fastPct = (fast / total) * 100;
    const midPct = (mid / total) * 100;
    const badPct = (bad / total) * 100;
    const uncheckedPct = (unchecked / total) * 100;

    const tooltip = $t('proxies.health_tooltip', {
      fast,
      mid,
      bad,
      unchecked
    });

    return {
      fast,
      mid,
      bad,
      unchecked,
      total: nodeNames.length,
      fastPct,
      midPct,
      badPct,
      uncheckedPct,
      tooltip
    };
  }

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

  function computeStats(): ObservatoryStats {
    const uniqueNodes = new Map<string, { alive: boolean; delay?: number }>();

    // 1. Root and Provider proxies from Mihomo
    for (const p of Object.values(proxies)) {
      const typeLower = (p.type || '').toLowerCase();
      const nameLower = (p.name || '').toLowerCase();

      // Исключаем группы прокси
      if (['selector', 'urltest', 'fallback', 'loadbalance', 'relay'].includes(typeLower)) {
        continue;
      }
      // Исключаем системные/встроенные прокси
      if (['direct', 'reject', 'compatible', 'pass'].includes(typeLower)) {
        continue;
      }
      if (['direct', 'reject', 'compatible', 'pass', 'global'].includes(nameLower)) {
        continue;
      }

      const delay = getLastDelay(p);
      const alive = isProxyAlive(p);
      uniqueNodes.set(p.name, { alive, delay });
    }

    // 2. External subscription nodes (subNodes)
    for (const [subId, nodesList] of Object.entries(subNodes)) {
      if (!Array.isArray(nodesList)) continue;
      const healthMap = subHealth[subId] || {};
      for (const n of nodesList) {
        if (!n || (!n.tag && !n.name)) continue;
        const key = n.tag || n.name || '';
        if (uniqueNodes.has(key)) continue;

        const h = healthMap[key];
        const alive = h ? h.alive : true;
        const delay = h?.tested && h?.delay !== undefined ? h.delay : undefined;
        uniqueNodes.set(key, { alive, delay });
      }
    }

    const total = uniqueNodes.size;
    let healthy = 0;
    let degraded = 0;
    let down = 0;
    let activeCount = 0;
    let activeDelaySum = 0;

    for (const { alive, delay } of uniqueNodes.values()) {
      if (alive && delay !== undefined && delay > 0 && delay < 300) {
        healthy++;
        activeCount++;
        activeDelaySum += delay;
      } else if (alive && delay !== undefined && delay >= 300 && delay < 800) {
        degraded++;
        activeCount++;
        activeDelaySum += delay;
      } else if (!alive || delay === 0 || (delay !== undefined && delay >= 800)) {
        down++;
      }
    }

    const avg = activeCount > 0 ? activeDelaySum / activeCount : 0;

    return {
      totalProxies: total,
      healthyProxies: healthy,
      degradedProxies: degraded,
      downProxies: down,
      avgLatency: Math.round(avg)
    };
  }

  let observatoryStats = $derived(computeStats());

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
    const timestamp = new Date(lastItem.time).getTime();
    return isNaN(timestamp) ? null : timestamp;
  }

  function getLatencyTitle(proxyName: string): string {
    if (isLatencyStale(proxyName)) {
      const tMs = getProxyLastTime(proxyName);
      if (tMs) {
        return $t('proxies.stale_tooltip', { timeAgo: formatTimeAgo(tMs, $t) });
      }
    }
    return '';
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
    }, 200);
  }

  function handleBadgeMouseLeave() {
    if (popoverHoverTimeout) {
      clearTimeout(popoverHoverTimeout);
      popoverHoverTimeout = null;
    }
  }

  function handleBadgeClick(e: MouseEvent, proxyName: string) {
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

  async function testLatency() {
    if (batchTester.isActive()) return;
    testingLatency = true;
    error = '';
    poller?.pause();

    const pingConfig = getCurrentPingConfig();
    const targetUrl = getTargetUrl(pingConfig);
    const timeoutMs = pingConfig.timeoutMs;

    try {
      const activeGroups = groups.filter((g) => g.name !== 'GLOBAL' && g.all && g.all.length > 0);
      if (activeGroups.length === 0) {
        testingLatency = false;
        poller?.resume();
        return;
      }

      const total = activeGroups.length;
      for (let i = 0; i < total; i++) {
        const g = activeGroups[i];
        batchProgress = {
          running: true,
          current: i + 1,
          total,
          currentNode: g.name,
          currentNodeFlag: getCountryFlag(g.name),
          retrying: false,
          retrySeconds: 0,
          targetUrl,
          timeoutMs
        };

        try {
          const res = await apiFetch(
            `/api/mihomo/proxy/group/${encodeURIComponent(g.name)}/delay?url=${encodeURIComponent(targetUrl)}&timeout=${timeoutMs}`,
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
            }
          }
        } catch {
          // continue with next group
        }
      }

      batchProgress = {
        running: false,
        current: total,
        total,
        currentNode: '',
        currentNodeFlag: '',
        retrying: false,
        retrySeconds: 0,
        targetUrl,
        timeoutMs
      };
      showToast('success', $t('proxies.test_all_success'));
    } catch (err: any) {
      if (err?.name !== 'AbortError') {
        showToast('error', err?.message || 'Error running batch latency test');
      }
    } finally {
      testingLatency = false;
      batchProgress = null;
      poller?.resume();
    }
  }

  async function testGroupLatency(group: ProxyGroup) {
    if (batchTester.isActive()) return;
    testingLatency = true;
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
          showToast('success', $t('proxies.test_all_success'));
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
          testingLatency = state.running;
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
      testingLatency = false;
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
        groups.some((g) => g.name === proxyName) ||
        ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay'].includes(
          proxies[proxyName]?.type || ''
        );

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
    const labels: Record<string, string> = {
      Selector: 'Selector',
      URLTest: 'URLTest',
      Fallback: 'Fallback',
      LoadBalance: 'LoadBalance'
    };
    return labels[type] || type;
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
    if (
      ['DIRECT', 'REJECT'].includes((proxy.name || proxyName).toUpperCase()) ||
      ['Direct', 'Reject', 'Compatible'].includes(proxy.type)
    )
      return 'lat dim';
    const delay = getProxyDelay(proxyName);
    let baseClass = 'lat';
    if (delay === undefined || delay === 0 || delay >= 800) {
      baseClass += ' bad';
    } else if (delay < 300) {
      baseClass += ' ok';
    } else {
      baseClass += ' mid';
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
    if (
      ['DIRECT', 'REJECT'].includes((proxy.name || proxyName).toUpperCase()) ||
      ['Direct', 'Reject', 'Compatible'].includes(proxy.type)
    )
      return '—';
    const delay = getProxyDelay(proxyName);
    if (delay === undefined || delay === 0 || delay >= 800) return 'timeout';
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
      const res = await apiFetch('/api/config/read?path=%2Fopt%2Fetc%2Fmihomo%2Fconfig.yaml');
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
              const name = trimmed
                .replace('- name:', '')
                .trim()
                .replace(/^['"]|['"]$/g, '');
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

  async function loadSubscriptions(signal?: AbortSignal) {
    const reqSignal = signal instanceof AbortSignal ? signal : undefined;
    if (subscriptions.length === 0 && Object.keys(proxies).length === 0) {
      loading = true;
    }
    try {
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
        {$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('proxies.title')}
      </div>
      <h1>{$t('proxies.title')}</h1>
      <p class="sub">{$t('proxies.subtitle')}</p>
    </div>
    {#if activeTab === 'groups'}
      <div class="ph-actions">
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

        <input
          class="group-search"
          type="search"
          bind:value={filterQuery}
          oninput={handleSearchInput}
          placeholder={$t('proxies.filter_placeholder')}
          aria-label={$t('proxies.filter_placeholder')}
        />
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
        <button class="btn btn-primary" onclick={testLatency} disabled={testingLatency}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="currentColor"
            style="margin-right: 6px;"><polygon points="5 3 19 12 5 21 5 3" /></svg
          >
          {testingLatency ? $t('proxies.testing') : $t('proxies.test_all_nodes')}
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
        {@const stats = observatoryStats}
        <div class="card obs-card">
          <div class="obs-head">
            <h2 class="card-title obs-title">{$t('proxies.observatory_title')}</h2>
          </div>
          <div class="obs-grid">
            <div class="stat-box obs-stat-box">
              <div class="stat-label">{$t('proxies.obs_total')}</div>
              <div class="obs-val-row">
                <span class="stat-value">{stats.totalProxies}</span>
                <span class="res-sub">
                  {$t('proxies.obs_total_sub', { groupsCount: groups.length })}
                </span>
              </div>
            </div>
            <div class="stat-box obs-stat-box">
              <div class="stat-label">{$t('proxies.obs_healthy')}</div>
              <div class="obs-val-row">
                <span class="stat-value ok">{stats.healthyProxies}</span>
                <span class="res-sub">{$t('proxies.obs_healthy_sub')}</span>
              </div>
            </div>
            <div class="stat-box obs-stat-box">
              <div class="stat-label">{$t('proxies.obs_degraded')}</div>
              <div class="obs-val-row">
                <span class="stat-value warn">{stats.degradedProxies}</span>
                <span class="res-sub">{$t('proxies.obs_degraded_sub')}</span>
              </div>
            </div>
            <div class="stat-box obs-stat-box">
              <div class="stat-label">{$t('proxies.obs_unreachable')}</div>
              <div class="obs-val-row">
                <span class="stat-value err">{stats.downProxies}</span>
                <span class="res-sub">{$t('proxies.obs_unreachable_sub')}</span>
              </div>
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
          description={$t('proxies.no_proxies_desc')}
          icon={WarningIcon}
          ctaText={$t('app.refresh')}
          oncta={fetchProxies}
        />
      {:else}
        <div class="group-grid">
          {#each filteredGroups as group}
            {@const isCollapsed = collapsedGroups.has(group.name)}
            {@const nodes = getFilteredNodes(group, searchDebouncedQuery)}
            <div class="group-card" class:expanded={!isCollapsed}>
              <button
                type="button"
                class="gc-head collapsible"
                aria-expanded={!isCollapsed}
                onclick={() => toggleCollapse(group.name)}
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
                  <span class="type-badge">{group.type.toUpperCase()}</span>

                  {#if group.now}
                    {@const latencyClass = getLatencyClass(group.now)}
                    {@const latencyText = getLatencyText(group.now)}
                    <div class="gc-lat-box {latencyClass}">{latencyText}</div>
                  {/if}

                  <span class="chevron-wrap" class:rotated={!isCollapsed} aria-hidden="true">
                    <ChevronDown
                      size={14}
                      color={isCollapsed ? 'var(--fg-dim)' : 'var(--accent)'}
                    />
                  </span>
                </div>

                <div class="gc-head-row2">
                  <span class="gc-count-text"
                    >{group.all.length}
                    {$t('proxies.nodes_label')}</span
                  >
                  <span class="gc-separator">·</span>
                  <span class="gc-active-label">{$t('proxies.active')}:</span>

                  {#each getSelectionChain(group.name) as item, index}
                    {@const itemFlag = !item.isGroup ? getCountryFlag(item.name) : null}
                    {@const itemLatencyText = getLatencyText(item.name)}
                    {@const itemLatencyClass = getLatencyClass(item.name)}
                    {#if index > 0}
                      <span class="gc-arrow">›</span>
                    {/if}
                    <div
                      class="gc-now-pill"
                      class:is-leaf={!item.isGroup}
                      class:lat-ok={itemLatencyClass === 'lat ok'}
                      class:lat-mid={itemLatencyClass === 'lat mid'}
                      class:lat-bad={itemLatencyClass === 'lat bad'}
                    >
                      <div
                        class="gc-now-dot"
                        class:is-leaf={!item.isGroup}
                        class:lat-ok={itemLatencyClass === 'lat ok'}
                        class:lat-mid={itemLatencyClass === 'lat mid'}
                        class:lat-bad={itemLatencyClass === 'lat bad'}
                      ></div>
                      {#if itemFlag}{itemFlag}
                      {/if}{item.name}
                    </div>
                  {:else}
                    <span style="color:var(--fg-dim)">—</span>
                  {/each}
                </div>
              </button>

              {#if isCollapsed}
                {@const hStats = getGroupHealthStats(nodes)}
                <div
                  class="health-bar"
                  title={hStats.tooltip}
                  aria-label={hStats.tooltip}
                  role="img"
                >
                  {#if hStats.fast > 0}
                    <div
                      class="health-segment fast"
                      style="width: {hStats.fastPct}%;"
                      title="{$t('proxies.health_fast')}: {hStats.fast}"
                    ></div>
                  {/if}
                  {#if hStats.mid > 0}
                    <div
                      class="health-segment mid"
                      style="width: {hStats.midPct}%;"
                      title="{$t('proxies.health_mid')}: {hStats.mid}"
                    ></div>
                  {/if}
                  {#if hStats.bad > 0}
                    <div
                      class="health-segment bad"
                      style="width: {hStats.badPct}%;"
                      title="{$t('proxies.health_bad')}: {hStats.bad}"
                    ></div>
                  {/if}
                  {#if hStats.unchecked > 0}
                    <div
                      class="health-segment unchecked"
                      style="width: {hStats.uncheckedPct}%;"
                      title="{$t('proxies.health_unchecked')}: {hStats.unchecked}"
                    ></div>
                  {/if}
                </div>
              {:else}
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
                    disabled={testingLatency || batchProgress?.running}
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

                {@const filteredNodesList = getFilteredGroupNodes(group.name, nodes)}
                <div class="proxy-grid">
                  {#each filteredNodesList as proxyName}
                    {@const isActive = group.now === proxyName}
                    {@const healthClass = getLatencyClass(proxyName)}
                    {@const healthText = getLatencyText(proxyName)}
                    {@const proxy = proxies[proxyName]}
                    {@const flag = getCountryFlag(proxyName)}

                    <div class="proxy-card" class:now={isActive}>
                      <button
                        type="button"
                        class="proxy-select-btn"
                        disabled={group.type !== 'Selector'}
                        title={group.type !== 'Selector'
                          ? $t('proxies.managed_automatically')
                          : undefined}
                        onclick={() =>
                          group.type === 'Selector' && selectProxy(group.name, proxyName)}
                      >
                        <div class="p-header">
                          <span class="p-name">
                            {#if flag}{flag}
                            {/if}{proxyName}
                          </span>
                          <span class="p-type">{getProxyTypeLabel(proxy)}</span>
                        </div>

                        <div class="p-footer">
                          {#if (batchProgress?.running && batchProgress?.currentNode === proxyName) || testingProxy === proxyName}
                            <span class="lat dim">
                              <span class="lat-spinner"></span>
                            </span>
                          {:else}
                            <span
                              class={healthClass}
                              title={getLatencyTitle(proxyName)}
                              onmouseenter={(e) => handleBadgeMouseEnter(e, proxyName)}
                              onmouseleave={handleBadgeMouseLeave}
                              onclick={(e) => handleBadgeClick(e, proxyName)}
                              role="button"
                              tabindex="0"
                              onkeydown={(e) => {
                                if (e.key === 'Enter' || e.key === ' ') {
                                  handleBadgeClick(e as any, proxyName);
                                }
                              }}
                            >
                              {healthText}
                            </span>
                          {/if}
                          {#if group.type === 'Selector'}
                            <span class="selector-dot" class:active={isActive}
                              >{isActive ? '●' : '○'}</span
                            >
                          {/if}
                        </div>
                      </button>

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
    border-left: 0;
    border-right: 0;
    border-top: 0;
    font: inherit;
    color: inherit;
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
    position: relative;
    transition: all var(--transition-fast);
    min-height: 84px;
  }
  .proxy-select-btn {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    padding: 10px 12px;
    width: 100%;
    height: 100%;
    min-height: 84px;
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    border-radius: var(--radius-md);
  }
  .proxy-select-btn:disabled {
    cursor: default;
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

  .health-bar {
    display: flex;
    height: 4px;
    background: rgba(255, 255, 255, 0.05);
    border-radius: var(--radius-xs, 2px);
    overflow: hidden;
    margin: 4px 18px 12px;
    transition: height 0.15s ease;
  }
  .health-bar:hover {
    height: 6px;
  }
  .health-segment {
    height: 100%;
    transition: width 0.3s ease;
  }
  .health-segment.fast {
    background: var(--success, #46d18a);
  }
  .health-segment.mid {
    background: var(--warning, #f0b450);
  }
  .health-segment.bad {
    background: var(--danger, #f4707f);
  }
  .health-segment.unchecked {
    background: var(--fg-dim, #869cb3);
    opacity: 0.4;
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
    width: 22px;
    height: 22px;
    flex-shrink: 0;
  }

  .brand-icon {
    width: 20px;
    height: 20px;
    object-fit: contain;
    display: block;
    flex-shrink: 0;
    border-radius: 4px;
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

  /* Compact Observatory Widget */
  .obs-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg, 10px);
    margin-bottom: 16px;
    overflow: hidden;
    box-shadow: var(--shadow-sm);
    padding: 0;
  }

  .obs-head {
    display: flex;
    align-items: center;
    padding: 6px 14px;
    background: linear-gradient(
      135deg,
      var(--bg-group-head-from, rgba(20, 51, 79, 0.6)),
      var(--bg-group-head-to, rgba(16, 42, 68, 0.7))
    );
    border-bottom: 1px solid var(--border-strong, var(--border));
  }

  .obs-head .card-title.obs-title {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-secondary);
    margin: 0;
    padding: 0;
    border: 0;
  }

  .obs-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    margin: 0;
    border: 0;
  }

  .obs-stat-box {
    padding: 8px 14px 10px;
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    justify-content: center;
    background: transparent;
  }

  .obs-stat-box:last-child {
    border-right: 0;
  }

  .obs-stat-box .stat-label {
    font-size: 9.5px;
    letter-spacing: 0.12em;
    margin-bottom: 2px;
    line-height: 1.2;
  }

  .obs-val-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
  }

  .obs-stat-box .stat-value {
    font-size: 17px;
    font-weight: 700;
    line-height: 1.15;
  }

  .obs-stat-box .stat-value.ok {
    color: var(--success);
  }

  .obs-stat-box .stat-value.warn {
    color: var(--warning);
  }

  .obs-stat-box .stat-value.err {
    color: var(--danger);
  }

  .obs-stat-box .res-sub {
    font-size: 11px;
    margin-top: 0;
    line-height: 1.2;
    white-space: nowrap;
  }

  @media (max-width: 768px) {
    .obs-grid {
      grid-template-columns: repeat(2, 1fr);
    }
    .obs-stat-box:nth-child(2) {
      border-right: 0;
    }
    .obs-stat-box:nth-child(1),
    .obs-stat-box:nth-child(2) {
      border-bottom: 1px solid var(--border);
    }
  }

  @media (max-width: 480px) {
    .obs-stat-box {
      padding: 6px 10px 8px;
    }
    .obs-stat-box .stat-value {
      font-size: 15px;
    }
    .obs-stat-box .res-sub {
      font-size: 10px;
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
