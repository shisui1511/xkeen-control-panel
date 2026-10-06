<script lang="ts">
  import { t } from './i18n';
  import PageHeader from './PageHeader.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import DraftBar from './components/config/DraftBar.svelte';
  import ConfigNotices from './components/config/ConfigNotices.svelte';
  import ManagedFiles from './components/config/ManagedFiles.svelte';
  import KernelVersions from './components/config/KernelVersions.svelte';
  import { layerStatus, refetchLayerState } from './lib/configLayer';

  let { onSwitchTab = () => {} }: { onSwitchTab?: (tab: string) => void } = $props();
</script>

<!--
  Каркас раздела «Конфигурация» (D-02): вертикальный стек независимых секций с
  устойчивыми data-testid. Порядок: уведомления (config-notices), DraftBar
  (config-draftbar), расхождения (config-drift) и файлы (config-files) внутри
  ManagedFiles, ход применения (config-progress, план 144-13), ядра
  (config-kernels), диагностика (config-diag). Фаза 149 оборачивает секции
  во вкладки, не переписывая их.
-->
<div class="container config-page" data-testid="config-page">
  <PageHeader
    title={$t('cfg.title')}
    subtitle={$t('cfg.subtitle')}
    breadcrumbs={[{ label: $t('nav.group_overview') }, { label: $t('nav.config') }]}
    {onSwitchTab}
  />

  {#if $layerStatus === 'error'}
    <EmptyState
      title={$t('cfg.load_error_title')}
      description={$t('cfg.load_error_body')}
      ctaText={$t('cfg.load_error_retry')}
      plain
      oncta={() => void refetchLayerState()}
    />
  {:else}
    <div class="config-stack">
      <ConfigNotices />
      <DraftBar />
      <!-- config-progress (ход применения) добавляет план 144-13 перед файлами -->
      <ManagedFiles />
      <KernelVersions />
    </div>
  {/if}
</div>

<style>
  .config-stack {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
  }
</style>
