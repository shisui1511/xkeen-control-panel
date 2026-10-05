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
    onRestoreBackup,
    onClose
  }: {
    backups: string[];
    selectedBackup: string;
    diffGroups: DiffGroup[];
    backupLoading: boolean;
    onSelectBackup: (backup: string) => void;
    onRestoreBackup: (backup: string) => void;
    onClose?: () => void;
  } = $props();

  // Мобильный лист — два экрана: список копий → сравнение на весь лист
  let mobileScreen = $state<'list' | 'diff'>('list');

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
  <div class="drawer-topbar">
    {#if mobileScreen === 'diff'}
      <button type="button" class="drawer-back-btn" onclick={() => (mobileScreen = 'list')}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <polyline points="15 18 9 12 15 6"></polyline>
        </svg>
        {$t('app.back')}
      </button>
    {/if}
    <span class="drawer-title">{$t('editor.backups')}</span>
    <button type="button" class="drawer-close-btn" onclick={() => onClose?.()}>
      {$t('app.close')}
    </button>
  </div>
  <div class="drawer-layout" class:show-diff={mobileScreen === 'diff'}>
    <!-- Список бэкапов слева -->
    <div class="drawer-sidebar">
      {#each backups as backup (backup)}
        <div class="backup-item" class:active={selectedBackup === backup}>
          <button
            type="button"
            class="backup-select-btn"
            onclick={() => {
              onSelectBackup(backup);
              mobileScreen = 'diff';
            }}
          >
            <span class="backup-time">{formatBackupDate(backup)}</span>
          </button>
          <button
            type="button"
            class="btn btn-sm btn-secondary restore-inline-btn"
            onclick={() => onRestoreBackup(backup)}
          >
            {$t('settings.restore')}
          </button>
        </div>
      {/each}
    </div>

    <!-- Зона diff-viewer справа -->
    <div class="drawer-main">
      {#if selectedBackup}
        <div class="diff-viewer-container">
          <div class="diff-header">
            <span
              >{$t('editor.compare_with_backup', { date: formatBackupDate(selectedBackup) })}</span
            >
            <button
              type="button"
              class="btn btn-primary btn-sm diff-restore-btn"
              onclick={() => onRestoreBackup(selectedBackup)}
            >
              {$t('settings.restore')}
            </button>
          </div>
          <div class="diff-body">
            {#if backupLoading}
              <div style="display:grid;place-items:center;height:100px;">
                <div class="spinner" style="--spinner-size: 24px;"></div>
              </div>
            {:else}
              {#each diffGroups as group, gi (gi)}
                {#if group.type === 'added'}
                  {#each group.lines as line, li (li)}
                    <div class="diff-line diff-line-added">+ {line}</div>
                  {/each}
                {:else if group.type === 'removed'}
                  {#each group.lines as line, li (li)}
                    <div class="diff-line diff-line-removed">- {line}</div>
                  {/each}
                {:else if group.type === 'collapsed'}
                  <div class="diff-line diff-line-collapsed">{group.lines[0]}</div>
                {:else}
                  {#each group.lines as line, li (li)}
                    <div class="diff-line diff-line-unchanged">{line}</div>
                  {/each}
                {/if}
              {/each}
            {/if}
          </div>
        </div>
      {:else}
        <div class="drawer-empty-state">{$t('editor.select_backup_hint')}</div>
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

  .drawer-topbar,
  .diff-restore-btn {
    display: none;
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
    padding: 4px 6px;
    background: transparent;
    color: var(--fg-dim);
    border-radius: var(--radius);
    transition: all 0.15s ease;
    width: 100%;
  }

  .backup-select-btn {
    display: flex;
    align-items: center;
    flex: 1;
    background: transparent;
    border: 0;
    color: inherit;
    font: inherit;
    font-size: 12px;
    text-align: left;
    cursor: pointer;
    padding: 4px 4px;
    border-radius: var(--radius);
  }

  .backup-select-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .backup-item:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .backup-item.active {
    background: var(--accent-dim);
    color: var(--fg-primary);
  }

  .restore-inline-btn {
    padding: 2px 6px;
    font-size: 12px;
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
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 14px;
    background: var(--surface-tint);
    border-bottom: 1px solid var(--border);
    font-size: 12px;
    color: var(--fg-dim);
  }

  .diff-body {
    flex: 1;
    overflow-y: auto;
    padding: 10px 14px;
    background: var(--bg-page);
    font-family: var(--font-family-mono);
    font-size: 12px;
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
    background: color-mix(in srgb, var(--success) 12%, transparent);
    color: var(--success);
    border-left: 3px solid var(--success);
    padding-left: 6px;
  }

  .diff-line-removed {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    color: var(--danger);
    border-left: 3px solid var(--danger);
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

  /* Узкий экран: лист закрывает всю карточку редактора; два экрана — список копий, затем сравнение */
  @media (max-width: 768px) {
    .editor-bottom-drawer {
      position: absolute;
      inset: 0;
      height: auto;
      z-index: 25;
      display: flex;
      flex-direction: column;
      border-top: none;
    }

    .drawer-topbar {
      display: flex;
      align-items: center;
      gap: 8px;
      min-height: 52px;
      padding: 4px 8px;
      border-bottom: 1px solid var(--border);
      flex-shrink: 0;
    }

    .drawer-title {
      flex: 1;
      font-weight: 600;
      color: var(--fg-primary);
    }

    .drawer-back-btn,
    .drawer-close-btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 4px;
      flex-shrink: 0;
      min-height: 44px;
      min-width: 44px;
      padding: 6px 14px;
      font-size: 13px;
      background: var(--surface-tint);
      border: 1px solid var(--border);
      border-radius: var(--radius);
      color: var(--fg-secondary);
      cursor: pointer;
    }

    .drawer-layout {
      flex: 1;
      min-height: 0;
      flex-direction: column;
    }

    .drawer-layout:not(.show-diff) .drawer-main {
      display: none;
    }

    .drawer-layout.show-diff .drawer-sidebar {
      display: none;
    }

    .drawer-sidebar {
      flex: 1;
      width: 100%;
      max-height: none;
      border: none;
      overflow-y: auto;
      padding: 6px;
    }

    .backup-item {
      min-height: 48px;
    }

    .backup-select-btn {
      min-height: 44px;
      font-size: 14px;
    }

    .drawer-main {
      flex: 1;
      min-height: 0;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    /* Восстановление только с экрана сравнения: случайный тап по списку ничего не меняет */
    .restore-inline-btn {
      display: none;
    }

    .drawer-empty-state {
      display: none;
    }

    .diff-header {
      padding: 8px 12px;
    }

    .diff-restore-btn {
      display: inline-flex;
      min-height: 44px;
      flex-shrink: 0;
    }

    .diff-body {
      overflow-x: auto;
      white-space: pre;
    }
  }
</style>
