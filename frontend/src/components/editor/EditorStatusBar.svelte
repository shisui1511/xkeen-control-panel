<script lang="ts">
  import { t } from '../../i18n';

  interface Props {
    cursorLine: number;
    cursorCol: number;
    isMac?: boolean;
    schemaEnabled: boolean;
    expertMode: boolean;
    applyLoading?: boolean;
    backgroundStatusText?: string;
    backupCount?: number;
    drawerOpen?: boolean;
    onToggleSchema: () => void;
    onToggleExpertMode: () => void;
    onToggleDrawer?: () => void;
  }

  let {
    cursorLine,
    cursorCol,
    isMac = false,
    schemaEnabled,
    expertMode,
    applyLoading = false,
    backgroundStatusText = '',
    backupCount = 0,
    drawerOpen = false,
    onToggleSchema,
    onToggleExpertMode,
    onToggleDrawer
  }: Props = $props();
</script>

<div class="editor-statusbar">
  <div class="sb-left">
    <span>Ln {cursorLine}, Col {cursorCol}</span>
    <span class="status-tip status-shortcut-tip">
      <kbd>{isMac ? '⌘' : 'Ctrl'}</kbd>+<kbd>S</kbd>
      {$t('editor.to_save')}
    </span>
  </div>

  <div class="sb-right">
    <button
      class="chip-toggle"
      class:active={schemaEnabled}
      onclick={onToggleSchema}
      type="button"
      title={$t(schemaEnabled ? 'editor.schema_on' : 'editor.schema_off')}
    >
      <span class="chip-dot"></span>
      {$t(schemaEnabled ? 'editor.schema_on' : 'editor.schema_off')}
    </button>
    <button
      class="chip-toggle"
      class:active={expertMode}
      onclick={onToggleExpertMode}
      type="button"
      title={$t(expertMode ? 'editor.expert_on' : 'editor.expert_off')}
    >
      <span class="chip-dot"></span>
      {$t(expertMode ? 'editor.expert_on' : 'editor.expert_off')}
    </button>

    {#if applyLoading && backgroundStatusText}
      <div class="status-apply-indicator">
        <span class="ks-dot-spin"
          ><span class="ks-dot"></span><span class="ks-dot"></span><span class="ks-dot"
          ></span></span
        >
        <span>{backgroundStatusText}</span>
      </div>
    {/if}

    {#if backupCount > 0}
      <button class="backups-toggle-btn" onclick={() => onToggleDrawer?.()}>
        <svg
          width="10"
          height="10"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          class="chevron-icon"
          class:rotated={drawerOpen}
        >
          <polyline points="18 15 12 9 6 15"></polyline>
        </svg>
        {$t('editor.backups')} ({backupCount})
      </button>
    {/if}
  </div>
</div>

<style>
  .editor-statusbar {
    padding: 6px 14px;
    background: var(--surface-tint);
    border-top: 1px solid var(--border);
    display: flex;
    align-items: center;
    font-family: var(--font-family-mono);
    font-size: 12px;
    color: var(--fg-secondary);
    min-height: 30px;
  }

  .sb-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .sb-right {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .status-shortcut-tip kbd {
    background: var(--surface-tint);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 1px 4px;
    font-size: 12px;
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
  }

  .chip-toggle {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    font-weight: 600;
    font-family: var(--font-family-mono);
    padding: 2px 7px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--surface-tint);
    color: var(--fg-dim);
    cursor: pointer;
    transition: all 0.15s;
    line-height: 1.3;
  }

  .chip-toggle:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .chip-toggle.active {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent);
  }

  .chip-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
  }

  .ks-dot-spin {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-right: 6px;
  }

  .ks-dot {
    width: 6px;
    height: 6px;
    background-color: currentColor;
    border-radius: 50%;
    animation: ks-dot-bounce 1.4s infinite ease-in-out both;
  }

  .ks-dot:nth-child(1) {
    animation-delay: -0.32s;
  }

  .ks-dot:nth-child(2) {
    animation-delay: -0.16s;
  }

  @keyframes ks-dot-bounce {
    0%,
    80%,
    100% {
      transform: scale(0);
    }
    40% {
      transform: scale(1);
    }
  }

  .status-apply-indicator {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--accent);
    padding: 0 10px;
    border-left: 1px solid var(--border);
  }

  .backups-toggle-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: var(--surface-tint);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--fg-secondary);
    font-size: 12px;
    padding: 4px 10px;
    cursor: pointer;
    font-family: var(--font-family-mono);
    transition: all 0.15s ease;
    margin-left: 10px;
  }

  .backups-toggle-btn:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .chevron-icon {
    transition: transform 0.2s ease;
  }

  .chevron-icon.rotated {
    transform: rotate(180deg);
  }
</style>
