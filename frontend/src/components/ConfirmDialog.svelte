<script lang="ts">
  import { confirmStore } from '../stores';
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
    <p class="dialog-message">{$confirmStore.message}</p>
    <div class="dialog-actions">
      <Button variant="secondary" onclick={cancel}>{$confirmStore.cancelLabel}</Button>
      <Button variant="primary" onclick={confirm}>{$confirmStore.confirmLabel}</Button>
    </div>
  {/if}
</Modal>

<style>
  .dialog-message {
    margin: 0 0 var(--spacing-6);
    font-size: var(--font-size-sm);
    color: var(--color-text-secondary);
    line-height: 1.5;
  }

  .dialog-actions {
    display: flex;
    gap: var(--spacing-3);
    justify-content: flex-end;
  }
</style>
