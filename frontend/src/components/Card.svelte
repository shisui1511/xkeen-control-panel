<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    variant = 'default',
    title = '',
    actions,
    children
  } = $props<{
    variant?: 'default' | 'flat';
    title?: string;
    actions?: Snippet;
    children?: Snippet;
  }>();
</script>

<div class="card card-{variant}">
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
  .card-title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: calc(-1 * var(--card-pad)) calc(-1 * var(--card-pad)) 18px;
    padding: 16px 22px 12px;
    font-size: var(--font-size-xs);
    font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--fg-secondary);
    border-radius: var(--radius-lg) var(--radius-lg) 0 0;
  }
  :global([data-density='compact']) .card-title {
    padding: 10px 14px 8px;
    margin-bottom: 12px;
  }
  .card-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }
</style>
