<script lang="ts">
  import type { Snippet } from 'svelte';

  // Секция дашборда: заголовок стоит над поверхностью, а не внутри неё.
  // Так группа читается по заголовку и отступу, без рамки вокруг рамок.
  let {
    title,
    meta,
    children
  }: {
    title: string;
    /** Правая часть шапки: статус, бейдж или тихая ссылка-действие. */
    meta?: Snippet;
    children?: Snippet;
  } = $props();

  const uid = $props.id();
</script>

<section class="dash-section" aria-labelledby="{uid}-title">
  <header class="dash-section-head">
    <h2 id="{uid}-title" class="dash-section-title">{title}</h2>
    {#if meta}
      <div class="dash-section-meta">{@render meta()}</div>
    {/if}
  </header>
  {@render children?.()}
</section>

<style>
  .dash-section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .dash-section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-3);
    min-height: 28px;
  }

  .dash-section-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.3;
    letter-spacing: -0.01em;
    color: var(--fg-primary);
  }

  .dash-section-meta {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  /* На узких экранах глобальный .card получает нижний отступ — внутри секции его даёт gap. */
  .dash-section :global(.card) {
    margin-bottom: 0;
  }
</style>
