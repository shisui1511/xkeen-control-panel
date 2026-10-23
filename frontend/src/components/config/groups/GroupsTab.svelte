<script lang="ts">
  import { onMount } from 'svelte';
  import { t, tp } from '../../../i18n';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import { layerSnapshot } from '../../../lib/configLayer';
  import { configSubTabHash } from '../../../lib/configSubTab';
  import { routingView, routingStatus, loadRouting } from '../../../lib/configRouting';

  // Представление маршрутизации читается при открытии вкладки; дальше его перечитывают
  // события SSE routing, sources и draft (подписки регистрирует configRouting.ts).
  onMount(() => {
    void loadRouting();
  });

  const loading = $derived(
    $layerSnapshot === null || $routingStatus === 'idle' || $routingStatus === 'loading'
  );
  const failed = $derived(!loading && $routingStatus === 'error' && $routingView === null);
  const groups = $derived($routingView?.groups ?? []);
  const hasSources = $derived($routingView?.has_sources ?? false);
  // Групп нет и источников нет: группы по умолчанию появятся с первым источником (D-09)
  const noSources = $derived(
    !loading && !failed && $routingView !== null && groups.length === 0 && !hasSources
  );
  const empty = $derived(
    !loading && !failed && $routingView !== null && groups.length === 0 && hasSources
  );
  const showList = $derived(!loading && !failed && groups.length > 0);

  function gotoNodes() {
    window.location.hash = configSubTabHash('nodes');
  }
</script>

<div class="groups-tab" data-testid="groups-tab">
  {#if loading}
    <div class="groups-skeleton" aria-hidden="true">
      <Skeleton type="card" height="120px" />
      <Skeleton type="card" height="120px" />
    </div>
  {:else if failed}
    <EmptyState
      title={$t('cfg.load_error_title')}
      description={$t('cfg.load_error_body')}
      ctaText={$t('cfg.load_error_retry')}
      plain
      oncta={() => void loadRouting()}
    />
  {:else if noSources}
    <EmptyState
      title={$t('cfg.groups.nosrc_title')}
      description={$t('cfg.groups.nosrc_body')}
      ctaText={$t('cfg.groups.nosrc_cta')}
      plain
      oncta={gotoNodes}
    />
  {:else if empty}
    <EmptyState
      title={$t('cfg.groups.empty_title')}
      description={$t('cfg.groups.empty_body')}
      plain
    />
  {:else if showList}
    <div class="groups-panel">
      <span class="groups-count" data-testid="groups-count">
        {$tp('cfg.groups.count', groups.length)}
      </span>
    </div>
    <ul class="groups-list">
      {#each groups as group (group.id)}
        <li class="card groups-row" data-testid="group-row-{group.id}">
          <span class="groups-name">{group.name}</span>
          <span class="chip chip-default">{$t(`cfg.groups.mode_${group.mode}`)}</span>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .groups-tab {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }

  .groups-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
  }

  .groups-panel {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--spacing-2) var(--spacing-4);
    min-height: var(--btn-h, 38px);
  }

  .groups-count {
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .groups-list {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .groups-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .groups-name {
    min-width: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    overflow-wrap: anywhere;
  }
</style>
