<script lang="ts">
  import { get } from 'svelte/store';
  import { t, tp } from '../../i18n';
  import { showConfirm } from '../../stores';
  import Button from '../Button.svelte';
  import Skeleton from '../Skeleton.svelte';
  import Spinner from '../Spinner.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    draftChanges,
    driftCount,
    layerStatus,
    applyNow,
    resetDraft,
    handleLayerError
  } from '../../lib/configLayer';
  import { routingView, loadRouting } from '../../lib/configRouting';
  import { geodataView, loadGeodata } from '../../lib/configGeodata';
  import TakeRoutingModal from './routing/TakeRoutingModal.svelte';
  import { ownerState } from './routing/routingOwner';
  import ApplyPreviewModal from './ApplyPreviewModal.svelte';
  import { applyPreviewOpen, openApplyPreview } from '../../lib/configPreview';
  import { applyGate, applyGateHint, applyHolderHint } from './applyGate';

  interface Props {
    /** Без полосы: остаются только перехват взятия и окно изменений (страница мастера, 149 D-23 шаг 5). */
    bare?: boolean;
  }

  let { bare = false }: Props = $props();

  let resetting = $state(false);

  // Причины блокировки считает общий стор applyGate: полоса и шаг 5 мастера читают один
  // источник, а не свои копии (149 D-08, D-15)
  const ready = $derived($applyGate.ready);
  const hasSnapshot = $derived($applyGate.hasSnapshot);
  // Первая загрузка: снимка ещё нет — скелетон строки статуса с резервом высоты. Обновление
  // поверх снимка: счётчик и кнопки из снимка, рядом спиннер, полоса не пустеет
  const firstLoad = $derived($applyGate.firstLoad);
  const refreshing = $derived($applyGate.refreshing);
  const shown = $derived(ready || refreshing);
  const dirty = $derived($applyGate.dirty);
  const lost = $derived($applyGate.lost);
  // Применение идёт из этой вкладки (флаг ставится до ответа сервера) или из другой (D-16)
  const applyingHere = $derived($applyGate.applyingHere);
  const applyingOther = $derived($applyGate.applyingOther);
  // Менеджер geodata держит общий замок (D-11): -ug, установка jq, уборка записи или запись
  // xkeen.json по кнопке. Пока замок занят, «Применить» неактивна и рядом видна причина
  const holderHint = $derived(applyHolderHint($applyGate, $t));
  // Локальный «идёт сброс» складывается только в занятость полосы, не в общий стор
  const busy = $derived($applyGate.busy || resetting);
  // Пустой черновик: две неактивные кнопки ничего не предлагают, а на телефоне занимают
  // две строки по 44 px. Кнопки появляются вместе с первой правкой или идущим применением
  const showActions = $derived(!firstLoad && (dirty || busy));
  const blockedByDrift = $derived($applyGate.blockedByDrift);
  // Проблемы проверки черновика (пустая группа, нет списка, цикл): «Применить» отказывает,
  // пока их не исправят (GRP-05, SRC-07)
  const checkCount = $derived($applyGate.checkCount);
  const conflictText = $derived($t('cfg.apply_blocked_conflict'));
  const driftText = $derived($t('cfg.apply_blocked_drift', { k: $driftCount }));
  // Связь со слоем потеряна: состояние черновика на сервере неизвестно, поэтому обе кнопки
  // неактивны, а «Применить» в title называет ту же причину, что и строка статуса
  const applyHint = $derived(applyGateHint($applyGate, $t));
  const showReasons = $derived(
    shown &&
      dirty &&
      ($applyGate.blockedByConflict || blockedByDrift || checkCount > 0 || holderHint !== '')
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
    // Конфликт ядер (D-18): правки в черновик принимаются, а окно изменений не открывается
    if ($applyGate.blockedByConflict) return;
    if (await shouldIntercept()) {
      interceptOpen = true;
      return;
    }
    openApplyPreview();
  }

  /**
   * Единственный вход шага 5 мастера в «Применить»: тот же путь, что у кнопки полосы —
   * перехват взятия маршрутизации, затем окно изменений. Конвейер запускает только
   * «Применить изменения» в окне, с ревизией черновика, которую окно показало (149 D-08).
   */
  export function apply(): void {
    void onApply();
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

{#if !bare && (hasSnapshot || $layerStatus !== 'error')}
  <section
    class="draft-bar card"
    class:draft-bar-idle={!showActions}
    data-testid="config-draftbar"
    aria-label={$t('cfg.draftbar_label')}
    aria-busy={firstLoad || refreshing}
  >
    <div class="draft-bar-status" aria-live="polite">
      {#if firstLoad}
        <span class="status-skeleton" data-testid="config-draftbar-loading">
          <Skeleton type="text-line" width="160px" />
        </span>
      {:else if lost}
        <span class="status-line status-muted">
          <span class="spinner" aria-hidden="true"></span>
          <span class="status-text">{$t('cfg.sse_lost')}</span>
        </span>
      {:else if applyingHere}
        <span class="status-line">
          <span class="spinner" aria-hidden="true"></span>
          <span class="status-text">{$t('cfg.applying')}</span>
        </span>
      {:else if applyingOther}
        <span class="status-line">
          <span class="status-icon status-icon-info"><Icon name="info" size={14} /></span>
          <span class="status-text">{$t('cfg.applying_other_tab')}</span>
        </span>
      {:else if shown && dirty}
        <span class="status-line">
          <span class="status-icon status-icon-accent"><Icon name="edit" size={14} /></span>
          <span class="status-text label-long">{$t('cfg.draft_dirty', { n: $draftChanges })}</span>
          <span class="status-text label-short">
            {$t('cfg.draft_dirty_short', { n: $draftChanges })}
          </span>
          {#if refreshing}<Spinner size={16} />{/if}
        </span>
      {:else if shown}
        <span class="status-line">
          <span class="status-icon status-icon-success"><Icon name="check" size={14} /></span>
          <span class="status-text">{$t('cfg.draft_clean')}</span>
          {#if refreshing}<Spinner size={16} />{/if}
        </span>
      {/if}
    </div>
    {#if showActions}
      <div class="draft-bar-actions">
        <Button
          variant="secondary"
          class="draft-bar-btn"
          ariaLabel={$t('cfg.reset_aria')}
          title={lost ? $t('cfg.sse_lost') : $t('cfg.reset_aria')}
          disabled={!ready || !dirty || busy || lost}
          onclick={onReset}
        >
          {$t('cfg.reset')}
        </Button>
        <Button
          variant="primary"
          class="draft-bar-btn"
          data-testid="config-apply"
          ariaLabel={$t('cfg.apply_aria')}
          disabled={!$applyGate.canApply || resetting}
          loading={applyingHere}
          title={applyHint}
          onclick={onApply}
        >
          {$t('cfg.apply')}
        </Button>
      </div>
    {/if}
    {#if showReasons || holderHint !== ''}
      <div class="draft-bar-reasons">
        {#if $applyGate.blockedByConflict}
          <span class="status-sub" data-testid="config-apply-blocked-conflict">{conflictText}</span>
        {/if}
        {#if shown && dirty && blockedByDrift}
          <span class="status-sub" data-testid="config-apply-blocked-drift">{driftText}</span>
        {/if}
        {#if shown && dirty && checkCount > 0}
          <span
            class="status-sub"
            title={$t('cfg.routing.apply_blocked_hint')}
            data-testid="config-apply-blocked"
          >
            {$tp('cfg.routing.apply_blocked', checkCount)}
          </span>
        {/if}
        {#if holderHint !== ''}
          <span class="status-sub status-holder" title={holderHint} data-testid="config-geo-busy">
            {holderHint}
          </span>
        {/if}
      </div>
    {/if}
  </section>
{/if}

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
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    column-gap: var(--spacing-4);
    row-gap: var(--spacing-1);
    min-height: 44px;
    padding: var(--spacing-3) var(--spacing-4);
    border: 1px solid var(--border);
    box-shadow: var(--shadow-sm);
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  /* Пустой черновик — спокойная строка состояния без тени и лишней высоты */
  .draft-bar.draft-bar-idle {
    padding-top: var(--spacing-2);
    padding-bottom: var(--spacing-2);
    box-shadow: none;
    background: var(--surface-tint);
  }

  .draft-bar-status {
    display: flex;
    align-items: center;
    flex: 1;
    min-width: 0;
  }

  .status-skeleton {
    display: flex;
    align-items: center;
    width: 160px;
    max-width: 100%;
  }

  .status-line {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
    color: var(--fg-primary);
  }

  .status-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .label-short {
    display: none;
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

  /* Причины блокировки — второй строкой под рядом статуса и кнопок */
  .draft-bar-reasons {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    flex-basis: 100%;
    min-width: 0;
  }

  .status-sub {
    color: var(--warning);
    font-size: var(--font-size-base);
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
    /* Один ряд: статус слева, кнопки по содержимому справа; высота с правками ≤ 64 px */
    .draft-bar {
      column-gap: var(--spacing-2);
      padding: var(--spacing-2) var(--spacing-3);
      /* Липкая мобильная шапка (.mobile-header, 69 px + safe-area) иначе перекрыла бы полосу */
      top: calc(var(--mhdr-h) + env(safe-area-inset-top) + var(--dns-banner-h, 0px));
    }

    .draft-bar-actions :global(.draft-bar-btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .label-long {
      display: none;
    }

    .label-short {
      display: inline;
    }
  }
</style>
