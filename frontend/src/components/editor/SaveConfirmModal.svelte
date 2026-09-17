<script lang="ts">
  import Modal from '../Modal.svelte';
  import { t } from '../../i18n';
  import { getDiffGroups } from './diff';

  interface Props {
    isOpen: boolean;
    originalContent: string;
    currentContent: string;
    saving?: boolean;
    onConfirm: () => void;
    onClose: () => void;
  }

  let {
    isOpen,
    originalContent,
    currentContent,
    saving = false,
    onConfirm,
    onClose
  }: Props = $props();

  let diffGroups = $derived(getDiffGroups(originalContent, currentContent));
</script>

<Modal {isOpen} title={$t('editor.confirm_save_title')} maxWidth="700px" onclose={onClose}>
  <!-- Diff Preview -->
  <div class="diff-preview" style="margin-top: 12px;">
    <div class="diff-preview-title">
      {$t('editor.diff_preview')}
    </div>
    <div class="diff-preview-body" style="max-height: 40vh; overflow-y: auto;">
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
    </div>
  </div>

  <div class="confirm-modal-actions" style="margin-top: 16px;">
    <button onclick={onClose} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={onConfirm} class="btn btn-primary" disabled={saving}>
      {saving ? $t('app.loading') : $t('app.save')}
    </button>
  </div>
</Modal>

<style>
  .diff-preview {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .diff-preview-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-dim);
    margin-bottom: 6px;
    flex-shrink: 0;
  }

  .diff-preview-body {
    flex: 1;
    overflow-y: auto;
    background: var(--cm-bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 10px;
    font-family: var(--font-family-mono);
    font-size: 12px;
    line-height: 1.5;
  }

  .diff-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .diff-line-added {
    color: var(--success);
    background: color-mix(in srgb, var(--success) 6%, transparent);
  }

  .diff-line-removed {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 6%, transparent);
  }

  .diff-line-collapsed {
    color: var(--fg-faint);
    font-style: italic;
  }

  .diff-line-unchanged {
    color: var(--fg-secondary);
  }
</style>
