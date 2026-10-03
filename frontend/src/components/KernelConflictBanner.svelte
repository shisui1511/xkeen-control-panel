<script lang="ts">
  import { t } from '../i18n';
  import Warning from '../lib/components/icons/Warning.svelte';
  import Button from './Button.svelte';
  import { runningKernels } from '../stores';
  import { kernelLabel } from '../lib/kernelState';
  import { confirmKernelStop, stopKernelAndReport, type KernelName } from '../lib/serviceControl';

  // Ядро, которое останавливается сейчас; пока не null — обе кнопки неактивны.
  let stopping = $state<KernelName | null>(null);

  // Подтверждение и итог (тосты, перечитывание capabilities) — в общих функциях
  // serviceControl, одинаковых для баннера и карточки дашборда.
  async function stopKernel(kernel: KernelName) {
    if (stopping) return;
    if (!(await confirmKernelStop(kernel))) return;

    stopping = kernel;
    try {
      await stopKernelAndReport(kernel);
    } finally {
      stopping = null;
    }
  }
</script>

<div
  class="alert alert-error kernel-conflict"
  role="alert"
  aria-label={$t('kernel.conflict_region')}
  data-testid="kernel-conflict-banner"
>
  <span class="kernel-conflict__icon" aria-hidden="true">
    <Warning size={18} />
  </span>
  <div class="kernel-conflict__text">
    <strong>{$t('kernel.conflict_title')}</strong>
    <p>{$t('kernel.conflict_desc')}</p>
  </div>
  <div class="kernel-conflict__actions">
    {#each $runningKernels as kernel (kernel)}
      <Button
        variant="secondary"
        class="btn-sm"
        title={$t('kernel.conflict_stop_title', { kernel: kernelLabel(kernel) })}
        ariaLabel={$t('kernel.conflict_stop_title', { kernel: kernelLabel(kernel) })}
        data-testid={'kernel-conflict-stop-' + kernel}
        loading={stopping === kernel}
        disabled={stopping !== null}
        onclick={() => stopKernel(kernel)}
      >
        {$t('kernel.conflict_stop', { kernel: kernelLabel(kernel) })}
      </Button>
    {/each}
  </div>
</div>

<style>
  .kernel-conflict {
    margin: 12px 16px 0;
    padding: 12px 16px;
    justify-content: space-between;
    align-items: flex-start;
    gap: 16px;
  }

  .kernel-conflict__icon {
    display: inline-flex;
    flex: none;
    margin-top: 2px;
  }

  .kernel-conflict__text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .kernel-conflict__text strong {
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    overflow-wrap: anywhere;
  }

  .kernel-conflict__text p {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 400;
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .kernel-conflict__actions {
    display: flex;
    flex: none;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }

  @media (max-width: 640px) {
    .kernel-conflict {
      flex-direction: column;
    }

    .kernel-conflict__actions {
      flex-direction: column;
      align-items: stretch;
      width: 100%;
    }

    .kernel-conflict__actions :global(.btn) {
      width: 100%;
      min-height: 40px;
    }
  }
</style>
