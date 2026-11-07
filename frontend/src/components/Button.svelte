<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import Spinner from './Spinner.svelte';

  // Кнопка панели. Своих стилей нет: вид задаёт .btn в global.css, поэтому <Button> и
  // <button class="btn …"> выглядят одинаково. Новый код пишет <Button> (scripts/check-controls.cjs).
  //   variant: primary — главное действие экрана; secondary — остальные; ghost — третьестепенное;
  //            danger — подтверждение удаления в диалоге; danger-soft — удаление в строке;
  //            warning — действие с риском (остановка, перезапуск).
  //   size:    md (по умолчанию) · sm — мельче текст, та же высота · xs — компактная, 28px.
  //   iconOnly — квадратная кнопка-иконка; ariaLabel тогда обязателен.
  interface Props extends Omit<HTMLButtonAttributes, 'class' | 'type'> {
    type?: 'button' | 'submit' | 'reset';
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-soft' | 'warning';
    size?: 'md' | 'sm' | 'xs';
    iconOnly?: boolean;
    disabled?: boolean;
    loading?: boolean;
    title?: string;
    ariaLabel?: string;
    /** Идентификатор элемента с постоянным пояснением, почему кнопка неактивна. */
    ariaDescribedby?: string;
    class?: string;
    testid?: string;
    'data-testid'?: string;
    children?: Snippet;
  }

  let {
    type = 'button',
    variant = 'primary',
    size = 'md',
    iconOnly = false,
    disabled = false,
    loading = false,
    title,
    ariaLabel,
    ariaDescribedby,
    class: className = '',
    testid,
    'data-testid': dataTestid,
    children,
    ...rest
  }: Props = $props();

  if (import.meta.env.DEV) {
    $effect(() => {
      if (iconOnly && !ariaLabel && !rest['aria-label'])
        console.warn('Button: iconOnly button needs ariaLabel');
    });
  }
</script>

<button
  {...rest}
  {type}
  class="btn btn-{variant} {className}"
  class:btn-sm={size === 'sm'}
  class:btn-xs={size === 'xs'}
  class:btn-icon-only={iconOnly}
  disabled={disabled || loading}
  aria-busy={loading || undefined}
  {title}
  aria-label={ariaLabel ?? rest['aria-label']}
  aria-describedby={ariaDescribedby ?? rest['aria-describedby']}
  data-testid={testid ?? dataTestid}
>
  {#if loading}
    <Spinner size={13} tone="current" />
  {/if}
  {@render children?.()}
</button>
