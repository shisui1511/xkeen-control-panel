<script module lang="ts">
  import type { Component } from 'svelte';

  export interface SegmentItem {
    value: string;
    label: string;
    icon?: Component<{ size?: number }>;
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
  interface Props {
    items: SegmentItem[];
    value: string;
    ariaLabel?: string;
    onchange?: (value: string) => void;
  }

  let { items, value = $bindable(), ariaLabel, onchange }: Props = $props();

  function handleClick(item: SegmentItem) {
    const next = resolveSegmentValue(item);
    if (next === value) return;
    value = next;
    onchange?.(next);
  }
</script>

<div class="seg" role="group" aria-label={ariaLabel}>
  {#each items as item (item.value)}
    <button
      type="button"
      class="seg-item"
      class:active={item.value === value}
      data-value={item.value}
      aria-pressed={item.value === value}
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
    display: inline-flex;
    border: 1px solid var(--border);
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

  .seg-item:hover:not(.active) {
    color: var(--fg-primary);
  }

  .seg-item.active {
    background: var(--accent);
    color: var(--btn-primary-text);
  }
</style>
