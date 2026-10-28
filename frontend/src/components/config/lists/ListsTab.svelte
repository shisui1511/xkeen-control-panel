<script lang="ts">
  import { onMount } from 'svelte';
  import { t, tp } from '../../../i18n';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import { formatBytes } from '../../../lib/format';
  import UpdateCard from './UpdateCard.svelte';
  import { geodataView, geodataStatus, loadGeodata } from '../../../lib/configGeodata';

  // Снимок менеджера читается при открытии вкладки; дальше его перечитывают события SSE
  // geodata и draft (подписки регистрирует configGeodata.ts). Карточки вкладки добавляют
  // следующие планы фазы, здесь каркас: загрузка, ошибка, состояние XKeen и сводка.
  onMount(() => {
    void loadGeodata();
  });

  const loading = $derived(
    $geodataView === null && ($geodataStatus === 'idle' || $geodataStatus === 'loading')
  );
  const failed = $derived($geodataView === null && $geodataStatus === 'error');
  const noXkeen = $derived($geodataView !== null && !$geodataView.xkeen.installed);
  const oldXkeen = $derived(
    $geodataView !== null && $geodataView.xkeen.installed && !$geodataView.xkeen.user_geodata
  );
  const summary = $derived(
    $geodataView === null
      ? ''
      : $tp('cfg.geo.summary', $geodataView.files, { size: formatBytes($geodataView.total_size) })
  );
</script>

<div class="config-stack" data-testid="lists-tab">
  {#if loading}
    <div class="lists-skeleton">
      <Skeleton type="card" height="120px" />
      <Skeleton type="card" height="120px" />
      <Skeleton type="card" height="120px" />
    </div>
  {:else if failed}
    <EmptyState
      title={$t('cfg.load_error_title')}
      description={$t('cfg.load_error_body')}
      ctaText={$t('cfg.load_error_retry')}
      plain
      oncta={() => void loadGeodata()}
    />
  {:else if noXkeen}
    <EmptyState
      title={$t('cfg.geo.no_xkeen_title')}
      description={$t('cfg.geo.no_xkeen_body')}
      plain
    />
  {:else if $geodataView !== null}
    {#if oldXkeen}
      <div class="alert alert-warning lists-alert" role="status" data-testid="geo-old-xkeen">
        <span class="lists-alert-text">{$t('cfg.geo.old_xkeen')}</span>
      </div>
    {/if}
    <UpdateCard />
    <p class="geo-summary" data-testid="geo-summary">{summary}</p>
  {/if}
</div>

<style>
  .config-stack {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }

  .lists-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
  }

  .lists-alert {
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .lists-alert-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .geo-summary {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }
</style>
