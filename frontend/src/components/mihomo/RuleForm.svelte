<script lang="ts">
  import { t } from '../../i18n';
  import Select from '../Select.svelte';

  let {
    nr = $bindable(),
    allProxyNames,
    onSave,
    onCancel
  }: {
    nr: any;
    allProxyNames: string[];
    onSave: () => void;
    onCancel: () => void;
  } = $props();

  const RULE_TYPES = [
    'DOMAIN-SUFFIX',
    'DOMAIN-KEYWORD',
    'DOMAIN',
    'GEOIP',
    'GEOSITE',
    'IP-CIDR',
    'PROCESS-NAME',
    'RULE-SET',
    'MATCH'
  ];
</script>

<div class="form-card">
  <div class="form-row2">
    <div class="form-col">
      <label class="form-label" for="rule-type">{$t('rules.rule_type')}</label>
      <Select id="rule-type" class="form-select" bind:value={nr.type}>
        {#each RULE_TYPES as t (t)}<option value={t}>{t}</option>{/each}
      </Select>
    </div>
    <div class="form-col">
      <label class="form-label" for="rule-outbound">{$t('rules.outbound')}</label>
      <Select id="rule-outbound" class="form-select" bind:value={nr.outbound}>
        {#each allProxyNames as n, i (i)}<option value={n}>{n}</option>{/each}
      </Select>
    </div>
  </div>
  {#if nr.type !== 'MATCH'}
    <div class="form-row">
      <label class="form-label" for="rule-value">{$t('rules.value')}</label>
      <input
        id="rule-value"
        class="input input-sm"
        bind:value={nr.value}
        placeholder={nr.type === 'GEOIP'
          ? 'CN'
          : nr.type === 'GEOSITE'
            ? 'google'
            : nr.type === 'IP-CIDR'
              ? '192.168.0.0/16'
              : 'example.com'}
      />
    </div>
  {/if}
  <div class="form-actions">
    <button class="btn btn-secondary" onclick={onCancel}>{$t('app.cancel')}</button>
    <button class="btn btn-primary" onclick={onSave}>{$t('app.create')}</button>
  </div>
</div>

<style>
  .form-card {
    background: transparent;
    border: none;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }
  .form-row2 {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-3);
  }
  .form-col {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    flex: 1 1 140px;
    min-width: 140px;
  }

  .form-label {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    font-weight: 500;
  }

  .form-actions {
    position: sticky;
    bottom: -20px;
    background: var(--bg-card);
    padding: var(--spacing-3) 0 0 0;
    margin-top: var(--spacing-3);
    border-top: 1px solid var(--border);
    display: flex;
    gap: var(--spacing-2);
    justify-content: flex-end;
    z-index: 10;
  }
</style>
