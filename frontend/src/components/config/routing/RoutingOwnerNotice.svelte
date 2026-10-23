<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning } from '../../../lib/configLayer';
  import { configSubTabHash } from '../../../lib/configSubTab';
  import { fetchTakeover } from '../../../lib/configRouting';
  import TakeRoutingModal from './TakeRoutingModal.svelte';
  import { ownerState, routingBusy, confirmRelease } from './routingOwner';

  // Плашка владельца маршрутизации на вкладках «Группы» и «Правила» (D-01…D-07): пока
  // управляет XKeen — предупреждение с кнопкой взятия; после взятия — строка с «Отпустить
  // маршрутизацию». Ядер нет — плашки нет. Решения принимает сервер: плашка читает снимок слоя.
  let { tab } = $props<{ tab: 'groups' | 'rules' }>();

  const kind = $derived($ownerState.kind);
  const blocked = $derived($routingBusy || $applyRunning);

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
    if (kind === 'xkeen' || kind === 'pending') untrack(loadFile);
  });

  let takeOpen = $state(false);
</script>

{#if kind === 'xkeen' || kind === 'pending'}
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
{:else if kind === 'panel'}
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

<TakeRoutingModal open={takeOpen} mode="take" onclose={() => (takeOpen = false)} />

<style>
  .ro-notice {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
  }

  .ro-notice :global(.ro-icon) {
    flex-shrink: 0;
    margin-top: 2px;
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
