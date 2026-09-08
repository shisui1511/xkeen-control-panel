<script lang="ts">
  import { t } from '../../i18n';
  import { formatBytes } from '../../lib/format';
  import Card from '../Card.svelte';
  import Skeleton from '../Skeleton.svelte';

  export interface SystemStats {
    memory: { total: number; used: number; free: number };
    disk: { total: number; used: number; free: number };
    ssl_cert_days?: number;
    load: [number, number, number];
    uptime: { seconds: number; days: number; hours: number; minutes: number };
    go_runtime?: {
      goroutines: number;
      heap_alloc: number;
      heap_sys: number;
      num_gc: number;
      go_version: string;
      gomaxprocs: number;
      goarch: string;
    };
    router_model?: string;
    hostname?: string;
    wan_status?: string;
    default_gateway?: string;
    dns_servers?: string[];
    dns_resolving?: boolean;
    invalid_config?: boolean;
    platform?: string;
    kernel_version?: string;
    ip_interface?: string;
    timezone?: string;
    config_path?: string;
    config_lines?: number;
    boot_time?: string;
  }

  let {
    systemStats,
    loadHistory = [],
    sparklineData = null
  } = $props<{
    systemStats: SystemStats | null;
    loadHistory?: number[];
    sparklineData?: { line: string; fill: string } | null;
  }>();

  function getUsageColor(pct: number): string {
    if (pct >= 90) return 'var(--danger)';
    if (pct >= 75) return 'var(--warning)';
    return 'var(--success)';
  }

  const diskUsedPct = $derived.by(() => {
    if (!systemStats?.disk || systemStats.disk.total === 0) return 0;
    return (systemStats.disk.used / systemStats.disk.total) * 100;
  });

  const diskBarColor = $derived(getUsageColor(diskUsedPct));

  const ramUsedPct = $derived.by(() => {
    if (!systemStats?.memory || systemStats.memory.total === 0) return 0;
    return (systemStats.memory.used / systemStats.memory.total) * 100;
  });

  const ramBarColor = $derived(getUsageColor(ramUsedPct));

  const ramUsedMb = $derived.by(() => {
    if (!systemStats?.memory) return '0.00';
    return (systemStats.memory.used / 1024 / 1024).toFixed(1);
  });

  const ramTotalMb = $derived.by(() => {
    if (!systemStats?.memory) return '0.00';
    return (systemStats.memory.total / 1024 / 1024).toFixed(1);
  });

  // Load average relative to the number of CPU cores — turns the raw figure
  // into a saturation percentage so "0.73" reads as "37% of CPU capacity".
  const loadPerCorePct = $derived.by(() => {
    const cores = systemStats?.go_runtime?.gomaxprocs ?? 0;
    if (!cores || !systemStats?.load) return null;
    return Math.min((systemStats.load[0] / cores) * 100, 999);
  });
</script>

