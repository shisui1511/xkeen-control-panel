<script lang="ts">
  import type { BatchProgressState } from '../lib/batchLatencyTester';
  import { t } from '../i18n';

  interface Props {
    progress: BatchProgressState;
    onCancel: () => void;
  }

  let { progress, onCancel }: Props = $props();

  let percent = $derived(
    progress.total > 0 ? Math.min(100, Math.round((progress.current / progress.total) * 100)) : 0
  );
</script>

<div class="floating-progress-bar" role="status" aria-live="polite">
  <div class="progress-icon">
    {#if progress.currentNodeFlag}
      <span aria-hidden="true">{progress.currentNodeFlag}</span>
    {:else}
      <span class="lat-spinner" aria-hidden="true"></span>
    {/if}
  </div>

  <div class="progress-content">
    <div class="progress-label">
      <span class="progress-text" title={progress.currentNode}>
        {$t('proxies.testing_progress', {
          current: progress.current,
          total: progress.total,
          nodeName: progress.currentNode || '...'
        })}
      </span>
      <span>{percent}%</span>
    </div>

    {#if progress.retrying}
      <div class="progress-retry-badge">
        <span class="lat-spinner"></span>
        {$t('proxies.rate_limit_retry', { seconds: progress.retrySeconds })}
      </div>
    {/if}

    <div class="progress-track">
      <div class="progress-fill" style="width: {percent}%"></div>
    </div>
  </div>

  <button type="button" class="btn-cancel" onclick={onCancel}>
    {$t('proxies.cancel_test')}
  </button>
</div>
