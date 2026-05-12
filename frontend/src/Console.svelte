<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'
  import PageHeader from './PageHeader.svelte'

  export let onSwitchTab: (tab: string) => void = () => {}

  interface CommandDef {
    name: string
    description: string
    command: string
    dangerous: boolean
  }

  interface CommandCategory {
    name: string
    commands: CommandDef[]
  }

  interface CommandResult {
    success: boolean
    output: string
    error: string
  }

  let categories: CommandCategory[] = []
  let loading = false
  let error = ''
  let executing = ''
  let output = ''
  let history: { command: string; output: string; success: boolean }[] = []

  async function fetchCommands() {
    try {
      const res = await fetch('/api/console/commands')
      if (!res.ok) throw new Error('Failed to load commands')
      categories = await res.json()
    } catch (e: any) {
      error = e.message
    }
  }

  async function executeCommand(command: string) {
    executing = command
    output = ''
    error = ''
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      const res = await fetch('/api/console/execute', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ command })
      })
      const result: CommandResult = await res.json()
      output = result.output || result.error
      history.unshift({ command, output, success: result.success })
      if (history.length > 20) history = history.slice(0, 20)
      if (!result.success) {
        error = result.error || $t('app.error')
      }
    } catch (e: any) {
      error = e.message
      output = e.message
    } finally {
      executing = ''
    }
  }

  onMount(fetchCommands)
</script>

<div class="container">
  <PageHeader
    title={$t('console.title')}
    subtitle={$t('console.subtitle')}
    breadcrumbs={[{ label: $t('console.title') }]}
    {onSwitchTab}
  />

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <div class="console-layout">
    <div class="commands-panel">
      {#each categories as category}
        <div class="category">
          <h3 class="category-title">{$t('console.cat_' + category.name) || category.name}</h3>
          <div class="command-list">
            {#each category.commands as cmd}
              <button
                class="btn cmd-btn"
                class:dangerous={cmd.dangerous}
                on:click={() => executeCommand(cmd.command)}
                disabled={executing === cmd.command}
              >
                <span class="cmd-name">{cmd.name}</span>
                <span class="cmd-desc">{cmd.description}</span>
              </button>
            {/each}
          </div>
        </div>
      {/each}
    </div>

    <div class="output-panel">
      <div class="panel-header">
        <h3>{$t('console.output')}</h3>
        {#if output}
          <button class="btn btn-secondary btn-sm" on:click={() => { output = '' }}>✕</button>
        {/if}
      </div>
      <pre class="output-box">{output || $t('console.no_output')}</pre>

      {#if history.length > 0}
        <h4 class="mt-2">{$t('console.history')}</h4>
        <div class="history-list">
          {#each history as entry, i}
            <div class="history-item" class:error={!entry.success} on:click={() => { output = entry.output }}>
              <span class="history-cmd">${entry.command}</span>
              <span class="history-status">{entry.success ? '✓' : '✗'}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .console-layout {
    display: grid;
    grid-template-columns: 280px 1fr;
    gap: 1rem;
  }

  .commands-panel {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .category-title {
    font-size: 0.85rem;
    font-weight: 600;
    margin: 0 0 0.5rem 0;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .command-list {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .cmd-btn {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    padding: 0.5rem 0.75rem;
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    text-align: left;
    width: 100%;
    transition: background 0.15s;
  }

  .cmd-btn:hover {
    background: var(--hover);
  }

  .cmd-btn.dangerous {
    border-color: var(--danger, #dc3545);
  }

  .cmd-name {
    font-weight: 600;
    font-size: 0.875rem;
  }

  .cmd-desc {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .output-panel {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .panel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .panel-header h3 {
    margin: 0;
  }

  .output-box {
    background: var(--code-bg, #0d1117);
    color: var(--code-fg, #e6edf3);
    padding: 1rem;
    border-radius: var(--radius);
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 0.8rem;
    overflow-x: auto;
    min-height: 200px;
    max-height: 500px;
    white-space: pre-wrap;
    word-wrap: break-word;
  }

  .history-list {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    max-height: 300px;
    overflow-y: auto;
  }

  .history-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.375rem 0.75rem;
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.8rem;
  }

  .history-item:hover {
    background: var(--hover);
  }

  .history-item.error {
    border-color: var(--danger, #dc3545);
  }

  .history-cmd {
    font-family: monospace;
  }

  .history-status {
    font-weight: 600;
  }

  .btn-sm {
    font-size: 0.75rem;
    padding: 0.25rem 0.5rem;
  }

  .mt-2 {
    margin-top: 0.5rem;
  }

  @media (max-width: 768px) {
    .console-layout {
      grid-template-columns: 1fr;
    }
  }
</style>