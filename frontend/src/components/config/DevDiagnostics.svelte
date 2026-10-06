<script lang="ts">
  import { t } from '../../i18n';
  import { configLayerEnabled } from '../../stores';
  import Button from '../Button.svelte';
  import { layerSnapshot, applyRunning, diagAction, handleLayerError } from '../../lib/configLayer';

  type DiagAction = Parameters<typeof diagAction>[0];

  // Карточка только в режиме разработчика и при включённом слое (D-19);
  // сервер всё равно отвечает 403 dev_mode_required вне dev_mode (T-144-49)
  const shown = $derived($configLayerEnabled && ($layerSnapshot?.dev_mode ?? false));

  const ACTIONS: { action: DiagAction; key: string }[] = [
    { action: 'add', key: 'cfg.diag.add' },
    { action: 'add_broken_xray', key: 'cfg.diag.add_broken_xray' },
    { action: 'add_broken_mihomo', key: 'cfg.diag.add_broken_mihomo' },
    { action: 'remove', key: 'cfg.diag.remove' }
  ];

  let pending = $state<DiagAction | null>(null);

  async function run(action: DiagAction) {
    pending = action;
    try {
      await diagAction(action);
    } catch (err) {
      handleLayerError(err);
    } finally {
      pending = null;
    }
  }
</script>

{#if shown}
  <section class="card" data-testid="config-diag" aria-label={$t('cfg.diag.title')}>
    <div class="card-title">{$t('cfg.diag.title')}</div>
    <p class="diag-desc">{$t('cfg.diag.desc')}</p>
    <div class="diag-actions">
      {#each ACTIONS as { action, key } (action)}
        <Button
          variant="secondary"
          class="btn-sm"
          data-testid="config-diag-{action}"
          disabled={$applyRunning || pending !== null}
          loading={pending === action}
          onclick={() => void run(action)}>{$t(key)}</Button
        >
      {/each}
    </div>
  </section>
{/if}

<style>
  .diag-desc {
    margin: 0 0 12px;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .diag-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  @media (max-width: 768px) {
    .diag-actions :global(.btn) {
      flex: 1 1 auto;
      min-height: 44px;
    }
  }
</style>
