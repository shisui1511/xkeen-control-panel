<script lang="ts">
  import { t } from '../../../i18n';
  import { showConfirm, showToast } from '../../../stores';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, handleLayerError } from '../../../lib/configLayer';
  import { deleteRule, moveRule, setRuleEnabled } from '../../../lib/configRouting';
  import type { Cond, IssueView, RuleView } from '../../../lib/configRouting';
  import { chipKind, condLabelKey, issueTextKey, targetLabel } from '../../../lib/routingView';

  // Строка правила (UI-SPEC «Screen Contracts» 4): номер, условия чипами, цель, переключатель,
  // кнопки «вверх/вниз», «изменить», «удалить» и чипы статуса. Перекрытия, дубли и ненайденные
  // списки приходят от сервера готовыми (warnings, issues) — клиент их не пересчитывает (D-40).
  // Перестановка идёт через onmove: фокус и объявление позиции ведёт список.
  let { rule, count, groups, onmove, onedit } = $props<{
    rule: RuleView;
    count: number;
    groups: ReadonlyArray<{ id: string; name: string }>;
    onmove: (rule: RuleView, dir: -1 | 1) => void;
    onedit?: (rule: RuleView) => void;
  }>();

  // Значений в чипе условия показано до трёх, остальное — чипом «+{n}»
  const VALUES_SHOWN = 3;
  // Своё правило ядра в чипе обрезается: полный текст — в title
  const RAW_SHOWN = 80;

  let busy = $state(false);

  const locked = $derived(busy || $applyRunning);
  const first = $derived(rule.index <= 1);
  const last = $derived(rule.index >= count);
  const hasIssues = $derived(rule.issues.length > 0);
  const hasWarnings = $derived(rule.warnings.length > 0);
  const hasStatus = $derived(
    rule.origin === 'preset' ||
      rule.only !== '' ||
      !rule.enabled ||
      rule.issues.length > 0 ||
      rule.warnings.length > 0
  );

  function valuesOf(c: Cond): string[] {
    const vals = c.type === 'network' ? c.values.map((v) => v.toUpperCase()) : c.values;
    return [...vals, ...c.lists.map((l) => `${l.file} · ${l.tag}`)];
  }

  function shownOf(c: Cond): string {
    return valuesOf(c).slice(0, VALUES_SHOWN).join(', ');
  }

  function hiddenOf(c: Cond): number {
    return Math.max(0, valuesOf(c).length - VALUES_SHOWN);
  }

  function warningN(w: IssueView): number {
    return Number(w.params?.n ?? 0);
  }

  function warningText(w: IssueView): string {
    const n = warningN(w);
    if (w.code === 'overlap') return $t('cfg.rules.overlap_chip', { n });
    if (w.code === 'exact_dup') return $t('cfg.rules.exact_dup', { n });
    if (w.code === 'below_catchall') return $t('cfg.rules.below_catchall', { n });
    return $t(issueTextKey(w.code), w.params ?? {});
  }

  function warningTitle(w: IssueView): string | undefined {
    return w.code === 'overlap'
      ? $t('cfg.rules.overlap_title', { n: warningN(w) })
      : warningText(w);
  }

  function issueText(i: IssueView): string {
    const p = { file: i.params?.file ?? '?', tag: i.params?.tag ?? '?' };
    if (i.code === 'list_not_found') return $t('cfg.rules.list_missing_chip', p);
    if (i.code === 'list_too_large') return $t('cfg.rules.list_too_large', p);
    return $t(issueTextKey(i.code), i.params ?? {});
  }

  const rawText = $derived(rule.raw ? rule.raw.text : '');
  const rawShort = $derived(
    rawText.length > RAW_SHOWN ? `${rawText.slice(0, RAW_SHOWN)}…` : rawText
  );

  async function toggleEnabled(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const want = input.checked;
    busy = true;
    try {
      await setRuleEnabled(rule.id, want);
    } catch (err) {
      input.checked = rule.enabled;
      handleLayerError(err);
    } finally {
      busy = false;
    }
  }

  async function raise(w: IssueView) {
    const n = warningN(w);
    if (n < 1 || busy) return;
    busy = true;
    try {
      await moveRule(rule.id, n - 1);
    } catch (err) {
      handleLayerError(err);
    } finally {
      busy = false;
    }
  }

  async function remove() {
    const ok = await showConfirm({
      variant: 'danger',
      title: $t('cfg.rules.delete_title', { n: rule.index }),
      message: $t('cfg.rules.delete_body'),
      confirmLabel: $t('cfg.rules.delete_confirm'),
      cancelLabel: $t('cfg.nodes.cancel')
    });
    if (!ok) return;
    busy = true;
    try {
      await deleteRule(rule.id);
      showToast('success', $t('cfg.rules.toast_deleted'));
    } catch (err) {
      // Запись в черновик не удалась: диалог закрыт, правило остаётся на месте
      handleLayerError(err);
    } finally {
      busy = false;
    }
  }
