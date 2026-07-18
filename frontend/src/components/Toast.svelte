<script lang="ts">
  import { toastStore, type ToastItem } from '../stores';
  import Icon from '../lib/components/Icon.svelte';

  function dismiss(id: number) {
    toastStore.update((items) => items.filter((t) => t.id !== id));
  }

  function getIconName(type: ToastItem['type']): string {
    if (type === 'success') return 'check';
    if (type === 'error') return 'cross';
    if (type === 'warning') return 'warning';
    return 'info';
  }
</script>

{#if $toastStore.length > 0}
  <div class="toast-container" role="region" aria-label="Notifications">
    {#each $toastStore as toast (toast.id)}
      <div class="toast toast--{toast.type}" role="alert">
        <span class="toast__icon">
          <Icon name={getIconName(toast.type)} size={16} />
        </span>
        <span class="toast__message">{toast.message}</span>
        <button class="toast__close" onclick={() => dismiss(toast.id)} aria-label="Dismiss"
          >×</button
        >
      </div>
    {/each}
  </div>
{/if}

<style>
  .toast-container {
    position: fixed;
    top: 78px;
    right: 18px;
    z-index: 9999;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 380px;
    pointer-events: none;
  }
  .toast {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 14px;
    border-radius: var(--radius-md);
    background: linear-gradient(180deg, var(--surface-overlay-from), var(--surface-overlay-to));
    color: var(--fg-primary);
    border: 1px solid var(--border);
    box-shadow: 0 20px 36px -16px rgba(0, 0, 0, 0.6);
    pointer-events: all;
    animation: toast-in 180ms ease;
    font-family: var(--font-family-sans);
    font-size: 13px;
    border-left-width: 3px;
  }
  .toast--success {
    border-left-color: var(--success);
    box-shadow:
      0 20px 36px -16px rgba(0, 0, 0, 0.6),
      0 0 24px -4px rgba(70, 209, 138, 0.25);
  }
  .toast--error {
    border-left-color: var(--danger);
    box-shadow:
      0 20px 36px -16px rgba(0, 0, 0, 0.6),
      0 0 24px -4px rgba(239, 91, 107, 0.25);
  }
  .toast--warning {
    border-left-color: var(--warning);
    box-shadow:
      0 20px 36px -16px rgba(0, 0, 0, 0.6),
      0 0 24px -4px rgba(240, 180, 80, 0.25);
  }
  .toast--info {
    border-left-color: var(--accent);
    box-shadow:
      0 20px 36px -16px rgba(0, 0, 0, 0.6),
      0 0 24px -4px var(--accent-soft);
  }
  .toast__icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    margin-top: 1px;
  }
  .toast--success .toast__icon {
    color: var(--success);
  }
  .toast--error .toast__icon {
    color: var(--danger);
  }
  .toast--warning .toast__icon {
    color: var(--warning);
  }
  .toast--info .toast__icon {
    color: var(--accent);
  }
  .toast__message {
    flex: 1;
    line-height: 1.4;
    word-break: break-word;
  }
  .toast__close {
    flex-shrink: 0;
    background: transparent;
    border: 0;
    cursor: pointer;
    color: var(--fg-dim);
    font-size: 18px;
    line-height: 1;
    padding: 0 0 0 6px;
    transition: color var(--transition-fast);
  }
  .toast__close:hover {
    color: var(--fg-primary);
  }

  @keyframes toast-in {
    from {
      opacity: 0;
      transform: translateX(12px);
    }
    to {
      opacity: 1;
      transform: translateX(0);
    }
  }
</style>
