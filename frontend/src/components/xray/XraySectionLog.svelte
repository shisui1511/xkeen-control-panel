<script lang="ts">
  import { t } from '../../i18n';
  import Select from '../Select.svelte';

  interface LogConfig {
    loglevel: string;
    dnsLog?: boolean;
  }

  let {
    logConfig = $bindable(),
    accessPath = '',
    errorPath = '',
    onchange
  }: {
    logConfig: LogConfig;
    accessPath?: string;
    errorPath?: string;
    onchange?: () => void;
  } = $props();

  function markChanged() {
    onchange?.();
  }
</script>

<div class="sec-body">
  <div class="section-title">{$t('editor.xray_section_log')}</div>

  <div class="form-row">
    <label class="form-label" for="log-level">{$t('xray.loglevel')}</label>
    <Select
      id="log-level"
      class="form-select"
      bind:value={logConfig.loglevel}
      onchange={markChanged}
    >
      <option value="none">none</option>
      <option value="error">error</option>
      <option value="warning">warning</option>
      <option value="info">info</option>
      <option value="debug">debug</option>
    </Select>
  </div>

  <div class="form-row" style="margin-top: 8px;">
    <label class="checkbox-container">
      <input type="checkbox" bind:checked={logConfig.dnsLog} onchange={markChanged} />
      <span class="checkmark"></span>
      {$t('xray.enable_dns_logging')}
    </label>
  </div>

  {#if accessPath || errorPath}
    <div class="logs-paths card" style="margin-top: 12px; padding: 12px;">
      <h4 style="margin: 0 0 8px 0; font-size: var(--font-size-base);">
        {$t('xray.logs_paths')}
      </h4>
      {#if accessPath}
        <div style="font-size: var(--font-size-sm);">
          <strong>Access:</strong> <code>{accessPath}</code>
        </div>
      {/if}
      {#if errorPath}
        <div style="font-size: var(--font-size-sm); margin-top: 4px;">
          <strong>Error:</strong> <code>{errorPath}</code>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .sec-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .section-title {
    font-size: var(--font-size-lg);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .form-label {
    font-size: var(--font-size-xs);
    font-weight: 500;
    color: var(--fg-secondary);
  }

  :global(.form-select) {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--spacing-1-5) var(--spacing-2-5);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
  }

  .checkbox-container {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    cursor: pointer;
    position: relative;
    user-select: none;
  }

  .checkbox-container input {
    position: absolute;
    opacity: 0;
    cursor: pointer;
    height: 0;
    width: 0;
  }

  .checkmark {
    height: 16px;
    width: 16px;
    background-color: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-xs);
    display: inline-block;
    position: relative;
  }

  .checkbox-container input:checked ~ .checkmark {
    background-color: var(--accent);
    border-color: var(--accent);
  }

  .checkbox-container input:checked ~ .checkmark:after {
    content: '';
    position: absolute;
    left: 5px;
    top: 2px;
    width: 4px;
    height: 8px;
    border: solid var(--btn-primary-text);
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }

  .logs-paths code {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--code-fg, var(--fg-primary));
    background: var(--code-bg, var(--bg-surface-active));
    padding: var(--spacing-0-5) var(--spacing-1);
    border-radius: var(--radius-xs);
  }
</style>
