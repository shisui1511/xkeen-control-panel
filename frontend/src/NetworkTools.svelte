<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'
  import PageHeader from './PageHeader.svelte'

  export let onSwitchTab: (tab: string) => void = () => {}

  interface ToolResult {
    success: boolean
    output?: string
    records?: string[]
    ip?: string
    error?: string
  }

  let activeTool = 'ping'
  let host = ''
  let url = ''
  let recordType = 'A'
  let count = 4
  let maxHops = 20
  let timeout = 10
  let loading = false
  let result: ToolResult | null = null
  let publicIP = ''

  const recordTypes = ['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT']

  async function runPing() {
    if (!host) return
    loading = true
    try {
      const res = await fetch('/api/network/ping', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host, count })
      })
      result = await res.json()
    } catch (e) {
      result = { success: false, error: 'Request failed' }
    } finally {
      loading = false
    }
  }

  async function runTraceroute() {
    if (!host) return
    loading = true
    try {
      const res = await fetch('/api/network/traceroute', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host, max_hops: maxHops })
      })
      result = await res.json()
    } catch (e) {
      result = { success: false, error: 'Request failed' }
    } finally {
      loading = false
    }
  }

  async function runDNS() {
    if (!host) return
    loading = true
    try {
      const res = await fetch('/api/network/dns', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host, record_type: recordType })
      })
      result = await res.json()
    } catch (e) {
      result = { success: false, error: 'Request failed' }
    } finally {
      loading = false
    }
  }

  async function runHTTP() {
    if (!url) return
    loading = true
    try {
      const res = await fetch('/api/network/http', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url, timeout })
      })
      result = await res.json()
    } catch (e) {
      result = { success: false, error: 'Request failed' }
    } finally {
      loading = false
    }
  }

  async function fetchIP() {
    try {
      const res = await fetch('/api/network/ip')
      const data = await res.json()
      if (data.success) {
        publicIP = data.ip
      }
    } catch (e) {
      // ignore
    }
  }

  function runTool() {
    switch (activeTool) {
      case 'ping': return runPing()
      case 'traceroute': return runTraceroute()
      case 'dns': return runDNS()
      case 'http': return runHTTP()
    }
  }

  onMount(() => {
    fetchIP()
  })
</script>

<div class="container">
  <PageHeader
    title="Сетевые инструменты"
    subtitle="Ping, traceroute, DNS lookup, HTTP test"
    breadcrumbs={[{ label: 'Сетевые инструменты' }]}
    {onSwitchTab}
  />

  {#if publicIP}
    <div class="card mb-2">
      <div style="display: flex; align-items: center; gap: 0.5rem;">
        <span>🌍</span>
        <span>Ваш IP: <strong>{publicIP}</strong></span>
      </div>
    </div>
  {/if}

  <div class="card mb-2">
    <div class="tool-tabs">
      <button class="tool-tab" class:active={activeTool === 'ping'} on:click={() => { activeTool = 'ping'; result = null; }}>
        📡 Ping
      </button>
      <button class="tool-tab" class:active={activeTool === 'traceroute'} on:click={() => { activeTool = 'traceroute'; result = null; }}>
        🔍 Traceroute
      </button>
      <button class="tool-tab" class:active={activeTool === 'dns'} on:click={() => { activeTool = 'dns'; result = null; }}>
        🌐 DNS
      </button>
      <button class="tool-tab" class:active={activeTool === 'http'} on:click={() => { activeTool = 'http'; result = null; }}>
        🚀 HTTP Test
      </button>
    </div>

    <div class="tool-form">
      {#if activeTool === 'ping'}
        <div class="form-row">
          <label>Хост/IP:</label>
          <input type="text" class="input" bind:value={host} placeholder="google.com" />
        </div>
        <div class="form-row">
          <label>Количество пакетов:</label>
          <input type="number" class="input" bind:value={count} min="1" max="20" />
        </div>
      {:else if activeTool === 'traceroute'}
        <div class="form-row">
          <label>Хост/IP:</label>
          <input type="text" class="input" bind:value={host} placeholder="google.com" />
        </div>
        <div class="form-row">
          <label>Максимум прыжков:</label>
          <input type="number" class="input" bind:value={maxHops} min="1" max="30" />
        </div>
      {:else if activeTool === 'dns'}
        <div class="form-row">
          <label>Домен:</label>
          <input type="text" class="input" bind:value={host} placeholder="google.com" />
        </div>
        <div class="form-row">
          <label>Тип записи:</label>
          <select class="input" bind:value={recordType}>
            {#each recordTypes as type}
              <option value={type}>{type}</option>
            {/each}
          </select>
        </div>
      {:else if activeTool === 'http'}
        <div class="form-row">
          <label>URL:</label>
          <input type="text" class="input" bind:value={url} placeholder="https://google.com" />
        </div>
        <div class="form-row">
          <label>Таймаут (сек):</label>
          <input type="number" class="input" bind:value={timeout} min="1" max="60" />
        </div>
      {/if}

      <button class="btn btn-primary" on:click={runTool} disabled={loading || (activeTool === 'http' ? !url : !host)}>
        {loading ? 'Выполняется...' : 'Запустить'}
      </button>
    </div>
  </div>

  {#if result}
    <div class="card">
      <h3>Результат</h3>
      {#if result.success}
        <div class="result-success">✅ Успешно</div>
      {:else}
        <div class="result-error">❌ Ошибка: {result.error || 'Неизвестная ошибка'}</div>
      {/if}

      {#if result.output}
        <pre class="result-output">{result.output}</pre>
      {/if}

      {#if result.records}
        <div class="result-records">
          {#each result.records as record}
            <div class="record-item">{record}</div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tool-tabs {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 1rem;
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.5rem;
  }

  .tool-tab {
    background: none;
    border: none;
    color: var(--fg-secondary);
    padding: 0.5rem 1rem;
    cursor: pointer;
    border-radius: var(--radius);
    font-size: 0.9rem;
  }

  .tool-tab:hover {
    background: var(--bg-hover);
  }

  .tool-tab.active {
    background: var(--primary);
    color: white;
  }

  .tool-form {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .form-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .form-row label {
    min-width: 140px;
    color: var(--fg-secondary);
    font-size: 0.9rem;
  }

  .form-row .input {
    flex: 1;
  }

  .result-success {
    color: var(--success);
    margin-bottom: 0.5rem;
  }

  .result-error {
    color: var(--error);
    margin-bottom: 0.5rem;
  }

  .result-output {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.75rem;
    margin: 0;
    font-size: 0.8rem;
    overflow-x: auto;
    max-height: 400px;
    overflow-y: auto;
  }

  .result-records {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .record-item {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.5rem;
    font-family: monospace;
    font-size: 0.85rem;
  }
</style>
