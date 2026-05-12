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
    type: string
  }

  let files: DATFile[] = []
  let loading = false
  let error = ''
  let updating: string | null = null

  // URL для каждого файла по его пути
  let updateUrls: Record<string, string> = {}

  async function fetchFiles() {
    loading = true
    try {
      const res = await fetch('/api/dat/list')
      if (!res.ok) throw new Error('Failed to load DAT files')
      
      let data = await res.json()
      // Sort files: xray first, then mihomo
      files = data.sort((a: DATFile, b: DATFile) => {
        if (a.type !== b.type) return a.type.localeCompare(b.type)
        return a.name.localeCompare(b.name)
      })
      
      // Заполняем дефолтные URL для известных файлов, если они пустые
      files.forEach(f => {
        if (!updateUrls[f.path]) {
          if (f.name.includes('geoip')) {
            updateUrls[f.path] = 'https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat'
          } else if (f.name.includes('geosite')) {
            updateUrls[f.path] = 'https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat'
          } else if (f.name.includes('mmdb') || f.name.includes('Country')) {
            updateUrls[f.path] = 'https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/Country.mmdb'
          }
        }
      })
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  async function updateFile(path: string) {
    const url = updateUrls[path]
    if (!url) {
      error = $t('dat.url_required')
      return
    }
    
    updating = path
    error = ''
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch(`/api/dat/update?path=${encodeURIComponent(path)}&url=${encodeURIComponent(url)}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text)
      }
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
    return new Date(ts * 1000).toLocaleString('ru-RU')
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
      <button class="btn btn-secondary" on:click={fetchFiles} disabled={loading}>
        🔄 {$t('app.refresh')}
      </button>
    </div>

    {#if loading}
      <p class="text-secondary">{$t('app.loading')}</p>
    {:else if files.length === 0}
      <p class="text-secondary">{$t('dat.no_files')}</p>
    {:else}
      <div class="file-list">
        {#each files as file}
          <div class="file-item">
            <div class="file-info">
              <div class="file-name">
                {file.name}
                {#if !file.exists}
                  <span class="badge badge-warning">{$t('dat.not_found')}</span>
                {:else}
                  <span class="badge badge-success">OK</span>
                {/if}
                <span class="badge badge-type">{file.type}</span>
              </div>
              <div class="file-details">
                <span>{file.path}</span>
                <span>{formatSize(file.size)}</span>
                <span>{$t('dat.updated')}: {formatDate(file.last_update)}</span>
              </div>
              
              <div class="url-input-wrapper mt-2">
                <input 
                  type="text" 
                  class="input url-input" 
                  bind:value={updateUrls[file.path]} 
                  placeholder={$t('dat.url_placeholder')} 
                />
              </div>
            </div>
            
            <div class="actions">
              <button
                class="btn btn-secondary"
                on:click={() => updateFile(file.path)}
                disabled={updating === file.path}
              >
                {updating === file.path ? '⏳' : '📥'} {$t('dat.update')}
              </button>
            </div>
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

  .file-info {
    flex: 1;
    min-width: 0;
  }

  .file-name {
    font-weight: 600;
    margin-bottom: 0.25rem;
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .file-details {
    display: flex;
    gap: 1rem;
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-bottom: 0.5rem;
  }

  .url-input-wrapper {
    max-width: 600px;
  }

  .url-input {
    width: 100%;
    padding: 0.4rem 0.5rem;
    font-size: 0.8rem;
    font-family: monospace;
  }

  .actions {
    margin-left: 1rem;
    flex-shrink: 0;
  }

  .mt-2 {
    margin-top: 0.5rem;
  }

  .badge {
    padding: 0.125rem 0.5rem;
    border-radius: 4px;
    font-size: 0.75rem;
  }

  .badge-success {
    background: rgba(16, 185, 129, 0.1);
    color: var(--success);
  }

  .badge-warning {
    background: rgba(255, 193, 7, 0.1);
    color: var(--warning, #ffc107);
  }

  .badge-type {
    background: var(--bg-page);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    text-transform: uppercase;
  }
</style>