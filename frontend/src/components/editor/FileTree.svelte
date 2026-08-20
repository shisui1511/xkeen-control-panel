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
    onLoadFile,
    onCreateFile,
    onRenameFile,
    onDuplicateFile,
    onDownloadFile,
    onDeleteFile,
    onViewBackups
  }: {
    xrayFiles: ConfigFileInfo[];
    mihomoFiles: ConfigFileInfo[];
    selectedFile: string;
    activeKernel: string;
    onLoadFile: (path: string, isPreviewClick: boolean) => void;
    onCreateFile: () => void;
    onRenameFile: (file: ConfigFileInfo) => void;
    onDuplicateFile: (file: ConfigFileInfo) => void;
    onDownloadFile: (file: ConfigFileInfo) => void;
    onDeleteFile: (file: ConfigFileInfo) => void;
    onViewBackups: (file: ConfigFileInfo) => void;
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

  function getFormatBadge(name: string): { label: string; cls: string } {
    const lower = name.toLowerCase();
    if (lower.endsWith('.yaml') || lower.endsWith('.yml')) {
      return { label: 'YML', cls: 'fmt-yaml' };
    }
    if (lower.endsWith('.json')) {
      return { label: 'JSON', cls: 'fmt-json' };
    }
    if (lower.endsWith('.conf')) {
      return { label: 'CONF', cls: 'fmt-conf' };
    }
    return { label: 'FILE', cls: 'fmt-other' };
  }

  function isActiveRunningConfig(file: ConfigFileInfo): boolean {
    if (activeKernel === 'mihomo' && file.name === 'config.yaml') return true;
    if (activeKernel === 'xray' && file.path.includes('/opt/etc/xray')) return true;
    return false;
  }

  // Context Menu State
  interface ContextMenuState {
    visible: boolean;
    x: number;
    y: number;
    file: ConfigFileInfo | null;
  }
  let ctxMenu = $state<ContextMenuState>({ visible: false, x: 0, y: 0, file: null });

  function handleContextMenu(e: MouseEvent, file: ConfigFileInfo) {
    e.preventDefault();
    ctxMenu = {
      visible: true,
      x: Math.min(e.clientX, window.innerWidth - 180),
      y: Math.min(e.clientY, window.innerHeight - 200),
      file
    };
  }

  function closeContextMenu() {
    ctxMenu = { visible: false, x: 0, y: 0, file: null };
  }
</script>

<svelte:window
  onclick={closeContextMenu}
  onkeydown={(e) => e.key === 'Escape' && closeContextMenu()}
/>

<div class="file-tree-card">
  <!-- File Search & Actions Bar -->
  <div class="file-tree-search-bar">
    <div class="search-input-wrapper">
      <input
        type="text"
        class="input file-search-input"
        placeholder={$t('editor.search_files')}
        bind:value={fileSearchQuery}
      />
      {#if fileSearchQuery}
        <button
          onclick={() => (fileSearchQuery = '')}
          class="file-tree-search-clear"
          title={$t('app.clear')}
        >
          ×
        </button>
      {/if}
    </div>
    <button
      class="btn-new-file"
      onclick={onCreateFile}
      title={$t('editor.new_file')}
      aria-label={$t('editor.new_file')}
    >
      <svg
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2.5"
      >
        <line x1="12" y1="5" x2="12" y2="19" />
        <line x1="5" y1="12" x2="19" y2="12" />
      </svg>
    </button>
  </div>

  <div class="file-tree-body">
    <!-- Xray Section (EDIT-03) -->
    <details
      class="editor-files nav-group"
      open={activeKernel === 'xray' || filteredXrayFiles.length > 0}
    >
      <summary class="editor-files-head">
        <div class="group-ttl-wrap">
          <span class="group-ttl">Xray</span>
          <span class="group-count">({filteredXrayFiles.length})</span>
        </div>
        <span class="group-path-wrap">
          <span class="group-path">{xrayDir}</span>
          <span class="nav-group-arrow">›</span>
        </span>
      </summary>
      <div class="file-list">
        {#each filteredXrayFiles as file}
          {@const fmt = getFormatBadge(file.name)}
          {@const activeConfig = isActiveRunningConfig(file)}
          <button
            class="file-row"
            class:active={file.path === selectedFile}
            onclick={() => onLoadFile(file.path, true)}
            ondblclick={() => onLoadFile(file.path, false)}
            oncontextmenu={(e) => handleContextMenu(e, file)}
          >
            <div class="fr-left">
              <span class="fmt-badge {fmt.cls}">{fmt.label}</span>
              {#if activeConfig}
                <span class="active-dot" title={$t('editor.active_config')}></span>
              {/if}
              <span class="fr-name file-name" title={file.name}>{file.name}</span>
            </div>
            <span class="fr-meta">{formatBytes(file.size)}</span>
          </button>
        {:else}
          <span class="sb-empty">—</span>
        {/each}
      </div>
    </details>

    <!-- Mihomo Section (EDIT-03) -->
    <details
      class="editor-files nav-group"
      open={activeKernel === 'mihomo' || filteredMihomoFiles.length > 0}
    >
      <summary class="editor-files-head">
        <div class="group-ttl-wrap">
          <span class="group-ttl">Mihomo</span>
          <span class="group-count">({filteredMihomoFiles.length})</span>
        </div>
        <span class="group-path-wrap">
          <span class="group-path">{mihomoDir}</span>
          <span class="nav-group-arrow">›</span>
        </span>
      </summary>
      <div class="file-list">
        {#each filteredMihomoFiles as file}
          {@const fmt = getFormatBadge(file.name)}
          {@const activeConfig = isActiveRunningConfig(file)}
          <button
            class="file-row"
            class:active={file.path === selectedFile}
            onclick={() => onLoadFile(file.path, true)}
            ondblclick={() => onLoadFile(file.path, false)}
            oncontextmenu={(e) => handleContextMenu(e, file)}
          >
            <div class="fr-left">
              <span class="fmt-badge {fmt.cls}">{fmt.label}</span>
              {#if activeConfig}
                <span class="active-dot" title={$t('editor.active_config')}></span>
              {/if}
              <span class="fr-name file-name" title={file.name}>{file.name}</span>
            </div>
            <span class="fr-meta">{formatBytes(file.size)}</span>
          </button>
        {:else}
          <span class="sb-empty">—</span>
        {/each}
      </div>
    </details>
  </div>
</div>

<!-- Context Menu (EDIT-03) -->
{#if ctxMenu.visible && ctxMenu.file}
  {@const f = ctxMenu.file}
  <div
    class="file-context-menu"
    role="menu"
    tabindex="-1"
    style="top: {ctxMenu.y}px; left: {ctxMenu.x}px;"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
  >
    <div class="ctx-header monospace">{f.name}</div>
    <button
      class="ctx-item"
      onclick={() => {
        closeContextMenu();
        onRenameFile(f);
      }}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        ><path d="M12 20h9" /><path
          d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"
        /></svg
      >
      <span>{$t('editor.rename_file')}</span>
    </button>
    <button
      class="ctx-item"
      onclick={() => {
        closeContextMenu();
        onDuplicateFile(f);
      }}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        ><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path
          d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
        /></svg
      >
      <span>{$t('editor.duplicate_file')}</span>
    </button>
    <button
      class="ctx-item"
      onclick={() => {
        closeContextMenu();
        onDownloadFile(f);
      }}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        ><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline
          points="7 10 12 15 17 10"
        /><line x1="12" y1="15" x2="12" y2="3" /></svg
      >
      <span>{$t('editor.download_file')}</span>
    </button>
    <button
      class="ctx-item"
      onclick={() => {
        closeContextMenu();
        onViewBackups(f);
      }}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        ><circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" /></svg
      >
      <span>{$t('editor.compare_backup')}</span>
    </button>
    <div class="ctx-divider"></div>
    <button
      class="ctx-item ctx-danger"
      onclick={() => {
        closeContextMenu();
        onDeleteFile(f);
      }}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        ><polyline points="3 6 5 6 21 6" /><path
          d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"
        /></svg
      >
      <span>{$t('editor.delete_file')}</span>
    </button>
  </div>
{/if}

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
    padding: 8px 10px;
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: 6px;
    background: rgba(0, 0, 0, 0.15);
  }

  .search-input-wrapper {
    position: relative;
    flex: 1;
  }

  .file-search-input {
    width: 100%;
    padding: 5px 24px 5px 8px;
    font-size: 12px;
    height: 28px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
  }

  .file-tree-search-clear {
    position: absolute;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    background: none;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: 14px;
    padding: 0;
    line-height: 1;
  }

  .btn-new-file {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    background: var(--accent);
    color: #03182a;
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    flex-shrink: 0;
    transition: opacity 0.15s ease;
  }

  .btn-new-file:hover {
    opacity: 0.9;
  }

  .file-tree-body {
    flex: 1;
    overflow-y: auto;
    padding: 6px 0;
  }

  .editor-files {
    border-bottom: 1px solid var(--border-light, rgba(255, 255, 255, 0.04));
  }

  .editor-files-head {
    padding: 8px 12px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    cursor: pointer;
    user-select: none;
    font-size: 12px;
    background: rgba(255, 255, 255, 0.02);
  }

  .editor-files-head:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  .group-ttl-wrap {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .group-ttl {
    font-weight: 700;
    color: var(--fg-primary);
  }

  .group-count {
    font-size: 11px;
    color: var(--fg-dim);
  }

  .group-path-wrap {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 10px;
    color: var(--fg-faint);
  }

  .group-path {
    max-width: 90px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-family-mono);
  }

  .file-list {
    display: flex;
    flex-direction: column;
    padding: 4px 6px;
    gap: 2px;
  }

  .file-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 5px 8px;
    border-radius: var(--radius-sm);
    border: none;
    background: transparent;
    color: var(--fg-secondary);
    font-size: 12px;
    cursor: pointer;
    text-align: left;
    transition: all 0.12s ease;
  }

  .file-row:hover {
    background: rgba(255, 255, 255, 0.04);
    color: var(--fg-primary);
  }

  .file-row.active {
    background: rgba(41, 194, 240, 0.12);
    color: var(--accent);
    font-weight: 600;
  }

  .fr-left {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex: 1;
  }

  .fr-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fr-meta {
    font-size: 10px;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    margin-left: 6px;
    flex-shrink: 0;
  }

  .fmt-badge {
    font-size: 8.5px;
    font-weight: 800;
    padding: 1px 4px;
    border-radius: 3px;
    flex-shrink: 0;
    font-family: var(--font-family-mono);
  }

  .fmt-yaml {
    background: rgba(167, 139, 250, 0.2);
    color: #c4b5fd;
  }

  .fmt-json {
    background: rgba(245, 166, 35, 0.2);
    color: #fcd34d;
  }

  .fmt-conf {
    background: rgba(70, 209, 138, 0.2);
    color: #86efac;
  }

  .fmt-other {
    background: rgba(255, 255, 255, 0.1);
    color: #94a3b8;
  }

  .active-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #46d18a;
    box-shadow: 0 0 5px rgba(70, 209, 138, 0.8);
    flex-shrink: 0;
  }

  .sb-empty {
    padding: 6px 12px;
    font-size: 11px;
    color: var(--fg-faint);
  }

  /* Context Menu */
  .file-context-menu {
    position: fixed;
    z-index: 1000;
    background: #0d2338;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.6);
    padding: 4px;
    min-width: 170px;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .ctx-header {
    font-size: 10px;
    color: var(--fg-dim);
    padding: 4px 8px;
    border-bottom: 1px solid var(--border-light, rgba(255, 255, 255, 0.05));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .ctx-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    font-size: 12px;
    color: var(--fg-primary);
    background: transparent;
    border: none;
    border-radius: 3px;
    cursor: pointer;
    text-align: left;
    transition: background 0.1s ease;
  }

  .ctx-item:hover {
    background: rgba(255, 255, 255, 0.06);
  }

  .ctx-danger {
    color: var(--danger, #f4707f);
  }

  .ctx-danger:hover {
    background: rgba(244, 112, 127, 0.15);
  }

  .ctx-divider {
    height: 1px;
    background: var(--border-light, rgba(255, 255, 255, 0.05));
    margin: 3px 0;
  }
</style>
