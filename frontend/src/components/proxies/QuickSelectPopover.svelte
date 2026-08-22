<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { t } from '../../i18n';
  import { getCountryFlag } from '../../lib/countryFlags';
  import type { LatencyBucket } from '../../lib/proxyClassification';

  export interface QuickSelectNode {
    name: string;
    isGroup: boolean;
    bucket: LatencyBucket;
    delay?: number;
    latencyText: string;
    latencyClass: string;
  }

  interface Props {
    groupName: string;
    groupType: string;
    nodes: QuickSelectNode[];
    currentNode: string;
    anchorEl: HTMLElement | null;
    onSelect: (nodeName: string) => void;
    onClose: () => void;
  }

  let {
    groupName,
    groupType,
    nodes = [],
    currentNode,
    anchorEl,
    onSelect,
    onClose
  }: Props = $props();

  let popoverEl: HTMLElement | null = $state(null);
  let listEl: HTMLElement | null = $state(null);
  let searchEl: HTMLInputElement | null = $state(null);
  let query = $state('');
  let highlightIndex = $state(0);
  let top = $state(0);
  let left = $state(0);
  let isBottomSheet = $state(false);

  let touchStartY = $state(0);
  let isDragging = $state(false);
  let dragTranslateY = $state(0);

  function handleTouchStart(e: TouchEvent) {
    if (!isBottomSheet || e.touches.length !== 1) return;
    const target = e.target as HTMLElement | null;
    if (target?.tagName === 'INPUT') return;

    touchStartY = e.touches[0].clientY;
    isDragging = true;
    dragTranslateY = 0;
  }

  function handleTouchMove(e: TouchEvent) {
    if (!isDragging || !isBottomSheet || e.touches.length !== 1) return;
    const currentY = e.touches[0].clientY;
    const deltaY = currentY - touchStartY;

    if (deltaY > 0) {
      dragTranslateY = deltaY;
      if (e.cancelable) {
        e.preventDefault();
      }
    } else {
      dragTranslateY = 0;
    }
  }

  function handleTouchEnd() {
    if (!isDragging || !isBottomSheet) return;
    isDragging = false;
    const threshold = 60;
    if (dragTranslateY >= threshold) {
      onClose();
    }
    dragTranslateY = 0;
  }

  let isSelector = $derived(groupType.toLowerCase() === 'selector');
  let isAuto = $derived(!isSelector);

  // Sorting (D-14):
  // 1. currentNode
  // 2. fast & mid by delay ascending
  // 3. unchecked alphabetically
  // 4. bad alphabetically
  let sortedNodes = $derived.by(() => {
    let list = [...nodes];
    if (query.trim() !== '') {
      const q = query.trim().toLowerCase();
      list = list.filter((n) => n.name.toLowerCase().includes(q));
    }

    list.sort((a, b) => {
      const aCurrent = a.name === currentNode;
      const bCurrent = b.name === currentNode;
      if (aCurrent && !bCurrent) return -1;
      if (!aCurrent && bCurrent) return 1;

      const aIsFastOrMid = a.bucket === 'fast' || a.bucket === 'mid';
      const bIsFastOrMid = b.bucket === 'fast' || b.bucket === 'mid';

      if (aIsFastOrMid && bIsFastOrMid) {
        const delayA = a.delay ?? Infinity;
        const delayB = b.delay ?? Infinity;
        if (delayA !== delayB) return delayA - delayB;
        return a.name.localeCompare(b.name);
      }
      if (aIsFastOrMid) return -1;
      if (bIsFastOrMid) return 1;

      if (a.bucket === 'unchecked' && b.bucket === 'unchecked') {
        return a.name.localeCompare(b.name);
      }
      if (a.bucket === 'unchecked') return -1;
      if (b.bucket === 'unchecked') return 1;

      if (a.bucket === 'bad' && b.bucket === 'bad') {
        return a.name.localeCompare(b.name);
      }
      if (a.bucket === 'bad') return 1;
      if (b.bucket === 'bad') return -1;

      return a.name.localeCompare(b.name);
    });

    return list;
  });

  $effect(() => {
    // Reset highlight on query change
    query;
    highlightIndex = 0;
  });

  let activeOptionId = $derived(
    sortedNodes.length > 0 && highlightIndex >= 0 && highlightIndex < sortedNodes.length
      ? `qs-opt-${highlightIndex}`
      : undefined
  );

  function updatePosition() {
    if (!anchorEl || !popoverEl || isBottomSheet) return;
    const anchorRect = anchorEl.getBoundingClientRect();
    const popoverRect = popoverEl.getBoundingClientRect();

    let newTop = anchorRect.bottom + 8;
    let newLeft = anchorRect.left + anchorRect.width / 2 - popoverRect.width / 2;

    const padding = 12;
    if (newLeft < padding) {
      newLeft = padding;
    } else if (newLeft + popoverRect.width > window.innerWidth - padding) {
      newLeft = window.innerWidth - popoverRect.width - padding;
    }

    if (newTop + popoverRect.height > window.innerHeight - padding) {
      newTop = anchorRect.top - popoverRect.height - 8;
      if (newTop < padding) {
        newTop = padding;
      }
    }

    top = newTop;
    left = newLeft;
  }

  function scrollToHighlighted() {
    if (!listEl) return;
    const itemEl = listEl.querySelector(`#qs-opt-${highlightIndex}`) as HTMLElement | null;
    if (itemEl) {
      itemEl.scrollIntoView({ block: 'nearest' });
    }
  }

  function handlePointerDown(e: PointerEvent) {
    const target = e.target as Node | null;
    if (popoverEl && !popoverEl.contains(target) && anchorEl && !anchorEl.contains(target)) {
      onClose();
    }
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
      return;
    }

    if (sortedNodes.length === 0) return;

    if (e.key === 'ArrowDown') {
      e.preventDefault();
      highlightIndex = (highlightIndex + 1) % sortedNodes.length;
      scrollToHighlighted();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      highlightIndex = (highlightIndex - 1 + sortedNodes.length) % sortedNodes.length;
      scrollToHighlighted();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (isSelector && sortedNodes[highlightIndex]) {
        onSelect(sortedNodes[highlightIndex].name);
      }
    }
  }

  let mql: MediaQueryList | null = null;
  function handleMqlChange(e: MediaQueryListEvent) {
    isBottomSheet = e.matches;
    if (!isBottomSheet) {
      updatePosition();
    }
  }

  onMount(async () => {
    if (typeof window !== 'undefined') {
      mql = window.matchMedia('(max-width: 768px)');
      isBottomSheet = mql.matches;
      mql.addEventListener('change', handleMqlChange);

      window.addEventListener('resize', updatePosition);
      window.addEventListener('scroll', updatePosition, { capture: true, passive: true });
      window.addEventListener('pointerdown', handlePointerDown, { capture: true });
      window.addEventListener('keydown', handleKeyDown);
    }

    await tick();
    searchEl?.focus();
    updatePosition();
  });

  onDestroy(() => {
    if (typeof window !== 'undefined') {
      if (mql) mql.removeEventListener('change', handleMqlChange);
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, { capture: true });
      window.removeEventListener('pointerdown', handlePointerDown, { capture: true });
      window.removeEventListener('keydown', handleKeyDown);
    }
  });

  $effect(() => {
    if (popoverEl && anchorEl && !isBottomSheet) {
      updatePosition();
    }
  });
