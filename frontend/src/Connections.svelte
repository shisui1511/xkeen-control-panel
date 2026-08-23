<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, pluralize, currentLang } from './i18n';
  import { capabilities, fetchCapabilities, showToast, showConfirm } from './stores';
  import { apiFetch } from './lib/api';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import PlayIcon from './lib/components/icons/Play.svelte';
  import WarningIcon from './lib/components/icons/Warning.svelte';

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

  interface TrafficHistory {
    upload: number;
    download: number;
    timestamp: number;
  }
  let trafficHistory = $state(new Map<string, TrafficHistory>());
  let connectionSpeeds = $state(new Map<string, { uploadSpeed: number; downloadSpeed: number }>());

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

  async function loadClients() {
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
        const nextSpeeds = new Map<string, { uploadSpeed: number; downloadSpeed: number }>();
        const nextHistory = new Map<string, TrafficHistory>();

        const rawConnections: Connection[] = data.connections || [];
        for (const conn of rawConnections) {
          const prev = trafficHistory.get(conn.id);
          let uploadSpeed = 0;
          let downloadSpeed = 0;

          if (prev) {
            const durationSec = (now - prev.timestamp) / 1000;
            if (durationSec > 0.2) {
              uploadSpeed = Math.max(0, (conn.upload - prev.upload) / durationSec);
              downloadSpeed = Math.max(0, (conn.download - prev.download) / durationSec);
              nextHistory.set(conn.id, {
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
              nextHistory.set(conn.id, prev);
            }
          } else {
            nextHistory.set(conn.id, {
              upload: conn.upload,
              download: conn.download,
              timestamp: now
            });
          }

          nextSpeeds.set(conn.id, { uploadSpeed, downloadSpeed });
        }

        connectionSpeeds = nextSpeeds;
        trafficHistory = nextHistory;
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
    if (!bytes || bytes === 0) return '0 B';
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
      const res = await apiFetch('/api/mihomo/control', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'start' })
      });
      if (!res.ok) throw new Error('Failed to start Mihomo');
      setTimeout(async () => {
        await fetchCapabilities();
        connectWS();
        mihomoLaunching = false;
      }, 1500);
      setTimeout(async () => {
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
    let list = connections.filter((conn) => {
      // Search query
      if (searchQuery) {
        const q = searchQuery.toLowerCase();
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

    // Sorting
    list.sort((a, b) => {
      let valA = 0;
      let valB = 0;
      if (sortKey === 'start') {
        valA = new Date(a.start).getTime() || 0;
        valB = new Date(b.start).getTime() || 0;
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

    const map = new Map<string, ConnectionGroup>();

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
        subtitle = isDirect(conn) ? 'Direct outbound' : 'Proxy tunnel chain';
      }

      if (!map.has(key)) {
        map.set(key, {
          key,
          title,
          subtitle,
          activeCount: 0,
          uploadTotal: 0,
          downloadTotal: 0,
          items: []
        });
      }

      const grp = map.get(key)!;
      grp.activeCount += 1;
      grp.uploadTotal += conn.upload;
      grp.downloadTotal += conn.download;
      grp.items.push(conn);
    }

    return Array.from(map.values()).sort((a, b) => b.downloadTotal - a.downloadTotal);
  });

  onMount(() => {
    loadClients();
    clientsRefreshTimer = setInterval(loadClients, 20000);
    if ($capabilities === null || $capabilities.mihomo.reachable) {
      loading = true;
      connectWS();
    }
  });

  onDestroy(() => {
    destroyed = true;
    if (clientsRefreshTimer) {
      clearInterval(clientsRefreshTimer);
      clientsRefreshTimer = null;
    }
    disconnectWS();
  });
</script>

<div class="container">
  <!-- page-head -->
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_observability')} <span class="crumb-sep">›</span>
        {$t('conn.title')}
      </div>
      <h1>
        {$t('conn.title')}
        {#if wsConnected && !paused}
          <span class="live-badge running">
            <span class="live-dot success"></span>{$t('traffic.live_badge')}
          </span>
        {:else if wsConnected && paused}
          <span class="live-badge paused">
            <span class="live-dot warning"></span>{$t('traffic.paused_badge')}
          </span>
        {:else if wsReconnecting}
          <span class="live-badge warning">
            <span class="live-dot warning"></span>{$t('conn.ws_reconnecting')}
          </span>
        {:else}
          <span class="live-badge stopped">
            <span class="live-dot error"></span>{$t('conn.ws_offline')}
          </span>
        {/if}
      </h1>
      <p class="sub">{$t('conn.h1_sub')}</p>
    </div>
    <div class="ph-actions">
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
    </div>
  </div>

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
      oncta={connectWS}
    />
  {:else}
    <!-- Monolithic Smart Toolbar (CONN-02) -->
    <div class="conn-toolbar">
      <!-- Search Input with Clear and Counter -->
      <div class="search-wrap">
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
          <span class="match-badge">{filteredConnections.length}/{connections.length}</span>
          <button class="clear-search-btn" onclick={() => (searchQuery = '')} aria-label="Clear"
            >×</button
          >
        {/if}
      </div>

      <!-- Quick Filter Chips -->
      <div class="filter-chips">
        <button
          type="button"
          class="f-chip"
          class:active={quickFilter === 'all'}
          onclick={() => (quickFilter = 'all')}
        >
          {$t('conn.filter_all')}
        </button>
        <button
          type="button"
          class="f-chip"
          class:active={quickFilter === 'proxy'}
          onclick={() => (quickFilter = 'proxy')}
        >
          {$t('conn.filter_proxy')}
        </button>
        <button
          type="button"
          class="f-chip"
          class:active={quickFilter === 'direct'}
          onclick={() => (quickFilter = 'direct')}
        >
          {$t('conn.filter_direct')}
        </button>
        <button
          type="button"
          class="f-chip"
          class:active={quickFilter === 'active'}
          onclick={() => (quickFilter = 'active')}
        >
          {$t('conn.filter_active')}
        </button>
      </div>

      <!-- Grouping Selector -->
      <div class="grouping-control">
        <span class="group-lbl">{$t('conn.group_by')}</span>
        <select bind:value={groupingMode} class="group-select">
          <option value="none">{$t('conn.group_none')}</option>
          <option value="client">{$t('conn.group_client')}</option>
          <option value="host">{$t('conn.group_host')}</option>
          <option value="route">{$t('conn.group_route')}</option>
        </select>
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
                <span class="badge badge-neutral">
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
                        onclick={() => toggleSort('upload')}
                      >
                        ↑ {$t('conn.upload')}
                        {sortKey === 'upload' ? (sortAsc ? '▲' : '▼') : ''}
                      </th>
                      <th
                        class="col-traffic col-download right-align pointer"
                        onclick={() => toggleSort('download')}
                      >
                        ↓ {$t('conn.download')}
                        {sortKey === 'download' ? (sortAsc ? '▲' : '▼') : ''}
                      </th>
                      <th
                        class="col-duration right-align pointer"
                        onclick={() => toggleSort('start')}
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
          <div class="empty-table-state">
            <p>{$t('conn.empty_title')}</p>
          </div>
        {/each}
      </div>
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
                onclick={() => toggleSort('upload')}
              >
                ↑ {$t('conn.upload')}
                {sortKey === 'upload' ? (sortAsc ? '▲' : '▼') : ''}
              </th>
              <th
                class="col-traffic col-download right-align pointer"
                onclick={() => toggleSort('download')}
              >
                ↓ {$t('conn.download')}
                {sortKey === 'download' ? (sortAsc ? '▲' : '▼') : ''}
              </th>
              <th class="col-duration right-align pointer" onclick={() => toggleSort('start')}>
                ⏱ {$t('conn.duration')}
                {sortKey === 'start' ? (sortAsc ? '▲' : '▼') : ''}
              </th>
              <th style="width: 44px;"></th>
            </tr>
          </thead>
          <tbody>
            {#if loading && connections.length === 0}
              {#each Array(6) as _}
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
                  <td class="col-duration"><Skeleton type="text-line" width="30px" /></td>
                  <td></td>
                </tr>
              {/each}
            {:else}
              {#each filteredConnections as conn (conn.id)}
                {@render connectionRow(conn)}
              {:else}
                <tr>
                  <td colspan="9" style="text-align: center; padding: 40px; color: var(--fg-dim);">
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
    onclick={() => (selectedConnectionId = conn.id)}
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
    <td>
      <span class="badge badge-rule">{conn.rule || 'Match'}</span>
      {#if conn.rulePayload}
        <div class="rule-payload monospace">{conn.rulePayload}</div>
      {/if}
    </td>

    <!-- Chain / Route Column (CONN-03) -->
    <td class="col-chain">
      {#if isDirect(conn)}
        <span class="badge badge-direct">DIRECT</span>
      {:else}
        <div class="chain-flow">
          {#each nodes as node, idx}
            <span class="chain-node">{node}</span>
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
        class="badge net-badge"
        class:net-tcp={conn.metadata.network?.toUpperCase() === 'TCP'}
        class:net-udp={conn.metadata.network?.toUpperCase() === 'UDP'}
      >
        {conn.metadata.network?.toUpperCase() || '—'}
      </span>
    </td>

    <!-- Upload Column (CONN-04: Mute 0 B/s) -->
    <td class="monospace col-traffic col-upload right-align">
      <div class="bytes-val text-upload">{formatBytes(conn.upload)}</div>
      {#if speed && speed.uploadSpeed > 0}
        <div class="speed-active">↑ {formatBytes(speed.uploadSpeed)}/s</div>
      {/if}
    </td>

    <!-- Download Column (CONN-04: Mute 0 B/s) -->
    <td class="monospace col-traffic col-download right-align">
      <div class="bytes-val text-download">{formatBytes(conn.download)}</div>
      {#if speed && speed.downloadSpeed > 0}
        <div class="speed-active">↓ {formatBytes(speed.downloadSpeed)}/s</div>
      {/if}
    </td>

    <!-- Duration Column -->
    <td class="monospace col-duration right-align">
      {getDuration(conn.start)}
    </td>

    <!-- Action Close Column -->
    <td style="text-align:center;">
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
    onclick={() => (selectedConnectionId = null)}
    aria-label={$t('app.close')}
  ></button>
  <aside class="inspector-drawer">
    <div class="drawer-header">
      <div class="drawer-title-group">
        <h3 class="drawer-title">{$t('conn.inspector_title')}</h3>
        <span class="drawer-subtitle monospace">{conn.id}</span>
      </div>
      <button class="drawer-close" onclick={() => (selectedConnectionId = null)} aria-label="Close">
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
            <span class="m-val badge badge-rule">{conn.rule || 'Match'}</span>
          </div>
          {#if conn.rulePayload}
            <div class="m-row">
              <span class="m-key">Rule Payload:</span>
              <span class="m-val monospace">{conn.rulePayload}</span>
            </div>
          {/if}
          <div class="m-row">
            <span class="m-key">{$t('conn.chain')}:</span>
            <div class="m-val chain-flow">
              {#each getChainNodes(conn) as node, idx}
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
            <span class="m-key">Start:</span>
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
  </aside>
{/if}

<style>
  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 20px;
    gap: 16px;
  }

  .page-head h1 {
    margin: 4px 0 6px;
    font-size: 22px;
    font-weight: 700;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .page-head .sub {
    margin: 0;
    color: var(--fg-secondary);
    font-size: 13px;
  }

  .crumbs {
    font-size: 12px;
    color: var(--fg-dim);
    margin-bottom: 2px;
  }

  .crumb-sep {
    color: var(--fg-faint);
    margin: 0 6px;
  }

  .ph-actions {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-top: 6px;
  }

  .live-badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: 12px;
    border: 1px solid var(--border);
    background: var(--bg-card);
  }

  .live-badge.running {
    color: #46d18a;
    border-color: rgba(70, 209, 138, 0.3);
  }

  .live-badge.paused {
    color: #f5a623;
    border-color: rgba(245, 166, 35, 0.3);
  }

  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }

  .live-dot.success {
    background: #46d18a;
    box-shadow: 0 0 6px rgba(70, 209, 138, 0.6);
  }

  .live-dot.warning {
    background: #f5a623;
    box-shadow: 0 0 6px rgba(245, 166, 35, 0.6);
  }

  .live-dot.error {
    background: #f4707f;
  }

  /* Monolithic Smart Toolbar (CONN-02) */
  .conn-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 8px 12px;
    margin-bottom: 16px;
  }

  .search-wrap {
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
    padding: 0 54px 0 28px;
    font-size: 12.5px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    color: var(--fg-primary);
    width: 220px;
    transition: all 0.15s ease;
  }

  .search-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(41, 194, 240, 0.2);
    width: 260px;
  }

  .match-badge {
    position: absolute;
    right: 22px;
    font-size: 10px;
    color: var(--accent);
    font-weight: 700;
    font-family: var(--font-family-mono);
  }

  .clear-search-btn {
    position: absolute;
    right: 6px;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: 14px;
    line-height: 1;
    padding: 0 4px;
  }

  /* Filter Chips */
  .filter-chips {
    display: inline-flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--bg-secondary);
    padding: 1px;
  }

  .f-chip {
    padding: 4px 10px;
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-dim);
    background: transparent;
    border: none;
    border-radius: 3px;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .f-chip:hover:not(.active) {
    color: var(--fg-primary);
    background: var(--bg-hover);
  }

  .f-chip.active {
    background: var(--accent);
    color: #03182a;
    font-weight: 700;
  }

  /* Grouping Control */
  .grouping-control {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .group-lbl {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .group-select {
    height: 30px;
    padding: 0 8px;
    font-size: 11.5px;
    font-weight: 600;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    color: var(--fg-primary);
    cursor: pointer;
  }

  /* Metrics Pill */
  .metrics-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 11.5px;
    padding: 4px 10px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-light, rgba(255, 255, 255, 0.06));
    border-radius: var(--radius-sm);
    font-family: var(--font-family-mono);
  }

  .m-stat b {
    color: var(--fg-primary);
  }

  .m-sep {
    color: var(--fg-faint);
  }

  .text-upload {
    color: #46d18a;
  }

  .text-download {
    color: #29c2f0;
  }

  .btn-danger-soft {
    background: rgba(244, 112, 127, 0.15);
    color: var(--danger, #f4707f);
    border: 1px solid rgba(244, 112, 127, 0.3);
  }

  .btn-danger-soft:hover {
    background: rgba(244, 112, 127, 0.25);
  }

  /* Tables & Groups (CONN-01) */
  .grouped-container {
    display: flex;
    flex-direction: column;
    gap: 12px;
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
    padding: 12px 16px;
    background: var(--bg-secondary);
    border: none;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    text-align: left;
    transition: background 0.15s ease;
  }

  .group-header:hover {
    background: var(--bg-hover);
  }

  .grp-title-group {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .grp-chevron {
    font-size: 14px;
    color: var(--accent);
    transition: transform 0.2s ease;
  }

  .grp-chevron.collapsed {
    transform: rotate(-90deg);
  }

  .grp-title {
    font-size: 13.5px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .grp-sub {
    font-size: 11px;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
  }

  .grp-metrics {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 11.5px;
    font-family: var(--font-family-mono);
  }

  /* Connections Table */
  .conn-table-container {
    overflow-x: auto;
    width: 100%;
  }

  .connections-table {
    width: 100%;
    min-width: 820px;
    border-collapse: collapse;
  }

  .connections-table th {
    font-size: 11px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    user-select: none;
  }

  .pointer {
    cursor: pointer;
  }

  .pointer:hover {
    color: var(--fg-primary);
  }

  .right-align {
    text-align: right;
  }

  .conn-row {
    border-bottom: 1px solid var(--border-light, rgba(255, 255, 255, 0.04));
    cursor: pointer;
    transition: background 0.1s ease;
  }

  .conn-row:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .conn-row.selected {
    background: rgba(41, 194, 240, 0.08);
  }

  .conn-row td {
    padding: 8px 12px;
    font-size: 12.5px;
    vertical-align: middle;
  }

  .monospace {
    font-family: var(--font-family-mono);
  }

  /* Source Cell */
  .src-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .src-main {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .src-name {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .src-main-ip {
    font-size: 12px;
    color: var(--fg-primary);
  }

  .src-sub {
    font-size: 11px;
    color: var(--fg-dim);
  }

  .badge-process {
    font-size: 9.5px;
    font-weight: 700;
    padding: 1px 4px;
    border-radius: 3px;
    background: rgba(167, 139, 250, 0.15);
    color: #c4b5fd;
  }

  /* Host Cell */
  .host-cell {
    display: inline-block;
    max-width: min(35vw, 360px);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .host-port {
    color: var(--fg-dim);
    font-size: 11px;
  }

  /* Badges & Route (CONN-03) */
  .badge-rule {
    background: rgba(255, 255, 255, 0.06);
    color: #94a3b8;
    font-size: 10px;
    font-weight: 600;
  }

  .rule-payload {
    font-size: 10px;
    color: var(--fg-dim);
    margin-top: 2px;
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge-direct {
    background: rgba(70, 209, 138, 0.15);
    color: #46d18a;
    font-size: 10px;
    font-weight: 700;
    padding: 2px 6px;
    border-radius: 4px;
  }

  .chain-flow {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }

  .chain-node {
    font-size: 11px;
    color: #29c2f0;
    background: rgba(41, 194, 240, 0.08);
    padding: 1px 5px;
    border-radius: 3px;
  }

  .chain-sep {
    color: var(--fg-faint);
    font-size: 11px;
  }

  .net-badge {
    font-size: 9.5px;
    font-weight: 700;
    padding: 1px 5px;
    border-radius: 3px;
  }

  .net-tcp {
    background: rgba(56, 189, 248, 0.15);
    color: #38bdf8;
  }

  .net-udp {
    background: rgba(167, 139, 250, 0.15);
    color: #a78bfa;
  }

  /* Speeds (CONN-04) */
  .speed-active {
    font-size: 10.5px;
    color: #29c2f0;
    font-weight: 600;
    margin-top: 2px;
  }

  .btn-close-conn {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--danger, #f4707f);
    font-size: 16px;
    cursor: pointer;
    transition: background 0.15s ease;
  }

  .btn-close-conn::before {
    content: '';
    position: absolute;
    top: -8px;
    bottom: -8px;
    left: -8px;
    right: -8px;
    min-width: 44px;
    min-height: 44px;
  }

  .btn-close-conn:hover {
    background: rgba(244, 112, 127, 0.2);
  }

  /* Inspector Drawer (CONN-06) */
  .drawer-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px);
    z-index: 100;
  }

  .inspector-drawer {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 420px;
    max-width: 90vw;
    background: #0d2338;
    border-left: 1px solid var(--border);
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.5);
    z-index: 101;
    display: flex;
    flex-direction: column;
  }

  .drawer-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    padding: 20px 24px;
    border-bottom: 1px solid var(--border);
  }

  .drawer-title {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .drawer-subtitle {
    font-size: 11px;
    color: var(--fg-dim);
  }

  .drawer-close {
    background: transparent;
    border: none;
    color: var(--fg-dim);
    font-size: 16px;
    cursor: pointer;
    padding: 4px;
  }

  .drawer-close:hover {
    color: var(--fg-primary);
  }

  .drawer-body {
    flex: 1;
    overflow-y: auto;
    padding: 20px 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .drawer-section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .section-heading {
    margin: 0;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--accent);
    font-weight: 700;
  }

  .meta-grid {
    display: flex;
    flex-direction: column;
    gap: 8px;
    background: var(--bg-card);
    padding: 12px 14px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-light, rgba(255, 255, 255, 0.04));
  }

  .m-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 8px;
    font-size: 12px;
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
    padding: 16px 24px;
    border-top: 1px solid var(--border);
  }

  .w-100 {
    width: 100%;
  }

  @media (max-width: 768px) {
    .conn-toolbar {
      flex-direction: column;
      align-items: stretch;
    }

    .search-input {
      width: 100%;
    }

    .search-input:focus {
      width: 100%;
    }
  }
</style>
