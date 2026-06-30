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
