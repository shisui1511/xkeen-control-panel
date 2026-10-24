<script lang="ts">
  import { untrack } from 'svelte';
  import { get } from 'svelte/store';
  import { t } from '../../../i18n';
  import { showConfirm, showToast } from '../../../stores';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import DiffView from '../DiffView.svelte';
  import { addGeodata, previewGeodata, routingErrorText } from '../../../lib/configRouting';
  import type { ExtendedSet, GeodataPreview, GeodataStatus } from '../../../lib/configRouting';
  import { reasonOf } from './routingOwner';

  // «Добавить в XKeen» (D-33): набор из встроенного каталога дописывается в xkeen.json как
  // запись geodata. Файлами набора владеет XKeen — панель их не скачивает. Диалог показывает
  // diff записи до подтверждения и спрашивает, обновить базы сейчас (xkeen -ug перезапустит
  // ядро) или дождаться ночного обновления. На сервер уходит только идентификатор набора:
  // адрес набора в интерфейсе не вводится и не отправляется.
  let { open, set, geodata, onadded, onclose } = $props<{
    open: boolean;
    /** Набор встроенного каталога; пока диалог открыт, не меняется. */
    set: ExtendedSet | null;
    /** Состояние geodata XKeen из каталога: по нему определяются «нет jq» и «обновление выключено». */
    geodata: GeodataStatus | null;
    /** Запись выполнена (в том числе при сбое запуска xkeen -ug): каталог нужно перечитать. */
    onadded: () => void;
    onclose: () => void;
  }>();

  const uid = $props.id();
  // Разделитель, по которому текст со словом «файл» режется на часть до и после имени файла
  const FILE_TOKEN = '\u0001file\u0001';

  let preview = $state<GeodataPreview | null>(null);
  let loading = $state(false);
  let previewFailure = $state('');
  let writeFailure = $state('');
  let busy = $state(false);
  // Файл уже записан, но запуск xkeen -ug не удался: повторная запись не нужна
  let written = $state(false);
  let updateMode = $state<'later' | 'now'>('later');
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  async function load(id: string) {
    const mine = ++seq;
    loading = true;
    previewFailure = '';
    preview = null;
    try {
      const data = await previewGeodata(id);
      if (mine === seq) preview = data;
    } catch (e) {
      if (mine === seq) previewFailure = reasonOf(e);
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
    if (open && set) {
      const id = set.id;
      untrack(() => {
        reset();
        void load(id);
      });
    } else {
      untrack(reset);
    }
  });

  /** Причина, по которой запись невозможна: нет jq либо выключено обновление баз. */
  const blockReason = $derived.by(() => {
    if (!geodata) return '';
    if (geodata.reason === 'no_jq' || geodata.jq === false) return 'no_jq';
    if (geodata.reason === 'geofile_update_off' || geodata.geofile_update === false)
      return 'disabled';
    return '';
  });

  const bodyParts = $derived.by((): [string, string] => {
    const [before, after = ''] = $t('cfg.geodata.body', { file: FILE_TOKEN }).split(FILE_TOKEN);
    return [before, after];
  });

  const canAdd = $derived(preview !== null && blockReason === '' && !written && !busy);

  function close() {
    // Пока идёт запись, диалог не закрывается: ответ определит, что лежит в xkeen.json
    if (busy) return;
    onclose();
  }

  async function submit() {
    if (!canAdd || !set || !preview) return;
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
      await addGeodata(set.id, preview.base, acceptCommentLoss, updateMode === 'now');
      showToast('success', get(t)('cfg.geodata.toast_added'));
      onadded();
      onclose();
    } catch (e) {
      const err = e as { code?: string; params?: Record<string, string> } | null;
      if (err?.code === 'update_failed') {
        // Запись выполнена, не удался только xkeen -ug: файл уже изменён
        written = true;
        writeFailure = routingErrorText(get(t), err.code, err.params);
        onadded();
      } else if (err?.code === 'geodata_changed') {
        // Файл изменился после показанного предпросмотра: показываем новый diff
        const id = set.id;
        await load(id);
        writeFailure = routingErrorText(get(t), err.code, err.params);
      } else {
        writeFailure = get(t)('cfg.geodata.write_failed', { reason: reasonOf(e) });
      }
    } finally {
      busy = false;
    }
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.geodata.title')}
  maxWidth="640px"
  dataTestid="geodata-add-modal"
  onclose={close}
>
  <div class="ag-body">
    {#if set}
      <p class="ag-text" id="{uid}-body" data-testid="geodata-add-body">
        {bodyParts[0]}<span class="ag-mono">{set.file}</span>{bodyParts[1]}
      </p>
    {/if}

    {#if loading}
      <div class="ag-state" role="status" data-testid="geodata-add-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span>{$t('cfg.list.loading')}</span>
      </div>
    {:else if previewFailure !== ''}
      <div class="alert alert-error ag-alert" role="alert" data-testid="geodata-add-preview-error">
        <Icon name="warning" size={16} class="ag-alert-icon" />
        <span>{$t('cfg.geodata.write_failed', { reason: previewFailure })}</span>
      </div>
    {:else if preview}
      <div class="ag-diff" data-testid="geodata-add-diff">
        <DiffView
          id="{uid}-diff"
          expected={preview.after}
          actual={preview.before}
          missing={preview.before.trim() === ''}
        />
      </div>
    {/if}

    {#if blockReason !== ''}
      <div class="alert alert-warning ag-alert" data-testid="geodata-add-reason-{blockReason}">
        <Icon name="warning" size={16} class="ag-alert-icon" />
        <span>{blockReason === 'no_jq' ? $t('cfg.geodata.no_jq') : $t('cfg.geodata.disabled')}</span
        >
      </div>
    {/if}

    <div class="ag-radios" role="radiogroup" aria-labelledby="{uid}-body">
      <label class="ag-radio">
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
      <label class="ag-radio">
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
      <div class="alert alert-error ag-alert" role="alert" data-testid="geodata-add-error">
        <Icon name="warning" size={16} class="ag-alert-icon" />
        <span>{writeFailure}</span>
      </div>
    {/if}

    <div class="ag-footer">
      <Button
        variant="primary"
        disabled={!canAdd}
        loading={busy}
        data-testid="geodata-add-confirm"
        onclick={submit}
      >
        {$t('cfg.geodata.add')}
      </Button>
      <Button variant="secondary" disabled={busy} data-testid="geodata-add-cancel" onclick={close}>
        {written ? $t('app.close') : $t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .ag-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .ag-text {
    margin: 0;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .ag-mono {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  /* Высота области не прыгает между чтением, ошибкой и diff */
  .ag-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2);
    min-height: 120px;
    color: var(--fg-secondary);
  }

  .ag-diff {
    min-width: 0;
  }

  .ag-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ag-alert :global(.ag-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .ag-radios {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .ag-radio {
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

  .ag-radio:hover {
    background: var(--bg-hover);
  }

  .ag-radio input {
    flex-shrink: 0;
    margin-top: 3px;
    accent-color: var(--accent);
  }

  .ag-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .ag-radio {
      min-height: 44px;
      align-items: center;
    }

    .ag-radio input {
      margin-top: 0;
    }

    .ag-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .ag-footer {
      flex-direction: column;
    }

    .ag-footer :global(.btn) {
      width: 100%;
    }
  }
</style>
