<script lang="ts">
  import type { Attachment } from 'svelte/attachments';
  import { t } from '../../i18n';
  import { isConflict } from '../../stores';
  import StatusBadge from '../StatusBadge.svelte';
  import LiveIndicator from '../LiveIndicator.svelte';
  import ImmediateMark from '../config/ImmediateMark.svelte';

  type SaveBadgeVariant = 'running' | 'warning' | 'stopped';

  interface Props {
    saveStatusState: {
      kind: 'live' | 'badge';
      label: string;
      variant?: SaveBadgeVariant;
    };
    selectedFile?: string;
    loading?: boolean;
    saving?: boolean;
    applyLoading?: boolean;
    onReloadFile: () => void;
    onSaveFile: () => void;
    onSaveAndApply: () => void;
    /** Файл панели под управлением слоя: сохранение недоступно (D-12) */
    managedReadOnly?: boolean;
    /** Прямая запись на диск сразу, мимо черновика слоя (D-02, D-14) */
    immediate?: boolean;
    /** Причина, по которой запись отключена (например, стоп-список) */
    saveDisabledReason?: string;
  }

  let {
    saveStatusState,
    selectedFile = '',
    loading = false,
    saving = false,
    applyLoading = false,
    onReloadFile,
    onSaveFile,
    onSaveAndApply,
    managedReadOnly = false,
    immediate = false,
    saveDisabledReason = ''
  }: Props = $props();

  let showOverflowMenu = $state(false);

  // В конфликте ядер применять нечего: сохранение файла (данные) остаётся доступным.
  const saveAndApplyLabel = $derived(
    $isConflict ? $t('kernel.conflict_blocked') : $t('editor.save_and_apply')
  );

  const isSaveDisabled = $derived(managedReadOnly || Boolean(saveDisabledReason));
  const effectiveSaveTitle = $derived(
    saveDisabledReason || (managedReadOnly ? $t('editor.managed_readonly_hint') : $t('app.save'))
  );
  const effectiveSaveAndApplyTitle = $derived(
    saveDisabledReason || (managedReadOnly ? $t('editor.managed_readonly_hint') : saveAndApplyLabel)
  );

  function toggleOverflowMenu(e: MouseEvent) {
    e.stopPropagation();
    showOverflowMenu = !showOverflowMenu;
  }

  // Пока меню открыто: Escape и клик вне блока кнопки закрывают его
  const closeMenuOnEscapeOrOutside: Attachment<HTMLElement> = (menu) => {
    const container = menu.parentElement;
    const onKeydown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') showOverflowMenu = false;
    };
    const onClickOutside = (e: MouseEvent) => {
      if (container && !container.contains(e.target as Node)) showOverflowMenu = false;
    };
    window.addEventListener('keydown', onKeydown);
    window.addEventListener('click', onClickOutside);
    return () => {
      window.removeEventListener('keydown', onKeydown);
      window.removeEventListener('click', onClickOutside);
    };
  };
</script>

