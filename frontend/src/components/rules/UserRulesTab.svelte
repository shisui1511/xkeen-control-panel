<script lang="ts">
  import { t } from '../../i18n';
  import type { UserRule } from '../../lib/api';
  import Select from '../Select.svelte';
  import Button from '../Button.svelte';
  import EmptyState from '../EmptyState.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import RulesIcon from '../../lib/components/icons/Rules.svelte';
  import RuleModal from './RuleModal.svelte';

  interface Props {
    rules: UserRule[];
    groups: string[];
    loading: boolean;
    onSave: (rules: UserRule[]) => Promise<void>;
  }

  let { rules, groups, loading, onSave }: Props = $props();

  // Quick add form state
  let quickValue = $state('');
  let quickType = $state('domain_suffix');
  let quickTarget = $state('proxy');
  let quickGroup = $state('');
  let quickError = $state('');
  let adding = $state(false);

  // Modal edit state
  let isModalOpen = $state(false);
  let editingRule: UserRule | null = $state(null);

  // Reorder / Drag and drop
  let draggedIndex: number | null = $state(null);

  const typeOptions = $derived([
    { value: 'domain_suffix', label: $t('rules.custom_type_suffix') },
    { value: 'domain', label: $t('rules.custom_type_domain') },
    { value: 'domain_keyword', label: $t('rules.custom_type_keyword') },
    { value: 'ip_cidr', label: $t('rules.custom_type_ip') },
    { value: 'port', label: $t('rules.custom_type_port') }
  ]);

  const targetOptions = $derived([
    { value: 'proxy', label: $t('rules.target_proxy') },
    { value: 'direct', label: $t('rules.target_direct') },
    { value: 'reject', label: $t('rules.target_reject') }
  ]);

  const groupOptions = $derived.by(() => {
    const list = groups.length > 0 ? groups : ['PROXY'];
    return list.map((g) => ({ value: g, label: g }));
  });

  $effect(() => {
    if (!quickGroup && groups.length > 0) {
      quickGroup = groups[0];
    }
  });

  async function handleQuickAdd(e?: Event) {
    if (e) e.preventDefault();
    const cleanVal = quickValue.trim();
    if (!cleanVal) {
      quickError = $t('rules.value_placeholder');
      return;
    }
    quickError = '';
    adding = true;

    const newRule: UserRule = {
      id: 'rule_' + Date.now(),
      type: quickType,
      value: cleanVal,
      target: quickTarget,
      group: quickTarget === 'proxy' ? quickGroup || groups[0] || 'PROXY' : undefined,
      enabled: true
    };

    const nextRules = [newRule, ...rules];
    try {
      await onSave(nextRules);
      quickValue = '';
    } finally {
      adding = false;
    }
  }

  async function toggleRule(id: string) {
    const nextRules = rules.map((r) => (r.id === id ? { ...r, enabled: !r.enabled } : r));
    await onSave(nextRules);
  }

  async function deleteRule(id: string) {
    if (!confirm($t('rules.delete_confirm'))) return;
    const nextRules = rules.filter((r) => r.id !== id);
    await onSave(nextRules);
  }

  function openEditModal(rule: UserRule) {
    editingRule = rule;
    isModalOpen = true;
  }

  function openAddModal() {
    editingRule = null;
    isModalOpen = true;
  }

  async function handleModalSave(savedRule: UserRule) {
    let nextRules: UserRule[];
    if (editingRule) {
      nextRules = rules.map((r) => (r.id === savedRule.id ? savedRule : r));
    } else {
      nextRules = [savedRule, ...rules];
    }
    await onSave(nextRules);
  }

  async function moveRule(index: number, direction: 'up' | 'down') {
    const targetIndex = direction === 'up' ? index - 1 : index + 1;
    if (targetIndex < 0 || targetIndex >= rules.length) return;

    const copy = [...rules];
    const item = copy.splice(index, 1)[0];
    copy.splice(targetIndex, 0, item);
    await onSave(copy);
  }

  function handleDragStart(index: number) {
    draggedIndex = index;
  }

  function handleDragOver(e: DragEvent, index: number) {
    e.preventDefault();
  }

  async function handleDrop(targetIndex: number) {
    if (draggedIndex === null || draggedIndex === targetIndex) {
      draggedIndex = null;
      return;
    }

    const copy = [...rules];
    const item = copy.splice(draggedIndex, 1)[0];
    copy.splice(targetIndex, 0, item);
    draggedIndex = null;
    await onSave(copy);
  }

  function getTargetBadgeClass(target: string): string {
    const upper = target.toUpperCase();
    if (upper === 'DIRECT') return 'badge badge-success';
    if (upper === 'REJECT') return 'badge badge-danger';
    return 'badge badge-primary';
  }

  function getTypeName(type: string): string {
    switch (type) {
      case 'domain_suffix':
      case 'suffix':
        return 'SUFFIX';
      case 'domain':
        return 'DOMAIN';
      case 'domain_keyword':
      case 'keyword':
        return 'KEYWORD';
      case 'ip_cidr':
      case 'ip':
        return 'IP-CIDR';
      case 'port':
        return 'PORT';
      default:
        return type.toUpperCase();
    }
  }
