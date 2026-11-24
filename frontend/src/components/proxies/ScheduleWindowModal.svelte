<script lang="ts">
  import { errorCode, errorParams } from '../../lib/apiErrors';
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { showToast } from '../../stores';
  import Modal from '../Modal.svelte';
  import Select from '../Select.svelte';
  import Input from '../Input.svelte';
  import Button from '../Button.svelte';
  import {
    loadRuntime,
    proxiesErrorText,
    saveScheduleWindow,
    type RuntimeGroup,
    type ScheduleWindowView
  } from '../../lib/configProxies';
  import {
    daysSummary,
    findOverlap,
    isOvernight,
    parseClock,
    windowSummary
  } from '../../lib/scheduleWindows';

  // Модалка окна расписания ручной группы (UI-SPEC 1.3, D-08, D-11). Проверка
  // пересечений на клиенте — подсказка, источник истины — ответ сервера
  // (409 schedule_overlap показывается тем же блоком конфликта).
  interface Props {
    group: RuntimeGroup;
    /** Правка окна; null — новое окно. */
    window: ScheduleWindowView | null;
    onclose: () => void;
    onsaved: () => void;
  }

  let { group, window: editWindow, onclose, onsaved }: Props = $props();

  const WEEKDAYS = [0, 1, 2, 3, 4];
  const WEEKEND = [5, 6];
  const ALL_DAYS = [0, 1, 2, 3, 4, 5, 6];

  // Значения формы при открытии: модалка монтируется заново на каждое открытие
  function initialForm() {
    const w = editWindow;
    const current = group.members.some((m) => m.member === group.current) ? group.current : '';
    return {
      member: w?.member ?? (current || group.members[0]?.member || ''),
      days: w ? [...w.days] : [...WEEKDAYS],
      from: w?.from ?? '09:00',
      to: w?.to ?? '18:00'
    };
  }

  const init = initialForm();
  let formMember = $state(init.member);
  let formDays = $state<number[]>(init.days);
  let formFrom = $state(init.from);
  let formTo = $state(init.to);

  let saving = $state(false);
  let saveError = $state('');
  // Конфликт из ответа сервера: ID окна и снимок полей, для которых он получен
  let serverOverlap = $state<{ id: string; sig: string } | null>(null);

  const uid = $props.id();
  const memberId = `schedule-window-member-${uid}`;
  const fromId = `schedule-window-from-${uid}`;
  const toId = `schedule-window-to-${uid}`;
  const daysErrorId = `schedule-window-days-error-${uid}`;
  const overlapId = `schedule-window-overlap-${uid}`;
  const memberWarningId = `schedule-window-member-warning-${uid}`;

  // Узел правимого окна пропал из состава группы (D-07): вариант остаётся с пометкой
  const memberMissing = $derived(
    formMember !== '' && !group.members.some((m) => m.member === formMember)
  );

  const memberOptions = $derived([
    ...group.members.map((m) => ({ value: m.member, label: m.name })),
    ...(memberMissing
      ? [
          {
            value: formMember,
            label: `${editWindow?.member === formMember ? editWindow.member_label || formMember : formMember} ${$t('proxies.rt.sched.member_unavailable')}`
          }
        ]
      : [])
  ]);

  const memberLabel = $derived(
    memberOptions.find((o) => o.value === formMember)?.label ?? formMember
  );

  const sortedDays = $derived([...formDays].sort((a, b) => a - b));
  const fromMin = $derived(parseClock(formFrom));
  const toMin = $derived(parseClock(formTo));
  const timesValid = $derived(fromMin !== null && toMin !== null);
  const noDays = $derived(formDays.length === 0);
  const sameTime = $derived(timesValid && fromMin === toMin);
  const overnight = $derived(
    timesValid && !sameTime && isOvernight({ from: formFrom, to: formTo })
  );

  const draft = $derived({
    id: editWindow?.id ?? '',
    days: sortedDays,
    from: formFrom,
    to: formTo
  });
  const signature = $derived(`${sortedDays.join(',')}|${formFrom}|${formTo}`);

  // Конфликт: пересечение по клиенту или ответ сервера для тех же дней и времени
  const conflict = $derived.by(() => {
    if (noDays || !timesValid || sameTime) return null;
    const local = findOverlap(draft, group.schedule);
    if (local) return local;
    if (serverOverlap && serverOverlap.sig === signature) {
      return group.schedule.find((w) => w.id === serverOverlap?.id) ?? null;
    }
    return null;
  });

  // Окно с пропавшим узлом сервер не сохранит (member_invalid): сначала нужен другой узел
  const canSave = $derived(
    !noDays && timesValid && !sameTime && formMember !== '' && !memberMissing && conflict === null
  );

  const summaryLine = $derived(
    !noDays && timesValid
      ? $t('proxies.rt.sched.summary', {
          days: daysSummary(sortedDays, $t),
          from: formFrom,
          to: formTo,
          node: memberLabel
        })
      : ''
  );

  const dayButtons = $derived(
    ALL_DAYS.map((d) => ({ day: d, label: $t(`proxies.rt.sched.day_${d}`) }))
  );

  function toggleDay(d: number) {
    formDays = formDays.includes(d) ? formDays.filter((x) => x !== d) : [...formDays, d];
  }

  function setDays(days: number[]) {
    formDays = [...days];
  }

  onMount(() => {
    // Фокус — на первое поле формы (Modal ставит его на первый фокусируемый элемент — крестик)
    const timer = setTimeout(() => document.getElementById(memberId)?.focus(), 20);
    return () => clearTimeout(timer);
  });

  async function save() {
    // Повторное нажатие во время запроса в очередь не ставится
    if (saving || !canSave) return;
    saving = true;
    saveError = '';
    const sig = signature;
    try {
      await saveScheduleWindow(group.id, {
        id: editWindow?.id ?? '',
        days: sortedDays,
        from: formFrom,
        to: formTo,
        member: formMember,
        enabled: editWindow?.enabled ?? true
      });
      await loadRuntime();
      onsaved();
    } catch (e) {
      if ((e as { status?: unknown } | null)?.status === 401) return;
      const code = errorCode(e);
      const params = errorParams(e);
      const other = params?.window;
      if (code === 'schedule_overlap' && other) {
        serverOverlap = { id: other, sig };
        // Окно конфликта могло появиться из другой вкладки: перечитать вид для сводки
        if (!group.schedule.some((w) => w.id === other)) await loadRuntime();
        if (group.schedule.some((w) => w.id === other)) return;
      }
      const reason = proxiesErrorText($t, code, params).replace(/[.\s]+$/, '');
      saveError = $t('proxies.rt.sched.save_failed', { reason });
      showToast('error', saveError);
    } finally {
      saving = false;
    }
  }
