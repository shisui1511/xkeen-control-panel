<script lang="ts">
  import { t } from './i18n';
  import { capabilities, showToast } from './stores';
  import Skeleton from './components/Skeleton.svelte';

  let {
    onSwitchTab = () => {},
    selectedFile = '',
    onInsertIntoEditor = () => {},
    embedded = false,
    invalidateCache = false
  }: {
    onSwitchTab?: (tab: string) => void;
    selectedFile?: string;
    onInsertIntoEditor?: (content: string) => void;
    embedded?: boolean;
    invalidateCache?: boolean;
  } = $props();

  let kernel = $state<'xray' | 'mihomo'>('xray');
  let kernelInitialized = $state(false);

  $effect(() => {
    if (!kernelInitialized && $capabilities?.active_kernel) {
      kernel = $capabilities.active_kernel as 'xray' | 'mihomo';
      kernelInitialized = true;
    }
  });

  // Nested lazy boundary for the kernel-specific constructor (Pitfall 1,
  // RESEARCH.md): XrayRoutingConstructor.svelte (3 473 lines) has no other
  // import() call site in the app, so Rollup folds it into whichever chunk
  // statically imports it unless this component gives it its own dynamic
  // import() — that's what produces a distinct XrayRoutingConstructor-*.js
  // chunk (MihomoGenerator.svelte already gets a separate shared chunk via
  // Dashboard.svelte's own 'mihomo-gen' tab; this second import() call site
  // just resolves to the same de-duped chunk). Same reload-based retry
  // reasoning as Dashboard.svelte/Editor.svelte.
  let kernelChunkReloadKey = $state(0);
  let kernelChunkErrorShown = false;

  function retryKernelChunkLoad() {
    kernelChunkErrorShown = false;
    kernelChunkReloadKey++;
    window.location.reload();
  }

  function reportKernelChunkError(err: unknown): void {
    console.error('Failed to load kernel constructor lazy chunk', kernel, err);
    if (kernelChunkErrorShown) return;
    kernelChunkErrorShown = true;
    showToast('error', $t('app.chunk_load_failed'), 0, {
      label: $t('app.retry'),
      onClick: retryKernelChunkLoad
    });
  }
</script>

<div class="constructor-wrapper">
  <div class="constructor-kernel-toggle">
    <button
      class="tab-btn"
      class:active={kernel === 'xray'}
      aria-pressed={kernel === 'xray'}
      onclick={() => {
        kernel = 'xray';
        kernelInitialized = true;
        kernelChunkErrorShown = false;
      }}
    >
      {$t('editor.kernel_xray')}
    </button>
    <button
      class="tab-btn"
      class:active={kernel === 'mihomo'}
      aria-pressed={kernel === 'mihomo'}
      onclick={() => {
        kernel = 'mihomo';
        kernelInitialized = true;
        kernelChunkErrorShown = false;
      }}
    >
      {$t('editor.kernel_mihomo')}
    </button>
  </div>

  <div class="constructor-body">
    {#key kernelChunkReloadKey}
      {#if kernel === 'xray'}
        {#await import('./XrayRoutingConstructor.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: XrayRoutingConstructor }}
          <XrayRoutingConstructor {onSwitchTab} {selectedFile} {onInsertIntoEditor} {embedded} />
        {:catch err}
          {@const _ = queueMicrotask(() => reportKernelChunkError(err))}
        {/await}
      {:else}
        {#await import('./MihomoGenerator.svelte')}
          <Skeleton type="card" height="60vh" />
        {:then { default: MihomoGenerator }}
          <MihomoGenerator
            {onSwitchTab}
            {selectedFile}
            {onInsertIntoEditor}
            {embedded}
            {invalidateCache}
          />
        {:catch err}
          {@const _ = queueMicrotask(() => reportKernelChunkError(err))}
        {/await}
      {/if}
    {/key}
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
