<script lang="ts">
  import { onDestroy } from 'svelte';
  import { t } from '../i18n';

  export interface ResponsiveSidebarProps {
    show?: boolean;
    side?: 'left' | 'right';
    storageKey?: string;
    defaultWidth?: number;
    minWidth?: number;
    maxWidth?: number;
    overlay?: boolean;
    onClose?: () => void;
    containerClass?: string;
    children: import('svelte').Snippet;
    header?: import('svelte').Snippet;
  }

  let {
    show = $bindable(true),
    side = 'left',
    storageKey = 'editor_filetree_width',
    defaultWidth = 240,
    minWidth = 160,
    maxWidth = 450,
    overlay,
    onClose,
    containerClass = '',
    children,
    header
  }: ResponsiveSidebarProps = $props();

  let isMediaNarrow = $state(
    typeof window !== 'undefined' ? window.matchMedia('(max-width: 768px)').matches : false
  );

  let isOverlay = $derived(overlay !== undefined ? overlay : isMediaNarrow);

  let customWidth = $state<number | null>(null);
  let initialWidth = $derived.by(() => {
    try {
      if (typeof localStorage !== 'undefined') {
        const stored = Number(localStorage.getItem(storageKey));
        if (stored) return stored;
      }
    } catch {
      // localStorage may be restricted
    }
    return defaultWidth;
  });
  let panelWidth = $derived(customWidth ?? initialWidth);

  let isResizing = $state(false);
  let activeResizeCleanup: (() => void) | null = null;

  // Touch gesture tracking for swipe closing
  let touchStartX = 0;
  let touchStartY = 0;
  let touchCurrentX = 0;
  let touchCurrentY = 0;
  let isSwiping = false;

  let previousBodyOverflow: string | null = null;
  let bodyLocked = false;
  let previouslyFocusedElement: HTMLElement | null = null;

  function lockBodyScroll() {
    if (typeof document === 'undefined' || bodyLocked) return;
    previousBodyOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    bodyLocked = true;
  }

  function restoreBodyScroll() {
    if (typeof document === 'undefined' || !bodyLocked) return;
    document.body.style.overflow = previousBodyOverflow ?? '';
    previousBodyOverflow = null;
    bodyLocked = false;
  }

  function handleClose() {
    show = false;
    onClose?.();
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && isOverlay && show) {
      e.stopPropagation();
      e.preventDefault();
      handleClose();
    }
  }

  function startResize(e: PointerEvent) {
    e.preventDefault();
    if (activeResizeCleanup) {
      activeResizeCleanup();
    }
    isResizing = true;
    const startX = e.clientX;
    const startWidth = panelWidth;

    function onMove(ev: PointerEvent) {
      const delta = ev.clientX - startX;
      const factor = side === 'right' ? -1 : 1;
      customWidth = Math.max(minWidth, Math.min(maxWidth, startWidth + delta * factor));
    }

    function onUp() {
      isResizing = false;
      try {
        localStorage.setItem(storageKey, String(panelWidth));
      } catch {
        // localStorage may be restricted
      }
      if (activeResizeCleanup) {
        activeResizeCleanup();
        activeResizeCleanup = null;
      }
    }

    activeResizeCleanup = () => {
      isResizing = false;
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onUp);
    };

    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onUp);
  }

  function onTouchStart(e: TouchEvent) {
    if (!isOverlay) return;
    if (e.touches.length !== 1) return;
    touchStartX = e.touches[0].clientX;
    touchStartY = e.touches[0].clientY;
    touchCurrentX = touchStartX;
    touchCurrentY = touchStartY;
    isSwiping = true;
  }

  function onTouchMove(e: TouchEvent) {
    if (!isSwiping || !isOverlay) return;
    if (e.touches.length !== 1) return;
    touchCurrentX = e.touches[0].clientX;
    touchCurrentY = e.touches[0].clientY;
  }

  function onTouchEnd() {
    if (!isSwiping || !isOverlay) return;
    isSwiping = false;
    const dx = touchCurrentX - touchStartX;
    const dy = touchCurrentY - touchStartY;
    const absDx = Math.abs(dx);
    const absDy = Math.abs(dy);

    if (absDx >= 60 && absDx > 1.5 * absDy) {
      if (side === 'left' && dx <= -60) {
        handleClose();
      } else if (side === 'right' && dx >= 60) {
        handleClose();
      }
    }
  }

  $effect(() => {
    if (typeof window === 'undefined') return;
    const mql = window.matchMedia('(max-width: 768px)');
    const onChange = (e: MediaQueryListEvent) => {
      isMediaNarrow = e.matches;
    };
    isMediaNarrow = mql.matches;
    mql.addEventListener('change', onChange);
    return () => {
      mql.removeEventListener('change', onChange);
    };
  });

  $effect(() => {
    if (isOverlay && show) {
      lockBodyScroll();
      if (typeof document !== 'undefined' && document.activeElement instanceof HTMLElement) {
        previouslyFocusedElement = document.activeElement;
      }
      window.addEventListener('keydown', handleKeydown, true);
    } else {
      restoreBodyScroll();
      window.removeEventListener('keydown', handleKeydown, true);
      if (previouslyFocusedElement && typeof previouslyFocusedElement.focus === 'function') {
        previouslyFocusedElement.focus();
        previouslyFocusedElement = null;
      }
    }

    return () => {
      restoreBodyScroll();
      if (typeof window !== 'undefined') {
        window.removeEventListener('keydown', handleKeydown, true);
      }
    };
  });

  onDestroy(() => {
    if (activeResizeCleanup) {
      activeResizeCleanup();
      activeResizeCleanup = null;
    }
    restoreBodyScroll();
    if (typeof window !== 'undefined') {
      window.removeEventListener('keydown', handleKeydown, true);
    }
  });
