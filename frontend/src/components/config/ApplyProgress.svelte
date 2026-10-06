<script lang="ts">
  import { t } from '../../i18n';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    layerSnapshot,
    applyStartedHere,
    type LayerStep,
    type LayerRestart,
    type StepId,
    type StepState
  } from '../../lib/configLayer';

  // Порядок шагов фиксирован (D-16): недостающие в состоянии показываются ожидающими
  const STEP_ORDER: StepId[] = ['build', 'validate_xray', 'validate_mihomo', 'write', 'restart'];
  const KERNEL_LABELS: Record<string, string> = { xray: 'Xray', mihomo: 'Mihomo' };

  let section = $state<HTMLElement | null>(null);
  // Ход виден, пока идёт применение, и после него — до «Скрыть» или следующего запуска.
  // Свежая загрузка страницы с уже завершённым применением его не показывает.
  let seenRunning = $state(false);
  let dismissed = $state(false);

  const apply = $derived($layerSnapshot?.apply ?? null);
  const running = $derived(apply?.running ?? false);
  const result = $derived(apply?.result ?? null);

  $effect(() => {
    if (running || $applyStartedHere) {
      seenRunning = true;
      dismissed = false;
    }
  });

  const visible = $derived(running || (seenRunning && !dismissed && result !== null));

  const steps = $derived(
    STEP_ORDER.map(
      (id): LayerStep => apply?.steps.find((s) => s.id === id) ?? { id, state: 'pending' }
    )
  );
  const restartRows = $derived<LayerRestart[]>(apply?.restart ?? []);

  // Карточка появилась — прокручиваем к ней, не дёргая страницу при reduced-motion
  let wasVisible = false;
  $effect(() => {
    if (visible && !wasVisible && section) {
      const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      section.scrollIntoView({ block: 'nearest', behavior: reduced ? 'auto' : 'smooth' });
    }
    wasVisible = visible;
  });

  const kernelLabel = (k: string) => KERNEL_LABELS[k] ?? k;

  function markerLabel(state: StepState): string {
    return $t('cfg.step_state.' + state);
  }

  function stepNote(step: LayerStep): string {
    if (!step.note_code) return '';
    const key = 'cfg.step_note.' + step.note_code;
    const text = $t(key);
    return text === key ? '' : text;
  }

  const orphans = $derived(result?.orphans_removed ?? []);
</script>

{#if visible}
  <section
    class="card apply-progress"
    data-testid="config-progress"
    aria-label={$t('cfg.progress_title')}
    bind:this={section}
  >
    <div class="card-title">{$t('cfg.progress_title')}</div>

    <ol class="steps" aria-live="polite">
      {#each steps as step (step.id)}
        {@const note = stepNote(step)}
        <li class="step" data-step={step.id} data-state={step.state}>
          <span class="marker marker-{step.state}" role="img" aria-label={markerLabel(step.state)}>
            {#if step.state === 'running'}
              <span class="spinner" aria-hidden="true"></span>
            {:else if step.state === 'done'}
              <Icon name="check" size={16} />
            {:else if step.state === 'failed'}
              <Icon name="cross" size={16} />
            {:else if step.state === 'deferred'}
              <Icon name="info" size={16} />
            {:else if step.state === 'skipped'}
              <span class="dash" aria-hidden="true">–</span>
            {:else}
              <span class="ring" aria-hidden="true"></span>
            {/if}
          </span>
          <div class="step-body">
            <span class="step-label">{$t('cfg.step.' + step.id)}</span>
            {#if note}
              <span class="step-note">{note}</span>
            {/if}
            {#if step.id === 'restart' && restartRows.length > 0}
              <ul class="restart-rows">
                {#each restartRows as row (row.kernel)}
                  <li class="restart-row" data-kernel={row.kernel} data-outcome={row.outcome}>
                    {$t('cfg.restart.' + row.outcome, { kernel: kernelLabel(row.kernel) })}
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        </li>
      {/each}
    </ol>

    {#if result?.ok}
      <div class="alert alert-success result" role="status" data-testid="config-apply-success">
        <Icon name="check" size={16} />
        <div class="result-body">
          <span>{$t('cfg.result.success', { n: result.written })}</span>
          {#if orphans.length > 0}
            <span class="result-line">{$t('cfg.result.orphans', { list: orphans.join(', ') })}</span
            >
          {/if}
        </div>
      </div>
    {/if}

    {#if !running && result}
      <div class="progress-actions">
        <Button variant="secondary" class="btn-sm" onclick={() => (dismissed = true)}
          >{$t('cfg.hide')}</Button
        >
      </div>
    {/if}
  </section>
{/if}

<style>
  .apply-progress {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .step {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    min-width: 0;
  }

  .marker {
    flex-shrink: 0;
    width: 20px;
    height: 20px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  .marker-done {
    color: var(--success);
  }

  .marker-failed {
    color: var(--danger);
  }

  .marker-skipped {
    color: var(--fg-dim);
  }

  .marker-deferred {
    color: var(--fg-secondary);
  }

  .marker .spinner {
    --spinner-size: 16px;
    --spinner-color: var(--accent);
  }

  .ring {
    box-sizing: border-box;
    width: 12px;
    height: 12px;
    border-radius: var(--radius-full);
    border: 2px solid var(--fg-faint);
  }

  .dash {
    font-size: var(--font-size-base);
    line-height: 1;
  }

  .step-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .step-label {
    font-size: var(--font-size-base);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .step-note {
    font-size: var(--font-size-xs);
    line-height: 1.4;
    color: var(--fg-dim);
  }

  .restart-rows {
    list-style: none;
    margin: 4px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .restart-row {
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .result {
    margin-bottom: 0;
    align-items: flex-start;
  }

  .result-body {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--fg-primary);
  }

  .progress-actions {
    display: flex;
    justify-content: flex-start;
  }

  @media (max-width: 768px) {
    .progress-actions :global(.btn) {
      min-height: 44px;
      flex: 1;
    }
  }
</style>
