<script lang="ts">
  import { confirmStore } from '../stores';
  import { t } from '../i18n';
  import Modal from './Modal.svelte';
  import Button from './Button.svelte';

  function confirm() {
    if ($confirmStore) {
      $confirmStore.resolve(true);
      confirmStore.set(null);
    }
  }

  function cancel() {
    if ($confirmStore) {
      $confirmStore.resolve(false);
      confirmStore.set(null);
    }
  }
</script>

<Modal isOpen={$confirmStore !== null} title={$confirmStore?.title || ''} onclose={cancel}>
  {#if $confirmStore}
    {@const variant = $confirmStore.variant || 'danger'}
    <div class="confirm-body">
      {#if $confirmStore.objectName}
        <div class="confirm-object">{$confirmStore.objectName}</div>
      {/if}

      {#if $confirmStore.message}
        <p class="confirm-message">{$confirmStore.message}</p>
      {/if}

      {#if $confirmStore.consequence}
        <div
          class="confirm-consequence"
          class:danger={variant === 'danger'}
          class:warning={variant === 'warning'}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path
              d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
            />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
          <span>{$confirmStore.consequence}</span>
        </div>
      {/if}
    </div>

    <div class="confirm-actions">
      <Button variant="secondary" onclick={cancel}>
        {$confirmStore.cancelLabel || $t('app.cancel')}
      </Button>
      <Button
        variant={variant === 'danger' ? 'danger' : variant === 'warning' ? 'warning' : 'primary'}
        onclick={confirm}
      >
        {$confirmStore.confirmLabel || $t('app.confirm')}
      </Button>
    </div>
  {/if}
</Modal>

<style>
  .confirm-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3, 12px);
    margin-bottom: var(--spacing-6, 24px);
  }

  .confirm-object {
    font-size: var(--font-size-base, 14px);
    font-weight: 600;
    color: var(--fg-primary);
    word-break: break-word;
    padding: 8px 12px;
    background: var(--bg-tertiary, rgba(255, 255, 255, 0.04));
    border-radius: var(--radius-sm, 6px);
    border: 1px solid var(--border-color, rgba(255, 255, 255, 0.08));
  }

  .confirm-message {
    margin: 0;
    font-size: var(--font-size-sm, 13px);
    color: var(--fg-secondary, rgba(255, 255, 255, 0.7));
    line-height: 1.5;
  }

  .confirm-consequence {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    font-size: 12.5px;
    line-height: 1.4;
    padding: 10px 12px;
    border-radius: var(--radius-sm, 6px);
    background: rgba(239, 68, 68, 0.1);
    color: var(--color-danger, #f87171);
    border: 1px solid rgba(239, 68, 68, 0.2);
  }

  .confirm-consequence.warning {
    background: rgba(245, 158, 11, 0.1);
    color: var(--color-warning, #fbbf24);
    border-color: rgba(245, 158, 11, 0.2);
  }

  .confirm-consequence svg {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .confirm-actions {
    display: flex;
    gap: var(--spacing-3, 12px);
    justify-content: flex-end;
  }
</style>
