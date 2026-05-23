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
  let applying = false;

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

  async function applyRules() {
    applying = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      const res = await fetch('/api/service/control?action=restart', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken || ''
        }
      });
      if (!res.ok) throw new Error('Failed to apply configuration');
      showToast('success', $t('rules.apply_success'));
    } catch (e: any) {
      showToast('error', e.message);
    } finally {
      applying = false;
    }
  }

  function goToAddRule() {
    showToast('info', $t('rules.add_tip'));
    window.location.hash = '#/editor';
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
    if (typeUpper.startsWith('DOMAIN')) return 'badge badge-info';
    if (typeUpper.startsWith('IP')) return 'badge badge-warning';
    if (typeUpper === 'GEOIP') return 'badge badge-success';
    if (typeUpper === 'MATCH') return 'badge badge-danger';
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

  onMount(() => {
    if ($capabilities === null || $capabilities.mihomo.reachable) {
      fetchRules();
    }
  });
</script>

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
    <div class="ph-actions">
      <button class="btn btn-secondary" on:click={goToAddRule}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('rules.add')}
      </button>
      <button class="btn btn-primary" on:click={applyRules} disabled={applying}>
        {#if applying}
          <span class="spinner" style="margin-right: 6px;">...</span>
          {$t('app.loading')}
        {:else}
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
          >
          {$t('rules.apply')}
        {/if}
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
          <option value="">{$currentLang === 'ru' ? 'Все таргеты' : 'All targets'}</option>
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
            <th style="width:60px;">#</th>
            <th>{$t('rules.type_col')}</th>
            <th>Payload</th>
            <th>{$t('conn.proxy') || 'Цель'}</th>
          </tr>
        </thead>
        <tbody>
          {#each getFilteredRules() as rule, i}
            <tr>
              <td class="mono" style="color:var(--fg-dim);">{String(i + 1).padStart(3, '0')}</td>
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
            </tr>
          {:else}
            <tr>
              <td
                colspan="4"
                class="empty-cell"
                style="text-align: center; padding: 2rem; color: var(--fg-dim);"
              >
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
</style>
