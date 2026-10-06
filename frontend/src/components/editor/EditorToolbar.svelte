<script lang="ts">
  import { fade } from 'svelte/transition';
  import { t } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import EditorTabs, { type EditorTab } from './EditorTabs.svelte';
  import EditorKernelWidget from '../status/EditorKernelWidget.svelte';

  interface Props {
    showSidebar?: boolean;
    tabs: EditorTab[];
    activeTabPath: string;
    selectedFile?: string;
    hasDraft?: boolean;
    fileType?: string;
    activeKernel?: string;
    onToggleSidebar?: () => void;
    onSwitchTab: (path: string) => void;
    onPinTab: (path: string) => void;
    onCloseTab: (path: string) => void;
    onRestoreDraft?: () => void;
    onDiscardDraft?: () => void;
    onDownloadFile?: () => void;
    onRenameFile?: () => void;
    onOpenTemplates?: () => void;
    onOpenGenerator?: () => void;
    onApplyQuickFixes?: () => void;
    onDeleteFile?: () => void;
    /** Файл панели под управлением слоя: правящие действия недоступны (D-12) */
    managedReadOnly?: boolean;
  }

  let {
    showSidebar = true,
    tabs,
    activeTabPath,
    selectedFile = '',
    hasDraft = false,
    fileType = '',
    activeKernel,
    onToggleSidebar,
    onSwitchTab,
    onPinTab,
    onCloseTab,
    onRestoreDraft,
    onDiscardDraft,
    onDownloadFile,
    onRenameFile,
    onOpenTemplates,
    onOpenGenerator,
    onApplyQuickFixes,
    onDeleteFile,
    managedReadOnly = false
  }: Props = $props();

  // Навигация, скачивание и поиск остаются доступными; всё, что меняет файл, — нет
  const readOnlyHint = $derived(managedReadOnly ? $t('editor.managed_readonly_hint') : undefined);

  let showKebabMenu = $state(false);

  function toggleKebab(e: MouseEvent) {
    e.stopPropagation();
    showKebabMenu = !showKebabMenu;
    if (showKebabMenu) {
      const close = () => {
        showKebabMenu = false;
        window.removeEventListener('click', close);
      };
      setTimeout(() => window.addEventListener('click', close), 0);
    }
  }
</script>

