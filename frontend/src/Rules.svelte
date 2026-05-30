<script lang="ts">
  import { onMount } from 'svelte';
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

  let rules: Rule[] = [];
  let loading = false;
  let error = '';
  let searchQuery = '';
  let typeFilter = '';
  let proxyFilter = '';
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
      setTimeout(async () => {
        await fetchCapabilities();
        fetchRules();
        mihomoLaunching = false;
      }, 1500);
      setTimeout(async () => {
        await fetchCapabilities();
        fetchRules();
      }, 4000);
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

  onMount(() => {
    if ($capabilities === null || $capabilities.mihomo.reachable) {
      fetchRules();
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
      <p class="sub">{$t('rules.subtitle')}</p>
    </div>
    <div class="ph-actions"></div>
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
        ><b>{getFilteredRules().length}</b> {$currentLang === 'ru' ? 'показано' : 'shown'}</span
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
          {#each getFilteredRules() as rule, i}
            {#if rule.type.toUpperCase() !== 'MATCH'}
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
            {/if}
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
          {#if getFilteredRules().some((r) => r.type.toUpperCase() === 'MATCH')}
            {#each getFilteredRules().filter((r) => r.type.toUpperCase() === 'MATCH') as rule}
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
  }
</style>
