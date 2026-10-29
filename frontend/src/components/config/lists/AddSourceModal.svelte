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
    type WriteOp
  } from '../../../lib/configGeodata';

  // Диалог «Добавить источник»: имя файла и ссылка, проверка тем же путём, что загрузка XKeen,
  // diff записи в xkeen.json и выбор «обновить сейчас / дождаться расписания». «Добавить»
  // активна, только если проверка проведена на текущих значениях полей и блоков нет; любая
  // правка поля сбрасывает результат и предпросмотр. Сервер повторяет проверку при записи.
  let {
    open,
    onadded,
    onclose
  }: {
    open: boolean;
    /** Запись выполнена (в том числе при сбое запуска xkeen -ug): снимок нужно перечитать. */
    onadded: () => void;
    onclose: () => void;
  } = $props();

  const uid = $props.id();

  let file = $state('');
  let url = $state('');
  // Результат хранится вместе со значениями, на которых сделан
  let checked = $state<{ file: string; url: string; probe: ProbeResult } | null>(null);
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

  const fileValue = $derived(file.trim());
  const urlValue = $derived(url.trim());
  const filled = $derived(fileValue !== '' && urlValue !== '');
  // Результат относится к текущим значениям полей
  const fresh = $derived(
    checked !== null && checked.file === fileValue && checked.url === urlValue ? checked : null
  );
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
    file = '';
    url = '';
    busy = false;
    written = false;
    updateMode = 'later';
  }

  $effect(() => {
    if (!open) untrack(reset);
  });

  function currentOp(): WriteOp {
    return { kind: 'add', file: fileValue, url: urlValue };
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
    checked = { file: op.file ?? '', url: op.url ?? '', probe: err.data.probe };
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
      const probe = await probeSource(fileValue, urlValue, 'add');
      if (mine !== seq) return;
      checked = { file: fileValue, url: urlValue, probe };
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
      showToast('success', get(t)('cfg.geo.add_done'));
      onadded();
      onclose();
    } catch (e) {
      const err = e as { code?: string } | null;
      if (err?.code === 'update_failed') {
        // Запись выполнена, не удался только xkeen -ug: файл уже изменён
        writeFailure = geodataErrorText(e);
        written = true;
        onadded();
      } else if (applyBlocked(e, op)) {
        // Сервер повторил проверку и нашёл блок: запись не сделана
        writeFailure = geodataErrorText(e);
      } else {
        writeFailure = geodataErrorText(e);
        if (err?.code === 'geodata_changed') {
          // Файл изменился после показанного предпросмотра: показывается новый diff
          await loadPreview(op);
        }
      }
    } finally {
      busy = false;
    }
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.geo.add_title')}
  maxWidth="720px"
  dataTestid="geo-add-modal"
  onclose={close}
>
  <div class="am-body">
    <div class="am-field" data-testid="geo-add-file">
      <Input
        id="{uid}-file"
        label={$t('cfg.geo.add_file')}
        placeholder="my-geosite.dat"
        bind:value={file}
        oninput={resetResult}
        disabled={busy || written}
      />
    </div>
    <div class="am-field am-mono" data-testid="geo-add-url">
      <Input
        id="{uid}-url"
        label={$t('cfg.geo.add_url')}
        placeholder="https://example.org/geosite.dat"
        bind:value={url}
        oninput={resetResult}
        disabled={busy || written}
      />
    </div>

    <div class="am-check">
      <Button
        variant="secondary"
        disabled={!filled || busy || written}
        loading={checking}
        title={filled ? undefined : $t('cfg.geo.add_fill_hint')}
        data-testid="geo-add-check"
        onclick={check}
      >
        {$t('cfg.geo.add_check')}
      </Button>
      <p class="am-hint">{$t('cfg.geo.add_check_hint')}</p>
    </div>

    {#if checkFailure !== ''}
      <div class="alert alert-error am-alert" role="alert" data-testid="geo-add-check-error">
        <Icon name="warning" size={16} class="am-alert-icon" />
        <span>{checkFailure}</span>
      </div>
    {/if}

    <ProbeResults result={fresh?.probe ?? null} {ghProxy} />

    {#if previewLoading}
      <div class="am-state" role="status" data-testid="geo-add-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span>{$t('cfg.list.loading')}</span>
      </div>
    {:else if previewFailure !== ''}
      <div class="alert alert-error am-alert" role="alert" data-testid="geo-add-preview-error">
        <Icon name="warning" size={16} class="am-alert-icon" />
        <span>{previewFailure}</span>
      </div>
    {:else if preview && fresh !== null && !blocked}
      <div class="am-diff" data-testid="geo-add-diff">
        <DiffView
          id="{uid}-diff"
          expected={preview.after}
          actual={preview.before}
          missing={preview.before.trim() === ''}
        />
      </div>
    {/if}

    <div class="am-radios" role="radiogroup" aria-label={$t('cfg.geo.add_title')}>
      <label class="am-radio">
        <input
          type="radio"
          name="{uid}-update"
          value="later"
          checked={updateMode === 'later'}
          disabled={busy || written}
          data-testid="geo-add-update-later"
          onchange={() => (updateMode = 'later')}
        />
        <span>{$t('cfg.geodata.update_later')}</span>
      </label>
      <label class="am-radio">
        <input
          type="radio"
          name="{uid}-update"
          value="now"
          checked={updateMode === 'now'}
          disabled={busy || written}
          data-testid="geo-add-update-now"
          onchange={() => (updateMode = 'now')}
        />
        <span>{$t('cfg.geodata.update_now')}</span>
      </label>
    </div>

    {#if writeFailure !== ''}
      <div class="alert alert-error am-alert" role="alert" data-testid="geo-add-error">
        <Icon name="warning" size={16} class="am-alert-icon" />
        <span>{writeFailure}</span>
      </div>
    {/if}

    <div class="am-footer">
      <Button
        variant="primary"
        disabled={!canSubmit}
        loading={busy}
        data-testid="geo-add-submit"
        onclick={submit}
      >
        {$t('cfg.geo.add_submit')}
      </Button>
      <Button variant="secondary" disabled={busy} data-testid="geo-add-cancel" onclick={close}>
        {written ? $t('app.close') : $t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .am-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .am-field {
    min-width: 0;
  }

  /* Ссылка — на всю ширину, моноширинным шрифтом */
  .am-mono :global(.input) {
    font-family: var(--font-family-mono);
  }

  .am-check {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .am-hint {
    flex: 1 1 240px;
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  /* Высота области не прыгает между чтением, ошибкой и diff */
  .am-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-2);
    min-height: 120px;
    color: var(--fg-secondary);
  }

  .am-diff {
    min-width: 0;
  }

  .am-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .am-alert :global(.am-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .am-radios {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .am-radio {
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

  .am-radio:hover {
    background: var(--bg-hover);
  }

  .am-radio input {
    flex-shrink: 0;
    margin-top: 3px;
    accent-color: var(--accent);
  }

  .am-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .am-radio {
      min-height: 44px;
      align-items: center;
    }

    .am-radio input {
      margin-top: 0;
    }

    .am-check :global(.btn),
    .am-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .am-footer {
      flex-direction: column;
    }

    .am-footer :global(.btn),
    .am-check :global(.btn) {
      width: 100%;
    }
  }
</style>
