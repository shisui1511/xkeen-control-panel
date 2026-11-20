<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { devMode, fetchDevMode } from '../../stores';
  import Button from '../Button.svelte';
  import Section from '../Section.svelte';
  import { layerSnapshot, applyRunning, diagAction, handleLayerError } from '../../lib/configLayer';

  type DiagAction = Parameters<typeof diagAction>[0];

  // Карточка только в режиме разработчика (D-19); сервер всё равно отвечает
  // 403 dev_mode_required вне dev_mode (T-144-49). Флаг берётся из того же хранилища, что и
  // переключатель в «Настройках», поэтому карточка появляется сразу после включения;
  // при открытии страницы значение читается с сервера
  const shown = $derived($devMode);

  onMount(() => {
    void fetchDevMode();
  });

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
  <Section title={$t('cfg.diag.title')} description={$t('cfg.diag.desc')} testid="config-diag">
    <div class="card diag-actions">
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
  </Section>
{/if}

<style>
  .diag-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  @media (max-width: 768px) {
    .diag-actions :global(.btn) {
      flex: 1 1 auto;
      min-height: 44px;
    }
  }
</style>
