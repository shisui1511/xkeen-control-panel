<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';

  // Переключатель «вкл/выкл» с мгновенным действием. Нативный флажок с role="switch":
  // экранный диктор читает «переключатель, включён». Стили — .toggle-switch в global.css.
  // Подпись — либо ariaLabel, либо внешний <label for={id}>, либо aria-labelledby.
  // Флажок в форме, который применяется по кнопке «Сохранить», — это Checkbox, не Toggle.
  interface Props extends Omit<HTMLInputAttributes, 'class' | 'type' | 'checked' | 'role'> {
    checked?: boolean;
    loading?: boolean;
    ariaLabel?: string;
    title?: string;
    class?: string;
    testid?: string;
  }

  let {
    checked = $bindable(false),
    loading = false,
    ariaLabel,
    title,
    class: className = '',
    testid,
    ...rest
  }: Props = $props();
</script>

<label class="toggle-switch {className}" class:is-loading={loading} {title}>
  <input
    {...rest}
    type="checkbox"
    role="switch"
    bind:checked
    aria-label={ariaLabel ?? rest['aria-label']}
    aria-busy={loading || undefined}
    data-testid={testid ?? rest['data-testid']}
  />
  <span class="toggle-slider" aria-hidden="true"></span>
</label>
