<script lang="ts">
  import { t } from './i18n';
  import Breadcrumbs from './Breadcrumbs.svelte';

  export let title: string;
  export let subtitle: string = '';
  export let breadcrumbs: { label: string; tab?: string }[] = [];
  export let onSwitchTab: (tab: string) => void = () => {};
  export let hideHome: boolean = false;
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
    <div class="page-header-actions">
      <slot name="actions" />
    </div>
  </div>
</div>
