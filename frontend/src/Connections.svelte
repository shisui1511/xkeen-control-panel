<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from './i18n';
  import { capabilities, fetchCapabilities, showToast } from './stores';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import PlayIcon from './lib/components/icons/Play.svelte';
  import WarningIcon from './lib/components/icons/Warning.svelte';
  import Icon from './lib/components/Icon.svelte';

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
      process?: string;  // Mihomo populates when find-process-mode=always
    };
    upload: number;
    download: number;
    start: string;
    chains: string[];
    rule: string;
    rulePayload: string;
  }

  let connections: Connection[] = [];
  let loading = false;
  let error = '';
  let wsConnected = false;
  let wsReconnecting = false;
  let destroyed = false;

  // WebSocket
  let ws: WebSocket | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let reconnectDelay = 2000;
  const MAX_RECONNECT_DELAY = 30000;

  // Filters
  let filterSource = '';
  let filterDest = '';
  let filterRule = '';
  let filterProxy = '';

  // Source-name toggle
  let showProcessName = false;
  let processModePatchPending = false;

  $: uniqueRules = [...new Set(connections.map((c) => c.rule).filter(Boolean))].sort();
  $: uniqueChains = [...new Set(connections.map((c) => getChainPath(c)).filter(Boolean))].sort();
  $: isMihomoActive = $capabilities === null || $capabilities.mihomo.reachable;

  async function loadProcessMode() {
    try {
      const res = await fetch('/api/mihomo/proxy/configs');
      if (res.ok) {
        const cfg = await res.json();
        showProcessName = cfg['find-process-mode'] === 'always';
      }
    } catch (_) {}
  }

  async function onToggleProcessName() {
    if (processModePatchPending) return;
    processModePatchPending = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/mihomo/proxy/configs', {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ 'find-process-mode': showProcessName ? 'always' : 'off' })
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
    } catch (_) {
      // Revert on network error or non-2xx HTTP response
      showProcessName = !showProcessName;
    } finally {
      processModePatchPending = false;
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
      try {
        const data = JSON.parse(event.data);
        connections = data.connections || [];
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

  async function closeConnection(id: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/mihomo/proxy/connections/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });

      if (!res.ok) throw new Error('Failed to close connection');
      showToast('success', $t('conn.close_success'));
    } catch (e: any) {
      error = e.message;
    }
  }

  async function closeAllConnections() {
    if (!confirm($t('conn.close_all_confirm'))) return;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/mihomo/proxy/connections', {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });

      if (!res.ok) throw new Error('Failed to close all connections');
      showToast('success', $t('conn.close_all_success'));
    } catch (e: any) {
      error = e.message;
    }
  }

  function getProxyName(conn: Connection): string {
    if (!conn.chains || conn.chains.length === 0) return 'DIRECT';
    return conn.chains[conn.chains.length - 1];
  }

  function getChainPath(conn: Connection): string {
    if (!conn.chains || conn.chains.length === 0) return 'DIRECT';
    return conn.chains.join(' → ');
  }

  function getHost(conn: Connection): string {
    return conn.metadata.host || conn.metadata.destinationIP;
  }

  function getSourceName(conn: Connection): string {
    if (showProcessName && conn.metadata.process) return conn.metadata.process;
    return `${conn.metadata.sourceIP}:${conn.metadata.sourcePort}`;
  }

  function getHostTooltip(conn: Connection): string {
    const host = conn.metadata.host || conn.metadata.destinationIP;
    return `${host}:${conn.metadata.destinationPort}`;
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
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
      if (diffMs < 0) return '0с';
      const diffSec = Math.floor(diffMs / 1000);
      if (diffSec < 60) return `${diffSec}с`;
      const diffMin = Math.floor(diffSec / 60);
      if (diffMin < 60) return `${diffMin}м`;
      const diffHrs = Math.floor(diffMin / 60);
      return `${diffHrs}ч ${diffMin % 60}м`;
    } catch (_) {
      return '—';
    }
  }



  let mihomoLaunching = false;

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
      setTimeout(async () => {
        await fetchCapabilities();
        connectWS();
        mihomoLaunching = false;
      }, 1500);
      setTimeout(async () => {
        await fetchCapabilities();
      }, 4000);
    } catch (e: any) {
      showToast('error', e.message);
      mihomoLaunching = false;
    }
  }

  $: totalUpload = connections.reduce((acc, c) => acc + c.upload, 0);
  $: totalDownload = connections.reduce((acc, c) => acc + c.download, 0);
  // Явное реактивное выражение — Svelte отслеживает connections, filterSource, filterDest, filterRule, filterProxy
  $: filteredConnections = connections.filter((conn) => {
    if (filterSource) {
      // Match against the value shown in the source column (process name or IP:port)
      const sourceName =
        showProcessName && conn.metadata.process
          ? conn.metadata.process
          : conn.metadata.sourceIP || '';
      if (!sourceName.includes(filterSource)) return false;
    }
    if (
      filterDest &&
      !(conn.metadata.host || '').includes(filterDest) &&
      !(conn.metadata.destinationIP || '').includes(filterDest)
    )
      return false;
    if (filterRule && conn.rule !== filterRule) return false;
    if (filterProxy && getChainPath(conn) !== filterProxy) return false;
    return true;
  });



  onMount(() => {
    if ($capabilities === null || $capabilities.mihomo.reachable) {
      loading = true;
      connectWS();
      loadProcessMode();
    }
  });

  onDestroy(() => {
    destroyed = true;
    disconnectWS();
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_services')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('conn.title')}
      </div>
      <h1>
        {$t('conn.title')}
        {#if wsConnected}
          <span class="live-indicator" title={$t('conn.ws_active')}>{$t('conn.live')}</span>
        {:else if wsReconnecting}
          <span class="live-indicator live-reconnecting">{$t('conn.ws_reconnecting')}</span>
        {/if}
      </h1>
      <p class="sub">{$t('conn.active')}</p>
    </div>
    <div class="ph-actions">
      <label
        class="toggle-label"
        class:disabled={!isMihomoActive}
        title={!isMihomoActive ? $t('conn.process_mode_disabled_hint') : ''}
      >
        <label class="toggle-switch">
          <input
            type="checkbox"
            bind:checked={showProcessName}
            on:change={onToggleProcessName}
            disabled={!isMihomoActive || processModePatchPending}
          />
          <span class="toggle-slider"></span>
        </label>
        {$t('conn.show_process_name')}
      </label>
      <button
        class="btn btn-secondary"
        style="color:var(--danger);"
        on:click={closeAllConnections}
        disabled={connections.length === 0}
      >
        {$t('conn.close_all')}
      </button>
    </div>
  </div>

  {#if $capabilities !== null && !$capabilities.mihomo.reachable}
    <EmptyState
      title={$t('ds.empty.mihomo_offline_title')}
      description={$t('ds.empty.mihomo_offline_desc')}
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
    <div class="toolbar mb-2">
      <div class="filters" style="display: flex; gap: 8px; flex-wrap: wrap; width: 100%;">
        <input
          type="text"
          placeholder={$t('conn.source') + ' (IP)...'}
          bind:value={filterSource}
          class="filter-input"
          style="flex: 1; min-width: 140px; padding: 6px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-card); color: var(--fg-primary);"
        />
        <input
          type="text"
          placeholder={$t('conn.destination') + ' (host / IP)...'}
          bind:value={filterDest}
          class="filter-input"
          style="flex: 1; min-width: 180px; padding: 6px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-card); color: var(--fg-primary);"
        />
        <select
          bind:value={filterRule}
          class="filter-input"
          style="flex: 1; min-width: 140px; padding: 6px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-card); color: var(--fg-primary); font-family: inherit; font-size: 13px;"
        >
          <option value="">{$t('conn.all_rules')}</option>
          {#each uniqueRules as rule}
            <option value={rule}>{rule}</option>
          {/each}
        </select>
        <select
          bind:value={filterProxy}
          class="filter-input"
          style="flex: 1; min-width: 140px; padding: 6px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-card); color: var(--fg-primary); font-family: inherit; font-size: 13px;"
        >
          <option value="">{$t('conn.all_chains')}</option>
          {#each uniqueChains as chain}
            <option value={chain}>{chain}</option>
          {/each}
        </select>
      </div>
    </div>

    <div
      class="stats mb-2"
      style="display: flex; gap: 16px; font-size: 13px; color: var(--fg-dim); align-items: center;"
    >
      <span class="stat"
        ><b>{connections.length}</b>
        {$t('conn.total', { count: '' }).replace(/:\s*$/, '').trim()}</span
      >
      <span class="stat"
        ><b>{filteredConnections.length}</b>
        {$t('conn.shown', { count: '' }).replace(/:\s*$/, '').trim()}</span
      >
      <span class="stat">↑ {formatBytes(totalUpload)}</span>
      <span class="stat">↓ {formatBytes(totalDownload)}</span>
    </div>

    <div class="table-container conn-table-container">
      <table class="connections-table">
        <thead>
          <tr>
            <th class="col-src">{$t('conn.source')}</th>
            <th class="col-host">{$t('conn.host')}</th>
            <th>{$t('conn.rule')}</th>
            <th class="col-chain">{$t('conn.chain')}</th>
            <th class="col-network">{$t('conn.network')}</th>
            <th class="col-traffic col-upload">↑ {$t('conn.upload')}</th>
            <th class="col-traffic col-download">↓ {$t('conn.download')}</th>
            <th class="col-duration">⏱ {$t('conn.duration')}</th>
            <th style="width: 40px;"></th>
          </tr>
        </thead>
        <tbody>
          {#if loading && connections.length === 0}
            {#each Array(5) as _}
              <tr>
                <td class="col-src"><Skeleton type="text-line" width="120px" /></td>
                <td class="col-host"><Skeleton type="text-line" width="160px" /></td>
                <td><Skeleton type="text-line" width="80px" /></td>
                <td class="col-chain"><Skeleton type="text-line" width="100px" /></td>
                <td class="col-network"><Skeleton type="text-line" width="40px" /></td>
                <td class="col-traffic col-upload"><Skeleton type="text-line" width="50px" /></td>
                <td class="col-traffic col-download"><Skeleton type="text-line" width="50px" /></td>
                <td class="col-duration"><Skeleton type="text-line" width="30px" /></td>
                <td></td>
              </tr>
            {/each}
          {:else}
            {#each filteredConnections as conn (conn.id)}
              <tr class="conn-row">
                <td class="mono col-src">{getSourceName(conn)}</td>
                <td class="mono col-host">
                  <span title={getHostTooltip(conn)} class="host-cell">
                    {getHost(conn)}
                    <span class="host-port">:{conn.metadata.destinationPort}</span>
                  </span>
                </td>
                <td>
                  <span class="badge badge-info">
                    {conn.rule}
                  </span>
                  {#if conn.rulePayload}
                    <div class="rule-payload mono">{conn.rulePayload}</div>
                  {/if}
                </td>
                <td class="col-chain cell-route">{getChainPath(conn)}</td>
                <td class="col-network">
                  <span
                    class="badge net-badge"
                    class:net-tcp={conn.metadata.network?.toUpperCase() === 'TCP'}
                    class:net-udp={conn.metadata.network?.toUpperCase() === 'UDP'}
                  >
                    {conn.metadata.network?.toUpperCase() || '—'}
                  </span>
                </td>
                <td
                  class="mono col-traffic col-upload"
                  style="text-align:right;color:var(--accent);">{formatBytes(conn.upload)}</td
                >
                <td
                  class="mono col-traffic col-download"
                  style="text-align:right;color:var(--accent);">{formatBytes(conn.download)}</td
                >
                <td class="mono col-duration" style="text-align:right;color:var(--fg-dim);"
                  >{getDuration(conn.start)}</td
                >
                <td style="text-align:center;">
                  <button
                    class="btn btn-secondary btn-close-conn"
                    style="padding: 4px 8px; color: var(--danger); border-color: transparent;"
                    on:click={() => closeConnection(conn.id)}
                    title={$t('app.close')}
                  >
                    ×
                  </button>
                </td>
              </tr>
            {:else}
              <tr>
                <td colspan="9" style="text-align: center; padding: 30px; color: var(--fg-dim);">
                  {wsConnected ? $t('conn.no_connections') : $t('conn.ws_offline')}
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  /* Toggle disabled state */
  .toggle-label.disabled {
    opacity: 0.45;
    cursor: not-allowed;
    pointer-events: none;
  }

  /* Live indicator */
  .live-indicator {
    display: inline-flex;
    align-items: center;
    font-size: 12px;
    font-weight: 500;
    color: #22d3ee;
    margin-left: 10px;
    letter-spacing: 0.03em;
    vertical-align: middle;
    animation: live-pulse 2s ease-in-out infinite;
  }
  .live-reconnecting {
    color: var(--fg-dim);
    animation: none;
  }
  @keyframes live-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.45; }
  }

  .conn-table-container {
    overflow-x: auto;
  }
  .connections-table {
    min-width: 800px;
  }
  .rule-payload {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 3px;
  }
  .conn-row:hover {
    background: var(--bg-hover, rgba(255, 255, 255, 0.02));
  }
  .btn-close-conn:hover {
    background: var(--danger) !important;
    color: white !important;
  }

  /* Host cell */
  .host-cell {
    display: inline-block;
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: middle;
    cursor: default;
  }
  .host-port {
    color: var(--fg-dim);
    font-size: 11px;
  }

  /* Network badge */
  .net-badge {
    font-size: 10px;
    font-weight: 700;
    padding: 2px 6px;
    border-radius: 4px;
    letter-spacing: 0.05em;
  }
  .net-tcp {
    background: rgba(56, 189, 248, 0.15);
    color: #38bdf8;
    border: 1px solid rgba(56, 189, 248, 0.25);
  }
  .net-udp {
    background: rgba(167, 139, 250, 0.15);
    color: #a78bfa;
    border: 1px solid rgba(167, 139, 250, 0.25);
  }

  /* Column priority — hide tier-2/3 columns on mobile */
  @media (max-width: 640px) {
    .col-src,
    .col-traffic,
    .col-duration,
    .col-network {
      display: none;
    }
    .connections-table {
      min-width: 0;
    }
  }
  @media (max-width: 480px) {
    .col-chain,
    .col-host {
      display: none;
    }
  }
</style>
