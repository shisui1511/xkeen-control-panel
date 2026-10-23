<script lang="ts">
  import { onMount } from 'svelte';
  import { t, tp } from '../../../i18n';
  import { showConfirm, showToast, activeKernelState } from '../../../stores';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Button from '../../Button.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import { handleLayerError, layerSnapshot } from '../../../lib/configLayer';
  import { configSubTabHash } from '../../../lib/configSubTab';
  import { loadSources, type KernelName } from '../../../lib/configSources';
  import {
    routingView,
    routingStatus,
    loadRouting,
    deleteGroup,
    fetchImpact,
    type GroupView,
    type Impact
  } from '../../../lib/configRouting';
  import { kernelNameOf } from '../../../lib/kernelState';
  import { chosenKernel } from '../../../lib/nodesView';
  import { expandedGroups, toggleGroupExpanded } from '../../../lib/routingView';
  import GroupCard from './GroupCard.svelte';
  import GroupEditModal from './GroupEditModal.svelte';
  import GroupDeleteModal from './GroupDeleteModal.svelte';

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

  // Одна копия диалога на вкладку: null — новая группа
  let modalOpen = $state(false);
  let editing = $state<GroupView | null>(null);

  function openNew() {
    editing = null;
    modalOpen = true;
  }

  function openEdit(group: GroupView) {
    editing = group;
    modalOpen = true;
  }

  function gotoNodes() {
    window.location.hash = configSubTabHash('nodes');
  }

  // Удаление группы (D-34): ссылки и последствия по ядрам приходят от сервера (Impact).
  // Ссылок нет — простое подтверждение; есть — диалог с выбором действия.
  let deleteOpen = $state(false);
  let deleting = $state<GroupView | null>(null);
  let deleteImpact = $state<Impact | null>(null);
  let removing = $state(false);

  function openDelete(group: GroupView, impact: Impact) {
    deleting = group;
    deleteImpact = impact;
    deleteOpen = true;
  }

  async function removeGroup(group: GroupView) {
    if (removing) return;
    removing = true;
    try {
      const impact = await fetchImpact('group', group.id);
      if (impact.rules.length > 0 || impact.groups.length > 0 || impact.final) {
        openDelete(group, impact);
        return;
      }
      const ok = await showConfirm({
        variant: 'danger',
        title: $t('cfg.groups.del_title', { name: group.name }),
        message: $t('cfg.groups.del_simple_body'),
        confirmLabel: $t('cfg.groups.del_confirm'),
        cancelLabel: $t('cfg.nodes.cancel')
      });
      if (!ok) return;
      try {
        await deleteGroup(group.id, { mode: '' });
        showToast('success', $t('cfg.groups.toast_deleted'));
      } catch (err) {
        // Ссылки появились между запросами: сервер вернул их вместе с ответом
        const e = err as { code?: string; data?: { impact?: Impact } };
        if (e.code === 'group_referenced' && e.data?.impact) {
          openDelete(group, e.data.impact);
        } else {
          handleLayerError(err);
        }
      }
    } catch (err) {
      handleLayerError(err);
    } finally {
      removing = false;
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
    <div class="groups-panel">
      <Button variant="primary" data-testid="groups-add" onclick={openNew}>
        {$t('cfg.groups.add')}
      </Button>
    </div>
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
      ctaText={$t('cfg.groups.empty_cta')}
      plain
      oncta={openNew}
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
      <Button variant="primary" data-testid="groups-add" onclick={openNew}>
        {$t('cfg.groups.add')}
      </Button>
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
          onedit={openEdit}
          ondelete={removeGroup}
        />
      {/each}
    </div>
  {/if}
</div>

<GroupEditModal
  open={modalOpen}
  group={editing}
  {groups}
  features={$routingView?.features ?? {}}
  onclose={() => (modalOpen = false)}
/>

<GroupDeleteModal
  open={deleteOpen}
  group={deleting}
  impact={deleteImpact}
  {groups}
  onclose={() => (deleteOpen = false)}
/>

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
