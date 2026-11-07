<script lang="ts">
  import { t } from '../../../i18n';
  import { showConfirm } from '../../../stores';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    applyRunning,
    handleLayerError,
    layerSnapshot,
    rebuildFiles,
    releaseFile,
    type LayerFile
  } from '../../../lib/configLayer';
  import { configSubTabHash } from '../../../lib/configSubTab';
  import type { CoreKernel, CoreSectionSnap, SectionView } from '../../../lib/configCores';
  import {
    cancelRelease,
    confirmPresetSection,
    confirmReleaseSection,
    coresBusy,
    reclaim
  } from './coresOwner';

  // Строка владельца секции (D-01…D-05, D-15, D-25): кто ведёт секцию и какие действия
  // доступны. Решения принимает сервер — строка отражает вид секции и снимок слоя. Все
  // действия идут в черновик, файлы меняются при «Применить». У секций Mihomo кнопок нет:
  // секция берётся и отпускается вместе с профилем (D-03).
  let {
    kernel,
    section,
    snap,
    ontake
  }: {
    kernel: CoreKernel;
    section: SectionView;
    snap: CoreSectionSnap | undefined;
    ontake: () => void;
  } = $props();

  const id = $derived(section.id);
  const name = $derived($t(`cfg.sec.title.${kernel}.${section.id}`));
  const isXray = $derived(kernel === 'xray');
  const blocked = $derived($coresBusy || $applyRunning);
  const busyHint = $derived($applyRunning ? $t('cfg.routing.apply_busy_hint') : undefined);

  // Чужой файл с ключом секции мешает файлу панели (section_foreign)
  const foreignDrift = $derived(snap?.drift ?? []);
  // Файл панели изменён снаружи
  const fileDrift = $derived(foreignDrift.length === 0 && section.owner === 'drift');
  const ownerKind = $derived(foreignDrift.length > 0 ? 'drift' : section.owner);

  const filesText = $derived(section.files.join(', '));
  const ownerName = $derived(
    section.owner === 'user' ? $t('cfg.sec.owner_user') : $t('cfg.sec.owner_xkeen')
  );

  // Файл панели в манифесте слоя: по нему идут «Пересобрать» и «Принять»
  const panelFile = $derived.by<LayerFile | null>(() => {
    const want = section.panel_file ?? snap?.file ?? '';
    if (want === '') return null;
    const files = $layerSnapshot?.files ?? [];
    return files.find((f) => f.path === want || f.path.endsWith(`/${want}`)) ?? null;
  });
  const driftFileText = $derived(panelFile?.path ?? section.panel_file ?? snap?.file ?? filesText);

  // Какое действие идёт: его кнопка показывает загрузку, остальные неактивны
  let action = $state<'' | 'release' | 'preset' | 'reclaim' | 'rebuild' | 'accept' | 'manage'>('');

  async function run(
    kind: 'release' | 'preset' | 'reclaim' | 'manage',
    fn: () => Promise<boolean>
  ): Promise<void> {
    action = kind;
    try {
      await fn();
    } finally {
      action = '';
    }
  }

  /** Решение дрейфа файла панели: подтверждение, затем «Пересобрать» либо «Принять». */
  async function fileAction(kind: 'rebuild' | 'accept'): Promise<void> {
    const file = panelFile;
    if (!file || $coresBusy || $applyRunning) return;
    const rebuild = kind === 'rebuild';
    const ok = await showConfirm({
      variant: 'warning',
      title: $t(rebuild ? 'cfg.confirm.rebuild_title' : 'cfg.confirm.release_title'),
      objectName: file.path,
      message: $t(rebuild ? 'cfg.confirm.rebuild_message' : 'cfg.confirm.release_message'),
      consequence: $t(
        rebuild ? 'cfg.confirm.rebuild_consequence' : 'cfg.confirm.release_consequence'
      ),
      confirmLabel: $t(rebuild ? 'cfg.confirm.rebuild_ok' : 'cfg.confirm.release_ok')
    });
    if (!ok) return;
    action = kind;
    coresBusy.set(true);
    try {
      if (rebuild) await rebuildFiles([file.key]);
      else await releaseFile(file.key);
    } catch (err) {
      handleLayerError(err);
    } finally {
      coresBusy.set(false);
      action = '';
    }
  }

  function blockedText(code: string, file: string | undefined): string {
    return $t(`cfg.sec.blocked.${code}`, { file: file ?? '?' });
  }
</script>

