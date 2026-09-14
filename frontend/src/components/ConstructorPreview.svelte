<script lang="ts">
  import { t } from '../i18n';
  import { showToast } from '../stores';
  import ResizableSplitter from './ResizableSplitter.svelte';

  export interface PreviewTab {
    id: string;
    title: string;
  }

  interface Props {
    content: string;
    language?: 'json' | 'yaml';
    storageKey: string;
    isOpen?: boolean;
    onClose?: () => void;
    title?: string;
    tabs?: PreviewTab[];
    activeTab?: string;
    onTabChange?: (tabId: string) => void;
    fileName?: string;
    testId?: string;
    children?: import('svelte').Snippet;
  }

  let {
    content,
    language = 'json',
    storageKey,
    isOpen = true,
    onClose,
    title,
    tabs,
    activeTab,
    onTabChange,
    fileName,
    testId,
    children
  }: Props = $props();

  let previewWidth = $state(440);
  let copyFeedback = $state(false);

  const fileSize = $derived.by(() => {
    if (!content) return '0 B';
    const bytes = new Blob([content]).size;
    if (bytes < 1024) return `${bytes} B`;
    return `${(bytes / 1024).toFixed(1)} KB`;
  });

  function handleCopy() {
    if (!content) return;
    navigator.clipboard
      .writeText(content)
      .then(() => {
        copyFeedback = true;
        showToast('success', $t('app.copied'));
        setTimeout(() => {
          copyFeedback = false;
        }, 2000);
      })
      .catch((err) => {
        console.warn('Clipboard write rejected:', err);
      });
  }

  function handleDownload() {
    if (!content) return;
    const defaultName = language === 'yaml' ? 'config.yaml' : 'config.json';
    const name = fileName || defaultName;
    const mimeType = language === 'yaml' ? 'text/yaml' : 'application/json';
    const blob = new Blob([content], { type: mimeType });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    a.click();
    URL.revokeObjectURL(url);
  }
</script>

{#if isOpen}
  <ResizableSplitter {storageKey} bind:width={previewWidth} />

  <div class="constructor-preview-wrapper" style="width: {previewWidth}px;">
    <div class="preview-card">
      {#if tabs && tabs.length > 0}
        <div class="preview-tabs-bar">
          {#each tabs as tab}
            <button
              type="button"
              class="preview-tab-btn"
              class:active={activeTab === tab.id}
              onclick={() => onTabChange?.(tab.id)}
            >
              {tab.title}
            </button>
          {/each}
        </div>
      {:else if title}
        <div class="preview-header-title">
          <span class="title-text">{title}</span>
          {#if onClose}
            <button
              type="button"
              class="btn-close-header"
              onclick={onClose}
              aria-label={$t('app.close')}
            >
              ✕
            </button>
          {/if}
        </div>
      {/if}

      <div class="preview-toolbar">
        <span class="preview-meta-size">{fileSize}</span>
        <div class="preview-tools-right">
          <button
            type="button"
            class="btn btn-sm btn-secondary btn-tool-action"
            onclick={handleCopy}
            title={$t('xray.copy_json')}
          >
            <svg
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
              <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
            </svg>
            <span>{$t(copyFeedback ? 'app.copied' : 'app.copy')}</span>
          </button>
          <button
            type="button"
            class="btn btn-sm btn-secondary btn-tool-action"
            onclick={handleDownload}
            title={$t('xray.download_json')}
          >
            <svg
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="7 10 12 15 17 10" />
              <line x1="12" y1="15" x2="12" y2="3" />
            </svg>
            <span>{$t('app.download')}</span>
          </button>
          {#if onClose && (!tabs || tabs.length === 0) && !title}
            <button
              type="button"
              class="btn btn-sm btn-secondary btn-tool-action"
              onclick={onClose}
              title={$t('app.close')}
            >
              <span>✕</span>
            </button>
          {/if}
        </div>
      </div>

      <pre
        id={testId}
        class="constructor-preview-panel yaml-preview"
        data-testid={testId || 'constructor-preview'}><code>{content}</code></pre>
    </div>

    {#if children}
      {@render children()}
    {/if}
  </div>
{/if}

<style>
  .constructor-preview-wrapper {
    flex-shrink: 0;
    min-width: 280px;
    max-width: 100%;
    display: flex;
    flex-direction: column;
  }

  .preview-card {
    display: flex;
    flex-direction: column;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15);
  }

  .preview-header-title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
  }

  .title-text {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .btn-close-header {
    background: transparent;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    padding: 2px 6px;
    font-size: 13px;
    border-radius: 4px;
    transition: color var(--transition-fast);
  }

  .btn-close-header:hover {
    color: var(--fg-primary);
  }

  .preview-tabs-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 2px;
    padding: 4px 6px;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
  }

  .preview-tab-btn {
    padding: 4px 8px;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-secondary);
    font-size: 0.75rem;
    font-family: var(--font-family-mono);
    cursor: pointer;
    transition:
      background-color var(--transition-fast),
      color var(--transition-fast);
  }

  .preview-tab-btn:hover {
    color: var(--fg-primary);
    background: var(--bg-surface-active);
  }

  .preview-tab-btn.active {
    background: var(--bg-card);
    color: var(--accent);
    font-weight: 600;
  }

  .preview-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px var(--spacing-3);
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
  }

  .preview-meta-size {
    font-size: 0.75rem;
    color: var(--fg-muted);
    font-family: var(--font-family-mono);
  }

  .preview-tools-right {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .btn-tool-action {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 24px;
    padding: 2px 8px;
    font-size: 0.75rem;
  }

  .constructor-preview-panel {
    flex: 1;
    margin: 0;
    padding: var(--spacing-3);
    background: var(--code-bg);
    color: var(--code-fg);
    border: none;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    overflow: auto;
    scrollbar-width: thin;
    max-height: 600px;
    min-height: 320px;
  }

  .constructor-preview-panel code {
    font-family: inherit;
    font-size: inherit;
    color: inherit;
    background: transparent;
    padding: 0;
    border: none;
  }

  @media (max-width: 1024px) {
    .constructor-preview-wrapper {
      width: 100% !important;
      margin-top: var(--spacing-3);
    }
  }
</style>
