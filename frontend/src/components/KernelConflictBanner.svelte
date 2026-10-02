<script lang="ts">
  import { t } from '../i18n';
  import Warning from '../lib/components/icons/Warning.svelte';
</script>

<div
  class="alert alert-error kernel-conflict"
  role="alert"
  aria-label={$t('kernel.conflict_region')}
  data-testid="kernel-conflict-banner"
>
  <span class="kernel-conflict__icon" aria-hidden="true">
    <Warning size={18} />
  </span>
  <div class="kernel-conflict__text">
    <strong>{$t('kernel.conflict_title')}</strong>
    <p>{$t('kernel.conflict_desc')}</p>
  </div>
</div>

<style>
  .kernel-conflict {
    margin: 12px 16px 0;
    padding: 12px 16px;
    justify-content: space-between;
    align-items: flex-start;
    gap: 16px;
  }

  .kernel-conflict__icon {
    display: inline-flex;
    flex: none;
    margin-top: 2px;
  }

  .kernel-conflict__text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .kernel-conflict__text strong {
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    overflow-wrap: anywhere;
  }

  .kernel-conflict__text p {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 400;
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  @media (max-width: 640px) {
    .kernel-conflict {
      flex-direction: column;
    }
  }
</style>
