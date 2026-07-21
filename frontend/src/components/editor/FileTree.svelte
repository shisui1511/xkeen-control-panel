<script lang="ts">
  import { t } from '../../i18n';

  interface ConfigFileInfo {
    name: string;
    path: string;
    size: number;
  }

  let {
    xrayFiles = [],
    mihomoFiles = [],
    selectedFile = '',
    activeKernel = '',
    onLoadFile
  }: {
    xrayFiles: ConfigFileInfo[];
    mihomoFiles: ConfigFileInfo[];
    selectedFile: string;
    activeKernel: string;
    onLoadFile: (path: string, isPreviewClick: boolean) => void;
  } = $props();

  let fileSearchQuery = $state('');

  let filteredXrayFiles = $derived(
    xrayFiles.filter((file) => file.name.toLowerCase().includes(fileSearchQuery.toLowerCase()))
  );

  let filteredMihomoFiles = $derived(
    mihomoFiles.filter((file) => file.name.toLowerCase().includes(fileSearchQuery.toLowerCase()))
  );

  const xrayDir = '/opt/etc/xray/configs';
  const mihomoDir = '/opt/etc/mihomo';

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }
</script>

<div class="file-tree-card">
  <!-- File Search -->
  <div class="file-tree-search-bar">
    <input
      type="text"
      class="input"
      style="width: 100%; padding: 7px 10px; font-size: 12.5px;"
      placeholder={$t('app.search') || 'Поиск файлов...'}
      bind:value={fileSearchQuery}
    />
    {#if fileSearchQuery}
      <button
        onclick={() => (fileSearchQuery = '')}
        class="file-tree-search-clear"
        title="Очистить"
      >
        ×
      </button>
    {/if}
  </div>

  <div class="file-tree-body">
    <!-- Xray Section -->
    <details class="editor-files nav-group" open={activeKernel === 'xray'}>
      <summary class="editor-files-head">
        <span class="group-ttl">Xray</span>
        <span class="group-path-wrap">
          <span class="group-path">{xrayDir}</span>
          <span class="nav-group-arrow">›</span>
        </span>
      </summary>
      <div class="file-list">
        {#each filteredXrayFiles as file}
          <button
            class="file-row"
            class:active={file.path === selectedFile}
            onclick={() => onLoadFile(file.path, true)}
            ondblclick={() => onLoadFile(file.path, false)}
          >
            <span class="fr-name">{file.name}</span>
            <span class="fr-meta">{formatBytes(file.size)}</span>
          </button>
        {:else}
          <span class="sb-empty">—</span>
        {/each}
      </div>
    </details>

    <!-- Mihomo Section -->
    <details class="editor-files nav-group" open={activeKernel === 'mihomo'}>
      <summary class="editor-files-head">
        <span class="group-ttl">Mihomo</span>
        <span class="group-path-wrap">
          <span class="group-path">{mihomoDir}</span>
          <span class="nav-group-arrow">›</span>
        </span>
      </summary>
      <div class="file-list">
        {#each filteredMihomoFiles as file}
          <button
            class="file-row"
            class:active={file.path === selectedFile}
            onclick={() => onLoadFile(file.path, true)}
            ondblclick={() => onLoadFile(file.path, false)}
          >
            <span class="fr-name">{file.name}</span>
            <span class="fr-meta">{formatBytes(file.size)}</span>
          </button>
        {:else}
          <span class="sb-empty">—</span>
        {/each}
      </div>
    </details>
  </div>
</div>

<style>
  .file-tree-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    box-sizing: border-box;
  }
  .file-tree-search-bar {
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
    position: relative;
    background: rgba(0, 0, 0, 0.15);
  }
  .file-tree-search-clear {
    position: absolute;
    right: 18px;
    top: 50%;
    transform: translateY(-50%);
    background: none;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: 16px;
    padding: 0 4px;
  }
  .file-tree-body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }
  .editor-files {
    border-bottom: 1px solid var(--border);
  }
  .editor-files:last-child {
    border-bottom: none;
  }
  .editor-files-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 14px;
    font-size: 11px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
    font-weight: 700;
    cursor: pointer;
    background: rgba(255, 255, 255, 0.02);
    user-select: none;
  }
  .group-path-wrap {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .group-path {
    color: var(--accent);
    font-family: var(--font-family-mono);
    text-transform: none;
    letter-spacing: 0;
    font-weight: 500;
    font-size: 11px;
  }
  .file-list {
    max-height: none;
    overflow-y: visible;
  }
  .sb-empty {
    padding: 10px 14px;
    display: block;
    color: var(--fg-faint);
    font-size: 12px;
  }
</style>
