<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from './i18n';
  import PageHeader from './PageHeader.svelte';

  export let onSwitchTab: (tab: string) => void = () => {};

  interface CommandDef {
    name: string;
    description: string;
    command: string;
    dangerous: boolean;
  }

  interface CommandCategory {
    name: string;
    commands: CommandDef[];
  }

  interface CommandResult {
    success: boolean;
    output: string;
    error: string;
  }

  let categories: CommandCategory[] = [];
  let loading = false;
  let error = '';
  let executing = '';
  let output = '';
  let history: { command: string; output: string; success: boolean }[] = [];

  // Confirmation modal state
  let confirmPending: CommandDef | null = null;

  async function fetchCommands() {
    loading = true;
    try {
      const res = await fetch('/api/console/commands');
      if (!res.ok) throw new Error($t('console.load_error'));
      categories = await res.json();
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function executeCommand(command: string) {
    executing = command;
    output = '';
    error = '';
    confirmPending = null;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/console/execute', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ command })
      });
      const result: CommandResult = await res.json();
      output = result.output || result.error;
      history = [{ command, output, success: result.success }, ...history];
      if (history.length > 20) history = history.slice(0, 20);
      if (!result.success) {
        error = result.error || $t('app.error');
      }
    } catch (e: any) {
      error = e.message;
      output = e.message;
    } finally {
      executing = '';
    }
  }

  function handleCommandClick(cmd: CommandDef) {
    if (cmd.dangerous) {
      confirmPending = cmd;
    } else {
      executeCommand(cmd.command);
    }
  }

  function cancelConfirm() {
    confirmPending = null;
  }

  function confirmExecute() {
    if (confirmPending) {
      executeCommand(confirmPending.command);
    }
  }

  function clearOutput() {
    output = '';
  }

  function copyOutput() {
    if (output) {
      navigator.clipboard.writeText(output);
    }
  }

  function getCommandSvg(command: string) {
    if (command === '-start') {
      return `<svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor" style="margin-right:8px;flex-shrink:0;"><polygon points="5 3 19 12 5 21 5 3"/></svg>`;
    }
    if (command === '-stop') {
      return `<svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor" style="margin-right:8px;flex-shrink:0;"><rect x="6" y="5" width="4" height="14"/><rect x="14" y="5" width="4" height="14"/></svg>`;
    }
    if (command === '-restart') {
      return `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-right:8px;flex-shrink:0;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5"/></svg>`;
    }
    if (command === '-status') {
      return `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-right:8px;flex-shrink:0;"><path d="M3 12h18"/></svg>`;
    }
    return '';
  }

  onMount(fetchCommands);
</script>

