<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../../i18n';
  import { showToast } from '../../../stores';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Input from '../../Input.svelte';
  import Select from '../../Select.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { handleLayerError, refetchLayerState } from '../../../lib/configLayer';
  import {
    revealUrl,
    updateSource,
    type SourcePatch,
    type SourceView
  } from '../../../lib/configSources';
  import { writeErrorText } from '../../../lib/sourceErrors';

  // Настройки подписки пишутся в черновик одним запросом update, только изменёнными полями
  // (D-04, D-05, D-06, D-09). Единственный фильтр — «Исключить узлы по имени»; настроек
  // сокетов и флажков ядер нет. Полный адрес запрашивается только по «Показать» (D-33).
  const EXCLUDE_MIN_ROWS = 3;
  const EXCLUDE_MAX_ROWS = 8;

  let { open, source, onclose } = $props<{
    open: boolean;
    source: SourceView;
    onclose: () => void;
  }>();

  const uid = $props.id();

  let name = $state('');
  let exclude = $state('');
  let interval = $state('provider');
  let hwid = $state('');
  let hwidReset = $state(false);
  // Полный адрес: null — показан замаскированный; строка — после «Показать»
  let urlFull = $state<string | null>(null);
  let urlEdit = $state('');
  let urlBusy = $state(false);
  let saving = $state(false);
  let saveError = $state('');
  // Исходные значения на момент открытия: в запрос уходят только отличающиеся поля
  let init = $state({ name: '', exclude: [] as string[], interval: 'provider' });

  const options = $derived([
    { value: 'provider', label: $t('cfg.nodes.interval_provider') },
    { value: '1', label: $t('cfg.nodes.interval_1') },
    { value: '3', label: $t('cfg.nodes.interval_3') },
    { value: '6', label: $t('cfg.nodes.interval_6') },
    { value: '12', label: $t('cfg.nodes.interval_12') },
    { value: '24', label: $t('cfg.nodes.interval_24') },
    { value: 'off', label: $t('cfg.nodes.interval_off') }
  ]);

  const excludeRows = $derived(
    Math.min(EXCLUDE_MAX_ROWS, Math.max(EXCLUDE_MIN_ROWS, exclude.split('\n').length))
  );
  const hwidCustom = $derived(source.settings?.hwid_custom === true);

  // Пустые строки списка исключений игнорируются
  function excludeList(raw: string): string[] {
    return raw
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== '');
  }

  function intervalValue(): string {
    const iv = source.settings?.interval;
    if (!iv || iv.mode === 'provider') return 'provider';
    if (iv.mode === 'off') return 'off';
    return String(iv.hours ?? 12);
  }

  const patch = $derived.by(() => {
    const p: SourcePatch = {};
    if (name.trim() !== init.name) p.name = name.trim();
    const list = excludeList(exclude);
    if (JSON.stringify(list) !== JSON.stringify(init.exclude)) p.exclude = list;
    if (interval !== init.interval) {
      p.interval =
        interval === 'provider' || interval === 'off'
          ? { mode: interval }
          : { mode: 'hours', hours: Number(interval) };
    }
    if (hwidReset) p.hwid = '';
    else if (hwid.trim() !== '') p.hwid = hwid.trim();
    if (urlFull !== null && urlEdit.trim() !== urlFull) p.url = urlEdit.trim();
    return p;
  });
  const changed = $derived(Object.keys(patch).length > 0);

  function initialize() {
    name = source.name;
    const list = source.settings?.exclude ?? [];
    exclude = list.join('\n');
    interval = intervalValue();
    hwid = '';
    hwidReset = false;
    urlFull = null;
    urlEdit = '';
    urlBusy = false;
    saving = false;
    saveError = '';
    init = { name: source.name, exclude: [...list], interval: intervalValue() };
  }

  function wipe() {
    // Полный адрес не остаётся в состоянии закрытой модали
    urlFull = null;
    urlEdit = '';
    hwid = '';
    saveError = '';
  }

  $effect(() => {
    if (open) untrack(initialize);
    else untrack(wipe);
  });

  async function toggleReveal() {
    if (urlFull !== null) {
      urlFull = null;
      urlEdit = '';
      return;
    }
    urlBusy = true;
    try {
      const url = await revealUrl(source.id);
      urlFull = url;
      urlEdit = url;
    } catch (err) {
      handleLayerError(err);
    } finally {
      urlBusy = false;
    }
  }

  async function copyUrl() {
    urlBusy = true;
    try {
      const url = urlFull ?? (await revealUrl(source.id));
      await navigator.clipboard.writeText(url);
      showToast('success', $t('cfg.nodes.copied'));
    } catch (err) {
      handleLayerError(err);
    } finally {
      urlBusy = false;
    }
  }

  function close() {
    // Пока идёт запись, модаль не закрывается: ответ определит, остаются ли значения
    if (saving) return;
    onclose();
  }

  async function save() {
    if (!changed || saving) return;
    saving = true;
    saveError = '';
    try {
      await updateSource(source.id, patch);
      saving = false;
      onclose();
    } catch (e) {
      saveError = writeErrorText(e, $t);
      if ((e as { code?: unknown })?.code === 'draft_conflict') void refetchLayerState();
    } finally {
      saving = false;
    }
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.nodes.settings_title')}
  maxWidth="560px"
  dataTestid="nodes-settings-modal"
  onclose={close}
>
  <div class="ss-body">
    <div>
      <Input id="{uid}-name" label={$t('cfg.nodes.settings_name')} bind:value={name} />
      <p class="ss-hint">{$t('cfg.nodes.settings_name_hint')}</p>
    </div>

    <div class="ss-field">
      <span class="form-label">{$t('cfg.nodes.settings_url')}</span>
      {#if urlFull === null}
        <code class="ss-url" data-testid="nodes-settings-url">
          {source.url_masked ?? ''}
        </code>
        <p class="ss-hint">{$t('cfg.nodes.settings_url_hint')}</p>
      {:else}
        <textarea
          id="{uid}-url"
          class="input ss-mono"
          rows="2"
          spellcheck="false"
          autocomplete="off"
          autocapitalize="off"
          aria-label={$t('cfg.nodes.settings_url_edit')}
          bind:value={urlEdit}
          data-testid="nodes-settings-url-input"></textarea>
      {/if}
      <div class="ss-row">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          disabled={urlBusy}
          aria-pressed={urlFull !== null}
          data-testid="nodes-settings-url-reveal"
          onclick={toggleReveal}
        >
          <Icon name={urlFull !== null ? 'eye-off' : 'eye'} size={14} />
          {urlFull !== null ? $t('cfg.nodes.hide') : $t('cfg.nodes.reveal')}
        </button>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          disabled={urlBusy}
          data-testid="nodes-settings-url-copy"
          onclick={copyUrl}
        >
          <Icon name="copy" size={14} />
          {$t('cfg.nodes.copy')}
        </button>
      </div>
    </div>

    <div class="ss-field">
      <label class="form-label" for="{uid}-exclude">{$t('cfg.nodes.settings_exclude')}</label>
      <textarea
        id="{uid}-exclude"
        class="input ss-mono"
        rows={excludeRows}
        spellcheck="false"
        autocomplete="off"
        autocapitalize="off"
        bind:value={exclude}
        data-testid="nodes-settings-exclude"></textarea>
      <p class="ss-hint">{$t('cfg.nodes.settings_exclude_hint')}</p>
    </div>

    <div class="ss-field">
      <label class="form-label" for="{uid}-interval">{$t('cfg.nodes.settings_interval')}</label>
      <Select
        id="{uid}-interval"
        bind:value={interval}
        {options}
        data-testid="nodes-settings-interval"
      />
    </div>

    <details class="ss-advanced" data-testid="nodes-settings-advanced">
      <summary>{$t('cfg.nodes.advanced')}</summary>
      <div class="ss-advanced-body">
        <div class="ss-hwid">
          <Input
            id="{uid}-hwid"
            label={$t('cfg.nodes.hwid_label')}
            placeholder={$t('cfg.nodes.hwid_placeholder')}
            disabled={hwidReset}
            bind:value={hwid}
          />
        </div>
        <p class="ss-hint">{$t('cfg.nodes.hwid_hint')}</p>
        {#if hwidCustom}
          <p class="ss-hint" data-testid="nodes-settings-hwid-custom">
            {$t('cfg.nodes.hwid_custom_set')}
          </p>
          <div class="ss-row">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              aria-pressed={hwidReset}
              data-testid="nodes-settings-hwid-reset"
              onclick={() => (hwidReset = !hwidReset)}
            >
              {$t('cfg.nodes.hwid_reset')}
            </button>
            {#if hwidReset}
              <span class="ss-hint">{$t('cfg.nodes.hwid_reset_on')}</span>
            {/if}
          </div>
        {/if}
      </div>
    </details>

    {#if saveError}
      <div class="alert alert-error ss-alert" role="alert" data-testid="nodes-settings-error">
        <Icon name="warning" size={16} class="ss-alert-icon" />
        <span>{saveError}</span>
      </div>
    {/if}

    <div class="ss-footer">
      <Button
        variant="primary"
        disabled={!changed}
        loading={saving}
        title={changed ? undefined : $t('cfg.nodes.settings_no_changes')}
        data-testid="nodes-settings-save"
        onclick={save}
      >
        {$t('cfg.nodes.save_draft')}
      </Button>
      <Button variant="secondary" disabled={saving} onclick={close}>
        {$t('cfg.nodes.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .ss-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .ss-body :global(.form-group) {
    margin-bottom: 0;
  }

  .ss-field {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .ss-hint {
    margin: var(--spacing-1) 0 0;
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.4;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .ss-field > .ss-hint {
    margin-top: 0;
  }

  .ss-url {
    display: block;
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .ss-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
  }

  textarea.ss-mono {
    width: 100%;
    height: auto;
    min-height: 0;
    padding: var(--spacing-2) var(--spacing-3);
    resize: vertical;
    overflow-y: auto;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    overflow-wrap: anywhere;
    word-break: break-word;
    white-space: pre-wrap;
  }

  .ss-advanced {
    min-width: 0;
  }

  .ss-advanced summary {
    cursor: pointer;
    min-height: 32px;
    display: flex;
    align-items: center;
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .ss-advanced-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    padding-top: var(--spacing-2);
  }

  .ss-hwid :global(.input) {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    overflow-wrap: anywhere;
  }

  .ss-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ss-alert :global(.ss-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .ss-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .ss-advanced summary {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .ss-footer :global(.btn) {
      flex: 1 1 auto;
    }
  }
</style>
