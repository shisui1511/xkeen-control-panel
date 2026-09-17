<script lang="ts">
  import { t } from '../i18n';
  import { showToast } from '../stores';
  import ResizableSplitter from './ResizableSplitter.svelte';
  import { jsonLanguage } from '@codemirror/lang-json';
  import { yamlLanguage } from '@codemirror/lang-yaml';
  import { highlightTree, classHighlighter } from '@lezer/highlight';

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

  let previewWidth = $state<number>(440);
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

  function escapeHtml(str: string): string {
    return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function highlightCode(code: string, lang: 'json' | 'yaml'): string {
    if (!code) return '';
    try {
      const parser = lang === 'yaml' ? yamlLanguage.parser : jsonLanguage.parser;
      const tree = parser.parse(code);
      let pos = 0;
      let html = '';
      highlightTree(tree, classHighlighter, (from, to, classes) => {
        if (from > pos) {
          html += escapeHtml(code.slice(pos, from));
        }
        html += `<span class="${classes}">${escapeHtml(code.slice(from, to))}</span>`;
        pos = to;
      });
      if (pos < code.length) {
        html += escapeHtml(code.slice(pos));
      }
      return html;
    } catch {
      return escapeHtml(code);
    }
  }

  const highlightedHtml = $derived(highlightCode(content, language));
</script>

{#if isOpen}
  <ResizableSplitter {storageKey} bind:width={previewWidth} />

  <div class="constructor-preview-wrapper" style="width: {previewWidth}px;">
    <div class="preview-card">
      {#if tabs && tabs.length > 0}
        <!-- Tabbed Header (Xray style) -->
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

        <div class="preview-toolbar">
          <span class="preview-meta-size">{fileSize}</span>
          <div class="preview-tools-right">
            <button
              type="button"
              class="btn btn-sm btn-secondary btn-tool-action"
              onclick={handleCopy}
              title={$t(language === 'yaml' ? 'mihomo.copy_yaml' : 'xray.copy_json')}
            >
              {#if copyFeedback}
                <svg
                  width="11"
                  height="11"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="var(--success)"
                  stroke-width="2.5"
                >
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              {:else}
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
              {/if}
              <span>{$t(copyFeedback ? 'app.copied' : 'app.copy')}</span>
            </button>
            <button
              type="button"
              class="btn btn-sm btn-secondary btn-tool-action"
              onclick={handleDownload}
              title={$t(language === 'yaml' ? 'mihomo.download_yaml_title' : 'xray.download_json')}
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
          </div>
        </div>
      {:else}
        <!-- Single Header (Mihomo original style) -->
        <div class="preview-header">
          <div class="preview-title-wrap">
            <span class="preview-title"
              >{title ||
                (language === 'yaml'
                  ? 'YAML ' + $t('mihomo.preview')
                  : 'JSON ' + $t('editor.constructor_preview'))}</span
            >
            <span class="preview-size-badge">{fileSize}</span>
          </div>
          <div class="preview-header-actions">
            <button
              type="button"
              class="btn btn-sm btn-secondary btn-tool-action"
              onclick={handleCopy}
              title={$t(language === 'yaml' ? 'mihomo.copy_yaml' : 'xray.copy_json')}
            >
              {#if copyFeedback}
                <svg
                  width="11"
                  height="11"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="var(--success)"
                  stroke-width="2.5"
                >
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              {:else}
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
              {/if}
              <span>{$t(copyFeedback ? 'app.copied' : 'app.copy')}</span>
            </button>
            <button
              type="button"
              class="btn btn-sm btn-secondary btn-tool-action"
              onclick={handleDownload}
              title={$t(language === 'yaml' ? 'mihomo.download_yaml_title' : 'xray.download_json')}
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
          </div>
        </div>
      {/if}

      <!-- eslint-disable svelte/no-at-html-tags -->
      <pre
        id={testId}
        class="constructor-preview-panel {language === 'yaml' ? 'yaml-preview' : ''}"
        data-testid={testId || 'constructor-preview'}><code>{@html highlightedHtml}</code></pre>
      <!-- eslint-enable svelte/no-at-html-tags -->
    </div>

    {#if children}
      <div class="constructor-preview-footer">
        {@render children()}
      </div>
    {/if}
  </div>
{/if}

<style>
  .constructor-preview-wrapper {
    flex-shrink: 0;
    min-width: 280px;
    max-width: 800px;
    display: flex;
    flex-direction: column;
    position: sticky;
    top: 16px;
    height: calc(100vh - 160px);
  }

  .preview-card {
    display: flex;
    flex-direction: column;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    flex: 1;
    min-height: 0;
  }

  .preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .preview-title-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .preview-title {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-dim);
  }

  .preview-size-badge {
    font-size: var(--font-size-xs);
    background: var(--surface-tint);
    border: 1px solid var(--border-light);
    color: var(--fg-secondary);
    padding: 1px 6px;
    border-radius: 10px;
  }

  .preview-header-actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .preview-tabs-bar {
    display: flex;
    align-items: center;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    scrollbar-width: none;
    padding: 2px 4px 0 4px;
    gap: 2px;
    flex-shrink: 0;
  }

  .preview-tabs-bar::-webkit-scrollbar {
    display: none;
  }

  .preview-tab-btn {
    padding: 6px 10px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--fg-secondary);
    font-size: 0.75rem;
    font-family: var(--font-mono);
    cursor: pointer;
    white-space: nowrap;
    transition:
      color var(--transition-fast),
      border-color var(--transition-fast);
    margin-bottom: -1px;
  }

  .preview-tab-btn:hover {
    color: var(--fg-primary);
  }

  .preview-tab-btn.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
    font-weight: 600;
  }

  .preview-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 10px;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .preview-meta-size {
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
    font-family: var(--font-mono);
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
    font-size: var(--font-size-xs);
  }

  .constructor-preview-panel {
    flex: 1;
    margin: 0;
    padding: 14px 16px;
    background: var(--code-bg);
    color: var(--code-fg);
    border: none;
    font-family: var(--font-mono);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
    min-height: 0;
  }

  .constructor-preview-panel code {
    font-family: inherit;
    font-size: inherit;
    color: inherit;
    background: transparent;
    padding: 0;
    border: none;
  }

  .constructor-preview-panel :global(.tok-propertyName) {
    color: var(--code-fg);
  }

  .constructor-preview-panel :global(.tok-string) {
    color: var(--code-string);
  }

  .constructor-preview-panel :global(.tok-number) {
    color: var(--code-number);
  }

  .constructor-preview-panel :global(.tok-keyword),
  .constructor-preview-panel :global(.tok-bool) {
    color: var(--code-key);
  }

  .constructor-preview-panel :global(.tok-null) {
    color: var(--code-null);
  }

  .constructor-preview-panel :global(.tok-punctuation) {
    color: var(--code-punctuation);
  }

  .constructor-preview-panel :global(.tok-comment) {
    color: var(--code-comment);
  }

  .constructor-preview-footer {
    margin-top: 12px;
    flex-shrink: 0;
  }

  @media (max-width: 1024px) {
    .constructor-preview-wrapper {
      width: 100% !important;
      position: static;
      height: auto;
      margin-top: var(--spacing-3);
    }

    .constructor-preview-panel {
      max-height: 500px;
    }
  }
</style>
