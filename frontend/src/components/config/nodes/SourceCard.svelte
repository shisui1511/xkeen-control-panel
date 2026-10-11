<script lang="ts">
  import { t, tp } from '../../../i18n';
  import { showConfirm, showToast } from '../../../stores';
  import { formatBytes } from '../../../lib/format';
  import Button from '../../Button.svelte';
  import StatusBadge from '../../StatusBadge.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, handleLayerError } from '../../../lib/configLayer';
  import {
    refreshSource,
    updateSource,
    deleteNode,
    deleteSource,
    revealUrl,
    loadSources,
    type KernelFlags,
    type KernelName,
    type SourceView
  } from '../../../lib/configSources';
  import { sourceErrorText } from '../../../lib/sourceErrors';
  import {
    PAGE_FIRST,
    PAGE_STEP,
    SEARCH_THRESHOLD,
    expireDate,
    filterByName,
    formatDate,
    isDeviceRejected,
    kernelStatusView,
    minuteClock,
    nodesForKernel,
    refreshToast,
    safeHttpUrl,
    statusLabel,
    trafficLevel,
    trafficPercent,
    type NodeItem
  } from '../../../lib/nodesView';
  import NodeRow from './NodeRow.svelte';
  import NodeEditModal from './NodeEditModal.svelte';
  import SourceSettingsModal from './SourceSettingsModal.svelte';
  import SourceDiagnosticsModal from './SourceDiagnosticsModal.svelte';

  let { source, kernel, installed, expanded, ontoggle } = $props<{
    source: SourceView;
    kernel: KernelName;
    installed: KernelFlags;
    expanded: boolean;
    ontoggle: () => void;
  }>();

  const uid = $props.id();
  const KERNELS: { id: KernelName; name: string }[] = [
    { id: 'xray', name: 'Xray' },
    { id: 'mihomo', name: 'Mihomo' }
  ];
  // Длинное объявление провайдера сворачивается до двух строк
  const ANNOUNCE_LONG = 120;

  let refreshing = $state(false);
  let toggling = $state(false);
  // Полный адрес подписки живёт только в состоянии карточки и только пока показан
  let revealedUrl = $state<string | null>(null);
  let urlBusy = $state(false);
  let announceOpen = $state(false);
  let query = $state('');
  let limit = $state(PAGE_FIRST);
  // Узел «Моих узлов», открытый в «Изменить»
  let editId = $state<string | null>(null);
  // Последний открытый id нужен модали, пока она закрывается: контент уже не показывается
  let lastEditId = $state('');
  let settingsOpen = $state(false);
  let diagOpen = $state(false);

  // «Сейчас» для подписей «2 ч назад» и срока подписки; обновляется раз в минуту
  const now = $derived($minuteClock);

  const isMy = $derived(source.kind === 'my');
  const displayName = $derived(isMy ? $t('cfg.nodes.my_name') : source.name);
  const off = $derived(!source.enabled);
  const meta = $derived(source.meta);

  // --- Сводка ---
  const summary = $derived.by(() => {
    const x = installed.xray ? (source.kernels.xray?.count ?? 0) : null;
    const m = installed.mihomo ? (source.kernels.mihomo?.count ?? 0) : null;
    const parts: string[] = [];
    if (x !== null && m !== null) parts.push($t('cfg.nodes.counts', { xray: x, mihomo: m }));
    else if (x !== null) parts.push($t('cfg.nodes.count_xray', { n: x }));
    else if (m !== null) parts.push($t('cfg.nodes.count_mihomo', { n: m }));
    const skipped = source.skipped + source.unsupported;
    if (skipped > 0) parts.push($tp('cfg.nodes.preview_skipped', skipped));
    if (source.stubs > 0) parts.push($tp('cfg.nodes.stubs', source.stubs));
    return parts.join(' · ');
  });

  // --- Метаданные подписки ---
  const total = $derived(meta?.total ?? 0);
  const used = $derived((meta?.upload ?? 0) + (meta?.download ?? 0));
  const hasTraffic = $derived(!isMy && total > 0);
  const level = $derived(trafficLevel(used, total));
  const percent = $derived(trafficPercent(used, total));
  const expireAt = $derived(expireDate(meta?.expire));
  const expired = $derived(expireAt !== null && expireAt.getTime() < now.getTime());
  const announce = $derived(meta?.announce?.trim() ?? '');
  const announceLong = $derived(announce.length > ANNOUNCE_LONG || announce.includes('\n'));
  const supportUrl = $derived(safeHttpUrl(meta?.support_url));
  const webUrl = $derived(safeHttpUrl(meta?.web_url));

  // --- Статусы по установленным ядрам ---
  const statusRows = $derived(
    KERNELS.filter((k) => installed[k.id]).map((k) => {
      const state = source.kernels[k.id];
      const v = kernelStatusView(state, source, installed[k.id], now);
      return {
        ...k,
        view: v,
        label: statusLabel(v, $t),
        count: state?.count ?? 0,
        cabinet: isDeviceRejected(state) ? webUrl : undefined
      };
    })
  );

  // --- Узлы ---
  const items = $derived(nodesForKernel(source, kernel));
  const filtered = $derived(filterByName(items, query));
  const shown = $derived(filtered.slice(0, limit));
  const searchable = $derived(source.nodes.length > SEARCH_THRESHOLD);
  const emptyReason = $derived.by(() => {
    const v = kernelStatusView(source.kernels[kernel], source, installed[kernel], now);
    return v.variant === 'running' ? '' : statusLabel(v, $t);
  });

  async function toggleEnabled(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const want = input.checked;
    toggling = true;
    try {
      await updateSource(source.id, { enabled: want });
    } catch (err) {
      input.checked = source.enabled;
      handleLayerError(err);
    } finally {
      toggling = false;
    }
  }

  async function refresh() {
    if (refreshing) return;
    refreshing = true;
    try {
      const outcome = await refreshSource(source.id);
      const toast = refreshToast(outcome);
      let message = $t(toast.key, toast.params);
      if (outcome.outcome === 'failed' && outcome.error) {
        message += `: ${sourceErrorText(outcome.error, $t).title}`;
      }
      showToast(toast.kind, message);
      void loadSources();
    } catch (err) {
      handleLayerError(err);
    } finally {
      refreshing = false;
    }
  }

  async function toggleReveal() {
    if (revealedUrl !== null) {
      revealedUrl = null;
      return;
    }
    urlBusy = true;
    try {
      revealedUrl = await revealUrl(source.id);
    } catch (err) {
      handleLayerError(err);
    } finally {
      urlBusy = false;
    }
  }

  async function copyUrl() {
    urlBusy = true;
    try {
      const url = await revealUrl(source.id);
      await navigator.clipboard.writeText(url);
      showToast('success', $t('cfg.nodes.copied'));
    } catch (err) {
      handleLayerError(err);
    } finally {
      urlBusy = false;
    }
  }

  async function removeSource() {
    const ok = await showConfirm({
      variant: 'danger',
      title: $t('cfg.nodes.delete_sub_title', { name: source.name }),
      message: $tp('cfg.nodes.delete_sub_body', source.nodes.length),
      confirmLabel: $t('cfg.nodes.delete_sub_btn'),
      cancelLabel: $t('cfg.nodes.cancel')
    });
    if (!ok) return;
    try {
      await deleteSource(source.id);
    } catch (err) {
      // Запись в черновик не удалась: диалог уже закрыт, источник остаётся на месте
      showToast('error', err instanceof Error ? err.message : String(err));
    }
  }

  function editNode(entryId: string) {
    lastEditId = entryId;
    editId = entryId;
  }

  async function removeNode(item: NodeItem) {
    const entryId = item.node.entry_id;
    if (!entryId) return;
    const ok = await showConfirm({
      variant: 'danger',
      title: $t('cfg.nodes.delete_node_title', { name: item.node.name }),
      message: $t('cfg.nodes.delete_node_body'),
      confirmLabel: $t('cfg.nodes.delete_node_btn'),
      cancelLabel: $t('cfg.nodes.cancel')
    });
    if (!ok) return;
    try {
      await deleteNode(entryId);
    } catch (err) {
      // Запись в черновик не удалась: диалог уже закрыт, узел остаётся на месте
      showToast('error', err instanceof Error ? err.message : String(err));
    }
  }

  function onQuery(e: Event) {
    query = (e.currentTarget as HTMLInputElement).value;
    limit = PAGE_FIRST;
  }
