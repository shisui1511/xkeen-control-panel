<script lang="ts">
  import { t, tp } from '../../i18n';
  import Pin from '../../lib/components/icons/Pin.svelte';
  import ChevronDown from '../../lib/components/icons/ChevronDown.svelte';
  import HealthBar from './HealthBar.svelte';
  import { getMissingCountryFlag } from '../../lib/countryFlags';
  import { isSystemProxy, type GroupRole } from '../../lib/proxyClassification';
  import { isProxyAlive, computeGroupHealthStats } from '../../lib/proxyStats';
  import type { ProxiesViewMode } from '../../lib/proxyViewPrefs';

  interface ChainItem {
    name: string;
    isGroup: boolean;
  }

  interface Props {
    group: any;
    role: GroupRole;
    groups?: any[];
    proxies?: Record<string, any>;
    isCollapsed?: boolean;
    viewMode?: ProxiesViewMode;
    searchQuery?: string;
    isPinned?: boolean;
    isAutoCore?: boolean;
    testingGroup?: boolean;
    testingProxy?: string;
    batchProgress?: any;
    groupFilter?: string;
    renderLimit?: number;
    quickSelectGroupName?: string | null;
    cardEl?: HTMLElement | null;
    onToggleCollapse?: (groupName: string) => void;
    onTogglePin?: (groupName: string) => void;
    onSelectProxy?: (groupName: string, proxyName: string) => void;
    onTestGroupLatency?: (group: any) => void;
    onTestProxyLatency?: (proxyName: string) => void;
    onOpenQuickSelect?: (anchorEl: HTMLElement, groupName: string) => void;
    onFocusGroupCard?: (groupName: string) => void;
    onSetGroupFilter?: (groupName: string, filter: string) => void;
    onIncreaseRenderLimit?: (groupName: string, total: number) => void;
    onBadgeMouseEnter?: (e: MouseEvent, name: string) => void;
    onBadgeMouseLeave?: () => void;
    onBadgeClick?: (e: MouseEvent, name: string) => void;
    resolveNodeSnapshot?: (name: string) => any;
    getLatencyClass?: (name: string) => string;
    getLatencyText?: (name: string) => string;
    getLatencyTitle?: (name: string) => string;
    getFilteredNodes?: (group: any, query: string) => string[];
    getFilteredGroupNodes?: (groupName: string, nodes: string[]) => string[];
  }

  let {
    group,
    role,
    groups = [],
    proxies = {},
    isCollapsed = false,
    viewMode = 'grid',
    searchQuery = '',
    isPinned = false,
    isAutoCore = false,
    testingGroup = false,
    testingProxy = '',
    batchProgress = null,
    groupFilter = 'all',
    renderLimit = 60,
    quickSelectGroupName = null,
    cardEl = $bindable(),
    onToggleCollapse,
    onTogglePin,
    onSelectProxy,
    onTestGroupLatency,
    onTestProxyLatency,
    onOpenQuickSelect,
    onFocusGroupCard,
    onSetGroupFilter,
    onIncreaseRenderLimit,
    onBadgeMouseEnter,
    onBadgeMouseLeave,
    onBadgeClick,
    resolveNodeSnapshot = () => ({}),
    getLatencyClass = () => '',
    getLatencyText = () => '',
    getLatencyTitle = () => '',
    getFilteredNodes = (g) => g?.all || [],
    getFilteredGroupNodes = (_, n) => n
  }: Props = $props();

  function getProxyTypeLabel(proxy: any): string {
    if (!proxy) return '';
    const type = (proxy.type || '').toLowerCase();
    if (type === 'shadowsocks') return 'SS';
    if (type === 'shadowsocksr') return 'SSR';
    if (type === 'vmess') return 'VMess';
    if (type === 'vless') return 'VLess';
    if (type === 'trojan') return 'Trojan';
    if (type === 'hysteria') return 'Hysteria';
    if (type === 'hysteria2') return 'Hysteria 2';
    if (type === 'tuic') return 'TUIC';
    if (type === 'socks5') return 'Socks5';
    if (type === 'http') return 'HTTP';
    if (type === 'wireguard') return 'WG';
    return proxy.type || '';
  }

  function getGroupTypeLabel(type: string): string {
    const key = (type || '').toLowerCase();
    const labelKeys: Record<string, string> = {
      selector: 'proxies.group_type_selector',
      urltest: 'proxies.group_type_urltest',
      fallback: 'proxies.group_type_fallback',
      loadbalance: 'proxies.group_type_loadbalance',
      relay: 'proxies.group_type_relay'
    };
    const translationKey = labelKeys[key];
    return translationKey ? $t(translationKey) : type;
  }

  function getSelectionChain(groupName: string): ChainItem[] {
    const chain: ChainItem[] = [];
    let current = groupName;
    const visited = new Set<string>();
    while (current && !visited.has(current)) {
      visited.add(current);
      const grp = groups.find((g) => g.name === current);
      if (!grp) break;
      const selected = grp.now;
      if (!selected) break;
      const isSelectedGroup = groups.some((g) => g.name === selected);
      chain.push({ name: selected, isGroup: isSelectedGroup });
      current = selected;
    }
    return chain;
  }

  function getGroupProviderName(grp: any): string | null {
    if (!grp || !Array.isArray(grp.all) || grp.all.length === 0) return null;
    const counts = new Map<string, number>();
    for (const nodeName of grp.all) {
      const prov = proxies[nodeName]?.provider;
      if (prov && typeof prov === 'string' && prov.trim() !== '') {
        counts.set(prov, (counts.get(prov) || 0) + 1);
      }
    }
    if (counts.size === 0) return null;
    let topProv: string | null = null;
    let topCount = 0;
    for (const [prov, count] of counts.entries()) {
      if (count > topCount) {
        topCount = count;
        topProv = prov;
      }
    }
    return topProv;
  }

  const nodes = $derived(getFilteredNodes(group, searchQuery));
  const isMini = $derived(role === 'system');
  const nowUpper = $derived((group.now || '').toUpperCase());
  const groupTypeKey = $derived((group.type || '').toLowerCase());
  const providerName = $derived(getGroupProviderName(group));

  const displayChain = $derived.by(() => {
    const fullChain = getSelectionChain(group.name);
    const fullText = fullChain.map((item) => item.name).join(' › ');
    if (fullChain.length <= 2) {
      return {
        items: fullChain,
        truncated: false,
        fullText
      };
    }
    return {
      items: [fullChain[0], fullChain[fullChain.length - 1]],
      truncated: true,
      fullText
    };
  });

  const filteredNodesList = $derived(getFilteredGroupNodes(group.name, nodes));
  const renderedNodes = $derived(filteredNodesList.slice(0, renderLimit));

  function handleHeadClick(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (target.closest('[data-stop-head-click]')) {
      return;
    }
    onToggleCollapse?.(group.name);
  }

  function handleHeadKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onToggleCollapse?.(group.name);
    }
  }
