<script lang="ts">
  let {
    id,
    type = 'text',
    label,
    value = $bindable(),
    placeholder = '',
    disabled = false,
    error = '',
    oninput
  } = $props<{
    id: string;
    type?: 'text' | 'password' | 'number' | 'email';
    label: string;
    value: string | number;
    placeholder?: string;
    disabled?: boolean;
    error?: string;
    oninput?: (event: Event & { currentTarget: HTMLInputElement }) => void;
  }>();
</script>

<!-- All styling lives in global.css under .form-group / .form-label / .input -->
<div class="form-group">
  <label class="form-label" for={id}>{label}</label>
  <input
    {id}
    {type}
    class="input"
    class:input-error={!!error}
    bind:value
    {placeholder}
    {disabled}
    {oninput}
    aria-invalid={!!error ? 'true' : 'false'}
    aria-describedby={error ? `${id}-error` : undefined}
  />
  {#if error}
    <span class="error-text" id={`${id}-error`}>{error}</span>
  {/if}
</div>

<style>
  .input-error {
    border-color: var(--danger) !important;
    box-shadow: 0 0 0 3px rgba(239,91,107,.18) !important;
  }
  .error-text {
    display: block;
    margin-top: 6px;
    font-size: 11.5px;
    color: var(--danger);
  }
</style>
