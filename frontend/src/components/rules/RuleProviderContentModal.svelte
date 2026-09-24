<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { fetchRuleProviderContent } from '../../lib/api';
  import Modal from '../Modal.svelte';
  import Button from '../Button.svelte';

  interface Props {
    /** Provider to show. The parent re-creates the modal per provider. */
    name: string;
    onclose: () => void;
  }

  let { name, onclose }: Props = $props();

  const PAGE = 200;
  const SEARCH_DELAY_MS = 300;

  let query = $state('');
  let entries = $state<string[]>([]);
  let total = $state(0);
  let matched = $state(0);
  let loading = $state(false);
  let error = $state('');

  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  let requestSeq = 0;

  const hasMore = $derived(entries.length < matched);

  async function load(reset: boolean) {
    const seq = ++requestSeq;
    loading = true;
    error = '';
    try {
      const page = await fetchRuleProviderContent(name, query, reset ? 0 : entries.length, PAGE);
      if (seq !== requestSeq) return;
      entries = reset ? page.entries : [...entries, ...page.entries];
      total = page.total;
      matched = page.matched;
    } catch (e: any) {
      if (seq !== requestSeq) return;
      if (e?.status === 401) return;
      error = e?.message || String(e);
      if (reset) entries = [];
    } finally {
      if (seq === requestSeq) loading = false;
    }
  }

  function handleSearch(e: Event) {
    query = (e.currentTarget as HTMLInputElement).value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => load(true), SEARCH_DELAY_MS);
  }

  function close() {
    clearTimeout(searchTimer);
    requestSeq++;
    onclose();
  }

  onMount(() => {
    load(true);
    return () => clearTimeout(searchTimer);
  });
</script>

<Modal
  isOpen={true}
  title={$t('rules.provider_content_title', { name })}
  maxWidth="720px"
  onclose={close}
>
  <div class="content-modal">
    <label class="visually-hidden" for="provider-content-search">
      {$t('rules.provider_content_search')}
    </label>
    <input
      id="provider-content-search"
      class="input font-mono"
      type="search"
      placeholder={$t('rules.provider_content_search')}
      value={query}
      oninput={handleSearch}
      autocomplete="off"
      spellcheck="false"
    />

    <p class="content-stats" aria-live="polite">
      {#if query}
        {$t('rules.provider_content_stats_filtered', {
          shown: String(entries.length),
          matched: String(matched),
          total: String(total)
        })}
      {:else}
        {$t('rules.provider_content_stats', {
          shown: String(entries.length),
          total: String(total)
        })}
      {/if}
    </p>

    {#if error}
      <p class="content-error" role="alert">{$t('rules.provider_content_error')}: {error}</p>
    {:else if !loading && entries.length === 0}
      <p class="content-empty">
        {query ? $t('rules.provider_content_no_match') : $t('rules.provider_content_empty')}
      </p>
    {:else}
      <ol class="content-list" start={1}>
        {#each entries as entry, i (i)}
          <li class="font-mono">{entry}</li>
        {/each}
      </ol>
    {/if}

    <div class="content-footer">
      {#if loading}
        <span class="spinner" aria-hidden="true"></span>
        <span class="content-loading">{$t('rules.provider_content_loading')}</span>
      {:else if hasMore}
        <Button variant="secondary" class="btn-sm" onclick={() => load(false)}>
          {$t('rules.provider_content_more')}
        </Button>
      {/if}
    </div>
  </div>
</Modal>

<style>
  .content-modal {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .content-stats,
  .content-empty,
  .content-loading {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--fg-muted);
  }

  .content-error {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--danger);
    word-break: break-word;
  }

  .content-list {
    margin: 0;
    padding: var(--spacing-2) var(--spacing-3) var(--spacing-2) 3.5em;
    max-height: 55vh;
    overflow-y: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb) transparent;
  }

  .content-list li {
    padding: 1px 0;
    word-break: break-all;
  }

  .content-list li::marker {
    color: var(--fg-faint);
  }

  .content-footer {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2);
    min-height: 28px;
  }

  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
</style>