</script>

<div class="user-rules-container">
  <!-- Quick Add Hero Card -->
  <div class="card quick-add-card">
    <div class="quick-add-header">
      <h3 class="quick-add-title">{$t('rules.quick_add')}</h3>
      <Button variant="secondary" class="btn-sm" onclick={openAddModal}>
        <Icon name="plus" size={14} />
        {$t('rules.add_rule')}
      </Button>
    </div>

    <form onsubmit={handleQuickAdd} class="quick-add-form">
      <div class="quick-add-field value-field">
        <input
          type="text"
          class="input font-mono"
          placeholder={$t('rules.value_placeholder')}
          bind:value={quickValue}
          required
        />
        {#if quickError}
          <span class="quick-error">{quickError}</span>
        {/if}
      </div>

      <div class="quick-add-field type-field">
        <Select bind:value={quickType} options={typeOptions} ariaLabel={$t('rules.rule_type')} />
      </div>

      <div class="quick-add-field target-field">
        <Select
          bind:value={quickTarget}
          options={targetOptions}
          ariaLabel={$t('rules.rule_target')}
        />
      </div>

      {#if quickTarget === 'proxy'}
        <div class="quick-add-field group-field">
          <Select
            bind:value={quickGroup}
            options={groupOptions}
            ariaLabel={$t('rules.rule_group')}
          />
        </div>
      {/if}

      <div class="quick-add-action">
        <Button variant="primary" type="submit" loading={adding} disabled={adding}>
          {$t('rules.add_custom_rule')}
        </Button>
      </div>
    </form>
  </div>

  <!-- Rules Table -->
  {#if loading && rules.length === 0}
    <div class="card loading-card">
      <div class="spinner"></div>
      <span>{$t('app.loading')}</span>
    </div>
  {:else if rules.length === 0}
    <EmptyState
      icon={RulesIcon}
      title={$t('rules.no_custom_rules')}
      description={$t('rules.custom_subtitle')}
      ctaText={$t('rules.add_rule')}
      oncta={openAddModal}
    />
  {:else}
    <div class="card table-card">
      <div class="table-responsive">
        <table class="rules-table">
          <thead>
            <tr>
              <th class="col-order">{$t('rules.order_col')}</th>
              <th class="col-status">{$t('rules.enabled_col')}</th>
              <th class="col-type">{$t('rules.type_col')}</th>
              <th class="col-value">{$t('rules.rule_value')}</th>
              <th class="col-target">{$t('rules.target')}</th>
              <th class="col-comment">{$t('rules.comment_col')}</th>
              <th class="col-actions">{$t('rules.actions_col')}</th>
            </tr>
          </thead>
          <tbody>
            {#each rules as rule, idx (rule.id)}
              <tr
                class="rule-row"
                class:row-disabled={!rule.enabled}
                draggable="true"
                ondragstart={() => handleDragStart(idx)}
                ondragover={(e) => handleDragOver(e, idx)}
                ondrop={() => handleDrop(idx)}
              >
                <!-- Reorder arrows / handle -->
                <td class="col-order">
                  <div class="order-controls">
                    <span class="drag-handle" title={$t('rules.drag_reorder')}>⠿</span>
                    <button
                      type="button"
                      class="icon-btn order-btn"
                      disabled={idx === 0}
                      onclick={() => moveRule(idx, 'up')}
                      title={$t('rules.priority_up')}
                      aria-label={$t('rules.priority_up')}
                    >
                      ▲
                    </button>
                    <button
                      type="button"
                      class="icon-btn order-btn"
                      disabled={idx === rules.length - 1}
                      onclick={() => moveRule(idx, 'down')}
                      title={$t('rules.priority_down')}
                      aria-label={$t('rules.priority_down')}
                    >
                      ▼
                    </button>
                  </div>
                </td>

                <!-- Toggle Switch -->
                <td class="col-status">
                  <label
                    class="toggle-switch"
                    title={rule.enabled ? $t('rules.rule_enabled') : $t('rules.rule_disabled')}
                  >
                    <input
                      type="checkbox"
                      checked={rule.enabled}
                      onchange={() => toggleRule(rule.id)}
                      aria-label={rule.enabled
                        ? $t('rules.rule_enabled')
                        : $t('rules.rule_disabled')}
                    />
                    <span class="toggle-slider"></span>
                  </label>
                </td>

                <!-- Rule Type Badge -->
                <td class="col-type">
                  <span class="type-badge">{getTypeName(rule.type)}</span>
                </td>

                <!-- Rule Value -->
                <td class="col-value mono font-mono">
                  <span class="value-text" title={rule.value}>{rule.value}</span>
                </td>

                <!-- Target / Group Badge -->
                <td class="col-target">
                  <div class="target-badges">
                    <span class={getTargetBadgeClass(rule.target)}>
                      {rule.target.toUpperCase()}
                    </span>
                    {#if rule.target === 'proxy' && rule.group}
                      <span class="badge badge-group" title={rule.group}>
                        {rule.group}
                      </span>
                    {/if}
                  </div>
                </td>

                <!-- Comment -->
                <td class="col-comment">
                  <span class="comment-text" title={rule.comment || ''}>
                    {rule.comment || '—'}
                  </span>
                </td>

                <!-- Actions -->
                <td class="col-actions">
                  <div class="row-actions">
                    <button
                      type="button"
                      class="icon-btn edit-btn"
                      onclick={() => openEditModal(rule)}
                      title={$t('rules.edit_rule')}
                      aria-label={$t('rules.edit_rule')}
                    >
                      <Icon name="edit" size={15} />
                    </button>
                    <button
                      type="button"
                      class="icon-btn delete-btn"
                      onclick={() => deleteRule(rule.id)}
                      title={$t('rules.delete_rule')}
                      aria-label={$t('rules.delete_rule')}
                    >
                      <Icon name="trash" size={15} />
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
</div>

<RuleModal
  isOpen={isModalOpen}
  rule={editingRule}
  {groups}
  onSave={handleModalSave}
  onClose={() => (isModalOpen = false)}
/>

<style>
  .user-rules-container {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .quick-add-card {
    padding: 16px 20px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  .quick-add-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 14px;
  }

  .quick-add-title {
    margin: 0;
    font-size: var(--font-size-md);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .quick-add-form {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 10px;
  }

  .value-field {
    flex: 2;
    min-width: 220px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .type-field,
  .target-field,
  .group-field {
    flex: 1;
    min-width: 140px;
  }

  .quick-add-action {
    display: flex;
    align-items: center;
  }

  .input {
    width: 100%;
    height: var(--input-h);
    padding: 0 12px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: var(--bg-elevated);
    color: var(--fg-primary);
    font-size: var(--font-size-sm);
    box-sizing: border-box;
    outline: none;
    transition: border-color var(--transition-fast);
  }

  .input:focus {
    border-color: var(--color-primary);
  }

  .font-mono {
    font-family: var(--font-family-mono);
  }

  .quick-error {
    font-size: var(--font-size-xs);
    color: var(--color-danger);
  }

  .table-card {
    padding: 0;
    overflow: hidden;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  .table-responsive {
    overflow-x: auto;
    scrollbar-width: thin;
  }

  .rules-table {
    width: 100%;
    border-collapse: collapse;
    text-align: left;
    font-size: var(--font-size-sm);
  }

  .rules-table th {
    padding: 12px 14px;
    background: var(--bg-elevated);
    color: var(--fg-secondary);
    font-weight: 600;
    font-size: var(--font-size-xs);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
  }

  .rules-table td {
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
    color: var(--fg-primary);
    vertical-align: middle;
  }

  .rules-table tbody tr:last-child td {
    border-bottom: none;
  }

  .rules-table tbody tr:hover {
    background: var(--bg-hover);
  }

  .row-disabled {
    opacity: 0.55;
  }

  .col-order {
    width: 80px;
    text-align: center;
  }

  .order-controls {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .drag-handle {
    cursor: grab;
    color: var(--fg-muted);
    font-size: 14px;
    user-select: none;
    padding: 2px 4px;
  }

  .drag-handle:active {
    cursor: grabbing;
  }

  .icon-btn {
    background: transparent;
    border: none;
    color: var(--fg-secondary);
    cursor: pointer;
    padding: 4px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all var(--transition-fast);
  }

  .icon-btn:hover:not(:disabled) {
    color: var(--fg-primary);
    background: var(--bg-hover);
  }

  .icon-btn:disabled {
    opacity: 0.25;
    cursor: not-allowed;
  }

  .order-btn {
    font-size: 10px;
    width: 20px;
    height: 20px;
  }

  .edit-btn:hover {
    color: var(--color-primary);
  }

  .delete-btn:hover {
    color: var(--color-danger);
  }

  .type-badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: var(--radius-sm);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    font-family: var(--font-family-mono);
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .value-text {
    display: block;
    max-width: 320px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .target-badges {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .badge-group {
    background: var(--bg-elevated);
    border: 1px dashed var(--border);
    color: var(--fg-secondary);
  }

  .comment-text {
    display: block;
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--fg-muted);
    font-size: var(--font-size-xs);
  }

  .row-actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .loading-card {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 48px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    color: var(--fg-muted);
  }

  .spinner {
    width: 18px;
    height: 18px;
    border: 2px solid var(--border);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
