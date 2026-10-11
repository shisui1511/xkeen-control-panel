<script lang="ts">
  import { t } from '../../../i18n';
  import { sourceErrorText, verdictReason } from '../../../lib/sourceErrors';
  import type { NodeItem } from '../../../lib/nodesView';

  let { item } = $props<{ item: NodeItem }>();

  const node = $derived(item.node);
  const reason = $derived(item.reason ? verdictReason(item.reason, $t) : '');
  const kernelName = $derived(
    item.mark === 'only_xray' ? 'Xray' : item.mark === 'only_mihomo' ? 'Mihomo' : ''
  );
  // Не поддерживается нигде: без вердикта причина — общий текст «тип не поддерживается»
  const unsupportedReason = $derived(
    reason || sourceErrorText({ code: 'unsupported_type' }, $t).title
  );
</script>

<li class="node-row" class:dimmed={item.dimmed} data-testid="nodes-node-{node.key}">
  <span class="node-name" title={node.name}>{node.name}</span>
  <span class="chip chip-default node-proto">{node.protocol}</span>
  <span class="node-addr">{node.address}</span>
  {#if item.mark === 'only_xray' || item.mark === 'only_mihomo'}
    <span class="chip chip-info node-mark" title={reason || undefined} data-mark={item.mark}>
      {$t('cfg.nodes.only_kernel', { kernel: kernelName })}
    </span>
  {:else if item.mark === 'unsupported'}
    <span class="chip chip-warning node-mark" title={unsupportedReason} data-mark="unsupported">
      {$t('cfg.nodes.unsupported', { reason: unsupportedReason })}
    </span>
  {:else if item.mark === 'excluded'}
    <span class="chip chip-default node-mark" data-mark="excluded">
      {$t('cfg.nodes.excluded')}
    </span>
  {/if}
</li>

<style>
  .node-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1) var(--spacing-2);
    min-width: 0;
    padding: var(--spacing-2) 0;
    border-top: 1px solid var(--border);
    font-size: var(--font-size-base);
    font-weight: 400;
    line-height: 1.4;
    color: var(--fg-primary);
  }

  .node-row:first-child {
    border-top: 0;
  }

  .node-name {
    min-width: 0;
    flex: 0 1 auto;
    max-width: 100%;
    overflow: hidden;
    overflow-wrap: anywhere;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
  }

  .node-proto {
    font-family: var(--font-family-mono);
  }

  .node-addr {
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .node-mark {
    white-space: normal;
    overflow-wrap: anywhere;
    max-width: 100%;
  }

  /* Узел вне просматриваемого набора приглушается цветом, а не прозрачностью (контраст) */
  .node-row.dimmed .node-name,
  .node-row.dimmed .node-addr {
    color: var(--fg-dim);
  }

  .node-row.dimmed .node-proto {
    color: var(--fg-dim);
  }
</style>
