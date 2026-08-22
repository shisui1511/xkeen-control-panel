<script lang="ts">
  import { t, tp } from '../../i18n';
  import type { ObservatoryStats, ObservatoryFilter } from '../../lib/proxyStats';

  interface Props {
    stats: ObservatoryStats;
    activeFilter: ObservatoryFilter;
    onFilterChange: (f: ObservatoryFilter) => void;
  }

  let { stats, activeFilter, onFilterChange }: Props = $props();
</script>

<div class="card obs-card">
  <div class="obs-head">
    <h2 class="card-title obs-title">{$t('proxies.observatory_title')}</h2>
  </div>
  <div class="obs-grid" class:has-unchecked={stats.unchecked > 0}>
    <!-- Total (Not interactive) -->
    <div class="stat-box obs-stat-box">
      <div class="stat-label">{$t('proxies.obs_total')}</div>
      <div class="obs-val-row">
        <span class="stat-value">{stats.totalNodes > 0 ? stats.totalNodes : '—'}</span>
        <span class="res-sub">
          {stats.proxyNodes}
          {$tp('proxies.obs_proxy_nodes', stats.proxyNodes)} + {stats.systemNodes}
          {$tp('proxies.obs_system_nodes', stats.systemNodes)}
        </span>
      </div>
    </div>

    <!-- Available (Healthy / Fast) -->
    <button
      type="button"
      class="stat-box obs-stat-box obs-stat-btn"
      aria-pressed={activeFilter === 'healthy'}
      onclick={() => onFilterChange(activeFilter === 'healthy' ? null : 'healthy')}
    >
      <div class="stat-label">{$t('proxies.obs_healthy')}</div>
      <div class="obs-val-row">
        <span class="stat-value ok">{stats.healthy}</span>
        <span class="res-sub">{$t('proxies.obs_healthy_sub')}</span>
      </div>
    </button>

    <!-- Degraded (Mid latency) -->
    <button
      type="button"
      class="stat-box obs-stat-box obs-stat-btn"
      aria-pressed={activeFilter === 'degraded'}
      onclick={() => onFilterChange(activeFilter === 'degraded' ? null : 'degraded')}
    >
      <div class="stat-label">{$t('proxies.obs_degraded')}</div>
      <div class="obs-val-row">
        <span class="stat-value warn">{stats.degraded}</span>
        <span class="res-sub">{$t('proxies.obs_degraded_sub')}</span>
      </div>
    </button>

    <!-- Down / Unreachable (Bad) -->
    <button
      type="button"
      class="stat-box obs-stat-box obs-stat-btn"
      aria-pressed={activeFilter === 'down'}
      onclick={() => onFilterChange(activeFilter === 'down' ? null : 'down')}
    >
      <div class="stat-label">{$t('proxies.obs_unreachable')}</div>
      <div class="obs-val-row">
        <span class="stat-value err">{stats.down}</span>
        <span class="res-sub">{$t('proxies.obs_unreachable_sub')}</span>
      </div>
    </button>

    <!-- Unchecked (Optional / Conditional) -->
    {#if stats.unchecked > 0}
      <div class="stat-box obs-stat-box">
        <div class="stat-label">{$t('proxies.obs_unchecked')}</div>
        <div class="obs-val-row">
          <span class="stat-value">{stats.unchecked}</span>
          <span class="res-sub">{$t('proxies.obs_unchecked_sub')}</span>
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .obs-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg, 10px);
    margin-bottom: 16px;
    overflow: hidden;
    box-shadow: var(--shadow-sm);
    padding: 0;
  }

  .obs-head {
    display: flex;
    align-items: center;
    padding: 6px 14px;
    background: linear-gradient(
      135deg,
      var(--bg-group-head-from, rgba(20, 51, 79, 0.6)),
      var(--bg-group-head-to, rgba(16, 42, 68, 0.7))
    );
    border-bottom: 1px solid var(--border-strong, var(--border));
  }

  .obs-head .card-title.obs-title {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.14em;
    text-transform: none;
    color: var(--fg-secondary);
    margin: 0;
    padding: 0;
    border: 0;
  }

  .obs-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    margin: 0;
    border: 0;
  }

  .obs-grid.has-unchecked {
    grid-template-columns: repeat(5, 1fr);
  }

  .obs-stat-box {
    padding: 8px 14px 10px;
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    justify-content: center;
    background: transparent;
  }

  .obs-stat-box:last-child {
    border-right: 0;
  }

  .obs-stat-box .stat-label {
    font-size: 9.5px;
    letter-spacing: 0.12em;
    margin-bottom: 2px;
    line-height: 1.2;
  }

  .obs-val-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
  }

  .obs-stat-box .stat-value {
    font-size: 17px;
    font-weight: 700;
    line-height: 1.15;
  }

  .obs-stat-box .stat-value.ok {
    color: var(--success);
  }

  .obs-stat-box .stat-value.warn {
    color: var(--warning);
  }

  .obs-stat-box .stat-value.err {
    color: var(--danger);
  }

  .obs-stat-box .res-sub {
    font-size: 11px;
    margin-top: 0;
    line-height: 1.2;
    white-space: nowrap;
    color: var(--fg-muted);
  }

  .obs-stat-btn {
    cursor: pointer;
    background: none;
    border: none;
    text-align: left;
    font: inherit;
    color: inherit;
    border-radius: 0;
    transition: background 0.15s ease;
  }

  .obs-stat-btn:hover {
    background: var(--hover);
  }

  .obs-stat-btn[aria-pressed='true'] {
    background: var(--accent-soft, var(--hover));
    box-shadow: inset 0 0 0 1px var(--accent);
  }

  .obs-stat-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  @media (max-width: 768px) {
    .obs-grid {
      grid-template-columns: repeat(2, 1fr);
    }
    .obs-grid.has-unchecked {
      grid-template-columns: repeat(2, 1fr);
    }
    .obs-stat-box:nth-child(2) {
      border-right: 0;
    }
    .obs-stat-box:nth-child(1),
    .obs-stat-box:nth-child(2) {
      border-bottom: 1px solid var(--border);
    }
  }

  @media (max-width: 480px) {
    .obs-stat-box {
      padding: 6px 10px 8px;
    }
    .obs-stat-box .stat-value {
      font-size: 15px;
    }
    .obs-stat-box .res-sub {
      font-size: 10px;
    }
  }
</style>