<div class="so" data-testid="section-owner-{kernel}-{id}" data-owner={ownerKind}>
  {#if foreignDrift.length > 0}
    <!-- Чужой файл с ключом секции: решает пользователь, «Применить» заблокировано -->
    <div class="alert alert-error so-alert" role="alert" data-testid="section-drift-{kernel}-{id}">
      <Icon name="warning" size={16} class="so-icon" />
      <span class="so-text">{$t('cfg.sec.drift_foreign', { file: foreignDrift.join(', ') })}</span>
    </div>
    {#if isXray}
      <div class="so-actions">
        <Button
          variant="primary"
          disabled={blocked}
          loading={action === 'reclaim'}
          title={busyHint}
          data-testid="section-reclaim-{kernel}-{id}"
          onclick={() => void run('reclaim', () => reclaim(kernel, id))}
        >
          {$t('cfg.routing.drift_reclaim_file')}
        </Button>
        <Button
          variant="secondary"
          disabled={blocked}
          loading={action === 'release'}
          title={busyHint}
          data-testid="section-release-{kernel}-{id}"
          onclick={() => void run('release', () => confirmReleaseSection(kernel, id, name))}
        >
          {$t('cfg.sec.release_btn')}
        </Button>
      </div>
    {/if}
  {:else if fileDrift}
    <!-- Файл панели изменён снаружи: «Пересобрать» или «Принять» -->
    <div class="alert alert-error so-alert" role="alert" data-testid="section-drift-{kernel}-{id}">
      <Icon name="warning" size={16} class="so-icon" />
      <span class="so-text">{$t('cfg.sec.drift_note', { file: driftFileText })}</span>
    </div>
    {#if isXray && panelFile}
      <div class="so-actions">
        <Button
          variant="primary"
          disabled={blocked}
          loading={action === 'rebuild'}
          title={busyHint}
          data-testid="section-rebuild-{kernel}-{id}"
          onclick={() => void fileAction('rebuild')}
        >
          {$t('cfg.action.rebuild')}
        </Button>
        <Button
          variant="secondary"
          disabled={blocked}
          loading={action === 'accept'}
          title={busyHint}
          data-testid="section-accept-{kernel}-{id}"
          onclick={() => void fileAction('accept')}
        >
          {$t('cfg.confirm.release_ok')}
        </Button>
      </div>
    {/if}
  {:else if section.owner === 'panel'}
    <p class="so-line">
      <span class="so-ok"><Icon name="check" size={16} /></span>
      <span class="so-text">{$t('cfg.sec.owner_panel_note')}</span>
    </p>
    {#if isXray}
      <div class="so-actions">
        <Button
          variant="secondary"
          disabled={blocked}
          loading={action === 'preset'}
          title={busyHint}
          data-testid="section-preset-{kernel}-{id}"
          onclick={() => void run('preset', () => confirmPresetSection(kernel, id, name))}
        >
          {$t('cfg.sec.preset_btn')}
        </Button>
        <Button
          variant="secondary"
          disabled={blocked}
          loading={action === 'release'}
          title={busyHint}
          data-testid="section-release-{kernel}-{id}"
          onclick={() => void run('release', () => confirmReleaseSection(kernel, id, name))}
        >
          {$t('cfg.sec.release_btn')}
        </Button>
      </div>
    {/if}
  {:else if section.owner === 'pending'}
    <p class="so-line" data-testid="section-pending-{kernel}-{id}">
      <span class="so-text">{$t('cfg.sec.owner_pending_note')}</span>
    </p>
  {:else if section.owner === 'releasing'}
    <!-- Отпускание ждёт «Применить»: значения прежние, правка закрыта, «Управлять» отменяет -->
    <p class="so-line" data-testid="section-releasing-{kernel}-{id}">
      <span class="so-text">{$t('cfg.sec.owner_releasing_note')}</span>
    </p>
    {#if isXray}
      <div class="so-actions">
        <Button
          variant="primary"
          disabled={blocked}
          loading={action === 'manage'}
          title={busyHint}
          data-testid="section-manage-{kernel}-{id}"
          onclick={() => void run('manage', () => cancelRelease(kernel, id))}
        >
          {$t('cfg.sec.take_btn')}
        </Button>
      </div>
    {/if}
  {:else}
    {#if section.owner === 'none'}
      <p class="so-line"><span class="so-text">{$t('cfg.sec.no_file_note')}</span></p>
    {:else}
      <p class="so-line">
        <span class="so-text">
          {$t('cfg.sec.foreign_note', { owner: ownerName, file: filesText })}
        </span>
      </p>
    {/if}
    {#if section.blocked}
      <div class="alert alert-warning so-alert" data-testid="section-blocked-{kernel}-{id}">
        <Icon name="warning" size={16} class="so-icon" />
        <span class="so-text">{blockedText(section.blocked, section.blocked_file)}</span>
      </div>
    {/if}
    {#if isXray}
      <div class="so-actions">
        <Button
          variant="primary"
          disabled={blocked || !!section.blocked}
          title={busyHint}
          data-testid="section-take-{kernel}-{id}"
          onclick={ontake}
        >
          {$t('cfg.sec.take_btn')}
        </Button>
        {#if section.owner !== 'none'}
          <a
            class="btn btn-secondary"
            href={configSubTabHash('files')}
            data-testid="section-open-files-{kernel}-{id}"
          >
            {$t('cfg.sec.open_files')}
          </a>
        {/if}
      </div>
    {/if}
  {/if}
  {#if !isXray}
    <p class="so-note" data-testid="section-profile-note-{kernel}-{id}">
      {$t('cfg.sec.mihomo_section_note')}
    </p>
  {/if}
</div>

<style>
  .so {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .so-line {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .so-ok {
    display: inline-flex;
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
    color: var(--success);
  }

  .so-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .so-note {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .so-alert {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
  }

  .so-alert :global(.so-icon) {
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
  }

  .so-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .so-actions :global(.btn) {
      flex: 1 1 auto;
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .so-actions :global(.btn) {
      flex: 1 1 100%;
    }
  }
</style>
