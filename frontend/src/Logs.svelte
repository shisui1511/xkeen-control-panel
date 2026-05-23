<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from './i18n';
  import EmptyState from './components/EmptyState.svelte';

  interface LogEntry {
    timestamp: string;
    source: string;
    level: string; // 'info' | 'warning' | 'error' | 'debug' | ''
    text: string;
    raw: string;
  }

  let logs: LogEntry[] = [];
  let ws: WebSocket | null = null;
  let connected = false;
  let paused = false;
  let filter = '';
  let sourceFilter = '';
  let levelFilter = '';
  let autoScroll = true;
  let logContainer: HTMLDivElement;
  let availableSources: string[] = [];

  function parseLogLine(raw: string): LogEntry {
    let timestamp = '';
    let source = '';
    let level = '';
    let text = raw;

    // 1. Extract timestamp from the beginning of the line
    // Formats: 2026/05/23 00:58:33 or 2026-05-23T00:58:33+03:00 or just 00:58:33
    const tsMatch = raw.match(/^(\d{4}[-/]\d{2}[-/]\d{2}[T\s])?(\d{2}:\d{2}:\d{2})/);
    if (tsMatch) {
      timestamp = tsMatch[2];
      text = text.substring(tsMatch[0].length).trim();
    } else {
      const now = new Date();
      timestamp = now.toTimeString().split(' ')[0];
    }

    // 2. Parse brackets tags like [INF], [Info], [xray], [mihomo]
    const tags: string[] = [];
    let tempText = text;
    while (true) {
      const tagMatch = tempText.match(/^\[([^\]]+)\]\s*/);
      if (!tagMatch) break;
      tags.push(tagMatch[1]);
      tempText = tempText.substring(tagMatch[0].length);
    }

    // Determine level and source from tags
    for (const tag of tags) {
      const lowerTag = tag.toLowerCase();
      if (['info', 'inf', 'information'].includes(lowerTag)) {
        level = 'info';
      } else if (['warning', 'warn', 'wrn'].includes(lowerTag)) {
        level = 'warning';
      } else if (['error', 'err'].includes(lowerTag)) {
        level = 'error';
      } else if (['debug', 'dbg'].includes(lowerTag)) {
        level = 'debug';
      } else {
        source = tag;
      }
    }

    // If source is not set but we have tags, pick the first non-level tag
    if (!source && tags.length > 0) {
      for (const tag of tags) {
        const lowerTag = tag.toLowerCase();
        if (!['info', 'inf', 'information', 'warning', 'warn', 'wrn', 'error', 'err', 'debug', 'dbg'].includes(lowerTag)) {
          source = tag;
          break;
        }
      }
    }

    // Fallback detection of level from raw text if not found in tags
    if (!level) {
      const lowerRaw = raw.toLowerCase();
      if (lowerRaw.includes('error') || lowerRaw.includes('err:')) {
        level = 'error';
      } else if (lowerRaw.includes('warning') || lowerRaw.includes('warn:')) {
        level = 'warning';
      } else if (lowerRaw.includes('debug') || lowerRaw.includes('dbg:')) {
        level = 'debug';
      } else {
        level = 'info';
      }
    }

    text = tempText;

    return { timestamp, source, level, text, raw };
  }

  function updateSources() {
    const sources = new Set<string>();
    for (const log of logs) {
      if (log.source) sources.add(log.source);
    }
    availableSources = Array.from(sources).sort();
  }

  function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/api/logs/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      connected = true;
      const msg = $t('logs.connected');
      logs = [...logs, parseLogLine(`[xkeen] ${msg}`)];
    };

    ws.onmessage = (event) => {
      if (!paused) {
        const entry = parseLogLine(event.data);
        logs = [...logs, entry].slice(-1000);
        updateSources();

        if (autoScroll && logContainer) {
          setTimeout(() => {
            logContainer.scrollTop = logContainer.scrollHeight;
          }, 10);
        }
      }
    };

    ws.onerror = () => {
      connected = false;
      const msg = $t('logs.connection_error');
      logs = [...logs, parseLogLine(`[error] ${msg}`)];
    };

    ws.onclose = () => {
      connected = false;
      const msg = $t('logs.disconnected');
      logs = [...logs, parseLogLine(`[xkeen] ${msg}`)];

      // Auto-reconnect after 3 seconds if not paused
      if (!paused) {
        setTimeout(connect, 3000);
      }
    };
  }

  function disconnect() {
    if (ws) {
      ws.close();
      ws = null;
    }
  }

  function clearLogs() {
    logs = [];
    availableSources = [];
  }

  function exportLogs() {
    const a = document.createElement('a');
    a.href = '/api/logs/download';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  function togglePause() {
    paused = !paused;
  }

  function getFilteredLogs(): LogEntry[] {
    let result = logs;
    if (filter) {
      const lowerFilter = filter.toLowerCase();
      result = result.filter((log) => log.raw.toLowerCase().includes(lowerFilter));
    }
    if (sourceFilter) {
      result = result.filter((log) => log.source === sourceFilter);
    }
    if (levelFilter) {
      result = result.filter((log) => log.level === levelFilter);
    }
    return result;
  }

  function getSourceColor(source: string): string {
    if (!source) return 'var(--fg-dim)';
    // Beautiful pastel colors for dark terminal theme
    const colors = ['#38bdf8', '#a78bfa', '#34d399', '#fbbf24', '#f87171'];
    let hash = 0;
    for (let i = 0; i < source.length; i++) {
      hash = source.charCodeAt(i) + ((hash << 5) - hash);
    }
    return colors[Math.abs(hash) % colors.length];
  }

  onMount(() => {
    connect();
  });

  onDestroy(() => {
    disconnect();
  });
</script>

<div class="logs-page">
  <!-- page-head -->
  <div class="page-head">
    <div>
      <div class="crumbs">{$t('nav.group_services')} <span style="color:var(--fg-faint);margin:0 6px;">/</span> {$t('nav.logs')}</div>
      <h1>{$t('logs.h1')}</h1>
      <p class="sub">{$t('logs.h1_sub')}</p>
    </div>
    <div class="ph-actions">
      <span class="status-indicator" class:connected={connected}>
        {connected ? $t('logs.status_connected') : $t('logs.status_disconnected')}
      </span>
      {#if !connected}
        <button on:click={connect} class="btn btn-primary" title={$t('logs.connect')}>{$t('logs.connect')}</button>
      {/if}
    </div>
  </div>

  <div class="logs-page-container">
    <!-- filter toolbar -->
    <div class="toolbar">
      <div class="toolbar-left">
        <button class="btn btn-secondary" on:click={togglePause} title={paused ? $t('logs.resume') : $t('logs.pause')}>
          {#if paused}
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><polygon points="5 3 19 12 5 21 5 3"/></svg>{$t('logs.resume')}
          {:else}
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><rect x="6" y="5" width="4" height="14" rx="1"/><rect x="14" y="5" width="4" height="14" rx="1"/></svg>{$t('logs.pause')}
          {/if}
        </button>
        
        <button class="btn btn-secondary" on:click={clearLogs} title={$t('logs.clear')}>
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/></svg>{$t('logs.clear')}
        </button>

        <button class="btn btn-secondary" on:click={exportLogs} title={$t('logs.export')}>
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>{$t('logs.export')}
        </button>
      </div>

      <div class="filters" style="flex:1;margin-left:14px;">
        <input type="text" class="filter-input" placeholder={$t('logs.filter')} bind:value={filter} style="flex:1;" />
        
        <select bind:value={sourceFilter} class="source-select">
          <option value="">{$t('logs.all_sources')}</option>
          {#each availableSources as source}
            <option value={source}>{source}</option>
          {/each}
        </select>

        <select bind:value={levelFilter} class="source-select">
          <option value="">{$t('logs.all_levels')}</option>
          <option value="info">info</option>
          <option value="warning">warning</option>
          <option value="error">error</option>
          <option value="debug">debug</option>
        </select>
      </div>

      <label class="toggle-label" style="margin-left:auto;">
        <label class="toggle-switch">
          <input type="checkbox" bind:checked={autoScroll}>
          <span class="toggle-slider"></span>
        </label>
        {$t('logs.autoscroll')}
      </label>
    </div>

    {#if !connected}
      <div
        class="alert alert-danger"
        style="margin: 0; padding: 10px 14px; border-radius: var(--radius-md); font-size: 13px; display: flex; justify-content: space-between; align-items: center; border: 1px solid var(--border);"
      >
        <span><strong>{$t('logs.disconnected_title')}</strong> — {$t('logs.disconnected_desc')}</span>
        <button
          on:click={connect}
          class="btn btn-secondary btn-small"
          style="padding: 4px 8px; font-size: 12px;"
        >
          {$t('logs.reconnect')}
        </button>
      </div>
    {/if}

    <div class="logs-pane" bind:this={logContainer}>
      {#each getFilteredLogs() as log}
        <div class="line">
          <span class="ts">{log.timestamp}</span>
          {#if log.source}
            <span class="src" style="color: {getSourceColor(log.source)};">[{log.source}]</span>
          {/if}
          <span class="lv-{log.level || 'info'}">{log.text}</span>
        </div>
      {/each}

      {#if getFilteredLogs().length === 0}
        <EmptyState
          title={!connected
            ? $t('logs.disconnected_title')
            : filter || sourceFilter || levelFilter
              ? $t('logs.no_filtered_logs')
              : $t('logs.no_logs')}
          description={!connected
            ? $t('logs.disconnected_desc')
            : connected
              ? $t('logs.waiting')
              : $t('logs.connect_hint')}
          ctaText={!connected ? $t('logs.reconnect') : ''}
          oncta={connect}
        />
      {/if}
    </div>

    <div class="stats">
      <span class="stat"><b>{logs.length}</b> {$t('logs.buffer_count', { count: logs.length }).replace(String(logs.length), '').trim()}</span>
      <span class="stat"><b>{availableSources.length}</b> {$t('logs.source_count', { count: availableSources.length }).replace(String(availableSources.length), '').trim()}</span>
      <span class="stat">{$t('logs.realtime_label')}</span>
    </div>
  </div>
</div>

<style>
  .logs-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    flex: 1;
    background: var(--bg);
  }

  .logs-page-container {
    display: flex;
    flex-direction: column;
    gap: 14px;
    flex: 1;
    min-height: 0;
    padding: 0 20px 20px;
  }

  .logs-pane {
    flex: 1;
    background: #050d16;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
    font-family: var(--font-family-mono);
    font-size: 12.5px;
    line-height: 1.5;
    overflow-y: auto;
    min-height: 350px;
  }

  .logs-pane .line {
    display: flex;
    gap: 10px;
    padding: 2px 0;
    align-items: flex-start;
  }

  .logs-pane .ts {
    color: var(--fg-faint);
    flex-shrink: 0;
    user-select: none;
  }

  .logs-pane .src {
    font-weight: 600;
    min-width: 60px;
    flex-shrink: 0;
    text-align: right;
    user-select: none;
  }

  .logs-pane .lv-info {
    color: var(--fg-primary);
    word-break: break-all;
    white-space: pre-wrap;
    flex: 1;
  }

  .logs-pane .lv-warn {
    color: var(--warning);
    word-break: break-all;
    white-space: pre-wrap;
    flex: 1;
  }

  .logs-pane .lv-err {
    color: var(--danger);
    word-break: break-all;
    white-space: pre-wrap;
    flex: 1;
  }

  .logs-pane .lv-debug {
    color: var(--fg-dim);
    word-break: break-all;
    white-space: pre-wrap;
    flex: 1;
  }
</style>
