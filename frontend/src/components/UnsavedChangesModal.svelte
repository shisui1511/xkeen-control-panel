<script lang="ts">
  import Modal from './Modal.svelte';
  import Button from './Button.svelte';
  import { t } from '../i18n';

  let {
    isOpen = false,
    dirtySources = [],
    isSaving = false,
    onSaveAndLeave,
    onLeaveWithoutSaving,
    onStay
  }: {
    isOpen: boolean;
    dirtySources?: string[];
    isSaving?: boolean;
    onSaveAndLeave: () => void;
    onLeaveWithoutSaving: () => void;
    onStay: () => void;
  } = $props();
</script>

<Modal
  {isOpen}
  title={$t('nav.unsaved_title')}
  maxWidth="500px"
  onclose={onStay}
  dataTestid="unsaved-changes-modal"
>
  <div class="unsaved-modal-body">
    <div class="warning-section">
      <div class="warning-icon-wrapper">
        <svg
          class="warning-icon"
          viewBox="0 0 24 24"
          width="24"
          height="24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
      </div>
      <div class="warning-text">
        <p class="main-msg">{$t('nav.unsaved_message')}</p>
        {#if dirtySources && dirtySources.length > 0}
          <div class="sources-list">
            <span class="sources-label"
              >{$t('nav.unsaved_sources', { sources: dirtySources.join(', ') })}</span
            >
            <div class="sources-tags">
              {#each dirtySources as source}
                <span class="source-badge">{source}</span>
              {/each}
            </div>
          </div>
        {/if}
      </div>
    </div>

    <div class="modal-actions-3way">
      <Button variant="primary" loading={isSaving} onclick={onSaveAndLeave}>
        {$t('nav.save_and_leave')}
      </Button>
      <Button variant="danger" disabled={isSaving} onclick={onLeaveWithoutSaving}>
        {$t('nav.leave_without_saving')}
      </Button>
      <Button variant="secondary" disabled={isSaving} onclick={onStay}>
        {$t('nav.stay')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .unsaved-modal-body {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .warning-section {
    display: flex;
    gap: 16px;
    align-items: flex-start;
  }

  .warning-icon-wrapper {
    flex-shrink: 0;
    width: 42px;
    height: 42px;
    border-radius: var(--radius-md);
    background: rgba(245, 158, 11, 0.12);
    border: 1px solid rgba(245, 158, 11, 0.3);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--warning, #f59e0b);
  }

  .warning-text {
    flex: 1;
  }

  .main-msg {
    margin: 0;
    font-size: 14px;
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .sources-list {
    margin-top: 12px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .sources-label {
    font-size: 12.5px;
    color: var(--fg-secondary);
  }

  .sources-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .source-badge {
    display: inline-block;
    padding: 3px 8px;
    font-size: 12px;
    font-weight: 500;
    border-radius: var(--radius-sm);
    background: var(--bg-hover, rgba(255, 255, 255, 0.06));
    border: 1px solid var(--border);
    color: var(--fg-primary);
  }

  .modal-actions-3way {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 10px;
    margin-top: 8px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }

  @media (max-width: 540px) {
    .modal-actions-3way {
      flex-direction: column;
    }
    .modal-actions-3way :global(button) {
      width: 100%;
    }
  }
</style>
