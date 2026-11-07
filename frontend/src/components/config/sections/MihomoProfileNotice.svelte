<script lang="ts">
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, draftChanges, layerSnapshot } from '../../../lib/configLayer';
  import type { ProfileView } from '../../../lib/configCores';
  import TakeRoutingModal from '../routing/TakeRoutingModal.svelte';
  import { routingBusy, confirmMihomoRelease, confirmMihomoReturn } from '../routing/routingOwner';

  // Плашка владельца профиля Mihomo над его секциями (D-03, D-05, D-06, D-07, D-09): действует
  // только на Mihomo, Xray и его файлы не затрагивает. Профилем управляет XKeen или
  // пользователь — предупреждение и «Управлять в панели»: при сохранённых значениях секций это
  // возврат с подтверждением, при первом взятии — диалог взятия с областью mihomo. Профиль у
  // панели — строка с «Отпустить Mihomo» (подтверждение, применение сразу, итог тостом). Взятие
  // ждёт «Применить» — бейдж без действий; файла профиля нет — пояснение и то же взятие.
  // Открытый черновик блокирует отпускание и возврат постоянной строкой (D-08): оба идут
  // применением. stopped — Mihomo остановлен: в подтверждение добавляется абзац об этом.
  let { profile, stopped = false }: { profile: ProfileView; stopped?: boolean } = $props();

  const uid = $props.id();
  const hintId = `${uid}-draft-block`;

  // Разделитель, по которому текст с именем профиля режется на часть до и после имени
  const NAME_TOKEN = '\u0001name\u0001';

  const owner = $derived(profile.owner);
  const foreign = $derived(owner === 'xkeen' || owner === 'user');
  const name = $derived(profile.name || 'config.yaml');
  const blocked = $derived($routingBusy || $applyRunning);
  const busyHint = $derived($applyRunning ? $t('cfg.routing.apply_busy_hint') : undefined);

  // Значения секций сохранены при прошлом отпускании: «Управлять в панели» — возврат
  const returnMode = $derived(
    owner !== 'releasing' && $layerSnapshot?.routing.mihomo_saved === true
  );
  const draftOpen = $derived($draftChanges > 0);
  // Отпускание и возврат идут применением всего черновика: открытый черновик их блокирует.
  // Первое взятие через диалог пишет в черновик и не блокируется
  const releaseBlocked = $derived(blocked || draftOpen);
  const takeBlocked = $derived(blocked || (returnMode && draftOpen));
  const showHint = $derived(draftOpen && (owner === 'panel' || returnMode));

  /** Имя профиля без каталога и расширения: так он называется в списке профилей. */
  function profileName(file: string): string {
    return file.replace(/^profiles\//, '').replace(/\.yaml$/, '');
  }

  // Профиль, к которому Mihomo вернётся при отпускании
  const returnName = $derived(
    profileName($layerSnapshot?.routing.mihomo?.prev_profile ?? '') || 'config.yaml'
  );

  function release(): void {
    void confirmMihomoRelease(returnName, stopped);
  }

  function take(): void {
    if (returnMode) void confirmMihomoReturn(name, stopped);
    else takeOpen = true;
  }

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
            disabled={takeBlocked}
            title={busyHint}
            ariaDescribedby={showHint ? hintId : undefined}
            data-testid="mihomo-profile-take"
            onclick={take}
          >
            {$t('cfg.sec.take_btn')}
          </Button>
        </div>
        {@render hint()}
      </div>
    </div>
  {:else if owner === 'panel'}
    <div class="mpn-row">
      <span class="mpn-ok"><Icon name="check" size={16} /></span>
      <span class="mpn-row-text">{$t('cfg.sec.profile_panel')}</span>
      <Button
        variant="secondary"
        class="mpn-btn"
        disabled={releaseBlocked}
        loading={$routingBusy}
        title={busyHint}
        ariaDescribedby={showHint ? hintId : undefined}
        data-testid="mihomo-profile-release"
        onclick={release}
      >
        {$t('cfg.sec.profile_release_btn')}
      </Button>
    </div>
    {@render hint()}
  {:else if owner === 'pending'}
    <span class="badge badge--tag badge-warning" data-testid="mihomo-profile-pending">
      {$t('cfg.sec.owner_pending')}
    </span>
  {:else if owner === 'releasing'}
    <div class="mpn-row" data-testid="mihomo-profile-releasing">
      <span class="badge badge--tag badge-warning">{$t('cfg.sec.owner_releasing')}</span>
      <span class="mpn-row-text">{$t('cfg.sec.profile_releasing')}</span>
      <Button
        variant="secondary"
        class="mpn-btn"
        disabled={takeBlocked}
        title={busyHint}
        ariaDescribedby={showHint ? hintId : undefined}
        data-testid="mihomo-profile-take"
        onclick={take}
      >
        {$t('cfg.sec.take_btn')}
      </Button>
    </div>
    {@render hint()}
  {:else}
    <div class="mpn-row">
      <span class="mpn-row-text">{$t('cfg.sec.no_file_note')}</span>
      <Button
        variant="primary"
        class="mpn-btn"
        disabled={takeBlocked}
        title={busyHint}
        ariaDescribedby={showHint ? hintId : undefined}
        data-testid="mihomo-profile-take"
        onclick={take}
      >
        {$t('cfg.sec.take_btn')}
      </Button>
    </div>
    {@render hint()}
  {/if}
</div>

{#snippet hint()}
  {#if showHint}
    <p class="mpn-hint" id={hintId} data-testid="mihomo-profile-draft-block">
      <Icon name="warning" size={16} class="mpn-hint-icon" />
      <span>{$t('cfg.prof.draft_block')}</span>
    </p>
  {/if}
{/snippet}

<TakeRoutingModal open={takeOpen} mode="take" scope="mihomo" onclose={() => (takeOpen = false)} />

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
    margin-top: var(--spacing-0-5);
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

  .mpn-hint {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    margin: var(--spacing-2) 0 0;
    min-width: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .mpn-hint :global(.mpn-hint-icon) {
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
    color: var(--warning);
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
