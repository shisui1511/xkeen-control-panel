<script lang="ts">
  import { tick } from 'svelte';
  import { t } from '../../i18n';
  import { showConfirm, showToast } from '../../stores';
  import Badge from '../Badge.svelte';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    loadRuntime,
    proxiesErrorText,
    restoreSchedule,
    type RuntimeClock,
    type RuntimeGroup
  } from '../../lib/configProxies';
  import { loadLimits, limitsErrorMeta, resetLimit } from '../../lib/trafficLimits';
  import { formatRouterTime } from '../../lib/scheduleWindows';
  import { holdLeftover, quotaHoldParams } from '../../lib/proxyRuntimeAdapter';

  // Строка состояния группы (UI-SPEC 1.1, D-04, D-05, D-07): чем арбитр держит группу
  // сейчас и чем это снять. Сервер уже выбрал старшее удержание (квота > перехват >
  // расписание), строка показывает ровно его. Нет удержания (в том числе до первого
  // ответа вида) — строка не рисуется. Время — по часам роутера (D-08).
  interface Props {
    group: RuntimeGroup;
    clock: RuntimeClock | undefined;
  }

  let { group, clock }: Props = $props();

  const hold = $derived(group.hold);
  // Только след неудачного возврата: показывается одна плашка «ядро не приняло»
  const leftover = $derived(!!hold && holdLeftover(hold));
  const quota = $derived(hold ? quotaHoldParams(hold, clock) : { name: '', date: '' });
  const quotaText = $derived($t('proxies.rt.quota.exhausted', quota));

  // Кнопка-источник действия в loading до ответа сервера; повторное нажатие не ставится в очередь
  let busy = $state<'' | 'reset' | 'restore'>('');
  let rootEl = $state<HTMLElement | null>(null);

  function routerTime(unix: number | undefined): string {
    return unix ? formatRouterTime(unix, clock?.offset ?? 0) : '--:--';
  }

  function codeOf(e: unknown): string | undefined {
    const code = (e as { code?: unknown } | null)?.code;
    return typeof code === 'string' && code !== '' ? code : undefined;
  }

  function paramsOf(e: unknown): Record<string, string> | undefined {
    const params = (e as { params?: unknown } | null)?.params;
    return params && typeof params === 'object' ? (params as Record<string, string>) : undefined;
  }

  function isUnauthorized(e: unknown): boolean {
    return (e as { status?: unknown } | null)?.status === 401;
  }

  async function onReset() {
    const id = hold?.limit;
    if (busy !== '' || !id) return;
    const ok = await showConfirm({
      title: $t('traffic.limits.reset_title', { name: quota.name }),
      message: $t('traffic.limits.reset_body'),
      confirmLabel: $t('traffic.limits.reset_confirm'),
      cancelLabel: $t('traffic.limits.reset_cancel'),
      variant: 'warning'
    });
    if (!ok) return;
    busy = 'reset';
    try {
      await resetLimit(id);
      showToast('success', $t('traffic.limits.toast_reset'));
      await Promise.all([loadRuntime(), loadLimits().catch(() => {})]);
    } catch (e) {
      // Строка сохраняет прежнее состояние: вид не перечитывается
      if (!isUnauthorized(e)) {
        const meta = limitsErrorMeta(e);
        showToast(
          'error',
          $t('proxies.rt.quota.reset_failed', { reason: meta.message || meta.code || '?' })
        );
      }
    } finally {
      busy = '';
    }
  }

  async function onRestore() {
    if (busy !== '') return;
    busy = 'restore';
    try {
      await restoreSchedule(group.id);
      showToast('success', $t('proxies.rt.sched.toast_restored'));
      await loadRuntime();
      // Кнопка исчезла вместе с перехватом: фокус — на строку состояния
      await tick();
      rootEl?.focus();
    } catch (e) {
      if (!isUnauthorized(e)) showToast('error', proxiesErrorText($t, codeOf(e), paramsOf(e)));
    } finally {
      busy = '';
    }
  }
