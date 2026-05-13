<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { t } from './i18n'

  interface Kernel {
    name: string
    display_name: string
    binary_path: string
    current_version: string
    latest_version: string
    has_update: boolean
    channel: string
    status: string
    message: string
  }

  let xkeenStatus = ''
  let mihomoStatus = ''
  let loading = false
  let actionLoading: Record<string, boolean> = {}
  
  let kernels: Kernel[] = []
  let statusIntervals: Record<string, ReturnType<typeof setInterval>> = {}

  async function fetchStatus() {
    try {
      const [xkeenRes, mihomoRes] = await Promise.all([
        fetch('/api/service/status'),
        fetch('/api/mihomo/status')
      ])
      xkeenStatus = xkeenRes.ok ? await xkeenRes.text() : $t('app.error')
      mihomoStatus = mihomoRes.ok ? await mihomoRes.text() : $t('app.error')
    } catch (e) {
      xkeenStatus = $t('app.unavailable')
      mihomoStatus = $t('app.unavailable')
    }
  }

  async function fetchKernels() {
    try {
      const res = await fetch('/api/kernels')
      if (res.ok) {
        kernels = await res.json()
        // Start polling for kernels that are not idle
        kernels.forEach(k => {
          if (k.status !== 'idle' && !statusIntervals[k.name]) {
            startPolling(k.name)
          }
        })
      }
    } catch (e) {}
  }

  async function controlService(service: 'xkeen' | 'mihomo', action: string) {
    const key = `${service}-${action}`
    actionLoading[key] = true
    
    try {
      const endpoint = service === 'xkeen' ? '/api/service/control' : '/api/mihomo/control'
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`${endpoint}?action=${action}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      
      const text = await res.text()
      if (!res.ok) throw new Error(text)
      
      await fetchStatus()
    } catch (e: any) {
      alert(`${$t('svc.action_error')}: ${e.message}`)
    } finally {
      actionLoading[key] = false
    }
  }

  async function checkKernelUpdate(name: string) {
    try {
      await fetch(`/api/kernels/${name}/check`, { method: 'POST' })
      startPolling(name)
    } catch (e) {}
  }

  async function installKernel(name: string) {
    try {
      await fetch(`/api/kernels/${name}/install`, { method: 'POST' })
      startPolling(name)
    } catch (e) {}
  }

  async function setKernelChannel(name: string, channel: string) {
    try {
      await fetch(`/api/kernels/${name}/channel`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ channel })
      })
      await fetchKernels()
    } catch (e) {}
  }

  async function fetchKernelStatus(name: string) {
    try {
      const res = await fetch(`/api/kernels/${name}/status`)
      if (res.ok) {
        const data = await res.json()
        const idx = kernels.findIndex(k => k.name === name)
        if (idx >= 0) {
          kernels[idx] = { ...kernels[idx], ...data }
          kernels = [...kernels]
        }
        if (data.status === 'idle' || data.status === 'done' || data.status === 'failed') {
          clearInterval(statusIntervals[name])
          delete statusIntervals[name]
        }
      }
    } catch (e) {
      clearInterval(statusIntervals[name])
      delete statusIntervals[name]
    }
  }

  function startPolling(name: string) {
    if (statusIntervals[name]) clearInterval(statusIntervals[name])
    fetchKernelStatus(name)
    statusIntervals[name] = setInterval(() => fetchKernelStatus(name), 2000)
  }

  function getKernel(name: string) {
    return kernels.find(k => k.name === name)
  }

  $: xray = getKernel('xray')
  $: mihomo = getKernel('mihomo')

  onMount(() => {
    fetchStatus()
    fetchKernels()
    const interval = setInterval(fetchStatus, 10000)
    return () => {
      clearInterval(interval)
      Object.values(statusIntervals).forEach(clearInterval)
    }
  })
</script>

<div class="container">
  <h1>{$t('svc.title')}</h1>
  <p class="text-secondary mb-3">{$t('svc.subtitle')}</p>

  <div class="services-grid">
    <!-- XKeen Card -->
    <div class="card">
      <div class="service-header">
        <div class="title-group">
          <h2>{$t('svc.xkeen')}</h2>
          <span class="version-tag">Tool</span>
        </div>
        <span class="status-badge" class:running={xkeenStatus.includes('running') || xkeenStatus.includes('работает') || xkeenStatus.includes('активен')}>
          {xkeenStatus || $t('app.loading')}
        </span>
      </div>
      <p class="text-secondary mb-2">{$t('svc.xkeen_desc')}</p>
      <div class="actions">
        <button class="btn btn-primary" on:click={() => controlService('xkeen', 'start')} disabled={actionLoading['xkeen-start']}>
          {actionLoading['xkeen-start'] ? $t('svc.starting') : '▶ ' + $t('app.start')}
        </button>
        <button class="btn btn-secondary" on:click={() => controlService('xkeen', 'stop')} disabled={actionLoading['xkeen-stop']}>
          {actionLoading['xkeen-stop'] ? $t('svc.stopping') : '⏹ ' + $t('app.stop')}
        </button>
        <button class="btn btn-secondary" on:click={() => controlService('xkeen', 'restart')} disabled={actionLoading['xkeen-restart']}>
          {actionLoading['xkeen-restart'] ? $t('svc.restarting') : '🔄 ' + $t('app.restart')}
        </button>
      </div>
    </div>

    <!-- Xray Card -->
    <div class="card">
      <div class="service-header">
        <div class="title-group">
          <h2>{$t('svc.xray')}</h2>
          {#if xray}
            <span class="version-tag">{xray.current_version === 'not installed' ? $t('kernels.not_installed') : 'v' + xray.current_version}</span>
          {/if}
        </div>
        <span class="status-badge" class:running={xkeenStatus.includes('running') || xkeenStatus.includes('работает') || xkeenStatus.includes('активен')}>
          {xkeenStatus || $t('app.loading')}
        </span>
      </div>
      <p class="text-secondary mb-2">{$t('svc.xray_desc')}</p>
      
      {#if xray}
        <div class="kernel-details mb-2">
          <div class="detail-row">
            <span>{$t('kernels.channel')}:</span>
            <select class="small-select" value={xray.channel} on:change={(e) => setKernelChannel('xray', e.currentTarget.value)}>
              <option value="stable">Stable</option>
              <option value="preview">Preview</option>
            </select>
          </div>
          {#if xray.latest_version && xray.has_update}
            <div class="detail-row update-available">
              <span>{$t('kernels.latest')}: v{xray.latest_version}</span>
              <button class="btn-link" on:click={() => installKernel('xray')} disabled={xray.status !== 'idle'}>
                {xray.status === 'downloading' || xray.status === 'installing' ? $t('kernels.installing') : $t('kernels.install')}
              </button>
            </div>
          {/if}
          {#if xray.status !== 'idle'}
            <div class="status-msg">{xray.message || xray.status}</div>
          {/if}
        </div>
      {/if}

      <div class="actions">
        <span class="text-secondary" style="font-size: 0.85rem;">{$t('svc.xray_managed')}</span>
        <button class="btn btn-icon-small" on:click={() => checkKernelUpdate('xray')} title={$t('kernels.check_update')} disabled={xray?.status !== 'idle'}>
          {xray?.status === 'checking' ? '...' : '🔄'}
        </button>
      </div>
    </div>

    <!-- Mihomo Card -->
    <div class="card">
      <div class="service-header">
        <div class="title-group">
          <h2>{$t('svc.mihomo')}</h2>
          {#if mihomo}
            <span class="version-tag">{mihomo.current_version === 'not installed' ? $t('kernels.not_installed') : 'v' + mihomo.current_version}</span>
          {/if}
        </div>
        <span class="status-badge" class:running={mihomoStatus.includes('running') || mihomoStatus.includes('pid')}>
          {mihomoStatus || $t('app.loading')}
        </span>
      </div>
      <p class="text-secondary mb-2">{$t('svc.mihomo_desc')}</p>

      {#if mihomo}
        <div class="kernel-details mb-2">
          <div class="detail-row">
            <span>{$t('kernels.channel')}:</span>
            <select class="small-select" value={mihomo.channel} on:change={(e) => setKernelChannel('mihomo', e.currentTarget.value)}>
              <option value="stable">Stable</option>
              <option value="preview">Preview</option>
            </select>
          </div>
          {#if mihomo.latest_version && mihomo.has_update}
            <div class="detail-row update-available">
              <span>{$t('kernels.latest')}: v{mihomo.latest_version}</span>
              <button class="btn-link" on:click={() => installKernel('mihomo')} disabled={mihomo.status !== 'idle'}>
                {mihomo.status === 'downloading' || mihomo.status === 'installing' ? $t('kernels.installing') : $t('kernels.install')}
              </button>
            </div>
          {/if}
          {#if mihomo.status !== 'idle'}
            <div class="status-msg">{mihomo.message || mihomo.status}</div>
          {/if}
        </div>
      {/if}

      <div class="actions">
        <button class="btn btn-primary" on:click={() => controlService('mihomo', 'start')} disabled={actionLoading['mihomo-start']}>
          {actionLoading['mihomo-start'] ? $t('svc.starting') : '▶ ' + $t('app.start')}
        </button>
        <button class="btn btn-secondary" on:click={() => controlService('mihomo', 'stop')} disabled={actionLoading['mihomo-stop']}>
          {actionLoading['mihomo-stop'] ? $t('svc.stopping') : '⏹ ' + $t('app.stop')}
        </button>
        <button class="btn btn-secondary" on:click={() => controlService('mihomo', 'restart')} disabled={actionLoading['mihomo-restart']}>
          {actionLoading['mihomo-restart'] ? $t('svc.restarting') : '🔄 ' + $t('app.restart')}
        </button>
        <button class="btn btn-icon-small" on:click={() => checkKernelUpdate('mihomo')} title={$t('kernels.check_update')} disabled={mihomo?.status !== 'idle'}>
          {mihomo?.status === 'checking' ? '...' : '🔄'}
        </button>
      </div>
    </div>
  </div>

  <div class="card mt-2">
    <h3>{$t('svc.refresh_status')}</h3>
    <button class="btn btn-secondary" on:click={() => { fetchStatus(); fetchKernels(); }} disabled={loading}>
      {loading ? $t('app.loading') : '🔄 ' + $t('app.refresh')}
    </button>
  </div>
</div>

<style>
  .services-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 1.5rem;
    margin-bottom: 1.5rem;
  }

  .service-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.75rem;
  }

  .title-group {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .service-header h2 {
    margin: 0;
  }

  .version-tag {
    font-size: 0.75rem;
    font-family: monospace;
    padding: 0.1rem 0.4rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-secondary);
  }

  .status-badge {
    padding: 0.25rem 0.75rem;
    border-radius: 999px;
    font-size: 0.75rem;
    font-weight: 500;
    background: var(--bg-page);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
  }

  .status-badge.running {
    background: rgba(16, 185, 129, 0.1);
    color: var(--success);
    border-color: var(--success);
  }

  .kernel-details {
    background: var(--bg);
    padding: 0.75rem;
    border-radius: var(--radius);
    font-size: 0.85rem;
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
  }

  .detail-row:last-child {
    margin-bottom: 0;
  }

  .update-available {
    color: var(--warning);
    font-weight: 500;
  }

  .status-msg {
    margin-top: 0.5rem;
    font-style: italic;
    color: var(--primary);
  }

  .small-select {
    padding: 0.1rem 0.3rem;
    font-size: 0.8rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--card-bg);
    color: var(--text);
  }

  .btn-link {
    background: none;
    border: none;
    color: var(--primary);
    text-decoration: underline;
    cursor: pointer;
    font-size: 0.8rem;
    padding: 0;
  }

  .btn-link:hover {
    color: var(--hover);
  }

  .btn-icon-small {
    background: none;
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    padding: 0.25rem 0.5rem;
    font-size: 0.8rem;
    transition: background 0.2s;
  }

  .btn-icon-small:hover {
    background: var(--hover);
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    flex-wrap: wrap;
  }

  .mt-2 {
    margin-top: 1rem;
  }
</style>