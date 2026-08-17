<script lang="ts">
  import { onMount } from 'svelte';
  import { fade } from 'svelte/transition';
  import { t } from '../../i18n';
  import { showToast, showConfirm, fetchCapabilities } from '../../stores';
  import { apiFetch } from '../../lib/api';
  import { activateRestartGrace } from '../../lib/serviceGrace';
  import Icon from '../../lib/components/Icon.svelte';

  let {
    isOpen = false,
    activeKernel = '',
    isXkeenRunning = true,
    onClose,
    onSwitchTab
  } = $props<{
    isOpen: boolean;
    activeKernel?: string;
    isXkeenRunning?: boolean;
    onClose: () => void;
    onSwitchTab: (tab: string) => void;
  }>();

  let isRestartingKernel = $state(false);
  let isRestartingXkeen = $state(false);
  let isTogglingService = $state(false);
  let menuEl = $state<HTMLElement | null>(null);

  async function handleRestartKernel() {
    if (isRestartingKernel) return;
    isRestartingKernel = true;
    activateRestartGrace(6000);

    const kernelName = activeKernel || 'Core';
    showToast('info', $t('capsule.toast_restarting_kernel', { kernel: kernelName }));
    onClose();

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
      isRestartingKernel = false;
    }
  }

  async function handleRestartXkeen() {
    if (isRestartingXkeen) return;
    isRestartingXkeen = true;
    activateRestartGrace(6000);

    showToast('info', $t('capsule.toast_restarting_xkeen'));
    onClose();

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
      showToast('error', e?.message || 'Error restarting XKeen');
    } finally {
      isRestartingXkeen = false;
    }
  }

  async function handleToggleService() {
    if (isTogglingService) return;

    if (isXkeenRunning) {
      const ok = await showConfirm({
        title: $t('capsule.confirm_stop_title'),
        message: $t('capsule.confirm_stop_message'),
        confirmLabel: $t('capsule.stop_service'),
        cancelLabel: $t('app.cancel'),
        variant: 'danger'
      });
      if (!ok) return;

      isTogglingService = true;
      onClose();

      try {
        const res = await apiFetch('/api/service/control?action=stop', {
          method: 'POST'
        });
        if (!res.ok) {
          const txt = await res.text();
          throw new Error(txt);
        }
        await fetchCapabilities();
        showToast('warning', $t('capsule.stop_service'));
      } catch (e: any) {
        if (e?.status === 401) return;
        showToast('error', e?.message || 'Error stopping service');
      } finally {
        isTogglingService = false;
      }
    } else {
      isTogglingService = true;
      activateRestartGrace(6000);
      onClose();

      try {
        const res = await apiFetch('/api/service/control?action=start', {
          method: 'POST'
        });
        if (!res.ok) {
          const txt = await res.text();
          throw new Error(txt);
        }
        await fetchCapabilities();
        showToast('success', $t('capsule.start_service'));
      } catch (e: any) {
        if (e?.status === 401) return;
        showToast('error', e?.message || 'Error starting service');
      } finally {
        isTogglingService = false;
      }
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!isOpen) return;
    if (e.key === 'Escape') {
      e.stopPropagation();
      onClose();
    }
  }

  function handleDocumentClick(e: MouseEvent) {
    if (!isOpen || !menuEl) return;
    if (!menuEl.contains(e.target as Node)) {
      onClose();
    }
  }

  $effect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeydown, true);
      // Attach click with microtask delay so trigger click doesn't close it instantly
      const timer = setTimeout(() => {
        document.addEventListener('click', handleDocumentClick);
      }, 0);
      return () => {
        clearTimeout(timer);
        document.removeEventListener('keydown', handleKeydown, true);
        document.removeEventListener('click', handleDocumentClick);
      };
    }
  });
</script>

