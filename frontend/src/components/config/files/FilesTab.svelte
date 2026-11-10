<script lang="ts">
  import { t } from '../../../i18n';
  import { showConfirm } from '../../../stores';
  import Icon from '../../../lib/components/Icon.svelte';
  import Button from '../../Button.svelte';
  import Editor from '../../../Editor.svelte';
  import DiffView from '../DiffView.svelte';
  import AppliesList from './AppliesList.svelte';
  import ImmediateMark from '../ImmediateMark.svelte';
  import TakeSectionModal from '../sections/TakeSectionModal.svelte';
  import type { CoreKernel } from '../../../lib/configCores';
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

  // Модальное окно «Управлять в панели» (D-03)
  let takeModalOpen = $state(false);
  let takeKernel = $state<CoreKernel>('xray');
  let takeMode = $state<'one' | 'all'>('one');
  let takeSection = $state('');
  let takeIds = $state<string[]>([]);

  function openTake(info: ConfigFileInfo) {
    if (!info.take_kernel || !info.take_sections || info.take_sections.length === 0) return;
    takeKernel = info.take_kernel as CoreKernel;
    if (info.take_sections.length === 1) {
      takeMode = 'one';
      takeSection = info.take_sections[0];
      takeIds = info.take_sections;
    } else {
      takeMode = 'all';
      takeSection = '';
      takeIds = info.take_sections;
    }
    takeModalOpen = true;
  }

  function ownerOf(path: string, info?: ConfigFileInfo): 'panel' | 'xkeen' | 'user' | null {
    if (info?.owner) return info.owner;
    const lf = $filesByPath.get(path);
    if (lf) return lf.state === 'released' ? 'user' : 'panel';
    if (path.startsWith('/opt/etc/xkeen')) return 'xkeen';
    if (path) return 'user';
    return null;
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
      // Итог называет одна строка контракта: «Файл станет пользовательским…», без повтора
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
      <!-- Слово стоп-списка — точная причина, имя не латиницей — отдельный случай (WR-05) -->
      {@const stoplistText = fileInfo?.stoplist_word
        ? $t('cfg.files.stoplist_banner', { word: fileInfo.stoplist_word })
        : fileInfo?.restricted
          ? $t('cfg.files.restricted_banner')
          : ''}
      {#if stoplistText}
        <div
          class="alert alert-warning files-stoplist-banner"
          role="alert"
          data-testid="files-stoplist-banner"
        >
          <Icon name="warning" size={16} />
          <span class="stoplist-message">
            {stoplistText}
          </span>
        </div>
      {:else if layerFile && layerFile.state?.startsWith('drift_')}
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
                  <Icon name={opened[layerFile.key] ? 'eye-off' : 'eye'} size={12} />
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
                  expected={d.expected}
                  actual={d.actual}
                  missing={d.missing}
                  truncated={d.truncated}
                />
              {:else}
                <div class="diff-loading" role="status" aria-busy="true">
                  <span class="spinner" aria-hidden="true"></span>
                  <span class="sr-only">{$t('app.loading')}</span>
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {:else}
        {@const owner = ownerOf(path, fileInfo)}
        {#if owner === 'xkeen' || owner === 'user'}
          <div class="files-immediate-note" data-testid="files-immediate-note">
            <!-- Пометка ImmediateMark стоит у «Сохранить» в шапке: здесь только текст, без
                 третьего повтора «мимо черновика» рядом -->
            <span class="immediate-note-text">{$t('cfg.files.immediate_note')}</span>
            {#if fileInfo?.take_kernel && (fileInfo.take_sections?.length ?? 0) > 0}
              <Button
                variant="secondary"
                size="sm"
                class="files-take-btn"
                testid="files-take-btn"
                onclick={() => openTake(fileInfo)}
              >
                {$t('cfg.files.take')}
              </Button>
            {/if}
          </div>
        {/if}
      {/if}
    {/snippet}
    {#snippet appliesFor(path, current)}
      {@const layerFile = path ? $filesByPath.get(path) : undefined}
      {#if layerFile}
        <AppliesList file={layerFile} {current} />
      {/if}
    {/snippet}
  </Editor>

  {#if takeModalOpen}
    <TakeSectionModal
      bind:open={takeModalOpen}
      kernel={takeKernel}
      mode={takeMode}
      section={takeSection}
      ids={takeIds}
      ontaken={() => {
        // события слоя сами обновляют список и снимок
      }}
    />
  {/if}
</section>

<style>
  .files-stoplist-banner {
    margin: 0;
    border-radius: 0;
    border-width: 0 0 1px 0;
    border-left-width: var(--spacing-1);
    border-left-style: solid;
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-3);
    font-size: var(--font-size-base);
    line-height: 1.5;
    min-width: 0;
    flex-shrink: 0;
  }

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
    font-size: var(--font-size-base);
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

  .files-immediate-note {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--bg-card);
    border-bottom: 1px solid var(--border);
    border-left: var(--spacing-1) solid var(--border-strong);
    font-size: var(--font-size-base);
    line-height: 1.5;
    flex-wrap: wrap;
    min-width: 0;
    flex-shrink: 0;
  }

  .immediate-note-text {
    flex: 1 1 auto;
    min-width: 0;
    color: var(--fg-secondary);
  }

  .files-immediate-note :global(.files-take-btn) {
    flex: none;
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

    .files-immediate-note {
      flex-direction: column;
      align-items: flex-start;
    }

    .files-immediate-note :global(.files-take-btn) {
      width: 100%;
      min-height: 44px;
    }
  }
</style>
