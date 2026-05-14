<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'

  interface Proxy {
    name: string
    type: string
    alive?: boolean
    delay?: number
    history?: { time: string; delay: number }[]
    udp?: boolean
    xudp?: boolean
    tfo?: boolean
  }

  interface ProxyGroup {
    name: string
    type: string
    now: string
    all: string[]
    alive?: boolean
    delay?: number
    history?: { time: string; delay: number }[]
  }

  interface ObservatoryStats {
    totalProxies: number
    healthyProxies: number
    degradedProxies: number
    downProxies: number
    avgLatency: number
  }

  let groups: ProxyGroup[] = []
  let proxies: Record<string, Proxy> = {}
  let loading = false
  let error = ''
  let testingLatency = false
  let testingProxy = ''
  let selectedGroup: ProxyGroup | null = null
  let showObservatory = false

  function computeStats(): ObservatoryStats {
    const proxyList = Object.values(proxies).filter(p => p.type !== 'Selector' && p.type !== 'URLTest' && p.type !== 'Fallback' && p.type !== 'LoadBalance')
    const total = proxyList.length
    const healthy = proxyList.filter(p => p.alive && (p.delay || 0) < 300).length
    const degraded = proxyList.filter(p => p.alive && (p.delay || 0) >= 300).length
    const down = proxyList.filter(p => !p.alive).length
    const avg = proxyList.length > 0
      ? proxyList.reduce((sum, p) => sum + (p.delay || 0), 0) / proxyList.length
      : 0
    return { totalProxies: total, healthyProxies: healthy, degradedProxies: degraded, downProxies: down, avgLatency: Math.round(avg) }
  }

  async function fetchProxies() {
    loading = true
    error = ''
    try {
      const res = await fetch('/api/mihomo/proxy/proxies')
      if (!res.ok) throw new Error($t('proxies.load_error'))
      const data = await res.json()
      proxies = data.proxies || {}
      groups = Object.values(proxies).filter((p: Proxy) => {
        return ['Selector', 'URLTest', 'Fallback', 'LoadBalance'].includes(p.type)
      }).map((p: any) => ({
        name: p.name,
        type: p.type,
        now: p.now || '',
        all: p.all || [],
        alive: p.alive,
        delay: p.history?.[p.history.length - 1]?.delay,
        history: p.history || []
      }))
      // Also enrich proxies with history
      Object.keys(proxies).forEach(name => {
        if (data.proxies[name]?.history) {
          proxies[name].history = data.proxies[name].history
        }
      })
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  async function selectProxy(groupName: string, proxyName: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`/api/mihomo/proxy/proxies/${encodeURIComponent(groupName)}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ name: proxyName })
      })
      if (!res.ok) throw new Error($t('proxies.select_error'))
      await fetchProxies()
    } catch (e: any) {
      error = e.message
    }
  }

  async function testLatency() {
    testingLatency = true
    error = ''
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch('/api/mihomo/proxy/group/UrlTest/delay?url=http://www.gstatic.com/generate_204&timeout=5000', {
        method: 'GET',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      setTimeout(async () => {
        await fetchProxies()
        testingLatency = false
      }, 2000)
    } catch (e: any) {
      error = e.message
      testingLatency = false
    }
  }

  async function testProxyLatency(proxyName: string) {
    testingProxy = proxyName
    error = ''
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch(`/api/mihomo/proxy/proxies/${encodeURIComponent(proxyName)}/delay?url=http://www.gstatic.com/generate_204&timeout=5000`, {
        method: 'GET',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      setTimeout(async () => {
        await fetchProxies()
        testingProxy = ''
      }, 1500)
    } catch (e: any) {
      error = e.message
      testingProxy = ''
    }
  }

  function getGroupTypeLabel(type: string): string {
    const labels: Record<string, string> = {
      'Selector': 'Select',
      'URLTest': 'URL Test',
      'Fallback': 'Fallback',
      'LoadBalance': 'Load Balance'
    }
    return labels[type] || type
  }

  function getProxyDelay(proxyName: string): number | undefined {
    const proxy = proxies[proxyName]
    if (!proxy?.history?.length) return undefined
    return proxy.history[proxy.history.length - 1].delay
  }

  function formatDelay(delay?: number): string {
    if (delay === undefined || delay === 0) return '-'
    return `${delay}ms`
  }

  function getHealthStatus(proxyName: string): 'healthy' | 'degraded' | 'down' {
    const proxy = proxies[proxyName]
    if (!proxy) return 'down'
    if (!proxy.alive) return 'down'
    if ((proxy.delay || 0) >= 300) return 'degraded'
    return 'healthy'
  }

  function getHealthLabel(status: string): string {
    const labels: Record<string, string> = {
      healthy: '✅',
      degraded: '⚠️',
      down: '❌'
    }
    return labels[status] || ''
  }

  function getSparklinePath(history?: { time: string; delay: number }[]): string {
    if (!history || history.length < 2) return ''
    const values = history.map(h => h.delay || 0)
    const max = Math.max(...values, 1)
    const min = Math.min(...values)
    const range = max - min || 1
    const width = 100
    const height = 24
    const step = width / (values.length - 1)
    return values.map((v, i) => {
      const x = i * step
      const y = height - ((v - min) / range) * height
      return `${i === 0 ? 'M' : 'L'}${x},${y}`
    }).join(' ')
  }

  onMount(() => {
    fetchProxies()
    const interval = setInterval(fetchProxies, 10000)
    return () => clearInterval(interval)
  })
</script>

<div class="container">
  <h1>{$t('proxies.title')}</h1>
  <p class="text-secondary mb-3">{$t('proxies.subtitle')}</p>

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <div class="toolbar mb-2">
    <button class="btn btn-secondary" on:click={fetchProxies} disabled={loading}>
      {loading ? $t('app.loading') : '🔄 ' + $t('app.refresh')}
    </button>
    <button class="btn btn-primary" on:click={testLatency} disabled={testingLatency}>
      {testingLatency ? $t('proxies.testing') : '⚡ ' + $t('proxies.test_latency')}
    </button>
    <button class="btn btn-secondary" on:click={() => showObservatory = !showObservatory}>
      📊 {$t('proxies.observatory')}
    </button>
  </div>

  {#if showObservatory}
    {@const stats = computeStats()}
    <div class="card mb-2 observatory-panel">
      <h2>{$t('proxies.observatory_title')}</h2>
      <div class="stats-grid">
        <div class="stat-box">
          <div class="stat-label">{$t('proxies.total')}</div>
          <div class="stat-value">{stats.totalProxies}</div>
        </div>
        <div class="stat-box healthy">
          <div class="stat-label">{$t('proxies.healthy')}</div>
          <div class="stat-value">{stats.healthyProxies}</div>
        </div>
        <div class="stat-box degraded">
          <div class="stat-label">{$t('proxies.degraded')}</div>
          <div class="stat-value">{stats.degradedProxies}</div>
        </div>
        <div class="stat-box down">
          <div class="stat-label">{$t('proxies.down')}</div>
          <div class="stat-value">{stats.downProxies}</div>
        </div>
        <div class="stat-box">
          <div class="stat-label">{$t('proxies.avg_latency')}</div>
          <div class="stat-value">{formatDelay(stats.avgLatency)}</div>
        </div>
      </div>
    </div>
  {/if}

  {#if groups.length === 0 && !loading}
    <div class="card">
      <p class="text-secondary">{$t('proxies.no_proxies')}</p>
    </div>
  {/if}

  <div class="groups-grid">
    {#each groups as group}
      <div class="card group-card">
        <div class="group-header">
          <div>
            <h3>{group.name}</h3>
            <span class="group-type">{getGroupTypeLabel(group.type)}</span>
            {#if group.type === 'Fallback'}
              <span class="fallback-badge">{$t('proxies.fallback_pool')}</span>
            {/if}
          </div>
          <div class="group-delay">
            {#if group.delay}
              <span class="delay-badge">{formatDelay(group.delay)}</span>
            {/if}
          </div>
        </div>

        <div class="proxy-list">
          {#each group.all as proxyName}
            {@const delay = getProxyDelay(proxyName)}
            {@const isActive = group.now === proxyName}
            {@const health = getHealthStatus(proxyName)}
            {@const proxy = proxies[proxyName]}
            
            <div 
              class="proxy-item" 
              class:active={isActive}
              role="button"
              tabindex="0"
              on:click={() => group.type === 'Selector' && selectProxy(group.name, proxyName)}
              on:keydown={(e) => e.key === 'Enter' && group.type === 'Selector' && selectProxy(group.name, proxyName)}
            >
              <div class="proxy-info">
                <span class="proxy-name">{proxyName}</span>
                <span class="health-badge" class:healthy={health === 'healthy'} class:degraded={health === 'degraded'} class:down={health === 'down'}>
                  {getHealthLabel(health)}
                </span>
              </div>
              <div class="proxy-metrics">
                {#if proxy?.history && proxy.history.length > 1}
                  <svg class="sparkline" viewBox="0 0 100 24" preserveAspectRatio="none">
                    <path d={getSparklinePath(proxy.history)} fill="none" stroke="currentColor" stroke-width="1.5" />
                  </svg>
                {/if}
                <span class="proxy-delay">{formatDelay(delay)}</span>
                <button 
                  class="btn-icon latency-btn" 
                  on:click|stopPropagation={() => testProxyLatency(proxyName)}
                  disabled={testingProxy === proxyName}
                  title={$t('proxies.test_single')}
                >
                  {testingProxy === proxyName ? '⏳' : '⚡'}
                </button>
              </div>
            </div>
          {/each}
        </div>

        {#if group.type === 'Fallback'}
          <div class="fallback-info">
            <p class="text-secondary">{$t('proxies.fallback_order')}: {group.all.join(' → ')}</p>
          </div>
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .toolbar {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .observatory-panel .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(100px, 1fr));
    gap: 1rem;
    margin-top: 0.5rem;
  }

  .stat-box {
    padding: 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    text-align: center;
  }

  .stat-box.healthy {
    border-color: var(--success, #28a745);
    background: var(--success-bg, rgba(40, 167, 69, 0.05));
  }

  .stat-box.degraded {
    border-color: var(--warning, #ffc107);
    background: rgba(255, 193, 7, 0.05);
  }

  .stat-box.down {
    border-color: var(--danger, #dc3545);
    background: rgba(220, 53, 69, 0.05);
  }

  .stat-label {
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin-bottom: 0.25rem;
  }

  .stat-value {
    font-weight: 600;
    font-size: 1.25rem;
  }

  .groups-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(380px, 1fr));
    gap: 1.5rem;
  }

  .group-card {
    padding: 1rem;
  }

  .group-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 1rem;
    padding-bottom: 0.75rem;
    border-bottom: 1px solid var(--border);
  }

  .group-header h3 {
    margin: 0 0 0.25rem 0;
    font-size: 1rem;
  }

  .group-type {
    font-size: 0.75rem;
    color: var(--text-secondary);
    background: var(--bg);
    padding: 0.125rem 0.5rem;
    border-radius: 4px;
  }

  .fallback-badge {
    font-size: 0.7rem;
    color: var(--primary);
    background: var(--primary-bg, rgba(0, 123, 255, 0.1));
    padding: 0.125rem 0.5rem;
    border-radius: 4px;
    margin-left: 0.5rem;
  }

  .delay-badge {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--success);
  }

  .proxy-list {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .proxy-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.5rem 0.75rem;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.15s ease;
    font-size: 0.875rem;
    user-select: none;
  }

  .proxy-item:hover {
    background: var(--hover);
  }

  .proxy-item.active {
    background: var(--primary);
    color: white;
    border-color: var(--primary);
  }

  .proxy-info {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .proxy-name {
    font-weight: 500;
  }

  .health-badge {
    font-size: 0.75rem;
  }

  .proxy-metrics {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .sparkline {
    width: 60px;
    height: 18px;
    color: var(--text-secondary);
  }

  .proxy-item.active .sparkline {
    color: rgba(255,255,255,0.7);
  }

  .proxy-delay {
    font-size: 0.75rem;
    color: var(--text-secondary);
    min-width: 45px;
    text-align: right;
  }

  .proxy-item.active .proxy-delay {
    color: rgba(255,255,255,0.8);
  }

  .latency-btn {
    padding: 0.125rem 0.25rem;
    font-size: 0.75rem;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 3px;
    cursor: pointer;
  }

  .proxy-item.active .latency-btn {
    border-color: rgba(255,255,255,0.4);
    color: white;
  }

  .fallback-info {
    margin-top: 0.75rem;
    padding-top: 0.5rem;
    border-top: 1px solid var(--border-light);
    font-size: 0.8rem;
  }
</style>
