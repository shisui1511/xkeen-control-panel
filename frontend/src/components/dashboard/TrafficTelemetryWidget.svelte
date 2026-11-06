<script lang="ts">
  import { t } from '../../i18n';
  import { trafficStream, formatTrafficSpeed, type TrafficState } from '../../lib/trafficStream';
  import Icon from '../../lib/components/Icon.svelte';
  import Card from '../Card.svelte';
  import Section from '../Section.svelte';
  import { isMihomo, mihomoApiState, mihomoOfflineReason } from '../../stores';

  let {
    onSwitchTab
  }: {
    onSwitchTab?: (tab: string) => void;
  } = $props();

  // trafficStream соответствует контракту стора Svelte (подписчик сразу получает текущее
  // состояние, subscribe возвращает отписку), поэтому подписка идёт через $trafficStream.
  const traffic: TrafficState = $derived($trafficStream);

  const formattedDown = $derived(formatTrafficSpeed(traffic.down));
  const formattedUp = $derived(formatTrafficSpeed(traffic.up));

  // Число подключений даёт только Mihomo: пока его API не отвечает, ноль был бы не данными,
  // а отсутствием источника. Подпись причины — только когда активно именно ядро Mihomo.
  const connectionsLive = $derived($mihomoApiState === 'up');
  const offlineReasonKey = $derived(
    $isMihomo && $mihomoOfflineReason ? `dash.mihomo_offline_${$mihomoOfflineReason}` : null
  );

  function handleTrafficClick() {
    if (onSwitchTab) onSwitchTab('traffic');
  }

  function handleConnectionsClick() {
    if (onSwitchTab) onSwitchTab('connections');
  }
</script>

<div class="traffic-telemetry-widget">
  <Section title={$t('nav.traffic')}>
    {#snippet meta()}
      <div class="telemetry-status">
        <span class="live-dot" class:live-dot-active={traffic.connected} aria-hidden="true"></span>
        <span class="status-lbl"
          >{traffic.connected ? $t('traffic.live_badge') : $t('traffic.offline_badge')}</span
        >
      </div>
    {/snippet}

    <Card flush>
      <div class="telemetry-grid">
        <!-- Download Speed -->
        <button
          type="button"
          class="telemetry-box"
          onclick={handleTrafficClick}
          title={$t('traffic.goto_charts')}
        >
          <span class="box-head">
            <Icon name="arrow-down" size={14} color="var(--accent)" />
            <span class="box-label">{$t('traffic.download')}</span>
          </span>
          <span class="box-value tabular-nums">{formattedDown}</span>
          <span class="box-sub">
            {traffic.connected ? $t('traffic.inbound_flow') : $t('traffic.waiting_flow')}
          </span>
        </button>

        <!-- Upload Speed -->
        <button
          type="button"
          class="telemetry-box"
          onclick={handleTrafficClick}
          title={$t('traffic.goto_charts')}
        >
          <span class="box-head">
            <Icon name="upload" size={14} color="var(--success)" />
            <span class="box-label">{$t('traffic.upload')}</span>
          </span>
          <span class="box-value tabular-nums">{formattedUp}</span>
          <span class="box-sub">
            {traffic.connected ? $t('traffic.outbound_flow') : $t('traffic.waiting_flow')}
          </span>
        </button>

        <!-- Active Connections -->
        <button
          type="button"
          class="telemetry-box"
          onclick={handleConnectionsClick}
          title={$t('traffic.goto_sessions')}
        >
          <span class="box-head">
            <Icon name="connections" size={14} color="var(--warning)" />
            <span class="box-label">{$t('dash.connections')}</span>
          </span>
          <span class="box-value tabular-nums" data-testid="dash-connections-value">
            {connectionsLive ? traffic.connections : '—'}
          </span>
          {#if connectionsLive}
            <span
              class="box-sub tabular-nums"
              title="TCP {traffic.tcp_connections} · UDP {traffic.udp_connections}"
            >
              TCP {traffic.tcp_connections} · UDP {traffic.udp_connections}
            </span>
          {:else}
            <span
              class="box-sub"
              data-testid={offlineReasonKey ? 'dash-connections-offline' : undefined}
              title={offlineReasonKey ? $t(offlineReasonKey) : undefined}
            >
              {offlineReasonKey ? $t(offlineReasonKey) : '\u00a0'}
            </span>
          {/if}
        </button>
      </div>
    </Card>
  </Section>
</div>

<style>
  .traffic-telemetry-widget {
    width: 100%;
    container: telemetry / inline-size;
  }

  .telemetry-status {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background-color: var(--fg-dim);
    transition:
      background-color var(--transition-normal),
      box-shadow var(--transition-normal);
  }

  .live-dot-active {
    background-color: var(--success);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--success) 22%, transparent);
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

  /* Три метрики одной полосой; линии между ячейками — как у «Системных ресурсов». */
  .telemetry-grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  /* Телефон: три колонки остаются, ячейки плотнее */
  @container telemetry (max-width: 480px) {
    .telemetry-box {
      min-height: 0;
      padding: var(--spacing-3);
    }

    .box-value {
      font-size: var(--font-size-xl);
    }
  }

  .telemetry-box {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
    min-height: 108px;
    padding: var(--spacing-4) var(--card-pad);
    border: 0;
    border-radius: 0;
    background: transparent;
    box-shadow:
      1px 0 0 var(--border),
      0 1px 0 var(--border);
    color: inherit;
    font-family: inherit;
    text-align: left;
    cursor: pointer;
    transition: background-color var(--transition-fast);
  }

  .telemetry-box:hover {
    background: var(--surface-tint-faint);
  }

  .telemetry-box:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .box-head {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    margin-bottom: var(--spacing-1);
  }

  .box-label {
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .box-value {
    font-size: var(--font-size-2xl);
    font-weight: 600;
    line-height: 1.2;
    letter-spacing: -0.02em;
    color: var(--fg-primary);
    white-space: nowrap;
  }

  .tabular-nums {
    font-variant-numeric: tabular-nums;
  }

  .box-sub {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    line-height: 1.4;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