</script>

{#if show && isOverlay}
  <button
    type="button"
    class="responsive-sidebar-backdrop file-tree-backdrop"
    aria-label={$t('app.close')}
    tabindex="-1"
    onclick={handleClose}
  ></button>
{/if}

{#if show}
  {#if !isOverlay && side === 'right'}
    <button
      type="button"
      class="editor-splitter splitter-right"
      class:active={isResizing}
      aria-label={$t('editor.resize_sidebar')}
      tabindex="-1"
      onpointerdown={startResize}
    ></button>
  {/if}

  <div
    class="responsive-sidebar-panel file-tree-pane {containerClass}"
    class:overlay={isOverlay}
    class:side-left={side === 'left'}
    class:side-right={side === 'right'}
    role={isOverlay ? 'dialog' : undefined}
    aria-modal={isOverlay ? 'true' : undefined}
    aria-label={isOverlay ? $t('editor.sidebar_panel') : undefined}
    ontouchstart={onTouchStart}
    ontouchmove={onTouchMove}
    ontouchend={onTouchEnd}
    ontouchcancel={() => {
      isSwiping = false;
    }}
    style={isOverlay ? undefined : `width: ${panelWidth}px;`}
  >
    {#if header}
      <div class="responsive-sidebar-header">
        {@render header()}
      </div>
    {/if}
    {@render children()}
  </div>

  {#if !isOverlay && side === 'left'}
    <button
      type="button"
      class="editor-splitter splitter-left"
      class:active={isResizing}
      aria-label={$t('editor.resize_sidebar')}
      tabindex="-1"
      onpointerdown={startResize}
    ></button>
  {/if}
{/if}

<style>
  .responsive-sidebar-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    flex-shrink: 0;
    overflow: hidden;
    max-width: 42%;
    box-sizing: border-box;
  }

  .responsive-sidebar-panel.overlay {
    position: absolute;
    top: 0;
    bottom: 0;
    z-index: 30;
    width: min(85%, 320px);
    max-width: none;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    transition: transform 0.2s ease-out;
  }

  .responsive-sidebar-panel.overlay.side-left {
    left: 0;
    right: auto;
  }

  .responsive-sidebar-panel.overlay.side-right {
    right: 0;
    left: auto;
  }

  .responsive-sidebar-backdrop {
    appearance: none;
    -webkit-appearance: none;
    position: absolute;
    inset: 0;
    z-index: 29;
    border: 0;
    padding: 0;
    margin: 0;
    background: color-mix(in srgb, var(--bg-deep) 55%, transparent);
    cursor: default;
    transition: opacity 0.2s ease-out;
  }

  .editor-splitter {
    appearance: none;
    -webkit-appearance: none;
    border: 0;
    padding: 0;
    margin: 0 2px;
    background: transparent;
    box-sizing: border-box;
    width: 10px;
    flex-shrink: 0;
    position: relative;
    z-index: 10;
    cursor: col-resize;
    touch-action: none;
  }

  .editor-splitter::before {
    content: '';
    position: absolute;
    inset: 0 auto;
    left: 50%;
    transform: translateX(-50%);
    width: 1px;
    height: 100%;
    background: var(--border);
    border-radius: 1px;
    transition:
      width 0.15s ease,
      background 0.15s ease;
  }

  .editor-splitter:hover::before,
  .editor-splitter:active::before,
  .editor-splitter.active::before {
    width: 2px;
    background: var(--accent);
  }

  .editor-splitter:focus-visible::before {
    background: var(--accent);
    width: 2px;
  }

  @media (prefers-reduced-motion: reduce) {
    .responsive-sidebar-panel.overlay,
    .responsive-sidebar-backdrop {
      animation: none !important;
      transition: none !important;
    }
  }
</style>
