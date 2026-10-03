<script lang="ts">
  import { t } from '../../i18n';
  import { showToast, fetchCapabilities, isConflict } from '../../stores';
  import { serviceAction } from '../../lib/serviceControl';
  import { isServiceRestarting } from '../../lib/serviceGrace';
  import Icon from '../../lib/components/Icon.svelte';

  let { activeKernel = '', onRestart } = $props<{
    activeKernel?: string;
    onRestart?: () => void;
  }>();

  let isRestarting = $state(false);

  let ledClass = $derived.by(() => {
    if ($isServiceRestarting || isRestarting) return 'led-amber-pulse';
    if ($isConflict) return 'led-red';
    if (activeKernel && activeKernel !== 'none') return 'led-green';
    return 'led-gray';
  });

  let kernelName = $derived.by(() => {
    if ($isConflict) return $t('kernel.state_conflict');
    if (!activeKernel || activeKernel === 'none') return 'Core';
    return activeKernel.charAt(0).toUpperCase() + activeKernel.slice(1);
  });

  async function handleQuickRestart(e: MouseEvent) {
    e.stopPropagation();
    if (isRestarting || $isServiceRestarting || $isConflict) return;

    isRestarting = true;

    showToast('info', $t('capsule.toast_restarting_kernel', { kernel: kernelName }));
    if (onRestart) onRestart();

    try {
      await serviceAction('restart');
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || $t('app.error'));
    } finally {
      isRestarting = false;
    }
  }
</script>

<div
  class="editor-kernel-widget"
  title={$isConflict
    ? $t('kernel.conflict_blocked')
    : $t('capsule.restart_kernel', { kernel: kernelName })}
>
  <div class="widget-status">
    <span class="led-dot {ledClass}"></span>
    <span class="widget-name">{kernelName}</span>
  </div>
  <button
    type="button"
    class="widget-restart-btn"
    onclick={handleQuickRestart}
    disabled={isRestarting || $isServiceRestarting || $isConflict}
    title={$isConflict ? $t('kernel.conflict_blocked') : undefined}
    aria-label={$isConflict
      ? `${$t('capsule.restart_kernel', { kernel: kernelName })}: ${$t('kernel.conflict_blocked')}`
      : $t('capsule.restart_kernel', { kernel: kernelName })}
  >
    <span class="icon-wrap" class:spinning={isRestarting || $isServiceRestarting}>
      <Icon name="refresh" size={13} />
    </span>
  </button>
</div>

<style>
  .editor-kernel-widget {
    display: inline-flex;
    align-items: center;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 6px);
    height: 28px;
    padding: 0 4px 0 8px;
    gap: 6px;
    user-select: none;
  }

  .widget-status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .widget-name {
    font-size: 12px;
    font-weight: 600;
    color: var(--text);
  }

  .led-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .led-green {
    background-color: var(--success);
    box-shadow: 0 0 5px color-mix(in srgb, var(--success) 55%, transparent);
  }

  .led-red {
    background-color: var(--danger);
    box-shadow: 0 0 5px color-mix(in srgb, var(--danger) 55%, transparent);
  }

  .led-amber-pulse {
    background-color: var(--warning);
    box-shadow: 0 0 7px color-mix(in srgb, var(--warning) 65%, transparent);
    animation: pulse-amber 1.2s infinite ease-in-out;
  }

  .led-gray {
    background-color: var(--fg-dim);
  }

  @keyframes pulse-amber {
    0%,
    100% {
      opacity: 1;
      transform: scale(1);
    }
    50% {
      opacity: 0.4;
      transform: scale(1.2);
    }
  }

  .widget-restart-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    padding: 0;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: var(--fg-dim);
    cursor: pointer;
    transition:
      background 0.15s ease,
      color 0.15s ease;
  }

  .widget-restart-btn:hover:not(:disabled) {
    background: var(--hover);
    color: var(--accent);
  }

  .widget-restart-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .icon-wrap {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  .spinning {
    animation: spin 1s linear infinite;
  }
</style>
