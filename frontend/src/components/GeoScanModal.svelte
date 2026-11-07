<script lang="ts">
  import Modal from './Modal.svelte';
  import Button from './Button.svelte';
  import { t } from '../i18n';
  import { apiFetchJSON } from '../lib/api';
  import { showToast } from '../stores';

  interface DATFile {
    name: string;
    path: string;
    size: number;
    exists: boolean;
    type: string;
    geo_type?: string;
  }

  interface GeoLookupResult {
    file: string;
    type: string;
    tag: string;
    rule: string;
    match_count: number;
    sample_matches: string[];
  }

  let {
    open: isOpen = false,
    availableFiles = [],
    onclose = () => {}
  }: {
    open?: boolean;
    availableFiles?: DATFile[];
    onclose?: () => void;
  } = $props();

  let query = $state('');
  let filterType: 'all' | 'domain' | 'ip' = $state('all');
  // Выбор пользователя; пока он пуст, а окно открыто, показываются все существующие файлы
  let chosenFiles: string[] = $state([]);
  let loading = $state(false);
  let error = $state('');
  let hasSearched = $state(false);
  let results: GeoLookupResult[] = $state([]);
  let copiedRule: string | null = $state(null);
  let showFileSelector = $state(false);

  const existingFileNames = $derived(
    availableFiles.filter((f: DATFile) => f.exists).map((f: DATFile) => f.name)
  );

  // Пустой выбор при открытом окне заменяется всеми существующими DAT-файлами
  // (в том числе сразу после «Снять все»)
  const selectedFiles = $derived(
    isOpen && chosenFiles.length === 0 && availableFiles.length > 0
      ? existingFileNames
      : chosenFiles
  );

  async function handleSearch() {
    const q = query.trim();
    if (!q) return;

    loading = true;
    error = '';
    hasSearched = true;
    try {
      let url = `/api/dat/lookup?query=${encodeURIComponent(q)}&type=${filterType}`;
      if (selectedFiles.length > 0 && selectedFiles.length < availableFiles.length) {
        url += `&files=${encodeURIComponent(selectedFiles.join(','))}`;
      }
      const data = await apiFetchJSON<GeoLookupResult[]>(url);
      results = Array.isArray(data) ? data : [];
    } catch (e: any) {
      if (e?.status === 401) return;
      error = e.message || 'Error executing lookup';
      results = [];
    } finally {
      loading = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleSearch();
    }
  }

  let copyTimer: ReturnType<typeof setTimeout>;
  function copyRule(rule: string) {
    navigator.clipboard.writeText(rule).catch(() => {});
    copiedRule = rule;
    showToast('success', $t('dat.geoscan_rule_copied', { rule }));
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copiedRule = null;
    }, 2000);
  }

  function toggleFile(name: string) {
    if (selectedFiles.includes(name)) {
      chosenFiles = selectedFiles.filter((f: string) => f !== name);
    } else {
      chosenFiles = [...selectedFiles, name];
    }
  }

  function selectAllFiles() {
    chosenFiles = [...existingFileNames];
  }

  function deselectAllFiles() {
    chosenFiles = [];
  }
</script>

