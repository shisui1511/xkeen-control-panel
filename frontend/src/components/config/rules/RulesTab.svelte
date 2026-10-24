<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { t, tp } from '../../../i18n';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Select from '../../Select.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, handleLayerError, layerSnapshot } from '../../../lib/configLayer';
  import {
    routingView,
    routingStatus,
    loadRouting,
    moveRule,
    setFinal
  } from '../../../lib/configRouting';
  import type { RuleView, Target } from '../../../lib/configRouting';
  import { targetLabel } from '../../../lib/routingView';
  import Button from '../../Button.svelte';
  import RuleRow from './RuleRow.svelte';
  import RuleEditModal from './RuleEditModal.svelte';
  import PresetModal from './PresetModal.svelte';
  import { routingBusy } from '../routing/routingOwner';

  const uid = $props.id();

  // Представление маршрутизации читается при открытии вкладки; дальше его перечитывают
  // события SSE routing, sources и draft (подписки регистрирует configRouting.ts).
  onMount(() => {
    void loadRouting();
  });

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

  // --- Перестановка кнопками и перетаскиванием ---
  // После ответа сервера порядок в представлении обновляется отдельным запросом: фокус и
  // объявление ждут, пока правило окажется на новой позиции. dir 0 — перетаскивание,
  // фокус при этом не двигается.
  interface PendingMove {
    id: string;
    dir: -1 | 0 | 1;
    to: number;
  }

  let moving = $state(false);
  let pending = $state<PendingMove | null>(null);
  let live = $state('');
  let pendingTimer: ReturnType<typeof setTimeout> | null = null;
  let tabEl: HTMLDivElement | undefined;

  function clearPending() {
    pending = null;
    if (pendingTimer !== null) clearTimeout(pendingTimer);
    pendingTimer = null;
  }

  async function onmove(rule: RuleView, dir: -1 | 1) {
    await performMove(rule.id, rule.index - 1 + dir, dir);
  }

  /** Переставляет правило на позицию `to` (с нуля); порядок пишет сервер, ревизию проверяет он. */
  async function performMove(id: string, to: number, dir: -1 | 0 | 1) {
    if (moving || $applyRunning) return;
    if (to < 0 || to >= rules.length) return;
    moving = true;
    try {
      await moveRule(id, to);
      pending = { id, dir, to: to + 1 };
      // Если представление не обновилось, объявление и фокус всё равно не зависают
      pendingTimer = setTimeout(clearPending, 4000);
      settleMove(rules);
    } catch (err) {
      handleLayerError(err);
    } finally {
      moving = false;
    }
  }

  // Перестановка завершена, когда правило в представлении стоит на новой позиции. Порядок
  // ответа и обновления представления не задан: проверка идёт и после ответа, и при
  // каждом обновлении представления
  function settleMove(list: RuleView[]) {
    const p = pending;
    if (p === null) return;
    const rule = list.find((r) => r.id === p.id);
    if (!rule || rule.index !== p.to) return;
    clearPending();
    live = $t('cfg.rules.moved_live', { n: rule.index });
    if (p.dir === 0) return;
    void tick().then(() => {
      // Фокус остаётся на той же кнопке той же строки; у границы она неактивна — берём парную
      const same = tabEl?.querySelector<HTMLButtonElement>(
        `[data-testid="rule-${p.dir < 0 ? 'up' : 'down'}-${p.id}"]`
      );
      const other = tabEl?.querySelector<HTMLButtonElement>(
        `[data-testid="rule-${p.dir < 0 ? 'down' : 'up'}-${p.id}"]`
      );
      (same && !same.disabled ? same : other)?.focus();
    });
  }

  onDestroy(routingView.subscribe((v) => settleMove(v?.rules ?? [])));

  // --- Перетаскивание (нативный HTML5 DnD за ручку) ---
  let dragId = $state<string | null>(null);
  let dropAt = $state<{ id: string; pos: 'before' | 'after' } | null>(null);
  const dragLocked = $derived(moving || $applyRunning);

  /** Позиция правила (с нуля) после переноса из `from` в щель над или под строкой `target`. */
  function destOf(from: number, target: number, pos: 'before' | 'after'): number {
    const slot = pos === 'before' ? target : target + 1;
    return from < slot ? slot - 1 : slot;
  }

  function onpickup(rule: RuleView) {
    if (dragLocked) return;
    dragId = rule.id;
    dropAt = null;
  }

  function onhover(rule: RuleView, pos: 'before' | 'after') {
    if (dragId === null || dragLocked) return;
    const from = rules.findIndex((r) => r.id === dragId);
    const target = rules.findIndex((r) => r.id === rule.id);
    // Линия не рисуется там, где правило и так стоит: на себе и на соседней щели
    if (from < 0 || target < 0 || destOf(from, target, pos) === from) {
      dropAt = null;
      return;
    }
    if (dropAt?.id !== rule.id || dropAt.pos !== pos) dropAt = { id: rule.id, pos };
  }

  function onleave(rule: RuleView) {
    if (dropAt?.id === rule.id) dropAt = null;
  }

  function onputdown() {
    dragId = null;
    dropAt = null;
  }

  function ondropon(rule: RuleView) {
    const id = dragId;
    const at = dropAt;
    onputdown();
    if (id === null || at === null || at.id !== rule.id) return;
    const from = rules.findIndex((r) => r.id === id);
    const target = rules.findIndex((r) => r.id === rule.id);
    if (from < 0 || target < 0) return;
    void performMove(id, destOf(from, target, at.pos), 0);
  }

  // --- Диалог правила (одна копия на вкладку) ---
  let editOpen = $state(false);
  // Правило остаётся в состоянии и после закрытия, пока диалог гаснет
  let editRule = $state<RuleView | null>(null);

  function openNew() {
    editRule = null;
    editOpen = true;
  }

  function openEdit(rule: RuleView) {
    editRule = rule;
    editOpen = true;
  }

  // --- Диалог пресета (одна копия на вкладку) ---
  let presetOpen = $state(false);

  // --- «Всё остальное» ---
  function targetValue(target: Target): string {
    return target.kind === 'group' ? `group:${target.id ?? ''}` : target.kind;
  }

  function targetOf(value: string): Target {
    if (value.startsWith('group:')) return { kind: 'group', id: value.slice('group:'.length) };
    return { kind: value === 'reject' ? 'reject' : 'direct' };
  }

  const targetOptions = $derived([
    ...groups.map((g) => ({ value: `group:${g.id}`, label: g.name })),
    { value: 'direct', label: 'DIRECT' },
    { value: 'reject', label: 'REJECT' }
  ]);
  const finalValue = $derived(targetValue(finalTarget));

  async function changeFinal(e: Event & { currentTarget: HTMLSelectElement }) {
    const select = e.currentTarget;
    try {
      await setFinal(targetOf(select.value));
    } catch (err) {
      select.value = finalValue;
      handleLayerError(err);
    }
  }
