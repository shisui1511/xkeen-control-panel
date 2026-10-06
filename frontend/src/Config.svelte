<script lang="ts">
  import { t } from './i18n';
  import PageHeader from './PageHeader.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import DraftBar from './components/config/DraftBar.svelte';
  import { layerStatus, refetchLayerState } from './lib/configLayer';

  let { onSwitchTab = () => {} }: { onSwitchTab?: (tab: string) => void } = $props();
</script>

<!--
  Каркас раздела «Конфигурация» (D-02): вертикальный стек независимых секций с
  устойчивыми data-testid. Порядок: уведомления, DraftBar, расхождения
  (config-drift), ход применения (config-progress), файлы (config-files),
  ядра (config-kernels), диагностика (config-diag). Фаза 149 оборачивает секции
  во вкладки, не переписывая их.
-->
<div class="config-page" data-testid="config-page">
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
      <section class="config-notices" data-testid="config-notices"></section>
      <DraftBar />
    </div>
  {/if}
</div>

<style>
  .config-stack {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
  }

  .config-notices:empty {
    display: none;
  }
</style>
