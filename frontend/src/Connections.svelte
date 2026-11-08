<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
  import { SvelteMap } from 'svelte/reactivity';
  import type { Attachment } from 'svelte/attachments';
  import { t, pluralize, currentLang } from './i18n';
  import {
    capabilities,
    fetchCapabilities,
    showToast,
    showConfirm,
    mihomoApiReady,
    mihomoApiState,
    mihomoOfflineReason,
    activeKernelName,
    runningKernels,
    isMihomo
  } from './stores';
  import { apiFetch } from './lib/api';
  import { switchKernel, notifySwitchOutcome } from './lib/serviceControl';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import PageHeader from './PageHeader.svelte';
  import Select from './components/Select.svelte';
  import SegmentedControl from './components/SegmentedControl.svelte';
  import LiveIndicator from './components/LiveIndicator.svelte';
  import PlayIcon from './lib/components/icons/Play.svelte';
  import DownloadIcon from './lib/components/icons/Download.svelte';
  import WarningIcon from './lib/components/icons/Warning.svelte';
  import { swipeToDismiss } from './lib/gestures';

  // Телефон: карточку подключения смахивают влево, чтобы закрыть его
  const isPhone = () => window.matchMedia('(max-width: 768px)').matches;
  const rowSwipe = (id: string) =>
    swipeToDismiss({
      direction: 'left',
      threshold: 110,
      enabled: isPhone,
      onDismiss: () => closeConnection(id)
    });
  let { onSwitchTab = () => {} }: { onSwitchTab?: (tab: string) => void } = $props();

  // Запуск Mihomo со страницы — только когда не работает ни одно ядро: при активном Xray
  // (или любом другом запущенном процессе ядра) запуск столкнул бы два ядра (FND-05)
  const canLaunchMihomo = $derived($activeKernelName !== 'xray' && $runningKernels.length === 0);
  const mihomoOfflineDesc = $derived(
    $activeKernelName === 'xray'
      ? $t('apioffline.reason_xray')
      : $mihomoOfflineReason === 'api_down'
        ? $t('apioffline.reason_api_down')
        : $t('ds.empty.mihomo_offline_desc')
  );

  interface Connection {
    id: string;
    metadata: {
      network: string;
      type: string;
      sourceIP: string;
      destinationIP: string;
      sourcePort: string;
      destinationPort: string;
      host: string;
      process?: string;
      sniffHost?: string;
      inboundIP?: string;
      inboundPort?: string;
      inboundName?: string;
      inboundUser?: string;
    };
    upload: number;
    download: number;
    start: string;
    chains: string[];
    rule: string;
    rulePayload: string;
  }

  interface ClientInfo {
    ip: string;
    mac: string;
    name?: string;
    hostname?: string;
    display_name: string;
    active: boolean;
    link?: string;
    interface?: string;
  }

  let connections = $state<Connection[]>([]);
  let clients = $state<Record<string, ClientInfo>>({});
  let clientsRefreshTimer: ReturnType<typeof setInterval> | null = null;
  let launchTimer1: ReturnType<typeof setTimeout> | null = null;
  let launchTimer2: ReturnType<typeof setTimeout> | null = null;

  interface TrafficHistory {
    upload: number;
    download: number;
    timestamp: number;
  }
  const trafficHistory = new SvelteMap<string, TrafficHistory>();
  const connectionSpeeds = new SvelteMap<string, { uploadSpeed: number; downloadSpeed: number }>();

  let loading = $state(false);
  let error = $state('');
  let wsConnected = $state(false);
  let wsReconnecting = $state(false);
  let paused = $state(false);
  let destroyed = $state(false);

  // WebSocket
  let ws: WebSocket | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let reconnectDelay = 2000;
  const MAX_RECONNECT_DELAY = 30000;

  // Search & Filters (CONN-02)
  let searchQuery = $state('');
  let quickFilter = $state<'all' | 'proxy' | 'direct' | 'active'>('all');
  let groupingMode = $state<'none' | 'client' | 'host' | 'route'>('none');
  let collapsedGroups = $state<Record<string, boolean>>({});

  // Sorting (CONN-05)
  type SortKey = 'start' | 'upload' | 'download' | 'speed';
  let sortKey = $state<SortKey>('download');
  let sortAsc = $state(false);

  // Connection Inspector Drawer (CONN-06)
  let selectedConnectionId = $state<string | null>(null);
  let selectedConnection = $derived(
    selectedConnectionId ? connections.find((c) => c.id === selectedConnectionId) : null
  );

  function getDrawerFocusableElements(drawer: HTMLElement): HTMLElement[] {
    const selectors = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';
    return Array.from(drawer.querySelectorAll(selectors)).filter(
      (el) => (el as HTMLElement).offsetParent !== null
    ) as HTMLElement[];
  }

  // Фокус в панель при её появлении и возврат на строку при закрытии.
  const drawerFocus: Attachment<HTMLElement> = (drawer) => {
    const opener = document.activeElement as HTMLElement | null;
    const timer = setTimeout(() => {
      const focusables = getDrawerFocusableElements(drawer);
      if (focusables.length > 0) focusables[0].focus();
      else drawer.focus();
    }, 0);
    return () => {
      clearTimeout(timer);
      opener?.focus();
    };
  };

  function handleDrawerKeydown(event: KeyboardEvent) {
    const drawer = event.currentTarget as HTMLElement;
    if (event.key === 'Escape') {
      selectedConnectionId = null;
      return;
    }
    if (event.key === 'Tab') {
      const focusables = getDrawerFocusableElements(drawer);
      if (focusables.length === 0) {
        event.preventDefault();
        return;
      }
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement;
      if (event.shiftKey) {
        if (active === first || active === drawer || !drawer.contains(active)) {
          last.focus();
          event.preventDefault();
        }
      } else {
        if (active === last) {
          first.focus();
          event.preventDefault();
        }
      }
    }
  }

  async function loadClients() {
    // Скрытая вкладка не опрашивает роутер; при возврате список обновится сам.
    if (typeof document !== 'undefined' && document.hidden) return;
    try {
      const res = await apiFetch('/api/system/clients');
      if (res.ok) {
        const data = await res.json();
        clients = data.clients || {};
      }
    } catch (e: any) {
      if (e?.status === 401) return;
    }
  }

  function connectWS() {
    if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/api/mihomo/connections/ws`;

    wsReconnecting = false;
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      wsConnected = true;
      wsReconnecting = false;
      reconnectDelay = 2000;
      error = '';
      loading = false;
    };

    ws.onmessage = (event) => {
      if (document.hidden || paused) return;
      try {
        const data = JSON.parse(event.data);
        const now = Date.now();

        const rawConnections: Connection[] = data.connections || [];
        for (const conn of rawConnections) {
          const prev = trafficHistory.get(conn.id);
          let uploadSpeed = 0;
          let downloadSpeed = 0;

          if (prev) {
            const durationSec = (now - prev.timestamp) / 1000;
            if (durationSec > 0.2 && durationSec <= 5) {
              uploadSpeed = Math.max(0, (conn.upload - prev.upload) / durationSec);
              downloadSpeed = Math.max(0, (conn.download - prev.download) / durationSec);
              trafficHistory.set(conn.id, {
                upload: conn.upload,
                download: conn.download,
                timestamp: now
              });
            } else if (durationSec > 5) {
              trafficHistory.set(conn.id, {
                upload: conn.upload,
                download: conn.download,
                timestamp: now
              });
            } else {
              const prevSpeed = connectionSpeeds.get(conn.id);
              if (prevSpeed) {
                uploadSpeed = prevSpeed.uploadSpeed;
                downloadSpeed = prevSpeed.downloadSpeed;
              }
            }
          } else {
            trafficHistory.set(conn.id, {
              upload: conn.upload,
              download: conn.download,
              timestamp: now
            });
          }

          connectionSpeeds.set(conn.id, { uploadSpeed, downloadSpeed });
        }

        // Записи исчезнувших соединений убираются — карты держат только текущие id.
        const liveIds = new Set(rawConnections.map((c) => c.id));
        for (const id of [...trafficHistory.keys()]) {
          if (!liveIds.has(id)) trafficHistory.delete(id);
        }
        for (const id of [...connectionSpeeds.keys()]) {
          if (!liveIds.has(id)) connectionSpeeds.delete(id);
        }
        connections = rawConnections;
        loading = false;
      } catch (_) {}
    };

    ws.onerror = () => {
      wsConnected = false;
    };

    ws.onclose = () => {
      wsConnected = false;
      if (!destroyed) {
        wsReconnecting = true;
        reconnectTimer = setTimeout(() => {
          reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY);
          connectWS();
        }, reconnectDelay);
      }
    };
  }

  function disconnectWS() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (ws) {
      ws.onclose = null;
      ws.close();
      ws = null;
    }
    wsConnected = false;
    wsReconnecting = false;
  }

  async function closeConnection(id: string, e?: Event) {
    if (e) e.stopPropagation();
    try {
      const res = await apiFetch(`/api/mihomo/proxy/connections/${encodeURIComponent(id)}`, {
        method: 'DELETE'
      });

      if (!res.ok) throw new Error('Failed to close connection');
      connections = connections.filter((c) => c.id !== id);
      if (selectedConnectionId === id) selectedConnectionId = null;
      showToast('success', $t('conn.close_success'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
      error = e.message;
    }
  }

  async function closeAllConnections() {
    const count = connections.length;
    const confirmed = await showConfirm(
      $t('conn.close_all_title'),
      $t('conn.close_all_desc', { count }) + ' ' + $t('conn.close_all_consequence'),
      $t('conn.close_all_confirm_btn'),
      $t('app.cancel')
    );
    if (!confirmed) return;
    try {
      const res = await apiFetch('/api/mihomo/proxy/connections', {
        method: 'DELETE'
      });

      if (!res.ok) throw new Error('Failed to close all connections');
      connections = [];
      selectedConnectionId = null;
      showToast('success', $t('conn.close_all_success'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
      error = e.message;
    }
  }

  function isDirect(conn: Connection): boolean {
    return !conn.chains || conn.chains.length === 0 || conn.chains[0].toUpperCase() === 'DIRECT';
  }

  function getChainNodes(conn: Connection): string[] {
    if (isDirect(conn)) return ['DIRECT'];
    return conn.chains;
  }

  function getHost(conn: Connection): string {
    return conn.metadata.host || conn.metadata.destinationIP;
  }

  function getClientForConn(conn: Connection): ClientInfo | undefined {
    const ip = (conn.metadata.sourceIP || '').trim();
    if (!ip) return undefined;
    return clients[ip];
  }

  function formatEndpoint(conn: Connection): string {
    const ip = (conn.metadata.sourceIP || '').trim();
    const port = conn.metadata.sourcePort;
    const hasValidPort = port !== undefined && port !== null && Number(port) > 0;
    if (!ip) return hasValidPort ? `localhost:${port}` : 'localhost';
    return hasValidPort ? `${ip}:${port}` : ip;
  }

  function formatBytes(bytes: number): string {
    if (!bytes || bytes <= 0 || isNaN(bytes)) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  function getDuration(startStr: string): string {
    try {
      const start = new Date(startStr);
      if (isNaN(start.getTime())) return '—';
      const diffMs = Date.now() - start.getTime();
      if (diffMs < 0) return `0 ${$t('conn.sec')}`;
      const diffSec = Math.floor(diffMs / 1000);
      if (diffSec < 60) return `${diffSec} ${$t('conn.sec')}`;
      const diffMin = Math.floor(diffSec / 60);
      if (diffMin < 60) return `${diffMin} ${$t('conn.min')} ${diffSec % 60} ${$t('conn.sec')}`;
      const diffHrs = Math.floor(diffMin / 60);
      return `${diffHrs} ${$t('conn.hrs')} ${diffMin % 60} ${$t('conn.min')}`;
    } catch (_) {
      return '—';
    }
  }

  let mihomoLaunching = $state(false);

  async function launchMihomo() {
    mihomoLaunching = true;
    try {
      const res = await switchKernel('mihomo');
      if (res.outcome !== 'switched') notifySwitchOutcome(res);
      launchTimer1 = setTimeout(async () => {
        if (destroyed) return;
        // Поток подключит $effect по mihomoApiReady, когда capabilities покажут «API отвечает»
        await fetchCapabilities();
        if (destroyed) return;
        mihomoLaunching = false;
      }, 1500);
      launchTimer2 = setTimeout(async () => {
        if (destroyed) return;
        await fetchCapabilities();
      }, 4000);
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
      mihomoLaunching = false;
    }
  }

  function toggleSort(key: SortKey) {
    if (sortKey === key) {
      sortAsc = !sortAsc;
    } else {
      sortKey = key;
      sortAsc = false;
    }
  }

  function toggleGroup(groupKey: string) {
    collapsedGroups[groupKey] = !collapsedGroups[groupKey];
  }

  let totalUpload = $derived(connections.reduce((acc, c) => acc + c.upload, 0));
  let totalDownload = $derived(connections.reduce((acc, c) => acc + c.download, 0));

  // Filtered connections list
  let filteredConnections = $derived.by(() => {
    const q = searchQuery ? searchQuery.toLowerCase() : '';
    let list = connections.filter((conn) => {
      // Search query
      if (q) {
        const endpoint = formatEndpoint(conn).toLowerCase();
        const host = getHost(conn).toLowerCase();
        const destIP = (conn.metadata.destinationIP || '').toLowerCase();
        const rule = (conn.rule || '').toLowerCase();
        const rulePayload = (conn.rulePayload || '').toLowerCase();
        const chainStr = (conn.chains || []).join(' ').toLowerCase();
        const client = getClientForConn(conn);
        const clientName = (client?.display_name || '').toLowerCase();
        const clientHost = (client?.hostname || '').toLowerCase();
        const clientMac = (client?.mac || '').toLowerCase();
        const process = (conn.metadata.process || '').toLowerCase();

        const matches =
          endpoint.includes(q) ||
          host.includes(q) ||
          destIP.includes(q) ||
          rule.includes(q) ||
          rulePayload.includes(q) ||
          chainStr.includes(q) ||
          clientName.includes(q) ||
          clientHost.includes(q) ||
          clientMac.includes(q) ||
          process.includes(q);

        if (!matches) return false;
      }

      // Quick filter chips
      if (quickFilter === 'proxy' && isDirect(conn)) return false;
      if (quickFilter === 'direct' && !isDirect(conn)) return false;
      if (quickFilter === 'active') {
        const sp = connectionSpeeds.get(conn.id);
        const isTrafficActive = sp && (sp.uploadSpeed > 0 || sp.downloadSpeed > 0);
        if (!isTrafficActive) return false;
      }

      return true;
    });

    // Sorting. Время начала разбирается один раз на соединение, а не в каждом
    // сравнении: сортировка идёт на каждом сообщении потока (раз в секунду),
    // и при сотнях соединений Date-парсинг в компараторе занимал заметную долю кадра.
    const now = Date.now();
    const startAge = new Map<string, number>();
    if (sortKey === 'start') {
      for (const c of list) startAge.set(c.id, now - (new Date(c.start).getTime() || now));
    }
    list.sort((a, b) => {
      let valA = 0;
      let valB = 0;
      if (sortKey === 'start') {
        valA = startAge.get(a.id) ?? 0;
        valB = startAge.get(b.id) ?? 0;
      } else if (sortKey === 'upload') {
        valA = a.upload;
        valB = b.upload;
      } else if (sortKey === 'download') {
        valA = a.download;
        valB = b.download;
      } else if (sortKey === 'speed') {
        const spA = connectionSpeeds.get(a.id);
        const spB = connectionSpeeds.get(b.id);
        valA = (spA?.downloadSpeed || 0) + (spA?.uploadSpeed || 0);
        valB = (spB?.downloadSpeed || 0) + (spB?.uploadSpeed || 0);
      }
      return sortAsc ? valA - valB : valB - valA;
    });

    return list;
  });

  // Grouped structure (CONN-01)
  interface ConnectionGroup {
    key: string;
    title: string;
    subtitle?: string;
    activeCount: number;
    uploadTotal: number;
    downloadTotal: number;
    items: Connection[];
  }

  let groupedConnections = $derived.by(() => {
    if (groupingMode === 'none') return [];

    // Словарь без прототипа для поиска по ключу + массив для порядка первого появления.
    const byKey: Record<string, ConnectionGroup> = Object.create(null);
    const ordered: ConnectionGroup[] = [];

    for (const conn of filteredConnections) {
      let key = '';
      let title = '';
      let subtitle = '';

      if (groupingMode === 'client') {
        const client = getClientForConn(conn);
        const ip = conn.metadata.sourceIP || 'Unknown IP';
        key = ip;
        title = client?.display_name || ip;
        subtitle = client?.mac ? `${ip} · ${client.mac}` : ip;
      } else if (groupingMode === 'host') {
        const host = getHost(conn);
        key = host;
        title = host;
        subtitle = conn.metadata.destinationIP ? `IP: ${conn.metadata.destinationIP}` : '';
      } else if (groupingMode === 'route') {
        const chainStr = isDirect(conn) ? 'DIRECT' : (conn.chains || []).join(' → ');
        key = chainStr;
        title = chainStr;
        subtitle = isDirect(conn) ? $t('conn.route_direct_sub') : $t('conn.route_proxy_sub');
      }

      let grp = byKey[key];
      if (!grp) {
        grp = {
          key,
          title,
          subtitle,
          activeCount: 0,
          uploadTotal: 0,
          downloadTotal: 0,
          items: []
        };
        byKey[key] = grp;
        ordered.push(grp);
      }

      grp.activeCount += 1;
      grp.uploadTotal += conn.upload;
      grp.downloadTotal += conn.download;
      grp.items.push(conn);
    }

    return ordered.sort((a, b) => b.downloadTotal - a.downloadTotal);
  });

  function handleVisibilityChange() {
    if (document.visibilityState === 'visible') {
      trafficHistory.clear();
      void loadClients();
    }
  }

  function handleWindowKeyDown(e: KeyboardEvent) {
    if (e.key === 'Escape' && selectedConnectionId) {
      selectedConnectionId = null;
    }
  }

  onMount(() => {
    loadClients();
    clientsRefreshTimer = setInterval(loadClients, 20000);
    document.addEventListener('visibilitychange', handleVisibilityChange);
  });

  // Поток подключений подчиняется тому же сигналу, что и опросы Mihomo: сокет не открывается,
  // пока API не отвечает (в том числе до первого ответа capabilities), закрывается при уходе
  // в down без цикла переподключений и открывается сам, когда API ответил.
  function syncStream(ready: boolean, unknown: boolean) {
    if (ready) {
      loading = true;
      connectWS();
    } else {
      disconnectWS();
      loading = unknown;
    }
  }

  $effect(() => {
    const ready = $mihomoApiReady;
    const unknown = $mihomoApiState === 'unknown';
    untrack(() => syncStream(ready, unknown));
  });

  onDestroy(() => {
    destroyed = true;
    if (clientsRefreshTimer) {
      clearInterval(clientsRefreshTimer);
      clientsRefreshTimer = null;
    }
    if (launchTimer1) {
      clearTimeout(launchTimer1);
      launchTimer1 = null;
    }
    if (launchTimer2) {
      clearTimeout(launchTimer2);
      launchTimer2 = null;
    }
    document.removeEventListener('visibilitychange', handleVisibilityChange);
    disconnectWS();
  });
</script>

<svelte:window onkeydown={handleWindowKeyDown} />

<div class="container">
  <PageHeader title={$t('conn.title')} subtitle={$t('conn.h1_sub')}>
    {#snippet actions()}
      <LiveIndicator
        live={wsConnected && !paused}
        label={wsConnected && !paused
          ? $t('traffic.live_badge')
          : wsConnected && paused
            ? $t('traffic.paused_badge')
            : wsReconnecting
              ? $t('conn.ws_reconnecting')
              : $t('conn.ws_offline')}
      />
      <button
        class="btn btn-secondary"
        onclick={() => (paused = !paused)}
        title={paused ? $t('conn.resume') : $t('conn.pause')}
      >
        {#if paused}
          <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"
            ><polygon points="5 3 19 12 5 21 5 3" /></svg
          >
          <span>{$t('conn.resume')}</span>
        {:else}
          <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"
            ><rect x="6" y="5" width="4" height="14" rx="1" /><rect
              x="14"
              y="5"
              width="4"
              height="14"
              rx="1"
            /></svg
          >
          <span>{$t('conn.pause')}</span>
        {/if}
      </button>
      <button
        class="btn btn-danger-soft"
        onclick={closeAllConnections}
        disabled={connections.length === 0}
        title={$t('conn.close_all')}
      >
        <svg
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          ><polyline points="3 6 5 6 21 6" /><path
            d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"
          /></svg
        >
        <span>{$t('conn.close_all')}</span>
      </button>
    {/snippet}
  </PageHeader>

  {#if $capabilities?.kernels?.mihomo?.installed === false}
    <EmptyState
      title={$t('ds.empty.mihomo_missing_title')}
      description={$t('ds.empty.mihomo_missing_desc')}
      icon={DownloadIcon}
      ctaText={$t('ds.empty.mihomo_missing_cta')}
      oncta={() => onSwitchTab('services')}
    />
  {:else if $capabilities !== null && !$capabilities.mihomo.reachable && !canLaunchMihomo}
    <!-- Работает другое ядро или API запущенного Mihomo молчит: запуск не предлагается,
         действие — переход на «Службы» -->
    <EmptyState
      title={$t('ds.empty.mihomo_offline_title')}
      description={mihomoOfflineDesc}
      icon={WarningIcon}
      ctaText={$t('apioffline.services_link')}
      oncta={() => onSwitchTab('services')}
    />
  {:else if $capabilities !== null && !$capabilities.mihomo.reachable}
    <EmptyState
      title={$t('ds.empty.mihomo_offline_title')}
      description={$isMihomo
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
      oncta={connectWS}
    />
  {:else}
    <!-- Monolithic Smart Toolbar (CONN-02) -->
    <div class="conn-toolbar">
      <!-- Search Input with Clear and Counter -->
      <div class="search-wrap">
        <div class="search-field">
          <svg
            class="search-icon"
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            ><circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" /></svg
          >
          <input
            type="text"
            id="filter-source"
            class="search-input"
            placeholder={$t('conn.search_placeholder')}
            bind:value={searchQuery}
          />
          {#if searchQuery}
            <button
              class="clear-search-btn"
              onclick={() => (searchQuery = '')}
              aria-label={$t('app.clear')}>×</button
            >
          {/if}
        </div>
        {#if searchQuery}
          <span class="match-badge"
            >{$t('conn.match_found')} {filteredConnections.length}/{connections.length}</span
          >
        {/if}
      </div>

      <!-- Quick Filter Chips -->
      <div class="filter-chips">
        <SegmentedControl
          bind:value={quickFilter}
          items={[
            { value: 'all', label: $t('conn.filter_all') },
            { value: 'proxy', label: $t('conn.filter_proxy') },
            { value: 'direct', label: $t('conn.filter_direct') },
            { value: 'active', label: $t('conn.filter_active') }
          ]}
        />
      </div>

      <!-- Grouping Selector -->
      <div class="grouping-control">
        <span class="group-lbl">{$t('conn.group_by')}</span>
        <Select bind:value={groupingMode} class="group-select" ariaLabel={$t('conn.group_by')}>
          <option value="none">{$t('conn.group_none')}</option>
          <option value="client">{$t('conn.group_client')}</option>
          <option value="host">{$t('conn.group_host')}</option>
          <option value="route">{$t('conn.group_route')}</option>
        </Select>
      </div>

      <!-- Сортировка на телефоне: заголовков колонок там нет, строки — карточки -->
      <div class="grouping-control sort-control-mobile">
        <span class="group-lbl">{$t('conn.sort_by')}</span>
        <Select
          bind:value={sortKey}
          class="group-select"
          ariaLabel={$t('conn.sort_by')}
          onchange={() => (sortAsc = false)}
        >
          <option value="download">↓ {$t('conn.download')}</option>
          <option value="upload">↑ {$t('conn.upload')}</option>
          <option value="speed">{$t('conn.speed')}</option>
          <option value="start">{$t('conn.duration')}</option>
        </Select>
      </div>

      <!-- Live Totals Summary -->
      <div class="metrics-pill">
        <span class="m-stat"
          ><b>{filteredConnections.length}</b>
          {$t('conn.shown', { count: '' }).replace(/:\s*$/, '').trim()}</span
        >
        <span class="m-sep">·</span>
        <span class="m-stat text-upload">↑ {formatBytes(totalUpload)}</span>
        <span class="m-sep">·</span>
        <span class="m-stat text-download">↓ {formatBytes(totalDownload)}</span>
      </div>
    </div>

    <!-- Main Content Area: Grouped Accordions or Flat Table -->
    {#if groupingMode !== 'none'}
      <!-- Grouped View (CONN-01) -->
      <div class="grouped-container">
        {#each groupedConnections as grp (grp.key)}
          <div class="group-card">
            <!-- Group Header Button -->
            <button
              type="button"
              class="group-header"
              onclick={() => toggleGroup(grp.key)}
              aria-expanded={!collapsedGroups[grp.key]}
            >
              <div class="grp-title-group">
                <span class="grp-chevron" class:collapsed={collapsedGroups[grp.key]}>▾</span>
                <div>
                  <div class="grp-title">{grp.title}</div>
                  {#if grp.subtitle}
                    <div class="grp-sub">{grp.subtitle}</div>
                  {/if}
                </div>
              </div>
              <div class="grp-metrics">
                <span class="badge badge--tag badge-neutral">
                  {pluralize(
                    grp.activeCount,
                    $t('conn.sessions_count_one', { count: String(grp.activeCount) }),
                    $t('conn.sessions_count_few', { count: String(grp.activeCount) }),
                    $t('conn.sessions_count_many', { count: String(grp.activeCount) }),
                    $currentLang
                  )}
                </span>
                <span class="m-stat text-upload">↑ {formatBytes(grp.uploadTotal)}</span>
                <span class="m-stat text-download">↓ {formatBytes(grp.downloadTotal)}</span>
              </div>
            </button>

            <!-- Group Table Body -->
            {#if !collapsedGroups[grp.key]}
              <div class="table-container conn-table-container">
                <table class="connections-table">
                  <thead>
                    <tr>
                      <th class="col-src">{$t('conn.source')}</th>
                      <th class="col-host">{$t('conn.host')}</th>
                      <th>{$t('conn.rule')}</th>
                      <th class="col-chain">{$t('conn.chain')}</th>
                      <th class="col-network">{$t('conn.network')}</th>
                      <th
                        class="col-traffic col-upload right-align pointer"
                        role="columnheader"
                        tabindex="0"
                        aria-sort={sortKey === 'upload'
                          ? sortAsc
                            ? 'ascending'
                            : 'descending'
                          : 'none'}
                        onclick={() => toggleSort('upload')}
                        onkeydown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            toggleSort('upload');
                          }
                        }}
                      >
                        ↑ {$t('conn.upload')}
                        {sortKey === 'upload' ? (sortAsc ? '▲' : '▼') : ''}
                      </th>
                      <th
                        class="col-traffic col-download right-align pointer"
                        role="columnheader"
                        tabindex="0"
                        aria-sort={sortKey === 'download'
                          ? sortAsc
                            ? 'ascending'
                            : 'descending'
                          : 'none'}
                        onclick={() => toggleSort('download')}
                        onkeydown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            toggleSort('download');
                          }
                        }}
                      >
                        ↓ {$t('conn.download')}
                        {sortKey === 'download' ? (sortAsc ? '▲' : '▼') : ''}
                      </th>
                      <th
                        class="col-traffic col-speed right-align pointer"
                        role="columnheader"
                        tabindex="0"
                        aria-sort={sortKey === 'speed'
                          ? sortAsc
                            ? 'ascending'
                            : 'descending'
                          : 'none'}
                        onclick={() => toggleSort('speed')}
                        onkeydown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            toggleSort('speed');
                          }
                        }}
                      >
                        ⚡ {$t('conn.speed')}
                        {sortKey === 'speed' ? (sortAsc ? '▲' : '▼') : ''}
                      </th>
                      <th
                        class="col-duration right-align pointer"
                        role="columnheader"
                        tabindex="0"
                        aria-sort={sortKey === 'start'
                          ? sortAsc
                            ? 'ascending'
                            : 'descending'
                          : 'none'}
                        onclick={() => toggleSort('start')}
                        onkeydown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            toggleSort('start');
                          }
                        }}
                      >
                        ⏱ {$t('conn.duration')}
                        {sortKey === 'start' ? (sortAsc ? '▲' : '▼') : ''}
                      </th>
                      <th style="width: 44px;"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each grp.items as conn (conn.id)}
                      {@render connectionRow(conn)}
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}
          </div>
        {:else}
          <EmptyState title={$t('conn.empty_title')} description={$t('conn.empty_desc')} />
        {/each}
      </div>
    {:else if !loading && connections.length === 0}
      <EmptyState title={$t('conn.empty_title')} description={$t('conn.empty_desc')} />
    {:else}
      <!-- Flat Table View -->
      <div class="table-container conn-table-container">
        <table class="connections-table">
          <thead>
            <tr>
              <th class="col-src">{$t('conn.source')}</th>
              <th class="col-host">{$t('conn.host')}</th>
              <th>{$t('conn.rule')}</th>
              <th class="col-chain">{$t('conn.chain')}</th>
              <th class="col-network">{$t('conn.network')}</th>
              <th
                class="col-traffic col-upload right-align pointer"
                role="columnheader"
                tabindex="0"
                aria-sort={sortKey === 'upload' ? (sortAsc ? 'ascending' : 'descending') : 'none'}
                onclick={() => toggleSort('upload')}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    toggleSort('upload');
                  }
                }}
              >
                ↑ {$t('conn.upload')}
                {sortKey === 'upload' ? (sortAsc ? '▲' : '▼') : ''}
              </th>
              <th
                class="col-traffic col-download right-align pointer"
                role="columnheader"
                tabindex="0"
                aria-sort={sortKey === 'download' ? (sortAsc ? 'ascending' : 'descending') : 'none'}
                onclick={() => toggleSort('download')}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    toggleSort('download');
                  }
                }}
              >
                ↓ {$t('conn.download')}
                {sortKey === 'download' ? (sortAsc ? '▲' : '▼') : ''}
              </th>
              <th
                class="col-traffic col-speed right-align pointer"
                role="columnheader"
                tabindex="0"
                aria-sort={sortKey === 'speed' ? (sortAsc ? 'ascending' : 'descending') : 'none'}
                onclick={() => toggleSort('speed')}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    toggleSort('speed');
                  }
                }}
              >
                ⚡ {$t('conn.speed')}
                {sortKey === 'speed' ? (sortAsc ? '▲' : '▼') : ''}
              </th>
              <th
                class="col-duration right-align pointer"
                role="columnheader"
                tabindex="0"
                aria-sort={sortKey === 'start' ? (sortAsc ? 'ascending' : 'descending') : 'none'}
                onclick={() => toggleSort('start')}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    toggleSort('start');
                  }
                }}
              >
                ⏱ {$t('conn.duration')}
                {sortKey === 'start' ? (sortAsc ? '▲' : '▼') : ''}
              </th>
              <th style="width: 44px;"></th>
            </tr>
          </thead>
          <tbody>
            {#if loading && connections.length === 0}
              {#each Array(6) as _, i (i)}
                <tr>
                  <td class="col-src"><Skeleton type="text-line" width="120px" /></td>
                  <td class="col-host"><Skeleton type="text-line" width="160px" /></td>
                  <td><Skeleton type="text-line" width="80px" /></td>
                  <td class="col-chain"><Skeleton type="text-line" width="100px" /></td>
                  <td class="col-network"><Skeleton type="text-line" width="40px" /></td>
                  <td class="col-traffic col-upload"><Skeleton type="text-line" width="50px" /></td>
                  <td class="col-traffic col-download"
                    ><Skeleton type="text-line" width="50px" /></td
                  >
                  <td class="col-traffic col-speed"><Skeleton type="text-line" width="60px" /></td>
                  <td class="col-duration"><Skeleton type="text-line" width="30px" /></td>
                  <td></td>
                </tr>
              {/each}
            {:else}
              {#each filteredConnections as conn (conn.id)}
                {@render connectionRow(conn)}
              {:else}
                <tr>
                  <td colspan="10" style="text-align: center; padding: 40px; color: var(--fg-dim);">
                    {wsConnected ? $t('conn.no_connections') : $t('conn.ws_offline')}
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}
</div>

<!-- Table Row Snippet -->
{#snippet connectionRow(conn: Connection)}
  {@const speed = connectionSpeeds.get(conn.id)}
  {@const client = getClientForConn(conn)}
  {@const nodes = getChainNodes(conn)}
  <tr
    class="conn-row"
    class:selected={selectedConnectionId === conn.id}
    {@attach rowSwipe(conn.id)}
    tabindex="0"
    role="button"
    aria-label={`${getHost(conn)}:${conn.metadata.destinationPort}`}
    onclick={() => (selectedConnectionId = conn.id)}
    onkeydown={(e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        if ((e.target as HTMLElement)?.tagName !== 'BUTTON') {
          e.preventDefault();
          selectedConnectionId = conn.id;
        }
      }
    }}
  >
    <!-- Source Column -->
    <td class="col-src">
      <div class="src-cell">
        {#if client && client.display_name && client.display_name !== client.ip}
          <div class="src-main">
            <span class="src-name">{client.display_name}</span>
            {#if conn.metadata.process}
              <span class="badge-process">{conn.metadata.process}</span>
            {/if}
          </div>
          <span class="monospace src-sub">{formatEndpoint(conn)}</span>
        {:else if conn.metadata.process}
          <div class="src-main">
            <span class="badge-process">{conn.metadata.process}</span>
          </div>
          <span class="monospace src-sub">{formatEndpoint(conn)}</span>
        {:else}
          <span class="monospace src-main-ip">{formatEndpoint(conn)}</span>
        {/if}
      </div>
    </td>

    <!-- Host Column -->
    <td class="monospace col-host">
      <span class="host-cell" title={`${getHost(conn)}:${conn.metadata.destinationPort}`}>
        {getHost(conn)}<span class="host-port">:{conn.metadata.destinationPort}</span>
      </span>
    </td>

    <!-- Rule Column -->
    <td class="col-rule">
      <span class="badge badge--tag badge-rule">{conn.rule || 'Match'}</span>
      {#if conn.rulePayload}
        <div class="rule-payload monospace">{conn.rulePayload}</div>
      {/if}
    </td>

    <!-- Chain / Route Column (CONN-03) -->
    <td class="col-chain">
      {#if isDirect(conn)}
        <span class="badge badge--tag badge-direct">DIRECT</span>
      {:else}
        <div class="chain-flow">
          {#each nodes as node, idx (idx)}
            <span class="chain-node" title={node}>{node}</span>
            {#if idx < nodes.length - 1}
              <span class="chain-sep">›</span>
            {/if}
          {/each}
        </div>
      {/if}
    </td>

    <!-- Network Column -->
    <td class="col-network">
      <span
        class="badge badge--tag net-badge"
        class:net-tcp={conn.metadata.network?.toUpperCase() === 'TCP'}
        class:net-udp={conn.metadata.network?.toUpperCase() === 'UDP'}
      >
        {conn.metadata.network?.toUpperCase() || '—'}
      </span>
    </td>

    <!-- Upload Column (CONN-04: Mute 0 B/s) -->
    <td class="num col-traffic col-upload right-align">
      <div class="bytes-val text-upload">{formatBytes(conn.upload)}</div>
    </td>

    <!-- Download Column (CONN-04: Mute 0 B/s) -->
    <td class="num col-traffic col-download right-align">
      <div class="bytes-val text-download">{formatBytes(conn.download)}</div>
    </td>

    <!-- Speed Column (CONN-04: Mute 0 B/s, CONN-05: sortable) -->
    <td class="num col-traffic col-speed right-align">
      {#if speed && speed.uploadSpeed > 0}
        <div class="speed-active">↑ {formatBytes(speed.uploadSpeed)}/s</div>
      {/if}
      {#if speed && speed.downloadSpeed > 0}
        <div class="speed-active">↓ {formatBytes(speed.downloadSpeed)}/s</div>
      {/if}
    </td>

    <!-- Duration Column -->
    <td class="num col-duration right-align">
      {getDuration(conn.start)}
    </td>

    <!-- Action Close Column -->
    <td class="col-close">
      <button
        class="btn-close-conn"
        onclick={(e) => closeConnection(conn.id, e)}
        title={$t('conn.close_this')}
        aria-label={$t('conn.close_this')}
      >
        ×
      </button>
    </td>
  </tr>
{/snippet}

<!-- Connection Inspector Drawer (CONN-06) -->
{#if selectedConnection}
  {@const conn = selectedConnection}
  {@const speed = connectionSpeeds.get(conn.id)}
  {@const client = getClientForConn(conn)}
  <button
    type="button"
    class="drawer-backdrop"
    tabindex="-1"
    aria-hidden="true"
    onclick={() => (selectedConnectionId = null)}
  ></button>
  <div
    class="inspector-drawer"
    role="dialog"
    aria-modal="true"
    aria-labelledby="inspector-title"
    tabindex="-1"
    {@attach drawerFocus}
    onkeydown={handleDrawerKeydown}
  >
    <div class="drawer-header">
      <div class="drawer-title-group">
        <h3 class="drawer-title" id="inspector-title">{$t('conn.inspector_title')}</h3>
        <span class="drawer-subtitle monospace">{conn.id}</span>
      </div>
      <button
        class="drawer-close"
        onclick={() => (selectedConnectionId = null)}
        aria-label={$t('app.close')}
      >
        ✕
      </button>
    </div>

    <div class="drawer-body">
      <!-- Section 1: Network Transport -->
      <div class="drawer-section">
        <h4 class="section-heading">{$t('conn.section_network')}</h4>
        <div class="meta-grid">
          <div class="m-row">
            <span class="m-key">{$t('conn.host')}:</span>
            <span class="m-val monospace">{getHost(conn)}:{conn.metadata.destinationPort}</span>
          </div>
          <div class="m-row">
            <span class="m-key">{$t('conn.destination')} IP:</span>
            <span class="m-val monospace">{conn.metadata.destinationIP || '—'}</span>
          </div>
          <div class="m-row">
            <span class="m-key">{$t('conn.source')}:</span>
            <span class="m-val monospace">{formatEndpoint(conn)}</span>
          </div>
          {#if client}
            <div class="m-row">
              <span class="m-key">{$t('conn.client_device')}:</span>
              <span class="m-val">{client.display_name} {client.mac ? `(${client.mac})` : ''}</span>
            </div>
          {/if}
          <div class="m-row">
            <span class="m-key">{$t('conn.network')}:</span>
            <span class="m-val"
              >{conn.metadata.network?.toUpperCase()} / {conn.metadata.type || 'Direct'}</span
            >
          </div>
          {#if conn.metadata.process}
            <div class="m-row">
              <span class="m-key">{$t('conn.process_name')}:</span>
              <span class="m-val monospace">{conn.metadata.process}</span>
            </div>
          {/if}
        </div>
      </div>

      <!-- Section 2: Routing & Rules -->
      <div class="drawer-section">
        <h4 class="section-heading">{$t('conn.section_routing')}</h4>
        <div class="meta-grid">
          <div class="m-row">
            <span class="m-key">{$t('conn.rule')}:</span>
            <span class="m-val badge badge--tag badge-rule">{conn.rule || 'Match'}</span>
          </div>
          {#if conn.rulePayload}
            <div class="m-row">
              <span class="m-key">{$t('conn.rule_payload')}:</span>
              <span class="m-val monospace">{conn.rulePayload}</span>
            </div>
          {/if}
          <div class="m-row">
            <span class="m-key">{$t('conn.chain')}:</span>
            <div class="m-val chain-flow">
              {#each getChainNodes(conn) as node, idx (idx)}
                <span class="chain-node">{node}</span>
                {#if idx < getChainNodes(conn).length - 1}
                  <span class="chain-sep">›</span>
                {/if}
              {/each}
            </div>
          </div>
        </div>
      </div>

      <!-- Section 3: Traffic Telemetry -->
      <div class="drawer-section">
        <h4 class="section-heading">{$t('conn.section_traffic')}</h4>
        <div class="meta-grid">
          <div class="m-row">
            <span class="m-key">{$t('conn.upload')}:</span>
            <span class="m-val monospace text-upload">
              {formatBytes(conn.upload)}
              {#if speed && speed.uploadSpeed > 0}
                ({formatBytes(speed.uploadSpeed)}/s)
              {/if}
            </span>
          </div>
          <div class="m-row">
            <span class="m-key">{$t('conn.download')}:</span>
            <span class="m-val monospace text-download">
              {formatBytes(conn.download)}
              {#if speed && speed.downloadSpeed > 0}
                ({formatBytes(speed.downloadSpeed)}/s)
              {/if}
            </span>
          </div>
          <div class="m-row">
            <span class="m-key">{$t('conn.duration')}:</span>
            <span class="m-val monospace">{getDuration(conn.start)}</span>
          </div>
          <div class="m-row">
            <span class="m-key">{$t('conn.start_time')}:</span>
            <span class="m-val monospace">{new Date(conn.start).toLocaleString()}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="drawer-footer">
      <button class="btn btn-danger-soft w-100" onclick={() => closeConnection(conn.id)}>
        ✕ {$t('conn.close_this')}
      </button>
    </div>
  </div>
{/if}

<style>
  /* Monolithic Smart Toolbar (CONN-02) */
  .conn-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-3);
    flex-wrap: wrap;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--spacing-2) var(--spacing-3);
    margin-bottom: var(--spacing-4);
  }

  .search-wrap {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .search-field {
    position: relative;
    display: flex;
    align-items: center;
  }

  .search-icon {
    position: absolute;
    left: 10px;
    color: var(--fg-dim);
    pointer-events: none;
  }

  .search-input {
    height: 32px;
    padding: 0 26px 0 28px;
    font-size: var(--font-size-sm);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--bg-elevated);
    color: var(--fg-primary);
    width: 220px;
    transition:
      border-color var(--transition-fast),
      box-shadow var(--transition-fast),
      width var(--transition-fast);
  }

  .search-input:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(var(--accent-rgb), 0.2);
    width: 260px;
  }

  .match-badge {
    font-size: var(--font-size-xs);
    color: var(--accent);
    font-weight: 700;
    font-family: var(--font-family-mono);
    white-space: nowrap;
  }

  .clear-search-btn {
    position: absolute;
    right: 6px;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: var(--font-size-base);
    line-height: 1;
    padding: 0 var(--spacing-1);
  }

  /* Filter Chips */
  .filter-chips {
    display: inline-flex;
    /* Never wider than the screen: the segmented control scrolls instead. */
    max-width: 100%;
    min-width: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--bg-elevated);
    padding: 1px;
  }

  /* Grouping Control */
  .grouping-control {
    display: flex;
    align-items: center;
    gap: var(--spacing-1-5);
  }

  .grouping-control :global(.xcp-select) {
    width: auto;
  }

  .group-lbl {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  /* Metrics Pill */
  .metrics-pill {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1-5);
    font-size: var(--font-size-xs);
    padding: var(--spacing-1) var(--spacing-2-5);
    background: var(--bg-elevated);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-sm);
    font-family: var(--font-family-sans);
    font-variant-numeric: tabular-nums;
  }

  .m-stat b {
    color: var(--fg-primary);
  }

  .m-sep {
    color: var(--fg-faint);
  }

  .text-upload {
    color: var(--success);
  }

  .text-download {
    color: var(--accent);
  }

  /* Tables & Groups (CONN-01) */
  .grouped-container {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .group-header {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--spacing-3) var(--spacing-4);
    background: var(--bg-elevated);
    border: none;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    text-align: left;
    transition: background var(--transition-fast);
  }

  .group-header:hover {
    background: var(--bg-hover);
  }

  .grp-title-group {
    display: flex;
    align-items: center;
    gap: var(--spacing-2-5);
  }

  .grp-chevron {
    font-size: var(--font-size-base);
    color: var(--accent);
    transition: transform var(--transition-move);
  }

  .grp-chevron.collapsed {
    transform: rotate(-90deg);
  }

  .grp-title {
    font-size: var(--font-size-base);
    font-weight: 700;
    color: var(--fg-primary);
  }

  .grp-sub {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
  }

  .grp-metrics {
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
    font-size: var(--font-size-xs);
    font-family: var(--font-family-mono);
  }

  /* Connections Table */
  .conn-table-container {
    overflow-x: auto;
    width: 100%;
  }

  .connections-table {
    width: 100%;
    min-width: 900px;
    border-collapse: collapse;
  }

  .connections-table th {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    font-weight: 600;
    padding: var(--spacing-2);
    border-bottom: 1px solid var(--border);
    user-select: none;
  }

  .pointer {
    cursor: pointer;
  }

  .pointer:hover {
    color: var(--fg-primary);
  }

  .connections-table th.pointer:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .right-align {
    text-align: right;
  }

  .conn-row {
    border-bottom: 1px solid var(--border-light);
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .conn-row:hover {
    background: var(--surface-tint-faint);
  }

  .conn-row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    background: rgba(var(--accent-rgb), 0.08);
  }

  .conn-row.selected {
    background: rgba(var(--accent-rgb), 0.08);
  }

  .conn-row td {
    padding: var(--spacing-2);
    font-size: var(--font-size-sm);
    vertical-align: middle;
  }

  .monospace {
    font-family: var(--font-family-mono);
  }

  /* Числа (байты, скорость, длительность) — основной шрифт с цифрами одной
     ширины: столбцы ровные, а текст не пестрит вторым шрифтом */
  .num {
    font-family: var(--font-family-sans);
    font-variant-numeric: tabular-nums;
  }

  /* Source Cell */
  .src-cell {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-0-5);
  }

  .src-main {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1-5);
  }

  .src-name {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .src-main-ip {
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
  }

  .src-sub {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .badge-process {
    font-size: var(--font-size-xs);
    font-weight: 700;
    padding: 1px var(--spacing-1);
    border-radius: var(--radius-xs);
    background: color-mix(in srgb, var(--seq-5) 15%, transparent);
    color: var(--seq-5);
  }

  /* Host Cell */
  .host-cell {
    display: inline-block;
    max-width: clamp(160px, 12vw, 260px);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .host-port {
    color: var(--fg-dim);
    font-size: var(--font-size-xs);
  }

  /* Badges & Route (CONN-03) */
  .badge-rule {
    background: var(--surface-tint);
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    font-weight: 600;
  }

  .rule-payload {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    margin-top: var(--spacing-0-5);
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge-direct {
    background: color-mix(in srgb, var(--success) 15%, transparent);
    color: var(--success);
    font-size: var(--font-size-xs);
    font-weight: 700;
    padding: var(--spacing-0-5) var(--spacing-1-5);
    border-radius: var(--radius-sm);
  }

  td.col-duration {
    white-space: nowrap;
  }

  td.col-chain {
    min-width: 150px;
    max-width: 170px;
  }

  .chain-flow {
    display: flex;
    align-items: center;
    gap: var(--spacing-1);
    flex-wrap: wrap;
  }

  /* DS2-02 decorative sequence mapping: chain-node -> --seq-1, net-tcp -> --seq-2, net-udp -> --seq-3, badge-process -> --seq-5 */
  .chain-node {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-xs);
    color: var(--seq-1);
    background: color-mix(in srgb, var(--seq-1) 12%, transparent);
    padding: 1px 5px;
    border-radius: var(--radius-xs);
  }

  .chain-sep {
    color: var(--fg-faint);
    font-size: var(--font-size-xs);
  }

  .net-badge {
    font-size: var(--font-size-xs);
    font-weight: 700;
    padding: 1px 5px;
    border-radius: var(--radius-xs);
  }

  .net-tcp {
    background: color-mix(in srgb, var(--seq-2) 15%, transparent);
    color: var(--seq-2);
  }

  .net-udp {
    background: color-mix(in srgb, var(--seq-3) 15%, transparent);
    color: var(--seq-3);
  }

  /* Speeds (CONN-04) */
  .speed-active {
    font-size: var(--font-size-xs);
    color: var(--accent);
    font-weight: 600;
    margin-top: var(--spacing-0-5);
    font-family: var(--font-family-sans);
    font-variant-numeric: tabular-nums;
  }

  .col-speed {
    width: 92px;
    white-space: nowrap;
  }

  .btn-close-conn {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--danger);
    font-size: var(--font-size-lg);
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .btn-close-conn::before {
    content: '';
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: 44px;
    height: 44px;
  }

  .btn-close-conn:hover {
    background: color-mix(in srgb, var(--danger) 20%, transparent);
  }

  /* Inspector Drawer (CONN-06) */
  .drawer-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    -webkit-backdrop-filter: blur(2px);
    backdrop-filter: blur(2px);
    z-index: var(--z-drawer);
    animation: drawer-backdrop-in var(--duration-enter) var(--ease-out);
  }

  .inspector-drawer {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 420px;
    max-width: 90vw;
    background: var(--bg-card);
    border-left: 1px solid var(--border);
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.5);
    z-index: calc(var(--z-drawer) + 1);
    display: flex;
    flex-direction: column;
    animation: drawer-in var(--duration-move) var(--ease-out);
  }

  /* Выезд справа при открытии. Переключение между соединениями шторку не
     пересоздаёт, поэтому анимация играет только на первое открытие. */
  @keyframes drawer-backdrop-in {
    from {
      opacity: 0;
    }
  }
  @keyframes drawer-in {
    from {
      transform: translateX(24px);
      opacity: 0;
    }
  }

  .drawer-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    padding: var(--spacing-5) var(--spacing-6);
    border-bottom: 1px solid var(--border);
  }

  .drawer-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 700;
    color: var(--fg-primary);
  }

  .drawer-subtitle {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .drawer-close {
    background: transparent;
    border: none;
    color: var(--fg-dim);
    font-size: var(--font-size-lg);
    cursor: pointer;
    padding: var(--spacing-1);
  }

  .drawer-close:hover {
    color: var(--fg-primary);
  }

  .drawer-body {
    flex: 1;
    overflow-y: auto;
    padding: var(--spacing-5) var(--spacing-6);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-5);
  }

  .drawer-section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2-5);
  }

  .section-heading {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--accent);
    font-weight: 600;
  }

  .meta-grid {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    background: var(--bg-card);
    padding: var(--spacing-3) var(--spacing-3-5);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-light);
  }

  .m-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
  }

  .m-key {
    color: var(--fg-dim);
    flex-shrink: 0;
  }

  .m-val {
    color: var(--fg-primary);
    text-align: right;
    word-break: break-all;
  }

  .drawer-footer {
    padding: var(--spacing-4) var(--spacing-6);
    border-top: 1px solid var(--border);
  }

  /* Цепочка в инспекторе переносится внутри своей колонки, а не за край шторки */
  .m-val.chain-flow {
    min-width: 0;
    justify-content: flex-end;
  }

  .m-val.chain-flow .chain-node {
    max-width: 100%;
    white-space: normal;
    overflow-wrap: anywhere;
  }

  /* Телефон: инспектор — во весь экран, без полоски фона слева */
  @media (max-width: 768px) {
    .inspector-drawer {
      width: 100%;
      max-width: 100%;
      border-left: 0;
      padding-top: env(safe-area-inset-top);
    }

    .drawer-header {
      padding: var(--spacing-3) var(--spacing-2) var(--spacing-3) var(--spacing-4);
    }

    .drawer-close {
      width: 44px;
      height: 44px;
    }

    .drawer-body {
      padding: var(--spacing-4);
    }

    .drawer-footer {
      padding: var(--spacing-3) var(--spacing-4)
        calc(var(--spacing-3) + env(safe-area-inset-bottom));
    }
  }

  .w-100 {
    width: 100%;
  }

  .col-close {
    text-align: center;
  }

  .sort-control-mobile {
    display: none;
  }

  /* Телефон: строка таблицы — карточка. Хост крупно, под ним источник, сеть
     и длительность, затем правило с цепочкой и трафик; всё видно без
     горизонтальной прокрутки */
  @media (max-width: 768px) {
    .sort-control-mobile {
      display: flex;
    }

    .connections-table {
      min-width: 0;
    }

    .connections-table thead {
      display: none;
    }

    .connections-table tbody {
      display: block;
    }

    .conn-row {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto auto;
      grid-template-areas:
        'host host host close'
        'src src net dur'
        'rule chain chain chain'
        'up down speed speed';
      align-items: center;
      gap: var(--spacing-1) var(--spacing-2-5);
      padding: var(--spacing-2-5) var(--spacing-1) var(--spacing-2-5) var(--spacing-3);
    }

    .conn-row td {
      display: block;
      min-width: 0;
      padding: 0;
    }

    .conn-row .col-host {
      grid-area: host;
      font-size: var(--font-size-base);
      font-weight: 600;
      color: var(--fg-primary);
    }

    .conn-row .col-host .host-cell {
      display: block;
      max-width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .conn-row .col-src {
      grid-area: src;
      color: var(--fg-secondary);
      font-size: var(--font-size-xs);
    }

    .conn-row .col-network {
      grid-area: net;
    }

    .conn-row .col-duration {
      grid-area: dur;
      font-size: var(--font-size-xs);
      color: var(--fg-secondary);
    }

    .conn-row .col-rule {
      grid-area: rule;
      display: flex;
      align-items: center;
      gap: var(--spacing-1-5);
      min-width: 0;
    }

    .conn-row .col-rule .rule-payload {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .conn-row .col-chain {
      grid-area: chain;
      overflow: hidden;
    }

    .conn-row .col-chain .chain-flow {
      flex-wrap: nowrap;
      justify-content: flex-end;
    }

    .conn-row .col-chain .chain-node {
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .conn-row .col-upload {
      grid-area: up;
      text-align: left;
    }

    .conn-row .col-download {
      grid-area: down;
      text-align: left;
    }

    /* Скорости — столбиком справа: в одну строку с объёмами на 390 px они наезжали
       на «↓ 105.92 MB» */
    .conn-row .col-speed {
      grid-area: speed;
      display: flex;
      flex-direction: column;
      align-items: flex-end;
      gap: 0;
      white-space: nowrap;
    }

    .conn-row .col-upload,
    .conn-row .col-download {
      white-space: nowrap;
    }

    .conn-row .col-upload,
    .conn-row .col-download,
    .conn-row .col-speed {
      font-size: var(--font-size-xs);
    }

    /* На десктопе направление подписывает заголовок колонки, здесь — стрелка */
    .conn-row .col-upload .bytes-val::before {
      content: '↑ ';
    }

    .conn-row .col-download .bytes-val::before {
      content: '↓ ';
    }

    .conn-row .col-close {
      grid-area: close;
      align-self: start;
    }

    .conn-row .btn-close-conn {
      width: 40px;
      height: 40px;
      margin: calc(-1 * var(--spacing-2-5)) 0 calc(-1 * var(--spacing-1-5));
    }

    /* Поиск и фильтры — во всю ширину, группировка и сортировка — рядом в одну строку */
    .conn-toolbar {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      gap: var(--spacing-2-5);
    }

    .conn-toolbar > :not(.grouping-control) {
      grid-column: 1 / -1;
    }

    .grouping-control .group-lbl {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
      white-space: nowrap;
    }

    .grouping-control,
    .grouping-control :global(.xcp-select) {
      min-width: 0;
      width: 100%;
    }

    .search-wrap,
    .search-field,
    .search-input {
      width: 100%;
      height: 38px;
      font-size: var(--font-size-lg);
    }

    .conn-row td {
      border: 0;
    }

    .search-input:focus {
      width: 100%;
    }
  }
</style>