<div class="eph-right">
  {#if saveStatusState.kind === 'live'}
    <LiveIndicator live={true} label={saveStatusState.label} />
  {:else if selectedFile}
    <!-- Without an open file there is nothing to be saved or unsaved. -->
    <StatusBadge variant={saveStatusState.variant || 'idle'} label={saveStatusState.label} />
  {/if}
  {#if selectedFile}
    <button
      class="btn btn-secondary btn-compact btn-desktop-only"
      onclick={onReloadFile}
      disabled={loading}
      title={$t('editor.reload')}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8" /><path d="M21 3v5h-5" /></svg
      >
      {$t('editor.reload')}
    </button>
    <button
      class="btn btn-secondary btn-compact btn-desktop-only"
      onclick={onSaveFile}
      disabled={saving || applyLoading || isSaveDisabled}
      title={effectiveSaveTitle}
    >
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        ><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z" /><polyline
          points="17 21 17 13 7 13 7 21"
        /><polyline points="7 3 7 8 15 8" /></svg
      >
      {saving ? $t('app.loading') : $t('app.save')}
    </button>
    {#if immediate}
      <span class="immediate-wrap btn-desktop-only">
        <ImmediateMark />
      </span>
    {/if}

    <div class="overflow-wrap">
      <button
        type="button"
        class="btn btn-secondary btn-compact btn-overflow-trigger"
        onclick={toggleOverflowMenu}
        aria-label={$t('editor.save_more_actions')}
        title={$t('editor.save_more_actions')}
        aria-haspopup="true"
        aria-expanded={showOverflowMenu}
      >
        <span>{$t('editor.more_short')}</span>
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>

      {#if showOverflowMenu}
        <div class="overflow-dropdown" role="menu" {@attach closeMenuOnEscapeOrOutside}>
          <button
            type="button"
            class="dropdown-item"
            role="menuitem"
            onclick={() => {
              showOverflowMenu = false;
              onSaveFile();
            }}
            disabled={saving || applyLoading || isSaveDisabled}
            title={effectiveSaveTitle}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z" />
              <polyline points="17 21 17 13 7 13 7 21" />
              <polyline points="7 3 7 8 15 8" />
            </svg>
            {saving ? $t('app.loading') : $t('app.save')}
          </button>
          <button
            type="button"
            class="dropdown-item"
            role="menuitem"
            onclick={() => {
              showOverflowMenu = false;
              onReloadFile();
            }}
            disabled={loading}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
              <path d="M21 3v5h-5" />
            </svg>
            {$t('editor.reload')}
          </button>
        </div>
      {/if}
    </div>

    <button
      class="btn btn-accent btn-compact"
      onclick={onSaveAndApply}
      disabled={saving || applyLoading || $isConflict || isSaveDisabled}
      title={effectiveSaveAndApplyTitle}
      aria-label={saveAndApplyLabel}
    >
      {#if applyLoading}
        <span class="ks-dot-spin"
          ><span class="ks-dot"></span><span class="ks-dot"></span><span class="ks-dot"
          ></span></span
        >
        {$t('app.loading')}
      {:else}
        <svg
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          ><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z" /><polyline
            points="17 21 17 13 7 13 7 21"
          /><polyline points="7 3 7 8 15 8" /><path d="m14 11-2 2-2-2" /><path d="M12 7v6" /></svg
        >
        {$t('editor.save_and_apply')}
      {/if}
    </button>
  {/if}
  {#if immediate && selectedFile}
    <div class="immediate-mobile-bar">
      <ImmediateMark />
    </div>
  {/if}
</div>

<style>
  .eph-right {
    display: inline-flex;
    align-items: center;
    flex-wrap: wrap;
    max-width: 100%;
    gap: var(--spacing-2);
    position: relative;
  }

  .overflow-wrap {
    position: relative;
    display: inline-flex;
  }

  .btn-overflow-trigger {
    display: none;
    padding: var(--spacing-1) var(--spacing-2-5);
    gap: var(--spacing-1);
    align-items: center;
    justify-content: center;
  }

  .overflow-dropdown {
    position: absolute;
    left: 0;
    top: calc(100% + 4px);
    z-index: 50;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    min-width: 180px;
    padding: var(--spacing-1);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-0-5);
    animation: menuPop var(--duration-fast) var(--ease-out);
  }

  .dropdown-item {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    width: 100%;
    padding: var(--spacing-2) var(--spacing-2-5);
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: var(--font-size-sm);
    text-align: left;
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .dropdown-item:hover:not(:disabled) {
    background: var(--hover);
  }

  .dropdown-item:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-compact {
    padding: var(--spacing-1) var(--spacing-2-5);
    font-size: var(--font-size-xs);
    height: 30px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .btn-accent {
    background: linear-gradient(180deg, var(--accent), var(--accent-2));
    border: 1px solid var(--accent);
    color: var(--btn-primary-text);
    font-weight: 600;
  }
  .btn-accent:hover:not(:disabled) {
    background: var(--accent-hover);
    box-shadow: 0 0 10px var(--accent-soft);
  }
  .btn-accent:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .ks-dot-spin {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    margin-right: var(--spacing-1-5);
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

  .immediate-wrap {
    display: inline-flex;
    align-items: center;
  }

  .immediate-mobile-bar {
    display: none;
    width: 100%;
    padding: var(--spacing-0-5) 0;
  }

  @media (max-width: 768px) {
    .btn-desktop-only {
      display: none !important;
    }
    .btn-overflow-trigger {
      display: inline-flex !important;
    }
    .immediate-wrap {
      display: none !important;
    }
    .immediate-mobile-bar {
      display: flex !important;
    }
  }
</style>
