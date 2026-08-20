<script lang="ts">
  import { t } from '../../i18n';
  import { trafficStream, formatTrafficSpeed, type TrafficState } from '../../lib/trafficStream';
  import Icon from '../../lib/components/Icon.svelte';
  import Card from '../Card.svelte';

  let { onSwitchTab } = $props<{
    onSwitchTab?: (tab: string) => void;
  }>();

  let traffic = $state<TrafficState>(trafficStream.getState());

  $effect(() => {
    const unsubscribe = trafficStream.subscribe((state) => {
      traffic = state;
    });
    return () => {
      unsubscribe();
    };
  });

  const formattedDown = $derived(formatTrafficSpeed(traffic.down));
  const formattedUp = $derived(formatTrafficSpeed(traffic.up));

  function handleTrafficClick() {
    if (onSwitchTab) onSwitchTab('traffic');
  }

  function handleConnectionsClick() {
    if (onSwitchTab) onSwitchTab('connections');
  }
</script>

<div class="traffic-telemetry-widget">
  <Card title={$t('nav.traffic')}>
    {#snippet actions()}
      <div class="telemetry-status">
        <span class="live-dot" class:live-dot-active={traffic.connected} aria-hidden="true"></span>
        <span class="status-lbl">{traffic.connected ? 'Live' : 'Offline'}</span>
      </div>
    {/snippet}

    <div class="telemetry-grid">
      <!-- Download Speed -->
      <button
        type="button"
        class="telemetry-box"
        onclick={handleTrafficClick}
        title={$t('traffic.goto_charts')}
      >
        <div class="box-head">
          <span class="ico-badge down-badge">
            <Icon name="arrow-down" size={14} color="var(--accent, #29c2f0)" />
          </span>
          <span class="box-label">{$t('traffic.download')}</span>
        </div>
        <div class="box-value tabular-nums">
          {formattedDown}
        </div>
        <div class="box-sub">
          {traffic.connected ? $t('traffic.inbound_flow') : $t('traffic.waiting_flow')}
        </div>
      </button>

      <!-- Upload Speed -->
      <button
        type="button"
        class="telemetry-box"
        onclick={handleTrafficClick}
        title={$t('traffic.goto_charts')}
      >
        <div class="box-head">
          <span class="ico-badge up-badge">
            <Icon name="upload" size={14} color="var(--success, #46d18a)" />
          </span>
          <span class="box-label">{$t('traffic.upload')}</span>
        </div>
        <div class="box-value tabular-nums">
          {formattedUp}
        </div>
        <div class="box-sub">
          {traffic.connected ? $t('traffic.outbound_flow') : $t('traffic.waiting_flow')}
        </div>
      </button>

      <!-- Active Connections -->
      <button
        type="button"
        class="telemetry-box"
        onclick={handleConnectionsClick}
        title={$t('traffic.goto_sessions')}
      >
        <div class="box-head">
          <span class="ico-badge conn-badge">
            <Icon name="connections" size={14} color="var(--warning, #f0b450)" />
          </span>
          <span class="box-label">{$t('dash.connections')}</span>
        </div>
        <div class="box-value tabular-nums">
          {traffic.connections}
        </div>
        <div class="box-sub">
          TCP {traffic.tcp_connections} · UDP {traffic.udp_connections}
        </div>
      </button>
    </div>
  </Card>
</div>

<style>
  .traffic-telemetry-widget {
    width: 100%;
  }

  .telemetry-status {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-secondary, #8fa3b8);
  }

  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background-color: var(--fg-dim, #63778a);
    transition:
      background-color 0.25s ease,
      box-shadow 0.25s ease;
  }

  .live-dot-active {
    background-color: var(--success, #46d18a);
    box-shadow: 0 0 6px rgba(70, 209, 138, 0.6);
    animation: livePulse 2s infinite;
  }

  @keyframes livePulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.4;
    }
  }

  .status-lbl {
    font-family: var(--font-family-mono, monospace);
  }

  .telemetry-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
    gap: 14px;
  }

  .telemetry-box {
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--border, rgba(255, 255, 255, 0.06));
    border-radius: var(--radius-md, 10px);
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    text-align: left;
    cursor: pointer;
    transition:
      background 0.15s ease,
      border-color 0.15s ease,
      transform 0.15s ease;
    width: 100%;
  }

  .telemetry-box:hover {
    background: rgba(255, 255, 255, 0.04);
    border-color: rgba(41, 194, 240, 0.3);
    transform: translateY(-1px);
  }

  .box-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .ico-badge {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border-radius: var(--radius-sm, 6px);
  }

  .down-badge {
    background: rgba(41, 194, 240, 0.12);
  }

  .up-badge {
    background: rgba(70, 209, 138, 0.12);
  }

  .conn-badge {
    background: rgba(240, 180, 80, 0.12);
  }

  .box-label {
    font-size: 13px;
    font-weight: 600;
    text-transform: none;
    color: var(--fg-secondary, #8fa3b8);
  }

  .box-value {
    font-size: 22px;
    font-weight: 600;
    color: var(--fg-primary, #ffffff);
    line-height: 1.2;
    font-family: var(--font-family-mono, monospace);
  }

  .tabular-nums {
    font-variant-numeric: tabular-nums;
  }

  .box-sub {
    font-size: 11.5px;
    color: var(--fg-muted, var(--fg-secondary, #8fa3b8));
    line-height: 1.4;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
