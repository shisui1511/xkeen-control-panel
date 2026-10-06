<script lang="ts">
  import { tick } from 'svelte';
  import { t } from '../../i18n';
  import { configLayerEnabled } from '../../stores';
  import { filesByPath } from '../../lib/configLayer';

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

  let stripEl = $state<HTMLElement | null>(null);

  // Слой «Конфигурация»: «панель» для файла под управлением, «ручной» для отпущенного
  function layerBadge(path: string): 'managed' | 'manual' | null {
    if (!$configLayerEnabled) return null;
    const f = $filesByPath.get(path);
    if (!f) return null;
    return f.state === 'released' ? 'manual' : 'managed';
  }

  // Активная вкладка всегда видна в полосе целиком (без scrollIntoView: он двигает и страницу)
  $effect(() => {
    const active = activeTabPath;
    const count = tabs.length;
    if (!stripEl || !active || count === 0) return;
    const strip = stripEl;
    void tick().then(() => {
      const tabEl = Array.from(strip.querySelectorAll<HTMLElement>('.editor-tab')).find(
        (el) => el.dataset.path === active
      );
      if (!tabEl) return;
      const tabRect = tabEl.getBoundingClientRect();
      const stripRect = strip.getBoundingClientRect();
      if (tabRect.left < stripRect.left) {
        strip.scrollLeft -= stripRect.left - tabRect.left;
      } else if (tabRect.right > stripRect.right) {
        strip.scrollLeft += tabRect.right - stripRect.right;
      }
    });
  });

  function handleWheel(e: WheelEvent) {
    if (e.deltaY !== 0) {
      const el = e.currentTarget as HTMLElement;
      el.scrollLeft += e.deltaY;
      e.preventDefault();
    }
  }
</script>

{#if tabs.length > 0}
  <div class="editor-tab-strip" role="tablist" bind:this={stripEl} onwheel={handleWheel}>
    {#each tabs as tab (tab.path)}
      <div
        class="editor-tab"
        data-path={tab.path}
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
          <span class="tab-name" title={tab.name}>{tab.name}</span>
          {#if layerBadge(tab.path) === 'managed'}
            <span class="badge badge-info layer-badge" data-testid="layer-badge"
              >{$t('editor.managed_badge')}</span
            >
          {:else if layerBadge(tab.path) === 'manual'}
            <span class="badge layer-badge" data-testid="layer-badge"
              >{$t('editor.manual_badge')}</span
            >
          {/if}
          {#if tab.isDirty}
            <span class="tab-dirty-dot">●</span>
          {/if}
        </button>
        <button
          type="button"
          class="tab-close-btn tap-zone-44"
          onclick={(e) => {
            e.stopPropagation();
            onCloseTab(tab.path);
          }}
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
    -webkit-overflow-scrolling: touch;
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
    flex-shrink: 0;
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

  .tab-name {
    display: inline-block;
  }

  /* Имя усекается, бейдж не сжимается */
  .layer-badge {
    flex-shrink: 0;
    padding: 0 4px;
    line-height: 1.4;
    font-weight: 600;
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

  @media (max-width: 768px) {
    .editor-tab {
      min-width: 0;
      max-width: 100%;
    }

    .tab-main {
      min-width: 0;
      overflow: hidden;
    }

    .tab-name {
      display: block;
      min-width: 0;
      max-width: 120px;
      overflow: hidden;
      white-space: nowrap;
      text-overflow: ellipsis;
    }
  }

  /* Зона × (по 15 px в стороны от кнопки 14 px) не накрывает имя и соседнюю вкладку */
  @media (max-width: 768px), (pointer: coarse) {
    .tab-close-btn {
      margin-left: 8px;
    }

    .editor-tab {
      padding-right: 15px;
    }
  }
</style>
