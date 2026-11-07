<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    variant = 'default',
    title = '',
    flush = false,
    actions,
    children
  }: {
    variant?: 'default' | 'flat';
    title?: string;
    /** Без внутренних отступов: содержимое само раскладывает ячейки (строки, полосы метрик). */
    flush?: boolean;
    actions?: Snippet;
    children?: Snippet;
  } = $props();
</script>

<div class="card card-{variant}" class:card-flush={flush}>
  {#if title}
    <h2 class="card-title">
      <span>{title}</span>
      {#if actions}
        <div class="card-actions">
          {@render actions()}
        </div>
      {/if}
    </h2>
  {/if}
  {@render children?.()}
</div>

<style>
  /* Note: global.css also defines .card / .card-title for non-component
     markup (e.g. server-rendered fragments). These scoped rules mirror
     those values but win here because Svelte's scope attribute makes them
     more specific — keep both copies in sync manually when changing either. */
  .card {
    background:
      linear-gradient(180deg, rgba(255, 255, 255, 0.012), transparent 60%), var(--bg-card);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-lg);
    padding: var(--card-pad);
    box-shadow: var(--shadow);
  }
  .card-flat {
    box-shadow: none;
  }
  .card-flush {
    padding: 0;
    overflow: hidden;
  }
  .card-title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: calc(-1 * var(--card-pad)) calc(-1 * var(--card-pad)) 18px;
    padding: var(--spacing-4) 22px var(--spacing-3);
    font-size: var(--font-size-xs);
    font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--fg-secondary);
    border-radius: var(--radius-lg) var(--radius-lg) 0 0;
  }
  :global([data-density='compact']) .card-title {
    padding: var(--spacing-2-5) var(--spacing-3-5) var(--spacing-2);
    margin-bottom: var(--spacing-3);
  }
  .card-actions {
    display: flex;
    gap: var(--spacing-2);
    align-items: center;
  }
</style>
