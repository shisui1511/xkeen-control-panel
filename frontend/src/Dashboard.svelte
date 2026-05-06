<script lang="ts">
  import { onMount } from 'svelte'
  import Editor from './Editor.svelte'
  import Logs from './Logs.svelte'
  import Services from './Services.svelte'
  import Settings from './Settings.svelte'
  import Proxies from './Proxies.svelte'
  import Connections from './Connections.svelte'
  import Rules from './Rules.svelte'
  import Traffic from './Traffic.svelte'

  let version = 'loading...'
  let loading = false
  let currentTab = 'dashboard'
  let theme = document.documentElement.getAttribute('data-theme') || 'light'

  interface SystemStats {
    memory: { total: number; used: number; free: number }
    load: [number, number, number]
    uptime: { days: number; hours: number; minutes: number }
    go_runtime: { goroutines: number; heap_alloc: number; heap_sys: number; num_gc: number }
  }

  let systemStats: SystemStats | null = null

  async function fetchSystemStats() {
    try {
      const res = await fetch('/api/system/stats')
      if (res.ok) {
        systemStats = await res.json()
      }
    } catch (e) {
      // ignore
    }
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
  }

  function toggleTheme() {
    theme = theme === 'dark' ? 'light' : 'dark'
    document.documentElement.setAttribute('data-theme', theme)
    localStorage.setItem('theme', theme)
  }

  async function fetchVersion() {
    try {
      const res = await fetch('/api/version')
      const data = await res.json()
      version = data.version
    } catch (e) {
      version = 'error'
    }
  }

  async function handleLogout() {
    loading = true
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch('/api/auth/logout', {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      })
      localStorage.removeItem('csrf_token')
      window.location.href = '/'
    } catch (e) {
      console.error('Logout error:', e)
    } finally {
      loading = false
    }
  }

  function switchTab(tab: string) {
    currentTab = tab
  }

  onMount(() => {
    fetchVersion()
    fetchSystemStats()
    const interval = setInterval(fetchSystemStats, 5000)
    return () => clearInterval(interval)
  })
</script>

<div class="dashboard-layout">
  <div class="sidebar" style="display: flex; flex-direction: column;">
    <div class="sidebar-logo">⚡ XKeen CP</div>
    <nav style="flex: 1; overflow-y: auto;">
      <button class="nav-item" class:active={currentTab === 'dashboard'} on:click={() => switchTab('dashboard')}>
        📊 Dashboard
      </button>
      <button class="nav-item" class:active={currentTab === 'editor'} on:click={() => switchTab('editor')}>
        📝 Editor
      </button>
      <button class="nav-item" class:active={currentTab === 'logs'} on:click={() => switchTab('logs')}>
        📋 Logs
      </button>
      <button class="nav-item" class:active={currentTab === 'proxies'} on:click={() => switchTab('proxies')}>
        🌐 Proxies
      </button>
      <button class="nav-item" class:active={currentTab === 'connections'} on:click={() => switchTab('connections')}>
        🔗 Connections
      </button>
      <button class="nav-item" class:active={currentTab === 'rules'} on:click={() => switchTab('rules')}>
        📋 Rules
      </button>
      <button class="nav-item" class:active={currentTab === 'traffic'} on:click={() => switchTab('traffic')}>
        📈 Traffic
      </button>
      <button class="nav-item" class:active={currentTab === 'services'} on:click={() => switchTab('services')}>
        🚀 Services
      </button>
      <button class="nav-item" class:active={currentTab === 'settings'} on:click={() => switchTab('settings')}>
        ⚙️ Settings
      </button>
    </nav>
    <div style="border-top: 1px solid var(--border); padding: 0.5rem 0;">
      <button class="nav-item" on:click={toggleTheme}>
        {theme === 'dark' ? '☀️' : '🌙'} {theme === 'dark' ? 'Светлая тема' : 'Тёмная тема'}
      </button>
      <button class="nav-item" on:click={handleLogout} disabled={loading}>
        🚪 {loading ? 'Выход...' : 'Выйти'}
      </button>
    </div>
  </div>

  <div class="main-content">
    {#if currentTab === 'dashboard'}
      <div class="container">
        <h1>Dashboard</h1>
        <p class="text-secondary mb-3">Добро пожаловать в панель управления XKeen</p>

        <div class="card mb-2">
          <h2>Информация о системе</h2>
          <p><strong>Версия:</strong> {version}</p>
          <p><strong>Статус:</strong> <span class="status-dot success"></span> Работает</p>
          <p class="text-secondary">v0.3.0 — Mihomo Dashboard</p>
        </div>

        {#if systemStats}
          <div class="card mb-2">
            <h2>System Stats</h2>
            <div class="stats-grid">
              <div class="stat-box">
                <div class="stat-label">RAM</div>
                <div class="stat-value">{formatBytes(systemStats.memory.used)} / {formatBytes(systemStats.memory.total)}</div>
                <div class="stat-bar">
                  <div class="stat-bar-fill" style="width: {(systemStats.memory.used / systemStats.memory.total * 100).toFixed(1)}%"></div>
                </div>
              </div>
              <div class="stat-box">
                <div class="stat-label">Load Average</div>
                <div class="stat-value">{systemStats.load[0].toFixed(2)}</div>
              </div>
              <div class="stat-box">
                <div class="stat-label">Uptime</div>
                <div class="stat-value">{systemStats.uptime.days}d {systemStats.uptime.hours}h {systemStats.uptime.minutes}m</div>
              </div>
              <div class="stat-box">
                <div class="stat-label">Go Goroutines</div>
                <div class="stat-value">{systemStats.go_runtime.goroutines}</div>
              </div>
            </div>
          </div>
        {/if}

        <div class="card mb-2">
          <h2>Релизы</h2>
          <ul style="list-style: none; padding-left: 0;">
            <li>✅ v0.1.0 — Auth + Design Foundation</li>
            <li>✅ v0.2.0 — Config Editor + Unified Logs</li>
            <li>✅ v0.3.0 — Mihomo Dashboard (proxies, connections, rules, traffic)</li>
            <li>⏳ v0.4.0 — Subscriptions + Smart Proxy Manager</li>
            <li>⏳ v0.5.0 — Network Tools + Notifications</li>
          </ul>
        </div>

      </div>
    {:else if currentTab === 'editor'}
      <Editor />
    {:else if currentTab === 'logs'}
      <Logs />
    {:else if currentTab === 'proxies'}
      <Proxies />
    {:else if currentTab === 'connections'}
      <Connections />
    {:else if currentTab === 'rules'}
      <Rules />
    {:else if currentTab === 'traffic'}
      <Traffic />
    {:else if currentTab === 'services'}
      <Services />
    {:else if currentTab === 'settings'}
      <Settings />
    {/if}
  </div>
</div>
