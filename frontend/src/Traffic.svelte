<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, currentLang } from './i18n';

  interface TrafficPoint {
    up: number;
    down: number;
    time: number;
  }

  let trafficData: TrafficPoint[] = [];
  let maxPoints = 60; // 60 points = 60 seconds (1 message/sec)
  let es: EventSource | null = null;
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
  let connInterval: any = null;

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

  async function fetchConnections() {
    try {
      const res = await fetch('/api/mihomo/proxy/connections');
      if (res.ok) {
        const data = await res.json();
        const conns = data.connections || [];
        activeConnectionsCount = conns.length;
        tcpConnectionsCount = conns.filter((c: any) => c.metadata?.network === 'TCP').length;
        udpConnectionsCount = conns.filter((c: any) => c.metadata?.network === 'UDP').length;
      }
    } catch (e) {
      // ignore
    }
  }

  function connect() {
    const protocol = window.location.protocol === 'https:' ? 'https:' : 'http:';
    const url = `${protocol}//${window.location.host}/api/mihomo/proxy/traffic`;

    es = new EventSource(url);

    es.onopen = () => {
      connected = true;
      lastTickTime = 0;
    };

    es.onmessage = (event) => {
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
      } catch (e) {
        // ignore
      }
    };

    es.onerror = () => {
      connected = false;
    };
  }

  function disconnect() {
    if (es) {
      es.close();
      es = null;
    }
    connected = false;
  }

  onMount(() => {
    connect();
    fetchConnections();
    connInterval = setInterval(fetchConnections, 5000);
  });

  onDestroy(() => {
    disconnect();
    if (connInterval) clearInterval(connInterval);
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
    const maxVal = Math.max(...points.map(p => Math.max(p.up, p.down))) || 1024;
    const width = 1000;
    const height = 240;
    const step = width / (maxPoints - 1);
    
    // Offset x to draw from right to left if points are less than maxPoints
    const startIdx = maxPoints - points.length;
    const getX = (idx: number) => (startIdx + idx) * step;

    // Download path
    let dLinePath = `M ${getX(0)} ${height - (points[0].down / maxVal) * (height - 20)}`;
    for (let i = 1; i < points.length; i++) {
      dLinePath += ` L ${getX(i)} ${height - (points[i].down / maxVal) * (height - 20)}`;
    }
    const dAreaPath = `${dLinePath} L ${getX(points.length - 1)} ${height} L ${getX(0)} ${height} Z`;

    // Upload path
    let uLinePath = `M ${getX(0)} ${height - (points[0].up / maxVal) * (height - 20)}`;
    for (let i = 1; i < points.length; i++) {
      uLinePath += ` L ${getX(i)} ${height - (points[i].up / maxVal) * (height - 20)}`;
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

    const maxUp = Math.max(...points.map(p => p.up)) || 1;
    const maxDown = Math.max(...points.map(p => p.down)) || 1;
    const width = 200;
    const height = 42;
    const step = width / 19; // 20 points, 19 steps

    const startX = width - (points.length - 1) * step;

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
      <div class="crumbs">{$currentLang === 'ru' ? 'Инструменты' : 'Tools'} <span style="color:var(--fg-faint);margin:0 6px;">/</span> {$t('traffic.title')}</div>
      <h1>{$t('traffic.title')}</h1>
      <p class="sub">{$t('traffic.realtime')}</p>
    </div>
    <div class="ph-actions">
      <span class="status-indicator" class:connected={connected}>
        {connected ? 'live' : 'offline'}
      </span>
    </div>
  </div>

  <div class="stats-grid mb-2">
    <!-- Upload Card -->
    <div class="card stat-card-spark">
      <div class="stat-card-content">
        <div class="stat-label">Upload</div>
        <div class="stat-value upload-color">{formatSpeed(totalUp)}</div>
        <div class="stat-session">Σ {$currentLang === 'ru' ? 'сессии' : 'session'} {formatBytes(sessionUp)}</div>
      </div>
      {#if trafficData.length >= 2}
        <svg class="sparkline" viewBox="0 0 200 42" preserveAspectRatio="none">
          <defs>
            <linearGradient id="sg-upload" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="var(--success)" stop-opacity="0.4"/>
              <stop offset="100%" stop-color="var(--success)" stop-opacity="0"/>
            </linearGradient>
          </defs>
          <path d={sparklines.uArea} fill="url(#sg-upload)"/>
          <path d={sparklines.uLine} fill="none" stroke="var(--success)" stroke-width="1.5"/>
        </svg>
      {/if}
    </div>

    <!-- Download Card -->
    <div class="card stat-card-spark">
      <div class="stat-card-content">
        <div class="stat-label">Download</div>
        <div class="stat-value download-color">{formatSpeed(totalDown)}</div>
        <div class="stat-session">Σ {$currentLang === 'ru' ? 'сессии' : 'session'} {formatBytes(sessionDown)}</div>
      </div>
      {#if trafficData.length >= 2}
        <svg class="sparkline" viewBox="0 0 200 42" preserveAspectRatio="none">
          <defs>
            <linearGradient id="sg-download" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.4"/>
              <stop offset="100%" stop-color="var(--accent)" stop-opacity="0"/>
            </linearGradient>
          </defs>
          <path d={sparklines.dArea} fill="url(#sg-download)"/>
          <path d={sparklines.dLine} fill="none" stroke="var(--accent)" stroke-width="1.5"/>
        </svg>
      {/if}
    </div>

    <!-- Active Connections Card -->
    <div class="card stat-card-normal">
      <div class="stat-label">{$currentLang === 'ru' ? 'Активные соединения' : 'Active Connections'}</div>
      <div class="stat-value active-connections-color">{activeConnectionsCount}</div>
      <div class="stat-session">{tcpConnectionsCount} TCP · {udpConnectionsCount} UDP</div>
    </div>
  </div>

  <!-- Main Chart Card -->
  <div class="card chart-card">
    <div class="chart-legend">
      <span class="key"><span class="sw download-bg"></span>Download</span>
      <span class="key"><span class="sw upload-bg"></span>Upload</span>
      <span class="chart-time-label">
        {$currentLang === 'ru' ? 'последняя минута · 1 точка/с' : 'last minute · 1 point/s'}
      </span>
    </div>
    
    <div class="chart-area-wrapper">
      {#if trafficData.length < 2}
        <div class="chart-empty">
          <span class="spinner" style="margin-right: 8px;">...</span>
          {$currentLang === 'ru' ? 'Ожидание данных трафика...' : 'Waiting for traffic data...'}
        </div>
      {:else}
        <div class="chart-y-axis">
          <span class="y-label">{chartData.maxSpeed}</span>
          <span class="y-label"></span>
          <span class="y-label"></span>
          <span class="y-label">0 B/s</span>
        </div>
        <div class="chart-svg-container">
          <svg viewBox="0 0 1000 240" preserveAspectRatio="none" style="width: 100%; height: 100%;">
            <defs>
              <linearGradient id="cg-download-main" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.25"/>
                <stop offset="100%" stop-color="var(--accent)" stop-opacity="0"/>
              </linearGradient>
              <linearGradient id="cg-upload-main" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--success)" stop-opacity="0.15"/>
                <stop offset="100%" stop-color="var(--success)" stop-opacity="0"/>
              </linearGradient>
            </defs>
            <!-- Grid Lines -->
            <line x1="0" y1="60" x2="1000" y2="60" stroke="rgba(255,255,255,.03)" stroke-dasharray="4"/>
            <line x1="0" y1="120" x2="1000" y2="120" stroke="rgba(255,255,255,.03)" stroke-dasharray="4"/>
            <line x1="0" y1="180" x2="1000" y2="180" stroke="rgba(255,255,255,.03)" stroke-dasharray="4"/>
            
            <!-- Download Path -->
            <path d={chartData.dArea} fill="url(#cg-download-main)"/>
            <path d={chartData.dLine} fill="none" stroke="var(--accent)" stroke-width="2"/>
            
            <!-- Upload Path -->
            <path d={chartData.uArea} fill="url(#cg-upload-main)"/>
            <path d={chartData.uLine} fill="none" stroke="var(--success)" stroke-width="2"/>
          </svg>
        </div>
      {/if}
    </div>
    
    <div class="chart-x">
      <span>60 {$currentLang === 'ru' ? 'сек назад' : 'sec ago'}</span>
      <span>-45 {$currentLang === 'ru' ? 'сек' : 'sec'}</span>
      <span>-30 {$currentLang === 'ru' ? 'сек' : 'sec'}</span>
      <span>-15 {$currentLang === 'ru' ? 'сек' : 'sec'}</span>
      <span>{$currentLang === 'ru' ? 'сейчас' : 'now'}</span>
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

  .stat-card-spark {
    padding: 0;
    position: relative;
    height: 110px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    overflow: hidden;
  }

  .stat-card-content {
    padding: 20px 20px 0 20px;
    z-index: 2;
  }

  .stat-card-normal {
    padding: 20px;
    height: 110px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
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

  .stat-session {
    font-size: 12px;
    color: var(--fg-secondary);
    margin-top: 4px;
    margin-bottom: 8px;
  }

  .sparkline {
    width: 100%;
    height: 36px;
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    z-index: 1;
    pointer-events: none;
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
    padding: 10px 8px;
    border-right: 1px solid var(--border);
    background: rgba(0, 0, 0, 0.1);
    font-family: var(--font-family-mono);
    font-size: 10px;
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
    padding: 0 10px 0 80px;
    font-size: 11px;
    color: var(--fg-dim);
  }
</style>
