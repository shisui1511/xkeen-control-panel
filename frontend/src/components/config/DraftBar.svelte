<script lang="ts">
  import { t } from '../../i18n';
  import { showConfirm } from '../../stores';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    draftChanges,
    driftCount,
    applyRunning,
    applyStartedHere,
    layerStatus,
    layerConnection,
    applyNow,
    resetDraft,
    handleLayerError
  } from '../../lib/configLayer';

  let resetting = $state(false);

  const ready = $derived($layerStatus === 'ready');
  const dirty = $derived($draftChanges > 0);
  const hasDrift = $derived($driftCount > 0);
  const lost = $derived($layerConnection === 'lost');
  // Применение идёт из этой вкладки (флаг ставится до ответа сервера) или из другой (D-16)
  const applyingHere = $derived($applyStartedHere);
  const applyingOther = $derived($applyRunning && !$applyStartedHere);
  const busy = $derived(applyingHere || applyingOther || resetting);
  const blockedByDrift = $derived(dirty && hasDrift);

  async function onApply() {
    try {
      await applyNow();
    } catch (err) {
      handleLayerError(err);
    }
  }

  async function onReset() {
    const confirmed = await showConfirm({
      variant: 'warning',
      title: $t('cfg.confirm.reset_title'),
      message: $t('cfg.confirm.reset_message', { n: $draftChanges }),
      consequence: $t('cfg.confirm.reset_consequence'),
      confirmLabel: $t('cfg.confirm.reset_ok')
    });
    if (!confirmed) return;
    resetting = true;
    try {
      await resetDraft();
    } catch (err) {
      handleLayerError(err);
    } finally {
      resetting = false;
    }
  }
</script>

<section class="draft-bar card" data-testid="config-draftbar" aria-label={$t('cfg.draftbar_label')}>
  <div class="draft-bar-status" aria-live="polite">
    {#if lost}
      <span class="status-line status-muted">
        <span class="spinner" aria-hidden="true"></span>
        {$t('cfg.sse_lost')}
      </span>
    {:else if applyingHere}
      <span class="status-line">
        <span class="spinner" aria-hidden="true"></span>
        {$t('cfg.applying')}
      </span>
    {:else if applyingOther}
      <span class="status-line">
        <span class="status-icon status-icon-info"><Icon name="info" size={14} /></span>
        {$t('cfg.applying_other_tab')}
      </span>
    {:else if ready && dirty}
      <span class="status-line">
        <span class="status-icon status-icon-accent"><Icon name="edit" size={14} /></span>
        {$t('cfg.draft_dirty', { n: $draftChanges })}
      </span>
      {#if blockedByDrift}
        <span class="status-sub">{$t('cfg.draft_blocked', { k: $driftCount })}</span>
      {/if}
    {:else if ready}
      <span class="status-line">
        <span class="status-icon status-icon-success"><Icon name="check" size={14} /></span>
        {$t('cfg.draft_clean')}
      </span>
    {/if}
  </div>
  <div class="draft-bar-actions">
    <Button
      variant="secondary"
      class="draft-bar-btn"
      disabled={!ready || !dirty || busy}
      onclick={onReset}>{$t('cfg.reset')}</Button
    >
    <Button
      variant="primary"
      class="draft-bar-btn"
      data-testid="config-apply"
      disabled={!ready || !dirty || busy || blockedByDrift}
      loading={applyingHere}
      title={blockedByDrift ? $t('cfg.apply_blocked_hint') : undefined}
      onclick={onApply}>{$t('cfg.apply')}</Button
    >
  </div>
</section>

<style>
  .draft-bar {
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 16px;
    border: 1px solid var(--border);
    box-shadow: var(--shadow-sm);
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .draft-bar-status {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .status-line {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--fg-primary);
  }

  .status-muted {
    color: var(--fg-dim);
    font-size: var(--font-size-xs);
  }

  .status-icon {
    display: inline-flex;
    flex-shrink: 0;
  }

  .status-icon-success {
    color: var(--success);
  }

  .status-icon-accent {
    color: var(--accent);
  }

  .status-icon-info {
    color: var(--fg-secondary);
  }

  .status-sub {
    color: var(--warning);
    font-size: var(--font-size-sm);
  }

  .draft-bar-actions {
    display: flex;
    gap: 8px;
    flex-shrink: 0;
  }

  @media (max-width: 768px) {
    .draft-bar {
      flex-wrap: wrap;
    }

    .draft-bar-status {
      flex: 1 1 100%;
    }

    .draft-bar-actions {
      flex: 1 1 100%;
    }

    .draft-bar-actions :global(.draft-bar-btn) {
      flex: 1;
      min-height: 44px;
    }
  }
</style>
