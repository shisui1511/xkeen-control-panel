<script lang="ts">
  import { t } from '../../i18n';
  import Select from '../Select.svelte';
  import EmptyState from '../EmptyState.svelte';
  import RulesIcon from '../../lib/components/icons/Rules.svelte';
  import { showToast } from '../../stores';

  export interface KernelRule {
    type: string;
    payload: string;
    proxy: string;
  }

  interface Props {
    rules: KernelRule[];
    loading?: boolean;
  }

  let { rules = [], loading = false }: Props = $props();

  let searchQuery = $state('');
  let typeFilter = $state('');
  let proxyFilter = $state('');
  let activeDropdownKey = $state<string | null>(null);

  // Dynamic filter options
  let availableTypes = $derived.by(() => {
    const set = new Set<string>();
    for (const r of rules) {
      if (r.type) set.add(r.type);
    }
    const list = Array.from(set).sort();
    return [
      { value: '', label: $t('rules.all_types') },
      ...list.map((type) => ({ value: type, label: type }))
    ];
  });

  let availableProxies = $derived.by(() => {
    const set = new Set<string>();
    for (const r of rules) {
      if (r.proxy) set.add(r.proxy);
    }
    const list = Array.from(set).sort();
    return [
      { value: '', label: $t('rules.all_targets') },
      ...list.map((proxy) => ({ value: proxy, label: proxy }))
    ];
  });

  let filteredRules = $derived.by(() => {
    return rules.filter((rule) => {
      if (searchQuery.trim()) {
        const q = searchQuery.toLowerCase().trim();
        const inPayload = rule.payload ? rule.payload.toLowerCase().includes(q) : false;
        const inProxy = rule.proxy ? rule.proxy.toLowerCase().includes(q) : false;
        const inType = rule.type ? rule.type.toLowerCase().includes(q) : false;
        if (!inPayload && !inProxy && !inType) return false;
      }
      if (typeFilter && rule.type !== typeFilter) return false;
      if (proxyFilter && rule.proxy !== proxyFilter) return false;
      return true;
    });
  });

  let nonMatchRules = $derived(filteredRules.filter((r) => r.type.toUpperCase() !== 'MATCH'));
  let matchRules = $derived(filteredRules.filter((r) => r.type.toUpperCase() === 'MATCH'));

  function getRuleBadgeClass(type: string): string {
    const tLower = (type || '').toLowerCase();
    if (tLower.includes('domain')) return 'rule-badge-accent';
    if (tLower.includes('geo')) return 'rule-badge-success';
    if (tLower.includes('ip') || tLower.includes('cidr')) return 'rule-badge-warning';
    if (tLower === 'match') return 'rule-badge-dim';
    return 'rule-badge-neutral';
  }

  function getTargetBadgeClass(target: string): string {
    const tUpper = (target || '').toUpperCase();
    if (tUpper === 'DIRECT') return 'target-badge-direct';
    if (tUpper === 'REJECT') return 'target-badge-reject';
    return 'target-badge-proxy';
  }

  async function copyPayload(payload: string) {
    activeDropdownKey = null;
    try {
      await navigator.clipboard.writeText(payload);
      showToast('success', $t('app.copied'));
    } catch {
      showToast('error', $t('rules.clipboard_error'));
    }
  }

  async function copyFullRule(rule: KernelRule) {
    activeDropdownKey = null;
    const text = `${rule.type},${rule.payload},${rule.proxy}`;
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', $t('app.copied'));
    } catch {
      showToast('error', $t('rules.clipboard_error'));
    }
  }

  function toggleDropdown(e: MouseEvent, key: string) {
    e.stopPropagation();
    activeDropdownKey = activeDropdownKey === key ? null : key;
  }

  function handleWindowClick() {
    activeDropdownKey = null;
  }

  function resetFilters() {
    searchQuery = '';
    typeFilter = '';
    proxyFilter = '';
  }
</script>

<svelte:window onclick={handleWindowClick} />

