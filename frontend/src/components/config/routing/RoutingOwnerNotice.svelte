<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import ImmediateMark from '../ImmediateMark.svelte';
  import { applyRunning } from '../../../lib/configLayer';
  import { configSubTabHash } from '../../../lib/configSubTab';
  import { fetchTakeover, reclaimRouting } from '../../../lib/configRouting';
  import TakeRoutingModal from './TakeRoutingModal.svelte';
  import {
    ownerState,
    routingBusy,
    confirmRelease,
    releaseAndPreview,
    guarded
  } from './routingOwner';

  // Плашка владельца маршрутизации на вкладках «Группы» и «Правила» (D-01…D-07): пока
  // управляет XKeen — предупреждение с кнопкой взятия; после взятия — строка с «Отпустить
  // маршрутизацию». Ядер нет — плашки нет. Пока Mihomo на своём профиле, под основной строкой
  // стоит строка D-07: настройки этих вкладок для Mihomo не действуют; без Xray остаётся только
  // она. Решения принимает сервер: плашка читает снимок слоя.
  let { tab }: { tab: 'groups' | 'rules' } = $props();

  const kind = $derived($ownerState.kind);
  const blocked = $derived($routingBusy || $applyRunning);

  // Строка о своём профиле Mihomo зависит только от него, не от kind плашки
  const ownProfile = $derived($ownerState.mihomoOwnProfile);
  // Xray не установлен, Mihomo на своём профиле: брать и отпускать на этих вкладках нечего
  const showMain = $derived(!(ownProfile !== '' && !$ownerState.xrayInstalled));

  // Разделитель, по которому текст с именем профиля режется на часть до и после имени
  const NAME_TOKEN = '\u0001name\u0001';
  const ownParts = $derived.by<[string, string]>(() => {
    const [before, after = ''] = $t('cfg.routing.mihomo_own_profile', {
      name: NAME_TOKEN
    }).split(NAME_TOKEN);
    return [before, after];
  });

  // Имя файла в плашке: первый файл routing Xray, который уйдёт в копии, иначе файл по
  // умолчанию; без Xray — активный профиль Mihomo
  let file = $state('');
  const fallbackFile = $derived($ownerState.xrayInstalled ? '05_routing.json' : 'config.yaml');

  async function loadFile() {
    try {
      const p = await fetchTakeover();
      file = p.xray ? (p.xray.move[0] ?? p.xray.left[0] ?? '') : (p.mihomo?.prev_profile ?? '');
    } catch {
      file = '';
    }
  }

  $effect(() => {
    if (showMain && (kind === 'xkeen' || kind === 'pending')) untrack(loadFile);
  });

  let takeOpen = $state(false);

  // Какое действие дрейфа идёт: его кнопка показывает загрузку, остальные неактивны
  let action = $state<'' | 'mihomo' | 'xray' | 'release'>('');

  async function run(name: 'mihomo' | 'xray' | 'release') {
    action = name;
    try {
      // «Отпустить» пишет возврат в черновик и открывает окно изменений (D-08)
      if (name === 'release') await releaseAndPreview();
      else await guarded(() => reclaimRouting(name));
    } finally {
      action = '';
    }
  }

  const mihomoDrift = $derived($ownerState.mihomoDrift.length > 0);
  const xrayDrift = $derived($ownerState.xrayDrift);
</script>

