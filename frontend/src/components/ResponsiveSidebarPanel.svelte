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
    /** Заголовок шторки в режиме оверлея (рядом с кнопкой «Закрыть») */
    title?: string;
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
    title,
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
        if (Number.isFinite(stored) && stored > 0) {
          return Math.max(minWidth, Math.min(maxWidth, stored));
        }
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

  function portalToBody(node: HTMLElement) {
    document.body.appendChild(node);
    return {
      destroy() {
        node.remove();
      }
    };
  }

  function handleClose() {
    show = false;
    onClose?.();
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && isOverlay && show) {
      if (
        typeof document !== 'undefined' &&
        document.querySelector('.modal-backdrop, .confirm-modal-backdrop, .confirm-dialog-backdrop')
      ) {
        return;
      }
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

  function onTouchEnd(e: TouchEvent) {
    if (!isSwiping || !isOverlay) return;
    isSwiping = false;
    if (e.changedTouches && e.changedTouches.length > 0) {
      touchCurrentX = e.changedTouches[0].clientX;
      touchCurrentY = e.changedTouches[0].clientY;
    }
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
      window.addEventListener('keydown', handleKeydown);
    } else {
      restoreBodyScroll();
      window.removeEventListener('keydown', handleKeydown);
      if (previouslyFocusedElement && typeof previouslyFocusedElement.focus === 'function') {
        previouslyFocusedElement.focus();
        previouslyFocusedElement = null;
      }
    }

    return () => {
      restoreBodyScroll();
      if (typeof window !== 'undefined') {
        window.removeEventListener('keydown', handleKeydown);
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
      window.removeEventListener('keydown', handleKeydown);
    }
  });
</script>

{#snippet panel()}
  <div
    class="responsive-sidebar-panel file-tree-pane {containerClass}"
    class:overlay={isOverlay}
    class:side-left={side === 'left'}
    class:side-right={side === 'right'}
    role={isOverlay ? 'dialog' : undefined}
    aria-modal={isOverlay ? 'true' : undefined}
    aria-label={isOverlay ? (title ?? $t('editor.sidebar_panel')) : undefined}
    ontouchstart={onTouchStart}
    ontouchmove={onTouchMove}
    ontouchend={onTouchEnd}
    ontouchcancel={() => {
      isSwiping = false;
    }}
    style={isOverlay ? undefined : `width: ${panelWidth}px;`}
  >
    {#if isOverlay}
      <div class="responsive-sidebar-overlay-header">
        {#if title}
          <span class="responsive-sidebar-title">{title}</span>
        {/if}
        <button
          type="button"
          class="responsive-sidebar-close"
          aria-label={$t('app.close')}
          title={$t('app.close')}
          onclick={handleClose}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <line x1="18" y1="6" x2="6" y2="18"></line>
            <line x1="6" y1="6" x2="18" y2="18"></line>
          </svg>
        </button>
      </div>
    {/if}
    {#if header}
      <div class="responsive-sidebar-header">
        {@render header()}
      </div>
    {/if}
    {@render children()}
  </div>
{/snippet}

{#if show && isOverlay}
  <!-- Оверлей выносится в body: предок с transform/filter (анимация страницы) ломает position: fixed и слои -->
  <div class="responsive-sidebar-portal" use:portalToBody>
    <button
      type="button"
      class="responsive-sidebar-backdrop file-tree-backdrop"
      aria-label={$t('app.close')}
      tabindex="-1"
      onclick={handleClose}
    ></button>
    {@render panel()}
  </div>
{:else if show}
  {#if side === 'right'}
    <button
      type="button"
      class="editor-splitter splitter-right"
      class:active={isResizing}
      aria-label={$t('editor.resize_sidebar')}
      tabindex="-1"
      onpointerdown={startResize}
    ></button>
  {/if}

  {@render panel()}

  {#if side === 'left'}
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

  .responsive-sidebar-portal {
    display: contents;
  }

  .responsive-sidebar-panel.overlay {
    position: fixed;
    top: 0;
    bottom: 0;
    z-index: 250;
    width: min(85vw, 320px);
    max-width: none;
    height: auto;
    padding-top: env(safe-area-inset-top);
    padding-bottom: env(safe-area-inset-bottom);
    background: var(--bg-card);
    border: none;
    box-shadow: var(--shadow-md);
  }

  .responsive-sidebar-panel.overlay.side-left {
    left: 0;
    right: auto;
    border-right: 1px solid var(--border);
    border-radius: 0 var(--radius-md) var(--radius-md) 0;
  }

  .responsive-sidebar-panel.overlay.side-right {
    right: 0;
    left: auto;
    border-left: 1px solid var(--border);
    border-radius: var(--radius-md) 0 0 var(--radius-md);
  }

  .responsive-sidebar-overlay-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 4px 4px 12px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .responsive-sidebar-title {
    flex: 1;
    min-width: 0;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .responsive-sidebar-close {
    appearance: none;
    -webkit-appearance: none;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    margin-left: auto;
    padding: 0;
    flex-shrink: 0;
    border: 0;
    background: transparent;
    color: var(--fg-secondary);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }

  .responsive-sidebar-close:hover {
    background: var(--hover);
  }

  .responsive-sidebar-close:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .responsive-sidebar-backdrop {
    appearance: none;
    -webkit-appearance: none;
    position: fixed;
    inset: 0;
    z-index: 249;
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
