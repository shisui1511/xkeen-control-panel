<script lang="ts">
  import type { Component } from 'svelte';
  import Button from './Button.svelte';

  let {
    title,
    description,
    icon: IconComponent = undefined,
    ctaText = '',
    ctaLoading = false,
    ctaVariant = 'primary',
    plain = false,
    oncta = undefined
  }: {
    title: string;
    description: string;
    icon?: Component<any>;
    ctaText?: string;
    ctaLoading?: boolean;
    /** secondary — когда основная кнопка того же действия уже стоит на панели над пустым состоянием. */
    ctaVariant?: 'primary' | 'secondary';
    plain?: boolean;
    oncta?: () => void;
  } = $props();
</script>

<div class="empty-state" class:empty-state--plain={plain} role="status" aria-label={title}>
  {#if IconComponent}
    <div class="empty-state__icon" aria-hidden="true">
      <IconComponent size={42} />
    </div>
  {/if}
  <h2 class="empty-state__title">{title}</h2>
  <p class="empty-state__description">{description}</p>
  {#if ctaText && oncta}
    <Button variant={ctaVariant} loading={ctaLoading} title={ctaText} onclick={oncta}
      >{ctaText}</Button
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
    padding: var(--spacing-10) var(--spacing-6);
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    gap: var(--spacing-2-5);
  }

  .empty-state.empty-state--plain,
  :global(.card) .empty-state,
  :global(.editor-empty-card) .empty-state,
  :global(.editor-main-card) .empty-state,
  :global(.table-responsive) .empty-state {
    background: transparent;
    border: none;
    border-radius: 0;
    box-shadow: none;
    padding: var(--spacing-8) var(--spacing-4);
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
    margin-bottom: var(--spacing-2);
    box-shadow: 0 0 24px -4px var(--accent-soft);
  }
  .empty-state__title {
    font-size: var(--font-size-lg);
    font-weight: 600;
    color: var(--fg-primary);
    margin: 0;
  }
  .empty-state > :global(.btn) {
    margin-top: var(--spacing-1-5);
  }
  .empty-state__description {
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    line-height: 1.5;
    max-width: 420px;
    margin: 0;
  }
</style>