<div class="kernel-rules-container">
  <!-- Filter Toolbar -->
  <div class="card toolbar-card filters">
    <div class="toolbar-row">
      <div class="search-field">
        <input
          type="text"
          class="input search-input filter-input font-mono"
          placeholder={$t('rules.all_rules_search_placeholder')}
          bind:value={searchQuery}
        />
        {#if searchQuery}
          <button
            class="clear-search-btn"
            onclick={() => (searchQuery = '')}
            aria-label={$t('app.clear')}
            title={$t('app.clear')}
          >
            ×
          </button>
        {/if}
      </div>

      <div class="filter-fields-row">
        <div class="filter-field">
          <Select
            class="source-select"
            bind:value={typeFilter}
            options={availableTypes}
            ariaLabel={$t('rules.all_types')}
          />
        </div>

        <div class="filter-field">
          <Select
            class="source-select"
            bind:value={proxyFilter}
            options={availableProxies}
            ariaLabel={$t('rules.all_targets')}
          />
        </div>
      </div>
    </div>

    <div class="toolbar-stats">
      <span class="stats-text">
        {$t('rules.shown', { count: filteredRules.length })}
        {#if filteredRules.length !== rules.length}
          <span class="stats-total">({$t('rules.total', { count: rules.length })})</span>
        {/if}
      </span>
    </div>
  </div>

  <!-- Loading State -->
  {#if loading}
    <div class="card loading-card">
      <div class="spinner"></div>
      <span>{$t('app.loading')}</span>
    </div>
  {:else if rules.length === 0}
    <EmptyState
      icon={RulesIcon}
      title={$t('rules.no_rules')}
      description={$t('rules.all_rules_subtitle')}
    />
  {:else if filteredRules.length === 0}
    <EmptyState
      icon={RulesIcon}
      title={$t('rules.all_rules_empty')}
      description={$t('rules.all_rules_subtitle')}
      ctaText={$t('rules.reset_filters')}
      oncta={resetFilters}
    />
  {:else}
    <!-- Rules Table -->
    <div class="card table-card">
      <div class="table-responsive">
        <table class="rules-table">
          <thead>
            <tr>
              <th class="col-num">#</th>
              <th class="col-type">{$t('rules.type_col')}</th>
              <th class="col-payload">Payload</th>
              <th class="col-target">{$t('rules.target')}</th>
              <th class="col-actions"></th>
            </tr>
          </thead>
          <tbody>
            {#each nonMatchRules as rule, i (i + '_' + rule.type + '_' + rule.payload)}
              <tr>
                <td class="col-num mono">{String(i + 1).padStart(3, '0')}</td>
                <td class="col-type">
                  <span class="badge {getRuleBadgeClass(rule.type)}">{rule.type}</span>
                </td>
                <td class="col-payload mono text-break">{rule.payload}</td>
                <td class="col-target">
                  <span class="badge {getTargetBadgeClass(rule.proxy)}">{rule.proxy}</span>
                </td>
                <td class="col-actions">
                  <button
                    class="btn-more"
                    onclick={(e) => toggleDropdown(e, 'rule_' + i)}
                    aria-label={$t('rules.actions_col')}
                    title={$t('rules.actions_col')}
                  >
                    ⋯
                  </button>
                  {#if activeDropdownKey === 'rule_' + i}
                    <div
                      class="dropdown-menu"
                      class:dropdown-menu-up={i >= nonMatchRules.length - 2}
                    >
                      <button onclick={() => copyPayload(rule.payload)}>
                        {$t('rules.copy_payload')}
                      </button>
                      <button onclick={() => copyFullRule(rule)}>
                        {$t('rules.copy_rule')}
                      </button>
                    </div>
                  {/if}
                </td>
              </tr>
            {/each}

            {#if matchRules.length > 0}
              {#each matchRules as rule, j ('match_' + j)}
                <tr class="match-row">
                  <td class="col-num mono">—</td>
                  <td class="col-type">
                    <span class="badge {getRuleBadgeClass(rule.type)}">{rule.type}</span>
                  </td>
                  <td class="col-payload mono match-payload">{$t('rules.match_fallback')}</td>
                  <td class="col-target">
                    <span class="badge {getTargetBadgeClass(rule.proxy)}">{rule.proxy}</span>
                  </td>
                  <td class="col-actions">
                    <button
                      class="btn-more"
                      onclick={(e) => toggleDropdown(e, 'match_' + j)}
                      aria-label={$t('rules.actions_col')}
                      title={$t('rules.actions_col')}
                    >
                      ⋯
                    </button>
                    {#if activeDropdownKey === 'match_' + j}
                      <div class="dropdown-menu dropdown-menu-up">
                        <button onclick={() => copyPayload(rule.proxy)}>
                          {$t('rules.copy_payload')}
                        </button>
                        <button onclick={() => copyFullRule(rule)}>
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
    </div>
  {/if}
</div>

<style>
  .kernel-rules-container {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .toolbar-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .toolbar-row {
    display: flex;
    gap: 10px;
    align-items: center;
    flex-wrap: wrap;
  }

  .search-field {
    flex: 2;
    min-width: 220px;
    position: relative;
    display: flex;
    align-items: center;
  }

  .search-input {
    width: 100%;
    padding: 8px 28px 8px 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: var(--font-size-sm);
    box-sizing: border-box;
    transition: border-color var(--transition-fast, 0.15s ease);
  }

  .clear-search-btn {
    position: absolute;
    right: 8px;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: var(--font-size-base);
    line-height: 1;
    padding: 4px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: color var(--transition-fast, 0.15s ease);
  }

  .clear-search-btn:hover {
    color: var(--fg-primary);
  }

  .search-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
  }

  .filter-field {
    flex: 1;
    min-width: 140px;
  }

  .toolbar-stats {
    display: flex;
    justify-content: flex-end;
  }

  .stats-text {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    font-family: var(--font-family-mono, monospace);
  }

  .stats-total {
    color: var(--fg-muted);
    margin-left: 4px;
  }

  .loading-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 48px;
    gap: 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--fg-secondary);
  }

  .spinner {
    width: 28px;
    height: 28px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  .table-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .table-responsive {
    overflow-x: auto;
  }

  .rules-table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--font-size-sm);
    text-align: left;
  }

  th {
    padding: 10px 14px;
    font-weight: 600;
    color: var(--fg-secondary);
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
    white-space: nowrap;
    font-size: var(--font-size-xs);
    letter-spacing: 0.02em;
  }

  td {
    padding: 9px 14px;
    border-bottom: 1px solid var(--border-light, var(--border));
    color: var(--fg-primary);
    vertical-align: middle;
  }

  tr:last-child td {
    border-bottom: 0;
  }

  tr:hover td {
    background: var(--hover);
  }

  .match-row td {
    background: color-mix(in srgb, var(--fg-dim) 5%, transparent);
    border-top: 1px solid var(--border);
  }

  .col-num {
    width: 48px;
    color: var(--fg-dim);
    font-size: var(--font-size-xs);
    text-align: center;
  }

  .col-type {
    width: 140px;
  }

  .col-target {
    width: 160px;
  }

  .col-actions {
    width: 44px;
    text-align: right;
    position: relative;
  }

  .mono {
    font-family: var(--font-family-mono, monospace);
  }

  .text-break {
    word-break: break-all;
  }

  .match-payload {
    color: var(--fg-dim);
    font-style: italic;
  }

  .badge {
    display: inline-block;
    font-size: var(--font-size-xs);
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 4px;
    letter-spacing: 0.02em;
    white-space: nowrap;
  }

  .rule-badge-accent {
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--accent);
    border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
  }

  .rule-badge-success {
    background: color-mix(in srgb, var(--success) 12%, transparent);
    color: var(--success);
    border: 1px solid color-mix(in srgb, var(--success) 25%, transparent);
  }

  .rule-badge-warning {
    background: color-mix(in srgb, var(--warning) 12%, transparent);
    color: var(--warning);
    border: 1px solid color-mix(in srgb, var(--warning) 25%, transparent);
  }

  .rule-badge-dim {
    background: color-mix(in srgb, var(--fg-dim) 12%, transparent);
    color: var(--fg-secondary);
    border: 1px solid color-mix(in srgb, var(--fg-dim) 22%, transparent);
  }

  .rule-badge-neutral {
    background: var(--bg-elevated);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
  }

  .target-badge-direct {
    background: color-mix(in srgb, var(--success) 14%, transparent);
    color: var(--success);
    border: 1px solid color-mix(in srgb, var(--success) 28%, transparent);
  }

  .target-badge-reject {
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    color: var(--danger);
    border: 1px solid color-mix(in srgb, var(--danger) 28%, transparent);
  }

  .target-badge-proxy {
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    color: var(--accent);
    border: 1px solid color-mix(in srgb, var(--accent) 28%, transparent);
  }

  .btn-more {
    background: none;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: var(--font-size-lg);
    line-height: 1;
    padding: 4px 8px;
    min-width: 32px;
    min-height: 32px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-sm);
    transition: all 0.15s ease;
  }

  .btn-more:hover {
    background: var(--hover);
    color: var(--fg-primary);
  }

  .dropdown-menu {
    position: absolute;
    right: 4px;
    top: 32px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-md);
    z-index: 100;
    min-width: 160px;
    display: flex;
    flex-direction: column;
    padding: 4px 0;
  }

  .dropdown-menu-up {
    top: auto;
    bottom: 32px;
  }

  .dropdown-menu button {
    background: none;
    border: none;
    color: var(--fg-primary);
    padding: 8px 12px;
    text-align: left;
    font-size: var(--font-size-xs);
    cursor: pointer;
    width: 100%;
    transition: background-color 0.15s ease;
  }

  .dropdown-menu button:hover {
    background: var(--hover);
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .filter-fields-row {
    display: flex;
    gap: 10px;
    flex: 1.5;
    min-width: 240px;
  }

  .filter-field {
    flex: 1;
    min-width: 0;
  }

  @media (max-width: 640px) {
    .search-input {
      font-size: 16px;
    }

    .toolbar-row {
      flex-direction: column;
      align-items: stretch;
    }

    .search-field {
      width: 100%;
    }

    .filter-fields-row {
      width: 100%;
      min-width: 0;
    }

    .dropdown-menu {
      right: 0;
      min-width: 140px;
    }
  }
</style>
