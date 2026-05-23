<script lang="ts">
  import { t } from './i18n';
  import Icon from './lib/components/Icon.svelte';

  export let items: { label: string; tab?: string }[] = [];
  export let onNavigate: (tab: string) => void = () => {};
</script>

<!-- Visual rules live in global.css under .breadcrumbs etc. -->
<nav class="breadcrumbs">
  <button class="breadcrumb-home" on:click={() => onNavigate('dashboard')}>
    <Icon name="dashboard" size={12} />
    {$t('nav.dashboard')}
  </button>
  {#each items as item, i}
    <span class="breadcrumb-separator">/</span>
    {#if item.tab && i < items.length - 1}
      <button class="breadcrumb-link" on:click={() => onNavigate(item.tab || '')}
        >{item.label}</button
      >
    {:else}
      <span class="breadcrumb-current">{item.label}</span>
    {/if}
  {/each}
</nav>
