<script lang="ts">
  import { t } from './i18n';
  import Breadcrumbs from './Breadcrumbs.svelte';

  export let title: string;
  export let subtitle: string = '';
  export let breadcrumbs: { label: string; tab?: string }[] = [];
  export let onSwitchTab: (tab: string) => void = () => {};
</script>

<div class="page-header">
  <Breadcrumbs items={breadcrumbs} onNavigate={onSwitchTab} />
  <div class="page-header-content">
    <div>
      <h1>{title}</h1>
      {#if subtitle}
        <p class="text-secondary">{subtitle}</p>
      {/if}
    </div>
    <div class="page-header-actions">
      <slot name="actions" />
    </div>
  </div>
</div>

<style>
  .page-header {
    margin-bottom: 1.5rem;
  }

  .page-header-content {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 1rem;
  }

  .page-header-content h1 {
    margin: 0;
  }

  .page-header-actions {
    display: flex;
    gap: 0.5rem;
    flex-shrink: 0;
  }
</style>
