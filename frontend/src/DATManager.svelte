<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang } from './i18n';
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

  interface DATTag {
    tag: string;
    count: number;
  }

  let files: DATFile[] = [];
  let loading = false;
  let error = '';
  let globalUpdating = false;

  // Tag browser state
  let tagDrawer: {
    open: boolean;
    file: DATFile | null;
    tags: DATTag[];
    loading: boolean;
    error: string;
    search: string;
    copied: string;
  } = { open: false, file: null, tags: [], loading: false, error: '', search: '', copied: '' };

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

  async function openTagBrowser(file: DATFile) {
    tagDrawer = { open: true, file, tags: [], loading: true, error: '', search: '', copied: '' };
    try {
      const res = await fetch(`/api/dat/tags?name=${encodeURIComponent(file.name)}`);
      const json = await res.json();
      if (!res.ok) throw new Error(json.error || 'Failed to load tags');
      tagDrawer = { ...tagDrawer, loading: false, tags: json.tags || [] };
    } catch (e: any) {
      tagDrawer = { ...tagDrawer, loading: false, error: e.message };
    }
  }

  function closeTagBrowser() {
    tagDrawer = { ...tagDrawer, open: false };
  }

  function getTagPrefix(file: DATFile): string {
    const name = file.name.toLowerCase();
    if (name.includes('geoip')) return 'geoip';
    if (name.includes('geosite')) return 'geosite';
    return file.name.replace(/\.dat$/i, '').toLowerCase();
  }

  function getRuleValue(file: DATFile, tag: string): string {
    return `${getTagPrefix(file)}:${tag}`;
  }

  let copyTimer: ReturnType<typeof setTimeout>;
  function copyTag(file: DATFile, tag: string) {
    const value = getRuleValue(file, tag);
    navigator.clipboard.writeText(value).catch(() => {});
    tagDrawer = { ...tagDrawer, copied: tag };
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      tagDrawer = { ...tagDrawer, copied: '' };
    }, 1500);
  }

  $: filteredTags = tagDrawer.search.trim()
    ? tagDrawer.tags.filter((t) => t.tag.toLowerCase().includes(tagDrawer.search.toLowerCase()))
    : tagDrawer.tags;

  function formatSize(b: number): string {
    if (b >= 1024 * 1024) return (b / (1024 * 1024)).toFixed(2) + ' MB';
    if (b >= 1024) return (b / 1024).toFixed(2) + ' KB';
    return b + ' B';
  }

  function formatDate(ts: number): string {
    if (!ts) return '-';
    return new Date(ts * 1000).toLocaleString($currentLang === 'ru' ? 'ru-RU' : 'en-US');
  }

  function isDatFile(file: DATFile): boolean {
    return file.name.toLowerCase().endsWith('.dat');
  }

  $: xrayFiles = files.filter((f) => f.type === 'xray');
  $: mihomoFiles = files.filter((f) => f.type === 'mihomo');
  $: otherFiles = files.filter((f) => f.type !== 'xray' && f.type !== 'mihomo');
  $: totalSize = files.reduce((sum, f) => sum + f.size, 0);
  $: lastUpdated = files.reduce((max, f) => Math.max(max, f.last_update || 0), 0);
  $: missingCount = files.filter((f) => !f.exists).length;

  onMount(fetchFiles);
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_services')} <span class="crumb-separator">/</span>
        {$t('nav.dat')}
      </div>
      <h1>{$t('dat.h1')}</h1>
      <p class="sub">{$t('dat.h1_sub')}</p>
    </div>
    <div class="ph-actions">
      <button
        class="btn btn-secondary"
        on:click={fetchFiles}
        disabled={loading}
        title={$t('app.refresh')}
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          style="margin-right: 6px;"
        >
          <polyline points="23 4 23 10 17 10" />
          <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
        </svg>
        {$t('app.refresh')}
      </button>
      <button
        class="btn btn-primary"
        on:click={updateAll}
        disabled={globalUpdating || loading}
        title={$t('dat.update_all')}
      >
        {#if globalUpdating}
          <span class="spinner" style="margin-right: 6px;">...</span>
          {$t('app.loading')}
        {:else}
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"
          >
            <polyline points="21 8 21 3 16 3" />
            <path d="M3 16v5h5M21 3l-9 9M3 21l9-9" />
          </svg>
          {$t('dat.update_all')}
        {/if}
      </button>
    </div>
  </div>

  {#if error}
    <div class="alert alert-error mb-3">{error}</div>
  {/if}

  <!-- Stats -->
  {#if !loading && files.length > 0}
    <div class="stats mb-3">
      <span class="stat"><b>{files.length}</b> {$t('dat.total_files')}</span>
      <span class="stat"
        ><b>{files.length - missingCount}</b>
        {$currentLang === 'ru' ? 'актуальных' : 'active'}</span
      >
      {#if missingCount > 0}
        <span class="stat" style="color: var(--warning);"
          ><b>{missingCount}</b> {$currentLang === 'ru' ? 'отсутствует' : 'missing'}</span
        >
      {/if}
      <span class="stat"
        >{$currentLang === 'ru' ? 'общий размер' : 'total size'}
        <b>{formatSize(totalSize)}</b></span
      >
    </div>
  {/if}

  {#if loading && !globalUpdating && files.length === 0}
    <p class="text-secondary">{$t('app.loading')}</p>
  {:else if files.length === 0 && !loading}
    <p class="text-secondary">{$t('dat.no_files')}</p>
  {:else}
    <!-- Xray Group -->
    {#if xrayFiles.length > 0}
      <div class="card card-tight mb-3">
        <h2 class="card-title" style="padding: 20px 24px 8px 24px;">
          Xray ({xrayFiles[0]?.path || '/opt/etc/xray/datfiles'})
        </h2>
        <div class="dat-list">
          {#each xrayFiles as file}
            <div class="dat-row" class:is-symlink={file.is_symlink}>
              <div class="dr-ico" class:warning={!file.exists}>
                {#if file.is_symlink}
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    ><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" /><path
                      d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"
                    /></svg
                  >
                {:else}
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    ><circle cx="12" cy="12" r="10" /><path
                      d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"
                    /><path d="M2 12h20" /></svg
                  >
                {/if}
              </div>
              <div class="dr-main">
                <div class="dr-name">
                  {file.name}
                  {#if !file.exists}
                    <span class="badge badge-error">{$t('dat.not_found')}</span>
                  {:else}
                    <span class="badge badge-success">OK</span>
                  {/if}
                  <span class="badge badge-type"
                    >{file.name.split('.').pop()?.toUpperCase() || 'DAT'}</span
                  >
                </div>
                <div class="dr-meta">
                  {formatSize(file.size)} ·
                  {#if file.is_symlink}
                    {$currentLang === 'ru' ? 'симлинк' : 'symlink'} → {file.symlink_to} ·
                  {/if}
                  {$t('dat.updated')}
                  {formatDate(file.last_update)}
                </div>
              </div>
              <div class="stat-bar" style="width:120px;">
                <div
                  class="stat-bar-fill"
                  style="width: {file.exists ? '100%' : '0%'}; background: {file.exists
                    ? 'var(--success)'
                    : 'var(--error)'}"
                ></div>
              </div>
              <div class="dr-actions">
                {#if isDatFile(file) && file.exists}
                  <button
                    class="btn btn-secondary"
                    on:click={() => openTagBrowser(file)}
                    title={$currentLang === 'ru' ? 'Просмотр тегов' : 'Browse tags'}
                  >
                    <svg
                      width="13"
                      height="13"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      style="margin-right:5px"
                      ><circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" /></svg
                    >
                    {$currentLang === 'ru' ? 'Теги' : 'Tags'}
                  </button>
                {/if}
                <button
                  class="btn btn-secondary btn-icon-only"
                  on:click={updateAll}
                  disabled={globalUpdating}
                  title={$t('dat.update_all')}
                >
                  ↓
                </button>
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Mihomo Group -->
    {#if mihomoFiles.length > 0}
      <div class="card card-tight mb-3">
        <h2 class="card-title" style="padding: 20px 24px 8px 24px;">
          Mihomo ({mihomoFiles[0]?.path || '/opt/etc/mihomo'})
        </h2>
        <div class="dat-list">
          {#each mihomoFiles as file}
            <div class="dat-row" class:is-symlink={file.is_symlink}>
              <div class="dr-ico" class:warning={!file.exists}>
                {#if file.is_symlink}
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    ><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" /><path
                      d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"
                    /></svg
                  >
                {:else}
                  <svg
                    width="18"
                    height="18"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    ><circle cx="12" cy="12" r="10" /><path
                      d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"
                    /><path d="M2 12h20" /></svg
                  >
                {/if}
              </div>
              <div class="dr-main">
                <div class="dr-name">
                  {file.name}
                  {#if !file.exists}
                    <span class="badge badge-error">{$t('dat.not_found')}</span>
                  {:else}
                    <span class="badge badge-success">OK</span>
                  {/if}
                  <span class="badge badge-type"
                    >{file.name.split('.').pop()?.toUpperCase() || 'DAT'}</span
                  >
                </div>
                <div class="dr-meta">
                  {formatSize(file.size)} ·
                  {#if file.is_symlink}
                    {$currentLang === 'ru' ? 'симлинк' : 'symlink'} → {file.symlink_to} ·
                  {/if}
                  {$t('dat.updated')}
                  {formatDate(file.last_update)}
                </div>
              </div>
              <div class="stat-bar" style="width:120px;">
                <div
                  class="stat-bar-fill"
                  style="width: {file.exists ? '100%' : '0%'}; background: {file.exists
                    ? 'var(--success)'
                    : 'var(--error)'}"
                ></div>
              </div>
              <div class="dr-actions">
                {#if isDatFile(file) && file.exists}
                  <button
                    class="btn btn-secondary"
                    on:click={() => openTagBrowser(file)}
                    title={$currentLang === 'ru' ? 'Просмотр тегов' : 'Browse tags'}
                  >
                    <svg
                      width="13"
                      height="13"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      style="margin-right:5px"
                      ><circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" /></svg
                    >
                    {$currentLang === 'ru' ? 'Теги' : 'Tags'}
                  </button>
                {/if}
                <button
                  class="btn btn-secondary btn-icon-only"
                  on:click={updateAll}
                  disabled={globalUpdating}
                  title={$t('dat.update_all')}
                >
                  ↓
                </button>
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Other Files -->
    {#if otherFiles.length > 0}
      <div class="card card-tight mb-3">
        <h2 class="card-title" style="padding: 20px 24px 8px 24px;">
          {$currentLang === 'ru' ? 'Прочие файлы' : 'Other files'}
        </h2>
        <div class="dat-list">
          {#each otherFiles as file}
            <div class="dat-row" class:is-symlink={file.is_symlink}>
              <div class="dr-ico" class:warning={!file.exists}>
                <svg
                  width="18"
                  height="18"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  ><circle cx="12" cy="12" r="10" /><path
                    d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"
                  /><path d="M2 12h20" /></svg
                >
              </div>
              <div class="dr-main">
                <div class="dr-name">
                  {file.name}
                  {#if !file.exists}
                    <span class="badge badge-error">{$t('dat.not_found')}</span>
                  {:else}
                    <span class="badge badge-success">OK</span>
                  {/if}
                  <span class="badge badge-type">{file.type?.toUpperCase() || 'DAT'}</span>
                </div>
                <div class="dr-meta">
                  {formatSize(file.size)} · {file.path} ·
                  {#if file.is_symlink}
                    {$currentLang === 'ru' ? 'симлинк' : 'symlink'} → {file.symlink_to} ·
                  {/if}
                  {$t('dat.updated')}
                  {formatDate(file.last_update)}
                </div>
              </div>
              <div class="stat-bar" style="width:120px;">
                <div
                  class="stat-bar-fill"
                  style="width: {file.exists ? '100%' : '0%'}; background: {file.exists
                    ? 'var(--success)'
                    : 'var(--error)'}"
                ></div>
              </div>
              <div class="dr-actions">
                <button
                  class="btn btn-secondary btn-icon-only"
                  on:click={updateAll}
                  disabled={globalUpdating}
                  title={$t('dat.update_all')}
                >
                  ↓
                </button>
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  {/if}
</div>

<!-- Tag Browser Modal -->
{#if tagDrawer.open}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="tag-overlay" on:click={closeTagBrowser}>
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="tag-drawer" on:click|stopPropagation>
      <div class="td-header">
        <div class="td-title">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="color:var(--primary)"
            ><circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" /></svg
          >
          <span>{tagDrawer.file?.name}</span>
          {#if !tagDrawer.loading && tagDrawer.tags.length > 0}
            <span class="td-count"
              >{tagDrawer.tags.length} {$currentLang === 'ru' ? 'тегов' : 'tags'}</span
            >
          {/if}
        </div>
        <button class="td-close" on:click={closeTagBrowser} aria-label="Close">✕</button>
      </div>

      <div class="td-hint">
        {#if tagDrawer.file}
          {$currentLang === 'ru' ? 'Формат для routing rule:' : 'Routing rule format:'}
          <code class="td-format">{getTagPrefix(tagDrawer.file)}:TAGNAME</code>
        {/if}
      </div>

      <div class="td-search">
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          class="td-search-ico"><circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" /></svg
        >
        <input
          class="td-search-input"
          type="text"
          placeholder={$currentLang === 'ru' ? 'Поиск тега...' : 'Search tag...'}
          bind:value={tagDrawer.search}
          autofocus
        />
        {#if tagDrawer.search}
          <button class="td-clear" on:click={() => (tagDrawer = { ...tagDrawer, search: '' })}
            >✕</button
          >
        {/if}
      </div>

      <div class="td-body">
        {#if tagDrawer.loading}
          <div class="td-state">
            <span class="spinner-circle"></span>
            <span>{$t('app.loading')}</span>
          </div>
        {:else if tagDrawer.error}
          <div class="td-state td-state-error">{tagDrawer.error}</div>
        {:else if filteredTags.length === 0}
          <div class="td-state">
            {$currentLang === 'ru' ? 'Ничего не найдено' : 'No tags found'}
          </div>
        {:else}
          <div class="td-list">
            {#each filteredTags as tag}
              {@const ruleValue = tagDrawer.file ? getRuleValue(tagDrawer.file, tag.tag) : tag.tag}
              {@const isCopied = tagDrawer.copied === tag.tag}
              <button
                class="td-tag"
                class:copied={isCopied}
                on:click={() => tagDrawer.file && copyTag(tagDrawer.file, tag.tag)}
                title={$currentLang === 'ru' ? `Копировать: ${ruleValue}` : `Copy: ${ruleValue}`}
              >
                <span class="td-tag-name">{tag.tag}</span>
                {#if tag.count > 0}
                  <span class="td-tag-count">{tag.count.toLocaleString()}</span>
                {/if}
                <span class="td-tag-copy">
                  {#if isCopied}
                    <svg
                      width="12"
                      height="12"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2.5"><polyline points="20 6 9 17 4 12" /></svg
                    >
                  {:else}
                    <svg
                      width="12"
                      height="12"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      ><rect x="9" y="9" width="13" height="13" rx="2" /><path
                        d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
                      /></svg
                    >
                  {/if}
                </span>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .crumb-separator {
    color: var(--fg-faint);
    margin: 0 6px;
  }

  .stats {
    display: flex;
    gap: 16px;
    flex-wrap: wrap;
    font-size: 13px;
    color: var(--fg-secondary);
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--border);
    padding: 10px 16px;
    border-radius: var(--radius);
  }

  .stat {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .stat b {
    color: var(--fg-primary);
  }

  .dat-list {
    display: flex;
    flex-direction: column;
  }

  .dat-row {
    display: flex;
    align-items: center;
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
    gap: 16px;
  }

  .dat-row:last-child {
    border-bottom: none;
  }

  .dat-row.is-symlink {
    background: rgba(255, 255, 255, 0.01);
  }

  .dr-ico {
    width: 32px;
    height: 32px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--primary);
    background: rgba(59, 130, 246, 0.05);
    flex-shrink: 0;
  }

  .dr-ico.warning {
    color: var(--error);
    background: rgba(239, 68, 68, 0.05);
    border-color: rgba(239, 68, 68, 0.2);
  }

  .dr-main {
    flex: 1;
    min-width: 0;
  }

  .dr-name {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-primary);
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 4px;
  }

  .dr-name .badge {
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
  }

  .dr-name .badge-success {
    background: rgba(16, 185, 129, 0.1);
    color: var(--success);
  }

  .dr-name .badge-error {
    background: rgba(239, 68, 68, 0.1);
    color: var(--error);
  }

  .dr-name .badge-type {
    background: rgba(255, 255, 255, 0.05);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
  }

  .dr-meta {
    font-size: 12px;
    color: var(--fg-secondary);
  }

  .stat-bar {
    height: 6px;
    background: rgba(255, 255, 255, 0.05);
    border-radius: 3px;
    overflow: hidden;
    flex-shrink: 0;
  }

  .stat-bar-fill {
    height: 100%;
    border-radius: 3px;
    transition: width 0.3s ease;
  }

  .dr-actions {
    flex-shrink: 0;
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .btn-icon-only {
    padding: 6px 10px;
    line-height: 1;
    font-size: 14px;
    height: auto;
  }

  .spinner {
    display: inline-block;
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  /* ── Tag Browser Modal ── */

  .tag-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 200;
    display: flex;
    align-items: stretch;
    justify-content: flex-end;
    animation: fadeIn 140ms ease;
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  .tag-drawer {
    width: 420px;
    max-width: 92vw;
    background: var(--bg-card);
    border-left: 1px solid var(--border-strong);
    display: flex;
    flex-direction: column;
    animation: slideIn 180ms cubic-bezier(0.4, 0, 0.2, 1);
    overflow: hidden;
  }

  @keyframes slideIn {
    from {
      transform: translateX(40px);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }

  .td-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 18px 20px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .td-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-primary);
    min-width: 0;
  }

  .td-title span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .td-count {
    font-size: 11px;
    font-weight: 500;
    color: var(--fg-dim);
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    border-radius: 20px;
    padding: 1px 8px;
    flex-shrink: 0;
  }

  .td-close {
    background: none;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: 14px;
    padding: 4px 6px;
    border-radius: var(--radius-sm);
    line-height: 1;
    transition:
      color var(--transition-fast),
      background var(--transition-fast);
    flex-shrink: 0;
  }

  .td-close:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.06);
  }

  .td-hint {
    padding: 10px 20px;
    font-size: 12px;
    color: var(--fg-secondary);
    border-bottom: 1px solid var(--border);
    background: rgba(255, 255, 255, 0.015);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .td-format {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    background: rgba(41, 194, 240, 0.1);
    color: var(--primary);
    border: 1px solid rgba(41, 194, 240, 0.2);
    border-radius: var(--radius-sm);
    padding: 2px 7px;
  }

  .td-search {
    padding: 12px 20px;
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    background: var(--bg-card);
  }

  .td-search-ico {
    color: var(--fg-dim);
    flex-shrink: 0;
  }

  .td-search-input {
    flex: 1;
    background: none;
    border: none;
    outline: none;
    color: var(--fg-primary);
    font-size: 13px;
    caret-color: var(--primary);
  }

  .td-search-input::placeholder {
    color: var(--fg-faint);
  }

  .td-clear {
    background: none;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: 11px;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
    line-height: 1;
    transition: color var(--transition-fast);
  }

  .td-clear:hover {
    color: var(--fg-primary);
  }

  .td-body {
    flex: 1;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }

  .td-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 40px 20px;
    color: var(--fg-dim);
    font-size: 13px;
  }

  .td-state-error {
    color: var(--error);
  }

  .spinner-circle {
    width: 16px;
    height: 16px;
    border: 2px solid var(--border-strong);
    border-top-color: var(--primary);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    display: inline-block;
  }

  .td-list {
    padding: 8px 0;
  }

  .td-tag {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 20px;
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    transition: background var(--transition-fast);
    color: var(--fg-primary);
  }

  .td-tag:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  .td-tag:hover .td-tag-copy {
    opacity: 1;
  }

  .td-tag.copied {
    background: rgba(70, 209, 138, 0.07);
  }

  .td-tag.copied .td-tag-copy {
    opacity: 1;
    color: var(--success);
  }

  .td-tag-name {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .td-tag-count {
    font-size: 11px;
    color: var(--fg-dim);
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 1px 7px;
    flex-shrink: 0;
    font-variant-numeric: tabular-nums;
  }

  .td-tag-copy {
    opacity: 0;
    color: var(--fg-dim);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    transition:
      opacity var(--transition-fast),
      color var(--transition-fast);
  }
</style>
