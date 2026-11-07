<script lang="ts">
  import type { Snippet } from 'svelte';

  // Секция страницы: заголовок стоит над поверхностью, а не внутри неё.
  // Так группа читается по заголовку и отступу, без рамки вокруг рамок.
  // Внутри — одна карточка (обычно Card flush или .card.card-rows) или
  // несколько карточек одного уровня, но не карточка с карточками.
  let {
    title,
    description = '',
    meta,
    children,
    testid
  }: {
    title: string;
    /** Пояснение под заголовком — одна строка вторичного текста. */
    description?: string;
    /** Правая часть шапки: статус, бейдж или тихая ссылка-действие. */
    meta?: Snippet;
    children?: Snippet;
    testid?: string;
  } = $props();

  const uid = $props.id();
</script>

<section class="page-section" aria-labelledby="{uid}-title" data-testid={testid}>
  <header class="page-section-head">
    <div class="page-section-heading">
      <h2 id="{uid}-title" class="page-section-title">{title}</h2>
      {#if description}
        <p class="page-section-desc">{description}</p>
      {/if}
    </div>
    {#if meta}
      <div class="page-section-meta">{@render meta()}</div>
    {/if}
  </header>
  {@render children?.()}
</section>

<style>
  .page-section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .page-section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-3);
    min-height: 28px;
  }

  .page-section-heading {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-0-5);
    min-width: 0;
  }

  .page-section-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.3;
    letter-spacing: -0.01em;
    color: var(--fg-primary);
  }

  .page-section-desc {
    margin: 0;
    font-size: var(--font-size-sm);
    line-height: 1.45;
    color: var(--fg-secondary);
  }

  .page-section-meta {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    flex-shrink: 0;
    min-width: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  /* На узких экранах глобальный .card получает нижний отступ — внутри секции его даёт gap. */
  .page-section :global(.card) {
    margin-bottom: 0;
  }
</style>
