<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';

  // Текстовое поле панели. Стили — в global.css (.input, .input-sm, .form-group, .form-label).
  //   label     — видимая подпись над полем; без неё обязателен ariaLabel (поле в строке, поиск).
  //   size      — md (по умолчанию) или sm для плотных форм конструкторов.
  //   error / warning — текст под полем, поле подсвечено и связано с текстом через aria-describedby.
  //   hint      — постоянное пояснение под полем.
  //   bare      — без обёртки .form-group: поле встаёт в строку (тулбар, ячейка таблицы).
  interface Props extends Omit<HTMLInputAttributes, 'class' | 'type' | 'value' | 'size'> {
    id: string;
    type?: 'text' | 'password' | 'number' | 'email' | 'search' | 'url' | 'tel';
    label?: string;
    ariaLabel?: string;
    value?: string | number | null;
    size?: 'md' | 'sm';
    error?: string;
    warning?: string;
    hint?: string;
    bare?: boolean;
    mono?: boolean;
    class?: string;
    testid?: string;
  }

  let {
    id,
    type = 'text',
    label,
    ariaLabel,
    value = $bindable(),
    size = 'md',
    error = '',
    warning = '',
    hint = '',
    bare = false,
    mono = false,
    class: className = '',
    testid,
    ...rest
  }: Props = $props();

  if (import.meta.env.DEV) {
    $effect(() => {
      if (!label && !ariaLabel && !rest['aria-labelledby'])
        console.warn(`Input #${id}: needs label or ariaLabel`);
    });
  }

  const describedBy = $derived(
    [error ? `${id}-error` : warning ? `${id}-warning` : '', hint ? `${id}-hint` : '']
      .filter(Boolean)
      .join(' ') || undefined
  );
</script>

{#snippet field()}
  <input
    {...rest}
    {id}
    {type}
    class="input {className}"
    class:input-sm={size === 'sm'}
    class:input-error={!!error}
    class:input-warning={!error && !!warning}
    class:input-mono={mono}
    bind:value
    aria-label={label ? undefined : ariaLabel}
    aria-invalid={error ? 'true' : undefined}
    aria-describedby={describedBy}
    data-testid={testid}
  />
  {#if error}
    <span class="error-text" id="{id}-error">{error}</span>
  {:else if warning}
    <span class="warning-text" id="{id}-warning">{warning}</span>
  {/if}
  {#if hint}
    <span class="form-hint" id="{id}-hint">{hint}</span>
  {/if}
{/snippet}

{#if bare}
  {@render field()}
{:else}
  <div class="form-group">
    {#if label}
      <label class="form-label" for={id}>{label}</label>
    {/if}
    {@render field()}
  </div>
{/if}
