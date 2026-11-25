<script lang="ts">
  import { tick } from 'svelte';
  import { t, currentLang } from '../../i18n';
  import { showConfirm, showToast } from '../../stores';
  import { usePoller } from '../../lib/poller';
  import {
    trafficLimits,
    loadLimits,
    deleteLimit,
    setLimitEnabled,
    resetLimit,
    loadLimitAlerts,
    clearLimitAlerts,
    formatLimitBytes,
    formatLimitDate,
    limitFill,
    limitFillKind,
    limitPercent,
    type LimitAlert,
    type LimitView,
    type LimitState
  } from '../../lib/trafficLimits';
  import type { BadgeTone } from '../Badge.svelte';
  import Badge from '../Badge.svelte';
  import Button from '../Button.svelte';
  import Toggle from '../Toggle.svelte';
  import EmptyState from '../EmptyState.svelte';
  import Skeleton from '../Skeleton.svelte';
  import Section from '../Section.svelte';
  import ProxyRuntimeNotice from '../proxies/ProxyRuntimeNotice.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import { motion } from '../../lib/motion.svelte';
  import TrafficLimitModal from './TrafficLimitModal.svelte';

  interface Props {
    /** Адрес #/traffic?tab=limits&new=1: открыть форму нового лимита. */
    newLimit?: boolean;
    /** ID группы для предвыбора цели новой формы (&group=<id>). */
    group?: string;
    /** ID лимита, к карточке которого прокрутить (&quota=<id>). */
    quota?: string;
    /**
     * Ключ ссылки, форма которой уже закрыта. Живёт у страницы: вкладка при переключении
     * размонтируется, и собственное состояние форму по той же ссылке открыло бы снова.
     */
    dismissedLink?: string;
  }

  let { newLimit = false, group = '', quota = '', dismissedLink = $bindable('') }: Props = $props();

  let view = $derived($trafficLimits);
  let alerts = $state<LimitAlert[]>([]);
  let loadFailed = $state(false);
  let retrying = $state(false);
  let clearingAlerts = $state(false);
  // Переключатель до ответа сервера показывает новое положение; ошибка возвращает прежнее
  let pendingEnabled = $state<Record<string, boolean>>({});
  // Кнопка-источник подтверждённого действия в loading до ответа сервера
  let busy = $state<Record<string, 'reset' | 'delete'>>({});

  // Форма лимита (модалка — TrafficLimitModal): правка лимита или новый с предвыбором группы
  let modalOpen = $state(false);
  let editing = $state<LimitView | null>(null);
  let presetGroup = $state('');

  function isAbort(e: unknown): boolean {
    return (e as { name?: unknown } | null)?.name === 'AbortError';
  }

  function isUnauthorized(e: unknown): boolean {
    return (e as { status?: unknown } | null)?.status === 401;
  }

  function errorMessage(e: unknown): string {
    const m = (e as { message?: unknown } | null)?.message;
    return typeof m === 'string' && m !== '' ? m : String(e);
  }

  async function refresh(signal?: AbortSignal) {
    try {
      await loadLimits(signal);
      alerts = await loadLimitAlerts(signal);
      loadFailed = false;
    } catch (e) {
      if (isAbort(e) || isUnauthorized(e)) return;
      loadFailed = true;
      throw e;
    }
  }

  // Опрос раз в 5 с: вкладка смонтирована только пока открыта, при скрытой странице опрос стоит
  const poller = usePoller((signal) => refresh(signal), 5000);

  async function retry() {
    retrying = true;
    try {
      await poller.refetch();
    } finally {
      retrying = false;
    }
  }

  function openCreate(preset = '') {
    editing = null;
    presetGroup = preset;
    modalOpen = true;
  }

  function openEdit(l: LimitView) {
    editing = l;
    presetGroup = '';
    modalOpen = true;
  }

  // #/traffic?tab=limits&new=1&group=<id> — форма нового лимита с предвыбранной группой;
  // ID проверяет сама форма по группам вида (неизвестный — без предвыбора). Закрытая
  // пользователем форма по тому же адресу больше не открывается.
  let linkKey = $derived(newLimit ? `new:${group}` : '');
  let linkModal = $derived(linkKey !== '' && linkKey !== dismissedLink);

  function closeModal() {
    modalOpen = false;
    editing = null;
    presetGroup = '';
    dismissedLink = linkKey;
  }

  async function onSaved() {
    closeModal();
    showToast('success', $t('traffic.limits.toast_saved'));
    await loadLimits().catch(() => {});
  }

  // Переход к карточке выполняется один раз на значение адреса
  let handledQuota = '';

  // #/traffic?tab=limits&quota=<id> — прокрутка к карточке лимита и фокус на ней; ID только
  // из загруженного списка (T-152-24)
  $effect(() => {
    const id = quota;
    const list = view?.limits;
    if (!id || id === handledQuota || !list?.some((l) => l.id === id)) return;
    handledQuota = id;
    tick().then(() => {
      const card = document.querySelector<HTMLElement>(
        `[data-testid="traffic-limit-${CSS.escape(id)}"]`
      );
      card?.scrollIntoView({ block: 'center', behavior: motion.reduced ? 'auto' : 'smooth' });
      card?.focus({ preventScroll: true });
    });
  });

  function stateTone(s: LimitState): BadgeTone {
    switch (s) {
      case 'ok':
        return 'success';
      case 'threshold':
      case 'group_deleted':
        return 'warning';
      case 'exhausted':
        return 'danger';
      default:
        return 'neutral';
    }
  }

  function periodLabel(p: string): string {
    switch (p) {
      case 'daily':
        return $t('trafficquotas.period_daily');
      case 'weekly':
        return $t('trafficquotas.period_weekly');
      default:
        return $t('trafficquotas.period_monthly');
    }
  }

  function targetLabel(l: LimitView): string {
    if (l.target_type === 'all') return $t('traffic.limits.target_all');
    return l.target_name || l.target_id;
  }

  // Подсказка «нет данных»: чего именно не хватает учёту
  let noDataTitle = $derived(
    view?.data === 'no_socket'
      ? $t('traffic.limits.no_socket')
      : view?.data === 'no_counters'
        ? $t('traffic.limits.no_counters')
        : view?.data === 'no_stream'
          ? $t('traffic.limits.no_stream')
          : $t('traffic.limits.no_data')
  );
  let hasData = $derived(view?.data === 'ok');

  function alertText(a: LimitAlert): string {
    const key =
      a.kind === 'exceeded' ? 'trafficquotas.alert_exceeded' : 'trafficquotas.alert_threshold_msg';
    return $t(key, {
      name: a.limit_name,
      current: formatLimitBytes(a.used_bytes, $currentLang),
      limit: formatLimitBytes(a.limit_bytes, $currentLang),
      percent: (a.percent ?? 0).toFixed(0)
    });
  }

  async function onClearAlerts() {
    clearingAlerts = true;
    try {
      await clearLimitAlerts();
      alerts = [];
    } catch (e) {
      if (!isUnauthorized(e)) showToast('error', errorMessage(e));
    } finally {
      clearingAlerts = false;
    }
  }

  function enabledOf(l: LimitView): boolean {
    return pendingEnabled[l.id] ?? l.enabled;
  }

  async function onToggle(l: LimitView) {
    const next = !enabledOf(l);
    pendingEnabled[l.id] = next;
    try {
      await setLimitEnabled(l.id, next);
      await loadLimits().catch(() => {});
    } catch (e) {
      if (!isUnauthorized(e)) showToast('error', errorMessage(e));
    } finally {
      delete pendingEnabled[l.id];
    }
  }

  async function onReset(l: LimitView) {
    const ok = await showConfirm({
      title: $t('traffic.limits.reset_title', { name: l.name }),
      message: $t('traffic.limits.reset_body'),
      confirmLabel: $t('traffic.limits.reset_confirm'),
      cancelLabel: $t('traffic.limits.reset_cancel'),
      variant: 'warning'
    });
    if (!ok) return;
    busy[l.id] = 'reset';
    try {
      await resetLimit(l.id);
      showToast('success', $t('traffic.limits.toast_reset'));
      await loadLimits().catch(() => {});
    } catch (e) {
      if (!isUnauthorized(e)) showToast('error', errorMessage(e));
    } finally {
      delete busy[l.id];
    }
  }

  async function onDelete(l: LimitView) {
    const ok = await showConfirm({
      title: $t('traffic.limits.delete_title', { name: l.name }),
      message: $t('traffic.limits.delete_body'),
      confirmLabel: $t('traffic.limits.delete_confirm'),
      cancelLabel: $t('traffic.limits.delete_cancel'),
      variant: 'warning'
    });
    if (!ok) return;
    busy[l.id] = 'delete';
    try {
      await deleteLimit(l.id);
      showToast('success', $t('traffic.limits.toast_deleted'));
      await loadLimits().catch(() => {});
      // После удаления фокус — на «Добавить лимит» (карточки больше нет)
      document.querySelector<HTMLElement>('[data-testid="traffic-limit-add"]')?.focus();
    } catch (e) {
      if (!isUnauthorized(e)) showToast('error', errorMessage(e));
    } finally {
      delete busy[l.id];
    }
  }
