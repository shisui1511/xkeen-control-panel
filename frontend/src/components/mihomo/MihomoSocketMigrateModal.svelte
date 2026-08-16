<script lang="ts">
  import Modal from '../Modal.svelte';
  import { t } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { fetchCapabilities, showToast } from '../../stores';

  let {
    open = $bindable(false),
    onclose,
    onsuccess
  }: {
    open: boolean;
    onclose: () => void;
    onsuccess?: () => void;
  } = $props();

  let loading = $state(false);
  let previewLoading = $state(false);
  let previewData = $state<{
    current_controller?: string;
    target_socket?: string;
    diff_old?: string;
    diff_new?: string;
    is_insecure?: boolean;
    already_migrated?: boolean;
  } | null>(null);
  let errorMessage = $state<string | null>(null);
  let stepMessage = $state<string>('');

  $effect(() => {
    if (open) {
      loadPreview();
    } else {
      previewData = null;
      errorMessage = null;
      loading = false;
      stepMessage = '';
    }
  });

  async function loadPreview() {
    previewLoading = true;
    errorMessage = null;
    try {
      const data = await apiFetchJSON<any>('/api/config/mihomo-migrate-socket?preview=true');
      previewData = data;
    } catch (e: any) {
      errorMessage = e.message || $t('app.error');
    } finally {
      previewLoading = false;
    }
  }

  async function handleApplyMigration() {
    loading = true;
    errorMessage = null;
    stepMessage = $t('mihomo.migrate_in_progress');

    try {
      const res = await apiFetch('/api/config/mihomo-migrate-socket', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'apply' })
      });

      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data.error || `HTTP ${res.status}`);
      }

      showToast('success', $t('mihomo.migrate_success'));
      await fetchCapabilities();
      if (onsuccess) onsuccess();
      onclose();
    } catch (e: any) {
      errorMessage = e.message || $t('app.error');
    } finally {
      loading = false;
      stepMessage = '';
    }
  }
</script>

<Modal isOpen={open} title={$t('mihomo.migrate_title')} maxWidth="560px" {onclose}>
  <div class="migrate-modal-body">
    <div class="info-banner">
      <div class="info-icon">
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
        </svg>
      </div>
      <div class="info-text">
        <p class="info-desc">{$t('mihomo.migrate_desc')}</p>
      </div>
    </div>

    {#if previewLoading}
      <div class="loading-state">
        <div class="spinner"></div>
        <span>{$t('app.loading')}</span>
      </div>
    {:else if errorMessage}
      <div class="error-banner">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
        <span>{errorMessage}</span>
      </div>
    {:else if previewData}
      {#if previewData.already_migrated}
        <div class="success-banner">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <polyline points="20 6 9 17 4 12" />
          </svg>
          <span>{$t('mihomo.migrate_already_migrated')}</span>
        </div>
      {:else}
        <div class="diff-container">
          <div class="diff-header">
            <span>{$t('mihomo.migrate_diff_title')}</span>
          </div>
          <div class="diff-content">
            {#if previewData.diff_old}
              <div class="diff-line diff-del">
                <span class="diff-sign">-</span>
                <span class="diff-text">{previewData.diff_old}</span>
              </div>
            {/if}
            {#if previewData.diff_new}
              <div class="diff-line diff-add">
                <span class="diff-sign">+</span>
                <span class="diff-text">{previewData.diff_new}</span>
              </div>
            {/if}
          </div>
        </div>

        <div class="guarantees-list">
          <div class="guarantees-title">{$t('mihomo.migrate_guarantees_title')}</div>
          <div class="guarantee-item">
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-success)"
              stroke-width="2"
            >
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <span>{$t('mihomo.migrate_backup_guarantee')}</span>
          </div>
          <div class="guarantee-item">
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-success)"
              stroke-width="2"
            >
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <span>{$t('mihomo.migrate_validation_guarantee')}</span>
          </div>
          <div class="guarantee-item">
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-success)"
              stroke-width="2"
            >
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <span>{$t('mihomo.migrate_rollback_guarantee')}</span>
          </div>
        </div>
      {/if}
    {/if}

    {#if loading}
      <div class="step-progress">
        <div class="spinner-small"></div>
        <span>{stepMessage}</span>
      </div>
    {/if}

    <div class="modal-actions">
      <button class="btn btn-secondary" onclick={onclose} disabled={loading}>
        {$t('app.cancel')}
      </button>
      {#if previewData && !previewData.already_migrated}
        <button
          class="btn btn-primary"
          onclick={handleApplyMigration}
          disabled={loading || previewLoading}
        >
          {$t('mihomo.migrate_btn')}
        </button>
      {/if}
    </div>
  </div>
</Modal>

<style>
  .migrate-modal-body {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .info-banner {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 12px 14px;
    background: rgba(59, 130, 246, 0.08);
    border: 1px solid rgba(59, 130, 246, 0.2);
    border-radius: var(--radius-md);
    color: var(--fg-primary);
  }

  .info-icon {
    color: var(--color-info, #3b82f6);
    flex-shrink: 0;
    margin-top: 2px;
  }

  .info-desc {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .loading-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 30px 0;
    color: var(--fg-dim);
    font-size: 13px;
  }

  .spinner {
    width: 20px;
    height: 20px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  .spinner-small {
    width: 14px;
    height: 14px;
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

  .diff-container {
    background: var(--bg-tertiary, #12151c);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    font-family: var(--font-mono, monospace);
    font-size: 12px;
  }

  .diff-header {
    padding: 6px 12px;
    background: rgba(255, 255, 255, 0.03);
    border-bottom: 1px solid var(--border);
    color: var(--fg-dim);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .diff-content {
    padding: 8px 0;
  }

  .diff-line {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 3px 12px;
    line-height: 1.4;
  }

  .diff-del {
    background: rgba(239, 68, 68, 0.12);
    color: #fca5a5;
  }

  .diff-add {
    background: rgba(34, 197, 94, 0.12);
    color: #86efac;
  }

  .diff-sign {
    font-weight: bold;
    width: 10px;
    user-select: none;
  }

  .guarantees-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px 14px;
  }

  .guarantees-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    margin-bottom: 4px;
  }

  .guarantee-item {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--fg-secondary);
  }

  .error-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.25);
    border-radius: var(--radius-md);
    color: var(--color-danger, #ef4444);
    font-size: 13px;
  }

  .success-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 14px;
    background: rgba(34, 197, 94, 0.1);
    border: 1px solid rgba(34, 197, 94, 0.25);
    border-radius: var(--radius-md);
    color: var(--color-success, #22c55e);
    font-size: 13px;
  }

  .step-progress {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--accent);
    padding: 4px 0;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    margin-top: 8px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }
</style>
