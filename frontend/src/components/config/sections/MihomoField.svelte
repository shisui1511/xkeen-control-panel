<script module lang="ts">
  /** Идентификаторы подсказки и ошибки поля: aria-describedby берёт ошибку, а при её отсутствии подсказку. */
  export function describedBy(id: string, error: string, hint: string): string | undefined {
    if (error !== '') return `${id}-error`;
    if (hint !== '') return `${id}-hint`;
    return undefined;
  }
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';

  // Строка поля формы секции Mihomo (D-07): подпись слева и элемент справа на ≥769 px,
  // на ≤768 px одна колонка с подписью над полем. Элемент приходит содержимым и
  // связывается с подписью по id; ошибка (.error-text) и подсказка идут под ним.
  let {
    id,
    label,
    hint = '',
    error = '',
    group = false,
    children
  }: {
    id: string;
    label: string;
    hint?: string;
    error?: string;
    /** Поле-список из нескольких элементов: подпись названия группы, а не одного input. */
    group?: boolean;
    children: Snippet;
  } = $props();
</script>

<div class="mf">
  {#if group}
    <span class="mf-label" id="{id}-label">{label}</span>
  {:else}
    <label class="mf-label" for={id}>{label}</label>
  {/if}
  <div
    class="mf-ctrl"
    role={group ? 'group' : undefined}
    aria-labelledby={group ? `${id}-label` : undefined}
  >
    {@render children()}
    {#if error !== ''}
      <span class="error-text mf-msg" id="{id}-error">{error}</span>
    {/if}
    {#if hint !== ''}
      <span class="mf-hint mf-msg" id="{id}-hint">{hint}</span>
    {/if}
  </div>
</div>

<style>
  .mf {
    display: grid;
    grid-template-columns: 1.1fr 2fr;
    gap: var(--spacing-2) var(--spacing-4);
    align-items: start;
    min-width: 0;
    padding: var(--spacing-3) 0;
    border-bottom: 1px solid var(--border-light);
  }

  .mf:last-child {
    border-bottom: 0;
  }

  .mf-label {
    min-width: 0;
    padding-top: var(--spacing-2);
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .mf-ctrl {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .mf-ctrl :global(.input) {
    min-width: 0;
  }

  .mf-msg {
    margin-top: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .mf-hint {
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  @media (max-width: 768px) {
    .mf {
      grid-template-columns: minmax(0, 1fr);
    }

    .mf-label {
      padding-top: 0;
    }
  }
</style>
