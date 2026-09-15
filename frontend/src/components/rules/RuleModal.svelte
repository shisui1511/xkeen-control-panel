<script lang="ts">
  import { t } from '../../i18n';
  import type { UserRule } from '../../lib/api';
  import Modal from '../Modal.svelte';
  import Select from '../Select.svelte';
  import Button from '../Button.svelte';

  interface Props {
    isOpen: boolean;
    rule: UserRule | null;
    groups: string[];
    onSave: (r: UserRule) => void;
    onClose: () => void;
  }

  let { isOpen, rule, groups, onSave, onClose }: Props = $props();

  let ruleValue = $state('');
  let ruleType = $state('domain_suffix');
  let ruleTarget = $state('proxy');
  let ruleGroup = $state('');
  let ruleComment = $state('');
  let validationError = $state('');

  $effect(() => {
    if (isOpen) {
      if (rule) {
        ruleValue = rule.value;
        ruleType = rule.type;
        ruleTarget = rule.target;
        ruleGroup = rule.group || groups[0] || 'PROXY';
        ruleComment = rule.comment || '';
      } else {
        ruleValue = '';
        ruleType = 'domain_suffix';
        ruleTarget = 'proxy';
        ruleGroup = groups[0] || 'PROXY';
        ruleComment = '';
      }
      validationError = '';
    }
  });

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

  function handleSubmit(e?: Event) {
    if (e) e.preventDefault();
    const cleanVal = ruleValue.trim();
    if (!cleanVal) {
      validationError = $t('rules.value_placeholder');
      return;
    }
    onSave({
      id: rule ? rule.id : 'rule_' + Date.now(),
      type: ruleType,
      value: cleanVal,
      target: ruleTarget,
      group: ruleTarget === 'proxy' ? ruleGroup : undefined,
      comment: ruleComment.trim(),
      enabled: rule ? rule.enabled : true
    });
    onClose();
  }
</script>

<Modal
  {isOpen}
  title={rule ? $t('rules.edit_rule') : $t('rules.add_rule')}
  onclose={onClose}
  maxWidth="500px"
>
  <form onsubmit={handleSubmit} class="rule-form">
    <div class="form-group">
      <label for="rule-value" class="form-label">{$t('rules.rule_value')}</label>
      <input
        id="rule-value"
        type="text"
        class="input font-mono"
        placeholder={$t('rules.value_placeholder')}
        bind:value={ruleValue}
        required
      />
      {#if validationError}
        <span class="form-error">{validationError}</span>
      {/if}
    </div>

    <div class="form-row">
      <div class="form-group">
        <label for="rule-type" class="form-label">{$t('rules.rule_type')}</label>
        <Select id="rule-type" bind:value={ruleType} options={typeOptions} />
      </div>

      <div class="form-group">
        <label for="rule-target" class="form-label">{$t('rules.rule_target')}</label>
        <Select id="rule-target" bind:value={ruleTarget} options={targetOptions} />
      </div>
    </div>

    {#if ruleTarget === 'proxy'}
      <div class="form-group">
        <label for="rule-group" class="form-label">{$t('rules.rule_group')}</label>
        <Select id="rule-group" bind:value={ruleGroup} options={groupOptions} />
      </div>
    {/if}

    <div class="form-group">
      <label for="rule-comment" class="form-label">{$t('rules.rule_comment')}</label>
      <input
        id="rule-comment"
        type="text"
        class="input"
        placeholder={$t('rules.custom_comment')}
        bind:value={ruleComment}
      />
    </div>

    <div class="modal-actions">
      <Button variant="secondary" onclick={onClose}>
        {$t('rules.cancel_modal')}
      </Button>
      <Button variant="primary" type="submit">
        {$t('rules.save_modal')}
      </Button>
    </div>
  </form>
</Modal>

<style>
  .rule-form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .form-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  @media (max-width: 480px) {
    .form-row {
      grid-template-columns: 1fr;
    }
  }

  .form-label {
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--fg-secondary);
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
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .font-mono {
    font-family: var(--font-family-mono);
  }

  .form-error {
    font-size: var(--font-size-xs);
    color: var(--danger);
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    margin-top: 8px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }
</style>
