<script lang="ts">
  import { t } from './i18n';
  import Icon from './lib/components/Icon.svelte';

  export let items: { label: string; tab?: string }[] = [];
  export let onNavigate: (tab: string) => void = () => {};
</script>

<nav class="breadcrumbs">
  <button class="breadcrumb-home" on:click={() => onNavigate('dashboard')}>
    <Icon name="dashboard" size={14} />
    {$t('nav.dashboard')}
  </button>
  {#each items as item, i}
    <span class="breadcrumb-separator">/</span>
    {#if item.tab && i < items.length - 1}
      <button class="breadcrumb-link" on:click={() => item.tab && onNavigate(item.tab)}>
        {item.label}
      </button>
    {:else}
      <span class="breadcrumb-current">{item.label}</span>
    {/if}
  {/each}
</nav>

<style>
  .breadcrumbs {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 1rem;
    font-size: 0.9rem;
  }

  .breadcrumb-home,
  .breadcrumb-link {
    background: none;
    border: none;
    color: var(--primary);
    cursor: pointer;
    padding: 0;
    font-size: 0.9rem;
  }

  .breadcrumb-home:hover,
  .breadcrumb-link:hover {
    text-decoration: underline;
  }

  .breadcrumb-separator {
    color: var(--fg-secondary);
    opacity: 0.5;
  }

  .breadcrumb-current {
    color: var(--fg);
    font-weight: 500;
  }
</style>