</script>

<div class="limits-tab" data-testid="traffic-limits">
  <div class="limits-head">
    <p class="limits-desc">{$t('traffic.limits.header_desc')}</p>
    <Button
      variant="primary"
      class="tap-zone-44"
      testid="traffic-limit-add"
      onclick={() => openCreate()}
    >
      <Icon name="add" size={14} />
      {$t('traffic.limits.add')}
    </Button>
  </div>

  {#if loadFailed}
    <div
      class="alert alert-error limits-load-error"
      role="alert"
      data-testid="traffic-limits-error"
    >
      <span>{$t('traffic.limits.load_failed')}</span>
      <Button
        variant="secondary"
        size="sm"
        class="tap-zone-44"
        loading={retrying}
        testid="traffic-limits-retry"
        onclick={retry}
      >
        {$t('traffic.limits.retry')}
      </Button>
    </div>
  {/if}

  {#if view === null}
    {#if !loadFailed}
      <div class="limits-grid" aria-busy="true">
        <Skeleton type="card" height="220px" />
        <Skeleton type="card" height="220px" />
      </div>
    {/if}
  {:else}
    {#if alerts.length > 0}
      <Section title={$t('trafficquotas.alerts')}>
        {#snippet meta()}
          <Button
            variant="secondary"
            size="sm"
            class="tap-zone-44"
            loading={clearingAlerts}
            testid="traffic-limits-alerts-clear"
            onclick={onClearAlerts}
          >
            {$t('trafficquotas.clear_alerts')}
          </Button>
        {/snippet}
        <div class="limits-alerts" data-testid="traffic-limit-alerts">
          {#each alerts as a, i (i)}
            <div
              class="alert"
              class:alert-warning={a.kind !== 'exceeded'}
              class:alert-error={a.kind === 'exceeded'}
            >
              {alertText(a)}
            </div>
          {/each}
        </div>
      </Section>
    {/if}

    {#if view.data === 'no_socket'}
      <ProxyRuntimeNotice viewState="no_socket" layout="inline" />
    {:else if view.data === 'no_counters'}
      <div class="alert alert-warning" data-testid="traffic-limits-no-counters">
        {$t('traffic.limits.no_counters')}
      </div>
    {:else if view.data === 'no_stream'}
      <div class="alert alert-warning" data-testid="traffic-limits-no-stream">
        {$t('traffic.limits.no_stream')}
      </div>
    {/if}

    {#if view.limits.length === 0}
      <EmptyState
        title={$t('traffic.limits.empty_title')}
        description={$t('traffic.limits.empty_desc')}
        ctaText={$t('traffic.limits.add')}
        ctaVariant="secondary"
        oncta={() => openCreate()}
      />
    {:else}
      <div class="limits-grid">
        {#each view.limits as l (l.id)}
          {@const pct = limitPercent(l)}
          {@const fill = limitFill(l)}
          {@const ofText = $t('traffic.limits.of', {
            used: formatLimitBytes(l.used_bytes, $currentLang),
            limit: formatLimitBytes(l.limit_bytes, $currentLang)
          })}
          <article
            class="card limit-card"
            data-testid="traffic-limit-{l.id}"
            tabindex="-1"
            aria-labelledby="traffic-limit-name-{l.id}"
          >
            <div class="limit-row">
              <h3 class="limit-name" id="traffic-limit-name-{l.id}" title={l.name}>{l.name}</h3>
              {#if l.state !== 'group_deleted'}
                <Badge tone={stateTone(l.state)} testid="traffic-limit-state-{l.id}">
                  {$t(`traffic.limits.state_${l.state}`)}
                </Badge>
              {/if}
            </div>

            <div class="limit-row limit-row--start">
              {#if l.state === 'group_deleted'}
                <Badge tone="warning" testid="traffic-limit-target-{l.id}">
                  <Icon name="warning" size={12} />
                  {$t('traffic.limits.state_group_deleted')}
                </Badge>
              {:else}
                <Badge
                  variant="tag"
                  class="limit-target"
                  title={targetLabel(l)}
                  testid="traffic-limit-target-{l.id}"
                >
                  {targetLabel(l)}
                </Badge>
              {/if}
            </div>

            <div class="limit-row limit-usage">
              {#if hasData}
                <span class="limit-value">{ofText}</span>
                <span class="limit-pct">{Math.round(pct)}%</span>
              {:else}
                <span class="limit-value limit-value--none" title={noDataTitle}>
                  {$t('traffic.limits.no_data')}
                </span>
              {/if}
            </div>

            <div
              class="stat-bar stat-bar--meter"
              role="progressbar"
              aria-label={l.name}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={hasData ? Math.round(fill) : 0}
              aria-valuetext={hasData ? ofText : $t('traffic.limits.no_data')}
            >
              <div
                class="stat-bar-fill stat-bar-fill--{limitFillKind(l)}"
                style:width="{hasData ? fill : 0}%"
              ></div>
            </div>

            {#if l.approx && hasData}
              <p class="limit-note">
                <Icon name="info" size={14} />
                <span>{$t('traffic.limits.approx')}</span>
              </p>
            {/if}

            <div class="limit-row limit-meta">
              <span class="limit-reset">
                {$t('traffic.limits.reset_line', {
                  date: formatLimitDate(l.reset_at, view?.offset),
                  period: periodLabel(l.period)
                })}
              </span>
              <Badge variant="tag">{$t(`traffic.limits.action_${l.action}`)}</Badge>
            </div>

            <div class="limit-foot">
              <span class="limit-toggle">
                <Toggle
                  checked={enabledOf(l)}
                  loading={pendingEnabled[l.id] !== undefined}
                  disabled={pendingEnabled[l.id] !== undefined}
                  ariaLabel={$t('traffic.limits.toggle_aria', { name: l.name })}
                  testid="traffic-limit-toggle-{l.id}"
                  onchange={() => onToggle(l)}
                />
                <span class="limit-toggle-label">{$t('traffic.limits.field_enabled')}</span>
              </span>
              <span class="limit-actions">
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  class="tap-zone-44"
                  ariaLabel={$t('traffic.limits.edit_aria', { name: l.name })}
                  title={$t('traffic.limits.edit_aria', { name: l.name })}
                  testid="traffic-limit-edit-{l.id}"
                  onclick={() => openEdit(l)}
                >
                  <Icon name="edit" size={16} />
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  class="tap-zone-44"
                  loading={busy[l.id] === 'reset'}
                  disabled={busy[l.id] !== undefined}
                  ariaLabel={$t('traffic.limits.reset_aria', { name: l.name })}
                  title={$t('traffic.limits.reset_aria', { name: l.name })}
                  testid="traffic-limit-reset-{l.id}"
                  onclick={() => onReset(l)}
                >
                  {#if busy[l.id] !== 'reset'}
                    <Icon name="refresh" size={16} />
                  {/if}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  class="tap-zone-44"
                  loading={busy[l.id] === 'delete'}
                  disabled={busy[l.id] !== undefined}
                  ariaLabel={$t('traffic.limits.delete_aria', { name: l.name })}
                  title={$t('traffic.limits.delete_aria', { name: l.name })}
                  testid="traffic-limit-delete-{l.id}"
                  onclick={() => onDelete(l)}
                >
                  {#if busy[l.id] !== 'delete'}
                    <Icon name="trash" size={16} />
                  {/if}
                </Button>
              </span>
            </div>
          </article>
        {/each}
      </div>
    {/if}
  {/if}
</div>

{#if modalOpen || linkModal}
  <TrafficLimitModal
    limit={modalOpen ? editing : null}
    presetGroup={modalOpen ? presetGroup : group}
    kernel={view?.kernel ?? ''}
    onclose={closeModal}
    onsaved={onSaved}
  />
{/if}

<style>
  .limits-tab {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
  }

  .limits-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--spacing-3);
  }

  .limits-desc {
    margin: 0;
    flex: 1 1 240px;
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.4;
  }

  .limits-load-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--spacing-3);
  }

  .limits-alerts {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .limits-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: var(--grid-gap);
  }

  .limit-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .limit-card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .limit-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .limit-row--start {
    justify-content: flex-start;
  }

  .limit-name {
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .limit-card :global(.limit-target) {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .limit-usage {
    align-items: baseline;
  }

  .limit-value {
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    font-variant-numeric: tabular-nums;
  }

  .limit-value--none {
    color: var(--fg-secondary);
  }

  .limit-pct,
  .limit-reset {
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.4;
    font-variant-numeric: tabular-nums;
  }

  .limit-note {
    display: flex;
    align-items: center;
    gap: var(--spacing-1);
    margin: 0;
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.4;
  }

  .limit-meta {
    flex-wrap: wrap;
  }

  .limit-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--spacing-2);
    margin-top: auto;
    padding-top: var(--spacing-2);
    border-top: 1px solid var(--border);
  }

  .limit-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .limit-toggle-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .limit-actions {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
  }

  /* Узкий экран и касание: у кнопок карточки 32 px зона tap-zone-44 выступает на 6 px
     в каждую сторону, при промежутке 4 px зона следующей кнопки накрывала край
     предыдущей. При 12 px зоны соседних кнопок смыкаются без пересечения, шаг ряда —
     44 px. Условие то же, что у зоны в global.css, поэтому на десктопе с точным
     указателем промежуток прежний. */
  @media (pointer: coarse), (max-width: 768px) {
    .limit-actions {
      gap: var(--spacing-3);
    }
  }
</style>
