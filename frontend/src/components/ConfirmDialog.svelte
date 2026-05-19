<script lang="ts">
  import { confirmStore } from '../stores'

  function confirm() {
    if ($confirmStore) {
      $confirmStore.resolve(true)
      confirmStore.set(null)
    }
  }

  function cancel() {
    if ($confirmStore) {
      $confirmStore.resolve(false)
      confirmStore.set(null)
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') cancel()
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) cancel()
  }
</script>

<svelte:window on:keydown={handleKeydown} />

{#if $confirmStore !== null}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="dialog-backdrop" on:click={handleBackdropClick} role="dialog" aria-modal="true" aria-labelledby="dialog-title" tabindex="-1">
    <div class="dialog-card">
      <h3 class="dialog-title" id="dialog-title">{$confirmStore.title}</h3>
      <p class="dialog-message">{$confirmStore.message}</p>
      <div class="dialog-actions">
        <button class="btn btn-secondary" on:click={cancel}>{$confirmStore.cancelLabel}</button>
        <button class="btn btn-primary" on:click={confirm}>{$confirmStore.confirmLabel}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-backdrop {
    position: fixed;
    inset: 0;
    z-index: 10000;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    animation: backdrop-in 0.15s ease;
  }

  .dialog-card {
    background: var(--card-bg);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1.5rem;
    max-width: 420px;
    width: 100%;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
    animation: card-in 0.15s ease;
  }

  .dialog-title {
    margin: 0 0 0.75rem;
    font-size: 1rem;
    font-weight: 600;
    color: var(--text);
  }

  .dialog-message {
    margin: 0 0 1.25rem;
    font-size: 0.875rem;
    color: var(--text-secondary, #8b949e);
    line-height: 1.5;
  }

  .dialog-actions {
    display: flex;
    gap: 0.75rem;
    justify-content: flex-end;
  }

  @keyframes backdrop-in {
    from { opacity: 0; }
    to   { opacity: 1; }
  }

  @keyframes card-in {
    from { opacity: 0; transform: scale(0.96) translateY(-4px); }
    to   { opacity: 1; transform: scale(1) translateY(0); }
  }
</style>
