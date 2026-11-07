<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { t } from '../../i18n';
  import { isServiceRestarting } from '../../lib/serviceGrace';
  import { isConflict } from '../../stores';
  import { trafficStream, formatTrafficSpeed, type TrafficState } from '../../lib/trafficStream';
  import { capsuleConfigStore } from '../../lib/capsuleSettings';
  import Icon from '../../lib/components/Icon.svelte';
  import SystemQuickMenu from './SystemQuickMenu.svelte';

  let {
    variant = 'sidebar',
    systemStats = null,
    activeKernel = '',
    isXkeenRunning = true,
    onSwitchTab
  }: {
    variant?: 'sidebar' | 'rail' | 'mobile' | 'desktop';
    systemStats?: any;
    activeKernel?: string;
    isXkeenRunning?: boolean;
    onSwitchTab: (tab: string) => void;
  } = $props();

  let isMenuOpen = $state(false);
  let triggerEl = $state<HTMLElement | null>(null);
  let trafficState = $state<TrafficState>(trafficStream.getState());

  let unsubTraffic: (() => void) | null = null;

  onMount(() => {
    unsubTraffic = trafficStream.subscribe((state) => {
      trafficState = state;
    });
  });

  onDestroy(() => {
    if (unsubTraffic) {
      unsubTraffic();
      unsubTraffic = null;
    }
  });

  // Derived metrics
  let cpuLoad = $derived.by(() => {
    if (!systemStats || !systemStats.load || systemStats.load.length === 0) return null;
    const load1m = systemStats.load[0] || 0;
    const cores = systemStats.go_runtime?.gomaxprocs || 2;
    const pct = Math.min(Math.round((load1m / cores) * 100), 100);
    return {
      raw: load1m,
      percent: pct,
      l1: systemStats.load[0]?.toFixed(2) ?? '0.00',
      l2: systemStats.load[1]?.toFixed(2) ?? '0.00',
      l3: systemStats.load[2]?.toFixed(2) ?? '0.00'
    };
  });

  let ramStats = $derived.by(() => {
    if (!systemStats || !systemStats.memory) return null;
    const { used, total, free } = systemStats.memory;
    const pct = total > 0 ? (used / total) * 100 : 0;
    const usedMB = (used / 1024 / 1024).toFixed(0);
    const totalMB = (total / 1024 / 1024).toFixed(0);
    const freeMB = (free / 1024 / 1024).toFixed(0);
    return {
      percent: pct,
      usedMB,
      totalMB,
      freeMB
    };
  });

  let maxResourcePercent = $derived.by(() => {
    const cPct = cpuLoad ? cpuLoad.percent : 0;
    const rPct = ramStats ? ramStats.percent : 0;
    return Math.max(cPct, rPct);
  });

  let resourceWarningClass = $derived.by(() => {
    if (maxResourcePercent >= 92) return 'danger-pulse';
    if (maxResourcePercent >= 80) return 'warning-glow';
    return '';
  });

  // Traffic speeds
  let downSpeedFormatted = $derived(formatTrafficSpeed(trafficState.down));
  let upSpeedFormatted = $derived(formatTrafficSpeed(trafficState.up));
  let combinedSpeedFormatted = $derived(formatTrafficSpeed(trafficState.down + trafficState.up));

  // LED state
  let ledClass = $derived.by(() => {
    if ($isServiceRestarting) return 'led-amber-pulse';
    if ($isConflict) return 'led-red';
    // Ядро не выбрано (XKeen ещё не поставлен или не настроен) — это не авария,
    // а незавершённая настройка: серая точка, а не красная.
    if (!activeKernel || activeKernel === 'none') return 'led-gray';
    if (!isXkeenRunning) return 'led-red';
    return 'led-green';
  });

  let kernelDisplayName = $derived.by(() => {
    if ($isServiceRestarting) return $t('app.restarting');
    if ($isConflict) return $t('kernel.state_conflict');
    if (!activeKernel || activeKernel === 'none') return $t('capsule.offline');
    return activeKernel.charAt(0).toUpperCase() + activeKernel.slice(1);
  });

  // Подпись состояния под именем ядра: одна красная точка рядом с «Mihomo»
  // не объясняла, что именно не так (служба XKeen остановлена).
  let stateLabel = $derived.by(() => {
    if ($isServiceRestarting || $isConflict) return '';
    if (!activeKernel || activeKernel === 'none') return '';
    return isXkeenRunning ? $t('kernel.status.running') : $t('kernel.status.stopped');
  });

  function toggleMenu(e: MouseEvent) {
    e.stopPropagation();
    isMenuOpen = !isMenuOpen;
  }

  function closeMenu() {
    isMenuOpen = false;
  }
