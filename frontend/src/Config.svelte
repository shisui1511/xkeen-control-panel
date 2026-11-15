<script lang="ts">
  import { onMount } from 'svelte';
  import { get } from 'svelte/store';
  import { t } from './i18n';
  import PageHeader from './PageHeader.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import Skeleton from './components/Skeleton.svelte';
  import Icon from './lib/components/Icon.svelte';
  import Tabs from './components/Tabs.svelte';
  import DraftBar from './components/config/DraftBar.svelte';
  import ConfigNotices from './components/config/ConfigNotices.svelte';
  import ApplyProgress from './components/config/ApplyProgress.svelte';
  import NodesTab from './components/config/nodes/NodesTab.svelte';
  import GroupsTab from './components/config/groups/GroupsTab.svelte';
  import RulesTab from './components/config/rules/RulesTab.svelte';
  import ListsTab from './components/config/lists/ListsTab.svelte';
  import SectionsTab from './components/config/sections/SectionsTab.svelte';
  import FilesTab from './components/config/files/FilesTab.svelte';
  import RoutingOwnerNotice from './components/config/routing/RoutingOwnerNotice.svelte';
  import SetupWizard from './components/config/setup/SetupWizard.svelte';
  import { watchOwnerToasts } from './components/config/routing/routingOwner';
  import { layerStatus, refetchLayerState } from './lib/configLayer';
  import { startWizard, stopWizard, wizardLoadError, wizardView } from './lib/configWizard';
  import { capabilities } from './stores';
  import {
    asConfigSubTab,
    configSubTab,
    configSubTabHash,
    isConfigSetupHash,
    type ConfigSubTab
  } from './lib/configSubTab';

  let { onSwitchTab = () => {} }: { onSwitchTab?: (tab: string) => void } = $props();

  // Вкладка раздела берётся из хэша (#/config/<вкладка>); состояние страницы,
  // черновик и полоса DraftBar при смене вкладки не пересоздаются
  let subTab = $state<ConfigSubTab>(configSubTab(window.location.hash));

  // «Файлы» — встроенный Редактор: после первого открытия вкладка остаётся смонтированной
  // и только скрывается, поэтому открытые файлы и несохранённые правки переживают переход
  // между вкладками раздела (T-149-22)
  let filesVisited = $state(configSubTab(window.location.hash) === 'files');

  // Мастер первого запуска (#/config/setup) — отдельная страница раздела, не вкладка: на
  // ней нет вкладок, уведомлений, полосы черновика и внешнего хода применения (D-03).
  // Хэш мастера не переписывается в хэш вкладки
  let setup = $state(isConfigSetupHash(window.location.hash));

  // Автооткрытие мастера (149 D-22, D-12): только при входе из меню на адрес ровно
  // #/config, один раз за монтирование раздела. Пока ответа сервера нет, вместо вкладки —
  // скелетон («Узлы» не мигают); явные адреса вкладок и другие страницы не перехватываются
  let autoPending = $state(window.location.hash === '#/config');

  // DraftBar смонтирован всегда: на странице мастера он «голый» (без полосы), а шаг 5 зовёт
  // его apply() — тот же путь через перехват и окно изменений, что у кнопки полосы
  let draftBar = $state<ReturnType<typeof DraftBar>>();

  function syncSubTab() {
    if (window.location.hash !== '#/config') autoPending = false;
    setup = isConfigSetupHash(window.location.hash);
    subTab = configSubTab(window.location.hash);
    if (subTab === 'files') filesVisited = true;
  }

  // Подпись вкладки ядер (D-15): установлено одно ядро — только его имя (имена ядер не
  // переводятся); оба, ни одного или capabilities ещё не пришли — «Xray │ Mihomo»
  const coresLabel = $derived.by(() => {
    const xray = $capabilities?.kernels?.xray?.installed === true;
    const mihomo = $capabilities?.kernels?.mihomo?.installed === true;
    if (xray && !mihomo) return 'Xray';
    if (mihomo && !xray) return 'Mihomo';
    return $t('cfg.tab.cores');
  });

  const tabItems = $derived([
    { value: 'nodes', label: $t('cfg.tab.nodes'), testId: 'config-tab-nodes' },
    { value: 'groups', label: $t('cfg.tab.groups'), testId: 'config-tab-groups' },
    { value: 'rules', label: $t('cfg.tab.rules'), testId: 'config-tab-rules' },
    { value: 'lists', label: $t('cfg.tab.lists'), testId: 'config-tab-lists' },
    { value: 'cores', label: coresLabel, testId: 'config-tab-cores' },
    { value: 'files', label: $t('cfg.tab.files'), testId: 'config-tab-files' }
  ]);

  // Тосты «Маршрутизацию взяла панель.» / «Маршрутизацию вернули XKeen.» по итогу применения
  // не зависят от открытой вкладки
  onMount(() => watchOwnerToasts());

  // Решение об автооткрытии: состояние слоя загружено и пришёл статус мастера. Признак
  // auto_open сервер даёт только при пустом состоянии и незакрытом, непройденном мастере;
  // переход без записи в историю — «Назад» не возвращает в петлю
  function decideAutoOpen() {
    if (!autoPending) return;
    if (window.location.hash !== '#/config') {
      autoPending = false;
      return;
    }
    if (get(layerStatus) === 'error' || get(wizardLoadError)) {
      autoPending = false;
      return;
    }
    const view = get(wizardView);
    if (get(layerStatus) !== 'ready' || view === null) return;
    autoPending = false;
    if (view.auto_open) window.location.replace('#/config/setup');
  }

  onMount(() => {
    startWizard();
    const offs = [
      layerStatus.subscribe(decideAutoOpen),
      wizardView.subscribe(decideAutoOpen),
      wizardLoadError.subscribe(decideAutoOpen)
    ];
    return () => {
      for (const off of offs) off();
      stopWizard();
    };
  });

  function selectTab(value: string) {
    const next = asConfigSubTab(value);
    if (next === 'files') filesVisited = true;
    window.location.hash = configSubTabHash(next);
  }
