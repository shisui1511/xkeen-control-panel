<script lang="ts">
  import { t } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { showToast, showConfirm, mihomoApiReady, mihomoOfflineReason } from '../../stores';
  import Icon from '../../lib/components/Icon.svelte';
  import Card from '../Card.svelte';
  import Section from '../Section.svelte';
  import { summarizeGroupDelay } from '../../lib/groupDelay';

  let {
    onSwitchTab
  }: {
    onSwitchTab?: (tab: string) => void;
  } = $props();

  let isRefreshingSubs = $state(false);
  let isTestingLatency = $state(false);
  let isResettingSessions = $state(false);
  let isCreatingBackup = $state(false);

  // Тест задержки и сброс сессий ходят в API Mihomo: пока он не отвечает, кнопки неактивны,
  // а title объясняет причину. Подписки и резервная копия от API ядра не зависят.
  const offlineTitle = $derived(
    $mihomoOfflineReason ? $t(`dash.mihomo_offline_${$mihomoOfflineReason}`) : null
  );

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
      const targetUrl = encodeURIComponent('http://www.gstatic.com/generate_204');
      const global = await apiFetchJSON<{ all?: string[]; now?: string }>(
        '/api/mihomo/proxy/proxies/GLOBAL'
      );
      const res = await apiFetch(
        `/api/mihomo/proxy/group/GLOBAL/delay?url=${targetUrl}&timeout=5000`
      );
      if (res.ok) {
        // The group endpoint returns { member: delayMs } for every member.
        const s = summarizeGroupDelay(await res.json(), global.all ?? [], global.now);
        if (s.current !== null && global.now) {
          showToast(
            'success',
            $t('dash.qa_latency_test_current', {
              name: global.now,
              delay: String(s.current),
              alive: String(s.alive),
              total: String(s.total)
            })
          );
        } else if (s.best !== null) {
          showToast(
            'success',
            $t('dash.qa_latency_test_summary', {
              alive: String(s.alive),
              total: String(s.total),
              best: String(s.best)
            })
          );
        } else {
          showToast('error', $t('dash.qa_latency_test_none_alive'));
        }
        return;
      }
      // Older cores without group delay: measure GLOBAL itself.
      const data = await apiFetchJSON<{ delay?: number }>(
        `/api/mihomo/proxy/proxies/GLOBAL/delay?url=${targetUrl}&timeout=5000`
      );
      if (data?.delay) {
        showToast('success', $t('dash.qa_latency_test_success', { delay: String(data.delay) }));
      } else {
        showToast('error', $t('dash.qa_latency_test_err'));
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
  <Section title={$t('dash.quick_actions')}>
    <Card flush>
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
              <Icon name="subscriptions" size={16} color="var(--accent)" />
            {/if}
          </div>
          <div class="qa-content">
            <div class="qa-title">{$t('dash.qa_refresh_subs_title')}</div>
            <div class="qa-sub">{$t('dash.qa_refresh_subs_sub')}</div>
          </div>
          <span class="qa-chevron"><Icon name="chevron-right" size={14} /></span>
        </button>

        <!-- 2. Latency Test -->
        <button
          type="button"
          class="qa-btn"
          onclick={handleLatencyTest}
          disabled={isTestingLatency || !$mihomoApiReady}
          title={offlineTitle ?? $t('dash.qa_latency_test_sub')}
        >
          <div class="qa-icon-wrap" class:is-loading={isTestingLatency}>
            {#if isTestingLatency}
              <span class="spinner" aria-hidden="true"></span>
            {:else}
              <Icon name="traffic" size={16} color="var(--accent)" />
            {/if}
          </div>
          <div class="qa-content">
            <div class="qa-title">{$t('dash.qa_latency_test_title')}</div>
            <div class="qa-sub">{$t('dash.qa_latency_test_sub')}</div>
          </div>
          <span class="qa-chevron"><Icon name="chevron-right" size={14} /></span>
        </button>

        <!-- 3. Reset Sessions -->
        <button
          type="button"
          class="qa-btn qa-btn-destructive"
          onclick={handleResetSessions}
          disabled={isResettingSessions || !$mihomoApiReady}
          title={offlineTitle ?? $t('dash.qa_reset_sessions_sub')}
        >
          <div class="qa-icon-wrap icon-wrap-danger" class:is-loading={isResettingSessions}>
            {#if isResettingSessions}
              <span class="spinner" aria-hidden="true"></span>
            {:else}
              <Icon name="stop" size={16} color="var(--danger)" />
            {/if}
          </div>
          <div class="qa-content">
            <div class="qa-title title-danger">{$t('dash.qa_reset_sessions_title')}</div>
            <div class="qa-sub">{$t('dash.qa_reset_sessions_sub')}</div>
          </div>
          <span class="qa-chevron"><Icon name="chevron-right" size={14} /></span>
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
              <Icon name="download" size={16} color="var(--accent)" />
            {/if}
          </div>
          <div class="qa-content">
            <div class="qa-title">{$t('dash.qa_backup_title')}</div>
            <div class="qa-sub">{$t('dash.qa_backup_sub')}</div>
          </div>
          <span class="qa-chevron"><Icon name="chevron-right" size={14} /></span>
        </button>
      </div>
    </Card>
  </Section>
</div>

<style>
  .quick-actions-widget {
    width: 100%;
  }

  /* Действия — строки одного списка: иконка, текст, шеврон. Без рамки у каждой плитки. */
  .qa-grid {
    display: flex;
    flex-direction: column;
  }

  .qa-grid > :global(* + *) {
    border-top: 1px solid var(--border);
  }

  .qa-btn {
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
    width: 100%;
    min-height: 60px;
    padding: var(--spacing-3) var(--card-pad);
    border: 0;
    border-radius: 0;
    background: transparent;
    color: inherit;
    font-family: inherit;
    text-align: left;
    cursor: pointer;
    transition: background-color var(--transition-fast);
  }

  .qa-btn:hover:not(:disabled) {
    background: var(--surface-tint-faint);
  }

  .qa-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .qa-btn:disabled {
    cursor: not-allowed;
  }

  .qa-btn:disabled .qa-icon-wrap,
  .qa-btn:disabled .qa-content,
  .qa-btn:disabled .qa-chevron {
    opacity: 0.5;
  }

  .qa-icon-wrap {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    flex-shrink: 0;
    border-radius: var(--radius-md);
    background: var(--accent-soft);
    color: var(--accent);
  }

  .icon-wrap-danger {
    background: var(--danger-soft);
    color: var(--danger);
  }

  .qa-content {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }

  .qa-title {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
    line-height: 1.3;
    letter-spacing: -0.01em;
  }

  .title-danger {
    color: var(--danger);
  }

  .qa-sub {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    line-height: 1.35;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .qa-chevron {
    display: flex;
    flex-shrink: 0;
    color: var(--fg-faint);
    transition:
      color var(--transition-fast),
      transform var(--transition-fast);
  }

  .qa-btn:hover:not(:disabled) .qa-chevron {
    color: var(--fg-secondary);
    transform: translateX(2px);
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