{#if isOpen}
  <div
    bind:this={menuEl}
    class="system-quick-menu"
    role="menu"
    aria-label={$t('capsule.quick_actions')}
    tabindex="-1"
    transition:fade={{ duration: 120 }}
  >
    <div class="menu-header">
      <div class="menu-title-row">
        <span class="menu-title">{$t('capsule.quick_actions')}</span>
        {#if activeKernel}
          <span class="badge-kernel">{activeKernel}</span>
        {/if}
      </div>
    </div>

    <div class="menu-section">
      <button
        type="button"
        class="menu-item action-btn"
        onclick={handleRestartKernel}
        disabled={isRestartingKernel}
      >
        <span class="item-icon restart-icon" class:spinning={isRestartingKernel}>
          <Icon name="refresh" size={15} />
        </span>
        <span class="item-label">
          {$t('capsule.restart_kernel', { kernel: activeKernel || 'Core' })}
        </span>
      </button>

      <button
        type="button"
        class="menu-item action-btn"
        onclick={handleRestartXkeen}
        disabled={isRestartingXkeen}
      >
        <span class="item-icon" class:spinning={isRestartingXkeen}>
          <Icon name="services" size={15} />
        </span>
        <span class="item-label">{$t('capsule.restart_xkeen')}</span>
      </button>

      <button
        type="button"
        class="menu-item action-btn"
        class:danger-btn={isXkeenRunning}
        class:success-btn={!isXkeenRunning}
        onclick={handleToggleService}
        disabled={isTogglingService}
      >
        <span class="item-icon">
          {#if isXkeenRunning}
            <Icon name="stop" size={15} />
          {:else}
            <Icon name="play" size={15} />
          {/if}
        </span>
        <span class="item-label">
          {isXkeenRunning ? $t('capsule.stop_service') : $t('capsule.start_service')}
        </span>
      </button>
    </div>

    <div class="menu-divider"></div>

    <div class="menu-section nav-section">
      <button
        type="button"
        class="menu-item nav-btn"
        onclick={() => {
          onSwitchTab('logs');
          onClose();
        }}
      >
        <span class="item-icon"><Icon name="logs" size={15} /></span>
        <span class="item-label">{$t('capsule.nav_logs')}</span>
      </button>

      <button
        type="button"
        class="menu-item nav-btn"
        onclick={() => {
          onSwitchTab('services');
          onClose();
        }}
      >
        <span class="item-icon"><Icon name="services" size={15} /></span>
        <span class="item-label">{$t('capsule.nav_services')}</span>
      </button>

      <button
        type="button"
        class="menu-item nav-btn"
        onclick={() => {
          onSwitchTab('traffic');
          onClose();
        }}
      >
        <span class="item-icon"><Icon name="traffic" size={15} /></span>
        <span class="item-label">{$t('capsule.nav_traffic')}</span>
      </button>
    </div>
  </div>
{/if}

<style>
  .system-quick-menu {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    min-width: 250px;
    background: var(--bg-card, #16202c);
    border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
    border-radius: var(--radius-md, 10px);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.45);
    backdrop-filter: blur(16px);
    -webkit-backdrop-filter: blur(16px);
    padding: 6px;
    z-index: 1000;
    user-select: none;
    animation: menu-pop 0.15s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes menu-pop {
    from {
      opacity: 0;
      transform: translateY(-4px) scale(0.98);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  .menu-header {
    padding: 6px 10px 8px;
    border-bottom: 1px solid var(--border-light, rgba(255, 255, 255, 0.06));
    margin-bottom: 4px;
  }

  .menu-title-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .menu-title {
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted, #8a99a8);
  }

  .badge-kernel {
    font-size: 10px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 4px;
    background: var(--accent-subtle, rgba(56, 189, 248, 0.15));
    color: var(--accent, #38bdf8);
    border: 1px solid var(--accent-border, rgba(56, 189, 248, 0.3));
    text-transform: uppercase;
  }

  .menu-section {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .menu-divider {
    height: 1px;
    background: var(--border-light, rgba(255, 255, 255, 0.06));
    margin: 4px 0;
  }

  .menu-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 7px 10px;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm, 6px);
    color: var(--text, #e2e8f0);
    font-size: 13px;
    font-weight: 500;
    text-align: left;
    cursor: pointer;
    transition:
      background 0.15s ease,
      color 0.15s ease,
      transform 0.1s ease;
  }

  .menu-item:hover:not(:disabled) {
    background: var(--hover, rgba(255, 255, 255, 0.08));
    color: #fff;
  }

  .menu-item:active:not(:disabled) {
    transform: scale(0.99);
  }

  .menu-item:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .item-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    color: var(--text-muted, #94a3b8);
  }

  .menu-item:hover:not(:disabled) .item-icon {
    color: var(--accent, #38bdf8);
  }

  .danger-btn:hover:not(:disabled) .item-icon {
    color: var(--color-danger, #ef4444);
  }

  .success-btn:hover:not(:disabled) .item-icon {
    color: var(--color-success, #22c55e);
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

  .item-label {
    flex: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