</script>

<article class="card source-card" class:off data-testid="nodes-source-{source.id}">
  <div class="sc-head">
    <button
      type="button"
      class="sc-expand"
      aria-expanded={expanded}
      aria-controls="{uid}-list"
      data-testid="nodes-source-toggle-{source.id}"
      onclick={ontoggle}
    >
      <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={16} />
      <span class="sc-name">{displayName}</span>
    </button>
    <span class="chip chip-default">
      {isMy ? $t('cfg.nodes.kind_my') : $t('cfg.nodes.kind_subscription')}
    </span>
    {#if off}
      <span class="chip chip-default" data-testid="nodes-source-off-{source.id}">
        {$t('cfg.nodes.disabled')}
      </span>
    {/if}
    {#if expired}
      <span class="chip chip-danger" data-testid="nodes-source-expired-{source.id}">
        {$t('cfg.nodes.expired')}
      </span>
    {/if}
    <label class="toggle-switch sc-switch">
      <input
        type="checkbox"
        checked={source.enabled}
        disabled={toggling || $applyRunning}
        aria-label={$t('cfg.nodes.source_enabled')}
        data-testid="nodes-enabled-{source.id}"
        onchange={toggleEnabled}
      />
      <span class="toggle-slider"></span>
    </label>
  </div>

  <p class="sc-summary" data-testid="nodes-source-counts-{source.id}">{summary}</p>

  {#if !isMy && meta}
    <div class="sc-meta">
      {#if hasTraffic}
        <div class="sc-traffic" data-testid="nodes-traffic-{source.id}">
          <span class="sc-meta-text">
            {$t('cfg.nodes.traffic_used', { used: formatBytes(used), total: formatBytes(total) })}
          </span>
          <div
            class="sc-bar"
            role="progressbar"
            aria-label={$t('cfg.nodes.traffic_aria')}
            aria-valuemin={0}
            aria-valuemax={total}
            aria-valuenow={Math.min(used, total)}
            data-level={level}
          >
            <div class="sc-bar-fill level-{level}" style:width="{percent}%"></div>
          </div>
        </div>
      {/if}
      {#if expireAt}
        <span
          class="sc-meta-text"
          class:sc-expired={expired}
          data-testid="nodes-expire-{source.id}"
        >
          {$t('cfg.nodes.expires_on', { date: formatDate(expireAt) })}
        </span>
      {/if}
      {#if announce}
        <div class="alert sc-announce" data-testid="nodes-announce-{source.id}">
          <Icon name="info" size={16} class="sc-announce-icon" />
          <div class="sc-announce-body">
            <p class="sc-announce-text" class:clamped={announceLong && !announceOpen}>
              {announce}
            </p>
            {#if announceLong}
              <button
                type="button"
                class="sc-link-btn"
                onclick={() => (announceOpen = !announceOpen)}
              >
                {announceOpen ? $t('cfg.nodes.show_less') : $t('cfg.nodes.show_full')}
              </button>
            {/if}
          </div>
        </div>
      {/if}
      {#if supportUrl || webUrl}
        <div class="sc-links">
          {#if supportUrl}
            <a
              class="sc-link"
              href={supportUrl}
              target="_blank"
              rel="noopener noreferrer"
              data-testid="nodes-support-{source.id}"
            >
              {$t('cfg.nodes.support')}
            </a>
          {/if}
          {#if webUrl}
            <a
              class="sc-link"
              href={webUrl}
              target="_blank"
              rel="noopener noreferrer"
              data-testid="nodes-cabinet-{source.id}"
            >
              {$t('cfg.nodes.cabinet')}
            </a>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  {#if !isMy && source.url_masked}
    <div class="sc-url-row">
      <code class="sc-url" data-testid="nodes-url-{source.id}">
        {revealedUrl ?? source.url_masked}
      </code>
      <div class="sc-url-actions">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          disabled={urlBusy}
          aria-pressed={revealedUrl !== null}
          data-testid="nodes-url-reveal-{source.id}"
          onclick={toggleReveal}
        >
          <Icon name={revealedUrl !== null ? 'eye-off' : 'eye'} size={14} />
          {revealedUrl !== null ? $t('cfg.nodes.hide') : $t('cfg.nodes.reveal')}
        </button>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          disabled={urlBusy}
          data-testid="nodes-url-copy-{source.id}"
          onclick={copyUrl}
        >
          <Icon name="copy" size={14} />
          {$t('cfg.nodes.copy')}
        </button>
      </div>
    </div>
  {/if}

  {#if statusRows.length > 0}
    <ul class="sc-kernels" aria-label={$t('cfg.nodes.status_aria')}>
      {#each statusRows as row (row.id)}
        <li class="sc-kernel" data-testid="nodes-status-{source.id}-{row.id}">
          <span class="sc-kernel-name">{row.name}</span>
          <StatusBadge variant={row.view.variant} label={row.label} />
          <span class="sc-kernel-count" class:sc-dim={off}>
            · {$tp('cfg.nodes.nodes_count', row.count)}
          </span>
          {#if row.cabinet}
            <a
              class="sc-link"
              href={row.cabinet}
              target="_blank"
              rel="noopener noreferrer"
              data-testid="source-status-cabinet"
            >
              {$t('cfg.nodes.cabinet')}
            </a>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  {#if !isMy}
    <div class="sc-actions">
      {#if source.enabled}
        <Button
          variant="secondary"
          class="btn-sm"
          loading={refreshing}
          data-testid="nodes-refresh-{source.id}"
          onclick={refresh}
        >
          <Icon name="refresh" size={14} />
          {$t('cfg.nodes.btn_refresh')}
        </Button>
      {/if}
      <Button
        variant="secondary"
        class="btn-sm"
        data-testid="nodes-settings-{source.id}"
        onclick={() => (settingsOpen = true)}
      >
        <Icon name="edit" size={14} />
        {$t('cfg.nodes.btn_settings')}
      </Button>
      <Button
        variant="secondary"
        class="btn-sm"
        data-testid="nodes-diag-{source.id}"
        onclick={() => (diagOpen = true)}
      >
        <Icon name="info" size={14} />
        {$t('cfg.nodes.btn_diag')}
      </Button>
      <Button
        variant="danger"
        class="btn-sm"
        data-testid="nodes-delete-{source.id}"
        onclick={removeSource}
      >
        <Icon name="delete" size={14} />
        {$t('cfg.nodes.btn_delete_source')}
      </Button>
    </div>
  {/if}

  {#if expanded}
    <div class="sc-list" id="{uid}-list" data-testid="nodes-list-{source.id}">
      {#if source.nodes.length === 0}
        <p class="sc-empty" data-testid="nodes-empty-{source.id}">
          {#if isMy}
            {$t('cfg.nodes.my_empty')}
          {:else}
            {$t('cfg.nodes.no_nodes')}{emptyReason ? ` · ${emptyReason}` : ''}
          {/if}
        </p>
      {:else}
        {#if searchable}
          <input
            class="input sc-search"
            type="search"
            value={query}
            placeholder={$t('cfg.nodes.search_placeholder')}
            aria-label={$t('cfg.nodes.search_placeholder')}
            data-testid="nodes-search-{source.id}"
            oninput={onQuery}
          />
        {/if}
        {#if filtered.length === 0}
          <p class="sc-empty" data-testid="nodes-search-empty-{source.id}">
            {$t('cfg.nodes.search_empty')}
          </p>
        {:else}
          <ul class="sc-nodes">
            {#each shown as item (item.node.key)}
              <NodeRow
                {item}
                onedit={isMy ? editNode : undefined}
                ondelete={isMy ? removeNode : undefined}
              />
            {/each}
          </ul>
          {#if filtered.length > limit}
            <div class="sc-more">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                data-testid="nodes-more-{source.id}"
                onclick={() => (limit += PAGE_STEP)}
              >
                {$t('cfg.nodes.show_more', { n: PAGE_STEP })}
              </button>
            </div>
          {/if}
        {/if}
        {#if source.stubs > 0}
          <p class="sc-stubs">{$tp('cfg.nodes.stubs_unused', source.stubs)}</p>
        {/if}
      {/if}
    </div>
  {/if}
</article>

{#if isMy}
  <NodeEditModal open={editId !== null} entryId={lastEditId} onclose={() => (editId = null)} />
{:else}
  <SourceSettingsModal open={settingsOpen} {source} onclose={() => (settingsOpen = false)} />
  <SourceDiagnosticsModal
    open={diagOpen}
    name={source.name}
    sourceId={source.id}
    {installed}
    initialKernel={kernel}
    onclose={() => (diagOpen = false)}
  />
{/if}

<style>
  .source-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .sc-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .sc-expand {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    flex: 1 1 auto;
    min-width: 0;
    min-height: 32px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--fg-primary);
    font: inherit;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    text-align: left;
    cursor: pointer;
  }

  .sc-expand :global(svg) {
    flex-shrink: 0;
  }

  .sc-name {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .source-card.off .sc-name {
    color: var(--fg-dim);
  }

  .sc-switch {
    flex-shrink: 0;
    margin-left: auto;
  }

  .sc-summary {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .source-card.off .sc-summary,
  .sc-dim {
    color: var(--fg-dim);
  }

  .sc-meta {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .sc-meta-text {
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .sc-expired {
    color: var(--danger);
  }

  .sc-traffic {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .sc-bar {
    height: 6px;
    border-radius: 999px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    overflow: hidden;
  }

  .sc-bar-fill {
    height: 100%;
    border-radius: 999px;
  }

  .level-normal {
    background: var(--accent);
  }

  .level-warning {
    background: var(--warning);
  }

  .level-full {
    background: var(--danger);
  }

  .sc-announce {
    margin: 0;
    min-width: 0;
    background: var(--bg-elevated);
    border-color: var(--border-strong);
    color: var(--fg-secondary);
  }

  .sc-announce :global(.sc-announce-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .sc-announce-body {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .sc-announce-text {
    margin: 0;
    font-size: var(--font-size-base);
    overflow-wrap: anywhere;
    white-space: pre-line;
  }

  .sc-announce-text.clamped {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
  }

  .sc-links {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2) var(--spacing-4);
  }

  .sc-link,
  .sc-link-btn {
    color: var(--accent);
    font-size: var(--font-size-base);
    text-decoration: none;
  }

  .sc-link:hover,
  .sc-link-btn:hover {
    text-decoration: underline;
  }

  .sc-link-btn {
    padding: 0;
    border: 0;
    background: transparent;
    font: inherit;
    font-size: var(--font-size-xs);
    font-weight: 600;
    cursor: pointer;
  }

  .sc-url-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .sc-url {
    flex: 1 1 200px;
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .sc-url-actions,
  .sc-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .sc-kernels {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .sc-kernel {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1) var(--spacing-2);
    min-width: 0;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .sc-kernel-name {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-primary);
    min-width: 56px;
  }

  .sc-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    padding-top: var(--spacing-3);
    border-top: 1px solid var(--border);
  }

  .sc-search {
    width: 100%;
    max-width: 360px;
  }

  .sc-nodes {
    list-style: none;
    margin: 0;
    padding: 0;
    min-width: 0;
  }

  .sc-empty,
  .sc-stubs {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .sc-more {
    display: flex;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .sc-expand,
    .sc-link-btn {
      min-height: 44px;
    }

    .sc-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }
</style>
