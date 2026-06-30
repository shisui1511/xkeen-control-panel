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
