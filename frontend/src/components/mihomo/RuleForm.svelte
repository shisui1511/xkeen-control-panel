<script lang="ts">
  import { currentLang } from '../../i18n';

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

  const ru = $derived($currentLang === 'ru');

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
      <label class="form-label">{ru ? 'Тип правила' : 'Rule type'}</label>
      <select class="form-select" bind:value={nr.type}>
        {#each RULE_TYPES as t}<option value={t}>{t}</option>{/each}
      </select>
    </div>
    <div class="form-col">
      <label class="form-label">{ru ? 'Исходящий' : 'Outbound'}</label>
      <select class="form-select" bind:value={nr.outbound}>
        {#each allProxyNames as n}<option value={n}>{n}</option>{/each}
      </select>
    </div>
  </div>
  {#if nr.type !== 'MATCH'}
    <div class="form-row">
      <label class="form-label">{ru ? 'Значение' : 'Value'}</label>
      <input
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
    <button class="btn btn-secondary" onclick={onCancel}
      >{ru ? 'Отмена' : 'Cancel'}</button
    >
    <button class="btn btn-primary" onclick={onSave}>{ru ? 'Добавить' : 'Add'}</button>
  </div>
</div>

<style>
  .form-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .form-row2 {
    display: flex;
    gap: 10px;
  }
  .form-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
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
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 4px;
  }
</style>
