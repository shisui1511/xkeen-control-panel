<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    type = 'button',
    variant = 'primary',
    disabled = false,
    loading = false,
    title,
    onclick,
    children
  } = $props<{
    type?: 'button' | 'submit' | 'reset';
    variant?: 'primary' | 'secondary' | 'danger';
    disabled?: boolean;
    loading?: boolean;
    title?: string;
    onclick?: (event: MouseEvent) => void;
    children?: Snippet;
  }>();
</script>

<button {type} class="btn btn-{variant}" disabled={disabled || loading} {title} {onclick}>
  {#if loading}
    <span class="spinner" aria-hidden="true"></span>
    <span class="sr-only">Loading...</span>
  {/if}
  {@render children?.()}
</button>

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 9px 14px;
    border-radius: var(--radius-md);
    font-family: var(--font-family-sans);
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition:
      background-color var(--transition-fast),
      border-color var(--transition-fast),
      color var(--transition-fast),
      filter var(--transition-fast),
      box-shadow var(--transition-fast),
      opacity var(--transition-fast);
    border: 1px solid transparent;
    outline: none;
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-primary {
    background: linear-gradient(180deg, var(--accent), var(--accent-2));
    color: var(--btn-primary-text, #03182a);
    box-shadow: 0 6px 18px -8px var(--accent);
  }
  .btn-primary:hover:not(:disabled) {
    filter: brightness(1.07);
    color: var(--btn-primary-text, #03182a);
  }

  .btn-secondary {
    background: transparent;
    border-color: var(--border);
    color: var(--fg-primary);
  }
  .btn-secondary:hover:not(:disabled) {
    border-color: var(--accent-line);
    color: var(--accent);
    background: var(--hover);
  }

  .btn-danger {
    background: var(--danger);
    color: #fff;
  }
  .btn-danger:hover:not(:disabled) {
    opacity: 0.92;
    color: #fff;
  }

  .spinner {
    width: 13px;
    height: 13px;
    border: 2px solid currentColor;
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
