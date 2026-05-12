<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'
  import PageHeader from './PageHeader.svelte'

  export let onSwitchTab: (tab: string) => void = () => {}

  interface DATFile {
    name: string
    path: string
    size: number
    last_update: number
    exists: boolean
    remote_url: string
  }

  let files: Record<string, DATFile> = {}
  let loading = false
  let error = ''
  let updating: string | null = null

  async function fetchFiles() {
    loading = true
    try {
      const res = await fetch('/api/dat/list')
      if (!res.ok) throw new Error('Failed to load DAT files')
      files = await res.json()
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  async function updateFile(type: string) {
    updating = type
    error = ''
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`/api/dat/update?type=${type}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) throw new Error('Failed to update')
      await fetchFiles()
    } catch (e: any) {
      error = e.message
    } finally {
      updating = null
    }
  }

  async function updateAll() {
    updating = 'all'
    error = ''
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch('/api/dat/update-all', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) throw new Error('Failed to update')
      await fetchFiles()
    } catch (e: any) {
      error = e.message
    } finally {
      updating = null
    }
  }

  function formatSize(b: number): string {
    if (b >= 1024 * 1024) return (b / (1024 * 1024)).toFixed(2) + ' MB'
    if (b >= 1024) return (b / 1024).toFixed(2) + ' KB'
    return b + ' B'
  }

  function formatDate(ts: number): string {
    if (!ts) return '-'
    return new Date(ts * 1000).toLocaleDateString('ru-RU')
  }

  onMount(fetchFiles)
</script>

<div class="container">
  <PageHeader
    title={$t('dat.title')}
    subtitle={$t('dat.subtitle')}
    breadcrumbs={[{ label: $t('dat.title') }]}
    {onSwitchTab}
  />

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <div class="card mb-2">
    <div class="flex-between">
      <h2>{$t('dat.database_files')}</h2>
      <button class="btn btn-primary" on:click={updateAll} disabled={updating === 'all'}>
        {updating === 'all' ? $t('dat.updating') : '🔄 ' + $t('dat.update_all')}
      </button>
    </div>

    {#if loading}
      <p class="text-secondary">{$t('app.loading')}</p>
    {:else}
      <div class="file-list">
        {#each Object.entries(files) as [key, file]}
          <div class="file-item">
            <div class="file-info">
              <div class="file-name">
                {file.name}
                {#if !file.exists}
                  <span class="badge badge-warning">{$t('dat.not_found')}</span>
                {/if}
              </div>
              <div class="file-details">
                <span>{file.path}</span>
                <span>{formatSize(file.size)}</span>
                <span>{$t('dat.updated')}: {formatDate(file.last_update)}</span>
              </div>
            </div>
            <button
              class="btn btn-secondary"
              on:click={() => updateFile(key)}
              disabled={updating === key}
            >
              {updating === key ? '⏳' : '📥'} {$t('dat.update')}
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .flex-between {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .file-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    margin-top: 1rem;
  }

  .file-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
  }

  .file-name {
    font-weight: 600;
    margin-bottom: 0.25rem;
  }

  .file-details {
    display: flex;
    gap: 1rem;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .badge {
    padding: 0.125rem 0.5rem;
    border-radius: 4px;
    font-size: 0.75rem;
  }

  .badge-warning {
    background: rgba(255, 193, 7, 0.1);
    color: var(--warning, #ffc107);
  }
</style>