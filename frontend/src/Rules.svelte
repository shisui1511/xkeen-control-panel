<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, currentLang } from './i18n';
  import { capabilities, fetchCapabilities, showToast } from './stores';
  import EmptyState from './components/EmptyState.svelte';
  import PlayIcon from './lib/components/icons/Play.svelte';
  import WarningIcon from './lib/components/icons/Warning.svelte';

  interface Rule {
    type: string;
    payload: string;
    proxy: string;
  }

  interface RuleProvider {
    name: string;
    behavior: string;
    type: string;
    ruleCount: number;
    updatedAt: string;
    vehicleType: string;
  }

  let rules: Rule[] = [];
  let loading = false;
  let error = '';
  let searchQuery = '';
  let typeFilter = '';
  let proxyFilter = '';
  let activeTab: 'rules' | 'providers' = 'rules';

  let ruleProviders: RuleProvider[] = [];
  let loadingProviders = false;
  let updatingProvider: string | null = null;
  let updatingAll = false;

  async function fetchRules() {
    loading = true;
    error = '';

    try {
      const res = await fetch('/api/mihomo/proxy/rules');
      if (!res.ok) throw new Error('Failed to load rules');

      const data = await res.json();
      rules = data.rules || [];
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function fetchRuleProviders() {
    loadingProviders = true;
    try {
      const res = await fetch('/api/mihomo/proxy/providers/rules');
      if (!res.ok) throw new Error('Failed to load rule providers');
      const data = await res.json();
      // API возвращает { providers: { "name": { ... }, ... } }
      const providersMap = data.providers || {};
      ruleProviders = Object.values(providersMap) as RuleProvider[];
    } catch (e: any) {
      ruleProviders = [];
      showToast('error', e.message);
    } finally {
      loadingProviders = false;
    }
  }

  async function updateProvider(name: string) {
    updatingProvider = name;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch(`/api/mihomo/proxy/providers/rules/${encodeURIComponent(name)}`, {
        method: 'PUT',
        headers: {
          'X-CSRF-Token': csrfToken || ''
        }
      });
      if (!res.ok) throw new Error(`Failed to update provider: ${name}`);
      showToast('success', $t('rules.update_success'));
      // Re-fetch чтобы обновить updatedAt и ruleCount
      await fetchRuleProviders();
    } catch (e: any) {
      showToast('error', e.message);
    } finally {
      updatingProvider = null;
    }
  }

  async function updateAllProviders() {
    updatingAll = true;
    let failCount = 0;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      for (const provider of ruleProviders) {
        const res = await fetch(`/api/mihomo/proxy/providers/rules/${encodeURIComponent(provider.name)}`, {
          method: 'PUT',
          headers: {
            'X-CSRF-Token': csrfToken || ''
          }
        });
        if (!res.ok) {
          failCount = failCount + 1;
          showToast('error', `${$t('rules.update')} ${provider.name}: failed`);
        }
      }
      if (failCount === 0) {
        showToast('success', $t('rules.update_all_success'));
      }
      await fetchRuleProviders();
    } catch (e: any) {
      showToast('error', e.message);
    } finally {
      updatingAll = false;
    }
  }

  function formatRelativeTime(isoDate: string): string {
    if (!isoDate) return '—';
    try {
      const date = new Date(isoDate);
      const now = new Date();
      const diffMs = now.getTime() - date.getTime();
      const diffMin = Math.floor(diffMs / 60000);
      if (diffMin < 1) return $t('rules.time_just_now');
      if (diffMin < 60) return $t('rules.time_min_ago', { n: diffMin });
      const diffHours = Math.floor(diffMin / 60);
      if (diffHours < 24) return $t('rules.time_h_ago', { n: diffHours });
      const diffDays = Math.floor(diffHours / 24);
      return $t('rules.time_d_ago', { n: diffDays });
    } catch {
      return isoDate;
    }
  }

  function getFilteredRules(): Rule[] {
    return rules.filter((rule) => {
      if (searchQuery) {
        const q = searchQuery.toLowerCase();
        if (!rule.payload.toLowerCase().includes(q) && !rule.proxy.toLowerCase().includes(q))
          return false;
      }
      if (typeFilter && rule.type !== typeFilter) return false;
      if (proxyFilter && rule.proxy !== proxyFilter) return false;
      return true;
    });
  }

  function getUniqueTypes(): string[] {
    const types = new Set(rules.map((r) => r.type));
    return Array.from(types).sort();
  }

  function getUniqueProxies(): string[] {
    const targets = new Set(rules.map((r) => r.proxy));
    return Array.from(targets).sort();
  }

  function getRuleBadgeClass(type: string): string {
    const typeUpper = type.toUpperCase();
    if (typeUpper === 'DOMAIN-SUFFIX') return 'badge rule-type-domain-suffix';
    if (typeUpper === 'DOMAIN-KEYWORD') return 'badge rule-type-domain-keyword';
    if (typeUpper.startsWith('DOMAIN')) return 'badge rule-type-domain';
    if (typeUpper === 'GEOIP') return 'badge rule-type-geoip';
    if (typeUpper === 'GEOSITE') return 'badge rule-type-geosite';
    if (typeUpper.startsWith('IP-CIDR')) return 'badge rule-type-ip-cidr';
    if (typeUpper.startsWith('IP')) return 'badge badge-warning';
    if (typeUpper === 'PROCESS-NAME') return 'badge rule-type-process';
    if (typeUpper === 'MATCH') return 'badge rule-type-match';
    return 'badge';
  }

  function getTargetBadgeClass(proxy: string): string {
    const proxyUpper = proxy.toUpperCase();
    if (proxyUpper === 'DIRECT') return 'status-badge active';
    if (proxyUpper === 'REJECT') return 'status-badge stopped';
    return 'status-badge';
  }

  let mihomoLaunching = false;
  let _launchTimer: ReturnType<typeof setTimeout> | null = null;

  onDestroy(() => { if (_launchTimer) clearTimeout(_launchTimer); });

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
      _launchTimer = setTimeout(async () => {
        try {
          await fetchCapabilities();
          fetchRules();
        } finally {
          mihomoLaunching = false;
        }
      }, 2500);
    } catch (e: any) {
      showToast('error', e.message);
      mihomoLaunching = false;
    }
  }

  let activeDropdownRule: Rule | null = null;

  function toggleDropdown(event: MouseEvent, rule: Rule) {
    event.stopPropagation();
    if (activeDropdownRule === rule) {
      activeDropdownRule = null;
    } else {
      activeDropdownRule = rule;
    }
  }

  function closeDropdowns() {
    activeDropdownRule = null;
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      closeDropdowns();
    }
  }

  async function copyToClipboard(text: string, successMsg: string) {
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', successMsg);
    } catch (err) {
      showToast('error', 'Failed to copy');
    }
  }

  function copyPayload(rule: Rule) {
    copyToClipboard(rule.payload, $currentLang === 'ru' ? 'Payload скопирован' : 'Payload copied');
    closeDropdowns();
  }

  function copyFullRule(rule: Rule) {
    const text =
      rule.type.toUpperCase() === 'MATCH'
        ? `${rule.type},${rule.proxy}`
        : `${rule.type},${rule.payload},${rule.proxy}`;
    copyToClipboard(text, $currentLang === 'ru' ? 'Правило скопировано' : 'Rule copied');
    closeDropdowns();
  }

  $: filteredRules = getFilteredRules();
  $: nonMatchRules = filteredRules.filter(r => r.type.toUpperCase() !== 'MATCH');
  $: matchRules = filteredRules.filter(r => r.type.toUpperCase() === 'MATCH');

  onMount(() => {
    if ($capabilities === null || $capabilities.mihomo.reachable) {
      fetchRules();
      fetchRuleProviders();
    }
  });