</script>

<div
  bind:this={popoverEl}
  class="qs-popover"
  class:qs-bottom-sheet={isBottomSheet}
  style={isBottomSheet
    ? `transform: translateY(${dragTranslateY}px); transition: ${isDragging ? 'none' : 'transform 0.2s cubic-bezier(0.16, 1, 0.3, 1)'};`
    : `top: ${top}px; left: ${left}px;`}
  role="dialog"
  aria-modal="false"
  aria-label={$t('proxies.quick_select_title')}
  tabindex="-1"
>
  {#if isBottomSheet}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="qs-drag-handle"
      aria-hidden="true"
      ontouchstart={handleTouchStart}
      ontouchmove={handleTouchMove}
      ontouchend={handleTouchEnd}
      ontouchcancel={handleTouchEnd}
    >
      <div class="qs-drag-bar"></div>
    </div>
  {/if}

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="qs-header"
    ontouchstart={handleTouchStart}
    ontouchmove={handleTouchMove}
    ontouchend={handleTouchEnd}
    ontouchcancel={handleTouchEnd}
  >
    <input
      bind:this={searchEl}
      type="search"
      class="qs-search"
      placeholder={$t('proxies.quick_select_search')}
      bind:value={query}
    />
  </div>

  {#if isAuto}
    <div class="qs-auto-note">
      {$t('proxies.quick_select_auto_note')}
    </div>
  {/if}

  <div
    bind:this={listEl}
    class="qs-list"
    role="listbox"
    tabindex="0"
    aria-label={groupName}
    aria-activedescendant={activeOptionId}
  >
    {#if sortedNodes.length === 0}
      <div class="qs-empty">
        {$t('proxies.quick_select_no_matches')}
      </div>
    {:else}
      {#each sortedNodes as node, index (node.name)}
        {@const isSelected = node.name === currentNode}
        {@const isHighlighted = index === highlightIndex}
        {@const flag = getCountryFlag(node.name)}
        <button
          type="button"
          id={`qs-opt-${index}`}
          class="qs-item"
          class:qs-item-active={isHighlighted}
          class:qs-item-dim={node.bucket === 'bad'}
          role="option"
          tabindex="-1"
          aria-selected={isSelected}
          aria-disabled={!isSelector}
          onclick={() => {
            if (isSelector) {
              onSelect(node.name);
            }
          }}
          onmouseenter={() => (highlightIndex = index)}
        >
          {#if flag}
            <span class="qs-flag" aria-hidden="true">{flag}</span>
          {/if}
          <span class="qs-name" title={node.name}>{node.name}</span>
          <span class="lat {node.latencyClass}">{node.latencyText}</span>
          {#if isSelected}
            <span class="qs-check" aria-hidden="true">✓</span>
          {/if}
        </button>
      {/each}
    {/if}
  </div>
</div>

<style>
  .qs-popover {
    position: fixed;
    z-index: 1100;
    width: 280px;
    max-height: 320px;
    background: var(--bg-elevated, var(--bg-card, #1e293b));
    border: 1px solid var(--border, #334155);
    border-radius: var(--radius-lg, 8px);
    box-shadow: var(--shadow-md, 0 4px 6px -1px rgba(0, 0, 0, 0.3));
    display: flex;
    flex-direction: column;
    overflow: hidden;
    color: var(--fg-primary);
  }

  .qs-popover.qs-bottom-sheet {
    left: 0 !important;
    right: 0 !important;
    bottom: 0 !important;
    top: auto !important;
    width: 100% !important;
    max-height: 60vh !important;
    border-radius: var(--radius-lg, 10px) var(--radius-lg, 10px) 0 0 !important;
    border-left: 0;
    border-right: 0;
    border-bottom: 0;
  }

  .qs-drag-handle {
    width: 100%;
    padding: 8px 0 2px;
    display: flex;
    justify-content: center;
    align-items: center;
    cursor: grab;
    touch-action: none;
    background: var(--bg-secondary, rgba(255, 255, 255, 0.02));
  }

  .qs-drag-bar {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--border-strong, var(--border, #475569));
  }

  .qs-header {
    padding: 8px 10px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-secondary, rgba(255, 255, 255, 0.02));
  }

  .qs-search {
    width: 100%;
    padding: 6px 10px;
    font-size: 13px;
    border-radius: var(--radius-md, 6px);
    border: 1px solid var(--border);
    background: var(--bg-input, rgba(0, 0, 0, 0.2));
    color: var(--fg-primary);
    outline: none;
    box-sizing: border-box;
  }

  .qs-search:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  .qs-auto-note {
    padding: 6px 10px;
    font-size: 11px;
    color: var(--warning, #eab308);
    background: rgba(234, 179, 8, 0.08);
    border-bottom: 1px solid var(--border);
    line-height: 1.3;
  }

  .qs-list {
    flex: 1;
    overflow-y: auto;
    scrollbar-width: thin;
    padding: 4px 0;
    max-height: 260px;
  }

  .qs-list::-webkit-scrollbar {
    width: 6px;
  }

  .qs-list::-webkit-scrollbar-thumb {
    background: var(--scrollbar-thumb, rgba(255, 255, 255, 0.15));
    border-radius: 3px;
  }

  .qs-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    min-height: 32px;
    cursor: pointer;
    font-size: 13px;
    transition: background 0.15s ease;
    user-select: none;
    background: none;
    border: none;
    text-align: left;
    font: inherit;
    width: 100%;
    color: inherit;
  }

  @media (max-width: 768px) {
    .qs-item {
      min-height: 44px;
      padding: 8px 12px;
    }
  }

  .qs-item:hover,
  .qs-item.qs-item-active {
    background: var(--hover, rgba(255, 255, 255, 0.08));
  }

  .qs-item[aria-selected='true'] {
    color: var(--accent, #3b82f6);
    font-weight: 600;
  }

  .qs-item[aria-disabled='true'] {
    cursor: default;
  }

  .qs-item.qs-item-dim {
    opacity: 0.6;
  }

  .qs-flag {
    font-size: 14px;
    line-height: 1;
    flex-shrink: 0;
  }

  .qs-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .qs-check {
    color: var(--success, #22c55e);
    font-weight: bold;
    font-size: 14px;
    margin-left: 4px;
    flex-shrink: 0;
  }

  .qs-empty {
    padding: 16px 12px;
    text-align: center;
    font-size: 12px;
    color: var(--fg-muted, #64748b);
  }

  .lat {
    font-family: var(--font-family-mono);
    font-size: 11px;
    padding: 2px 4px;
    border-radius: 3px;
    flex-shrink: 0;
  }

  .lat.ok {
    color: var(--success, #22c55e);
    background: rgba(34, 197, 94, 0.1);
  }

  .lat.mid {
    color: var(--warning, #eab308);
    background: rgba(234, 179, 8, 0.1);
  }

  .lat.bad {
    color: var(--danger, #ef4444);
    background: rgba(239, 68, 68, 0.1);
  }

  .lat.dim {
    color: var(--fg-dim, #64748b);
    background: rgba(100, 116, 139, 0.1);
  }
</style>
