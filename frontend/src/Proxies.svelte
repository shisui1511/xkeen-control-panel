<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang } from './i18n';
  import { capabilities, fetchCapabilities, showToast } from './stores';
  import Skeleton from './components/Skeleton.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import PlayIcon from './lib/components/icons/Play.svelte';
  import WarningIcon from './lib/components/icons/Warning.svelte';
  import ChevronDown from './lib/components/icons/ChevronDown.svelte';

  interface Proxy {
    name: string;
    type: string;
    alive?: boolean;
    delay?: number;
    history?: { time: string; delay: number }[];
    udp?: boolean;
    xudp?: boolean;
    tfo?: boolean;
    tls?: boolean;
    network?: string;
    cipher?: string;
  }

  function getProxyTypeLabel(proxy: Proxy | undefined): string {
    if (!proxy) return 'unknown';
    const parts: string[] = [proxy.type];
    if (proxy.network && proxy.network !== 'tcp') parts.push(proxy.network.toUpperCase());
    else if (proxy.tls) parts.push('TLS');
    if (proxy.cipher && !['auto', 'none', ''].includes(proxy.cipher)) parts.push(proxy.cipher);
    return parts.join(' · ');
  }

  interface ProxyGroup {
    name: string;
    type: string;
    now: string;
    all: string[];
    alive?: boolean;
    delay?: number;
    history?: { time: string; delay: number }[];
  }

  interface ObservatoryStats {
    totalProxies: number;
    healthyProxies: number;
    degradedProxies: number;
    downProxies: number;
    avgLatency: number;
  }

  let groups: ProxyGroup[] = [];
  let proxies: Record<string, Proxy> = {};
  let loading = false;
  let error = '';
  let loadTimedOut = false;
  let testingLatency = false;
  let testingProxy = '';
  let loadTimeoutId: ReturnType<typeof setTimeout> | null = null;
  let collapsedGroups = new Set<string>();
  let filterQuery = '';
  let seenGroups = new Set<string>();
  const pendingTimeouts: ReturnType<typeof setTimeout>[] = [];

  function safeTimeout(fn: () => void | Promise<void>, ms: number): ReturnType<typeof setTimeout> {
    const id = setTimeout(fn, ms);
    pendingTimeouts.push(id);
    return id;
  }

  $: filteredGroups =
    filterQuery.trim() === ''
      ? groups
      : groups.filter((g) => g.name.toLowerCase().includes(filterQuery.trim().toLowerCase()));

  function getLastDelay(proxy: Proxy): number | undefined {
    if (proxy.history && proxy.history.length > 0) {
      return proxy.history[proxy.history.length - 1].delay;
    }
    return proxy.delay;
  }

  function isProxyAlive(proxy: Proxy): boolean {
    if (proxy.history && proxy.history.length > 0) {
      return proxy.history[proxy.history.length - 1].delay > 0;
    }
    return proxy.alive ?? false;
  }

  function updateCollapsed() {
    const current = new Set(groups.map((g) => g.name));
    // Удалять устаревшие записи для исчезнувших групп
    for (const name of [...collapsedGroups]) {
      if (!current.has(name)) collapsedGroups.delete(name);
    }
    // Авто-сворачивать новые большие группы
    for (const g of groups) {
      if (g.all.length > 8 && !seenGroups.has(g.name)) {
        collapsedGroups.add(g.name);
      }
      seenGroups.add(g.name);
    }
    collapsedGroups = collapsedGroups;
  }

  function toggleCollapse(groupName: string) {
    if (collapsedGroups.has(groupName)) {
      collapsedGroups.delete(groupName);
    } else {
      collapsedGroups.add(groupName);
    }
    collapsedGroups = collapsedGroups;
  }

  function getCollapsedProxies(group: ProxyGroup): string[] {
    const active = group.now && proxies[group.now] ? group.now : '';
    const others = group.all.filter((name) => name !== active);

    const sortByDelay = (names: string[]) =>
      [...names].sort((a, b) => {
        const da = proxies[a] ? (getLastDelay(proxies[a]) ?? Infinity) : Infinity;
        const db = proxies[b] ? (getLastDelay(proxies[b]) ?? Infinity) : Infinity;
        return da - db;
      });

    const alive = others.filter((n) => proxies[n] && isProxyAlive(proxies[n]));
    const dead = others.filter((n) => !proxies[n] || !isProxyAlive(proxies[n]));

    const top3 = sortByDelay(alive).slice(0, 3);
    if (top3.length < 3) {
      top3.push(...sortByDelay(dead).slice(0, 3 - top3.length));
    }

    return active ? [active, ...top3] : top3.slice(0, 4);
  }

  function computeStats(): ObservatoryStats {
    const proxyList = Object.values(proxies).filter(
      (p) =>
        p.type !== 'Selector' &&
        p.type !== 'URLTest' &&
        p.type !== 'Fallback' &&
        p.type !== 'LoadBalance' &&
        p.type !== 'Direct' &&
        p.type !== 'Reject'
    );
    const total = proxyList.length;
    const healthy = proxyList.filter(
      (p) => isProxyAlive(p) && (getLastDelay(p) || 0) > 0 && (getLastDelay(p) || 0) < 300
    ).length;
    const degraded = proxyList.filter(
      (p) => isProxyAlive(p) && (getLastDelay(p) || 0) >= 300 && (getLastDelay(p) || 0) < 800
    ).length;
    const down = proxyList.filter(
      (p) => !isProxyAlive(p) || (getLastDelay(p) || 0) === 0 || (getLastDelay(p) || 0) >= 800
    ).length;

    const activeList = proxyList.filter((p) => isProxyAlive(p) && (getLastDelay(p) || 0) > 0);
    const avg =
      activeList.length > 0
        ? activeList.reduce((sum, p) => sum + (getLastDelay(p) || 0), 0) / activeList.length
        : 0;

    return {
      totalProxies: total,
      healthyProxies: healthy,
      degradedProxies: degraded,
      downProxies: down,
      avgLatency: Math.round(avg)
    };
  }

  async function fetchProxies() {
    loading = true;
    error = '';
    loadTimedOut = false;
    if (loadTimeoutId) clearTimeout(loadTimeoutId);
    loadTimeoutId = setTimeout(() => {
      if (loading) {
        loading = false;
        loadTimedOut = true;
        error = $t('ds.empty.load_timeout');
      }
    }, 10000);
    try {
      const res = await fetch('/api/mihomo/proxy/proxies');
      if (!res.ok) throw new Error($t('proxies.load_error'));
      const data = await res.json();
      proxies = data.proxies || {};
      groups = Object.values(proxies)
        .filter((p: Proxy) => {
          return ['Selector', 'URLTest', 'Fallback', 'LoadBalance'].includes(p.type);
        })
        .map((p: any) => ({
          name: p.name,
          type: p.type,
          now: p.now || '',
          all: p.all || [],
          alive: p.alive,
          delay: p.history?.[p.history.length - 1]?.delay,
          history: p.history || []
        }));
      // Enrich proxies with history
      Object.keys(proxies).forEach((name) => {
        if (data.proxies[name]?.history) {
          proxies[name].history = data.proxies[name].history;
        }
      });
      updateCollapsed();
    } catch (e: any) {
      error = e.message;
    } finally {
      if (loadTimeoutId) {
        clearTimeout(loadTimeoutId);
        loadTimeoutId = null;
      }
      loading = false;
    }
  }

  async function selectProxy(groupName: string, proxyName: string) {
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/mihomo/proxy/proxies/${encodeURIComponent(groupName)}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ name: proxyName })
      });
      if (!res.ok) throw new Error($t('proxies.select_error'));
      await fetchProxies();
    } catch (e: any) {
      showToast('error', e.message);
    }
  }

  async function testLatency() {
    testingLatency = true;
    error = '';
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const urlTestGroups = groups.filter((g) => g.type === 'URLTest');
      if (urlTestGroups.length > 0) {
        await Promise.all(
          urlTestGroups.map((g) =>
            fetch(
              `/api/mihomo/proxy/group/${encodeURIComponent(g.name)}/delay?url=http://www.gstatic.com/generate_204&timeout=5000`,
              {
                method: 'GET',
                headers: { 'X-CSRF-Token': csrfToken || '' }
              }
            )
          )
        );
      } else {
        const res = await fetch(
          '/api/mihomo/proxy/proxies/delay?url=http://www.gstatic.com/generate_204&timeout=5000',
          {
            method: 'GET',
            headers: { 'X-CSRF-Token': csrfToken || '' }
          }
        );
        if (!res.ok) throw new Error($t('proxies.load_error'));
      }
      safeTimeout(async () => {
        await fetchProxies();
        testingLatency = false;
      }, 2000);
    } catch (e: any) {
      showToast('error', e.message);
      testingLatency = false;
    }
  }

  async function testProxyLatency(proxyName: string) {
    testingProxy = proxyName;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(
        `/api/mihomo/proxy/proxies/${encodeURIComponent(proxyName)}/delay?url=http://www.gstatic.com/generate_204&timeout=5000`,
        {
          method: 'GET',
          headers: { 'X-CSRF-Token': csrfToken || '' }
        }
      );
      if (!res.ok) throw new Error($t('proxies.load_error'));
      safeTimeout(async () => {
        await fetchProxies();
        testingProxy = '';
      }, 1500);
    } catch (e: any) {
      showToast('error', e.message);
      testingProxy = '';
    }
  }

  function getGroupTypeLabel(type: string): string {
    const labels: Record<string, string> = {
      Selector: 'Selector',
      URLTest: 'URLTest',
      Fallback: 'Fallback',
      LoadBalance: 'LoadBalance'
    };
    return labels[type] || type;
  }

  function nodesLabel(count: number): string {
    if ($currentLang === 'ru') {
      const m10 = count % 10;
      const m100 = count % 100;
      if (m10 === 1 && m100 !== 11) return `${count} узел`;
      if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return `${count} узла`;
      return `${count} узлов`;
    }
    return count === 1 ? `1 node` : `${count} nodes`;
  }

  function getProxyDelay(proxyName: string): number | undefined {
    const proxy = proxies[proxyName];
    if (!proxy) return undefined;
    return getLastDelay(proxy);
  }

  function getLatencyClass(proxyName: string): string {
    const proxy = proxies[proxyName];
    if (!proxy) return 'lat dim';
    if (
      ['DIRECT', 'REJECT'].includes(proxyName.toUpperCase()) ||
      ['Direct', 'Reject', 'Compatible'].includes(proxy.type)
    )
      return 'lat dim';
    const delay = getProxyDelay(proxyName);
    if (delay === undefined || delay === 0 || delay >= 800) return 'lat bad';
    if (delay < 300) return 'lat ok';
    return 'lat mid';
  }

  function getLatencyText(proxyName: string): string {
    const proxy = proxies[proxyName];
    if (!proxy) return '—';
    if (
      ['DIRECT', 'REJECT'].includes(proxyName.toUpperCase()) ||
      ['Direct', 'Reject', 'Compatible'].includes(proxy.type)
    )
      return '—';
    const delay = getProxyDelay(proxyName);
    if (delay === undefined || delay === 0 || delay >= 800) return 'timeout';
    return `${delay} ${$t('app.ms')}`;
  }

  let mihomoLaunching = false;

  async function launchMihomo() {
    mihomoLaunching = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/mihomo/control', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        },
        body: JSON.stringify({ action: 'start' })
      });
      if (!res.ok) throw new Error('Failed to start Mihomo');
      safeTimeout(async () => {
        await fetchCapabilities();
        await fetchProxies();
        mihomoLaunching = false;
      }, 1500);
      safeTimeout(async () => {
        await fetchCapabilities();
        await fetchProxies();
      }, 4000);
    } catch (e: any) {
      showToast('error', e.message);
      mihomoLaunching = false;
    }
  }

  onMount(() => {
    fetchProxies();
    const interval = setInterval(fetchProxies, 10000);
    return () => {
      clearInterval(interval);
      if (loadTimeoutId) clearTimeout(loadTimeoutId);
      pendingTimeouts.forEach(clearTimeout);
    };
  });
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('proxies.title')}
      </div>
      <h1>{$t('proxies.title')}</h1>
      <p class="sub">{$t('proxies.subtitle')}</p>
    </div>
    <div class="ph-actions">
      <input
        class="group-search"
        type="search"
        bind:value={filterQuery}
        placeholder={$t('proxies.filter_placeholder')}
        aria-label={$t('proxies.filter_placeholder')}
      />
      <button class="btn btn-secondary" on:click={fetchProxies} disabled={loading}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
        >
        {loading ? $t('app.loading') : $t('app.refresh')}
      </button>
      <button class="btn btn-primary" on:click={testLatency} disabled={testingLatency}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="currentColor"
          style="margin-right: 6px;"><polygon points="5 3 19 12 5 21 5 3" /></svg
        >
        {testingLatency ? $t('proxies.testing') : $t('proxies.test_latency')}
      </button>
    </div>
  </div>

  {#if $capabilities !== null && !$capabilities.mihomo.reachable}
    <EmptyState
      title={$t('ds.empty.mihomo_offline_title')}
      description={$t('ds.empty.mihomo_offline_desc')}
      icon={PlayIcon}
      ctaText={mihomoLaunching
        ? $t('ds.empty.mihomo_offline_loading')
        : $t('ds.empty.mihomo_offline_cta')}
      ctaLoading={mihomoLaunching}
      oncta={launchMihomo}
    />
  {:else if error}
    <EmptyState
      title={$t('ds.empty.error_title')}
      description={error}
      icon={WarningIcon}
      ctaText={$t('app.refresh')}
      oncta={fetchProxies}
    />
  {:else}
    <!-- Observatory statistics -->
    {#if groups.length > 0 && $capabilities?.mihomo?.reachable}
      {@const stats = computeStats()}
      <div class="card" style="margin-bottom:18px;">
        <h2 class="card-title" style="margin-top: 0;">{$t('proxies.observatory_title')}</h2>
        <div class="stats-grid">
          <div class="stat-box">
            <div class="stat-label">{$t('proxies.obs_total')}</div>
            <div class="stat-value">{stats.totalProxies}</div>
            <div class="res-sub">{$t('proxies.obs_total_sub', { groupsCount: groups.length })}</div>
          </div>
          <div class="stat-box">
            <div class="stat-label">{$t('proxies.obs_healthy')}</div>
            <div class="stat-value" style="color:var(--success);">{stats.healthyProxies}</div>
            <div class="res-sub">{$t('proxies.obs_healthy_sub')}</div>
          </div>
          <div class="stat-box">
            <div class="stat-label">{$t('proxies.obs_degraded')}</div>
            <div class="stat-value" style="color:var(--warning);">{stats.degradedProxies}</div>
            <div class="res-sub">{$t('proxies.obs_degraded_sub')}</div>
          </div>
          <div class="stat-box">
            <div class="stat-label">{$t('proxies.obs_unreachable')}</div>
            <div class="stat-value" style="color:var(--danger);">{stats.downProxies}</div>
            <div class="res-sub">{$t('proxies.obs_unreachable_sub')}</div>
          </div>
        </div>
      </div>
    {/if}

    <!-- Groups Grid -->
    {#if loading && groups.length === 0}
      <div class="group-grid">
        {#each Array(4) as _}
          <div class="group-card">
            <div class="gc-head">
              <Skeleton type="text-line" width="120px" />
              <Skeleton type="text-line" width="60px" style="margin-left: 10px;" />
            </div>
            <div class="gc-body">
              {#each Array(3) as _}
                <div
                  class="proxy-row"
                  style="display: flex; justify-content: space-between; align-items: center; padding: 11px 18px;"
                >
                  <Skeleton type="text-line" width="100px" />
                  <Skeleton type="text-line" width="40px" />
                </div>
              {/each}
            </div>
          </div>
        {/each}
      </div>
    {:else if groups.length === 0 && !loading}
      <div class="card">
        <p class="text-secondary">{$t('proxies.no_proxies')}</p>
      </div>
    {:else}
      <div class="group-grid">
        {#each groups as group}
          {@const isFiltered =
            filterQuery.trim() !== '' &&
            !group.name.toLowerCase().includes(filterQuery.trim().toLowerCase())}
          {@const isCollapsed = collapsedGroups.has(group.name)}
          {@const collapsible = group.all.length > 8}
          {@const shownProxies = isCollapsed ? getCollapsedProxies(group) : group.all}
          {@const hiddenCount = isCollapsed ? group.all.length - shownProxies.length : 0}
          {@const ROW_HEIGHT_PX = 44}
          <div class="group-card" style={isFiltered ? 'display:none;' : ''}>
            <div
              class="gc-head"
              class:collapsible
              role={collapsible ? 'button' : undefined}
              tabindex={collapsible ? 0 : undefined}
              aria-expanded={collapsible ? !isCollapsed : undefined}
              on:click={() => collapsible && toggleCollapse(group.name)}
              on:keydown={(e) =>
                (e.key === 'Enter' || e.key === ' ') && collapsible && toggleCollapse(group.name)}
            >
              <span class="name">{group.name}</span>
              <span class="type">{getGroupTypeLabel(group.type)}</span>
              {#if group.type !== 'Fallback'}
                <span style="margin-left:auto;" class="status-badge active">
                  {nodesLabel(group.all.length)}
                </span>
              {:else}
                <span style="margin-left:auto;" class="status-badge active">{group.now || '—'}</span
                >
              {/if}
              {#if collapsible}
                <span class="chevron-wrap" class:rotated={!isCollapsed} aria-hidden="true">
                  <ChevronDown size={14} color={isCollapsed ? 'var(--fg-dim)' : 'var(--accent)'} />
                </span>
              {/if}
            </div>
            <div
              class="gc-body"
              style="max-height: {isCollapsed
                ? shownProxies.length * ROW_HEIGHT_PX + (hiddenCount > 0 ? 30 : 4) + 'px'
                : '2000px'};"
            >
              {#each shownProxies as proxyName}
                {@const isActive = group.now === proxyName}
                {@const healthClass = getLatencyClass(proxyName)}
                {@const healthText = getLatencyText(proxyName)}
                {@const proxy = proxies[proxyName]}

                <div
                  class="proxy-row"
                  class:now={isActive}
                  role="button"
                  tabindex="0"
                  on:click={() => group.type === 'Selector' && selectProxy(group.name, proxyName)}
                  on:keydown={(e) =>
                    e.key === 'Enter' &&
                    group.type === 'Selector' &&
                    selectProxy(group.name, proxyName)}
                  style={group.type === 'Selector' ? 'cursor: pointer;' : ''}
                >
                  <div>
                    <div class="p-name">{proxyName}</div>
                    <div class="p-type">{getProxyTypeLabel(proxy)}</div>
                  </div>

                  <div style="display: flex; align-items: center; gap: 8px;">
                    <span class={healthClass}>{healthText}</span>
                    {#if !['DIRECT', 'REJECT'].includes(proxyName.toUpperCase()) && !['Direct', 'Reject', 'Compatible'].includes(proxy?.type || '')}
                      <button
                        class="btn-latency-test"
                        on:click|stopPropagation={() => testProxyLatency(proxyName)}
                        disabled={testingProxy === proxyName}
                        title={$t('proxies.test_single')}
                        style="background: transparent; border: none; padding: 4px; color: var(--fg-dim); cursor: pointer; display: inline-flex; align-items: center; justify-content: center; transition: color 0.2s;"
                      >
                        {#if testingProxy === proxyName}
                          <span class="spinner" style="font-size: 10px; font-family: monospace;"
                            >...</span
                          >
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
                  </div>

                  {#if group.type === 'Selector'}
                    <div>
                      <button
                        class="btn-select"
                        style="background: none; border: none; padding: 0 4px; color: var(--accent); cursor: pointer; font-size: 14px;"
                        on:click|stopPropagation={() => selectProxy(group.name, proxyName)}
                      >
                        {isActive ? '●' : '○'}
                      </button>
                    </div>
                  {:else}
                    <div></div>
                  {/if}
                </div>
              {/each}
              {#if isCollapsed}
                {#if hiddenCount > 0}
                  <div
                    class="more-hint"
                    role="button"
                    tabindex="0"
                    on:click={() => toggleCollapse(group.name)}
                    on:keydown={(e) =>
                      (e.key === 'Enter' || e.key === ' ') && toggleCollapse(group.name)}
                  >
                    {$t('proxies.more_hint', { count: hiddenCount })}
                  </div>
                {/if}
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  {/if}
</div>

<style>
  /* Observatory: compact padding for this page only */
  .stat-box {
    padding: 12px 18px 14px;
  }

  /* proxies: group cards grid */
  .group-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 16px;
  }
  .group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .group-card .gc-head {
    padding: 14px 18px;
    display: flex;
    align-items: center;
    gap: 10px;
    border-bottom: 1px solid var(--border);
  }
  .group-card .gc-head.collapsible {
    cursor: pointer;
    user-select: none;
  }
  .group-card .gc-head.collapsible:hover {
    background: var(--hover);
  }
  .group-card .gc-head .name {
    font-weight: 700;
    color: var(--fg-primary);
    font-size: 14px;
  }
  .group-card .gc-head .type {
    color: var(--fg-dim);
    font-size: 11px;
    font-family: var(--font-family-mono);
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }
  .group-card .gc-body {
    padding: 0;
    overflow: hidden;
    transition: max-height var(--transition-normal);
  }
  .proxy-row {
    position: relative;
    display: grid;
    grid-template-columns: 1fr auto auto;
    gap: 14px;
    align-items: center;
    padding: 4px 8px;
    min-height: 36px;
    border-bottom: 1px solid var(--border-light);
  }
  .proxy-row:last-child {
    border-bottom: 0;
  }
  .proxy-row:hover {
    background: var(--hover);
  }
  .proxy-row.now {
    background: var(--accent-soft);
    padding-left: 20px;
  }
  .proxy-row.now::before {
    content: '→';
    color: var(--accent);
    font-weight: 700;
    position: absolute;
    left: 4px;
    top: 50%;
    transform: translateY(-50%);
  }
  .proxy-row .p-name {
    font-weight: 500;
    color: var(--fg-primary);
    font-size: 13px;
  }
  .proxy-row .p-type {
    color: var(--fg-dim);
    font-size: 11px;
    font-family: var(--font-family-mono);
  }
  .lat {
    font-family: var(--font-family-mono);
    font-size: 12px;
    padding: 2px 8px;
    border-radius: 3px;
    border: 1px solid var(--border);
  }
  .lat.ok {
    color: var(--success);
    border-color: rgba(70, 209, 138, 0.4);
    background: rgba(70, 209, 138, 0.08);
  }
  .lat.mid {
    color: var(--warning);
    border-color: rgba(240, 180, 80, 0.4);
    background: rgba(240, 180, 80, 0.08);
  }
  .lat.bad {
    color: var(--danger);
    border-color: rgba(239, 91, 107, 0.4);
    background: rgba(239, 91, 107, 0.08);
  }
  .lat.dim {
    color: var(--fg-dim);
  }

  .btn-latency-test:hover {
    color: var(--accent) !important;
  }
  .btn-latency-test:hover svg {
    opacity: 1 !important;
  }

  .group-search {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 13px;
    font-family: var(--font-family-sans);
    padding: 4px 12px;
    width: 200px;
    outline: none;
    transition: border-color var(--transition-fast);
  }
  .group-search:focus {
    border-color: var(--accent);
  }
  .group-search::placeholder {
    color: var(--fg-dim);
  }
  .group-search::-webkit-search-cancel-button {
    display: none;
  }

  .chevron-wrap {
    display: inline-flex;
    flex-shrink: 0;
    transition: transform var(--transition-fast);
  }
  .chevron-wrap.rotated {
    transform: rotate(180deg);
  }

  .more-hint {
    padding: 4px 16px;
    text-align: center;
    cursor: pointer;
    color: var(--fg-dim);
    font-size: 12px;
    border-top: 1px solid var(--border-light);
  }
  .more-hint:hover {
    background: var(--hover);
  }

  /* Mobile: proxy cards stack, observatory stats handled globally at 768px */
  @media (max-width: 640px) {
    .group-grid {
      gap: 10px;
    }
    .group-card .gc-head {
      padding: 12px 14px;
      flex-wrap: wrap;
      gap: 6px;
    }
    .proxy-row {
      padding: 10px 14px;
      gap: 8px;
    }
    .proxy-row .p-name {
      font-size: 12px;
    }
    .lat {
      font-size: 11px;
      padding: 2px 5px;
    }
    .group-search {
      width: 100%;
    }
  }
</style>
