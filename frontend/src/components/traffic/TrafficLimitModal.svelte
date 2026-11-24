<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { showToast } from '../../stores';
  import {
    trafficLimits,
    saveLimit,
    limitsErrorText,
    limitsErrorMeta,
    splitLimitBytes,
    LIMIT_UNITS,
    type LimitAction,
    type LimitGroup,
    type LimitPeriod,
    type LimitUnit,
    type LimitView
  } from '../../lib/trafficLimits';
  import Modal from '../Modal.svelte';
  import Input from '../Input.svelte';
  import Select from '../Select.svelte';
  import Toggle from '../Toggle.svelte';
  import Button from '../Button.svelte';

  interface Props {
    /** Правка лимита; null — новый лимит. */
    limit: LimitView | null;
    /** ID группы для предвыбора цели нового лимита (переход из карточки группы). */
    presetGroup?: string;
    /** Активное ядро из списка лимитов: xray | mihomo | ''. */
    kernel: string;
    onclose: () => void;
    onsaved: () => void;
  }

  let { limit, presetGroup = '', kernel, onclose, onsaved }: Props = $props();

  // Значения формы при открытии: компонент монтируется заново на каждое открытие
  function initialForm() {
    const split = limit
      ? splitLimitBytes(limit.limit_bytes)
      : { value: 10, unit: 'GB' as LimitUnit };
    return {
      name: limit?.name ?? '',
      target: limit ? (limit.target_type === 'all' ? 'all' : limit.target_id) : presetGroup,
      value: split.value as number | null,
      unit: split.unit as string,
      period: (limit?.period ?? 'monthly') as string,
      threshold: (limit?.threshold ?? 80) as number | null,
      action: (limit?.action ?? 'notify') as string,
      enabled: limit?.enabled ?? true
    };
  }

  const init = initialForm();
  let formName = $state(init.name);
  let formTarget = $state(init.target);
  let formValue = $state<number | null>(init.value);
  let formUnit = $state(init.unit);
  let formPeriod = $state(init.period);
  let formThreshold = $state<number | null>(init.threshold);
  let formAction = $state(init.action);
  let formEnabled = $state(init.enabled);

  let saving = $state(false);
  let submitted = $state(false);

  // Группы применённого состояния маршрутизации по стабильному ID (ответ списка лимитов)
  let groups = $derived<LimitGroup[]>($trafficLimits?.groups ?? []);
  let groupsLoaded = $derived($trafficLimits !== null);
  let targetGroup = $derived(groups.find((g) => g.id === formTarget));
  // Цели нет среди групп: у правки — группа удалена из слоя, у нового лимита — неизвестный
  // ID из адреса (T-152-24, без предвыбора). В обоих случаях требуется выбрать цель.
  let targetUnknown = $derived(
    groupsLoaded && formTarget !== '' && formTarget !== 'all' && targetGroup === undefined
  );
  let targetMissing = $derived(targetUnknown && limit !== null);
  let autoTarget = $derived(targetGroup?.mode === 'auto' || targetGroup?.mode === 'fallback');
  let switchingAction = $derived(formAction === 'block' || formAction === 'direct');

  function roleLabel(g: LimitGroup): string {
    const key =
      g.mode === 'auto'
        ? 'traffic.limits.role_auto'
        : g.mode === 'fallback'
          ? 'traffic.limits.role_fallback'
          : 'traffic.limits.role_manual';
    return $t(key, { name: g.name });
  }

  let targetOptions = $derived([
    { value: '', label: $t('traffic.limits.err_target'), disabled: true },
    { value: 'all', label: $t('traffic.limits.target_all') },
    ...groups.map((g) => ({ value: g.id, label: roleLabel(g) })),
    ...(targetMissing
      ? [{ value: formTarget, label: $t('traffic.limits.state_group_deleted'), disabled: true }]
      : [])
  ]);

  let unitOptions = LIMIT_UNITS.map((u) => ({ value: u.value, label: u.value }));

  let periodOptions = $derived([
    { value: 'daily', label: $t('trafficquotas.period_daily') },
    { value: 'weekly', label: $t('trafficquotas.period_weekly') },
    { value: 'monthly', label: $t('trafficquotas.period_monthly') }
  ]);

  // D-13: у групп «авто» и «резерв» узел выбирает ядро — доступно только «Уведомить»
  let actionOptions = $derived([
    { value: 'notify', label: $t('traffic.limits.action_notify') },
    { value: 'block', label: $t('traffic.limits.action_block'), disabled: autoTarget },
    { value: 'direct', label: $t('traffic.limits.action_direct'), disabled: autoTarget }
  ]);

  // Сохранение лимита с переключением всего трафика или ручной группы под Mihomo само
  // перечитывает профиль (сверка служебного хвоста); пользователь узнаёт об этом заранее
  let showMihomoReload = $derived(
    kernel === 'mihomo' &&
      switchingAction &&
      (formTarget === 'all' || targetGroup?.mode === 'manual')
  );

  let nameError = $derived(
    submitted && formName.trim() === '' ? $t('traffic.limits.err_name') : ''
  );
  let limitError = $derived(
    submitted && !(typeof formValue === 'number' && formValue > 0)
      ? $t('traffic.limits.err_limit')
      : ''
  );
  let targetError = $derived(
    submitted && (formTarget === '' || targetUnknown) ? $t('traffic.limits.err_target') : ''
  );

  // Новая цель — группа «авто»/«резерв»: выбранная блокировка или «напрямую» сбрасывается
  function onTargetChange(e: Event & { currentTarget: HTMLSelectElement }) {
    const g = groups.find((x) => x.id === e.currentTarget.value);
    if ((g?.mode === 'auto' || g?.mode === 'fallback') && formAction !== 'notify') {
      formAction = 'notify';
    }
  }

  onMount(() => {
    // Фокус — на первое поле формы (Modal ставит его на первый фокусируемый элемент — крестик)
    const timer = setTimeout(() => document.getElementById('traffic-limit-name')?.focus(), 20);
    return () => clearTimeout(timer);
  });

  async function save() {
    if (saving) return;
    submitted = true;
    if (nameError || limitError || targetError) return;
    const unit = LIMIT_UNITS.find((u) => u.value === formUnit) ?? LIMIT_UNITS[1];
    const threshold = Math.min(99, Math.max(1, Math.round(Number(formThreshold) || 80)));
    saving = true;
    try {
      await saveLimit({
        id: limit?.id ?? '',
        name: formName.trim(),
        target_type: formTarget === 'all' ? 'all' : 'group',
        target_id: formTarget === 'all' ? '' : formTarget,
        limit_bytes: Math.round((formValue ?? 0) * unit.bytes),
        period: formPeriod as LimitPeriod,
        threshold,
        action: (autoTarget ? 'notify' : formAction) as LimitAction,
        enabled: formEnabled
      });
      onsaved();
    } catch (e) {
      if ((e as { status?: unknown } | null)?.status === 401) return;
      const meta = limitsErrorMeta(e);
      showToast('error', limitsErrorText($t, meta.code, meta.params, meta.message));
    } finally {
      saving = false;
    }
  }
