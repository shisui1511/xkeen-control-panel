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

      {#if $confirmStore.extra}
        {@const extra = $confirmStore.extra}
        <extra.component {...extra.props} />
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
    gap: var(--spacing-3);
    margin-bottom: var(--spacing-6);
  }

  .confirm-object {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
    word-break: break-word;
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--bg-card);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
  }

  .confirm-message {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .confirm-consequence {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    font-size: var(--font-size-sm);
    line-height: 1.4;
    padding: var(--spacing-2-5) var(--spacing-3);
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    color: var(--danger);
    border: 1px solid color-mix(in srgb, var(--danger) 30%, transparent);
  }

  .confirm-consequence.warning {
    background: var(--warning-soft);
    color: var(--warning);
    border-color: color-mix(in srgb, var(--warning) 30%, transparent);
  }

  .confirm-consequence svg {
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
  }

  .confirm-actions {
    display: flex;
    gap: var(--spacing-3);
    justify-content: flex-end;
  }
</style>
