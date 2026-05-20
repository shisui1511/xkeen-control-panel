<script lang="ts">
  import type { Component } from 'svelte';
  import Button from './Button.svelte';

  let {
    title,
    description,
    icon: IconComponent = undefined,
    ctaText = '',
    ctaLoading = false,
    oncta = undefined
  } = $props<{
    title: string;
    description: string;
    icon?: Component<any>;
    ctaText?: string;
    ctaLoading?: boolean;
    oncta?: () => void;
  }>();
</script>

<div class="empty-state" role="status" aria-label={title}>
  {#if IconComponent}
    <div class="empty-state__icon" aria-hidden="true">
      <IconComponent size={48} />
    </div>
  {/if}
  <h2 class="empty-state__title">{title}</h2>
  <p class="empty-state__description">{description}</p>
  {#if ctaText && oncta}
    <Button
      variant="primary"
      loading={ctaLoading}
      title={ctaText}
      onclick={oncta}
    >
      {ctaText}
    </Button>
  {/if}
</div>

<style>
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: var(--spacing-8) var(--spacing-6);
    background-color: var(--color-bg-surface);
    border: 1px solid var(--color-border-subtle);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-sm);
    gap: var(--spacing-3);
  }

  .empty-state__icon {
    color: var(--color-text-secondary);
    opacity: 0.6;
    margin-bottom: var(--spacing-2);
  }

  .empty-state__title {
    font-family: var(--font-family-sans);
    font-size: var(--font-size-xl);
    font-weight: 600;
    color: var(--color-text-primary);
    margin: 0;
  }

  .empty-state__description {
    font-family: var(--font-family-sans);
    font-size: var(--font-size-sm);
    color: var(--color-text-secondary);
    line-height: 1.5;
    max-width: 400px;
    margin: 0;
  }
</style>
