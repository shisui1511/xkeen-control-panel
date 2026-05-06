<script lang="ts">
  import { onMount, onDestroy } from 'svelte'

  interface LogEntry {
    text: string
    source: string
    raw: string
  }

  let logs: LogEntry[] = []
  let ws: WebSocket | null = null
  let connected = false
  let paused = false
  let filter = ''
  let sourceFilter = ''
  let autoScroll = true
  let logContainer: HTMLDivElement
  let availableSources: string[] = []

  function parseLogLine(raw: string): LogEntry {
    const match = raw.match(/^\[([^\]]+)\]\s*(.*)$/)
    if (match) {
      return { source: match[1], text: match[2], raw }
    }
    return { source: '', text: raw, raw }
  }

  function updateSources() {
    const sources = new Set<string>()
    for (const log of logs) {
      if (log.source) sources.add(log.source)
    }
    availableSources = Array.from(sources).sort()
  }

  function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/logs/ws`
    
    ws = new WebSocket(wsUrl)
    
    ws.onopen = () => {
      connected = true
      logs = [...logs, { text: '[Подключено к серверу логов]', source: '', raw: '[Подключено к серверу логов]' }]
    }
    
    ws.onmessage = (event) => {
      if (!paused) {
        const entry = parseLogLine(event.data)
        logs = [...logs, entry].slice(-1000)
        updateSources()
        
        if (autoScroll && logContainer) {
          setTimeout(() => {
            logContainer.scrollTop = logContainer.scrollHeight
          }, 10)
        }
      }
    }
    
    ws.onerror = () => {
      connected = false
      logs = [...logs, { text: '[Ошибка подключения к серверу логов]', source: '', raw: '[Ошибка подключения к серверу логов]' }]
    }
    
    ws.onclose = () => {
      connected = false
      logs = [...logs, { text: '[Отключено от сервера логов]', source: '', raw: '[Отключено от сервера логов]' }]
    }
  }

  function disconnect() {
    if (ws) {
      ws.close()
      ws = null
    }
  }

  function clearLogs() {
    logs = []
    availableSources = []
  }

  function togglePause() {
    paused = !paused
  }

  function toggleAutoScroll() {
    autoScroll = !autoScroll
  }

  function getFilteredLogs(): LogEntry[] {
    let result = logs
    if (filter) {
      const lowerFilter = filter.toLowerCase()
      result = result.filter(log => log.raw.toLowerCase().includes(lowerFilter))
    }
    if (sourceFilter) {
      result = result.filter(log => log.source === sourceFilter)
    }
    return result
  }

  function getSourceColor(source: string): string {
    if (!source) return 'var(--text-secondary)'
    const colors = ['#58a6ff', '#a371f7', '#3fb950', '#d29922', '#f85149']
    let hash = 0
    for (let i = 0; i < source.length; i++) {
      hash = source.charCodeAt(i) + ((hash << 5) - hash)
    }
    return colors[Math.abs(hash) % colors.length]
  }

  onMount(() => {
    connect()
  })

  onDestroy(() => {
    disconnect()
  })
</script>

<div class="logs-page">
  <div class="toolbar">
    <div class="toolbar-left">
      <h2>Логи</h2>
      <span class="status-indicator" class:connected>
        {connected ? '● Подключено' : '○ Отключено'}
      </span>
      {#if availableSources.length > 0}
        <span class="source-count">{availableSources.length} источника</span>
      {/if}
    </div>
    
    <div class="toolbar-right">
      {#if availableSources.length > 0}
        <select bind:value={sourceFilter} class="source-select" title="Фильтр по источнику">
          <option value="">Все источники</option>
          {#each availableSources as source}
            <option value={source}>{source}</option>
          {/each}
        </select>
      {/if}

      <input 
        type="text" 
        placeholder="Фильтр..." 
        bind:value={filter}
        class="filter-input"
      />
      
      <button on:click={togglePause} class="btn-icon" title={paused ? 'Возобновить' : 'Пауза'}>
        {paused ? '▶' : '⏸'}
      </button>
      
      <button on:click={toggleAutoScroll} class="btn-icon" class:active={autoScroll} title="Авто-прокрутка">
        ⬇
      </button>
      
      <button on:click={clearLogs} class="btn-icon" title="Очистить">
        🗑
      </button>
      
      {#if connected}
        <button on:click={disconnect} class="btn-small btn-danger">Отключить</button>
      {:else}
        <button on:click={connect} class="btn-small btn-primary">Подключить</button>
      {/if}
    </div>
  </div>

  <div class="log-container" bind:this={logContainer}>
    {#each getFilteredLogs() as log, i}
      <div class="log-line">
        <span class="log-number">{i + 1}</span>
        {#if log.source}
          <span class="log-source" style="color: {getSourceColor(log.source)}">{log.source}</span>
        {/if}
        <span class="log-text">{log.text}</span>
      </div>
    {/each}
    
    {#if getFilteredLogs().length === 0}
      <div class="empty-state">
        {filter || sourceFilter ? 'Нет логов, соответствующих фильтру' : 'Нет логов'}
      </div>
    {/if}
  </div>
</div>

<style>
  .logs-page {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: var(--bg);
  }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 1rem;
    background: var(--card-bg);
    border-bottom: 1px solid var(--border);
    gap: 1rem;
    flex-wrap: wrap;
  }

  .toolbar-left {
    display: flex;
    align-items: center;
    gap: 1rem;
  }

  .toolbar-left h2 {
    margin: 0;
    font-size: 1.25rem;
  }

  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .status-indicator {
    font-size: 0.875rem;
    color: var(--text-secondary);
  }

  .status-indicator.connected {
    color: var(--success, #28a745);
  }

  .source-count {
    font-size: 0.75rem;
    color: var(--text-secondary);
    background: var(--bg);
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
  }

  .filter-input {
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--text);
    font-size: 0.875rem;
    min-width: 200px;
  }

  .source-select {
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--text);
    font-size: 0.875rem;
  }

  .btn-icon {
    padding: 0.5rem;
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    font-size: 1rem;
    color: var(--text);
    transition: background 0.2s;
  }

  .btn-icon:hover {
    background: var(--hover);
  }

  .btn-icon.active {
    background: var(--primary);
    color: white;
    border-color: var(--primary);
  }

  .btn-small {
    padding: 0.5rem 1rem;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.875rem;
    transition: opacity 0.2s;
  }

  .btn-small:hover {
    opacity: 0.9;
  }

  .btn-primary {
    background: var(--primary);
    color: white;
  }

  .btn-danger {
    background: var(--danger, #dc3545);
    color: white;
  }

  .log-container {
    flex: 1;
    overflow-y: auto;
    padding: 1rem;
    font-family: 'Courier New', monospace;
    font-size: 0.875rem;
    line-height: 1.5;
    background: var(--bg);
  }

  .log-line {
    display: flex;
    gap: 1rem;
    padding: 0.25rem 0;
    border-bottom: 1px solid var(--border-light, rgba(0,0,0,0.05));
    align-items: flex-start;
  }

  .log-number {
    color: var(--text-secondary);
    min-width: 50px;
    text-align: right;
    user-select: none;
    flex-shrink: 0;
  }

  .log-source {
    font-weight: 600;
    min-width: 120px;
    flex-shrink: 0;
    font-size: 0.75rem;
    padding: 0.125rem 0.375rem;
    background: rgba(0,0,0,0.05);
    border-radius: 3px;
    text-align: center;
  }

  .log-text {
    color: var(--text);
    word-break: break-all;
    white-space: pre-wrap;
    flex: 1;
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-secondary);
  }
</style>
