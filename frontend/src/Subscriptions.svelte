<script lang="ts">
  import { onMount } from 'svelte';
  import { t, currentLang } from './i18n';
  import { showConfirm } from './stores';

  export let onSwitchTab: (tab: string) => void = () => {};

  interface Subscription {
    id: string;
    name: string;
    url: string;
    tag_prefix: string;
    interval: number;
    last_update: string;
    enabled: boolean;
    type?: string;
    filter_name?: string;
    filter_type?: string;
    filter_transport?: string;
    proxy_count?: number;
    last_error?: string;
    upload?: number;
    download?: number;
    total?: number;
    rule_count?: number;
  }

  function getSubTypeBadge(sub: Subscription): string {
    if (sub.type === 'mihomo') return 'clash · YAML';
    return 'xray · JSON';
  }

  function formatTraffic(bytes: number): string {
    const gb = bytes / (1024 * 1024 * 1024);
    if (gb >= 1) return `${gb.toFixed(1)} GB`;
    const mb = bytes / (1024 * 1024);
    return `${mb.toFixed(1)} MB`;
  }

  function formatTrafficUsage(upload?: number, download?: number, total?: number): string {
    const used = (upload || 0) + (download || 0);
    if (used === 0 && (!total || total === 0)) return '—';
    const usedStr = formatTraffic(used);
    if (total && total > 0) {
      return `${usedStr} / ${formatTraffic(total)}`;
    }
    return usedStr;
  }

  let subscriptions: Subscription[] = [];
  let loading = false;
  let refreshLoading: Record<string, boolean> = {};
  let showAddModal = false;
  let editingSub: Subscription | null = null;
  let activeDropdownId: string | null = null;

  // Form fields
  let formName = '';
  let formURL = '';
  let formTagPrefix = '';
  let formInterval = 24;
  let formFilterName = '';
  let formFilterType = '';
  let formEnabled = true;

  async function loadSubscriptions() {
    loading = true;
    try {
      const res = await fetch('/api/subscriptions');
      if (res.ok) {
        const envelope = await res.json();
        subscriptions = Array.isArray(envelope) ? envelope : (envelope.data ?? []);
      }
    } catch (e) {
      // ignore
    } finally {
      loading = false;
    }
  }

  async function refreshSubscription(id: string) {
    refreshLoading[id] = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      await fetch(`/api/subscriptions/refresh?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      await loadSubscriptions();
    } catch (e) {
      // ignore
    } finally {
      refreshLoading[id] = false;
    }
  }

  async function refreshAll() {
    loading = true;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      await fetch('/api/subscriptions/refresh-all', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      await loadSubscriptions();
    } catch (e) {
      // ignore
    } finally {
      loading = false;
    }
  }

  async function saveSubscription() {
    const csrfToken = localStorage.getItem('csrf_token');
    const sub = {
      name: formName,
      url: formURL,
      tag_prefix: formTagPrefix,
      interval: formInterval,
      enabled: formEnabled,
      filter_name: formFilterName || undefined,
      filter_type: formFilterType || undefined
    };

    try {
      if (editingSub) {
        await fetch(`/api/subscriptions/update?id=${editingSub.id}`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken || ''
          },
          body: JSON.stringify(sub)
        });
      } else {
        await fetch('/api/subscriptions/add', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken || ''
          },
          body: JSON.stringify(sub)
        });
      }
      closeModal();
      await loadSubscriptions();
    } catch (e) {
      // ignore
    }
  }

  async function deleteSubscription(id: string) {
    if (!(await showConfirm($t('app.confirm'), $t('subscr.delete_confirm')))) return;
    try {
      const csrfToken = localStorage.getItem('csrf_token');
      await fetch(`/api/subscriptions/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      });
      await loadSubscriptions();
    } catch (e) {
      // ignore
    }
  }

  function openAddModal() {
    editingSub = null;
    formName = '';
    formURL = '';
    formTagPrefix = '';
    formInterval = 24;
    formFilterName = '';
    formFilterType = '';
    formEnabled = true;
    showAddModal = true;
  }

  function openEditModal(sub: Subscription) {
    editingSub = sub;
    formName = sub.name;
    formURL = sub.url;
    formTagPrefix = sub.tag_prefix || '';
    formInterval = sub.interval;
    formFilterName = sub.filter_name || '';
    formFilterType = sub.filter_type || '';
    formEnabled = sub.enabled;
    showAddModal = true;
  }

  function closeModal() {
    showAddModal = false;
    editingSub = null;
  }

  function formatDate(dateStr: string): string {
    if (!dateStr || dateStr.startsWith('0001')) return '—';
    const d = new Date(dateStr);
    return d.toLocaleString($currentLang === 'ru' ? 'ru-RU' : 'en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  }

  function toggleDropdown(id: string) {
    activeDropdownId = activeDropdownId === id ? null : id;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      closeModal();
      activeDropdownId = null;
    }
  }

  function handleClickOutside(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (!target.closest('.dropdown-container')) {
      activeDropdownId = null;
    }
  }

  onMount(() => {
    loadSubscriptions();
    window.addEventListener('click', handleClickOutside);
    window.addEventListener('keydown', handleKeydown);
    return () => {
      window.removeEventListener('click', handleClickOutside);
      window.removeEventListener('keydown', handleKeydown);
    };
  });

  $: stats = (() => {
    const totalNodes = subscriptions.reduce((sum, s) => sum + (s.proxy_count || 0), 0);
    let minNext = Infinity;
    subscriptions.forEach((s) => {
      if (s.enabled && s.last_update && !s.last_update.startsWith('0001')) {
        const next = new Date(s.last_update).getTime() + s.interval * 3600 * 1000;
        const diff = next - Date.now();
        if (diff > 0 && diff < minNext) {
          minNext = diff;
        }
      }
    });
    let nextStr = '—';
    if (minNext !== Infinity) {
      const diffHours = Math.floor(minNext / (3600 * 1000));
      const diffMins = Math.floor((minNext % (3600 * 1000)) / (60 * 1000));
      nextStr = `${diffHours}ч ${diffMins}м`;
    }
    return {
      total: subscriptions.length,
      nodes: totalNodes,
      next: nextStr
    };
  })();
</script>

<div class="container">
  <div class="page-head">
    <div>
      <div class="crumbs">
        {$t('nav.group_proxy')} <span style="color:var(--fg-faint);margin:0 6px;">/</span>
        {$t('nav.subscriptions')}
      </div>
      <h1>{$t('subscr.title')}</h1>
      <p class="sub">{$t('subscr.subtitle')}</p>
    </div>
    <div class="ph-actions">
      <button class="btn btn-secondary" on:click={refreshAll} disabled={loading}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></svg
        >
        {$t('subscr.refresh_all')}
      </button>
      <button class="btn btn-primary" on:click={openAddModal}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('subscr.add')}
      </button>
    </div>
  </div>

  {#if subscriptions.length === 0}
    <div
      class="card text-center"
      style="padding: 3rem; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1rem;"
    >
      <p style="color: var(--fg-secondary); margin: 0;">{$t('subscr.empty')}</p>
      <button class="btn btn-primary" on:click={openAddModal}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="margin-right: 6px;"><path d="M12 5v14M5 12h14" /></svg
        >
        {$t('subscr.add_first')}
      </button>
    </div>
  {:else}
    <div class="stats mb-2">
      <span class="stat"
        ><b>{stats.total}</b> {$currentLang === 'ru' ? 'подписки' : 'subscriptions'}</span
      >
      <span class="stat"
        ><b>{stats.nodes}</b> {$currentLang === 'ru' ? 'узлов суммарно' : 'nodes total'}</span
      >
      {#if stats.next !== '—'}
        <span class="stat"
          >{$currentLang === 'ru' ? 'след. обновление через' : 'next update in'}
          <b>{stats.next}</b></span
        >
      {/if}
    </div>

    <div class="subscriptions-list">
      {#each subscriptions as sub}
        <div class="card sub-card">
          <div class="sub-header-row">
            <div class="sub-icon-wrapper">
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                ><path d="M4 11a9 9 0 0 1 9 9" /><path d="M4 4a16 16 0 0 1 16 16" /><circle
                  cx="5"
                  cy="19"
                  r="1.5"
                  fill="currentColor"
                /></svg
              >
            </div>
            <div class="sub-title-wrapper">
              <div class="sub-title-line">
                {sub.name}
                {#if sub.last_error}
                  <span class="status-badge error-badge" title={sub.last_error}>
                    {$currentLang === 'ru' ? 'ошибка' : 'error'}
                  </span>
                {:else if sub.enabled}
                  <span class="status-badge active"
                    ><span class="status-dot success" style="margin:0;"></span>{$currentLang ===
                    'ru'
                      ? 'активна'
                      : 'active'}</span
                  >
                {:else}
                  <span class="status-badge stopped"
                    ><span class="status-dot error" style="margin:0;"></span>{$currentLang === 'ru'
                      ? 'выключена'
                      : 'disabled'}</span
                  >
                {/if}
              </div>
              <div class="sub-url-line">
                <span class="sub-type-badge">{getSubTypeBadge(sub)}</span>
                {sub.url}
              </div>
            </div>
            <div class="sub-actions-wrapper">
              <button
                class="btn {sub.last_error ? 'btn-danger-outline' : 'btn-secondary'} btn-sm"
                on:click={() => refreshSubscription(sub.id)}
                disabled={refreshLoading[sub.id]}
              >
                {#if refreshLoading[sub.id]}
                  <span class="spinner" style="margin-right: 4px;">...</span>
                  {$t('app.loading')}
                {:else if sub.last_error}
                  {$currentLang === 'ru' ? 'Повторить' : 'Retry'}
                {:else}
                  {$t('subscr.refresh')}
                {/if}
              </button>

              <div class="dropdown-container">
                <button
                  class="btn btn-secondary action-btn-dots"
                  on:click={() => toggleDropdown(sub.id)}>⋯</button
                >
                {#if activeDropdownId === sub.id}
                  <div class="dropdown-menu">
                    <button
                      on:click={() => {
                        openEditModal(sub);
                        activeDropdownId = null;
                      }}>{$t('app.edit')}</button
                    >
                    <button
                      on:click={() => {
                        deleteSubscription(sub.id);
                        activeDropdownId = null;
                      }}
                      class="delete-action">{$t('app.delete')}</button
                    >
                  </div>
                {/if}
              </div>
            </div>
          </div>

          <div class="sub-meta-grid">
            <div class="meta-column">
              <div class="meta-label">{$currentLang === 'ru' ? 'УЗЛЫ' : 'NODES'}</div>
              <div class="meta-value">{sub.proxy_count || 0}</div>
            </div>
            <div class="meta-column">
              <div class="meta-label">{$currentLang === 'ru' ? 'ПРАВИЛ' : 'RULES'}</div>
              <div class="meta-value">{sub.type === 'mihomo' ? (sub.rule_count || 0) : '—'}</div>
            </div>
            <div class="meta-column">
              <div class="meta-label">{$currentLang === 'ru' ? 'ТРАФИК' : 'TRAFFIC'}</div>
              <div class="meta-value">{formatTrafficUsage(sub.upload, sub.download, sub.total)}</div>
            </div>
            <div class="meta-column">
              <div class="meta-label">{$currentLang === 'ru' ? 'ОБНОВЛЕНО' : 'UPDATED'}</div>
              <div class="meta-value">{formatDate(sub.last_update)}</div>
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if showAddModal}
  <div
    class="modal-overlay"
    role="button"
    tabindex="0"
    on:click={closeModal}
    on:keydown={handleKeydown}
  >
    <div class="modal-card" role="presentation" on:click|stopPropagation>
      <div class="modal-card-header">
        <h2>{editingSub ? $t('subscr.edit_title') : $t('subscr.add_title')}</h2>
        <button class="modal-close-btn" on:click={closeModal}>&times;</button>
      </div>
      <div class="modal-card-body">
        <div class="form-group">
          <label for="form-name" class="form-label">{$t('subscr.name')}</label>
          <input
            id="form-name"
            type="text"
            class="input"
            bind:value={formName}
            placeholder={$t('subscr.name_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="form-url" class="form-label">{$t('subscr.url')}</label>
          <input
            id="form-url"
            type="text"
            class="input"
            bind:value={formURL}
            placeholder="https://..."
          />
        </div>

        <div class="form-group">
          <label for="form-tag-prefix" class="form-label">{$t('subscr.tag_prefix')}</label>
          <input
            id="form-tag-prefix"
            type="text"
            class="input"
            bind:value={formTagPrefix}
            placeholder={$t('subscr.tag_prefix_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="form-interval" class="form-label"
            >{$t('subscr.interval')} ({$currentLang === 'ru' ? 'часов' : 'hours'})</label
          >
          <input
            id="form-interval"
            type="number"
            class="input"
            bind:value={formInterval}
            min="1"
            max="168"
          />
        </div>

        <div class="form-group">
          <label for="form-filter-name" class="form-label">{$t('subscr.filter_name')}</label>
          <input
            id="form-filter-name"
            type="text"
            class="input"
            bind:value={formFilterName}
            placeholder={$t('subscr.filter_placeholder')}
          />
        </div>

        <div class="form-group">
          <label for="form-filter-type" class="form-label">{$t('subscr.filter_type')}</label>
          <input
            id="form-filter-type"
            type="text"
            class="input"
            bind:value={formFilterType}
            placeholder="vmess, vless, trojan..."
          />
        </div>

        <div class="form-group-checkbox">
          <label class="toggle-switch">
            <input type="checkbox" id="enabled" bind:checked={formEnabled} />
            <span class="toggle-slider"></span>
          </label>
          <label for="enabled" class="checkbox-label">{$t('subscr.enabled')}</label>
        </div>
      </div>
      <div class="modal-card-footer">
        <button class="btn btn-secondary" on:click={closeModal}>{$t('app.cancel')}</button>
        <button class="btn btn-primary" on:click={saveSubscription}>{$t('app.save')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .subscriptions-list {
    display: grid;
    grid-template-columns: 1fr;
    gap: 14px;
  }

  .sub-card {
    padding: 0;
    overflow: hidden;
  }

  .sub-header-row {
    padding: 18px 22px;
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: 16px;
    align-items: center;
  }

  .sub-icon-wrapper {
    width: 42px;
    height: 42px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent);
    border: 1px solid var(--accent-line);
  }

  .sub-title-wrapper {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .sub-title-line {
    display: flex;
    align-items: center;
    gap: 10px;
    font-weight: 700;
    color: var(--fg-primary);
    font-size: 14px;
  }

  .sub-url-line {
    color: var(--fg-dim);
    font-size: 12px;
    font-family: var(--font-family-mono);
    word-break: break-all;
  }

  .sub-actions-wrapper {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .btn-sm {
    padding: 6px 12px;
    font-size: 12px;
  }

  .sub-meta-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    border-top: 1px solid var(--border);
    background: rgba(0, 0, 0, 0.05);
  }

  .meta-column {
    padding: 12px 18px;
    border-right: 1px solid var(--border);
  }

  .meta-column:last-child {
    border-right: none;
  }

  .meta-label {
    font-size: 10.5px;
    color: var(--fg-dim);
    letter-spacing: 0.18em;
    text-transform: uppercase;
    font-weight: 700;
  }

  .meta-value {
    font-family: var(--font-family-mono);
    font-size: 14px;
    color: var(--fg-primary);
    margin-top: 4px;
  }

  /* Dropdown Styles */
  .dropdown-container {
    position: relative;
    display: inline-block;
  }

  .action-btn-dots {
    padding: 6px 10px;
    font-size: 14px;
    line-height: 1;
  }

  .dropdown-menu {
    position: absolute;
    right: 0;
    top: 100%;
    margin-top: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: 0 10px 25px rgba(0, 0, 0, 0.3);
    z-index: 100;
    min-width: 140px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .dropdown-menu button {
    background: none;
    border: none;
    padding: 10px 14px;
    text-align: left;
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    width: 100%;
    transition: background var(--transition-fast);
  }

  .dropdown-menu button:hover {
    background: var(--hover);
  }

  .dropdown-menu button.delete-action {
    color: var(--danger);
  }

  .dropdown-menu button.delete-action:hover {
    background: rgba(235, 94, 85, 0.1);
  }

  /* Modal Styles */
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 20px;
  }

  .modal-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: 100%;
    max-width: 520px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: 90vh;
    animation: modal-anim 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes modal-anim {
    from {
      transform: scale(0.95) translateY(10px);
      opacity: 0;
    }
    to {
      transform: scale(1) translateY(0);
      opacity: 1;
    }
  }

  .modal-card-header {
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-card-header h2 {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .modal-close-btn {
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 24px;
    cursor: pointer;
    line-height: 1;
    padding: 4px;
  }

  .modal-close-btn:hover {
    color: var(--fg-primary);
  }

  .modal-card-body {
    padding: 24px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .form-group-checkbox {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 4px;
  }

  .checkbox-label {
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    user-select: none;
  }

  .modal-card-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  :global(.error-badge) {
    background: rgba(239, 68, 68, 0.12);
    color: #ef4444;
    border: 1px solid rgba(239, 68, 68, 0.3);
    font-size: 11px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 4px;
  }

  :global(.btn-danger-outline) {
    background: rgba(239, 68, 68, 0.08);
    color: #ef4444;
    border: 1px solid rgba(239, 68, 68, 0.35);
  }
  :global(.btn-danger-outline:hover) {
    background: rgba(239, 68, 68, 0.16);
  }

  :global(.sub-type-badge) {
    display: inline-block;
    font-size: 10.5px;
    font-weight: 600;
    font-family: var(--font-family-mono);
    padding: 1px 6px;
    border-radius: 3px;
    background: rgba(41, 194, 240, 0.08);
    color: var(--accent);
    border: 1px solid rgba(41, 194, 240, 0.2);
    margin-right: 6px;
    vertical-align: middle;
  }
</style>