<div class="editor-subhead-bar">
  <button
    class="btn-sidebar-toggle"
    class:active={showSidebar}
    onclick={() => onToggleSidebar?.()}
    title={showSidebar ? $t('editor.hide_sidebar') : $t('editor.show_sidebar')}
    aria-label={showSidebar ? $t('editor.hide_sidebar') : $t('editor.show_sidebar')}
    aria-expanded={showSidebar}
  >
    {#if showSidebar}
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"><polyline points="15 18 9 12 15 6" /></svg
      >
    {:else}
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"><polyline points="9 18 15 12 9 6" /></svg
      >
    {/if}
  </button>

  <div class="subhead-tabs-container">
    <EditorTabs {tabs} {activeTabPath} {onSwitchTab} {onPinTab} {onCloseTab} />
  </div>

  {#if selectedFile}
    <div class="subhead-meta-container">
      {#if hasDraft}
        <div class="editor-draft-bar" style="display: inline-flex; align-items: center; gap: 4px;">
          <span class="badge badge-warning" style="font-size: 12px; padding: 2px 6px;">
            {$t('editor.has_draft')}
          </span>
          <button class="btn btn-xs btn-primary" onclick={() => onRestoreDraft?.()}>
            {$t('editor.restore_draft')}
          </button>
          <button class="btn btn-xs btn-secondary" onclick={() => onDiscardDraft?.()}>
            {$t('editor.discard_draft')}
          </button>
        </div>
      {/if}
      <EditorKernelWidget {activeKernel} />
      <span class="subhead-file-meta">{fileType} • UTF‑8</span>
      <div class="kebab-wrap">
        <button
          class="btn-kebab tap-zone-44"
          onclick={toggleKebab}
          aria-label={$t('editor.file_actions')}
          title={$t('editor.file_actions')}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <circle cx="12" cy="12" r="1" /><circle cx="12" cy="5" r="1" /><circle
              cx="12"
              cy="19"
              r="1"
            />
          </svg>
        </button>
        {#if showKebabMenu}
          <div class="kebab-dropdown" transition:fade={{ duration: 100 }}>
            <button class="kebab-item" onclick={() => onDownloadFile?.()}>
              <Icon name="download" size={14} />
              {$t('editor.download_file')}
            </button>
            <button
              class="kebab-item"
              onclick={() => onRenameFile?.()}
              disabled={managedReadOnly}
              title={readOnlyHint}
            >
              <Icon name="edit" size={14} />
              {$t('app.rename')}
            </button>
            <button
              class="kebab-item"
              onclick={() => onOpenTemplates?.()}
              disabled={managedReadOnly}
              title={readOnlyHint}
            >
              <Icon name="settings" size={14} />
              {$t('editor.templates')}
            </button>
            {#if fileType === 'JSON'}
              <button
                class="kebab-item"
                onclick={() => onOpenGenerator?.()}
                disabled={managedReadOnly}
                title={readOnlyHint}
              >
                <Icon name="settings" size={14} />
                {$t('editor.generator')}
              </button>
            {/if}
            <button
              class="kebab-item"
              onclick={() => onApplyQuickFixes?.()}
              disabled={managedReadOnly}
              title={readOnlyHint}
            >
              <Icon name="settings" size={14} />
              {$t('editor.quick_fixes')}
            </button>
            <div class="kebab-divider"></div>
            <button
              class="kebab-item danger"
              onclick={() => onDeleteFile?.()}
              disabled={managedReadOnly}
              title={readOnlyHint}
            >
              <Icon name="trash" size={14} />
              {$t('app.delete')}
            </button>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .editor-subhead-bar {
    display: flex;
    align-items: stretch;
    justify-content: space-between;
    border-bottom: 1px solid var(--border);
    background: var(--bg-card);
    min-height: 36px;
    gap: 0;
    padding-right: 8px;
    flex-shrink: 0;
  }

  .btn-sidebar-toggle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    min-height: 36px;
    height: 100%;
    background: transparent;
    border: none;
    border-right: 1px solid var(--border);
    color: var(--fg-dim);
    cursor: pointer;
    transition: all 0.15s;
    flex-shrink: 0;
  }

  .btn-sidebar-toggle:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .btn-sidebar-toggle.active {
    color: var(--accent);
    background: var(--surface-tint);
  }

  .subhead-tabs-container {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: stretch;
    height: 100%;
    overflow: hidden;
  }

  .subhead-meta-container {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-left: 8px;
    flex-shrink: 0;
  }

  .subhead-file-meta {
    font-size: 12px;
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
    background: var(--surface-tint);
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
  }

  .kebab-wrap {
    position: relative;
    display: inline-block;
  }

  .btn-kebab {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-dim);
    cursor: pointer;
    transition: all 0.15s;
  }

  .btn-kebab:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .kebab-dropdown {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: var(--shadow-md);
    min-width: 180px;
    z-index: 100;
    overflow: hidden;
    padding: 4px;
  }

  .kebab-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 12px;
    background: transparent;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    font-size: 12px;
    color: var(--fg-primary);
    text-align: left;
    transition: background 0.15s;
  }

  .kebab-item:hover:not(:disabled) {
    background: var(--hover);
  }

  .kebab-item:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .kebab-item.danger {
    color: var(--danger);
  }

  .kebab-divider {
    height: 1px;
    background: var(--border);
    margin: 4px 0;
  }

  @media (max-width: 768px) {
    .editor-subhead-bar {
      padding-right: 6px;
    }

    /* Тип файла и виджет ядра есть в шапке и дереве файлов; на узком экране они вытесняют вкладки и меню */
    .subhead-file-meta,
    .subhead-meta-container :global(.editor-kernel-widget) {
      display: none;
    }

    .subhead-meta-container {
      margin-left: 6px;
      gap: 6px;
    }
  }

  /* Сенсорная раскладка: полоса вкладок ≥44 px внутренней высоты, зона «⋮» не обрезается карточкой */
  @media (max-width: 768px), (pointer: coarse) {
    .editor-subhead-bar {
      min-height: 48px;
      padding-right: 10px;
    }

    .subhead-meta-container {
      margin-left: 10px;
    }
  }
</style>
