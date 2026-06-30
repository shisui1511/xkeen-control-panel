<script lang="ts">
  interface EditorTab {
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
</script>

{#if tabs.length > 0}
  <div class="editor-tab-strip">
    {#each tabs as tab (tab.path)}
      <button
        class="editor-tab"
        class:active={tab.path === activeTabPath}
        class:preview={tab.isPreview}
        onclick={() => onSwitchTab(tab.path)}
        ondblclick={() => onPinTab(tab.path)}
      >
        <span class="tab-name">{tab.name}</span>
        {#if tab.isDirty}
          <span class="tab-dirty-dot">●</span>
        {/if}
        <span
          class="tab-close-btn"
          role="button"
          tabindex="-1"
          onclick={(e) => { e.stopPropagation(); onCloseTab(tab.path); }}
          onkeydown={(e) => {
            e.stopPropagation();
            if (e.key === 'Enter' || e.key === ' ') onCloseTab(tab.path);
          }}
          title="Закрыть"
          aria-label="Закрыть"
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
        </span>
      </button>
    {/each}
  </div>
{/if}

<style>
  .editor-tab-strip {
    display: flex;
    gap: 2px;
    background: var(--bg-card);
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }

  .editor-tab-strip::-webkit-scrollbar {
    height: 3px;
  }

  .editor-tab-strip::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius);
  }

  .editor-tab {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    background: rgba(255, 255, 255, 0.01);
    color: var(--fg-dim);
    border: 0;
    border-right: 1px solid var(--border);
    cursor: pointer;
    font-size: 12px;
    font-weight: 500;
    transition: all 0.15s ease;
    position: relative;
  }

  .editor-tab:hover {
    background: rgba(255, 255, 255, 0.03);
    color: var(--fg-primary);
  }

  .editor-tab.active {
    background: var(--bg-page);
    color: var(--fg-primary);
    font-weight: 600;
  }

  .editor-tab.active::after {
    content: '';
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    height: 2px;
    background: var(--accent);
  }

  .editor-tab.preview .tab-name {
    font-style: italic;
    opacity: 0.8;
  }

  .tab-dirty-dot {
    color: var(--warning);
    font-size: 10px;
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
    background: rgba(255, 255, 255, 0.1);
    color: var(--fg-primary);
  }
</style>
