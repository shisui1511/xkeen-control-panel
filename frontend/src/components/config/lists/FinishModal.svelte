<script module lang="ts">
  /** Что записывает диалог: вид операции над xkeen.json. */
  export type FinishMode =
    'gh_proxy' | 'enable_update' | 'dedup' | 'finish_include' | 'finish_delete';
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import { get } from 'svelte/store';
  import { t } from '../../../i18n';
  import { showConfirm } from '../../../stores';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import DiffView from '../DiffView.svelte';
  import {
    previewOp,
    writeOp,
    geodataErrorText,
    type PreviewView
  } from '../../../lib/configGeodata';

  // Общий диалог записи в xkeen.json с diff: gh_proxy, geofile_update, «Убрать дубль»,
  // «Довести включение» и «Довести удаление». Запись идёт по показанному файлу (base):
  // если xkeen.json изменили, пока окно было открыто, показывается новый diff и запись
  // просит повторить. Обновление баз здесь не запускается.
  let {
    open,
    mode,
    file,
    value,
    onclose,
    ondone
  }: {
    open: boolean;
    mode: FinishMode;
    file?: string;
    /** Значение настройки (gh_proxy). */
    value?: string;
    onclose: () => void;
    /** Запись выполнена: вызывающий закрывает диалог сам через onclose после этого вызова. */
    ondone?: () => void;
  } = $props();

  const uid = $props.id();

  let preview = $state<PreviewView | null>(null);
  let loading = $state(false);
  let previewFailure = $state('');
  let writeFailure = $state('');
  let busy = $state(false);
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  const TITLE: Record<FinishMode, string> = {
    gh_proxy: 'cfg.geo.ghproxy_diff_title',
    enable_update: 'cfg.geo.update_enable_diff_title',
    dedup: 'cfg.geo.dup_title',
    finish_include: 'cfg.geo.finish_include_title',
    finish_delete: 'cfg.geo.finish_delete_title'
  };
  const BODY: Partial<Record<FinishMode, string>> = {
    dedup: 'cfg.geo.dup_body',
    finish_include: 'cfg.geo.finish_include_body',
    finish_delete: 'cfg.geo.finish_delete_body'
  };
  const SUBMIT: Record<FinishMode, string> = {
    gh_proxy: 'cfg.geo.ghproxy_write',
    enable_update: 'cfg.geo.update_write',
    dedup: 'cfg.geo.dup_fix',
    finish_include: 'cfg.geo.finish_submit_include',
    finish_delete: 'cfg.geo.finish_submit_delete'
  };

  const params = $derived({ file: file ?? '' });
  const title = $derived($t(TITLE[mode], params));
  const body = $derived(BODY[mode] !== undefined ? $t(BODY[mode], params) : '');
  const op = $derived({ kind: mode, file, value });

  async function load() {
    const mine = ++seq;
    loading = true;
    previewFailure = '';
    preview = null;
    try {
      const data = await previewOp(op);
      if (mine === seq) preview = data;
    } catch (e) {
      if (mine === seq) previewFailure = geodataErrorText(e);
    } finally {
      if (mine === seq) loading = false;
    }
  }

  function reset() {
    seq++;
    preview = null;
    loading = false;
    previewFailure = '';
    writeFailure = '';
    busy = false;
  }

  $effect(() => {
    if (open) {
      untrack(() => {
        reset();
        void load();
      });
    } else {
      untrack(reset);
    }
  });

  const canWrite = $derived(preview !== null && !busy && !loading);

  function close() {
    // Пока идёт запись, диалог не закрывается: ответ определит, что лежит в xkeen.json
    if (busy) return;
    onclose();
  }

  async function submit() {
    if (!canWrite || preview === null) return;
    writeFailure = '';
    let acceptCommentLoss = false;
    if (!preview.comments_kept) {
      const ok = await showConfirm({
        title: get(t)('cfg.geodata.comments_lost_title'),
        message: get(t)('cfg.geodata.comments_lost_body'),
        confirmLabel: get(t)('cfg.geodata.comments_lost_confirm'),
        variant: 'warning'
      });
      if (!ok) return;
      acceptCommentLoss = true;
    }
    busy = true;
    try {
      await writeOp(op, preview.base, acceptCommentLoss, false);
      ondone?.();
      onclose();
    } catch (e) {
      const err = e as { code?: string } | null;
      writeFailure = geodataErrorText(e);
      if (err?.code === 'geodata_changed') {
        // Файл изменился после показанного предпросмотра: показывается новый diff
        await load();
      }
    } finally {
      busy = false;
    }
  }
</script>

<Modal isOpen={open} {title} maxWidth="640px" dataTestid="geo-finish-modal" onclose={close}>
  <div class="gf-body">
    {#if body !== ''}
      <p class="gf-text" id="{uid}-body">{body}</p>
    {/if}

    {#if loading}
      <div class="gf-state" role="status" data-testid="geo-finish-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span>{$t('cfg.list.loading')}</span>
      </div>
    {:else if previewFailure !== ''}
      <div class="alert alert-error gf-alert" role="alert" data-testid="geo-finish-preview-error">
        <Icon name="warning" size={16} class="gf-alert-icon" />
        <span>{previewFailure}</span>
      </div>
    {:else if preview}
      <div class="gf-diff" data-testid="geo-finish-diff">
        <DiffView
          id="{uid}-diff"
          expected={preview.after}
          actual={preview.before}
          missing={preview.before.trim() === ''}
        />
      </div>
    {/if}

    {#if writeFailure !== ''}
      <div class="alert alert-error gf-alert" role="alert" data-testid="geo-finish-error">
        <Icon name="warning" size={16} class="gf-alert-icon" />
        <span>{writeFailure}</span>
      </div>
    {/if}

    <div class="gf-footer">
      <Button
        variant="primary"
        disabled={!canWrite}
        loading={busy}
        data-testid="geo-finish-submit"
        onclick={submit}
      >
        {$t(SUBMIT[mode])}
      </Button>
      <Button variant="secondary" disabled={busy} data-testid="geo-finish-cancel" onclick={close}>
        {$t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .gf-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .gf-text {
    margin: 0;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  /* Высота области не прыгает между чтением, ошибкой и diff */
  .gf-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2);
    min-height: 120px;
    color: var(--fg-secondary);
  }

  .gf-diff {
    min-width: 0;
  }

  .gf-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .gf-alert :global(.gf-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .gf-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .gf-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .gf-footer {
      flex-direction: column;
    }

    .gf-footer :global(.btn) {
      width: 100%;
    }
  }
</style>