<!-- Дрейф (D-06, D-07): профиль Mihomo или чужой файл routing решает пользователь; пока он
     не решён, «Применить» заблокировано механизмом дрейфа слоя -->
{#if mihomoDrift}
  <div class="alert alert-error ro-notice" role="alert" data-testid="routing-drift-mihomo">
    <Icon name="warning" size={16} class="ro-icon" />
    <div class="ro-body">
      <span class="ro-text">{$t('cfg.routing.drift_profile')}</span>
      <div class="ro-actions">
        <!-- «Вернуть профиль панели» пишет файлы сразу, мимо черновика (D-14) -->
        <ImmediateMark />
        <Button
          variant="primary"
          disabled={blocked}
          loading={action === 'mihomo'}
          data-testid="routing-reclaim-mihomo"
          onclick={() => void run('mihomo')}
        >
          {$t('cfg.routing.drift_reclaim_profile')}
        </Button>
        <Button
          variant="secondary"
          disabled={blocked}
          loading={action === 'release'}
          data-testid="routing-drift-release"
          onclick={() => void run('release')}
        >
          {$t('cfg.routing.drift_release')}
        </Button>
      </div>
    </div>
  </div>
{/if}
{#if xrayDrift.length > 0}
  <div class="alert alert-error ro-notice" role="alert" data-testid="routing-drift-xray">
    <Icon name="warning" size={16} class="ro-icon" />
    <div class="ro-body">
      <span class="ro-text">{$t('cfg.routing.drift_foreign', { file: xrayDrift.join(', ') })}</span>
      <div class="ro-actions">
        <!-- «Вернуть файл панели» пишет файл сразу, мимо черновика (D-14) -->
        <ImmediateMark />
        <Button
          variant="primary"
          disabled={blocked}
          loading={action === 'xray'}
          data-testid="routing-reclaim-xray"
          onclick={() => void run('xray')}
        >
          {$t('cfg.routing.drift_reclaim_file')}
        </Button>
        <Button
          variant="secondary"
          disabled={blocked}
          loading={action === 'release'}
          data-testid="routing-drift-release-xray"
          onclick={() => void run('release')}
        >
          {$t('cfg.routing.drift_release')}
        </Button>
      </div>
    </div>
  </div>
{/if}

{#if showMain && (kind === 'xkeen' || kind === 'pending')}
  <div
    class="alert alert-warning ro-notice"
    data-testid="routing-notice"
    data-owner={kind === 'pending' ? 'pending' : 'xkeen'}
  >
    <Icon name="warning" size={16} class="ro-icon" />
    <div class="ro-body">
      <span class="ro-text">{$t('cfg.routing.notice_xkeen', { file: file || fallbackFile })}</span>
      {#if kind === 'pending'}
        <span class="ro-text" data-testid="routing-take-pending">
          {$t('cfg.routing.take_pending')}
        </span>
      {:else if tab === 'rules'}
        <div class="ro-actions">
          <Button
            variant="primary"
            disabled={blocked}
            data-testid="routing-take"
            onclick={() => (takeOpen = true)}
          >
            {$t('cfg.routing.take_btn')}
          </Button>
        </div>
      {:else}
        <div class="ro-actions">
          <a
            class="btn btn-secondary"
            href={configSubTabHash('rules')}
            data-testid="routing-goto-rules"
          >
            {$t('cfg.routing.goto_rules')}
          </a>
        </div>
      {/if}
    </div>
  </div>
{:else if showMain && kind === 'panel'}
  <div class="ro-panel" data-testid="routing-notice" data-owner="panel">
    <span class="ro-ok"><Icon name="check" size={16} /></span>
    <span class="ro-panel-text">{$t('cfg.routing.notice_panel')}</span>
    <Button
      variant="secondary"
      class="btn-sm ro-release"
      disabled={blocked}
      loading={$routingBusy}
      title={$applyRunning ? $t('cfg.routing.apply_busy_hint') : undefined}
      data-testid="routing-release"
      onclick={() => void confirmRelease()}
    >
      {$t('cfg.routing.release_btn')}
    </Button>
  </div>
{/if}

{#if ownProfile !== ''}
  <div class="ro-own" role="status" data-testid="routing-mihomo-own-profile">
    <span class="ro-own-icon"><Icon name="info" size={16} /></span>
    <div class="ro-own-body">
      <span class="ro-own-text">
        {ownParts[0]}<span class="ro-own-name">{ownProfile}</span>{ownParts[1]}
      </span>
      <a
        class="ro-own-link"
        href={configSubTabHash('cores')}
        data-testid="routing-mihomo-profiles-link"
      >
        {$t('cfg.prof.title')}
      </a>
    </div>
  </div>
{/if}

<TakeRoutingModal open={takeOpen} mode="take" onclose={() => (takeOpen = false)} />

<style>
  .ro-notice {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
  }

  .ro-notice :global(.ro-icon) {
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
  }

  .ro-body {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .ro-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ro-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
  }

  .ro-panel {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-3);
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .ro-ok {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--success);
  }

  .ro-panel-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ro-own {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-width: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .ro-own-icon {
    display: inline-flex;
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
  }

  .ro-own-body {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-width: 0;
  }

  .ro-own-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ro-own-name {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .ro-own-link {
    color: var(--accent-text);
    font-weight: 600;
  }

  @media (max-width: 768px) {
    .ro-own-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .ro-actions :global(.btn),
    .ro-panel :global(.ro-release) {
      min-height: 44px;
    }

    .ro-panel :global(.ro-release) {
      flex: 1 1 100%;
    }
  }
</style>
