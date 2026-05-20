<script lang="ts">
  import { toastStore, type ToastItem } from '../stores'
  import Icon from '../lib/components/Icon.svelte'

  function dismiss(id: number) {
    toastStore.update(items => items.filter(t => t.id !== id))
  }

  function getIconName(type: ToastItem['type']): string {
    if (type === 'success') return 'check';
    if (type === 'error') return 'cross';
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
        <button class="toast__close" on:click={() => dismiss(toast.id)} aria-label="Dismiss">&times;</button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .toast-container {
    position: fixed;
    top: var(--spacing-4);
    right: var(--spacing-4);
    z-index: 9999;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    max-width: 360px;
    pointer-events: none;
  }

  .toast {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-3);
    padding: var(--spacing-3) var(--spacing-4);
    border-radius: var(--radius-md);
    background-color: var(--color-bg-surface);
    color: var(--color-text-primary);
    border: 1px solid var(--color-border-subtle);
    box-shadow: var(--shadow-md);
    pointer-events: all;
    animation: toast-in var(--transition-fast) ease;
    font-family: var(--font-family-sans);
  }

  .toast--success {
    border-left: 4px solid var(--success);
  }

  .toast--error {
    border-left: 4px solid var(--danger);
  }

  .toast--info {
    border-left: 4px solid var(--primary);
  }

  .toast__icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    margin-top: 2px;
  }

  .toast--success .toast__icon { color: var(--success); }
  .toast--error .toast__icon { color: var(--danger); }
  .toast--info .toast__icon { color: var(--primary); }

  .toast__message {
    flex: 1;
    font-size: var(--font-size-sm);
    line-height: 1.4;
    word-break: break-word;
  }

  .toast__close {
    flex-shrink: 0;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--color-text-secondary);
    font-size: var(--font-size-lg);
    line-height: 1;
    padding: 0;
    margin-top: -2px;
    opacity: 0.7;
    transition: opacity var(--transition-fast), color var(--transition-fast);
  }

  .toast__close:hover {
    opacity: 1;
    color: var(--color-text-primary);
  }

  @keyframes toast-in {
    from { opacity: 0; transform: translateX(1rem); }
    to   { opacity: 1; transform: translateX(0); }
  }
</style>
