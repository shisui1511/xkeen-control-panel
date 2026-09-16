<script lang="ts">
  export interface ScenarioOption {
    id: string;
    label: string;
    description?: string;
  }

  interface Props {
    label: string;
    options: ScenarioOption[];
    active: string;
    modifiedBadge?: string;
    onSelect: (id: string) => void;
  }

  let { label, options, active, modifiedBadge, onSelect }: Props = $props();

  const activeOption = $derived(options.find((o) => o.id === active));
</script>

<div class="scenario-bar">
  <div class="scenario-chip-row">
    <span class="scenario-label">{label}:</span>
    {#each options as opt (opt.id)}
      <button
        type="button"
        class="scenario-chip"
        class:active={active === opt.id}
        onclick={() => onSelect(opt.id)}
      >
        {opt.label}
        {#if active === opt.id && modifiedBadge}
          <span class="preset-mod-badge">{modifiedBadge}</span>
        {/if}
      </button>
    {/each}
  </div>
  {#if activeOption?.description}
    <p class="scenario-description">{activeOption.description}</p>
  {/if}
</div>

<style>
  .scenario-bar {
    margin-bottom: 12px;
  }

  .scenario-chip-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .scenario-label {
    font-size: 0.8125rem;
    color: var(--fg-secondary);
    font-weight: 500;
  }

  .scenario-chip {
    padding: 4px 10px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    color: var(--fg-primary);
    font-size: 0.75rem;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    transition: all var(--transition-fast, 0.15s ease);
  }

  .scenario-chip:hover {
    background: var(--hover);
    border-color: var(--primary);
  }

  .scenario-chip.active {
    background: color-mix(in srgb, var(--primary) 15%, transparent);
    border-color: var(--primary);
    color: var(--primary);
    font-weight: 600;
  }

  .preset-mod-badge {
    margin-left: 5px;
    font-size: var(--font-size-xs, 0.6875rem);
    color: var(--warning);
    opacity: 0.9;
    font-style: italic;
  }

  .scenario-description {
    margin: 6px 0 0 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    line-height: 1.4;
  }
</style>
