<script lang="ts">
  import { onDestroy } from 'svelte';

  interface Props {
    storageKey: string;
    defaultWidth?: number;
    minWidth?: number;
    maxWidth?: number;
    width?: number;
    ariaLabel?: string;
    onResize?: (width: number) => void;
  }

  let {
    storageKey,
    defaultWidth = 440,
    minWidth = 280,
    maxWidth = 800,
    width = $bindable(),
    ariaLabel = 'Resize preview panel',
    onResize
  }: Props = $props();

  let isResizing = $state(false);
  let activeCleanup: (() => void) | null = null;

  // Инициализация ширины из localStorage, если она не была явно передана
  if (width === undefined) {
    width = (() => {
      if (typeof localStorage === 'undefined') return defaultWidth;
      try {
        const raw = localStorage.getItem(storageKey);
        if (!raw) return defaultWidth;
        const num = Number(raw);
        return !isNaN(num) ? Math.max(minWidth, Math.min(maxWidth, num)) : defaultWidth;
      } catch {
        return defaultWidth;
      }
    })();
  }

  function startResize(e: MouseEvent | PointerEvent) {
    e.preventDefault();
    if (activeCleanup) {
      activeCleanup();
      activeCleanup = null;
    }

    isResizing = true;
    const startX = e.clientX;
    const startWidth = width ?? defaultWidth;

    function onMove(ev: MouseEvent | PointerEvent) {
      const delta = startX - ev.clientX;
      const newWidth = Math.max(minWidth, Math.min(maxWidth, startWidth + delta));
      width = newWidth;
      onResize?.(newWidth);
    }

    function onUp() {
      isResizing = false;
      try {
        if (typeof localStorage !== 'undefined' && width !== undefined) {
          localStorage.setItem(storageKey, String(width));
        }
      } catch {
        // ignore
      }
      cleanup();
    }

    function cleanup() {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      activeCleanup = null;
    }

    activeCleanup = cleanup;
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
  }

  function handleKeyDown(e: KeyboardEvent) {
    let delta = 0;
    if (e.key === 'ArrowLeft') {
      delta = 10;
    } else if (e.key === 'ArrowRight') {
      delta = -10;
    } else {
      return;
    }

    e.preventDefault();
    const current = width ?? defaultWidth;
    const newWidth = Math.max(minWidth, Math.min(maxWidth, current + delta));
    width = newWidth;
    onResize?.(newWidth);
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(storageKey, String(newWidth));
      }
    } catch {
      // ignore
    }
  }

  onDestroy(() => {
    if (activeCleanup) {
      activeCleanup();
      activeCleanup = null;
    }
  });
</script>

<button
  type="button"
  class="resizable-splitter"
  class:is-resizing={isResizing}
  aria-label={ariaLabel}
  onpointerdown={startResize}
  onkeydown={handleKeyDown}
>
  <div class="splitter-handle" aria-hidden="true"></div>
</button>

<style>
  .resizable-splitter {
    width: 12px;
    margin: 0 4px;
    cursor: col-resize;
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    user-select: none;
    z-index: 10;
    background: transparent;
    border: none;
    padding: 0;
    outline: none;
    touch-action: none;
  }

  .splitter-handle {
    width: 4px;
    height: 40px;
    border-radius: 2px;
    background: var(--border);
    transition:
      background var(--transition-fast),
      box-shadow var(--transition-fast);
  }

  .resizable-splitter:hover .splitter-handle,
  .resizable-splitter.is-resizing .splitter-handle,
  .resizable-splitter:focus-visible .splitter-handle {
    background: var(--accent);
    box-shadow: 0 0 8px color-mix(in srgb, var(--accent) 40%, transparent);
  }

  @media (max-width: 1024px) {
    .resizable-splitter {
      display: none;
    }
  }
</style>
