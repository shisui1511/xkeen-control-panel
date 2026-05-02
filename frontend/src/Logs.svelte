<script lang="ts">
  import { onMount, onDestroy } from 'svelte'

  let logs: string[] = []
  let ws: WebSocket | null = null
  let connected = false
  let paused = false
  let filter = ''
  let autoScroll = true
  let logContainer: HTMLDivElement

  function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/logs/ws`
    
    ws = new WebSocket(wsUrl)
    
    ws.onopen = () => {
      connected = true
      logs.push('[Подключено к серверу логов]')
    }
    
    ws.onmessage = (event) => {
      if (!paused) {
        logs.push(event.data)
        logs = logs.slice(-1000) // Keep last 1000 lines
        
        if (autoScroll && logContainer) {
          setTimeout(() => {
            logContainer.scrollTop = logContainer.scrollHeight
          }, 10)
        }
      }
    }
    
    ws.onerror = () => {
      connected = false
      logs.push('[Ошибка подключения к серверу логов]')
    }
    
    ws.onclose = () => {
      connected = false
      logs.push('[Отключено от сервера логов]')
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
  }

  function togglePause() {
    paused = !paused
  }

  function toggleAutoScroll() {
    autoScroll = !autoScroll
  }

  function getFilteredLogs() {
    if (!filter) return logs
    const lowerFilter = filter.toLowerCase()
    return logs.filter(log => log.toLowerCase().includes(lowerFilter))
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
    </div>
    
    <div class="toolbar-right">
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
        <span class="log-text">{log}</span>
      </div>
    {/each}
    
    {#if getFilteredLogs().length === 0}
      <div class="empty-state">
        {filter ? 'Нет логов, соответствующих фильтру' : 'Нет логов'}
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
  }

  .status-indicator {
    font-size: 0.875rem;
    color: var(--text-secondary);
  }

  .status-indicator.connected {
    color: var(--success, #28a745);
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
  }

  .log-number {
    color: var(--text-secondary);
    min-width: 50px;
    text-align: right;
    user-select: none;
  }

  .log-text {
    color: var(--text);
    word-break: break-all;
    white-space: pre-wrap;
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-secondary);
  }
</style>
