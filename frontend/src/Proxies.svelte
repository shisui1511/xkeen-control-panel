<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { t, tp, currentLang } from './i18n';
  import { usePoller } from './lib/poller';
  import { capabilities, fetchCapabilities, showToast, devMode, showConfirm } from './stores';
  import { apiFetch, apiFetchJSON } from './lib/api';
  import { parseValidationError } from './lib/errorParser';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import PlayIcon from './lib/components/icons/Play.svelte';
  import WarningIcon from './lib/components/icons/Warning.svelte';
  import FloatingProgress from './components/FloatingProgress.svelte';
  import LatencyHistoryPopover from './components/LatencyHistoryPopover.svelte';
  import PageHeader from './PageHeader.svelte';
  import Tabs from './components/Tabs.svelte';
  import Button from './components/Button.svelte';
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
    groupMatchesLatencyFilter,
    type ObservatoryFilter,
    type NodeSnapshot
  } from './lib/proxyStats';
  import { preserveInFlightLatency } from './lib/proxyMerge';
  import ObservatoryPanel from './components/proxies/ObservatoryPanel.svelte';
  import QuickSelectPopover, {
    type QuickSelectNode
  } from './components/proxies/QuickSelectPopover.svelte';

  // Subcomponents for proxies & providers (Plan 122-04 / ARCH-04)
  import ProxyFilterBar from './components/proxies/ProxyFilterBar.svelte';
  import ProxyGroupCard from './components/proxies/ProxyGroupCard.svelte';
  import ProvidersTab from './components/proxies/ProvidersTab.svelte';
  import ProxyDiagnosticModal from './components/proxies/ProxyDiagnosticModal.svelte';
  import { ProvidersState, type Subscription } from './components/proxies/providersState.svelte';
  import ClientExitIpBadge from './components/network/ClientExitIpBadge.svelte';
  import { fetchClientExitIP } from './lib/clientIp';

  interface Props {
    onSwitchTab?: (tab: string) => void;
  }

  let { onSwitchTab = () => {} }: Props = $props();

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
  // Потрековое состояние тестируемых групп (D-16, G-90-7): Set пересоздаётся при
  // каждом изменении, а не мутируется на месте — под $state обычный Set в Svelte 5
  // не является глубоко реактивным (тот же паттерн, что и у collapsedGroups ниже).
  let testingGroupNames = $state(new Set<string>());
  let testingProxy = $state('');

  function markGroupTesting(name: string) {
    testingGroupNames = new Set(testingGroupNames).add(name);
  }

  function unmarkGroupTesting(name: string) {
    const next = new Set(testingGroupNames);
    next.delete(name);
    testingGroupNames = next;
  }
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

  // Subscription state managed via ProvidersState (Plan 122-04 / ARCH-04)
  let providers = $state(new ProvidersState());

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
  $effect(() => {
    writeProxiesViewMode(viewMode);
  });

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

  function handleSearchInput() {
    if (searchTimeoutId) clearTimeout(searchTimeoutId);
    searchTimeoutId = setTimeout(() => {
      searchDebouncedQuery = filterQuery;
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
        const p = proxies[name];
        const delay = getProxyDelay(name);
        return delay !== undefined && classifyLatency(delay, isProxyAlive(p)) !== 'bad';
      });
    } else if (filter === 'timeouts') {
      list = list.filter((name) => {
        const p = proxies[name];
        const delay = getProxyDelay(name);
        return delay === undefined || classifyLatency(delay, isProxyAlive(p)) === 'bad';
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

  let observatoryStats = $derived(
    computeObservatoryStats(proxies, providers.subNodes, providers.subHealth)
  );

  // Имена узлов, чьи поля измерения (delay/alive/history) сейчас защищены от
  // затирания ответом фонового опроса (G-90-7): сами тестируемые группы, все
  // узлы внутри них и одиночный узел из testProxyLatency(). Дешёвая при простое.
  function getProtectedNodeNames(): Set<string> {
    if (testingGroupNames.size === 0 && !testingProxy) return new Set();
    const protectedNames = new Set<string>();
    for (const groupName of testingGroupNames) {
      protectedNames.add(groupName);
      const group = groups.find((g) => g.name === groupName);
      if (group) {
        for (const node of group.all) protectedNames.add(node);
      }
    }
    if (testingProxy) protectedNames.add(testingProxy);
    return protectedNames;
  }

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

      if (proxiesRes.status === 'rejected') {
        const e = proxiesRes.reason;
        if (e?.name !== 'AbortError' && e?.status !== 401) {
          error = e?.message || $t('proxies.load_error');
        }
      }

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

      proxies = preserveInFlightLatency(mergedProxies, proxies, getProtectedNodeNames());

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
      setTimeout(() => {
        fetchClientExitIP(true, $currentLang);
      }, 1500);
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
    if (testingGroupNames.has(group.name)) return;
    markGroupTesting(group.name);
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

      // Fallback to batch tester. Общий тестер один на всю страницу и остаётся
      // строго одиночным — если он уже занят другой группой, молчать нельзя
      // (T-90-08-05): сообщаем тостом и выходим, не плодя параллельный прогон.
      if (batchTester.isActive()) {
        showToast('info', $t('proxies.test_group_busy'));
        return;
      }

      const nodeSet = new Set<string>();
      for (const node of group.all) {
        const p = proxies[node];
        if (node && !isSystemProxy(node, p?.type)) {
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
      unmarkGroupTesting(group.name);
      if (!batchTester.isActive()) {
        batchProgress = null;
      }
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

  function getProxyHistory(proxyName: string): { time: string; delay: number }[] {
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

  onMount(() => {
    const hash = window.location.hash;
    if (hash.includes('tab=providers') || window.location.search.includes('tab=providers')) {
      activeTab = 'providers';
    }

    poller = usePoller(async (signal) => {
      await fetchProxies(signal);
      await providers.loadSubscriptions(signal, Object.keys(proxies).length > 0);
      providers.checkAutoExpand();
    }, 10000);

    const handleHashChange = () => {
      if (window.location.hash.includes('tab=providers')) {
        activeTab = 'providers';
      } else if (window.location.hash.includes('tab=groups')) {
        activeTab = 'groups';
      }
      providers.checkAutoExpand();
    };

    window.addEventListener('hashchange', handleHashChange);

    return () => {
      poller?.stop();
      batchTester.cancel();
      if (popoverHoverTimeout) clearTimeout(popoverHoverTimeout);
      if (loadTimeoutId) clearTimeout(loadTimeoutId);
      pendingTimeouts.forEach(clearTimeout);
      window.removeEventListener('hashchange', handleHashChange);
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
  <PageHeader
    title={$t('proxies.title')}
    subtitle={$t('proxies.subtitle')}
    breadcrumbs={[{ label: $t('nav.group_proxy_subs') }, { label: $t('proxies.title') }]}
    {onSwitchTab}
  >
    <ClientExitIpBadge />
    {#if activeTab === 'groups'}
      <ProxyFilterBar
        bind:filterQuery
        bind:viewMode
        {loading}
        onSearchInput={handleSearchInput}
        onExpandAll={expandAll}
        onCollapseAll={collapseAll}
        onRefresh={() => fetchProxies()}
      />
    {:else}
      <Button
        variant="secondary"
        onclick={() => providers.refreshAll()}
        disabled={providers.loading}
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
        >
        {$t('subscr.refresh_all')}
      </Button>

      <Button variant="primary" onclick={() => providers.openAddModal()}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('subscr.add')}
      </Button>
    {/if}
  </PageHeader>

  <Tabs
    bind:value={activeTab}
    items={[
      { value: 'groups', label: $t('proxies.tab_groups') },
      { value: 'providers', label: $t('proxies.tab_providers') }
    ]}
  />

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
      {:else if (searchDebouncedQuery.trim() !== '' || observatoryFilter !== null) && groupSections.core.length + groupSections.service.length + groupSections.system.length === 0}
        <div class="search-empty-state">
          {searchDebouncedQuery.trim() !== ''
            ? $t('proxies.search_no_matches')
            : $t('proxies.filter_no_matches')}
        </div>
      {:else}
        {#snippet renderGroup(group: ProxyGroup, role: GroupRole)}
          <ProxyGroupCard
            {group}
            {role}
            {groups}
            {proxies}
            isCollapsed={collapsedGroups.has(group.name)}
            {viewMode}
            searchQuery={searchDebouncedQuery}
            isPinned={pinnedCoreGroups.includes(group.name)}
            isAutoCore={role === 'core' && !pinnedCoreGroups.includes(group.name)}
            testingGroup={testingGroupNames.has(group.name)}
            {testingProxy}
            {batchProgress}
            groupFilter={groupFilters[group.name] ?? 'all'}
            renderLimit={getRenderLimit(group.name)}
            quickSelectGroupName={quickSelect?.groupName}
            bind:cardEl={groupCardEls[group.name]}
            onToggleCollapse={(name) => toggleCollapse(name)}
            onTogglePin={() => (pinnedCoreGroups = togglePinnedCoreGroup(group.name))}
            onSelectProxy={(gName, pName) => selectProxy(gName, pName)}
            onTestGroupLatency={() => testGroupLatency(group)}
            onTestProxyLatency={(pName) => testProxyLatency(pName)}
            onOpenQuickSelect={(anchor, gName) => openQuickSelect(anchor, gName)}
            onFocusGroupCard={(gName) => focusGroupCard(gName)}
            onSetGroupFilter={(gName, filter) => (groupFilters[gName] = filter as any)}
            onIncreaseRenderLimit={(gName, total) => increaseRenderLimit(gName, total)}
            onBadgeMouseEnter={(e, name) => handleBadgeMouseEnter(e, name)}
            onBadgeMouseLeave={() => handleBadgeMouseLeave()}
            onBadgeClick={(e, name) => handleBadgeClick(e, name)}
            {resolveNodeSnapshot}
            {getLatencyClass}
            {getLatencyText}
            {getLatencyTitle}
            {getFilteredNodes}
            {getFilteredGroupNodes}
          />
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
              {@render renderGroup(group, 'core')}
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
                {@render renderGroup(group, 'service')}
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
                {@render renderGroup(group, 'system')}
              {/each}
            </div>
          </section>
        {/if}
      {/if}
    {/if}
  {:else if activeTab === 'providers'}
    <ProvidersTab bind:state={providers} onOpenDiagnostic={openDiagnosticModal} />
  {/if}
</div>

<ProxyDiagnosticModal
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
  .group-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 420px), 1fr));
    gap: var(--grid-gap);
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
  .proxy-section-system .group-grid {
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 240px), 1fr));
  }

  /* Group List View layout */
  .group-grid.group-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  /* Skeleton styles */
  .skeleton-card {
    border: 1px dashed var(--border);
    border-radius: var(--radius-lg);
    background: transparent;
    overflow: hidden;
  }
  .skeleton-card .gc-head {
    padding: 14px 18px;
    display: flex;
    align-items: center;
    border-bottom: 1px solid var(--border-strong);
  }
  .skeleton-card .proxy-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 8px;
    padding: 12px;
  }
  .skeleton-card .proxy-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    min-height: 84px;
  }
  .skeleton-card .p-header {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .skeleton-card .p-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: auto;
  }

  @media (max-width: 767px) {
    .core-grid {
      grid-template-columns: 1fr;
    }
  }

  @media (max-width: 640px) {
    :global(.page-header-actions) {
      flex-wrap: wrap;
      width: 100%;
    }
    .group-grid {
      gap: 10px;
    }
  }
</style>
