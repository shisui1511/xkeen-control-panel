<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from '../lib/components/Icon.svelte';

  interface Option {
    value: string;
    label: string;
    disabled?: boolean;
  }

  interface Props {
    value?: string;
    id?: string;
    class?: string;
    wrapperClass?: string;
    style?: string;
    disabled?: boolean;
    title?: string;
    name?: string;
    ariaLabel?: string;
    'data-testid'?: string;
    onchange?: (event: Event & { currentTarget: HTMLSelectElement }) => void;
    options?: Option[];
    children?: Snippet;
  }

  let {
    value = $bindable(),
    id,
    class: className = '',
    wrapperClass = '',
    style,
    disabled = false,
    title,
    name,
    ariaLabel,
    'data-testid': testId,
    onchange,
    options,
    children
  }: Props = $props();

  const resolvedWrapperClass = $derived(
    ['xcp-select', wrapperClass || className].filter(Boolean).join(' ')
  );
</script>

<!-- Обёртка над нативным select (D5): appearance:none + иконка стрелки, семантика без изменений -->
<span class={resolvedWrapperClass} {style}>
  <select
    {id}
    class={className}
    {name}
    {disabled}
    {title}
    aria-label={ariaLabel}
    data-testid={testId}
    bind:value
    {onchange}
  >
    {#if options}
      {#each options as option (option.value)}
        <option value={option.value} disabled={option.disabled}>{option.label}</option>
      {/each}
    {:else}
      {@render children?.()}
    {/if}
  </select>
  <Icon name="chevron-down" size={14} class="xcp-select-arrow" />
</span>

<style>
  .xcp-select {
    position: relative;
    display: inline-flex;
    width: 100%;
  }

  .xcp-select select {
    appearance: none;
    -webkit-appearance: none;
    -moz-appearance: none;
    width: 100%;
    height: var(--input-h);
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: var(--bg-elevated);
    color: var(--fg-primary);
    padding: 0 32px 0 12px;
    font-family: var(--font-family-sans);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }

  .xcp-select select:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .xcp-select :global(.xcp-select-arrow) {
    position: absolute;
    top: 50%;
    right: 10px;
    transform: translateY(-50%);
    pointer-events: none;
    color: var(--fg-secondary);
  }
</style>
