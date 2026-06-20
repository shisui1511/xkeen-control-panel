<script lang="ts">
  import { t } from './i18n';
  import Icon from './lib/components/Icon.svelte';

  export let items: { label: string; tab?: string }[] = [];
  export let onNavigate: (tab: string) => void = () => {};
  export let hideHome: boolean = false;
</script>

<!-- Visual rules live in global.css under .breadcrumbs etc. -->
<nav class="breadcrumbs">
  {#if !hideHome}
    <button class="breadcrumb-home" onclick={() => onNavigate('dashboard')}>
      <Icon name="dashboard" size={12} />
      {$t('nav.dashboard')}
    </button>
  {/if}
  {#each items as item, i}
    {#if !hideHome || i > 0}
      <span class="breadcrumb-separator">/</span>
    {/if}
    {#if item.tab && i < items.length - 1}
      <button class="breadcrumb-link" onclick={() => onNavigate(item.tab || '')}
        >{item.label}</button
      >
    {:else}
      <span class="breadcrumb-current">{item.label}</span>
    {/if}
  {/each}
</nav>
