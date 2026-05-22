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
    aria-invalid={error ? 'true' : 'false'}
    aria-describedby={error ? `${id}-error` : undefined}
  />
  {#if error}
    <span class="error-text" id={`${id}-error`}>{error}</span>
  {/if}
</div>

<style>
  .form-group {
    margin-bottom: var(--spacing-4);
  }

  .form-label {
    display: block;
    margin-bottom: var(--spacing-2);
    font-family: var(--font-family-sans);
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--color-text-primary);
  }

  .input {
    width: 100%;
    padding: 8px 12px;
    border: 1px solid var(--color-border-subtle);
    border-radius: var(--radius-sm);
    font-family: var(--font-family-sans);
    font-size: var(--font-size-sm);
    color: var(--color-text-primary);
    background-color: var(--color-bg-surface);
    transition: border-color var(--transition-fast);
    outline: none;
  }

  .input:focus {
    border-color: var(--color-primary-500);
  }

  .input:disabled {
    opacity: 0.6;
    background-color: var(--color-bg-canvas);
    cursor: not-allowed;
  }

  .input-error {
    border-color: var(--color-danger);
  }

  .error-text {
    display: block;
    margin-top: var(--spacing-1);
    font-size: var(--font-size-xs);
    color: var(--color-danger);
  }
</style>
