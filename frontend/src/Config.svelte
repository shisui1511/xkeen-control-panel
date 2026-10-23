<script lang="ts">
  import { t } from './i18n';
  import PageHeader from './PageHeader.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import Tabs from './components/Tabs.svelte';
  import DraftBar from './components/config/DraftBar.svelte';
  import ConfigNotices from './components/config/ConfigNotices.svelte';
  import ApplyProgress from './components/config/ApplyProgress.svelte';
  import DevDiagnostics from './components/config/DevDiagnostics.svelte';
  import ManagedFiles from './components/config/ManagedFiles.svelte';
  import KernelVersions from './components/config/KernelVersions.svelte';
  import NodesTab from './components/config/nodes/NodesTab.svelte';
  import GroupsTab from './components/config/groups/GroupsTab.svelte';
  import RulesTab from './components/config/rules/RulesTab.svelte';
  import { layerStatus, refetchLayerState } from './lib/configLayer';
  import {
    asConfigSubTab,
    configSubTab,
    configSubTabHash,
    type ConfigSubTab
  } from './lib/configSubTab';

  let { onSwitchTab = () => {} }: { onSwitchTab?: (tab: string) => void } = $props();

  // Вкладка раздела берётся из хэша (#/config/<вкладка>); состояние страницы,
  // черновик и полоса DraftBar при смене вкладки не пересоздаются
  let subTab = $state<ConfigSubTab>(configSubTab(window.location.hash));

  function syncSubTab() {
    subTab = configSubTab(window.location.hash);
  }

  const tabItems = $derived([
    { value: 'nodes', label: $t('cfg.tab.nodes'), testId: 'config-tab-nodes' },
    { value: 'groups', label: $t('cfg.tab.groups'), testId: 'config-tab-groups' },
    { value: 'rules', label: $t('cfg.tab.rules'), testId: 'config-tab-rules' },
    { value: 'files', label: $t('cfg.tab.files'), testId: 'config-tab-files' }
  ]);

  function selectTab(value: string) {
    window.location.hash = configSubTabHash(asConfigSubTab(value));
  }
</script>

<svelte:window onhashchange={syncSubTab} />

<!--
  Каркас раздела «Конфигурация» (D-02, D-15): уведомления (config-notices), DraftBar
  (config-draftbar) и ход применения (config-progress) общие и стоят над вкладками;
  вкладки «Узлы» (nodes-tab), «Группы» (groups-tab), «Правила» (rules-tab) и «Файлы панели» — нынешний стек: расхождения
  (config-drift) и файлы (config-files) внутри ManagedFiles, ядра (config-kernels),
  диагностика (config-diag). Фазы 146–148 добавляют вкладки в тот же Tabs.
-->
<div class="container config-page" data-testid="config-page">
  <PageHeader
    title={$t('cfg.title')}
    subtitle={$t('cfg.subtitle')}
    breadcrumbs={[{ label: $t('nav.group_overview') }, { label: $t('nav.config') }]}
    {onSwitchTab}
  />

  <div class="config-stack">
    <ConfigNotices />
    <DraftBar />
    <ApplyProgress />

    <div class="config-tabs" data-testid="config-tabs">
      <Tabs
        items={tabItems}
        bind:value={subTab}
        variant="line"
        ariaLabel={$t('cfg.tabs_aria')}
        onchange={selectTab}
      />
    </div>

    {#if $layerStatus === 'error'}
      <EmptyState
        title={$t('cfg.load_error_title')}
        description={$t('cfg.load_error_body')}
        ctaText={$t('cfg.load_error_retry')}
        plain
        oncta={() => void refetchLayerState()}
      />
    {:else if subTab === 'nodes'}
      <NodesTab />
    {:else if subTab === 'groups'}
      <GroupsTab />
    {:else if subTab === 'rules'}
      <RulesTab />
    {:else if subTab === 'files'}
      <ManagedFiles />
      <KernelVersions />
      <DevDiagnostics />
    {/if}
  </div>
</div>

<style>
  .config-stack {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
  }

  /* Полоса вкладок прокручивается по горизонтали внутри себя (скроллбар скрыт в Tabs):
     страница не получает горизонтальной прокрутки */
  .config-tabs {
    min-width: 0;
    overflow-x: auto;
    scrollbar-width: none;
    white-space: nowrap;
  }

  .config-tabs::-webkit-scrollbar {
    display: none;
  }
</style>
