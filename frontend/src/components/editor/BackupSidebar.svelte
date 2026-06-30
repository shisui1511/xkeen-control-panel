<script lang="ts">
  import { slide } from 'svelte/transition';
  import { t } from '../../i18n';

  interface DiffGroup {
    type: 'added' | 'removed' | 'collapsed' | 'unchanged';
    lines: string[];
  }

  let {
    backups = [],
    selectedBackup = '',
    diffGroups = [],
    backupLoading = false,
    onSelectBackup,
    onRestoreBackup
  }: {
    backups: string[];
    selectedBackup: string;
    diffGroups: DiffGroup[];
    backupLoading: boolean;
    onSelectBackup: (backup: string) => void;
    onRestoreBackup: (backup: string) => void;
  } = $props();

  function formatBackupDate(backup: string): string {
    const parts = backup.split('.backup-');
    if (parts.length < 2) return backup;
    const tsStr = parts[1];
    const yyyymmdd = tsStr.slice(0, 8); // YYYYMMDD
    const hhmmss = tsStr.slice(9, 15); // HHMMSS
    if (yyyymmdd.length === 8 && hhmmss.length === 6) {
      const y = yyyymmdd.slice(0, 4);
      const m = yyyymmdd.slice(4, 6);
      const d = yyyymmdd.slice(6, 8);
      const hh = hhmmss.slice(0, 2);
      const mm = hhmmss.slice(2, 4);
      const ss = hhmmss.slice(4, 6);
      return `${d}.${m}.${y} ${hh}:${mm}:${ss}`;
    }
    return tsStr;
  }
</script>

<div class="editor-bottom-drawer" transition:slide={{ duration: 200 }}>
  <div class="drawer-layout">
    <!-- Список бэкапов слева -->
    <div class="drawer-sidebar">
      {#each backups as backup}
        <div
          class="backup-item"
          class:active={selectedBackup === backup}
          role="button"
          tabindex="0"
          onclick={() => onSelectBackup(backup)}
          onkeydown={(e) =>
            (e.key === 'Enter' || e.key === ' ') &&
            (e.preventDefault(), onSelectBackup(backup))}
        >
          <span class="backup-time">{formatBackupDate(backup)}</span>
          <button
            class="btn btn-sm btn-secondary restore-inline-btn"
            onclick={(e) => { e.stopPropagation(); onRestoreBackup(backup); }}
          >
            Восстановить
          </button>
        </div>
      {/each}
    </div>

    <!-- Зона diff-viewer справа -->
    <div class="drawer-main">
      {#if selectedBackup}
        <div class="diff-viewer-container">
          <div class="diff-header">
            <span>Сравнение с бэкапом от {formatBackupDate(selectedBackup)}</span>
          </div>
          <div class="diff-body">
            {#if backupLoading}
              <div style="display:grid;place-items:center;height:100px;">
                <div class="spinner"></div>
              </div>
            {:else}
              {#each diffGroups as group}
                {#if group.type === 'added'}
                  {#each group.lines as line}
                    <div class="diff-line diff-line-added">+ {line}</div>
                  {/each}
                {:else if group.type === 'removed'}
                  {#each group.lines as line}
                    <div class="diff-line diff-line-removed">- {line}</div>
                  {/each}
                {:else if group.type === 'collapsed'}
                  <div class="diff-line diff-line-collapsed">{group.lines[0]}</div>
                {:else}
                  {#each group.lines as line}
                    <div class="diff-line diff-line-unchanged">{line}</div>
                  {/each}
                {/if}
              {/each}
            {/if}
          </div>
        </div>
      {:else}
        <div class="drawer-empty-state">
          Выберите резервную копию слева для сравнения изменений
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .editor-bottom-drawer {
    height: 250px;
    background: var(--bg-card);
    border-top: 1px solid var(--border);
    overflow: hidden;
  }

  .drawer-layout {
    display: flex;
    height: 100%;
  }

  .drawer-sidebar {
    width: 240px;
    border-right: 1px solid var(--border);
    overflow-y: auto;
    padding: 6px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    scrollbar-width: thin;
  }

  .drawer-sidebar::-webkit-scrollbar {
    width: 4px;
    height: 4px;
  }
  .drawer-sidebar::-webkit-scrollbar-track {
    background: transparent;
  }
  .drawer-sidebar::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius-sm);
  }

  .backup-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 10px;
    background: transparent;
    color: var(--fg-dim);
    border: 0;
    border-radius: var(--radius);
    cursor: pointer;
    font-size: 12px;
    text-align: left;
    transition: all 0.15s ease;
  }

  .backup-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .backup-item:hover {
    background: rgba(255, 255, 255, 0.02);
    color: var(--fg-primary);
  }

  .backup-item.active {
    background: var(--accent-dim);
    color: var(--fg-primary);
  }

  .restore-inline-btn {
    padding: 2px 6px;
    font-size: 10px;
    opacity: 0;
    transition: opacity 0.15s ease;
  }

  .backup-item:hover .restore-inline-btn,
  .backup-item.active .restore-inline-btn {
    opacity: 1;
  }

  .drawer-main {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .diff-viewer-container {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .diff-header {
    padding: 8px 14px;
    background: rgba(255, 255, 255, 0.01);
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    color: var(--fg-dim);
  }

  .diff-body {
    flex: 1;
    overflow-y: auto;
    padding: 10px 14px;
    background: var(--bg-page);
    font-family: var(--font-family-mono);
    font-size: 11px;
    line-height: 1.5;
    scrollbar-width: thin;
  }

  .diff-body::-webkit-scrollbar {
    width: 4px;
    height: 4px;
  }
  .diff-body::-webkit-scrollbar-track {
    background: transparent;
  }
  .diff-body::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius-sm);
  }

  .diff-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .diff-line-added {
    background: rgba(46, 160, 67, 0.12);
    color: #3fb950;
    border-left: 3px solid #2ea043;
    padding-left: 6px;
  }

  .diff-line-removed {
    background: rgba(248, 81, 73, 0.12);
    color: #f85149;
    border-left: 3px solid #f85149;
    padding-left: 6px;
  }

  .diff-line-collapsed {
    background: rgba(255, 255, 255, 0.02);
    color: var(--fg-faint);
    text-align: center;
    font-style: italic;
    padding: 4px 0;
    border-top: 1px dashed var(--border);
    border-bottom: 1px dashed var(--border);
    margin: 4px 0;
  }

  .diff-line-unchanged {
    color: var(--fg-dim);
    padding-left: 9px;
  }

  .drawer-empty-state {
    display: grid;
    place-items: center;
    height: 100%;
    color: var(--fg-faint);
    font-size: 12px;
  }

  .spinner {
    width: 24px;
    height: 24px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
