<script lang="ts">
  import { onMount } from 'svelte'
  import { slide } from 'svelte/transition'
  import { t } from './i18n'
  import PageHeader from './PageHeader.svelte'
  import Modal from './components/Modal.svelte'
  import Icon from './lib/components/Icon.svelte'

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

  // Confirmation modal state
  let confirmPending: CommandDef | null = null

  async function fetchCommands() {
    try {
      const res = await fetch('/api/console/commands')
      if (!res.ok) throw new Error($t('console.load_error'))
      categories = await res.json()
    } catch (e: any) {
      error = e.message
    }
  }

  async function executeCommand(command: string) {
    executing = command
    output = ''
    error = ''
    confirmPending = null
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

  function handleCommandClick(cmd: CommandDef) {
    if (cmd.dangerous) {
      confirmPending = cmd
    } else {
      executeCommand(cmd.command)
    }
  }

  function cancelConfirm() {
    confirmPending = null
  }

  function confirmExecute() {
    if (confirmPending) {
      executeCommand(confirmPending.command)
    }
  }

  onMount(fetchCommands)
</script>

<!-- Confirmation modal for dangerous commands -->
<Modal
  isOpen={confirmPending !== null}
  title={$t('console.confirm_title')}
  onclose={cancelConfirm}
>
  {#if confirmPending}
    <p style="margin: 0 0 var(--spacing-6); line-height: 1.5; color: var(--color-text-secondary);">
      {$t('console.confirm_desc', { name: confirmPending.name })}
    </p>
    <div style="display: flex; gap: var(--spacing-3); justify-content: flex-end;">
      <button
        class="btn btn-secondary"
        on:click={cancelConfirm}
        title={$t('app.cancel')}
      >
        {$t('app.cancel')}
      </button>
      <button
        class="btn btn-danger"
        on:click={confirmExecute}
        title={$t('app.confirm')}
      >
        <Icon name="warning" size={14} /> {$t('app.confirm')}
      </button>
    </div>
  {/if}
</Modal>

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
        <details class="category-details" open>
          <summary class="category-title" title={$t('console.cat_' + category.name) || category.name}>
            {$t('console.cat_' + category.name) || category.name}
            <span class="cat-arrow">▶</span>
          </summary>
          <div class="command-list commands-grid" transition:slide={{ duration: 180 }}>
            {#each category.commands as cmd}
              <button
                class="btn cmd-btn"
                class:dangerous={cmd.dangerous}
                on:click={() => handleCommandClick(cmd)}
                disabled={executing === cmd.command}
                title={cmd.description}
              >
                {#if cmd.dangerous}
                  <span class="danger-icon" aria-label={$t('console.danger_label')}><Icon name="warning" size={14} /></span>
                {/if}
                <span class="cmd-name">{cmd.name}</span>
                <span class="cmd-desc">{cmd.description}</span>
              </button>
            {/each}
          </div>
        </details>
      {/each}
    </div>

    <div class="output-panel">
      <div class="panel-header">
        <h3>{$t('console.output')}</h3>
        {#if output}
          <button
            class="btn btn-secondary btn-sm"
            on:click={() => { output = '' }}
            title={$t('app.close')}
          ><Icon name="cross" size={14} /></button>
        {/if}
      </div>
      <pre class="output-box">{output || $t('console.no_output')}</pre>

      {#if history.length > 0}
        <h4 class="mt-2">{$t('console.history')}</h4>
        <div class="history-list">
          {#each history as entry}
            <button
              class="history-item"
              class:error={!entry.success}
              on:click={() => { output = entry.output }}
              title={entry.command}
            >
              <span class="history-cmd">${entry.command}</span>
              <span class="history-status"><Icon name={entry.success ? 'check' : 'cross'} size={12} /></span>
            </button>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .console-layout {
    display: grid;
    grid-template-columns: 320px 1fr;
    gap: 1rem;
  }

  .commands-panel {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .category-details {
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .category-title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.6rem 0.75rem;
    font-size: 0.8rem;
    font-weight: 700;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    cursor: pointer;
    list-style: none;
    user-select: none;
  }

  .category-title::-webkit-details-marker {
    display: none;
  }

  .category-title:hover {
    color: var(--accent);
  }

  .cat-arrow {
    font-size: 10px;
    transition: transform 0.2s ease;
  }

  .category-details[open] .cat-arrow {
    transform: rotate(90deg);
  }

  .command-list {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0.25rem 0.5rem 0.5rem;
  }

  .commands-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 0.4rem;
  }

  .cmd-btn {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    padding: 0.5rem 0.75rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    text-align: left;
    width: 100%;
    color: var(--fg-primary, #111111);
    transition: background 0.15s;
    gap: 0.25rem;
  }

  .cmd-btn:hover {
    background: var(--hover);
  }

  .cmd-btn.dangerous {
    border-color: var(--danger);
    background: rgba(239, 68, 68, 0.04);
  }

  .cmd-btn.dangerous:hover {
    background: rgba(239, 68, 68, 0.1);
  }

  .cmd-btn.dangerous .cmd-name {
    color: var(--danger);
  }

  .danger-icon {
    flex-shrink: 0;
    font-size: 0.9rem;
  }

  .cmd-name {
    font-weight: 600;
    font-size: 0.875rem;
  }

  .cmd-desc {
    font-size: 0.75rem;
    color: var(--text-secondary);
    flex-basis: 100%;
    margin-left: 0;
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
    border-color: var(--danger);
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
