<script lang="ts">
  import { t } from '../../i18n';

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
      <select id="rule-type" class="form-select" bind:value={nr.type}>
        {#each RULE_TYPES as t}<option value={t}>{t}</option>{/each}
      </select>
    </div>
    <div class="form-col">
      <label class="form-label" for="rule-outbound">{$t('rules.outbound')}</label>
      <select id="rule-outbound" class="form-select" bind:value={nr.outbound}>
        {#each allProxyNames as n}<option value={n}>{n}</option>{/each}
      </select>
    </div>
  </div>
  {#if nr.type !== 'MATCH'}
    <div class="form-row">
      <label class="form-label" for="rule-value">{$t('rules.value')}</label>
      <input
        id="rule-value"
        class="form-input"
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
    gap: 12px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .form-row2 {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }
  .form-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1 1 140px;
    min-width: 140px;
  }

  .form-label {
    font-size: 11px;
    color: var(--fg-dim);
    font-weight: 500;
  }

  .form-input,
  .form-select {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 13px;
    padding: 6px 10px;
    outline: none;
    width: 100%;
    transition: border-color var(--transition-fast);
  }

  .form-input:focus,
  .form-select:focus {
    border-color: var(--primary);
  }

  .form-actions {
    position: sticky;
    bottom: -20px;
    background: var(--bg-card);
    padding: 12px 0 0 0;
    margin-top: 12px;
    border-top: 1px solid var(--border);
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    z-index: 10;
  }
</style>
