<script lang="ts">
  import { onMount } from 'svelte';
  import { t, tp } from '../../../i18n';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import { formatBytes } from '../../../lib/format';
  import { showConfirm, showToast } from '../../../stores';
  import { dropSetName } from './setName';
  import CronNotice from './CronNotice.svelte';
  import DownloadBlock from './DownloadBlock.svelte';
  import UpdateCard from './UpdateCard.svelte';
  import StandardSetsGroup from './StandardSetsGroup.svelte';
  import SourceGroup from './SourceGroup.svelte';
  import EnableSetModal, { type EnableTarget } from './EnableSetModal.svelte';
  import AddSourceModal from './AddSourceModal.svelte';
  import EditUrlModal from './EditUrlModal.svelte';
  import FinishModal, { type FinishMode } from './FinishModal.svelte';
  import DeleteSourceModal from './DeleteSourceModal.svelte';
  import {
    geodataView,
    geodataStatus,
    loadGeodata,
    fetchImpact,
    dropSource,
    restoreSource,
    geodataErrorText,
    type ImpactView,
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

  const panel = $derived(
    $geodataView === null ? [] : $geodataView.sources.filter((s) => s.origin === 'panel')
  );
  // Свои: все остальные записи geodata (в том числе вписанные вручную) и dat без записи
  const own = $derived(
    $geodataView === null
      ? []
      : $geodataView.sources.filter((s) => s.origin === 'own' || s.origin === 'file')
  );

  let enableTarget = $state<EnableTarget | null>(null);
  let finish = $state<{ mode: FinishMode; file: string } | null>(null);
  let addOpen = $state(false);
  let editSource = $state<SourceView | null>(null);
  // Диалог каскада: источник, у которого нашлись ссылки
  let cascade = $state<{
    file: string;
    mode: 'delete' | 'off';
    name: string;
    aliases: string[];
    impact: ImpactView;
  } | null>(null);

  function enableStandard(s: SourceView) {
    enableTarget = {
      id: s.set_id ?? s.id,
      name: s.file,
      file: s.file,
      origin: 'standard'
    };
  }

  function askDedup(s: SourceView) {
    finish = { mode: 'dedup', file: s.file };
  }

  function askFinish(s: SourceView) {
    finish = { mode: 'finish_include', file: s.file };
  }

  function askFinishDelete(s: SourceView) {
    finish = { mode: 'finish_delete', file: s.file };
  }

  // «Довести» у строки: удаление, если оно не завершено, иначе включение
  function onFinish(s: SourceView) {
    if (s.delete_incomplete !== undefined && s.delete_incomplete !== '') askFinishDelete(s);
    else askFinish(s);
  }

  function dropFailure(err: unknown) {
    const code = (err as { code?: string } | null)?.code;
    // Черновик изменили в другой вкладке: показывается свежий снимок
    if (code === 'draft_conflict') void loadGeodata();
    showToast('error', geodataErrorText(err));
  }

  let dropBusy = false;

  // Удаление или выключение источника: ссылки ищет сервер; без ссылок — подтверждение и
  // пометка в черновике, иначе диалог каскада. Файл и запись геоданных уходят только после
  // «Применить».
  async function askDrop(s: SourceView, mode: 'delete' | 'off') {
    if (dropBusy) return;
    dropBusy = true;
    try {
      const impact = await fetchImpact(s.file);
      const name = dropSetName(s);
      const clean =
        impact.lists.length === 0 && impact.raw.length === 0 && impact.foreign.length === 0;
      if (!clean) {
        cascade = { file: s.file, mode, name, aliases: s.aliases ?? [], impact };
        return;
      }
      const link = (s.aliases ?? []).filter((a) => a !== s.file).join(', ');
      const ok = await showConfirm({
        variant: 'danger',
        title:
          mode === 'delete'
            ? $t('cfg.geo.del_title', { file: s.file })
            : $t('cfg.geo.off_title', { name }),
        message: $t('cfg.geo.del_nolinks'),
        consequence: $t('cfg.geo.del_effects', {
          symlink: link === '' ? '' : $t('cfg.geo.del_symlink', { link })
        }),
        confirmLabel: $t(mode === 'delete' ? 'cfg.geo.del_submit' : 'cfg.geo.off_submit')
      });
      if (!ok) return;
      await dropSource(s.file, mode, []);
      showToast('success', $t('cfg.geo.del_toast'));
    } catch (err) {
      dropFailure(err);
    } finally {
      dropBusy = false;
    }
  }

  async function restore(s: SourceView) {
    try {
      await restoreSource(s.file);
    } catch (err) {
      dropFailure(err);
    }
  }

  function enablePanel(s: SourceView) {
    enableTarget = { id: s.set_id ?? s.id, name: s.file, file: s.file, origin: 'panel' };
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
      onoff={(s) => void askDrop(s, 'off')}
      ondedup={askDedup}
      onfinish={onFinish}
      onrestore={(s) => void restore(s)}
    />
    <SourceGroup
      kind="panel"
      sources={panel}
      disabled={oldXkeen}
      onenable={enablePanel}
      onoff={(s) => void askDrop(s, 'off')}
      ondedup={askDedup}
      onfinish={onFinish}
      onrestore={(s) => void restore(s)}
    />
    <SourceGroup
      kind="own"
      sources={own}
      disabled={oldXkeen}
      onadd={() => (addOpen = true)}
      onedit={(s) => (editSource = s)}
      ondelete={(s) => void askDrop(s, 'delete')}
      onrestore={(s) => void restore(s)}
      ondedup={askDedup}
      onfinish={onFinish}
    />
  {/if}
</div>

<EnableSetModal
  open={enableTarget !== null}
  target={enableTarget}
  onadded={() => void loadGeodata()}
  onclose={() => (enableTarget = null)}
/>

<AddSourceModal
  open={addOpen}
  onadded={() => void loadGeodata()}
  onclose={() => (addOpen = false)}
/>

<EditUrlModal
  open={editSource !== null}
  source={editSource}
  onsaved={() => void loadGeodata()}
  onclose={() => (editSource = null)}
/>

<DeleteSourceModal
  open={cascade !== null}
  file={cascade?.file ?? ''}
  mode={cascade?.mode ?? 'delete'}
  name={cascade?.name ?? ''}
  aliases={cascade?.aliases ?? []}
  impact={cascade?.impact ?? null}
  onclose={() => (cascade = null)}
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
