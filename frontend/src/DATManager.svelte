<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang, pluralize } from './i18n';
  import { showToast, capabilities } from './stores';
  import { apiFetch, apiFetchJSON } from './lib/api';
  import PageHeader from './PageHeader.svelte';
  import Button from './components/Button.svelte';
  import StatusBadge from './components/StatusBadge.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import GeoScanModal from './components/GeoScanModal.svelte';

  interface Props {
    onSwitchTab?: (tab: string) => void;
  }

  let { onSwitchTab = () => {} }: Props = $props();

  function autofocusAction(node: HTMLElement) {
    node.focus();
  }

  interface DATFile {
    name: string;
    path: string;
    size: number;
    last_update: number;
    exists: boolean;
    type: string;
    is_symlink: boolean;
    symlink_to?: string;
    tag_count?: number;
    record_count?: number;
    version?: string;
    info?: string;
    geo_type?: string;
    has_backup?: boolean;
  }

  interface DATTag {
    tag: string;
    count: number;
  }

  interface GeoLookupResult {
    file: string;
    type: string;
    tag: string;
    rule: string;
    match_count: number;
    sample_matches: string[];
  }

  let files: DATFile[] = $state([]);
  let loading = $state(false);
  let error = $state('');
  let globalUpdating = $state(false);
  let rollbacking = $state(false);
  let updatingFile: string | null = $state(null);
  let showGeoScanModal = $state(false);

  // Selected database for inspector
  let selectedFile: DATFile | null = $state(null);
  let kernelFilter: 'all' | 'xray' | 'mihomo' = $state('all');
  let fileSearch = $state('');

  // Quick GeoScan bar state
  let quickQuery = $state('');
  let quickType: 'all' | 'domain' | 'ip' = $state('all');
  let quickLoading = $state(false);
  let quickResults: GeoLookupResult[] = $state([]);
  let quickHasSearched = $state(false);
  let quickError = $state('');
  let quickCopiedRule: string | null = $state(null);
  let showQuickScan = $state(true);

  // Tag inspector state
  let tags: DATTag[] = $state([]);
  let tagsLoading = $state(false);
  let tagsError = $state('');
  let tagSearch = $state('');
  let copiedTag = $state('');
  let copiedPath = $state(false);

  // Entry drill-down state (viewing records of a tag)
  let activeTag: string | null = $state(null);
  let entries: string[] = $state([]);
  let entriesTotal = $state(0);
  let entriesPage = $state(0);
  let entriesHasMore = $state(false);
  let entriesLoading = $state(false);
  let entriesError = $state('');
  let entrySearch = $state('');
  let copiedEntry = $state('');

  async function fetchFiles() {
    loading = true;
    try {
      const res = await apiFetch('/api/dat/list');
      if (!res.ok) throw new Error('Failed to load DAT files');

      let data: DATFile[] = await res.json();
      // Sort files: xray first, then mihomo
      files = data.sort((a: DATFile, b: DATFile) => {
        if (a.type !== b.type) return a.type.localeCompare(b.type);
        return a.name.localeCompare(b.name);
      });

      // Auto-select first existing dat file if none selected
      if (!selectedFile && files.length > 0) {
        const preferred =
          files.find((f) => f.exists && f.name.toLowerCase().includes('geosite')) ||
          files.find((f) => f.exists && isDatFile(f)) ||
          files[0];
        if (preferred) {
          selectFile(preferred);
        }
      } else if (selectedFile) {
        // Sync selected file state with updated data
        const current = files.find(
          (f) => f.name === selectedFile?.name && f.path === selectedFile?.path
        );
        if (current) selectedFile = current;
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function selectFile(file: DATFile) {
    if (selectedFile?.name === file.name && selectedFile?.path === file.path && !activeTag) {
      return;
    }
    selectedFile = file;
    activeTag = null;
    tagSearch = '';
    copiedPath = false;

    if (isDatFile(file) && file.exists) {
      loadTags(file);
    } else {
      tags = [];
      tagsLoading = false;
      tagsError = '';
    }
  }

  async function loadTags(file: DATFile) {
    tagsLoading = true;
    tagsError = '';
    try {
      const res = await apiFetchJSON<any>(`/api/dat/tags?name=${encodeURIComponent(file.name)}`);
      const tagList = Array.isArray(res) ? res : res?.tags || [];
      tags = tagList;
    } catch (e: any) {
      if (e?.status === 401) return;
      tagsError = e.message;
      tags = [];
    } finally {
      tagsLoading = false;
    }
  }

  function selectTag(tagName: string) {
    if (!selectedFile) return;
    activeTag = tagName;
    entrySearch = '';
    entriesPage = 0;
    entries = [];
    entriesTotal = 0;
    entriesHasMore = false;
    loadEntries(true);
  }

  function backToTags() {
    activeTag = null;
    entrySearch = '';
    entries = [];
  }

  let searchDebounceTimer: ReturnType<typeof setTimeout>;
  function handleEntrySearch() {
    clearTimeout(searchDebounceTimer);
    searchDebounceTimer = setTimeout(() => {
      entriesPage = 0;
      loadEntries(true);
    }, 300);
  }

  async function loadEntries(replace = false) {
    if (!selectedFile || !activeTag) return;
    entriesLoading = true;
    entriesError = '';
    try {
      const url = `/api/dat/search?name=${encodeURIComponent(selectedFile.name)}&tag=${encodeURIComponent(activeTag)}&query=${encodeURIComponent(entrySearch)}&page=${entriesPage}`;
      const json = await apiFetchJSON<{ entries: string[]; total: number; has_more: boolean }>(url);

      if (replace) {
        entries = json.entries || [];
      } else {
        entries = [...entries, ...(json.entries || [])];
      }
      entriesTotal = json.total || 0;
      entriesHasMore = json.has_more || false;
    } catch (e: any) {
      if (e?.status === 401) return;
      entriesError = e.message;
    } finally {
      entriesLoading = false;
    }
  }

  function loadMoreEntries() {
    if (entriesLoading || !entriesHasMore) return;
    entriesPage += 1;
    loadEntries(false);
  }

  let entryCopyTimer: ReturnType<typeof setTimeout>;
  function copyEntry(entry: string) {
    navigator.clipboard.writeText(entry).catch(() => {});
    copiedEntry = entry;
    clearTimeout(entryCopyTimer);
    entryCopyTimer = setTimeout(() => {
      copiedEntry = '';
    }, 1500);
  }

  let copyTimer: ReturnType<typeof setTimeout>;
  function copyTagRule(file: DATFile, tag: string) {
    const value = getRuleValue(file, tag);
    navigator.clipboard.writeText(value).catch(() => {});
    copiedTag = tag;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copiedTag = '';
    }, 1500);
  }

  let pathCopyTimer: ReturnType<typeof setTimeout>;
  function copyFilePath(path: string) {
    navigator.clipboard.writeText(path).catch(() => {});
    copiedPath = true;
    showToast('success', $t('dat.path_copied'));
    clearTimeout(pathCopyTimer);
    pathCopyTimer = setTimeout(() => {
      copiedPath = false;
    }, 2000);
  }

  async function updateAll(filename?: string, fileType?: string) {
    if (filename) {
      updatingFile = filename;
    } else {
      globalUpdating = true;
    }
    error = '';
    try {
      const payload: { file?: string; type?: string } = {};
      if (filename) payload.file = filename;
      if (fileType) payload.type = fileType;
      const body = filename ? JSON.stringify(payload) : undefined;
      const headers: Record<string, string> = {};
      if (body) {
        headers['Content-Type'] = 'application/json';
      }
      const res = await apiFetch('/api/dat/update', {
        method: 'POST',
        headers,
        body
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text);
      }
      showToast(
        'success',
        filename ? `${filename}: ${$t('app.saved')}` : $t('dat.update_all') + ' OK'
      );
      await fetchFiles();
      if (selectedFile && isDatFile(selectedFile)) {
        await loadTags(selectedFile);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      error = e.message;
      showToast('error', e.message);
    } finally {
      globalUpdating = false;
      updatingFile = null;
    }
  }

  async function rollbackAll() {
    rollbacking = true;
    error = '';
    try {
      const res = await apiFetch('/api/dat/rollback', {
        method: 'POST'
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text);
      }
      showToast('success', $t('dat.rollback_success'));
      await fetchFiles();
      if (selectedFile && isDatFile(selectedFile)) {
        await loadTags(selectedFile);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      error = e.message;
      showToast('error', `${$t('dat.rollback_error')}: ${e.message}`);
    } finally {
      rollbacking = false;
    }
  }

  // Quick GeoScan handler
  async function handleQuickSearch() {
    const q = quickQuery.trim();
    if (!q) return;

    quickLoading = true;
    quickError = '';
    quickHasSearched = true;
    try {
      const url = `/api/dat/lookup?query=${encodeURIComponent(q)}&type=${quickType}`;
      const data = await apiFetchJSON<GeoLookupResult[]>(url);
      quickResults = Array.isArray(data) ? data : [];
    } catch (e: any) {
      if (e?.status === 401) return;
      quickError = e.message || 'Error executing lookup';
      quickResults = [];
    } finally {
      quickLoading = false;
    }
  }

  function handleQuickKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleQuickSearch();
    }
  }

  let quickCopyTimer: ReturnType<typeof setTimeout>;
  function copyQuickRule(rule: string) {
    navigator.clipboard.writeText(rule).catch(() => {});
    quickCopiedRule = rule;
    showToast('success', $t('dat.geoscan_rule_copied', { rule }));
    clearTimeout(quickCopyTimer);
    quickCopyTimer = setTimeout(() => {
      quickCopiedRule = null;
    }, 2000);
  }

  function jumpToResult(res: GeoLookupResult) {
    const targetFile = files.find((f) => f.name.toLowerCase() === res.file.toLowerCase());
    if (targetFile) {
      selectFile(targetFile);
      selectTag(res.tag);
    }
  }

  function getTagPrefix(file: DATFile): string {
    const name = file.name.toLowerCase();
    if (name.includes('geoip')) return 'geoip';
    if (name.includes('geosite')) return 'geosite';
    return file.name.replace(/\.dat$/i, '').toLowerCase();
  }

  function getRuleValue(file: DATFile, tag: string): string {
    const lower = file.name.toLowerCase();
    if (lower === 'geosite.dat') {
      return `geosite:${tag}`;
    }
    if (lower === 'geoip.dat') {
      return `geoip:${tag}`;
    }
    return `ext:${file.name}:${tag}`;
  }

  function formatSize(b: number): string {
    if (b >= 1024 * 1024) return (b / (1024 * 1024)).toFixed(2) + ' MB';
    if (b >= 1024) return (b / 1024).toFixed(2) + ' KB';
    return b + ' B';
  }

  const numLocale = $derived($currentLang === 'ru' ? 'ru-RU' : 'en-US');

  function formatDate(ts: number): string {
    if (!ts) return '-';
    return new Date(ts * 1000).toLocaleString(numLocale);
  }

  function isDatFile(file: DATFile): boolean {
    return file.name.toLowerCase().endsWith('.dat');
  }

  const DAT_STALE_DAYS = 30;
  const DAT_WARN_DAYS = 7;

  function fileAgeDays(file: DATFile): number {
    if (!file.last_update) return 999;
    return (Date.now() / 1000 - file.last_update) / 86400;
  }

  function getFileStatus(file: DATFile): 'missing' | 'outdated' | 'warning' | 'ok' {
    if (!file.exists) return 'missing';
    const age = fileAgeDays(file);
    if (age >= DAT_STALE_DAYS) return 'outdated';
    if (age >= DAT_WARN_DAYS) return 'warning';
    return 'ok';
  }

  function getStatusBadge(file: DATFile): {
    variant: 'running' | 'stopped' | 'warning';
    label: string;
  } {
    const s = getFileStatus(file);
    if (s === 'missing') return { variant: 'stopped', label: $t('dat.status_missing') };
    if (s === 'outdated') return { variant: 'warning', label: $t('dat.status_outdated') };
    if (s === 'warning') return { variant: 'warning', label: $t('dat.status_warning') };
    return { variant: 'running', label: 'OK' };
  }

  function getTypeBadge(file: DATFile): string {
    if (file.geo_type === 'geoip') return 'GEOIP';
    if (file.geo_type === 'geosite') return 'GEOSITE';
    const n = file.name.toLowerCase();
    if (n.includes('geoip')) return 'GEOIP';
    if (n.includes('geosite')) return 'GEOSITE';
    if (n.endsWith('.mmdb')) return 'MMDB';
    if (n.endsWith('.dat')) return 'DAT';
    return file.name.split('.').pop()?.toUpperCase() || 'FILE';
  }

  function getFreshnessPct(file: DATFile): number {
    if (!file.exists) return 0;
    const age = fileAgeDays(file);
    return Math.max(0, Math.min(100, 100 - (age / DAT_STALE_DAYS) * 100));
  }

  function getFreshnessColor(file: DATFile): string {
    const s = getFileStatus(file);
    if (s === 'outdated' || s === 'warning') return 'var(--warning)';
    return 'var(--success)';
  }

  function formatRelativeDate(ts: number): string {
    if (!ts) return '-';
    const diffSec = Math.floor(Date.now() / 1000 - ts);
    if (diffSec < 3600) return $t('dat.min_ago', { count: Math.floor(diffSec / 60) });
    if (diffSec < 86400) return $t('dat.hours_ago', { count: Math.floor(diffSec / 3600) });
    if (diffSec < 86400 * 30) return $t('dat.days_ago', { count: Math.floor(diffSec / 86400) });
    return formatDate(ts);
  }

  let activeKernel = $derived($capabilities?.active_kernel || null);

  // Filtered files in master column
  let displayedFiles = $derived(
    files.filter((f) => {
      if (kernelFilter === 'xray') {
        if (f.type !== 'xray') return false;
      } else if (kernelFilter === 'mihomo') {
        if (f.type !== 'mihomo') return false;
      } else if (activeKernel) {
        if (f.type === 'xray' && activeKernel !== 'xray') return false;
        if (f.type === 'mihomo' && activeKernel !== 'mihomo') return false;
      }
      if (fileSearch.trim()) {
        const q = fileSearch.toLowerCase();
        return f.name.toLowerCase().includes(q) || f.path.toLowerCase().includes(q);
      }
      return true;
    })
  );

  let filteredTags = $derived(
    tagSearch.trim()
      ? tags.filter((t) => t.tag.toLowerCase().includes(tagSearch.toLowerCase()))
      : tags
  );

  let canRollback = $derived(files.some((f) => f.has_backup));
  let actualCount = $derived(displayedFiles.filter((f) => getFileStatus(f) === 'ok').length);
  let missingCount = $derived(displayedFiles.filter((f) => !f.exists).length);
  let totalSize = $derived(displayedFiles.reduce((sum, f) => sum + (f.size || 0), 0));

  onMount(fetchFiles);
</script>

<div class="container">
  <PageHeader
    title={$t('dat.h1')}
    subtitle={$t('dat.h1_sub')}
    breadcrumbs={[{ label: $t('nav.group_tools') }, { label: $t('nav.dat') }]}
    {onSwitchTab}
    hideHome={true}
  >
    <Button variant="secondary" title={$t('dat.geoscan')} onclick={() => (showGeoScanModal = true)}>
      <svg
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <circle cx="11" cy="11" r="8" />
        <path d="m21 21-4.35-4.35" />
      </svg>
      {$t('dat.geoscan')}
    </Button>
    <Button
      variant="secondary"
      loading={rollbacking}
      disabled={rollbacking || loading || globalUpdating || updatingFile !== null || !canRollback}
      title={canRollback ? $t('dat.rollback_title') : $t('dat.rollback_none')}
      onclick={rollbackAll}
    >
      {#if !rollbacking}
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <polyline points="3 7 3 12 8 12" />
          <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
        </svg>
      {/if}
      {rollbacking ? $t('dat.rolling') : $t('dat.rollback')}
    </Button>
    <Button
      variant="primary"
      loading={globalUpdating}
      disabled={globalUpdating || loading || updatingFile !== null}
      title={$t('dat.update_all')}
      onclick={() => updateAll()}
    >
      {#if !globalUpdating}
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <polyline points="21 8 21 3 16 3" />
          <path d="M3 16v5h5M21 3l-9 9M3 21l9-9" />
        </svg>
      {/if}
      {globalUpdating ? $t('app.loading') : $t('dat.update_all')}
    </Button>
  </PageHeader>

  {#if error}
    <div class="alert alert-error mb-3">{error}</div>
  {/if}

  <!-- Stats Bar -->
  {#if !loading && displayedFiles.length > 0}
    <div class="stats mb-3">
      <span class="stat"
        ><b>{displayedFiles.length}</b>
        {pluralize(
          displayedFiles.length,
          $t('dat.files_one'),
          $t('dat.files_few'),
          $t('dat.files_many'),
          $currentLang
        )}</span
      >
      <span class="stat"
        ><b>{actualCount}</b>
        {pluralize(
          actualCount,
          $t('dat.active_one'),
          $t('dat.active_few'),
          $t('dat.active_many'),
          $currentLang
        )}</span
      >
      {#if missingCount > 0}
        <span class="stat" style="color: var(--warning);">
          <b>{missingCount}</b>
          {pluralize(
            missingCount,
            $t('dat.missing_one'),
            $t('dat.missing_few'),
            $t('dat.missing_many'),
            $currentLang
          )}
        </span>
      {/if}
      <span class="stat">
        {$t('dat.total_size')}
        <b>{formatSize(totalSize)}</b>
      </span>
    </div>
  {/if}

  <!-- Integrated Quick GeoScan Bar -->
  <div class="quick-scan-card card mb-3">
    <div class="quick-scan-header">
      <div class="qs-title-row">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          class="qs-icon"
        >
          <circle cx="11" cy="11" r="8" />
          <path d="m21 21-4.35-4.35" />
        </svg>
        <span class="qs-title">{$t('dat.quick_lookup')}</span>
        <span class="qs-subtitle">{$t('dat.quick_geoscan_desc')}</span>
      </div>

      <div class="qs-type-toggles" role="group" aria-label="Quick scan type">
        <button
          type="button"
          class="qs-type-btn"
          class:active={quickType === 'all'}
          onclick={() => (quickType = 'all')}
        >
          {$t('dat.geoscan_type_all')}
        </button>
        <button
          type="button"
          class="qs-type-btn"
          class:active={quickType === 'domain'}
          onclick={() => (quickType = 'domain')}
        >
          {$t('dat.geoscan_type_domain')}
        </button>
        <button
          type="button"
          class="qs-type-btn"
          class:active={quickType === 'ip'}
          onclick={() => (quickType = 'ip')}
        >
          {$t('dat.geoscan_type_ip')}
        </button>
      </div>
    </div>

    <div class="qs-input-row">
      <div class="qs-input-wrapper">
        <input
          type="text"
          class="qs-input"
          placeholder={$t('dat.quick_lookup_placeholder')}
          bind:value={quickQuery}
          onkeydown={handleQuickKeydown}
          disabled={quickLoading}
        />
        {#if quickQuery}
          <button
            class="qs-clear-btn"
            type="button"
            onclick={() => {
              quickQuery = '';
              quickResults = [];
              quickHasSearched = false;
            }}
            aria-label={$t('app.clear')}
          >
            ✕
          </button>
        {/if}
      </div>

      <Button
        variant="primary"
        onclick={handleQuickSearch}
        disabled={quickLoading || !quickQuery.trim()}
        loading={quickLoading}
      >
        {quickLoading ? $t('dat.geoscan_searching') : $t('app.search')}
      </Button>
    </div>

    <!-- Quick Scan Results Tray -->
    {#if quickError}
      <div class="alert alert-error mt-2">{quickError}</div>
    {/if}

    {#if quickHasSearched && !quickLoading}
      <div class="qs-results-tray">
        {#if quickResults.length === 0}
          <div class="qs-empty-state">
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
              <line x1="8" y1="11" x2="14" y2="11" />
            </svg>
            <span>{$t('dat.geoscan_no_results')}</span>
          </div>
        {:else}
          <div class="qs-results-header">
            <span>{$t('dat.geoscan_matches', { count: quickResults.length })}</span>
            <button
              class="btn-link"
              onclick={() => {
                quickResults = [];
                quickHasSearched = false;
              }}
            >
              {$t('app.close')}
            </button>
          </div>
          <div class="qs-chips-grid">
            {#each quickResults as res, i (i)}
              {@const isCopied = quickCopiedRule === res.rule}
              <div class="qs-result-chip">
                <div class="qs-chip-left">
                  <span class="badge badge-type" class:badge-geoip={res.type === 'geoip'}>
                    {res.type.toUpperCase()}
                  </span>
                  <span class="qs-chip-file">{res.file}</span>
                  <span class="qs-chip-arrow">→</span>
                  <span class="qs-chip-tag"><b>{res.tag}</b></span>
                  {#if res.match_count > 1}
                    <span class="qs-chip-count">+{res.match_count}</span>
                  {/if}
                </div>

                <div class="qs-chip-actions">
                  <code class="qs-chip-rule">{res.rule}</code>
                  <button
                    class="btn btn-secondary btn-sm"
                    class:copied={isCopied}
                    onclick={() => copyQuickRule(res.rule)}
                    title={$t('dat.copy_rule_value', { val: res.rule })}
                  >
                    {#if isCopied}
                      ✓ {$t('app.copied')}
                    {:else}
                      {$t('app.copy')}
                    {/if}
                  </button>
                  <button
                    class="btn btn-primary btn-sm"
                    onclick={() => jumpToResult(res)}
                    title={$t('dat.open_in_inspector')}
                  >
                    {$t('dat.open_in_inspector')} →
                  </button>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>

  <!-- Master-Detail Workspace -->
  {#if loading && files.length === 0}
    <p class="text-secondary">{$t('app.loading')}</p>
  {:else if files.length === 0}
    <p class="text-secondary">{$t('dat.no_files')}</p>
  {:else}
    <div class="dat-workspace" class:detail-open={selectedFile !== null}>
      <!-- Master Column: Database Catalog -->
      <div class="dat-master">
        <div class="master-card card">
          <div class="master-header">
            <div class="master-filters">
              <div class="segmented-pills">
                <button
                  class="pill-btn"
                  class:active={kernelFilter === 'all'}
                  onclick={() => (kernelFilter = 'all')}
                >
                  {$t('dat.filter_all')}
                </button>
                <button
                  class="pill-btn"
                  class:active={kernelFilter === 'xray'}
                  onclick={() => (kernelFilter = 'xray')}
                >
                  Xray
                </button>
                <button
                  class="pill-btn"
                  class:active={kernelFilter === 'mihomo'}
                  onclick={() => (kernelFilter = 'mihomo')}
                >
                  Mihomo
                </button>
              </div>
            </div>

            <div class="master-search">
              <input
                type="text"
                class="master-search-input"
                placeholder={$t('app.search')}
                bind:value={fileSearch}
              />
              {#if fileSearch}
                <button class="td-clear" onclick={() => (fileSearch = '')}>✕</button>
              {/if}
            </div>
          </div>

          <!-- File List -->
          <div class="master-list">
            {#each displayedFiles as file, i (i)}
              {@const isSelected =
                selectedFile?.name === file.name && selectedFile?.path === file.path}
              {@const status = getStatusBadge(file)}
              <div
                class="db-card-item dat-row"
                class:selected={isSelected}
                class:is-symlink={file.is_symlink}
                role="button"
                tabindex="0"
                onclick={() => selectFile(file)}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    selectFile(file);
                  }
                }}
              >
                <div class="db-card-left">
                  <div class="db-icon" class:warning={!file.exists}>
                    {#if file.is_symlink}
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
                        <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
                      </svg>
                    {:else}
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <circle cx="12" cy="12" r="10" />
                        <path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20" />
                        <path d="M2 12h20" />
                      </svg>
                    {/if}
                  </div>

                  <div class="db-info">
                    <div class="db-title-row">
                      <span class="db-name">{file.name}</span>
                      <span class="badge badge-type">{getTypeBadge(file)}</span>
                    </div>

                    <div class="db-sub-row">
                      <span class="db-size">{formatSize(file.size)}</span>
                      <span class="db-sep">·</span>
                      {#if file.is_symlink}
                        <span class="db-symlink">{$t('dat.symlink')}</span>
                        <span class="db-sep">·</span>
                      {/if}
                      {#if (file.geo_type === 'geosite' || file.name
                          .toLowerCase()
                          .includes('geosite')) && file.tag_count}
                        <span>{file.tag_count} {$t('dat.categories')}</span>
                        <span class="db-sep">·</span>
                      {:else if (file.geo_type === 'geoip' || file.name
                          .toLowerCase()
                          .includes('geoip')) && file.record_count}
                        <span>
                          {pluralize(
                            file.record_count,
                            $t('dat.record_count_one', {
                              count: file.record_count.toLocaleString(numLocale)
                            }),
                            $t('dat.record_count_few', {
                              count: file.record_count.toLocaleString(numLocale)
                            }),
                            $t('dat.record_count_many', {
                              count: file.record_count.toLocaleString(numLocale)
                            }),
                            $currentLang
                          )}
                        </span>
                        <span class="db-sep">·</span>
                      {/if}
                      <span>{formatRelativeDate(file.last_update)}</span>
                    </div>

                    <div class="stat-bar-wrapper">
                      <div class="stat-bar">
                        <div
                          class="stat-bar-fill"
                          style="width: {getFreshnessPct(file)}%; background: {getFreshnessColor(
                            file
                          )}"
                        ></div>
                      </div>
                      <StatusBadge variant={status.variant} label={status.label} />
                    </div>
                  </div>
                </div>

                <div class="db-actions">
                  <button
                    class="btn btn-secondary btn-icon-only"
                    class:btn-loading={updatingFile === file.name}
                    onclick={(e) => {
                      e.stopPropagation();
                      updateAll(file.name, file.type);
                    }}
                    disabled={globalUpdating || updatingFile !== null}
                    title={$t('dat.update_file')}
                  >
                    {#if updatingFile === file.name}
                      …
                    {:else}
                      ↓
                    {/if}
                  </button>
                </div>
              </div>
            {/each}
          </div>
        </div>
      </div>

      <!-- Detail Column: Live Inspector -->
      <div class="dat-detail">
        {#if selectedFile === null}
          <div class="card detail-empty-card">
            <EmptyState
              title={$t('dat.select_database')}
              description={$t('dat.select_database_desc')}
            />
          </div>
        {:else}
          <div class="card inspector-card">
            <!-- Inspector Header -->
            <div class="inspector-header">
              <div class="ih-left">
                <!-- Mobile back button -->
                <button
                  class="btn btn-secondary btn-sm mobile-back-btn"
                  onclick={() => (selectedFile = null)}
                >
                  ← {$t('dat.back_to_databases')}
                </button>

                <div class="ih-title-group">
                  <div class="ih-title-row">
                    <span class="ih-title">{selectedFile.name}</span>
                    <span class="badge badge-type">{getTypeBadge(selectedFile)}</span>
                    <StatusBadge
                      variant={getStatusBadge(selectedFile).variant}
                      label={getStatusBadge(selectedFile).label}
                    />
                  </div>

                  <div class="ih-path-row">
                    <code class="ih-path">{selectedFile.path}</code>
                    <button
                      class="btn-copy-path"
                      onclick={() => copyFilePath(selectedFile?.path || '')}
                      title={$t('dat.copy_path')}
                    >
                      {#if copiedPath}
                        ✓
                      {:else}
                        <svg
                          width="12"
                          height="12"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <rect x="9" y="9" width="13" height="13" rx="2" />
                          <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                        </svg>
                      {/if}
                    </button>
                    <span class="ih-meta">
                      · {formatSize(selectedFile.size)} ·
                      {$t('dat.updated')}
                      {formatRelativeDate(selectedFile.last_update)}
                    </span>
                  </div>
                </div>
              </div>

              <div class="ih-actions">
                <Button
                  variant="secondary"
                  class="btn-sm"
                  loading={updatingFile === selectedFile.name}
                  disabled={globalUpdating || updatingFile !== null}
                  onclick={() => selectedFile && updateAll(selectedFile.name, selectedFile.type)}
                >
                  {#if updatingFile === selectedFile.name}
                    {$t('dat.updating')}
                  {:else}
                    {$t('dat.update')}
                  {/if}
                </Button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm td-close"
                  onclick={() => (selectedFile = null)}
                  aria-label={$t('app.close')}
                  title={$t('app.close')}
                >
                  ✕
                </button>
              </div>
            </div>

            <!-- Inspector Content Area -->
            {#if !isDatFile(selectedFile)}
              <div class="inspector-body p-4">
                <div class="alert alert-info">
                  {$t('dat.non_dat_notice')}
                </div>
              </div>
            {:else if activeTag !== null}
              <!-- Drill-down: Entries Inside Tag -->
              <div class="inspector-drilldown">
                <div class="drilldown-subnav">
                  <div class="dd-nav-left">
                    <button class="btn-dd-back" onclick={backToTags} title={$t('dat.back_to_tags')}>
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      >
                        <line x1="19" y1="12" x2="5" y2="12" />
                        <polyline points="12 19 5 12 12 5" />
                      </svg>
                      <span>{$t('dat.back_to_tags')}</span>
                    </button>
                    <span class="dd-nav-sep">/</span>
                    <span class="dd-tag-name">{activeTag}</span>
                    {#if !entriesLoading && entriesTotal > 0}
                      <span class="badge badge-count">
                        {$t('dat.entries_count', { count: entriesTotal.toLocaleString(numLocale) })}
                      </span>
                    {/if}
                  </div>

                  <div class="dd-rule-hint">
                    <code>{getTagPrefix(selectedFile)}:{activeTag}</code>
                  </div>
                </div>

                <div class="inspector-search-bar">
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    class="search-bar-ico"
                  >
                    <circle cx="11" cy="11" r="8" />
                    <path d="m21 21-4.35-4.35" />
                  </svg>
                  <input
                    type="text"
                    class="inspector-search-input"
                    placeholder={$t('dat.search_entries')}
                    bind:value={entrySearch}
                    oninput={handleEntrySearch}
                    use:autofocusAction
                  />
                  {#if entrySearch}
                    <button
                      class="td-clear"
                      onclick={() => {
                        entrySearch = '';
                        handleEntrySearch();
                      }}
                    >
                      ✕
                    </button>
                  {/if}
                </div>

                <!-- Entries List -->
                <div class="inspector-scroll-area">
                  {#if entriesLoading && entries.length === 0}
                    <div class="td-state">
                      <span class="spinner-circle"></span>
                      <span>{$t('app.loading')}</span>
                    </div>
                  {:else if entriesError}
                    <div class="td-state td-state-error">{entriesError}</div>
                  {:else if entries.length === 0}
                    <div class="td-state">{$t('dat.no_entries')}</div>
                  {:else}
                    <div class="entries-list">
                      {#each entries as entry, i (i)}
                        {@const isCopied = copiedEntry === entry}
                        <div class="entry-row" class:copied={isCopied}>
                          <code class="entry-text">{entry}</code>
                          <button
                            class="entry-copy-btn"
                            onclick={() => copyEntry(entry)}
                            title={$t('dat.copy_entry')}
                          >
                            {#if isCopied}
                              <svg
                                width="12"
                                height="12"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="2.5"
                              >
                                <polyline points="20 6 9 17 4 12" />
                              </svg>
                            {:else}
                              <svg
                                width="12"
                                height="12"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="2"
                              >
                                <rect x="9" y="9" width="13" height="13" rx="2" />
                                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                              </svg>
                            {/if}
                          </button>
                        </div>
                      {/each}

                      {#if entriesHasMore}
                        <div class="load-more-box">
                          <button
                            class="btn btn-secondary btn-sm"
                            onclick={loadMoreEntries}
                            disabled={entriesLoading}
                          >
                            {#if entriesLoading}
                              <span
                                class="spinner-circle"
                                style="vertical-align: middle; margin-right: 6px;"
                              ></span>
                            {/if}
                            {$t('dat.load_more')}
                          </button>
                        </div>
                      {/if}
                    </div>
                  {/if}
                </div>
              </div>
            {:else}
              <!-- Tag Browser View -->
              <div class="inspector-tags-view">
                <div class="tag-rule-banner">
                  <!-- Не "banner-label": блокировщики рекламы скрывают этот класс -->
                  <span class="rule-format-label">
                    {#if getTagPrefix(selectedFile) === 'geoip'}
                      {$t('dat.geoip_rule_format')}
                    {:else}
                      {$t('dat.geosite_rule_format')}
                    {/if}
                  </span>
                  <code class="banner-format">
                    {#if getTagPrefix(selectedFile) === 'geoip'}
                      geoip:TAGNAME
                    {:else}
                      geosite:TAGNAME
                    {/if}
                  </code>
                </div>

                <div class="inspector-search-bar">
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    class="search-bar-ico"
                  >
                    <circle cx="11" cy="11" r="8" />
                    <path d="m21 21-4.35-4.35" />
                  </svg>
                  <input
                    type="text"
                    class="inspector-search-input"
                    placeholder={$t('dat.search_tag')}
                    bind:value={tagSearch}
                  />
                  {#if tagSearch}
                    <button class="td-clear" onclick={() => (tagSearch = '')}>✕</button>
                  {/if}
                  <span class="tag-counter">
                    {$t('dat.tags_count', { count: filteredTags.length })}
                  </span>
                </div>

                <div class="inspector-scroll-area">
                  {#if tagsLoading}
                    <div class="td-state">
                      <span class="spinner-circle"></span>
                      <span>{$t('app.loading')}</span>
                    </div>
                  {:else if tagsError}
                    <div class="td-state td-state-error">{tagsError}</div>
                  {:else if filteredTags.length === 0}
                    <div class="td-state">{$t('dat.no_tags_found')}</div>
                  {:else}
                    <div class="tags-grid">
                      {#each filteredTags as tagItem, i (i)}
                        {@const ruleVal = selectedFile
                          ? getRuleValue(selectedFile, tagItem.tag)
                          : tagItem.tag}
                        {@const isCopied = copiedTag === tagItem.tag}
                        <div class="tag-card-row" class:copied={isCopied}>
                          <button
                            class="tag-main-btn"
                            onclick={() => selectTag(tagItem.tag)}
                            title={$t('dat.inspect_tag')}
                          >
                            <span class="tag-name td-tag-name">{tagItem.tag}</span>
                            {#if tagItem.count > 0}
                              <span class="tag-count">
                                {tagItem.count.toLocaleString(numLocale)}
                                {pluralize(
                                  tagItem.count,
                                  $t('dat.record_one'),
                                  $t('dat.record_few'),
                                  $t('dat.record_many'),
                                  $currentLang
                                )}
                              </span>
                            {/if}
                            <span class="tag-arrow">→</span>
                          </button>

                          <button
                            class="tag-copy-btn"
                            onclick={() => selectedFile && copyTagRule(selectedFile, tagItem.tag)}
                            title={$t('dat.copy_rule_value', { val: ruleVal })}
                          >
                            {#if isCopied}
                              <svg
                                width="13"
                                height="13"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="2.5"
                              >
                                <polyline points="20 6 9 17 4 12" />
                              </svg>
                            {:else}
                              <svg
                                width="13"
                                height="13"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="2"
                              >
                                <rect x="9" y="9" width="13" height="13" rx="2" />
                                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                              </svg>
                            {/if}
                          </button>
                        </div>
                      {/each}
                    </div>
                  {/if}
                </div>
              </div>
            {/if}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<!-- Standalone Extended GeoScan Modal -->
<GeoScanModal
  isOpen={showGeoScanModal}
  availableFiles={files}
  onclose={() => (showGeoScanModal = false)}
/>

<style>
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

  /* ── Quick GeoScan Bar ── */
  .quick-scan-card {
    padding: 14px 18px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .quick-scan-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    margin-bottom: 12px;
  }

  .qs-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .qs-icon {
    color: var(--primary);
  }

  .qs-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .qs-subtitle {
    font-size: 12px;
    color: var(--fg-muted);
  }

  .qs-type-toggles {
    display: inline-flex;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 6px);
    padding: 2px;
    gap: 2px;
  }

  .qs-type-btn {
    background: transparent;
    border: none;
    padding: 4px 10px;
    font-size: 12px;
    font-weight: 500;
    color: var(--fg-secondary);
    border-radius: var(--radius-sm, 4px);
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .qs-type-btn:hover {
    color: var(--fg-primary);
  }

  .qs-type-btn.active {
    background: var(--primary);
    color: var(--btn-primary-text);
  }

  .qs-input-row {
    display: flex;
    gap: 10px;
    align-items: center;
  }

  .qs-input-wrapper {
    position: relative;
    flex: 1;
    display: flex;
    align-items: center;
  }

  .qs-input {
    width: 100%;
    padding: 8px 32px 8px 12px;
    font-size: 13px;
    background: var(--bg-surface);
    color: var(--fg-primary);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    outline: none;
    transition:
      border-color 0.15s ease,
      box-shadow 0.15s ease;
  }

  .qs-input:focus {
    border-color: var(--primary);
    box-shadow: 0 0 0 2px rgba(var(--primary-rgb, 59, 130, 246), 0.15);
  }

  .qs-clear-btn {
    position: absolute;
    right: 8px;
    background: transparent;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    font-size: 13px;
    padding: 2px 4px;
    border-radius: 50%;
  }

  .qs-clear-btn:hover {
    color: var(--fg-primary);
  }

  .qs-results-tray {
    margin-top: 14px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .qs-results-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
    color: var(--fg-secondary);
  }

  .qs-chips-grid {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 220px;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .qs-result-chip {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 10px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 6px 12px;
    flex-wrap: wrap;
  }

  .qs-chip-left {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }

  .qs-chip-file {
    font-family: var(--font-mono, monospace);
    color: var(--fg-secondary);
  }

  .qs-chip-arrow {
    color: var(--fg-muted);
  }

  .qs-chip-tag {
    color: var(--fg-primary);
  }

  .qs-chip-count {
    background: rgba(var(--primary-rgb, 59, 130, 246), 0.15);
    color: var(--primary);
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 10px;
    font-weight: 600;
  }

  .qs-chip-actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .qs-chip-rule {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    background: rgba(41, 194, 240, 0.08);
    border: 1px solid rgba(41, 194, 240, 0.25);
    border-radius: var(--radius-sm);
    padding: 3px 8px;
    color: var(--primary);
    letter-spacing: 0.2px;
  }

  .qs-empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 16px;
    color: var(--fg-muted);
    font-size: 13px;
  }

  .btn-link {
    background: none;
    border: none;
    color: var(--primary);
    font-size: 12px;
    cursor: pointer;
    padding: 0;
  }

  .btn-link:hover {
    text-decoration: underline;
  }

  /* ── Master-Detail Workspace Grid ── */
  .dat-workspace {
    display: grid;
    grid-template-columns: 380px 1fr;
    gap: 20px;
    align-items: start;
    min-height: 580px;
  }

  /* Master Column */
  .master-card {
    padding: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .master-header {
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: rgba(255, 255, 255, 0.01);
  }

  .master-filters {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .segmented-pills {
    display: flex;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 6px);
    padding: 2px;
    gap: 2px;
    width: 100%;
  }

  .pill-btn {
    flex: 1;
    background: transparent;
    border: none;
    padding: 5px 8px;
    font-size: 11px;
    font-weight: 500;
    color: var(--fg-secondary);
    border-radius: var(--radius-sm, 4px);
    cursor: pointer;
    text-align: center;
    transition: all 0.15s ease;
  }

  .pill-btn:hover {
    color: var(--fg-primary);
  }

  .pill-btn.active {
    background: var(--primary);
    color: var(--btn-primary-text);
  }

  .master-search {
    position: relative;
    display: flex;
    align-items: center;
  }

  .master-search-input {
    width: 100%;
    padding: 6px 28px 6px 10px;
    font-size: 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    outline: none;
  }

  .master-search-input:focus {
    border-color: var(--primary);
  }

  .master-list {
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 280px);
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .db-card-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition:
      background 0.15s ease,
      border-color 0.15s ease;
    user-select: none;
    gap: 10px;
    border-left: 3px solid transparent;
  }

  .db-card-item:last-child {
    border-bottom: none;
  }

  .db-card-item:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .db-card-item.selected {
    background: rgba(var(--primary-rgb, 59, 130, 246), 0.08);
    border-left-color: var(--primary);
  }

  .db-card-left {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-width: 0;
    flex: 1;
  }

  .db-icon {
    width: 28px;
    height: 28px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--primary);
    background: rgba(59, 130, 246, 0.08);
    flex-shrink: 0;
    margin-top: 2px;
  }

  .db-icon.warning {
    color: var(--error);
    background: rgba(239, 68, 68, 0.08);
    border-color: rgba(239, 68, 68, 0.2);
  }

  .db-info {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }

  .db-title-row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .db-name {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge-type {
    font-size: 10px;
    font-weight: 600;
    padding: 1px 5px;
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.06);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
  }

  .badge-geoip {
    color: var(--primary);
  }

  .db-sub-row {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    color: var(--fg-muted);
    flex-wrap: wrap;
  }

  .db-sep {
    color: var(--border-strong, rgba(255, 255, 255, 0.1));
  }

  .stat-bar-wrapper {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 3px;
  }

  .stat-bar {
    height: 4px;
    width: 60px;
    background: rgba(255, 255, 255, 0.06);
    border-radius: 2px;
    overflow: hidden;
    flex-shrink: 0;
  }

  .stat-bar-fill {
    height: 100%;
    border-radius: 2px;
    transition: width 0.3s ease;
  }

  .db-actions {
    flex-shrink: 0;
  }

  .btn-icon-only {
    padding: 4px 8px;
    line-height: 1;
    font-size: 13px;
    height: auto;
  }

  /* Detail Column (Inspector) */
  .dat-detail {
    min-width: 0;
  }

  .inspector-card {
    padding: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    min-height: 520px;
  }

  .inspector-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 18px;
    border-bottom: 1px solid var(--border);
    gap: 12px;
    flex-wrap: wrap;
    background: rgba(255, 255, 255, 0.015);
  }

  .ih-left {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    flex: 1;
  }

  .ih-title-group {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .ih-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .ih-title {
    font-size: 16px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .ih-path-row {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--fg-muted);
    flex-wrap: wrap;
  }

  .ih-path {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    background: rgba(0, 0, 0, 0.2);
    border-radius: var(--radius-sm);
    padding: 1px 5px;
    color: var(--fg-secondary);
  }

  .btn-copy-path {
    background: none;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
  }

  .btn-copy-path:hover {
    color: var(--fg-primary);
  }

  .mobile-back-btn {
    display: none;
  }

  /* Tag Rule Banner */
  .tag-rule-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 18px;
    background: rgba(41, 194, 240, 0.03);
    border-bottom: 1px solid var(--border);
    font-size: 12px;
    color: var(--fg-secondary);
    flex-wrap: wrap;
  }

  .banner-format {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    background: rgba(41, 194, 240, 0.1);
    color: var(--primary);
    border: 1px solid rgba(41, 194, 240, 0.2);
    border-radius: var(--radius-sm);
    padding: 2px 6px;
  }

  /* Inspector Search Bar */
  .inspector-search-bar {
    display: flex;
    align-items: center;
    padding: 10px 18px;
    border-bottom: 1px solid var(--border);
    gap: 8px;
    background: var(--bg-card);
  }

  .search-bar-ico {
    color: var(--fg-muted);
    flex-shrink: 0;
  }

  .inspector-search-input {
    flex: 1;
    background: none;
    border: none;
    color: var(--fg-primary);
    font-size: 13px;
    outline: none;
  }

  .inspector-search-input::placeholder {
    color: var(--fg-muted);
  }

  .tag-counter {
    font-size: 11px;
    color: var(--fg-muted);
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 1px 7px;
    white-space: nowrap;
  }

  .td-clear {
    background: none;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    font-size: 12px;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
  }

  .td-clear:hover {
    color: var(--fg-primary);
  }

  /* Inspector Scroll Area */
  .inspector-scroll-area {
    max-height: calc(100vh - 360px);
    overflow-y: auto;
    scrollbar-width: thin;
    padding: 8px 12px;
  }

  .tags-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
    gap: 8px;
  }

  .tag-card-row {
    display: flex;
    align-items: center;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    transition:
      border-color 0.15s ease,
      background 0.15s ease;
  }

  .tag-card-row:hover {
    border-color: var(--border-hover, var(--fg-muted));
    background: rgba(255, 255, 255, 0.03);
  }

  .tag-card-row.copied {
    border-color: var(--success);
    background: rgba(70, 209, 138, 0.08);
  }

  .tag-main-btn {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    min-width: 0;
    color: var(--fg-primary);
  }

  .tag-name {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }

  .tag-count {
    font-size: 11px;
    color: var(--fg-muted);
    background: rgba(255, 255, 255, 0.05);
    border-radius: 8px;
    padding: 1px 6px;
    white-space: nowrap;
  }

  .tag-arrow {
    color: var(--fg-muted);
    font-size: 12px;
    transition: transform 0.15s ease;
  }

  .tag-main-btn:hover .tag-arrow {
    transform: translateX(2px);
    color: var(--primary);
  }

  .tag-copy-btn {
    background: none;
    border: none;
    border-left: 1px solid var(--border);
    color: var(--fg-muted);
    cursor: pointer;
    padding: 8px 10px;
    display: flex;
    align-items: center;
    justify-content: center;
    transition:
      color 0.15s ease,
      background 0.15s ease;
  }

  .tag-copy-btn:hover {
    color: var(--primary);
    background: rgba(255, 255, 255, 0.05);
  }

  .tag-card-row.copied .tag-copy-btn {
    color: var(--success);
  }

  /* ── Drill-down View: Entries Inside Tag ── */
  .drilldown-subnav {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 18px;
    background: rgba(255, 255, 255, 0.02);
    border-bottom: 1px solid var(--border);
    gap: 10px;
    flex-wrap: wrap;
  }

  .dd-nav-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .btn-dd-back {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: none;
    border: none;
    color: var(--primary);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
  }

  .btn-dd-back:hover {
    text-decoration: underline;
  }

  .dd-nav-sep {
    color: var(--fg-muted);
  }

  .dd-tag-name {
    font-family: var(--font-mono, monospace);
    font-weight: 600;
    font-size: 13px;
    color: var(--fg-primary);
  }

  .badge-count {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    font-size: 11px;
    padding: 1px 6px;
    border-radius: 10px;
  }

  .dd-rule-hint code {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    background: rgba(41, 194, 240, 0.1);
    color: var(--primary);
    border: 1px solid rgba(41, 194, 240, 0.2);
    border-radius: var(--radius-sm);
    padding: 2px 6px;
  }

  .entries-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .entry-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    border-radius: var(--radius-sm);
    gap: 8px;
    transition: background 0.15s ease;
  }

  .entry-row:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .entry-row.copied {
    background: rgba(70, 209, 138, 0.08);
  }

  .entry-text {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
    color: var(--fg-primary);
    word-break: break-all;
  }

  .entry-copy-btn {
    background: none;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    padding: 4px;
    border-radius: var(--radius-sm);
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .entry-copy-btn:hover {
    color: var(--fg-primary);
  }

  .entry-row.copied .entry-copy-btn {
    color: var(--success);
  }

  .load-more-box {
    padding: 12px;
    text-align: center;
  }

  /* Shared States */
  .td-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 40px 20px;
    color: var(--fg-muted);
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

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  /* Responsive Behavior */
  @media (max-width: 1024px) {
    .dat-workspace {
      grid-template-columns: 1fr;
    }

    .dat-workspace.detail-open .dat-master {
      display: none;
    }

    .dat-workspace:not(.detail-open) .dat-detail {
      display: none;
    }

    .mobile-back-btn {
      display: inline-flex;
    }

    .tags-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
