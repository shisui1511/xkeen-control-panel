<script lang="ts">
  import { t } from './i18n';
  import type { Snippet } from 'svelte';
  import Breadcrumbs from './Breadcrumbs.svelte';

  interface Props {
    title: string;
    subtitle?: string;
    breadcrumbs?: { label: string; tab?: string }[];
    onSwitchTab?: (tab: string) => void;
    hideHome?: boolean;
    actions?: Snippet;
    children?: Snippet;
  }

  let {
    title,
    subtitle = '',
    breadcrumbs = [],
    onSwitchTab = () => {},
    hideHome = false,
    actions,
    children
  }: Props = $props();
</script>

<!-- Styles live in global.css under .page-header / .page-header-content / .page-header-actions -->
<div class="page-header">
  <Breadcrumbs items={breadcrumbs} onNavigate={onSwitchTab} {hideHome} />
  <div class="page-header-content">
    <div>
      <h1>{title}</h1>
      {#if subtitle}
        <p class="text-secondary" style="margin: 6px 0 0;">{subtitle}</p>
      {/if}
    </div>
    {#if actions || children}
      <div class="page-header-actions">
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
