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
  /* All visual rules now live in global.css under .card / .card-title so
     dark-navy redesign + theme variables apply consistently. This file
     stays minimal so component scoping does not strand the new tokens. */
  .card {
    background:
      linear-gradient(180deg, rgba(255, 255, 255, 0.012), transparent 60%), var(--bg-card);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: var(--radius-lg);
    padding: 24px;
    box-shadow: var(--shadow);
  }
  .card-flat {
    box-shadow: none;
  }
  .card-title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: -24px -24px 18px;
    padding: 16px 22px 12px;
    font-size: 11.5px;
    font-weight: 700;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--fg-secondary);
    border-radius: var(--radius-lg) var(--radius-lg) 0 0;
  }
  .card-actions {
    display: flex;
    gap: 8px;
    align-items: center;
    text-transform: none;
    letter-spacing: normal;
  }
</style>
