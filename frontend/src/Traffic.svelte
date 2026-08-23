<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, currentLang } from './i18n';
  import { showToast, showConfirm } from './stores';
  import { apiFetchJSON } from './lib/api';

  interface TrafficPoint {
    up: number;
    down: number;
    time: number;
  }

  interface ClientTraffic {
    ip: string;
    upload: number;
    download: number;
    total_bytes: number;
    active_connections: number;
  }

  interface Peaks {
    peak_hour_up: number;
    peak_hour_down: number;
    peak_day_up: number;
    peak_day_down: number;
    peak_week_up: number;
    peak_week_down: number;
    peak_hour_up_time?: number;
    peak_hour_down_time?: number;
    peak_day_up_time?: number;
    peak_day_down_time?: number;
    peak_week_up_time?: number;
    peak_week_down_time?: number;
    hour_start: number;
    day_start: number;
    week_start: number;
  }

  interface Props {
    onSwitchTab?: (tab: string) => void;
  }

  let { onSwitchTab = () => {} }: Props = $props();

  type Timeframe = '1m' | '5m' | '15m' | '1h';
  let activeTimeframe: Timeframe = $state('1m');

  const timeframePoints: Record<Timeframe, number> = {
    '1m': 60,
    '5m': 300,
    '15m': 900,
    '1h': 3600
  };

  let allTrafficData: TrafficPoint[] = $state([]);
  let frozenPoints: TrafficPoint[] | null = $state(null);
  let isPaused = $state(false);
  let showDownload = $state(true);
  let showUpload = $state(true);

  let ws: WebSocket | null = null;
  let connected = $state(false);
  let totalUp = $state(0);
  let totalDown = $state(0);
  let sessionUp = $state(0);
  let sessionDown = $state(0);
  let lastTickTime = 0;

  // Active connections
  let activeConnectionsCount = $state(0);
  let tcpConnectionsCount = $state(0);
  let udpConnectionsCount = $state(0);

  // Top clients
  let topClients: ClientTraffic[] = $state([]);
  // Sum of total_bytes across ALL LAN clients (server-side, before truncation to top 5)
  let totalClientsBytes = $state(0);

  // Connection history for stats
  const CONN_HISTORY_MAX = 3600; // 1 hour at 1 sample/sec
  let connHistory: { ts: number; count: number }[] = $state([]);

  let peaks: Peaks = $state({
    peak_hour_up: 0,
    peak_hour_down: 0,
    peak_day_up: 0,
    peak_day_down: 0,
    peak_week_up: 0,
    peak_week_down: 0,
    peak_hour_up_time: 0,
    peak_hour_down_time: 0,
    peak_day_up_time: 0,
    peak_day_down_time: 0,
    peak_week_up_time: 0,
    peak_week_down_time: 0,
    hour_start: 0,
    day_start: 0,
    week_start: 0
  });

  // Crosshair & tooltip state
  let hoveredPoint: TrafficPoint | null = $state(null);
  let crosshairSvgX = $state(0);
  let tooltipX = $state(0);
  let tooltipY = $state(0);

  let connDeltaPerMin = $derived.by(() => {
    if (connHistory.length < 2) return null;
    const now = connHistory[connHistory.length - 1];
    if (now.ts - connHistory[0].ts < 60000) return null;
    let minuteAgo = connHistory[0];
    for (let i = connHistory.length - 1; i >= 0; i--) {
      if (now.ts - connHistory[i].ts >= 60000) {
        minuteAgo = connHistory[i];
        break;
      }
    }
    return now.count - minuteAgo.count;
  });

  let connPeakHour = $derived(connHistory.reduce((max, h) => (h.count > max ? h.count : max), 0));

  let visibleTrafficData = $derived.by(() => {
    const maxPts = timeframePoints[activeTimeframe];
    return allTrafficData.slice(-maxPts);
  });

  let totalClientsTraffic = $derived.by(() => {
    // Prefer the server-computed total across ALL clients so the per-client
    // share reflects the real network, not just the visible top-5 subset.
    if (totalClientsBytes > 0) return totalClientsBytes;
    return topClients.reduce((sum, c) => sum + c.total_bytes, 0) || 1;
  });

  function formatSpeed(bytesPerSecond: number): string {
    if (bytesPerSecond === 0) return '0 B/s';
    const k = 1024;
    const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
    const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k));
    return parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1) return `${bytes.toFixed(0)} B`;
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.min(sizes.length - 1, Math.floor(Math.log(bytes) / Math.log(k)));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  function formatTime(ts?: number): string {
    if (!ts || ts === 0) return '—';
    const d = new Date(ts * 1000);
    const hours = String(d.getHours()).padStart(2, '0');
    const mins = String(d.getMinutes()).padStart(2, '0');
    return `${hours}:${mins}`;
  }

  function formatTooltipTime(ts: number): string {
    const d = new Date(ts);
    const hours = String(d.getHours()).padStart(2, '0');
    const mins = String(d.getMinutes()).padStart(2, '0');
    const secs = String(d.getSeconds()).padStart(2, '0');
    return `${hours}:${mins}:${secs}`;
  }

  function togglePause() {
    isPaused = !isPaused;
    if (isPaused) {
      frozenPoints = [...visibleTrafficData];
    } else {
      frozenPoints = null;
      hoveredPoint = null;
    }
  }

  let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  let reconnectDelay = 1000;
  const MAX_RECONNECT_DELAY = 16000;

  function connect() {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/api/traffic/ws`;

    ws = new WebSocket(url);

    ws.onopen = () => {
      connected = true;
      lastTickTime = 0;
      reconnectDelay = 1000;
    };

    ws.onmessage = (event) => {
      if (typeof document !== 'undefined' && document.hidden) return;
      try {
        const data = JSON.parse(event.data);
        const upSpeed = data.up || 0;
        const downSpeed = data.down || 0;
        const now = Date.now();

        // Keep buffering into allTrafficData and bookkeeping lastTickTime even
        // while paused — the main chart resumes from this buffer via
        // frozenPoints, and lastTickTime must stay current so a resumed
        // session doesn't multiply the instantaneous speed by the entire
        // paused interval on the next tick.
        allTrafficData.push({
          up: upSpeed,
          down: downSpeed,
          time: now
        });

        // Retain max 3600 points (1 hour buffer)
        if (allTrafficData.length > 3600) {
          allTrafficData = allTrafficData.slice(-3600);
        } else {
          allTrafficData = allTrafficData;
        }

        const elapsedSec = lastTickTime > 0 ? (now - lastTickTime) / 1000 : 0;
        lastTickTime = now;

        // "Paused" implies the whole live view is frozen for inspection —
        // skip every other state update so KPI cards, sparklines, top
        // clients and peaks stop changing while the badge reads "Paused".
        if (!isPaused) {
          totalUp = upSpeed;
          totalDown = downSpeed;

          if (elapsedSec > 0) {
            sessionUp += upSpeed * elapsedSec;
            sessionDown += downSpeed * elapsedSec;
          }

          activeConnectionsCount = data.connections || 0;
          tcpConnectionsCount = data.tcp_connections || 0;
          udpConnectionsCount = data.udp_connections || 0;

          connHistory.push({ ts: now, count: activeConnectionsCount });
          if (connHistory.length > CONN_HISTORY_MAX) connHistory.shift();
          connHistory = connHistory;

          if (data.peaks) {
            peaks = data.peaks;
          }
          if (data.top_clients) {
            topClients = data.top_clients;
          }
          if (typeof data.total_clients_bytes === 'number') {
            totalClientsBytes = data.total_clients_bytes;
          }
        }
      } catch (e) {
        // ignore
      }
    };

    ws.onclose = () => {
      connected = false;
      scheduleReconnect();
    };

    ws.onerror = () => {
      connected = false;
    };
  }

  function scheduleReconnect() {
    if (ws && ws.readyState !== WebSocket.CLOSED) return;
    if (reconnectTimeout) return;

    reconnectTimeout = setTimeout(() => {
      reconnectTimeout = null;
      reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY);
      connect();
    }, reconnectDelay);
  }

  function disconnect() {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    if (ws) {
      ws.onclose = null;
      ws.onerror = null;
      ws.close();
      ws = null;
    }
    connected = false;
  }

  async function resetStatistics() {
    if (
      !(await showConfirm({
        title: $t('traffic.reset_title'),
        consequence: $t('traffic.reset_confirm'),
        variant: 'danger',
        confirmLabel: $t('app.reset')
      }))
    )
      return;
    try {
      await apiFetchJSON('/api/traffic/reset', { method: 'POST' });
      sessionUp = 0;
      sessionDown = 0;
      allTrafficData = [];
      frozenPoints = null;
      hoveredPoint = null;
      peaks = {
        peak_hour_up: 0,
        peak_hour_down: 0,
        peak_day_up: 0,
        peak_day_down: 0,
        peak_week_up: 0,
        peak_week_down: 0,
        peak_hour_up_time: 0,
        peak_hour_down_time: 0,
        peak_day_up_time: 0,
        peak_day_down_time: 0,
        peak_week_up_time: 0,
        peak_week_down_time: 0,
        hour_start: 0,
        day_start: 0,
        week_start: 0
      };
      showToast('success', $t('app.success'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
    }
  }

  function handleVisibilityChange() {
    if (!document.hidden) {
      lastTickTime = 0; // avoid huge elapsedSec spike from messages dropped while hidden
      if (!ws || ws.readyState !== WebSocket.OPEN) {
        connect();
      }
    }
  }

  function handlePointerMove(e: PointerEvent) {
    const currentPoints = frozenPoints || visibleTrafficData;
    if (currentPoints.length < 2) return;
    const svg = e.currentTarget as SVGSVGElement;
    const rect = svg.getBoundingClientRect();
    const relX = Math.max(0, Math.min(rect.width, e.clientX - rect.left));
    const fraction = relX / rect.width;
    const idx = Math.min(
      currentPoints.length - 1,
      Math.max(0, Math.round(fraction * (currentPoints.length - 1)))
    );
    if (idx >= 0 && idx < currentPoints.length) {
      hoveredPoint = currentPoints[idx];
      crosshairSvgX = (idx / (currentPoints.length - 1)) * 1000;
      tooltipX = relX;
      tooltipY = Math.max(20, Math.min(rect.height - 70, e.clientY - rect.top));
    }
  }

  function handlePointerLeave() {
    hoveredPoint = null;
  }

  onMount(() => {
    connect();
    window.addEventListener('visibilitychange', handleVisibilityChange);
  });

  onDestroy(() => {
    disconnect();
    window.removeEventListener('visibilitychange', handleVisibilityChange);
  });

  // SVG Chart path generator
  let chartData = $derived.by(() => {
    const points = frozenPoints || visibleTrafficData;
    if (points.length < 2) {
      return {
        dLine: '',
        dArea: '',
        uLine: '',
        uArea: '',
        maxSpeed: '0 B/s',
        maxVal: 1024,
        pointsCount: 0
      };
    }

    const maxVal = Math.max(...points.map((p) => Math.max(p.up, p.down))) || 1024;
    const width = 1000;
    const height = 240;
    const count = points.length;
    const step = width / (count - 1);

    const getDownloadY = (val: number) => height - (val / maxVal) * (height - 24);
    const getUploadY = (valUp: number, valDown: number) => {
      const y = height - (valUp / maxVal) * (height - 24);
      return valUp === valDown ? y - 1.5 : y;
    };

    // Download path
    let dLinePath = `M 0 ${getDownloadY(points[0].down)}`;
    for (let i = 1; i < count; i++) {
      dLinePath += ` L ${i * step} ${getDownloadY(points[i].down)}`;
    }
    const dAreaPath = `${dLinePath} L ${width} ${height} L 0 ${height} Z`;

    // Upload path
    let uLinePath = `M 0 ${getUploadY(points[0].up, points[0].down)}`;
    for (let i = 1; i < count; i++) {
      uLinePath += ` L ${i * step} ${getUploadY(points[i].up, points[i].down)}`;
    }
    const uAreaPath = `${uLinePath} L ${width} ${height} L 0 ${height} Z`;

    return {
      dLine: dLinePath,
      dArea: dAreaPath,
      uLine: uLinePath,
      uArea: uAreaPath,
      maxSpeed: formatSpeed(maxVal),
      maxVal,
      pointsCount: count
    };
  });

  // Card Sparkline generator (last 20 points)
  let sparklines = $derived.by(() => {
    // Mirror the main chart's frozenPoints behavior so the sparkline cards
    // actually stop moving while paused instead of continuing to animate
    // from the live allTrafficData buffer.
    const points = (frozenPoints || allTrafficData).slice(-20);
    if (points.length < 2) {
      return { uLine: '', uArea: '', dLine: '', dArea: '' };
    }

    const maxUp = Math.max(...points.map((p) => p.up)) || 1;
    const maxDown = Math.max(...points.map((p) => p.down)) || 1;
    const width = 200;
    const height = 42;
    const step = width / (points.length - 1);
    const startX = 0;

    // Up
    let uLine = `M ${startX} ${height - (points[0].up / maxUp) * (height - 8)}`;
    for (let i = 1; i < points.length; i++) {
      uLine += ` L ${startX + i * step} ${height - (points[i].up / maxUp) * (height - 8)}`;
    }
    const uArea = `${uLine} L 200 42 L ${startX} 42 Z`;

    // Down
    let dLine = `M ${startX} ${height - (points[0].down / maxDown) * (height - 8)}`;
    for (let i = 1; i < points.length; i++) {
      dLine += ` L ${startX + i * step} ${height - (points[i].down / maxDown) * (height - 8)}`;
    }
    const dArea = `${dLine} L 200 42 L ${startX} 42 Z`;

    return { uLine, uArea, dLine, dArea };
  });

  let timeLabels = $derived.by(() => {
    const s = $t('traffic.sec');
    const m = $t('traffic.per_min');
    switch (activeTimeframe) {
      case '1m':
        return [`-60 ${s}`, `-45 ${s}`, `-30 ${s}`, `-15 ${s}`, $t('traffic.now')];
      case '5m':
        return [`-5 ${m}`, `-3.5 ${m}`, `-2.5 ${m}`, `-1 ${m}`, $t('traffic.now')];
      case '15m':
        return [`-15 ${m}`, `-11 ${m}`, `-7.5 ${m}`, `-3.5 ${m}`, $t('traffic.now')];
      case '1h':
        return [`-60 ${m}`, `-45 ${m}`, `-30 ${m}`, `-15 ${m}`, $t('traffic.now')];
    }
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_observability')}
        <span class="crumb-sep">›</span>
        {$t('traffic.title')}
      </div>
      <h1>{$t('traffic.title')}</h1>
      <p class="sub">{$t('traffic.realtime')}</p>
    </div>
    <div class="ph-actions">
      <span
        class="badge-live-indicator"
        class:is-live={connected && !isPaused}
        class:is-paused={isPaused}
        class:is-offline={!connected}
      >
        <span class="live-dot"></span>
        {#if !connected}
          {$t('traffic.offline_badge')}
        {:else if isPaused}
          {$t('traffic.paused_badge')}
        {:else}
          {$t('traffic.live_badge')}
        {/if}
      </span>

      <button
        type="button"
        class="btn btn-secondary btn-sm"
        class:btn-active={isPaused}
        onclick={togglePause}
        aria-label={isPaused ? $t('traffic.resume_action') : $t('traffic.pause_action')}
      >
        {#if isPaused}
          <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
            <polygon points="5 3 19 12 5 21 5 3" />
          </svg>
          {$t('traffic.resume_action')}
        {:else}
          <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
            <rect x="6" y="4" width="4" height="16" />
            <rect x="14" y="4" width="4" height="16" />
          </svg>
          {$t('traffic.pause_action')}
        {/if}
      </button>

      <button type="button" class="btn btn-secondary btn-sm btn-reset" onclick={resetStatistics}>
        {$t('traffic.reset_stats')}
      </button>
    </div>
  </div>

  <!-- Standard Order KPI Grid: 1. Download (Left), 2. Upload (Center), 3. Connections (Right) -->
  <div class="traffic-stats-grid mb-2">
    <!-- Download Card (TRAF-01: First from left) -->
    <div class="card stat-card-spark">
      <div class="stat-card-content">
        <div class="stat-label">
          <span class="sw download-bg"></span>
          {$t('traffic.download')}
        </div>
        <div class="stat-value download-color">{formatSpeed(totalDown)}</div>
        <div class="stat-session">
          {$t('traffic.session_stat', { bytes: formatBytes(sessionDown) })}
        </div>
      </div>
      {#if allTrafficData.length >= 2}
        <svg
          class="sparkline"
          viewBox="0 0 200 42"
          preserveAspectRatio="none"
          role="img"
          aria-label={$t('traffic.download_sparkline')}
        >
          <defs>
            <linearGradient id="sg-download" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.4" />
              <stop offset="100%" stop-color="var(--accent)" stop-opacity="0" />
            </linearGradient>
          </defs>
          <path d={sparklines.dArea} fill="url(#sg-download)" />
          <path d={sparklines.dLine} fill="none" stroke="var(--accent)" stroke-width="1.5" />
        </svg>
      {/if}
    </div>

    <!-- Upload Card (TRAF-01: Second) -->
    <div class="card stat-card-spark">
      <div class="stat-card-content">
        <div class="stat-label">
          <span class="sw upload-bg"></span>
          {$t('traffic.upload')}
        </div>
        <div class="stat-value upload-color">{formatSpeed(totalUp)}</div>
        <div class="stat-session">
          {$t('traffic.session_stat', { bytes: formatBytes(sessionUp) })}
        </div>
      </div>
      {#if allTrafficData.length >= 2}
        <svg
          class="sparkline"
          viewBox="0 0 200 42"
          preserveAspectRatio="none"
          role="img"
          aria-label={$t('traffic.upload_sparkline')}
        >
          <defs>
            <linearGradient id="sg-upload" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="var(--success)" stop-opacity="0.4" />
              <stop offset="100%" stop-color="var(--success)" stop-opacity="0" />
            </linearGradient>
          </defs>
          <path d={sparklines.uArea} fill="url(#sg-upload)" />
          <path d={sparklines.uLine} fill="none" stroke="var(--success)" stroke-width="1.5" />
        </svg>
      {/if}
    </div>

    <!-- Active Connections Card (TRAF-01: Third) -->
    <div class="card stat-card-normal">
      <div class="stat-card-content">
        <div class="stat-label">{$t('traffic.active_connections')}</div>
        <div class="stat-value active-connections-color">{activeConnectionsCount}</div>
        <div class="stat-session conns-protocols">
          {tcpConnectionsCount} TCP · {udpConnectionsCount} UDP
        </div>
        <div class="stat-session conns-dynamics">
          {$t('traffic.dynamics')}
          {#if connDeltaPerMin === null}
            — / {$t('traffic.per_min')}
          {:else}
            <span class:delta-up={connDeltaPerMin > 0} class:delta-down={connDeltaPerMin < 0}>
              {connDeltaPerMin >= 0 ? '+' : ''}{connDeltaPerMin} / {$t('traffic.per_min')}
            </span>
          {/if}
          · {$t('traffic.hour_peak')}{connPeakHour}
        </div>
      </div>
    </div>
  </div>

  <!-- Main Chart Card with Toolbar (TRAF-02, TRAF-03, TRAF-04) -->
  <div class="card chart-card">
    <div class="chart-header-toolbar">
      <!-- Interactive Legend with filter toggle -->
      <div class="chart-legend">
        <button
          type="button"
          class="key-btn"
          class:key-inactive={!showDownload}
          onclick={() => (showDownload = !showDownload)}
          title={$t('traffic.download')}
        >
          <span class="sw download-bg"></span>
          {$t('traffic.download')}
        </button>
        <button
          type="button"
          class="key-btn"
          class:key-inactive={!showUpload}
          onclick={() => (showUpload = !showUpload)}
          title={$t('traffic.upload')}
        >
          <span class="sw upload-bg"></span>
          {$t('traffic.upload')}
        </button>
      </div>

      <!-- Timeframe Switcher (TRAF-02) -->
      <div class="timeframe-picker">
        <button
          type="button"
          class="tf-pill"
          class:active={activeTimeframe === '1m'}
          onclick={() => (activeTimeframe = '1m')}
        >
          {$t('traffic.timeframe_1m')}
        </button>
        <button
          type="button"
          class="tf-pill"
          class:active={activeTimeframe === '5m'}
          onclick={() => (activeTimeframe = '5m')}
        >
          {$t('traffic.timeframe_5m')}
        </button>
        <button
          type="button"
          class="tf-pill"
          class:active={activeTimeframe === '15m'}
          onclick={() => (activeTimeframe = '15m')}
        >
          {$t('traffic.timeframe_15m')}
        </button>
        <button
          type="button"
          class="tf-pill"
          class:active={activeTimeframe === '1h'}
          onclick={() => (activeTimeframe = '1h')}
        >
          {$t('traffic.timeframe_1h')}
        </button>
      </div>
    </div>

    <div class="chart-area-wrapper">
      {#if chartData.pointsCount < 2}
        <div class="chart-empty">
          <span class="spinner"></span>
          <span class="chart-empty-title">{$t('traffic.waiting')}</span>
          <p class="chart-empty-sub">{$t('traffic.empty_state_body')}</p>
        </div>
      {:else}
        <div class="chart-y-axis">
          <span class="y-label">{chartData.maxSpeed}</span>
          <span class="y-label">{formatSpeed(Math.round(chartData.maxVal * 0.66))}</span>
          <span class="y-label">{formatSpeed(Math.round(chartData.maxVal * 0.33))}</span>
          <span class="y-label">0 B/s</span>
        </div>
        <div class="chart-svg-container">
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <svg
            viewBox="0 0 1000 240"
            preserveAspectRatio="none"
            class="main-traffic-svg"
            onpointermove={handlePointerMove}
            onpointerleave={handlePointerLeave}
            role="img"
            aria-label={$t('traffic.main_chart')}
          >
            <defs>
              <linearGradient id="cg-download-main" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.3" />
                <stop offset="100%" stop-color="var(--accent)" stop-opacity="0.0" />
              </linearGradient>
              <linearGradient id="cg-upload-main" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--success)" stop-opacity="0.25" />
                <stop offset="100%" stop-color="var(--success)" stop-opacity="0.0" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line
              x1="0"
              y1="60"
              x2="1000"
              y2="60"
              stroke="var(--border)"
              opacity="0.4"
              stroke-dasharray="4"
            />
            <line
              x1="0"
              y1="120"
              x2="1000"
              y2="120"
              stroke="var(--border)"
              opacity="0.4"
              stroke-dasharray="4"
            />
            <line
              x1="0"
              y1="180"
              x2="1000"
              y2="180"
              stroke="var(--border)"
              opacity="0.4"
              stroke-dasharray="4"
            />

            <!-- Download Path (TRAF-01 & TRAF-03) -->
            {#if showDownload}
              <path d={chartData.dArea} fill="url(#cg-download-main)" />
              <path d={chartData.dLine} fill="none" stroke="var(--accent)" stroke-width="2" />
            {/if}

            <!-- Upload Path (TRAF-01 & TRAF-03) -->
            {#if showUpload}
              <path d={chartData.uArea} fill="url(#cg-upload-main)" />
              <path d={chartData.uLine} fill="none" stroke="var(--success)" stroke-width="2" />
            {/if}

            <!-- Crosshair Interactive Line (TRAF-03) -->
            {#if hoveredPoint !== null}
              <line
                x1={crosshairSvgX}
                y1="0"
                x2={crosshairSvgX}
                y2="240"
                stroke="var(--fg-primary)"
                stroke-width="1.5"
                stroke-dasharray="3 3"
                opacity="0.75"
              />
              {#if showDownload}
                <circle
                  cx={crosshairSvgX}
                  cy={240 - (hoveredPoint.down / chartData.maxVal) * 216}
                  r="4"
                  fill="var(--accent)"
                  stroke="#fff"
                  stroke-width="1.5"
                />
              {/if}
              {#if showUpload}
                <circle
                  cx={crosshairSvgX}
                  cy={240 - (hoveredPoint.up / chartData.maxVal) * 216}
                  r="4"
                  fill="var(--success)"
                  stroke="#fff"
                  stroke-width="1.5"
                />
              {/if}
            {/if}
          </svg>

          <!-- Floating Interactive Tooltip (TRAF-03) -->
          {#if hoveredPoint !== null}
            <div
              class="chart-tooltip"
              style="left: {tooltipX}px; top: {tooltipY}px; transform: translate({tooltipX > 500
                ? '-110%'
                : '10%'}, -50%);"
            >
              <div class="tt-time">{formatTooltipTime(hoveredPoint.time)}</div>
              <div class="tt-row tt-down">
                <span class="tt-label">↓ {$t('traffic.download')}:</span>
                <span class="tt-val">{formatSpeed(hoveredPoint.down)}</span>
              </div>
              <div class="tt-row tt-up">
                <span class="tt-label">↑ {$t('traffic.upload')}:</span>
                <span class="tt-val">{formatSpeed(hoveredPoint.up)}</span>
              </div>
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <div class="chart-x">
      {#each timeLabels as label}
        <span>{label}</span>
      {/each}
    </div>
  </div>

  <!-- Bottom Analytics Grid (TRAF-05 Peak Load & TRAF-06 Top Clients) -->
  <div class="traffic-analytics-grid mt-3">
    <!-- Peak Load Cards (TRAF-05: timestamps & separated rows) -->
    <div class="card analytics-section-card">
      <div class="section-header-box">
        <div class="section-title">{$t('traffic.peak_load')}</div>
        <div class="section-desc">{$t('traffic.peak_load_desc')}</div>
      </div>

      <div class="peak-cards-grid">
        <!-- Hour Peak -->
        <div class="peak-period-card">
          <div class="peak-period-title">{$t('traffic.peak_hour_title')}</div>
          <div class="peak-metric-row">
            <span class="peak-flow download-color">
              <span class="peak-arrow">↓</span>
              {$t('traffic.download')}:
            </span>
            <span class="peak-val mono">{formatSpeed(peaks.peak_hour_down)}</span>
            <span class="peak-ts">{formatTime(peaks.peak_hour_down_time)}</span>
          </div>
          <div class="peak-metric-row">
            <span class="peak-flow upload-color">
              <span class="peak-arrow">↑</span>
              {$t('traffic.upload')}:
            </span>
            <span class="peak-val mono">{formatSpeed(peaks.peak_hour_up)}</span>
            <span class="peak-ts">{formatTime(peaks.peak_hour_up_time)}</span>
          </div>
        </div>

        <!-- Day Peak -->
        <div class="peak-period-card">
          <div class="peak-period-title">{$t('traffic.peak_day_title')}</div>
          <div class="peak-metric-row">
            <span class="peak-flow download-color">
              <span class="peak-arrow">↓</span>
              {$t('traffic.download')}:
            </span>
            <span class="peak-val mono">{formatSpeed(peaks.peak_day_down)}</span>
            <span class="peak-ts">{formatTime(peaks.peak_day_down_time)}</span>
          </div>
          <div class="peak-metric-row">
            <span class="peak-flow upload-color">
              <span class="peak-arrow">↑</span>
              {$t('traffic.upload')}:
            </span>
            <span class="peak-val mono">{formatSpeed(peaks.peak_day_up)}</span>
            <span class="peak-ts">{formatTime(peaks.peak_day_up_time)}</span>
          </div>
        </div>

        <!-- Week Peak -->
        <div class="peak-period-card">
          <div class="peak-period-title">{$t('traffic.peak_week_title')}</div>
          <div class="peak-metric-row">
            <span class="peak-flow download-color">
              <span class="peak-arrow">↓</span>
              {$t('traffic.download')}:
            </span>
            <span class="peak-val mono">{formatSpeed(peaks.peak_week_down)}</span>
            <span class="peak-ts">{formatTime(peaks.peak_week_down_time)}</span>
          </div>
          <div class="peak-metric-row">
            <span class="peak-flow upload-color">
              <span class="peak-arrow">↑</span>
              {$t('traffic.upload')}:
            </span>
            <span class="peak-val mono">{formatSpeed(peaks.peak_week_up)}</span>
            <span class="peak-ts">{formatTime(peaks.peak_week_up_time)}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Top LAN Devices (TRAF-06) -->
    <div class="card analytics-section-card">
      <div class="section-header-box">
        <div class="section-title">{$t('traffic.top_clients')}</div>
        <div class="section-desc">{$t('traffic.top_clients_desc')}</div>
      </div>

      {#if topClients.length === 0}
        <div class="no-clients-box">
          <svg
            viewBox="0 0 24 24"
            width="28"
            height="28"
            fill="none"
            stroke="var(--fg-faint)"
            stroke-width="1.5"
          >
            <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
            <line x1="8" y1="21" x2="16" y2="21" />
            <line x1="12" y1="17" x2="12" y2="21" />
          </svg>
          <span>{$t('traffic.no_clients')}</span>
        </div>
      {:else}
        <div class="clients-list">
          {#each topClients as client}
            {@const sharePercent = Math.min(
              100,
              Math.max(2, Math.round((client.total_bytes / totalClientsTraffic) * 100))
            )}
            <div class="client-row">
              <div class="client-info">
                <div class="client-ip-box">
                  <span class="client-icon">
                    <svg
                      viewBox="0 0 24 24"
                      width="14"
                      height="14"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <rect x="2" y="3" width="20" height="14" rx="2" />
                      <line x1="8" y1="21" x2="16" y2="21" />
                      <line x1="12" y1="17" x2="12" y2="21" />
                    </svg>
                  </span>
                  <span class="client-ip mono">{client.ip}</span>
                  <span class="badge-sessions"
                    >{client.active_connections} {$t('traffic.client_conns').toLowerCase()}</span
                  >
                </div>
                <div class="client-bytes mono">{formatBytes(client.total_bytes)}</div>
              </div>
              <div class="client-progress-track">
                <div class="client-progress-bar" style="width: {sharePercent}%;"></div>
              </div>
              <div class="client-meta-sub">
                <span class="download-color">↓ {formatSpeed(client.download)}</span>
                <span class="sep">·</span>
                <span class="upload-color">↑ {formatSpeed(client.upload)}</span>
                <span class="sep">·</span>
                <span class="share-label">{sharePercent}% {$t('traffic.session')}</span>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .badge-live-indicator {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    border-radius: 9999px;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    border: 1px solid transparent;
  }

  .badge-live-indicator .live-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
  }

  .badge-live-indicator.is-live {
    background: rgba(70, 209, 138, 0.12);
    color: var(--success);
    border-color: rgba(70, 209, 138, 0.25);
  }

  .badge-live-indicator.is-live .live-dot {
    background: var(--success);
    box-shadow: 0 0 6px var(--success);
    animation: livePulse 2s infinite ease-in-out;
  }

  .badge-live-indicator.is-paused {
    background: rgba(245, 166, 35, 0.12);
    color: #f5a623;
    border-color: rgba(245, 166, 35, 0.25);
  }

  .badge-live-indicator.is-paused .live-dot {
    background: #f5a623;
  }

  .badge-live-indicator.is-offline {
    background: rgba(100, 116, 139, 0.12);
    color: var(--fg-dim);
    border-color: rgba(100, 116, 139, 0.2);
  }

  .badge-live-indicator.is-offline .live-dot {
    background: var(--fg-dim);
  }

  @keyframes livePulse {
    0%,
    100% {
      opacity: 1;
      transform: scale(1);
    }
    50% {
      opacity: 0.4;
      transform: scale(0.85);
    }
  }

  .btn-reset {
    color: var(--danger);
  }

  .btn-reset:hover {
    background: rgba(244, 112, 127, 0.12);
    border-color: var(--danger);
  }

  .traffic-stats-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--grid-gap, 14px);
  }

  @media (max-width: 900px) {
    .traffic-stats-grid {
      grid-template-columns: 1fr;
    }
  }

  .stat-card-spark,
  .stat-card-normal {
    padding: 20px 24px;
    position: relative;
    min-height: 130px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    overflow: hidden;
    box-sizing: border-box;
  }

  .stat-card-content {
    padding: 0;
    z-index: 2;
  }

  .stat-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.1em;
    font-weight: 700;
  }

  .stat-value {
    font-size: 26px;
    font-weight: 800;
    font-family: var(--font-family-mono);
    line-height: 1.2;
    margin-top: 4px;
  }

  .stat-session {
    font-size: 12px;
    color: var(--fg-dim);
    margin-top: 4px;
  }

  .conns-protocols {
    color: var(--fg-primary);
    font-weight: 600;
  }

  .conns-dynamics {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 2px;
  }

  .delta-up {
    color: var(--accent);
  }

  .delta-down {
    color: var(--fg-dim);
  }

  .upload-color {
    color: var(--success);
  }

  .download-color {
    color: var(--accent);
  }

  .active-connections-color {
    color: var(--fg-primary);
  }

  .sparkline {
    position: absolute;
    right: 0;
    bottom: 0;
    width: 60%;
    height: 48px;
    pointer-events: none;
    z-index: 1;
  }

  /* Chart Card */
  .chart-card {
    padding: 20px 24px;
  }

  .chart-header-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
    flex-wrap: wrap;
    gap: 12px;
  }

  .chart-legend {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .key-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-primary);
    background: transparent;
    border: none;
    cursor: pointer;
    padding: 4px 8px;
    border-radius: var(--radius-sm, 6px);
    transition:
      opacity 0.2s ease,
      background 0.2s ease;
  }

  .key-btn:hover {
    background: var(--bg-card-hover);
  }

  .key-btn.key-inactive {
    opacity: 0.35;
    text-decoration: line-through;
  }

  .sw {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
  }

  .download-bg {
    background: var(--accent);
  }

  .upload-bg {
    background: var(--success);
  }

  .timeframe-picker {
    display: inline-flex;
    background: var(--bg-card-hover);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 8px);
    padding: 2px;
    gap: 2px;
  }

  .tf-pill {
    padding: 4px 10px;
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-dim);
    background: transparent;
    border: none;
    border-radius: var(--radius-sm, 6px);
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .tf-pill:hover {
    color: var(--fg-primary);
  }

  .tf-pill.active {
    color: #fff;
    background: var(--accent);
  }

  .chart-area-wrapper {
    display: flex;
    height: 240px;
    position: relative;
  }

  .chart-empty {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }

  .chart-empty-title {
    font-weight: 700;
    color: var(--fg-primary);
  }

  .chart-empty-sub {
    font-size: 13px;
    color: var(--fg-dim);
    margin: 0;
  }

  .chart-y-axis {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    padding-right: 12px;
    font-size: 11px;
    font-family: var(--font-family-mono);
    color: var(--fg-dim);
    text-align: right;
    min-width: 65px;
    user-select: none;
  }

  .chart-svg-container {
    flex: 1;
    position: relative;
    overflow: hidden;
  }

  .main-traffic-svg {
    width: 100%;
    height: 100%;
    cursor: crosshair;
  }

  .chart-tooltip {
    position: absolute;
    pointer-events: none;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 8px);
    padding: 8px 12px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
    z-index: 10;
    min-width: 150px;
  }

  .tt-time {
    font-size: 11px;
    font-weight: 700;
    color: var(--fg-dim);
    margin-bottom: 4px;
    font-family: var(--font-family-mono);
    border-bottom: 1px solid var(--border);
    padding-bottom: 3px;
  }

  .tt-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
    gap: 8px;
    margin-top: 3px;
  }

  .tt-down {
    color: var(--accent);
  }

  .tt-up {
    color: var(--success);
  }

  .tt-val {
    font-family: var(--font-family-mono);
    font-weight: 700;
  }

  .chart-x {
    display: flex;
    justify-content: space-between;
    padding: 8px 0 0 77px;
    font-size: 11px;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
  }

  /* Analytics Grid (2 Columns) */
  .traffic-analytics-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: var(--grid-gap, 14px);
  }

  @media (max-width: 900px) {
    .traffic-analytics-grid {
      grid-template-columns: 1fr;
    }
  }

  .analytics-section-card {
    padding: 20px 24px;
  }

  .section-header-box {
    margin-bottom: 16px;
  }

  .section-title {
    font-size: 12px;
    font-weight: 700;
    color: var(--fg-primary);
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }

  .section-desc {
    font-size: 12px;
    color: var(--fg-dim);
    margin-top: 2px;
  }

  /* Peak Cards */
  .peak-cards-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 10px;
  }

  @media (max-width: 600px) {
    .peak-cards-grid {
      grid-template-columns: 1fr;
    }
  }

  .peak-period-card {
    background: var(--bg-card-hover);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 8px);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .peak-period-title {
    font-size: 11px;
    font-weight: 700;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .peak-metric-row {
    display: flex;
    flex-direction: column;
    font-size: 12px;
  }

  .peak-flow {
    font-weight: 600;
    font-size: 11px;
    display: flex;
    align-items: center;
    gap: 2px;
  }

  .peak-val {
    font-size: 14px;
    font-weight: 800;
    color: var(--fg-primary);
    margin-top: 1px;
  }

  .peak-ts {
    font-size: 10px;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
  }

  /* Top Clients List */
  .no-clients-box {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 32px 0;
    color: var(--fg-dim);
    font-size: 13px;
  }

  .clients-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .client-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 10px;
    background: var(--bg-card-hover);
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 8px);
  }

  .client-info {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .client-ip-box {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .client-icon {
    color: var(--fg-dim);
    display: flex;
    align-items: center;
  }

  .client-ip {
    font-weight: 700;
    font-size: 13px;
    color: var(--fg-primary);
  }

  .badge-sessions {
    font-size: 10px;
    background: rgba(41, 194, 240, 0.12);
    color: var(--accent);
    padding: 2px 6px;
    border-radius: 4px;
    font-weight: 600;
  }

  .client-bytes {
    font-size: 12px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .client-progress-track {
    width: 100%;
    height: 4px;
    background: rgba(255, 255, 255, 0.05);
    border-radius: 2px;
    overflow: hidden;
  }

  .client-progress-bar {
    height: 100%;
    background: linear-gradient(90deg, var(--accent), var(--success));
    border-radius: 2px;
    transition: width 0.3s ease;
  }

  .client-meta-sub {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    font-family: var(--font-family-mono);
  }

  .client-meta-sub .sep {
    color: var(--fg-faint);
  }

  .share-label {
    color: var(--fg-dim);
    margin-left: auto;
  }

  .mono {
    font-family: var(--font-family-mono);
  }
</style>