</script>

<Modal
  open={true}
  title={editWindow ? $t('proxies.rt.sched.edit_title') : $t('proxies.rt.sched.new_title')}
  maxWidth="480px"
  closeDisabled={saving}
  {onclose}
>
  <form
    class="sw-form"
    data-testid="schedule-window-modal"
    novalidate
    onsubmit={(e) => {
      e.preventDefault();
      save();
    }}
  >
    <div class="form-group">
      <label class="form-label" for={memberId}>{$t('proxies.rt.sched.field_member')}</label>
      <Select
        id={memberId}
        bind:value={formMember}
        options={memberOptions}
        ariaDescribedby={memberMissing ? memberWarningId : undefined}
        data-testid="schedule-window-member"
      />
      {#if memberMissing}
        <span class="warning-text" id={memberWarningId}>
          {$t('proxies.rt.sched.node_missing')}
        </span>
      {/if}
    </div>

    <div class="form-group">
      <span class="form-label" id="{daysErrorId}-label">{$t('proxies.rt.sched.field_days')}</span>
      <div
        class="sw-days"
        role="group"
        aria-labelledby="{daysErrorId}-label"
        aria-describedby={noDays ? daysErrorId : undefined}
      >
        {#each dayButtons as b (b.day)}
          <button
            type="button"
            class="filter-chip sw-day"
            aria-pressed={formDays.includes(b.day)}
            data-testid="schedule-window-day-{b.day}"
            onclick={() => toggleDay(b.day)}
          >
            {b.label}
          </button>
        {/each}
      </div>
      <div class="sw-quick">
        <Button variant="ghost" size="xs" onclick={() => setDays(WEEKDAYS)}>
          {$t('proxies.rt.sched.days_weekdays')}
        </Button>
        <Button variant="ghost" size="xs" onclick={() => setDays(WEEKEND)}>
          {$t('proxies.rt.sched.days_weekend')}
        </Button>
        <Button variant="ghost" size="xs" onclick={() => setDays(ALL_DAYS)}>
          {$t('proxies.rt.sched.days_all')}
        </Button>
      </div>
      {#if noDays}
        <span class="error-text" id={daysErrorId}>{$t('proxies.rt.sched.err_no_days')}</span>
      {/if}
    </div>

    <div class="form-row-grid">
      <Input
        id={fromId}
        type="time"
        step={60}
        label={$t('proxies.rt.sched.field_from')}
        bind:value={formFrom}
        mono
        class={conflict ? 'input-error' : ''}
        aria-invalid={conflict ? 'true' : undefined}
        aria-describedby={conflict ? overlapId : undefined}
        testid="schedule-window-from"
      />
      <Input
        id={toId}
        type="time"
        step={60}
        label={$t('proxies.rt.sched.field_to')}
        bind:value={formTo}
        mono
        class={conflict ? 'input-error' : ''}
        aria-invalid={conflict ? 'true' : undefined}
        aria-describedby={conflict ? overlapId : undefined}
        error={sameTime ? $t('proxies.rt.sched.err_same_time') : ''}
        hint={overnight ? $t('proxies.rt.sched.hint_overnight', { time: formTo }) : ''}
        testid="schedule-window-to"
      />
    </div>

    {#if conflict}
      <span class="error-text" id={overlapId} role="alert" data-testid="schedule-window-overlap">
        {$t('proxies.rt.sched.err_overlap', { window: windowSummary(conflict, $t) })}
      </span>
    {/if}

    <p class="sw-summary" aria-live="polite" data-testid="schedule-window-summary">
      {summaryLine}
    </p>

    {#if saveError}
      <span class="error-text" data-testid="schedule-window-error">{saveError}</span>
    {/if}

    <div class="sw-foot">
      <Button variant="secondary" disabled={saving} onclick={onclose}>
        {$t('proxies.rt.sched.cancel')}
      </Button>
      <Button
        type="submit"
        variant="primary"
        loading={saving}
        disabled={!canSave}
        ariaDescribedby={conflict ? overlapId : undefined}
        testid="schedule-window-save"
      >
        {$t('proxies.rt.sched.save')}
      </Button>
    </div>
  </form>
</Modal>

<style>
  .sw-form {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .sw-form :global(.form-group) {
    margin: 0;
    min-width: 0;
  }

  .form-row-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--spacing-4);
  }

  /* 7 дней в один ряд даже на 390 px */
  .sw-days {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    gap: var(--spacing-1);
  }

  .sw-day {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 0;
    min-height: 32px;
    padding: var(--spacing-1);
    border-radius: var(--radius-full);
    font: inherit;
    font-size: var(--font-size-xs);
    font-weight: 600;
    background: var(--surface-tint);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    cursor: pointer;
    transition:
      color var(--transition-fast),
      background var(--transition-fast),
      border-color var(--transition-fast);
  }

  .sw-day:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .sw-day[aria-pressed='true'] {
    background: var(--accent-soft);
    border-color: var(--accent-line);
    color: var(--fg-primary);
  }

  .sw-quick {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
    margin-top: var(--spacing-2);
  }

  .sw-summary {
    margin: 0;
    min-height: 1.5em;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
    font-variant-numeric: tabular-nums;
    overflow-wrap: anywhere;
  }

  .sw-foot {
    display: flex;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: var(--spacing-3);
    margin-top: var(--spacing-2);
  }

  @media (max-width: 768px) {
    .sw-day {
      min-height: 44px;
    }
  }
</style>