</script>

<div
  class="group-card"
  class:expanded={!isCollapsed}
  class:gc-mini={isMini}
  class:out-direct={isMini && nowUpper === 'DIRECT'}
  class:out-reject={isMini && (nowUpper === 'REJECT' || nowUpper === 'REJECT-DROP')}
  class:out-pass={isMini && nowUpper === 'PASS'}
  data-group={group.name}
  data-role={role}
  bind:this={cardEl}
>
  <div
    class="gc-head"
    class:collapsible={!isMini}
    role="button"
    tabindex={isMini ? -1 : 0}
    aria-expanded={isMini ? undefined : !isCollapsed}
    onclick={isMini ? undefined : handleHeadClick}
    onkeydown={isMini ? undefined : handleHeadKeydown}
  >
    <div class="gc-head-row1">
      {#if group.icon}
        <span class="group-icon-wrap" aria-hidden="true">
          <img
            src={group.icon}
            alt=""
            loading="lazy"
            referrerpolicy="no-referrer"
            class="brand-icon"
            onerror={(e) => {
              const target = e.currentTarget as HTMLElement;
              if (target) target.style.display = 'none';
            }}
          />
        </span>
      {/if}
      <span class="name">{group.name}</span>
      <span class="type-badge">
        <span class="type-badge-icon" aria-hidden="true">
          {#if groupTypeKey === 'selector'}
            <svg
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"><polyline points="20 6 9 17 4 12" /></svg
            >
          {:else if groupTypeKey === 'urltest' || groupTypeKey === 'fallback'}
            <svg
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7" /><path d="M21 3v6h-6" /></svg
            >
          {:else if groupTypeKey === 'loadbalance'}
            <svg
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"><path d="M7 7h11l-3-3" /><path d="M17 17H6l3 3" /></svg
            >
          {:else if groupTypeKey === 'relay'}
            <svg
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><circle cx="8" cy="12" r="3" /><circle cx="16" cy="12" r="3" /><path
                d="M10.5 12h3"
              /></svg
            >
          {/if}
        </span>
        {getGroupTypeLabel(group.type)}
      </span>

      <div class="gc-head-actions">
        {#if role === 'core' || role === 'service'}
          <button
            type="button"
            class="gc-pin-btn"
            data-stop-head-click
            aria-pressed={isPinned}
            title={isAutoCore
              ? $t('proxies.pin_auto_core')
              : isPinned
                ? $t('proxies.unpin_from_core')
                : $t('proxies.pin_to_core')}
            aria-label={isAutoCore
              ? $t('proxies.pin_auto_core')
              : isPinned
                ? $t('proxies.unpin_from_core')
                : $t('proxies.pin_to_core')}
            disabled={isAutoCore}
            onclick={(e) => {
              e.stopPropagation();
              onTogglePin?.(group.name);
            }}
          >
            <Pin size={13} />
          </button>
        {/if}

        {#if isMini}
          <span class="gc-static-out" title={$t('proxies.static_output')}>{group.now}</span>
        {:else if group.now}
          {@const latencyClass = getLatencyClass(group.now)}
          {@const latencyText = getLatencyText(group.now)}
          <button
            type="button"
            class="gc-lat-box {latencyClass}"
            data-stop-head-click
            title={getLatencyTitle(group.now)}
            onmouseenter={(e) => onBadgeMouseEnter?.(e, group.now)}
            onmouseleave={() => onBadgeMouseLeave?.()}
            onclick={(e) => {
              e.stopPropagation();
              onBadgeClick?.(e, group.now);
            }}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                e.stopPropagation();
                onBadgeClick?.(e as any, group.now);
              }
            }}
          >
            {latencyText}
          </button>
          <button
            type="button"
            class="gc-ping-btn"
            data-stop-head-click
            title={$t('proxies.test_group')}
            aria-label={$t('proxies.test_group')}
            aria-busy={testingGroup}
            disabled={testingGroup}
            onclick={(e) => {
              e.stopPropagation();
              onTestGroupLatency?.(group);
            }}
          >
            {#if testingGroup}
              <span
                class="spinner"
                style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
              ></span>
            {:else}
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" fill="currentColor" />
              </svg>
            {/if}
          </button>
        {/if}

        {#if viewMode === 'list' && !isMini}
          <HealthBar compact={true} stats={computeGroupHealthStats(nodes, resolveNodeSnapshot)} />
        {/if}

        {#if !isMini}
          <button
            type="button"
            class="gc-chevron-btn"
            data-stop-head-click
            aria-expanded={!isCollapsed}
            aria-label={$t('proxies.toggle_group_nodes')}
            onclick={(e) => {
              e.stopPropagation();
              onToggleCollapse?.(group.name);
            }}
          >
            <span class="chevron-wrap" class:rotated={!isCollapsed} aria-hidden="true">
              <ChevronDown size={14} color={isCollapsed ? 'var(--fg-dim)' : 'var(--accent)'} />
            </span>
          </button>
        {/if}
      </div>
    </div>

    {#if !isMini}
      <div class="gc-head-row2" title={displayChain.fullText ? displayChain.fullText : undefined}>
        <span class="gc-count-text">
          {group.all.length}
          {$tp('proxies.nodes', group.all.length)}
        </span>
        {#if providerName}
          <span class="gc-provider-badge" title={$t('proxies.from_provider')}>
            <svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
            </svg>
            {providerName}
          </span>
        {/if}
        <span class="gc-separator">·</span>
        <span class="gc-active-label">{$t('proxies.active')}:</span>

        {#snippet chainPill(item: ChainItem)}
          {@const itemFlag = !item.isGroup ? getMissingCountryFlag(item.name) : null}
          {@const itemLatencyText = getLatencyText(item.name)}
          {@const itemLatencyClass = getLatencyClass(item.name)}
          {#if item.isGroup}
            <button
              type="button"
              class="gc-now-pill gc-now-pill-link"
              class:lat-ok={itemLatencyClass === 'lat ok'}
              class:lat-mid={itemLatencyClass === 'lat mid'}
              class:lat-bad={itemLatencyClass === 'lat bad'}
              title={$t('proxies.goto_parent_group')}
              data-stop-head-click
              onclick={(e) => {
                e.stopPropagation();
                onFocusGroupCard?.(item.name);
              }}
            >
              <div
                class="gc-now-dot"
                class:lat-ok={itemLatencyClass === 'lat ok'}
                class:lat-mid={itemLatencyClass === 'lat mid'}
                class:lat-bad={itemLatencyClass === 'lat bad'}
              ></div>
              {#if itemFlag}
                <span class="flag-icon" aria-hidden="true">{itemFlag}</span>
              {/if}{item.name}
            </button>
          {:else}
            <button
              type="button"
              class="gc-now-pill is-leaf gc-now-pill-trigger"
              class:lat-ok={itemLatencyClass === 'lat ok'}
              class:lat-mid={itemLatencyClass === 'lat mid'}
              class:lat-bad={itemLatencyClass === 'lat bad'}
              data-stop-head-click
              aria-haspopup="listbox"
              aria-expanded={quickSelectGroupName === group.name}
              title={$t('proxies.quick_select_title')}
              onclick={(e) => {
                e.stopPropagation();
                onOpenQuickSelect?.(e.currentTarget as HTMLElement, group.name);
              }}
            >
              <div
                class="gc-now-dot is-leaf"
                class:lat-ok={itemLatencyClass === 'lat ok'}
                class:lat-mid={itemLatencyClass === 'lat mid'}
                class:lat-bad={itemLatencyClass === 'lat bad'}
              ></div>
              {#if itemFlag}
                <span class="flag-icon" aria-hidden="true">{itemFlag}</span>
              {/if}{item.name}
            </button>
          {/if}
        {/snippet}

        {#if displayChain.truncated}
          {@render chainPill(displayChain.items[0])}
          <span class="gc-arrow">›</span>
          <span class="gc-chain-ellipsis" title={displayChain.fullText}>…</span>
          <span class="gc-arrow">›</span>
          {@render chainPill(displayChain.items[1])}
        {:else if displayChain.items.length > 0}
          {#each displayChain.items as item, index}
            {#if index > 0}
              <span class="gc-arrow">›</span>
            {/if}
            {@render chainPill(item)}
          {/each}
        {:else}
          <span style="color:var(--fg-dim)" aria-label={$t('proxies.not_tested')}>—</span>
        {/if}
      </div>
    {/if}
  </div>

  {#if !isMini}
    {#if viewMode === 'grid' && isCollapsed}
      <HealthBar stats={computeGroupHealthStats(nodes, resolveNodeSnapshot)} />
    {:else if !isCollapsed}
      <div class="gc-body">
        <div class="gc-body-inner">
          <div class="group-filters">
            <button
              type="button"
              class="filter-chip"
              class:active={groupFilter === 'all'}
              onclick={() => onSetGroupFilter?.(group.name, 'all')}
            >
              {$t('proxies.filter_all')}
              <span class="filter-count">{nodes.length}</span>
            </button>
            <button
              type="button"
              class="filter-chip"
              class:active={groupFilter === 'working'}
              onclick={() => onSetGroupFilter?.(group.name, 'working')}
            >
              {$t('proxies.filter_working')}
            </button>
            <button
              type="button"
              class="filter-chip"
              class:active={groupFilter === 'timeouts'}
              onclick={() => onSetGroupFilter?.(group.name, 'timeouts')}
            >
              {$t('proxies.filter_timeouts')}
            </button>
            <button
              type="button"
              class="filter-chip"
              class:active={groupFilter === 'latency'}
              onclick={() => onSetGroupFilter?.(group.name, 'latency')}
            >
              {$t('proxies.filter_by_latency')}
            </button>

            <div class="group-actions-spacer"></div>

            <button
              type="button"
              class="filter-chip group-test-btn"
              onclick={() => onTestGroupLatency?.(group)}
              disabled={testingGroup}
              title={$t('proxies.test_group')}
            >
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="currentColor"
                style="margin-right: 4px;"
              >
                <polygon points="5 3 19 12 5 21 5 3"></polygon>
              </svg>
              {$t('proxies.test_group')}
            </button>
          </div>

          <div class="proxy-grid">
            {#each renderedNodes as proxyName}
              {@const proxy = proxies[proxyName]}
              {@const isActive = group.now === proxyName}
              {@const flag = getMissingCountryFlag(proxyName)}
              {@const healthClass = getLatencyClass(proxyName)}
              {@const healthText = getLatencyText(proxyName)}
              <div class="proxy-card" class:now={isActive}>
                <div
                  class="proxy-select-btn"
                  role="button"
                  tabindex={group.type === 'Selector' ? 0 : -1}
                  aria-disabled={group.type !== 'Selector'}
                  title={group.type !== 'Selector'
                    ? $t('proxies.managed_automatically')
                    : undefined}
                  onclick={() =>
                    group.type === 'Selector' && onSelectProxy?.(group.name, proxyName)}
                  onkeydown={(e) => {
                    if (group.type === 'Selector' && (e.key === 'Enter' || e.key === ' ')) {
                      e.preventDefault();
                      onSelectProxy?.(group.name, proxyName);
                    }
                  }}
                >
                  <div class="p-header">
                    <span class="p-name" title={proxyName}>
                      {#if flag}
                        <span class="flag-icon" aria-hidden="true">{flag}</span>
                      {/if}
                      {proxyName}
                    </span>
                    <span class="p-type">{getProxyTypeLabel(proxy)}</span>
                  </div>
                </div>

                <div class="p-footer">
                  {#if (batchProgress?.running && batchProgress?.currentNode === proxyName) || testingProxy === proxyName}
                    <span class="lat dim">
                      <span class="lat-spinner"></span>
                    </span>
                  {:else}
                    <button
                      type="button"
                      class="lat {healthClass}"
                      title={getLatencyTitle(proxyName)}
                      onmouseenter={(e) => onBadgeMouseEnter?.(e, proxyName)}
                      onmouseleave={() => onBadgeMouseLeave?.()}
                      onclick={(e) => onBadgeClick?.(e, proxyName)}
                      onkeydown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          onBadgeClick?.(e as any, proxyName);
                        }
                      }}
                    >
                      {healthText}
                    </button>
                  {/if}

                  <div class="p-actions-wrap">
                    {#if !isSystemProxy(proxyName, proxy?.type)}
                      <button
                        type="button"
                        class="btn-latency-test"
                        onclick={() => onTestProxyLatency?.(proxyName)}
                        disabled={testingProxy === proxyName}
                        title={$t('proxies.test_single')}
                      >
                        {#if testingProxy === proxyName}
                          <span
                            class="spinner"
                            style="--spinner-size: 12px; --spinner-track: currentColor; --spinner-color: transparent;"
                          ></span>
                        {:else}
                          <svg
                            width="12"
                            height="12"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            stroke-width="2"
                            style="opacity: 0.6;"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" /></svg
                          >
                        {/if}
                      </button>
                    {/if}

                    {#if group.type === 'Selector'}
                      <span class="selector-dot" class:active={isActive}
                        >{isActive ? '●' : '○'}</span
                      >
                    {/if}
                  </div>
                </div>
              </div>
            {/each}
          </div>

          {#if filteredNodesList.length > renderLimit}
            {@const remaining = filteredNodesList.length - renderLimit}
            <div class="proxy-grid-footer">
              <button
                type="button"
                class="proxy-grid-more"
                onclick={() => onIncreaseRenderLimit?.(group.name, filteredNodesList.length)}
              >
                {$t('proxies.show_more_nodes')}
                {remaining}
                {$tp('proxies.nodes', remaining)}
              </button>
              <span class="rendered-nodes-hint">
                {$t('proxies.rendered_nodes_hint', {
                  shown: renderLimit,
                  total: filteredNodesList.length
                })}
              </span>
            </div>
          {/if}
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    box-shadow: var(--shadow-sm);
    transition: all 0.2s ease;
  }
  .group-card.expanded {
    grid-column: 1 / -1;
  }
  .group-card:hover {
    box-shadow:
      0 4px 20px rgba(0, 0, 0, 0.35),
      0 0 0 1px rgba(41, 194, 240, 0.15);
  }
  .group-card .gc-head {
    background: linear-gradient(135deg, var(--bg-group-head-from), var(--bg-group-head-to));
    padding: 14px 18px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-bottom: 1px solid var(--border-strong);
    position: relative;
    overflow: hidden;
    width: 100%;
    text-align: left;
  }
  .group-card .gc-head::before {
    content: '';
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: radial-gradient(ellipse at top left, rgba(41, 194, 240, 0.05), transparent 60%);
    pointer-events: none;
  }
  .group-card .gc-head.collapsible {
    cursor: pointer;
    user-select: none;
  }
  .group-card .gc-head.collapsible:hover {
    background: var(--hover);
  }
  .group-card .gc-head.collapsible:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .gc-head-row1 {
    display: flex;
    align-items: center;
    width: 100%;
    gap: 8px;
  }
  .gc-head-row2 {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    font-size: 12px;
    color: var(--fg-secondary);
    width: 100%;
    margin-top: 2px;
  }
  .group-card .gc-head .name {
    font-weight: 800;
    color: var(--fg-primary);
    font-size: 15px;
    letter-spacing: -0.01em;
  }
  .gc-head-actions {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .type-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--font-size-xs);
    padding: 0;
    color: var(--fg-dim);
    font-family: var(--font-family-sans);
    font-weight: 600;
    letter-spacing: 0;
    text-transform: none;
  }
  .type-badge-icon {
    display: inline-flex;
    opacity: 0.7;
  }
  .gc-pin-btn {
    background: none;
    border: none;
    padding: 3px;
    cursor: pointer;
    color: var(--fg-faint);
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s ease;
    opacity: 0.6;
  }
  .gc-pin-btn:hover:not(:disabled) {
    opacity: 1;
    color: var(--accent);
    background: var(--hover);
  }
  .gc-pin-btn[aria-pressed='true'] {
    color: var(--accent);
    opacity: 1;
  }
  .gc-pin-btn:disabled {
    opacity: 0.3;
    cursor: default;
  }
  .gc-static-out {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 700;
    padding: 2px 8px;
    border-radius: var(--radius-sm);
    background: rgba(255, 255, 255, 0.05);
    color: var(--fg-secondary);
  }
  .gc-lat-box {
    padding: 3px 10px;
    border-radius: 99px;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 800;
    background: none;
    border: none;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
  }
  .gc-lat-box:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .gc-lat-box.lat.ok {
    color: var(--success);
    background: rgba(70, 209, 138, 0.15);
    border: 1px solid rgba(70, 209, 138, 0.35);
  }
  .gc-lat-box.lat.mid {
    color: var(--warning);
    background: rgba(240, 180, 80, 0.15);
    border: 1px solid rgba(240, 180, 80, 0.35);
  }
  .gc-lat-box.lat.bad {
    color: var(--danger);
    background: rgba(239, 91, 107, 0.15);
    border: 1px solid rgba(239, 91, 107, 0.35);
  }
  .gc-lat-box.lat.dim {
    color: var(--fg-dim);
    background: rgba(92, 116, 145, 0.15);
    border: 1px solid rgba(92, 116, 145, 0.35);
  }
  .gc-ping-btn {
    background: none;
    border: none;
    padding: 2px 4px;
    cursor: pointer;
    color: var(--fg-faint);
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s ease;
  }
  .gc-ping-btn:hover:not(:disabled) {
    color: var(--accent);
    background: var(--hover);
  }
  .gc-ping-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .gc-ping-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .gc-chevron-btn {
    background: none;
    border: none;
    padding: 2px;
    cursor: pointer;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .chevron-wrap {
    display: inline-flex;
    align-items: center;
    transition: transform 0.2s ease;
  }
  .chevron-wrap.rotated {
    transform: rotate(180deg);
  }
  .gc-count-text {
    color: var(--fg-dim);
  }
  .gc-provider-badge {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 11px;
    color: var(--accent);
    background: rgba(41, 194, 240, 0.08);
    padding: 1px 6px;
    border-radius: 4px;
  }
  .gc-separator {
    color: var(--fg-faint);
  }
  .gc-active-label {
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
  }
  .gc-arrow {
    color: var(--fg-faint);
    margin: 0 2px;
  }
  .gc-chain-ellipsis {
    color: var(--fg-dim);
  }
  .gc-now-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 10px;
    border-radius: var(--radius-lg);
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--border);
    color: var(--fg-primary);
    font-size: var(--font-size-xs);
    font-weight: 600;
    transition: all 0.2s;
    text-align: left;
    font-family: inherit;
  }
  .gc-now-pill.is-leaf {
    background: rgba(41, 194, 240, 0.08);
    border-color: rgba(41, 194, 240, 0.2);
    color: var(--accent);
  }
  .gc-now-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--fg-dim);
  }
  .gc-now-dot.is-leaf {
    background: var(--accent);
  }
  .gc-now-dot.lat-ok {
    background: var(--success);
  }
  .gc-now-dot.lat-mid {
    background: var(--warning);
  }
  .gc-now-dot.lat-bad {
    background: var(--danger);
  }
  .gc-now-pill.lat-ok {
    background: rgba(70, 209, 138, 0.08);
    border-color: rgba(70, 209, 138, 0.2);
    color: var(--success);
  }
  .gc-now-pill.lat-mid {
    background: rgba(240, 180, 80, 0.08);
    border-color: rgba(240, 180, 80, 0.2);
    color: var(--warning);
  }
  .gc-now-pill.lat-bad {
    background: rgba(239, 91, 107, 0.08);
    border-color: rgba(239, 91, 107, 0.2);
    color: var(--danger);
  }
  .gc-now-pill-link {
    cursor: pointer;
  }
  .gc-now-pill-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .gc-now-pill-trigger {
    cursor: pointer;
  }
  .gc-now-pill-trigger:hover {
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .gc-now-pill-trigger:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .proxy-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 8px;
    padding: 12px;
    content-visibility: auto;
    contain-intrinsic-size: 80px;
  }

  .proxy-card {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 0;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    position: relative;
    transition: all var(--transition-fast);
    min-height: 84px;
  }
  .proxy-select-btn {
    display: flex;
    flex-direction: column;
    width: 100%;
    background: none;
    border: 0;
    padding: 10px 12px 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    border-radius: var(--radius-md) var(--radius-md) 0 0;
    flex: 1;
  }
  .proxy-select-btn:disabled,
  .proxy-select-btn[aria-disabled='true'] {
    cursor: default;
  }
  .proxy-select-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .proxy-card::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: var(--radius-md);
    background: linear-gradient(135deg, rgba(41, 194, 240, 0.03), transparent);
    opacity: 0;
    transition: opacity var(--transition-fast);
    pointer-events: none;
  }
  .proxy-card:hover {
    border-color: var(--border-strong);
    transform: translateY(-1px);
    background: var(--hover);
  }
  .proxy-card:hover::after {
    opacity: 1;
  }
  .proxy-card.now {
    background: linear-gradient(135deg, rgba(41, 194, 240, 0.12), rgba(41, 194, 240, 0.04));
    border-color: rgba(41, 194, 240, 0.45);
    box-shadow:
      inset 0 0 0 1px rgba(41, 194, 240, 0.08),
      0 2px 8px rgba(41, 194, 240, 0.08);
  }
  .proxy-card .p-header {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-bottom: 8px;
  }
  .proxy-card .p-name {
    font-weight: 600;
    color: var(--fg-primary);
    font-size: 13px;
    word-break: break-all;
  }
  .flag-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: 'TwemojiMozilla', var(--font-family-sans);
    font-size: 1.15em;
    line-height: 1;
    vertical-align: -0.1em;
    margin-right: 0.35rem;
    flex-shrink: 0;
    user-select: none;
  }
  .proxy-card .p-type {
    color: var(--fg-dim);
    font-size: var(--font-size-xs);
    font-family: var(--font-family-mono);
  }
  .proxy-card .p-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    padding: 4px 12px 10px;
    margin-top: auto;
  }
  .p-actions-wrap {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .btn-latency-test {
    background: transparent;
    border: none;
    padding: 4px;
    width: 24px;
    height: 24px;
    color: var(--fg-dim);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s;
    border-radius: var(--radius-sm);
    flex-shrink: 0;
  }
  .btn-latency-test:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.08);
  }
  .btn-latency-test:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .group-filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    padding: 8px 16px 10px;
    border-bottom: 1px solid var(--border);
  }
  .filter-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px;
    border-radius: var(--radius-full);
    font-size: var(--font-size-xs);
    font-weight: 500;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .filter-chip:hover {
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-primary);
  }
  .filter-chip.active {
    background: rgba(41, 194, 240, 0.12);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 600;
  }
  .filter-chip .filter-count {
    font-size: var(--font-size-xs);
    opacity: 0.7;
  }
  .group-actions-spacer {
    flex: 1;
  }
  .group-test-btn {
    margin-left: auto;
    color: var(--accent);
    border-color: rgba(41, 194, 240, 0.3);
  }
  .group-test-btn:hover {
    background: rgba(41, 194, 240, 0.15);
    border-color: var(--accent);
    color: var(--accent);
  }
  .group-test-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .selector-dot {
    font-size: 14px;
    color: var(--fg-dim);
    font-weight: 700;
  }
  .selector-dot.active {
    color: var(--accent);
  }
  .group-icon-wrap {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: 0 0 26px;
    border-radius: 8px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .group-icon-wrap .brand-icon {
    width: 18px;
    height: 18px;
    object-fit: contain;
    display: block;
  }
  .lat {
    font-family: var(--font-family-mono);
    font-size: 12px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 4px;
    white-space: nowrap;
  }
  button.lat {
    border: none;
    cursor: pointer;
    font: inherit;
    font-family: var(--font-family-mono);
    font-size: 12px;
    font-weight: 600;
    line-height: 1.2;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: opacity 0.15s ease;
  }
  button.lat:hover {
    opacity: 0.85;
  }
  button.lat:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .lat.ok {
    color: var(--success);
    background: color-mix(in srgb, var(--success) 10%, transparent);
  }
  .lat.mid {
    color: var(--warning);
    background: color-mix(in srgb, var(--warning) 10%, transparent);
  }
  .lat.bad {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }
  .lat.dim {
    color: var(--fg-dim);
    background: color-mix(in srgb, var(--fg-dim) 10%, transparent);
  }
  .lat-spinner {
    display: inline-block;
    width: 8px;
    height: 8px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: spin 0.75s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .proxy-grid-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 12px 12px;
    border-top: 1px solid var(--border);
  }
  .proxy-grid-more {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--accent);
    padding: 6px 12px;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.15s;
  }
  .proxy-grid-more:hover {
    background: var(--bg-card-hover);
    border-color: var(--accent);
  }
  .rendered-nodes-hint {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  /* Group List View (D-18, D-19) */
  :global(.group-grid.group-list) .group-card {
    border-radius: var(--radius-sm);
  }
  :global(.group-grid.group-list) .group-card.expanded {
    grid-column: auto;
  }
  :global(.group-list) .gc-head {
    flex-direction: row;
    align-items: center;
    height: 40px;
    padding: 0 12px;
    gap: 10px;
  }
  :global(.group-list) .gc-head-row1 {
    flex: 0 1 auto;
    width: auto;
    gap: 8px;
  }
  :global(.group-list) .gc-head-row2 {
    width: auto;
    margin-top: 0;
    flex: 1 1 auto;
    min-width: 0;
    justify-content: flex-end;
    gap: 6px;
  }
  :global(.group-list) .gc-count-text,
  :global(.group-list) .gc-active-label,
  :global(.group-list) .gc-provider-badge {
    display: none;
  }
  :global(.group-list) .gc-head .name {
    font-size: 13px;
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  :global(.group-list) .health-bar {
    width: 64px;
    margin: 0;
    flex: 0 0 64px;
  }
  :global(.group-list) .proxy-grid {
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 200px), 1fr));
    gap: 6px;
    padding: 8px 12px;
  }
  :global(.group-list) .gc-body {
    display: grid;
    grid-template-rows: 0fr;
    transition: grid-template-rows 0.18s ease;
  }
  :global(.group-list) .expanded .gc-body,
  :global(.group-list .group-card.expanded) .gc-body {
    grid-template-rows: 1fr;
  }
  :global(.group-list) .gc-body-inner {
    overflow: hidden;
    min-height: 0;
  }

  @media (max-width: 768px) {
    :global(.group-list) .gc-head {
      height: 44px;
    }
    :global(.group-list) .gc-chevron-btn,
    :global(.group-list) .gc-ping-btn,
    :global(.group-list) .gc-pin-btn {
      min-width: 44px;
      min-height: 44px;
      justify-content: center;
    }
  }
</style>
