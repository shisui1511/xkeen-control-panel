<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../../i18n';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Button from '../../Button.svelte';
  import { layerSnapshot } from '../../../lib/configLayer';
  import { sourcesView, sourcesStatus, loadSources } from '../../../lib/configSources';

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

  // Поле «Добавить» появляется в плане 145-13; до тех пор действие ничего не делает
  function focusAddField() {
    document.getElementById('nodes-add-input')?.focus();
  }
</script>

<div class="nodes-tab" data-testid="nodes-tab">
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
