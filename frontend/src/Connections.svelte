<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { t } from './i18n'
  import { capabilities, toastStore } from './stores'
  import Skeleton from './components/Skeleton.svelte'
  import EmptyState from './components/EmptyState.svelte'
  import PlayIcon from './lib/components/icons/Play.svelte'
  import Icon from './lib/components/Icon.svelte'

  interface Connection {
    id: string
    metadata: {
      network: string
      type: string
      sourceIP: string
      destinationIP: string
      sourcePort: string
      destinationPort: string
      host: string
    }
    upload: number
    download: number
    start: string
    chains: string[]
    rule: string
    rulePayload: string
  }

  let connections: Connection[] = []
  let loading = false
  let error = ''
  let loadTimedOut = false
  let loadTimeoutId: ReturnType<typeof setTimeout> | null = null
  let refreshInterval: ReturnType<typeof setInterval>
  let autoRefresh = true

  // Filters
  let filterSource = ''
  let filterDest = ''
  let filterRule = ''
  let filterProxy = ''

  async function fetchConnections() {
    loading = true
    error = ''
    loadTimedOut = false
    if (loadTimeoutId) clearTimeout(loadTimeoutId)
    loadTimeoutId = setTimeout(() => {
      if (loading) {
        loading = false
        loadTimedOut = true
        error = $t('ds.empty.load_timeout')
      }
    }, 10000)
    try {
      const res = await fetch('/api/mihomo/proxy/connections')
      if (!res.ok) throw new Error('Failed to load connections')
      
      const data = await res.json()
      connections = data.connections || []
    } catch (e: any) {
      error = e.message
    } finally {
      if (loadTimeoutId) { clearTimeout(loadTimeoutId); loadTimeoutId = null }
      loading = false
    }
  }

  async function closeConnection(id: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`/api/mihomo/proxy/connections/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      
      if (!res.ok) throw new Error('Failed to close connection')
      
      await fetchConnections()
    } catch (e: any) {
      error = e.message
    }
  }

  function getProxyName(conn: Connection): string {
    if (!conn.chains || conn.chains.length === 0) return 'DIRECT'
    return conn.chains[conn.chains.length - 1]
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
  }

  function getFilteredConnections(): Connection[] {
    return connections.filter(conn => {
      if (filterSource && !conn.metadata.sourceIP.includes(filterSource)) return false
      if (filterDest && !conn.metadata.host.includes(filterDest) && !conn.metadata.destinationIP.includes(filterDest)) return false
      if (filterRule && !conn.rule.toLowerCase().includes(filterRule.toLowerCase())) return false
      if (filterProxy) {
        const proxy = getProxyName(conn).toLowerCase()
        if (!proxy.includes(filterProxy.toLowerCase())) return false
      }
      return true
    })
  }

  function toggleAutoRefresh() {
    autoRefresh = !autoRefresh
    if (autoRefresh) {
      refreshInterval = setInterval(fetchConnections, 3000)
    } else {
      clearInterval(refreshInterval)
    }
  }

  let mihomoLaunching = false

  async function launchMihomo() {
    mihomoLaunching = true
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch('/api/mihomo/control', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ action: 'start' })
      })
      if (!res.ok) throw new Error('Failed to start Mihomo')
      setTimeout(() => fetchConnections(), 1500)
    } catch (e: any) {
      toastStore.update(items => [...items, { id: Date.now(), type: 'error', message: e.message }])
      mihomoLaunching = false
    }
  }

  onMount(() => {
    // Не запускать polling если Mihomo недоступен
    if ($capabilities === null || $capabilities.mihomo.reachable) {
      fetchConnections()
      refreshInterval = setInterval(fetchConnections, 3000)
    }
  })

  onDestroy(() => {
    clearInterval(refreshInterval)
    if (loadTimeoutId) clearTimeout(loadTimeoutId)
  })
</script>

<div class="container">
  <h1>{$t('conn.title')}</h1>
  <p class="text-secondary mb-3">{$t('conn.active')}</p>

  {#if $capabilities !== null && !$capabilities.mihomo.reachable}
    <EmptyState
      title={$t('ds.empty.mihomo_offline_title')}
      description={$t('ds.empty.mihomo_offline_desc')}
      icon={PlayIcon}
      ctaText={mihomoLaunching ? $t('ds.empty.mihomo_offline_loading') : $t('ds.empty.mihomo_offline_cta')}
      ctaLoading={mihomoLaunching}
      oncta={launchMihomo}
    />
  {:else}

  {#if error}
    <EmptyState
      title={$t('ds.empty.error_title')}
      description={error}
      icon="warning"
      ctaText={$t('app.refresh')}
      oncta={fetchConnections}
    />
  {:else}

  <div class="toolbar mb-2">
    <div class="filters">
      <input type="text" placeholder={$t('conn.source')} bind:value={filterSource} class="filter-input" />
      <input type="text" placeholder={$t('conn.destination')} bind:value={filterDest} class="filter-input" />
      <input type="text" placeholder={$t('conn.rule')} bind:value={filterRule} class="filter-input" />
      <input type="text" placeholder={$t('conn.proxy')} bind:value={filterProxy} class="filter-input" />
    </div>
    <div class="actions">
      <button class="btn btn-secondary" on:click={fetchConnections} disabled={loading}>
        <Icon name="refresh" size={14} />
        {loading ? $t('app.loading') : $t('app.refresh')}
      </button>
      <button class="btn btn-icon" class:active={autoRefresh} on:click={toggleAutoRefresh} title={$t('conn.autorefresh')}>
        {#if autoRefresh}<Icon name="pause" size={14} />{:else}<Icon name="play" size={14} />{/if}
      </button>
    </div>
  </div>

  <div class="stats mb-2">
    <span class="stat">{$t('conn.total', { count: connections.length })}</span>
    <span class="stat">{$t('conn.shown', { count: getFilteredConnections().length })}</span>
  </div>

  <div class="table-container">
    <table class="connections-table">
      <thead>
        <tr>
          <th>{$t('conn.source')}</th>
          <th>{$t('conn.destination')}</th>
          <th>{$t('conn.rule')}</th>
          <th>{$t('conn.proxy')}</th>
          <th>{$t('traffic.upload')}</th>
          <th>{$t('traffic.download')}</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#if loading && connections.length === 0}
          {#each Array(5) as _}
            <tr>
              <td>
                <div class="cell-source">
                  <Skeleton type="text-line" width="60px" />
                  <Skeleton type="text-line" width="100px" />
                </div>
              </td>
              <td>
                <div class="cell-dest">
                  <Skeleton type="text-line" width="140px" />
                </div>
              </td>
              <td>
                <Skeleton type="text-line" width="80px" />
              </td>
              <td>
                <Skeleton type="text-line" width="70px" />
              </td>
              <td class="bytes">
                <Skeleton type="text-line" width="50px" />
              </td>
              <td class="bytes">
                <Skeleton type="text-line" width="50px" />
              </td>
              <td></td>
            </tr>
          {/each}
        {:else}
          {#each getFilteredConnections() as conn}
            <tr>
              <td>
                <div class="cell-source">
                  <span class="network-badge">{conn.metadata.network}</span>
                  {conn.metadata.sourceIP}:{conn.metadata.sourcePort}
                </div>
              </td>
              <td>
                <div class="cell-dest">
                  <div class="host">{conn.metadata.host || conn.metadata.destinationIP}</div>
                  <div class="port">:{conn.metadata.destinationPort}</div>
                </div>
              </td>
              <td>
                <span class="rule-badge">{conn.rule}</span>
                {#if conn.rulePayload}
                  <span class="rule-payload">{conn.rulePayload}</span>
                {/if}
              </td>
              <td>
                <span class="proxy-name">{getProxyName(conn)}</span>
              </td>
              <td class="bytes">{formatBytes(conn.upload)}</td>
              <td class="bytes">{formatBytes(conn.download)}</td>
              <td>
                <button class="btn-close" on:click={() => closeConnection(conn.id)} title={$t('app.close')}>
                  <Icon name="close" size={12} />
                </button>
              </td>
            </tr>
          {:else}
            <tr>
              <td colspan="7" class="empty-cell">
                {$t('conn.no_connections')}
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

<style>
  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 1rem;
  }

  .filters {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
  }

  .filter-input {
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--text);
    font-size: 0.875rem;
    min-width: 120px;
  }

  .btn-icon {
    padding: 0.5rem;
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
  }

  .btn-icon.active {
    background: var(--primary);
    color: white;
    border-color: var(--primary);
  }

  .stats {
    display: flex;
    gap: 1rem;
    font-size: 0.875rem;
    color: var(--text-secondary);
  }

  .table-container {
    overflow-x: auto;
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .connections-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.875rem;
  }

  .connections-table th {
    padding: 0.75rem;
    text-align: left;
    font-weight: 600;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border);
    background: var(--bg);
    white-space: nowrap;
  }

  .connections-table td {
    padding: 0.75rem;
    border-bottom: 1px solid var(--border-light, rgba(0,0,0,0.05));
    vertical-align: top;
  }

  .connections-table tr:hover {
    background: var(--hover);
  }

  .cell-source {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .network-badge {
    font-size: 0.625rem;
    text-transform: uppercase;
    padding: 0.125rem 0.375rem;
    background: var(--primary);
    color: white;
    border-radius: 3px;
    font-weight: 600;
  }

  .cell-dest .host {
    font-weight: 500;
  }

  .cell-dest .port {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .rule-badge {
    display: inline-block;
    padding: 0.125rem 0.375rem;
    background: var(--bg);
    border-radius: 3px;
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .rule-payload {
    display: block;
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin-top: 0.25rem;
  }

  .proxy-name {
    font-weight: 500;
  }

  .bytes {
    font-family: monospace;
    white-space: nowrap;
    color: var(--text-secondary);
  }

  .btn-close {
    padding: 0.25rem 0.5rem;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    color: var(--danger);
    font-size: 0.75rem;
  }

  .btn-close:hover {
    background: var(--danger);
    color: white;
    border-color: var(--danger);
  }

  .empty-cell {
    text-align: center;
    color: var(--text-secondary);
    padding: 2rem;
  }
</style>
