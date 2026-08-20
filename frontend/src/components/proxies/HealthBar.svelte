<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { t } from '../../i18n';
  import type { GroupHealthStats } from '../../lib/proxyStats';

  interface Props {
    stats: GroupHealthStats;
    compact?: boolean;
  }

  let { stats, compact = false }: Props = $props();

  let barEl: HTMLElement | null = $state(null);
  let tooltipEl: HTMLElement | null = $state(null);
  let isHovered = $state(false);
  let tooltipTop = $state(0);
  let tooltipLeft = $state(0);

  let rows = $derived.by(() => {
    const list: { label: string; count: number; pct: number; colorClass: string }[] = [];
    if (stats.fast > 0) {
      list.push({
        label: $t('proxies.health_fast'),
        count: stats.fast,
        pct: stats.fastPct,
        colorClass: 'color-fast'
      });
    }
    if (stats.mid > 0) {
      list.push({
        label: $t('proxies.health_mid'),
        count: stats.mid,
        pct: stats.midPct,
        colorClass: 'color-mid'
      });
    }
    if (stats.bad > 0) {
      list.push({
        label: $t('proxies.health_bad'),
        count: stats.bad,
        pct: stats.badPct,
        colorClass: 'color-bad'
      });
    }
    if (stats.unchecked > 0) {
      list.push({
        label: $t('proxies.health_unchecked'),
        count: stats.unchecked,
        pct: stats.uncheckedPct,
        colorClass: 'color-unchecked'
      });
    }
    if (stats.system > 0) {
      list.push({
        label: $t('proxies.health_system'),
        count: stats.system,
        pct: stats.systemPct,
        colorClass: 'color-system'
      });
    }
    return list;
  });

  let ariaSummary = $derived(
    rows.map((r) => `${r.label}: ${r.count} (${r.pct}%)`).join(', ') ||
      $t('proxies.health_unchecked')
  );

  async function updatePosition() {
    if (!barEl) return;
    await tick();
    const barRect = barEl.getBoundingClientRect();
    const tooltipRect = tooltipEl ? tooltipEl.getBoundingClientRect() : { width: 180, height: 60 };

    let newTop = barRect.top - tooltipRect.height - 8;
    let newLeft = barRect.left + barRect.width / 2 - tooltipRect.width / 2;

    const padding = 10;
    if (newLeft < padding) {
      newLeft = padding;
    } else if (newLeft + tooltipRect.width > window.innerWidth - padding) {
      newLeft = window.innerWidth - tooltipRect.width - padding;
    }

    if (newTop < padding) {
      newTop = barRect.bottom + 8;
    }

    tooltipTop = newTop;
    tooltipLeft = newLeft;
  }

  function showTooltip() {
    isHovered = true;
    updatePosition();
  }

  function hideTooltip() {
    isHovered = false;
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Escape' && isHovered) {
      hideTooltip();
    }
  }

  onMount(() => {
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, { capture: true, passive: true });
    window.addEventListener('keydown', handleKeyDown);
  });

  onDestroy(() => {
    window.removeEventListener('resize', updatePosition);
    window.removeEventListener('scroll', updatePosition, { capture: true });
    window.removeEventListener('keydown', handleKeyDown);
  });
</script>

<div
  bind:this={barEl}
  class="health-bar"
  class:compact
  role="group"
  tabindex="0"
  aria-label={ariaSummary}
  onmouseenter={showTooltip}
  onmouseleave={hideTooltip}
  onfocus={showTooltip}
  onblur={hideTooltip}
>
  {#if stats.fastPct > 0}
    <div class="health-segment fast" style="width: {stats.fastPct}%;"></div>
  {/if}
  {#if stats.midPct > 0}
    <div class="health-segment mid" style="width: {stats.midPct}%;"></div>
  {/if}
  {#if stats.badPct > 0}
    <div class="health-segment bad" style="width: {stats.badPct}%;"></div>
  {/if}
  {#if stats.uncheckedPct > 0}
    <div class="health-segment unchecked" style="width: {stats.uncheckedPct}%;"></div>
  {/if}
  {#if stats.systemPct > 0}
    <div class="health-segment system" style="width: {stats.systemPct}%;"></div>
  {/if}
</div>

{#if isHovered && rows.length > 0}
  <div
    bind:this={tooltipEl}
    class="health-tooltip"
    style="top: {tooltipTop}px; left: {tooltipLeft}px;"
  >
    {#each rows as row}
      <div class="ht-row">
        <span class="ht-dot {row.colorClass}"></span>
        <span class="ht-label">{row.label}:</span>
        <span class="ht-count">{row.count}</span>
        <span class="ht-pct">({row.pct}%)</span>
      </div>
    {/each}
  </div>
{/if}

<style>
  .health-bar {
    margin: 6px 18px 10px;
    border-radius: var(--radius-sm, 4px);
    height: 4px;
    display: flex;
    overflow: hidden;
    background: var(--bg-secondary, rgba(255, 255, 255, 0.05));
    cursor: pointer;
    transition: height 0.15s ease;
  }

  .health-bar:hover,
  .health-bar:focus-visible {
    height: 6px;
    outline: 2px solid var(--accent, #3b82f6);
    outline-offset: 2px;
  }

  .health-segment {
    height: 100%;
    transition: width 0.3s ease;
  }

  .health-segment.fast {
    background-color: var(--success, #22c55e);
  }

  .health-segment.mid {
    background-color: var(--warning, #eab308);
  }

  .health-segment.bad {
    background-color: var(--danger, #ef4444);
  }

  .health-segment.unchecked {
    background-color: var(--fg-dim, #64748b);
  }

  .health-segment.system {
    background-color: var(--fg-faint, #475569);
  }

  .health-tooltip {
    position: fixed;
    z-index: 1100;
    background: var(--bg-elevated, var(--bg-card, #1e293b));
    border: 1px solid var(--border, #334155);
    border-radius: var(--radius-lg, 8px);
    padding: 8px 10px;
    font-size: 12px;
    color: var(--fg-primary, #f8fafc);
    box-shadow: var(--shadow-md, 0 4px 6px -1px rgba(0, 0, 0, 0.3));
    pointer-events: none;
    white-space: nowrap;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .ht-row {
    display: flex;
    align-items: center;
    gap: 6px;
    line-height: 1.2;
  }

  .ht-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .ht-dot.color-fast {
    background: var(--success, #22c55e);
  }

  .ht-dot.color-mid {
    background: var(--warning, #eab308);
  }

  .ht-dot.color-bad {
    background: var(--danger, #ef4444);
  }

  .ht-dot.color-unchecked {
    background: var(--fg-dim, #64748b);
  }

  .ht-dot.color-system {
    background: var(--fg-faint, #475569);
  }

  .ht-label {
    color: var(--fg-secondary, #94a3b8);
  }

  .ht-count {
    font-weight: 600;
    color: var(--fg-primary, #f8fafc);
  }

  .ht-pct {
    color: var(--fg-muted, #64748b);
    font-size: 11px;
  }
</style>
