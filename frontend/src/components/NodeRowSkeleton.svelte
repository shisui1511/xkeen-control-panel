<script lang="ts">
  import Skeleton from './Skeleton.svelte';
</script>

<div class="node-skeleton-list">
  {#each Array(3) as _}
    <div class="node-skeleton-row">
      <div class="left-section">
        <div class="skeleton-circle-wrapper">
          <Skeleton type="circle" width="22px" height="22px" />
        </div>
        <div class="meta-section">
          <Skeleton type="text-line" width="120px" height="14px" style="margin-bottom: 4px;" />
          <Skeleton type="text-line" width="180px" height="11px" style="margin: 0;" />
        </div>
      </div>
      <div class="right-section">
        <Skeleton type="text-line" width="50px" height="12px" style="margin: 0; margin-right: 8px;" />
        <Skeleton type="rect" width="80px" height="28px" style="border-radius: var(--radius-md);" />
      </div>
    </div>
  {/each}
</div>

<style>
  .node-skeleton-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
  }

  .node-skeleton-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 14px;
    background: rgba(255, 255, 255, 0.01);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .left-section {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .skeleton-circle-wrapper {
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .meta-section {
    display: flex;
    flex-direction: column;
  }

  .right-section {
    display: flex;
    align-items: center;
    gap: 12px;
  }
</style>
