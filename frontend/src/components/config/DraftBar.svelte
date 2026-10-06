<script lang="ts">
  import { t } from '../../i18n';
  import Button from '../Button.svelte';
  import { draftChanges, layerStatus } from '../../lib/configLayer';

  const ready = $derived($layerStatus === 'ready');
  const dirty = $derived($draftChanges > 0);
</script>

<section class="draft-bar card" data-testid="config-draftbar" aria-label={$t('cfg.draftbar_label')}>
  <div class="draft-bar-status" aria-live="polite">
    {#if dirty}
      <span>{$t('cfg.draft_dirty', { n: $draftChanges })}</span>
    {:else if ready}
      <span>{$t('cfg.draft_clean')}</span>
    {/if}
  </div>
  <div class="draft-bar-actions">
    <Button variant="secondary" disabled={!ready || !dirty}>{$t('cfg.reset')}</Button>
    <Button variant="primary" disabled={!ready || !dirty} data-testid="config-apply"
      >{$t('cfg.apply')}</Button
    >
  </div>
</section>

<style>
  .draft-bar {
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 16px;
    border: 1px solid var(--border);
    box-shadow: var(--shadow-sm);
    font-size: var(--font-size-base);
  }

  .draft-bar-actions {
    display: flex;
    gap: 8px;
  }
</style>
