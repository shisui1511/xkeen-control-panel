<script lang="ts">
  import { t } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import { matchXKeenStoplist } from '../../lib/xkeenStoplist';
  import { configLayerEnabled } from '../../stores';
  import { filesByPath, type FileState } from '../../lib/configLayer';
  import Badge, { type BadgeTone } from '../Badge.svelte';
  import Button from '../Button.svelte';
  import EmptyState from '../EmptyState.svelte';
  import Skeleton from '../Skeleton.svelte';
  import OwnerBadge from './OwnerBadge.svelte';
  import type { ConfigFileInfo, FileListStatus, FileOwner } from './fileOps';

  let {
    xrayFiles = [],
    mihomoFiles = [],
    xkeenFiles,
    status = 'ready',
    selectedFile = '',
    loadingPath = null,
    activeKernel = '',
    onLoadFile,
    onCreateFile,
    onRenameFile,
    onDuplicateFile,
    onDownloadFile,
    onDeleteFile,
    onViewBackups,
    onRetry
  }: {
    xrayFiles: ConfigFileInfo[];
    mihomoFiles: ConfigFileInfo[];
    /**
     * Файлы каталога XKeen: задан только во вкладке «Файлы» раздела (D-04); без него
     * список — прежние две группы Редактора
     */
    xkeenFiles?: ConfigFileInfo[];
    /** Исход загрузки списка: скелетон, ошибка с «Повторить» или группы */
    status?: FileListStatus;
    selectedFile: string;
    /** Путь файла, который сейчас открывается: его строка помечена aria-busy и подсвечена */
    loadingPath?: string | null;
    activeKernel: string;
    onLoadFile: (path: string, isPreviewClick: boolean) => void;
    onCreateFile: () => void;
    onRenameFile: (file: ConfigFileInfo) => void;
    onDuplicateFile: (file: ConfigFileInfo) => void;
    onDownloadFile: (file: ConfigFileInfo) => void;
    onDeleteFile: (file: ConfigFileInfo) => void;
    onViewBackups: (file: ConfigFileInfo) => void;
    onRetry?: () => void;
  } = $props();

  let fileSearchQuery = $state('');

  const scoped = $derived(xkeenFiles !== undefined);
  const query = $derived(fileSearchQuery.toLowerCase());
  const matches = (file: ConfigFileInfo) => file.name.toLowerCase().includes(query);

  let filteredXrayFiles = $derived(xrayFiles.filter(matches));
  let filteredMihomoFiles = $derived(mihomoFiles.filter(matches));
  let filteredXkeenFiles = $derived((xkeenFiles ?? []).filter(matches));

  // Фильтр не нашёл ни одного файла: вместо пустых групп — одно пустое состояние
  const nothingFound = $derived(
    scoped &&
      query !== '' &&
      filteredXrayFiles.length + filteredMihomoFiles.length + filteredXkeenFiles.length === 0
  );

  // Владелец (D-05): поле ответа scope=files, без него — по манифесту слоя (файл манифеста —
  // «Панель», отпущенный — «Пользователь»). Выключенный флаг — без бейджа, как до слоя.
  function ownerOf(file: ConfigFileInfo): FileOwner | null {
    if (!$configLayerEnabled) return null;
    if (file.owner) return file.owner;
    const f = $filesByPath.get(file.path);
    if (!f) return null;
    return f.state === 'released' ? 'user' : 'panel';
  }

  function stateOf(file: ConfigFileInfo): FileState | undefined {
    if (!$configLayerEnabled) return undefined;
    return file.state ?? $filesByPath.get(file.path)?.state;
  }

  const DRIFT_STATES: readonly FileState[] = [
    'drift_modified',
    'drift_renamed',
    'drift_empty',
    'drift_missing'
  ];

  /** Бейдж статуса файла (D-07): у совпадающего файла бейджа статуса нет */
  function statusOf(
    file: ConfigFileInfo
  ): { state: FileState; tone: BadgeTone; label: string } | null {
    const state = stateOf(file);
    if (state === 'drift_missing') {
      return { state, tone: 'warning', label: $t('cfg.files.badge_missing') };
    }
    if (state && DRIFT_STATES.includes(state)) {
      return { state, tone: 'warning', label: $t('cfg.files.badge_drift') };
    }
    if (state === 'pending') {
      return { state, tone: 'neutral', label: $t('cfg.files.badge_pending') };
    }
    return null;
  }

  function driftCount(list: ConfigFileInfo[]): number {
    return list.filter((f) => {
      const state = stateOf(f);
      return state !== undefined && DRIFT_STATES.includes(state);
    }).length;
  }

  const xrayDir = '/opt/etc/xray/configs';
  const mihomoDir = '/opt/etc/mihomo';
  const xkeenDir = '/opt/etc/xkeen';

  /** Части имени для переноса строки после «.», «_», «-», «/» (разделитель остаётся в конце части) */
  function breakableParts(name: string): string[] {
    return name.split(/(?<=[._\-/])/);
  }

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

  <div class="file-tree-body" class:scoped>
    {#if status === 'loading'}
      <div class="ft-skeleton" aria-busy="true">
        {#each [0, 1, 2, 3, 4, 5] as i (i)}
          <Skeleton height="28px" />
        {/each}
      </div>
    {:else if status === 'error'}
      <div class="alert alert-error ft-error" role="alert">
        <span>{$t('cfg.files.list_error')}</span>
        <Button variant="secondary" size="sm" onclick={() => onRetry?.()}
          >{$t('cfg.files.retry')}</Button
        >
      </div>
    {:else if nothingFound}
      <EmptyState
        title={$t('cfg.files.empty_title')}
        description={$t('cfg.files.empty_body')}
        plain
      />
    {:else}
      <!-- Xray Section (EDIT-03) -->
      {@render group(
        scoped ? $t('cfg.files.group_xray') : 'Xray',
        xrayDir,
        filteredXrayFiles,
        activeKernel === 'xray' || filteredXrayFiles.length > 0,
        true
      )}
      <!-- Mihomo Section (EDIT-03) -->
      {@render group(
        scoped ? $t('cfg.files.group_mihomo') : 'Mihomo',
        mihomoDir,
        filteredMihomoFiles,
        activeKernel === 'mihomo' || filteredMihomoFiles.length > 0,
        false
      )}
      <!-- XKeen Section: только во вкладке «Файлы» раздела (D-04) -->
      {#if scoped}
        {@render group(
          $t('cfg.files.group_xkeen'),
          xkeenDir,
          filteredXkeenFiles,
          filteredXkeenFiles.length > 0,
          false
        )}
      {/if}
    {/if}
  </div>
</div>

{#snippet group(
  title: string,
  dir: string,
  list: ConfigFileInfo[],
  open: boolean,
  xrayRoot: boolean
)}
  {@const drift = driftCount(list)}
  <details class="editor-files nav-group" {open}>
    <summary class="editor-files-head">
      <div class="group-ttl-wrap">
        <span class="group-ttl">{title}</span>
        <span class="group-count">({list.length})</span>
        {#if drift > 0}
          <Badge tone="warning">{$t('cfg.files.drift_count', { k: drift })}</Badge>
        {/if}
      </div>
      <span class="group-path-wrap">
        <span class="group-path" title={dir}
          >{#each breakableParts(dir) as part, i (i)}{part}<wbr />{/each}</span
        >
        <span class="nav-group-arrow">›</span>
      </span>
    </summary>
    <div class="file-list">
      {#each list as file (file.path)}
        {@render row(file, xrayRoot)}
      {:else}
        <span class="sb-empty">—</span>
      {/each}
    </div>
  </details>
{/snippet}

{#snippet row(file: ConfigFileInfo, xrayRoot: boolean)}
  {@const fmt = getFormatBadge(file.name)}
  {@const stopWord = xrayRoot ? file.stoplist_word || matchXKeenStoplist(file.name) : null}
  {@const activeConfig = isActiveRunningConfig(file)}
  {@const busy = file.path === loadingPath}
  {@const owner = ownerOf(file)}
  {@const fileStatus = statusOf(file)}
  <button
    class="file-row"
    class:active={loadingPath ? busy : file.path === selectedFile}
    class:busy
    aria-busy={busy ? 'true' : undefined}
    data-path={file.path}
    onclick={() => onLoadFile(file.path, true)}
    ondblclick={() => onLoadFile(file.path, false)}
    oncontextmenu={(e) => handleContextMenu(e, file)}
  >
    <span class="fr-main">
      <span class="fr-left">
        <span class="fmt-badge {fmt.cls}">{fmt.label}</span>
        {#if activeConfig}
          <span class="active-dot" title={$t('editor.active_config')}></span>
        {/if}
        <span class="fr-name file-name" title={file.name}
          >{#each breakableParts(file.name) as part, i (i)}{part}<wbr />{/each}</span
        >
        {#if file.restricted}
          {@const markTitle = $t('cfg.files.restricted_title')}
          <span class="stoplist-mark" title={markTitle} aria-label={markTitle} role="img">
            <Icon name="warning" size={12} />
          </span>
        {:else if stopWord}
          {@const markTitle = $t('stoplist.badge_title', { word: stopWord })}
          <span class="stoplist-mark" title={markTitle} aria-label={markTitle} role="img">
            <Icon name="warning" size={12} />
          </span>
        {/if}
      </span>
      {#if !file.missing}
        <span class="fr-meta">{formatBytes(file.size)}</span>
      {/if}
    </span>
    {#if owner || fileStatus}
      <span class="fr-labels">
        {#if owner}
          <OwnerBadge {owner} />
        {/if}
        {#if fileStatus}
          <Badge
            variant="status"
            tone={fileStatus.tone}
            testid="files-drift-badge"
            data-state={fileStatus.state}>{fileStatus.label}</Badge
          >
        {/if}
      </span>
    {/if}
  </button>
{/snippet}

<!-- Context Menu (EDIT-03). Обработчик вызывается до closeContextMenu(): после закрытия f обнуляется. -->
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
      role="menuitem"
      onclick={() => {
        onRenameFile(f);
        closeContextMenu();
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
      role="menuitem"
      onclick={() => {
        onDuplicateFile(f);
        closeContextMenu();
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
      role="menuitem"
      onclick={() => {
        onDownloadFile(f);
        closeContextMenu();
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
      role="menuitem"
      onclick={() => {
        onViewBackups(f);
        closeContextMenu();
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
      role="menuitem"
      onclick={() => {
        onDeleteFile(f);
        closeContextMenu();
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
    padding: var(--spacing-2) var(--spacing-2-5);
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: var(--spacing-1-5);
    background: var(--bg-group-head-from);
  }

  .search-input-wrapper {
    position: relative;
    flex: 1;
  }

  .file-search-input {
    width: 100%;
    padding: 5px var(--spacing-6) 5px var(--spacing-2);
    font-size: var(--font-size-xs);
    height: 28px;
    background: var(--bg-elevated);
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
    font-size: var(--font-size-base);
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
    color: var(--btn-primary-text);
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    flex-shrink: 0;
    transition: opacity var(--transition-fast);
  }

  .btn-new-file:hover {
    opacity: 0.9;
  }

  .file-tree-body {
    flex: 1;
    overflow-y: auto;
    scrollbar-width: thin;
    padding: var(--spacing-1-5) 0;
  }

  .editor-files {
    border-bottom: 1px solid var(--border);
  }

  .editor-files-head {
    padding: var(--spacing-2) var(--spacing-3);
    display: flex;
    align-items: center;
    justify-content: space-between;
    cursor: pointer;
    user-select: none;
    font-size: var(--font-size-xs);
    /* .nav-group brings the sidebar's uppercase tracking; directory paths are
       case-sensitive and must render as-is. */
    text-transform: none;
    letter-spacing: normal;
    background: var(--surface-tint);
    transition: background var(--transition-fast);
  }

  .editor-files-head:hover {
    background: var(--hover);
  }

  .group-ttl-wrap {
    display: flex;
    align-items: center;
    gap: var(--spacing-1);
  }

  .group-ttl {
    font-weight: 700;
    color: var(--fg-primary);
  }

  .group-count {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .group-path-wrap {
    display: flex;
    align-items: center;
    gap: var(--spacing-1);
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .group-path {
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-family-mono);
  }

  .file-list {
    display: flex;
    flex-direction: column;
    padding: var(--spacing-1) var(--spacing-1-5);
    gap: var(--spacing-0-5);
  }

  .file-row {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: var(--spacing-1);
    padding: 5px var(--spacing-2);
    border-radius: var(--radius-sm);
    border: none;
    background: transparent;
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    cursor: pointer;
    text-align: left;
    transition:
      color var(--transition-fast),
      background var(--transition-fast);
  }

  .file-row:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .file-row.active {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 600;
  }

  /* Файл открывается: подсветка и aria-busy ставятся сразу, пульс — после 150 мс, поэтому
     быстрое чтение не мигает; раскладка строки не меняется (G-142-4-WR02) */
  .file-row.busy {
    cursor: progress;
    animation: fr-busy-pulse 0.9s ease-in-out 0.15s infinite alternate;
  }

  @keyframes fr-busy-pulse {
    from {
      opacity: 1;
    }
    to {
      opacity: 0.55;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .file-row.busy {
      animation: none;
    }
  }

  .fr-main {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-width: 0;
  }

  .fr-left {
    display: flex;
    align-items: center;
    gap: var(--spacing-1-5);
    min-width: 0;
    flex: 1;
  }

  .fr-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fr-meta {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    margin-left: var(--spacing-1-5);
    flex-shrink: 0;
  }

  .fmt-badge {
    font-size: var(--font-size-xs);
    font-weight: 800;
    padding: 1px var(--spacing-1);
    border-radius: var(--radius-xs);
    flex-shrink: 0;
    font-family: var(--font-family-mono);
  }

  .fmt-yaml {
    background: color-mix(in srgb, var(--seq-1) 15%, transparent);
    color: var(--seq-1);
  }

  .fmt-json {
    background: color-mix(in srgb, var(--seq-2) 15%, transparent);
    color: var(--seq-2);
  }

  .fmt-conf {
    background: color-mix(in srgb, var(--seq-3) 15%, transparent);
    color: var(--seq-3);
  }

  .fmt-other {
    background: color-mix(in srgb, var(--seq-4) 15%, transparent);
    color: var(--seq-4);
  }

  .active-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--success);
    box-shadow: 0 0 5px color-mix(in srgb, var(--success) 80%, transparent);
    flex-shrink: 0;
  }

  /* Второй ряд строки: владелец и статус файла; бейджи не сжимают имя */
  .fr-labels {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1);
    min-width: 0;
  }

  /* Подложка бейджа поверх выбранной строки (--accent-soft) складывалась бы с собственной:
     контраст ниже 4.5:1. Плотная подложка карточки держит его в норме в обеих темах. */
  .file-row.active .fr-labels :global(.badge) {
    background: var(--bg-card);
  }

  .ft-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-3);
  }

  .ft-error {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-2);
    margin: var(--spacing-2) var(--spacing-3);
    overflow-wrap: anywhere;
  }

  /* Вкладка «Файлы»: длинное имя переносится по «.», «_», «-», «/», а не обрезается */
  .file-tree-body.scoped .fr-name {
    white-space: normal;
    overflow-wrap: anywhere;
    min-width: 0;
  }

  .stoplist-mark {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    color: var(--warning);
  }

  .sb-empty {
    padding: var(--spacing-1-5) var(--spacing-3);
    font-size: var(--font-size-xs);
    color: var(--fg-faint);
  }

  /* Context Menu */
  .file-context-menu {
    position: fixed;
    z-index: var(--z-overlay);
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-md);
    padding: var(--spacing-1);
    min-width: 170px;
    display: flex;
    flex-direction: column;
    gap: 1px;
    animation: menuPop var(--duration-fast) var(--ease-out);
  }

  .ctx-header {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    padding: var(--spacing-1) var(--spacing-2);
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .ctx-item {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    padding: var(--spacing-1-5) var(--spacing-2);
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
    background: transparent;
    border: none;
    border-radius: var(--radius-xs);
    cursor: pointer;
    text-align: left;
    transition: background var(--transition-fast);
  }

  .ctx-item:hover {
    background: var(--hover);
  }

  .ctx-danger {
    color: var(--danger);
  }

  .ctx-danger:hover {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
  }

  .ctx-divider {
    height: 1px;
    background: var(--border);
    margin: 3px 0;
  }

  @media (max-width: 768px) {
    .file-row {
      min-height: 40px;
      padding: var(--spacing-2-5) var(--spacing-3);
      gap: var(--spacing-2);
      font-size: var(--font-size-sm);
    }

    .fr-name {
      white-space: normal;
      overflow-wrap: anywhere;
      display: -webkit-box;
      -webkit-box-orient: vertical;
      -webkit-line-clamp: 2;
      line-clamp: 2;
      overflow: hidden;
      line-height: 1.3;
      min-width: 0;
    }

    .fr-meta {
      align-self: center;
    }

    .group-path-wrap {
      min-width: 0;
      flex: 1;
      justify-content: flex-end;
    }

    .group-path {
      max-width: none;
      white-space: normal;
      overflow-wrap: anywhere;
      text-align: right;
    }

    .file-search-input {
      height: 36px;
      font-size: var(--font-size-sm);
    }

    .btn-new-file {
      width: 36px;
      height: 36px;
    }

    .file-tree-search-clear {
      width: 28px;
      height: 28px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
    }
  }
</style>
