<script lang="ts">
  import type { PathSegment } from '../../lib/editor-utils';

  let {
    breadcrumbs = [],
    onJump = () => {}
  }: {
    breadcrumbs?: PathSegment[];
    onJump?: (pos: number) => void;
  } = $props();
</script>

{#if breadcrumbs.length > 0}
  <div class="editor-breadcrumbs">
    {#each breadcrumbs as segment, i}
      {#if i > 0}
        <span class="breadcrumb-divider">&gt;</span>
      {/if}
      <button class="breadcrumb-segment" onclick={() => onJump(segment.pos)}>
        {segment.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .editor-breadcrumbs {
    display: flex;
    flex-wrap: nowrap;
    align-items: center;
    gap: 4px;
    padding: 8px 14px;
    background: var(--surface-tint);
    border-bottom: 1px solid var(--border);
    font-size: 12px;
    color: var(--fg-secondary);
    overflow-x: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }
  .editor-breadcrumbs::-webkit-scrollbar {
    height: 3px;
  }
  .editor-breadcrumbs::-webkit-scrollbar-thumb {
    background: var(--border);
    border-radius: var(--radius);
  }
  .breadcrumb-segment {
    display: inline-block;
    background: transparent;
    border: none;
    padding: 0;
    color: var(--fg-dim);
    font: inherit;
    cursor: pointer;
    max-width: 160px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .breadcrumb-divider {
    flex-shrink: 0;
  }
  @media (max-width: 768px) {
    .editor-breadcrumbs {
      display: none !important;
    }
  }
</style>