</script>

{#if hold}
  <!-- Действия строки не сворачивают карточку (шапка кликается целиком) -->
  <div
    class="pc-state"
    data-testid="proxy-group-state-{group.id}"
    data-stop-head-click
    role="status"
    aria-live="polite"
    tabindex="-1"
    bind:this={rootEl}
  >
    {#if leftover}
      <!-- Возврат после удержания не принят ядром: повтор идёт на сервере -->
    {:else if hold.kind === 'quota'}
      <Badge tone="danger" class="pc-state-badge" title={quotaText}>
        <Icon name="warning" size={12} />
        <span class="pc-state-text">{quotaText}</span>
      </Badge>
      <a
        class="pc-state-link"
        href="#/traffic?tab=limits&quota={encodeURIComponent(hold.limit ?? '')}"
        data-testid="proxy-group-quota-open-{group.id}">{$t('proxies.rt.quota.open')}</a
      >
      <Button
        variant="secondary"
        size="sm"
        class="tap-zone-44"
        testid="proxy-group-quota-reset-{group.id}"
        loading={busy === 'reset'}
        disabled={busy !== ''}
        onclick={() => void onReset()}
      >
        {$t('proxies.rt.quota.reset')}
      </Button>
    {:else if hold.kind === 'override'}
      <Badge tone="warning" class="pc-state-badge">
        <span class="pc-state-text">
          {$t('proxies.rt.sched.state_override', { time: routerTime(hold.until) })}
        </span>
      </Badge>
      <Button
        variant="secondary"
        size="sm"
        class="tap-zone-44"
        testid="proxy-group-restore-{group.id}"
        loading={busy === 'restore'}
        disabled={busy !== ''}
        onclick={() => void onRestore()}
      >
        {$t('proxies.rt.sched.restore')}
      </Button>
    {:else if hold.kind === 'schedule'}
      <Badge tone="info" class="pc-state-badge">
        <span class="pc-state-text">
          {$t('proxies.rt.sched.state_active', { time: routerTime(hold.until) })}
        </span>
      </Badge>
    {:else if hold.kind === 'node_missing'}
      <Badge tone="warning" class="pc-state-badge">
        <Icon name="warning" size={12} />
        <span class="pc-state-text">{$t('proxies.rt.sched.node_missing')}</span>
      </Badge>
    {/if}
    {#if hold.stalled}
      <!-- Ядро не приняло выбор арбитра (гибрид D-13): своей кнопки нет, сверку повторяет
           сервер, причина — в уведомлениях «Конфигурации» -->
      {@const stalledText = $t(
        hold.kind === 'quota' ? 'proxies.rt.quota.stalled' : 'proxies.rt.sched.stalled'
      )}
      <Badge
        tone="warning"
        class="pc-state-badge pc-state-badge--wrap"
        testid="proxy-group-stalled-{group.id}"
      >
        <Icon name="warning" size={12} />
        <span class="pc-state-text pc-state-text--wrap">{stalledText}</span>
      </Badge>
    {/if}
  </div>
{/if}

<style>
  .pc-state {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    margin-top: var(--spacing-2);
    min-width: 0;
    font-variant-numeric: tabular-nums;
  }
  .pc-state:focus {
    outline: none;
  }
  .pc-state:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius-sm);
  }
  /* Длинное имя лимита усекается, полный текст — в title Badge */
  .pc-state :global(.pc-state-badge) {
    max-width: 100%;
    min-width: 0;
    gap: var(--spacing-1);
  }
  .pc-state :global(.pc-state-badge--wrap) {
    white-space: normal;
  }
  .pc-state-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pc-state-text--wrap {
    white-space: normal;
  }
  .pc-state-link {
    color: var(--accent-text);
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-decoration: none;
  }
  .pc-state-link:hover,
  .pc-state-link:focus-visible {
    text-decoration: underline;
  }
  .pc-state-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
    border-radius: var(--radius-sm);
  }

  @media (max-width: 768px) {
    .pc-state-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }
</style>
