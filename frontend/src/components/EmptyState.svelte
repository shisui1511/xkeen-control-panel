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
      <IconComponent size={42} />
    </div>
  {/if}
  <h2 class="empty-state__title">{title}</h2>
  <p class="empty-state__description">{description}</p>
  {#if ctaText && oncta}
    <Button variant="primary" loading={ctaLoading} title={ctaText} onclick={oncta}>{ctaText}</Button
    >
  {/if}
</div>

<style>
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 40px 24px;
    background: var(--bg-card);
    border: 1px dashed var(--border);
    border-radius: var(--radius-lg);
    gap: 10px;
  }
  .empty-state__icon {
    width: 64px;
    height: 64px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: var(--accent);
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    margin-bottom: 8px;
  }
  .empty-state__title {
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
    margin: 0;
  }
  .empty-state__description {
    font-size: 13px;
    color: var(--fg-secondary);
    line-height: 1.5;
    max-width: 420px;
    margin: 0;
  }
</style>
