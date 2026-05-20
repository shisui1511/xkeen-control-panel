<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    variant = 'default',
    title = '',
    children
  } = $props<{
    variant?: 'default' | 'flat';
    title?: string;
    children?: Snippet;
  }>();
</script>

<div class="card card-{variant}">
  {#if title}
    <h2 class="card-title">{title}</h2>
  {/if}
  {@render children?.()}
</div>

<style>
  .card {
    background-color: var(--color-bg-surface);
    border: 1px solid var(--color-border-subtle);
    border-radius: var(--radius-md);
    padding: var(--spacing-6);
  }

  .card-default {
    box-shadow: var(--shadow-sm);
  }

  .card-flat {
    box-shadow: none;
  }

  .card-title {
    font-family: var(--font-family-sans);
    font-size: var(--font-size-base);
    font-weight: 600;
    margin-top: 0;
    margin-bottom: var(--spacing-4);
    color: var(--color-text-primary);
  }
</style>
