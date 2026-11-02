<script lang="ts">
  import { toastStore, type ToastItem } from '../stores';
  import Icon from '../lib/components/Icon.svelte';
  import { t } from '../i18n';
  import { fly, flip, easeOut } from '../lib/motion.svelte';

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

<div class="toast-container" role="region" aria-label={$t('app.notifications')}>
  {#each $toastStore as toast (toast.id)}
    <!-- Вход справа, уход туда же; оставшиеся тосты съезжают на место ушедшего (flip),
         а не прыгают. -->
    <div
      class="toast toast--{toast.type}"
      in:fly={{ x: 24, duration: 220, easing: easeOut }}
      out:fly={{ x: 24, duration: 160, opacity: 0 }}
      animate:flip={{ duration: 220, easing: easeOut }}
      role={toast.type === 'error' ? 'alert' : 'status'}
      aria-live={toast.type === 'error' ? 'assertive' : 'polite'}
      aria-atomic="true"
    >
      <span class="toast__icon">
        <Icon name={getIconName(toast.type)} size={16} />
      </span>
      <span class="toast__message">{toast.message}</span>
      {#if toast.action}
        <button
          type="button"
          class="btn btn-secondary toast__action"
          onclick={() => {
            toast.action?.onClick();
            dismiss(toast.id);
          }}
        >
          {toast.action.label}
        </button>
      {/if}
      <button
        type="button"
        class="toast__close"
        onclick={() => dismiss(toast.id)}
        aria-label={$t('app.dismiss')}>×</button
      >
    </div>
  {/each}
</div>

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
    font-family: var(--font-family-sans);
    font-size: var(--font-size-sm);
    border-left-width: 3px;
  }
  .toast--success {
    border-left-color: var(--success);
    box-shadow:
      0 20px 36px -16px rgba(0, 0, 0, 0.6),
      0 0 24px -4px color-mix(in srgb, var(--success) 25%, transparent);
  }
  .toast--error {
    border-left-color: var(--danger);
    box-shadow:
      0 20px 36px -16px rgba(0, 0, 0, 0.6),
      0 0 24px -4px color-mix(in srgb, var(--danger) 25%, transparent);
  }
  .toast--warning {
    border-left-color: var(--warning);
    box-shadow:
      0 20px 36px -16px rgba(0, 0, 0, 0.6),
      0 0 24px -4px color-mix(in srgb, var(--warning) 25%, transparent);
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
  .toast__action {
    flex-shrink: 0;
    padding: 4px 10px;
    font-size: var(--font-size-xs);
  }
  .toast__close {
    flex-shrink: 0;
    background: transparent;
    border: 0;
    cursor: pointer;
    color: var(--fg-dim);
    font-size: var(--font-size-xl);
    line-height: 1;
    padding: 0 0 0 6px;
    transition: color var(--transition-fast);
  }
  .toast__close:hover {
    color: var(--fg-primary);
  }
</style>
