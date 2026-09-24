<script lang="ts">
  import Modal from '../Modal.svelte';
  import Icon from '../Icon.svelte';
  import RuleForm from './RuleForm.svelte';
  import { t } from '../../i18n';
  import { getMihomoContext } from './MihomoContext.svelte';
  import type { Rule } from '../../lib/mihomoYaml';

  let ctx = getMihomoContext();

  let showRuleForm = $state(false);
  let editingRuleId = $state<string | null>(null);

  function newRuleDefaults(): Omit<Rule, 'id'> {
    return { type: 'DOMAIN-SUFFIX', value: '', outbound: 'DIRECT' };
  }

  let nr = $state<Omit<Rule, 'id'>>(newRuleDefaults());

  const allProxyNames = $derived([
    'DIRECT',
    'REJECT',
    ...ctx.proxies.map((p) => p.name),
    ...ctx.groups.map((g) => g.name)
  ]);

  const nonMatchRules = $derived(ctx.rules.filter((r) => r.type !== 'MATCH'));
  const matchRule = $derived(ctx.rules.find((r) => r.type === 'MATCH'));

  function openAddRule() {
    editingRuleId = null;
    nr = newRuleDefaults();
    showRuleForm = true;
  }

  function editRule(r: Rule) {
    editingRuleId = r.id;
    nr = { type: r.type, value: r.value, outbound: r.outbound };
    showRuleForm = true;
  }

  function saveRule() {
    if (!nr.outbound.trim()) return;
    if (editingRuleId) {
      ctx.updateRule(editingRuleId, { ...nr });
    } else {
      ctx.addRule({
        ...nr,
        id: crypto.randomUUID()
      });
    }
    showRuleForm = false;
    editingRuleId = null;
  }

  function moveRule(id: string, delta: number) {
    const idx = ctx.rules.findIndex((r) => r.id === id);
    if (idx < 0) return;
    const targetIdx = idx + delta;
    if (targetIdx >= 0 && targetIdx < ctx.rules.length) {
      ctx.moveRule(idx, targetIdx);
    }
  }
</script>

<div class="sec-body" data-testid="mihomo-section-rules">
  <div style="margin-bottom: 12px;">
    <button type="button" class="add-btn btn-secondary" onclick={openAddRule}>
      + {$t('mihomo.add_rule')}
    </button>
  </div>

  {#each nonMatchRules as r, i (r.id)}
    <div class="item-row item-row-rule">
      <div class="rule-order">
        <button
          type="button"
          class="order-btn"
          onclick={() => moveRule(r.id, -1)}
          disabled={i === 0}
          aria-label={$t('app.move_up')}><Icon name="chevron-up" size={11} /></button
        >
        <button
          type="button"
          class="order-btn"
          onclick={() => moveRule(r.id, 1)}
          disabled={i === nonMatchRules.length - 1}
          aria-label={$t('app.move_down')}><Icon name="chevron-down" size={11} /></button
        >
      </div>
      <span class="item-badge type-rule">{r.type}</span>
      <span class="item-name rule-value">{r.value}</span>
      <span class="item-meta">→ {r.outbound}</span>
      <button type="button" class="item-btn" onclick={() => editRule(r)} title={$t('app.edit')}>
        <Icon name="edit" />
      </button>
      <button
        type="button"
        class="item-del"
        onclick={() => ctx.removeRule(r.id)}
        title={$t('app.delete')}><Icon name="close" size={12} /></button
      >
    </div>
  {/each}

  {#if matchRule}
    <div class="item-row item-row-rule match-rule-row">
      <span class="item-badge type-match">MATCH</span>
      <span class="item-name rule-value" style="color: var(--fg-secondary); font-style: italic;">
        {$t('mihomo.match_rule_desc')}
      </span>
      <span class="item-meta">→ {matchRule.outbound}</span>
      <button
        type="button"
        class="item-btn"
        onclick={() => editRule(matchRule)}
        title={$t('app.edit')}
      >
        <Icon name="edit" />
      </button>
      <button
        type="button"
        class="item-del"
        onclick={() => ctx.removeRule(matchRule.id)}
        title={$t('app.delete')}><Icon name="close" size={12} /></button
      >
    </div>
  {/if}
</div>

<!-- Modal Rule Form -->
<Modal
  isOpen={showRuleForm}
  title={editingRuleId ? $t('mihomo.edit_rule_title') : $t('mihomo.add_rule_title')}
  onclose={() => {
    showRuleForm = false;
    editingRuleId = null;
  }}
>
  <RuleForm
    bind:nr
    {allProxyNames}
    onSave={saveRule}
    onCancel={() => {
      showRuleForm = false;
      editingRuleId = null;
    }}
  />
</Modal>

<style>
  .sec-body {
    padding: 16px;
  }

  .item-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    margin-bottom: 6px;
  }

  .item-badge {
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 4px;
    font-weight: 500;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  .type-rule {
    background: var(--surface-tint);
    color: var(--fg-dim);
    font-size: var(--font-size-xs);
  }

  .type-match {
    background: color-mix(in srgb, var(--seq-5) 15%, transparent);
    color: var(--seq-5);
    font-weight: 600;
  }

  .match-rule-row {
    margin-top: 8px;
    border-top: 1px dashed var(--border);
    background: var(--bg-elevated);
    border-left: 3px solid var(--seq-5);
  }

  .item-name {
    flex: 1;
    min-width: 50px;
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .rule-value {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
  }

  .item-meta {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    /* Long values (provider URLs, server addresses) shrink with an ellipsis
       instead of pushing the row actions off a phone screen. */
    min-width: 0;
    max-width: 50%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .item-btn {
    background: none;
    border: none;
    color: var(--fg-faint);
    cursor: pointer;
    font-size: 12px;
    padding: 4px 6px;
    border-radius: var(--radius-sm);
    transition: all var(--transition-fast);
    display: inline-flex;
    align-items: center;
  }

  .item-btn:hover {
    color: var(--fg-primary);
    background: var(--hover);
  }

  .item-del {
    background: none;
    border: none;
    color: var(--fg-faint);
    cursor: pointer;
    font-size: var(--font-size-xs);
    padding: 2px 4px;
    border-radius: var(--radius-sm);
    transition: color var(--transition-fast);
    flex-shrink: 0;
    line-height: 1;
    display: inline-flex;
    align-items: center;
  }

  .item-del:hover {
    color: var(--danger);
  }

  .rule-order {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex-shrink: 0;
  }

  .order-btn {
    background: none;
    border: none;
    color: var(--fg-faint);
    font-size: var(--font-size-xs);
    cursor: pointer;
    padding: 1px 3px;
    line-height: 1;
    transition: color var(--transition-fast);
    display: inline-flex;
    align-items: center;
  }

  .order-btn:hover:not(:disabled) {
    color: var(--fg-primary);
  }

  .order-btn:disabled {
    opacity: 0.3;
    cursor: default;
  }

  .add-btn {
    font-size: 12px;
    padding: 6px 12px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all var(--transition-fast);
  }

  .btn-secondary {
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--fg-primary);
  }

  .btn-secondary:hover {
    background: var(--bg-hover);
  }
</style>
