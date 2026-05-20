<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    isOpen = false,
    title = '',
    onclose,
    children
  } = $props<{
    isOpen: boolean;
    title: string;
    onclose: () => void;
    children?: Snippet;
  }>();

  let modalElement: HTMLDivElement | null = $state(null);
  let previouslyFocusedElement: HTMLElement | null = null;

  $effect(() => {
    if (isOpen) {
      previouslyFocusedElement = document.activeElement as HTMLElement;
      // Focus first focusable element after render
      setTimeout(() => {
        if (modalElement) {
          const focusables = getFocusableElements();
          if (focusables.length > 0) {
            focusables[0].focus();
          } else {
            modalElement.focus();
          }
        }
      }, 0);
    } else {
      if (previouslyFocusedElement) {
        previouslyFocusedElement.focus();
      }
    }
  });

  function getFocusableElements(): HTMLElement[] {
    if (!modalElement) return [];
    const selectors = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';
    return Array.from(modalElement.querySelectorAll(selectors)) as HTMLElement[];
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      onclose();
      return;
    }

    if (event.key === 'Tab') {
      const focusables = getFocusableElements();
      if (focusables.length === 0) {
        event.preventDefault();
        return;
      }

      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement;

      if (event.shiftKey) {
        if (active === first) {
          last.focus();
          event.preventDefault();
        }
      } else {
        if (active === last) {
          first.focus();
          event.preventDefault();
        }
      }
    }
  }
</script>

{#if isOpen}
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    class="modal-backdrop"
    role="presentation"
    onclick={onclose}
  >
    <div
      class="modal-container"
      role="dialog"
      aria-modal="true"
      aria-labelledby="modal-title"
      bind:this={modalElement}
      onkeydown={handleKeydown}
      onclick={(e) => e.stopPropagation()}
      tabindex="-1"
    >
      <header class="modal-header">
        <h2 id="modal-title" class="modal-title">{title}</h2>
        <button
          class="modal-close-btn"
          onclick={onclose}
          aria-label="Close dialog"
          title="Close dialog"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </header>
      <div class="modal-content">
        {@render children?.()}
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    top: 0;
    left: 0;
    width: 100vw;
    height: 100vh;
    background-color: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal-container {
    background-color: var(--color-bg-surface);
    border: 1px solid var(--color-border-subtle);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-md);
    width: 90%;
    max-width: 500px;
    max-height: 90vh;
    display: flex;
    flex-direction: column;
    outline: none;
    overflow: hidden;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--spacing-4) var(--spacing-6);
    border-bottom: 1px solid var(--color-border-subtle);
  }

  .modal-title {
    font-family: var(--font-family-sans);
    font-size: var(--font-size-lg);
    font-weight: 600;
    margin: 0;
    color: var(--color-text-primary);
  }

  .modal-close-btn {
    background: none;
    border: none;
    font-size: 1.25rem;
    cursor: pointer;
    color: var(--color-text-secondary);
    padding: var(--spacing-2);
    border-radius: var(--radius-sm);
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background-color var(--transition-fast);
  }

  .modal-close-btn:hover {
    background-color: var(--hover);
    color: var(--color-text-primary);
  }

  .modal-content {
    padding: var(--spacing-6);
    overflow-y: auto;
    color: var(--color-text-primary);
    font-family: var(--font-family-sans);
    font-size: var(--font-size-sm);
  }
</style>
