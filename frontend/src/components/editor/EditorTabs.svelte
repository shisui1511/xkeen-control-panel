<script lang="ts">
  import { tick } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import { t } from '../../i18n';
  import { configLayerEnabled } from '../../stores';
  import { filesByPath } from '../../lib/configLayer';
  import OwnerBadge from './OwnerBadge.svelte';
  import type { FileOwner } from './fileOps';

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

  // Слой «Конфигурация»: значок владельца файла («Панель», «Пользователь»)
  function layerOwner(path: string): FileOwner | null {
    if (!$configLayerEnabled) return null;
    const f = $filesByPath.get(path);
    if (!f) return null;
    return f.state === 'released' ? 'user' : 'panel';
  }

  // Активная вкладка всегда видна в полосе целиком (без scrollIntoView: он двигает и страницу)
  const keepActiveTabVisible: Attachment<HTMLElement> = (strip) => {
    const active = activeTabPath;
    const count = tabs.length;
    if (!active || count === 0) return;
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
  };

  function handleWheel(e: WheelEvent) {
    if (e.deltaY !== 0) {
      const el = e.currentTarget as HTMLElement;
      el.scrollLeft += e.deltaY;
      e.preventDefault();
    }
  }
</script>

{#if tabs.length > 0}
  <div class="editor-tab-strip" role="tablist" onwheel={handleWheel} {@attach keepActiveTabVisible}>
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
          {#if layerOwner(tab.path)}
            <OwnerBadge owner={layerOwner(tab.path)!} />
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
    padding: 0 var(--spacing-2) 0 0;
    background: transparent;
    color: var(--fg-dim);
    border-right: 1px solid var(--border);
    transition:
      color var(--transition-fast),
      background var(--transition-fast);
    position: relative;
    flex-shrink: 0;
  }

  .tab-main {
    display: flex;
    align-items: center;
    height: 100%;
    gap: var(--spacing-2);
    padding: 0 var(--spacing-2) 0 var(--spacing-3);
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    font-size: var(--font-size-xs);
    font-weight: 500;
    cursor: pointer;
  }

  .tab-name {
    display: inline-block;
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
    font-size: var(--font-size-xs);
    margin-left: var(--spacing-0-5);
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
    margin-left: var(--spacing-1);
    transition:
      color var(--transition-fast),
      background var(--transition-fast);
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
      margin-left: var(--spacing-2);
    }

    .editor-tab {
      padding-right: 15px;
    }
  }
</style>
