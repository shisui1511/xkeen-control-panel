<script lang="ts">
  import { onMount } from 'svelte';
  import { t, tp } from '../../../i18n';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import { layerSnapshot } from '../../../lib/configLayer';
  import { routingView, routingStatus, loadRouting } from '../../../lib/configRouting';
  import type { Cond, Target } from '../../../lib/configRouting';
  import { condLabelKey, targetLabel } from '../../../lib/routingView';

  // Представление маршрутизации читается при открытии вкладки; дальше его перечитывают
  // события SSE routing, sources и draft (подписки регистрирует configRouting.ts).
  onMount(() => {
    void loadRouting();
  });

  // Значений в чипе условия показано до трёх, остальное — чипом «+{n}»
  const VALUES_SHOWN = 3;

  const loading = $derived(
    $layerSnapshot === null || $routingStatus === 'idle' || $routingStatus === 'loading'
  );
  const failed = $derived(!loading && $routingStatus === 'error' && $routingView === null);
  const rules = $derived($routingView?.rules ?? []);
  const groups = $derived($routingView?.groups ?? []);
  const desired = $derived($routingView?.desired ?? false);
  const empty = $derived(!loading && !failed && $routingView !== null && rules.length === 0);
  const showList = $derived(!loading && !failed && rules.length > 0);
  // «Всё остальное» есть, если маршрутизация желаемо взята или в черновике есть правила
  const showFallback = $derived(
    !loading && !failed && $routingView !== null && (desired || rules.length > 0)
  );
  const finalTarget = $derived($routingView?.final ?? { kind: 'direct' });

  function valuesOf(c: Cond): string[] {
    return [...c.values, ...c.lists.map((l) => `${l.file} · ${l.tag}`)];
  }

  function shown(c: Cond): string[] {
    return valuesOf(c).slice(0, VALUES_SHOWN);
  }

  function hiddenCount(c: Cond): number {
    return Math.max(0, valuesOf(c).length - VALUES_SHOWN);
  }

  function isReject(target: Target): boolean {
    return target.kind === 'reject';
  }
</script>

<div class="rules-tab" data-testid="rules-tab">
  {#if loading}
    <div class="rules-skeleton" aria-hidden="true">
      <Skeleton type="card" height="120px" />
      <Skeleton type="card" height="120px" />
    </div>
  {:else if failed}
    <EmptyState
      title={$t('cfg.load_error_title')}
      description={$t('cfg.load_error_body')}
      ctaText={$t('cfg.load_error_retry')}
      plain
      oncta={() => void loadRouting()}
    />
  {:else}
    {#if empty}
      <EmptyState
        title={$t('cfg.rules.empty_title')}
        description={$t('cfg.rules.empty_body')}
        plain
      />
    {:else if showList}
      <div class="rules-panel">
        <span class="rules-count" data-testid="rules-count">
          {$tp('cfg.rules.count', rules.length)}
        </span>
      </div>
      <div class="card rules-card">
        <ol class="rules-list" aria-label={$t('cfg.rules.list_aria')}>
          {#each rules as rule (rule.id)}
            <li class="rules-row" data-testid="rule-row-{rule.id}">
              <span class="rules-num">{rule.index}</span>
              <div class="rules-conds">
                {#each rule.conds as cond, i (i)}
                  {#if i > 0}
                    <span class="rules-and">{$t('cfg.rules.and')}</span>
                  {/if}
                  <span class="chip chip-default rules-cond">
                    <span>{$t(condLabelKey(cond.type))}</span>
                    {#each shown(cond) as value, vi (vi)}
                      <span class="rules-value">{vi === 0 ? '· ' : ''}{value}</span>
                    {/each}
                  </span>
                  {#if hiddenCount(cond) > 0}
                    <span class="chip chip-default" title={valuesOf(cond).join(', ')}>
                      {$t('cfg.rules.more_values', { n: hiddenCount(cond) })}
                    </span>
                  {/if}
                {/each}
                {#if rule.raw}
                  <span class="chip chip-default rules-cond">{$t('cfg.rules.raw_title')}</span>
                {/if}
              </div>
              <span class="rules-target">
                <span aria-hidden="true">→</span>
                <span class="chip {isReject(rule.target) ? 'chip-danger' : 'chip-default'}">
                  {targetLabel($t, rule.target, groups)}
                </span>
              </span>
            </li>
          {/each}
        </ol>
      </div>
    {/if}
    {#if showFallback}
      <div class="rules-fallback" data-testid="rules-fallback">
        <span class="rules-fallback-text">
          {$t('cfg.rules.fallback', { target: targetLabel($t, finalTarget, groups) })}
        </span>
      </div>
    {/if}
  {/if}
</div>

<style>
  .rules-tab {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
    min-width: 0;
  }

  .rules-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--grid-gap, 16px);
  }

  .rules-panel {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--spacing-2) var(--spacing-4);
    min-height: var(--btn-h, 38px);
  }

  .rules-count {
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .rules-card {
    min-width: 0;
    padding: 0;
  }

  .rules-list {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .rules-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
    padding: var(--spacing-2) var(--spacing-4);
    border-top: 1px solid var(--border);
  }

  .rules-row:first-child {
    border-top: none;
  }

  .rules-num {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .rules-conds {
    display: flex;
    flex: 1 1 200px;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .rules-cond {
    flex-wrap: wrap;
    gap: var(--spacing-1);
    overflow-wrap: anywhere;
  }

  .rules-value {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .rules-and {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .rules-target {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .rules-fallback {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
    padding: var(--spacing-2) var(--spacing-4);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-top: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
  }

  .rules-fallback-text {
    min-width: 0;
    font-size: var(--font-size-base);
    overflow-wrap: anywhere;
  }
</style>
