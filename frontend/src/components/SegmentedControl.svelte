<script module lang="ts">
  import type { Component } from 'svelte';

  export interface SegmentItem {
    value: string;
    label: string;
    icon?: Component<{ size?: number }>;
    class?: string;
    /** data-testid кнопки сегмента. */
    testId?: string;
  }

  /**
   * Возвращает значение, которое станет активным после клика по сегменту.
   * Активность определяется значением, а не подписью — два сегмента с
   * одинаковым label и разными value остаются независимыми (см. тест
   * адъяцентности ниже). Вынесено на уровень модуля для юнит-тестирования
   * без DOM — в проекте нет jsdom/@testing-library (T-120-SC).
   */
  export function resolveSegmentValue(item: SegmentItem): string {
    return item.value;
  }
</script>

<script lang="ts">
  import { slidingIndicator } from '../lib/motion.svelte';

  interface Props {
    items: SegmentItem[];
    value: string;
    ariaLabel?: string;
    onchange?: (value: string) => void;
    /** Значения, показываемые в частично выбранном виде (aria-pressed="mixed"). */
    partial?: string[];
  }

  let { items, value = $bindable(), ariaLabel, onchange, partial = [] }: Props = $props();

  function pressedState(item: SegmentItem): boolean | 'mixed' {
    if (item.value === value) return true;
    return partial.includes(item.value) ? 'mixed' : false;
  }

  function handleClick(item: SegmentItem) {
    const next = resolveSegmentValue(item);
    if (next === value) return;
    value = next;
    onchange?.(next);
  }
</script>

<div class="seg" role="group" aria-label={ariaLabel} {@attach slidingIndicator('.seg-item.active')}>
  {#each items as item (item.value)}
    <button
      type="button"
      class="seg-item {item.class ?? ''}"
      class:active={item.value === value}
      class:partial={item.value !== value && partial.includes(item.value)}
      data-value={item.value}
      data-testid={item.testId}
      aria-pressed={pressedState(item)}
      onclick={() => handleClick(item)}
    >
      {#if item.icon}
        <item.icon size={14} />
      {/if}
      {item.label}
    </button>
  {/each}
</div>

<style>
  .seg {
    position: relative;
    isolation: isolate;
    display: inline-flex;
    border: 1px solid var(--border-control);
    border-radius: var(--radius-md);
    overflow-x: auto;
    flex-wrap: nowrap;
    padding: 2px;
    gap: 2px;
  }

  .seg-item {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: var(--btn-h);
    padding: 0 12px;
    background: transparent;
    border: 0;
    border-radius: var(--radius-md);
    font-family: inherit;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
    white-space: nowrap;
    cursor: pointer;
    transition:
      background var(--transition-fast),
      color var(--transition-fast);
  }

  .seg-item:hover:not(.active):not(.partial) {
    color: var(--fg-primary);
  }

  .seg-item.active {
    background: var(--accent);
    color: var(--btn-primary-text);
  }

  /* Подложка активного сегмента переезжает к выбранному. Пока позиция не измерена
     (первый кадр, скрытый контейнер), фон рисует сам сегмент. */
  .seg::before {
    content: '';
    position: absolute;
    z-index: -1;
    top: 0;
    left: 0;
    width: var(--ind-w, 0);
    height: var(--ind-h, 0);
    transform: translate(var(--ind-x, 0), var(--ind-y, 0));
    border-radius: var(--radius-md);
    background: var(--accent);
    opacity: 0;
    pointer-events: none;
  }

  .seg:global([data-indicator])::before {
    opacity: 1;
  }

  .seg:global([data-indicator='animate'])::before {
    transition:
      transform var(--duration-move) var(--ease-out),
      width var(--duration-move) var(--ease-out),
      height var(--duration-move) var(--ease-out);
  }

  .seg:global([data-indicator]) .seg-item.active {
    background: transparent;
  }

  .seg:global([data-indicator='animate']) .seg-item {
    transition:
      background var(--transition-fast),
      color var(--duration-move) var(--ease-out);
  }

  .seg-item.partial {
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    box-shadow: inset 0 0 0 1px var(--accent);
    color: var(--fg-primary);
  }
</style>
