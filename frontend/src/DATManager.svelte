<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from './i18n';
  import PageHeader from './PageHeader.svelte';
  import Icon from './lib/components/Icon.svelte';

  export let onSwitchTab: (tab: string) => void = () => {};

  interface DATFile {
    name: string;
    path: string;
    size: number;
    last_update: number;
    exists: boolean;
    type: string;
    is_symlink: boolean;
    symlink_to?: string;
  }

  let files: DATFile[] = [];
  let loading = false;
  let error = '';
  let globalUpdating = false;

  async function fetchFiles() {
    loading = true;
    try {
      const res = await fetch('/api/dat/list');
      if (!res.ok) throw new Error('Failed to load DAT files');

      let data = await res.json();
      // Sort files: xray first, then mihomo
      files = data.sort((a: DATFile, b: DATFile) => {
        if (a.type !== b.type) return a.type.localeCompare(b.type);
        return a.name.localeCompare(b.name);
      });
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function updateAll() {
    globalUpdating = true;
    error = '';
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/dat/update', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text);
      }
      await fetchFiles();
    } catch (e: any) {
      error = e.message;
    } finally {
      globalUpdating = false;
    }
  }

  function formatSize(b: number): string {
    if (b >= 1024 * 1024) return (b / (1024 * 1024)).toFixed(2) + ' MB';
    if (b >= 1024) return (b / 1024).toFixed(2) + ' KB';
    return b + ' B';
  }

  function formatDate(ts: number): string {
    if (!ts) return '-';
    return new Date(ts * 1000).toLocaleString('ru-RU');
  }

  onMount(fetchFiles);
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
      <div class="title-group">
        <h2>{$t('dat.database_files')}</h2>
        <button
          class="btn btn-primary ml-2"
          on:click={updateAll}
          disabled={globalUpdating || loading}
        >
          {#if globalUpdating}<Icon name="refresh" size={14} /> {$t('app.loading')}{:else}<Icon
              name="download"
              size={14}
            />
            {$t('dat.update_all')}{/if}
        </button>
      </div>
      <button class="btn btn-secondary" on:click={fetchFiles} disabled={loading}>
        <Icon name="refresh" size={14} />
        {$t('app.refresh')}
      </button>
    </div>

    {#if loading && !globalUpdating}
      <p class="text-secondary mt-2">{$t('app.loading')}</p>
    {:else if files.length === 0}
      <p class="text-secondary mt-2">{$t('dat.no_files')}</p>
    {:else}
      <div class="file-list">
        {#each files as file}
          <div class="file-item" class:is-symlink={file.is_symlink}>
            <div class="file-info">
              <div class="file-name">
                <span class="icon"
                  ><Icon name={file.is_symlink ? 'chevron-right' : 'editor'} size={14} /></span
                >
                {file.name}
                {#if file.is_symlink}
                  <span class="symlink-target">→ {file.symlink_to}</span>
                {/if}
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

  .title-group {
    display: flex;
    align-items: center;
  }

  .ml-2 {
    margin-left: 1rem;
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
    transition: transform 0.2s;
  }

  .file-item:hover {
    border-color: var(--primary);
  }

  .file-item.is-symlink {
    border-style: dashed;
    background: rgba(var(--primary-rgb), 0.02);
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
    flex-wrap: wrap;
  }

  .icon {
    font-size: 1.1rem;
  }

  .symlink-target {
    font-weight: 400;
    color: var(--primary);
    font-family: monospace;
    font-size: 0.85rem;
    background: var(--bg-page);
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
  }

  .file-details {
    display: flex;
    gap: 1rem;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .mt-2 {
    margin-top: 1rem;
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
