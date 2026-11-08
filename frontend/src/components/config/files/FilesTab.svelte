<script lang="ts">
  import { t } from '../../../i18n';
  import { showConfirm } from '../../../stores';
  import Icon from '../../../lib/components/Icon.svelte';
  import Button from '../../Button.svelte';
  import Editor from '../../../Editor.svelte';
  import DiffView from '../DiffView.svelte';
  import ImmediateMark from '../ImmediateMark.svelte';
  import type { ConfigFileInfo } from '../../editor/fileOps';
  import {
    filesByPath,
    applyRunning,
    rebuildFiles,
    releaseFile,
    fetchDiff,
    handleLayerError,
    type LayerFile,
    type LayerDiff
  } from '../../../lib/configLayer';

  // Вкладка «Файлы» раздела «Конфигурация» (D-01, CFG-03): нынешний Редактор во встроенном
  // режиме, без копии вёрстки. Раздел держит вкладку смонтированной после первого открытия
  // и скрывает её, пока открыта другая вкладка: открытые файлы и несохранённые правки живут.
  let { active = true }: { active?: boolean } = $props();

  let activePath = $state<string | null>(null);
  let activeFileInfo = $state<ConfigFileInfo | undefined>(undefined);

  type DiffState = { status: 'loading' | 'ready'; diff?: LayerDiff };
  let opened = $state<Record<string, boolean>>({});
  let diffs = $state<Record<string, DiffState>>({});
  let acting = $state(false);

  const locked = $derived($applyRunning || acting);
  const diffId = (key: string) => `config-diff-${key.replace(/[^a-zA-Z0-9_-]/g, '-')}`;

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
      consequence: $t('cfg.files.release_consequence'),
      confirmLabel: $t('cfg.confirm.release_ok')
    });
    if (!confirmed) return;
    await run(() => releaseFile(f.key));
  }
</script>

<section class="files-tab" data-testid="files-tab" hidden={!active}>
  <Editor
    embedded
    {active}
    onactivefile={(p, f) => {
      activePath = p;
      activeFileInfo = f;
    }}
  >
    {#snippet aboveCode(path, info)}
      {@const fileInfo = info ?? activeFileInfo}
      {@const layerFile = path ? $filesByPath.get(path) : undefined}
      {#if layerFile && layerFile.state?.startsWith('drift_')}
        <div
          class="alert alert-warning files-drift-banner"
          role="alert"
          data-testid="files-drift-banner"
        >
          <div class="drift-header">
            <Icon name="warning" size={16} />
            <span class="drift-message">
              {layerFile.state === 'drift_missing'
                ? $t('cfg.files.drift_missing')
                : $t('cfg.files.drift_modified')}
            </span>
            <div class="drift-actions">
              {#if layerFile.state !== 'drift_missing'}
                <Button
                  variant="secondary"
                  size="sm"
                  class="drift-btn"
                  aria-expanded={opened[layerFile.key] ? 'true' : 'false'}
                  aria-controls={diffId(layerFile.key)}
                  onclick={() => void toggleDiff(layerFile.key)}
                >
                  <Icon name={opened[layerFile.key] ? 'eye-off' : 'eye'} size={13} />
                  {opened[layerFile.key] ? $t('cfg.action.hide_diff') : $t('cfg.files.show_diff')}
                </Button>
              {/if}
              <Button
                variant="secondary"
                size="sm"
                class="drift-btn"
                disabled={locked}
                onclick={() => void onRebuild(layerFile)}
              >
                {$t('cfg.files.rebuild')}
              </Button>
              {#if layerFile.state !== 'drift_missing'}
                <Button
                  variant="secondary"
                  size="sm"
                  class="drift-btn"
                  disabled={locked}
                  onclick={() => void onRelease(layerFile)}
                >
                  {$t('cfg.files.accept')}
                </Button>
              {/if}
              <ImmediateMark />
            </div>
          </div>
          {#if layerFile.state !== 'drift_missing' && opened[layerFile.key]}
            <div class="drift-diff" id={diffId(layerFile.key)}>
              {#if diffs[layerFile.key]?.diff}
                {@const d = diffs[layerFile.key].diff!}
                <DiffView
                  id={diffId(layerFile.key)}
                  expected={d.expected}
                  actual={d.actual}
                  missing={d.missing}
                  truncated={d.truncated}
                />
              {:else}
                <div class="diff-loading">
                  <span class="spinner" aria-hidden="true"></span>
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/if}
    {/snippet}
  </Editor>
</section>

<style>
  .files-drift-banner {
    margin: 0;
    border-radius: 0;
    border-width: 0 0 1px 0;
    border-left-width: var(--spacing-1);
    border-left-style: solid;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    flex-shrink: 0;
  }

  .drift-header {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    flex-wrap: wrap;
    min-width: 0;
  }

  .drift-header :global(svg) {
    flex: none;
  }

  .drift-message {
    margin: 0;
    flex: 1 1 auto;
    min-width: 0;
    font-size: var(--font-size-sm);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .drift-actions {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    flex-wrap: wrap;
    flex-shrink: 0;
  }

  .drift-diff {
    width: 100%;
    min-width: 0;
  }

  .diff-loading {
    display: flex;
    justify-content: center;
    padding: var(--spacing-3);
    color: var(--fg-dim);
  }

  @media (max-width: 768px) {
    .drift-header {
      align-items: flex-start;
    }

    .drift-actions {
      width: 100%;
      flex-direction: column;
    }

    .drift-actions :global(.drift-btn) {
      width: 100%;
      min-height: 44px;
    }
  }
</style>
