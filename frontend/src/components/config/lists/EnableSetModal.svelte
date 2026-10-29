<script module lang="ts">
  /** Набор, который включает диалог. */
  export interface EnableTarget {
    /** Идентификатор набора: стандартного XKeen или каталога панели. */
    id: string;
    /** Название в заголовке диалога. */
    name: string;
    /** Имя файла набора. */
    file: string;
    /** standard — временная запись (D-01), panel — постоянная запись каталога. */
    origin: 'standard' | 'panel';
  }
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import { get } from 'svelte/store';
  import { t } from '../../../i18n';
  import { showConfirm, showToast } from '../../../stores';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import DiffView from '../DiffView.svelte';
  import {
    previewOp,
    writeOp,
    geodataErrorText,
    type PreviewView,
    type WriteOp
  } from '../../../lib/configGeodata';

  // Общий диалог включения набора: из вкладки «Списки» и из выбора списка в правилах.
  // Показывает diff записи в xkeen.json до подтверждения и спрашивает, обновить базы сейчас
  // (xkeen -ug перезапустит ядро) или дождаться расписания. На сервер уходит только
  // идентификатор набора; запись идёт по показанному файлу (base).
  let {
    open,
    target,
    onadded,
    onclose
  }: {
    open: boolean;
    /** Набор для включения; пока диалог открыт, не меняется. */
    target: EnableTarget | null;
    /** Запись выполнена (в том числе при сбое запуска xkeen -ug): списки нужно перечитать. */
    onadded: () => void;
    onclose: () => void;
  } = $props();

  const uid = $props.id();
  // Разделитель, по которому текст со словом «файл» режется на часть до и после имени файла
  const FILE_TOKEN = '\u0001file\u0001';

  let preview = $state<PreviewView | null>(null);
  let loading = $state(false);
  let previewFailure = $state('');
  let writeFailure = $state('');
  let busy = $state(false);
  // Запись уже выполнена, но запуск xkeen -ug не удался: повторная запись не нужна
  let written = $state(false);
  let updateMode = $state<'later' | 'now'>('later');
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  const op = $derived<WriteOp>({ kind: 'enable_set', set: target?.id ?? '' });

  async function load(next: WriteOp) {
    const mine = ++seq;
    loading = true;
    previewFailure = '';
    preview = null;
    try {
      const data = await previewOp(next);
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
    written = false;
    updateMode = 'later';
  }

  $effect(() => {
    if (open && target) {
      const id = target.id;
      untrack(() => {
        reset();
        void load({ kind: 'enable_set', set: id });
      });
    } else {
      untrack(reset);
    }
  });

  const title = $derived($t('cfg.geo.enable_title', { name: target?.name ?? '' }));
  const bodyKey = $derived(
    target?.origin === 'standard' ? 'cfg.geo.enable_body_standard' : 'cfg.geo.enable_body_panel'
  );
  const bodyParts = $derived.by((): [string, string] => {
    const [before, after = ''] = $t(bodyKey, { file: FILE_TOKEN }).split(FILE_TOKEN);
    return [before, after];
  });

  const canAdd = $derived(preview !== null && !written && !busy && !loading);

  function close() {
    // Пока идёт запись, диалог не закрывается: ответ определит, что лежит в xkeen.json
    if (busy) return;
    onclose();
  }

  async function submit() {
    if (!canAdd || !target || !preview) return;
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
      await writeOp(op, preview.base, acceptCommentLoss, updateMode === 'now');
      showToast('success', get(t)('cfg.geo.enable_done'));
      onadded();
      onclose();
    } catch (e) {
      const err = e as { code?: string } | null;
      writeFailure = geodataErrorText(e);
      if (err?.code === 'update_failed') {
        // Запись выполнена, не удался только xkeen -ug: файл уже изменён
        written = true;
        onadded();
      } else if (err?.code === 'geodata_changed') {
        // Файл изменился после показанного предпросмотра: показывается новый diff
        await load(op);
      }
    } finally {
      busy = false;
    }
  }
</script>

<Modal isOpen={open} {title} maxWidth="640px" dataTestid="geodata-add-modal" onclose={close}>
  <div class="es-body">
    {#if target}
      <p class="es-text" id="{uid}-body" data-testid="geodata-add-body">
        {bodyParts[0]}<span class="es-mono">{target.file}</span>{bodyParts[1]}
      </p>
    {/if}

    {#if loading}
      <div class="es-state" role="status" data-testid="geodata-add-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span>{$t('cfg.list.loading')}</span>
      </div>
    {:else if previewFailure !== ''}
      <div class="alert alert-error es-alert" role="alert" data-testid="geodata-add-preview-error">
        <Icon name="warning" size={16} class="icon-first-line" />
        <span>{previewFailure}</span>
      </div>
    {:else if preview}
      <div class="es-diff" data-testid="geodata-add-diff">
        <DiffView
          id="{uid}-diff"
          expected={preview.after}
          actual={preview.before}
          missing={preview.before.trim() === ''}
        />
      </div>
    {/if}

    <div class="es-radios" role="radiogroup" aria-labelledby="{uid}-body">
      <label class="es-radio">
        <input
          type="radio"
          name="{uid}-update"
          value="later"
          checked={updateMode === 'later'}
          disabled={busy || written}
          data-testid="geodata-update-later"
          onchange={() => (updateMode = 'later')}
        />
        <span>{$t('cfg.geodata.update_later')}</span>
      </label>
      <label class="es-radio">
        <input
          type="radio"
          name="{uid}-update"
          value="now"
          checked={updateMode === 'now'}
          disabled={busy || written}
          data-testid="geodata-update-now"
          onchange={() => (updateMode = 'now')}
        />
        <span>{$t('cfg.geodata.update_now')}</span>
      </label>
    </div>

    {#if writeFailure !== ''}
      <div class="alert alert-error es-alert" role="alert" data-testid="geodata-add-error">
        <Icon name="warning" size={16} class="icon-first-line" />
        <span>{writeFailure}</span>
      </div>
    {/if}

    <div class="es-footer">
      <Button
        variant="primary"
        disabled={!canAdd}
        loading={busy}
        data-testid="geodata-add-confirm"
        onclick={submit}
      >
        {$t('cfg.geo.enable_submit')}
      </Button>
      <Button variant="secondary" disabled={busy} data-testid="geodata-add-cancel" onclick={close}>
        {written ? $t('app.close') : $t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .es-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .es-text {
    margin: 0;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .es-mono {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  /* Высота области не прыгает между чтением, ошибкой и diff */
  .es-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2);
    min-height: 120px;
    color: var(--fg-secondary);
  }

  .es-diff {
    min-width: 0;
  }

  .es-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .es-radios {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .es-radio {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-height: 32px;
    padding: var(--spacing-1) var(--spacing-2);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    cursor: pointer;
    overflow-wrap: anywhere;
  }

  .es-radio:hover {
    background: var(--bg-hover);
  }

  .es-radio input {
    flex-shrink: 0;
    margin-top: var(--spacing-1);
    accent-color: var(--accent);
  }

  .es-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .es-radio {
      min-height: 44px;
      align-items: center;
    }

    .es-radio input {
      margin-top: 0;
    }

    .es-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .es-footer {
      flex-direction: column;
    }

    .es-footer :global(.btn) {
      width: 100%;
    }
  }
</style>
