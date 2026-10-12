<script lang="ts">
  import { t, tp } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import type {
    PreviewView,
    PreviewNode,
    PreviewLineError,
    PreviewSubscription,
    SourceErrorPayload
  } from '../../../lib/configSources';
  import {
    sourceErrorText,
    verdictReason,
    errorDetailLines,
    truncateFragment
  } from '../../../lib/sourceErrors';

  let {
    preview,
    loading,
    error,
    mode = 'add'
  } = $props<{
    preview: PreviewView | null;
    loading: boolean;
    error: SourceErrorPayload | null;
    mode?: 'add' | 'single';
  }>();

  const uid = $props.id();
  const MAX_ERROR_ROWS = 20;
  const KERNELS = [
    { id: 'xray', name: 'Xray', countKey: 'cfg.nodes.count_xray' },
    { id: 'mihomo', name: 'Mihomo', countKey: 'cfg.nodes.count_mihomo' }
  ] as const;

  // Список узлов «Мои узлы» свёрнут по умолчанию; в режиме одного узла он всегда открыт
  let expanded = $state(false);

  const subs = $derived(mode === 'single' ? [] : (preview?.subscriptions ?? []));
  const my = $derived(
    preview?.my_nodes && preview.my_nodes.nodes.length > 0 ? preview.my_nodes : null
  );
  const lineErrors = $derived(preview?.errors ?? []);
  // Пустой результат не показывается пустой рамкой: вместо неё — ошибка (E2/E3)
  const nothing = $derived(preview !== null && subs.length === 0 && my === null);
  const headError = $derived<SourceErrorPayload>(lineErrors[0]?.error ?? { code: 'unrecognized' });
  const restErrors = $derived(nothing ? lineErrors.slice(1) : lineErrors);
  const shownErrors = $derived(restErrors.slice(0, MAX_ERROR_ROWS));
  const moreErrors = $derived(Math.max(0, restErrors.length - MAX_ERROR_ROWS));
  // Отказ правки одного узла приходит вместе с предпросмотром: под общей ошибкой —
  // причины по строкам
  const errorLines = $derived(mode === 'single' && error ? lineErrors : []);
  const showList = $derived(mode === 'single' || expanded);

  const mySummary = $derived.by(() => {
    if (!my || !preview) return '';
    const parts = [$tp('cfg.nodes.preview_my', my.count)];
    if (my.only.xray > 0)
      parts.push($t('cfg.nodes.preview_only', { n: my.only.xray, kernel: 'Xray' }));
    if (my.only.mihomo > 0) {
      parts.push($t('cfg.nodes.preview_only', { n: my.only.mihomo, kernel: 'Mihomo' }));
    }
    if (my.skipped > 0) parts.push($tp('cfg.nodes.preview_skipped', my.skipped));
    if (preview.collapsed > 0) parts.push($t('cfg.nodes.collapsed', { n: preview.collapsed }));
    return parts.join(' · ');
  });

  function stubsOf(sub: PreviewSubscription): number {
    return Math.max(0, ...Object.values(sub.kernels).map((k) => (k.installed ? k.stubs : 0)));
  }

  function countOf(sub: PreviewSubscription, id: string): number {
    return sub.kernels[id]?.count ?? 0;
  }

  function onlyKernel(n: PreviewNode): { name: string; reason: string } | null {
    const x = n.support.xray;
    const m = n.support.mihomo;
    if (x.ok && !m.ok) return { name: 'Xray', reason: verdictReason(m, $t) };
    if (m.ok && !x.ok) return { name: 'Mihomo', reason: verdictReason(x, $t) };
    return null;
  }

  function unsupportedReason(n: PreviewNode): string {
    const reason = verdictReason(n.support.xray, $t) || verdictReason(n.support.mihomo, $t);
    return reason || sourceErrorText({ code: 'unsupported_type' }, $t).title;
  }
</script>