</script>

<div class="rules-tab" data-testid="rules-tab" bind:this={tabEl}>
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
    <div class="rules-panel">
      <Button
        variant="secondary"
        disabled={$applyRunning || $routingBusy}
        data-testid="rules-preset"
        onclick={() => (presetOpen = true)}
      >
        {$t('cfg.rules.preset_btn')}
      </Button>
      {#if showList}
        <span class="rules-count" data-testid="rules-count">
          {$tp('cfg.rules.count', rules.length)}
        </span>
      {/if}
      <Button
        variant="primary"
        class="rules-add-btn"
        disabled={$applyRunning}
        data-testid="rules-add"
        onclick={openNew}
      >
        <Icon name="add" size={14} />
        {$t('cfg.rules.add')}
      </Button>
    </div>
    {#if empty}
      <EmptyState
        title={$t('cfg.rules.empty_title')}
        description={$t('cfg.rules.empty_body')}
        ctaText={$t('cfg.rules.empty_cta')}
        plain
        oncta={() => (presetOpen = true)}
      />
    {:else if showList}
      <div class="card rules-card">
        <ol class="rules-list" aria-label={$t('cfg.rules.list_aria')}>
          {#each rules as rule (rule.id)}
            <RuleRow
              {rule}
              count={rules.length}
              {groups}
              {onmove}
              onedit={openEdit}
              {dragId}
              {dropAt}
              {dragLocked}
              {onpickup}
              {onhover}
              {onleave}
              {ondropon}
              {onputdown}
            />
          {/each}
        </ol>
        {#if showFallback}
          {@render fallback()}
        {/if}
      </div>
    {/if}
    {#if empty && showFallback}
      <div class="card rules-card">
        {@render fallback()}
      </div>
    {/if}
  {/if}
  <div class="sr-only" aria-live="polite" data-testid="rules-live">{live}</div>
</div>

<RuleEditModal open={editOpen} rule={editRule} onclose={() => (editOpen = false)} />
<PresetModal open={presetOpen} mode="preset" onclose={() => (presetOpen = false)} />

{#snippet fallback()}
  <div class="rules-fallback" data-testid="rules-fallback">
    <label class="rules-fallback-label" for="{uid}-final">
      <span>{$t('cfg.rules.fallback_label')}</span>
      <Icon name="arrow-right" size={14} />
    </label>
    <Select
      id="{uid}-final"
      wrapperClass="rules-fallback-select"
      value={finalValue}
      disabled={$applyRunning}
      options={targetOptions}
      data-testid="rules-fallback-select"
      onchange={changeFinal}
    />
  </div>
{/snippet}

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
    gap: var(--spacing-2) var(--spacing-4);
    min-height: var(--btn-h, 38px);
  }

  /* «Добавить правило» всегда у правого края, «Пресет» и счётчик — слева */
  .rules-panel :global(.rules-add-btn) {
    margin-left: auto;
  }

  .rules-count {
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .rules-card {
    min-width: 0;
    padding: 0;
    overflow: hidden;
  }

  .rules-list {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .rules-fallback {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
    padding: var(--spacing-2) var(--spacing-4);
    background: var(--bg-elevated);
    border-top: 1px solid var(--border-strong);
    font-size: var(--font-size-base);
  }

  .rules-fallback-label {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .rules-fallback :global(.rules-fallback-select) {
    flex: 0 1 280px;
    width: auto;
    min-width: 160px;
  }

  @media (max-width: 768px) {
    .rules-fallback {
      flex-direction: column;
      align-items: stretch;
    }

    .rules-fallback :global(.rules-fallback-select) {
      flex: 1 1 auto;
      width: 100%;
    }
  }
</style>
