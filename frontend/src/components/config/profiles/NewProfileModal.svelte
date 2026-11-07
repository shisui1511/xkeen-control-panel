<script lang="ts">
  import { untrack } from 'svelte';
  import { t, tp } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Select from '../../Select.svelte';
  import SegmentedControl, { type SegmentItem } from '../../SegmentedControl.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    PROFILE_TEMPLATES,
    createProfile,
    previewProfile,
    profileErrorText,
    type ProfilePreview,
    type ProfilesState
  } from '../../../lib/configProfiles';

  // Диалог «Новый профиль» (PART-02, D-01…D-04, D-11, D-24): профиль из одного из восьми
  // шаблонов либо копией любого профиля. До «Создать профиль» ничего не пишется: сводка
  // «Что войдёт в профиль» приходит с сервера (предпросмотр без записи на диск) и без неё
  // создание недоступно. Профиль сам не активируется: после успеха диалог закрывается,
  // а карточка «Профили» ставит фокус на новую строку и предлагает «Сделать активным».
  let {
    open = $bindable(false),
    startMode = 'template',
    startFrom = '',
    profiles,
    oncreated
  }: {
    open?: boolean;
    /** Сегмент при открытии: «Из шаблона» или «Копия профиля». */
    startMode?: 'template' | 'copy';
    /** Имя копируемого профиля при открытии (кнопка «Копировать» на строке). */
    startFrom?: string;
    profiles: ProfilesState | null;
    /** Профиль создан; state — обновлённый список профилей из ответа. */
    oncreated: (name: string, state: ProfilesState) => void | Promise<void>;
  } = $props();

  const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$/;
  const PANEL_NAME = 'xcp-main';
  const PLAIN_NAME = 'config.yaml';
  const DEFAULT_TEMPLATE = 'rule-based';

  const uid = $props.id();
  const nameId = `${uid}-name`;
  const nameErrId = `${nameId}-error`;
  const sourceId = `${uid}-source`;
  const sourceHintId = `${uid}-source-hint`;
  const radioGroup = `${uid}-tpl`;

  let name = $state('');
  let mode = $state<'template' | 'copy'>('template');
  let template = $state(DEFAULT_TEMPLATE);
  let source = $state('');
  let serverNameError = $state('');

  let preview = $state<ProfilePreview | null>(null);
  let previewLoading = $state(false);
  let previewFailed = $state(false);
  let busy = $state(false);
  let createError = $state<{ reason: string; detail: string } | null>(null);
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  const panelPresent = $derived(profiles?.profiles.some((p) => p.owner === 'panel') === true);
  const userNames = $derived(
    (profiles?.profiles ?? []).filter((p) => p.owner === 'user').map((p) => p.name)
  );
  const plainPresent = $derived(profiles?.plain_config != null);

  // Источники копии: профили пользователя, config.yaml (обычный файл), профиль панели
  const sourceOptions = $derived([
    ...userNames.map((n) => ({ value: n, label: n, disabled: false })),
    ...(plainPresent ? [{ value: PLAIN_NAME, label: PLAIN_NAME, disabled: false }] : []),
    {
      value: PANEL_NAME,
      label: panelPresent ? $t('cfg.prof.panel_name') : $t('cfg.prof.copy_panel_opt_off'),
      disabled: !panelPresent
    }
  ]);
  const firstSource = $derived(sourceOptions.find((o) => !o.disabled)?.value ?? '');
  const templateFile = $derived(
    PROFILE_TEMPLATES.find((p) => p.id === template)?.file ?? `${DEFAULT_TEMPLATE}.yaml`
  );

  const modeItems = $derived<SegmentItem[]>([
    { value: 'template', label: $t('cfg.prof.src_template'), testId: 'new-profile-mode-template' },
    { value: 'copy', label: $t('cfg.prof.src_copy'), testId: 'new-profile-mode-copy' }
  ]);

  // Ошибка имени показывается после первого символа: формат, префикс панели, дубликат
  const nameError = $derived.by(() => {
    if (name === '') return '';
    if (!NAME_RE.test(name) || /\.ya?ml$/i.test(name)) return $t('profiles.name_rules');
    if (/^xcp-/i.test(name)) return $t('cfg.prof.err_reserved');
    if (userNames.includes(name) || name === PLAIN_NAME) {
      return $t('cfg.prof.err_exists', { name });
    }
    return serverNameError;
  });
  const nameValid = $derived(name !== '' && nameError === '');
  const canCreate = $derived(
    nameValid &&
      !busy &&
      !previewLoading &&
      !previewFailed &&
      preview !== null &&
      (mode === 'template' || source !== '')
  );

  async function loadPreview(query: { template?: string; from?: string } | null): Promise<void> {
    const mine = ++seq;
    preview = null;
    previewFailed = false;
    if (query === null) {
      previewLoading = false;
      return;
    }
    previewLoading = true;
    try {
      const data = await previewProfile(query);
      if (mine !== seq) return;
      preview = data;
    } catch {
      if (mine !== seq) return;
      previewFailed = true;
    } finally {
      if (mine === seq) previewLoading = false;
    }
  }

  function currentQuery(): { template?: string; from?: string } | null {
    if (mode === 'template') return { template: templateFile };
    return source !== '' ? { from: source } : null;
  }

  function reset(): void {
    seq++;
    name = '';
    serverNameError = '';
    preview = null;
    previewLoading = false;
    previewFailed = false;
    busy = false;
    createError = null;
  }

  function init(): void {
    reset();
    template = DEFAULT_TEMPLATE;
    mode = startMode;
    const wanted = sourceOptions.find((o) => o.value === startFrom && !o.disabled);
    source = wanted ? wanted.value : firstSource;
  }

  $effect(() => {
    if (open) untrack(init);
    else untrack(reset);
  });

  // Предпросмотр на каждую смену выбора; пока открыт диалог, читаются только выбор и источник
  $effect(() => {
    if (!open) return;
    const query =
      mode === 'template' ? { template: templateFile } : source !== '' ? { from: source } : null;
    untrack(() => void loadPreview(query));
  });

  function onname(): void {
    serverNameError = '';
    createError = null;
  }

  function close(): void {
    // Пока идёт создание, диалог не закрывается: ответ определит, что записано на диск
    if (busy) return;
    open = false;
  }

  async function create(): Promise<void> {
    if (!canCreate) return;
    busy = true;
    createError = null;
    const made = name;
    try {
      const state = await createProfile(
        mode === 'template' ? { name: made, template: templateFile } : { name: made, from: source }
      );
      open = false;
      busy = false;
      await oncreated(made, state);
    } catch (e) {
      const err = e as { code?: string; detail?: string } | null;
      if (err?.code === 'profile_exists' || err?.code === 'profile_reserved') {
        serverNameError = profileErrorText($t, e, made);
      } else {
        createError = {
          reason: profileErrorText($t, e, made),
          detail: typeof err?.detail === 'string' ? err.detail : ''
        };
      }
    } finally {
      busy = false;
    }
  }

  function retryPreview(): void {
    void loadPreview(currentQuery());
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.prof.modal_title')}
  maxWidth="640px"
  dataTestid="new-profile-modal"
  onclose={close}
>
  <div class="np-body">
    <div class="np-scroll" data-testid="new-profile-scroll">
      <div class="form-group np-field">
        <label class="form-label" for={nameId}>{$t('cfg.prof.name_label')}</label>
        <input
          id={nameId}
          class="input np-name"
          bind:value={name}
          oninput={onname}
          autocomplete="off"
          spellcheck="false"
          disabled={busy}
          aria-invalid={nameError !== '' ? 'true' : 'false'}
          aria-describedby={nameError !== '' ? nameErrId : undefined}
          data-testid="new-profile-name"
        />
        {#if nameError !== ''}
          <span class="error-text" id={nameErrId} data-testid="new-profile-name-error">
            {nameError}
          </span>
        {/if}
      </div>

      <div class="np-mode" data-testid="new-profile-mode">
        <SegmentedControl
          items={modeItems}
          bind:value={mode}
          ariaLabel={$t('cfg.prof.modal_title')}
        />
      </div>

      {#if mode === 'template'}
        <div
          class="np-tpls"
          role="radiogroup"
          aria-label={$t('cfg.prof.src_template')}
          data-testid="new-profile-templates"
        >
          {#each PROFILE_TEMPLATES as tpl (tpl.id)}
            <label class="np-tpl" class:selected={template === tpl.id}>
              <input
                type="radio"
                name={radioGroup}
                value={tpl.id}
                bind:group={template}
                disabled={busy}
                data-testid="new-profile-template-{tpl.id}"
              />
              <span class="np-tpl-text">
                <span class="np-tpl-name">{$t(`cfg.prof.tpl.${tpl.id}.name`)}</span>
                <span class="np-tpl-desc">{$t(`cfg.prof.tpl.${tpl.id}.desc`)}</span>
              </span>
            </label>
          {/each}
        </div>
      {:else}
        <div class="form-group np-field">
          <label class="form-label" for={sourceId}>{$t('cfg.prof.copy_from')}</label>
          <Select
            id={sourceId}
            bind:value={source}
            options={sourceOptions}
            disabled={busy}
            data-testid="new-profile-source"
          />
          {#if !panelPresent}
            <span class="np-hint" id={sourceHintId} data-testid="new-profile-source-hint">
              {$t('cfg.prof.copy_panel_off')}
            </span>
          {/if}
        </div>
      {/if}

      <section
        class="np-summary"
        aria-labelledby="{uid}-sum-title"
        aria-busy={previewLoading}
        data-testid="new-profile-summary"
      >
        <h3 class="np-sum-title" id="{uid}-sum-title">{$t('cfg.prof.sum_title')}</h3>
        {#if previewLoading}
          <div class="np-sum-skeleton" aria-hidden="true" data-testid="new-profile-summary-loading">
            <Skeleton type="text-line" />
            <Skeleton type="text-line" width="70%" />
          </div>
        {:else if previewFailed}
          <div
            class="alert alert-error np-alert"
            role="alert"
            data-testid="new-profile-preview-error"
          >
            <Icon name="warning" size={16} class="np-alert-icon" />
            <span class="np-alert-text">{$t('cfg.prof.err_preview')}</span>
            <Button
              variant="secondary"
              data-testid="new-profile-preview-retry"
              onclick={retryPreview}
            >
              {$t('cfg.load_error_retry')}
            </Button>
          </div>
        {:else if preview}
          <ul class="np-sum-list">
            {#if preview.kind === 'template'}
              {#if preview.no_sources}
                <li class="np-sum-wide">
                  <div class="alert alert-warning np-alert" data-testid="new-profile-no-sources">
                    <Icon name="warning" size={16} class="np-alert-icon" />
                    <span class="np-alert-text">{$t('cfg.prof.sum_no_sources')}</span>
                  </div>
                </li>
              {:else}
                <li class="np-sum-row" data-testid="new-profile-sum-nodes">
                  <Icon name="check" size={16} class="np-sum-icon" />
                  <span>
                    {$tp('cfg.prof.sum_nodes', preview.nodes ?? 0, {
                      sources: preview.sources ?? 0
                    })}
                  </span>
                </li>
              {/if}
              <li class="np-sum-row" data-testid="new-profile-sum-added">
                <Icon name="check" size={16} class="np-sum-icon" />
                <span>
                  {$t('cfg.prof.sum_added', {
                    redir: preview.redir ?? 0,
                    tproxy: preview.tproxy ?? 0
                  })}
                </span>
              </li>
              <li class="np-sum-row" data-testid="new-profile-sum-check">
                <Icon name="check" size={16} class="np-sum-icon" />
                <span>{$t('cfg.prof.sum_check')}</span>
              </li>
              {#if preview.unix_unsupported}
                <li class="np-sum-wide">
                  <span class="warning-text np-unix" data-testid="new-profile-unix-old">
                    {$t('cfg.prof.sum_unix_old')}
                  </span>
                </li>
              {/if}
            {:else if preview.kind === 'copy_panel'}
              <li class="np-sum-row" data-testid="new-profile-sum-copy">
                <Icon name="check" size={16} class="np-sum-icon" />
                <span>{$t('cfg.prof.sum_copy_panel')}</span>
              </li>
            {:else}
              <li class="np-sum-row" data-testid="new-profile-sum-copy">
                <Icon name="check" size={16} class="np-sum-icon" />
                <span>{$t('cfg.prof.sum_copy_user')}</span>
              </li>
            {/if}
          </ul>
        {/if}
      </section>
    </div>

    {#if createError}
      <div class="alert alert-error np-alert" role="alert" data-testid="new-profile-error">
        <Icon name="warning" size={16} class="np-alert-icon" />
        <div class="np-fail">
          <span>{$t('cfg.prof.err_create', { reason: createError.reason })}</span>
          {#if createError.detail !== ''}
            <pre class="np-pre" data-testid="new-profile-error-output">{createError.detail}</pre>
          {/if}
        </div>
      </div>
    {/if}

    <div class="np-footer">
      <span class="np-creating" aria-live="polite" data-testid="new-profile-creating">
        {busy ? $t('cfg.prof.creating') : ''}
      </span>
      <Button
        variant="primary"
        disabled={!canCreate}
        loading={busy}
        data-testid="new-profile-confirm"
        onclick={() => void create()}
      >
        {$t('cfg.prof.create_btn')}
      </Button>
      <Button variant="secondary" disabled={busy} data-testid="new-profile-cancel" onclick={close}>
        {$t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .np-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  /* Восемь карточек шаблонов прокручиваются в теле диалога, кнопки остаются на виду */
  .np-scroll {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
    max-height: 50dvh;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .np-field {
    margin: 0;
    min-width: 0;
  }

  .np-name {
    font-family: var(--font-family-mono);
  }

  .np-hint {
    display: block;
    margin-top: var(--spacing-1);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .np-mode {
    display: flex;
    min-width: 0;
  }

  .np-tpls {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .np-tpl {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-3);
    min-height: 44px;
    padding: var(--spacing-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-card-subtle);
    cursor: pointer;
    min-width: 0;
  }

  .np-tpl.selected {
    border-color: var(--accent-border);
    background: var(--accent-soft);
  }

  .np-tpl input {
    flex-shrink: 0;
    width: 18px;
    height: 18px;
    margin: 1px 0 0;
    accent-color: var(--accent);
  }

  .np-tpl-text {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .np-tpl-name {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .np-tpl-desc {
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  /* На подсвеченном фоне выбранной карточки вторичный цвет не дотягивает до 4,5:1. */
  .np-tpl.selected .np-tpl-desc {
    color: var(--fg-primary);
  }

  .np-summary {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    padding: var(--spacing-4);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-md);
    background: var(--bg-elevated);
    min-width: 0;
  }

  .np-sum-title {
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .np-sum-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .np-sum-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .np-sum-row {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
    min-width: 0;
  }

  .np-sum-row :global(.np-sum-icon) {
    flex-shrink: 0;
    margin-top: 3px;
    color: var(--success);
  }

  .np-sum-wide {
    min-width: 0;
  }

  .np-unix {
    margin: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .np-alert {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .np-alert :global(.np-alert-icon) {
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
  }

  .np-alert-text {
    flex: 1 1 12rem;
    min-width: 0;
  }

  .np-fail {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    flex: 1 1 auto;
  }

  /* Вывод ядра: ограниченная высота и прокрутка, длинные строки переносятся */
  .np-pre {
    margin: 0;
    padding: var(--spacing-2);
    max-height: 160px;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    background: var(--bg-elevated);
    border-radius: var(--radius-sm);
    scrollbar-width: thin;
  }

  .np-footer {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  .np-creating {
    margin-right: auto;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .np-footer :global(.btn),
    .np-alert :global(.btn) {
      min-height: 44px;
    }

    .np-tpl {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .np-footer :global(.btn),
    .np-alert :global(.btn) {
      flex: 1 1 100%;
    }

    .np-creating {
      flex: 1 1 100%;
      order: 3;
      margin-right: 0;
    }
  }
</style>
