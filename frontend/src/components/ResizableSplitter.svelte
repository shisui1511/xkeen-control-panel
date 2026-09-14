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
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
      activeCleanup = null;
    }

    activeCleanup = cleanup;
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
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
  onmousedown={startResize}
  onkeydown={handleKeyDown}
>
  <span class="splitter-line" aria-hidden="true"></span>
</button>

<style>
  .resizable-splitter {
    width: 8px;
    background: transparent;
    border: none;
    cursor: col-resize;
    position: relative;
    padding: 0;
    margin: 0 4px;
    flex-shrink: 0;
    outline: none;
    user-select: none;
    touch-action: none;
    transition: background-color var(--transition-fast);
  }

  .resizable-splitter:focus-visible .splitter-line {
    background: var(--accent);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 30%, transparent);
  }

  .splitter-line {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 3px;
    width: 2px;
    background: var(--border);
    border-radius: 1px;
    transition:
      background-color var(--transition-fast),
      width var(--transition-fast);
  }

  .resizable-splitter:hover .splitter-line,
  .resizable-splitter.is-resizing .splitter-line {
    background: var(--accent);
    width: 3px;
    left: 2.5px;
  }

  @media (max-width: 1024px) {
    .resizable-splitter {
      display: none;
    }
  }
</style>
