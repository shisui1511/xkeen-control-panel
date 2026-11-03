<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Select from '../../Select.svelte';
  import SegmentedControl, { type SegmentItem } from '../../SegmentedControl.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { handleLayerError } from '../../../lib/configLayer';
  import { coresErrorText } from '../../../lib/configCores';
  import { PROFILE_TEMPLATES, fillSections } from '../../../lib/configProfiles';
  import { showToast } from '../../../stores';

  // Диалог «Заполнить из профиля» (D-18 фазы 147, D-09): значения секций Mihomo профиля
  // панели берутся из шаблона или профиля пользователя и уходят в черновик. Маршрутизация
  // не переносится, в файл значения попадут при «Применить». Открытый черновик не мешает:
  // это правка черновика, а не применение.
  let {
    open = $bindable(false),
    profileNames
  }: {
    open?: boolean;
    /** Профили пользователя, из которых можно взять значения. */
    profileNames: string[];
  } = $props();

  const uid = $props.id();
  const selectId = `${uid}-select`;
  const hintId = `${uid}-hint`;
  const DEFAULT_TEMPLATE = PROFILE_TEMPLATES[0].id;

  let mode = $state<'template' | 'profile'>('template');
  let template = $state(DEFAULT_TEMPLATE);
  let profile = $state('');
  let busy = $state(false);
  let failure = $state('');

  const noProfiles = $derived(profileNames.length === 0);
  const modeItems = $derived<SegmentItem[]>([
    {
      value: 'template',
      label: $t('cfg.prof.fill_src_template'),
      testId: 'fill-sections-mode-template'
    },
    {
      value: 'profile',
      label: $t('cfg.prof.fill_src_profile'),
      testId: 'fill-sections-mode-profile'
    }
  ]);
  const templateOptions = $derived(
    PROFILE_TEMPLATES.map((p) => ({ value: p.id, label: $t(`cfg.prof.tpl.${p.id}.name`) }))
  );
  const profileOptions = $derived(profileNames.map((n) => ({ value: n, label: n })));
  const chosen = $derived(mode === 'template' ? template : profile);
  const canFill = $derived(!busy && chosen !== '' && !(mode === 'profile' && noProfiles));

  function init(): void {
    mode = 'template';
    template = DEFAULT_TEMPLATE;
    profile = profileNames[0] ?? '';
    busy = false;
    failure = '';
  }

  $effect(() => {
    if (open) untrack(init);
  });

  function close(): void {
    // Пока идёт запрос, диалог не закрывается: ответ определит, что записано в черновик
    if (busy) return;
    open = false;
  }

  async function fill(): Promise<void> {
    if (!canFill) return;
    busy = true;
    failure = '';
    try {
      if (mode === 'template') {
        const file = PROFILE_TEMPLATES.find((p) => p.id === template)?.file ?? '';
        await fillSections({ template: file });
      } else {
        await fillSections({ profile });
      }
    } catch (e) {
      const err = e as { code?: string; params?: Record<string, string> } | null;
      if (err?.code === 'draft_conflict') {
        // Устаревшая ревизия: тост и перечитывание, диалог закрывается
        handleLayerError(e);
        busy = false;
        open = false;
        return;
      }
      failure = $t('cfg.prof.fill_error', { reason: coresErrorText($t, err?.code, err?.params) });
      return;
    } finally {
      busy = false;
    }
    open = false;
    showToast('success', $t('cfg.prof.filled_toast'), 8000);
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.prof.fill_title')}
  maxWidth="560px"
  dataTestid="fill-sections-modal"
  onclose={close}
>
  <div class="fs-body">
    <div class="fs-scroll">
      <div class="fs-mode">
        <SegmentedControl
          items={modeItems}
          bind:value={mode}
          ariaLabel={$t('cfg.prof.fill_title')}
        />
      </div>

      <div class="form-group fs-field">
        <label class="form-label" for={selectId}>{$t('cfg.prof.fill_select')}</label>
        {#if mode === 'template'}
          <Select
            id={selectId}
            bind:value={template}
            options={templateOptions}
            disabled={busy}
            data-testid="fill-sections-select"
          />
        {:else}
          <Select
            id={selectId}
            bind:value={profile}
            options={profileOptions}
            disabled={busy || noProfiles}
            data-testid="fill-sections-select"
          />
          {#if noProfiles}
            <span class="fs-hint" id={hintId} data-testid="fill-sections-no-profiles">
              {$t('cfg.prof.fill_no_profiles')}
            </span>
          {/if}
        {/if}
      </div>

      <p class="fs-text">{$t('cfg.prof.fill_body')}</p>

      <div class="alert alert-warning fs-alert" data-testid="fill-sections-warning">
        <Icon name="warning" size={16} class="fs-alert-icon" />
        <span>{$t('cfg.prof.fill_warn')}</span>
      </div>
    </div>

    {#if failure !== ''}
      <div class="alert alert-error fs-alert" role="alert" data-testid="fill-sections-error">
        <Icon name="warning" size={16} class="fs-alert-icon" />
        <span>{failure}</span>
      </div>
    {/if}

    <div class="fs-footer">
      <Button
        variant="primary"
        disabled={!canFill}
        loading={busy}
        data-testid="fill-sections-confirm"
        onclick={() => void fill()}
      >
        {$t('cfg.prof.fill_btn')}
      </Button>
      <Button
        variant="secondary"
        disabled={busy}
        data-testid="fill-sections-cancel"
        onclick={close}
      >
        {$t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .fs-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  /* Тело диалога прокручивается, кнопки остаются на виду */
  .fs-scroll {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
    max-height: 50dvh;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .fs-mode {
    display: flex;
    min-width: 0;
  }

  .fs-field {
    margin: 0;
    min-width: 0;
  }

  .fs-hint {
    display: block;
    margin-top: var(--spacing-1);
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .fs-text {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .fs-alert {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .fs-alert :global(.fs-alert-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .fs-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .fs-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .fs-footer :global(.btn) {
      flex: 1 1 100%;
    }
  }
</style>
