<script module lang="ts">
  export interface TabItem {
    value: string;
    label: string;
    disabled?: boolean;
  }

  /**
   * Вычисляет значение, которое станет активным после клика по вкладке.
   * Отключённая вкладка не меняет текущее значение. Вынесено на уровень
   * модуля, чтобы логику выбора можно было юнит-тестировать напрямую —
   * в проекте нет jsdom/@testing-library (T-120-SC запрещает новые
   * зависимости), поэтому клик по кнопке через DOM не воспроизводим.
   */
  export function resolveTabValue(item: TabItem, currentValue: string): string {
    return item.disabled ? currentValue : item.value;
  }
</script>

<script lang="ts">
  interface Props {
    items: TabItem[];
    value: string;
    ariaLabel?: string;
    onchange?: (value: string) => void;
  }

  let { items, value = $bindable(), ariaLabel, onchange }: Props = $props();

  function handleClick(item: TabItem) {
    const next = resolveTabValue(item, value);
    if (next === value) return;
    value = next;
    onchange?.(next);
  }
</script>

{#if items.length > 0}
  <div class="tabs" role="tablist" aria-label={ariaLabel}>
    {#each items as item (item.value)}
      <button
        type="button"
        class="tab-btn"
        class:active={item.value === value}
        disabled={item.disabled}
        onclick={() => handleClick(item)}
      >
        {item.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .tabs {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    flex-wrap: nowrap;
  }

  .tab-btn {
    flex-shrink: 0;
    background: transparent;
    border: 0;
    padding: 11px 16px;
    font-family: inherit;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
    cursor: pointer;
    white-space: nowrap;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    transition:
      color var(--transition-fast),
      border-color var(--transition-fast);
  }

  .tab-btn:hover:not(:disabled) {
    color: var(--fg-primary);
  }

  .tab-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .tab-btn.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }
</style>
