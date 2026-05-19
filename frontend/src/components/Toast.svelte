<script lang="ts">
  import { toastStore, type ToastItem } from '../stores'

  function dismiss(id: number) {
    toastStore.update(items => items.filter(t => t.id !== id))
  }

  function icon(type: ToastItem['type']): string {
    if (type === 'success') return '✓'
    if (type === 'error') return '✗'
    return 'ℹ'
  }
</script>

{#if $toastStore.length > 0}
  <div class="toast-container" role="region" aria-label="Notifications">
    {#each $toastStore as toast (toast.id)}
      <div class="toast toast--{toast.type}" role="alert">
        <span class="toast__icon">{icon(toast.type)}</span>
        <span class="toast__message">{toast.message}</span>
        <button class="toast__close" on:click={() => dismiss(toast.id)} aria-label="Dismiss">&times;</button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .toast-container {
    position: fixed;
    top: 1rem;
    right: 1rem;
    z-index: 9999;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-width: 360px;
    pointer-events: none;
  }

  .toast {
    display: flex;
    align-items: flex-start;
    gap: 0.625rem;
    padding: 0.75rem 1rem;
    border-radius: 6px;
    background: var(--card-bg);
    color: var(--text);
    border: 1px solid var(--border);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
    pointer-events: all;
    animation: toast-in 0.2s ease;
  }

  .toast--success {
    border-left: 3px solid var(--success, #3fb950);
  }

  .toast--error {
    border-left: 3px solid var(--danger, #f85149);
  }

  .toast--info {
    border-left: 3px solid var(--accent, #58a6ff);
  }

  .toast__icon {
    flex-shrink: 0;
    font-weight: bold;
    font-size: 0.9rem;
    margin-top: 1px;
  }

  .toast--success .toast__icon { color: var(--success, #3fb950); }
  .toast--error .toast__icon { color: var(--danger, #f85149); }
  .toast--info .toast__icon { color: var(--accent, #58a6ff); }

  .toast__message {
    flex: 1;
    font-size: 0.875rem;
    line-height: 1.4;
    word-break: break-word;
  }

  .toast__close {
    flex-shrink: 0;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-secondary, #8b949e);
    font-size: 1.1rem;
    line-height: 1;
    padding: 0;
    margin-top: -1px;
    opacity: 0.7;
    transition: opacity 0.15s;
  }

  .toast__close:hover {
    opacity: 1;
  }

  @keyframes toast-in {
    from { opacity: 0; transform: translateX(0.5rem); }
    to   { opacity: 1; transform: translateX(0); }
  }
</style>
