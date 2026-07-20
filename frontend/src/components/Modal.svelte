<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    isOpen = false,
    title = '',
    maxWidth = '520px',
    onclose,
    children
  } = $props<{
    isOpen: boolean;
    title: string;
    maxWidth?: string;
    onclose: () => void;
    children?: Snippet;
  }>();

  let modalElement: HTMLDivElement | null = $state(null);
  let previouslyFocusedElement: HTMLElement | null = null;

  $effect(() => {
    if (isOpen) {
      previouslyFocusedElement = document.activeElement as HTMLElement;
      setTimeout(() => {
        if (modalElement) {
          const focusables = getFocusableElements();
          if (focusables.length > 0) focusables[0].focus();
          else modalElement.focus();
        }
      }, 0);
    } else if (previouslyFocusedElement) {
      previouslyFocusedElement.focus();
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
  <div class="modal-backdrop" role="presentation" onclick={onclose}>
    <div
      class="modal-container"
      role="dialog"
      aria-modal="true"
      aria-labelledby="modal-title"
      bind:this={modalElement}
      onkeydown={handleKeydown}
      onclick={(e) => e.stopPropagation()}
      style="max-width: {maxWidth};"
      tabindex="-1"
    >
      <header class="modal-header">
        <h2 id="modal-title" class="modal-title">{title}</h2>
        <button class="modal-close-btn" onclick={onclose} aria-label="Close" title="Close">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
          </svg>
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
    inset: 0;
    background: rgba(0, 0, 0, 0.65);
    backdrop-filter: blur(2px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 24px;
  }
  .modal-container {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow:
      0 30px 60px -16px rgba(0, 0, 0, 0.7),
      0 0 0 1px rgba(255, 255, 255, 0.02) inset;
    width: 100%;
    max-width: 520px;
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
    padding: 14px 20px;
    border-bottom: 1px solid var(--border);
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.02), transparent);
  }
  .modal-title {
    font-size: 14px;
    font-weight: 700;
    letter-spacing: 0.02em;
    margin: 0;
    color: var(--fg-primary);
  }
  .modal-close-btn {
    background: transparent;
    border: 0;
    width: 28px;
    height: 28px;
    border-radius: var(--radius-sm);
    color: var(--fg-dim);
    cursor: pointer;
    display: grid;
    place-items: center;
    transition:
      background var(--transition-fast),
      color var(--transition-fast);
  }
  .modal-close-btn:hover {
    background: rgba(255, 255, 255, 0.05);
    color: var(--fg-primary);
  }
  .modal-content {
    padding: 20px;
    overflow-y: auto;
    color: var(--fg-primary);
    font-size: 13px;
  }
</style>