<div class="system-resources-card">
  <Card title={$t('dash.system_stats')}>
    {#if !systemStats}
      <div class="stats-grid">
        {#each [1, 2, 3, 4] as _}
          <div class="stat-box">
            <Skeleton type="rect" width="80px" height="16px" />
            <Skeleton type="rect" width="120px" height="28px" />
            <Skeleton type="rect" width="100%" height="8px" />
          </div>
        {/each}
      </div>
    {:else}
      <div class="stats-grid">
        <!-- Storage (Disk) -->
        {#if systemStats.disk}
          <div class="stat-box">
            <div class="stat-content-top">
              <div class="stat-head">
                <span class="stat-label">{$t('dash.disk')}</span>
                <span class="stat-pct" style="color: {diskBarColor};"
                  >{diskUsedPct.toFixed(1)}%</span
                >
              </div>
              <div class="stat-value">
                {formatBytes(systemStats.disk.free)}
                <span class="stat-unit">{$t('dash.free_suffix')}</span>
              </div>
              <div
                class="res-sub"
                title={$t('dash.disk_of_total', {
                  total: formatBytes(systemStats.disk.total)
                })}
              >
                {$t('dash.disk_of_total', {
                  total: formatBytes(systemStats.disk.total)
                })}
              </div>
            </div>
            <div class="stat-bottom">
              <div class="stat-bar">
                <div
                  class="stat-bar-fill"
                  style="width: {Math.min(diskUsedPct, 100).toFixed(
                    1
                  )}%; background: {diskBarColor}; box-shadow: 0 0 6px -1px {diskBarColor};"
                ></div>
              </div>
            </div>
          </div>
        {/if}

        <!-- RAM -->
        <div class="stat-box">
          <div class="stat-content-top">
            <div class="stat-head">
              <span class="stat-label">{$t('dash.ram')}</span>
              <span class="stat-pct" style="color: {ramBarColor};">{ramUsedPct.toFixed(1)}%</span>
            </div>
            <div class="stat-value">
              {ramUsedMb}
              <span class="stat-unit">{$t('dash.unit_mb')}</span>
            </div>
            <div
              class="res-sub"
              title={$t('dash.ram_of_total', {
                total: ramTotalMb
              })}
            >
              {$t('dash.ram_of_total', {
                total: ramTotalMb
              })}
            </div>
          </div>
          <div class="stat-bottom">
            <div class="stat-bar">
              <div
                class="stat-bar-fill"
                style="width: {Math.min(ramUsedPct, 100).toFixed(
                  1
                )}%; background: {ramBarColor}; box-shadow: 0 0 6px -1px {ramBarColor};"
              ></div>
            </div>
          </div>
        </div>

        <!-- Load Average -->
        <div class="stat-box">
          <div class="stat-content-top">
            <div class="stat-head">
              <span class="stat-label">{$t('dash.load')}</span>
              {#if loadPerCorePct !== null}
                <span class="stat-pct" style="color: {getUsageColor(loadPerCorePct)};"
                  >{loadPerCorePct.toFixed(0)}%</span
                >
              {/if}
            </div>
            <div class="stat-value">
              {systemStats.load[0].toFixed(2)}
            </div>
            <div
              class="res-sub res-sub-load"
              title={$t('dash.load_avg_line', {
                v1: systemStats.load[0].toFixed(2),
                v2: systemStats.load[1].toFixed(2),
                v3: systemStats.load[2].toFixed(2)
              })}
            >
              {$t('dash.load_avg_line', {
                v1: systemStats.load[0].toFixed(2),
                v2: systemStats.load[1].toFixed(2),
                v3: systemStats.load[2].toFixed(2)
              })}
            </div>
          </div>
          <div class="stat-bottom">
            {#if sparklineData}
              <div class="sparkline-container">
                <svg
                  class="sparkline"
                  viewBox="0 0 200 42"
                  preserveAspectRatio="none"
                  aria-hidden="true"
                >
                  <defs>
                    <linearGradient id="loadGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop class="sparkline-grad-from" offset="0%" />
                      <stop class="sparkline-grad-to" offset="100%" />
                    </linearGradient>
                  </defs>
                  <line class="sparkline-baseline" x1="0" y1="41" x2="200" y2="41" />
                  <path d={sparklineData.fill} fill="url(#loadGrad)" />
                  <path
                    class="sparkline-line"
                    d={sparklineData.line}
                    vector-effect="non-scaling-stroke"
                  />
                </svg>
              </div>
            {/if}
          </div>
        </div>

        <!-- Uptime -->
        <div class="stat-box">
          <div class="stat-content-top">
            <div class="stat-head">
              <span class="stat-label">{$t('dash.uptime')}</span>
            </div>
            <div class="stat-value">
              {$t('dash.uptime_dhm', {
                days: systemStats.uptime.days,
                hours: systemStats.uptime.hours,
                minutes: systemStats.uptime.minutes
              })}
            </div>
            {#if systemStats.boot_time}
              <div class="res-sub" title={$t('dash.uptime_since', { time: systemStats.boot_time })}>
                {$t('dash.uptime_since', { time: systemStats.boot_time })}
              </div>
            {/if}
          </div>
          <div class="stat-bottom uptime-badge-row">
            <span class="uptime-badge">
              <span class="uptime-dot"></span>
              {$t('dash.uptime_stable')}
            </span>
          </div>
        </div>
      </div>
    {/if}
  </Card>
</div>

<style>
  .system-resources-card {
    width: 100%;
    container: sysres / inline-size;
  }

  /* Four metrics — always an even grid so the last card never orphans
     onto a row of its own. Two up by default, four up once there is room. */
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px;
  }

  @container sysres (max-width: 400px) {
    .stats-grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  @container sysres (min-width: 820px) {
    .stats-grid {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
  }

  .stat-box {
    background: var(--surface-tint);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 14px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    min-height: 126px;
    gap: 8px;
    position: relative;
  }

  .stat-content-top {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .stat-bottom {
    margin-top: auto;
    width: 100%;
  }

  .stat-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .stat-label {
    font-size: 13px;
    font-weight: 600;
    text-transform: none; /* D-07: natural case, no uppercase fatigue */
    color: var(--fg-secondary);
    letter-spacing: -0.01em;
  }

  .stat-pct {
    font-size: 12px;
    font-weight: 600;
    font-family: var(--font-family-mono, monospace);
  }

  .stat-value {
    font-size: 22px;
    font-weight: 600;
    color: var(--fg-primary);
    line-height: 1.2;
    display: flex;
    align-items: baseline;
    gap: 4px;
    font-variant-numeric: tabular-nums;
  }

  .stat-unit {
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .res-sub {
    font-size: 11.5px;
    color: var(--fg-secondary);
    line-height: 1.4;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .res-sub-load {
    font-size: 11px;
    font-family: var(--font-family-mono, monospace);
    letter-spacing: -0.02em;
    font-variant-numeric: tabular-nums;
  }

  .stat-bar {
    width: 100%;
    height: 6px;
    background: var(--border);
    border-radius: 999px;
    overflow: hidden;
    margin-top: 4px;
  }

  .stat-bar-fill {
    height: 100%;
    border-radius: 999px;
    transition:
      width 0.4s cubic-bezier(0.4, 0, 0.2, 1),
      background-color 0.3s ease;
  }

  .sparkline-container {
    margin-top: 2px;
    height: 36px;
    position: relative;
    overflow: hidden;
    border-radius: var(--radius-sm);
  }

  .sparkline {
    width: 100%;
    height: 100%;
    display: block;
  }

  .sparkline-grad-from {
    stop-color: var(--accent);
    stop-opacity: 0.28;
  }

  .sparkline-grad-to {
    stop-color: var(--accent);
    stop-opacity: 0;
  }

  .sparkline-baseline {
    stroke: var(--border);
    stroke-width: 1;
  }

  .sparkline-line {
    fill: none;
    stroke: var(--accent);
    stroke-width: 1.75;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  @media (prefers-reduced-motion: reduce) {
    .stat-bar-fill {
      transition: none;
    }
  }

  .uptime-badge-row {
    margin-top: 2px;
    display: flex;
    align-items: center;
  }

  .uptime-badge {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11px;
    font-weight: 600;
    color: var(--success);
    background: color-mix(in srgb, var(--success) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--success) 28%, transparent);
    padding: 2px 7px;
    border-radius: var(--radius-sm);
  }

  .uptime-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--success);
  }
</style>
