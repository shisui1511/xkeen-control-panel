<script lang="ts">
  import { t } from '../../i18n';
  import type { AwgDiffResult } from '../../lib/awgPresets';

  let {
    diff,
    targetKernel = 'mihomo',
    compact = false
  }: {
    diff: AwgDiffResult;
    targetKernel?: 'mihomo' | 'xray';
    compact?: boolean;
  } = $props();

  const hasAnyItems = $derived(
    (diff?.kept?.length ?? 0) > 0 ||
      (diff?.warnings?.length ?? 0) > 0 ||
      (diff?.dropped?.length ?? 0) > 0
  );
</script>

{#if hasAnyItems}
  <div class="awg-diff-card" class:compact data-testid="awg-diff-card">
    <div class="awg-diff-header">
      <span class="diff-title">{$t('proxies.diff_title')} ({targetKernel.toUpperCase()})</span>
    </div>

    <div class="diff-sections">
      {#if diff.kept && diff.kept.length > 0}
        <div class="diff-row">
          <span class="diff-cat-label text-success">{$t('proxies.diff_kept')}:</span>
          <div class="diff-badges">
            {#each diff.kept as item}
              <span class="badge badge-success">{item.key}</span>
            {/each}
          </div>
        </div>
      {/if}

      {#if diff.warnings && diff.warnings.length > 0}
        <div class="diff-row">
          <span class="diff-cat-label text-warning">{$t('proxies.diff_warnings')}:</span>
          <div class="diff-badges">
            {#each diff.warnings as warn}
              {@const warnReason = $t(warn.messageKey)}
              <span class="badge badge-warning" title={warnReason}>
                {warn.key}: {warnReason}
              </span>
            {/each}
          </div>
        </div>
      {/if}

      {#if diff.dropped && diff.dropped.length > 0}
        <div class="diff-row">
          <span class="diff-cat-label text-danger">{$t('proxies.diff_dropped')}:</span>
          <div class="diff-badges">
            {#each diff.dropped as drop}
              {@const dropReason = $t(drop.reasonKey)}
              <span class="badge badge-danger" title={dropReason}>
                {drop.key}: {dropReason}
              </span>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .awg-diff-card {
    background: var(--bg-surface-hover, rgba(255, 255, 255, 0.03));
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 4px);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 6px;
    font-size: 12px;
  }

  .awg-diff-card.compact {
    padding: 6px 8px;
    gap: 4px;
    font-size: 11px;
  }

  .awg-diff-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-weight: 600;
    color: var(--fg);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .diff-sections {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .diff-row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    flex-wrap: wrap;
  }

  .diff-cat-label {
    font-weight: 500;
    min-width: 110px;
    flex-shrink: 0;
  }

  .text-success {
    color: var(--success);
  }

  .text-warning {
    color: var(--warning);
  }

  .text-danger {
    color: var(--danger);
  }

  .diff-badges {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
  }
</style>
