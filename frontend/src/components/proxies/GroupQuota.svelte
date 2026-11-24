<script lang="ts">
  import { t, currentLang } from '../../i18n';
  import Badge from '../Badge.svelte';
  import Skeleton from '../Skeleton.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import {
    formatLimitBytes,
    formatLimitDate,
    limitFill,
    limitFillKind,
    type LimitsView,
    type LimitView
  } from '../../lib/trafficLimits';
  import type { RuntimeClock, RuntimeGroup } from '../../lib/configProxies';

  // Блок «Лимит» раскрытой карточки группы (UI-SPEC 1.4, D-02, D-14, D-17): расход и
  // состояние лимитов, действующих на группу (свой и общий «Весь трафик»). Своей формы
  // лимита нет: «Задать лимит» ведёт на «Трафик → Лимиты» с выбранной группой.
  interface Props {
    group: RuntimeGroup;
    /** Список лимитов страницы; null — ещё не загружен. */
    limits: LimitsView | null;
    /** Часы роутера: дата сброса — по ним (D-08). */
    clock: RuntimeClock | undefined;
  }

  let { group, limits, clock }: Props = $props();

  const autoLike = $derived(group.mode === 'auto' || group.mode === 'fallback');
  const hasData = $derived(limits?.data === 'ok');
  const noDataTitle = $derived(
    limits?.data === 'no_socket'
      ? $t('traffic.limits.no_socket')
      : limits?.data === 'no_counters'
        ? $t('traffic.limits.no_counters')
        : limits?.data === 'no_stream'
          ? $t('traffic.limits.no_stream')
          : $t('traffic.limits.no_data')
  );

  // Порядок строк: исчерпанный, затем достигший порога, затем остальные (выключенные — в конце)
  const RANK: Record<string, number> = { exhausted: 0, threshold: 1, ok: 2, disabled: 3 };

  const rows = $derived.by(() => {
    const list = (limits?.limits ?? []).filter(
      (l) => l.target_type === 'all' || (l.target_type === 'group' && l.target_id === group.id)
    );
    return list
      .map((l, i) => ({ l, i }))
      .sort((a, b) => (RANK[a.l.state] ?? 2) - (RANK[b.l.state] ?? 2) || a.i - b.i)
      .map((x) => x.l);
  });

  /** У групп «авто» и «резерв» свой лимит только уведомляет (узел выбирает ядро). */
  function onlyNotify(l: LimitView): boolean {
    return autoLike && l.target_type === 'group';
  }

  /** Общий лимит с блокировкой или «напрямую» не остановит группу «авто»/«резерв» (D-14). */
  function globalNote(l: LimitView): boolean {
    return autoLike && l.target_type === 'all' && (l.action === 'block' || l.action === 'direct');
  }

  function ofText(l: LimitView): string {
    return $t('traffic.limits.of', {
      used: formatLimitBytes(l.used_bytes, $currentLang),
      limit: formatLimitBytes(l.limit_bytes, $currentLang)
    });
  }
</script>

{#if group.mode !== 'source'}
  <div class="gq" data-testid="proxy-group-quota-{group.id}">
    {#if limits === null}
      <div class="gq-row" aria-busy="true">
        <Skeleton type="text-line" width="60%" />
        <Skeleton type="text-line" height="8px" />
      </div>
    {:else if rows.length === 0}
      <p class="gq-none">
        <span>{$t('proxies.rt.quota.none')}</span>
        <a
          class="gq-link"
          href="#/traffic?tab=limits&new=1&group={encodeURIComponent(group.id)}"
          data-testid="proxy-group-quota-set-{group.id}">{$t('proxies.rt.quota.set')}</a
        >
      </p>
    {:else}
      {#each rows as l (l.id)}
        {@const title = $t('proxies.rt.quota.title', { name: l.name })}
        {@const of = ofText(l)}
        {@const fill = hasData ? limitFill(l) : 0}
        <div class="gq-row" data-testid="proxy-group-quota-{group.id}-{l.id}">
          <div class="gq-line">
            <span class="gq-label gq-name" {title}>{title}</span>
            <span class="gq-tags">
              {#if l.state === 'disabled'}
                <Badge tone="neutral">{$t('traffic.limits.state_disabled')}</Badge>
              {/if}
              {#if onlyNotify(l)}
                <Badge tone="neutral">{$t('proxies.rt.quota.only_notify')}</Badge>
              {:else}
                <Badge variant="tag">{$t(`traffic.limits.action_${l.action}`)}</Badge>
              {/if}
            </span>
          </div>

          <div class="gq-line gq-line--start">
            {#if hasData}
              <span class="gq-value">{of}</span>
              {#if l.approx}
                <span class="gq-approx" title={$t('traffic.limits.approx')}>
                  <Icon name="info" size={14} />
                  <span class="gq-label">{$t('traffic.limits.approx')}</span>
                </span>
              {/if}
            {:else}
              <span class="gq-value gq-value--none" title={noDataTitle}>
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
            aria-valuenow={Math.round(fill)}
            aria-valuetext={hasData ? of : $t('traffic.limits.no_data')}
          >
            <div
              class="stat-bar-fill stat-bar-fill--{limitFillKind(l)}"
              style:width="{fill}%"
            ></div>
          </div>

          <span class="gq-label gq-meta">
            {$t('proxies.rt.quota.reset_at', {
              date: formatLimitDate(l.reset_at, clock?.offset)
            })}
          </span>

          {#if onlyNotify(l)}
            <span class="gq-label gq-meta">{$t('proxies.rt.quota.auto_note')}</span>
          {/if}
          {#if globalNote(l)}
            <span class="gq-label warning-text">
              {$t('proxies.rt.quota.global_note', { name: l.name })}
            </span>
          {/if}
        </div>
      {/each}
    {/if}
  </div>
{/if}

<style>
  .gq {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    padding: var(--spacing-3);
    margin-bottom: var(--spacing-4);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    min-width: 0;
  }

  .gq-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .gq-line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .gq-line--start {
    justify-content: flex-start;
  }

  .gq-tags {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
  }

  .gq-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.4;
  }

  .gq-name {
    flex: 1 1 0;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .gq-value {
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    font-variant-numeric: tabular-nums;
  }

  .gq-value--none {
    color: var(--fg-secondary);
  }

  .gq-approx {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    color: var(--fg-secondary);
  }

  .gq-meta {
    color: var(--fg-secondary);
  }

  .gq-none {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    color: var(--fg-secondary);
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .gq-link {
    color: var(--accent-text);
    text-decoration: none;
  }

  .gq-link:hover,
  .gq-link:focus-visible {
    text-decoration: underline;
  }

  .gq-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
    border-radius: var(--radius-sm);
  }

  @media (max-width: 768px) {
    .gq-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }
</style>
