<script lang="ts">
  import { onMount } from 'svelte'

  interface Proxy {
    name: string
    type: string
    alive?: boolean
    delay?: number
    history?: { delay: number }[]
  }

  interface ProxyGroup {
    name: string
    type: string
    now: string
    all: string[]
    alive?: boolean
    delay?: number
  }

  let groups: ProxyGroup[] = []
  let proxies: Record<string, Proxy> = {}
  let loading = false
  let error = ''
  let testingLatency = false

  async function fetchProxies() {
    loading = true
    error = ''
    
    try {
      const res = await fetch('/api/mihomo/proxy/proxies')
      if (!res.ok) throw new Error('Failed to load proxies')
      
      const data = await res.json()
      proxies = data.proxies || {}
      
      // Extract groups (select, url-test, fallback, load-balance)
      groups = Object.values(proxies).filter((p: Proxy) => {
        return ['Selector', 'URLTest', 'Fallback', 'LoadBalance'].includes(p.type)
      }).map((p: any) => ({
        name: p.name,
        type: p.type,
        now: p.now || '',
        all: p.all || [],
        alive: p.alive,
        delay: p.history?.[p.history.length - 1]?.delay
      }))
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
      
      if (!res.ok) throw new Error('Failed to select proxy')
      
      // Refresh proxy list
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
      
      // Wait a bit for tests to complete
      setTimeout(async () => {
        await fetchProxies()
        testingLatency = false
      }, 2000)
    } catch (e: any) {
      error = e.message
      testingLatency = false
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

  onMount(() => {
    fetchProxies()
  })
</script>

<div class="container">
  <h1>Proxy Groups</h1>
  <p class="text-secondary mb-3">Управление прокси-группами Mihomo</p>

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <div class="toolbar mb-2">
    <button class="btn btn-secondary" on:click={fetchProxies} disabled={loading}>
      {loading ? 'Загрузка...' : '🔄 Обновить'}
    </button>
    <button class="btn btn-primary" on:click={testLatency} disabled={testingLatency}>
      {testingLatency ? 'Тестирование...' : '⚡ Latency Test'}
    </button>
  </div>

  {#if groups.length === 0 && !loading}
    <div class="card">
      <p class="text-secondary">Нет доступных proxy groups. Убедитесь, что Mihomo запущен и настроен.</p>
    </div>
  {/if}

  <div class="groups-grid">
    {#each groups as group}
      <div class="card group-card">
        <div class="group-header">
          <div>
            <h3>{group.name}</h3>
            <span class="group-type">{getGroupTypeLabel(group.type)}</span>
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
            
            <button 
              class="proxy-item" 
              class:active={isActive}
              on:click={() => group.type === 'Selector' && selectProxy(group.name, proxyName)}
            >
              <span class="proxy-name">{proxyName}</span>
              <span class="proxy-delay">{formatDelay(delay)}</span>
            </button>
          {/each}
        </div>
      </div>
    {/each}
  </div>
</div>

<style>
  .toolbar {
    display: flex;
    gap: 0.5rem;
  }

  .groups-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(350px, 1fr));
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
  }

  .proxy-item:hover {
    background: var(--hover);
  }

  .proxy-item.active {
    background: var(--primary);
    color: white;
    border-color: var(--primary);
  }

  .proxy-name {
    font-weight: 500;
  }

  .proxy-delay {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .proxy-item.active .proxy-delay {
    color: rgba(255,255,255,0.8);
  }
</style>
