<script lang="ts">
  import { t } from '../i18n';
  import Icon from '../lib/components/Icon.svelte';

  // Переиспользуемое поле пароля с кнопкой «показать пароль» (D-18).
  // Используется на входе, в setup и в форме смены пароля.
  interface Props {
    id: string;
    value?: string;
    placeholder?: string;
    autocomplete: 'current-password' | 'new-password';
    disabled?: boolean;
    inputClass?: string;
    autofocus?: boolean;
    onkeydown?: (e: KeyboardEvent) => void;
    ariaDescribedby?: string;
  }

  let {
    id,
    value = $bindable(''),
    placeholder,
    autocomplete,
    disabled = false,
    inputClass = 'input',
    autofocus = false,
    onkeydown,
    ariaDescribedby
  }: Props = $props();

  let visible = $state(false);

  function toggleVisible() {
    visible = !visible;
  }
</script>

<div class="password-field">
  <input
    {id}
    type={visible ? 'text' : 'password'}
    class={inputClass}
    bind:value
    {placeholder}
    {autocomplete}
    {disabled}
    {onkeydown}
    aria-describedby={ariaDescribedby}
    {@attach (node) => {
      if (autofocus) node.focus();
    }}
  />
  <button
    type="button"
    class="password-toggle"
    aria-pressed={visible}
    aria-label={visible ? $t('auth.hide_password') : $t('auth.show_password')}
    onclick={toggleVisible}
  >
    <Icon name={visible ? 'eye-off' : 'eye'} size={16} />
  </button>
</div>

<style>
  .password-field {
    position: relative;
  }

  .password-field :global(input) {
    padding-right: 40px;
  }

  .password-toggle {
    position: absolute;
    top: 50%;
    right: 6px;
    transform: translateY(-50%);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-dim);
    cursor: pointer;
    transition: color var(--transition-fast);
  }

  .password-toggle:hover {
    color: var(--fg-primary);
  }
</style>
