<script lang="ts">
  import { t } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { showToast, showConfirm } from '../../stores';
  import Icon from '../../lib/components/Icon.svelte';
  import Card from '../Card.svelte';

  let { onSwitchTab } = $props<{
    onSwitchTab?: (tab: string) => void;
  }>();

  let isRefreshingSubs = $state(false);
  let isTestingLatency = $state(false);
  let isResettingSessions = $state(false);
  let isCreatingBackup = $state(false);

  async function handleRefreshSubs() {
    if (isRefreshingSubs) return;
    isRefreshingSubs = true;
    showToast('info', $t('dash.qa_refresh_subs_title') + '...');
    try {
      const res = await apiFetch('/api/subscriptions/refresh-all', { method: 'POST' });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      showToast('success', $t('dash.qa_refresh_subs_success'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    } finally {
      isRefreshingSubs = false;
    }
  }

  async function handleLatencyTest() {
    if (isTestingLatency) return;
    isTestingLatency = true;
    showToast('info', $t('dash.qa_latency_test_title') + '...');
    try {
      const targetUrl = 'http://www.gstatic.com/generate_204';
      const res = await apiFetch(
        `/api/mihomo/proxy/group/GLOBAL/delay?url=${encodeURIComponent(targetUrl)}&timeout=5000`
      );
      if (res.ok) {
        const data = await res.json();
        const delayVal = data?.delay ?? data?.GLOBAL ?? 0;
        showToast(
          'success',
          $t('dash.qa_latency_test_success', { delay: String(delayVal || '—') })
        );
      } else {
        // Fallback check on generic proxy latency
        const fbRes = await apiFetch(
          `/api/mihomo/proxy/proxies/GLOBAL/delay?url=${encodeURIComponent(targetUrl)}&timeout=5000`
        );
        if (fbRes.ok) {
          const fbData = await fbRes.json();
          const delayVal = fbData?.delay ?? 0;
          showToast(
            'success',
            $t('dash.qa_latency_test_success', { delay: String(delayVal || '—') })
          );
        } else {
          showToast('error', $t('dash.qa_latency_test_err'));
        }
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('dash.qa_latency_test_err'));
    } finally {
      isTestingLatency = false;
    }
  }

  async function handleResetSessions() {
    if (isResettingSessions) return;
    const confirmed = await showConfirm({
      title: $t('dash.qa_reset_sessions_confirm_title'),
      message: $t('dash.qa_reset_sessions_confirm_msg'),
      confirmLabel: $t('dash.qa_reset_sessions_btn'),
      cancelLabel: $t('app.cancel'),
      variant: 'danger'
    });
    if (!confirmed) return;

    isResettingSessions = true;
    try {
      const res = await apiFetch('/api/mihomo/proxy/connections', { method: 'DELETE' });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      showToast('success', $t('dash.qa_reset_sessions_success'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    } finally {
      isResettingSessions = false;
    }
  }

  async function handleDownloadBackup() {
    if (isCreatingBackup) return;
    isCreatingBackup = true;
    showToast('info', $t('dash.qa_backup_title') + '...');
    try {
      const dateStr = new Date().toISOString().slice(0, 10);
      const meta = await apiFetchJSON<{ id: string }>('/api/snapshots/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label: `dashboard-backup-${dateStr}` })
      });
      if (meta && meta.id) {
        window.location.href = `/api/snapshots/${meta.id}/download`;
        showToast('success', $t('dash.qa_backup_success'));
      } else {
        showToast('error', $t('dash.qa_backup_err_id'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('dash.qa_backup_err'));
    } finally {
      isCreatingBackup = false;
    }
  }
</script>

<div class="quick-actions-widget">
  <Card title={$t('dash.quick_actions')}>
    <div class="qa-grid">
      <!-- 1. Refresh Subscriptions -->
      <button
        type="button"
        class="qa-btn"
        onclick={handleRefreshSubs}
        disabled={isRefreshingSubs}
        title={$t('dash.qa_refresh_subs_sub')}
      >
        <div class="qa-icon-wrap" class:is-loading={isRefreshingSubs}>
          {#if isRefreshingSubs}
            <span class="spinner" aria-hidden="true"></span>
          {:else}
            <Icon name="subscriptions" size={18} color="var(--accent, #29c2f0)" />
          {/if}
        </div>
        <div class="qa-content">
          <div class="qa-title">{$t('dash.qa_refresh_subs_title')}</div>
          <div class="qa-sub">{$t('dash.qa_refresh_subs_sub')}</div>
        </div>
      </button>

      <!-- 2. Latency Test -->
      <button
        type="button"
        class="qa-btn"
        onclick={handleLatencyTest}
        disabled={isTestingLatency}
        title={$t('dash.qa_latency_test_sub')}
      >
        <div class="qa-icon-wrap" class:is-loading={isTestingLatency}>
          {#if isTestingLatency}
            <span class="spinner" aria-hidden="true"></span>
          {:else}
            <Icon name="traffic" size={18} color="var(--success, #46d18a)" />
          {/if}
        </div>
        <div class="qa-content">
          <div class="qa-title">{$t('dash.qa_latency_test_title')}</div>
          <div class="qa-sub">{$t('dash.qa_latency_test_sub')}</div>
        </div>
      </button>

      <!-- 3. Reset Sessions -->
      <button
        type="button"
        class="qa-btn qa-btn-destructive"
        onclick={handleResetSessions}
        disabled={isResettingSessions}
        title={$t('dash.qa_reset_sessions_sub')}
      >
        <div class="qa-icon-wrap icon-wrap-danger" class:is-loading={isResettingSessions}>
          {#if isResettingSessions}
            <span class="spinner" aria-hidden="true"></span>
          {:else}
            <Icon name="stop" size={18} color="var(--error, #f4707f)" />
          {/if}
        </div>
        <div class="qa-content">
          <div class="qa-title title-danger">{$t('dash.qa_reset_sessions_title')}</div>
          <div class="qa-sub">{$t('dash.qa_reset_sessions_sub')}</div>
        </div>
      </button>

      <!-- 4. Download Backup -->
      <button
        type="button"
        class="qa-btn"
        onclick={handleDownloadBackup}
        disabled={isCreatingBackup}
        title={$t('dash.qa_backup_sub')}
      >
        <div class="qa-icon-wrap" class:is-loading={isCreatingBackup}>
          {#if isCreatingBackup}
            <span class="spinner" aria-hidden="true"></span>
          {:else}
            <Icon name="download" size={18} color="var(--warning, #f0b450)" />
          {/if}
        </div>
        <div class="qa-content">
          <div class="qa-title">{$t('dash.qa_backup_title')}</div>
          <div class="qa-sub">{$t('dash.qa_backup_sub')}</div>
        </div>
      </button>
    </div>
  </Card>
</div>

<style>
  .quick-actions-widget {
    width: 100%;
  }

  .qa-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }

  @media (max-width: 640px) {
    .qa-grid {
      grid-template-columns: 1fr;
    }
  }

  .qa-btn {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--border, rgba(255, 255, 255, 0.06));
    border-radius: var(--radius-md, 10px);
    padding: 12px;
    text-align: left;
    cursor: pointer;
    transition:
      background 0.15s ease,
      border-color 0.15s ease,
      transform 0.12s ease;
    width: 100%;
    color: inherit;
    font-family: inherit;
  }

  .qa-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.04);
    border-color: rgba(41, 194, 240, 0.3);
    transform: translateY(-1px);
  }

  .qa-btn:active:not(:disabled) {
    transform: translateY(0);
  }

  .qa-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .qa-btn-destructive:hover:not(:disabled) {
    border-color: rgba(244, 112, 127, 0.4);
    background: rgba(244, 112, 127, 0.04);
  }

  .qa-icon-wrap {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 34px;
    height: 34px;
    border-radius: var(--radius-sm, 8px);
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid rgba(255, 255, 255, 0.06);
    flex-shrink: 0;
  }

  .icon-wrap-danger {
    background: rgba(244, 112, 127, 0.08);
    border-color: rgba(244, 112, 127, 0.2);
  }

  .qa-content {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .qa-title {
    font-size: 13.5px;
    font-weight: 600;
    color: var(--fg-primary, #ffffff);
    line-height: 1.25;
    letter-spacing: -0.01em;
  }

  .title-danger {
    color: var(--error, #f4707f);
  }

  .qa-sub {
    font-size: 11.5px;
    color: var(--fg-muted, var(--fg-secondary, #8fa3b8));
    line-height: 1.35;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .spinner {
    width: 14px;
    height: 14px;
    border: 2px solid currentColor;
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
