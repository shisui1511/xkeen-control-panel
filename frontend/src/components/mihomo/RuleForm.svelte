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
