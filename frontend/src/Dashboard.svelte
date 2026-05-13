<script lang="ts">
  import { onMount } from 'svelte'
  import { t, setLang } from './i18n'
  import Editor from './Editor.svelte'
  import Logs from './Logs.svelte'
  import Services from './Services.svelte'
  import Settings from './Settings.svelte'
  import Proxies from './Proxies.svelte'
  import Connections from './Connections.svelte'
  import Rules from './Rules.svelte'
  import Traffic from './Traffic.svelte'
  import Subscriptions from './Subscriptions.svelte'
  import NetworkTools from './NetworkTools.svelte'
  import SmartProxy from './SmartProxy.svelte'
  import TrafficQuotas from './TrafficQuotas.svelte'
  import DATManager from './DATManager.svelte'
  import Console from './Console.svelte'

  let version = 'loading...'
  let loading = false
  let currentTab = 'dashboard'
  let theme = document.documentElement.getAttribute('data-theme') || 'light'
  let pwaInstallPrompt: any = null

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
    window.addEventListener('beforeinstallprompt', (e: Event) => {
      e.preventDefault()
      pwaInstallPrompt = e
    })
    return () => clearInterval(interval)
  })

  async function installPWA() {
    if (!pwaInstallPrompt) return
    pwaInstallPrompt.prompt()
    const { outcome } = await pwaInstallPrompt.userChoice
    if (outcome === 'accepted') {
      pwaInstallPrompt = null
    }
  }
</script>

