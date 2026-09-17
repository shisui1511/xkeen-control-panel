<script lang="ts">
  import { t } from '../../i18n';
  import StatusBadge from '../StatusBadge.svelte';
  import LiveIndicator from '../LiveIndicator.svelte';

  type SaveBadgeVariant = 'running' | 'warning' | 'stopped';

  interface Props {
    saveStatusState: {
      kind: 'live' | 'badge';
      label: string;
      variant?: SaveBadgeVariant;
    };
    selectedFile?: string;
    loading?: boolean;
    saving?: boolean;
    applyLoading?: boolean;
    onReloadFile: () => void;
    onSaveFile: () => void;
    onSaveAndApply: () => void;
  }

  let {
    saveStatusState,
    selectedFile = '',
    loading = false,
    saving = false,
    applyLoading = false,
    onReloadFile,
    onSaveFile,
    onSaveAndApply
  }: Props = $props();
</script>

<div class="eph-right">
  {#if saveStatusState.kind === 'live'}
    <LiveIndicator live={true} label={saveStatusState.label} />
  {:else}
    <StatusBadge variant={saveStatusState.variant || 'idle'} label={saveStatusState.label} />
  {/if}
  {#if selectedFile}
    <button
      class="btn btn-secondary btn-compact"
      onclick={onReloadFile}
      disabled={loading}
      title={$t('editor.reload')}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8" /><path d="M21 3v5h-5" /></svg
      >
      {$t('editor.reload')}
    </button>
    <button
      class="btn btn-secondary btn-compact"
      onclick={onSaveFile}
      disabled={saving || applyLoading}
      title={$t('app.save')}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        ><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z" /><polyline
          points="17 21 17 13 7 13 7 21"
        /><polyline points="7 3 7 8 15 8" /></svg
      >
      {saving ? $t('app.loading') : $t('app.save')}
    </button>
    <button
      class="btn btn-accent btn-compact"
      onclick={onSaveAndApply}
      disabled={saving || applyLoading}
      title={$t('editor.save_and_apply')}
    >
      {#if applyLoading}
        <span class="ks-dot-spin"
          ><span class="ks-dot"></span><span class="ks-dot"></span><span class="ks-dot"
          ></span></span
        >
        {$t('app.loading')}
      {:else}
        <svg
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          ><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z" /><polyline
            points="17 21 17 13 7 13 7 21"
          /><polyline points="7 3 7 8 15 8" /><path d="m14 11-2 2-2-2" /><path d="M12 7v6" /></svg
        >
        {$t('editor.save_and_apply')}
      {/if}
    </button>
  {/if}
</div>

<style>
  .eph-right {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .btn-compact {
    padding: 4px 10px;
    font-size: 12px;
    height: 30px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .btn-accent {
    background: linear-gradient(180deg, var(--accent), var(--accent-2));
    border: 1px solid var(--accent);
    color: var(--btn-primary-text, var(--bg-deep));
    font-weight: 600;
  }
  .btn-accent:hover:not(:disabled) {
    background: var(--accent-hover);
    box-shadow: 0 0 10px var(--accent-soft);
  }
  .btn-accent:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .ks-dot-spin {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-right: 6px;
  }
  .ks-dot {
    width: 6px;
    height: 6px;
    background-color: currentColor;
    border-radius: 50%;
    animation: ks-dot-bounce 1.4s infinite ease-in-out both;
  }
  .ks-dot:nth-child(1) {
    animation-delay: -0.32s;
  }
  .ks-dot:nth-child(2) {
    animation-delay: -0.16s;
  }

  @keyframes ks-dot-bounce {
    0%,
    80%,
    100% {
      transform: scale(0);
    }
    40% {
      transform: scale(1);
    }
  }
</style>
