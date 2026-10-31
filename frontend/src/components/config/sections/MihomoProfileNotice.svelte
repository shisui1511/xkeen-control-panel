<script lang="ts">
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning } from '../../../lib/configLayer';
  import type { ProfileView } from '../../../lib/configCores';
  import TakeRoutingModal from '../routing/TakeRoutingModal.svelte';
  import { routingBusy, confirmRelease } from '../routing/routingOwner';

  // Плашка владельца профиля Mihomo над его секциями (D-03, D-05, D-06): профилем управляет
  // XKeen или пользователь — предупреждение и «Управлять в панели», которое открывает
  // существующий диалог взятия маршрутизации (секции берутся вместе с профилем, одним
  // действием); профиль у панели — строка с «Отпустить маршрутизацию и секции»; взятие ждёт
  // «Применить» — бейдж без действий; файла профиля нет — пояснение и то же взятие.
  let { profile }: { profile: ProfileView } = $props();

  // Разделитель, по которому текст с именем профиля режется на часть до и после имени
  const NAME_TOKEN = '\u0001name\u0001';

  const owner = $derived(profile.owner);
  const foreign = $derived(owner === 'xkeen' || owner === 'user');
  const name = $derived(profile.name || 'config.yaml');
  const blocked = $derived($routingBusy || $applyRunning);
  const busyHint = $derived($applyRunning ? $t('cfg.routing.apply_busy_hint') : undefined);

  const foreignParts = $derived.by<[string, string]>(() => {
    const ownerLabel = $t(owner === 'user' ? 'cfg.sec.owner_user' : 'cfg.sec.owner_xkeen');
    const [before, after = ''] = $t('cfg.sec.profile_foreign', {
      name: NAME_TOKEN,
      owner: ownerLabel
    }).split(NAME_TOKEN);
    return [before, after];
  });

  let takeOpen = $state(false);
</script>

<div class="mpn" data-testid="mihomo-profile-notice" data-owner={owner}>
  {#if foreign}
    <div class="alert alert-warning mpn-alert">
      <Icon name="warning" size={16} class="mpn-icon" />
      <div class="mpn-body">
        <span class="mpn-text">
          {foreignParts[0]}<span class="mpn-name">{name}</span>{foreignParts[1]}
        </span>
        <div class="mpn-actions">
          <Button
            variant="primary"
            disabled={blocked}
            title={busyHint}
            data-testid="mihomo-profile-take"
            onclick={() => (takeOpen = true)}
          >
            {$t('cfg.sec.take_btn')}
          </Button>
        </div>
      </div>
    </div>
  {:else if owner === 'panel'}
    <div class="mpn-row">
      <span class="mpn-ok"><Icon name="check" size={16} /></span>
      <span class="mpn-row-text">{$t('cfg.sec.profile_panel')}</span>
      <Button
        variant="secondary"
        class="mpn-btn"
        disabled={blocked}
        loading={$routingBusy}
        title={busyHint}
        data-testid="mihomo-profile-release"
        onclick={() => void confirmRelease()}
      >
        {$t('cfg.sec.profile_release_btn')}
      </Button>
    </div>
  {:else if owner === 'pending'}
    <span class="badge badge-warning" data-testid="mihomo-profile-pending">
      {$t('cfg.sec.owner_pending')}
    </span>
  {:else}
    <div class="mpn-row">
      <span class="mpn-row-text">{$t('cfg.sec.no_file_note')}</span>
      <Button
        variant="primary"
        class="mpn-btn"
        disabled={blocked}
        title={busyHint}
        data-testid="mihomo-profile-take"
        onclick={() => (takeOpen = true)}
      >
        {$t('cfg.sec.take_btn')}
      </Button>
    </div>
  {/if}
</div>

<TakeRoutingModal open={takeOpen} mode="take" onclose={() => (takeOpen = false)} />

<style>
  .mpn {
    min-width: 0;
  }

  .mpn-alert {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
  }

  .mpn-alert :global(.mpn-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .mpn-body {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .mpn-text,
  .mpn-row-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .mpn-name {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    overflow-wrap: anywhere;
  }

  .mpn-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .mpn-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-3);
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .mpn-ok {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--success);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .mpn-actions :global(.btn),
    .mpn-row :global(.mpn-btn) {
      flex: 1 1 auto;
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .mpn-actions :global(.btn),
    .mpn-row :global(.mpn-btn) {
      flex: 1 1 100%;
    }
  }
</style>
