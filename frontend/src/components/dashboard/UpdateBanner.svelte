<script lang="ts">
  import { t } from '../../i18n';
  import { updateState, isUpdateDismissed, dismissUpdate } from '../../lib/updateNotify';
  import { releaseKind } from '../../lib/releaseNotes';

  // Скрытие действует до выхода следующей версии
  let dismissed = $state('');

  const latest = $derived($updateState?.has_update ? ($updateState.latest_version ?? '') : '');
  const visible = $derived(!!latest && dismissed !== latest && !isUpdateDismissed(latest));
  const kind = $derived(releaseKind(latest));

  function hide() {
    dismissUpdate(latest);
    dismissed = latest;
  }
</script>

{#if visible}
  <div class="update-banner" role="status">
    <div class="update-banner-text">
      <strong>{$t('dash.update_banner_title', { version: latest })}</strong>
      {#if kind !== 'stable'}
        <span class="badge badge-primary">{$t(`settings.update_kind_${kind}`)}</span>
      {/if}
      <span class="update-banner-desc">
        {$updateState?.auto_install
          ? $t('dash.update_banner_auto', { window: $updateState.install_window })
          : $t('dash.update_banner_manual')}
      </span>
    </div>
    <div class="update-banner-actions">
      <a class="btn btn-primary btn-sm" href="#/settings?tab=updates"
        >{$t('dash.update_banner_open')}</a
      >
      <button class="btn btn-secondary btn-sm" onclick={hide}
        >{$t('dash.update_banner_hide')}</button
      >
    </div>
  </div>
{/if}

<style>
  .update-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px 16px;
    flex-wrap: wrap;
    margin-bottom: 18px;
    padding: 12px 14px;
    border-radius: var(--radius-md);
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
  }

  .update-banner-text {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 10px;
    font-size: 13px;
    color: var(--fg-secondary);
    min-width: 0;
  }

  .update-banner-text strong {
    color: var(--fg-primary);
    font-size: 14px;
  }

  .update-banner-actions {
    display: flex;
    gap: 8px;
    flex-shrink: 0;
  }

  .btn-sm {
    padding: 6px 12px;
    font-size: 12px;
  }
</style>
