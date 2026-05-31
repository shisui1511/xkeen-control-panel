<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, currentLang } from './i18n';
  import { showToast } from './stores';

  interface TrafficPoint {
    up: number;
    down: number;
    time: number;
  }

  interface Peaks {
    peak_hour_up: number;
    peak_hour_down: number;
    peak_day_up: number;
    peak_day_down: number;
    peak_week_up: number;
    peak_week_down: number;
    hour_start: number;
    day_start: number;
    week_start: number;
  }

  let trafficData: TrafficPoint[] = [];
  let maxPoints = 60; // 60 points = 60 seconds (1 message/sec)
  let ws: WebSocket | null = null;
  let connected = false;
  let totalUp = 0;
  let totalDown = 0;
  let sessionUp = 0;
  let sessionDown = 0;
  let lastTickTime = 0;

  // Active connections
  let activeConnectionsCount = 0;
  let tcpConnectionsCount = 0;
  let udpConnectionsCount = 0;

  // Connection history for stats
  const CONN_HISTORY_MAX = 3600; // 1 hour at 1 sample/sec
  let connHistory: { ts: number; count: number }[] = [];

  let peaks: Peaks = {
    peak_hour_up: 0,
    peak_hour_down: 0,
    peak_day_up: 0,
    peak_day_down: 0,
    peak_week_up: 0,
    peak_week_down: 0,
    hour_start: 0,
    day_start: 0,
    week_start: 0
  };

  $: connDeltaPerMin = (() => {
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
  })();

  $: connPeakHour = connHistory.reduce((max, h) => h.count > max ? h.count : max, 0);

  function formatSpeed(bytesPerSecond: number): string {
    if (bytesPerSecond === 0) return '0 B/s';
    const k = 1024;
    const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
    const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k));
    return parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  let reconnectTimeout: any = null;
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
      reconnectDelay = 1000; // сбросить задержку при успешном подключении
    };

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        const upSpeed = data.up || 0;
        const downSpeed = data.down || 0;
        const now = Date.now();

        trafficData.push({
          up: upSpeed,
          down: downSpeed,
          time: now
        });

        if (trafficData.length > maxPoints) {
          trafficData = trafficData.slice(-maxPoints);
        } else {
          trafficData = trafficData; // trigger reactivity
        }

        totalUp = upSpeed;
        totalDown = downSpeed;

        if (lastTickTime > 0) {
          const elapsedSec = (now - lastTickTime) / 1000;
          sessionUp += upSpeed * elapsedSec;
          sessionDown += downSpeed * elapsedSec;
        }
        lastTickTime = now;

        activeConnectionsCount = data.connections || 0;
        tcpConnectionsCount = data.tcp_connections || 0;
        udpConnectionsCount = data.udp_connections || 0;

        connHistory.push({ ts: now, count: activeConnectionsCount });
        if (connHistory.length > CONN_HISTORY_MAX) connHistory.shift();
        connHistory = connHistory;

        if (data.peaks) {
          peaks = data.peaks;
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
      ws.onclose = null; // предотвратить повторное подключение при явном закрытии
      ws.onerror = null;
      ws.close();
      ws = null;
    }
    connected = false;
  }

  async function resetStatistics() {
    if (!confirm($t('traffic.reset_confirm'))) return;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/traffic/reset', {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      });
      if (res.ok) {
        sessionUp = 0;
        sessionDown = 0;
        trafficData = [];
        peaks = {
          peak_hour_up: 0,
          peak_hour_down: 0,
          peak_day_up: 0,
          peak_day_down: 0,
          peak_week_up: 0,
          peak_week_down: 0,
          hour_start: 0,
          day_start: 0,
          week_start: 0
        };
        showToast('success', $t('app.success') || 'Success');
      } else {
        showToast('error', 'Failed to reset statistics');
      }
    } catch (e: any) {
      showToast('error', e.message);
    }
  }

  onMount(() => {
    connect();
  });

  onDestroy(() => {
    disconnect();
  });

  // SVG Chart path generators
  $: chartData = (() => {
    if (trafficData.length < 2) {
      return {
        dLine: '',
        dArea: '',
        uLine: '',
        uArea: '',
        maxSpeed: '0 KB/s'
      };
    }

    const points = trafficData;
    const maxVal = Math.max(...points.map((p) => Math.max(p.up, p.down))) || 1024;
    const width = 1000;
    const height = 240;
    const step = width / (maxPoints - 1);

    // Offset x to draw from right to left if points are less than maxPoints
    const startIdx = maxPoints - points.length;
    const getX = (idx: number) => (startIdx + idx) * step;

    const getDownloadY = (val: number) => height - (val / maxVal) * (height - 20);
    const getUploadY = (valUp: number, valDown: number) => {
      const y = height - (valUp / maxVal) * (height - 20);
      // Сдвигаем линию Upload чуть выше при совпадении для видимости обеих линий
      return valUp === valDown ? y - 1.5 : y;
    };

    // Download path
    let dLinePath = `M ${getX(0)} ${getDownloadY(points[0].down)}`;
    for (let i = 1; i < points.length; i++) {
      dLinePath += ` L ${getX(i)} ${getDownloadY(points[i].down)}`;
    }
    const dAreaPath = `${dLinePath} L ${getX(points.length - 1)} ${height} L ${getX(0)} ${height} Z`;

    // Upload path
    let uLinePath = `M ${getX(0)} ${getUploadY(points[0].up, points[0].down)}`;
    for (let i = 1; i < points.length; i++) {
      uLinePath += ` L ${getX(i)} ${getUploadY(points[i].up, points[i].down)}`;
    }
    const uAreaPath = `${uLinePath} L ${getX(points.length - 1)} ${height} L ${getX(0)} ${height} Z`;

    return {
      dLine: dLinePath,
      dArea: dAreaPath,
      uLine: uLinePath,
      uArea: uAreaPath,
      maxSpeed: formatSpeed(maxVal)
    };
  })();

  // Card Sparkline generators (last 20 points)
  $: sparklines = (() => {
    const points = trafficData.slice(-20);
    if (points.length < 2) {
      return { uLine: '', uArea: '', dLine: '', dArea: '' };
    }

    const maxUp = Math.max(...points.map((p) => p.up)) || 1;
    const maxDown = Math.max(...points.map((p) => p.down)) || 1;
    const width = 200;
    const height = 42;
    // Динамически растягиваем спарклайн на всю ширину карточки при малом числе точек
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
  })();
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_tools')}
        <span style="color:var(--fg-faint);margin:0 8px;">/</span>
        {$t('traffic.title')}
      </div>
      <h1>{$t('traffic.title')}</h1>
      <p class="sub">{$t('traffic.realtime')}</p>
    </div>
    <div class="ph-actions" style="display: flex; gap: 12px; align-items: center;">
      <span class="status-indicator" class:connected class:live={connected}>
        {connected ? 'live' : 'offline'}
      </span>
      <button
        class="btn btn-secondary btn-reset"
        style="color:var(--danger);"
        on:click={resetStatistics}
      >
        {$t('traffic.reset_stats')}
      </button>
    </div>
  </div>

  <div class="stats-grid mb-2">
    <!-- Upload Card -->
    <div class="card stat-card-spark">
      <div class="stat-card-content">
        <div class="stat-label">{$t('traffic.upload')}</div>
        <div class="stat-value upload-color">{formatSpeed(totalUp)}</div>
        <div class="stat-session">
          Σ {$t('traffic.session')} {formatBytes(sessionUp)}
        </div>
      </div>
      {#if trafficData.length >= 2}
        <svg class="sparkline" viewBox="0 0 200 42" preserveAspectRatio="none" role="img" aria-label={$t('traffic.upload_sparkline')}>
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

    <!-- Download Card -->
    <div class="card stat-card-spark">
      <div class="stat-card-content">
        <div class="stat-label">{$t('traffic.download')}</div>
        <div class="stat-value download-color">{formatSpeed(totalDown)}</div>
        <div class="stat-session">
          Σ {$t('traffic.session')} {formatBytes(sessionDown)}
        </div>
      </div>
      {#if trafficData.length >= 2}
        <svg class="sparkline" viewBox="0 0 200 42" preserveAspectRatio="none" role="img" aria-label={$t('traffic.download_sparkline')}>
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

    <!-- Active Connections Card -->
    <div class="card stat-card-normal">
      <div class="stat-label">
        {$t('traffic.active_connections')}
      </div>
      <div class="stat-value active-connections-color">{activeConnectionsCount}</div>
      <div class="stat-session">{tcpConnectionsCount} TCP · {udpConnectionsCount} UDP</div>
      <div class="stat-session" style="margin-top: 4px; color: var(--fg-dim);">
        {#if connDeltaPerMin === null}
          — / {$t('traffic.per_min')}
        {:else}
          <span style={connDeltaPerMin < 0 ? 'color: var(--fg-dim);' : ''}>
            {connDeltaPerMin >= 0 ? '+' : ''}{connDeltaPerMin} / {$t('traffic.per_min')}
          </span>
        {/if}
        · {$t('traffic.peak')} {connPeakHour}
      </div>
    </div>
  </div>

  <!-- Main Chart Card -->
  <div class="card chart-card">
    <div class="chart-legend">
      <span class="key"><span class="sw download-bg"></span>{$t('traffic.download')}</span>
      <span class="key"><span class="sw upload-bg"></span>{$t('traffic.upload')}</span>
      <span class="chart-time-label">
        {$t('traffic.chart_legend_time')}
      </span>
    </div>

    <div class="chart-area-wrapper">
      {#if trafficData.length < 2}
        <div class="chart-empty" style="flex-direction: column; gap: 8px;">
          <div style="display: flex; align-items: center; justify-content: center; gap: 8px;">
            <span class="spinner">...</span>
            <span style="font-weight: 700;">{$t('traffic.waiting')}</span>
          </div>
          <p style="font-size: 14px; color: var(--fg-dim); margin: 0;">
            {$t('traffic.empty_state_body')}
          </p>
        </div>
      {:else}
        <div class="chart-y-axis">
          <span class="y-label">{chartData.maxSpeed}</span>
          <span class="y-label"></span>
          <span class="y-label"></span>
          <span class="y-label">0 B/s</span>
        </div>
        <div class="chart-svg-container">
          <svg viewBox="0 0 1000 240" preserveAspectRatio="none" style="width: 100%; height: 100%;" role="img" aria-label={$t('traffic.main_chart')}>
            <defs>
              <linearGradient id="cg-download-main" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.25" />
                <stop offset="100%" stop-color="var(--accent)" stop-opacity="0" />
              </linearGradient>
              <linearGradient id="cg-upload-main" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--success)" stop-opacity="0.15" />
                <stop offset="100%" stop-color="var(--success)" stop-opacity="0" />
              </linearGradient>
            </defs>
            <!-- Grid Lines -->
            <line
              x1="0"
              y1="60"
              x2="1000"
              y2="60"
              stroke="var(--border)"
              opacity="0.3"
              stroke-dasharray="4"
            />
            <line
              x1="0"
              y1="120"
              x2="1000"
              y2="120"
              stroke="var(--border)"
              opacity="0.3"
              stroke-dasharray="4"
            />
            <line
              x1="0"
              y1="180"
              x2="1000"
              y2="180"
              stroke="var(--border)"
              opacity="0.3"
              stroke-dasharray="4"
            />

            <!-- Download Path -->
            <path d={chartData.dArea} fill="url(#cg-download-main)" />
            <path d={chartData.dLine} fill="none" stroke="var(--accent)" stroke-width="2" />

            <!-- Upload Path -->
            <path d={chartData.uArea} fill="url(#cg-upload-main)" />
            <path d={chartData.uLine} fill="none" stroke="var(--success)" stroke-width="2" />
          </svg>
        </div>
      {/if}
    </div>

    <div class="chart-x">
      <span>60 {$t('traffic.sec_ago')}</span>
      <span>-45 {$t('traffic.sec')}</span>
      <span>-30 {$t('traffic.sec')}</span>
      <span>-15 {$t('traffic.sec')}</span>
      <span>{$t('traffic.now')}</span>
    </div>
  </div>

  <!-- Peak Load Card -->
  <div class="card" style="margin-top: 16px; padding: 24px;">
    <div style="font-size: 11px; font-weight: 700; color: var(--fg-primary); margin-bottom: 12px; text-transform: uppercase; letter-spacing: 0.1em;">
      {$t('traffic.peak_load')}
    </div>
    <div style="font-size: 11px; color: var(--fg-dim); margin-bottom: 16px;">
      {$t('traffic.peak_load_desc')}
    </div>
    <div class="table-container" style="overflow-x: auto;">
      <table class="connections-table" style="min-width: 100%; border-collapse: collapse;">
        <thead>
          <tr style="border-bottom: 1px solid var(--border); text-align: left;">
            <th style="padding: 8px 16px; color: var(--fg-dim); font-size: 11px; font-weight: 700; text-transform: uppercase;">{$t('traffic.peak_hour')}</th>
            <th style="padding: 8px 16px; color: var(--fg-dim); font-size: 11px; font-weight: 700; text-transform: uppercase;">{$t('traffic.peak_day')}</th>
            <th style="padding: 8px 16px; color: var(--fg-dim); font-size: 11px; font-weight: 700; text-transform: uppercase;">{$t('traffic.peak_week')}</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td style="padding: 8px 16px; font-family: var(--font-family-mono); font-size: 14px;">
              <span class="upload-color">↑ {formatSpeed(peaks.peak_hour_up)}</span>
              <span style="color: var(--fg-dim); margin: 0 8px;">/</span>
              <span class="download-color">↓ {formatSpeed(peaks.peak_hour_down)}</span>
            </td>
            <td style="padding: 8px 16px; font-family: var(--font-family-mono); font-size: 14px;">
              <span class="upload-color">↑ {formatSpeed(peaks.peak_day_up)}</span>
              <span style="color: var(--fg-dim); margin: 0 8px;">/</span>
              <span class="download-color">↓ {formatSpeed(peaks.peak_day_down)}</span>
            </td>
            <td style="padding: 8px 16px; font-family: var(--font-family-mono); font-size: 14px;">
              <span class="upload-color">↑ {formatSpeed(peaks.peak_week_up)}</span>
              <span style="color: var(--fg-dim); margin: 0 8px;">/</span>
              <span class="download-color">↓ {formatSpeed(peaks.peak_week_down)}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</div>

<style>
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 14px;
  }

  @media (max-width: 768px) {
    .stats-grid {
      grid-template-columns: 1fr;
    }
  }

  .stat-card-spark,
  .stat-card-normal { /* hot-fix */
    padding: 20px 24px;
    position: relative;
    height: 130px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    overflow: hidden;
    box-sizing: border-box;
  }

  .stat-card-content { /* hot-fix */
    padding: 0;
    z-index: 2;
  }

  .stat-label {
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
    margin-top: 2px;
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

  .stat-session { /* hot-fix */
    font-size: 11px;
    color: var(--fg-secondary);
    margin-top: 2px;
    margin-bottom: 0;
  }

  .sparkline {
    width: 100%;
    height: 36px;
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    top: auto;
    z-index: 1;
    pointer-events: none;
    overflow: hidden; /* clip any bleed/overflow */
  }

  /* Main Chart Card */
  .chart-card {
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .chart-legend {
    display: flex;
    align-items: center;
    gap: 16px;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .key {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .sw {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    display: inline-block;
  }

  .download-bg {
    background: var(--accent);
  }

  .upload-bg {
    background: var(--success);
  }

  .chart-time-label {
    margin-left: auto;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    font-size: 11px;
  }

  .chart-area-wrapper {
    position: relative;
    height: 240px;
    display: flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: rgba(0, 0, 0, 0.15);
    overflow: hidden;
  }

  .chart-empty {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--fg-dim);
    font-size: 14px;
  }

  .chart-y-axis {
    width: 70px;
    height: 100%;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    padding: 12px 8px;
    border-right: 1px solid var(--border);
    background: rgba(0, 0, 0, 0.1);
    font-family: var(--font-family-mono);
    font-size: 11px;
    color: var(--fg-dim);
    text-align: right;
    z-index: 5;
  }

  .y-label {
    white-space: nowrap;
  }

  .chart-svg-container {
    flex: 1;
    height: 100%;
    position: relative;
  }

  .chart-x {
    display: flex;
    justify-content: space-between;
    padding: 0 8px 0 80px;
    font-size: 11px;
    color: var(--fg-dim);
  }

  :global(.status-indicator.live) {
    color: var(--accent);
    border-color: rgba(41, 194, 240, 0.4);
    background: rgba(41, 194, 240, 0.08);
  }
  :global(.status-indicator.live::before) {
    display: inline-block !important;
    background: var(--accent) !important;
    box-shadow: 0 0 8px var(--accent) !important;
    animation: ledPulse 2.4s infinite !important;
  }
</style>
