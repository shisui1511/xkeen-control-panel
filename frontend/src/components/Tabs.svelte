<script module lang="ts">
  export interface TabItem {
    value: string;
    label: string;
    disabled?: boolean;
    testId?: string;
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
  import { slidingIndicator } from '../lib/motion.svelte';

  interface Props {
    items: TabItem[];
    value: string;
    variant?: 'line' | 'pill';
    ariaLabel?: string;
    onchange?: (value: string) => void;
  }

  let { items, value = $bindable(), variant = 'line', ariaLabel, onchange }: Props = $props();

  // На телефоне вкладки прокручиваются вбок: выбранная (в том числе открытая по адресу)
  // докручивается в видимую часть полосы, а не остаётся за краем.
  function revealActive(node: HTMLElement) {
    void value;
    const active = node.querySelector<HTMLElement>('.tab-btn.active');
    if (!active || node.scrollWidth <= node.clientWidth) return;
    const left = active.offsetLeft - node.offsetLeft;
    const right = left + active.offsetWidth;
    if (left < node.scrollLeft + 24) {
      node.scrollTo({ left: Math.max(0, left - 24), behavior: 'smooth' });
    } else if (right > node.scrollLeft + node.clientWidth - 24) {
      node.scrollTo({ left: right - node.clientWidth + 24, behavior: 'smooth' });
    }
  }

  function handleClick(item: TabItem) {
    const next = resolveTabValue(item, value);
    if (next === value) return;
    value = next;
    onchange?.(next);
  }
</script>

{#if items.length > 0}
  <div
    class="tabs"
    class:tabs-pill={variant === 'pill'}
    role="tablist"
    aria-label={ariaLabel}
    {@attach slidingIndicator('.tab-btn.active')}
    {@attach revealActive}
  >
    {#each items as item (item.value)}
      <button
        type="button"
        role="tab"
        aria-selected={item.value === value}
        class="tab-btn"
        class:active={item.value === value}
        disabled={item.disabled}
        data-testid={item.testId}
        onclick={() => handleClick(item)}
      >
        {item.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .tabs {
    position: relative;
    isolation: isolate;
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    overflow-y: hidden;
    flex-wrap: nowrap;
    scrollbar-width: none;
    -ms-overflow-style: none;
  }

  .tabs::-webkit-scrollbar {
    display: none;
  }

  /* Края прокручиваемой полосы растворяются, пока за ними есть вкладки: видно, что
     полоса листается. Без прокрутки шкала неактивна и маски нет. */
  @supports (animation-timeline: scroll()) {
    .tabs:not(.tabs-pill) {
      mask-image: linear-gradient(
        to right,
        transparent 0,
        black var(--tabs-fade-l),
        black calc(100% - var(--tabs-fade-r)),
        transparent 100%
      );
      animation: tabs-edge-fade linear both;
      animation-timeline: scroll(self inline);
    }
  }

  @keyframes tabs-edge-fade {
    0% {
      --tabs-fade-l: 0px;
      --tabs-fade-r: 32px;
    }
    8%,
    92% {
      --tabs-fade-l: 32px;
      --tabs-fade-r: 32px;
    }
    100% {
      --tabs-fade-l: 32px;
      --tabs-fade-r: 0px;
    }
  }

  .tabs-pill {
    border-bottom: none;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 3px;
    gap: 3px;
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
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
      border-color var(--transition-fast),
      background var(--transition-fast);
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

  .tabs-pill .tab-btn {
    border-bottom: none;
    margin-bottom: 0;
    border-radius: var(--radius-sm);
    padding: 4px 12px;
    height: 28px;
    display: inline-flex;
    align-items: center;
    font-size: var(--font-size-xs);
  }

  .tabs-pill .tab-btn:hover:not(:disabled):not(.active) {
    background: var(--hover);
  }

  .tabs-pill .tab-btn.active {
    background: var(--accent);
    color: var(--btn-primary-text);
    border-bottom: none;
    box-shadow: var(--shadow-sm);
  }

  /* Индикатор активной вкладки переезжает к выбранной: черта под вкладкой
     в варианте line, подложка в варианте pill. Пока позиция не измерена,
     активную вкладку рисуют её собственные border и фон. */
  .tabs::after {
    content: '';
    position: absolute;
    z-index: -1;
    left: 0;
    bottom: 0;
    width: var(--ind-w, 0);
    height: 2px;
    transform: translateX(var(--ind-x, 0));
    background: var(--accent);
    opacity: 0;
    pointer-events: none;
  }

  .tabs-pill::after {
    top: 0;
    bottom: auto;
    height: var(--ind-h, 0);
    transform: translate(var(--ind-x, 0), var(--ind-y, 0));
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-sm);
  }

  .tabs:global([data-indicator])::after {
    opacity: 1;
  }

  .tabs:global([data-indicator='animate'])::after {
    transition:
      transform var(--duration-move) var(--ease-out),
      width var(--duration-move) var(--ease-out),
      height var(--duration-move) var(--ease-out);
  }

  .tabs:global([data-indicator]) .tab-btn.active {
    border-bottom-color: transparent;
  }

  .tabs-pill:global([data-indicator]) .tab-btn.active {
    background: transparent;
    box-shadow: none;
  }
</style>
