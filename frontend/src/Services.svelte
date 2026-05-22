<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { t } from './i18n'
  import { showToast, fetchCapabilities } from './stores'
  import Skeleton from './components/Skeleton.svelte'

  interface Kernel {
    name: string
    display_name: string
    binary_path: string
    current_version: string
    latest_version: string
    has_update: boolean
    channel: string
    status: string
    process_status: string
    message: string
  }

  let xkeenStatus = ''
  let mihomoStatus = ''
  let loading = false
  let actionLoading: Record<string, boolean> = {}
  
  let kernels: Kernel[] = []
  let kernelsLoaded = false
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
        const envelope = await res.json()
        // KernelList uses JSONSuccess envelope: {success, data: [...]}
        const list = Array.isArray(envelope) ? envelope : (envelope.data ?? [])
        kernels = list
        // Start polling for kernels that are not idle
        kernels.forEach((k: typeof kernels[0]) => {
          if (k.status !== 'idle' && !statusIntervals[k.name]) {
            startPolling(k.name)
          }
        })
      }
    } catch (e) {}
    finally {
      kernelsLoaded = true
    }
  }

  async function controlService(action: string) {
    const key = `xkeen-${action}`
    actionLoading[key] = true
    
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`/api/service/control?action=${action}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      
      const text = await res.text()
      if (!res.ok) throw new Error(text)
      
      await fetchStatus()
      await fetchCapabilities()
    } catch (e: any) {
      showToast('error', `${$t('svc.action_error')}: ${e.message}`)
    } finally {
      actionLoading[key] = false
    }
  }

  let switchingKernel = false

  async function switchKernel(kernel: string) {
    switchingKernel = true
    const key = `switch-${kernel}`
    actionLoading[key] = true
    
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`/api/service/control?action=switch_kernel&kernel=${kernel}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      
      const text = await res.text()
      if (!res.ok) throw new Error(text)
      
      await fetchStatus()
      await fetchKernels()
      await fetchCapabilities()
    } catch (e: any) {
      showToast('error', `${$t('svc.action_error')}: ${e.message}`)
    } finally {
      actionLoading[key] = false
      switchingKernel = false
    }
  }

  async function checkKernelUpdate(name: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch(`/api/kernels/${name}/check`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      startPolling(name)
    } catch (e) {}
  }

  async function installKernel(name: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch(`/api/kernels/${name}/install`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      startPolling(name)
    } catch (e) {}
  }

  async function setKernelChannel(name: string, channel: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch(`/api/kernels/${name}/channel`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken || '' },
        body: JSON.stringify({ channel })
      })
      await fetchKernels()
    } catch (e) {}
  }

  async function fetchKernelStatus(name: string) {
    try {
      const res = await fetch(`/api/kernels/${name}/status`)
      if (res.ok) {
        const envelope = await res.json()
        // KernelStatus uses JSONSuccess envelope: {success, data: {...}}
        const data = envelope.data ?? envelope
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

  let selectedKernel = ''

  $: xray = getKernel('xray')
  $: mihomo = getKernel('mihomo')
  $: activeKernel = xray?.process_status === 'running' ? 'xray'
      : mihomo?.process_status === 'running' ? 'mihomo'
      : 'unknown'

  $: if (activeKernel !== 'unknown') {
    selectedKernel = activeKernel
  } else if (kernelsLoaded && !selectedKernel) {
    const xrayInstalled = xray && xray.current_version && xray.current_version !== 'not installed' && xray.current_version !== 'error'
    const mihomoInstalled = mihomo && mihomo.current_version && mihomo.current_version !== 'not installed' && mihomo.current_version !== 'error'
    if (mihomoInstalled && !xrayInstalled) {
      selectedKernel = 'mihomo'
    } else {
      selectedKernel = 'xray'
    }
  }

  // Use boolean process_status from kernel API instead of fragile string matching on i18n status text
  $: isRunning = xray?.process_status === 'running' || mihomo?.process_status === 'running'

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
    <!-- XKeen Main Control Card -->
    <div class="card main-control">
      <div class="service-header">
        <div class="title-group">
          <h2>{$t('svc.xkeen')}</h2>
          <span class="version-tag">{$t('svc.service_label')}</span>
        </div>
        <span class="status-badge" class:running={isRunning}>
          {xkeenStatus || $t('app.loading')}
        </span>
      </div>
      
      <div class="kernel-selector mb-2">
        <label for="kernel-select" class="text-secondary mr-2">{$t('svc.active_kernel')}:</label>
        <select
          id="kernel-select"
          title={$t('svc.kernel_switch')}
          value={selectedKernel}
          on:change={(e) => {
            const val = e.currentTarget.value
            if (val) {
              selectedKernel = val
              switchKernel(val)
            }
          }}
          disabled={switchingKernel}
        >
          <option value="xray">{$t('svc.xray')}</option>
          <option value="mihomo">{$t('svc.mihomo')}</option>
        </select>
        {#if switchingKernel}
          <span class="text-secondary ml-2">{$t('svc.switching')}</span>
        {/if}
      </div>

      <div class="actions">
        <button class="btn btn-primary" on:click={() => controlService('start')} disabled={actionLoading['xkeen-start']}>
          {actionLoading['xkeen-start'] ? $t('svc.starting') : $t('app.start')}
        </button>
        <button class="btn btn-secondary" on:click={() => controlService('stop')} disabled={actionLoading['xkeen-stop']}>
          {actionLoading['xkeen-stop'] ? $t('svc.stopping') : $t('app.stop')}
        </button>
        <button class="btn btn-secondary" on:click={() => controlService('restart')} disabled={actionLoading['xkeen-restart']}>
          {actionLoading['xkeen-restart'] ? $t('svc.restarting') : $t('app.restart')}
        </button>
      </div>
    </div>

    <!-- Xray Details Card -->
    <div class="card" class:active-card={activeKernel === 'xray'}>
      <div class="service-header">
        <div class="title-group">
          <h2>{$t('svc.xray')}</h2>
          {#if xray}
            <span class="version-tag process-status-{xray.process_status}">
              {$t('kernel.status.' + (xray.process_status || 'unknown'))}
            </span>
          {/if}
        </div>
        {#if activeKernel === 'xray'}
          <span class="active-tag">{$t('svc.active_label')}</span>
        {/if}
      </div>
      <p class="text-secondary mb-2">{$t('svc.xray_desc')}</p>
      
      {#if xray}
        <div class="kernel-details mb-2">
          {#if xray.current_version && xray.current_version !== 'not installed'}
            <div class="detail-row">
              <span>{$t('svc.version')}:</span>
              <span>v{xray.current_version}</span>
            </div>
          {/if}
          <div class="detail-row">
            <span>{$t('kernels.channel')}:</span>
            <select class="small-select" value={xray.channel} on:change={(e) => setKernelChannel('xray', e.currentTarget.value)}>
              <option value="stable">{$t('svc.channel_stable')}</option>
              <option value="preview">{$t('svc.channel_preview')}</option>
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
      {:else if !kernelsLoaded}
        <div class="kernel-details mb-2">
          <div class="detail-row">
            <Skeleton type="text-line" width="100px" />
            <Skeleton type="text-line" width="60px" />
          </div>
          <div class="detail-row">
            <Skeleton type="text-line" width="80px" />
            <Skeleton type="text-line" width="90px" />
          </div>
        </div>
      {:else}
        <p class="text-secondary">{$t('kernels.not_installed')}</p>
      {/if}

      <div class="actions">
        <button class="btn btn-icon-small" on:click={() => checkKernelUpdate('xray')} title={$t('kernels.check_update')} disabled={xray?.status !== 'idle'}>
          {xray?.status === 'checking' ? '...' : $t('app.refresh')}
        </button>
      </div>
    </div>

    <!-- Mihomo Details Card -->
    <div class="card" class:active-card={activeKernel === 'mihomo'}>
      <div class="service-header">
        <div class="title-group">
          <h2>{$t('svc.mihomo')}</h2>
          {#if mihomo}
            <span class="version-tag process-status-{mihomo.process_status}">
              {$t('kernel.status.' + (mihomo.process_status || 'unknown'))}
            </span>
          {/if}
        </div>
        {#if activeKernel === 'mihomo'}
          <span class="active-tag">{$t('svc.active_label')}</span>
        {/if}
      </div>
      <p class="text-secondary mb-2">{$t('svc.mihomo_desc')}</p>

      {#if mihomo}
        <div class="kernel-details mb-2">
          {#if mihomo.current_version && mihomo.current_version !== 'not installed'}
            <div class="detail-row">
              <span>{$t('svc.version')}:</span>
              <span>v{mihomo.current_version}</span>
            </div>
          {/if}
          <div class="detail-row">
            <span>{$t('svc.status')}:</span>
            <span class="status-text" class:text-success={mihomoStatus.includes('running')}>
              {mihomoStatus}
            </span>
          </div>
          <div class="detail-row">
            <span>{$t('kernels.channel')}:</span>
            <select class="small-select" value={mihomo.channel} on:change={(e) => setKernelChannel('mihomo', e.currentTarget.value)}>
              <option value="stable">{$t('svc.channel_stable')}</option>
              <option value="preview">{$t('svc.channel_preview')}</option>
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
      {:else if !kernelsLoaded}
        <div class="kernel-details mb-2">
          <div class="detail-row">
            <Skeleton type="text-line" width="100px" />
            <Skeleton type="text-line" width="60px" />
          </div>
          <div class="detail-row">
            <Skeleton type="text-line" width="80px" />
            <Skeleton type="text-line" width="90px" />
          </div>
        </div>
      {:else}
        <p class="text-secondary">{$t('kernels.not_installed')}</p>
      {/if}

      <div class="actions">
        <button class="btn btn-icon-small" on:click={() => checkKernelUpdate('mihomo')} title={$t('kernels.check_update')} disabled={mihomo?.status !== 'idle'}>
          {mihomo?.status === 'checking' ? '...' : $t('app.refresh')}
        </button>
      </div>
    </div>
  </div>

</div>

<style>
  .main-control {
    grid-column: 1 / -1;
  }

  .kernel-selector {
    display: flex;
    align-items: center;
    background: var(--bg);
    padding: 0.5rem 1rem;
    border-radius: var(--radius);
  }

  .btn-group {
    display: flex;
    gap: 1px;
    background: var(--border);
    padding: 2px;
    border-radius: 6px;
  }

  .btn-sm {
    padding: 0.25rem 1rem;
    font-size: 0.85rem;
    border-radius: 4px;
  }

  .active-card {
    border: 1px solid var(--primary);
    box-shadow: 0 0 10px rgba(var(--primary-rgb), 0.1);
  }

  .active-tag {
    font-size: 0.7rem;
    text-transform: uppercase;
    font-weight: bold;
    color: var(--success);
    background: rgba(16, 185, 129, 0.1);
    padding: 0.1rem 0.5rem;
    border-radius: 4px;
  }

  .status-text {
    font-family: monospace;
    font-size: 0.8rem;
  }

  .text-success {
    color: var(--success);
  }

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