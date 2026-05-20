<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'
  import { capabilities } from './stores'

  interface Rule {
    type: string
    payload: string
    proxy: string
  }

  let rules: Rule[] = []
  let loading = false
  let error = ''
  let searchQuery = ''
  let typeFilter = ''

  async function fetchRules() {
    loading = true
    error = ''
    
    try {
      const res = await fetch('/api/mihomo/proxy/rules')
      if (!res.ok) throw new Error('Failed to load rules')
      
      const data = await res.json()
      rules = data.rules || []
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  function getFilteredRules(): Rule[] {
    return rules.filter(rule => {
      if (searchQuery) {
        const q = searchQuery.toLowerCase()
        if (!rule.payload.toLowerCase().includes(q) && 
            !rule.proxy.toLowerCase().includes(q)) return false
      }
      if (typeFilter && rule.type !== typeFilter) return false
      return true
    })
  }

  function getUniqueTypes(): string[] {
    const types = new Set(rules.map(r => r.type))
    return Array.from(types).sort()
  }

  function getRuleColor(type: string): string {
    const colors: Record<string, string> = {
      'DOMAIN': '#58a6ff',
      'DOMAIN-SUFFIX': '#a371f7',
      'DOMAIN-KEYWORD': '#3fb950',
      'IP-CIDR': '#d29922',
      'IP-CIDR6': '#d29922',
      'GEOIP': '#f85149',
      'SRC-IP-CIDR': '#ff7b72',
      'DST-PORT': '#79c0ff',
      'SRC-PORT': '#79c0ff',
      'MATCH': '#ff7b72'
    }
    return colors[type] || 'var(--text-secondary)'
  }

  onMount(() => {
    fetchRules()
  })
</script>

<div class="container">
  <h1>{$t('rules.title')}</h1>
  <p class="text-secondary mb-3">{$t('rules.subtitle')}</p>

  {#if $capabilities !== null && !$capabilities.mihomo.reachable}
    <div class="card" style="text-align: center; padding: 40px 20px;">
      <div style="font-size: 48px; margin-bottom: 12px;">🔌</div>
      <h2>{$t('capabilities.mihomo_empty_title')}</h2>
      <p class="text-secondary">{$t('capabilities.mihomo_empty_desc')}</p>
    </div>
  {:else}

  {#if error}
    <div class="alert alert-error mb-2">{error}</div>
  {/if}

  <div class="toolbar mb-2">
    <div class="filters">
      <input 
        type="text" 
        placeholder={$t('rules.search')} 
        bind:value={searchQuery}
        class="filter-input"
      />
      <select bind:value={typeFilter} class="filter-select">
        <option value="">{$t('rules.all_types')}</option>
        {#each getUniqueTypes() as type}
          <option value={type}>{type}</option>
        {/each}
      </select>
    </div>
    <button class="btn btn-secondary" on:click={fetchRules} disabled={loading}>
      {loading ? $t('app.loading') : '🔄 ' + $t('app.refresh')}
    </button>
  </div>

  <div class="stats mb-2">
    <span class="stat">{$t('rules.total', { count: rules.length })}</span>
    <span class="stat">{$t('rules.shown', { count: getFilteredRules().length })}</span>
  </div>

  <div class="table-container">
    <table class="rules-table">
      <thead>
        <tr>
          <th>{$t('rules.type_col')}</th>
          <th>Payload</th>
          <th>{$t('conn.proxy')}</th>
        </tr>
      </thead>
      <tbody>
        {#each getFilteredRules() as rule}
          <tr>
            <td>
              <span class="type-badge" style="background: {getRuleColor(rule.type)}20; color: {getRuleColor(rule.type)}; border-color: {getRuleColor(rule.type)}40">
                {rule.type}
              </span>
            </td>
            <td class="payload">{rule.payload}</td>
            <td>
              <span class="proxy-name">{rule.proxy}</span>
            </td>
          </tr>
        {:else}
          <tr>
            <td colspan="3" class="empty-cell">
              {$t('rules.no_rules')}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
  {/if}
</div>

<style>
  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 1rem;
  }

  .filters {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .filter-input {
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--text);
    font-size: 0.875rem;
    min-width: 200px;
  }

  .filter-select {
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--text);
    font-size: 0.875rem;
  }

  .stats {
    display: flex;
    gap: 1rem;
    font-size: 0.875rem;
    color: var(--text-secondary);
  }

  .table-container {
    overflow-x: auto;
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .rules-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.875rem;
  }

  .rules-table th {
    padding: 0.75rem;
    text-align: left;
    font-weight: 600;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border);
    background: var(--bg);
  }

  .rules-table td {
    padding: 0.75rem;
    border-bottom: 1px solid var(--border-light, rgba(0,0,0,0.05));
  }

  .rules-table tr:hover {
    background: var(--hover);
  }

  .type-badge {
    display: inline-block;
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
    font-size: 0.75rem;
    font-weight: 600;
    border: 1px solid;
  }

  .payload {
    font-family: monospace;
    font-size: 0.8125rem;
  }

  .proxy-name {
    font-weight: 500;
  }

  .empty-cell {
    text-align: center;
    color: var(--text-secondary);
    padding: 2rem;
  }
</style>
