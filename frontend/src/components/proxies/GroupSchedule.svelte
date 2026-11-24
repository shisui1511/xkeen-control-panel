<script lang="ts">
  import { tick } from 'svelte';
  import { t } from '../../i18n';
  import { showConfirm, showToast } from '../../stores';
  import Button from '../Button.svelte';
  import Toggle from '../Toggle.svelte';
  import Skeleton from '../Skeleton.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import ScheduleWindowModal from './ScheduleWindowModal.svelte';
  import { errorCode, errorParams, isUnauthorized } from '../../lib/apiErrors';
  import {
    deleteScheduleWindow,
    loadRuntime,
    proxiesErrorText,
    setScheduleWindowEnabled,
    type RuntimeClock,
    type RuntimeGroup,
    type ScheduleWindowView
  } from '../../lib/configProxies';
  import {
    formatOffset,
    formatRouterTime,
    isOvernight,
    windowSummary
  } from '../../lib/scheduleWindows';

  // Блок «Расписание» раскрытой карточки ручной группы (UI-SPEC 1.2, D-08…D-10):
  // окна группы с узлами, включение, правка и удаление. Время — по часам роутера.
  interface Props {
    group: RuntimeGroup;
    clock: RuntimeClock | undefined;
  }

  let { group, clock }: Props = $props();

  /** Сколько окон видно без «Показать все». */
  const VISIBLE = 5;

  let showAll = $state(false);
  // Положение переключателя на время запроса: id окна → включено
  let pendingEnabled = $state<Record<string, boolean>>({});
  // Окно, удаление которого идёт сейчас
  let deleting = $state('');
  // Окно, которое правится в модалке: null — модалка закрыта, 'new' — новое окно
  let editing = $state<ScheduleWindowView | 'new' | null>(null);
  // Кнопка, открывшая модалку: на неё возвращается фокус после закрытия
  let sourceEl: HTMLElement | null = null;

  // До первого ответа вида (нет часов или окон) — скелетон, а не пустое состояние
  const loaded = $derived(clock !== undefined && Array.isArray(group.schedule));
  const windows = $derived(group.schedule ?? []);
  const shown = $derived(showAll ? windows : windows.slice(0, VISIBLE));

  const routerTime = $derived(
    clock
      ? $t('proxies.rt.sched.router_time', {
          time: formatRouterTime(clock.now, clock.offset),
          tz: formatOffset(clock.offset)
        })
      : ''
  );

  function summaryOf(w: ScheduleWindowView): string {
    return windowSummary(w, $t);
  }

  function nodeLabel(w: ScheduleWindowView): string {
    const name = w.member_label || w.member;
    return w.missing ? `${name} ${$t('proxies.rt.sched.member_unavailable')}` : name;
  }

  function enabledOf(w: ScheduleWindowView): boolean {
    return pendingEnabled[w.id] ?? w.enabled;
  }

  async function toggle(w: ScheduleWindowView, enabled: boolean) {
    if (pendingEnabled[w.id] !== undefined) return;
    pendingEnabled[w.id] = enabled;
    try {
      await setScheduleWindowEnabled(group.id, w.id, enabled);
      await loadRuntime();
    } catch (e) {
      if (!isUnauthorized(e))
        showToast('error', proxiesErrorText($t, errorCode(e), errorParams(e)));
    } finally {
      // Сбой — переключатель возвращается к значению вида; успех — вид уже перечитан
      delete pendingEnabled[w.id];
    }
  }

  /** Узел, к которому группа вернётся после удаления действующего окна. */
  function originName(): string {
    const origin = group.hold?.origin;
    const m = origin ? group.members.find((x) => x.member === origin) : group.members[0];
    return m?.name ?? origin ?? '';
  }

  async function remove(w: ScheduleWindowView) {
    if (deleting) return;
    const holds =
      w.active &&
      group.hold?.window === w.id &&
      (group.hold.kind === 'schedule' || group.hold.kind === 'override');
    const back = holds ? originName() : '';
    const ok = await showConfirm({
      title: $t('proxies.rt.sched.delete_title'),
      message: summaryOf(w),
      consequence: back ? $t('proxies.rt.sched.delete_active_note', { name: back }) : undefined,
      confirmLabel: $t('proxies.rt.sched.delete_confirm'),
      cancelLabel: $t('proxies.rt.sched.delete_cancel'),
      variant: 'warning'
    });
    if (!ok) return;
    deleting = w.id;
    try {
      await deleteScheduleWindow(group.id, w.id);
      await loadRuntime();
      deleting = '';
      await tick();
      document
        .querySelector<HTMLButtonElement>(`[data-testid="proxy-group-schedule-add-${group.id}"]`)
        ?.focus();
    } catch (e) {
      if (!isUnauthorized(e))
        showToast('error', proxiesErrorText($t, errorCode(e), errorParams(e)));
    } finally {
      deleting = '';
    }
  }

  function openEditor(w: ScheduleWindowView | 'new', e: MouseEvent) {
    sourceEl = e.currentTarget as HTMLElement;
    editing = w;
  }

  /** Закрытие модалки: фокус — на кнопку-источник (или «Добавить окно», если её уже нет). */
  async function closeEditor() {
    editing = null;
    await tick();
    if (sourceEl?.isConnected) sourceEl.focus();
    else
      document
        .querySelector<HTMLButtonElement>(`[data-testid="proxy-group-schedule-add-${group.id}"]`)
        ?.focus();
    sourceEl = null;
  }
