<script lang="ts">
  import { untrack } from 'svelte';
  import { get } from 'svelte/store';
  import { t } from '../../../i18n';
  import { showConfirm, showToast } from '../../../stores';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Input from '../../Input.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import DiffView from '../DiffView.svelte';
  import ProbeResults from './ProbeResults.svelte';
  import {
    geodataView,
    probeSource,
    previewOp,
    writeOp,
    geodataErrorText,
    type PreviewView,
    type ProbeResult,
    type SourceView,
    type WriteOp
  } from '../../../lib/configGeodata';

  // Диалог «Изменить ссылку» своего источника, в том числе вписанного в xkeen.json вручную.
  // Имя файла не меняется. Ссылка проходит ту же проверку, что в «Добавить источник» (режим
  // edit), затем показывается diff записи и выбор «обновить сейчас / дождаться расписания».
  // «Сохранить ссылку» активна, только если проверка проведена на текущем значении без блоков.
  let {
    open,
    source,
    onsaved,
    onclose
  }: {
    open: boolean;
    /** Источник, чья ссылка правится; пока диалог открыт, не меняется. */
    source: SourceView | null;
    /** Запись выполнена (в том числе при сбое запуска xkeen -ug): снимок нужно перечитать. */
    onsaved: () => void;
    onclose: () => void;
  } = $props();

  const uid = $props.id();

  let url = $state('');
  // Результат хранится вместе со значением, на котором сделан
  let checked = $state<{ url: string; probe: ProbeResult } | null>(null);
  let checking = $state(false);
  let checkFailure = $state('');
  let preview = $state<PreviewView | null>(null);
  let previewLoading = $state(false);
  let previewFailure = $state('');
  let writeFailure = $state('');
  let busy = $state(false);
  // Запись уже выполнена, но запуск xkeen -ug не удался: повторная запись не нужна
  let written = $state(false);
  let updateMode = $state<'later' | 'now'>('later');
  // Растёт с каждой правкой и чтением: устаревший ответ не затирает свежий
  let seq = 0;

  const file = $derived(source?.file ?? '');
  const urlValue = $derived(url.trim());
  const filled = $derived(file !== '' && urlValue !== '');
  // Результат относится к текущему значению поля
  const fresh = $derived(checked !== null && checked.url === urlValue ? checked : null);
  const blocked = $derived(fresh?.probe.blocked ?? false);
  const canSubmit = $derived(
    fresh !== null &&
      !blocked &&
      preview !== null &&
      !checking &&
      !previewLoading &&
      !busy &&
      !written
  );
  const ghProxy = $derived($geodataView?.download.gh_proxy ?? '');

  function resetResult() {
    seq++;
    checked = null;
    checking = false;
    checkFailure = '';
    preview = null;
    previewLoading = false;
    previewFailure = '';
    writeFailure = '';
  }

  function reset() {
    resetResult();
    url = '';
    busy = false;
    written = false;
    updateMode = 'later';
  }

  $effect(() => {
    if (open && source) {
      const initial = source.url ?? '';
      untrack(() => {
        reset();
        url = initial;
      });
    } else {
      untrack(reset);
    }
  });

  function currentOp(): WriteOp {
    return { kind: 'set_url', file, url: urlValue };
  }

  async function loadPreview(op: WriteOp) {
    const mine = ++seq;
    previewLoading = true;
    previewFailure = '';
    preview = null;
    try {
      const data = await previewOp(op);
      if (mine === seq) preview = data;
    } catch (e) {
      if (mine !== seq) return;
      if (!applyBlocked(e, op)) previewFailure = geodataErrorText(e);
    } finally {
      if (mine === seq) previewLoading = false;
    }
  }

  /** Ответ probe_blocked несёт результат проверки: он показывается вместо предпросмотра. */
  function applyBlocked(e: unknown, op: WriteOp): boolean {
    const err = e as { code?: string; data?: { probe?: ProbeResult } } | null;
    if (err?.code !== 'probe_blocked' || !err.data?.probe) return false;
    checked = { url: op.url ?? '', probe: err.data.probe };
    preview = null;
    return true;
  }

  async function check() {
    if (!filled || checking) return;
    resetResult();
    const mine = seq;
    const op = currentOp();
    checking = true;
    try {
      const probe = await probeSource(file, urlValue, 'edit');
      if (mine !== seq) return;
      checked = { url: urlValue, probe };
      // loadPreview заводит свой номер чтения: кнопка проверки освобождается до него
      checking = false;
      if (!probe.blocked) await loadPreview(op);
    } catch (e) {
      if (mine === seq) checkFailure = geodataErrorText(e);
    } finally {
      if (mine === seq) checking = false;
    }
  }

  function close() {
    // Пока идёт запись, диалог не закрывается: ответ определит, что лежит в xkeen.json
    if (busy) return;
    onclose();
  }

  async function submit() {
    if (!canSubmit || preview === null) return;
    writeFailure = '';
    const op = currentOp();
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
      showToast('success', get(t)('cfg.geo.url_done'));
      onsaved();
      onclose();
    } catch (e) {
      const err = e as { code?: string } | null;
      writeFailure = geodataErrorText(e);
      if (err?.code === 'update_failed') {
        // Запись выполнена, не удался только xkeen -ug: файл уже изменён
        written = true;
        onsaved();
      } else if (!applyBlocked(e, op) && err?.code === 'geodata_changed') {
        // Файл изменился после показанного предпросмотра: показывается новый diff
        await loadPreview(op);
      }
    } finally {
      busy = false;
    }
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.geo.url_title', { file })}
  maxWidth="640px"
  dataTestid="geo-url-modal"
  onclose={close}
