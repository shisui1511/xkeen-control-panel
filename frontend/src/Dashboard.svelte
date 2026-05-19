<script lang="ts">
  import { onMount } from 'svelte'
  import { fade } from 'svelte/transition'
  import { t, setLang } from './i18n'
  import { isSidebarOpen } from './stores'
  import Sidebar from './components/Sidebar.svelte'
  import Toast from './components/Toast.svelte'
  import ConfirmDialog from './components/ConfirmDialog.svelte'
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

  let version = $t('app.loading')
  let loading = false
  let currentTab = 'dashboard'
  let theme = document.documentElement.getAttribute('data-theme') || 'light'
  let pwaInstallPrompt: any = null

  // Dashboard live monitoring state
  interface ServiceStatus {
    xkeen: string
    xray: string
    mihomo: string
    connections: number
    xrayVersion: string
    mihomoVersion: string
  }

  let serviceStatus: ServiceStatus = {
    xkeen: 'loading',
    xray: 'loading',
    mihomo: 'loading',
    connections: 0,
    xrayVersion: '',
    mihomoVersion: ''
  }
  let statusError = false
  let statusLoading = true

  interface SystemStats {
    memory: { total: number; used: number; free: number }
    load: [number, number, number]
    uptime: { days: number; hours: number; minutes: number }
    go_runtime: { goroutines: number; heap_alloc: number; heap_sys: number; num_gc: number }
  }

  let systemStats: SystemStats | null = null

  async function fetchLiveStatus() {
    statusError = false
    try {
      const [svcRes, mihomoRes] = await Promise.allSettled([
        fetch('/api/service/status'),
        fetch('/api/mihomo/status')
      ])

      const svcText = svcRes.status === 'fulfilled' && svcRes.value.ok
        ? await svcRes.value.text()
        : ''
      const mihomoText = mihomoRes.status === 'fulfilled' && mihomoRes.value.ok
        ? await mihomoRes.value.text()
        : ''

      // Try to get connection count from mihomo
      let connCount = 0
      try {
        const connRes = await fetch('/api/mihomo/proxy/connections?limit=1')
        if (connRes.ok) {
          const connData = await connRes.json()
          connCount = connData?.connections?.length ?? 0
        }
      } catch (_) {}

      // Get kernel versions and process_status from /api/kernels
      let xrayVer = ''
      let mihomoVer = ''
      let xrayProcessStatus = 'unknown'
      let mihomoProcessStatus = 'unknown'
      try {
        const kernelsRes = await fetch('/api/kernels')
        if (kernelsRes.ok) {
          const kernels = await kernelsRes.json()
          for (const k of kernels) {
            if (k.name === 'xray') {
              xrayVer = k.current_version || ''
              xrayProcessStatus = k.process_status || 'unknown'
            }
            if (k.name === 'mihomo') {
              mihomoVer = k.current_version || ''
              mihomoProcessStatus = k.process_status || 'unknown'
            }
          }
        } else {
          xrayProcessStatus = 'error'
          mihomoProcessStatus = 'error'
        }
      } catch (_) {
        xrayProcessStatus = 'error'
        mihomoProcessStatus = 'error'
      }

      serviceStatus = {
        xkeen: svcText.toLowerCase().includes('running') ? 'running' : svcText || 'unknown',
        xray: xrayProcessStatus,
        mihomo: mihomoProcessStatus,
        connections: connCount,
        xrayVersion: xrayVer,
        mihomoVersion: mihomoVer
      }
    } catch (_) {
      statusError = true
      serviceStatus = { ...serviceStatus, xray: 'error', mihomo: 'error' }
    } finally {
      statusLoading = false
    }
  }

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
      version = $t('app.error')
    }
  }

  async function handleLogout() {
    loading = true
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch('/api/auth/logout', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
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

  function toggleSidebar() {
    isSidebarOpen.update(v => !v)
  }

  function closeSidebar() {
    isSidebarOpen.set(false)
  }

  async function installPWA() {
    if (!pwaInstallPrompt) return
    pwaInstallPrompt.prompt()
    const { outcome } = await pwaInstallPrompt.userChoice
    if (outcome === 'accepted') {
      pwaInstallPrompt = null
    }
  }

  function statusColor(status: string): string {
    if (status === 'running') return 'success'
    if (status === 'stopped' || status === 'not_installed') return 'error'
    if (status === 'error') return 'error'
    if (status === 'loading') return 'warning'
    return 'warning' // unknown
  }

  onMount(() => {
    fetchVersion()
    fetchLiveStatus()
    fetchSystemStats()
    const statusInterval = setInterval(fetchLiveStatus, 10000)
    const statsInterval = setInterval(fetchSystemStats, 5000)
    window.addEventListener('beforeinstallprompt', (e: Event) => {
      e.preventDefault()
      pwaInstallPrompt = e
    })
    return () => {
      clearInterval(statusInterval)
      clearInterval(statsInterval)
    }
  })
</script>

<div class="dashboard-layout">
  <!-- Mobile header bar -->
  <header class="mobile-header">
    <button
      class="burger-btn"
      on:click={toggleSidebar}
      aria-label={$t('nav.open_menu')}
      title={$t('nav.open_menu')}
    >
      <svg width="22" height="22" viewBox="0 0 22 22" fill="none" aria-hidden="true">
        <rect y="3" width="22" height="2.5" rx="1.25" fill="currentColor"/>
        <rect y="9.75" width="22" height="2.5" rx="1.25" fill="currentColor"/>
        <rect y="16.5" width="22" height="2.5" rx="1.25" fill="currentColor"/>
      </svg>
    </button>
    <span style="font-weight: 600; font-size: 16px;">⚡ XKeen CP</span>
    <span style="width: 34px;"></span>
  </header>

  <!-- Off-canvas overlay (mobile only) -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="sidebar-overlay"
    class:hidden={!$isSidebarOpen}
    on:click={closeSidebar}
    role="button"
    tabindex="0"
    aria-label={$t('nav.close_menu')}
    title={$t('nav.close_menu')}
  ></div>

  <!-- Sidebar -->
  <div
    class="sidebar"
    class:sidebar-open={$isSidebarOpen}
    style="display: flex; flex-direction: column;"
  >
    <Sidebar
      {currentTab}
      onSwitchTab={switchTab}
      {theme}
      onToggleTheme={toggleTheme}
      onLogout={handleLogout}
      {loading}
      {pwaInstallPrompt}
      onInstallPWA={installPWA}
    />
  </div>

  <!-- Main content area -->
  <div class="main-content">
    {#if currentTab === 'dashboard'}
      <div class="container" transition:fade={{ duration: 150 }}>
        <h1>{$t('nav.dashboard')}</h1>
        <p class="text-secondary mb-3">{$t('dash.welcome')}</p>

        <!-- Live Service Status card -->
        <div class="card mb-2">
          <h2>{$t('dash.service_status')}</h2>
          {#if statusLoading}
            <p class="text-secondary">{$t('app.loading')}</p>
          {:else if statusError}
            <div class="status-error-row">
              <span>⚠️ {$t('dash.status_error')}</span>
              <button
                class="btn btn-secondary"
                style="padding: 4px 12px; font-size: 12px;"
                on:click={fetchLiveStatus}
                title={$t('app.refresh')}
              >
                ↺ {$t('app.refresh')}
              </button>
            </div>
          {:else}
            <div class="status-badges-row">
              <div class="status-badge-item">
                <span class="status-dot {statusColor(serviceStatus.xkeen)}"></span>
                <span class="status-badge-label">XKeen</span>
                <span class="status-badge-value status-{statusColor(serviceStatus.xkeen)}">
                  {serviceStatus.xkeen === 'running' ? $t('app.running') : $t('app.stop')}
                </span>
              </div>
              <div class="status-badge-item">
                <span class="status-dot {statusColor(serviceStatus.xray)}"></span>
                <span class="status-badge-label">Xray</span>
                <span class="status-badge-value status-{statusColor(serviceStatus.xray)}">
                  {$t('kernel.status.' + (serviceStatus.xray || 'unknown'))}
                  {#if serviceStatus.xrayVersion && serviceStatus.xray !== 'not_installed'}
                    <span class="version-badge">{serviceStatus.xrayVersion}</span>
                  {/if}
                </span>
              </div>
              <div class="status-badge-item">
                <span class="status-dot {statusColor(serviceStatus.mihomo)}"></span>
                <span class="status-badge-label">Mihomo</span>
                <span class="status-badge-value status-{statusColor(serviceStatus.mihomo)}">
                  {$t('kernel.status.' + (serviceStatus.mihomo || 'unknown'))}
                  {#if serviceStatus.mihomoVersion && serviceStatus.mihomo !== 'not_installed'}
                    <span class="version-badge">{serviceStatus.mihomoVersion}</span>
                  {/if}
                </span>
              </div>
              <div class="status-badge-item">
                <span class="status-dot {serviceStatus.connections > 0 ? 'success' : 'warning'}"></span>
                <span class="status-badge-label">{$t('dash.connections')}</span>
                <span class="status-badge-value">{serviceStatus.connections}</span>
              </div>
            </div>
          {/if}
        </div>

        <div class="card mb-2">
          <h2>{$t('dash.system_info')}</h2>
          <p><strong>{$t('app.version')}:</strong> {version}</p>
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
            <button class="btn btn-secondary" on:click={() => switchTab('proxies')} title={$t('nav.proxies')}>
              🌐 {$t('nav.proxies')}
            </button>
            <button class="btn btn-secondary" on:click={() => switchTab('subscriptions')} title={$t('nav.subscriptions')}>
              📡 {$t('nav.subscriptions')}
            </button>
            <button class="btn btn-secondary" on:click={() => switchTab('editor')} title={$t('nav.editor')}>
              📝 {$t('nav.editor')}
            </button>
            <button class="btn btn-secondary" on:click={() => switchTab('logs')} title={$t('nav.logs')}>
              📋 {$t('nav.logs')}
            </button>
          </div>
        </div>
      </div>
    {:else if currentTab === 'editor'}
      <div transition:fade={{ duration: 150 }}>
        <Editor onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'logs'}
      <div transition:fade={{ duration: 150 }}>
        <Logs />
      </div>
    {:else if currentTab === 'proxies'}
      <div transition:fade={{ duration: 150 }}>
        <Proxies />
      </div>
    {:else if currentTab === 'connections'}
      <div transition:fade={{ duration: 150 }}>
        <Connections />
      </div>
    {:else if currentTab === 'rules'}
      <div transition:fade={{ duration: 150 }}>
        <Rules />
      </div>
    {:else if currentTab === 'traffic'}
      <div transition:fade={{ duration: 150 }}>
        <Traffic />
      </div>
    {:else if currentTab === 'subscriptions'}
      <div transition:fade={{ duration: 150 }}>
        <Subscriptions onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'services'}
      <div transition:fade={{ duration: 150 }}>
        <Services onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'smartproxy'}
      <div transition:fade={{ duration: 150 }}>
        <SmartProxy onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'trafficquotas'}
      <div transition:fade={{ duration: 150 }}>
        <TrafficQuotas onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'dat'}
      <div transition:fade={{ duration: 150 }}>
        <DATManager onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'console'}
      <div transition:fade={{ duration: 150 }}>
        <Console onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'network'}
      <div transition:fade={{ duration: 150 }}>
        <NetworkTools onSwitchTab={switchTab} />
      </div>
    {:else if currentTab === 'settings'}
      <div transition:fade={{ duration: 150 }}>
        <Settings onSwitchTab={switchTab} />
      </div>
    {/if}
  </div>
</div>

<Toast />
<ConfirmDialog />

<style>
  .quick-actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  /* Live status badges */
  .status-badges-row {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    margin-top: 4px;
  }

  .status-badge-item {
    display: flex;
    align-items: center;
    gap: 6px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 6px 12px;
    font-size: 13px;
  }

  .status-badge-label {
    font-weight: 600;
    color: var(--fg-primary);
  }

  .status-badge-value {
    color: var(--fg-secondary);
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .status-success {
    color: var(--success);
  }

  .status-error {
    color: var(--danger);
  }

  .status-warning {
    color: var(--warning);
  }

  .version-badge {
    font-size: 11px;
    background: var(--border);
    border-radius: 4px;
    padding: 1px 5px;
    font-family: monospace;
    color: var(--fg-secondary);
  }

  .status-error-row {
    display: flex;
    align-items: center;
    gap: 12px;
    color: var(--danger);
    padding: 8px 0;
  }
</style>