</script>

<svelte:window on:click={closeDropdowns} on:keydown={handleKeydown} />

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('nav.rules')}
      </div>
      <h1>{$t('rules.title')}</h1>
      <p class="sub">{activeTab === 'rules' ? $t('rules.subtitle') : $t('rules.providers_subtitle')}</p>
    </div>
    <div class="ph-actions">
      {#if activeTab === 'providers' && ruleProviders.length > 0}
        <button
          class="btn btn-primary"
          on:click={updateAllProviders}
          disabled={updatingAll}
        >
          {#if updatingAll}
            <span class="spinner-sm"></span>
            {$t('rules.updating')}
          {:else}
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg>
            {$t('rules.update_all')}
          {/if}
        </button>
      {/if}
    </div>
  </div>

  <div class="rules-tabs">
    <button
      class="tab-btn"
      class:active={activeTab === 'rules'}
      on:click={() => (activeTab = 'rules')}
    >{$t('rules.tab_rules')}</button>
    <button
      class="tab-btn"
      class:active={activeTab === 'providers'}
      on:click={() => (activeTab = 'providers')}
    >{$t('rules.tab_providers')}</button>
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
  {:else if activeTab === 'rules'}
    {#if error}
    <EmptyState
      title={$t('ds.empty.error_title')}
      description={error}
      icon={WarningIcon}
      ctaText={$t('app.refresh')}
      oncta={fetchRules}
    />
    {:else}
    <div class="toolbar mb-2">
      <div class="filters">
        <input
          type="text"
          placeholder={$t('rules.search')}
          bind:value={searchQuery}
          class="filter-input"
          style="flex: 1;"
        />
        <select bind:value={typeFilter} class="source-select">
          <option value="">{$t('rules.all_types')}</option>
          {#each getUniqueTypes() as type}
            <option value={type}>{type}</option>
          {/each}
        </select>
        <select bind:value={proxyFilter} class="source-select">
          <option value="">{$t('rules.all_targets')}</option>
          {#each getUniqueProxies() as proxy}
            <option value={proxy}>{proxy}</option>
          {/each}
        </select>
      </div>
    </div>

    <div class="stats mb-2">
      <span class="stat"><b>{rules.length}</b> {$currentLang === 'ru' ? 'всего' : 'total'}</span>
      <span class="stat"
        ><b>{filteredRules.length}</b> {$currentLang === 'ru' ? 'показано' : 'shown'}</span
      >
    </div>

    <div class="table-container">
      <table>
        <thead>
          <tr>
            <th class="col-num" style="width:60px;">#</th>
            <th>{$t('rules.type_col')}</th>
            <th>Payload</th>
            <th>{$t('rules.target')}</th>
            <th style="width:50px;"></th>
          </tr>
        </thead>
        <tbody>
          {#each nonMatchRules as rule, i}
            <tr>
              <td class="mono col-num" style="color:var(--fg-dim);"
                >{String(i + 1).padStart(3, '0')}</td
              >
              <td>
                <span class={getRuleBadgeClass(rule.type)}>
                  {rule.type}
                </span>
              </td>
              <td class="mono">{rule.payload}</td>
              <td>
                <span class={getTargetBadgeClass(rule.proxy)}>
                  {rule.proxy}
                </span>
              </td>
              <td style="position: relative; text-align: right;">
                <button class="action-btn" on:click={(e) => toggleDropdown(e, rule)}>⋯</button>
                {#if activeDropdownRule === rule}
                  <div class="dropdown-menu">
                    <button on:click={() => copyPayload(rule)}>
                      {$t('rules.copy_payload')}
                    </button>
                    <button on:click={() => copyFullRule(rule)}>
                      {$t('rules.copy_rule')}
                    </button>
                  </div>
                {/if}
              </td>
            </tr>
          {:else}
            <tr>
              <td
                colspan="5"
                class="empty-cell"
                style="text-align: center; padding: 2rem; color: var(--fg-dim);"
              >
                {$t('rules.no_rules')}
              </td>
            </tr>
          {/each}
          {#if matchRules.length > 0}
            {#each matchRules as rule}
              <tr class="match-fallback-row">
                <td class="mono col-num" style="color:var(--fg-dim);">—</td>
                <td><span class={getRuleBadgeClass(rule.type)}>{rule.type}</span></td>
                <td class="mono" style="color:var(--fg-dim);">{$t('rules.match_fallback')}</td>
                <td><span class={getTargetBadgeClass(rule.proxy)}>{rule.proxy}</span></td>
                <td style="position: relative; text-align: right;">
                  <button class="action-btn" on:click={(e) => toggleDropdown(e, rule)}>⋯</button>
                  {#if activeDropdownRule === rule}
                    <div class="dropdown-menu">
                      <button on:click={() => copyPayload(rule)}>
                        {$t('rules.copy_payload')}
                      </button>
                      <button on:click={() => copyFullRule(rule)}>
                        {$t('rules.copy_rule')}
                      </button>
                    </div>
                  {/if}
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
    {/if}
  {:else}
    {#if loadingProviders}
      <div class="loading-state">
        <span class="spinner"></span>
      </div>
    {:else if ruleProviders.length === 0}
      <div class="empty-providers">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" style="opacity: 0.4; margin-bottom: 12px;">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <line x1="16" y1="13" x2="8" y2="13" />
          <line x1="16" y1="17" x2="8" y2="17" />
          <polyline points="10 9 9 9 8 9" />
        </svg>
        <p style="font-size: 14px; font-weight: 500; color: var(--fg-secondary); margin-bottom: 4px;">{$t('rules.no_providers')}</p>
      </div>
    {:else}
      <div class="providers-list">
        {#each ruleProviders as provider}
          <div class="provider-card">
            <div class="provider-info">
              <div class="provider-name">{provider.name}</div>
              <div class="provider-meta">
                <span class="provider-badge">{provider.vehicleType || provider.type}</span>
                {#if provider.behavior}
                  <span class="provider-badge">{provider.behavior}</span>
                {/if}
                <span class="provider-count">{provider.ruleCount} {$t('rules.provider_rules_count')}</span>
              </div>
            </div>
            <div class="provider-actions">
              <span class="provider-updated">
                {$t('rules.provider_updated')}: {formatRelativeTime(provider.updatedAt)}
              </span>
              <button
                class="btn btn-secondary btn-sm"
                on:click={() => updateProvider(provider.name)}
                disabled={updatingProvider === provider.name || updatingAll}
              >
                {#if updatingProvider === provider.name}
                  <span class="spinner-sm"></span>
                {:else}
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 4px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg>
                {/if}
                {$t('rules.update')}
              </button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  {/if}
</div>

<style>
  /* Local styles matching redesign spec */
  .table-container {
    overflow-x: auto;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  th {
    padding: 12px 18px;
    text-align: left;
    font-weight: 600;
    color: var(--fg-secondary);
    border-bottom: 1px solid var(--border);
    background: rgba(0, 0, 0, 0.1);
  }

  td {
    padding: 11px 18px;
    border-bottom: 1px solid var(--border-light);
    color: var(--fg-primary);
  }

  tr:last-child td {
    border-bottom: 0;
  }

  tr:hover td {
    background: var(--hover);
  }

  .mono {
    font-family: var(--font-family-mono);
  }

  /* Rule type colored badges */
  :global(.rule-type-domain-suffix) {
    background: rgba(41, 194, 240, 0.12);
    color: #29c2f0;
    border: 1px solid rgba(41, 194, 240, 0.25);
  }
  :global(.rule-type-domain-keyword) {
    background: rgba(234, 179, 8, 0.12);
    color: #eab308;
    border: 1px solid rgba(234, 179, 8, 0.25);
  }
  :global(.rule-type-domain) {
    background: rgba(41, 194, 240, 0.08);
    color: #7dd3fc;
    border: 1px solid rgba(41, 194, 240, 0.18);
  }
  :global(.rule-type-geoip) {
    background: rgba(16, 185, 129, 0.12);
    color: #10b981;
    border: 1px solid rgba(16, 185, 129, 0.25);
  }
  :global(.rule-type-geosite) {
    background: rgba(16, 185, 129, 0.08);
    color: #6ee7b7;
    border: 1px solid rgba(16, 185, 129, 0.18);
  }
  :global(.rule-type-ip-cidr) {
    background: rgba(249, 115, 22, 0.12);
    color: #f97316;
    border: 1px solid rgba(249, 115, 22, 0.25);
  }
  :global(.rule-type-process) {
    background: rgba(156, 163, 175, 0.12);
    color: #9ca3af;
    border: 1px solid rgba(156, 163, 175, 0.25);
  }
  :global(.rule-type-match) {
    background: rgba(156, 163, 175, 0.1);
    color: #9ca3af;
    border: 1px solid rgba(156, 163, 175, 0.2);
  }

  .match-fallback-row td {
    background: rgba(156, 163, 175, 0.04);
    color: var(--fg-dim);
    border-top: 1px solid var(--border);
  }

  .action-btn {
    background: none;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: 16px;
    padding: 4px 8px;
    border-radius: var(--radius-sm);
  }

  .action-btn:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .dropdown-menu {
    position: absolute;
    right: 18px;
    top: 36px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
    z-index: 100;
    min-width: 150px;
    display: flex;
    flex-direction: column;
    padding: 4px 0;
  }

  .dropdown-menu button {
    background: none;
    border: none;
    color: var(--fg-primary);
    padding: 8px 12px;
    text-align: left;
    font-size: 12px;
    cursor: pointer;
    width: 100%;
  }

  .dropdown-menu button:hover {
    background: var(--hover);
  }

  .rules-tabs {
    display: inline-flex;
    gap: 4px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 4px;
    margin-bottom: 16px;
  }

  .tab-btn {
    background: none;
    border: none;
    color: var(--fg-secondary);
    font-size: 13px;
    font-weight: 500;
    padding: 6px 14px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
  }

  .tab-btn:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.04);
  }

  .tab-btn.active {
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-primary);
  }

  .providers-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .provider-card {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 18px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    transition: border-color 0.15s;
  }

  .provider-card:hover {
    border-color: var(--border-hover, var(--border));
  }

  .provider-info {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }

  .provider-name {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .provider-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .provider-badge {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    padding: 2px 8px;
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    letter-spacing: 0.03em;
  }

  .provider-count {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .provider-actions {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
  }

  .provider-updated {
    font-size: 11.5px;
    color: var(--fg-faint);
    white-space: nowrap;
  }

  .btn-sm {
    padding: 4px 10px;
    font-size: 12px;
    height: 28px;
  }

  .spinner-sm {
    display: inline-block;
    width: 12px;
    height: 12px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    margin-right: 4px;
    vertical-align: middle;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .empty-providers {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 60px 20px;
    text-align: center;
    color: var(--fg-dim);
  }

  .loading-state {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 60px 20px;
  }

  /* Column priority on mobile — hide # index, truncate payload */
  @media (max-width: 640px) {
    .col-num {
      display: none;
    }
    .table-container {
      overflow-x: visible;
    }
    table {
      table-layout: fixed;
      width: 100%;
    }
    th:nth-child(2) {
      width: 25%;
    }
    th:nth-child(3) {
      width: auto;
    }
    th:nth-child(4) {
      width: 28%;
    }
    th:last-child {
      width: 40px;
    }
    td.mono {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      max-width: 0;
    }
    td,
    th {
      padding: 10px 10px;
      font-size: 12px;
    }

    /* Mobile: stack provider card vertically */
    .provider-card {
      flex-direction: column;
      align-items: flex-start;
      gap: 10px;
    }
    .provider-actions {
      width: 100%;
      justify-content: space-between;
    }
  }
</style>
