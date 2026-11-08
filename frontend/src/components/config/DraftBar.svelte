<script lang="ts">
  import { get } from 'svelte/store';
  import { t, tp } from '../../i18n';
  import { showConfirm } from '../../stores';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    draftChanges,
    driftCount,
    applyRunning,
    applyStartedHere,
    layerSnapshot,
    layerStatus,
    layerConnection,
    geodataHolder,
    applyNow,
    resetDraft,
    handleLayerError
  } from '../../lib/configLayer';
  import { routingView, loadRouting } from '../../lib/configRouting';
  import { geodataView, loadGeodata } from '../../lib/configGeodata';
  import TakeRoutingModal from './routing/TakeRoutingModal.svelte';
  import { ownerState, routingBusy } from './routing/routingOwner';
  import ApplyPreviewModal from './ApplyPreviewModal.svelte';
  import { applyPreviewOpen, openApplyPreview } from '../../lib/configPreview';

  let resetting = $state(false);

  const ready = $derived($layerStatus === 'ready');
  const dirty = $derived($draftChanges > 0);
  const hasDrift = $derived($driftCount > 0);
  const lost = $derived($layerConnection === 'lost');
  // Применение идёт из этой вкладки (флаг ставится до ответа сервера) или из другой (D-16)
  const applyingHere = $derived($applyStartedHere);
  const applyingOther = $derived($applyRunning && !$applyStartedHere);
  // Менеджер geodata держит общий замок (D-11): -ug, установка jq или уборка записи. Пока
  // замок занят, «Применить» неактивна и рядом видна причина
  const ugBusy = $derived($geodataHolder === 'geodata_update');
  const removeBusy = $derived($geodataHolder === 'geodata_remove');
  // Запись xkeen.json по кнопке и уборка временной записи тоже идут под замком
  const writeBusy = $derived($geodataHolder === 'geodata_write');
  const holderHint = $derived(
    ugBusy
      ? $t('cfg.geo.ug_busy')
      : removeBusy
        ? $t('cfg.routing.apply_busy_hint')
        : writeBusy
          ? $t('cfg.geo.write_busy')
          : ''
  );
  const busy = $derived(applyingHere || applyingOther || resetting || $routingBusy);
  // Пустой черновик: две неактивные кнопки ничего не предлагают, а на телефоне занимают
  // две строки по 44 px. Кнопки появляются вместе с первой правкой или идущим применением
  const showActions = $derived(dirty || busy);
  const blockedByDrift = $derived(dirty && hasDrift);
  // Проблемы проверки черновика (пустая группа, нет списка, цикл): «Применить» отказывает,
  // пока их не исправят (GRP-05, SRC-07)
  const checkCount = $derived($layerSnapshot?.checks.length ?? 0);
  const blockedByChecks = $derived(dirty && checkCount > 0);
  const applyHint = $derived(
    holderHint !== ''
      ? holderHint
      : blockedByChecks
        ? $t('cfg.routing.apply_blocked_hint')
        : blockedByDrift
          ? $t('cfg.apply_blocked_hint')
          : undefined
  );

  // «Применить» при группах или правилах в черновике, пока маршрутизацией управляет XKeen:
  // сначала диалог взятия с тремя исходами (D-01)
  let interceptOpen = $state(false);

  async function shouldIntercept(): Promise<boolean> {
    if ($ownerState.kind !== 'xkeen') return false;
    if (get(routingView) === null) await loadRouting();
    const view = get(routingView);
    return (view?.groups.length ?? 0) > 0 || (view?.rules.length ?? 0) > 0;
  }

  // «Применить» открывает окно изменений, а не конвейер (D-08): конвейер запускает только
  // «Применить изменения» в окне, с ревизией черновика, которую окно показало
  async function onApply() {
    if (await shouldIntercept()) {
      interceptOpen = true;
      return;
    }
    openApplyPreview();
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
      // Пометки менеджера тоже сброшены: «Списки» перечитываются, не дожидаясь события
      if (get(geodataView) !== null) void loadGeodata();
    } catch (err) {
      handleLayerError(err);
    } finally {
      resetting = false;
    }
  }
</script>

<section
  class="draft-bar card"
  class:draft-bar-idle={!showActions}
  data-testid="config-draftbar"
  aria-label={$t('cfg.draftbar_label')}
>
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
      {#if checkCount > 0}
        <span
          class="status-sub"
          title={$t('cfg.routing.apply_blocked_hint')}
          data-testid="config-apply-blocked"
        >
          {$tp('cfg.routing.apply_blocked', checkCount)}
        </span>
      {/if}
    {:else if ready}
      <span class="status-line">
        <span class="status-icon status-icon-success"><Icon name="check" size={14} /></span>
        {$t('cfg.draft_clean')}
      </span>
    {/if}
    {#if holderHint !== ''}
      <span class="status-sub status-holder" title={holderHint} data-testid="config-geo-busy">
        {holderHint}
      </span>
    {/if}
  </div>
  {#if showActions}
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
        disabled={!ready ||
          !dirty ||
          busy ||
          holderHint !== '' ||
          blockedByDrift ||
          blockedByChecks}
        loading={applyingHere}
        title={applyHint}
        onclick={onApply}>{$t('cfg.apply')}</Button
      >
    </div>
  {/if}
</section>

<TakeRoutingModal open={interceptOpen} mode="intercept" onclose={() => (interceptOpen = false)} />

<ApplyPreviewModal
  open={$applyPreviewOpen}
  onclose={() => applyPreviewOpen.set(false)}
  onconfirm={(rev) => applyNow(rev)}
/>

<style>
  .draft-bar {
    position: sticky;
    /* Под приклеенным баннером окна подтверждения DNS (--dns-banner-h, 0 без окна) */
    top: calc(0px + var(--dns-banner-h, 0px));
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-4);
    padding: var(--spacing-3) var(--spacing-4);
    border: 1px solid var(--border);
    box-shadow: var(--shadow-sm);
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  /* Пустой черновик — спокойная строка состояния без тени и лишней высоты */
  .draft-bar.draft-bar-idle {
    padding-top: var(--spacing-2-5);
    padding-bottom: var(--spacing-2-5);
    box-shadow: none;
    background: var(--surface-tint);
  }

  .draft-bar-status {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .status-line {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
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
    overflow-wrap: anywhere;
  }

  .status-holder {
    color: var(--fg-secondary);
  }

  .draft-bar-actions {
    display: flex;
    gap: var(--spacing-2);
    flex-shrink: 0;
  }

  .draft-bar-actions :global(.draft-bar-btn) {
    flex-shrink: 0;
    white-space: nowrap;
  }

  @media (max-width: 768px) {
    .draft-bar {
      flex-wrap: wrap;
      /* Липкая мобильная шапка (.mobile-header, 69 px + safe-area) иначе перекрыла бы полосу */
      top: calc(var(--mhdr-h) + env(safe-area-inset-top) + var(--dns-banner-h, 0px));
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
