<script lang="ts">
  import { get } from 'svelte/store';
  import { t } from '../i18n';
  import Warning from '../lib/components/icons/Warning.svelte';
  import Button from './Button.svelte';
  import { fetchCapabilities, isConflict, runningKernels, showConfirm, showToast } from '../stores';
  import { KERNEL_NAMES, kernelLabel } from '../lib/kernelState';
  import { stopKernelProcess, type KernelName } from '../lib/serviceControl';

  // Ядро, которое останавливается сейчас; пока не null — обе кнопки неактивны.
  let stopping = $state<KernelName | null>(null);

  async function stopKernel(kernel: KernelName) {
    if (stopping) return;
    const label = kernelLabel(kernel);
    const confirmed = await showConfirm({
      title: $t('kernel.conflict_stop_confirm_title', { kernel: label }),
      message: $t('kernel.conflict_stop_confirm_msg', { kernel: label }),
      objectName: label,
      confirmLabel: $t('kernel.conflict_stop', { kernel: label }),
      cancelLabel: $t('app.cancel'),
      variant: 'warning'
    });
    if (!confirmed) return;

    stopping = kernel;
    try {
      await stopKernelProcess(kernel);
      await fetchCapabilities();
      if (!get(isConflict)) {
        const other = KERNEL_NAMES.find((k) => k !== kernel) ?? kernel;
        showToast('success', $t('kernel.conflict_resolved', { kernel: kernelLabel(other) }));
      } else {
        // Конфликт остался: процесс не остановился (still_running) либо второй
        // процесс появился снова — в обоих случаях пользователю нужен повтор или логи.
        showToast('error', $t('kernel.stop_still_running', { kernel: label }), 10000);
      }
    } catch (e: unknown) {
      if ((e as { status?: number })?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e), 10000);
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