<div class="dashboard-layout">
  <div class="sidebar" style="display: flex; flex-direction: column;">
    <div class="sidebar-logo">⚡ XKeen CP</div>
    <nav style="flex: 1; overflow-y: auto;">
      <button class="nav-item" class:active={currentTab === 'dashboard'} on:click={() => switchTab('dashboard')}>
        📊 {$t('nav.dashboard')}
      </button>
      <button class="nav-item" class:active={currentTab === 'editor'} on:click={() => switchTab('editor')}>
        📝 {$t('nav.editor')}
      </button>
      <button class="nav-item" class:active={currentTab === 'logs'} on:click={() => switchTab('logs')}>
        📋 {$t('nav.logs')}
      </button>
      <button class="nav-item" class:active={currentTab === 'proxies'} on:click={() => switchTab('proxies')}>
        🌐 {$t('nav.proxies')}
      </button>
      <button class="nav-item" class:active={currentTab === 'connections'} on:click={() => switchTab('connections')}>
        🔗 {$t('nav.connections')}
      </button>
      <button class="nav-item" class:active={currentTab === 'rules'} on:click={() => switchTab('rules')}>
        📋 {$t('nav.rules')}
      </button>
      <button class="nav-item" class:active={currentTab === 'traffic'} on:click={() => switchTab('traffic')}>
        📈 {$t('nav.traffic')}
      </button>
      <button class="nav-item" class:active={currentTab === 'subscriptions'} on:click={() => switchTab('subscriptions')}>
        📡 {$t('nav.subscriptions')}
      </button>
      <button class="nav-item" class:active={currentTab === 'services'} on:click={() => switchTab('services')}>🚀 {$t('nav.services')}</button>
      <button class="nav-item" class:active={currentTab === 'smartproxy'} on:click={() => switchTab('smartproxy')}>⚡ {$t('nav.smartproxy')}</button>
      <button class="nav-item" class:active={currentTab === 'trafficquotas'} on:click={() => switchTab('trafficquotas')}>📊 {$t('nav.trafficquotas')}</button>
      <button class="nav-item" class:active={currentTab === 'dat'} on:click={() => switchTab('dat')}>🌍 {$t('nav.dat')}</button>
      <button class="nav-item" class:active={currentTab === 'console'} on:click={() => switchTab('console')}>💻 {$t('nav.console')}</button>
      <button class="nav-item" class:active={currentTab === 'network'} on:click={() => switchTab('network')}>🌐 {$t('nav.network')}</button>
      <button class="nav-item" class:active={currentTab === 'settings'} on:click={() => switchTab('settings')}>⚙️ {$t('nav.settings')}</button>
    </nav>
    <div style="border-top: 1px solid var(--border); padding: 0.5rem 0;">
      {#if pwaInstallPrompt}
        <button class="nav-item" on:click={installPWA}>
          📲 {$t('nav.install_pwa')}
        </button>
      {/if}
      <button class="nav-item" on:click={toggleTheme}>
        {theme === 'dark' ? '☀️' : '🌙'} {theme === 'dark' ? $t('nav.theme_light') : $t('nav.theme_dark')}
      </button>
      <button class="nav-item" on:click={handleLogout} disabled={loading}>
        🚪 {loading ? $t('auth.logging_out') : $t('auth.logout')}
      </button>
    </div>
  </div>

  <div class="main-content">
    {#if currentTab === 'dashboard'}
      <div class="container">
        <h1>{$t('nav.dashboard')}</h1>
        <p class="text-secondary mb-3">{$t('dash.welcome')}</p>

        <div class="card mb-2">
          <h2>{$t('dash.system_info')}</h2>
          <p><strong>{$t('app.version')}:</strong> {version}</p>
          <p><strong>{$t('app.status')}:</strong> <span class="status-dot success"></span> {$t('app.running')}</p>
        </div>

        {#if systemStats}
          <div class="card mb-2">
            <h2>{$t('dash.system_stats')}</h2>
            <div class="stats-grid">
              <div class="stat-box">
                <div class="stat-label">{$t('dash.ram')}</div>
                <div class="stat-value">{formatBytes(systemStats.memory.used)} / {formatBytes(systemStats.memory.total)}</div>
                <div class="stat-bar">
                  <div class="stat-bar-fill" style="width: {(systemStats.memory.used / systemStats.memory.total * 100).toFixed(1)}%"></div>
                </div>
              </div>
              <div class="stat-box">
                <div class="stat-label">{$t('dash.load')}</div>
                <div class="stat-value">{systemStats.load[0].toFixed(2)}</div>
              </div>
              <div class="stat-box">
                <div class="stat-label">{$t('dash.uptime')}</div>
                <div class="stat-value">{systemStats.uptime.days}d {systemStats.uptime.hours}h {systemStats.uptime.minutes}m</div>
              </div>
              <div class="stat-box">
                <div class="stat-label">{$t('dash.goroutines')}</div>
                <div class="stat-value">{systemStats.go_runtime.goroutines}</div>
              </div>
            </div>
          </div>
        {/if}

        <div class="card mb-2">
          <h2>{$t('dash.quick_actions')}</h2>
          <div class="quick-actions">
            <button class="btn btn-secondary" on:click={() => switchTab('proxies')}>
              🌐 {$t('nav.proxies')}
            </button>
            <button class="btn btn-secondary" on:click={() => switchTab('subscriptions')}>
              📡 {$t('nav.subscriptions')}
            </button>
            <button class="btn btn-secondary" on:click={() => switchTab('editor')}>
              📝 {$t('nav.editor')}
            </button>
            <button class="btn btn-secondary" on:click={() => switchTab('logs')}>
              📋 {$t('nav.logs')}
            </button>
          </div>
        </div>

        <style>
          .quick-actions {
            display: flex;
            gap: 0.5rem;
            flex-wrap: wrap;
          }
        </style>

      </div>
    {:else if currentTab === 'editor'}
      <Editor onSwitchTab={switchTab} />
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
    {:else if currentTab === 'subscriptions'}
      <Subscriptions onSwitchTab={switchTab} />
    {:else if currentTab === 'services'}
      <Services onSwitchTab={switchTab} />
    {:else if currentTab === 'smartproxy'}
      <SmartProxy onSwitchTab={switchTab} />
    {:else if currentTab === 'trafficquotas'}
      <TrafficQuotas onSwitchTab={switchTab} />
    {:else if currentTab === 'dat'}
      <DATManager onSwitchTab={switchTab} />
    {:else if currentTab === 'console'}
      <Console onSwitchTab={switchTab} />
    {:else if currentTab === 'network'}
      <NetworkTools onSwitchTab={switchTab} />
    {:else if currentTab === 'settings'}
      <Settings onSwitchTab={switchTab} />
    {/if}
  </div>
</div>