</script>

<svelte:window onhashchange={syncSubTab} />

<!--
  Каркас раздела «Конфигурация» (D-02, D-15): уведомления (config-notices), DraftBar
  (config-draftbar) и ход применения (config-progress) общие и стоят над вкладками;
  вкладки «Узлы» (nodes-tab), «Группы» (groups-tab), «Правила» (rules-tab), «Списки»
  (lists-tab, менеджер geodata XKeen), «Xray │ Mihomo» (cores-tab: секции ядер и версии
  ядер config-kernels) и «Файлы» (files-tab: встроенный Редактор, D-01). «Файлы» после
  первого открытия остаётся смонтированной и скрывается. Диагностика слоя (config-diag) —
  в «Настройки → для разработчиков».
-->
<div class="container config-page" data-testid="config-page">
  {#if !setup}
    <PageHeader title={$t('cfg.title')} subtitle={$t('cfg.subtitle')}>
      {#snippet actions()}
        <!-- Постоянный вход в мастер (D-13): неприметная ссылка, не кнопка -->
        <a
          href="#/config/setup"
          class="setup-link"
          data-testid="config-setup-link"
          aria-label={$t('cfg.setup.link')}
        >
          {$t('cfg.setup.link')}
          <Icon name="chevron-right" size={12} />
        </a>
      {/snippet}
    </PageHeader>
  {/if}

  <div class="config-stack">
    <!-- Порядок как до мастера: уведомления, полоса черновика, ход применения. DraftBar
         смонтирован всегда — мастеру нужен его apply() -->
    {#if !setup}
      <ConfigNotices />
    {/if}
    <DraftBar bind:this={draftBar} bare={setup} />
    {#if !setup}
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
    {/if}

    {#if setup}
      <SetupWizard onapply={() => draftBar?.apply()} />
    {:else if $layerStatus === 'error'}
      <EmptyState
        title={$t('cfg.load_error_title')}
        description={$t('cfg.load_error_body')}
        ctaText={$t('cfg.load_error_retry')}
        plain
        oncta={() => void refetchLayerState()}
      />
    {:else if autoPending}
      <Skeleton type="card" height="40vh" />
    {:else if subTab === 'nodes'}
      <NodesTab />
    {:else if subTab === 'groups'}
      <RoutingOwnerNotice tab="groups" />
      <GroupsTab />
    {:else if subTab === 'rules'}
      <RoutingOwnerNotice tab="rules" />
      <RulesTab />
    {:else if subTab === 'lists'}
      <ListsTab />
    {:else if subTab === 'cores'}
      <SectionsTab />
    {/if}

    {#if filesVisited}
      <FilesTab active={subTab === 'files' && !setup && $layerStatus !== 'error'} />
    {/if}
  </div>
</div>

<style>
  .config-stack {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap);
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

  .setup-link {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  .setup-link:hover {
    text-decoration: underline;
  }

  .setup-link:focus-visible {
    outline: none;
    border-radius: var(--radius-sm);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  @media (max-width: 768px) {
    .setup-link {
      min-height: 44px;
    }
  }
</style>
