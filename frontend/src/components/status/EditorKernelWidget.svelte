<script lang="ts">
  import { t } from '../../i18n';
  import { showToast, fetchCapabilities } from '../../stores';
  import { apiFetch } from '../../lib/api';
  import { isServiceRestarting, activateRestartGrace } from '../../lib/serviceGrace';
  import Icon from '../../lib/components/Icon.svelte';

  let { activeKernel = '', onRestart } = $props<{
    activeKernel?: string;
    onRestart?: () => void;
  }>();

  let isRestarting = $state(false);

  let ledClass = $derived.by(() => {
    if ($isServiceRestarting || isRestarting) return 'led-amber-pulse';
    if (activeKernel && activeKernel !== 'none') return 'led-green';
    return 'led-gray';
  });

  let kernelName = $derived.by(() => {
    if (!activeKernel || activeKernel === 'none') return 'Core';
    return activeKernel.charAt(0).toUpperCase() + activeKernel.slice(1);
  });

  async function handleQuickRestart(e: MouseEvent) {
    e.stopPropagation();
    if (isRestarting || $isServiceRestarting) return;

    isRestarting = true;
    activateRestartGrace(6000);

    showToast('info', $t('capsule.toast_restarting_kernel', { kernel: kernelName }));
    if (onRestart) onRestart();

    try {
      const res = await apiFetch('/api/service/control?action=restart', {
        method: 'POST'
      });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt);
      }
      await fetchCapabilities();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e?.message || 'Error restarting kernel');
    } finally {
      isRestarting = false;
    }
  }
</script>

<div class="editor-kernel-widget" title={$t('capsule.restart_kernel', { kernel: kernelName })}>
  <div class="widget-status">
    <span class="led-dot {ledClass}"></span>
    <span class="widget-name">{kernelName}</span>
  </div>
  <button
    type="button"
    class="widget-restart-btn"
    onclick={handleQuickRestart}
    disabled={isRestarting || $isServiceRestarting}
    aria-label={$t('capsule.restart_kernel', { kernel: kernelName })}
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
    font-size: 11px;
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
