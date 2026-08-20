<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, currentLang, pluralize } from './i18n';
  import { showToast } from './stores';

  interface LogEntry {
    id: number;
    timestamp: string;
    source: string;
    level: string; // 'info' | 'warning' | 'error' | 'debug' | ''
    text: string;
    raw: string;
  }

  const MAX_LOG_BUFFER = 500;
  const ROW_HEIGHT = 28;
  const BUFFER_ROWS = 10;

  let destroyed = false;
  let logIdCounter = 0;
  let logs = $state<LogEntry[]>([]);
  let ws = $state<WebSocket | null>(null);
  let containerHeight = $state(0);
  let scrollTop = $state(0);

  function handleScroll(e: Event) {
    const target = e.currentTarget as HTMLElement;
    scrollTop = target.scrollTop;
  }

  let connected = $state(false);
  let paused = $state(false);
  let pausedNewCount = $state(0);
  let filter = $state('');
  let sourceFilter = $state('');
  let levelFilter = $state('');
  let autoScroll = $state(true);
  let wordWrap = $state(false);
  let logContainer = $state<HTMLDivElement>();
  let availableSources = $state<string[]>([]);

  // Known sources for tabs
  const KNOWN_SOURCES = ['xkeen', 'xray', 'mihomo', 'xcp'];

  const filteredLogs = $derived.by(() => {
    let result = logs;
    if (filter) {
      const lf = filter.toLowerCase();
      result = result.filter((log) => log.raw.toLowerCase().includes(lf));
    }
    if (sourceFilter) {
      result = result.filter((log) => log.source.toLowerCase() === sourceFilter.toLowerCase());
    }
    if (levelFilter) {
      result = result.filter((log) => log.level === levelFilter);
    }
    return result;
  });

  const totalItems = $derived(filteredLogs.length);
  const visibleCount = $derived(Math.ceil(containerHeight / ROW_HEIGHT));
  const startIndex = $derived(Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - BUFFER_ROWS));
  const endIndex = $derived(Math.min(totalItems, startIndex + visibleCount + 2 * BUFFER_ROWS));

  const visibleLogs = $derived.by(() => {
    if (wordWrap) {
      return filteredLogs.map((log) => ({ log, y: 0 }));
    }
    return filteredLogs.slice(startIndex, endIndex).map((log, idx) => ({
      log,
      y: (startIndex + idx) * ROW_HEIGHT
    }));
  });

  function parseLogLine(raw: string): LogEntry {
    let timestamp = '';
    let source = '';
    let level = '';
    let text = raw.trim();

    // 1. Bracket source prefix ^\[([^\]]+)\]\s*
    const bracketMatch = text.match(/^\[([^\]]+)\]\s*/);
    if (bracketMatch) {
      const tag = bracketMatch[1].toLowerCase();
      if (tag.includes('access.log') || tag.includes('error.log') || tag === 'xray') {
        source = 'xray';
      } else if (tag.includes('mihomo.log') || tag === 'mihomo') {
        source = 'mihomo';
      } else if (tag.includes('xkeen-detached') || tag.includes('xkeen.log') || tag === 'xkeen') {
        source = 'xkeen';
      } else if (tag.includes('xcp.log') || tag === 'xcp') {
        source = 'xcp';
      } else {
        source = bracketMatch[1];
      }
      text = text.substring(bracketMatch[0].length).trim();
    }

    // 2. Timestamp extraction
    const tsMatch = text.match(
      /^(\d{4}[-/]\d{2}[-/]\d{2}[T\s]|\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}\s*)?(\d{2}:\d{2}:\d{2})/
    );
    if (tsMatch) {
      timestamp = tsMatch[2];
      text = text.substring(tsMatch[0].length).trim();
    } else {
      const now = new Date();
      timestamp = now.toTimeString().split(' ')[0];
    }

    // 3. Bracket tags for severity
    const tags: string[] = [];
    let tempText = text;
    while (true) {
      const tagMatch = tempText.match(/^\[([^\]]+)\]\s*/);
      if (!tagMatch) break;
      tags.push(tagMatch[1]);
      tempText = tempText.substring(tagMatch[0].length).trim();
    }

    for (const tag of tags) {
      const lowerTag = tag.toLowerCase();
      if (['info', 'inf', 'information'].includes(lowerTag)) {
        level = 'info';
      } else if (['warning', 'warn', 'wrn'].includes(lowerTag)) {
        level = 'warning';
      } else if (['error', 'err', 'fatal'].includes(lowerTag)) {
        level = 'error';
      } else if (['debug', 'dbg'].includes(lowerTag)) {
        level = 'debug';
      } else if (!source) {
        if (lowerTag === 'xray') source = 'xray';
        else if (lowerTag === 'mihomo') source = 'mihomo';
        else if (lowerTag === 'xkeen') source = 'xkeen';
      }
    }

    if (!source) {
      const lowerRaw = raw.toLowerCase();
      if (lowerRaw.includes('xray')) source = 'xray';
      else if (lowerRaw.includes('mihomo')) source = 'mihomo';
      else source = 'xkeen';
    }

    if (!level) {
      const lowerText = text.toLowerCase();
      if (lowerText.includes('error') || lowerText.includes('err:')) {
        level = 'error';
      } else if (lowerText.includes('warning') || lowerText.includes('warn:')) {
        level = 'warning';
      } else if (lowerText.includes('debug') || lowerText.includes('dbg:')) {
        level = 'debug';
      } else {
        level = 'info';
      }
    }

    text = tempText;
    logIdCounter += 1;
    return { id: logIdCounter, timestamp, source, level, text, raw };
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
      if (document.hidden) return;
      if (paused) {
        pausedNewCount += 1;
        return;
      }
      const entry = parseLogLine(event.data);
      logs = [...logs, entry].slice(-MAX_LOG_BUFFER);
      updateSources();

      if (autoScroll && logContainer) {
        setTimeout(() => {
          if (logContainer) {
            logContainer.scrollTop = logContainer.scrollHeight;
          }
        }, 0);
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

      if (!paused && !destroyed) {
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
    pausedNewCount = 0;
  }

  function togglePause() {
    paused = !paused;
    if (!paused) {
      pausedNewCount = 0;
      if (autoScroll && logContainer) {
        setTimeout(() => {
          if (logContainer) logContainer.scrollTop = logContainer.scrollHeight;
        }, 0);
      }
    }
  }

  function unpauseAndScroll() {
    paused = false;
    pausedNewCount = 0;
    if (logContainer) {
      setTimeout(() => {
        if (logContainer) logContainer.scrollTop = logContainer.scrollHeight;
      }, 0);
    }
  }

  function exportFiltered() {
    const textContent = filteredLogs.map((l) => l.raw).join('\n');
    const blob = new Blob([textContent], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `xcp_logs_filtered_${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  function exportFull() {
    const a = document.createElement('a');
    a.href = '/api/logs/download';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  async function copyRow(entry: LogEntry) {
    try {
      await navigator.clipboard.writeText(entry.raw);
      showToast('success', $t('logs.copied'));
    } catch {
      showToast('error', 'Failed to copy');
    }
  }

  function highlightMatches(text: string, query: string): string {
    if (!query) return escapeHtml(text);
    const escaped = escapeHtml(text);
    const safeQuery = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const regex = new RegExp(`(${safeQuery})`, 'gi');
    return escaped.replace(regex, '<mark class="log-mark">$1</mark>');
  }

  function escapeHtml(str: string): string {
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  function handleVisibilityChange() {
    if (!document.hidden && (!ws || ws.readyState !== WebSocket.OPEN) && !paused && !destroyed) {
      connect();
    }
  }

  onMount(() => {
    connect();
    window.addEventListener('visibilitychange', handleVisibilityChange);
    const mainContent = document.querySelector('.main-content') as HTMLElement;
    if (mainContent) {
      mainContent.style.overflowY = 'hidden';
    }
  });

  onDestroy(() => {
    destroyed = true;
    disconnect();
    window.removeEventListener('visibilitychange', handleVisibilityChange);
    const mainContent = document.querySelector('.main-content') as HTMLElement;
    if (mainContent) {
      mainContent.style.overflowY = '';
    }
  });
</script>

<div class="logs-page">
  <!-- page-head -->
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_system')} <span class="crumb-sep">›</span>
        {$t('nav.logs')}
      </div>
      <h1>{$t('logs.h1')}</h1>
      <p class="sub">{$t('logs.h1_sub')}</p>
    </div>
    <div class="ph-actions">
      {#if !connected}
        <span class="status-badge stopped">
          <span class="status-dot error"></span>{$t('logs.status_disconnected')}
        </span>
        <button onclick={connect} class="btn btn-primary btn-sm" title={$t('logs.connect')}>
          {$t('logs.connect')}
        </button>
      {:else if paused}
        <span class="status-badge warning">
          <span class="status-dot warning"></span>{$t('logs.status_paused')}
        </span>
      {:else}
        <span class="status-badge running">
          <span class="status-dot success"></span>{$t('logs.status_connected')}
        </span>
      {/if}
    </div>
  </div>

  <div class="logs-page-container">
    <!-- Unified Balanced Toolbar (LOGS-01) -->
    <div class="logs-toolbar">
      <!-- Left Controls: Stream Lifecycle -->
      <div class="tb-group tb-stream">
        <button
          class="btn btn-secondary btn-sm"
          onclick={togglePause}
          title={paused ? $t('logs.resume') : $t('logs.pause')}
        >
          {#if paused}
            <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"
              ><polygon points="5 3 19 12 5 21 5 3" /></svg
            >
            <span>{$t('logs.resume')}</span>
          {:else}
            <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"
              ><rect x="6" y="5" width="4" height="14" rx="1" /><rect
                x="14"
                y="5"
                width="4"
                height="14"
                rx="1"
              /></svg
            >
            <span>{$t('logs.pause')}</span>
          {/if}
        </button>

        <button class="btn btn-secondary btn-sm" onclick={clearLogs} title={$t('logs.clear')}>
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            ><polyline points="3 6 5 6 21 6" /><path
              d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"
            /></svg
          >
          <span>{$t('logs.clear')}</span>
        </button>

        <button
          class="btn btn-secondary btn-sm"
          class:btn-active={autoScroll}
          onclick={() => (autoScroll = !autoScroll)}
          title={$t('logs.autoscroll')}
        >
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            ><polyline points="7 13 12 18 17 13" /><polyline points="7 6 12 11 17 6" /></svg
          >
          <span>{$t('logs.autoscroll')}</span>
        </button>

        <button
          class="btn btn-secondary btn-sm"
          class:btn-active={wordWrap}
          onclick={() => (wordWrap = !wordWrap)}
          title={$t('logs.word_wrap')}
        >
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            ><polyline points="9 10 4 15 9 20" /><path d="M20 4v7a4 4 0 0 1-4 4H4" /></svg
          >
          <span>{$t('logs.word_wrap')}</span>
        </button>

        <div class="export-split">
          <button
            class="btn btn-secondary btn-sm"
            onclick={exportFiltered}
            title={$t('logs.export_filtered')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline
                points="7 10 12 15 17 10"
              /><line x1="12" y1="15" x2="12" y2="3" /></svg
            >
            <span>{$t('logs.export_filtered')}</span>
          </button>
          <button
            class="btn btn-secondary btn-sm btn-icon"
            onclick={exportFull}
            title={$t('logs.export_full')}
            aria-label={$t('logs.export_full')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"><path d="M6 9l6 6 6-6" /></svg
            >
          </button>
        </div>
      </div>

      <!-- Right Controls: Search & Filtering -->
      <div class="tb-group tb-filters">
        <!-- Search Input with Clear and Counter -->
        <div class="search-wrap">
          <svg
            class="search-icon"
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            ><circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" /></svg
          >
          <input
            type="text"
            class="search-input"
            placeholder={$t('logs.search_placeholder')}
            bind:value={filter}
          />
          {#if filter}
            <span class="match-badge">
              {filteredLogs.length}/{logs.length}
            </span>
            <button
              class="clear-search-btn"
              onclick={() => (filter = '')}
              title="Clear search"
              aria-label="Clear search"
            >
              ×
            </button>
          {/if}
        </div>

        <!-- Dynamic Source Filter Tabs -->
        <div class="source-pills" role="group" aria-label={$t('logs.source')}>
          <button
            type="button"
            class="source-pill"
            class:active={sourceFilter === ''}
            onclick={() => (sourceFilter = '')}
          >
            {$t('logs.all_sources')}
          </button>
          {#each KNOWN_SOURCES as src}
            <button
              type="button"
              class="source-pill"
              class:active={sourceFilter === src}
              onclick={() => (sourceFilter = sourceFilter === src ? '' : src)}
            >
              {src}
            </button>
          {/each}
        </div>

        <!-- Severity Level Dropdown -->
        <select bind:value={levelFilter} class="level-select" aria-label={$t('logs.level')}>
          <option value="">{$t('logs.all_levels')}</option>
          <option value="error">ERROR</option>
          <option value="warning">WARN</option>
          <option value="info">INFO</option>
          <option value="debug">DEBUG</option>
        </select>
      </div>
    </div>

    <!-- Log Console Pane (LOGS-02) -->
    <div
      class="logs-console"
      class:wrap-mode={wordWrap}
      bind:this={logContainer}
      bind:clientHeight={containerHeight}
      onscroll={handleScroll}
    >
      {#if totalItems > 0}
        {#if !wordWrap}
          <div
            class="logs-spacer"
            style="height: {totalItems *
              ROW_HEIGHT}px; width: 100%; pointer-events: none; position: absolute; top: 0; left: 0;"
          ></div>
        {/if}

        <div class="lines-container" class:static-layout={wordWrap}>
          {#each visibleLogs as item (item.log.id)}
            <div
              class="log-row"
              class:row-error={item.log.level === 'error'}
              class:row-warning={item.log.level === 'warning'}
              class:row-debug={item.log.level === 'debug'}
              style={!wordWrap
                ? `position: absolute; top: 0; left: 0; right: 0; height: ${ROW_HEIGHT}px; transform: translateY(${item.y}px);`
                : ''}
            >
              <span class="col-ts monospace">{item.log.timestamp}</span>
              <span class="col-src">
                <span class="src-tag">{item.log.source || 'sys'}</span>
              </span>
              <span class="col-level">
                {#if item.log.level === 'error'}
                  <span class="lvl-badge lvl-error">ERR</span>
                {:else if item.log.level === 'warning'}
                  <span class="lvl-badge lvl-warn">WRN</span>
                {:else if item.log.level === 'debug'}
                  <span class="lvl-badge lvl-debug">DBG</span>
                {:else}
                  <span class="lvl-badge lvl-info">INF</span>
                {/if}
              </span>
              <span class="col-msg">
                <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                {@html highlightMatches(item.log.text, filter)}
              </span>

              <button
                type="button"
                class="copy-row-btn"
                onclick={() => copyRow(item.log)}
                title={$t('logs.copy_row')}
                aria-label={$t('logs.copy_row')}
              >
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  ><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path
                    d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
                  /></svg
                >
              </button>
            </div>
          {/each}
        </div>
      {/if}

      {#if totalItems === 0}
        <div class="empty-state">
          <svg
            width="32"
            height="32"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            ><circle cx="12" cy="12" r="10" /><line x1="8" y1="12" x2="16" y2="12" /></svg
          >
          <div class="empty-title">
            {!connected
              ? $t('logs.disconnected_title')
              : filter || sourceFilter || levelFilter
                ? $t('logs.no_filtered_logs')
                : $t('logs.no_logs')}
          </div>
          <div class="empty-desc">
            {!connected
              ? $t('logs.disconnected_desc')
              : connected
                ? $t('logs.waiting')
                : $t('logs.connect_hint')}
          </div>
        </div>
      {/if}

      <!-- Floating Paused Notification Banner -->
      {#if paused && pausedNewCount > 0}
        <div class="floating-pause-banner">
          <span>{$t('logs.paused_notice', { count: String(pausedNewCount) })}</span>
          <button class="btn btn-sm btn-primary" onclick={unpauseAndScroll}>
            {$t('logs.scroll_to_bottom')}
          </button>
        </div>
      {/if}
    </div>

    <!-- Status Bar / Stats Footer -->
    <div class="logs-footer">
      <div class="footer-stat">
        {pluralize(
          logs.length,
          $t('logs.buffer_count_one', { count: String(logs.length) }),
          $t('logs.buffer_count_few', { count: String(logs.length) }),
          $t('logs.buffer_count_many', { count: String(logs.length) }),
          $currentLang
        )}
      </div>
      <div class="footer-stat">
        {pluralize(
          availableSources.length,
          $t('logs.active_sources_count_one', { count: String(availableSources.length) }),
          $t('logs.active_sources_count_few', { count: String(availableSources.length) }),
          $t('logs.active_sources_count_many', { count: String(availableSources.length) }),
          $currentLang
        )}
      </div>
      <div class="footer-stat footer-live">
        <span class="live-dot" class:paused class:disconnected={!connected}></span>
        {$t('logs.realtime_label')}
      </div>
    </div>
  </div>
</div>

<style>
  .logs-page {
    display: flex;
    flex-direction: column;
    height: 100vh;
    box-sizing: border-box;
    padding: 24px 32px 16px;
    gap: 14px;
    background: var(--bg);
  }

  @media (max-width: 768px) {
    .logs-page {
      height: calc(100vh - 50px);
      padding: 16px;
      gap: 10px;
    }
  }

  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }

  .page-head h1 {
    margin: 4px 0 6px;
    font-size: 22px;
    font-weight: 700;
  }

  .page-head .sub {
    margin: 0;
    color: var(--fg-secondary);
    font-size: 13px;
  }

  .crumbs {
    font-size: 12px;
    color: var(--fg-dim);
    margin-bottom: 2px;
  }

  .crumb-sep {
    color: var(--fg-faint);
    margin: 0 6px;
  }

  .ph-actions {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-top: 6px;
  }

  .logs-page-container {
    display: flex;
    flex-direction: column;
    gap: 10px;
    flex: 1;
    min-height: 0;
  }

  /* Unified Toolbar (LOGS-01) */
  .logs-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 8px 12px;
  }

  .tb-group {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .btn-active {
    background: rgba(41, 194, 240, 0.15) !important;
    border-color: var(--accent) !important;
    color: var(--accent) !important;
  }

  .export-split {
    display: inline-flex;
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .export-split button:first-child {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }

  .export-split button:last-child {
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
    border-left: 1px solid var(--border);
    padding: 0 6px;
  }

  /* Search Input */
  .search-wrap {
    position: relative;
    display: flex;
    align-items: center;
  }

  .search-icon {
    position: absolute;
    left: 10px;
    color: var(--fg-dim);
    pointer-events: none;
  }

  .search-input {
    height: 30px;
    padding: 0 54px 0 28px;
    font-size: 12px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    color: var(--fg-primary);
    width: 200px;
    transition: all 0.15s ease;
  }

  .search-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(41, 194, 240, 0.2);
    width: 240px;
  }

  .match-badge {
    position: absolute;
    right: 22px;
    font-size: 10px;
    color: var(--accent);
    font-weight: 700;
    font-family: var(--font-family-mono);
  }

  .clear-search-btn {
    position: absolute;
    right: 6px;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: 14px;
    padding: 0 4px;
    line-height: 1;
  }

  .clear-search-btn:hover {
    color: var(--fg-primary);
  }

  /* Source Pills */
  .source-pills {
    display: inline-flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--bg-secondary);
    padding: 1px;
  }

  .source-pill {
    padding: 3px 8px;
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-dim);
    background: transparent;
    border: none;
    border-radius: 3px;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .source-pill:hover:not(.active) {
    color: var(--fg-primary);
    background: var(--bg-hover);
  }

  .source-pill.active {
    background: var(--accent);
    color: #03182a;
    font-weight: 700;
  }

  /* Level Select */
  .level-select {
    height: 30px;
    padding: 0 20px 0 8px;
    font-size: 11px;
    font-weight: 600;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    color: var(--fg-primary);
    cursor: pointer;
  }

  /* Log Console (LOGS-02) */
  .logs-console {
    flex: 1;
    min-height: 0;
    background: #071422;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    font-family: var(--font-family-mono);
    font-size: 12px;
    line-height: 1.4;
    overflow-y: auto;
    overflow-x: auto;
    position: relative;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }

  .lines-container.static-layout {
    display: flex;
    flex-direction: column;
    padding: 8px 0;
  }

  .log-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    box-sizing: border-box;
    white-space: nowrap;
    border-left: 2px solid transparent;
    transition: background 0.1s ease;
  }

  .logs-console.wrap-mode .log-row {
    white-space: normal;
    padding: 4px 12px;
    min-height: 26px;
    align-items: flex-start;
  }

  .log-row:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .log-row.row-error {
    background: rgba(244, 112, 127, 0.08);
    border-left-color: var(--danger, #f4707f);
  }

  .log-row.row-warning {
    background: rgba(245, 166, 35, 0.06);
    border-left-color: var(--warning, #f5a623);
  }

  .col-ts {
    width: 65px;
    color: #64748b;
    flex-shrink: 0;
    user-select: none;
    font-size: 11px;
  }

  .col-src {
    width: 60px;
    flex-shrink: 0;
    user-select: none;
  }

  .src-tag {
    display: inline-block;
    padding: 1px 5px;
    border-radius: 3px;
    background: rgba(255, 255, 255, 0.06);
    color: #94a3b8;
    font-size: 10px;
    font-weight: 600;
  }

  .col-level {
    width: 40px;
    flex-shrink: 0;
    user-select: none;
  }

  .lvl-badge {
    display: inline-block;
    padding: 1px 4px;
    border-radius: 3px;
    font-size: 9.5px;
    font-weight: 700;
    text-align: center;
    width: 28px;
  }

  .lvl-error {
    background: rgba(244, 112, 127, 0.2);
    color: #f4707f;
  }

  .lvl-warn {
    background: rgba(245, 166, 35, 0.2);
    color: #f5a623;
  }

  .lvl-info {
    background: rgba(255, 255, 255, 0.05);
    color: #94a3b8;
  }

  .lvl-debug {
    background: rgba(167, 139, 250, 0.2);
    color: #a78bfa;
  }

  .col-msg {
    flex: 1;
    color: #e2e8f0;
    word-break: break-all;
  }

  .log-row.row-error .col-msg {
    color: #fca5a5;
  }

  .log-row.row-warning .col-msg {
    color: #fde047;
  }

  :global(.log-mark) {
    background: #f59e0b;
    color: #000;
    padding: 0 2px;
    border-radius: 2px;
  }

  .copy-row-btn {
    opacity: 0;
    background: rgba(255, 255, 255, 0.08);
    border: none;
    color: #94a3b8;
    padding: 2px 5px;
    border-radius: 3px;
    cursor: pointer;
    margin-left: auto;
    transition: all 0.15s ease;
  }

  .log-row:hover .copy-row-btn {
    opacity: 1;
  }

  .copy-row-btn:hover {
    background: var(--accent);
    color: #03182a;
  }

  .floating-pause-banner {
    position: sticky;
    bottom: 12px;
    left: 50%;
    transform: translateX(-50%);
    display: inline-flex;
    align-items: center;
    gap: 12px;
    background: #14334f;
    border: 1px solid var(--accent);
    color: #e2e8f0;
    padding: 6px 14px;
    border-radius: 20px;
    font-size: 12px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    z-index: 10;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    padding: 40px 20px;
    color: var(--fg-dim);
    text-align: center;
    gap: 8px;
  }

  .empty-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .empty-desc {
    font-size: 12px;
    color: var(--fg-faint);
  }

  /* Footer Status Bar */
  .logs-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 8px;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .footer-stat {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .footer-live {
    color: var(--fg-secondary);
  }

  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #46d18a;
    box-shadow: 0 0 6px rgba(70, 209, 138, 0.6);
  }

  .live-dot.paused {
    background: #f5a623;
    box-shadow: 0 0 6px rgba(245, 166, 35, 0.6);
  }

  .live-dot.disconnected {
    background: #f4707f;
    box-shadow: 0 0 6px rgba(244, 112, 127, 0.6);
  }
</style>
