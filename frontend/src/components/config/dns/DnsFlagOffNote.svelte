<script lang="ts">
  import { t } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import DependentProfilesNote from '../profiles/DependentProfilesNote.svelte';

  // Абзац в подтверждении выключения флага слоя при включённом или желаемом перехвате DNS
  // (D-36): DNS вернётся прошивке, старое перенаправление получит исходное значение.
  // Действие не блокируется; абзац о зависимых профилях идёт следом, как раньше.
  let { profilesAll }: { profilesAll: boolean } = $props();
</script>

<div class="warning-text dns-flag-note" data-testid="dns-flag-off-note">
  <Icon name="warning" size={14} class="icon-first-line" />
  <span>{$t('cfg.dns.flag_off_note')}</span>
</div>
<DependentProfilesNote all={profilesAll} />

<style>
  .dns-flag-note {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    margin-top: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
</style>
