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
      <h4 style="margin: 0 0 8px 0; font-size: 0.875rem;">
        {$t('xray.logs_paths')}
      </h4>
      {#if accessPath}
        <div style="font-size: 0.8125rem;">
          <strong>Access:</strong> <code>{accessPath}</code>
        </div>
      {/if}
      {#if errorPath}
        <div style="font-size: 0.8125rem; margin-top: 4px;">
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
    gap: 12px;
  }

  .section-title {
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--fg-secondary);
  }

  :global(.form-select) {
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    font-size: 0.8125rem;
    color: var(--fg-primary);
  }

  .checkbox-container {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.8125rem;
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
    background-color: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-xs);
    display: inline-block;
    position: relative;
  }

  .checkbox-container input:checked ~ .checkmark {
    background-color: var(--color-primary);
    border-color: var(--color-primary);
  }

  .checkbox-container input:checked ~ .checkmark:after {
    content: '';
    position: absolute;
    left: 5px;
    top: 2px;
    width: 4px;
    height: 8px;
    border: solid var(--color-on-primary);
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }

  .logs-paths code {
    font-family: var(--font-family-mono);
    font-size: 0.75rem;
    color: var(--code-fg, var(--fg-primary));
    background: var(--code-bg, var(--bg-surface-active));
    padding: 2px 4px;
    border-radius: var(--radius-xs);
  }
</style>
