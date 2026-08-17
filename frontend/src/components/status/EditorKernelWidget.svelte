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
    background: var(--bg-card, #16202c);
    border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
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
    color: var(--text, #e2e8f0);
  }

  .led-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .led-green {
    background-color: #22c55e;
    box-shadow: 0 0 5px rgba(34, 197, 94, 0.6);
  }

  .led-amber-pulse {
    background-color: #f59e0b;
    box-shadow: 0 0 7px rgba(245, 158, 11, 0.8);
    animation: pulse-amber 1.2s infinite ease-in-out;
  }

  .led-gray {
    background-color: #64748b;
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
    color: var(--text-muted, #94a3b8);
    cursor: pointer;
    transition:
      background 0.15s ease,
      color 0.15s ease;
  }

  .widget-restart-btn:hover:not(:disabled) {
    background: var(--hover, rgba(255, 255, 255, 0.08));
    color: var(--accent, #38bdf8);
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

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }
</style>
