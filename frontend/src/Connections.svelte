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
  let loadTimedOut = false;
  let loadTimeoutId: ReturnType<typeof setTimeout> | null = null;
  let refreshInterval: ReturnType<typeof setInterval>;
  let autoRefresh = true;

  // Filters
  let filterSource = '';
  let filterDest = '';
  let filterRule = '';
  let filterProxy = '';

  $: uniqueRules = [...new Set(connections.map((c) => c.rule).filter(Boolean))].sort();
  $: uniqueChains = [...new Set(connections.map((c) => getChainPath(c)).filter(Boolean))].sort();

  async function fetchConnections() {
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
      const res = await fetch('/api/mihomo/proxy/connections');
      if (!res.ok) throw new Error('Failed to load connections');

      const data = await res.json();
      connections = data.connections || [];
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

  async function closeConnection(id: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/mihomo/proxy/connections/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });

      if (!res.ok) throw new Error('Failed to close connection');
      showToast('success', $t('conn.close_success'));
      await fetchConnections();
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
      await fetchConnections();
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

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  function getDuration(startStr: string): string {
    try {
      const start = new Date(startStr);
      const diffMs = Date.now() - start.getTime();
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

  function getFilteredConnections(): Connection[] {
    return connections.filter((conn) => {
      if (filterSource && !conn.metadata.sourceIP.includes(filterSource)) return false;
      if (
        filterDest &&
        !conn.metadata.host.includes(filterDest) &&
        !conn.metadata.destinationIP.includes(filterDest)
      )
        return false;
      if (filterRule && conn.rule !== filterRule) return false;
      if (filterProxy && getChainPath(conn) !== filterProxy) return false;
      return true;
    });
  }

  function toggleAutoRefresh() {
    autoRefresh = !autoRefresh;
    clearInterval(refreshInterval); // always clear before (re)creating
    if (autoRefresh) {
      refreshInterval = setInterval(fetchConnections, 3000);
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
        fetchConnections();
        mihomoLaunching = false;
      }, 1500);
      setTimeout(async () => {
        await fetchCapabilities();
        fetchConnections();
      }, 4000);
    } catch (e: any) {
      showToast('error', e.message);
      mihomoLaunching = false;
    }
  }

  $: totalUpload = connections.reduce((acc, c) => acc + c.upload, 0);
  $: totalDownload = connections.reduce((acc, c) => acc + c.download, 0);
  $: filteredConnections = getFilteredConnections();

  onMount(() => {
    if ($capabilities === null || $capabilities.mihomo.reachable) {
      fetchConnections();
      refreshInterval = setInterval(fetchConnections, 3000);
    }
  });

  onDestroy(() => {
    clearInterval(refreshInterval);
    if (loadTimeoutId) clearTimeout(loadTimeoutId);
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_services')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('conn.title')}
      </div>
      <h1>{$t('conn.title')}</h1>
      <p class="sub">{$t('conn.active')}</p>
    </div>
    <div class="ph-actions">
      <label
        class="toggle-label"
        style="display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-secondary); cursor: pointer; user-select: none;"
      >
        <label
          class="toggle-switch"
          style="position: relative; display: inline-block; width: 36px; height: 20px;"
        >
          <input
            type="checkbox"
            checked={autoRefresh}
            on:change={toggleAutoRefresh}
            style="opacity: 0; width: 0; height: 0;"
          />
          <span
            class="toggle-slider"
            style="position: absolute; cursor: pointer; top: 0; left: 0; right: 0; bottom: 0; background-color: var(--border); transition: .2s; border-radius: 20px;"
          ></span>
        </label>
        {$t('conn.autorefresh')}
      </label>
      <button class="btn btn-secondary" on:click={fetchConnections} disabled={loading}>
        <Icon name="refresh" size={14} />
        {$t('app.refresh')}
      </button>
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
      oncta={fetchConnections}
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
            <th>{$t('conn.destination')}</th>
            <th>{$t('conn.rule')}</th>
            <th class="col-chain">{$t('conn.chain')}</th>
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
                <td><Skeleton type="text-line" width="180px" /></td>
                <td><Skeleton type="text-line" width="80px" /></td>
                <td class="col-chain"><Skeleton type="text-line" width="100px" /></td>
                <td class="col-traffic col-upload"><Skeleton type="text-line" width="50px" /></td>
                <td class="col-traffic col-download"><Skeleton type="text-line" width="50px" /></td>
                <td class="col-duration"><Skeleton type="text-line" width="30px" /></td>
                <td></td>
              </tr>
            {/each}
          {:else}
            {#each filteredConnections as conn (conn.id)}
              <tr class="conn-row">
                <td class="mono col-src">{conn.metadata.sourceIP}:{conn.metadata.sourcePort}</td>
                <td class="mono" style="word-break:break-all;"
                  >{conn.metadata.host || conn.metadata.destinationIP}:{conn.metadata
                    .destinationPort}</td
                >
                <td>
                  <span class="badge badge-info">
                    {conn.rule}
                  </span>
                  {#if conn.rulePayload}
                    <div class="rule-payload mono">{conn.rulePayload}</div>
                  {/if}
                </td>
                <td class="col-chain cell-route">{getChainPath(conn)}</td>
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
                <td colspan="8" style="text-align: center; padding: 30px; color: var(--fg-dim);">
                  {$t('conn.no_connections')}
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
  /* Custom slider styles to match global design system */
  .toggle-switch input:checked + .toggle-slider {
    background-color: var(--accent) !important;
  }
  .toggle-switch input:checked + .toggle-slider::before {
    transform: translateX(16px);
  }
  .toggle-slider::before {
    position: absolute;
    content: '';
    height: 16px;
    width: 16px;
    left: 2px;
    bottom: 2px;
    background-color: white;
    transition: 0.2s;
    border-radius: 50%;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
  }
  .conn-table-container {
    overflow-x: auto;
  }
  .connections-table {
    min-width: 700px;
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

  /* Column priority — hide tier-2/3 columns on mobile */
  @media (max-width: 640px) {
    .col-src,
    .col-traffic,
    .col-duration {
      display: none;
    }
    .connections-table {
      min-width: 0;
    }
  }
  @media (max-width: 480px) {
    .col-chain {
      display: none;
    }
  }
</style>
