<script lang="ts">
  import { t, currentLang } from '../../../i18n';
  import Button from '../../Button.svelte';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import DiffView from '../DiffView.svelte';
  import {
    fetchApplies,
    fetchApplyContent,
    type ApplyCopy,
    type LayerFile
  } from '../../../lib/configLayer';

  // «Последние применения» для файлов панели (D-06, CFG-03).
  // До 5 строк «Версия до применения от …» со сравнением с текущим файлом.
  // Кнопки отката нет: отката к отдельному применению для файла панели нет (D-06).
  let {
    file,
    current = ''
  }: {
    file: LayerFile;
    current?: string;
  } = $props();

  let loading = $state(true);
  let error = $state('');
  let applies = $state<ApplyCopy[]>([]);
  let selectedId = $state<string | null>(null);
  let selectedContent = $state<string | null>(null);
  let selectedTruncated = $state(false);
  let contentLoading = $state(false);
  let contentError = $state('');

  function formatDate(iso: string): string {
    try {
      const d = new Date(iso);
      if (isNaN(d.getTime())) return iso;
      return d.toLocaleString($currentLang === 'ru' ? 'ru-RU' : 'en-US');
    } catch {
      return iso;
    }
  }

  // Счётчики запросов: поздний ответ устаревшего запроса не должен подменять
  // список или содержимое, показанные для текущего файла и выбранной строки.
  let listSeq = 0;
  let contentSeq = 0;

  async function load(): Promise<void> {
    const mine = ++listSeq;
    contentSeq++; // отменить идущий запрос содержимого
    const key = file.key;
    loading = true;
    error = '';
    selectedId = null;
    selectedContent = null;
    contentLoading = false;
    try {
      const res = await fetchApplies(key);
      if (mine !== listSeq) return;
      applies = res.applies ?? [];
    } catch (e) {
      if (mine === listSeq) error = e instanceof Error ? e.message : String(e);
    } finally {
      if (mine === listSeq) loading = false;
    }
  }

  async function loadContent(item: ApplyCopy): Promise<void> {
    const mine = ++contentSeq;
    const key = file.key;
    selectedContent = null;
    selectedTruncated = false;
    contentLoading = true;
    contentError = '';
    try {
      const res = await fetchApplyContent(key, item.id);
      if (mine !== contentSeq || selectedId !== item.id) return;
      selectedContent = res.content;
      selectedTruncated = res.truncated;
    } catch (e) {
      if (mine === contentSeq) contentError = e instanceof Error ? e.message : String(e);
    } finally {
      if (mine === contentSeq) contentLoading = false;
    }
  }

  function toggleApply(item: ApplyCopy): void {
    if (selectedId === item.id) {
      contentSeq++;
      selectedId = null;
      selectedContent = null;
      contentLoading = false;
      return;
    }
    selectedId = item.id;
    void loadContent(item);
  }

  $effect(() => {
    void file.key;
    void load();
  });
</script>

<div class="files-applies-list" data-testid="files-applies-list">
  <div class="applies-header">
    <h3 class="applies-title">{$t('cfg.files.applies_title')}</h3>
    <p class="applies-note">{$t('cfg.files.applies_note')}</p>
  </div>

  {#if loading}
    <div class="applies-loading" aria-busy="true">
      <Skeleton type="text-line" width="70%" />
      <Skeleton type="text-line" width="85%" />
      <Skeleton type="text-line" width="60%" />
    </div>
  {:else if error}
    <div class="alert alert-error applies-alert" role="alert">
      <span>{$t('cfg.files.applies_error')}</span>
      <Button variant="secondary" size="sm" onclick={() => void load()}>
        {$t('app.retry')}
      </Button>
    </div>
  {:else if applies.length === 0}
    <div class="applies-empty">
      <EmptyState title={$t('editor.backups_empty')} description="" plain={true} />
    </div>
  {:else}
    <div class="applies-items">
      {#each applies.slice(0, 5) as item (item.id)}
        <div class="apply-row" class:active={selectedId === item.id}>
          <button
            type="button"
            class="apply-row-btn"
            aria-expanded={selectedId === item.id ? 'true' : 'false'}
            onclick={() => toggleApply(item)}
          >
            <span class="apply-row-title">
              {$t('cfg.files.apply_row', { date: formatDate(item.at) })}
            </span>
          </button>
          {#if selectedId === item.id}
            <div class="apply-diff-container">
              {#if contentLoading}
                <div class="diff-loading">
                  <span class="spinner" aria-hidden="true"></span>
                </div>
              {:else if contentError}
                <div class="alert alert-error" role="alert">
                  <span>{contentError}</span>
                  <Button variant="secondary" size="sm" onclick={() => toggleApply(item)}>
                    {$t('app.retry')}
                  </Button>
                </div>
              {:else if selectedContent !== null}
                <DiffView
                  expected={current}
                  actual={selectedContent}
                  truncated={selectedTruncated}
                />
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .files-applies-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-3);
    height: 100%;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .applies-header {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .applies-title {
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .applies-note {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .applies-loading {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1-5);
    padding: var(--spacing-2) 0;
  }

  .applies-alert {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
  }

  .applies-empty {
    padding: var(--spacing-3) 0;
  }

  .applies-items {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1-5);
  }

  .apply-row {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-card);
    overflow: hidden;
  }

  .apply-row.active {
    border-color: var(--border-strong);
  }

  .apply-row-btn {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: var(--spacing-1-5) var(--spacing-2);
    background: transparent;
    border: none;
    color: var(--fg-primary);
    font-size: var(--font-size-xs);
    text-align: left;
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .apply-row-btn:hover {
    background: var(--bg-hover);
  }

  .apply-diff-container {
    padding: var(--spacing-2);
    border-top: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .diff-loading {
    display: flex;
    justify-content: center;
    padding: var(--spacing-3);
  }
</style>
