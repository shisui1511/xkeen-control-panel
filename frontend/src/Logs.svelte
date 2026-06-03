<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, pluralize } from './i18n';

  interface LogEntry {
    timestamp: string;
    source: string;
    level: string; // 'info' | 'warning' | 'error' | 'debug' | ''
    text: string;
    raw: string;
  }

  let logs = $state<LogEntry[]>([]);
  let ws = $state<WebSocket | null>(null);
  let connected = $state(false);
  let paused = $state(false);
  let filter = $state('');
  let sourceFilter = $state('');
  let levelFilter = $state('');
  let autoScroll = $state(true);
  let logContainer = $state<HTMLDivElement>();
  let availableSources = $state<string[]>([]);

  // Предопределённые вкладки источников
  const KNOWN_SOURCES = ['xkeen', 'xray', 'mihomo'];

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

  function parseLogLine(raw: string): LogEntry {
    let timestamp = '';
    let source = '';
    let level = '';
    let text = raw.trim();

    // 1. Сначала извлекаем префикс источника, добавляемый бэкендом: ^\[([^\]]+)\]\s*
    const bracketMatch = text.match(/^\[([^\]]+)\]\s*/);
    if (bracketMatch) {
      const tag = bracketMatch[1].toLowerCase();
      // Нормализуем источник
      if (tag.includes('access.log') || tag.includes('error.log') || tag === 'xray') {
        source = 'xray';
      } else if (tag.includes('mihomo.log') || tag === 'mihomo') {
        source = 'mihomo';
      } else if (tag.includes('xkeen-detached') || tag.includes('xkeen.log') || tag === 'xkeen') {
        source = 'xkeen';
      } else {
        source = bracketMatch[1];
      }
      text = text.substring(bracketMatch[0].length).trim();
    }

    // 2. Теперь извлекаем таймстамп из начала строки
    // Поддерживаем форматы: 2026/05/23 00:58:33, 2026-05-23T00:58:33+03:00, 2026-05-23 00:58:33, 00:58:33
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

    // 3. Парсим Bracket теги в оставшемся тексте (например, уровень лога [INF], [Warning])
    const tags: string[] = [];
    let tempText = text;
    while (true) {
      const tagMatch = tempText.match(/^\[([^\]]+)\]\s*/);
      if (!tagMatch) break;
      tags.push(tagMatch[1]);
      tempText = tempText.substring(tagMatch[0].length).trim();
    }

    // Ищем уровень лога в тегах
    for (const tag of tags) {
      const lowerTag = tag.toLowerCase();
      if (['info', 'inf', 'information'].includes(lowerTag)) {
        level = 'info';
      } else if (['warning', 'warn', 'wrn'].includes(lowerTag)) {
        level = 'warn';
      } else if (['error', 'err'].includes(lowerTag)) {
        level = 'err';
      } else if (['debug', 'dbg'].includes(lowerTag)) {
        level = 'debug';
      } else if (!source) {
        // Если источник еще не определен, берем первый неизвестный тег
        if (lowerTag === 'xray') source = 'xray';
        else if (lowerTag === 'mihomo') source = 'mihomo';
        else if (lowerTag === 'xkeen') source = 'xkeen';
      }
    }

    // Дефолтный источник на основе содержимого, если не определен
    if (!source) {
      const lowerRaw = raw.toLowerCase();
      if (lowerRaw.includes('xray')) {
        source = 'xray';
      } else if (lowerRaw.includes('mihomo')) {
        source = 'mihomo';
      } else {
        source = 'xkeen';
      }
    }

    // Fallback детекция уровня из текста
    if (!level) {
      const lowerText = text.toLowerCase();
      if (lowerText.includes('error') || lowerText.includes('err:')) {
        level = 'err';
      } else if (lowerText.includes('warning') || lowerText.includes('warn:')) {
        level = 'warn';
      } else if (lowerText.includes('debug') || lowerText.includes('dbg:')) {
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
            if (logContainer) {
              logContainer.scrollTop = logContainer.scrollHeight;
            }
          }, 0);
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
    return filteredLogs;
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
    const mainContent = document.querySelector('.main-content') as HTMLElement;
    if (mainContent) {
      mainContent.style.overflowY = 'hidden';
    }
  });

  onDestroy(() => {
    disconnect();
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
        {$t('nav.group_services')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('nav.logs')}
      </div>
      <h1>{$t('logs.h1')}</h1>
      <p class="sub">{$t('logs.h1_sub')}</p>
    </div>
    <div class="ph-actions">
      <span class="status-indicator" class:connected>
        {connected ? $t('logs.status_connected') : $t('logs.status_disconnected')}
      </span>
      {#if !connected}
        <button on:click={connect} class="btn btn-primary" title={$t('logs.connect')}
          >{$t('logs.connect')}</button
        >
      {/if}
    </div>
  </div>

  <div class="logs-page-container">
    <!-- filter toolbar -->
    <div class="toolbar">
      <div class="toolbar-left">
        <button
          class="btn btn-secondary"
          on:click={togglePause}
          title={paused ? $t('logs.resume') : $t('logs.pause')}
        >
          {#if paused}
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              style="margin-right: 6px;"><polygon points="5 3 19 12 5 21 5 3" /></svg
            >{$t('logs.resume')}
          {:else}
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              style="margin-right: 6px;"
              ><rect x="6" y="5" width="4" height="14" rx="1" /><rect
                x="14"
                y="5"
                width="4"
                height="14"
                rx="1"
              /></svg
            >{$t('logs.pause')}
          {/if}
        </button>

        <button class="btn btn-secondary" on:click={clearLogs} title={$t('logs.clear')}>
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"
            ><polyline points="3 6 5 6 21 6" /><path
              d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"
            /></svg
          >{$t('logs.clear')}
        </button>

        <button class="btn btn-secondary" on:click={exportLogs} title={$t('logs.export')}>
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"
            ><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline
              points="7 10 12 15 17 10"
            /><line x1="12" y1="15" x2="12" y2="3" /></svg
          >{$t('logs.export')}
        </button>
      </div>

      <div class="toolbar-right">
        <input
          type="text"
          class="filter-input"
          placeholder={$t('logs.filter')}
          bind:value={filter}
        />

        <!-- Вкладки источников -->
        <div class="source-tabs" role="group" aria-label={$t('logs.source')}>
          <button
            class="stab"
            class:stab-active={sourceFilter === ''}
            on:click={() => (sourceFilter = '')}>{$t('logs.all_sources')}</button
          >
          {#each KNOWN_SOURCES as src}
            <button
              class="stab"
              class:stab-active={sourceFilter === src}
              on:click={() => (sourceFilter = sourceFilter === src ? '' : src)}>{src}</button
            >
          {/each}
        </div>

        <select bind:value={levelFilter} class="source-select">
          <option value="">{$t('logs.all_levels')}</option>
          <option value="info">info</option>
          <option value="warning">warning</option>
          <option value="error">error</option>
          <option value="debug">debug</option>
        </select>

        <label class="toggle-label">
          <label class="toggle-switch">
            <input type="checkbox" bind:checked={autoScroll} />
            <span class="toggle-slider"></span>
          </label>
          {$t('logs.autoscroll')}
        </label>
      </div>
    </div>

    {#if !connected}
      <div
        class="alert alert-danger"
        style="margin: 0; padding: 10px 14px; border-radius: var(--radius-md); font-size: 13px; display: flex; justify-content: space-between; align-items: center; border: 1px solid var(--border);"
      >
        <span
          ><strong>{$t('logs.disconnected_title')}</strong> — {$t('logs.disconnected_desc')}</span
        >
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
        <div class="logs-empty-placeholder">
          <svg
            width="24"
            height="24"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            class="empty-icon"
            style="margin-bottom: 12px; opacity: 0.6;"
          >
            <circle cx="12" cy="12" r="10" />
            <line x1="8" y1="12" x2="16" y2="12" />
          </svg>
          <div
            class="empty-title"
            style="font-size: 14px; font-weight: 500; color: var(--fg-secondary); margin-bottom: 6px;"
          >
            {!connected
              ? $t('logs.disconnected_title')
              : filter || sourceFilter || levelFilter
                ? $t('logs.no_filtered_logs')
                : $t('logs.no_logs')}
          </div>
          <div
            class="empty-desc"
            style="font-size: 12px; color: var(--fg-faint); max-width: 280px; line-height: 1.4;"
          >
            {!connected
              ? $t('logs.disconnected_desc')
              : connected
                ? $t('logs.waiting')
                : $t('logs.connect_hint')}
          </div>
          {#if !connected}
            <button
              on:click={connect}
              class="btn btn-secondary btn-small"
              style="margin-top: 14px; padding: 4px 8px; font-size: 12px;"
            >
              {$t('logs.reconnect')}
            </button>
          {/if}
        </div>
      {/if}
    </div>

    <div class="stats">
      <span class="stat"
        ><b>{logs.length}</b>
        {$t('logs.buffer_count', { count: logs.length })
          .replace(String(logs.length), '')
          .trim()}</span
      >
      <span class="stat"
        ><b>{availableSources.length}</b>
        {pluralize(availableSources.length, $t('logs.source_count_one', { count: '' }).trim(), $t('logs.source_count_few', { count: '' }).trim(), $t('logs.source_count_many', { count: '' }).trim())}</span
      >
      <span class="stat">{$t('logs.realtime_label')}</span>
    </div>
  </div>
</div>

<style>
  .logs-page {
    display: flex;
    flex-direction: column;
    height: 100vh;
    box-sizing: border-box;
    padding: 28px 36px 16px;
    gap: 16px;
    background: var(--bg);
  }

  @media (max-width: 768px) {
    .logs-page {
      height: calc(100vh - 50px);
      padding: 16px 16px 12px;
      gap: 12px;
    }
  }

  .logs-page-container {
    display: flex;
    flex-direction: column;
    gap: 12px;
    flex: 1;
    min-height: 0;
  }

  .logs-pane {
    flex: 1;
    min-height: 0;
    background: #050d16;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
    font-family: var(--font-family-mono);
    font-size: 12.5px;
    line-height: 1.5;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
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

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    background: var(--bg);
    padding: 4px 0 10px;
    border-bottom: 1px solid var(--border-light);
  }

  .toolbar-left {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 12px;
    flex: 1;
    min-width: 300px;
    justify-content: flex-end;
  }

  /* Unified sizing and style for all toolbar controls */
  .toolbar :global(.btn) {
    height: 34px;
    padding: 0 14px;
    font-size: 13px;
    font-weight: 600;
    border-radius: var(--radius-md);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    box-sizing: border-box;
  }

  .toolbar .filter-input {
    height: 34px;
    padding: 0 12px;
    font-size: 12.5px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: var(--bg-card);
    color: var(--fg-primary);
    box-sizing: border-box;
    flex: 1;
    max-width: 280px;
    min-width: 120px;
    transition:
      border-color var(--transition-fast),
      box-shadow var(--transition-fast);
  }
  .toolbar .filter-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .toolbar .source-select {
    height: 34px;
    padding: 0 28px 0 12px;
    font-size: 12.5px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: var(--bg-card);
    color: var(--fg-primary);
    box-sizing: border-box;
    cursor: pointer;
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='8' height='8' viewBox='0 0 24 24' fill='none' stroke='%238aa0b7' stroke-width='3' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpolyline points='6 9 12 15 18 9'%3E%3C/polyline%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 10px center;
    background-size: 10px;
    transition:
      border-color var(--transition-fast),
      box-shadow var(--transition-fast);
  }
  .toolbar .source-select:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .toolbar .source-tabs {
    display: inline-flex;
    height: 34px;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    background: var(--bg-secondary);
    box-sizing: border-box;
    padding: 2px;
  }

  .toolbar .stab {
    height: 100%;
    display: inline-flex;
    align-items: center;
    padding: 0 14px;
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-secondary);
    background: transparent;
    border: none;
    border-radius: calc(var(--radius-md) - 2px);
    cursor: pointer;
    transition:
      background 0.15s,
      color 0.15s;
    white-space: nowrap;
  }
  .toolbar .stab:hover:not(.stab-active) {
    background: var(--bg-hover);
    color: var(--fg-primary);
  }
  .toolbar .stab.stab-active {
    background: var(--accent);
    color: #03182a;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--fg-secondary);
    cursor: pointer;
    flex-shrink: 0;
  }

  .status-indicator {
    font-size: 12px;
    color: var(--fg-dim);
    padding: 3px 8px;
    border-radius: 8px;
    border: 1px solid var(--border);
    white-space: nowrap;
  }
  .status-indicator.connected {
    color: #4ade80;
    border-color: rgba(74, 222, 128, 0.3);
    background: rgba(74, 222, 128, 0.06);
  }

  .stats {
    display: flex;
    gap: 16px;
    font-size: 11.5px;
    color: var(--fg-dim);
    padding: 4px 0;
    flex-shrink: 0;
  }
  .stat b {
    color: var(--fg-secondary);
  }

  .logs-empty-placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 60px 20px;
    text-align: center;
    color: var(--fg-dim);
    height: 100%;
  }
</style>