</script>

{#if variant === 'mobile'}
  <div class="capsule-mobile-wrapper">
    <button
      type="button"
      class="capsule-mobile"
      onclick={toggleMenu}
      bind:this={triggerEl}
      aria-expanded={isMenuOpen}
    >
      <span class="led-dot {ledClass}"></span>
      <span class="mobile-kernel" class:is-wide={!activeKernel || activeKernel === 'none'}
        >{kernelDisplayName}</span
      >
      <!-- Без ядра трафик всегда «0 B/s»: место в узкой шапке отдаём подписи состояния -->
      {#if $capsuleConfigStore.showTraffic && activeKernel && activeKernel !== 'none'}
        <span class="mobile-traffic">↓↑ {combinedSpeedFormatted}</span>
      {/if}
      <!-- Accessible name = visible text + purpose (WCAG 2.5.3 Label in Name) -->
      <span class="sr-only">{$t('capsule.quick_actions')}</span>
    </button>

    <SystemQuickMenu
      open={isMenuOpen}
      anchorEl={triggerEl}
      {activeKernel}
      {isXkeenRunning}
      onclose={closeMenu}
      {onSwitchTab}
    />
  </div>
{:else if variant === 'rail'}
  <div class="sidebar-rail-wrapper">
    <button
      type="button"
      class="rail-status-btn nav-item"
      onclick={toggleMenu}
      bind:this={triggerEl}
      data-label="{$t('capsule.kernel_status')}: {kernelDisplayName}"
      aria-label={$t('capsule.quick_actions')}
      aria-expanded={isMenuOpen}
      title={$t('capsule.quick_actions')}
    >
      <span class="led-dot {ledClass}"></span>
      <span class="rail-code"
        >{$isConflict ? 'X+M' : activeKernel ? activeKernel.slice(0, 3).toUpperCase() : 'OFF'}</span
      >
    </button>

    <SystemQuickMenu
      open={isMenuOpen}
      anchorEl={triggerEl}
      {activeKernel}
      {isXkeenRunning}
      onclose={closeMenu}
      {onSwitchTab}
    />
  </div>
{:else if variant === 'sidebar'}
  <div class="sidebar-status-card" role="region" aria-label={$t('capsule.kernel_status')}>
    <!-- Вся строка — кнопка меню быстрых действий; шеврон показывает, что она раскрывается -->
    <button
      type="button"
      class="sidebar-kernel-row"
      onclick={toggleMenu}
      bind:this={triggerEl}
      aria-expanded={isMenuOpen}
      aria-haspopup="menu"
      title={$t('capsule.quick_actions')}
    >
      <span class="led-dot {ledClass}" aria-hidden="true"></span>
      <span class="kernel-text">
        <span class="kernel-name">{kernelDisplayName}</span>
        {#if stateLabel}
          <span class="kernel-state" class:is-down={!isXkeenRunning}>{stateLabel}</span>
        {/if}
      </span>
      <span class="sr-only">{$t('capsule.quick_actions')}</span>
      <span class="chevron-icon" class:open={isMenuOpen} aria-hidden="true">
        <Icon name="chevron-down" size={14} />
      </span>
    </button>

    {#if $capsuleConfigStore.showResources || $capsuleConfigStore.showTraffic}
      <div class="sidebar-metrics-block">
        {#if $capsuleConfigStore.showResources}
          <button
            type="button"
            class="sidebar-metric-row {resourceWarningClass}"
            onclick={() => onSwitchTab('dashboard')}
            title={cpuLoad && ramStats
              ? `${$t('capsule.load_avg', { l1: cpuLoad.l1, l2: cpuLoad.l2, l3: cpuLoad.l3 })}\n${$t('capsule.ram_details', { used: ramStats.usedMB + ' MB', total: ramStats.totalMB + ' MB', free: ramStats.freeMB + ' MB' })}`
              : $t('capsule.cpu')}
          >
            <span class="res-item">
              <span class="res-label">{$t('capsule.cpu')}</span>
              <span class="res-val">{cpuLoad ? `${cpuLoad.percent}%` : '—'}</span>
            </span>
            <span class="res-item">
              <span class="res-label">{$t('capsule.ram')}</span>
              <span class="res-val">{ramStats ? `${ramStats.usedMB} MB` : '—'}</span>
            </span>
          </button>
        {/if}

        {#if $capsuleConfigStore.showTraffic}
          <button
            type="button"
            class="sidebar-metric-row"
            onclick={() => onSwitchTab('traffic')}
            title={`${$t('capsule.traffic_down')}: ${downSpeedFormatted}\n${$t('capsule.traffic_up')}: ${upSpeedFormatted}`}
          >
            <span class="res-item traffic-speed down">
              <span class="arr" aria-hidden="true">↓</span>
              <span class="sr-only">{$t('capsule.traffic_down')}</span>
              <span class="res-val">{downSpeedFormatted}</span>
            </span>
            <span class="res-item traffic-speed up">
              <span class="arr" aria-hidden="true">↑</span>
              <span class="sr-only">{$t('capsule.traffic_up')}</span>
              <span class="res-val">{upSpeedFormatted}</span>
            </span>
          </button>
        {/if}
      </div>
    {/if}

    <SystemQuickMenu
      open={isMenuOpen}
      anchorEl={triggerEl}
      {activeKernel}
      {isXkeenRunning}
      onclose={closeMenu}
      {onSwitchTab}
    />
  </div>
{:else}
  <div class="system-status-capsule-container">
    <div class="system-status-capsule" role="region" aria-label={$t('capsule.kernel_status')}>
      <!-- Segment 1: Kernel & Quick Actions Trigger -->
      <button
        type="button"
        class="capsule-segment kernel-segment"
        onclick={toggleMenu}
        bind:this={triggerEl}
        aria-label={$t('capsule.quick_actions')}
        aria-expanded={isMenuOpen}
      >
        <span class="led-dot {ledClass}"></span>
        <span class="segment-text kernel-name">{kernelDisplayName}</span>
        <span class="chevron-icon" class:open={isMenuOpen}>
          <Icon name="chevron-down" size={12} />
        </span>
      </button>

      <!-- Segment 2: CPU & RAM Resources -->
      {#if $capsuleConfigStore.showResources}
        <button
          type="button"
          class="capsule-segment resource-segment {resourceWarningClass}"
          onclick={() => onSwitchTab('dashboard')}
          title={cpuLoad && ramStats
            ? `${$t('capsule.load_avg', { l1: cpuLoad.l1, l2: cpuLoad.l2, l3: cpuLoad.l3 })}\n${$t('capsule.ram_details', { used: ramStats.usedMB + ' MB', total: ramStats.totalMB + ' MB', free: ramStats.freeMB + ' MB' })}`
            : $t('capsule.cpu')}
          aria-label={$t('capsule.cpu')}
        >
          <span class="res-item">
            <span class="res-label">CPU</span>
            <span class="res-value">{cpuLoad ? `${cpuLoad.percent}%` : '—'}</span>
          </span>
          <span class="res-divider">·</span>
          <span class="res-item">
            <span class="res-label">RAM</span>
            <span class="res-value">{ramStats ? `${ramStats.usedMB}M` : '—'}</span>
          </span>
        </button>
      {/if}

      <!-- Segment 3: Live Traffic -->
      {#if $capsuleConfigStore.showTraffic}
        <button
          type="button"
          class="capsule-segment traffic-segment"
          onclick={() => onSwitchTab('traffic')}
          aria-label={$t('capsule.nav_traffic')}
          title={`${$t('capsule.traffic_down')}: ${downSpeedFormatted}\n${$t('capsule.traffic_up')}: ${upSpeedFormatted}`}
        >
          <span class="traffic-item down">
            <span class="traffic-arrow">↓</span>
            <span class="traffic-val">{downSpeedFormatted}</span>
          </span>
          <span class="traffic-item up">
            <span class="traffic-arrow">↑</span>
            <span class="traffic-val">{upSpeedFormatted}</span>
          </span>
        </button>
      {/if}
    </div>

    <SystemQuickMenu
      open={isMenuOpen}
      anchorEl={triggerEl}
      {activeKernel}
      {isXkeenRunning}
      onclose={closeMenu}
      {onSwitchTab}
    />
  </div>
{/if}

<style>
  /* ==================== Sidebar Card Variant ==================== */
  .sidebar-status-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    margin: var(--spacing-1-5) var(--spacing-3) var(--spacing-2);
    padding: var(--spacing-1);
    display: flex;
    flex-direction: column;
  }

  .sidebar-kernel-row {
    display: flex;
    align-items: center;
    gap: var(--spacing-2-5);
    width: 100%;
    background: transparent;
    border: none;
    padding: var(--spacing-1-5) var(--spacing-2);
    cursor: pointer;
    color: var(--fg-primary);
    text-align: left;
    border-radius: var(--radius-sm);
    transition: background var(--transition-fast);
  }

  .sidebar-kernel-row:hover,
  .sidebar-kernel-row[aria-expanded='true'] {
    background: color-mix(in srgb, var(--fg-primary) 6%, transparent);
  }

  .kernel-text {
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    min-width: 0;
    line-height: 1.25;
  }

  .kernel-name {
    font-size: var(--font-size-sm);
    font-weight: 700;
    letter-spacing: -0.01em;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .kernel-state {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .kernel-state.is-down {
    color: var(--danger);
  }

  .sidebar-kernel-row .chevron-icon {
    color: var(--fg-dim);
  }

  .sidebar-kernel-row:hover .chevron-icon {
    color: var(--fg-primary);
  }

  .sidebar-metrics-block {
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--border-light);
    margin-top: var(--spacing-1);
    padding-top: var(--spacing-1);
  }

  /* Две колонки одинаковой ширины: значения обеих строк стоят друг под другом */
  .sidebar-metric-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--spacing-2);
    width: 100%;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    padding: var(--spacing-1) var(--spacing-2);
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    text-align: left;
    cursor: pointer;
    transition:
      background var(--transition-fast),
      color var(--transition-fast);
  }

  .sidebar-metric-row:hover {
    background: color-mix(in srgb, var(--fg-primary) 6%, transparent);
    color: var(--fg-primary);
  }

  .traffic-speed .arr {
    font-weight: 700;
  }

  .traffic-speed.down .arr {
    color: var(--accent);
  }

  .traffic-speed.up .arr {
    color: var(--purple);
  }

  .res-item {
    display: inline-flex;
    align-items: baseline;
    gap: var(--spacing-1-5);
    min-width: 0;
    white-space: nowrap;
  }

  .res-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-dim);
  }

  .res-val {
    font-size: var(--font-size-xs);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .res-divider {
    color: var(--fg-faint);
  }

  /* ==================== Sidebar Rail Variant ==================== */
  .sidebar-rail-wrapper {
    display: flex;
    justify-content: center;
    padding: var(--spacing-1) 0 var(--spacing-2);
  }

  .rail-status-btn {
    width: 44px;
    height: 44px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 3px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    cursor: pointer;
    padding: 0;
    color: var(--fg-primary);
    transition:
      background var(--transition-fast),
      border-color var(--transition-fast);
  }

  .rail-status-btn:hover {
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    border-color: var(--accent);
  }

  .rail-code {
    font-size: var(--font-size-xs);
    font-weight: 700;
    letter-spacing: 0.05em;
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
  }

  /* ==================== LED Dots ==================== */
  .led-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
    transition:
      background-color var(--transition-normal),
      box-shadow var(--transition-normal);
  }

  .led-green {
    background-color: var(--success);
    box-shadow: 0 0 6px color-mix(in srgb, var(--success) 60%, transparent);
  }

  .led-amber-pulse {
    background-color: var(--warning);
    box-shadow: 0 0 8px color-mix(in srgb, var(--warning) 80%, transparent);
    animation: pulse-amber 1.2s infinite ease-in-out;
  }

  .led-red {
    background-color: var(--danger);
    box-shadow: 0 0 6px color-mix(in srgb, var(--danger) 60%, transparent);
  }

  .led-gray {
    background-color: var(--fg-faint);
  }

  @keyframes pulse-amber {
    0%,
    100% {
      opacity: 1;
      transform: scale(1);
    }
    50% {
      opacity: 0.4;
      transform: scale(1.2);
    }
  }

  .chevron-icon {
    display: inline-flex;
    align-items: center;
    transition: transform var(--transition-move);
  }

  .chevron-icon.open {
    transform: rotate(180deg);
  }

  .warning-glow {
    color: var(--warning) !important;
    border-color: color-mix(in srgb, var(--warning) 30%, transparent) !important;
  }

  .danger-pulse {
    color: var(--danger) !important;
    border-color: color-mix(in srgb, var(--danger) 40%, transparent) !important;
    animation: resource-danger 1.5s infinite alternate;
  }

  @keyframes resource-danger {
    0% {
      box-shadow: inset 0 0 4px color-mix(in srgb, var(--danger) 30%, transparent);
    }
    100% {
      box-shadow: inset 0 0 10px color-mix(in srgb, var(--danger) 60%, transparent);
    }
  }

  /* ==================== Mobile Header Pill ==================== */
  .capsule-mobile-wrapper {
    position: relative;
    display: inline-flex;
    align-items: center;
  }

  .capsule-mobile {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1-5);
    padding: 3px var(--spacing-2-5);
    background: rgba(18, 28, 40, 0.72);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    border: 1px solid var(--border);
    border-radius: var(--radius-full);
    /* Pill background stays a fixed dark glass regardless of site theme
       (matches the always-dark mobile header), so its text stays fixed
       light too instead of flipping with theme tokens. */
    color: var(--fg-terminal);
    font-size: var(--font-size-xs);
    font-weight: 600;
    cursor: pointer;
    height: 28px;
  }

  .mobile-kernel {
    max-width: 70px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Без ядра скорость не показывается — её место занимает подпись состояния */
  .mobile-kernel.is-wide {
    max-width: 128px;
  }

  .mobile-traffic {
    font-family: var(--font-family-sans);
    font-variant-numeric: tabular-nums;
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
  }

  /* ==================== Standalone Desktop Pill ==================== */
  .system-status-capsule-container {
    position: relative;
    display: inline-flex;
    align-items: center;
  }

  .system-status-capsule {
    display: inline-flex;
    align-items: stretch;
    background: rgba(18, 28, 40, 0.72);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid var(--border);
    border-radius: var(--radius-full);
    box-shadow:
      0 2px 8px rgba(0, 0, 0, 0.25),
      inset 0 1px 0 rgba(255, 255, 255, 0.05);
    height: 32px;
    padding: var(--spacing-0-5);
    gap: 1px;
    user-select: none;
  }

  .capsule-segment {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1-5);
    padding: 0 var(--spacing-2-5);
    background: transparent;
    border: none;
    border-radius: var(--radius-full);
    color: var(--fg-primary);
    font-size: var(--font-size-xs);
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
  }

  .capsule-segment:hover {
    background: var(--bg-hover);
    color: var(--fg-primary);
  }

  .resource-segment {
    border-left: 1px solid var(--border-light);
  }

  .traffic-segment {
    border-left: 1px solid var(--border-light);
    gap: var(--spacing-2);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
  }

  .traffic-item {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }

  .traffic-arrow {
    font-weight: 700;
    font-size: var(--font-size-xs);
  }

  .traffic-item.down .traffic-arrow {
    color: var(--accent);
  }

  .traffic-item.up .traffic-arrow {
    color: var(--purple);
  }
</style>
