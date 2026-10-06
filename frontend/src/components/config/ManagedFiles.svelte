<script lang="ts">
  import { t } from '../../i18n';
  import { showConfirm } from '../../stores';
  import StatusBadge from '../StatusBadge.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import DiffView from './DiffView.svelte';
  import {
    layerSnapshot,
    driftCount,
    applyRunning,
    rebuildFiles,
    releaseFile,
    fetchDiff,
    handleLayerError,
    type FileState,
    type LayerFile,
    type LayerDiff
  } from '../../lib/configLayer';

  type BadgeVariant = 'running' | 'stopped' | 'warning' | 'idle' | 'info';

  // Единый словарь состояний (UI-SPEC): бейдж по состоянию файла
  const BADGE: Record<FileState, BadgeVariant> = {
    ok: 'running',
    pending: 'info',
    drift_modified: 'warning',
    drift_missing: 'warning',
    drift_renamed: 'warning',
    drift_empty: 'warning',
    released: 'idle'
  };

  // Порядок групп: дрейф → ожидает → совпадает → отпущен
  const RANK: Record<FileState, number> = {
    drift_modified: 0,
    drift_missing: 0,
    drift_renamed: 0,
    drift_empty: 0,
    pending: 1,
    ok: 2,
    released: 3
  };

  const isDrift = (f: LayerFile) => f.state.startsWith('drift_');

  const files = $derived(
    [...($layerSnapshot?.files ?? [])].sort(
      (a, b) => RANK[a.state] - RANK[b.state] || a.path.localeCompare(b.path)
    )
  );

  type DiffState = { status: 'loading' | 'ready'; diff?: LayerDiff };
  let opened = $state<Record<string, boolean>>({});
  let diffs = $state<Record<string, DiffState>>({});
  // Идёт запрос действия: повторные клики не нужны
  let acting = $state(false);

  const locked = $derived($applyRunning || acting);

  const diffId = (key: string) => `config-diff-${key.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
  const baseName = (path: string) => path.split('/').pop() ?? path;

  function detail(f: LayerFile): string {
    if (f.state !== 'drift_renamed') return '';
    const name = baseName(f.path);
    return $t('cfg.renamed_detail', { obsolete: f.obsolete_name ?? `${name}.obsolete`, name });
  }

  async function toggleDiff(key: string) {
    if (opened[key]) {
      opened[key] = false;
      return;
    }
    opened[key] = true;
    diffs[key] = { status: 'loading' };
    try {
      diffs[key] = { status: 'ready', diff: await fetchDiff(key) };
    } catch (err) {
      opened[key] = false;
      delete diffs[key];
      handleLayerError(err);
    }
  }

  async function run(action: () => Promise<void>) {
    acting = true;
    try {
      await action();
    } catch (err) {
      handleLayerError(err);
    } finally {
      acting = false;
    }
  }

  async function onRebuild(f: LayerFile) {
    const confirmed = await showConfirm({
      variant: 'warning',
      title: $t('cfg.confirm.rebuild_title'),
      objectName: f.path,
      message: $t('cfg.confirm.rebuild_message'),
      consequence: $t('cfg.confirm.rebuild_consequence'),
      confirmLabel: $t('cfg.confirm.rebuild_ok')
    });
    if (!confirmed) return;
    await run(() => rebuildFiles([f.key]));
  }

  async function onRelease(f: LayerFile) {
    const confirmed = await showConfirm({
      variant: 'warning',
      title: $t('cfg.confirm.release_title'),
      objectName: f.path,
      message: $t('cfg.confirm.release_message'),
      consequence: $t('cfg.confirm.release_consequence'),
      confirmLabel: $t('cfg.confirm.release_ok')
    });
    if (!confirmed) return;
    await run(() => releaseFile(f.key));
  }
</script>

<section class="files-section" data-testid="config-files">
  {#if $driftCount > 0}
    <div class="alert alert-warning drift-alert" role="alert" data-testid="config-drift">
      <Icon name="warning" size={16} />
      <div class="drift-text">
        <strong>{$t('cfg.drift.title')}</strong>
        <p>{$t('cfg.drift.body')}</p>
      </div>
    </div>
  {/if}

  <div class="card files-card">
    <h2 class="card-title">
      <span>{$t('cfg.files_title')}</span>
    </h2>

    <ul class="file-list" role="list">
      {#each files as f (f.key)}
        {@const drift = isDrift(f)}
        {@const note = detail(f)}
        <li class="file-row" class:file-row-drift={drift} data-state={f.state}>
          <div class="file-name">
            <span class="file-path" title={f.path}>{f.path}</span>
            {#if note}
              <span class="file-detail">{note}</span>
            {/if}
          </div>
          <div class="file-meta">
            <span class="badge">{f.kernel}</span>
            <span class="file-owner">{$t(`cfg.owner.${f.owner}`)}</span>
            <StatusBadge variant={BADGE[f.state]} label={$t(`cfg.state.${f.state}`)} />
          </div>
          {#if drift || f.state === 'released'}
            <div class="file-actions">
              {#if drift}
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  aria-expanded={opened[f.key] ? 'true' : 'false'}
                  aria-controls={diffId(f.key)}
                  aria-label={`${opened[f.key] ? $t('cfg.action.hide_diff') : $t('cfg.action.show_diff')}: ${f.path}`}
                  onclick={() => void toggleDiff(f.key)}
                >
                  <Icon name={opened[f.key] ? 'eye-off' : 'eye'} size={13} />
                  {opened[f.key] ? $t('cfg.action.hide_diff') : $t('cfg.action.show_diff')}
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  disabled={locked}
                  aria-label={`${$t('cfg.action.rebuild')}: ${f.path}`}
                  onclick={() => void onRebuild(f)}>{$t('cfg.action.rebuild')}</button
                >
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  disabled={locked}
                  aria-label={`${$t('cfg.action.release')}: ${f.path}`}
                  onclick={() => void onRelease(f)}>{$t('cfg.action.release')}</button
                >
              {:else}
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  disabled={locked}
                  aria-label={`${$t('cfg.action.return')}: ${f.path}`}
                  onclick={() => void onRebuild(f)}>{$t('cfg.action.return')}</button
                >
              {/if}
            </div>
          {/if}
          {#if drift && opened[f.key]}
            <div class="file-diff">
              {#if diffs[f.key]?.diff}
                {@const d = diffs[f.key].diff!}
                <DiffView
                  id={diffId(f.key)}
                  expected={d.expected}
                  actual={d.actual}
                  missing={d.missing}
                  truncated={d.truncated}
                />
              {:else}
                <div class="diff-loading" id={diffId(f.key)}>
                  <span class="spinner" aria-hidden="true"></span>
                </div>
              {/if}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  </div>
</section>

<style>
  .files-section {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }

  .drift-alert {
    margin: 0;
    align-items: flex-start;
  }

  .drift-text {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .drift-text p {
    margin: 0;
  }

  .files-card {
    overflow: hidden;
    min-width: 0;
  }

  /* Строки идут от края до края карточки: сетка общая для всех строк (subgrid) */
  .file-list {
    list-style: none;
    margin: 0 calc(-1 * var(--card-pad)) calc(-1 * var(--card-pad));
    padding: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
  }

  .file-row {
    grid-column: 1 / -1;
    display: grid;
    grid-template-columns: subgrid;
    align-items: center;
    column-gap: 12px;
    row-gap: 8px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-light);
    font-size: var(--font-size-sm);
    line-height: 1.5;
    min-width: 0;
  }

  .file-row:last-child {
    border-bottom: 0;
  }

  .file-row-drift {
    border-left: 3px solid var(--warning);
    background: color-mix(in srgb, var(--warning) 6%, transparent);
    padding-left: 13px;
  }

  .file-name {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .file-path {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    line-height: 1.4;
    color: var(--fg-primary);
    word-break: break-all;
  }

  .file-detail {
    font-size: var(--font-size-xs);
    line-height: 1.4;
    color: var(--fg-dim);
    word-break: break-all;
  }

  .file-meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
  }

  .file-owner {
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
  }

  .file-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }

  .file-diff {
    grid-column: 1 / -1;
    min-width: 0;
  }

  .diff-loading {
    display: flex;
    justify-content: center;
    padding: 16px;
    color: var(--fg-dim);
  }

  /* 769–1023: действия уходят второй строкой под имя */
  @media (max-width: 1023px) {
    .file-list {
      grid-template-columns: minmax(0, 1fr) auto;
    }
    .file-actions {
      grid-column: 1 / -1;
      justify-content: flex-start;
    }
  }

  /* ≤768: одна колонка; имя, затем метки одной строкой, затем кнопки на всю ширину */
  @media (max-width: 768px) {
    .file-list {
      grid-template-columns: minmax(0, 1fr);
    }
    .file-actions :global(.btn) {
      flex: 1 1 auto;
      min-height: 44px;
    }
  }
</style>
