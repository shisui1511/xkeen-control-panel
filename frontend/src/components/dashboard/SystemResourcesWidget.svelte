<script lang="ts">
  import { t } from '../../i18n';
  import { formatBytes } from '../../lib/format';
  import Card from '../Card.svelte';
  import Section from '../Section.svelte';
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
  }: {
    systemStats: SystemStats | null;
    loadHistory?: number[];
    sparklineData?: { line: string; fill: string } | null;
  } = $props();

  // Цвет несёт смысл только у порогов: в норме полоса акцентная, а процент нейтральный.
  function getUsageColor(pct: number): string {
    if (pct >= 90) return 'var(--danger)';
    if (pct >= 75) return 'var(--warning)';
    return 'var(--accent)';
  }

  function getPctColor(pct: number): string {
    if (pct >= 75) return getUsageColor(pct);
    return 'var(--fg-secondary)';
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
  <Section title={$t('dash.system_stats')}>
    <Card flush>
      {#if !systemStats}
        <div class="res-grid">
          {#each [1, 2, 3, 4] as _, i (i)}
            <div class="res-cell">
              <Skeleton type="rect" width="80px" height="14px" />
              <Skeleton type="rect" width="120px" height="28px" />
              <Skeleton type="rect" width="100%" height="6px" />
            </div>
          {/each}
        </div>
      {:else}
        <div class="res-grid">
          <!-- Storage (Disk) -->
          {#if systemStats.disk}
            <div class="res-cell">
              <div class="res-head">
                <span class="res-label">{$t('dash.disk')}</span>
                <span class="res-pct" style="color: {getPctColor(diskUsedPct)};"
                  >{diskUsedPct.toFixed(1)}%</span
                >
              </div>
              <div class="res-value">
                {formatBytes(systemStats.disk.free)}
                <span class="res-unit">{$t('dash.free_suffix')}</span>
              </div>
              <div
                class="res-note"
                title={$t('dash.disk_of_total', { total: formatBytes(systemStats.disk.total) })}
              >
                {$t('dash.disk_of_total', { total: formatBytes(systemStats.disk.total) })}
              </div>
              <div class="res-bar">
                <div
                  class="res-bar-fill"
                  style="width: {Math.min(diskUsedPct, 100).toFixed(
                    1
                  )}%; background: {diskBarColor};"
                ></div>
              </div>
            </div>
          {/if}

          <!-- RAM -->
          <div class="res-cell">
            <div class="res-head">
              <span class="res-label">{$t('dash.ram')}</span>
              <span class="res-pct" style="color: {getPctColor(ramUsedPct)};"
                >{ramUsedPct.toFixed(1)}%</span
              >
            </div>
            <div class="res-value">
              {ramUsedMb}
              <span class="res-unit">{$t('dash.unit_mb')}</span>
            </div>
            <div class="res-note" title={$t('dash.ram_of_total', { total: ramTotalMb })}>
              {$t('dash.ram_of_total', { total: ramTotalMb })}
            </div>
            <div class="res-bar">
              <div
                class="res-bar-fill"
                style="width: {Math.min(ramUsedPct, 100).toFixed(1)}%; background: {ramBarColor};"
              ></div>
            </div>
          </div>

          <!-- Load Average -->
          <div class="res-cell">
            <div class="res-head">
              <span class="res-label">{$t('dash.load')}</span>
              {#if loadPerCorePct !== null}
                <span class="res-pct" style="color: {getPctColor(loadPerCorePct)};"
                  >{loadPerCorePct.toFixed(0)}%</span
                >
              {/if}
            </div>
            <div class="res-value">
              {systemStats.load[0].toFixed(2)}
            </div>
            <div
              class="res-note res-note-mono"
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

          <!-- Uptime -->
          <div class="res-cell">
            <div class="res-head">
              <span class="res-label">{$t('dash.uptime')}</span>
            </div>
            <div class="res-value">
              {$t('dash.uptime_dhm', {
                days: systemStats.uptime.days,
                hours: systemStats.uptime.hours,
                minutes: systemStats.uptime.minutes
              })}
            </div>
            {#if systemStats.boot_time}
              <div
                class="res-note"
                title={$t('dash.uptime_since', { time: systemStats.boot_time })}
              >
                {$t('dash.uptime_since', { time: systemStats.boot_time })}
              </div>
            {/if}
          </div>
        </div>
      {/if}
    </Card>
  </Section>
</div>

<style>
  .system-resources-card {
    width: 100%;
    container: sysres / inline-size;
  }

  /* Четыре метрики — одна полоса в общей карточке, ячейки разделены тонкими
     линиями. Узко — сетка 2×2, совсем узко — столбик. Каждая ячейка рисует
     линию справа и снизу тенью; у внешних краёв её срезает overflow карточки,
     поэтому линии не двоятся и не зависят от числа колонок. */
  .res-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  @container sysres (max-width: 280px) {
    .res-grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  /* Телефон: сетка 2×2 остаётся, ячейки плотнее */
  @container sysres (max-width: 440px) {
    .res-cell {
      min-height: 0;
      padding: var(--spacing-3) var(--spacing-4);
    }

    .res-value {
      font-size: var(--font-size-xl);
    }
  }

  @container sysres (min-width: 820px) {
    .res-grid {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
  }

  .res-cell {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
    min-height: 128px;
    padding: var(--spacing-4) var(--card-pad);
    box-shadow:
      1px 0 0 var(--border),
      0 1px 0 var(--border);
  }

  .res-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--spacing-2);
    margin-bottom: var(--spacing-1);
  }

  .res-label {
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .res-pct {
    font-size: var(--font-size-xs);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }

  .res-value {
    display: flex;
    align-items: baseline;
    gap: var(--spacing-1);
    font-size: var(--font-size-2xl);
    font-weight: 600;
    line-height: 1.2;
    letter-spacing: -0.02em;
    color: var(--fg-primary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .res-unit {
    font-size: var(--font-size-sm);
    font-weight: 500;
    letter-spacing: 0;
    color: var(--fg-dim);
  }

  .res-note {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    line-height: 1.4;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .res-note-mono {
    font-family: var(--font-family-mono);
    font-variant-numeric: tabular-nums;
  }

  .res-bar {
    width: 100%;
    height: 4px;
    margin-top: auto;
    border-radius: var(--radius-full);
    background: var(--surface-tint-strong);
    overflow: hidden;
  }

  .res-bar-fill {
    height: 100%;
    border-radius: var(--radius-full);
    transition:
      width var(--duration-slow) var(--ease-out),
      background-color var(--transition-normal);
  }

  .sparkline-container {
    margin-top: auto;
    height: 32px;
    position: relative;
    overflow: hidden;
  }

  .sparkline {
    width: 100%;
    height: 100%;
    display: block;
  }

  .sparkline-grad-from {
    stop-color: var(--accent);
    stop-opacity: 0.22;
  }

  .sparkline-grad-to {
    stop-color: var(--accent);
    stop-opacity: 0;
  }

  .sparkline-line {
    fill: none;
    stroke: var(--accent);
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  @media (prefers-reduced-motion: reduce) {
    .res-bar-fill {
      transition: none;
    }
  }
</style>