<div class="container">
  <PageHeader
    title={$t('console.title')}
    subtitle={$t('console.subtitle')}
    breadcrumbs={[{ label: $t('nav.group_services') }, { label: $t('nav.console') }]}
    {onSwitchTab}
  />

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <div class="console-grid">
    <div>
      {#if loading}
        <div class="loading">{$t('app.loading')}</div>
      {:else}
        {#each categories as category}
          <div class="cmd-list mb-3">
            <div class="cmd-cat-head">
              {$t('console.cat_' + category.name) || category.name}
            </div>
            {#each category.commands as cmd}
              <button
                class="cmd-row"
                on:click={() => handleCommandClick(cmd)}
                disabled={executing !== ''}
                title={cmd.description}
              >
                <div class="cmd-name" class:dangerous-text={cmd.dangerous}>
                  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                  {@html getCommandSvg(cmd.command)}
                  xkeen {cmd.command}
                </div>
                <div class="cmd-desc">{cmd.description}</div>
              </button>
            {/each}
          </div>
        {/each}
      {/if}
    </div>

    <div>
      <div class="toolbar mb-2">
        <div class="toolbar-left">
          <span style="font-family:var(--font-family-mono);font-size:13px;color:var(--accent);"
            >root@xkeen ~ #</span
          >
        </div>
        <div class="toolbar-right">
          <button
            class="btn btn-secondary btn-sm"
            on:click={clearOutput}
            disabled={!output && !executing}
          >
            {$t('console.clear')}
          </button>
          <button class="btn btn-secondary btn-sm" on:click={copyOutput} disabled={!output}>
            {$t('console.copy')}
          </button>
        </div>
      </div>

      <div class="term-output">
        {#if executing}
          <span class="prompt">root@xkeen:~# xkeen {executing}</span><br />
          <span style="color:var(--fg-dim);">Running...</span>
        {:else if output}
          <span class="prompt">root@xkeen:~# xkeen {history[0]?.command || ''}</span><br />
          {output}
        {:else}
          <span class="prompt">root@xkeen:~# _</span>
        {/if}
      </div>

      {#if history.length > 0}
        <h4
          class="mt-3"
          style="font-size: 11px; font-weight: 700; color: var(--fg-dim); text-transform: uppercase; letter-spacing: 0.18em; padding: 0 4px;"
        >
          {$t('console.history')}
        </h4>
        <div class="history-list">
          {#each history as entry}
            <button
              class="history-item"
              class:error={!entry.success}
              on:click={() => {
                output = entry.output;
              }}
              title={entry.command}
            >
              <span class="history-cmd">xkeen {entry.command}</span>
              <span class="history-status" class:error-text={!entry.success}>
                {#if entry.success}
                  SUCCESS
                {:else}
                  ERROR
                {/if}
              </span>
            </button>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

{#if confirmPending}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    on:click={cancelConfirm}
    on:keydown={(e) => e.key === 'Escape' && cancelConfirm()}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{$t('console.confirm_title')}</h2>
        <button class="modal-close-btn" on:click={cancelConfirm}>&times;</button>
      </div>
      <div class="modal-card-body">
        <p style="margin: 0; line-height: 1.5; color: var(--fg-secondary);">
          {$t('console.confirm_desc', { name: 'xkeen ' + confirmPending.command })}
        </p>
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={cancelConfirm} title={$t('app.cancel')}>
          {$t('app.cancel')}
        </button>
        <button class="btn btn-danger" on:click={confirmExecute} title={$t('app.confirm')}>
          {$t('app.confirm')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .console-grid {
    display: grid;
    grid-template-columns: 300px 1fr;
    gap: 14px;
    align-items: start;
  }

  .cmd-list {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .cmd-cat-head {
    padding: 11px 14px;
    background: rgba(0, 0, 0, 0.18);
    font-size: 10.5px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--fg-dim);
    font-weight: 700;
    border-bottom: 1px solid var(--border);
  }

  .cmd-row {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    font-family: inherit;
    padding: 10px 14px;
    border-bottom: 1px solid var(--border-light);
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .cmd-row:hover:not(:disabled) {
    background: var(--hover);
  }

  .cmd-row:disabled {
    cursor: not-allowed;
    opacity: 0.6;
  }

  .cmd-row:last-child {
    border-bottom: 0;
  }

  .cmd-row .cmd-name {
    color: var(--fg-primary);
    font-weight: 600;
    font-size: 13px;
    display: flex;
    align-items: center;
  }

  .cmd-row .cmd-name.dangerous-text {
    color: var(--danger);
  }

  .cmd-row .cmd-desc {
    color: var(--fg-dim);
    font-size: 11.5px;
    margin-top: 2px;
  }

  .term-output {
    background: #050d16;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 14px 18px;
    font-family: var(--font-family-mono);
    font-size: 12.5px;
    line-height: 1.6;
    color: var(--fg-primary);
    min-height: 440px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
  }

  .term-output .prompt {
    color: var(--accent);
  }

  .history-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 200px;
    overflow-y: auto;
    margin-top: 8px;
  }

  .history-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: 12px;
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
    width: 100%;
    text-align: left;
    transition: background var(--transition-fast);
  }

  .history-item:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .history-item.error {
    border-color: rgba(239, 91, 107, 0.4);
    background: rgba(239, 91, 107, 0.04);
  }

  .history-status {
    font-weight: 700;
    color: var(--success);
    font-size: 10px;
  }

  .history-status.error-text {
    color: var(--danger);
  }

  /* Modal Styles */
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 20px;
  }

  .modal-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: 100%;
    max-width: 520px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: 90vh;
    animation: modal-anim 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes modal-anim {
    from {
      transform: scale(0.95) translateY(10px);
      opacity: 0;
    }
    to {
      transform: scale(1) translateY(0);
      opacity: 1;
    }
  }

  .modal-card-header {
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-card-header h2 {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .modal-close-btn {
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 24px;
    cursor: pointer;
    line-height: 1;
    padding: 4px;
  }

  .modal-close-btn:hover {
    color: var(--fg-primary);
  }

  .modal-card-body {
    padding: 24px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .modal-card-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  @media (max-width: 768px) {
    .console-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