</script>

<li
  class="rr"
  class:rr-off={!rule.enabled}
  class:rr-danger={hasIssues}
  class:rr-warn={!hasIssues && hasWarnings}
  class:rr-has-status={hasStatus}
  data-testid="rule-row-{rule.id}"
>
  <span class="rr-grip" aria-hidden="true" data-rule-grip>
    <Icon name="grip" size={16} />
  </span>
  <span class="rr-num" data-testid="rule-num-{rule.id}">{rule.index}</span>

  <div class="rr-conds" data-testid="rule-conds-{rule.id}">
    {#each rule.conds as cond, i (i)}
      {#if i > 0}
        <span class="rr-and">{$t('cfg.rules.and')}</span>
      {/if}
      <span class="chip chip-default rr-cond">
        <span>{$t(condLabelKey(cond.type))}</span>
        {#if valuesOf(cond).length > 0}
          <span class="rr-value">· {shownOf(cond)}</span>
        {/if}
      </span>
      {#if hiddenOf(cond) > 0}
        <span class="chip chip-default" title={valuesOf(cond).join(', ')}>
          {$t('cfg.rules.more_values', { n: hiddenOf(cond) })}
        </span>
      {/if}
    {/each}
    {#if rule.raw}
      <span class="chip chip-default rr-cond" title={rawText}>
        <span>{$t('cfg.rules.raw_title')}</span>
        <span class="rr-value">· {rawShort}</span>
      </span>
    {/if}
  </div>

  <span class="rr-target">
    <Icon name="arrow-right" size={14} />
    <span
      class="chip {rule.target.kind === 'reject' ? 'chip-danger' : 'chip-default'} rr-target-chip"
      class:rr-mono={rule.target.kind !== 'group'}
    >
      {targetLabel($t, rule.target, groups)}
    </span>
  </span>

  <label class="toggle-switch rr-switch">
    <input
      type="checkbox"
      checked={rule.enabled}
      disabled={locked}
      aria-label={$t('cfg.rules.enabled_aria', { n: rule.index })}
      data-testid="rule-enabled-{rule.id}"
      onchange={toggleEnabled}
    />
    <span class="toggle-slider"></span>
  </label>

  <span class="rr-actions">
    <button
      type="button"
      class="btn btn-secondary btn-sm rr-act"
      title={$t('cfg.rules.up_aria', { n: rule.index })}
      aria-label={$t('cfg.rules.up_aria', { n: rule.index })}
      disabled={first || locked}
      data-testid="rule-up-{rule.id}"
      onclick={() => onmove(rule, -1)}
    >
      <Icon name="chevron-up" size={14} />
    </button>
    <button
      type="button"
      class="btn btn-secondary btn-sm rr-act"
      title={$t('cfg.rules.down_aria', { n: rule.index })}
      aria-label={$t('cfg.rules.down_aria', { n: rule.index })}
      disabled={last || locked}
      data-testid="rule-down-{rule.id}"
      onclick={() => onmove(rule, 1)}
    >
      <Icon name="chevron-down" size={14} />
    </button>
    {#if onedit}
      <button
        type="button"
        class="btn btn-secondary btn-sm rr-act"
        title={$t('cfg.rules.edit_aria', { n: rule.index })}
        aria-label={$t('cfg.rules.edit_aria', { n: rule.index })}
        disabled={$applyRunning}
        data-testid="rule-edit-{rule.id}"
        onclick={() => onedit(rule)}
      >
        <Icon name="edit" size={14} />
      </button>
    {/if}
    <button
      type="button"
      class="btn btn-secondary btn-sm rr-act"
      title={$t('cfg.rules.delete_aria', { n: rule.index })}
      aria-label={$t('cfg.rules.delete_aria', { n: rule.index })}
      disabled={locked}
      data-testid="rule-delete-{rule.id}"
      onclick={() => void remove()}
    >
      <Icon name="delete" size={14} />
    </button>
  </span>

  {#if hasStatus}
    <div class="rr-status" data-testid="rule-status-{rule.id}">
      {#if rule.origin === 'preset'}
        <span class="chip chip-default" data-chip="preset">{$t('cfg.rules.from_preset_chip')}</span>
      {/if}
      {#if rule.only === 'xray' || rule.only === 'mihomo'}
        <span class="chip chip-info" data-chip="only-{rule.only}">
          {$t(rule.only === 'xray' ? 'cfg.rules.only_xray' : 'cfg.rules.only_mihomo')}
        </span>
      {/if}
      {#if !rule.enabled}
        <span class="chip chip-default" data-chip="disabled">{$t('cfg.rules.disabled_chip')}</span>
      {/if}
      {#each rule.issues as issue, i (i)}
        <span
          class="chip {chipKind(issue) === 'danger' ? 'chip-danger' : 'chip-warning'} rr-chip-text"
          data-chip={issue.code}
        >
          {issueText(issue)}
        </span>
      {/each}
      {#each rule.warnings as w, i (i)}
        <span class="chip chip-warning rr-chip-text" title={warningTitle(w)} data-chip={w.code}>
          {warningText(w)}
        </span>
        {#if w.code === 'overlap' && warningN(w) >= 1}
          <button
            type="button"
            class="btn btn-secondary btn-sm rr-raise"
            disabled={locked}
            data-testid="rule-raise-{rule.id}"
            onclick={() => void raise(w)}
          >
            {$t('cfg.rules.raise_btn')}
          </button>
        {/if}
      {/each}
    </div>
  {/if}
</li>

<style>
  .rr {
    display: grid;
    grid-template-columns: 16px 28px minmax(0, 1fr) auto auto auto;
    grid-template-areas: 'grip num conds target toggle actions';
    align-items: center;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
    padding: var(--spacing-2) var(--spacing-4);
    border-top: 1px solid var(--border);
    border-left: 3px solid transparent;
    font-size: var(--font-size-base);
    line-height: 1.4;
    color: var(--fg-primary);
  }

  .rr:first-child {
    border-top: none;
  }

  .rr-has-status {
    grid-template-areas:
      'grip num conds target toggle actions'
      '.    .   status status status status';
  }

  .rr-danger {
    border-left-color: var(--danger);
  }

  .rr-warn {
    border-left-color: var(--warning);
  }

  /* Выключенное правило приглушается цветом текста, а не прозрачностью (контраст) */
  .rr-off,
  .rr-off .rr-num,
  .rr-off .chip-default,
  .rr-off .rr-and,
  .rr-off .rr-target {
    color: var(--fg-dim);
  }

  /* Ручка перетаскивания видна только при точном указателе (перетаскивание подключает 146-29) */
  .rr-grip {
    grid-area: grip;
    display: none;
    color: var(--fg-dim);
  }

  @media (pointer: fine) {
    .rr-grip {
      display: inline-flex;
    }
  }

  .rr-num {
    grid-area: num;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .rr-conds {
    grid-area: conds;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .rr-cond {
    flex-wrap: wrap;
    gap: var(--spacing-1);
    white-space: normal;
    max-width: 100%;
    overflow-wrap: anywhere;
  }

  .rr-value {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .rr-and {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .rr-target {
    grid-area: target;
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    min-width: 0;
    color: var(--fg-secondary);
  }

  .rr-target-chip {
    white-space: normal;
    overflow-wrap: anywhere;
  }

  .rr-mono {
    font-family: var(--font-family-mono);
  }

  .rr-switch {
    grid-area: toggle;
    flex-shrink: 0;
  }

  .rr-actions {
    grid-area: actions;
    display: inline-flex;
    gap: var(--spacing-1);
  }

  .rr-act {
    min-width: 32px;
    min-height: 32px;
    padding: 0 var(--spacing-2);
    justify-content: center;
  }

  .rr-status {
    grid-area: status;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1) var(--spacing-2);
    min-width: 0;
  }

  .rr-chip-text {
    white-space: normal;
    max-width: 100%;
    overflow-wrap: anywhere;
  }

  .rr-raise {
    min-height: 32px;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .rr-act,
    .rr-raise {
      min-width: 44px;
      min-height: 44px;
    }
  }

  /* Узко: три ряда — номер, переключатель и кнопки; условия; цель и статусы */
  @media (max-width: 768px) {
    .rr,
    .rr-has-status {
      grid-template-columns: auto auto minmax(0, 1fr);
      grid-template-areas:
        'num toggle actions'
        'conds conds conds'
        'target target target';
      gap: var(--spacing-2) var(--spacing-3);
      padding: var(--spacing-2) var(--spacing-3);
    }

    .rr-has-status {
      grid-template-areas:
        'num toggle actions'
        'conds conds conds'
        'target status status';
    }

    .rr-grip {
      display: none;
    }

    .rr-actions {
      justify-self: end;
    }
  }
</style>