<Modal open={isOpen} title={$t('dat.geoscan_title')} maxWidth="680px" {onclose}>
  <div class="geoscan-modal">
    <p class="geoscan-desc">{$t('dat.geoscan_desc')}</p>

    <!-- Search Input & Type Controls -->
    <div class="geoscan-search-bar">
      <div class="geoscan-input-wrapper">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          class="geoscan-search-icon"
          aria-hidden="true"
        >
          <circle cx="11" cy="11" r="8" />
          <path d="m21 21-4.35-4.35" />
        </svg>
        <input
          type="text"
          class="geoscan-input"
          placeholder={$t('dat.geoscan_query_placeholder')}
          bind:value={query}
          onkeydown={handleKeydown}
          disabled={loading}
        />
        {#if query}
          <button
            class="geoscan-clear-btn"
            type="button"
            onclick={() => (query = '')}
            aria-label={$t('app.clear')}
          >
            ✕
          </button>
        {/if}
      </div>

      <Button
        variant="primary"
        onclick={handleSearch}
        disabled={loading || !query.trim()}
        {loading}
      >
        {loading ? $t('dat.geoscan_searching') : $t('app.search')}
      </Button>
    </div>

    <!-- Filter Type Segmented Controls & File Filter Toggle -->
    <div class="geoscan-filters-row">
      <div class="geoscan-type-toggles" role="group" aria-label="Search filter type">
        <button
          type="button"
          class="geoscan-toggle-btn"
          class:active={filterType === 'all'}
          onclick={() => (filterType = 'all')}
        >
          {$t('dat.geoscan_type_all')}
        </button>
        <button
          type="button"
          class="geoscan-toggle-btn"
          class:active={filterType === 'domain'}
          onclick={() => (filterType = 'domain')}
        >
          {$t('dat.geoscan_type_domain')}
        </button>
        <button
          type="button"
          class="geoscan-toggle-btn"
          class:active={filterType === 'ip'}
          onclick={() => (filterType = 'ip')}
        >
          {$t('dat.geoscan_type_ip')}
        </button>
      </div>

      {#if availableFiles.length > 0}
        <button
          type="button"
          class="geoscan-file-filter-btn"
          onclick={() => (showFileSelector = !showFileSelector)}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            aria-hidden="true"
          >
            <polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3" />
          </svg>
          <span>{$t('dat.database_filter')} ({selectedFiles.length}/{availableFiles.length})</span>
        </button>
      {/if}
    </div>

    <!-- Collapsible File Selector -->
    {#if showFileSelector}
      <div class="geoscan-files-box">
        <div class="geoscan-files-header">
          <span class="geoscan-files-title">{$t('dat.select_databases')}</span>
          <div class="geoscan-files-actions">
            <button type="button" class="btn-link" onclick={selectAllFiles}
              >{$t('app.select_all')}</button
            >
            <span class="sep">·</span>
            <button type="button" class="btn-link" onclick={deselectAllFiles}
              >{$t('app.deselect_all')}</button
            >
          </div>
        </div>
        <div class="geoscan-files-grid">
          {#each availableFiles as file (file.name)}
            <label class="geoscan-file-item" class:disabled={!file.exists}>
              <input
                type="checkbox"
                checked={selectedFiles.includes(file.name)}
                disabled={!file.exists}
                onchange={() => toggleFile(file.name)}
              />
              <span class="file-name">{file.name}</span>
              <span class="file-type-badge">{file.type}</span>
            </label>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Error State -->
    {#if error}
      <div class="alert alert-error mt-3">{error}</div>
    {/if}

    <!-- Results Section -->
    <div class="geoscan-results-container">
      {#if loading}
        <div class="geoscan-state-box">
          <span class="spinner-circle"></span>
          <span>{$t('dat.geoscan_searching')}</span>
        </div>
      {:else if hasSearched && results.length === 0}
        <div class="geoscan-state-box geoscan-empty">
          <svg
            width="24"
            height="24"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="8" />
            <line x1="21" y1="21" x2="16.65" y2="16.65" />
            <line x1="8" y1="11" x2="14" y2="11" />
          </svg>
          <p>{$t('dat.geoscan_no_results')}</p>
        </div>
      {:else if results.length > 0}
        <div class="geoscan-results-header">
          <span>{$t('dat.geoscan_matches', { count: results.length })}</span>
        </div>
        <div class="geoscan-results-list">
          {#each results as res, i (i)}
            {@const isCopied = copiedRule === res.rule}
            <div class="geoscan-result-card">
              <div class="geoscan-card-top">
                <div class="geoscan-card-meta">
                  <span
                    class="badge badge--tag badge-emphasis"
                    class:badge-geoip={res.type === 'geoip'}
                  >
                    {res.type.toUpperCase()}
                  </span>
                  <span class="geoscan-filename">{res.file}</span>
                  <span class="geoscan-arrow">→</span>
                  <span class="geoscan-tag-name"><strong>{res.tag}</strong></span>
                  {#if res.match_count > 1}
                    <span class="geoscan-match-pill">+{res.match_count}</span>
                  {/if}
                </div>

                <div class="geoscan-rule-block">
                  <code class="geoscan-rule-code">{res.rule}</code>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm copy-btn"
                    class:copied={isCopied}
                    onclick={() => copyRule(res.rule)}
                    title={$t('dat.copy_rule_value', { val: res.rule })}
                  >
                    {#if isCopied}
                      <svg
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2.5"
                        aria-hidden="true"
                      >
                        <polyline points="20 6 9 17 4 12" />
                      </svg>
                      <span>{$t('app.copied')}</span>
                    {:else}
                      <svg
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                        aria-hidden="true"
                      >
                        <rect x="9" y="9" width="13" height="13" rx="2" />
                        <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                      </svg>
                      <span>{$t('app.copy')}</span>
                    {/if}
                  </button>
                </div>
              </div>

              {#if res.sample_matches && res.sample_matches.length > 0}
                <div class="geoscan-samples">
                  <span class="geoscan-samples-lbl">{$t('dat.geoscan_sample_matches')}</span>
                  <div class="geoscan-samples-tags">
                    {#each res.sample_matches as sample, i (i)}
                      <span class="sample-tag">{sample}</span>
                    {/each}
                  </div>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</Modal>

<style>
  .geoscan-modal {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3-5);
    padding-top: var(--spacing-1);
  }

  .geoscan-desc {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    line-height: 1.4;
  }

  .geoscan-search-bar {
    display: flex;
    gap: var(--spacing-2-5);
    align-items: center;
  }

  .geoscan-input-wrapper {
    position: relative;
    flex: 1;
    display: flex;
    align-items: center;
  }

  .geoscan-search-icon {
    position: absolute;
    left: 12px;
    color: var(--fg-dim);
    pointer-events: none;
  }

  .geoscan-input {
    width: 100%;
    padding: 9px 36px 9px 36px;
    font-size: var(--font-size-sm);
    background: var(--bg-card);
    color: var(--fg-primary);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    outline: none;
    transition:
      border-color var(--transition-fast),
      box-shadow var(--transition-fast);
  }

  .geoscan-input:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(var(--accent-rgb), 0.15);
  }

  .geoscan-clear-btn {
    position: absolute;
    right: 10px;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: var(--font-size-base);
    padding: var(--spacing-1);
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .geoscan-clear-btn:hover {
    color: var(--fg-primary);
    background: var(--surface-tint-strong);
  }

  .geoscan-filters-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .geoscan-type-toggles {
    display: inline-flex;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--spacing-0-5);
    gap: var(--spacing-0-5);
  }

  .geoscan-toggle-btn {
    background: transparent;
    border: none;
    padding: 5px var(--spacing-3);
    font-size: var(--font-size-xs);
    font-weight: 500;
    color: var(--fg-secondary);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition:
      background var(--transition-fast),
      color var(--transition-fast);
  }

  .geoscan-toggle-btn:hover {
    color: var(--fg-primary);
  }

  .geoscan-toggle-btn.active {
    background: var(--accent);
    color: var(--btn-primary-text);
  }

  .geoscan-file-filter-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1-5);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 5px var(--spacing-2-5);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    cursor: pointer;
    transition:
      color var(--transition-fast),
      border-color var(--transition-fast);
  }

  .geoscan-file-filter-btn:hover {
    border-color: var(--border-hover);
    color: var(--fg-primary);
  }

  .geoscan-files-box {
    background: rgba(0, 0, 0, 0.15);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: var(--spacing-2-5) var(--spacing-3-5);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .geoscan-files-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: var(--font-size-xs);
  }

  .geoscan-files-title {
    font-weight: 600;
    color: var(--fg-dim);
  }

  .geoscan-files-actions {
    display: flex;
    align-items: center;
    gap: var(--spacing-1-5);
  }

  .btn-link {
    background: none;
    border: none;
    color: var(--accent);
    font-size: var(--font-size-xs);
    cursor: pointer;
    padding: 0;
  }

  .btn-link:hover {
    text-decoration: underline;
  }

  .sep {
    color: var(--fg-dim);
  }

  .geoscan-files-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: var(--spacing-2);
    max-height: 140px;
    overflow-y: auto;
    padding-right: var(--spacing-1);
    scrollbar-width: thin;
  }

  .geoscan-file-item {
    display: flex;
    align-items: center;
    gap: var(--spacing-1-5);
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
    cursor: pointer;
    user-select: none;
  }

  .geoscan-file-item.disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .file-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .file-type-badge {
    font-size: var(--font-size-2xs);
    color: var(--fg-dim);
  }

  .geoscan-results-container {
    margin-top: var(--spacing-1);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    max-height: 380px;
    overflow-y: auto;
    scrollbar-width: thin;
    padding-right: var(--spacing-1);
  }

  .geoscan-state-box {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2-5);
    padding: var(--spacing-8) var(--spacing-4);
    color: var(--fg-dim);
    font-size: var(--font-size-sm);
  }

  .geoscan-state-box.geoscan-empty svg {
    color: var(--fg-dim);
  }

  .geoscan-results-header {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .geoscan-results-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .geoscan-result-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: var(--spacing-2-5) var(--spacing-3);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    transition: border-color var(--transition-fast);
  }

  .geoscan-result-card:hover {
    border-color: var(--border-hover);
  }

  .geoscan-card-top {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--spacing-2);
    flex-wrap: wrap;
  }

  .geoscan-card-meta {
    display: flex;
    align-items: center;
    gap: var(--spacing-1-5);
    font-size: var(--font-size-sm);
  }

  .geoscan-filename {
    color: var(--fg-secondary);
    font-family: var(--font-mono);
    font-size: var(--font-size-xs);
  }

  .geoscan-arrow {
    color: var(--fg-dim);
    font-size: var(--font-size-2xs);
  }

  .geoscan-tag-name {
    color: var(--fg-primary);
  }

  .geoscan-match-pill {
    background: rgba(var(--accent-rgb), 0.15);
    color: var(--accent);
    font-size: var(--font-size-2xs);
    padding: 1px 5px;
    border-radius: var(--radius-lg);
    font-weight: 600;
  }

  .geoscan-rule-block {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .geoscan-rule-code {
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 3px var(--spacing-2);
    font-size: var(--font-size-xs);
    font-family: var(--font-mono);
    color: var(--accent);
  }

  .copy-btn {
    padding: var(--spacing-1) var(--spacing-2);
    gap: var(--spacing-1);
  }

  .geoscan-samples {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
    border-top: 1px solid var(--border-light);
    padding-top: var(--spacing-1-5);
  }

  .geoscan-samples-lbl {
    color: var(--fg-dim);
    white-space: nowrap;
  }

  .geoscan-samples-tags {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
  }

  .sample-tag {
    background: var(--surface-tint);
    padding: 1px var(--spacing-1-5);
    border-radius: var(--radius-xs);
    font-family: var(--font-mono);
    color: var(--fg-secondary);
  }
</style>
