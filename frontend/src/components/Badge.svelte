<script lang="ts" module>
  export type BadgeTone = 'neutral' | 'emphasis' | 'info' | 'success' | 'warning' | 'danger';
  export type BadgeVariant = 'soft' | 'tag' | 'status';
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  // Единый бейдж панели. Стили — в global.css под .badge (см. комментарий там).
  //   variant: soft — метка, фильтр, счётчик; tag — тип/версия (моноширинный, прописными);
  //            status — состояние сервиса или процесса, с точкой.
  //   tone:    neutral | emphasis | info | success | warning | danger.
  //   dot:     точка-индикатор перед текстом (у status — всегда); pulse — точка мигает
  //            (идёт процесс: подключение, загрузка).
  interface Props extends Omit<HTMLAttributes<HTMLSpanElement>, 'class'> {
    tone?: BadgeTone;
    variant?: BadgeVariant;
    dot?: boolean;
    pulse?: boolean;
    title?: string;
    class?: string;
    testid?: string;
    children?: Snippet;
  }

  let {
    tone = 'neutral',
    variant = 'soft',
    dot = false,
    pulse = false,
    title,
    class: className = '',
    testid,
    children,
    ...rest
  }: Props = $props();

  const showDot = $derived(dot || variant === 'status');
</script>

<span
  {...rest}
  class="badge badge-{tone} {className}"
  class:badge--tag={variant === 'tag'}
  class:badge--status={variant === 'status'}
  {title}
  data-testid={testid}
>
  {#if showDot}
    <span class="badge-dot" class:badge-dot--pulse={pulse} aria-hidden="true"></span>
  {/if}
  {@render children?.()}
</span>
