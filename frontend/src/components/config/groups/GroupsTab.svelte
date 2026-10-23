<script lang="ts">
  import { onMount } from 'svelte';
  import { t, tp } from '../../../i18n';
  import { showConfirm, showToast, activeKernelState } from '../../../stores';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import { handleLayerError, layerSnapshot } from '../../../lib/configLayer';
  import { configSubTabHash } from '../../../lib/configSubTab';
  import { loadSources, type KernelName } from '../../../lib/configSources';
  import {
    routingView,
    routingStatus,
    loadRouting,
    deleteGroup,
    routingErrorText,
    type GroupView
  } from '../../../lib/configRouting';
  import { kernelNameOf } from '../../../lib/kernelState';
  import { chosenKernel } from '../../../lib/nodesView';
  import { expandedGroups, toggleGroupExpanded } from '../../../lib/routingView';
  import GroupCard from './GroupCard.svelte';

  // Представление маршрутизации читается при открытии вкладки; дальше его перечитывают
  // события SSE routing, sources и draft (подписки регистрирует configRouting.ts).
  // Список источников и узлов нужен диалогу группы и подписи резервного узла.
  onMount(() => {
    void loadRouting();
    void loadSources();
  });

  const loading = $derived(
    $layerSnapshot === null || $routingStatus === 'idle' || $routingStatus === 'loading'
  );
  const failed = $derived(!loading && $routingStatus === 'error' && $routingView === null);
  const groups = $derived($routingView?.groups ?? []);
  const hasSources = $derived($routingView?.has_sources ?? false);
  const installed = $derived($routingView?.installed ?? {});
  // Групп нет и источников нет: группы по умолчанию появятся с первым источником (D-09)
  const noSources = $derived(
    !loading && !failed && $routingView !== null && groups.length === 0 && !hasSources
  );
  const empty = $derived(
    !loading && !failed && $routingView !== null && groups.length === 0 && hasSources
  );
  const showList = $derived(!loading && !failed && groups.length > 0);

  // Сегмент ядра показывается только при двух установленных ядрах (E1 partial)
  const both = $derived(installed.xray === true && installed.mihomo === true);
  const kernel = $derived.by<KernelName>(() => {
    const chosen = $chosenKernel;
    if (chosen !== null && installed[chosen]) return chosen;
    const running = kernelNameOf($activeKernelState);
    if (running !== '' && installed[running]) return running;
    return installed.mihomo && !installed.xray ? 'mihomo' : 'xray';
  });
  const segmentItems = [
    { value: 'xray', label: 'Xray' },
    { value: 'mihomo', label: 'Mihomo' }
  ];

  function gotoNodes() {
    window.location.hash = configSubTabHash('nodes');
  }

  // Удаление группы без ссылок; со ссылками сервер отвечает group_referenced, диалог
  // каскада открывает вкладка в плане 146-21
  async function removeGroup(group: GroupView) {
    const ok = await showConfirm({
      variant: 'danger',
      title: $t('cfg.groups.del_title', { name: group.name }),
      message: $t('cfg.groups.del_simple_body'),
      confirmLabel: $t('cfg.groups.del_confirm'),
      cancelLabel: $t('cfg.nodes.cancel')
    });
    if (!ok) return;
    try {
      await deleteGroup(group.id);
      showToast('success', $t('cfg.groups.toast_deleted'));
    } catch (err) {
      const e = err as { code?: string; params?: Record<string, string> };
      if (e.code === 'group_referenced') {
        showToast('error', routingErrorText($t, e.code, e.params));
      } else {
        handleLayerError(err);
      }
    }
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
    <!-- Строка управления есть всегда: сегмент появляется в ней без сдвига списка -->
    <div class="groups-panel">
      {#if both}
        <div class="groups-segment" data-testid="groups-kernel-segment">
          <SegmentedControl
            items={segmentItems}
            value={kernel}
            ariaLabel={$t('cfg.nodes.kernel_segment_aria')}
            onchange={(v) => chosenKernel.set(v as KernelName)}
          />
        </div>
      {/if}
      <span class="groups-count" data-testid="groups-count">
        {$tp('cfg.groups.count', groups.length)}
      </span>
    </div>
    <div class="groups-list">
      {#each groups as group (group.id)}
        <GroupCard
          {group}
          {groups}
          {kernel}
          {installed}
          expanded={$expandedGroups[group.id] === true}
          ontoggle={() => toggleGroupExpanded(group.id)}
          ondelete={removeGroup}
        />
      {/each}
    </div>
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

  .groups-segment {
    flex-shrink: 0;
    margin-right: auto;
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
  }
</style>
