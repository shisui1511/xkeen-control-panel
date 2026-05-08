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

  let kernels: Kernel[] = []
  let loading = false
  let statusIntervals: Record<string, ReturnType<typeof setInterval>> = {}

  async function fetchKernels() {
    try {
      const res = await fetch('/api/kernels')
      if (res.ok) {
        kernels = await res.json()
      }
    } catch (e) {
      // ignore
    }
  }

  async function checkUpdate(name: string) {
    try {
      await fetch(`/api/kernels/${name}/check`, { method: 'POST' })
      startPolling(name)
    } catch (e) {
      // ignore
    }
  }

  async function installKernel(name: string) {
    try {
      await fetch(`/api/kernels/${name}/install`, { method: 'POST' })
      startPolling(name)
    } catch (e) {
      // ignore
    }
  }

  async function setChannel(name: string, channel: string) {
    try {
      await fetch(`/api/kernels/${name}/channel`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ channel })
      })
      await fetchKernels()
    } catch (e) {
      // ignore
    }
  }

  async function fetchStatus(name: string) {
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
    fetchStatus(name)
    statusIntervals[name] = setInterval(() => fetchStatus(name), 2000)
  }

  onMount(() => {
    fetchKernels()
  })

  onDestroy(() => {
    Object.values(statusIntervals).forEach(clearInterval)
  })
</script>

<div class="container">
  <h1>{$t('kernels.title')}</h1>
  <p class="text-secondary mb-3">{$t('kernels.subtitle')}</p>

  {#each kernels as kernel}
    <div class="card mb-2">
      <div class="kernel-header">
        <div>
          <h2 style="margin: 0">{kernel.display_name}</h2>
          <p class="text-secondary" style="margin: 0.25rem 0 0 0; font-size: 0.85rem">{kernel.binary_path}</p>
        </div>
        <div class="kernel-version">
          {#if kernel.current_version === 'not installed'}
            <span class="badge badge-warning">{$t('kernels.not_installed')}</span>
          {:else}
            <span class="badge badge-success">v{kernel.current_version}</span>
          {/if}
        </div>
      </div>

      <div class="kernel-info">
        <div class="info-row">
          <span class="info-label">{$t('kernels.channel')}</span>
          <select class="input" value={kernel.channel} on:change={(e) => setChannel(kernel.name, e.currentTarget.value)}>
            <option value="stable">Stable</option>
            <option value="preview">Preview</option>
          </select>
        </div>

        {#if kernel.latest_version}
          <div class="info-row">
            <span class="info-label">{$t('kernels.latest')}</span>
            <span class="info-value">v{kernel.latest_version}</span>
          </div>
        {/if}

        {#if kernel.status !== 'idle'}
          <div class="info-row">
            <span class="info-label">{$t('kernels.status')}</span>
            <span class="info-value">{kernel.message || kernel.status}</span>
          </div>
        {/if}
      </div>

      <div class="kernel-actions">
        <button class="btn btn-secondary" on:click={() => checkUpdate(kernel.name)} disabled={kernel.status !== 'idle'}>
          {kernel.status === 'checking' ? $t('kernels.checking') : $t('kernels.check_update')}
        </button>

        {#if kernel.has_update}
          <button class="btn btn-primary" on:click={() => installKernel(kernel.name)} disabled={kernel.status !== 'idle'}>
            {kernel.status === 'downloading' || kernel.status === 'installing' ? $t('kernels.installing') : $t('kernels.install')}
          </button>
        {/if}
      </div>
    </div>
  {/each}

  {#if kernels.length === 0}
    <div class="card">
      <p class="text-secondary">{$t('kernels.empty')}</p>
    </div>
  {/if}
</div>

<style>
  .kernel-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 1rem;
  }

  .kernel-version {
    flex-shrink: 0;
  }

  .badge {
    display: inline-block;
    padding: 0.25rem 0.5rem;
    border-radius: var(--radius);
    font-size: 0.85rem;
    font-weight: 500;
  }

  .badge-success {
    background: var(--success-bg);
    color: var(--success);
  }

  .badge-warning {
    background: var(--warning-bg);
    color: var(--warning);
  }

  .kernel-info {
    margin-bottom: 1rem;
  }

  .info-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.5rem 0;
    border-bottom: 1px solid var(--border-light);
  }

  .info-row:last-child {
    border-bottom: none;
  }

  .info-label {
    color: var(--fg-secondary);
    font-size: 0.9rem;
  }

  .info-value {
    font-weight: 500;
    font-size: 0.9rem;
  }

  .kernel-actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
</style>
