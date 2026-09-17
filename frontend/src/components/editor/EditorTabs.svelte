<script lang="ts">
  import { t } from '../../i18n';

  export interface EditorTab {
    path: string;
    name: string;
    isDirty: boolean;
    isPreview: boolean;
  }

  let {
    tabs = [],
    activeTabPath = '',
    onSwitchTab,
    onPinTab,
    onCloseTab
  }: {
    tabs: EditorTab[];
    activeTabPath: string;
    onSwitchTab: (path: string) => void;
    onPinTab: (path: string) => void;
    onCloseTab: (path: string) => void;
  } = $props();

  function handleWheel(e: WheelEvent) {
    if (e.deltaY !== 0) {
      const el = e.currentTarget as HTMLElement;
      el.scrollLeft += e.deltaY;
      e.preventDefault();
    }
  }
</script>

{#if tabs.length > 0}
  <div class="editor-tab-strip" role="tablist" onwheel={handleWheel}>
    {#each tabs as tab (tab.path)}
      <div
        class="editor-tab"
        class:active={tab.path === activeTabPath}
        class:preview={tab.isPreview}
      >
        <button
          type="button"
          class="tab-main"
          role="tab"
          aria-selected={tab.path === activeTabPath}
          onclick={() => onSwitchTab(tab.path)}
          ondblclick={() => onPinTab(tab.path)}
        >
          <span class="tab-name">{tab.name}</span>
          {#if tab.isDirty}
            <span class="tab-dirty-dot">●</span>
          {/if}
        </button>
        <button
          type="button"
          class="tab-close-btn"
          onclick={() => onCloseTab(tab.path)}
          title={$t('app.close')}
          aria-label={$t('app.close')}
        >
          <svg
            width="8"
            height="8"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="3"
          >
            <line x1="18" y1="6" x2="6" y2="18"></line>
            <line x1="6" y1="6" x2="18" y2="18"></line>
          </svg>
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .editor-tab-strip {
    display: flex;
    gap: 0;
    height: 100%;
    min-width: 0;
    flex: 1;
    align-items: stretch;
    background: transparent;
    border-bottom: none;
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none;
  }

  .editor-tab-strip::-webkit-scrollbar {
    display: none;
  }

  .editor-tab {
    display: flex;
    align-items: center;
    height: 100%;
    padding: 0 8px 0 0;
    background: transparent;
    color: var(--fg-dim);
    border-right: 1px solid var(--border);
    transition: all 0.15s ease;
    position: relative;
  }

  .tab-main {
    display: flex;
    align-items: center;
    height: 100%;
    gap: 8px;
    padding: 0 8px 0 12px;
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
  }

  .editor-tab:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .editor-tab.active {
    background: var(--bg-card);
    color: var(--fg-primary);
    font-weight: 600;
  }

  .editor-tab.active::after {
    content: '';
    position: absolute;
    bottom: -1px;
    left: 0;
    right: 0;
    height: 2px;
    background: var(--accent);
    z-index: 1;
  }

  .editor-tab.preview .tab-name {
    font-style: italic;
    opacity: 0.8;
  }

  .tab-dirty-dot {
    color: var(--warning);
    font-size: 12px;
    margin-left: 2px;
    line-height: 1;
  }

  .tab-close-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: transparent;
    color: var(--fg-dim);
    border: 0;
    cursor: pointer;
    padding: 0;
    margin-left: 4px;
    transition: all 0.1s ease;
  }

  .tab-close-btn:hover {
    background: var(--hover);
    color: var(--danger);
  }
</style>
