<script lang="ts">
  import { onMount } from 'svelte';
  import { t, tp } from '../../../i18n';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import { formatBytes } from '../../../lib/format';
  import CronNotice from './CronNotice.svelte';
  import DownloadBlock from './DownloadBlock.svelte';
  import UpdateCard from './UpdateCard.svelte';
  import StandardSetsGroup from './StandardSetsGroup.svelte';
  import EnableSetModal, { type EnableTarget } from './EnableSetModal.svelte';
  import FinishModal, { type FinishMode } from './FinishModal.svelte';
  import {
    geodataView,
    geodataStatus,
    loadGeodata,
    type SourceView
  } from '../../../lib/configGeodata';

  // Снимок менеджера читается при открытии вкладки; дальше его перечитывают события SSE
  // geodata и draft (подписки регистрирует configGeodata.ts). Здесь каркас: загрузка, ошибка,
  // состояние XKeen, сводка и группы источников; диалоги включения набора и записи в
  // xkeen.json открываются отсюда.
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
  const standard = $derived(
    $geodataView === null ? [] : $geodataView.sources.filter((s) => s.origin === 'standard')
  );

  let enableTarget = $state<EnableTarget | null>(null);
  let finish = $state<{ mode: FinishMode; file: string } | null>(null);

  function enableStandard(s: SourceView) {
    enableTarget = {
      id: s.set_id ?? s.id,
      name: s.file,
      file: s.file,
      origin: 'standard'
    };
  }

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
    <CronNotice />
    <UpdateCard />
    <DownloadBlock />
    <p class="geo-summary" data-testid="geo-summary">{summary}</p>
    <StandardSetsGroup
      sources={standard}
      disabled={oldXkeen}
      onenable={enableStandard}
      ondedup={(s) => (finish = { mode: 'dedup', file: s.file })}
      onfinish={(s) => (finish = { mode: 'finish_include', file: s.file })}
    />
  {/if}
</div>

<EnableSetModal
  open={enableTarget !== null}
  target={enableTarget}
  onadded={() => void loadGeodata()}
  onclose={() => (enableTarget = null)}
/>

<FinishModal
  open={finish !== null}
  mode={finish?.mode ?? 'dedup'}
  file={finish?.file}
  onclose={() => (finish = null)}
/>

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
