<script lang="ts">
  import Toggle from '../../Toggle.svelte';
  import { t } from '../../../i18n';
  import MihomoField, { describedBy } from './MihomoField.svelte';

  // Переключатель в форме секции Mihomo: настоящий input type=checkbox с подписью.
  // У чужой секции (readonly) вместо него поле только для чтения с текстом значения
  // («Включено» / «Выключено»): его можно прочитать и скопировать с клавиатуры.
  let {
    id,
    label,
    checked,
    readonly = false,
    hint = '',
    testid,
    onchange
  }: {
    id: string;
    label: string;
    checked: boolean;
    readonly?: boolean;
    hint?: string;
    testid: string;
    onchange: (value: boolean) => void;
  } = $props();
</script>

<MihomoField {id} {label} {hint}>
  {#if readonly}
    <input
      {id}
      class="input"
      readonly
      aria-readonly="true"
      aria-describedby={describedBy(id, '', hint)}
      value={checked ? $t('cfg.sec.value_on') : $t('cfg.sec.value_off')}
      data-testid={testid}
    />
  {:else}
    <Toggle
      class="mt-switch"
      {id}
      {checked}
      aria-describedby={describedBy(id, '', hint)}
      data-testid={testid}
      onchange={(e) => onchange(e.currentTarget.checked)}
    />
  {/if}
</MihomoField>

<style>
  /* Сенсорная зона .toggle-switch выходит за 34x18; запас по высоте не даёт строкам слипаться */
  :global(.mt-switch) {
    margin: var(--spacing-2) 0;
  }
</style>
