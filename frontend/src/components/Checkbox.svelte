<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLInputAttributes } from 'svelte/elements';

  // Флажок с подписью: выбор в форме или списке, применяется вместе с формой.
  // Мгновенное «вкл/выкл» настройки — это Toggle. Стили — .checkbox в global.css.
  // Подпись — label (текст) или children (разметка); без них нужен ariaLabel.
  interface Props extends Omit<HTMLInputAttributes, 'class' | 'type' | 'checked'> {
    checked?: boolean;
    indeterminate?: boolean;
    label?: string;
    ariaLabel?: string;
    class?: string;
    testid?: string;
    children?: Snippet;
  }

  let {
    checked = $bindable(false),
    indeterminate = false,
    label,
    ariaLabel,
    class: className = '',
    testid,
    children,
    ...rest
  }: Props = $props();
</script>

<label class="checkbox {className}">
  <input
    {...rest}
    type="checkbox"
    bind:checked
    {indeterminate}
    aria-label={ariaLabel ?? rest['aria-label']}
    data-testid={testid ?? rest['data-testid']}
  />
  {#if children}
    {@render children()}
  {:else if label}
    <span>{label}</span>
  {/if}
</label>