</script>

<Modal
  open={true}
  title={limit ? $t('traffic.limits.edit_title') : $t('traffic.limits.new_title')}
  maxWidth="520px"
  closeDisabled={saving}
  {onclose}
>
  <form
    class="limit-form"
    data-testid="traffic-limit-modal"
    novalidate
    onsubmit={(e) => {
      e.preventDefault();
      save();
    }}
  >
    <Input
      id="traffic-limit-name"
      label={$t('traffic.limits.field_name')}
      bind:value={formName}
      maxlength={64}
      error={nameError}
      testid="traffic-limit-field-name"
    />

    <div class="form-group">
      <label class="form-label" for="traffic-limit-target"
        >{$t('traffic.limits.field_target')}</label
      >
      <Select
        id="traffic-limit-target"
        bind:value={formTarget}
        options={targetOptions}
        onchange={onTargetChange}
        ariaInvalid={targetError !== ''}
        ariaDescribedby={targetError
          ? 'traffic-limit-target-error'
          : targetMissing
            ? 'traffic-limit-target-warning'
            : undefined}
        data-testid="traffic-limit-field-target"
      />
      {#if targetError}
        <span class="error-text" id="traffic-limit-target-error">{targetError}</span>
      {:else if targetMissing}
        <span class="warning-text" id="traffic-limit-target-warning">
          {$t('traffic.limits.state_group_deleted')}
        </span>
      {/if}
    </div>

    <div class="limit-form-row">
      <Input
        id="traffic-limit-value"
        type="number"
        label={$t('traffic.limits.field_limit')}
        bind:value={formValue}
        min="0.001"
        step="any"
        inputmode="decimal"
        error={limitError}
        hint={kernel === 'xray' ? $t('traffic.limits.hint_xray_sum') : ''}
        testid="traffic-limit-field-limit"
      />
      <div class="form-group">
        <label class="form-label" for="traffic-limit-unit">{$t('traffic.limits.field_unit')}</label>
        <Select
          id="traffic-limit-unit"
          bind:value={formUnit}
          options={unitOptions}
          data-testid="traffic-limit-field-unit"
        />
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="traffic-limit-period"
        >{$t('traffic.limits.field_period')}</label
      >
      <Select
        id="traffic-limit-period"
        bind:value={formPeriod}
        options={periodOptions}
        data-testid="traffic-limit-field-period"
      />
    </div>

    <Input
      id="traffic-limit-threshold"
      type="number"
      label={$t('traffic.limits.field_threshold')}
      bind:value={formThreshold}
      min="1"
      max="99"
      step="1"
      testid="traffic-limit-field-threshold"
    />

    <div class="form-group">
      <label class="form-label" for="traffic-limit-action"
        >{$t('traffic.limits.field_action')}</label
      >
      <Select
        id="traffic-limit-action"
        bind:value={formAction}
        options={actionOptions}
        ariaDescribedby={[
          autoTarget ? 'traffic-limit-hint-auto' : '',
          formTarget === 'all' && switchingAction ? 'traffic-limit-hint-all' : '',
          showMihomoReload ? 'traffic-limit-hint-reload' : ''
        ]
          .filter(Boolean)
          .join(' ') || undefined}
        data-testid="traffic-limit-field-action"
      />
      {#if autoTarget}
        <span class="form-hint" id="traffic-limit-hint-auto">
          {$t('traffic.limits.hint_auto_notify')}
        </span>
      {/if}
      {#if formTarget === 'all' && switchingAction}
        <span class="form-hint warning-text" id="traffic-limit-hint-all">
          {$t('traffic.limits.hint_all_block')}
        </span>
      {/if}
      {#if showMihomoReload}
        <span
          class="form-hint"
          id="traffic-limit-hint-reload"
          data-testid="traffic-limit-hint-reload"
        >
          {$t('traffic.limits.hint_mihomo_reload')}
        </span>
      {/if}
    </div>

    <div class="limit-form-toggle">
      <Toggle
        id="traffic-limit-enabled"
        bind:checked={formEnabled}
        testid="traffic-limit-field-enabled"
      />
      <label for="traffic-limit-enabled">{$t('traffic.limits.field_enabled')}</label>
    </div>

    <div class="limit-form-foot">
      <Button variant="secondary" disabled={saving} onclick={onclose}>
        {$t('traffic.limits.cancel')}
      </Button>
      <Button type="submit" variant="primary" loading={saving} testid="traffic-limit-save">
        {$t('traffic.limits.save')}
      </Button>
    </div>
  </form>
</Modal>

<style>
  .limit-form {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .limit-form :global(.form-group) {
    margin: 0;
    min-width: 0;
  }

  .limit-form-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--spacing-4);
  }

  .limit-form-toggle {
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
    font-size: var(--font-size-base);
  }

  .limit-form-foot {
    display: flex;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: var(--spacing-3);
    margin-top: var(--spacing-2);
  }
</style>
