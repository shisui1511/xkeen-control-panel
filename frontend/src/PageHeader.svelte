<script lang="ts">
  import type { Snippet } from 'svelte';

  // Шапка страницы: заголовок, подзаголовок и действия. Хлебных крошек нет: средний уровень
  // был группой меню (не ссылкой), последний повторял заголовок, а где ты — и так показывают
  // подсвеченный пункт сайдбара или нижней панели.
  interface Props {
    title: string;
    subtitle?: string;
    actions?: Snippet;
    children?: Snippet;
  }

  let { title, subtitle = '', actions, children }: Props = $props();
</script>

<!-- Styles live in global.css under .page-header / .page-header-content / .page-header-actions -->
<div class="page-header">
  <div class="page-header-content">
    <div>
      <h1>{title}</h1>
      {#if subtitle}
        <p class="text-secondary page-header-subtitle">{subtitle}</p>
      {/if}
    </div>
    {#if actions || children}
      <div class="page-header-actions ph-actions">
        {#if actions}
          {@render actions()}
        {/if}
        {#if children}
          {@render children()}
        {/if}
      </div>
    {/if}
  </div>
</div>