>
  <div class="eu-body">
    <div class="eu-field eu-mono" data-testid="geo-url-file">
      <Input id="{uid}-file" label={$t('cfg.geo.add_file')} value={file} disabled />
      <p class="eu-hint">{$t('cfg.geo.url_name_fixed')}</p>
    </div>
    <div class="eu-field eu-mono" data-testid="geo-url-url">
      <Input
        id="{uid}-url"
        label={$t('cfg.geo.add_url')}
        placeholder="https://example.org/geosite.dat"
        bind:value={url}
        oninput={resetResult}
        disabled={busy || written}
      />
    </div>

    <div class="eu-check">
      <Button
        variant="secondary"
        disabled={!filled || busy || written}
        loading={checking}
        title={filled ? undefined : $t('cfg.geo.add_fill_hint')}
        data-testid="geo-url-check"
        onclick={check}
      >
        {$t('cfg.geo.add_check')}
      </Button>
      <p class="eu-hint">{$t('cfg.geo.add_check_hint')}</p>
    </div>

    {#if checkFailure !== ''}
      <div class="alert alert-error eu-alert" role="alert" data-testid="geo-url-check-error">
        <Icon name="warning" size={16} class="icon-first-line" />
        <span>{checkFailure}</span>
      </div>
    {/if}

    <ProbeResults result={fresh?.probe ?? null} {ghProxy} />

    {#if previewLoading}
      <div class="eu-state" role="status" data-testid="geo-url-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span>{$t('cfg.list.loading')}</span>
      </div>
    {:else if previewFailure !== ''}
      <div class="alert alert-error eu-alert" role="alert" data-testid="geo-url-preview-error">
        <Icon name="warning" size={16} class="icon-first-line" />
        <span>{previewFailure}</span>
      </div>
    {:else if preview && fresh !== null && !blocked}
      <div class="eu-diff" data-testid="geo-url-diff">
        <DiffView id="{uid}-diff" expected={preview.after} actual={preview.before} />
      </div>
    {/if}

    <div class="eu-radios" role="radiogroup" aria-label={$t('cfg.geo.url_title', { file })}>
      <label class="eu-radio">
        <input
          type="radio"
          name="{uid}-update"
          value="later"
          checked={updateMode === 'later'}
          disabled={busy || written}
          data-testid="geo-url-update-later"
          onchange={() => (updateMode = 'later')}
        />
        <span>{$t('cfg.geodata.update_later')}</span>
      </label>
      <label class="eu-radio">
        <input
          type="radio"
          name="{uid}-update"
          value="now"
          checked={updateMode === 'now'}
          disabled={busy || written}
          data-testid="geo-url-update-now"
          onchange={() => (updateMode = 'now')}
        />
        <span>{$t('cfg.geodata.update_now')}</span>
      </label>
    </div>

    {#if writeFailure !== ''}
      <div class="alert alert-error eu-alert" role="alert" data-testid="geo-url-error">
        <Icon name="warning" size={16} class="icon-first-line" />
        <span>{writeFailure}</span>
      </div>
    {/if}

    <div class="eu-footer">
      <Button
        variant="primary"
        disabled={!canSubmit}
        loading={busy}
        data-testid="geo-url-submit"
        onclick={submit}
      >
        {$t('cfg.geo.url_submit')}
      </Button>
      <Button variant="secondary" disabled={busy} data-testid="geo-url-cancel" onclick={close}>
        {written ? $t('app.close') : $t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .eu-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .eu-field {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  /* Имя файла и ссылка — моноширинным шрифтом, поле на всю ширину */
  .eu-mono :global(.input) {
    font-family: var(--font-family-mono);
  }

  .eu-check {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .eu-hint {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  /* basis 240px — ширина в строке с кнопкой; в колонке .eu-field он растянул бы подсказку по высоте */
  .eu-check .eu-hint {
    flex: 1 1 240px;
  }

  /* Высота области не прыгает между чтением, ошибкой и diff */
  .eu-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2);
    min-height: 120px;
    color: var(--fg-secondary);
  }

  .eu-diff {
    min-width: 0;
  }

  .eu-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .eu-radios {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .eu-radio {
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

  .eu-radio:hover {
    background: var(--hover);
  }

  .eu-radio input {
    flex-shrink: 0;
    margin-top: var(--spacing-1);
    accent-color: var(--accent);
  }

  .eu-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .eu-radio {
      min-height: 44px;
      align-items: center;
    }

    .eu-radio input {
      margin-top: 0;
    }

    .eu-check :global(.btn),
    .eu-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .eu-footer {
      flex-direction: column;
    }

    .eu-footer :global(.btn),
    .eu-check :global(.btn) {
      width: 100%;
    }
  }
</style>
