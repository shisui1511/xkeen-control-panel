<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../i18n';
  import { formatTimeAgo } from '../lib/batchLatencyTester';

  interface HistoryItem {
    time: string;
    delay: number;
  }

  interface Props {
    proxyName: string;
    history: HistoryItem[];
    anchorEl: HTMLElement | null;
    onClose: () => void;
  }

  let { proxyName, history = [], anchorEl, onClose }: Props = $props();

  let popoverEl: HTMLElement | null = $state(null);
  let top = $state(0);
  let left = $state(0);

  let recentHistory = $derived(history.slice(-10));

  function updatePosition() {
    if (!anchorEl || !popoverEl) return;
    const anchorRect = anchorEl.getBoundingClientRect();
    const popoverRect = popoverEl.getBoundingClientRect();

    let newTop = anchorRect.bottom + 8;
    let newLeft = anchorRect.left + anchorRect.width / 2 - popoverRect.width / 2;

    // Viewport overflow bounds checks
    const padding = 12;
    if (newLeft < padding) {
      newLeft = padding;
    } else if (newLeft + popoverRect.width > window.innerWidth - padding) {
      newLeft = window.innerWidth - popoverRect.width - padding;
    }

    if (newTop + popoverRect.height > window.innerHeight - padding) {
      // Show above anchor if bottom overflows
      newTop = anchorRect.top - popoverRect.height - 8;
      if (newTop < padding) {
        newTop = padding;
      }
    }

    top = newTop;
    left = newLeft;
  }

  function handlePointerDown(e: PointerEvent) {
    const target = e.target as Node | null;
    if (popoverEl && !popoverEl.contains(target) && anchorEl && !anchorEl.contains(target)) {
      onClose();
    }
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      onClose();
    }
  }

  $effect(() => {
    if (popoverEl && anchorEl) {
      updatePosition();
    }
  });

  onMount(() => {
    window.addEventListener('pointerdown', handlePointerDown, true);
    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);
    // Initial position
    setTimeout(updatePosition, 0);
  });

  onDestroy(() => {
    if (typeof window !== 'undefined') {
      window.removeEventListener('pointerdown', handlePointerDown, true);
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    }
  });

  function getBarHeight(delay: number): number {
    if (delay <= 0) return 100;
    return Math.max(15, Math.min(100, Math.round((delay / 800) * 100)));
  }

  function getBarColor(delay: number): string {
    if (delay <= 0) return 'var(--danger)';
    if (delay < 150) return 'var(--success)';
    if (delay <= 400) return 'var(--warning)';
    return 'var(--danger)';
  }

  function getLatencyBadgeClass(delay: number): string {
    if (delay <= 0) return 'lat bad';
    if (delay < 150) return 'lat ok';
    if (delay <= 400) return 'lat mid';
    return 'lat bad';
  }
</script>

<div
  bind:this={popoverEl}
  class="latency-history-popover"
  style="top: {top}px; left: {left}px;"
  role="dialog"
  aria-modal="false"
  tabindex="-1"
>
  <div class="popover-header">
    <div class="popover-title" title={proxyName}>
      {$t('proxies.history_title', { nodeName: proxyName })}
    </div>
    <button type="button" class="popover-close-btn" onclick={onClose} aria-label={$t('app.close')}>
      &times;
    </button>
  </div>

  {#if recentHistory.length === 0}
    <div class="popover-empty">
      <div class="empty-title">{$t('proxies.history_empty_title')}</div>
      <div class="empty-desc">{$t('proxies.history_empty_desc')}</div>
    </div>
  {:else}
    <div class="sparkline-section">
      <div class="history-sparkline">
        {#each recentHistory as item}
          <div
            class="sparkline-bar"
            style="height: {getBarHeight(item.delay)}%; background-color: {getBarColor(
              item.delay
            )};"
            title="{item.delay > 0 ? item.delay + ' ms' : 'timeout'} ({formatTimeAgo(
              new Date(item.time).getTime(),
              $t
            )})"
          ></div>
        {/each}
      </div>
    </div>

    <div class="history-list">
      {#each [...recentHistory].reverse() as item}
        <div class="history-row">
          <span class="history-time">
            {formatTimeAgo(new Date(item.time).getTime(), $t)}
          </span>
          <span class="history-badge {getLatencyBadgeClass(item.delay)}">
            {item.delay > 0 ? `${item.delay} ms` : 'timeout'}
          </span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .latency-history-popover {
    position: fixed;
    z-index: 1100;
    width: 280px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow:
      0 12px 32px -4px rgba(0, 0, 0, 0.6),
      0 0 0 1px rgba(255, 255, 255, 0.05);
    padding: 14px;
    color: var(--fg-primary);
    font-family: var(--font-family-sans);
    animation: popoverFadeIn 0.16s ease-out forwards;
  }

  @keyframes popoverFadeIn {
    from {
      opacity: 0;
      transform: scale(0.96) translateY(-4px);
    }
    to {
      opacity: 1;
      transform: scale(1) translateY(0);
    }
  }

  .popover-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 12px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 8px;
  }

  .popover-title {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 220px;
  }

  .popover-close-btn {
    background: transparent;
    border: none;
    color: var(--fg-secondary);
    font-size: 18px;
    line-height: 1;
    cursor: pointer;
    padding: 0 4px;
    border-radius: 4px;
    transition: color 0.15s ease;
  }

  .popover-close-btn:hover {
    color: var(--fg-primary);
  }

  .popover-empty {
    padding: 12px 0;
    text-align: center;
  }

  .empty-title {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
    margin-bottom: 4px;
  }

  .empty-desc {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    line-height: 1.4;
  }

  .sparkline-section {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 10px;
    margin-bottom: 12px;
  }

  .history-sparkline {
    display: flex;
    align-items: flex-end;
    gap: 6px;
    height: 36px;
    width: 100%;
  }

  .sparkline-bar {
    flex: 1;
    min-width: 6px;
    border-radius: 2px;
    transition:
      height 0.2s ease,
      opacity 0.15s ease;
    cursor: pointer;
  }

  .sparkline-bar:hover {
    opacity: 0.8;
  }

  .history-list {
    max-height: 160px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-right: 4px;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }

  .history-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: var(--font-size-xs);
    padding: 2px 0;
  }

  .history-time {
    color: var(--fg-secondary);
  }

  .history-badge {
    font-family: var(--font-family-mono);
    font-size: 11px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
  }

  .history-badge.ok {
    background: rgba(70, 209, 138, 0.15);
    color: var(--success);
  }

  .history-badge.mid {
    background: rgba(240, 180, 80, 0.15);
    color: var(--warning);
  }

  .history-badge.bad {
    background: rgba(244, 112, 127, 0.15);
    color: var(--danger);
  }
</style>
