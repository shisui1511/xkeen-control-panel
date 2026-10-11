<script lang="ts">
  import { onMount } from 'svelte';
  import { t, tp } from '../../../i18n';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Button from '../../Button.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import { layerSnapshot } from '../../../lib/configLayer';
  import { sourcesView, sourcesStatus, loadSources } from '../../../lib/configSources';
  import type { KernelName } from '../../../lib/configSources';
  import AddSourceField from './AddSourceField.svelte';
  import SourceCard from './SourceCard.svelte';

  // Источники читаются при открытии вкладки; дальше представление перечитывает
  // событие SSE sources (обработчик регистрирует configSources.ts)
  onMount(() => {
    void loadSources();
  });

  const loading = $derived(
    $layerSnapshot === null || $sourcesStatus === 'idle' || $sourcesStatus === 'loading'
  );
  const failed = $derived(!loading && $sourcesStatus === 'error' && $sourcesView === null);
  const empty = $derived(!loading && !failed && ($sourcesView?.sources.length ?? 0) === 0);

  // Раскрытие карточек живёт здесь, а не в карточке: смена вкладки и перерисовка списка
  // его не сбрасывают
  let expandedIds = $state<Record<string, boolean>>({});
  // Явный выбор ядра в сегменте; пока выбора нет, берётся ядро по умолчанию
  let chosen = $state<KernelName | null>(null);

  const installed = $derived($sourcesView?.installed ?? { xray: false, mihomo: false });
  const both = $derived(installed.xray && installed.mihomo);
  const kernel = $derived.by<KernelName>(() => {
    if (chosen !== null && installed[chosen]) return chosen;
    const def = $sourcesView?.default_kernel;
    if ((def === 'xray' || def === 'mihomo') && installed[def]) return def;
    return installed.mihomo && !installed.xray ? 'mihomo' : 'xray';
  });
  const segmentItems = [
    { value: 'xray', label: 'Xray' },
    { value: 'mihomo', label: 'Mihomo' }
  ];
  const showList = $derived(!loading && !failed && !empty && $sourcesView !== null);

  function focusAddField() {
    document.getElementById('nodes-add-input')?.focus();
  }
</script>

<div class="nodes-tab" data-testid="nodes-tab">
  <AddSourceField />
  {#if loading}
    <div class="nodes-skeleton" aria-hidden="true">
      <Skeleton type="card" height="120px" />
      <Skeleton type="card" height="120px" />
    </div>
  {:else if failed}
    <div class="alert alert-error nodes-error" role="alert" data-testid="nodes-load-error">
      <span class="nodes-error-text">{$t('cfg.nodes.load_error')}</span>
      <Button variant="secondary" onclick={() => void loadSources()}>{$t('cfg.nodes.retry')}</Button
      >
    </div>
  {:else if empty}
    <EmptyState
      title={$t('cfg.nodes.empty_title')}
      description={$t('cfg.nodes.empty_body')}
      ctaText={$t('cfg.nodes.empty_cta')}
      plain
      oncta={focusAddField}
    />
  {:else if showList && $sourcesView}
    <!-- Строка счётчика есть всегда: сегмент появляется в ней без сдвига списка -->
    <div class="nodes-panel">
      {#if both}
        <div class="nodes-segment" data-testid="nodes-kernel-segment">
          <SegmentedControl
            items={segmentItems}
            value={kernel}
            ariaLabel={$t('cfg.nodes.kernel_segment_aria')}
            onchange={(v) => (chosen = v as KernelName)}
          />
        </div>
      {/if}
      <span class="nodes-count" data-testid="nodes-sources-count">
        {$tp('cfg.nodes.sources_count', $sourcesView.sources.length)}
      </span>
    </div>
    <div class="nodes-list">
      {#each $sourcesView.sources as source (source.id)}
        <SourceCard
          {source}
          {kernel}
          {installed}
          expanded={expandedIds[source.id] === true}
          ontoggle={() => (expandedIds[source.id] = !expandedIds[source.id])}
        />
      {/each}
    </div>
  {/if}
</div>

<style>
  .nodes-tab {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }

  .nodes-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
  }

  .nodes-panel {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px 16px;
    min-height: var(--btn-h, 38px);
  }

  .nodes-segment {
    flex-shrink: 0;
  }

  .nodes-count {
    margin-left: auto;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .nodes-list {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }

  .nodes-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 12px;
    margin: 0;
  }

  .nodes-error-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
