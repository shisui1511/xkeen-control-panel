<script lang="ts">
  import type { Attachment } from 'svelte/attachments';
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

  // Modal монтирует содержимое только пока он открыт: вложение на теле диалога
  // грузит предпросмотр при каждом открытии, а при закрытии сбрасывает состояние.
  const syncWithOpenState: Attachment = () => {
    loadPreview();
    return () => {
      previewData = null;
      errorMessage = null;
      loading = false;
      stepMessage = '';
    };
  };

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

<Modal {open} title={$t('mihomo.migrate_title')} maxWidth="560px" {onclose}>
  <div class="migrate-modal-body" {@attach syncWithOpenState}>
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
        <div class="spinner" style="--spinner-size: 20px;"></div>
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
              stroke="var(--success)"
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
              stroke="var(--success)"
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
              stroke="var(--success)"
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
        <div class="spinner" style="--spinner-size: 14px;"></div>
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
    gap: var(--spacing-4);
  }

  .info-banner {
    display: flex;
    gap: var(--spacing-3);
    align-items: flex-start;
    padding: var(--spacing-3) var(--spacing-3-5);
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
    border-radius: var(--radius-md);
    color: var(--fg-primary);
  }

  .info-icon {
    color: var(--accent);
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
  }

  .info-desc {
    margin: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .loading-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2-5);
    padding: 30px 0;
    color: var(--fg-dim);
    font-size: var(--font-size-sm);
  }

  .diff-container {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
  }

  .diff-header {
    padding: var(--spacing-1-5) var(--spacing-3);
    background: var(--surface-tint-faint);
    border-bottom: 1px solid var(--border);
    color: var(--fg-dim);
    font-size: var(--font-size-xs);
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  .diff-content {
    padding: var(--spacing-2) 0;
  }

  .diff-line {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    padding: 3px var(--spacing-3);
    line-height: 1.4;
  }

  .diff-del {
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    color: var(--danger);
  }

  .diff-add {
    background: color-mix(in srgb, var(--success) 14%, transparent);
    color: var(--success);
  }

  .diff-sign {
    font-weight: bold;
    width: 10px;
    user-select: none;
  }

  .guarantees-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1-5);
    background: var(--surface-tint-faint);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--spacing-3) var(--spacing-3-5);
  }

  .guarantees-title {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-dim);
    letter-spacing: 0.02em;
    margin-bottom: var(--spacing-1);
  }

  .guarantee-item {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .error-banner {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    padding: var(--spacing-2-5) var(--spacing-3);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 25%, transparent);
    border-radius: var(--radius-md);
    color: var(--danger);
    font-size: var(--font-size-sm);
  }

  .success-banner {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    padding: var(--spacing-3) var(--spacing-3-5);
    background: color-mix(in srgb, var(--success) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--success) 25%, transparent);
    border-radius: var(--radius-md);
    color: var(--success);
    font-size: var(--font-size-sm);
  }

  .step-progress {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
    color: var(--accent);
    padding: var(--spacing-1) 0;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--spacing-2-5);
    margin-top: var(--spacing-2);
    padding-top: var(--spacing-3);
    border-top: 1px solid var(--border);
  }
</style>
