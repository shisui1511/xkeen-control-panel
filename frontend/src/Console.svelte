<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from './components/Modal.svelte';
  import Terminal from './components/Terminal.svelte';
  import { t } from './i18n';
  import PageHeader from './PageHeader.svelte';
  import { showToast } from './stores';
  import { apiFetch } from './lib/api';

  interface Props {
    onSwitchTab?: (tab: string) => void;
  }

  let { onSwitchTab = () => {} }: Props = $props();

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

  let activeTab = $state<'terminal' | 'commands'>('terminal');
  let categories = $state<CommandCategory[]>([]);
  let loading = $state(true);
  let error = $state('');
  let executing = $state('');
  let output = $state('');
  let history = $state<{ command: string; output: string; success: boolean }[]>([]);

  // Confirmation modal state
  let confirmPending = $state<CommandDef | null>(null);

  async function fetchCommands() {
    loading = true;
    try {
      const res = await apiFetch('/api/console/commands');
      if (!res.ok) throw new Error($t('console.load_error'));
      categories = await res.json();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
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
      const res = await apiFetch('/api/console/execute', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
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
      if (e?.status === 401) return;
      showToast('error', e instanceof Error ? e.message : String(e));
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

  onMount(fetchCommands);
</script>

<div class="container">
  <PageHeader
    title={$t('console.title')}
    subtitle={$t('console.subtitle')}
    breadcrumbs={[{ label: $t('nav.group_services') }, { label: $t('nav.console') }]}
    {onSwitchTab}
    hideHome={true}
  />

  <!-- Tab Navigation -->
  <div class="settings-tabs">
    <button
      class="stab"
      class:active={activeTab === 'terminal'}
      onclick={() => (activeTab = 'terminal')}
    >
      {$t('console.tab_terminal')}
    </button>
    <button
      class="stab"
      class:active={activeTab === 'commands'}
      onclick={() => (activeTab = 'commands')}
    >
      {$t('console.tab_commands')}
    </button>
  </div>

  {#if activeTab === 'terminal'}
    <Terminal />
  {:else}
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
                {$t(`console.cat_${category.name}`) || category.name}
              </div>
              <div class="cmd-tile-grid">
                {#each category.commands as cmd}
                  <button
                    class="cmd-tile"
                    onclick={() => handleCommandClick(cmd)}
                    disabled={executing !== ''}
                    title={cmd.description}
                  >
                    <div class="tile-name" class:dangerous-text={cmd.dangerous}>
                      {#if cmd.command === '-start'}
                        <svg
                          width="13"
                          height="13"
                          viewBox="0 0 24 24"
                          fill="currentColor"
                          style="margin-right:8px;flex-shrink:0;"
                          ><polygon points="5 3 19 12 5 21 5 3" /></svg
                        >
                      {:else if cmd.command === '-stop'}
                        <svg
                          width="13"
                          height="13"
                          viewBox="0 0 24 24"
                          fill="currentColor"
                          style="margin-right:8px;flex-shrink:0;"
                          ><rect x="6" y="5" width="4" height="14" /><rect
                            x="14"
                            y="5"
                            width="4"
                            height="14"
                          /></svg
                        >
                      {:else if cmd.command === '-restart'}
                        <svg
                          width="13"
                          height="13"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          style="margin-right:8px;flex-shrink:0;"
                          ><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
                        >
                      {:else if cmd.command === '-status'}
                        <svg
                          width="13"
                          height="13"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          style="margin-right:8px;flex-shrink:0;"><path d="M3 12h18" /></svg
                        >
                      {/if}
                      xkeen {cmd.command}
                    </div>
                    <div class="tile-desc">{cmd.description}</div>
                  </button>
                {/each}
              </div>
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
              onclick={clearOutput}
              disabled={!output && !executing}
            >
              {$t('console.clear')}
            </button>
            <button class="btn btn-secondary btn-sm" onclick={copyOutput} disabled={!output}>
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
                onclick={() => {
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
  {/if}
</div>

<Modal isOpen={!!confirmPending} title={$t('console.confirm_title')} onclose={cancelConfirm}>
  {#if confirmPending}
    <p style="margin: 0; line-height: 1.5; color: var(--fg-secondary);">
      {$t('console.confirm_desc', { name: 'xkeen ' + confirmPending.command })}
    </p>
    <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
      <button class="btn btn-secondary" onclick={cancelConfirm} title={$t('app.cancel')}>
        {$t('app.cancel')}
      </button>
      <button class="btn btn-danger" onclick={confirmExecute} title={$t('app.confirm')}>
        {$t('app.confirm')}
      </button>
    </div>
  {/if}
</Modal>

<style>
  .settings-tabs {
    display: flex;
    gap: 2px;
    margin-bottom: 20px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 0;
  }

  .stab {
    padding: 8px 16px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-secondary);
    cursor: pointer;
    border-radius: 4px 4px 0 0;
    transition:
      color 0.15s,
      border-color 0.15s;
  }

  .stab:hover {
    color: var(--fg-primary);
  }

  .stab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  .console-grid {
    display: grid;
    grid-template-columns: 2fr 3fr;
    gap: 14px;
    align-items: start;
  }

  .cmd-list {
    margin-bottom: 18px;
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
    margin-bottom: 8px;
  }

  .cmd-tile-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--spacing-2, 8px);
  }

  .cmd-tile {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1, 4px);
    padding: 8px 12px;
    border-radius: var(--radius-md);
    background: var(--bg-card);
    border: 1px solid var(--border);
    cursor: pointer;
    transition:
      background var(--transition-fast),
      border-color var(--transition-fast);
    text-align: left;
    font-family: inherit;
    min-height: 44px;
    width: 100%;
  }

  .cmd-tile:hover:not(:disabled) {
    background: var(--hover);
    border-color: var(--accent-line);
  }

  .cmd-tile:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .cmd-tile .tile-name {
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-primary);
    display: flex;
    align-items: center;
    gap: var(--spacing-1, 4px);
  }

  .cmd-tile .tile-name.dangerous-text {
    color: var(--danger);
  }

  .cmd-tile .tile-desc {
    font-size: var(--font-size-xs, 11px);
    color: var(--fg-dim);
    line-height: 1.3;
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

  @media (max-width: 768px) {
    .console-grid {
      grid-template-columns: 1fr;
    }
    .cmd-tile-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
