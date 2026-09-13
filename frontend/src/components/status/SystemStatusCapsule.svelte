<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { t } from '../../i18n';
  import { isServiceRestarting } from '../../lib/serviceGrace';
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
  } = $props<{
    variant?: 'sidebar' | 'rail' | 'mobile' | 'desktop';
    systemStats?: any;
    activeKernel?: string;
    isXkeenRunning?: boolean;
    onSwitchTab: (tab: string) => void;
  }>();

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
    if (!isXkeenRunning) return 'led-red';
    if (activeKernel && activeKernel !== 'none') return 'led-green';
    return 'led-gray';
  });

  let kernelDisplayName = $derived.by(() => {
    if ($isServiceRestarting) return $t('app.restarting');
    if (!activeKernel || activeKernel === 'none') return $t('capsule.offline');
    return activeKernel.charAt(0).toUpperCase() + activeKernel.slice(1);
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
      aria-label={$t('capsule.quick_actions')}
      aria-expanded={isMenuOpen}
    >
      <span class="led-dot {ledClass}"></span>
      <span class="mobile-kernel">{kernelDisplayName}</span>
      {#if $capsuleConfigStore.showTraffic}
        <span class="mobile-traffic">↓↑ {combinedSpeedFormatted}</span>
      {/if}
    </button>

    <SystemQuickMenu
      isOpen={isMenuOpen}
      anchorEl={triggerEl}
      {activeKernel}
      {isXkeenRunning}
      onClose={closeMenu}
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
      <span class="rail-code">{activeKernel ? activeKernel.slice(0, 3).toUpperCase() : 'OFF'}</span>
    </button>

    <SystemQuickMenu
      isOpen={isMenuOpen}
      anchorEl={triggerEl}
      {activeKernel}
      {isXkeenRunning}
      onClose={closeMenu}
      {onSwitchTab}
    />
  </div>
{:else if variant === 'sidebar'}
  <div class="sidebar-status-card" role="region" aria-label={$t('capsule.kernel_status')}>
    <!-- Top Row: Kernel Status & Quick Actions Trigger -->
    <button
      type="button"
      class="sidebar-kernel-row"
      onclick={toggleMenu}
      bind:this={triggerEl}
      aria-label={$t('capsule.quick_actions')}
      aria-expanded={isMenuOpen}
    >
      <div class="kernel-left">
        <span class="led-dot {ledClass}"></span>
        <span class="kernel-name">{kernelDisplayName}</span>
      </div>
      <div class="kernel-right">
        <span class="quick-ctrl-badge">
          <Icon name="zap" size={11} />
          <span>{$t('capsule.quick_actions')}</span>
          <span class="chevron-icon" class:open={isMenuOpen}>
            <Icon name="chevronDown" size={10} />
          </span>
        </span>
      </div>
    </button>

    <!-- Metrics container -->
    {#if $capsuleConfigStore.showResources || $capsuleConfigStore.showTraffic}
      <div class="sidebar-metrics-block">
        <!-- CPU & RAM Row -->
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
              <span class="res-label">CPU</span>
              <span class="res-val">{cpuLoad ? `${cpuLoad.percent}%` : '—'}</span>
            </span>
            <span class="res-divider">·</span>
            <span class="res-item">
              <span class="res-label">RAM</span>
              <span class="res-val">{ramStats ? `${ramStats.usedMB}M` : '—'}</span>
            </span>
          </button>
        {/if}

        <!-- Traffic Row -->
        {#if $capsuleConfigStore.showTraffic}
          <button
            type="button"
            class="sidebar-traffic-row"
            onclick={() => onSwitchTab('traffic')}
            title={`${$t('capsule.traffic_down')}: ${downSpeedFormatted}\n${$t('capsule.traffic_up')}: ${upSpeedFormatted}`}
          >
            <span class="traffic-speed down">
              <span class="arr">↓</span>
              <span class="val">{downSpeedFormatted}</span>
            </span>
            <span class="traffic-divider">·</span>
            <span class="traffic-speed up">
              <span class="arr">↑</span>
              <span class="val">{upSpeedFormatted}</span>
            </span>
          </button>
        {/if}
      </div>
    {/if}

    <SystemQuickMenu
      isOpen={isMenuOpen}
      anchorEl={triggerEl}
      {activeKernel}
      {isXkeenRunning}
      onClose={closeMenu}
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
          <Icon name="chevronDown" size={12} />
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
      isOpen={isMenuOpen}
      anchorEl={triggerEl}
      {activeKernel}
      {isXkeenRunning}
      onClose={closeMenu}
      {onSwitchTab}
    />
  </div>
{/if}

<style>
  /* ==================== Sidebar Card Variant ==================== */
  .sidebar-status-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 8px);
    margin: 6px 12px 8px;
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    transition:
      border-color var(--transition-fast),
      background var(--transition-fast);
  }

  .sidebar-status-card:hover {
    border-color: var(--accent-line);
  }

  .sidebar-kernel-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    background: transparent;
    border: none;
    padding: 2px 2px;
    cursor: pointer;
    color: var(--fg-primary);
    border-radius: 4px;
    transition: opacity 0.15s ease;
  }

  .sidebar-kernel-row:hover {
    opacity: 0.95;
  }

  .kernel-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .kernel-name {
    font-size: 13px;
    font-weight: 700;
    letter-spacing: -0.01em;
    color: var(--fg-primary);
  }

  .kernel-right {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .quick-ctrl-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 7px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
    color: var(--accent);
    font-size: var(--font-size-xs);
    font-weight: 700;
    transition: all var(--transition-fast);
  }

  .sidebar-kernel-row:hover .quick-ctrl-badge {
    background: color-mix(in srgb, var(--accent) 20%, transparent);
    border-color: var(--accent);
  }

  .sidebar-metrics-block {
    display: flex;
    flex-direction: column;
    gap: 4px;
    border-top: 1px solid var(--border-light);
    padding-top: 5px;
  }

  .sidebar-metric-row,
  .sidebar-traffic-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    background: var(--surface-tint);
    border: 1px solid var(--border-light);
    border-radius: 5px;
    padding: 4px 8px;
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    cursor: pointer;
    transition:
      background 0.15s ease,
      color 0.15s ease,
      border-color 0.15s ease;
  }

  .sidebar-metric-row:hover,
  .sidebar-traffic-row:hover {
    background: color-mix(in srgb, var(--accent) 8%, transparent);
    border-color: color-mix(in srgb, var(--accent) 20%, transparent);
    color: var(--fg-primary);
  }

  .sidebar-traffic-row {
    font-family: var(--font-family-mono, monospace);
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
  }

  .traffic-speed {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }

  .traffic-speed.down .arr {
    color: var(--accent);
    font-weight: 700;
  }

  .traffic-speed.up .arr {
    color: var(--purple);
    font-weight: 700;
  }

  .traffic-divider {
    color: var(--fg-faint);
  }

  .res-item {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .res-label {
    font-size: var(--font-size-xs);
    font-weight: 700;
    color: var(--fg-primary);
  }

  .res-val {
    font-family: var(--font-family-mono, monospace);
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .res-divider {
    color: var(--fg-faint);
  }

  /* ==================== Sidebar Rail Variant ==================== */
  .sidebar-rail-wrapper {
    display: flex;
    justify-content: center;
    padding: 4px 0 8px;
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
    border-radius: var(--radius-md, 6px);
    cursor: pointer;
    padding: 0;
    color: var(--fg-primary);
    transition:
      background 0.15s ease,
      border-color 0.15s ease;
  }

  .rail-status-btn:hover {
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    border-color: var(--accent);
  }

  .rail-code {
    font-size: var(--font-size-xs);
    font-weight: 700;
    letter-spacing: 0.05em;
    font-family: var(--font-family-mono, monospace);
    color: var(--fg-secondary);
  }

  /* ==================== LED Dots ==================== */
  .led-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
    transition:
      background-color 0.3s ease,
      box-shadow 0.3s ease;
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
    transition: transform 0.2s ease;
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
    gap: 6px;
    padding: 3px 10px;
    background: rgba(18, 28, 40, 0.72);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    border: 1px solid var(--border);
    border-radius: 9999px;
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

  .mobile-traffic {
    font-family: var(--font-family-mono, monospace);
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
    border-radius: 9999px;
    box-shadow:
      0 2px 8px rgba(0, 0, 0, 0.25),
      inset 0 1px 0 rgba(255, 255, 255, 0.05);
    height: 32px;
    padding: 2px;
    gap: 1px;
    user-select: none;
  }

  .capsule-segment {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    background: transparent;
    border: none;
    border-radius: 9999px;
    color: var(--fg-primary);
    font-size: 12px;
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
    gap: 8px;
    font-family: var(--font-family-mono, monospace);
    font-size: var(--font-size-xs);
  }

  .traffic-item {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }

  .traffic-arrow {
    font-weight: 700;
    font-size: 12px;
  }

  .traffic-item.down .traffic-arrow {
    color: var(--accent);
  }

  .traffic-item.up .traffic-arrow {
    color: var(--purple);
  }
</style>
