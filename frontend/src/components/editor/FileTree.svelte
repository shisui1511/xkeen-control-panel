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

<div>
  <!-- File Search -->
  <div style="margin-bottom: 12px; position: relative;">
    <input
      type="text"
      class="input"
      style="width: 100%; padding: 8px 12px; font-size: 13px;"
      placeholder={$t('app.search') || 'Поиск файлов...'}
      bind:value={fileSearchQuery}
    />
    {#if fileSearchQuery}
      <button
        onclick={() => (fileSearchQuery = '')}
        style="position: absolute; right: 10px; top: 50%; transform: translateY(-50%); background: none; border: none; color: var(--fg-dim); cursor: pointer; font-size: 16px; padding: 0 4px;"
        title="Очистить"
      >
        ×
      </button>
    {/if}
  </div>

  <!-- Xray Section -->
  <details
    class="editor-files nav-group"
    style="margin-bottom:12px;"
    open={activeKernel === 'xray'}
  >
    <summary
      class="editor-files-head"
      style="display:flex;align-items:center;justify-content:space-between;cursor:pointer;"
    >
      <span class="group-ttl">Xray</span>
      <span style="display:flex;align-items:center;gap:6px;">
        <span
          style="color:var(--accent);font-family:var(--font-family-mono);text-transform:none;letter-spacing:0;font-weight:500;font-size:11px;"
          >{xrayDir}</span
        >
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
        <span
          class="sb-empty"
          style="padding:10px 14px;display:block;color:var(--fg-faint);font-size:12px;"
          >—</span
        >
      {/each}
    </div>
  </details>

  <!-- Mihomo Section -->
  <details class="editor-files nav-group" open={activeKernel === 'mihomo'}>
    <summary
      class="editor-files-head"
      style="display:flex;align-items:center;justify-content:space-between;cursor:pointer;"
    >
      <span class="group-ttl">Mihomo</span>
      <span style="display:flex;align-items:center;gap:6px;">
        <span
          style="color:var(--accent);font-family:var(--font-family-mono);text-transform:none;letter-spacing:0;font-weight:500;font-size:11px;"
          >{mihomoDir}</span
        >
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
        <span
          class="sb-empty"
          style="padding:10px 14px;display:block;color:var(--fg-faint);font-size:12px;"
          >—</span
        >
      {/each}
    </div>
  </details>
</div>

<style>
  .editor-files {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .editor-files-head {
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--fg-dim);
    font-weight: 700;
  }
</style>
