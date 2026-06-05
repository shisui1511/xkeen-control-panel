<script lang="ts">
  import { t } from './i18n';
  import XrayRoutingConstructor from './XrayRoutingConstructor.svelte';
  import MihomoGenerator from './MihomoGenerator.svelte';

  export let onSwitchTab: (tab: string) => void = () => {};
  export let selectedFile: string = '';
  export let onInsertIntoEditor: (content: string) => void = () => {};
  export let embedded: boolean = false;

  let kernel: 'xray' | 'mihomo' = 'xray';
</script>

<div class="constructor-wrapper">
  <div class="constructor-kernel-toggle">
    <button
      class="tab-btn"
      class:active={kernel === 'xray'}
      aria-pressed={kernel === 'xray'}
      on:click={() => {
        kernel = 'xray';
      }}
    >
      {$t('editor.kernel_xray')}
    </button>
    <button
      class="tab-btn"
      class:active={kernel === 'mihomo'}
      aria-pressed={kernel === 'mihomo'}
      on:click={() => {
        kernel = 'mihomo';
      }}
    >
      {$t('editor.kernel_mihomo')}
    </button>
  </div>

  <div class="constructor-body">
    {#if kernel === 'xray'}
      <XrayRoutingConstructor {onSwitchTab} {selectedFile} {onInsertIntoEditor} {embedded} />
    {:else}
      <MihomoGenerator {onSwitchTab} {selectedFile} {onInsertIntoEditor} {embedded} />
    {/if}
  </div>
</div>

<style>
  .constructor-wrapper {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4, 16px);
  }

  .constructor-kernel-toggle {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--border);
    padding-bottom: 0;
  }

  .tab-btn {
    padding: 8px 16px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--fg-secondary);
    font-size: var(--font-size-sm, 0.8125rem);
    font-family: inherit;
    cursor: pointer;
    transition:
      color var(--transition-fast),
      border-color var(--transition-fast);
    margin-bottom: -1px;
    min-height: 36px;
  }

  .tab-btn:hover {
    color: var(--fg);
  }

  .tab-btn.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
    font-weight: 500;
  }

  .constructor-body {
    padding-top: var(--spacing-2, 8px);
  }
</style>
