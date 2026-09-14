<script lang="ts">
  import { onDestroy } from 'svelte';
  import FileTree from './FileTree.svelte';
  import { t } from '../../i18n';

  export interface ConfigFileInfo {
    name: string;
    path: string;
    size: number;
  }

  interface Props {
    show: boolean;
    xrayFiles: ConfigFileInfo[];
    mihomoFiles: ConfigFileInfo[];
    selectedFile: string;
    activeKernel?: string;
    onLoadFile: (path: string) => void;
    onCreateFile: () => void;
    onRenameFile: (file: ConfigFileInfo) => void;
    onDuplicateFile: (file: ConfigFileInfo) => void;
    onDownloadFile: (file: ConfigFileInfo) => void;
    onDeleteFile: (file: ConfigFileInfo) => void;
    onViewBackups: (file: ConfigFileInfo) => void;
  }

  let {
    show,
    xrayFiles,
    mihomoFiles,
    selectedFile,
    activeKernel = '',
    onLoadFile,
    onCreateFile,
    onRenameFile,
    onDuplicateFile,
    onDownloadFile,
    onDeleteFile,
    onViewBackups
  }: Props = $props();

  let fileTreeWidth = $state(
    typeof localStorage !== 'undefined'
      ? Number(localStorage.getItem('editor_filetree_width')) || 240
      : 240
  );
  let isResizing = $state(false);
  let activeCleanup: (() => void) | null = null;

  onDestroy(() => {
    if (activeCleanup) {
      activeCleanup();
      activeCleanup = null;
    }
  });

  function startResize(e: PointerEvent) {
    e.preventDefault();
    if (activeCleanup) {
      activeCleanup();
    }
    isResizing = true;
    const startX = e.clientX;
    const startWidth = fileTreeWidth;

    function onMove(ev: PointerEvent) {
      const newWidth = Math.max(160, Math.min(450, startWidth + (ev.clientX - startX)));
      fileTreeWidth = newWidth;
    }

    function onUp() {
      isResizing = false;
      localStorage.setItem('editor_filetree_width', String(fileTreeWidth));
      if (activeCleanup) {
        activeCleanup();
        activeCleanup = null;
      }
    }

    activeCleanup = () => {
      isResizing = false;
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onUp);
    };

    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onUp);
  }
</script>

{#if show}
  <div class="file-tree-pane" style="width: {fileTreeWidth}px;">
    <FileTree
      {xrayFiles}
      {mihomoFiles}
      {selectedFile}
      {activeKernel}
      {onLoadFile}
      {onCreateFile}
      {onRenameFile}
      {onDuplicateFile}
      {onDownloadFile}
      {onDeleteFile}
      {onViewBackups}
    />
  </div>
  <button
    type="button"
    class="editor-splitter"
    class:active={isResizing}
    aria-label={$t('editor.resize_sidebar')}
    tabindex="-1"
    onpointerdown={startResize}
  ></button>
{/if}

<style>
  .file-tree-pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    flex-shrink: 0;
    overflow: hidden;
    max-width: 42%;
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
</style>
