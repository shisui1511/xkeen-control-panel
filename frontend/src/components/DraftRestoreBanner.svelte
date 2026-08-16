<script lang="ts">
  import { t, currentLang } from '../i18n';
  import Button from './Button.svelte';

  let {
    timestamp,
    onRestore,
    onDiscard
  }: {
    timestamp: number;
    onRestore: () => void;
    onDiscard: () => void;
  } = $props();

  let formattedTime = $derived.by(() => {
    if (!timestamp) return '';
    try {
      const date = new Date(timestamp);
      return (
        date.toLocaleTimeString($currentLang === 'ru' ? 'ru-RU' : 'en-US', {
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit'
        }) +
        ' (' +
        date.toLocaleDateString($currentLang === 'ru' ? 'ru-RU' : 'en-US', {
          day: '2-digit',
          month: '2-digit'
        }) +
        ')'
      );
    } catch {
      return new Date(timestamp).toLocaleTimeString();
    }
  });
</script>

<div class="draft-restore-banner" role="alert">
  <div class="banner-content">
    <svg
      class="warning-icon"
      viewBox="0 0 24 24"
      width="18"
      height="18"
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
    <div class="banner-text">
      {$t('draft.detected', { time: formattedTime })}
    </div>
  </div>
  <div class="banner-actions">
    <Button variant="primary" onclick={onRestore}>
      {$t('draft.restore')}
    </Button>
    <Button variant="secondary" onclick={onDiscard}>
      {$t('draft.discard')}
    </Button>
  </div>
</div>

<style>
  .draft-restore-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 18px;
    background: rgba(245, 158, 11, 0.12);
    border: 1px solid var(--warning, #f59e0b);
    border-radius: var(--radius-md);
    margin-bottom: 16px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
    animation: fadeIn var(--transition-fast, 0.2s) ease-out;
  }

  .banner-content {
    display: flex;
    align-items: center;
    gap: 12px;
    color: var(--fg-primary);
    font-size: 13.5px;
    font-weight: 500;
  }

  .warning-icon {
    flex-shrink: 0;
    color: var(--warning, #f59e0b);
  }

  .banner-text {
    line-height: 1.4;
  }

  .banner-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  @media (max-width: 640px) {
    .draft-restore-banner {
      flex-direction: column;
      align-items: flex-start;
    }
    .banner-actions {
      width: 100%;
      justify-content: flex-end;
    }
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
</style>
