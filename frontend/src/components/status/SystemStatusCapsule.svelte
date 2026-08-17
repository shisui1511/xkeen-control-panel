<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { t } from '../../i18n';
  import { isServiceRestarting } from '../../lib/serviceGrace';
  import { trafficStream, formatTrafficSpeed, type TrafficState } from '../../lib/trafficStream';
  import { capsuleConfigStore } from '../../lib/capsuleSettings';
  import Icon from '../../lib/components/Icon.svelte';
  import SystemQuickMenu from './SystemQuickMenu.svelte';

  let {
    variant = 'desktop',
    systemStats = null,
    activeKernel = '',
    isXkeenRunning = true,
    onSwitchTab
  } = $props<{
    variant?: 'desktop' | 'mobile';
    systemStats?: any;
    activeKernel?: string;
    isXkeenRunning?: boolean;
    onSwitchTab: (tab: string) => void;
  }>();

  let isMenuOpen = $state(false);
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
      {activeKernel}
      {isXkeenRunning}
      onClose={closeMenu}
      {onSwitchTab}
    />
  </div>
{/if}

<style>
  .system-status-capsule-container,
  .capsule-mobile-wrapper {
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
    border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
    border-radius: 9999px;
    box-shadow:
      0 2px 8px rgba(0, 0, 0, 0.25),
      inset 0 1px 0 rgba(255, 255, 255, 0.05);
    height: 32px;
    padding: 2px;
    gap: 1px;
    user-select: none;
    transition:
      border-color 0.2s ease,
      box-shadow 0.2s ease;
  }

  .system-status-capsule:hover {
    border-color: var(--border-hover, rgba(56, 189, 248, 0.3));
  }

  .capsule-segment {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    background: transparent;
    border: none;
    border-radius: 9999px;
    color: var(--text, #e2e8f0);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition:
      background 0.15s ease,
      color 0.15s ease;
    white-space: nowrap;
  }

  .capsule-segment:hover {
    background: var(--hover, rgba(255, 255, 255, 0.08));
    color: #fff;
  }

  .capsule-segment:active {
    transform: scale(0.98);
  }

  /* LED Dots */
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
    background-color: #22c55e;
    box-shadow: 0 0 6px rgba(34, 197, 94, 0.6);
  }

  .led-amber-pulse {
    background-color: #f59e0b;
    box-shadow: 0 0 8px rgba(245, 158, 11, 0.8);
    animation: pulse-amber 1.2s infinite ease-in-out;
  }

  .led-red {
    background-color: #ef4444;
    box-shadow: 0 0 6px rgba(239, 68, 68, 0.6);
  }

  .led-gray {
    background-color: #64748b;
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

  .kernel-name {
    font-weight: 600;
    color: var(--text, #e2e8f0);
  }

  .chevron-icon {
    display: inline-flex;
    align-items: center;
    color: var(--text-muted, #94a3b8);
    transition: transform 0.2s ease;
  }

  .chevron-icon.open {
    transform: rotate(180deg);
  }

  /* Resource segment */
  .resource-segment {
    border-left: 1px solid var(--border-light, rgba(255, 255, 255, 0.08));
  }

  .res-item {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .res-label {
    font-size: 10px;
    font-weight: 600;
    color: var(--text-muted, #94a3b8);
    text-transform: uppercase;
  }

  .res-value {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    font-weight: 600;
  }

  .res-divider {
    color: var(--text-muted, #64748b);
  }

  .warning-glow {
    color: #f59e0b;
    background: rgba(245, 158, 11, 0.12);
  }

  .danger-pulse {
    color: #ef4444;
    background: rgba(239, 68, 68, 0.15);
    animation: resource-danger 1.5s infinite alternate;
  }

  @keyframes resource-danger {
    0% {
      box-shadow: inset 0 0 4px rgba(239, 68, 68, 0.3);
    }
    100% {
      box-shadow: inset 0 0 10px rgba(239, 68, 68, 0.6);
    }
  }

  /* Traffic segment */
  .traffic-segment {
    border-left: 1px solid var(--border-light, rgba(255, 255, 255, 0.08));
    gap: 8px;
    font-family: var(--font-mono, monospace);
    font-size: 11px;
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
    color: #38bdf8;
  }

  .traffic-item.up .traffic-arrow {
    color: #a78bfa;
  }

  /* Mobile pill */
  .capsule-mobile {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px;
    background: rgba(18, 28, 40, 0.72);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
    border-radius: 9999px;
    color: var(--text, #e2e8f0);
    font-size: 11px;
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
    font-family: var(--font-mono, monospace);
    color: var(--text-muted, #94a3b8);
    font-size: 10px;
  }

  /* Tablet / medium screen responsive */
  @media (max-width: 1024px) {
    .res-label {
      display: none;
    }
    .traffic-segment {
      gap: 5px;
    }
  }
</style>