{#snippet lineErrorList(list: PreviewLineError[], more: number, head: boolean)}
  <div class="pv-row" data-testid="nodes-preview-errors">
    {#if head}
      <div class="pv-head">
        <span class="pv-name">{$t('cfg.nodes.unrecognized_head')}</span>
      </div>
    {/if}
    {#each list as le, i (i)}
      <div class="alert alert-warning pv-alert" data-testid="nodes-preview-line-error">
        <Icon name="warning" size={16} class="pv-alert-icon" />
        <div class="pv-err-text">
          <span class="pv-line-ref">
            {le.file
              ? $t('cfg.nodes.line_ref_file', { file: le.file, line: le.line })
              : $t('cfg.nodes.line_ref', { line: le.line })}
          </span>
          {#if le.fragment}
            <code class="pv-fragment">{truncateFragment(le.fragment)}</code>
          {/if}
          {@render errorBlock(le.error, le.file)}
        </div>
      </div>
    {/each}
    {#if more > 0}
      <span class="pv-hint">{$t('cfg.nodes.errors_more', { n: more })}</span>
    {/if}
  </div>
{/snippet}

{#snippet errorBlock(e: SourceErrorPayload, file?: string)}
  {@const text = sourceErrorText(e, $t)}
  <div class="pv-err-text">
    <strong>{text.title}</strong>
    <span class="pv-hint">{text.hint}</span>
    <details class="pv-details">
      <summary>{$t('cfg.nodes.details')}</summary>
      <ul class="pv-detail-list">
        {#each errorDetailLines(e, $t, file) as line (line)}
          <li>{line}</li>
        {/each}
      </ul>
    </details>
  </div>
{/snippet}

<div
  class="pv"
  role="region"
  aria-live="polite"
  aria-label={$t('cfg.nodes.preview_aria')}
  data-testid="nodes-preview"
>
  {#if loading}
    <div class="pv-loading" data-testid="nodes-preview-loading">
      <span class="spinner" aria-hidden="true"></span>
      <span>{$t('cfg.nodes.checking')}</span>
    </div>
  {:else if error}
    <div class="alert alert-error pv-alert" role="alert" data-testid="nodes-preview-error">
      <Icon name="warning" size={16} class="pv-alert-icon" />
      {@render errorBlock(error)}
    </div>
    {#if errorLines.length > 0}
      {@render lineErrorList(
        errorLines.slice(0, MAX_ERROR_ROWS),
        Math.max(0, errorLines.length - MAX_ERROR_ROWS),
        false
      )}
    {/if}
  {:else if preview}
    {#if nothing}
      <div class="alert alert-error pv-alert" data-testid="nodes-preview-error">
        <Icon name="warning" size={16} class="pv-alert-icon" />
        {@render errorBlock(headError, lineErrors[0]?.file)}
      </div>
    {/if}

    {#if preview.nodes_only}
      <div class="alert pv-alert pv-alert-info" data-testid="nodes-preview-nodes-only">
        <Icon name="info" size={16} class="pv-alert-icon" />
        <span>{$t('cfg.nodes.nodes_only')}</span>
      </div>
    {/if}

    {#each subs as sub (sub.line + sub.url_masked)}
      <div
        class="pv-row"
        role="group"
        aria-label={$t('cfg.nodes.preview_sub', {
          name: sub.name,
          xray: countOf(sub, 'xray'),
          mihomo: countOf(sub, 'mihomo')
        })}
        data-testid="nodes-preview-sub"
      >
        <div class="pv-head">
          <span class="pv-name">{sub.name}</span>
          <span class="pv-url">{sub.url_masked}</span>
        </div>
        <div class="pv-chips">
          {#each KERNELS as k (k.id)}
            {@const ks = sub.kernels[k.id]}
            {#if ks}
              {#if ks.installed}
                <span class="chip chip-info" data-testid="nodes-preview-count-{k.id}">
                  {$t(k.countKey, { n: ks.count })}
                </span>
              {:else}
                <span
                  class="chip chip-default"
                  title={$t('cfg.nodes.kernel_not_installed_hint')}
                  data-testid="nodes-preview-missing-{k.id}"
                >
                  {$t('cfg.nodes.kernel_not_installed', { kernel: k.name })}
                </span>
              {/if}
            {/if}
          {/each}
          {#if stubsOf(sub) > 0}
            <span
              class="chip chip-warning"
              title={$t('cfg.nodes.stubs_hint')}
              data-testid="nodes-preview-stubs"
            >
              {$tp('cfg.nodes.stubs', stubsOf(sub))}
            </span>
          {/if}
        </div>
        {#each sub.warnings as w, i (w.code + i)}
          <div class="alert alert-warning pv-alert">
            <Icon name="warning" size={16} class="pv-alert-icon" />
            {@render errorBlock(w)}
          </div>
        {/each}
        {#if sub.error}
          <div class="alert alert-warning pv-alert" data-testid="nodes-preview-sub-error">
            <Icon name="warning" size={16} class="pv-alert-icon" />
            {@render errorBlock(sub.error)}
          </div>
        {:else}
          {#each KERNELS as k (k.id)}
            {@const ke = sub.kernels[k.id]?.error}
            {#if ke}
              <div class="alert alert-warning pv-alert">
                <Icon name="warning" size={16} class="pv-alert-icon" />
                <div class="pv-err-text">
                  <span class="pv-kernel">{k.name}</span>
                  {@render errorBlock(ke)}
                </div>
              </div>
            {/if}
          {/each}
        {/if}
      </div>
    {/each}

    {#if my}
      <div class="pv-row" data-testid="nodes-preview-my">
        <div class="pv-head pv-head-between">
          <span class="pv-name" data-testid="nodes-preview-my-summary">{mySummary}</span>
          {#if mode !== 'single'}
            <button
              type="button"
              class="pv-toggle"
              aria-expanded={expanded}
              aria-controls="{uid}-nodes"
              data-testid="nodes-preview-my-toggle"
              onclick={() => (expanded = !expanded)}
            >
              <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={14} />
              <span>{expanded ? $t('cfg.nodes.my_collapse') : $t('cfg.nodes.my_expand')}</span>
            </button>
          {/if}
        </div>
        {#if showList}
          <ul class="pv-nodes" id="{uid}-nodes" data-testid="nodes-preview-nodes">
            {#each my.nodes as n (n.key)}
              {@const only = onlyKernel(n)}
              {@const unsupported = !n.support.xray.ok && !n.support.mihomo.ok}
              <li class="pv-node">
                <div class="pv-node-main">
                  <span class="pv-node-name">{n.name}</span>
                  {#if n.marks.includes('dup_exists')}
                    <span class="chip chip-default">{$t('cfg.nodes.dup_exists')}</span>
                  {/if}
                  {#if n.marks.includes('dup_in_sub')}
                    <span class="chip chip-warning pv-chip-wrap">
                      {$t('cfg.nodes.dup_in_sub', { name: n.dup_sub_name ?? '' })}
                    </span>
                  {/if}
                  {#if only}
                    <span class="chip chip-info" title={only.reason}>
                      {$t('cfg.nodes.only_kernel', { kernel: only.name })}
                    </span>
                  {/if}
                  {#if unsupported}
                    <span class="chip chip-warning pv-chip-wrap">
                      {$t('cfg.nodes.unsupported', { reason: unsupportedReason(n) })}
                    </span>
                  {/if}
                </div>
                <div class="pv-node-meta">
                  <span>{n.protocol}</span>
                  <span>{n.address}</span>
                  {#if n.cred_mask}<span>{n.cred_mask}</span>{/if}
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    {/if}

    {#if shownErrors.length > 0}
      {@render lineErrorList(shownErrors, moreErrors, !nothing)}
    {/if}
  {/if}
</div>

<style>
  .pv {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .pv-loading {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    color: var(--fg-secondary);
  }

  .pv-row {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    padding: var(--spacing-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-elevated);
  }

  .pv-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--spacing-1) var(--spacing-3);
    min-width: 0;
  }

  .pv-head-between {
    justify-content: space-between;
    align-items: center;
  }

  .pv-name {
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pv-url {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    min-width: 0;
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .pv-chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
  }

  .pv-chip-wrap {
    white-space: normal;
    overflow-wrap: anywhere;
    max-width: 100%;
  }

  .pv-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    min-height: 32px;
    padding: 0 var(--spacing-2);
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--fg-secondary);
    font: inherit;
    font-size: var(--font-size-xs);
    font-weight: 600;
    cursor: pointer;
  }

  .pv-toggle:hover {
    color: var(--fg-primary);
  }

  .pv-nodes {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    max-height: 280px;
    overflow-y: auto;
  }

  .pv-node {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    padding-top: var(--spacing-2);
    border-top: 1px solid var(--border);
  }

  .pv-node:first-child {
    padding-top: 0;
    border-top: 0;
  }

  .pv-node-main {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1) var(--spacing-2);
    min-width: 0;
  }

  .pv-node-name {
    color: var(--fg-primary);
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pv-node-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 0 var(--spacing-3);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .pv-alert {
    margin: 0;
    min-width: 0;
  }

  .pv-alert-info {
    background: var(--bg-elevated);
    border-color: var(--border-strong);
    color: var(--fg-secondary);
  }

  .pv-alert :global(.pv-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .pv-err-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pv-hint {
    color: var(--fg-secondary);
  }

  .pv-kernel,
  .pv-line-ref {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .pv-fragment {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .pv-details {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .pv-details summary {
    cursor: pointer;
    font-weight: 600;
    min-height: 24px;
  }

  .pv-detail-list {
    margin: var(--spacing-1) 0 0;
    padding-left: var(--spacing-4);
    font-family: var(--font-family-mono);
    font-weight: 400;
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .pv-toggle {
      min-height: 44px;
    }

    .pv-details summary {
      min-height: 44px;
      display: flex;
      align-items: center;
    }
  }
</style>