</script>

<section
  class="gs"
  data-testid="proxy-group-schedule-{group.id}"
  aria-labelledby="proxy-group-schedule-title-{group.id}"
>
  <div class="gs-head">
    <h3 class="gs-title" id="proxy-group-schedule-title-{group.id}">
      {$t('proxies.rt.sched.title')}
    </h3>
    {#if routerTime}
      <span class="gs-meta" data-testid="proxy-group-schedule-clock-{group.id}">{routerTime}</span>
    {/if}
  </div>

  {#if !loaded}
    <div class="gs-skeleton">
      <Skeleton type="text-line" width="100%" height="36px" />
    </div>
  {:else if windows.length === 0}
    <p class="gs-empty">{$t('proxies.rt.sched.empty')}</p>
    <div class="gs-add">
      <Button
        variant="secondary"
        size="sm"
        class="tap-zone-44"
        testid="proxy-group-schedule-add-{group.id}"
        onclick={(e) => openEditor('new', e)}
      >
        <Icon name="add" size={14} />
        {$t('proxies.rt.sched.add')}
      </Button>
    </div>
  {:else}
    <ul class="gs-list" class:gs-list-all={showAll}>
      {#each shown as w (w.id)}
        {@const summary = summaryOf(w)}
        {@const node = nodeLabel(w)}
        <li class="gs-row" data-testid="proxy-group-schedule-window-{w.id}">
          <div class="gs-main">
            <span class="gs-summary">
              {summary}
              {#if isOvernight(w)}
                <span class="gs-tag">{$t('proxies.rt.sched.overnight_label')}</span>
              {/if}
            </span>
            <span class="gs-target">
              <span class="gs-arrow" aria-hidden="true"><Icon name="arrow-right" size={14} /></span>
              <span class="gs-node" class:gs-node-missing={w.missing} title={node}>{node}</span>
            </span>
          </div>
          <div class="gs-controls">
            <Toggle
              checked={enabledOf(w)}
              loading={pendingEnabled[w.id] !== undefined}
              disabled={pendingEnabled[w.id] !== undefined || deleting === w.id}
              ariaLabel={$t('proxies.rt.sched.toggle_aria', { window: summary })}
              testid="proxy-group-schedule-toggle-{w.id}"
              onchange={(e) => toggle(w, e.currentTarget.checked)}
            />
            <Button
              variant="ghost"
              size="sm"
              iconOnly
              class="tap-zone-44"
              ariaLabel={$t('proxies.rt.sched.edit_aria', { window: summary })}
              title={$t('proxies.rt.sched.edit_aria', { window: summary })}
              disabled={deleting === w.id}
              testid="proxy-group-schedule-edit-{w.id}"
              onclick={(e) => openEditor(w, e)}
            >
              <Icon name="edit" size={14} />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              iconOnly
              class="tap-zone-44"
              ariaLabel={$t('proxies.rt.sched.delete_aria', { window: summary })}
              title={$t('proxies.rt.sched.delete_aria', { window: summary })}
              loading={deleting === w.id}
              disabled={deleting !== '' && deleting !== w.id}
              testid="proxy-group-schedule-delete-{w.id}"
              onclick={() => remove(w)}
            >
              <Icon name="trash" size={14} />
            </Button>
          </div>
        </li>
      {/each}
    </ul>
    {#if windows.length > VISIBLE}
      <button
        type="button"
        class="gs-more"
        aria-expanded={showAll}
        data-testid="proxy-group-schedule-more-{group.id}"
        onclick={() => (showAll = !showAll)}
      >
        {showAll
          ? $t('proxies.rt.sched.collapse')
          : $t('proxies.rt.sched.show_all', { count: windows.length })}
      </button>
    {/if}
    <div class="gs-add">
      <Button
        variant="ghost"
        size="sm"
        class="tap-zone-44"
        testid="proxy-group-schedule-add-{group.id}"
        onclick={(e) => openEditor('new', e)}
      >
        <Icon name="add" size={14} />
        {$t('proxies.rt.sched.add')}
      </Button>
    </div>
  {/if}
</section>

{#if editing !== null}
  <ScheduleWindowModal
    {group}
    window={editing === 'new' ? null : editing}
    onclose={closeEditor}
    onsaved={closeEditor}
  />
{/if}

<style>
  .gs {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    margin-bottom: var(--spacing-4);
    padding: var(--spacing-3);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    min-width: 0;
  }

  .gs-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--spacing-2);
  }

  .gs-title {
    margin: 0;
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.4;
    color: var(--fg-primary);
  }

  .gs-meta {
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.4;
    color: var(--fg-secondary);
    font-variant-numeric: tabular-nums;
  }

  .gs-empty {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .gs-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .gs-list-all {
    max-height: 360px;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }

  .gs-list-all::-webkit-scrollbar {
    width: 6px;
  }

  .gs-list-all::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius-full);
  }

  .gs-row {
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    min-width: 0;
  }

  .gs-main {
    display: flex;
    flex: 1;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .gs-summary {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--spacing-2);
    color: var(--fg-primary);
    font-variant-numeric: tabular-nums;
  }

  .gs-tag {
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.4;
    color: var(--fg-secondary);
  }

  .gs-target {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    flex: 1;
    min-width: 0;
  }

  .gs-arrow {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--fg-secondary);
  }

  .gs-node {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-family-mono);
    font-variant-numeric: tabular-nums;
    color: var(--fg-primary);
  }

  .gs-node-missing {
    color: var(--warning);
  }

  .gs-controls {
    display: flex;
    flex-shrink: 0;
    align-items: center;
    gap: var(--spacing-2);
  }

  .gs-more {
    align-self: flex-start;
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--accent-text);
    cursor: pointer;
  }

  .gs-more:hover {
    text-decoration: underline;
  }

  .gs-add {
    display: flex;
  }

  /* 390 px: сводка и узел — две строки, переключатель и иконки — третья справа */
  @media (max-width: 480px) {
    .gs-row {
      flex-direction: column;
      align-items: stretch;
      gap: var(--spacing-2);
    }

    .gs-main {
      flex-direction: column;
      align-items: flex-start;
    }

    .gs-target {
      width: 100%;
    }

    .gs-controls {
      align-self: flex-end;
    }
  }

  /* Узкий экран и касание: ссылка xs без отступов — около 18 px. Зона tap-zone-44
     здесь не годится: выступ 13 px при промежутке блока 12 px перекрыл бы зону
     «Добавить окно» под ней, поэтому кнопка растёт видимо; вид ссылки (без фона и
     рамки, текст по центру высоты) прежний. */
  @media (pointer: coarse), (max-width: 768px) {
    .gs-more {
      min-height: 44px;
      min-width: 44px;
    }
  }
</style>
